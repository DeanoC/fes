/* SPDX-License-Identifier: BSD-3-Clause */
/* Private selected cold-entry vectors. No SP use, calls, RAM writes or return.
 * Switch to park-only vectors before any diagnostic load/store, so a fault in
 * UART logging cannot recurse into the logger. Warm/runtime vectors are not
 * replaced. FES_RAM_FAULT_PARK is injectable only for off-board fixtures. */
#ifndef FES_RAM_FAULT_PARK
#define FES_RAM_FAULT_PARK .Lfes_ram_fault_park
#endif

.macro fes_ram_uart_hex source
    mov x14, \source
    mov w16, #16
.Lfes_ram_hex_\@:
    lsr x9, x14, #60
    cmp w9, #10
    b.lo .Lfes_ram_digit_\@
    add w9, w9, #87
    b .Lfes_ram_send_\@
.Lfes_ram_digit_\@:
    add w9, w9, #48
.Lfes_ram_send_\@:
    fes_ram_uart_char 1
    lsl x14, x14, #4
    subs w16, w16, #1
    b.ne .Lfes_ram_hex_\@
    fes_ram_uart_wait 0x40
.endm

.pushsection .vectors,"ax"
.balign 2048
.global fes_nano_ram_fault_vectors
.type fes_nano_ram_fault_vectors, %object
fes_nano_ram_fault_vectors:
.irp slot,0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15
    msr daifset, #15
    mov x4, #\slot
    b fes_nano_ram_fault
    .space 116
.endr
.size fes_nano_ram_fault_vectors, .-fes_nano_ram_fault_vectors

.balign 2048
.global fes_nano_ram_fault_park_vectors
.type fes_nano_ram_fault_park_vectors, %object
fes_nano_ram_fault_park_vectors:
.rept 16
    b FES_RAM_FAULT_PARK
    .space 124
.endr
.size fes_nano_ram_fault_park_vectors, .-fes_nano_ram_fault_park_vectors

.global fes_nano_ram_fault
.type fes_nano_ram_fault, %function
fes_nano_ram_fault:
    adr x8, fes_nano_ram_fault_park_vectors
    msr vbar_el3, x8
    isb
    /* All potentially faulting diagnostics follow the fail-closed VBAR. */
    mov x20, x0
    mov x21, x1
    mov x22, x2
    mov x23, x3
    mrs x0, esr_el3
    mrs x1, elr_el3
    mrs x2, far_el3
    mrs x3, spsr_el3
    mov w17, #FES_RAM_UART_BYTES
    fes_ram_uart_text fes_nano_ram_fault_prefix, 1
    fes_ram_uart_hex x4
    fes_ram_uart_text fes_nano_ram_fault_esr, 1
    fes_ram_uart_hex x0
    fes_ram_uart_text fes_nano_ram_fault_elr, 1
    fes_ram_uart_hex x1
    fes_ram_uart_text fes_nano_ram_fault_far, 1
    fes_ram_uart_hex x2
    fes_ram_uart_text fes_nano_ram_fault_spsr, 1
    fes_ram_uart_hex x3
    fes_ram_uart_text fes_nano_ram_fault_r0, 1
    fes_ram_uart_hex x20
    fes_ram_uart_text fes_nano_ram_fault_r1, 1
    fes_ram_uart_hex x21
    fes_ram_uart_text fes_nano_ram_fault_r2, 1
    fes_ram_uart_hex x22
    fes_ram_uart_text fes_nano_ram_fault_r3, 1
    fes_ram_uart_hex x23
    fes_ram_uart_text fes_nano_ram_fault_newline, 1
    b FES_RAM_FAULT_PARK
.Lfes_ram_fault_park:
    wfe
    b .Lfes_ram_fault_park
.size fes_nano_ram_fault, .-fes_nano_ram_fault
.popsection

.pushsection .rodata.fes_ram_fault,"a"
fes_nano_ram_fault_prefix: .asciz "FES_NANO_RAM EXCEPTION vector="
fes_nano_ram_fault_esr: .asciz " esr="
fes_nano_ram_fault_elr: .asciz " elr="
fes_nano_ram_fault_far: .asciz " far="
fes_nano_ram_fault_spsr: .asciz " spsr="
fes_nano_ram_fault_r0: .asciz " r0="
fes_nano_ram_fault_r1: .asciz " r1="
fes_nano_ram_fault_r2: .asciz " r2="
fes_nano_ram_fault_r3: .asciz " r3="
fes_nano_ram_fault_newline: .asciz "\r\n"
.popsection
