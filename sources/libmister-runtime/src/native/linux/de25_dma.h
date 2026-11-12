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

/* Independent additive diagnostics ABI; INFO/PUBLISH and ABI 3 stay fixed. */
#define FES_DE25_DMA_DIAGNOSTICS_VERSION 1U
#define FES_DE25_DMA_DIAGNOSTICS_VALID 1U

struct fes_de25_dma_diagnostics {
	__u64 generation;
	__u32 version;
	__u32 reserved;
	__u32 build_id[4];
	/* VALID authenticates the cached observation, including no first fault.
	 * The hardware first_fault_state valid bit separately reports a fault.
	 */
	__u32 flags;
	__u32 first_fault_state;
	__u32 first_fault_occupancy;
	__u32 first_fault_gaps;
};

/* Scanout1.3 companion: immutable cached pre-CLEAR retirement.
 * VALID is published only after same stored epoch/build/window STOP+drain,
 * successful CLEAR and cold confirmation. No address is exposed. */
#define FES_DE25_DMA_RETIREMENT_VERSION 1U
#define FES_DE25_DMA_RETIREMENT_VALID 1U
struct fes_de25_dma_retirement {
 __u64 generation;
 __u32 version, reserved;
 __u32 build_id[4];
 __u32 flags;
 __u32 allocation_bytes, slot_bytes, frame_bytes, width, height, stride, slots;
 __u32 hardware_status, hardware_errors, underflows, outstanding;
 __u32 accepted_sequence, displayed_sequence, current_sequence, slot_state;
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

/* Grant-only cached query: input current generation, version 1, reserved zero.
 * Other input fields are ignored. Always overwrites all output fields. No MMIO
 * or refresh occurs, even for an uncertain/revoked grant. Zero flags and raw
 * words mean no authenticated observation is available, not "no fault".
 */
#define FES_DE25_DMA_DIAGNOSTICS _IOWR(0xD5, 0x05, struct fes_de25_dma_diagnostics)

/* Grant-only cached query: generation/version1/reserved0 input; all output
 * overwritten. ENODATA until actual successful retirement. No MMIO, refresh,
 * REAP, CLEAR or lease change. The owned grant FD binds the hidden epoch;
 * VALID is historical retirement evidence, not a global programming fence. */
#define FES_DE25_DMA_RETIREMENT _IOWR(0xD5, 0x06, struct fes_de25_dma_retirement)

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
/* Cached active-grant metadata for a privileged serialized lifecycle. Returns
 * ENODATA without an owner handle. No MMIO, drain or reference release occurs.
 */
#define FES_DE25_DMA_OWNER_INFO _IOR(0xD5, 0x14, struct fes_de25_dma_info)

#endif
