/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_FIRMWARE_DECODE_INC
#define FES_NANO_FIRMWARE_DECODE_INC
#ifndef FES_RAM_PARK
#define FES_RAM_PARK .Lfes_ram_park
#endif
/* Selected cold-only decode logging. x26 tracks a one-attempt lifetime;
 * x27 retains status during restoration. x0..x7/x20..x23 are preserved.
 * No SP, calls, RAM stores, status clear, ECC or interrupt-control writes. */
#ifndef FES_DECODE_EDR
#define FES_DECODE_EDR 0x1c000100
#endif
#ifndef FES_DECODE_IRQ
#define FES_DECODE_IRQ 0x1c000104
#endif
#ifndef FES_DECODE_STATUS
#define FES_DECODE_STATUS 0x1c000108
#endif
#ifndef FES_DECODE_RSTMGR
#define FES_DECODE_RSTMGR 0x10d11000
#endif
#ifndef FES_DECODE_PWRMGR
#define FES_DECODE_PWRMGR 0x10d14000
#endif
#ifndef FES_DECODE_MIDR
#define FES_DECODE_MIDR midr_el1
#endif
#ifndef FES_DECODE_MPIDR
#define FES_DECODE_MPIDR mpidr_el1
#endif
#define FES_DECODE_ACTIVE 0xdcd1
#define FES_DECODE_RESTORING 0xdcd2

.macro fes_decode_message label
    mov w17, #FES_RAM_UART_BYTES
    fes_ram_uart_text \label, 1
.endm

.macro fes_decode_fail label
    fes_decode_message \label
    b FES_RAM_PARK
.endm

.macro fes_decode_identity reject
    mrs x8, CurrentEL
    cmp x8, #12
    b.ne \reject
    mrs x8, FES_DECODE_MIDR
    ldr x9, =0x412fd050
    cmp x8, x9
    b.ne \reject
    mrs x8, FES_DECODE_MPIDR
    ldr x9, =0xff00ffffff
    tst x8, x9
    b.ne \reject
    mrs x8, sctlr_el3
    tst x8, #1
    b.ne \reject
    tst x8, #4
    b.ne \reject
    tst x8, #0x03000000
    b.ne \reject
.endm

.macro fes_decode_scope reject
    fes_decode_identity \reject
    mrs x8, daif
    mov x9, #0x2c0
    and x8, x8, x9
    cmp x8, x9
    b.ne \reject
    dsb sy
    ldr x8, =FES_DECODE_RSTMGR
    ldr w9, [x8, #0x24]
    ldr w10, =FES_RAM_MASTER_RESET
    and w9, w9, w10
    cmp w9, w10
    b.ne \reject
    ldr w9, [x8, #0x2c]
    and w9, w9, #15
    cmp w9, #15
    b.ne \reject
    ldr w9, [x8, #0x90]
    and w9, w9, #14
    cmp w9, #14
    b.ne \reject
    ldr w9, [x8, #0x94]
    tst w9, #15
    b.ne \reject
    ldr x8, =FES_DECODE_PWRMGR
    ldr w9, [x8, #0x50]
    and w9, w9, #0x1e
    cmp w9, #2
    b.ne \reject
    ldr w9, [x8, #0x54]
    and w9, w9, #0x1e
    cmp w9, #2
    b.ne \reject
    ldr w9, [x8, #0x58]
    ubfx w9, w9, #10, #2
    cbnz w9, \reject
    ldr w9, [x8, #0x24]
    cbnz w9, \reject
    ldr w9, [x8, #0x2c]
    cbnz w9, \reject
    ldr w9, [x8, #0x34]
    cbnz w9, \reject
    dsb sy
.endm

.macro fes_decode_init
    mov x26, #0
    fes_decode_identity .Lfes_decode_init_reject_\@
    fes_decode_message fes_decode_idle
    b .Lfes_decode_init_done_\@
.Lfes_decode_init_reject_\@:
    fes_decode_fail fes_decode_fail_identity
.Lfes_decode_init_done_\@:
.endm

.macro fes_decode_setup
    cbnz x26, .Lfes_decode_setup_reject_\@
    fes_decode_message fes_decode_setup_before
    fes_decode_scope .Lfes_decode_setup_reject_\@
    ldr x8, =FES_DECODE_EDR
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_setup_reject_\@
    ldr x8, =FES_DECODE_IRQ
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_setup_reject_\@
    ldr x8, =FES_DECODE_STATUS
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_setup_reject_\@
    fes_decode_message fes_decode_baseline_zero
    fes_decode_message fes_decode_enable_before
    /* Mark intent before the potentially faulting store. */
    mov x26, #FES_DECODE_ACTIVE
    ldr x8, =FES_DECODE_EDR
    mov w9, #8
    str w9, [x8]
    dsb sy
    ldr w9, [x8]
    cmp w9, #8
    b.ne .Lfes_decode_enable_reject_\@
    ldr x8, =FES_DECODE_IRQ
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_enable_reject_\@
    ldr x8, =FES_DECODE_STATUS
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_enable_reject_\@
    fes_decode_message fes_decode_enabled
    b .Lfes_decode_setup_done_\@
.Lfes_decode_setup_reject_\@:
    fes_decode_fail fes_decode_fail_admit
.Lfes_decode_enable_reject_\@:
    fes_decode_fail fes_decode_fail_enable
.Lfes_decode_setup_done_\@:
.endm

.macro fes_decode_restore fatal=0
    cbz x26, .Lfes_decode_inactive_\@
    mov x8, #FES_DECODE_RESTORING
    cmp x26, x8
    b.eq .Lfes_decode_interrupted_\@
    mov x8, #FES_DECODE_ACTIVE
    cmp x26, x8
    b.ne .Lfes_decode_restore_reject_\@
    /* An exception after this point must never retry restoration. */
    mov x26, #FES_DECODE_RESTORING
    fes_decode_message fes_decode_restore_before
    fes_decode_scope .Lfes_decode_restore_reject_\@
    ldr x8, =FES_DECODE_EDR
    ldr w9, [x8]
    cmp w9, #8
    b.ne .Lfes_decode_restore_reject_\@
    ldr x8, =FES_DECODE_IRQ
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_restore_reject_\@
    ldr x8, =FES_DECODE_STATUS
    ldr w27, [x8]
    ldr x8, =FES_DECODE_EDR
    str wzr, [x8]
    dsb sy
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_restore_reject_\@
    ldr x8, =FES_DECODE_IRQ
    ldr w9, [x8]
    cbnz w9, .Lfes_decode_restore_reject_\@
    ldr x8, =FES_DECODE_STATUS
    ldr w9, [x8]
    cmp w9, w27
    b.ne .Lfes_decode_restore_reject_\@
    mov x26, #0
    fes_decode_message fes_decode_restored
    b .Lfes_decode_restore_done_\@
.Lfes_decode_inactive_\@:
    .if \fatal
    fes_decode_message fes_decode_inactive
    b .Lfes_decode_restore_done_\@
    .else
    b .Lfes_decode_restore_reject_\@
    .endif
.Lfes_decode_interrupted_\@:
    .if \fatal
    fes_decode_message fes_decode_interrupted
    b .Lfes_decode_restore_done_\@
    .else
    b .Lfes_decode_restore_reject_\@
    .endif
.Lfes_decode_restore_reject_\@:
    fes_decode_fail fes_decode_fail_restore
.Lfes_decode_restore_done_\@:
.endm

.pushsection .rodata.fes_decode,"a"
fes_decode_idle: .asciz "FES_NANO_RAM DECODE phase=idle\r\n"
fes_decode_setup_before: .asciz "FES_NANO_RAM DECODE phase=setup-before\r\n"
fes_decode_baseline_zero: .asciz "FES_NANO_RAM DECODE phase=baseline-zero\r\n"
fes_decode_enable_before: .asciz "FES_NANO_RAM DECODE phase=enable-before\r\n"
fes_decode_enabled: .asciz "FES_NANO_RAM DECODE phase=enabled\r\n"
fes_decode_restore_before: .asciz "FES_NANO_RAM DECODE phase=restore-before\r\n"
fes_decode_restored: .asciz "FES_NANO_RAM DECODE phase=restored\r\n"
fes_decode_inactive: .asciz "FES_NANO_RAM DECODE phase=inactive\r\n"
fes_decode_interrupted: .asciz "FES_NANO_RAM DECODE phase=restore-interrupted\r\n"
fes_decode_fail_identity: .asciz "FES_NANO_RAM DECODE FAIL phase=identity\r\n"
fes_decode_fail_admit: .asciz "FES_NANO_RAM DECODE FAIL phase=admit\r\n"
fes_decode_fail_enable: .asciz "FES_NANO_RAM DECODE FAIL phase=enable\r\n"
fes_decode_fail_restore: .asciz "FES_NANO_RAM DECODE FAIL phase=restore\r\n"
.popsection

#endif /* FES_NANO_FIRMWARE_DECODE_INC */
