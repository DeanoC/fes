/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stdint.h>
#include "crc32.h"
#include "payload.h"

#ifdef __aarch64__
__asm__(".arch_extension crc");
typedef uint64_t word_alias __attribute__((__may_alias__));
#endif

int fes_entry_crc32_supported(void)
{
#ifdef __aarch64__
    uint64_t features;
    __asm__ volatile("mrs %0, id_aa64isar0_el1" : "=r"(features));
    return ((features >> 16) & 15) == 1;
#else
    return 1;
#endif
}

int fes_entry_crc32(const uint8_t *data, uint64_t length, uint32_t *result)
{
    if (!result) return 0;
    *result = 0;
    if ((!data && length) || length > FES_ENTRY_PAYLOAD_MAX ||
        (uintptr_t)data > UINTPTR_MAX - length || !fes_entry_crc32_supported()) return 0;
    uint32_t value = 0xffffffffU;
#ifdef __aarch64__
    while (length && ((uintptr_t)data & 7)) {
        uint32_t byte = *data++; length--;
        __asm__ volatile("crc32b %w0, %w0, %w1" : "+r"(value) : "r"(byte));
    }
    while (length >= 8) {
        uint64_t word = *(const word_alias *)data;
        __asm__ volatile("crc32x %w0, %w0, %1" : "+r"(value) : "r"(word));
        data += 8; length -= 8;
    }
    while (length--) {
        uint32_t byte = *data++;
        __asm__ volatile("crc32b %w0, %w0, %w1" : "+r"(value) : "r"(byte));
    }
#else
    static const uint32_t table[16] = {
        0x00000000U, 0x1db71064U, 0x3b6e20c8U, 0x26d930acU,
        0x76dc4190U, 0x6b6b51f4U, 0x4db26158U, 0x5005713cU,
        0xedb88320U, 0xf00f9344U, 0xd6d6a3e8U, 0xcb61b38cU,
        0x9b64c2b0U, 0x86d3d2d4U, 0xa00ae278U, 0xbdbdf21cU
    };
    while (length--) {
        value ^= *data++;
        value = (value >> 4) ^ table[value & 15U];
        value = (value >> 4) ^ table[value & 15U];
    }
#endif
    *result = ~value;
    return 1;
}
