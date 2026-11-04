/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stddef.h>
#include <stdint.h>
#include "payload.h"
#include "vendor/puff/puff.h"

static uint32_t little32(const uint8_t *p)
{
    return (uint32_t)p[0] | (uint32_t)p[1] << 8 |
           (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}

static uint32_t crc32(const uint8_t *p, uint64_t length)
{
    static const uint32_t table[16] = {
        0x00000000U, 0x1db71064U, 0x3b6e20c8U, 0x26d930acU,
        0x76dc4190U, 0x6b6b51f4U, 0x4db26158U, 0x5005713cU,
        0xedb88320U, 0xf00f9344U, 0xd6d6a3e8U, 0xcb61b38cU,
        0x9b64c2b0U, 0x86d3d2d4U, 0xa00ae278U, 0xbdbdf21cU
    };
    uint32_t value = 0xffffffffU;
    for (uint64_t i = 0; i < length; i++) {
        value ^= p[i];
        value = (value >> 4) ^ table[value & 15U];
        value = (value >> 4) ^ table[value & 15U];
    }
    return ~value;
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
        input != size - 18 || output != expected ||
        crc32(destination, output) != little32(source + size - 8)) return 0;
    *written = output;
    return 1;
}
