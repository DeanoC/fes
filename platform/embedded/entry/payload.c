/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stddef.h>
#include <stdint.h>
#include "payload.h"
#include "crc32.h"
#include "vendor/puff/puff.h"

#ifdef FES_ENTRY_UART_BASE
void fes_entry_checkpoint(const char *phase);
#define CHECKPOINT(phase) fes_entry_checkpoint(phase)
#else
#define CHECKPOINT(phase) ((void)0)
#endif

static uint32_t little32(const uint8_t *p)
{
    return (uint32_t)p[0] | (uint32_t)p[1] << 8 |
           (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}

int fes_entry_unpack(const uint8_t *source, uint64_t size,
                     uint8_t *destination, uint64_t capacity, uint64_t *written)
{
    static const uint8_t header[10] = {0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 255};
    if (!written) return 0;
    *written = 0;
    if (!source || !destination || size <= 18 || size > FES_ENTRY_PAYLOAD_MAX ||
        !capacity || capacity > FES_ENTRY_PAYLOAD_MAX ||
        (uintptr_t)source > UINTPTR_MAX - size ||
        (uintptr_t)destination > UINTPTR_MAX - capacity ||
        ((uintptr_t)source < (uintptr_t)destination + capacity &&
         (uintptr_t)destination < (uintptr_t)source + size)) return 0;
    for (unsigned i = 0; i < sizeof(header); i++)
        if (source[i] != header[i]) return 0;
    uint32_t expected = little32(source + size - 4);
    if (expected < 64 || expected > capacity) return 0;
    unsigned long input = (unsigned long)(size - 18), output = expected;
    if (puff(destination, &output, source + 10, &input) != 0 ||
        input != size - 18 || output != expected) return 0;
    CHECKPOINT("inflate_end");
    uint32_t checksum;
    if (!fes_entry_crc32(destination, output, &checksum)) return 0;
    CHECKPOINT("checksum_end");
    if (checksum != little32(source + size - 8)) return 0;
    *written = output;
    return 1;
}
