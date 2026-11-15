/* SPDX-License-Identifier: BSD-3-Clause */
/* Selected late-MMU diagnostic. Only DSU decode detection is written, 0/8.
 * All masters remain reset. No stack, calls, acknowledgements or IRQ writes.
 * State is private file-backed DDR because C runs between arm and restore. */
#ifndef FES_MMU_LOG_CONTROL
#define FES_MMU_LOG_CONTROL 0x1c000100
#define FES_MMU_LOG_IRQ 0x1c000104
#define FES_MMU_LOG_STATUS 0x1c000108
#define FES_MMU_LOG_RESET 0x10d11000
#define FES_MMU_LOG_POWER 0x10d14000
#endif

.macro mmu_log_message label
    mov w17,#384
    mmu_text \label
.endm
.macro mmu_log_fail label
    mmu_log_message \label
    b fes_nano_mmu_transition_park
.endm
.macro mmu_log_state name,value
.global fes_nano_mmu_log_state_\name
.type fes_nano_mmu_log_state_\name,%function
fes_nano_mmu_log_state_\name:
    adr x5,fes_nano_mmu_logging_state
    mov w1,#\value
    str w1,[x5]
.size fes_nano_mmu_log_state_\name,.-fes_nano_mmu_log_state_\name
    dsb sy
.endm
.macro mmu_log_store name,value
.global fes_nano_mmu_log_store_\name
.type fes_nano_mmu_log_store_\name,%function
fes_nano_mmu_log_store_\name:
    mmu_constant x0,FES_MMU_LOG_CONTROL
    mov w1,#\value
    str w1,[x0]
.size fes_nano_mmu_log_store_\name,.-fes_nano_mmu_log_store_\name
    dsb sy
.endm
.macro mmu_log_scope
    mmu_cpu fes_nano_mmu_transition_park
    mmu_registers fes_nano_mmu_transition_park
    mrs x2,sctlr_el3
    mmu_constant x3,0x3081005
    and x2,x2,x3
    cmp x2,#0x1000
    b.eq .Llog_cpu_\@
    mmu_constant x3,0x81005
    cmp x2,x3
    b.ne fes_nano_mmu_transition_park
.Llog_cpu_\@:
    dsb sy
    mmu_constant x0,FES_MMU_LOG_RESET
    ldr w2,[x0,#0x24]
    mmu_constant w3,0xff21bfbf
    and w2,w2,w3
    cmp w2,w3
    b.ne fes_nano_mmu_transition_park
    ldr w2,[x0,#0x2c]
    and w2,w2,#15
    cmp w2,#15
    b.ne fes_nano_mmu_transition_park
    ldr w2,[x0,#0x90]
    and w2,w2,#14
    cmp w2,#14
    b.ne fes_nano_mmu_transition_park
    ldr w2,[x0,#0x94]
    tst w2,#15
    b.ne fes_nano_mmu_transition_park
    mmu_constant x0,FES_MMU_LOG_POWER
    ldr w2,[x0,#0x50]
    and w2,w2,#0x1e
    cmp w2,#2
    b.ne fes_nano_mmu_transition_park
    ldr w2,[x0,#0x54]
    and w2,w2,#0x1e
    cmp w2,#2
    b.ne fes_nano_mmu_transition_park
    ldr w2,[x0,#0x58]
    ubfx w2,w2,#10,#2
    cbnz w2,fes_nano_mmu_transition_park
    ldr w2,[x0,#0x24]
    cbnz w2,fes_nano_mmu_transition_park
    ldr w2,[x0,#0x2c]
    cbnz w2,fes_nano_mmu_transition_park
    ldr w2,[x0,#0x34]
    cbnz w2,fes_nano_mmu_transition_park
    dsb sy
.endm

/* fatal=1 permits cleanup of a potentially interrupted enable, never return
 * to the boot path. A nested exception cannot retry a restoring transaction. */
.macro mmu_log_close name,fatal=0
    adr x5,fes_nano_mmu_logging_state
    ldr w1,[x5]
    .if \fatal
    cbz w1,.Llog_inactive_\@
    cmp w1,#3
    b.eq .Llog_interrupted_\@
    cmp w1,#4
    b.eq .Llog_retired_\@
    cmp w1,#1
    b.eq .Llog_close_\@
    .endif
    cmp w1,#2
    b.ne .Llog_restore_fail_\@
.Llog_close_\@:
    mov w6,w1
    mmu_log_state \name\()_restoring,3
    mmu_log_message .Llog_restore_before
    mmu_log_scope
    mmu_constant x0,FES_MMU_LOG_CONTROL
    ldr w1,[x0]
    cbnz w1,.Llog_control_check_\@
    cmp w6,#1
    b.ne .Llog_restore_fail_\@
    b .Llog_control_\@
.Llog_control_check_\@:
    cmp w1,#8
    b.ne .Llog_restore_fail_\@
.Llog_control_\@:
    mmu_constant x0,FES_MMU_LOG_IRQ
    ldr w1,[x0]
    cbnz w1,.Llog_restore_fail_\@
    mmu_constant x0,FES_MMU_LOG_STATUS
    ldr w4,[x0]
    mmu_log_scope
    mmu_log_store \name,0
    mmu_constant x0,FES_MMU_LOG_CONTROL
    ldr w1,[x0]
    cbnz w1,.Llog_restore_fail_\@
    mmu_constant x0,FES_MMU_LOG_IRQ
    ldr w1,[x0]
    cbnz w1,.Llog_restore_fail_\@
    mmu_constant x0,FES_MMU_LOG_STATUS
    ldr w1,[x0]
    cmp w1,w4
    b.ne .Llog_restore_fail_\@
    mmu_log_scope
    mmu_log_state \name\()_retired,4
    mmu_log_message .Llog_restored
    .if !\fatal
    cbnz w4,.Llog_status_fail_\@
    .endif
    b .Llog_done_\@
.Llog_restore_fail_\@:
    mmu_log_fail .Llog_fail_restore
    .if !\fatal
.Llog_status_fail_\@:
    mmu_log_fail .Llog_fail_status
    .endif
    .if \fatal
.Llog_inactive_\@:
    mmu_log_message .Llog_inactive
    b .Llog_done_\@
.Llog_interrupted_\@:
    mmu_log_message .Llog_interrupted
    b .Llog_done_\@
.Llog_retired_\@:
    mmu_log_message .Llog_retired
    .endif
.Llog_done_\@:
.endm

.macro mmu_log_arm
    adr x5,fes_nano_mmu_logging_state
    ldr w1,[x5]
    cbnz w1,.Llog_admit_fail_\@
    mmu_log_scope
    mmu_constant x0,FES_MMU_LOG_CONTROL
    ldr w1,[x0]
    cbnz w1,.Llog_admit_fail_\@
    mmu_constant x0,FES_MMU_LOG_IRQ
    ldr w1,[x0]
    cbnz w1,.Llog_admit_fail_\@
    mmu_constant x0,FES_MMU_LOG_STATUS
    ldr w1,[x0]
    cbnz w1,.Llog_admit_fail_\@
    mmu_log_scope
    mmu_log_message .Llog_arm_before
    mmu_log_state arm_before,1
    mmu_log_store arm,8
    mmu_constant x0,FES_MMU_LOG_CONTROL
    ldr w1,[x0]
    cmp w1,#8
    b.ne .Llog_enable_fail_\@
    mmu_constant x0,FES_MMU_LOG_IRQ
    ldr w1,[x0]
    cbnz w1,.Llog_enable_fail_\@
    mmu_constant x0,FES_MMU_LOG_STATUS
    ldr w1,[x0]
    cbnz w1,.Llog_enable_fail_\@
    mmu_log_scope
    mmu_log_state armed,2
    mmu_log_message .Llog_armed
    b .Llog_arm_done_\@
.Llog_admit_fail_\@:
    mmu_log_fail .Llog_fail_admit
.Llog_enable_fail_\@:
    mmu_log_message .Llog_fail_enable
    b fes_nano_mmu_logging_capture_close
.Llog_arm_done_\@:
.endm

.pushsection .rodata.fes_mmu_logging,"a"
.Llog_arm_before: .asciz "FES_NANO_EL3_MMU_LOG phase=arm-before\r\n"
.Llog_armed: .asciz "FES_NANO_EL3_MMU_LOG phase=armed\r\n"
.Llog_restore_before: .asciz "FES_NANO_EL3_MMU_LOG phase=restore-before\r\n"
.Llog_restored: .asciz "FES_NANO_EL3_MMU_LOG phase=restored\r\n"
.Llog_inactive: .asciz "FES_NANO_EL3_MMU_LOG phase=inactive\r\n"
.Llog_interrupted: .asciz "FES_NANO_EL3_MMU_LOG phase=interrupted\r\n"
.Llog_retired: .asciz "FES_NANO_EL3_MMU_LOG phase=retired\r\n"
.Llog_fail_admit: .asciz "FES_NANO_EL3_MMU_LOG FAIL phase=admit\r\n"
.Llog_fail_enable: .asciz "FES_NANO_EL3_MMU_LOG FAIL phase=enable\r\n"
.Llog_fail_restore: .asciz "FES_NANO_EL3_MMU_LOG FAIL phase=restore\r\n"
.Llog_fail_status: .asciz "FES_NANO_EL3_MMU_LOG FAIL phase=status\r\n"
.popsection
