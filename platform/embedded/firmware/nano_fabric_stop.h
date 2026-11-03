/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_FABRIC_STOP_H
#define FES_NANO_FABRIC_STOP_H
#include "nano_probe_stop.h"

/* The fixed-burst and fixed-window DDR diagnostics have two live readers and two maintenance masters
 * held in reset from configuration. A qualified adapter binds their complete
 * sealed inventory and apertures; this helper accepts no address, reset,
 * bridge, memory release or configuration callback. */
#define FES_NANO_FABRIC_STOP_POLLS 128U
struct fes_nano_fabric_stop_io {
    void *context;
    int (*owns)(void *); /* exact activation/CPU/bridge scope, exactly one */
    int (*read32)(void *, uint32_t offset, uint32_t *);
    int (*write32)(void *, uint32_t offset, uint32_t value);
    void (*wait)(void *);
};
struct fes_nano_fabric_snapshot { uint32_t words[22]; };
struct fes_nano_fabric_stop {
    struct fes_nano_fabric_stop_io io;
    struct fes_nano_probe_stop probe;
    uint32_t abi, build[4], bus_base, bytes;
    uint64_t cookie;
    unsigned qualified, attempted, failed, done, disable_issued, polls, reads;
    int first_error;
    struct fes_nano_fabric_snapshot last;
};
#ifdef __cplusplus
extern "C" {
#endif
/* Zero-initialize once per owned activation. Stop uses only DISABLE and the
 * inherited probe's CANCEL; it never starts/restarts either reader. Failed or
 * uncertain work is retired without replay. All accesses require open control.
 * Successful repeats re-observe both receipts, without issuing commands. */
int fes_nano_fabric_stop(struct fes_nano_fabric_stop *);
/* Independent readback after stop. Returns 0 only for the same complete
 * receipts. The caller retains them after control closes, without more MMIO. */
int fes_nano_fabric_stopped(struct fes_nano_fabric_stop *);
#ifdef __cplusplus
}
#endif
#endif
