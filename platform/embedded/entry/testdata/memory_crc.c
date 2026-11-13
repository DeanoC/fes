/* SPDX-License-Identifier: GPL-3.0-or-later */
/* QEMU-only payload: execute the actual target primitives at EL1, MMU off. */
#include <stdint.h>
#include "crc32.h"
#include "memory.h"

static uint8_t source[320], destination[352];

static void finish(int success) __attribute__((noreturn));
static void finish(int success)
{
    volatile uint32_t *uart = (volatile uint32_t *)0x09000000;
    const char *s = success ? "FES_ENTRY_MEMORY_CRC PASS\n" : "FES_ENTRY_MEMORY_CRC FAIL\n";
    while (*s) {
        for (unsigned polls = 0; polls < 1000000; polls++) {
            if (!(uart[0x18/4] & (1U << 5))) {
                uart[0] = (uint8_t)*s; break;
            }
        }
        s++;
    }
    for (;;) __asm__ volatile("wfe");
}

/* Independent bit-at-a-time IEEE oracle, without the target CRC instructions. */
static uint32_t reference(const uint8_t *p, unsigned length)
{
    uint32_t value = 0xffffffffU;
    while (length--) {
        value ^= *p++;
        for (unsigned bit = 0; bit < 8; bit++)
            value = (value >> 1) ^ ((0U - (value & 1)) & 0xedb88320U);
    }
    return ~value;
}

void fes_entry_test_main(void)
{
    if (!fes_entry_crc32_supported()) finish(0);
    for (unsigned i = 0; i < sizeof(source); i++) source[i] = (i*17+3)&255;
    for (unsigned length = 0; length <= 257; length++) {
        for (unsigned in = 0; in < 8; in++) {
            uint32_t checksum;
            if (!fes_entry_crc32(source+in, length, &checksum) ||
                checksum != reference(source+in, length)) finish(0);
            for (unsigned out = 0; out < 8; out++) {
                for (unsigned i = 0; i < sizeof(destination); i++) destination[i] = 0xa5;
                fes_entry_copy(destination+16+out, source+in, length);
                for (unsigned i = 0; i < sizeof(destination); i++) {
                    uint8_t expected = i < 16+out || i >= 16+out+length ? 0xa5 : source[in+i-16-out];
                    if (destination[i] != expected) finish(0);
                }
                fes_entry_zero(destination+16+out, length);
                for (unsigned i = 0; i < sizeof(destination); i++) {
                    uint8_t expected = i < 16+out || i >= 16+out+length ? 0xa5 : 0;
                    if (destination[i] != expected) finish(0);
                }
            }
        }
    }
    uint32_t checksum;
    if (!fes_entry_crc32((const uint8_t *)"123456789",9,&checksum) || checksum != 0xcbf43926U) finish(0);
    finish(1);
}
