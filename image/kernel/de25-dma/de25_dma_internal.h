/* SPDX-License-Identifier: GPL-2.0 */
#ifndef FES_DE25_DMA_INTERNAL_H
#define FES_DE25_DMA_INTERNAL_H

#include <linux/device.h>
#include <linux/fs.h>
#include <linux/types.h>

#include "de25_dma.h"
#include "generated/fes_de25_scanout.h"

#define FES_DE25_DMA_ALLOCATION_BYTES FES_DE25_SCANOUT_ALLOCATION_BYTES
#define FES_DE25_DMA_SLOT_BYTES FES_DE25_SCANOUT_SLOT_BYTES
#define FES_DE25_DMA_FRAME_BYTES FES_DE25_SCANOUT_FRAME_BYTES
#define FES_DE25_DMA_WIDTH FES_DE25_SCANOUT_FRAME_WIDTH
#define FES_DE25_DMA_HEIGHT FES_DE25_SCANOUT_FRAME_HEIGHT
#define FES_DE25_DMA_STRIDE FES_DE25_SCANOUT_FRAME_STRIDE
#define FES_DE25_DMA_SLOTS FES_DE25_SCANOUT_SLOT_COUNT

/* Private, exclusive driver-controlled register transport. Accessors must be
 * ordered and bounded even when the external clock is absent. barrier orders
 * completed writes before later accesses. No physical MMIO backend is admitted.
 * epoch is a nonzero external reset identity; register generation alone cannot
 * establish drain after reset. No userspace operation supplies either proof.
 * The caller must retain callback code/context, exclusive engine ownership and
 * device DMA/domain resources until final grant destruction, including uncertain
 * exposure. get_device does not retain devm resources or a detached domain.
 */
struct fes_de25_dma_backend {
	u64 (*epoch)(void *context);
	int (*read32)(void *context, u32 offset, u32 *value);
	int (*write32)(void *context, u32 offset, u32 value);
	int (*barrier)(void *context);
	void *context;
};

struct file *fes_de25_dma_create(struct device *dev,
		const struct fes_de25_dma_backend *backend);
int fes_de25_dma_publish(struct file *file,
		const struct fes_de25_dma_publish *publish);
int fes_de25_dma_revoke(struct file *file);
int fes_de25_dma_poll(struct file *file, struct fes_de25_dma_info *info);
void fes_de25_dma_info(struct file *file, struct fes_de25_dma_info *info);
int fes_de25_dma_binding(struct device *dev);

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
#include <linux/atomic.h>
u64 fes_de25_dma_test_next_generation(atomic64_t *counter);
struct completion;
struct fes_de25_dma_test_observer {
	unsigned int freed;
	struct completion *mmap_entered;
	struct completion *mmap_release;
	struct completion *revoked;
};
void fes_de25_dma_test_observe(struct file *file,
		struct fes_de25_dma_test_observer *observer);
#endif
#endif
