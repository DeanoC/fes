; SPDX-License-Identifier: GPL-2.0-or-later
; Expressions. The value goes on the calculator stack; FLAGS bit 6 is set
; for a number and clear for a string. Strings are five-byte descriptors:
; 0, address, length.

AE_OFF   equ PRBUFF+18
AE_DIMS  equ PRBUFF+20
AE_N     equ PRBUFF+22
AE_I     equ PRBUFF+23

; B = priority of operator A (higher binds tighter), 0 if A is not one.
binop_prio:
        ld hl,prio_table
.l:     ld b,(hl)
        inc b
        dec b
        ret z
        inc hl
        cp (hl)
        inc hl
        jr nz,.l
        ret
prio_table:
        db 2,T_OR, 3,T_AND, 5,C_EQ, 5,C_LT, 5,C_GT, 5,T_LE, 5,T_GE, 5,T_NE
        db 6,C_PLUS, 6,C_MINUS, 8,C_STAR, 8,C_SLASH, 10,T_POW, 0

expr:   ld c,0
expr_c: push bc
        call operand
.loop:  pop bc
        push bc
        rst 0x18
        call binop_prio
        ld a,b
        cp c
        jr c,.done
        jr z,.done
        rst 0x18
        ld e,a
        ld d,(iy+Y_FLAGS)
        push de
        ld c,b
        rst 0x20
        call expr_c
        pop de
        call apply_binop
        jr .loop
.done:  pop bc
        ret

expr_num:
        call expr
need_num:
        bit 6,(iy+Y_FLAGS)
        ret nz
        jr nonsense
expr_str:
        call expr
need_str:
        bit 6,(iy+Y_FLAGS)
        ret z
        jr nonsense

; Numeric expression popped as 0..65535 / 0..255.
expr_uint:
        call expr_num
        jr get_uint_pop
expr_byte:
        call expr_num
        jr get_byte_pop

; ---------------------------------------------------------------------
operand:
        rst 0x18
        cp C_MINUS
        jr z,op_neg
        cp T_NOT
        jr z,op_not
        cp C_LPAREN
        jr z,op_paren
        cp C_QUOTE
        jr z,op_string
        cp C_DOT
        jr z,op_number
        call is_digit
        jr c,op_number
        call is_letter
        jr c,op_variable
        cp C_RND
        jr z,fp_rnd
        cp C_PI
        jr z,op_pi
        cp C_INKEY
        jr z,op_inkey
        cp T_CODE
        jr c,nonsense
        cp T_NOT
        jr c,op_function
        jr nonsense

op_neg: rst 0x20
        ld c,9
        call expr_c
        call need_num
        jr fp_neg

op_not: rst 0x20
        ld c,4
        call expr_c
        call need_num
        call fp_pop_zero
        sub 1                   ; carry if it was zero
        jr fp_push_flag

op_paren:
        rst 0x20
        call expr
        call expect_rparen
        jr maybe_slice

op_string:
        ld hl,(CH_ADD)
        inc hl
        ld d,h
        ld e,l
        ld bc,0
.s:     ld a,(hl)
        cp C_QUOTE
        jr z,.end
        cp C_NL
        jr z,nonsense
        inc hl
        inc bc
        jr .s
.end:   inc hl
        ld (CH_ADD),hl
        call stk_push_str
maybe_slice:
        bit 6,(iy+Y_FLAGS)
        ret nz
        rst 0x18
        cp C_LPAREN
        ret nz
        jr slice

op_number:
        ld hl,(CH_ADD)
.n:     ld a,(hl)
        inc hl
        cp C_NUMBER
        jr z,.got
        cp C_NL
        jr z,nonsense
        jr .n
.got:   call stk_push
        ld (CH_ADD),hl
        ret


op_pi:  rst 0x20
        ld hl,const_pi
        jr stk_push

op_inkey:
        rst 0x20
        call kscan
        ld b,h
        ld c,l
        call decode
        jr c,.none
        ld a,(hl)
        jr str_char
.none:  ld bc,0                  ; "" (any address)
        jr stk_push_str

; One-character string A in the workspace.
str_char:
        push af
        ld bc,1
        rst 0x30
        pop af
        ld (de),a
        jr stk_push_str

; ---------------------------------------------------------------------
; Variables.

; DE -> name, B = length, C = kind (0 number, 1 string, 2 number array,
; 3 string array). Z and HL -> entry if found, else HL -> end marker.
find_var:
        ld hl,(VARS)
.next:  ld a,(hl)
        cp 0x80
        jr z,.none
        push hl
        call var_match
        pop hl
        ret z
        push de
        call var_skip
        pop de
        jr .next
.none:  or a
        ret

; Z if the entry at HL is the variable DE/B/C.
var_match:
        ld a,(de)
        xor (hl)
        and 0x1f
        ret nz
        ld a,(hl)
        and 0xe0
        push hl
        push de
        ld hl,kind_types
        ld e,c
        ld d,0
        add hl,de
        pop de
        cp (hl)
        pop hl
        jr z,.single
        ld a,c                  ; numbers also match FOR and long names
        and a
        ret nz
        ld a,(hl)
        and 0xe0
        cp 0xe0
        jr z,.single
        cp 0xa0
        ret nz
        ld a,b
        dec a
        jr z,.no
        push de
        push bc
        push hl
        ld b,a
.c:     inc de
        inc hl
        ld a,(de)
        ld c,a
        ld a,(hl)
        and 0x7f
        cp c
        jr nz,.end
        ld a,(hl)
        rla
        jr c,.last
        djnz .c
        ld a,1                  ; the entry's name is longer
        or a
        jr .end
.last:  dec b
.end:   pop hl
        pop bc
        pop de
        ret
.single:
        ld a,b
        dec a
        ret
.no:    inc a
        ret

kind_types:
        db 0x60, 0x40, 0x80, 0xc0

; HL = entry -> HL = next entry.
var_skip:
        ld a,(hl)
        and 0xe0
        cp 0x60
        jr z,.six
        cp 0xe0
        jr z,.for
        cp 0xa0
        jr z,.long
        inc hl
        ld e,(hl)
        inc hl
        ld d,(hl)
        inc hl
        add hl,de
        ret
.six:   ld de,6
        add hl,de
        ret
.for:   ld de,18
        add hl,de
        ret
.long:  inc hl
        ld a,(hl)
        rla
        jr nc,.long
        ld de,6
        add hl,de
        ret

; HL = numeric entry -> HL -> its five-byte value.
var_value:
        ld a,(hl)
        and 0xe0
        cp 0xa0
        inc hl
        ret nz
.l:     ld a,(hl)
        inc hl
        rla
        jr nc,.l
        ret

; Name at CH_ADD: DE -> start, B = length; CH_ADD after it, A = next.
scan_name:
        ld hl,(CH_ADD)
        ld d,h
        ld e,l
        ld b,0
.l:     ld a,(hl)
        call is_alnum
        jr nc,.e
        inc hl
        inc b
        jr .l
.e:     ld (CH_ADD),hl
        ret

; A variable's value. With OF_LVAL set, where to assign it instead:
; TG_KIND 1 number at DEST, 2 new number (name at X_PTR, TG_LEN long),
; 3 string variable (entry at DEST, or 0 if new), 4 text at DEST, STRLEN
; long (array element or slice).
op_variable:
        ld hl,OFLAGS
        ld a,(hl)
        res OF_LVAL,(hl)        ; subscripts are values
        push af
        call scan_name
        ld (X_PTR),de
        ld (iy+TG_LEN-0x4000),b
        cp C_DOLLAR
        jr z,.string
        push de
        push bc
        rst 0x18
        pop bc
        pop de
        cp C_LPAREN
        ld c,0
        jr nz,.simple
        ld c,2
        call find_var
        jr nz,undefined
        push hl
        call subscripts
        pop hl
        call num_element
        jr .num
.simple:
        call find_var
        ld c,2
        jr nz,.out
        call var_value
.num:   ld (DEST),hl
        ld c,1
.out:   pop af
        bit OF_LVAL,a
        jr z,.value
        ld (iy+TG_KIND-0x4000),c
        ret
.string:
        ld a,b                  ; string names are one letter
        dec a
        jr nz,nonsense
        rst 0x20
        ld c,1
        call find_var
        jr z,.have
        ld hl,0
.have:  ld (DEST),hl
        rst 0x18
        cp C_LPAREN
        ld c,3
        jr nz,.out
        ld hl,(DEST)
        ld a,h
        or l
        jr z,undefined
        call str_entry
        call slice
        call stk_pop_str
.fixed: ld (DEST),de
        ld (STRLEN),bc
        ld c,4
        jr .out
.value: ld hl,(DEST)
        dec c
        jr z,stk_push
        dec c
        jr z,undefined
        dec c
        jr nz,.fix
        ld a,h
        or l
        jr z,undefined
        call str_entry
        jr maybe_slice
.fix:   ex de,hl
        ld bc,(STRLEN)
        call stk_push_str
        jr maybe_slice

; Push the string variable whose entry is at HL.
str_entry:
        inc hl
        ld c,(hl)
        inc hl
        ld b,(hl)
        inc hl
        ex de,hl
        jr stk_push_str

undefined:
        rst 8
        db R_UNDEF

; Parse "(e, e, ...)" at CH_ADD onto the calculator stack. A = count.
subscripts:
        rst 0x20
        ld b,0
.l:     push bc
        call expr_num
        pop bc
        inc b
        rst 0x18
        cp C_COMMA
        jr nz,.end
        push bc
        rst 0x20
        pop bc
        jr .l
.end:   push bc
        call expect_rparen
        pop bc
        ld a,b
        ret

subscript_error:
        rst 8
        db R_SUBSCR

; HL -> array entry, A = subscripts on the stack (popped). Offset over the
; first A dimensions: HL = offset, DE -> first element, B = dimensions.
array_offset:
        ld (AE_N),a
        inc hl
        inc hl
        inc hl
        ld a,(hl)
        ld b,a
        ld a,(AE_N)
        cp b
        jr z,.ok
        jr nc,subscript_error
.ok:    push bc
        inc hl
        ld (AE_DIMS),hl
        ld e,b
        ld d,0
        add hl,de
        add hl,de
        push hl
        ld hl,0
        ld (AE_OFF),hl
        xor a
        ld (AE_I),a
.loop:  ld a,(AE_N)
        ld hl,AE_I
        sub (hl)
        jr z,.done
        ld b,a
        ld hl,(STKEND)
        ld de,-5
.p:     add hl,de
        djnz .p
        ld de,FA
        call fp_unpack
        call fa_to_long
        jr c,subscript_error
        and a
        jr nz,subscript_error
        ld a,d
        or e
        jr nz,subscript_error
        ld a,h
        or l
        jr z,subscript_error
        dec hl                  ; zero-based subscript
        push hl
        ld a,(AE_I)
        add a,a
        ld e,a
        ld d,0
        ld hl,(AE_DIMS)
        add hl,de
        ld e,(hl)
        inc hl
        ld d,(hl)               ; DE = this dimension
        pop bc                  ; BC = subscript - 1
        ld h,b
        ld l,c
        and a
        sbc hl,de               ; subscript - 1 - dimension, must borrow
        jr nc,subscript_error
        ld hl,(AE_OFF)
        call mul16
        add hl,bc
        ld (AE_OFF),hl
        ld hl,AE_I
        inc (hl)
        jr .loop
.done:  ld a,(AE_N)
        ld b,a
.drop:  push bc
        call stk_drop
        pop bc
        djnz .drop
        pop de
        pop bc
        ld hl,(AE_OFF)
        ret

; Numeric element: HL -> array entry, A = subscripts. HL -> value.
num_element:
        push af
        push hl
        inc hl
        inc hl
        inc hl
        ld b,(hl)
        pop hl
        pop af
        cp b
        jr nz,subscript_error
        call array_offset
        ld b,h
        ld c,l
        add hl,hl
        add hl,hl
        add hl,bc
        add hl,de
        ret

; String on the stack, CH_ADD at "(": replace it by the slice.
slice:  rst 0x20
        ld bc,1
        cp T_TO
        jr z,.to
        call expr_uint
        rst 0x18
        cp T_TO
        jr z,.to
        ld d,b
        ld e,c
        jr .close
.to:    push bc
        rst 0x20
        ld de,0xffff
        cp C_RPAREN
        jr z,.nou
        call expr_uint
        ld d,b
        ld e,c
.nou:   pop bc
.close: push bc
        push de
        call expect_rparen
        call stk_pop_str
        ld (AE_OFF),de
        pop hl                  ; upper
        ld a,h
        and l
        inc a
        jr nz,.u
        ld h,b
        ld l,c
.u:     pop de                  ; lower
        push hl
        and a
        sbc hl,de
        pop hl
        jr c,.empty
        ld a,d
        or e
        jr z,.bad
        push hl
        and a
        sbc hl,bc
        pop hl
        jr z,.ok
        jr nc,.bad
.ok:    and a
        sbc hl,de
        inc hl
        ld b,h
        ld c,l
        ld hl,(AE_OFF)
        add hl,de
        dec hl
        ex de,hl
        jr .push
.empty: ld bc,0
        ld de,(AE_OFF)
.push:  call stk_push_str
        jr maybe_slice
.bad:   jr subscript_error

; ---------------------------------------------------------------------
; Binary operators. E = operator, D = left operand FLAGS.
apply_binop:
        ld a,d
        xor (iy+Y_FLAGS)
        and 0x40
        jr nz,nonsense          ; operands of different types
        bit 6,d
        jr z,.strings
        ld a,e
        cp C_PLUS
        jr z,fp_add
        cp C_MINUS
        jr z,fp_sub
        cp C_STAR
        jr z,fp_mul
        cp C_SLASH
        jr z,fp_div
        cp T_POW
        jr z,fp_pow
        cp T_AND
        jr z,.and
        cp T_OR
        jr z,.or
        push de
        call fp_compare
        pop de
        jr rel_result
; a AND b = a if b else 0; a OR b = 1 if b else a.
.and:   call fp_pop_zero
        ret nz
        ld c,0
        jr .set
.or:    call fp_pop_zero
        ret z
        ld c,1
.set:   call stk_drop
        ld b,0
        jr fp_from_bc
.strings:
        ld a,e
        cp C_PLUS
        jr z,str_concat
        cp T_AND
        jr z,nonsense
        cp T_OR
        jr z,nonsense
        push de
        call str_compare
        pop de

; Z / carry from a comparison -> 1 or 0 for operator E. Each operator's
; mask has bit 0 for equal, bit 1 for less, bit 2 for greater.
rel_result:
        ld b,1
        jr z,.k
        ld b,2
        jr c,.k
        ld b,4
.k:     ld a,e
        cp T_LE
        jr c,.c
        sub T_LE-C_EQ-1
.c:     sub C_GT
        ld hl,rel_masks
        ld e,a
        ld d,0
        add hl,de
        ld a,(hl)
        and b
        add a,0xff
        jr fp_push_flag
rel_masks:
        db 4,2,1,3,5,6          ; > < = <= >= <>

; Compare strings second, top (dropped). Z equal, carry if second < top.
str_compare:
        call stk_pop_str
        ld (AE_OFF),de
        ld (AE_DIMS),bc
        call stk_pop_str
        ld hl,(AE_OFF)
.l:     ld a,b
        or c
        jr z,.aend
        push hl
        ld hl,(AE_DIMS)
        ld a,h
        or l
        pop hl
        jr z,.gt
        ld a,(de)
        cp (hl)
        ret nz
        inc de
        inc hl
        dec bc
        push hl
        ld hl,(AE_DIMS)
        dec hl
        ld (AE_DIMS),hl
        pop hl
        jr .l
.aend:  ld hl,(AE_DIMS)
        ld a,h
        or l
        ret z
        scf
        ret
.gt:    ld a,1
        or a
        ret

str_concat:
        call stk_pop_str
        ld (AE_OFF),de
        ld (AE_DIMS),bc
        call stk_pop_str
        push de
        push bc
        ld hl,(AE_DIMS)
        add hl,bc
        ld b,h
        ld c,l
        push bc
        rst 0x30
        pop hl                  ; total
        pop bc                  ; first length
        ex (sp),hl              ; HL = first text, stack = total
        push de
        ld a,b
        or c
        jr z,.b
        ldir
.b:     ld hl,(AE_OFF)
        ld bc,(AE_DIMS)
        ld a,b
        or c
        jr z,.d
        ldir
.d:     pop de
        pop bc
        jr stk_push_str

; ---------------------------------------------------------------------
; Functions: the argument is the next operand.
op_function:
        push af
        rst 0x20
        call operand
        pop af
        cp T_LEN+1              ; CODE, VAL and LEN take a string
        push af
        call c,need_str
        pop af
        call nc,need_num
        sub T_CODE
        cp T_SIN-T_CODE
        jr c,.t
        cp T_INT-T_CODE
        jr c,arg_error          ; SIN to EXP: not provided yet
        sub T_INT-T_SIN
.t:     add a,a
        ld e,a
        ld d,0
        ld hl,fn_table
        add hl,de
        ld a,(hl)
        inc hl
        ld h,(hl)
        ld l,a
        jp (hl)

fn_table:
        dw fn_code, fn_val, fn_len, fp_int, arg_error, fp_sgn
        dw fp_abs, fn_peek, fn_usr, fn_strs, fn_chrs


fn_code:
        call stk_pop_str
        ld a,b
        or c
        jr z,.z
        ld a,(de)
.z:     jr fp_from_a

fn_len: call stk_pop_str
        jr fp_from_bc


fn_peek:
        call get_uint_pop
        ld a,(bc)
        jr fp_from_a

fn_usr: call get_uint_pop
        ld h,b
        ld l,c
        call jp_hl
        ld iy,0x4000
        jr fp_from_bc
jp_hl:  jp (hl)

fn_chrs:
        call get_byte_pop
        jr str_char

fn_strs:
        call fp_format
        ld c,b
        ld b,0
        rst 0x30                ; BC kept
        push de
        push bc
        ld hl,FMT
        ldir
        pop bc
        pop de
        jr stk_push_str

fn_val: call stk_pop_str
        push de
        inc bc
        rst 0x30
        dec bc
        pop hl
        push de
        ld a,b
        or c
        jr z,.e
        ldir
.e:     ld a,C_NL
        ld (de),a
        pop hl
        push hl
        xor a
        call tokenize
        pop hl
        ld de,(CH_ADD)
        push de
        ld (CH_ADD),hl
        call expr_num
        rst 0x18
        cp C_NL
        jr nz,nonsense
        pop hl
        ld (CH_ADD),hl
        ret

; ---------------------------------------------------------------------
; Insert hidden five-byte values after the numbers in the text at HL
; (NEWLINE-terminated). A nonzero: skip a leading line number. Strings
; and REM text are left alone. C bit 0: in a string; bit 1: just after a
; letter or digit (part of a name).
tokenize:
        and a
        jr z,.go
.ln:    ld a,(hl)
        call is_digit
        jr c,.lnext
        and a
        jr nz,.go
.lnext: inc hl
        jr .ln
.go:    ld c,0
.loop:  ld a,(hl)
        cp C_NL
        ret z
        inc hl
        cp C_QUOTE
        jr nz,.nq
        ld a,c
        cpl
        and 1
        ld c,a
        jr .loop
.nq:    bit 0,c
        jr nz,.loop
        cp T_REM
        ret z
        cp C_NUMBER
        jr z,.skip5
        bit 1,c
        jr nz,.word
        call is_digit
        jr c,.number
        cp C_DOT
        jr nz,.word
        ld a,(hl)
        call is_digit
        jr c,.number
        ld a,C_DOT
.word:  call is_alnum
        ld c,0
        jr nc,.loop
        ld c,2
        jr .loop
.skip5: ld de,5
        add hl,de
        jr .loop
.number:
        dec hl
        call dec_to_fp
        ld bc,6
        call make_room
        ld (hl),C_NUMBER
        inc hl
        push hl
        call stk_drop
        pop de
        ld bc,5
        ldir
        ex de,hl
        jr .go
