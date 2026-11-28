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
    /* Optional trusted compiled return policy. No I/O; return exactly one only
     * for this platform's retained closure. Absence keeps held-data-only return.
     * Live scope checks remain required around each SD operation. */
    int (*returned)(void *, const struct fes_nano_bridges *, uint64_t *generation);
};
struct fes_nano_sd;
/* Optional compiled shared-coexistence policy. valid() returns 1 when the
 * trusted platform binding holds. contained() checks live controller/master
 * readiness for concurrent partner calls. Zero leaves legacy closed-bridge mode. */
struct fes_nano_sd_shared_ops {
    void *context;
    int (*valid)(void *, const struct fes_nano_bridges *, const struct fes_nano_sd *);
    int (*contained)(void *, uint64_t owner, uint64_t epoch, unsigned active, unsigned prepared);
    unsigned allow_during_config;
};
struct fes_nano_sd {
    struct fes_nano_bridges *bridges;
    struct fes_nano_sd_ops ops;
    uint64_t owner, epoch;
    uint64_t return_generation, grant_generation, stopped_generation;
    uint64_t generation; /* independent SD generation when shared.valid is set */
    unsigned attempted, initialized, failed, active, prepared, reservation;
    int first_error;
    struct fes_nano_sd_shared_ops shared;
};
#ifdef __cplusplus
extern "C" {
#endif
/* Zero once per boot. Secure setup only; caller holds the shared platform lock. */
int fes_nano_sd_init(struct fes_nano_sd *, struct fes_nano_bridges *,
                     struct fes_nano_sd_ops, unsigned qualifications);
int fes_nano_sd_init_shared(struct fes_nano_sd *, struct fes_nano_bridges *,
                            struct fes_nano_sd_ops, unsigned qualifications,
                            struct fes_nano_sd_shared_ops);
/* Caller holds the same lock as bridge/configuration operations. */
void fes_nano_sd_sip(struct fes_nano_sd *, uint64_t operation, uint64_t owner,
                     uint64_t generation, unsigned configuring, uint64_t result[4]);
/* Shared dispatcher gate: 0 permits concurrent partner calls, 1 means busy,
 * -1 retires shared authority. Closed-bridge mode returns 0 (uses device_lease). */
int fes_nano_sd_gate(struct fes_nano_sd *, unsigned for_config);
/* Check a completed SD cycle against its retained policy-verified return.
 * Used only by trusted configuration completion under the shared lock. */
int fes_nano_sd_returned(struct fes_nano_sd *, uint64_t owner,
                         uint64_t return_generation, uint64_t generation);
#ifdef __cplusplus
}
#endif
#endif
