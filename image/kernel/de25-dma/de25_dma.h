/* SPDX-License-Identifier: GPL-2.0 WITH Linux-syscall-note */
#ifndef FES_DE25_DMA_H
#define FES_DE25_DMA_H

#include <linux/ioctl.h>
#include <linux/types.h>

/* A separate, currently unbound diagnostic ABI; not the DE10 fes-window ABI. */
#define FES_DE25_DMA_ABI_VERSION 3

struct fes_de25_dma_publish {
	__u64 generation;
	__u32 sequence;
	__u32 slot;
	__u32 version;
	__u32 reserved;
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
	__u32 accepted_sequence;
	__u32 displayed_sequence;
	__u32 displayed_slot;
	__u32 pending_sequence;
	__u32 pending_slot;
	__u32 back_slot;
	__u32 hardware_status;
	__u32 hardware_errors;
	__u32 underflows;
	__u32 outstanding;
	/* Immutable observed hardware build identity, least-significant word first.
	 * No physical or DMA address is exposed by these opaque identity words.
	 */
	__u32 build_id[4];
};

#define FES_DE25_DMA_REVOKED 1U
#define FES_DE25_DMA_EXPOSED 2U
#define FES_DE25_DMA_UNCERTAIN 4U
#define FES_DE25_DMA_PENDING 8U
#define FES_DE25_DMA_NO_SLOT (~0U)

/* Neither request accepts or returns CPU physical addresses or DMA addresses. */
#define FES_DE25_DMA_INFO _IOR(0xD5, 0x01, struct fes_de25_dma_info)
#define FES_DE25_DMA_PUBLISH _IOW(0xD5, 0x02, struct fes_de25_dma_publish)
#define FES_DE25_DMA_REVOKE _IO(0xD5, 0x03)
/* POLL input requires current generation, version and zero reserved. */
#define FES_DE25_DMA_POLL _IOWR(0xD5, 0x04, struct fes_de25_dma_info)

/* Permanent owner control: no argument. ALLOC returns a CLOEXEC grant fd.
 * REAP revokes the active grant using kernel hardware proof, then releases the
 * owner's recovery handle. Client storage references still retain its lease.
 */
#define FES_DE25_DMA_OWNER_ALLOC _IO(0xD5, 0x10)
#define FES_DE25_DMA_OWNER_REAP _IO(0xD5, 0x11)
/* Read-only reference check: 0 when ready with no active allocation or lease,
 * -EBUSY while any allocation/client/VMA retains the lease, -ENODEV when the
 * profile is not ready. No argument, MMIO, drain, REAP or lease acquisition.
 * A result is only an observation; callers still serialize transitions with
 * their maintenance lease and validate the configuration separately.
 */
#define FES_DE25_DMA_OWNER_IDLE _IO(0xD5, 0x12)
/* Read-only ABI version, returned as the positive ioctl result. Query before
 * ALLOC so an older kernel cannot leave an unwanted legacy allocation behind.
 */
#define FES_DE25_DMA_OWNER_ABI _IO(0xD5, 0x13)

#endif
