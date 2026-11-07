/* SPDX-License-Identifier: BSD-3-Clause */
/* Private cold BSS exit only. Preserve writeback intent using the pinned
 * vendor DCCSW selector, including its erratum clean/invalidate alias.
 * Never skip maintenance or fall back to a range operation after rejection. */
#ifndef FES_EXIT_START
#define FES_EXIT_START __BSS_START__
#endif
#ifndef FES_EXIT_END
#define FES_EXIT_END __BSS_END__
#endif
#ifndef FES_EXIT_CALL
#define FES_EXIT_CALL dcsw_op_all
#endif

.macro fes_ram_bss_clean
    .if DCCSW != 1 && DCCSW != 2
    .error "reviewed set/way clean selector changed"
    .endif
    fes_decode_scope .Lfes_exit_reject_\@
    cbnz x26, .Lfes_exit_reject_\@
    ldr x8, =FES_EXIT_START
    cmp x0, x8
    b.ne .Lfes_exit_reject_\@
    ldr x9, =FES_RAM_BSS_BASE
    cmp x8, x9
    b.ne .Lfes_exit_reject_\@
    ldr x9, =FES_EXIT_END
    ldr x10, =FES_RAM_BSS_LIMIT
    cmp x9, x10
    b.hi .Lfes_exit_reject_\@
    cmp x9, x8
    b.ls .Lfes_exit_reject_\@
    tst x8, #63
    b.ne .Lfes_exit_reject_\@
    tst x9, #63
    b.ne .Lfes_exit_reject_\@
    sub x9, x9, x8
    cmp x1, x9
    b.ne .Lfes_exit_reject_\@
    mrs x8, csselr_el1
    cbnz x8, .Lfes_exit_reject_\@
    fes_decode_message fes_exit_cache_before
    mov x0, #DCCSW
    bl FES_EXIT_CALL
    fes_decode_scope .Lfes_exit_reject_\@
    cbnz x26, .Lfes_exit_reject_\@
    mrs x8, csselr_el1
    cbnz x8, .Lfes_exit_reject_\@
    fes_decode_message fes_exit_cache_after
    b .Lfes_exit_done_\@
.Lfes_exit_reject_\@:
    fes_decode_fail fes_exit_cache_reject
.Lfes_exit_done_\@:
.endm

.pushsection .rodata.fes_exit_cache,"a"
fes_exit_cache_before: .asciz "FES_NANO_RAM EXIT_CACHE phase=clean-before\r\n"
fes_exit_cache_after: .asciz "FES_NANO_RAM EXIT_CACHE phase=clean-after\r\n"
fes_exit_cache_reject: .asciz "FES_NANO_RAM EXIT_CACHE FAIL phase=admit\r\n"
.balign 8
.global fes_nano_ram_exit_cache_mode
.type fes_nano_ram_exit_cache_mode, %object
fes_nano_ram_exit_cache_mode: .quad DCCSW
.size fes_nano_ram_exit_cache_mode, 8
.popsection
