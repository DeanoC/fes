/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_MMU_MEMORY_H
#define FES_NANO_MMU_MEMORY_H
#include <stdint.h>
/* One private 64-byte line, under exclusive cold ownership; not an NS API.
 * Maintenance must clean/invalidate the accessible data cache hierarchy. */
int fes_nano_mmu_memory_probe(volatile uint64_t *, void *, int (*)(void *));
#endif
