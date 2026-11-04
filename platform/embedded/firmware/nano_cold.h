/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_COLD_H
#define FES_NANO_COLD_H
#include "nano_boot.h"

/* Dedicated diagnostic cold/serial profile, not an active-device drain policy.
 * Shared by actual SPL and BL31; no NS initialization API. */
#define FES_NANO_COLD_MAGIC 0x464e4332U
#define FES_NANO_COLD_VERSION 2U
#define FES_NANO_COLD_COOKIE 0x464e433200000002ULL
#define FES_NANO_COLD_FLAGS 15U /* secure entry, matched entry, SD drained, initial CPU parking */
/* CS/R1 register map RC28: STAT[0] SDM cold, STAT[2] sticky last POR.
 * These flags are not boot-history proof; the SDM boot kind is independent. */
#define FES_NANO_COLD_CAUSE_ALLOWED 0x5U
/* RC28 PER0MODRST: bit7 resets the controller; bit15 resets SDMMC ECC OCP
 * diagnostics. The PIO SPL releases only bit7. Keep unused diagnostics held,
 * independently of proving the controller idle and stopping its card clock. */
#define FES_NANO_COLD_SD_RESET 0x80U
#define FES_NANO_COLD_SD_ECC_RESET 0x8000U
#define FES_NANO_COLD_USB_RESET 0x8U
#define FES_NANO_COLD_UNUSED (FES_NANO_MASTER_RESET & ~FES_NANO_COLD_SD_RESET)
#define FES_NANO_COLD_SD_BUSY 0xb07U
#define FES_NANO_COLD_POLLS 1000U
#define FES_NANO_COLD_RSTMGR 0x10d11000UL
#define FES_NANO_COLD_PWRMGR 0x10d14000UL
#define FES_NANO_COLD_CPU_CHANNELS 0x1eU
#define FES_NANO_COLD_BOOT_CORE 0xc00U
#define FES_NANO_COLD_SDHCI 0x10808200UL /* Cadence SRS: device base + 0x200 */
enum fes_nano_cold_register {
    FES_COLD_CAUSE, FES_COLD_PER0, FES_COLD_BRG, FES_COLD_CPUS, FES_COLD_RELEASE,
    FES_COLD_SD_STATE, FES_COLD_SD_MODE, FES_COLD_SD_CLOCK, FES_COLD_SD_INT, FES_COLD_SD_SIGNAL,
    FES_COLD_BOOT_KIND,
    FES_COLD_CPU0_POWER, FES_COLD_CPU1_POWER, FES_COLD_CPU2_POWER, FES_COLD_CPU3_POWER,
    FES_COLD_PCH_REQUEST, FES_COLD_PCH_STATUS, FES_COLD_BOOT_CONFIG,
    FES_COLD_REGISTER_COUNT
};
struct fes_nano_cold_io {
    void *context;
    int (*read)(void *, unsigned reg, uint32_t *);
    int (*write)(void *, unsigned reg, uint32_t);
    void (*barrier)(void *);
    void (*wait)(void *);
};
struct fes_nano_cold_sample {
    uint32_t cause, per0, bridge, cpus, release, boot_kind;
    uint32_t power[4], pch_request, pch_status, boot_config;
};
struct fes_nano_cold_record {
    uint32_t magic, version, size, checksum;
    struct fes_nano_boot_record boot;
    struct fes_nano_cold_sample first, last;
    uint32_t core, flags, sd_before, sd_after, sd_mode, sd_clock;
};
/* Local first-failure observations, never part of the sealed handoff record.
 * A stopped stage retains only reads already made by the original policy. */
enum fes_nano_cold_finish_stage {
    FES_COLD_FINISH_NONE, FES_COLD_FINISH_ENTRY, FES_COLD_FINISH_SAMPLE,
    FES_COLD_FINISH_INITIAL, FES_COLD_FINISH_STABLE, FES_COLD_FINISH_SD_RESET,
    FES_COLD_FINISH_MODE_READ, FES_COLD_FINISH_DMA,
    FES_COLD_FINISH_INT, FES_COLD_FINISH_SIGNAL, FES_COLD_FINISH_IDLE_BEFORE,
    FES_COLD_FINISH_CLOCK_READ, FES_COLD_FINISH_CLOCK_STOP,
    FES_COLD_FINISH_IDLE_AFTER, FES_COLD_FINISH_CLOCK_READBACK,
    FES_COLD_FINISH_CLOCK_CHECK, FES_COLD_FINISH_FUNCTION_RESET,
    FES_COLD_FINISH_ALL_RESET, FES_COLD_FINISH_SAMPLE_LAST,
    FES_COLD_FINISH_STABLE_LAST, FES_COLD_FINISH_RESET_CHECK,
    FES_COLD_FINISH_COMPLETE
};
struct fes_nano_cold {
    struct fes_nano_cold_io io;
    struct fes_nano_cold_record record;
    unsigned attempted, valid, failed;
    /* Secure read-only control transaction only. The trusted raw driver arms
     * the expected mask before its write, then owns() verifies the readback.
     * No non-secure operation can supply this field or open a data path. */
    unsigned control_expected;
    /* Private bounded DDR diagnostic only. The raw driver arms this exact
     * F2SDRAM0 bit while control is open; no SiP caller can supply it. */
    unsigned ddr_expected;
    struct fes_nano_cold_sample before;
    unsigned finish_stage;
    uint32_t sd_clock_before, reset_readback;
};
uint32_t fes_nano_cold_checksum(const struct fes_nano_cold_record *);
int fes_nano_cold_begin(struct fes_nano_cold *, struct fes_nano_cold_io,
                         const uint8_t chain[32], uint64_t owner, uint64_t epoch, unsigned core);
int fes_nano_cold_finish(struct fes_nano_cold *, unsigned entry_verified,
                          struct fes_nano_cold_record *);
int fes_nano_cold_consume(struct fes_nano_cold *, const struct fes_nano_cold_record *,
                           struct fes_nano_cold_io, const uint8_t chain[32], uint64_t owner);
int fes_nano_cold_owns(void *, uint64_t owner, uint64_t epoch);
int fes_nano_cold_control_target(struct fes_nano_cold *, uint64_t owner,
                                  uint64_t epoch, unsigned control_open);
int fes_nano_cold_ddr_target(struct fes_nano_cold *,uint64_t owner,uint64_t epoch);
int fes_nano_cold_ddr_close_target(struct fes_nano_cold *,uint64_t owner,uint64_t epoch);
/* Trusted SD platform only: validate the original boot with exactly SD's
 * controller reset released when active=1. This never grants bridge access
 * or changes the strict all-master semantics of cold_owns. */
int fes_nano_cold_sd_scope(struct fes_nano_cold *, uint64_t owner,
                           uint64_t epoch, unsigned active);
/* Separate USB-only PIO lease. SD and every other HPS master stay reset;
 * bridges stay closed. This never changes cold_owns or grants FPGA access. */
int fes_nano_cold_usb_scope(struct fes_nano_cold *, uint64_t owner,
                            uint64_t epoch, unsigned active);
int fes_nano_cold_quiesce(void *, uint32_t producers);
int fes_nano_cold_quiescent(void *, uint32_t *producers);
/* Strong implementation lives beside the cold profile's PSCI CPU_ON frontend.
 * Linking an observer alone must not supply secondary-release authority. */
int fes_nano_cold_cpu_release_locked(void);
#endif
