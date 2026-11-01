/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_SD_H
#define FES_NANO_SD_H
#include "nano_bridges.h"
#include "generated/fes_embedded_sd.h"

/* Trusted platform qualification, never accepted from an SMC argument. */
#define FES_NANO_SD_BOOT_SCOPE 1U
#define FES_NANO_SD_CONTROLLER_RESET 2U
#define FES_NANO_SD_CLOCK_PHY 4U
#define FES_NANO_SD_PINMUX 8U
#define FES_NANO_SD_PIO_STOP 16U
#define FES_NANO_SD_QUALIFIED 0x1fU
struct fes_nano_sd_ops {
    void *context;
    /* Independently verify the same boot owner/epoch, parked CPUs, other HPS
     * masters, closed bridges and the expected inactive/active SD reset scope.
     * Must not call the all-masters-contained cold owns callback while active. */
    int (*scope)(void *, uint64_t owner, uint64_t epoch, unsigned active);
    int (*resume)(void *); /* establish controller access; no card commands */
    int (*prepare)(void *); /* after host reset/internal clock, before card clock */
    int (*stop)(void *); /* independently drain, stop clock, assert/read back resets */
};
struct fes_nano_sd {
    struct fes_nano_bridges *bridges;
    struct fes_nano_sd_ops ops;
    uint64_t owner, epoch;
    unsigned attempted, initialized, failed, active, prepared;
    int first_error;
};
#ifdef __cplusplus
extern "C" {
#endif
/* Zero once per boot. Secure setup only; caller holds the shared platform lock. */
int fes_nano_sd_init(struct fes_nano_sd *, struct fes_nano_bridges *,
                     struct fes_nano_sd_ops, unsigned qualifications);
/* Caller holds the same lock as bridge/configuration operations. */
void fes_nano_sd_sip(struct fes_nano_sd *, uint64_t operation, uint64_t owner,
                     uint64_t generation, unsigned configuring, uint64_t result[4]);
#ifdef __cplusplus
}
#endif
#endif
