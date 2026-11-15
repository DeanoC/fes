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
    /* Decoder faults must park here, never return through U-Boot's vectors. */
    ldr x2, =fes_entry_vectors
    msr vbar_el2, x2
    isb
    mrs x0, sctlr_el2
    ldr x1, =0x1005               /* I-cache, D-cache and MMU must be off */
    tst x0, x1
    b.ne .Lbad_state
    mrs x0, mpidr_el1
    ldr x1, =0xff00ffffff
    and x0, x0, x1
#ifndef FES_ENTRY_QEMU_DIAGNOSTIC
    cmp x0, #0x200               /* Agilex 5 can cold-boot on primary A76 */
    b.eq 1f
#endif
    cmp x0, #0                   /* QEMU or Agilex 5 primary A55 */
    b.ne .Lbad_cpu
1:
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

/* Fixed EL2 regime: little endian, 4K pages, >=40-bit PA, no VHE/stage2. */
.global fes_entry_cache_supported
fes_entry_cache_supported:
    mrs x0, id_aa64mmfr0_el1
    ubfx x1, x0, #28, #4
    cbnz x1, .Lcache_unsupported
    and x0, x0, #15
    cmp x0, #2
    b.lo .Lcache_unsupported
    cmp x0, #6
    b.hi .Lcache_unsupported
    mrs x0, hcr_el2
    ldr x1, =0x400000001
    tst x0, x1
    b.ne .Lcache_unsupported
    mrs x0, sctlr_el2
    ldr x1, =0x2081005             /* EE/WXN/M/C/I must be clear */
    tst x0, x1
    b.ne .Lcache_unsupported
    mrs x0, ctr_el0
    ubfx x0, x0, #16, #4
    cmp x0, #2
    b.lo .Lcache_unsupported
    cmp x0, #8
    b.hi .Lcache_unsupported
    mov x0, #1
    ret
.Lcache_unsupported:
    mov x0, #0
    ret

/* x0 root, x1 saved state, x2..x5 owned RAM/image bounds. No stack writes.
 * Firmware's clean-cache ownership obligation is unchanged. Discard stale
 * clean lines only for these owned ranges before enabling the private map. */
.global fes_entry_cache_enable
fes_entry_cache_enable:
    mrs x9, sctlr_el2
    str x9, [x1, #0]
    mrs x9, tcr_el2
    str x9, [x1, #8]
    mrs x9, ttbr0_el2
    str x9, [x1, #16]
    mrs x9, mair_el2
    str x9, [x1, #24]
    stp x2, x3, [x1, #32]
    stp x4, x5, [x1, #48]
    /* Warm the trusted instruction stream before the bounded line walk. */
    dsb sy
    ic iallu
    dsb sy
    isb
    ldr x9, [x1, #0]
    orr x9, x9, #0x1000
    msr sctlr_el2, x9
    isb
    mrs x6, ctr_el0
    ubfx x6, x6, #16, #4
    mov x7, #4
    lsl x6, x7, x6
    dsb sy
    mov x7, x2
1:  dc ivac, x7
    add x7, x7, x6
    cmp x7, x3
    b.lo 1b
    mov x7, x4
2:  dc ivac, x7
    add x7, x7, x6
    cmp x7, x5
    b.lo 2b
    dsb sy
    ldr x9, =0xff00
    msr mair_el2, x9
    ldr x9, =0x80823519
    msr tcr_el2, x9
    msr ttbr0_el2, x0
    isb
    tlbi alle2
    dsb sy
    isb
    ic iallu
    dsb sy
    isb
    ldr x9, [x1, #0]
    orr x9, x9, #1
    orr x9, x9, #4
    orr x9, x9, #0x1000
    msr sctlr_el2, x9
    isb
    mov x0, #1
    ret

/* x0 saved state. Everything needed is loaded before cache maintenance.
 * No stack or memory stores follow the clean/invalidate loops. */
.global fes_entry_cache_disable
fes_entry_cache_disable:
    ldp x9, x10, [x0, #0]
    ldp x11, x12, [x0, #16]
    ldp x2, x3, [x0, #32]
    ldp x4, x5, [x0, #48]
    mrs x6, ctr_el0
    ubfx x6, x6, #16, #4
    mov x7, #4
    lsl x6, x7, x6
    dsb sy
    mov x7, x2
1:  dc civac, x7
    add x7, x7, x6
    cmp x7, x3
    b.lo 1b
    mov x7, x4
2:  dc civac, x7
    add x7, x7, x6
    cmp x7, x5
    b.lo 2b
    dsb sy
    msr sctlr_el2, x9
    isb
    tlbi alle2
    dsb sy
    isb
    ic iallu
    dsb sy
    isb
    msr tcr_el2, x10
    msr ttbr0_el2, x11
    msr mair_el2, x12
    isb
    mrs x0, sctlr_el2
    cmp x0, x9
    b.ne .Lcache_restore_bad
    mrs x0, tcr_el2
    cmp x0, x10
    b.ne .Lcache_restore_bad
    mrs x0, ttbr0_el2
    cmp x0, x11
    b.ne .Lcache_restore_bad
    mrs x0, mair_el2
    cmp x0, x12
    b.ne .Lcache_restore_bad
    mov x0, #1
    ret
.Lcache_restore_bad:
    mov x0, #0
    ret

/* x0: validated native entry; x1: aligned temporary EL1 stack. */
.global fes_entry_enter_el1
fes_entry_enter_el1:
    mrs x2, sctlr_el2
    ldr x3, =0x1005               /* preserve the clean handoff requirement */
    tst x2, x3
    b.ne .Lbad_state
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
