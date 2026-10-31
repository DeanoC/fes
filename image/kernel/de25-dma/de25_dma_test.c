// SPDX-License-Identifier: GPL-2.0
/* Actual DMA API/file/VMA tests on a KUnit device, never a DE25 binding. */
#include <kunit/device.h>
#include <kunit/test.h>
#include <linux/completion.h>
#include <linux/capability.h>
#include <linux/cred.h>
#include <linux/fdtable.h>
#include <linux/notifier.h>
#include <linux/file.h>
#include <linux/kthread.h>
#include <linux/mman.h>
#include <linux/mm.h>
#include <linux/of.h>
#include <linux/platform_device.h>
#include <linux/sched/mm.h>
#include <linux/uaccess.h>

#include "de25_dma_internal.h"
#include "de25_dma_guard.h"
#include "de25_dma_owner.h"

#define TEST_ADDRESS 0x10000000UL

/* Scripted register device, not a second allocation/lifetime implementation.
 * The driver under test writes its real descriptor and consumes frozen tuples.
 */
struct test_descriptor {
	u64 dma, generation;
	u32 span, sequence, slot;
};
struct test_transport {
	atomic_t lease;
	int acquire_error;
	unsigned int acquired, released, unleased_access, reads, epochs, freed_at_release;
	struct fes_de25_dma_test_observer *observer;
	bool bad_version;
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
	struct fes_de25_dma *owner;
	struct device *dev;
	struct file *file;
	struct test_transport transport;
	struct fes_de25_dma_test_observer observer;
};

static int transport_acquire(void *context)
{
	struct test_transport *t = context;

	if (t->acquire_error)
		return t->acquire_error;
	if (atomic_cmpxchg(&t->lease, 0, 1))
		return -EBUSY;
	t->acquired++;
	return 0;
}

static void transport_release(void *context)
{
	struct test_transport *t = context;

	t->released++;
	if (t->observer)
		t->freed_at_release = t->observer->freed;
	atomic_set(&t->lease, 0);
}

static bool transport_held(struct test_transport *t)
{
	if (atomic_read(&t->lease) == 1)
		return true;
	t->unleased_access++;
	return false;
}

static u64 transport_epoch(void *context)
{
	struct test_transport *transport = context;

	if (!transport_held(transport))
		return 0;
	transport->epochs++;
	return transport->epoch_unknown ? 0 : transport->epoch;
}

static int transport_read(void *context, u32 offset, u32 *value)
{
	struct test_transport *t = context;

	if (!transport_held(t))
		return -EACCES;
	t->reads++;
	if (offset == FES_DE25_SCANOUT_ID_OFFSET)
		*value = FES_DE25_SCANOUT_IDENTITY;
	else if (offset == FES_DE25_SCANOUT_VERSION_OFFSET)
		*value = t->bad_version ? 0 : FES_DE25_SCANOUT_VERSION;
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

	if (!transport_held(t))
		return -EACCES;
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

	if (!transport_held(t))
		return -EACCES;
	t->barriers++;
	if (t->trace_count < ARRAY_SIZE(t->trace_offset)) {
		t->trace_offset[t->trace_count] = ~0U;
		t->trace_value[t->trace_count++] = 0;
	}
	return 0;
}

static struct fes_de25_dma_backend transport_backend(struct test_transport *t)
{
	return (struct fes_de25_dma_backend) {
		.acquire = transport_acquire, .release = transport_release,
		.epoch = transport_epoch, .read32 = transport_read,
		.write32 = transport_write, .io_fence = transport_barrier, .context = t,
	};
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
	if (fixture->owner) {
		fixture->transport.stop_error = 0;
		fixture->transport.drain_error = 0;
		fixture->transport.epoch_unknown = false;
		fixture->transport.epoch = 1;
		fes_de25_dma_revoke_owned(fixture->owner);
		fes_de25_dma_put(fixture->owner);
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
	atomic_set(&fixture->transport.lease, 0);
	fixture->transport.regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_DRAINED;
	backend = transport_backend(&fixture->transport);
	fixture->file = fes_de25_dma_create(fixture->dev, &backend);
	if (IS_ERR(fixture->file))
		return PTR_ERR(fixture->file);
	fes_de25_dma_test_observe(fixture->file, &fixture->observer);
	fixture->transport.observer = &fixture->observer;
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

static void backend_lease_required_before_access(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_backend backend = transport_backend(t);
	unsigned int reads = t->reads, epochs = t->epochs;
	struct file *other;

	backend.release = NULL;
	other = fes_de25_dma_create(fixture->dev, &backend);
	if (!IS_ERR(other))
		__fput_sync(other);
	KUNIT_ASSERT_TRUE(test, IS_ERR(other));
	KUNIT_EXPECT_EQ(test, PTR_ERR(other), (long)-EINVAL);
	backend = transport_backend(t);
	t->acquire_error = -ENODEV;
	other = fes_de25_dma_create(fixture->dev, &backend);
	t->acquire_error = 0;
	if (!IS_ERR(other))
		__fput_sync(other);
	KUNIT_ASSERT_TRUE(test, IS_ERR(other));
	KUNIT_EXPECT_EQ(test, PTR_ERR(other), (long)-ENODEV);
	KUNIT_EXPECT_EQ(test, t->reads, reads);
	KUNIT_EXPECT_EQ(test, t->epochs, epochs);
	KUNIT_EXPECT_EQ(test, t->acquired, 1U);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	KUNIT_EXPECT_EQ(test, t->unleased_access, 0U);
}

static void unconfigured_engine_has_one_allocation_lease(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_backend backend = transport_backend(t);
	unsigned int reads = t->reads, epochs = t->epochs;
	struct file *other = fes_de25_dma_create(fixture->dev, &backend);

	if (!IS_ERR(other))
		__fput_sync(other);
	KUNIT_ASSERT_TRUE(test, IS_ERR(other));
	KUNIT_EXPECT_EQ(test, PTR_ERR(other), (long)-EBUSY);
	KUNIT_EXPECT_EQ(test, t->starts, 0U);
	KUNIT_EXPECT_EQ(test, t->reads, reads);
	KUNIT_EXPECT_EQ(test, t->epochs, epochs);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	KUNIT_EXPECT_EQ(test, t->unleased_access, 0U);
}

static void constructor_failure_releases_backend_lease(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_backend backend = transport_backend(t);
	struct file *other;

	__fput_sync(fixture->file);
	fixture->file = NULL;
	KUNIT_ASSERT_EQ(test, t->released, 1U);
	t->epoch_unknown = true;
	other = fes_de25_dma_create(fixture->dev, &backend);
	t->epoch_unknown = false;
	if (!IS_ERR(other))
		__fput_sync(other);
	KUNIT_ASSERT_TRUE(test, IS_ERR(other));
	KUNIT_EXPECT_EQ(test, PTR_ERR(other), (long)-EOPNOTSUPP);
	KUNIT_EXPECT_EQ(test, t->acquired, 2U);
	KUNIT_EXPECT_EQ(test, t->released, 2U);
	t->bad_version = true;
	other = fes_de25_dma_create(fixture->dev, &backend);
	t->bad_version = false;
	if (!IS_ERR(other))
		__fput_sync(other);
	KUNIT_ASSERT_TRUE(test, IS_ERR(other));
	KUNIT_EXPECT_EQ(test, PTR_ERR(other), (long)-EPROTONOSUPPORT);
	KUNIT_EXPECT_EQ(test, t->acquired, 3U);
	KUNIT_EXPECT_EQ(test, t->released, 3U);
	KUNIT_EXPECT_EQ(test, atomic_read(&t->lease), 0);
	KUNIT_EXPECT_EQ(test, t->unleased_access, 0U);
}

static void uncertain_dma_retains_lease_until_storage_free(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct test_transport *t = &fixture->transport;
	struct fes_de25_dma_publish publish = publish_for(fixture->file);

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	t->drain_error = -ETIMEDOUT;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), -EAGAIN);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	KUNIT_EXPECT_EQ(test, atomic_read(&t->lease), 1);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	t->drain_error = 0;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	__fput_sync(fixture->file);
	fixture->file = NULL;
	KUNIT_EXPECT_EQ(test, t->released, 1U);
	KUNIT_EXPECT_EQ(test, t->freed_at_release, 1U);
	KUNIT_EXPECT_EQ(test, atomic_read(&t->lease), 0);
	KUNIT_EXPECT_EQ(test, t->unleased_access, 0U);
}

/* Drop the actual last file, not just a model client's flag. Owner recovery
 * must not dereference that dead struct file or free still-exposed storage.
 */
static void owner_recovers_drain_after_last_client_close(struct kunit *test)
{
	struct dma_fixture *f = test->priv;
	struct test_transport *t = &f->transport;
	struct fes_de25_dma_publish publish = publish_for(f->file);
	struct fes_de25_dma_backend backend = transport_backend(t);
	struct file *replacement;

	f->owner = fes_de25_dma_get(f->file);
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(f->file, &publish), 0);
	t->drain_error = -ETIMEDOUT;
	__fput_sync(f->file);
	f->file = NULL;
	KUNIT_EXPECT_EQ(test, f->observer.freed, 0U);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke_owned(f->owner), -EAGAIN);
	replacement = fes_de25_dma_create(f->dev, &backend);
	KUNIT_EXPECT_TRUE(test, IS_ERR(replacement));
	if (IS_ERR(replacement))
		KUNIT_EXPECT_EQ(test, PTR_ERR(replacement), -EBUSY);
	else
		__fput_sync(replacement);
	t->drain_error = 0;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_revoke_owned(f->owner), 0);
	KUNIT_EXPECT_EQ(test, f->observer.freed, 0U);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	fes_de25_dma_put(f->owner);
	f->owner = NULL;
	KUNIT_EXPECT_EQ(test, f->observer.freed, 1U);
	KUNIT_EXPECT_EQ(test, t->released, 1U);
	KUNIT_EXPECT_EQ(test, t->freed_at_release, 1U);
	KUNIT_EXPECT_EQ(test, t->unleased_access, 0U);
	replacement = fes_de25_dma_create(f->dev, &backend);
	KUNIT_ASSERT_FALSE(test, IS_ERR(replacement));
	__fput_sync(replacement);
	KUNIT_EXPECT_EQ(test, t->released, 2U);
}

static void owner_recovery_cannot_use_reset_as_drain(struct kunit *test)
{
	struct dma_fixture *f = test->priv;
	struct test_transport *t = &f->transport;
	struct fes_de25_dma_publish publish = publish_for(f->file);
	unsigned int stops;

	f->owner = fes_de25_dma_get(f->file);
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(f->file, &publish), 0);
	t->epoch_unknown = true;
	__fput_sync(f->file);
	f->file = NULL;
	stops = t->stops;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke_owned(f->owner), -ESTALE);
	t->epoch_unknown = false;
	t->epoch = 2;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke_owned(f->owner), -ESTALE);
	KUNIT_EXPECT_EQ(test, t->stops, stops);
	KUNIT_EXPECT_EQ(test, t->clears, 0U);
	KUNIT_EXPECT_EQ(test, t->released, 0U);
	KUNIT_EXPECT_EQ(test, f->observer.freed, 0U);
	/* Test-only restoration of the original scripted epoch. Production has
	 * no override; a missing old proof retains storage and the backend lease.
	 */
	t->epoch = 1;
	KUNIT_ASSERT_EQ(test, fes_de25_dma_revoke_owned(f->owner), 0);
	fes_de25_dma_put(f->owner);
	f->owner = NULL;
	KUNIT_EXPECT_EQ(test, f->observer.freed, 1U);
}

static void owner_reference_release_does_not_revoke_client(struct kunit *test)
{
	struct dma_fixture *f = test->priv;
	struct fes_de25_dma *owner = fes_de25_dma_get(f->file);
	struct fes_de25_dma_publish publish = publish_for(f->file);

	fes_de25_dma_put(owner);
	KUNIT_EXPECT_EQ(test, f->observer.freed, 0U);
	KUNIT_EXPECT_EQ(test, f->transport.stops, 0U);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(f->file, &publish), 0);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(f->file), 0);
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
		.acquire = transport_acquire, .release = transport_release,
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

static void refused_mapping_releases_new_mm_reference(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct mm_struct *mm = test_mm(test);
	unsigned long address;
	int refs;

	if (!mm)
		return;
	kthread_use_mm(mm);
	refs = atomic_read(&mm->mm_count);
	fixture->observer.mmap_refuse = true;
	address = vm_mmap(fixture->file, TEST_ADDRESS, PAGE_SIZE,
			PROT_READ | PROT_WRITE, MAP_FIXED | MAP_SHARED, 0);
	KUNIT_EXPECT_EQ(test, address, (unsigned long)-EOPNOTSUPP);
	KUNIT_EXPECT_EQ(test, fes_de25_dma_test_mmap_count(fixture->file), 0U);
	KUNIT_EXPECT_EQ(test, atomic_read(&mm->mm_count), refs);
	fixture->observer.mmap_refuse = false;
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

struct guard_test_device {
	struct fes_de25_dma_guard guard;
	u32 regs[FES_DE25_SCANOUT_CONTROL_SPAN / 4];
	u32 reads, writes, endpoint_reads, endpoint_writes;
	bool ignore_epoch, timeout_read, response_error, reset_read;
	u32 mutate_guard_offset;
	struct test_transport *endpoint;
};

static u32 guard_test_read(void *context, u32 offset)
{
	struct guard_test_device *device = context;
	u32 value = device->regs[offset / 4];

	device->reads++;
	if (offset < FES_DE25_SCANOUT_SPAN) {
		device->endpoint_reads++;
		if (device->endpoint)
			transport_read(device->endpoint, offset, &value);
		if (device->timeout_read) {
			device->regs[FES_DE25_SCANOUT_GUARD_FAULT_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_FAULT_TIMEOUT;
			device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] |= FES_DE25_SCANOUT_GUARD_STATUS_POISONED;
			value = 0;
		}
		if (device->reset_read) {
			device->regs[FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET / 4] = 0;
			device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] = 0;
		}
		if (device->mutate_guard_offset)
			device->regs[device->mutate_guard_offset / 4] = 0;
	}
	return value;
}

static void guard_test_write(void *context, u32 offset, u32 value)
{
	struct guard_test_device *device = context;

	device->writes++;
	if (offset == FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET) {
		if (!device->ignore_epoch) {
			device->regs[offset / 4] = value;
			device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET;
		}
	} else {
		device->endpoint_writes++;
		device->regs[offset / 4] = value;
		if (device->endpoint)
			transport_write(device->endpoint, offset, value);
		if (device->response_error) {
			device->regs[FES_DE25_SCANOUT_GUARD_FAULT_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_FAULT_RESPONSE;
			device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] |= FES_DE25_SCANOUT_GUARD_STATUS_POISONED;
		}
	}
}

static struct guard_test_device *guard_test_device(struct kunit *test, u32 epoch)
{
	struct guard_test_device *device = kunit_kzalloc(test, sizeof(*device), GFP_KERNEL);

	if (!device)
		return NULL;
	device->regs[FES_DE25_SCANOUT_GUARD_ID_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_IDENTITY;
	device->regs[FES_DE25_SCANOUT_GUARD_VERSION_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_VERSION;
	device->regs[FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES;
	if (fes_de25_guard_init(&device->guard, (void __iomem *)device->regs, sizeof(device->regs)))
		return NULL;
	device->guard.test_read = guard_test_read;
	device->guard.test_write = guard_test_write;
	device->guard.test_context = device;
	if (epoch && fes_de25_guard_claim(&device->guard, epoch))
		return NULL;
	return device;
}

static void guard_real_access_and_bounds(struct kunit *test)
{
	struct guard_test_device *device = guard_test_device(test, 7);
	u32 value;

	KUNIT_ASSERT_NOT_NULL(test, device);
	/* Exercise the real readl/writel path on kernel-owned test storage. This
	 * is not a physical-device or AXI-completion model.
	 */
	device->guard.test_read = NULL;
	device->guard.test_write = NULL;
	device->regs[FES_DE25_SCANOUT_ID_OFFSET / 4] = FES_DE25_SCANOUT_IDENTITY;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, FES_DE25_SCANOUT_ID_OFFSET, &value), 0);
	KUNIT_EXPECT_EQ(test, value, FES_DE25_SCANOUT_IDENTITY);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_write(&device->guard, FES_DE25_SCANOUT_GRANT_LO_OFFSET, 0x1000), 0);
	KUNIT_EXPECT_EQ(test, device->regs[FES_DE25_SCANOUT_GRANT_LO_OFFSET / 4], 0x1000U);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, 1, &value), -EINVAL);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_write(&device->guard, FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET, 8), -EINVAL);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_fence(&device->guard), 0);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_epoch(&device->guard), 7ULL);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_claim(&device->guard, 8), -EBUSY);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_init(&device->guard, (void __iomem *)device->regs,
			sizeof(device->regs)), -EBUSY);
}

static void guard_claim_rejects_wrong_or_existing_identity(struct kunit *test)
{
	u32 offsets[] = { FES_DE25_SCANOUT_GUARD_ID_OFFSET, FES_DE25_SCANOUT_GUARD_VERSION_OFFSET,
		FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET };
	struct guard_test_device *device;
	unsigned int i;

	for (i = 0; i < ARRAY_SIZE(offsets); i++) {
		device = guard_test_device(test, 0);
		KUNIT_ASSERT_NOT_NULL(test, device);
		device->regs[offsets[i] / 4] = 0;
		KUNIT_EXPECT_EQ(test, fes_de25_guard_claim(&device->guard, 9), -EPROTONOSUPPORT);
		KUNIT_EXPECT_EQ(test, device->writes, 0U);
		KUNIT_EXPECT_EQ(test, fes_de25_guard_epoch(&device->guard), 0ULL);
	}
	device = guard_test_device(test, 0);
	KUNIT_ASSERT_NOT_NULL(test, device);
	device->regs[FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET / 4] = 123;
	device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_claim(&device->guard, 9), -ESTALE);
	KUNIT_EXPECT_EQ(test, device->writes, 0U);
	KUNIT_EXPECT_EQ(test, device->regs[FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET / 4], 123U);
	device = guard_test_device(test, 0);
	KUNIT_ASSERT_NOT_NULL(test, device);
	device->ignore_epoch = true;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_claim(&device->guard, 9), -ESTALE);
	KUNIT_EXPECT_EQ(test, device->writes, 1U);
	KUNIT_EXPECT_EQ(test, device->endpoint_writes, 0U);
}

static void guard_dummy_zero_timeout_is_not_success(struct kunit *test)
{
	struct guard_test_device *device = guard_test_device(test, 7);
	u32 value = 0x12345678, reads;

	KUNIT_ASSERT_NOT_NULL(test, device);
	device->timeout_read = true;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET, &value), -EIO);
	KUNIT_EXPECT_EQ(test, value, 0U);
	KUNIT_EXPECT_EQ(test, device->endpoint_reads, 1U);
	reads = device->reads;
	/* Apparent recovery cannot rearm the poisoned software context. */
	device->timeout_read = false;
	device->regs[FES_DE25_SCANOUT_GUARD_FAULT_OFFSET / 4] = 0;
	device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, FES_DE25_SCANOUT_ID_OFFSET, &value), -EIO);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_write(&device->guard, FES_DE25_SCANOUT_COMMAND_OFFSET, 2), -EIO);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_fence(&device->guard), -EIO);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_epoch(&device->guard), 0ULL);
	KUNIT_EXPECT_EQ(test, device->reads, reads);
	KUNIT_EXPECT_EQ(test, device->endpoint_writes, 0U);
}

static void guard_post_access_fault_and_reset_reject_results(struct kunit *test)
{
	struct guard_test_device *device = guard_test_device(test, 7);
	u32 value = 0x12345678;
	u32 offsets[] = { FES_DE25_SCANOUT_GUARD_ID_OFFSET, FES_DE25_SCANOUT_GUARD_VERSION_OFFSET,
		FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET };
	unsigned int i;

	KUNIT_ASSERT_NOT_NULL(test, device);
	device->response_error = true;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_write(&device->guard, FES_DE25_SCANOUT_COMMAND_OFFSET, 8), -EIO);
	KUNIT_EXPECT_EQ(test, device->endpoint_writes, 1U);
	KUNIT_EXPECT_EQ(test, fes_de25_guard_epoch(&device->guard), 0ULL);
	device = guard_test_device(test, 8);
	KUNIT_ASSERT_NOT_NULL(test, device);
	device->regs[FES_DE25_SCANOUT_ID_OFFSET / 4] = FES_DE25_SCANOUT_IDENTITY;
	device->reset_read = true;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, FES_DE25_SCANOUT_ID_OFFSET, &value), -ESTALE);
	KUNIT_EXPECT_EQ(test, value, 0U);
	KUNIT_EXPECT_EQ(test, device->endpoint_reads, 1U);
	for (i = 0; i < ARRAY_SIZE(offsets); i++) {
		device = guard_test_device(test, 9);
		KUNIT_ASSERT_NOT_NULL(test, device);
		device->mutate_guard_offset = offsets[i];
		KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, FES_DE25_SCANOUT_ID_OFFSET,
				&value), -EPROTONOSUPPORT);
		KUNIT_EXPECT_EQ(test, value, 0U);
		KUNIT_EXPECT_EQ(test, device->endpoint_reads, 1U);
	}
}

static void guard_busy_or_unknown_status_prevents_access(struct kunit *test)
{
	struct guard_test_device *device = guard_test_device(test, 7);
	u32 value;

	KUNIT_ASSERT_NOT_NULL(test, device);
	device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] |=
		FES_DE25_SCANOUT_GUARD_STATUS_PENDING | FES_DE25_SCANOUT_GUARD_STATUS_ACCEPTED;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_read(&device->guard, FES_DE25_SCANOUT_ID_OFFSET, &value), -EBUSY);
	KUNIT_EXPECT_EQ(test, device->endpoint_reads, 0U);
	device = guard_test_device(test, 8);
	KUNIT_ASSERT_NOT_NULL(test, device);
	device->regs[FES_DE25_SCANOUT_GUARD_STATUS_OFFSET / 4] |= 0x80000000;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_write(&device->guard, FES_DE25_SCANOUT_COMMAND_OFFSET, 1), -EPROTO);
	KUNIT_EXPECT_EQ(test, device->endpoint_writes, 0U);
	device = guard_test_device(test, 9);
	KUNIT_ASSERT_NOT_NULL(test, device);
	device->regs[FES_DE25_SCANOUT_GUARD_FAULT_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_FAULT_ACCESS;
	KUNIT_EXPECT_EQ(test, fes_de25_guard_fence(&device->guard), -EIO);
	KUNIT_EXPECT_EQ(test, device->endpoint_reads, 0U);
}

static int guard_owner_acquire(void *context)
{
	struct guard_test_device *device = context;
	int ret = transport_acquire(device->endpoint);

	if (ret)
		return ret;
	ret = device->guard.claimed ? fes_de25_guard_fence(&device->guard) :
		fes_de25_guard_claim(&device->guard, 7);
	if (ret)
		transport_release(device->endpoint);
	return ret;
}

static void guard_owner_release(void *context)
{
	struct guard_test_device *device = context;

	transport_release(device->endpoint);
}

static u64 guard_owner_epoch(void *context)
{
	return fes_de25_guard_epoch(&((struct guard_test_device *)context)->guard);
}

static int guard_owner_read(void *context, u32 offset, u32 *value)
{
	return fes_de25_guard_read(&((struct guard_test_device *)context)->guard, offset, value);
}

static int guard_owner_write(void *context, u32 offset, u32 value)
{
	return fes_de25_guard_write(&((struct guard_test_device *)context)->guard, offset, value);
}

static int guard_owner_fence(void *context)
{
	return fes_de25_guard_fence(&((struct guard_test_device *)context)->guard);
}

static void guarded_transport_constructs_real_dma_grant(struct kunit *test)
{
	struct dma_fixture *fixture = test->priv;
	struct guard_test_device *device = guard_test_device(test, 0);
	struct fes_de25_dma_backend backend = {
		.acquire = guard_owner_acquire, .release = guard_owner_release,
		.epoch = guard_owner_epoch, .read32 = guard_owner_read,
		.write32 = guard_owner_write, .io_fence = guard_owner_fence, .context = device,
	};
	struct file *file;

	KUNIT_ASSERT_NOT_NULL(test, device);
	device->endpoint = kunit_kzalloc(test, sizeof(*device->endpoint), GFP_KERNEL);
	KUNIT_ASSERT_NOT_NULL(test, device->endpoint);
	atomic_set(&device->endpoint->lease, 0);
	device->endpoint->epoch = 7;
	device->endpoint->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_DRAINED;
	file = fes_de25_dma_create(fixture->dev, &backend);
	KUNIT_EXPECT_FALSE(test, IS_ERR(file));
	if (!IS_ERR(file))
		__fput_sync(file);
	KUNIT_EXPECT_GT(test, device->endpoint_reads, 0U);
	KUNIT_EXPECT_EQ(test, device->endpoint->acquired, 1U);
	KUNIT_EXPECT_EQ(test, device->endpoint->released, 1U);
	KUNIT_EXPECT_EQ(test, device->endpoint->unleased_access, 0U);
}

struct owner_fixture {
	struct fes_de25_owner *owner;
	struct guard_test_device *device;
	struct file *file;
	int fd, base_overlay, profile_overlay, program_error;
	struct device_node *region;
	struct notifier_block programmer;
	bool programmer_registered;
	unsigned int programs, removes;
	bool wrong_identity;
	struct fes_de25_dma_test_observer control_observer;
};

static void owner_fixture_close_client(struct owner_fixture *f)
{
	struct file *file;

	if (!IS_ERR_OR_NULL(f->file))
		__fput_sync(f->file);
	f->file = NULL;
	if (f->fd >= 0) {
		file = file_close_fd(f->fd);
		f->fd = -1;
		if (file)
			__fput_sync(file);
	}
}

static void owner_fixture_cleanup(void *context)
{
	struct owner_fixture *f = context;

	f->device->endpoint->drain_error = 0;
	f->device->endpoint->stop_error = 0;
	fes_de25_owner_test_reap(f->owner);
	owner_fixture_close_client(f);
	if (f->profile_overlay)
		of_overlay_remove(&f->profile_overlay);
	if (f->programmer_registered)
		of_overlay_notifier_unregister(&f->programmer);
	fes_de25_owner_test_detach(f->owner);
	of_node_put(f->region);
	if (f->base_overlay)
		of_overlay_remove(&f->base_overlay);
	fes_de25_owner_test_destroy(f->owner);
}

static struct owner_fixture *owner_fixture_create(struct kunit *test, bool ready)
{
	struct dma_fixture *dma = test->priv;
	struct owner_fixture *f = kunit_kzalloc(test, sizeof(*f), GFP_KERNEL);

	if (!f)
		return NULL;
	f->fd = -1;
	f->device = guard_test_device(test, 0);
	if (!f->device)
		return NULL;
	f->device->endpoint = kunit_kzalloc(test, sizeof(*f->device->endpoint), GFP_KERNEL);
	if (!f->device->endpoint)
		return NULL;
	/* The register script is hardware, not a second software owner. The real
	 * provider's lease below gates every allocation and configuration event.
	 */
	atomic_set(&f->device->endpoint->lease, 1);
	f->device->endpoint->epoch = 7;
	f->device->endpoint->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_DRAINED;
	if (ready && fes_de25_guard_claim(&f->device->guard, 7))
		return NULL;
	f->owner = fes_de25_owner_test_create(dma->dev, ready ? &f->device->guard : NULL);
	if (!f->owner)
		return NULL;
	if (kunit_add_action_or_reset(test, owner_fixture_cleanup, f))
		return NULL;
	return f;
}

static void permanent_owner_blocks_configuration_before_first_submit(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, true);
	struct file *second;
	unsigned int reads;

	KUNIT_ASSERT_NOT_NULL(test, f);
	f->file = fes_de25_owner_test_allocate(f->owner);
	KUNIT_ASSERT_FALSE(test, IS_ERR(f->file));
	reads = f->device->endpoint_reads;
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_APPLY), -EBUSY);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_REMOVE), -EBUSY);
	KUNIT_EXPECT_EQ(test, f->device->endpoint_reads, reads);
	second = fes_de25_owner_test_allocate(f->owner);
	KUNIT_EXPECT_TRUE(test, IS_ERR(second));
	if (IS_ERR(second))
		KUNIT_EXPECT_EQ(test, PTR_ERR(second), -EBUSY);
	else
		__fput_sync(second);
	/* Reaping the owner's reference is insufficient while the client storage
	 * still exists, even though this engine has never submitted a frame.
	 */
	KUNIT_ASSERT_EQ(test, fes_de25_owner_test_reap(f->owner), 0);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_REMOVE), -EBUSY);
	__fput_sync(f->file);
	f->file = NULL;
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_REMOVE), 0);
}

static void permanent_owner_retains_uncertain_closed_client(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, true);
	struct fes_de25_dma_publish publish;

	KUNIT_ASSERT_NOT_NULL(test, f);
	f->file = fes_de25_owner_test_allocate(f->owner);
	KUNIT_ASSERT_FALSE(test, IS_ERR(f->file));
	publish = publish_for(f->file);
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(f->file, &publish), 0);
	f->device->endpoint->drain_error = -ETIMEDOUT;
	__fput_sync(f->file);
	f->file = NULL;
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_reap(f->owner), -EAGAIN);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_APPLY), -EBUSY);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_REMOVE), -EBUSY);
	f->device->endpoint->drain_error = 0;
	KUNIT_ASSERT_EQ(test, fes_de25_owner_test_reap(f->owner), 0);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_APPLY), 0);
	/* Replacement is reserved before any stock bridge-disable call. */
	f->file = fes_de25_owner_test_allocate(f->owner);
	KUNIT_EXPECT_TRUE(test, IS_ERR(f->file));
	if (IS_ERR(f->file)) {
		KUNIT_EXPECT_EQ(test, PTR_ERR(f->file), -ENODEV);
		f->file = NULL;
	}
}

static void permanent_owner_never_adopts_unknown_configuration(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);
	unsigned int reads;

	KUNIT_ASSERT_NOT_NULL(test, f);
	reads = f->device->reads;
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_PRE_APPLY), 0);
	/* POST_APPLY without a known firmware/profile is not admission. */
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_notify(f->owner, OF_OVERLAY_POST_APPLY), 0);
	f->file = fes_de25_owner_test_allocate(f->owner);
	KUNIT_EXPECT_TRUE(test, IS_ERR(f->file));
	if (IS_ERR(f->file)) {
		KUNIT_EXPECT_EQ(test, PTR_ERR(f->file), -ENODEV);
		f->file = NULL;
	}
	KUNIT_EXPECT_EQ(test, f->device->reads, reads);
}

static void permanent_owner_rejects_topology_overrides(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);
	static const char * const names[] = {
		"fpga-bridges", "fpga-mgr", "compatible", "status", "ranges",
	};
	unsigned int i, reads;

	KUNIT_ASSERT_NOT_NULL(test, f);
	reads = f->device->reads;
	for (i = 0; i < ARRAY_SIZE(names); i++)
		KUNIT_EXPECT_EQ(test, fes_de25_owner_test_override(f->owner, names[i]), -EINVAL);
	KUNIT_EXPECT_EQ(test, f->device->reads, reads);
}

static void permanent_owner_rejects_ancestor_mutation(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);

	KUNIT_ASSERT_NOT_NULL(test, f);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ancestor(f->owner, OF_OVERLAY_PRE_APPLY), -EBUSY);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ancestor(f->owner, OF_OVERLAY_PRE_REMOVE), -EBUSY);
}

static void permanent_owner_probe_refuses_unbound_device(struct kunit *test)
{
	struct platform_device *pdev = platform_device_register_simple("de25-owner-refusal", -1, NULL, 0);

	KUNIT_ASSERT_FALSE(test, IS_ERR(pdev));
	KUNIT_EXPECT_EQ(test, fes_de25_owner_probe(pdev), -EOPNOTSUPP);
	platform_device_unregister(pdev);
}

static void permanent_owner_checks_real_supplier_links(struct kunit *test)
{
	struct platform_device *consumer, *supplier;
	struct device_link *link;

	consumer = platform_device_register_simple("de25-owner-consumer", -1, NULL, 0);
	KUNIT_ASSERT_FALSE(test, IS_ERR(consumer));
	supplier = platform_device_register_simple("de25-owner-supplier", -1, NULL, 0);
	if (IS_ERR(supplier)) {
		KUNIT_FAIL(test, "supplier registration failed");
		platform_device_unregister(consumer);
		return;
	}
	link = device_link_add(&consumer->dev, &supplier->dev, DL_FLAG_STATELESS);
	KUNIT_EXPECT_NOT_NULL(test, link);
	if (link) {
		/* An actual unbound supplier cannot retain the consumer's resources,
		 * even though its permanent consumer/parent objects still exist.
		 */
		KUNIT_EXPECT_EQ(test, fes_de25_owner_test_dependencies(&consumer->dev), -EPROBE_DEFER);
		device_link_del(link);
	}
	platform_device_unregister(supplier);
	platform_device_unregister(consumer);
}

/* Actual OF apply/remove and owner ioctl/FD table. The priority-zero
 * programmer models a completed firmware transfer and register reset; it is
 * not the SDM manager, an RBF loader, an SMMU or physical FPGA hardware.
 */
extern u8 __dtbo_fes_de25_owner_base_begin[], __dtbo_fes_de25_owner_base_end[];
extern u8 __dtbo_fes_de25_owner_profile_begin[], __dtbo_fes_de25_owner_profile_end[];

static int owner_fixture_program(struct notifier_block *nb, unsigned long action, void *data)
{
	struct owner_fixture *f = container_of(nb, struct owner_fixture, programmer);
	struct of_overlay_notify_data *nd = data;
	struct guard_test_device *device = f->device;
	struct test_transport *endpoint = device->endpoint;

	if (nd->target != f->region)
		return NOTIFY_OK;
	if (action == OF_OVERLAY_PRE_REMOVE)
		f->removes++;
	if (action != OF_OVERLAY_PRE_APPLY)
		return NOTIFY_OK;
	f->programs++;
	if (f->program_error)
		return notifier_from_errno(f->program_error);
	memset(device->regs, 0, sizeof(device->regs));
	device->regs[FES_DE25_SCANOUT_GUARD_ID_OFFSET / 4] =
		f->wrong_identity ? 0 : FES_DE25_SCANOUT_GUARD_IDENTITY;
	device->regs[FES_DE25_SCANOUT_GUARD_VERSION_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_VERSION;
	device->regs[FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET / 4] = FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES;
	memset(endpoint->regs, 0, sizeof(endpoint->regs));
	memset(endpoint->live, 0, sizeof(endpoint->live));
	memset(endpoint->frozen, 0, sizeof(endpoint->frozen));
	endpoint->serial = 0;
	endpoint->regs[FES_DE25_SCANOUT_STATUS_OFFSET / 4] = FES_DE25_SCANOUT_STATUS_DRAINED;
	return NOTIFY_OK;
}

static int owner_fixture_overlay_setup(struct owner_fixture *f)
{
	int ret;

	/* Descriptor mutations stay in this test task's own table. */
	if (!current->files)
		return -EINVAL;
	ret = unshare_files();
	if (ret)
		return ret;
	ret = of_overlay_fdt_apply(__dtbo_fes_de25_owner_base_begin,
		__dtbo_fes_de25_owner_base_end - __dtbo_fes_de25_owner_base_begin,
		&f->base_overlay, NULL);
	if (ret)
		return ret;
	f->region = of_find_node_by_path("/fes-de25-kunit-region");
	if (!f->region)
		return -ENODEV;
	ret = fes_de25_owner_test_attach(f->owner, f->region, &f->device->guard);
	if (ret)
		return ret;
	f->programmer.notifier_call = owner_fixture_program;
	f->programmer.priority = 0;
	ret = of_overlay_notifier_register(&f->programmer);
	if (!ret)
		f->programmer_registered = true;
	return ret;
}

static int owner_fixture_apply(struct owner_fixture *f)
{
	return of_overlay_fdt_apply(__dtbo_fes_de25_owner_profile_begin,
		__dtbo_fes_de25_owner_profile_end - __dtbo_fes_de25_owner_profile_begin,
		&f->profile_overlay, NULL);
}

static void owner_overlay_and_control_fd_complete_two_cycles(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);
	struct fes_de25_dma_publish publish;
	struct device_node *node;
	u64 previous = 0;
	unsigned int cycle;
	bool cloexec;

	KUNIT_ASSERT_NOT_NULL(test, f);
	KUNIT_ASSERT_EQ(test, owner_fixture_overlay_setup(f), 0);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0), -ENODEV);
	for (cycle = 1; cycle <= 2; cycle++) {
		KUNIT_ASSERT_EQ(test, owner_fixture_apply(f), 0);
		KUNIT_EXPECT_EQ(test, f->programs, cycle);
		KUNIT_EXPECT_EQ(test, f->device->regs[FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET / 4], cycle);
		f->fd = fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0);
		KUNIT_ASSERT_GE(test, f->fd, 0);
		spin_lock(&current->files->file_lock);
		cloexec = close_on_exec(f->fd, current->files);
		spin_unlock(&current->files->file_lock);
		KUNIT_EXPECT_TRUE(test, cloexec);
		f->file = fget(f->fd);
		KUNIT_ASSERT_NOT_NULL(test, f->file);
		fes_de25_dma_test_observe(f->file, &f->control_observer);
		publish = publish_for(f->file);
		KUNIT_EXPECT_GT(test, publish.generation, previous);
		previous = publish.generation;
		KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(f->file, &publish), 0);
		KUNIT_EXPECT_EQ(test, of_overlay_remove(&f->profile_overlay), -EBUSY);
		KUNIT_EXPECT_EQ(test, f->removes, cycle - 1);
		node = of_find_node_by_path("/fes-de25-kunit-region/scanout");
		KUNIT_EXPECT_NOT_NULL(test, node);
		of_node_put(node);
		KUNIT_ASSERT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_REAP, 0), 0);
		KUNIT_EXPECT_EQ(test, of_overlay_remove(&f->profile_overlay), -EBUSY);
		KUNIT_EXPECT_EQ(test, f->control_observer.freed, cycle - 1);
		owner_fixture_close_client(f);
		KUNIT_EXPECT_EQ(test, f->control_observer.freed, cycle);
		KUNIT_ASSERT_EQ(test, of_overlay_remove(&f->profile_overlay), 0);
		KUNIT_EXPECT_EQ(test, f->removes, cycle);
		KUNIT_EXPECT_EQ(test, f->profile_overlay, 0);
		KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0), -ENODEV);
	}
}

static void owner_overlay_refuses_remove_after_uncertain_last_close(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);
	struct fes_de25_dma_publish publish;

	KUNIT_ASSERT_NOT_NULL(test, f);
	KUNIT_ASSERT_EQ(test, owner_fixture_overlay_setup(f), 0);
	KUNIT_ASSERT_EQ(test, owner_fixture_apply(f), 0);
	f->fd = fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0);
	KUNIT_ASSERT_GE(test, f->fd, 0);
	f->file = fget(f->fd);
	KUNIT_ASSERT_NOT_NULL(test, f->file);
	fes_de25_dma_test_observe(f->file, &f->control_observer);
	publish = publish_for(f->file);
	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(f->file, &publish), 0);
	f->device->endpoint->drain_error = -ETIMEDOUT;
	owner_fixture_close_client(f);
	KUNIT_EXPECT_EQ(test, f->control_observer.freed, 0U);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_REAP, 0), -EAGAIN);
	KUNIT_EXPECT_EQ(test, of_overlay_remove(&f->profile_overlay), -EBUSY);
	KUNIT_EXPECT_EQ(test, f->removes, 0U);
	f->device->endpoint->drain_error = 0;
	KUNIT_ASSERT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_REAP, 0), 0);
	KUNIT_EXPECT_EQ(test, f->control_observer.freed, 1U);
	KUNIT_EXPECT_EQ(test, of_overlay_remove(&f->profile_overlay), 0);
}

static void owner_overlay_failure_never_admits_a_guard(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);
	unsigned int reads;

	KUNIT_ASSERT_NOT_NULL(test, f);
	KUNIT_ASSERT_EQ(test, owner_fixture_overlay_setup(f), 0);
	f->program_error = -EIO;
	KUNIT_EXPECT_EQ(test, owner_fixture_apply(f), -EIO);
	reads = f->device->reads;
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0), -ENODEV);
	KUNIT_EXPECT_EQ(test, f->device->reads, reads);
	KUNIT_ASSERT_EQ(test, of_overlay_remove(&f->profile_overlay), 0);
	f->program_error = 0;
	f->wrong_identity = true;
	KUNIT_EXPECT_EQ(test, owner_fixture_apply(f), -EPROTONOSUPPORT);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0), -ENODEV);
	KUNIT_ASSERT_EQ(test, of_overlay_remove(&f->profile_overlay), 0);
	f->wrong_identity = false;
	KUNIT_ASSERT_EQ(test, owner_fixture_apply(f), 0);
	KUNIT_EXPECT_EQ(test, f->device->regs[FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET / 4], 2U);
}

static void owner_control_checks_capability_and_command_before_effects(struct kunit *test)
{
	struct owner_fixture *f = owner_fixture_create(test, false);
	struct cred *denied;
	const struct cred *saved;
	unsigned int reads;
	long ret;

	KUNIT_ASSERT_NOT_NULL(test, f);
	KUNIT_ASSERT_EQ(test, owner_fixture_overlay_setup(f), 0);
	KUNIT_ASSERT_EQ(test, owner_fixture_apply(f), 0);
	reads = f->device->reads;
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 1), -EINVAL);
	KUNIT_EXPECT_EQ(test, fes_de25_owner_test_ioctl(f->owner, 0, 0), -ENOTTY);
	denied = prepare_creds();
	KUNIT_ASSERT_NOT_NULL(test, denied);
	cap_lower(denied->cap_effective, CAP_SYS_RAWIO);
	saved = override_creds(denied);
	ret = fes_de25_owner_test_ioctl(f->owner, FES_DE25_DMA_OWNER_ALLOC, 0);
	revert_creds(saved);
	put_cred(denied);
	KUNIT_EXPECT_EQ(test, ret, -EPERM);
	KUNIT_EXPECT_EQ(test, f->device->reads, reads);
}

static struct kunit_case fes_de25_dma_cases[] = {
	KUNIT_CASE(owner_overlay_and_control_fd_complete_two_cycles),
	KUNIT_CASE(owner_overlay_refuses_remove_after_uncertain_last_close),
	KUNIT_CASE(owner_overlay_failure_never_admits_a_guard),
	KUNIT_CASE(owner_control_checks_capability_and_command_before_effects),
	KUNIT_CASE(permanent_owner_checks_real_supplier_links),
	KUNIT_CASE(permanent_owner_probe_refuses_unbound_device),
	KUNIT_CASE(permanent_owner_rejects_topology_overrides),
	KUNIT_CASE(permanent_owner_rejects_ancestor_mutation),
	KUNIT_CASE(permanent_owner_blocks_configuration_before_first_submit),
	KUNIT_CASE(permanent_owner_retains_uncertain_closed_client),
	KUNIT_CASE(permanent_owner_never_adopts_unknown_configuration),
	KUNIT_CASE(owner_recovers_drain_after_last_client_close),
	KUNIT_CASE(owner_recovery_cannot_use_reset_as_drain),
	KUNIT_CASE(owner_reference_release_does_not_revoke_client),
	KUNIT_CASE(guard_real_access_and_bounds),
	KUNIT_CASE(guard_claim_rejects_wrong_or_existing_identity),
	KUNIT_CASE(guard_dummy_zero_timeout_is_not_success),
	KUNIT_CASE(guard_post_access_fault_and_reset_reject_results),
	KUNIT_CASE(guard_busy_or_unknown_status_prevents_access),
	KUNIT_CASE(guarded_transport_constructs_real_dma_grant),
	KUNIT_CASE(binding_stays_closed),
	KUNIT_CASE(backend_lease_required_before_access),
	KUNIT_CASE(unconfigured_engine_has_one_allocation_lease),
	KUNIT_CASE(constructor_failure_releases_backend_lease),
	KUNIT_CASE(uncertain_dma_retains_lease_until_storage_free),
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
	KUNIT_CASE(refused_mapping_releases_new_mm_reference),
	KUNIT_CASE(revoke_catches_inflight_mmap_link),
	{ }
};

static struct kunit_suite fes_de25_dma_suite = {
	.name = "fes-de25-dma",
	.init = fixture_init,
	.test_cases = fes_de25_dma_cases,
};
kunit_test_suite(fes_de25_dma_suite);
