/* SPDX-License-Identifier: GPL-2.0 */
#ifndef FES_DE25_DMA_OWNER_H
#define FES_DE25_DMA_OWNER_H
#include <linux/types.h>

struct platform_device;
struct device;
struct file;
struct fes_de25_dma_guard;
struct fes_de25_owner;

int fes_de25_owner_probe(struct platform_device *pdev);
void fes_de25_owner_remove(struct platform_device *pdev);

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
/* Kernel software fixture only: no physical binding or IOMMU qualification. */
struct fes_de25_owner *fes_de25_owner_test_create(struct device *dev,
		struct fes_de25_dma_guard *guard);
struct file *fes_de25_owner_test_allocate(struct fes_de25_owner *owner);
int fes_de25_owner_test_reap(struct fes_de25_owner *owner);
int fes_de25_owner_test_notify(struct fes_de25_owner *owner, unsigned long action);
int fes_de25_owner_test_override(struct fes_de25_owner *owner, const char *name);
int fes_de25_owner_test_ancestor(struct fes_de25_owner *owner, unsigned long action);
void fes_de25_owner_test_destroy(struct fes_de25_owner *owner);
#endif
#endif
