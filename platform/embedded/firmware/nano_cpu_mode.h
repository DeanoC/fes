/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_CPU_MODE_H
#define FES_NANO_CPU_MODE_H
/* The selected profile checks its complete live register set. Cold allowance
 * ends at the first nonsecure entry or attempted enable; failures never fall
 * back to the original cache-off mode. Default builds keep the existing rule. */
#if defined(FES_NANO_FIRMWARE_MMU) && FES_NANO_FIRMWARE_MMU
#if !defined(FES_NANO_PROGRAM_ROLES) || !FES_NANO_PROGRAM_ROLES || \
    !defined(FES_NANO_COLD_ROM) || !FES_NANO_COLD_ROM || \
    !defined(FES_NANO_SHARED_USB) || !FES_NANO_SHARED_USB || \
    !defined(FES_NANO_CONTROLLED_SD) || !FES_NANO_CONTROLLED_SD || \
    !defined(FES_NANO_READER_CPU_BASE)
#error Static_EL3_MMU_requires_complete_mixed_ROM_shared_USB_and_SD_profile
#endif
#include "nano_mmu_platform.h"
#define FES_COLD_CPU_MODE() fes_nano_mmu_platform_scope()
#define FES_COLD_CPU_MODE_FROM(value) fes_nano_mmu_platform_scope()
#else
#define FES_COLD_CPU_MODE() (!(read_sctlr_el3()&5U))
#define FES_COLD_CPU_MODE_FROM(value) (!((value)&5U))
#endif
#endif
