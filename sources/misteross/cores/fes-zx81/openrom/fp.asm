; SPDX-License-Identifier: GPL-2.0-or-later
; Floating point in the ZX81 five-byte format: exponent byte (0 = zero,
; bias 128) and a 32-bit mantissa 0.1xxx with the sign in its top bit.
; Values live on the calculator stack between STKBOT and STKEND. Arithmetic
; unpacks into FA and FB: sign, exponent, m1 m2 m3 m4, guard.

PARSE_ES equ FT+3               ; exponent sign
PARSE_EV equ FT+4               ; exponent value / print exponent
DIGITS   equ PRBUFF+24

fp_calc:
        rst 8                   ; the RST 28 calculator is not provided yet
        db R_ARG

; ---------------------------------------------------------------------
; Multi-byte helpers. B = byte count; "end" is the last (lowest) byte.

; (HL..) += (DE..) from the ends backwards; carry out.
madd:   and a
.l:     ld a,(de)
        adc a,(hl)
        ld (hl),a
        dec hl
        dec de
        djnz .l
        ret

; (DE..) -= (HL..) from the ends backwards; borrow out.
msub:   and a
.l:     ld a,(de)
        sbc a,(hl)
        ld (de),a
        dec hl
        dec de
        djnz .l
        ret

; Negate (HL..) from the end backwards.
mneg:   and a
.l:     ld a,0
        sbc a,(hl)
        ld (hl),a
        dec hl
        djnz .l
        ret

; Shift right from HL forwards (mshr_c shifts the carry in).
mshr:   and a
mshr_c: rr (hl)
        inc hl
        djnz mshr_c
        ret

; Shift left from the end at HL backwards (mshl_c shifts the carry in).
mshl:   and a
mshl_c: rl (hl)
        dec hl
        djnz mshl_c
        ret

mclear: xor a
.l:     ld (hl),a
        inc hl
        djnz .l
        ret

; Exchange B bytes at HL and DE.
swap_mem:
        ld a,(de)
        ld c,(hl)
        ld (hl),a
        ld a,c
        ld (de),a
        inc hl
        inc de
        djnz swap_mem
        ret

; ---------------------------------------------------------------------
; Stack primitives.

; Push the five bytes at HL.
stk_push:
        set 6,(iy+Y_FLAGS)      ; a number
        push bc
        push de
        ld bc,5
        call test_room
        ld de,(STKEND)
        ldir
        ld (STKEND),de
        pop de
        pop bc
        ret

; HL -> top item (DE = -5).
stk_top:
        ld hl,(STKEND)
        ld de,-5
        add hl,de
        ret

; Drop the top item. HL -> where it was.
stk_drop:
        call stk_top
        ld (STKEND),hl
        ret

stk_dup:
        call stk_top
        jr stk_push

; Push a copy of the item below the top.
stk_over:
        call stk_top
        add hl,de
        jr stk_push

stk_swap:
        call stk_top
        push hl
        add hl,de
        pop de
        ld b,5
        jr swap_mem

; Push FA, packed.
stk_push_fa:
        set 6,(iy+Y_FLAGS)
        ld bc,5
        call test_room
        ld de,(STKEND)
        call fp_pack_fa
        ld (STKEND),de
        ret

; Pop the top item into FA.
stk_pop_fa:
        call stk_drop
        ld de,FA
        jr fp_unpack

; Push a string descriptor: DE = address, BC = length.
stk_push_str:
        res 6,(iy+Y_FLAGS)      ; a string
        push bc
        ld bc,5
        call test_room
        ld hl,(STKEND)
        ld (hl),0
        inc hl
        ld (hl),e
        inc hl
        ld (hl),d
        inc hl
        pop bc
        ld (hl),c
        inc hl
        ld (hl),b
        inc hl
        ld (STKEND),hl
        ret

; Pop a string descriptor: DE = address, BC = length.
stk_pop_str:
        call stk_drop
        inc hl
        ld e,(hl)
        inc hl
        ld d,(hl)
        inc hl
        ld c,(hl)
        inc hl
        ld b,(hl)
        ret

; ---------------------------------------------------------------------
; Packing.

; HL -> packed, DE -> unpacked (7 bytes).
fp_unpack:
        ld a,(hl)
        inc hl
        and a
        jr z,.zero
        ld c,a
        ld a,(hl)
        and 0x80
        ld (de),a
        inc de
        ld a,c
        ld (de),a
        inc de
        ld a,(hl)
        or 0x80
        ld (de),a
        inc hl
        inc de
        ld bc,3
        ldir
        xor a
        ld (de),a
        ret
.zero:  ex de,hl
        ld b,7
        jr mclear

; Round FA and pack it at DE; DE advances by five.
fp_pack_fa:
        ld a,(FA+1)
        and a
        jr z,.zero
        ld a,(FA+6)
        rla
        jr nc,.store
        ld hl,FA+5
        ld b,4
.inc:   inc (hl)
        jr nz,.store
        dec hl
        djnz .inc
        ld a,0x80               ; carried out of m1: 0.1000...
        ld (FA+2),a
        inc (hl)                ; HL -> exponent
        jr z,fp_overflow
.store: ld hl,FA+1
        ldi
        ld a,(FA)
        ld c,a
        ld a,(hl)
        xor c
        and 0x7f
        xor c                   ; sign bit from FA, the rest from m1
        ld (de),a
        inc de
        inc hl
        ld bc,3
        ldir
        ret
.zero:  ex de,hl
        ld b,5
        call mclear
        ex de,hl
        ret

fp_overflow:
        rst 8
        db R_OVERFLOW

; Normalize FA (zero mantissa or exponent underflow gives zero).
fp_norm_fa:
        ld hl,FA+2
        ld b,5
        xor a
.t:     or (hl)
        inc hl
        djnz .t
        jr z,fa_zero
.loop:  ld a,(FA+2)
        rla
        ret c
        ld hl,FA+6
        ld b,5
        call mshl
        ld hl,FA+1
        dec (hl)
        jr nz,.loop
fa_zero:
        ld hl,FA
        ld b,7
        jr mclear

; Shift the five mantissa bytes at HL right by A bits.
shr5:   ld c,a
.bytes: ld a,c
        sub 8
        jr c,.bits
        ld c,a
        push hl
        push bc
        ld de,4
        add hl,de
        ld d,h
        ld e,l
        dec hl
        ld bc,4
        lddr
        xor a
        ld (de),a
        pop bc
        pop hl
        jr .bytes
.bits:  inc c
.bit:   dec c
        ret z
        push hl
        ld b,5
        call mshr
        pop hl
        jr .bit

swap_ab:
        ld hl,FA
        ld de,FB
        ld b,7
        jr swap_mem

sign_xor:
        ld a,(FB)
        ld hl,FA
        xor (hl)
        ld (hl),a
        ret

; FT = HL + FA exponent.
add_fa_exp:
        ld a,(FA+1)
        ld e,a
        ld d,0
        add hl,de
        ld (FT),hl
        ret

dec_ft: ld hl,(FT)
        dec hl
        ld (FT),hl
        ret

; ---------------------------------------------------------------------
; Arithmetic on FA, FB.

fa_add_fb:
        ld a,(FB+1)
        and a
        ret z
        ld a,(FA+1)
        and a
        jr nz,.both
        ld hl,FB
        ld de,FA
        ld bc,7
        ldir
        ret
.both:  ld hl,FB+1
        cp (hl)
        call c,swap_ab
        ld a,(FB+1)
        ld b,a
        ld a,(FA+1)
        sub b
        cp 40
        ret nc
        ld hl,FB+2
        call shr5
        ld a,(FA)
        ld hl,FB
        xor (hl)
        ld hl,FA+6
        ld de,FB+6
        ld b,5
        jp m,.sub
        call madd
        ret nc
        ld hl,FA+2
        ld b,5
        call mshr_c
        ld hl,FA+1
        inc (hl)
        ret nz
        jr fp_overflow
.sub:   ex de,hl
        call msub
        jr nc,fp_norm_fa
        ld hl,FA+6
        ld b,5
        call mneg
        ld hl,FA
        ld a,(hl)
        xor 0x80
        ld (hl),a
        jr fp_norm_fa

; Exponent in FT (16-bit) to FA, with overflow and underflow.
fa_set_exp:
        ld hl,(FT)
        ld a,h
        and a
        jr nz,.range
        ld a,l
        and a
        jr z,fa_zero
        ld (FA+1),a
        ret
.range: bit 7,h
        jr nz,fa_zero
        jr fp_overflow

fa_mul_fb:
        ld a,(FA+1)
        and a
        ret z
        ld a,(FB+1)
        and a
        jr z,fa_zero
        call sign_xor
        ld a,(FB+1)
        ld e,a
        ld d,0
        ld hl,-128
        add hl,de
        call add_fa_exp
        ld hl,FW
        ld b,9
        call mclear
        ld c,32
.loop:  ld hl,FB+2
        ld b,4
        call mshr
        jr nc,.shift
        ld hl,FW+3
        ld de,FA+5
        ld b,4
        call madd
.shift: ld hl,FW
        ld b,8
        call mshr_c
        dec c
        jr nz,.loop
        ld a,(FW)
        rla
        jr c,.norm
        ld hl,FW+7
        ld b,8
        call mshl
        call dec_ft
.norm:  ld hl,FW
        ld de,FA+2
        ld bc,5
        ldir
        jr fa_set_exp

fa_div_fb:
        ld a,(FB+1)
        and a
        jr z,fp_overflow
        ld a,(FA+1)
        and a
        ret z
        call sign_xor
        ld a,(FB+1)
        ld e,a
        ld d,0
        ld hl,129
        and a
        sbc hl,de
        call add_fa_exp
        xor a
        ld (FB+1),a             ; divisor: 0, m1..m4
        ld (FW),a               ; remainder: 0, m1..m4
        ld hl,FA+2
        ld de,FW+1
        ld bc,4
        ldir
        ld c,40
.loop:  ld de,FW+4
        ld hl,FB+5
        ld b,5
        call msub
        ccf                     ; quotient bit: no borrow
        jr c,.q
        ld hl,FW+4
        ld de,FB+5
        ld b,5
        call madd
        and a
.q:     ld hl,FA+6
        ld b,5
        call mshl_c
        ld hl,FW+4
        ld b,5
        call mshl
        dec c
        jr nz,.loop
        ld a,(FA+2)
        rla
        jr c,fa_set_exp
        ld hl,FA+6
        ld b,5
        call mshl
        call dec_ft
        jr fa_set_exp

; ---------------------------------------------------------------------
; Stack operations: second op top -> second.

binop_load:
        call stk_top
        push hl
        add hl,de
        ld de,FA
        call fp_unpack
        pop hl
        ld de,FB
        jr fp_unpack

binop_store:
        call stk_drop
        call stk_top
        ex de,hl
        jr fp_pack_fa

negate_fb:
        ld hl,FB
        ld a,(hl)
        xor 0x80
        ld (hl),a
        ret

fp_add: call binop_load
        call fa_add_fb
        jr binop_store
fp_sub: call binop_load
        call negate_fb
        call fa_add_fb
        jr binop_store
fp_mul: call binop_load
        call fa_mul_fb
        jr binop_store
fp_div: call binop_load
        call fa_div_fb
        jr binop_store

; Compare second with top, dropping both: Z if equal, carry if second < top.
fp_compare:
        call binop_load
        call negate_fb
        call fa_add_fb
        call stk_drop
        call stk_drop
fa_sign:
        ld a,(FA+1)
        and a
        ret z
        ld a,(FA)
        rla                     ; carry = sign (A is now 0)
        inc a                   ; NZ, carry kept
        ret

; Compare the top item with the constant at HL, leaving the stack as it
; was: carry if top < constant.
fp_cmp_const:
        push hl
        call stk_top
        ld de,FA
        call fp_unpack
        pop hl
        ld de,FB
        call fp_unpack
        call negate_fb
        call fa_add_fb
        jr fa_sign

; Unary operations on the top item.
fp_neg: call stk_top
        ld a,(hl)
        and a
        ret z
        inc hl
        ld a,(hl)
        xor 0x80
        ld (hl),a
        ret
fp_abs: call stk_top
        inc hl
        res 7,(hl)
        ret
fp_sgn: call stk_top
        ld a,(hl)
        and a
        ret z
        ld (hl),0x81
        inc hl
        ld a,(hl)
        and 0x80
        ld (hl),a
        inc hl
        ld b,3
        jr mclear

; Top item is zero: Z.
fp_is_zero:
        call stk_top
        ld a,(hl)
        and a
        ret

; Pop the top item: Z (and A = 0) if it was zero.
fp_pop_zero:
        call stk_top
        ld (STKEND),hl
        ld a,(hl)
        and a
        ret

; INT: largest integer not above the top item. Truncate toward zero, then
; one less when that moved a negative number up.
fp_int: call stk_dup
        call stk_top
        push hl
        ld de,FA
        call fp_unpack
        ld a,(FA+1)
        cp 160
        jr nc,.pk               ; already whole
        sub 129
        jr c,.z                 ; |x| < 1
        cpl
        add a,32                ; 160 - exponent
        ld hl,FA+2
        call shr5               ; the integer part, as a 32-bit number
        ld hl,FA+1
        ld (hl),160
        xor a
        ld (FA+6),a             ; no fraction
        call fp_norm_fa
        jr .pk
.z:     call fa_zero
.pk:    pop de
        call fp_pack_fa         ; x trunc(x)
        call stk_over
        call stk_over
        call fp_compare         ; x < trunc(x)?
        jr nc,.ok
        ld hl,const_one
        call stk_push
        call fp_sub
.ok:    call stk_swap
        jr stk_drop

; Pop the top item as a rounded 32-bit magnitude in DE:HL (D high).
; A = sign (0x80 negative). Carry if the magnitude is 2^32 or more.
fp_to_long:
        call stk_pop_fa
fa_to_long:
        ld a,(FA+1)
        cp 128
        jr c,.zero
        cp 161
        ccf
        ret c
        ld b,a
        ld a,160
        sub b
        ld hl,FA+2
        call shr5
        ld hl,(FA+2)
        ld e,h
        ld d,l
        ld hl,(FA+4)
        ld a,h
        ld h,l
        ld l,a
        ld a,(FA+6)
        rla
        jr nc,.sign
        inc hl
        ld a,h
        or l
        jr nz,.sign
        inc de
        ld a,d
        or e
        scf
        ret z
.sign:  ld a,(FA)
        and a
        ret
.zero:  ld de,0
        ld h,d
        ld l,e
        xor a
        ret

; Pop the top item as BC. A = sign; carry if the magnitude exceeds 65535.
fp_to_bc:
        call fp_to_long
        ret c
        ld b,h
        ld c,l
        inc d
        dec d
        scf
        ret nz
        inc e
        dec e
        scf
        ret nz
        and a
        ret

; Pop an integer 0..65535 (report B otherwise).
get_uint_pop:
        call fp_to_bc
        jr c,range_bad
        and a
        ret z
        ld a,b
        or c
        ret z
range_bad:
        rst 8
        db R_RANGE

; Pop an integer 0..255 into A.
get_byte_pop:
        call get_uint_pop
        ld a,b
        and a
        ld a,c
        ret z
        jr range_bad

; Push BC (unsigned).
fp_from_bc:
        push bc
        call fa_zero
        pop bc
        ld hl,FA+1
        ld (hl),144             ; 0.BC * 2^16
        inc hl
        ld (hl),b
        inc hl
        ld (hl),c
        call fp_norm_fa
        jr stk_push_fa

fp_from_a:
        ld c,a
        ld b,0
        jr fp_from_bc

; Push 1 if carry else 0.
fp_push_flag:
        ld bc,0
        rl c
        jr fp_from_bc

const_one:   db 0x81,0x00,0x00,0x00,0x00
const_ten:   db 0x84,0x20,0x00,0x00,0x00
const_1e7:   db 0x98,0x18,0x96,0x80,0x00
const_1e8:   db 0x9b,0x3e,0xbc,0x20,0x00
const_pi:    db 0x82,0x49,0x0f,0xda,0xa2

; Multiply / divide the top item by ten.
fp_times10:
        ld hl,const_ten
        call stk_push
        jr fp_mul
fp_div10:
        ld hl,const_ten
        call stk_push
        jr fp_div

; ---------------------------------------------------------------------
; Decimal text to number. HL -> digits; pushes the value, HL after it.
dec_to_fp:
        push hl
        ld bc,0
        call fp_from_bc
        pop hl
        ld bc,0                 ; B = digits after the point, C = 1 there
.dig:   ld a,(hl)
        call is_digit
        jr nc,.nd
        push bc
        push hl
        sub C_0
        push af
        call fp_times10
        pop af
        call fp_from_a
        call fp_add
        pop hl
        pop bc
        ld a,b
        add a,c
        ld b,a
        inc hl
        jr .dig
.nd:    cp C_DOT
        jr nz,.scale
        ld a,c
        and a
        jr nz,.scale
        inc c
        inc hl
        jr .dig
.scale: ld a,b
        push hl
        call .pow10
        call nz,fp_div
        pop hl
        ret
; Push 10 ** A (A > 0); Z if A = 0 (nothing pushed).
.pow10: and a
        ret z
        ld b,a
        ld hl,const_one
        call stk_push
.p:     push bc
        call fp_times10
        pop bc
        djnz .p
        or 1
        ret

; ---------------------------------------------------------------------
; Number to text: pops the top item, writes FMT, B = length.
fp_format:
        call stk_top
        ld de,FMT
        ld a,(hl)
        and a
        jr nz,.nonzero
        call stk_drop           ; (DE is gone)
        ld a,C_0
        ld (FMT),a
        ld b,1
        ret
.nonzero:
        inc hl
        bit 7,(hl)
        jr z,.pos
        res 7,(hl)
        ld a,C_MINUS
        ld (de),a
        inc de
.pos:   push de
        xor a
        ld (PARSE_EV),a
.big:   ld hl,const_1e8
        call fp_cmp_const
        jr c,.small
        call fp_div10
        ld hl,PARSE_EV
        inc (hl)
        jr .big
.small: ld hl,const_1e7
        call fp_cmp_const
        jr nc,.int
        call fp_times10
        ld hl,PARSE_EV
        dec (hl)
        jr .small
.int:   call fp_to_long
        ld b,8
.d:     push bc
        call div10_32
        pop bc
        push hl
        ld hl,DIGITS-1
        add a,C_0
        push de
        ld e,b
        ld d,0
        add hl,de
        ld (hl),a
        pop de
        pop hl
        djnz .d
        ld a,l                  ; rounded up to 100000000: "10000000"
        and a
        jr z,.nine
        ld a,C_0+1
        ld (DIGITS),a
        ld hl,PARSE_EV
        inc (hl)
.nine:  ld hl,DIGITS+7          ; B = significant digits
        ld b,8
.z:     ld a,(hl)              ; the first digit is never zero
        cp C_0
        jr nz,.sig
        dec hl
        djnz .z
; B = significant digits. With P digits before the point, 1..8 prints
; plainly, -4..0 as .000ddd, anything else as d.dddE+n.
.sig:   ld a,(PARSE_EV)
        add a,8
        ld c,a                  ; C = P
        dec a
        cp 8
        jr c,.plain
        add a,5
        cp 5
        jr c,.plain
        ld a,c
        dec a                   ; exponent P - 1
        ld c,1
        jr .mode
.plain: ld a,0x7f               ; no exponent
.mode:  ld (PARSE_ES),a
        pop de
        ld hl,DIGITS
        ld a,c
        dec a
        jp p,.whole
        ld a,C_DOT
        call .put
.lz:    ld a,c
        and a
        jr z,.frac
        ld a,C_0
        call .put
        inc c
        jr .lz
.whole: push bc
        ld b,0
        ldir                    ; C digits (zeros past the significant ones)
        pop bc
        ld a,b
        sub c
        jr c,.exp
        jr z,.exp
        ld b,a
        ld a,C_DOT
        call .put
.frac:  ld c,b
        ld b,0
        ldir
.exp:   ld a,(PARSE_ES)
        cp 0x7f
        jr z,.end
        ld b,C_PLUS
        and a
        jp p,.ep
        neg
        ld b,C_MINUS
.ep:    ld c,a
        ld a,C_E
        call .put
        ld a,b
        call .put
        ld a,c
        ld b,C_0-1
.t:     inc b
        sub 10
        jr nc,.t
        add a,10+C_0
        ld c,a
        ld a,b
        cp C_0
        call nz,.put
        ld a,c
        call .put
.end:   ld a,e
        sub FMT & 0xff
        ld b,a
        ret
.put:   ld (de),a
        inc de
        ret

; DE:HL /= 10, A = remainder.
div10_32:
        ld b,32
        xor a
.l:     add hl,hl
        rl e
        rl d
        rla
        cp 10
        jr c,.n
        sub 10
        inc l
.n:     djnz .l
        ret


; ---------------------------------------------------------------------
; Functions.

; RND: SEED = (SEED + 1) * 75 mod 65537 - 1; value SEED / 65536.
fp_rnd: rst 0x20                ; past RND
        ld de,(SEED)
        inc de
        ld a,d
        or e
        ld b,0
        jr nz,.s
        inc b
.s:     ld hl,0
        ld c,l
        ld a,75
.m:     add hl,de
        jr nc,.nc
        inc c
.nc:    push af
        ld a,c
        add a,b
        ld c,a
        pop af
        dec a
        jr nz,.m
        ld e,c
        ld d,0
        and a
        sbc hl,de
        jr c,.w
        dec hl
.w:     ld (SEED),hl
        ld b,h
        ld c,l
        call fp_from_bc
        call stk_top
        ld a,(hl)
        and a
        ret z
        sub 16
        ld (hl),a
        ret

arg_error:
        rst 8
        db R_ARG

; a ** b for whole b: repeated multiplication (1 / a ** -b when negative).
fp_pow: call stk_dup
        call fp_int
        call stk_over
        call fp_compare
        jr nz,arg_error
        call fp_to_bc
        jr c,arg_error
        push af
        ld hl,const_one
        call stk_push           ; a r
.loop:  ld a,b
        or c
        jr z,.done
        push bc
        call stk_over
        call fp_mul
        pop bc
        dec bc
        jr .loop
.done:  call stk_swap
        call stk_drop
        pop af
        and a
        ret z
        ld hl,const_one
        call stk_push
        call stk_swap
        jr fp_div
