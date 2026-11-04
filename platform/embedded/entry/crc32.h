/* SPDX-License-Identifier: GPL-3.0-or-later */
#ifndef FES_ENTRY_CRC32_H
#define FES_ENTRY_CRC32_H
#include <stdint.h>

/* AArch64 requires its architectural IEEE CRC32 capability. The host build
 * supplies the portable oracle; it never executes target instructions. */
int fes_entry_crc32_supported(void);
int fes_entry_crc32(const uint8_t *data, uint64_t length, uint32_t *result);
#endif
