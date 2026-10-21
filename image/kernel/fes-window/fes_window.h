/* SPDX-License-Identifier: GPL-2.0 WITH Linux-syscall-note */
/*
 * fes-window user interface. /dev/fes-window grants page-aligned ranges of
 * the reserved HPS DDR window that fpga2sdram ports read. Each grant is its
 * own file: map it shared, clean what was written before the fabric reads it,
 * and revoke it before that memory belongs to anything else.
 */
#ifndef FES_WINDOW_H
#define FES_WINDOW_H

#include <linux/ioctl.h>
#include <linux/types.h>

/* FES_WINDOW_GRANT on /dev/fes-window. */
struct fes_window_grant {
	__u64 offset;   /* window-relative, page aligned */
	__u64 length;   /* nonzero, page aligned */
	__s32 fd;       /* out: the grant file, close-on-exec */
	__u32 reserved; /* must be 0 */
};

/*
 * FES_WINDOW_CLEAN on a grant file: write back height rows of width bytes,
 * stride bytes apart, from the CPU caches to DDR.
 */
struct fes_window_clean {
	__u64 offset;   /* grant-relative byte offset of the first row */
	__u32 width;    /* bytes per row */
	__u32 height;   /* rows */
	__u32 stride;   /* bytes from one row to the next; unused when height is 1 */
	__u32 reserved; /* must be 0 */
};

#define FES_WINDOW_IOC_MAGIC 0xFE
#define FES_WINDOW_GRANT  _IOWR(FES_WINDOW_IOC_MAGIC, 0x01, struct fes_window_grant)
#define FES_WINDOW_CLEAN  _IOW(FES_WINDOW_IOC_MAGIC, 0x02, struct fes_window_clean)
/*
 * FES_WINDOW_REVOKE on a grant file: remove every process's mapping of the
 * grant, write back and invalidate its cached lines, and refuse later maps,
 * faults and cleans. Accesses through an old mapping raise SIGBUS.
 */
#define FES_WINDOW_REVOKE _IO(FES_WINDOW_IOC_MAGIC, 0x03)

#endif
