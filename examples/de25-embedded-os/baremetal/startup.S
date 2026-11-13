/* SPDX-License-Identifier: GPL-3.0-or-later */
/* QEMU virt entry only: firmware, DDR and MMU state for DE25 are unspecified. */
.section .text.boot, "ax"
.global _start
.type _start, %function
_start:
    msr daifset, #0xf
    mrs x0, mpidr_el1
    and x0, x0, #0xffffff
    cbnz x0, .Lpark
    msr spsel, #1
    ldr x0, =__stack_top
    mov sp, x0
    ldr x0, =__bss_start
    ldr x1, =__bss_end
.Lclear:
    cmp x0, x1
    b.hs .Lmain
    str xzr, [x0], #8
    b .Lclear
.Lmain:
    bl baremetal_main
.Lpark:
    wfe
    b .Lpark
.size _start, . - _start
