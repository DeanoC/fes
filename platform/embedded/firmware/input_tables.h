/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef NANO_SDM_INPUT_TABLES_H
#define NANO_SDM_INPUT_TABLES_H
#include <stdint.h>
#define NANO_SDM_INPUT_BASE UINT64_C(0x94800000)
#define NANO_SDM_INPUT_MAX 0x7ff000U
#define NANO_SDM_INPUT_ARENA UINT64_C(0x94000000)
#define NANO_SDM_INPUT_ARENA_BYTES 0x10000U
#define NANO_SDM_INPUT_L1 0x4000U
#define NANO_SDM_INPUT_L2 0x5000U
/* Trusted secure capture, not an NS table request. Complete payload digest
 * verification and immutable ownership of each rounded source page must be
 * supplied by the platform under its existing CPU/master/configuration lock.
 * The final metadata page at94fff000 is never mapped. Byte permissions have
 * 4KiB granularity; page padding is owned source-window memory, not a claim
 * that those bytes belong to the verified payload or are zero. */
struct nano_sdm_input_binding {
 uint64_t owner,epoch,generation;
 uint32_t bytes,chunk;
 uint8_t digest384[48];
 uint32_t idr0,idr1,idr5;
};
/* The device may read (bit 0) or write (bit 1) this Normal Non-cacheable,
 * outer-shareable 4 KiB page. The callers bind the full physical address. */
static inline uint64_t nano_stage2_page(uint64_t address, unsigned access)
{
 if ((address & 4095U) || address > UINT32_MAX || access<1 || access>3)
  return 0;
 return address | (UINT64_C(3)<<53) | (1U<<10) | (2U<<8) | ((uint64_t)access<<6) | (5U<<2) | 3U;
}
#define NANO_MAC_POOL_BASE UINT64_C(0x95c00000)
/* Prospective MAC SID 1 view. No address outside the five whole pages of
 * the XGMAC driver allocation resolves. A descriptor value grants no DMA. */
static inline uint64_t nano_mac_stage2_leaf(uint64_t address)
{
 if (address<NANO_MAC_POOL_BASE || address>=NANO_MAC_POOL_BASE+5*4096U || (address&4095U))
  return 0;
 unsigned page=(unsigned)((address-NANO_MAC_POOL_BASE)/4096U);
 return nano_stage2_page(address, page==0 ? 3U : page<3 ? 1U : 2U);
}
int nano_sdm_input_validate(const struct nano_sdm_input_binding *);
int nano_sdm_input_same(const struct nano_sdm_input_binding *,const struct nano_sdm_input_binding *);
/* Pure descriptor values, at most256 output bytes, no I/O/publication/cache
 * operations. Invalid inputs leave output unchanged. Platform stores must be
 * little-endian, under fresh ownership. Source/output host storage is disjoint.
 * Queue areas and spare pages are zero in this unpublished expected image. */
int nano_sdm_input_words(const struct nano_sdm_input_binding *,uint32_t offset,uint64_t *,unsigned words);
int nano_sdm_input_table_range(uint32_t offset,uint32_t bytes);
#endif
