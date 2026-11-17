/* SPDX-License-Identifier: BSD-3-Clause */
#ifndef FES_NANO_FIRMWARE_CACHE_INC
#define FES_NANO_FIRMWARE_CACHE_INC
/* Selected, exclusively owned cold entry only. Explicitly invalidate the
 * accessible cache hierarchy rather than assume that cache-off means empty.
 * Address-based PoC maintenance is not issued against the OCRAM DII port.
 * The pinned TF-A helper uses registers only and preserves x20..x30. */
#ifndef FES_CACHE_START
#define FES_CACHE_START __NOBITS_START__
#endif
#ifndef FES_CACHE_END
#define FES_CACHE_END __NOBITS_END__
#endif
#ifndef FES_CACHE_CALL
#define FES_CACHE_CALL dcsw_op_all
#endif

.macro fes_ram_nobits_cache
    .if DCISW != 0
    .error "reviewed set/way invalidate selector changed"
    .endif
    fes_decode_scope .Lfes_cache_reject_\@
    mov x8, #FES_DECODE_ACTIVE
    cmp x26, x8
    b.ne .Lfes_cache_reject_\@
    ldr x8, =FES_CACHE_START
    cmp x0, x8
    b.ne .Lfes_cache_reject_\@
    ldr x9, =FES_RAM_BASE
    cmp x8, x9
    b.ne .Lfes_cache_reject_\@
    ldr x9, =FES_CACHE_END
    cmp x9, x8
    b.ls .Lfes_cache_reject_\@
    tst x9, #63
    b.ne .Lfes_cache_reject_\@
    ldr x10, =FES_RAM_LIMIT
    cmp x9, x10
    b.hi .Lfes_cache_reject_\@
    sub x9, x9, x8
    cmp x1, x9
    b.ne .Lfes_cache_reject_\@
    /* The vendor helper restores CSSELR to0; admit only that initial value. */
    mrs x8, csselr_el1
    cbnz x8, .Lfes_cache_reject_\@
    fes_decode_message fes_cache_before
    mov x0, #DCISW
    bl FES_CACHE_CALL
    fes_decode_scope .Lfes_cache_reject_\@
    mov x8, #FES_DECODE_ACTIVE
    cmp x26, x8
    b.ne .Lfes_cache_reject_\@
    mrs x8, csselr_el1
    cbnz x8, .Lfes_cache_reject_\@
    fes_decode_message fes_cache_after
    b .Lfes_cache_done_\@
.Lfes_cache_reject_\@:
    fes_decode_fail fes_cache_reject
.Lfes_cache_done_\@:
.endm

.pushsection .rodata.fes_cache,"a"
fes_cache_before: .asciz "FES_NANO_RAM CACHE phase=setway-before\r\n"
fes_cache_after: .asciz "FES_NANO_RAM CACHE phase=setway-after\r\n"
fes_cache_reject: .asciz "FES_NANO_RAM CACHE FAIL phase=admit\r\n"
.popsection
#endif
