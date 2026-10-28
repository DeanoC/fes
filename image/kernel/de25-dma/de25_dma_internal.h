/* SPDX-License-Identifier: GPL-2.0 */
#ifndef FES_DE25_DMA_INTERNAL_H
#define FES_DE25_DMA_INTERNAL_H

#include <linux/device.h>
#include <linux/fs.h>
#include <linux/types.h>

#include "de25_dma.h"
#include "generated/fes_de25_ddr_probe.h"

#define FES_DE25_DMA_ALLOCATION_BYTES FES_DE25_DDR_PROBE_ALLOCATION_BYTES
#define FES_DE25_DMA_SLOT_BYTES FES_DE25_DDR_PROBE_SLOT_BYTES
#define FES_DE25_DMA_FRAME_BYTES FES_DE25_DDR_PROBE_FRAME_BYTES
#define FES_DE25_DMA_WIDTH FES_DE25_DDR_PROBE_FRAME_WIDTH
#define FES_DE25_DMA_HEIGHT FES_DE25_DDR_PROBE_FRAME_HEIGHT
#define FES_DE25_DMA_STRIDE FES_DE25_DDR_PROBE_FRAME_STRIDE
#define FES_DE25_DMA_SLOTS FES_DE25_DDR_PROBE_SLOT_COUNT

struct fes_de25_dma_descriptor {
	dma_addr_t dma;
	u32 span, offset, bytes;
	u64 generation;
	u32 sequence;
};

/* Private driver-controlled transport. There is no userspace proof operation.
 * start may have exposed DMA even when it returns an error. stop only stops
 * new requests; drain must establish all accepted traffic has retired in the
 * same nonzero external reset epoch. Neither callback is implemented for DE25
 * hardware yet. Production probe therefore refuses binding.
 */
struct fes_de25_dma_backend {
	u64 (*epoch)(void *context);
	int (*start)(void *context, const struct fes_de25_dma_descriptor *descriptor);
	int (*stop)(void *context, u64 generation);
	int (*drain)(void *context, u64 generation);
	void *context;
};

struct file *fes_de25_dma_create(struct device *dev,
		const struct fes_de25_dma_backend *backend);
int fes_de25_dma_publish(struct file *file,
		const struct fes_de25_dma_publish *publish);
int fes_de25_dma_revoke(struct file *file);
void fes_de25_dma_info(struct file *file, struct fes_de25_dma_info *info);
int fes_de25_dma_binding(struct device *dev);

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
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
