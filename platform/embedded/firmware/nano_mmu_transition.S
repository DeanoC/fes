/* SPDX-License-Identifier: BSD-3-Clause */
/* Selected cold-only diagnostic. No stack, C calls, cache-off fallback or
 * owned device access. Fixed read-only SoC error status and current RAS bank
 * are diagnostic only. Fixtures substitute CPU/UART/read targets only. */
.arch_extension ras
#ifndef FES_MMU_MIDR
#define FES_MMU_MIDR 0x412fd050
#endif
#ifndef FES_MMU_MPIDR
#define FES_MMU_MPIDR 0x81000000
#endif
#ifndef FES_MMU_UART_DATA
#define FES_MMU_UART_DATA 0x10c02100
#endif
#ifndef FES_MMU_UART_STATUS
#define FES_MMU_UART_STATUS 0x10c02114
#endif

.macro mmu_constant reg,value
    movz \reg, #(\value & 0xffff)
    movk \reg, #((\value >> 16) & 0xffff), lsl #16
.endm
.macro mmu_cpu reject
    mrs x2,CurrentEL
    cmp x2,#12
    b.ne \reject
    mrs x2,midr_el1
    mmu_constant x3,FES_MMU_MIDR
    cmp x2,x3
    b.ne \reject
    mrs x2,mpidr_el1
    mmu_constant x3,FES_MMU_MPIDR
    cmp x2,x3
    b.ne \reject
    mrs x2,daif
    mmu_constant x3,0x2c0
    and x2,x2,x3
    cmp x2,#0x2c0
    b.ne \reject
    mrs x2,scr_el3
    tbnz x2,#0,\reject
    mrs x2,sctlr_el3
    tbnz x2,#25,\reject
.endm
.macro mmu_registers reject
    mrs x2,mair_el3
    mmu_constant x3,0x4400ff
    cmp x2,x3
    b.ne \reject
    mrs x2,tcr_el3
    mmu_constant x3,0x80803520
    cmp x2,x3
    b.ne \reject
    mrs x2,ttbr0_el3
    adr x3,fes_nano_mmu_root
    cmp x2,x3
    b.ne \reject
.endm
/* Preserve x0..x7,x18..x30. Every line has one shared 384-byte budget;
 * every character has at most 100000 ready-status polls. */
.macro mmu_char
    cbz w17,fes_nano_mmu_transition_park
    sub w17,w17,#1
    mmu_constant x10,FES_MMU_UART_STATUS
    mmu_constant w11,100000
.Lpoll\@:
    ldr w12,[x10]
    cmn w12,#1
    b.eq fes_nano_mmu_transition_park
    tst w12,#0x20
    b.ne .Lready\@
    subs w11,w11,#1
    b.ne .Lpoll\@
    b fes_nano_mmu_transition_park
.Lready\@:
    mmu_constant x10,FES_MMU_UART_DATA
    str w9,[x10]
    dsb sy
.endm
.macro mmu_text label
    adr x8,\label
.Ltext\@:
    cbz w17,fes_nano_mmu_transition_park
    ldrb w9,[x8],#1
    cbz w9,.Lend\@
    mmu_char
    b .Ltext\@
.Lend\@:
.endm
.macro mmu_hex value
    mov x14,\value
    mov w16,#16
.Lhex\@:
    lsr x9,x14,#60
    cmp w9,#9
    add w9,w9,#48
    b.ls .Ldigit\@
    add w9,w9,#39
.Ldigit\@:
    mmu_char
    lsl x14,x14,#4
    subs w16,w16,#1
    b.ne .Lhex\@
.endm
.macro mmu_field label,value
    mmu_text \label
    mmu_hex \value
.endm

#include "nano_mmu_status.inc"

.section .text.fes_nano_mmu_transition,"ax"
.balign 4
.global fes_nano_mmu_transition_write
.type fes_nano_mmu_transition_write,%function
fes_nano_mmu_transition_write:
    mmu_cpu .Lreject
    mmu_registers .Lreject
    mrs x2,sctlr_el3
    mmu_constant x3,0x3081005
    and x3,x2,x3
    cmp x3,#0x1000
    b.ne .Lreject
    orr x2,x2,#1
    orr x2,x2,#4
    orr x2,x2,#0x80000
    cmp x0,x2
    b.ne .Lreject
    mrs x1,vbar_el3
    adr x2,runtime_exceptions
    cmp x1,x2
    b.ne .Lreject
    adr x2,fes_nano_mmu_transition_saved_vbar
    ldr x3,[x2]
    cbnz x3,.Lreject
    str x1,[x2]
    dsb sy
    adr x1,fes_nano_mmu_transition_vectors
    msr vbar_el3,x1
    isb
    mov x7,x0
    mmu_inventory before
    mov x0,x7
    mmu_cpu .Lreject
    mmu_registers .Lreject
.global fes_nano_mmu_transition_sctlr
.type fes_nano_mmu_transition_sctlr,%function
fes_nano_mmu_transition_sctlr:
    msr sctlr_el3,x0
    isb
.size fes_nano_mmu_transition_sctlr,.-fes_nano_mmu_transition_sctlr
    mrs x0,sctlr_el3
    mrs x1,vbar_el3
    mov w17,#384
    mmu_text .Lenabled
    mmu_hex x0
    mmu_field .Lvbar,x1
    mmu_text .Lnewline
    mov w0,#0
    ret
.Lreject:
    mov w0,#-1
    ret
.size fes_nano_mmu_transition_write,.-fes_nano_mmu_transition_write

.global fes_nano_mmu_transition_restore
.type fes_nano_mmu_transition_restore,%function
fes_nano_mmu_transition_restore:
    mmu_cpu .Lrestore_reject
    mmu_registers .Lrestore_reject
    mrs x2,sctlr_el3
    mmu_constant x3,0x3081005
    and x2,x2,x3
    mmu_constant x3,0x81005
    cmp x2,x3
    b.ne .Lrestore_reject
    mrs x1,vbar_el3
    adr x2,fes_nano_mmu_transition_vectors
    cmp x1,x2
    b.ne .Lrestore_reject
    adr x2,fes_nano_mmu_transition_saved_vbar
    ldr x1,[x2]
    adr x2,runtime_exceptions
    cmp x1,x2
    b.ne .Lrestore_reject
.global fes_nano_mmu_transition_restore_vbar
.type fes_nano_mmu_transition_restore_vbar,%function
fes_nano_mmu_transition_restore_vbar:
    msr vbar_el3,x1
    isb
    mrs x3,vbar_el3
.size fes_nano_mmu_transition_restore_vbar,.-fes_nano_mmu_transition_restore_vbar
    cmp x3,x1
    b.ne fes_nano_mmu_transition_park
    mrs x0,sctlr_el3
    mov w17,#384
    mmu_text .Lrestored
    mmu_hex x0
    mmu_field .Lvbar,x1
    mmu_text .Lnewline
    mov w0,#0
    ret
.Lrestore_reject:
    mov w0,#-1
    ret
.size fes_nano_mmu_transition_restore,.-fes_nano_mmu_transition_restore

.balign 2048
.global fes_nano_mmu_transition_vectors
.type fes_nano_mmu_transition_vectors,%object
fes_nano_mmu_transition_vectors:
.rept 16
    b fes_nano_mmu_transition_capture
    .space 124
.endr
.size fes_nano_mmu_transition_vectors,.-fes_nano_mmu_transition_vectors
.balign 2048
.global fes_nano_mmu_transition_park_vectors
.type fes_nano_mmu_transition_park_vectors,%object
fes_nano_mmu_transition_park_vectors:
.rept 16
    b fes_nano_mmu_transition_park
    .space 124
.endr
.size fes_nano_mmu_transition_park_vectors,.-fes_nano_mmu_transition_park_vectors

.global fes_nano_mmu_transition_capture
.type fes_nano_mmu_transition_capture,%function
fes_nano_mmu_transition_capture:
    mrs x19,esr_el3
    mrs x20,elr_el3
    mrs x21,far_el3
    mrs x22,spsr_el3
    mov x23,sp
    mrs x24,sctlr_el3
    mrs x25,tcr_el3
    mrs x26,ttbr0_el3
    mrs x27,mair_el3
    mrs x28,scr_el3
    mrs x29,vbar_el3
    mrs x30,sp_el0
    /* Consume before any diagnostic read/output. A nested exception parks. */
    adr x8,fes_nano_mmu_transition_park_vectors
    msr vbar_el3,x8
    isb
    mmu_cpu fes_nano_mmu_transition_park
    mov w17,#384
    mmu_field .Lfault,x19
    mmu_field .Lelr,x20
    mmu_field .Lfar,x21
    mmu_field .Lspsr,x22
    mmu_field .Lsp,x23
    mmu_field .Lsctlr,x24
    mmu_field .Ltcr,x25
    mmu_field .Lroot,x26
    mmu_field .Lmair,x27
    mmu_field .Lscr,x28
    mmu_field .Lvbar,x29
    mmu_field .Lsp0,x30
    mmu_text .Lnewline
    mmu_inventory fault
    b fes_nano_mmu_transition_park
.size fes_nano_mmu_transition_capture,.-fes_nano_mmu_transition_capture

.global fes_nano_mmu_transition_park
.type fes_nano_mmu_transition_park,%function
fes_nano_mmu_transition_park:
    wfe
    b fes_nano_mmu_transition_park
.size fes_nano_mmu_transition_park,.-fes_nano_mmu_transition_park

.section .rodata.fes_nano_mmu_transition,"a"
.Lenabled: .asciz "FES_NANO_EL3_MMU_TRANSITION phase=enabled sctlr="
.Lrestored: .asciz "FES_NANO_EL3_MMU_TRANSITION phase=restored sctlr="
.Lfault: .asciz "\r\nFES_NANO_EL3_MMU_FAULT esr="
.Lelr: .asciz " elr="
.Lfar: .asciz " far="
.Lspsr: .asciz " spsr="
.Lsp: .asciz " sp="
.Lsp0: .asciz " sp0="
.Lsctlr: .asciz " sctlr="
.Ltcr: .asciz " tcr="
.Lroot: .asciz " root="
.Lmair: .asciz " mair="
.Lscr: .asciz " scr="
.Lvbar: .asciz " vbar="
.Lnewline: .asciz "\r\n"
.section .data.fes_nano_mmu_transition,"aw"
.balign 8
.global fes_nano_mmu_transition_saved_vbar
.type fes_nano_mmu_transition_saved_vbar,%object
fes_nano_mmu_transition_saved_vbar:
    .quad 0 /* Remains consumed after successful restoration. */
.size fes_nano_mmu_transition_saved_vbar,.-fes_nano_mmu_transition_saved_vbar
.section .note.GNU-stack,"",%progbits
