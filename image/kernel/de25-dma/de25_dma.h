/* SPDX-License-Identifier: GPL-2.0 WITH Linux-syscall-note */
#ifndef FES_DE25_DMA_H
#define FES_DE25_DMA_H

#include <linux/ioctl.h>
#include <linux/types.h>

/* A separate, currently unbound diagnostic ABI; not the DE10 fes-window ABI. */
#define FES_DE25_DMA_ABI_VERSION 1

struct fes_de25_dma_publish {
	__u64 generation;
	__u32 sequence;
	__u32 slot;
	__u64 reserved;
};

struct fes_de25_dma_info {
	__u64 generation;
	__u32 version;
	__u32 allocation_bytes;
	__u32 slot_bytes;
	__u32 frame_bytes;
	__u32 width;
	__u32 height;
	__u32 stride;
	__u32 slots;
	__u32 state;
	__u32 reserved;
};

#define FES_DE25_DMA_REVOKED 1U
#define FES_DE25_DMA_EXPOSED 2U
#define FES_DE25_DMA_UNCERTAIN 4U

/* Neither request accepts or returns CPU physical addresses or DMA addresses. */
#define FES_DE25_DMA_INFO _IOR(0xD5, 0x01, struct fes_de25_dma_info)
#define FES_DE25_DMA_PUBLISH _IOW(0xD5, 0x02, struct fes_de25_dma_publish)
#define FES_DE25_DMA_REVOKE _IO(0xD5, 0x03)

#endif
