/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_COLD_H
#define FES_NANO_COLD_H
#include "nano_boot.h"

/* Dedicated diagnostic cold/serial profile, not an active-device drain policy.
 * Shared by actual SPL and BL31; no NS initialization API. */
#define FES_NANO_COLD_MAGIC 0x464e4331U
#define FES_NANO_COLD_COOKIE 0x464e433100000001ULL
#define FES_NANO_COLD_FLAGS 7U /* secure entry, matched preloaded entry, SD drained */
#define FES_NANO_COLD_SD_RESET 0x8080U
#define FES_NANO_COLD_UNUSED (FES_NANO_MASTER_RESET & ~FES_NANO_COLD_SD_RESET)
#define FES_NANO_COLD_SD_BUSY 0xb07U
#define FES_NANO_COLD_POLLS 1000U
#define FES_NANO_COLD_RSTMGR 0x10d11000UL
#define FES_NANO_COLD_SDHCI 0x10808200UL /* Cadence SRS: device base + 0x200 */
enum fes_nano_cold_register {
    FES_COLD_CAUSE, FES_COLD_PER0, FES_COLD_BRG, FES_COLD_CPUS, FES_COLD_RELEASE,
    FES_COLD_SD_STATE, FES_COLD_SD_MODE, FES_COLD_SD_CLOCK, FES_COLD_SD_INT, FES_COLD_SD_SIGNAL,
    FES_COLD_BOOT_KIND
};
struct fes_nano_cold_io {
    void *context;
    int (*read)(void *, unsigned reg, uint32_t *);
    int (*write)(void *, unsigned reg, uint32_t);
    void (*barrier)(void *);
    void (*wait)(void *);
};
struct fes_nano_cold_sample { uint32_t cause, per0, bridge, cpus, release, boot_kind; };
struct fes_nano_cold_record {
    uint32_t magic, version, size, checksum;
    struct fes_nano_boot_record boot;
    struct fes_nano_cold_sample first, last;
    uint32_t core, flags, sd_before, sd_after, sd_mode, sd_clock;
};
struct fes_nano_cold {
    struct fes_nano_cold_io io;
    struct fes_nano_cold_record record;
    unsigned attempted, valid, failed;
};
uint32_t fes_nano_cold_checksum(const struct fes_nano_cold_record *);
int fes_nano_cold_begin(struct fes_nano_cold *, struct fes_nano_cold_io,
                         const uint8_t chain[32], uint64_t owner, uint64_t epoch, unsigned core);
int fes_nano_cold_finish(struct fes_nano_cold *, unsigned entry_verified,
                          struct fes_nano_cold_record *);
int fes_nano_cold_consume(struct fes_nano_cold *, const struct fes_nano_cold_record *,
                           struct fes_nano_cold_io, const uint8_t chain[32], uint64_t owner);
int fes_nano_cold_owns(void *, uint64_t owner, uint64_t epoch);
int fes_nano_cold_quiesce(void *, uint32_t producers);
int fes_nano_cold_quiescent(void *, uint32_t *producers);
#endif
