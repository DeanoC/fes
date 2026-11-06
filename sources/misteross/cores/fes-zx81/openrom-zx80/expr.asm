; SPDX-License-Identifier: GPL-2.0-or-later
; Expressions. A value is a 16-bit integer or the address of a string's
; text, which ends at its closing quote; it is returned in HL with F_NUM
; giving the type. While checking, types are worked out but not values.

; An optional number (A = the current character): none gives 0.
opt_num:
        sub C_NL
        ld h,a
        ld l,a
        ret z                   ; (operand reads the character again)
expr_num:
        call expr
need_num:
        bit F_NUM,(iy+Y_FLAGS)
        ret nz
; A symbol that does not fit. While checking, mark it and leave the check;
; a line that was never checked (a hand-made image) stops with report 0.
syntax_err:
        ld hl,(CH_ADD)
        ld (X_PTR),hl
unwind: ld sp,(ERR_SP)
        ret

; A whole expression.
expr:   ld b,0

; Operators binding tighter than B, by precedence climbing.
expr_b: push bc
        call operand
.loop:  ex de,hl                ; DE = value so far
        rst 0x18
        ld c,0                  ; C = the operator's priority, 0 if none
        sub T_MINUS
        cp T_LT - T_MINUS + 1
        jr nc,.got
        ld hl,op_prio
        ld c,a
        ld b,0
        add hl,bc
        ld c,(hl)
.got:   rst 0x18                ; A = the operator again
        pop hl
        push hl
        ld l,a
        ld a,h
        cp c
        jr nc,.done             ; binds no tighter than the floor
        push de
        ld a,(FLAGS)
        ld h,a
        push hl                 ; the left type, the operator
        call next_ch
        ld b,c
        call expr_b
        pop bc
        pop de
        call apply
        jr .loop
.done:  ex de,hl
        pop bc
        ret
op_prio:                        ; - + * / AND OR ** = > <
        db 6, 6, 8, 7, 3, 2, 10, 5, 5, 5

; HL = DE <operator C> HL; B = the left value's FLAGS.
apply:  call same_type
        ld a,c
        cp T_EQ
        jr nc,compare
        call need_num
        call run_only
        cp T_POWER
        jr z,power
        sub T_MINUS
        jr z,.sub
        dec a
        jr z,.add
        dec a
        jr z,mul
        dec a
        jr z,div
        dec a                   ; AND, else OR
        ld a,h
        jr z,.and
        or d
        ld h,a
        ld a,l
        or e
        ld l,a
        ret
.and:   and d
        ld h,a
        ld a,l
        and e
        ld l,a
        ret
.sub:   ex de,hl
        and a
        sbc hl,de
        jr .v
.add:   and a
        adc hl,de
.v:     ret po
; Reports. RST 8 stops the statement; so does arithmetic overflow.
overflow:
        rst 8
        db R_OVERFLOW

; The value is of the type B's F_NUM says.
same_type:
        ld a,(FLAGS)
        xor b
        and 1 << F_NUM
        ret z
        jr syntax_err           ; mixed types

power:  bit 7,h
        jr nz,overflow          ; negative powers
        ld b,h
        ld c,l
        ld hl,1
.l:     ld a,b
        or c
        ret z
        push bc
        push de
        call mul
        pop de
        pop bc
        dec bc
        jr .l

; Relations give -1 for true and 0 for false, for numbers or strings.
compare:
        ld a,(FLAGS)
        call run_num
        ld b,T_EQ               ; the relation that holds: = > <
        and 1 << F_NUM
        jr z,.sl
        ex de,hl
        and a
        sbc hl,de
        jr z,.rel
        ld a,h
        jp po,.nov
        cpl
.nov:   rla
        jr c,.lt
.gt:    inc b
        jr .rel
.sl:    ld a,(de)
        cp (hl)
        jr nz,.diff
        dec a                   ; both at their closing quotes
        jr z,.rel
        inc de
        inc hl
        jr .sl
.diff:  dec a
        jr z,.lt                ; the left one ended first
        ld a,(hl)
        dec a
        jr z,.gt                ; the right one ended first
        ld a,(de)
        cp (hl)
        jr nc,.gt
.lt:    ld b,T_LT
.rel:   ld a,c
        cp b
        ld hl,0
        ret nz
        dec hl
        ret

mul:    call signs
        push bc
        call mulu
        pop bc
        ld a,d
        or e
        jr nz,overflow
        jr signed

div:    ld a,h
        or l
        jr z,overflow
        call signs
        push bc
        ex de,hl
        call divu
        pop bc
; Magnitude HL with sign B bit 7; overflow if it does not fit.
signed: bit 7,b
        call nz,neg_hl
        ld a,h
        or l
        ret z
        ld a,h
        xor b
        ret p
        jr overflow

; DE, HL made non-negative; B bit 7 = the sign of their product.
signs:  ld a,h
        xor d
        ld b,a
        call .a                 ; DE, then (coming back here) HL
.a:     ex de,hl
abs_hl: bit 7,h
        ret z
neg_hl: xor a
        sub l
        ld l,a
        sbc a,a
        sub h
        ld h,a
        ret

; DE:HL = DE * HL, unsigned.
mulu:   ld b,h
        ld c,l
        ld hl,0
        ld a,16
.l:     add hl,hl
        rl e
        rl d
        jr nc,.n
        add hl,bc
        jr nc,.n
        inc de
.n:     dec a
        jr nz,.l
        ret

; HL = HL / DE, DE = remainder, unsigned (DE not 0, at most 32768).
divu:   ld b,d
        ld c,e
        ld de,0
        ld a,16
.l:     add hl,hl               ; the next bit of HL to carry
        inc l                   ; a 1 in the quotient, unless
        ex de,hl
        adc hl,hl               ; (no carry: the remainder is below 32768)
        sbc hl,bc
        jr nc,.k
        add hl,bc
        dec e                   ; it does not go
.k:     ex de,hl
        dec a
        jr nz,.l
        ret

; ---------------------------------------------------------------------
operand:
        rst 0x18                ; (HL = CH_ADD)
        cp T_LPAREN
        jr z,subscript.in
        cp T_MINUS
        ld b,9
        jr z,.unary
        cp T_NOT
        ld b,4
        jr z,.unary
        cp C_QUOTE
        jr z,.str
        call is_alnum
        jr nc,syntax_err
        cp C_A
        jr nc,name_operand
.num:   call parse_uint         ; a digit
        jr c,syntax_err         ; 32768 or more
        ld (CH_ADD),hl
        ex de,hl
        set F_NUM,(iy+Y_FLAGS)
        ret
.unary: push af
        call next_ch
        call expr_b
        call need_num
        pop af
        call run_only
        cp T_NOT
        jr nz,neg_chk
        inc hl                  ; NOT x = -(x + 1)
        jr neg_hl
.str:   inc hl
        push hl                 ; the text
.s:     ld a,(hl)
        ld (CH_ADD),hl
        cp C_NL
        jr z,syntax_err         ; not closed
        inc hl
        dec a                   ; the closing quote
        jr nz,.s
        ld (CH_ADD),hl
        pop hl
        res F_NUM,(iy+Y_FLAGS)
        ret

; A function call, variable or array element.
; A function: the name with its "(" from fn_table, then the routine.
name_operand:
        ld de,fn_table
.e:     ld a,(de)
        or a
        jr z,.var               ; not a function
        push hl
.c:     ld a,(de)
        inc de
        cp (hl)
        jr nz,.no
        inc hl
        rla
        jr nc,.c                ; up to and including the "("
        pop af
        dec hl
        ld (CH_ADD),hl          ; at the function's "("
        ex de,hl
        ld e,(hl)
        inc hl
        ld d,(hl)               ; the routine, as in fn_table
        push de
        call subscript.in       ; the argument and ")"
        pop de
        ld a,(FLAGS)            ; F_NUM to bit 7: 1 for a number
        add a,a
        xor d
        jp p,syntax_err         ; not the argument this function takes
        ld a,(FLAGS)            ; F_NUM = bit 6 of D: the result's type
        xor d
        and ~(1 << F_NUM) & 0xff
        xor d
        ld (FLAGS),a
        call run_only
        ld a,d
        and 0x3f
        ld d,a
        push de
        ret
.no:    pop hl
.skip:  rla                     ; the rest of this name
        ld a,(de)
        inc de
        jr nc,.skip
        inc de                  ; and its routine
        jr .e
.var:   call var_ref
        call run_only
        jr c,undefined
        ex de,hl
        bit F_NUM,(iy+Y_FLAGS)
        ret z                   ; a string: HL -> its text

; HL = the word at HL.
peek_word:
        ld a,(hl)
        inc hl
        ld h,(hl)
        ld l,a
        ret

undefined:
        rst 8
        db R_UNDEF

; CH_ADD -> "(": the numeric expression and ")" -> HL.
subscript:
        call .in
        jr need_num
.in:    call next_ch
        call expr
        rst 0x30
        db T_RPAREN
        ret

fn_abs: bit 7,h
        ret z
neg_chk:
        ld a,h
        xor 0x80
        or l
        jr z,overflow           ; -(-32768)
        jr neg_hl
fn_code:
        ld a,(hl)
        dec a
        jr nz,fn_peek
        ld h,a                  ; "": 0
        ld l,a
        ret
fn_peek:
        ld l,(hl)
        ld h,0
        ret
; Seed * 77 modulo 65537, as low word minus high word (a seed of 0 counts
; as 65536); the result is n * seed / 65536 + 1.
fn_rnd: push hl
        ld hl,(SEED)
        dec hl
        ld de,77
        call mulu               ; (seed - 1) * 77, then + 77: 0 is 65536
        ld bc,77
        add hl,bc
        jr nc,.z
        inc e
.z:     and a                   ; (D is 0)
        sbc hl,de
        ld (SEED),hl
        pop de
        call mulu
        ex de,hl
        inc hl
        ret
fn_usr: call .go
        ld iy,0x4000
        ret
.go:    jp (hl)
fn_chr: ld a,l
        call str_open
        call put_char
        jr fn_str.q
fn_str: call str_open
        call print_num
.q:     ld a,C_QUOTE
        call put_char
        res F_EDIT,(iy+Y_FLAGS)
        ld hl,(P_PTR)           ; the display file starts after it
        ld de,(D_FILE)          ; the string
        ld (D_FILE),hl
        ex de,hl
        ret

; Output goes in at D_FILE (through P_PTR, as EDIT types a line in): a new
; string at the end of the workspace.
str_open:
        ld de,(D_FILE)
        ld (P_PTR),de
        set F_EDIT,(iy+Y_FLAGS)
        ret
fn_tl:  ld a,(hl)
        dec a                   ; ""
        ret z
        inc hl
        ret

; HL -> a name: HL -> the byte after it, B = its length, A = that byte.
; A name of 256 characters is a wrong symbol.
name_end:
        ld b,0
.l:     inc b
        jr z,syntax_err         ; 256 characters: too long
        inc hl
        ld a,(hl)
        call is_alnum
        jr c,.l
        ret

; ---------------------------------------------------------------------
; Variables. Records: 011xxxxx value (one-letter integer); 010xxxxx
; letters, the last with bit 7, value (longer names); 100xxxxx text quote
; (string); 101xxxxx max (one byte) elements (array); 111xxxxx value limit
; line (FOR); $80 ends them. xxxxx is the first letter's code minus 32.

; HL = CH_ADD -> a variable name. Running: DE -> an integer's or
; element's value, or a string's text; carry if there is none.
var_ref:
        push hl
        call name_end
        ex (sp),hl
        pop de                  ; DE -> after the name
        cp C_DOLLAR
        jr nz,.n
        inc de                  ; past the $
.n:     ld (CH_ADD),de
        jr z,.str
        cp T_LPAREN
        jr z,.arr
        call run_num
        ld c,0x60 ^ 0x20
        jr find_var
.arr:   djnz .bad
        push hl
        call subscript
        pop de
        call run_only
        jr elem_addr
.bad:
        ld (CH_ADD),hl
        jr syntax_err
.str:   djnz .bad
        res F_NUM,(iy+Y_FLAGS)
        call run_only
        ld bc,0x0180 ^ 0x20

; (HL) = a name of length B, C = record type ^ $20 (an integer: $60, $40
; used for longer names): DE -> just after the name in its record (value, text,
; or largest subscript); carry if none. Type $60 also finds a FOR record of
; the same letter.
find_var:
        ld a,(hl)               ; the letter is $20 + 0-31
        xor c
        dec b
        jr z,.one
        and 0xdf                ; longer names: type $40
.one:   inc b
        ld c,a
        ld de,(VARS)
.l:     ld a,(de)
        cp 0x80
        scf
        ret z
        xor c
        jr z,.hit
        cp 0x80                 ; FOR for an integer of its letter
        jr nz,.next
        bit 7,c
        jr nz,.next
.hit:   push bc
        push hl
        push de
        dec b
        jr z,.yes               ; one letter: the type byte names it
.c:     inc hl
        inc de
        ld a,(de)
        xor (hl)
        add a,a                 ; NZ: other letters; C: the record's last
        jr nz,.no
        jr c,.end
        djnz .c
        jr .no                  ; the name ended first
.end:   dec b                   ; the name must end there too
        jr nz,.no
.yes:   pop af
        pop hl
        pop bc
        inc de
        and a
        ret
.no:    pop de
        pop hl
        pop bc
.next:  call var_next
        jr .l

; DE -> the record after the one at DE.
var_next:
        ld a,(de)
        inc de
        add a,a
        jr c,.hi
        add a,a
        add a,a
        jr c,.two               ; one letter
.name:  ld a,(de)
        inc de
        rla
        jr nc,.name
        jr .two
.hi:    add a,a
        jr c,.for
        add a,a
        jr c,.arr
.str:   ld a,(de)               ; up to the closing quote
        inc de
        dec a
        jr nz,.str
        ret
.for:   inc de                  ; value, limit, line
        inc de
        inc de
        inc de
.two:   inc de
        inc de
        ret
.arr:   ld a,(de)               ; largest subscript, then the elements
        inc de
.e:     inc de
        inc de
        sub 1
        jr nc,.e
        ret

; (DE) = an array's letter, HL = subscript: DE -> the element (NC).
elem_addr:
        push hl
        ex de,hl
        ld bc,0x01a0 ^ 0x20
        call find_var
        pop hl
        jr c,undefined
        ld a,(de)               ; the largest subscript
        inc de
        inc h
        dec h
        jr nz,.range
        cp l
        jr c,.range
        add hl,hl
        add hl,de
        ex de,hl
        ret
.range: rst 8
        db R_SUBSCR

; BC bytes for a new variable: HL = its record (where the end marker
; was); the end marker follows it.
new_var:
        ld hl,(E_LINE)
        call make_room
        dec hl
        push hl
        add hl,bc
        ld (hl),0x80
        pop hl
        ret

; A destination for LET and INPUT: DEST = its name; DE as var_ref, 0 if
; it is not made yet.
dest:   call name_at
        call var_ref
        ret nc
        ld de,0
        ret

; A name must start here: DEST = it (HL), A = its first letter.
name_at:
        rst 0x18
        call is_letter
        jr nc,syntax_err
        ld (DEST),hl
        ret

st_let: call dest
        ld a,(FLAGS)
        push de
        push af
        rst 0x30
        db T_EQ
        call expr
        pop bc                  ; B = the destination's FLAGS
        pop de
        call same_type
        rst 0x20

; Assign value HL to the destination DE found by dest.
store:  bit F_NUM,(iy+Y_FLAGS)
        jr z,store_str
        ld a,d
        or e
        jr z,.new
        ex de,hl
        jr put_de
.new:   push hl
        ld hl,(DEST)
        call name_end
        push bc
        ld c,b
        ld b,0
        inc bc
        inc bc
        call new_var
        pop bc
        ld a,0x60 ^ 0x20        ; one letter
        djnz .multi
        call letter_byte
        jr .val
.multi: ld a,0x40 ^ 0x20        ; longer: the letters, the last marked
        call letter_byte
        ld de,(DEST)
.mc:    inc de
        ld a,(de)
        ld (hl),a
        inc hl
        djnz .mc
        dec hl
        set 7,(hl)
        inc hl
.val:   pop de
        jr put_de

; A string is made afresh at the end of the variables, then the old one
; goes (so LET A$=A$ works).
store_str:
        push de
        ld bc,2
        push hl
.len:   ld a,(hl)
        cp C_QUOTE
        jr z,.got
        inc hl
        inc bc
        jr .len
.got:   pop hl
        ld de,(E_LINE)
        rst 0x28
        jr c,.low
        add hl,bc               ; in the workspace: it moves up too
.low:   push hl
        push bc
        call new_var
        pop bc
        pop de
        ld a,0x80 ^ 0x20
        call letter_byte
        ex de,hl
        dec bc
        ldir                    ; the text and its quote
        pop hl
        ld a,h
        or l
        ret z
del_var:                        ; remove the record whose type byte is at HL-1
        dec hl
        ld d,h
        ld e,l
        call var_next
        ex de,hl
        jr reclaim_de
