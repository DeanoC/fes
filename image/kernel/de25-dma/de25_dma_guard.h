/* SPDX-License-Identifier: GPL-2.0 */
#ifndef FES_DE25_DMA_GUARD_H
#define FES_DE25_DMA_GUARD_H

#include <linux/io.h>
#include <linux/mutex.h>
#include <linux/types.h>

/* Private transport, not a hardware admission or userspace register API.
 * The owner must hold its resource/reconfiguration lease for every call and
 * supply a mapped, live system-clock guard window. The hardware proxy bounds
 * downstream accesses; it cannot rescue a dead HPS/bridge/guard clock itself.
 * A zero-initialized object is initialized once. After an error it is retired,
 * never rearmed in place. claim's epoch comes from the kernel loader's fresh
 * configuration, not a caller-supplied generation or a drain assertion.
 */
struct fes_de25_dma_guard {
	struct mutex lock;
	void __iomem *base;
	u32 epoch;
	bool claimed, poisoned;
#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
	u32 (*test_read)(void *context, u32 offset);
	void (*test_write)(void *context, u32 offset, u32 value);
	void *test_context;
#endif
};

int fes_de25_guard_init(struct fes_de25_dma_guard *guard, void __iomem *base, size_t span);
int fes_de25_guard_claim(struct fes_de25_dma_guard *guard, u32 epoch);
u64 fes_de25_guard_epoch(struct fes_de25_dma_guard *guard);
int fes_de25_guard_read(struct fes_de25_dma_guard *guard, u32 offset, u32 *value);
int fes_de25_guard_write(struct fes_de25_dma_guard *guard, u32 offset, u32 value);
int fes_de25_guard_fence(struct fes_de25_dma_guard *guard);

#endif
