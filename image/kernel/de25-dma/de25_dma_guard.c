// SPDX-License-Identifier: GPL-2.0
/* Ordered accesses through the compiled system-clock timeout proxy. */
#include <linux/errno.h>
#include <linux/build_bug.h>
#include <linux/kernel.h>

#include "de25_dma_guard.h"
#include "generated/fes_de25_scanout.h"

static_assert(FES_DE25_SCANOUT_SPAN == FES_DE25_SCANOUT_GUARD_ID_OFFSET);
static_assert(FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET + sizeof(u32) <= FES_DE25_SCANOUT_CONTROL_SPAN);

static u32 guard_read(struct fes_de25_dma_guard *guard, u32 offset)
{
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (guard->test_read)
		return guard->test_read(guard->test_context, offset);
#endif
	return readl(guard->base + offset);
}

static void guard_write(struct fes_de25_dma_guard *guard, u32 offset, u32 value)
{
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	if (guard->test_write) {
		guard->test_write(guard->test_context, offset, value);
		return;
	}
#endif
	writel(value, guard->base + offset);
}

static int guard_health(struct fes_de25_dma_guard *guard, u32 expected_epoch)
{
	u32 before, after, status, fault;

	before = guard_read(guard, FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET);
	if (guard_read(guard, FES_DE25_SCANOUT_GUARD_ID_OFFSET) != FES_DE25_SCANOUT_GUARD_IDENTITY ||
	    guard_read(guard, FES_DE25_SCANOUT_GUARD_VERSION_OFFSET) != FES_DE25_SCANOUT_GUARD_VERSION ||
	    guard_read(guard, FES_DE25_SCANOUT_GUARD_TIMEOUT_OFFSET) != FES_DE25_SCANOUT_GUARD_TIMEOUT_CYCLES)
		return -EPROTONOSUPPORT;
	status = guard_read(guard, FES_DE25_SCANOUT_GUARD_STATUS_OFFSET);
	fault = guard_read(guard, FES_DE25_SCANOUT_GUARD_FAULT_OFFSET);
	after = guard_read(guard, FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET);
	if (status & ~(FES_DE25_SCANOUT_GUARD_STATUS_POISONED | FES_DE25_SCANOUT_GUARD_STATUS_PENDING |
	    FES_DE25_SCANOUT_GUARD_STATUS_ACCEPTED | FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET))
		return -EPROTO;
	if (fault || (status & FES_DE25_SCANOUT_GUARD_STATUS_POISONED))
		return -EIO;
	if (before != expected_epoch || after != expected_epoch ||
	    !!(status & FES_DE25_SCANOUT_GUARD_STATUS_EPOCH_SET) != !!expected_epoch)
		return -ESTALE;
	if (status & (FES_DE25_SCANOUT_GUARD_STATUS_PENDING | FES_DE25_SCANOUT_GUARD_STATUS_ACCEPTED))
		return -EBUSY;
	return 0;
}

static int guard_ready(struct fes_de25_dma_guard *guard)
{
	int ret;

	if (guard->poisoned)
		return -EIO;
	if (!guard->claimed)
		return -ENODEV;
	ret = guard_health(guard, guard->epoch);
	if (ret)
		guard->poisoned = true;
	return ret;
}

int fes_de25_guard_init(struct fes_de25_dma_guard *guard, void __iomem *base, size_t span)
{
	if (!guard || !base || !IS_ALIGNED((unsigned long)base, sizeof(u32)) ||
	    span != FES_DE25_SCANOUT_CONTROL_SPAN)
		return -EINVAL;
	if (guard->base)
		return -EBUSY;
	mutex_init(&guard->lock);
	guard->base = base;
	return 0;
}

int fes_de25_guard_claim(struct fes_de25_dma_guard *guard, u32 epoch)
{
	int ret;

	if (!guard || !guard->base || !epoch)
		return -EINVAL;
	mutex_lock(&guard->lock);
	if (guard->poisoned || guard->claimed) {
		ret = -EBUSY;
		goto out;
	}
	/* Never overwrite or adopt an existing owner's epoch. */
	ret = guard_health(guard, 0);
	if (!ret) {
		guard_write(guard, FES_DE25_SCANOUT_GUARD_EPOCH_OFFSET, epoch);
		mb();
		ret = guard_health(guard, epoch);
	}
	if (ret) {
		guard->poisoned = true;
		goto out;
	}
	guard->epoch = epoch;
	guard->claimed = true;
out:
	mutex_unlock(&guard->lock);
	return ret;
}

u64 fes_de25_guard_epoch(struct fes_de25_dma_guard *guard)
{
	u64 epoch = 0;

	if (!guard || !guard->base)
		return 0;
	mutex_lock(&guard->lock);
	if (!guard_ready(guard))
		epoch = guard->epoch;
	mutex_unlock(&guard->lock);
	return epoch;
}

int fes_de25_guard_read(struct fes_de25_dma_guard *guard, u32 offset, u32 *value)
{
	u32 data;
	int ret;

	if (!value)
		return -EINVAL;
	*value = 0;
	if (!guard || !guard->base || !IS_ALIGNED(offset, sizeof(u32)) || offset >= FES_DE25_SCANOUT_SPAN)
		return -EINVAL;
	mutex_lock(&guard->lock);
	ret = guard_ready(guard);
	if (!ret) {
		data = guard_read(guard, offset);
		mb();
		ret = guard_ready(guard);
		if (!ret)
			*value = data;
	}
	mutex_unlock(&guard->lock);
	return ret;
}

int fes_de25_guard_write(struct fes_de25_dma_guard *guard, u32 offset, u32 value)
{
	int ret;

	if (!guard || !guard->base || !IS_ALIGNED(offset, sizeof(u32)) || offset >= FES_DE25_SCANOUT_SPAN)
		return -EINVAL;
	mutex_lock(&guard->lock);
	ret = guard_ready(guard);
	if (!ret) {
		guard_write(guard, offset, value);
		mb();
		ret = guard_ready(guard);
	}
	mutex_unlock(&guard->lock);
	return ret;
}

int fes_de25_guard_fence(struct fes_de25_dma_guard *guard)
{
	int ret;

	if (!guard || !guard->base)
		return -EINVAL;
	mutex_lock(&guard->lock);
	mb();
	ret = guard_ready(guard);
	mutex_unlock(&guard->lock);
	return ret;
}
