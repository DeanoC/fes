// SPDX-License-Identifier: GPL-2.0
/* Actual DMA API/file/VMA tests on a KUnit device, never a DE25 binding. */
#include <kunit/device.h>
#include <kunit/test.h>
#include <linux/completion.h>
#include <linux/file.h>
#include <linux/kthread.h>
#include <linux/mman.h>
#include <linux/mm.h>
#include <linux/sched/mm.h>
#include <linux/uaccess.h>

#include "de25_dma_internal.h"

#define TEST_ADDRESS 0x10000000UL

/* Scripted register device, not a second allocation/lifetime implementation.
 * The driver under test writes its real descriptor and consumes frozen tuples.
 */
struct test_descriptor {
	u64 dma, generation;
	u32 span, sequence, slot;
};
struct test_transport {
	u64 epoch;
	bool epoch_unknown, lose_epoch_after_start, freeze_serial, corrupt_serial;
	int start_error, stop_error, drain_error;
	unsigned int starts, stops, drains, clears, snapshots, writes, barriers;
	u32 regs[32], live[13], frozen[13], serial;
	u32 trace_offset[128], trace_value[128];
	unsigned int trace_count;
	struct test_descriptor descriptor;
	unsigned long revoked_address;
	bool writes_revoked_before_stop;
};

struct dma_fixture {
	struct device *dev;
	struct file *file;
	struct test_transport transport;
	struct fes_de25_dma_test_observer observer;
};

static u64 transport_epoch(void *context)
{
	struct test_transport *transport = context;

	return transport->epoch_unknown ? 0 : transport->epoch;
}

static int transport_read(void *context, u32 offset, u32 *value)
{
	struct test_transport *t = context;

	if (offset == FES_DE25_SCANOUT_ID_OFFSET)
		*value = FES_DE25_SCANOUT_IDENTITY;
	else if (offset == FES_DE25_SCANOUT_VERSION_OFFSET)
		*value = FES_DE25_SCANOUT_VERSION;
	else if (offset == FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET)
		*value = t->serial;
	else if (offset >= FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET &&
		 offset <= FES_DE25_SCANOUT_SNAPSHOT_GRANT_SPAN_OFFSET) {
		*value = t->frozen[(offset - FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET) / 4];
		if (t->corrupt_serial && offset == FES_DE25_SCANOUT_SNAPSHOT_GRANT_SPAN_OFFSET)
			t->serial++;
	} else
		return -EINVAL;
	return 0;
}

static int transport_write(void *context, u32 offset, u32 value)
{
	struct test_transport *t = context;
	u32 test_value = 0x12345678;

	t->writes++;
	if (t->trace_count < ARRAY_SIZE(t->trace_offset)) {
		t->trace_offset[t->trace_count] = offset;
		t->trace_value[t->trace_count++] = value;
	}
	if (offset != FES_DE25_SCANOUT_COMMAND_OFFSET) {
		if (offset < FES_DE25_SCANOUT_GRANT_LO_OFFSET || offset > FES_DE25_SCANOUT_SLOT_OFFSET)
			return -EINVAL;
		t->regs[offset / 4] = value;
		return 0;
	}
	if (value == FES_DE25_SCANOUT_COMMAND_SNAPSHOT) {
		t->snapshots++;
		/* Explicit hardware tuple, atomically copied by CMD SNAPSHOT. */
		t->live[0] = t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4];
		t->live[1] = lower_32_bits(t->descriptor.generation);
		t->live[2] = upper_32_bits(t->descriptor.generation);
		memcpy(t->frozen, t->live, sizeof(t->frozen));
		if (!t->freeze_serial)
			t->serial++;
		return 0;
	}
	if (value == FES_DE25_SCANOUT_COMMAND_SUBMIT) {
		t->starts++;
		t->descriptor.dma = (u64)t->regs[FES_DE25_SCANOUT_GRANT_HI_OFFSET / 4] << 32 |
			t->regs[FES_DE25_SCANOUT_GRANT_LO_OFFSET / 4];
		t->descriptor.generation = (u64)t->regs[FES_DE25_SCANOUT_GENERATION_HI_OFFSET / 4] << 32 |
			t->regs[FES_DE25_SCANOUT_GENERATION_LO_OFFSET / 4];
		t->descriptor.span = t->regs[FES_DE25_SCANOUT_GRANT_SPAN_OFFSET / 4];
		t->descriptor.sequence = t->regs[FES_DE25_SCANOUT_SEQUENCE_OFFSET / 4];
		t->descriptor.slot = t->regs[FES_DE25_SCANOUT_SLOT_OFFSET / 4];
		t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_CONFIGURED |
			FES_DE25_SCANOUT_STATUS_RUNNING | FES_DE25_SCANOUT_STATUS_PENDING;
		t->live[3] = t->descriptor.sequence;
		t->live[6] = (t->live[6] & 0x101U) | t->descriptor.slot << 16;
		t->live[10] = lower_32_bits(t->descriptor.dma);
		t->live[11] = upper_32_bits(t->descriptor.dma);
		t->live[12] = t->descriptor.span;
		if (t->lose_epoch_after_start)
			t->epoch_unknown = true;
		return t->start_error;
	}
	if (value == FES_DE25_SCANOUT_COMMAND_STOP) {
		t->stops++;
		if (t->revoked_address)
			t->writes_revoked_before_stop = copy_to_user((void __user *)t->revoked_address,
				&test_value, sizeof(test_value)) != 0;
		if (t->stop_error)
			return t->stop_error;
		t->drains++;
		t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_CONFIGURED |
			(t->drain_error ? 0 : FES_DE25_SCANOUT_STATUS_DRAINED);
		t->live[9] = t->drain_error ? 1 : 0;
		return 0;
	}
	if (value == FES_DE25_SCANOUT_COMMAND_CLEAR) {
		t->clears++;
		memset(&t->descriptor, 0, sizeof(t->descriptor));
		memset(t->live, 0, sizeof(t->live));
		t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_DRAINED;
		return 0;
	}
	return -EINVAL;
}

static int transport_barrier(void *context)
{
	struct test_transport *t = context;

	t->barriers++;
	if (t->trace_count < ARRAY_SIZE(t->trace_offset)) {
		t->trace_offset[t->trace_count] = ~0U;
		t->trace_value[t->trace_count++] = 0;
	}
	return 0;
}

static void transport_display(struct test_transport *t)
{
	t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] =
		FES_DE25_SCANOUT_STATUS_CONFIGURED | FES_DE25_SCANOUT_STATUS_RUNNING;
	t->live[4] = t->descriptor.sequence;
	t->live[5] = t->descriptor.sequence;
	t->live[6] = t->descriptor.slot | t->descriptor.slot << 8;
}

static int poll_file(struct file *file, struct fes_de25_dma_info *info)
{
	fes_de25_dma_info(file, info);
	return fes_de25_dma_poll(file, info);
}

static void fixture_cleanup(void *context)
{
	struct dma_fixture *fixture = context;

	if (fixture->file) {
		/* A controlled test backend can now prove the same operation drained.
		 * This is not a production qualification or userspace override.
		 */
		fixture->transport.stop_error = 0;
		fixture->transport.drain_error = 0;
		fixture->transport.freeze_serial = false;
		fixture->transport.corrupt_serial = false;
		fixture->transport.epoch_unknown = false;
		fixture->transport.epoch = 1;
		fixture->observer.mmap_entered = NULL;
		fixture->observer.revoked = NULL;
		__fput_sync(fixture->file);
	}
}

static int fixture_init(struct kunit *test)
{
	struct dma_fixture *fixture;
	struct fes_de25_dma_backend backend;

	fixture = kunit_kzalloc(test, sizeof(*fixture), GFP_KERNEL);
	if (!fixture)
		return -ENOMEM;
	fixture->dev = kunit_device_register(test, "fes-dma-test");
	if (IS_ERR(fixture->dev))
		return PTR_ERR(fixture->dev);
	fixture->transport.epoch = 1;
	fixture->transport.regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_DRAINED;
	backend.epoch = transport_epoch;
	backend.read32 = transport_read;
	backend.write32 = transport_write;
	backend.io_fence = transport_barrier;
	backend.context = &fixture->transport;
	fixture->file = fes_de25_dma_create(fixture->dev, &backend);
	if (IS_ERR(fixture->file))
		return PTR_ERR(fixture->file);
	fes_de25_dma_test_observe(fixture->file, &fixture->observer);
	test->priv = fixture;
	return kunit_add_action_or_reset(test, fixture_cleanup, fixture);
}

static struct fes_de25_dma_publish publish_for(struct file *file)
{
	struct fes_de25_dma_info info;
	struct fes_de25_dma_publish publish = { };

	fes_de25_dma_info(file, &info);
	publish.generation = info.generation;
	publish.version = FES_DE25_DMA_ABI_VERSION;
	publish.sequence = 1;
	publish.slot = 1;
	return publish;
}

static void binding_stays_closed(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_info info;

	KUNIT_EXPECT_EQ(test, fes_de25_dma_binding(fixture->dev), -EOPNOTSUPP);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_NE(test, info.generation, 0ULL);
	KUNIT_EXPECT_EQ(test, info.allocation_bytes, FES_DE25_DMA_ALLOCATION_BYTES);
	KUNIT_EXPECT_EQ(test, info.slot_bytes, FES_DE25_DMA_SLOT_BYTES);
	KUNIT_EXPECT_EQ(test, info.state, 0U);
	KUNIT_EXPECT_EQ(test, fixture->transport.starts, 0U);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, 0xdead, 0), -ENOTTY);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_PUBLISH, 0), -EFAULT);
}

static void rejects_bad_publish_before_effects(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_publish valid = publish_for(fixture->file), changed;

	changed = valid; changed.generation++;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &changed), -EINVAL);
	changed = valid; changed.sequence = 0;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &changed), -EINVAL);
	changed = valid; changed.slot = 2;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &changed), -EINVAL);
	changed = valid; changed.version = 1;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &changed), -EINVAL);
	changed = valid; changed.slot = 0;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &changed), -EINVAL);
	changed = valid; changed.reserved = 1;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &changed), -EINVAL);
	fixture->transport.epoch = 2;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &valid), -ESTALE);
	KUNIT_EXPECT_EQ(test, fixture->transport.starts, 0U);
	KUNIT_EXPECT_EQ(test, fixture->transport.stops, 0U);
	/* No DMA was exposed, so the stale unsubmitted allocation is safe to free. */
}

static void kernel_owns_descriptor_and_drain_lifetime(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;
	struct test_descriptor *descriptor = &fixture->transport.descriptor;
	struct fes_de25_dma_backend competing = {
		.epoch = transport_epoch, .read32 = transport_read,
		.write32 = transport_write, .io_fence = transport_barrier,
		.context = &fixture->transport,
	};
	struct file *other;

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	KUNIT_EXPECT_EQ(test, descriptor->span, FES_DE25_DMA_ALLOCATION_BYTES);
	KUNIT_EXPECT_EQ(test, descriptor->slot, 1U);
	KUNIT_EXPECT_EQ(test, descriptor->generation, publish.generation);
	KUNIT_EXPECT_EQ(test, descriptor->sequence, publish.sequence);
	KUNIT_EXPECT_TRUE(test, IS_ALIGNED(descriptor->dma, 32));
	KUNIT_EXPECT_LE(test, (u64)descriptor->dma + descriptor->span, 1ULL << 32);
	/* The constructor must not clear a configured engine to steal ownership. */
	other = fes_de25_dma_create(fixture->dev, &competing);
	KUNIT_EXPECT_TRUE(test, IS_ERR(other));
	if (IS_ERR(other))
		KUNIT_EXPECT_EQ(test, PTR_ERR(other), (long)-EBUSY);
	else
		__fput_sync(other);
	KUNIT_EXPECT_EQ(test, fixture->transport.clears, 0U);
	publish.sequence++;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EBUSY);
	KUNIT_EXPECT_EQ(test, fixture->transport.starts, 1U);
	fixture->transport.drain_error = -ETIMEDOUT;
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_REVOKE, 0), -EAGAIN);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_REVOKED | FES_DE25_DMA_EXPOSED | FES_DE25_DMA_UNCERTAIN | FES_DE25_DMA_PENDING);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	KUNIT_EXPECT_EQ(test, fixture->transport.stops, 1U);
	KUNIT_EXPECT_EQ(test, fixture->transport.drains, 1U);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EBUSY);
	/* Last close may free only after the backend actually proves drain. */
	fixture->transport.drain_error = 0;
	__fput_sync(fixture->file);
	fixture->file = NULL;
	KUNIT_EXPECT_EQ(test, fixture->transport.drains, 2U);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 1U);
}

static void failed_start_is_held_until_drain(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;

	fixture->transport.start_error = -EIO;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EIO);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_EXPOSED | FES_DE25_DMA_UNCERTAIN | FES_DE25_DMA_PENDING);
	fixture->transport.stop_error = -EIO;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), -EIO);
	KUNIT_EXPECT_EQ(test, fixture->transport.drains, 0U);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	fixture->transport.stop_error = 0;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_REVOKED);
}

static void unreadable_epoch_retains_exposed_storage(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;

	fixture->transport.lose_epoch_after_start = true;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -ESTALE);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), -ESTALE);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_REVOKED | FES_DE25_DMA_EXPOSED | FES_DE25_DMA_UNCERTAIN | FES_DE25_DMA_PENDING);
	KUNIT_EXPECT_EQ(test, fixture->transport.stops, 0U);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	/* Restore visibility of the ORIGINAL epoch, not a reset/epoch override. */
	fixture->transport.epoch_unknown = false;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
}

static void acceptance_is_not_display_and_slots_alternate(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	KUNIT_ASSERT_EQ(test, poll_file(fixture->file, &info), 0);
	KUNIT_EXPECT_EQ(test, info.accepted_sequence, 1U);
	KUNIT_EXPECT_EQ(test, info.displayed_sequence, 0U);
	KUNIT_EXPECT_EQ(test, info.displayed_slot, 0U);
	KUNIT_EXPECT_EQ(test, info.pending_slot, 1U);
	KUNIT_EXPECT_EQ(test, info.back_slot, FES_DE25_DMA_NO_SLOT);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_EXPOSED | FES_DE25_DMA_PENDING);
	publish.sequence = 2; publish.slot = 0;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EBUSY);
	/* Fetch switching to the new slot is still not full-frame completion. */
	t->live[5] = 1; t->live[6] |= 1;
	KUNIT_ASSERT_EQ(test, poll_file(fixture->file, &info), 0);
	KUNIT_EXPECT_EQ(test, info.back_slot, FES_DE25_DMA_NO_SLOT);
	transport_display(t);
	KUNIT_ASSERT_EQ(test, poll_file(fixture->file, &info), 0);
	KUNIT_EXPECT_EQ(test, info.displayed_sequence, 1U);
	KUNIT_EXPECT_EQ(test, info.displayed_slot, 1U);
	KUNIT_EXPECT_EQ(test, info.back_slot, 0U);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_EXPOSED);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	publish.slot = 1;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EINVAL);
	publish.slot = 0;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	KUNIT_ASSERT_EQ(test, poll_file(fixture->file, &info), 0);
	/* The old displayed acknowledgement must not release the next slot. */
	KUNIT_EXPECT_EQ(test, info.displayed_sequence, 1U);
	KUNIT_EXPECT_EQ(test, info.pending_sequence, 2U);
	KUNIT_EXPECT_EQ(test, info.back_slot, FES_DE25_DMA_NO_SLOT);
	transport_display(t);
	KUNIT_ASSERT_EQ(test, poll_file(fixture->file, &info), 0);
	KUNIT_EXPECT_EQ(test, info.displayed_slot, 0U);
	KUNIT_EXPECT_EQ(test, info.back_slot, 1U);
	KUNIT_EXPECT_EQ(test, t->starts, 2U);
	KUNIT_EXPECT_EQ(test, t->stops, 0U);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
}

static void ordered_descriptor_precedes_first_submit(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	const u32 offsets[] = { FES_DE25_SCANOUT_GRANT_LO_OFFSET, FES_DE25_SCANOUT_GRANT_HI_OFFSET,
		FES_DE25_SCANOUT_GRANT_SPAN_OFFSET, FES_DE25_SCANOUT_GENERATION_LO_OFFSET,
		FES_DE25_SCANOUT_GENERATION_HI_OFFSET, FES_DE25_SCANOUT_SEQUENCE_OFFSET,
		FES_DE25_SCANOUT_SLOT_OFFSET, ~0U, FES_DE25_SCANOUT_COMMAND_OFFSET, ~0U };
	unsigned int i;

	t->trace_count = 0;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	KUNIT_ASSERT_GE(test, t->trace_count, (unsigned int)ARRAY_SIZE(offsets));
	for (i = 0; i < ARRAY_SIZE(offsets); i++)
		KUNIT_EXPECT_EQ(test, t->trace_offset[i], offsets[i]);
	KUNIT_EXPECT_EQ(test, t->trace_value[8], FES_DE25_SCANOUT_COMMAND_SUBMIT);
	KUNIT_EXPECT_EQ(test, t->trace_value[6], 1U);
	KUNIT_EXPECT_EQ(test, t->descriptor.generation, publish.generation);
}

static void stale_generation_and_reset_cannot_prove_drain(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;
	u64 generation;

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	generation = t->descriptor.generation;
	/* Same low32 bits still fail when the high32 generation differs. */
	t->descriptor.generation ^= 1ULL << 32;
	KUNIT_EXPECT_EQ(test, poll_file(fixture->file, &info), -ESTALE);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), -ESTALE);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	/* Reset can make idle/drained true, but destroys the old ownership proof. */
	t->descriptor.generation = 0;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), -ESTALE);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	/* Test fixture restores its original scripted device, never a user flag. */
	t->descriptor.generation = generation;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
}

static void mismatched_completion_never_releases_slot(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	/* A new displayed tuple with the old slot still current cannot free it. */
	t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] &= ~FES_DE25_SCANOUT_STATUS_PENDING;
	t->live[4] = publish.sequence;
	t->live[6] = 1U << FES_DE25_SCANOUT_DISPLAYED_SLOT_SHIFT;
	KUNIT_EXPECT_EQ(test, poll_file(fixture->file, &info), -EPROTO);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.displayed_sequence, 0U);
	KUNIT_EXPECT_EQ(test, info.back_slot, FES_DE25_DMA_NO_SLOT);
	KUNIT_EXPECT_NE(test, info.state & FES_DE25_DMA_EXPOSED, 0U);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
}

static void faults_never_release_pending_slot(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	/* Even a matching displayed counter is not success when frame errored. */
	transport_display(t);
	t->live[7] = FES_DE25_SCANOUT_ERROR_UNDERFLOW;
	t->live[8] = 1;
	t->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] |= FES_DE25_SCANOUT_STATUS_FAULT;
	KUNIT_EXPECT_EQ(test, poll_file(fixture->file, &info), -EIO);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.displayed_sequence, 0U);
	KUNIT_EXPECT_EQ(test, info.back_slot, FES_DE25_DMA_NO_SLOT);
	KUNIT_EXPECT_NE(test, info.state & FES_DE25_DMA_PENDING, 0U);
	KUNIT_EXPECT_NE(test, info.state & FES_DE25_DMA_UNCERTAIN, 0U);
	KUNIT_EXPECT_EQ(test, info.hardware_errors, FES_DE25_SCANOUT_ERROR_UNDERFLOW);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
}

static void snapshot_handshake_is_bounded_and_consistent(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;

	/* Serial is modular independently of the never-wrapping lease identity. */
	t->serial = U32_MAX;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	t->freeze_serial = true;
	KUNIT_EXPECT_EQ(test, poll_file(fixture->file, &info), -ETIMEDOUT);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	t->freeze_serial = false;
	t->corrupt_serial = true;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), -EPROTO);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	t->corrupt_serial = false;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
}

static void sequence_and_generation_never_wrap(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;
	atomic64_t counter = ATOMIC64_INIT(S64_MAX);

	KUNIT_EXPECT_EQ(test, fes_de25_dma_test_next_generation(&counter), 1ULL << 63);
	atomic64_set(&counter, -2);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_test_next_generation(&counter), U64_MAX);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_test_next_generation(&counter), 0ULL);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_test_next_generation(&counter), 0ULL);
	publish.sequence = U32_MAX;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	transport_display(&fixture->transport);
	KUNIT_ASSERT_EQ(test, poll_file(fixture->file, &info), 0);
	publish.sequence = 0; publish.slot = 0;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EINVAL);
	publish.sequence = 1;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EINVAL);
}

static void poll_identity_rejects_before_register_effects(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct fes_de25_dma_info info;
	unsigned int writes = fixture->transport.writes;

	fes_de25_dma_info(fixture->file, &info);
	info.version = 1;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_poll(fixture->file, &info), -EINVAL);
	fes_de25_dma_info(fixture->file, &info);
	info.generation++;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_poll(fixture->file, &info), -EINVAL);
	fes_de25_dma_info(fixture->file, &info);
	info.reserved = 1;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_poll(fixture->file, &info), -EINVAL);
	KUNIT_EXPECT_EQ(test, fixture->transport.writes, writes);
}

static struct mm_struct *test_mm(struct kunit *test)
{
	struct rlimit stack = { .rlim_cur = 8 * 1024 * 1024, .rlim_max = 8 * 1024 * 1024 };
	struct mm_struct *mm;

	if (current->mm || !(current->flags & PF_KTHREAD)) {
		KUNIT_FAIL(test, "VMA test requires its KUnit kernel thread");
		return NULL;
	}
	mm = mm_alloc();
	if (!mm) {
		KUNIT_FAIL(test, "mm_alloc failed");
		return NULL;
	}
	arch_pick_mmap_layout(mm, &stack);
	return mm;
}

static void coherent_mapping_uapi_and_revoke(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct mm_struct *mm = test_mm(test);
	struct fes_de25_dma_publish publish = publish_for(fixture->file);
	struct fes_de25_dma_info info;
	struct vm_area_struct *vma;
	struct page *pinned = NULL;
	long pin_count;
	unsigned long address;
	u32 value = 0xfeed1234, copy = 0;

	if (!mm)
		return;
	kthread_use_mm(mm);
	address = vm_mmap(fixture->file, TEST_ADDRESS, FES_DE25_DMA_ALLOCATION_BYTES,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_SHARED, 0);
	KUNIT_EXPECT_EQ(test, address, TEST_ADDRESS);
	if (IS_ERR_VALUE(address))
		goto out;
	mmap_read_lock(mm);
	vma = find_vma(mm, address);
	KUNIT_EXPECT_NOT_NULL(test, vma);
	if (vma)
		KUNIT_EXPECT_EQ(test, vma->vm_flags & (VM_IO | VM_PFNMAP), (vm_flags_t)(VM_IO | VM_PFNMAP));
	mmap_read_unlock(mm);
	pin_count = pin_user_pages_unlocked(address, 1, &pinned, FOLL_WRITE);
	KUNIT_EXPECT_LE(test, pin_count, 0L);
	if (pin_count == 1)
		unpin_user_page(pinned);
	KUNIT_EXPECT_EQ(test, copy_to_user((void __user *)address, &value, sizeof(value)), 0UL);
	KUNIT_EXPECT_EQ(test, copy_from_user(&copy, (void __user *)address, sizeof(copy)), 0UL);
	KUNIT_EXPECT_EQ(test, copy, value);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_INFO, address), 0L);
	KUNIT_EXPECT_EQ(test, copy_from_user(&info, (void __user *)address, sizeof(info)), 0UL);
	KUNIT_EXPECT_EQ(test, info.generation, publish.generation);
	KUNIT_EXPECT_EQ(test, info.allocation_bytes, FES_DE25_DMA_ALLOCATION_BYTES);
	KUNIT_EXPECT_EQ(test, copy_to_user((void __user *)address, &publish, sizeof(publish)), 0UL);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_PUBLISH, address), 0L);
	/* Exercise the actual bidirectional POLL ioctl copy path too. */
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, copy_to_user((void __user *)address, &info, sizeof(info)), 0UL);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_POLL, address), 0L);
	KUNIT_EXPECT_EQ(test, copy_from_user(&info, (void __user *)address, sizeof(info)), 0UL);
	KUNIT_EXPECT_EQ(test, info.accepted_sequence, publish.sequence);
	KUNIT_EXPECT_EQ(test, info.displayed_sequence, 0U);
	fixture->transport.revoked_address = address;
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_REVOKE, 0), 0L);
	KUNIT_EXPECT_TRUE(test, fixture->transport.writes_revoked_before_stop);
	KUNIT_EXPECT_NE(test, copy_from_user(&copy, (void __user *)address, sizeof(copy)), 0UL);
	KUNIT_EXPECT_EQ(test, vm_mmap(fixture->file, TEST_ADDRESS + (16UL << 20), PAGE_SIZE,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_SHARED, 0), (unsigned long)-EACCES);
	KUNIT_EXPECT_EQ(test, vm_munmap(address, FES_DE25_DMA_ALLOCATION_BYTES), 0);
out:
	kthread_unuse_mm(mm);
	mmput(mm);
}

static void contiguous_aliases_are_revoked(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct mm_struct *mm = test_mm(test);
	struct vm_area_struct *vma;
	unsigned long address, alias, first_pfn = 0, page;
	u32 value = 0x7193ac5e, copy = 0;
	int ret;

	if (!mm)
		return;
	kthread_use_mm(mm);
	address = vm_mmap(fixture->file, TEST_ADDRESS, FES_DE25_DMA_ALLOCATION_BYTES,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_SHARED, 0);
	KUNIT_EXPECT_EQ(test, address, TEST_ADDRESS);
	if (IS_ERR_VALUE(address))
		goto out;
	alias = vm_mmap(fixture->file, TEST_ADDRESS + (16UL << 20), PAGE_SIZE,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_SHARED, 3 * PAGE_SIZE);
	KUNIT_EXPECT_FALSE(test, IS_ERR_VALUE(alias));
	if (IS_ERR_VALUE(alias))
		goto unmap;
	mmap_read_lock(mm);
	vma = find_vma(mm, address);
	KUNIT_EXPECT_NOT_NULL(test, vma);
	if (vma) {
		for (page = 0; page < FES_DE25_DMA_ALLOCATION_BYTES / PAGE_SIZE; page++) {
			struct follow_pfnmap_args args = {
				.vma = vma, .address = address + page * PAGE_SIZE,
			};

			ret = follow_pfnmap_start(&args);
			KUNIT_EXPECT_EQ(test, ret, 0);
			if (ret)
				break;
			if (!page)
				first_pfn = args.pfn;
			KUNIT_EXPECT_EQ(test, args.pfn, first_pfn + page);
			follow_pfnmap_end(&args);
		}
	}
	vma = find_vma(mm, alias);
	KUNIT_EXPECT_NOT_NULL(test, vma);
	if (vma) {
		struct follow_pfnmap_args args = { .vma = vma, .address = alias };

		ret = follow_pfnmap_start(&args);
		KUNIT_EXPECT_EQ(test, ret, 0);
		if (!ret) {
			KUNIT_EXPECT_EQ(test, args.pfn, first_pfn + 3);
			follow_pfnmap_end(&args);
		}
	}
	mmap_read_unlock(mm);
	KUNIT_EXPECT_EQ(test, copy_to_user((void __user *)alias, &value, sizeof(value)), 0UL);
	KUNIT_EXPECT_EQ(test, copy_from_user(&copy, (void __user *)(address + 3 * PAGE_SIZE), sizeof(copy)), 0UL);
	KUNIT_EXPECT_EQ(test, copy, value);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
	KUNIT_EXPECT_NE(test, copy_from_user(&copy, (void __user *)alias, sizeof(copy)), 0UL);
	KUNIT_EXPECT_NE(test, copy_from_user(&copy, (void __user *)(address + 3 * PAGE_SIZE), sizeof(copy)), 0UL);
	KUNIT_EXPECT_EQ(test, vm_munmap(alias, PAGE_SIZE), 0);
unmap:
	KUNIT_EXPECT_EQ(test, vm_munmap(address, FES_DE25_DMA_ALLOCATION_BYTES), 0);
out:
	kthread_unuse_mm(mm);
	mmput(mm);
}

static void rejects_invalid_mapping_shapes(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct mm_struct *mm = test_mm(test);

	if (!mm)
		return;
	kthread_use_mm(mm);
	KUNIT_EXPECT_EQ(test, vm_mmap(fixture->file, TEST_ADDRESS, PAGE_SIZE,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_PRIVATE, 0), (unsigned long)-EINVAL);
	KUNIT_EXPECT_EQ(test, vm_mmap(fixture->file, TEST_ADDRESS, PAGE_SIZE,
			PROT_READ | PROT_EXEC, MAP_FIXED | MAP_SHARED, 0), (unsigned long)-EINVAL);
	KUNIT_EXPECT_EQ(test, vm_mmap(fixture->file, TEST_ADDRESS, FES_DE25_DMA_ALLOCATION_BYTES + PAGE_SIZE,
			PROT_READ, MAP_FIXED | MAP_SHARED, 0), (unsigned long)-EINVAL);
	KUNIT_EXPECT_EQ(test, vm_mmap(fixture->file, TEST_ADDRESS, PAGE_SIZE,
			PROT_READ, MAP_FIXED | MAP_SHARED, FES_DE25_DMA_ALLOCATION_BYTES), (unsigned long)-EINVAL);
	kthread_unuse_mm(mm);
	mmput(mm);
}

struct race_context {
	struct file *file;
	struct mm_struct *mm;
	struct completion done;
	unsigned long address;
	int result;
};

static int racing_mmap(void *context)
{
	struct race_context *race = context;

	kthread_use_mm(race->mm);
	race->address = vm_mmap(race->file, TEST_ADDRESS, FES_DE25_DMA_ALLOCATION_BYTES,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_SHARED, 0);
	kthread_unuse_mm(race->mm);
	complete(&race->done);
	while (!kthread_should_stop())
		schedule_timeout_interruptible(1);
	return 0;
}

static int racing_revoke(void *context)
{
	struct race_context *race = context;

	race->result = race->file->f_op->unlocked_ioctl(race->file, FES_DE25_DMA_REVOKE, 0);
	complete(&race->done);
	while (!kthread_should_stop())
		schedule_timeout_interruptible(1);
	return 0;
}

static void revoke_catches_inflight_mmap_link(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct mm_struct *mm = test_mm(test);
	struct completion entered, release, revoked;
	struct race_context mapper = { .file = fixture->file, .mm = mm };
	struct race_context revoker = { .file = fixture->file };
	struct task_struct *map_task = NULL, *revoke_task = NULL;
	u32 copy;

	if (!mm)
		return;
	init_completion(&entered); init_completion(&release); init_completion(&revoked);
	init_completion(&mapper.done); init_completion(&revoker.done);
	fixture->observer.mmap_entered = &entered;
	fixture->observer.mmap_release = &release;
	fixture->observer.revoked = &revoked;
	map_task = kthread_run(racing_mmap, &mapper, "fes-dma-mmap-test");
	if (IS_ERR(map_task)) {
		KUNIT_FAIL(test, "mmap worker creation failed");
		map_task = NULL;
		goto out;
	}
	if (!wait_for_completion_timeout(&entered, 2 * HZ)) {
		KUNIT_FAIL(test, "mmap never reached the pre-link race window");
		goto out;
	}
	revoke_task = kthread_run(racing_revoke, &revoker, "fes-dma-revoke-test");
	if (IS_ERR(revoke_task)) {
		KUNIT_FAIL(test, "revoke worker creation failed");
		revoke_task = NULL;
		goto out;
	}
	if (!wait_for_completion_timeout(&revoked, 2 * HZ)) {
		KUNIT_FAIL(test, "revoke did not mark the grant closed");
		goto out;
	}
	KUNIT_EXPECT_FALSE(test, completion_done(&revoker.done));
out:
	complete(&release);
	if (map_task)
		kthread_stop(map_task);
	if (revoke_task)
		kthread_stop(revoke_task);
	fixture->observer.mmap_entered = NULL;
	fixture->observer.mmap_release = NULL;
	fixture->observer.revoked = NULL;
	if (map_task && revoke_task) {
		KUNIT_EXPECT_EQ(test, mapper.address, TEST_ADDRESS);
		KUNIT_EXPECT_EQ(test, revoker.result, 0);
		if (!IS_ERR_VALUE(mapper.address)) {
			kthread_use_mm(mm);
			KUNIT_EXPECT_NE(test, copy_from_user(&copy, (void __user *)mapper.address, sizeof(copy)), 0UL);
			KUNIT_EXPECT_EQ(test, vm_munmap(mapper.address, FES_DE25_DMA_ALLOCATION_BYTES), 0);
			kthread_unuse_mm(mm);
		}
	}
	mmput(mm);
}

static struct kunit_case fes_de25_dma_cases[] = {
	KUNIT_CASE(binding_stays_closed),
	KUNIT_CASE(rejects_bad_publish_before_effects),
	KUNIT_CASE(kernel_owns_descriptor_and_drain_lifetime),
	KUNIT_CASE(failed_start_is_held_until_drain),
	KUNIT_CASE(unreadable_epoch_retains_exposed_storage),
	KUNIT_CASE(acceptance_is_not_display_and_slots_alternate),
	KUNIT_CASE(ordered_descriptor_precedes_first_submit),
	KUNIT_CASE(stale_generation_and_reset_cannot_prove_drain),
	KUNIT_CASE(mismatched_completion_never_releases_slot),
	KUNIT_CASE(faults_never_release_pending_slot),
	KUNIT_CASE(snapshot_handshake_is_bounded_and_consistent),
	KUNIT_CASE(sequence_and_generation_never_wrap),
	KUNIT_CASE(poll_identity_rejects_before_register_effects),
	KUNIT_CASE(coherent_mapping_uapi_and_revoke),
	KUNIT_CASE(contiguous_aliases_are_revoked),
	KUNIT_CASE(rejects_invalid_mapping_shapes),
	KUNIT_CASE(revoke_catches_inflight_mmap_link),
	{ }
};

static struct kunit_suite fes_de25_dma_suite = {
	.name = "fes-de25-dma",
	.init = fixture_init,
	.test_cases = fes_de25_dma_cases,
};
kunit_test_suite(fes_de25_dma_suite);
