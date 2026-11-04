/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stdint.h>
#include "map.h"

extern const uint8_t __image_start[], __image_end[];

static int translated(uint64_t address, int expected)
{
    for (unsigned write = 0; write < 2; write++) {
        uint64_t par;
        if (write) __asm__ volatile("at s1e2w, %0" :: "r"(address) : "memory");
        else __asm__ volatile("at s1e2r, %0" :: "r"(address) : "memory");
        __asm__ volatile("isb\n\tmrs %0, par_el1" : "=r"(par) :: "memory");
        if (expected < 0) {
            if (!(par & 1)) return 0;
        } else if ((par & 1) || (par & 0xfffffff000ULL) != (address & ~4095ULL) ||
                   (par >> 56) != (uint64_t)expected) return 0;
    }
    return 1;
}

static int controls(void)
{
    uint64_t sctlr,tcr,ttbr,mair,hcr;
    __asm__ volatile("mrs %0, sctlr_el2\n\tmrs %1, tcr_el2\n\tmrs %2, ttbr0_el2\n\tmrs %3, mair_el2\n\tmrs %4, hcr_el2"
                     : "=r"(sctlr),"=r"(tcr),"=r"(ttbr),"=r"(mair),"=r"(hcr));
    return (sctlr & 0x1005) == 0x1005 && !(sctlr & (1ULL << 25)) &&
        tcr == FES_ENTRY_MAP_TCR && mair == FES_ENTRY_MAP_MAIR &&
        ttbr == (uintptr_t)&fes_entry_tables && !(hcr & ((1ULL << 34) | 1));
}

int fes_entry_cache_observe(void)
{
    uint64_t saved_par;
    __asm__ volatile("mrs %0, par_el1" : "=r"(saved_par));
    int valid = controls();
    /* Both ends of every possible RAM block, including invalid gaps. */
    uint64_t region = FES_ENTRY_RAM_START & ~((1ULL << 30)-1);
    uint64_t image_start = (uintptr_t)__image_start, image_end = (uintptr_t)__image_end;
    for (uint64_t i = 0; valid && i < 512; i++) {
        for (unsigned end = 0; valid && end < 2; end++) {
            uint64_t address = region+i*(2ULL << 20)+(end ? (2ULL << 20)-4096 : 0);
            int expected = (address >= FES_ENTRY_RAM_START && address < FES_ENTRY_RAM_END) ||
                (address >= image_start && address < image_end) ? 255 : -1;
            valid = translated(address, expected);
        }
    }
    /* Every loader page, including code, stack and all table storage. */
    for (uint64_t p = image_start; valid && p < image_end; p += 4096)
        valid = translated(p, 255);
    if (valid) valid = translated(FES_ENTRY_UART_BASE, 0);
    if (valid) valid = translated(0, -1);
    if (valid) valid = controls();
    __asm__ volatile("msr par_el1, %0\n\tisb" :: "r"(saved_par) : "memory");
    return valid;
}
