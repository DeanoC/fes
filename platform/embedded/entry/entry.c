/* SPDX-License-Identifier: GPL-3.0-or-later */
#include <stddef.h>
#include <stdint.h>
#include "elf_plan.h"
#include "payload.h"

#ifndef FES_ENTRY_RAM_START
#error FES_ENTRY_RAM_START must be supplied by the selected diagnostic profile
#endif
#ifndef FES_ENTRY_RAM_END
#error FES_ENTRY_RAM_END must be supplied by the selected diagnostic profile
#endif
#ifndef FES_ENTRY_UART_BASE
#error FES_ENTRY_UART_BASE must be supplied by the selected diagnostic profile
#endif

extern const uint8_t __image_start[], __image_end[], __payload_start[], __payload_end[];
extern uint8_t __stack_top[];
void fes_entry_enter_el1(uint64_t entry, uint64_t stack) __attribute__((noreturn));
void fes_entry_park(void) __attribute__((noreturn));

/* Freestanding GCC may use these for the small local plan. */
void *memset(void *destination, int c, size_t length)
{
    uint8_t *out = destination;
    for (size_t i = 0; i < length; i++) out[i] = (uint8_t)c;
    return destination;
}

void *memcpy(void *destination, const void *source, size_t length)
{
    uint8_t *out = destination;
    const uint8_t *in = source;
    for (size_t i = 0; i < length; i++) out[i] = in[i];
    return destination;
}

static void putc(char value)
{
    volatile uint32_t *uart = (volatile uint32_t *)(uintptr_t)FES_ENTRY_UART_BASE;
    for (unsigned i = 0; i < 1000000; i++) {
#ifdef FES_ENTRY_QEMU_DIAGNOSTIC
        if ((uart[0x18 / 4] & (1U << 5)) == 0) {
#else
        if (uart[0x14 / 4] & (1U << 5)) {
#endif
            uart[0] = (uint32_t)(uint8_t)value;
            return;
        }
    }
}

static void puts(const char *s)
{
    while (*s) putc(*s++);
}

void fes_entry_bad_el(void) { puts("FES_ENTRY REJECT EL\n"); }
void fes_entry_bad_state(void) { puts("FES_ENTRY REJECT MMU_CACHE\n"); }
void fes_entry_bad_cpu(void) { puts("FES_ENTRY REJECT CPU\n"); }
void fes_entry_exception_c(void) { puts("FES_ENTRY EXCEPTION\n"); }

void fes_entry_main(void)
{
    struct fes_entry_plan plan;
    uint64_t image_start = (uintptr_t)__image_start, image_end = (uintptr_t)__image_end;
    uint64_t stored_size = (uintptr_t)__payload_end - (uintptr_t)__payload_start;
    uint64_t payload_size = 0;
    puts("FES_ENTRY EL2 CLEAN\n");
    if (FES_ENTRY_RAM_END <= FES_ENTRY_RAM_START ||
        FES_ENTRY_RAM_END - FES_ENTRY_RAM_START <= FES_ENTRY_PAYLOAD_MAX + FES_ENTRY_STACK_RESERVE + 0x10000) {
        puts("FES_ENTRY REJECT MEMORY\n");
        fes_entry_park();
    }
    uint64_t decode_base = FES_ENTRY_RAM_END - FES_ENTRY_STACK_RESERVE - FES_ENTRY_PAYLOAD_MAX;
    uint8_t *payload = (uint8_t *)(uintptr_t)decode_base;
    if ((image_start < decode_base + FES_ENTRY_PAYLOAD_MAX && decode_base < image_end) ||
        !fes_entry_unpack(__payload_start, stored_size, payload, FES_ENTRY_PAYLOAD_MAX, &payload_size)) {
        puts("FES_ENTRY REJECT PAYLOAD\n");
        fes_entry_park();
    }
    if (!fes_entry_plan_decoded_elf(payload, payload_size, FES_ENTRY_RAM_START,
                            FES_ENTRY_RAM_END, image_start, image_end,
                            decode_base, FES_ENTRY_PAYLOAD_MAX, &plan)) {
        puts("FES_ENTRY REJECT ELF\n");
        fes_entry_park();
    }
    for (uint32_t i = 0; i < plan.count; i++) {
        const struct fes_entry_segment *segment = &plan.segments[i];
        volatile uint8_t *destination = (volatile uint8_t *)(uintptr_t)segment->address;
        const uint8_t *source = payload + segment->file_offset;
        for (uint64_t j = 0; j < segment->file_size; j++) destination[j] = source[j];
        for (uint64_t j = segment->file_size; j < segment->memory_size; j++) destination[j] = 0;
    }
    __asm__ volatile("dsb sy\n\tic iallu\n\tdsb sy\n\tisb" ::: "memory");
    puts("FES_ENTRY EL1 HANDOFF\n");
    fes_entry_enter_el1(plan.entry, (uintptr_t)__stack_top);
}
