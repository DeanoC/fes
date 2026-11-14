/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_SD_PLATFORM_H
#define FES_NANO_SD_PLATFORM_H
#include "nano_sd.h"
#include "nano_cold.h"

#define FES_NANO_SD_BASE 0x10808000UL
#define FES_NANO_SD_CLKMGR 0x10d10000UL
#define FES_NANO_SD_SYSMGR 0x10d12000UL
#define FES_NANO_SD_PHY_RESET 0x40U
#define FES_NANO_SD_INPUT_HZ 200000000U
#define FES_NANO_SD_IDENT_CLOCK 0x000efa03U
#define FES_NANO_SD_CLOCK_REGS 14U
/* Ordered, privileged MMIO. Every poll has a fixed ten-microsecond wait and
 * finite budget in the actual adapter. No default MMIO or NS-supplied ops. */
struct fes_nano_sd_io {
    void *context;
    int (*read)(void *, uintptr_t, uint32_t *);
    int (*write)(void *, uintptr_t, uint32_t);
    void (*barrier)(void *);
    void (*wait)(void *);
};
enum fes_nano_sd_stage {
    FES_SD_NONE, FES_SD_BOOT_SCOPE, FES_SD_CLOCK_SCOPE, FES_SD_ROUTE_SCOPE,
    FES_SD_RESET_ASSERT, FES_SD_PHY_RELEASE, FES_SD_CONTROLLER_RELEASE,
    FES_SD_HOST_RESET, FES_SD_PHY_CONFIG, FES_SD_PHY_READY,
    FES_SD_PREPARE_CLOCK, FES_SD_STOP_MODE, FES_SD_STOP_IDLE,
    FES_SD_STOP_CLOCK, FES_SD_STOP_RESET, FES_SD_COMPLETE
};
struct fes_nano_sd_platform {
    struct fes_nano_cold *cold;
    struct fes_nano_sd_io io;
    uint32_t clocks[FES_NANO_SD_CLOCK_REGS], observed[FES_NANO_SD_CLOCK_REGS];
    unsigned attempted, initialized, failed, active, prepared, stage;
    int first_error;
    uintptr_t last_address;
    uint32_t last_value, last_mask, last_expected;
};
int fes_nano_sd_platform_init(struct fes_nano_sd_platform *, struct fes_nano_cold *,
                              struct fes_nano_sd_io, unsigned qualifications);
struct fes_nano_sd_ops fes_nano_sd_platform_ops(struct fes_nano_sd_platform *);
#endif
