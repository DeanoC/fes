/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_FIRMWARE_RAM_H
#define FES_NANO_FIRMWARE_RAM_H
#include "nano_cold.h"
#include <stddef.h>

/* Private candidate exclusion at the final SPL branch, not a memory grant.
 * The matched ELF, entry and native-map contracts must qualify placement
 * separately before BL31 may use this range. */
#define FES_NANO_FW_RAM_BASE 0x00050000ULL
#define FES_NANO_FW_RAM_LIMIT 0x00060000ULL
#define FES_NANO_FW_RAM_BSS_BASE 0x00058000ULL
#define FES_NANO_FW_RAM_BSS_END 0x0005ddc0ULL
/* The array is file-backed within the existing reserved BL31 DDR image. */
#define FES_NANO_FW_PERCPU_COUNT 4U
#define FES_NANO_FW_PERCPU_STRIDE 64U
#define FES_NANO_FW_PERCPU_ALIGNMENT 64U
#define FES_NANO_FW_PERCPU_BYTES 256U
#define FES_NANO_FW_PERCPU_AFFINITY_OFFSET 24U
#define FES_NANO_FW_PERCPU_AFFINITY_BYTES 4U
#define FES_NANO_FW_RAM_STACK_FLOOR 0x00061000ULL
#define FES_NANO_FW_RAM_COOKIE 0x464e52414d000001ULL
#define FES_NANO_FW_RAM_UART_DATA 0x10c02100ULL
#define FES_NANO_FW_RAM_UART_STATUS 0x10c02114ULL
#define FES_NANO_FW_RAM_UART_POLLS 100000U
#define FES_NANO_FW_RAM_UART_BYTES 384U
/* The allocation-free EL3 entry reads this wire record before C/BSS exists. */
_Static_assert(sizeof(struct fes_nano_cold_record)==224 &&
               offsetof(struct fes_nano_cold_record,checksum)==12 &&
               offsetof(struct fes_nano_cold_record,boot)==16 &&
               offsetof(struct fes_nano_cold_record,core)==200 &&
               offsetof(struct fes_nano_cold_record,flags)==204,
               "early EL3 cold record layout changed");
_Static_assert(offsetof(struct fes_nano_boot_record,chain)==32 &&
               FES_NANO_MASTER_RESET==0xff21bfbfU,
               "early EL3 chain/reset layout changed");
struct fes_nano_spl_ram_view {
    uint64_t sp, gd_base, gd_bytes, early_base, early_bytes;
    uint64_t image_bytes, cold_base, cold_bytes, params_base;
    uint64_t heap_base, heap_bytes;
};
static inline int fes_nano_ram_within(uint64_t base,uint64_t bytes,uint64_t low,uint64_t high)
{ return bytes && base>=low && base<high && bytes<=high-base; }
static inline int fes_nano_spl_ram_excluded(const struct fes_nano_spl_ram_view *v)
{
    if(!v || (v->sp&15U) || v->sp<FES_NANO_FW_RAM_STACK_FLOOR ||
       v->sp>=0x71000ULL || !v->image_bytes || v->image_bytes>0x40000ULL ||
       (v->cold_base&7U) || v->cold_bytes!=sizeof(struct fes_nano_cold_record) ||
       !fes_nano_ram_within(v->cold_base,v->cold_bytes,0x1000ULL,v->image_bytes) ||
       (v->gd_base&15U) || v->gd_bytes>0x1000ULL ||
       !fes_nano_ram_within(v->gd_base,v->gd_bytes,FES_NANO_FW_RAM_LIMIT,0x6f000ULL) ||
       v->sp>v->gd_base || v->early_base!=0x6f000ULL || v->early_bytes!=0x2000ULL ||
       v->gd_base+v->gd_bytes>v->early_base || (v->params_base&7U) ||
       !fes_nano_ram_within(v->params_base,64,0xbff00000ULL,0xc0000000ULL) ||
       v->heap_base!=0xbfa00000ULL || v->heap_bytes!=0x500000ULL) return 0;
    return 1;
}
#endif
