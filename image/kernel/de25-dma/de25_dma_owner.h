/* SPDX-License-Identifier: GPL-2.0 */
#ifndef FES_DE25_DMA_OWNER_H
#define FES_DE25_DMA_OWNER_H
#include <linux/types.h>

struct platform_device;
struct device;
struct file;
struct fes_de25_dma_guard;
struct fes_de25_owner;
struct device_node;

int fes_de25_owner_probe(struct platform_device *pdev);
void fes_de25_owner_remove(struct platform_device *pdev);

#if IS_ENABLED(CONFIG_FES_DE25_DMA_KUNIT_TEST)
/* Kernel software fixture only: no physical binding or IOMMU qualification. */
struct fes_de25_owner *fes_de25_owner_test_create(struct device *dev,
		struct fes_de25_dma_guard *guard);
int fes_de25_owner_test_attach(struct fes_de25_owner *owner, struct device_node *region,
		struct fes_de25_dma_guard *transport);
void fes_de25_owner_test_detach(struct fes_de25_owner *owner);
long fes_de25_owner_test_ioctl(struct fes_de25_owner *owner, unsigned int command,
		unsigned long argument);
struct file *fes_de25_owner_test_allocate(struct fes_de25_owner *owner);
int fes_de25_owner_test_reap(struct fes_de25_owner *owner);
int fes_de25_owner_test_notify(struct fes_de25_owner *owner, unsigned long action);
int fes_de25_owner_test_override(struct fes_de25_owner *owner, const char *name);
int fes_de25_owner_test_ancestor(struct fes_de25_owner *owner, unsigned long action);
int fes_de25_owner_test_dependencies(struct device *dev);
int fes_de25_owner_test_window(struct fes_de25_owner *owner,
		u32 (*read)(void *context, u32 offset),
		void (*write)(void *context, u32 offset, u32 value), void *context);
struct file *fes_de25_owner_test_window_allocate(struct fes_de25_owner *owner);
void fes_de25_owner_test_destroy(struct fes_de25_owner *owner);
#endif
#endif
