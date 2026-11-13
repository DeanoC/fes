/* SPDX-License-Identifier: BSD-3-Clause */
/* Cold C-call leaf. AAPCS caller-saved x8..x14/NZCV only; no SP, RAM stores,
 * nested calls, console framework, cache operations or exception-vector writes.
 * The selected cold entry has already established stack and mappings. */
.pushsection .text.fes_ram_runtime,"ax"
.balign 4
.global fes_nano_ram_runtime_trace
.type fes_nano_ram_runtime_trace, %function
fes_nano_ram_runtime_trace:
    cmp x0, #FES_RAM_RT_COUNT
    b.hs FES_RAM_PARK
    ldr x8, =fes_nano_ram_runtime_table
    ldr x8, [x8, x0, lsl #3]
    mov w13, #FES_RAM_UART_BYTES
.Lfes_ram_runtime_text:
    cbz w13, FES_RAM_PARK
    ldrb w9, [x8], #1
    sub w13, w13, #1
    cbz w9, .Lfes_ram_runtime_end
    fes_ram_uart_char
    b .Lfes_ram_runtime_text
.Lfes_ram_runtime_end:
    fes_ram_uart_wait 0x40
    ret
.size fes_nano_ram_runtime_trace, .-fes_nano_ram_runtime_trace
.popsection
