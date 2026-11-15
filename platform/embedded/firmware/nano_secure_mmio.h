/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_SECURE_MMIO_H
#define FES_NANO_SECURE_MMIO_H

#include <stdint.h>
#include <arch_helpers.h>

/* Only selected BL31 builds install this header. Unrelated addresses keep
 * their original access path without a system-register read. The secure TCU
 * alias is read-only; this selector never redirects a TCU write to it. */
static inline uintptr_t fes_nano_secure_mmio_address(uintptr_t address,
                                                    unsigned write)
{
    uintptr_t page = address & ~(uintptr_t)0xfffU;
    uintptr_t alias;
    if (page == 0x10d12000UL)
        alias = 0xa0000UL;
    else if (!write && page == 0x16008000UL)
        alias = 0xb0000UL;
    else
        return address;
    return (read_sctlr_el3() & 1U) ? alias + (address & 0xfffU) : address;
}

#endif
