/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_PROBE_STOP_H
#define FES_NANO_PROBE_STOP_H
#include <stdint.h>

/* Secure platform helper for the inherited read-only DDR probe. The existing
 * controller separately stops its generation/cookie reader. This helper is
 * only one part of the complete FPGA/HPS producer inventory. No addresses,
 * reset writes, allocations or bridge authority are accepted here. */
#define FES_NANO_PROBE_STOP_POLLS 128U
struct fes_nano_probe_stop_io {
    void *context;
    int (*owns)(void *); /* exact boot/reset/bridge scope, returns exactly one */
    int (*read32)(void *, uint32_t offset, uint32_t *);
    int (*write32)(void *, uint32_t offset, uint32_t value);
    void (*wait)(void *); /* bounded platform delay; no command replay */
};
struct fes_nano_probe_snapshot {
    uint32_t identity, version, status, accepted, completed, errors;
    uint32_t sequence, generation_lo, generation_hi;
};
struct fes_nano_probe_stop {
    struct fes_nano_probe_stop_io io;
    struct fes_nano_probe_snapshot last;
    unsigned attempted, failed, done, cancel_issued, polls;
    int first_error;
};

#ifdef __cplusplus
extern "C" {
#endif
/* Zero-initialize per owned FPGA activation. Install only a qualified fixed
 * probe aperture. Call while control is open; never read it after control
 * closes. A failed/uncertain call retires this helper and keeps memory owned.
 * Successful repeats independently observe the same completed receipt. */
int fes_nano_probe_stop(struct fes_nano_probe_stop *);
#ifdef __cplusplus
}
#endif
#endif
