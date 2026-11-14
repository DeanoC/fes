/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef FES_ENTRY_MEMORY_H
#define FES_ENTRY_MEMORY_H
#include <stdint.h>

/* The complete ELF plan must establish these disjoint, CPU-owned ranges.
 * Word accesses occur only with natural alignment and eight remaining bytes. */
void fes_entry_copy(uint8_t *destination, const uint8_t *source, uint64_t length);
void fes_entry_zero(uint8_t *destination, uint64_t length);
#endif
