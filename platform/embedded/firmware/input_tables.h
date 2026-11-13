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
int nano_sdm_input_validate(const struct nano_sdm_input_binding *);
int nano_sdm_input_same(const struct nano_sdm_input_binding *,const struct nano_sdm_input_binding *);
/* Pure descriptor values, at most256 output bytes, no I/O/publication/cache
 * operations. Invalid inputs leave output unchanged. Platform stores must be
 * little-endian, under fresh ownership. Source/output host storage is disjoint.
 * Queue areas and spare pages are zero in this unpublished expected image. */
int nano_sdm_input_words(const struct nano_sdm_input_binding *,uint32_t offset,uint64_t *,unsigned words);
int nano_sdm_input_table_range(uint32_t offset,uint32_t bytes);
#endif
