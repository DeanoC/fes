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

struct test_transport {
	u64 epoch;
	bool epoch_unknown, lose_epoch_after_start;
	int start_error, stop_error, drain_error;
	unsigned int starts, stops, drains;
	struct fes_de25_dma_descriptor descriptor;
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

static int transport_start(void *context, const struct fes_de25_dma_descriptor *descriptor)
{
	struct test_transport *transport = context;

	transport->starts++;
	transport->descriptor = *descriptor;
	if (transport->lose_epoch_after_start)
		transport->epoch_unknown = true;
	return transport->start_error;
}

static int transport_stop(void *context, u64 generation)
{
	struct test_transport *transport = context;
	u32 value = 0x12345678;

	transport->stops++;
	if (generation != transport->descriptor.generation)
		return -EINVAL;
	if (transport->revoked_address)
		transport->writes_revoked_before_stop =
			copy_to_user((void __user *)transport->revoked_address, &value, sizeof(value)) != 0;
	return transport->stop_error;
}

static int transport_drain(void *context, u64 generation)
{
	struct test_transport *transport = context;

	transport->drains++;
	if (generation != transport->descriptor.generation)
		return -EINVAL;
	return transport->drain_error;
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
	backend.epoch = transport_epoch;
	backend.start = transport_start;
	backend.stop = transport_stop;
	backend.drain = transport_drain;
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
	struct fes_de25_dma_descriptor *descriptor = &fixture->transport.descriptor;

	KUNIT_ASSERT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), 0);
	KUNIT_EXPECT_EQ(test, descriptor->span, FES_DE25_DMA_ALLOCATION_BYTES);
	KUNIT_EXPECT_EQ(test, descriptor->offset, FES_DE25_DMA_SLOT_BYTES);
	KUNIT_EXPECT_EQ(test, descriptor->bytes, FES_DE25_DMA_FRAME_BYTES);
	KUNIT_EXPECT_EQ(test, descriptor->generation, publish.generation);
	KUNIT_EXPECT_EQ(test, descriptor->sequence, publish.sequence);
	KUNIT_EXPECT_TRUE(test, IS_ALIGNED(descriptor->dma, 32));
	KUNIT_EXPECT_LE(test, (u64)descriptor->dma + descriptor->span, 1ULL << 32);
	publish.sequence++;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_publish(fixture->file, &publish), -EBUSY);
	KUNIT_EXPECT_EQ(test, fixture->transport.starts, 1U);
	fixture->transport.drain_error = -ETIMEDOUT;
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_REVOKE, 0), -ETIMEDOUT);
	fes_de25_dma_info(fixture->file, &info);
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_REVOKED | FES_DE25_DMA_EXPOSED | FES_DE25_DMA_UNCERTAIN);
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
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_EXPOSED | FES_DE25_DMA_UNCERTAIN);
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
	KUNIT_EXPECT_EQ(test, info.state, FES_DE25_DMA_REVOKED | FES_DE25_DMA_EXPOSED | FES_DE25_DMA_UNCERTAIN);
	KUNIT_EXPECT_EQ(test, fixture->transport.stops, 0U);
	KUNIT_EXPECT_EQ(test, fixture->observer.freed, 0U);
	/* Restore visibility of the ORIGINAL epoch, not a reset/epoch override. */
	fixture->transport.epoch_unknown = false;
	KUNIT_EXPECT_EQ(test, fes_de25_dma_revoke(fixture->file), 0);
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
	KUNIT_EXPECT_EQ(test, copy_to_user((void __user *)address, &value, sizeof(value)), 0UL);
	KUNIT_EXPECT_EQ(test, copy_from_user(&copy, (void __user *)address, sizeof(copy)), 0UL);
	KUNIT_EXPECT_EQ(test, copy, value);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_INFO, address), 0L);
	KUNIT_EXPECT_EQ(test, copy_from_user(&info, (void __user *)address, sizeof(info)), 0UL);
	KUNIT_EXPECT_EQ(test, info.generation, publish.generation);
	KUNIT_EXPECT_EQ(test, info.allocation_bytes, FES_DE25_DMA_ALLOCATION_BYTES);
	KUNIT_EXPECT_EQ(test, copy_to_user((void __user *)address, &publish, sizeof(publish)), 0UL);
	KUNIT_EXPECT_EQ(test, fixture->file->f_op->unlocked_ioctl(fixture->file, FES_DE25_DMA_PUBLISH, address), 0L);
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
	KUNIT_CASE(coherent_mapping_uapi_and_revoke),
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
