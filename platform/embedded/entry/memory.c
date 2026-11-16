/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stdint.h>
#include "memory.h"

typedef uint64_t word_alias __attribute__((__may_alias__));

void fes_entry_copy(uint8_t *destination, const uint8_t *source, uint64_t length)
{
    volatile uint8_t *out = destination;
    /* Incongruent alignment requires byte accesses for the complete range. */
    if (((uintptr_t)out ^ (uintptr_t)source) & 7) {
        while (length--) *out++ = *source++;
        return;
    }
    while (length && ((uintptr_t)out & 7)) {
        *out++ = *source++; length--;
    }
    while (length >= 8) {
        *(volatile word_alias *)out = *(const word_alias *)source;
        out += 8; source += 8; length -= 8;
    }
    while (length--) *out++ = *source++;
}

void fes_entry_zero(uint8_t *destination, uint64_t length)
{
    volatile uint8_t *out = destination;
    while (length && ((uintptr_t)out & 7)) {
        *out++ = 0; length--;
    }
    while (length >= 8) {
        *(volatile word_alias *)out = 0;
        out += 8; length -= 8;
    }
    while (length--) *out++ = 0;
}
