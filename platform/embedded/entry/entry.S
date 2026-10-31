/* SPDX-License-Identifier: GPL-3.0-or-later */
/* U-Boot booti-compatible header. This image contains no Linux kernel. */
.section .text.header,"ax"
.global _start
_start:
    b fes_entry_start
    .long 0
    .quad 0x80000                 /* 2 MiB-aligned base + this offset */
    .quad __image_size
    .quad 8                       /* base may be anywhere in physical RAM */
    .quad 0
    .quad 0
    .quad 0
    .long 0x644d5241              /* arm64 Image header magic */
    .long 0

.section .text.start,"ax"
.global fes_entry_start
fes_entry_start:
    msr daifset, #0xf
    ldr x0, =__stack_top
    mov sp, x0
    mrs x0, CurrentEL
    cmp x0, #8                    /* EL2: the tested vendor U-Boot path */
    b.ne .Lbad_el
    mrs x0, sctlr_el2
    ldr x1, =0x1005               /* I-cache, D-cache and MMU must be off */
    tst x0, x1
    b.ne .Lbad_state
    mrs x0, mpidr_el1
    ldr x1, =0xff00ffffff         /* affinity 3:0; do not accept secondaries */
    tst x0, x1
    b.ne .Lbad_cpu
    bl fes_entry_main
    b fes_entry_park
.Lbad_el:
#ifdef FES_ENTRY_QEMU_DIAGNOSTIC
    bl fes_entry_bad_el
#endif
    b fes_entry_park
.Lbad_state:
#ifdef FES_ENTRY_QEMU_DIAGNOSTIC
    bl fes_entry_bad_state
#endif
    b fes_entry_park
.Lbad_cpu:
#ifdef FES_ENTRY_QEMU_DIAGNOSTIC
    bl fes_entry_bad_cpu
#endif
    b fes_entry_park

.global fes_entry_park
fes_entry_park:
    wfe
    b fes_entry_park

/* x0: validated native entry; x1: aligned temporary EL1 stack. */
.global fes_entry_enter_el1
fes_entry_enter_el1:
    mrs x2, cnthctl_el2
    orr x2, x2, #3                /* EL1 physical timer/counter access */
    msr cnthctl_el2, x2
    msr cntvoff_el2, xzr
    mrs x2, midr_el1
    msr vpidr_el2, x2
    mrs x2, mpidr_el1
    msr vmpidr_el2, x2
    ldr x2, =0x33ff               /* CPTR_EL2 RES1; disable coprocessor traps */
    msr cptr_el2, x2
    msr hstr_el2, xzr
    ldr x2, =0x300000             /* enable EL1 FP/SIMD */
    msr cpacr_el1, x2
    ldr x2, =0x30d00800           /* SCTLR_EL1 RES1; MMU and caches off */
    msr sctlr_el1, x2
    msr sp_el1, x1
    ldr x2, =fes_entry_vectors
    msr vbar_el1, x2
    mrs x2, id_aa64isar1_el1
    ldr x3, =0xff000ff0           /* same pointer-auth feature fields as U-Boot */
    tst x2, x3
    ldr x2, =0xa0000000           /* AArch64 EL1; disable HVC */
    b.eq 1f
    ldr x3, =0x30000000000        /* do not trap pointer authentication */
    orr x2, x2, x3
1:  msr hcr_el2, x2
    mov x2, #0x3c5                /* EL1h, all exceptions masked */
    msr spsr_el2, x2
    msr elr_el2, x0
    dsb sy
    isb
    mov x0, xzr
    mov x1, xzr
    mov x2, xzr
    mov x3, xzr
    eret

.section .text.vectors,"ax"
.balign 2048
.global fes_entry_vectors
fes_entry_vectors:
.rept 16
    b fes_entry_exception
    .space 124
.endr

.section .text.start,"ax"
fes_entry_exception:
    bl fes_entry_exception_c
    b fes_entry_park
