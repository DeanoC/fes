/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef FES_ENTRY_PAYLOAD_H
#define FES_ENTRY_PAYLOAD_H
#include <stdint.h>
#define FES_ENTRY_PAYLOAD_MAX (32ULL << 20)
#define FES_ENTRY_STACK_RESERVE 0x20000ULL
/* Decode one fixed-header gzip member into CPU-owned scratch. Failed output is
 * never a loadable ELF. No allocation, trailing member, or partial result. */
int fes_entry_unpack(const uint8_t *source, uint64_t size,
                     uint8_t *destination, uint64_t capacity, uint64_t *written);
#endif
