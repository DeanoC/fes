; SPDX-License-Identifier: GPL-2.0-or-later
; Expressions. A value is a 16-bit integer or the address of a string's
; text, which ends at its closing quote; it is returned in HL with F_NUM
; giving the type. While checking, types are worked out but not values.

expr_num:
        call expr
need_num:
        bit F_NUM,(iy+Y_FLAGS)
        ret nz
        jr syntax_err

; A whole expression.
expr:   ld b,0
        jr expr_b

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
apply:  ld a,(FLAGS)
        xor b
        and 1 << F_NUM
        jr nz,syntax_err        ; mixed types
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
        jr overflow

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
        call abs_hl
        ex de,hl
        call abs_hl
        ex de,hl
        ret
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
.l:     add hl,hl
        ex de,hl
        adc hl,hl
        and a
        sbc hl,bc
        jr nc,.one
        add hl,bc
        ex de,hl
        jr .n
.one:   ex de,hl
        inc l
.n:     dec a
        jr nz,.l
        ret

; ---------------------------------------------------------------------
operand:
        rst 0x18
        cp T_LPAREN
        jr z,.paren
        cp T_MINUS
        ld b,9
        jr z,.unary
        cp T_NOT
        ld b,4
        jr z,.unary
        cp C_QUOTE
        jr z,.str
        call is_digit
        jr c,.num
        call is_letter
        jr c,name_operand
        jr syntax_err
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
.paren: jr subscript.in
.num:   ld hl,(CH_ADD)
        call parse_uint
        jr c,syntax_err         ; 32768 or more
        ld (CH_ADD),hl
        ex de,hl
        set F_NUM,(iy+Y_FLAGS)
        ret
.str:   ld hl,(CH_ADD)
        inc hl
        push hl                 ; the text
.s:     ld a,(hl)
        ld (CH_ADD),hl
        cp C_NL
        jr z,syntax_err         ; not closed
        inc hl
        cp C_QUOTE
        jr nz,.s
        ld (CH_ADD),hl
        pop hl
        res F_NUM,(iy+Y_FLAGS)
        ret

; A function call, variable or array element.
name_operand:
        ld hl,(CH_ADD)
        call find_fn
        jr c,.var
        ld (CH_ADD),hl          ; past the function's "("
        add a,a
        ld e,a
        ld d,0
        ld hl,fn_jumps
        add hl,de
        ld e,(hl)
        inc hl
        ld d,(hl)               ; bit 15: string argument; 14: string result
        push de
        call expr
        rst 0x30
        db T_RPAREN
        pop de
        ld a,(FLAGS)            ; F_NUM to bit 7: 1 for a number
        add a,a
        xor d
        jp p,syntax_err         ; not the argument this function takes
        set F_NUM,(iy+Y_FLAGS)
        bit 6,d
        jr z,.r
        res F_NUM,(iy+Y_FLAGS)
.r:     call run_only
        ld a,d
        and 0x3f
        ld d,a
        push de
        ret
.var:   call var_ref
        call run_only
        jr c,undefined
        ex de,hl
        bit F_NUM,(iy+Y_FLAGS)
        ret z                   ; a string: HL -> its text
        jr peek_word

undefined:
        rst 8
        db R_UNDEF

; HL = the word at HL.
peek_word:
        ld a,(hl)
        inc hl
        ld h,(hl)
        ld l,a
        ret

; CH_ADD -> "(": the numeric expression and ")" -> HL.
subscript:
        call .in
        jr need_num
.in:    call next_ch
        call expr
        rst 0x30
        db T_RPAREN
        ret

; HL -> a name: if it is a function followed by its "(", A = the
; function's number and HL -> after the "("; carry if not.
find_fn:
        ld de,fn_table
        ld c,0
.e:     ld a,(de)
        or a
        scf
        ret z
        push hl
.c:     ld a,(de)
        inc de
        cp (hl)
        jr nz,.no
        inc hl
        rla
        jr nc,.c                ; up to and including the "("
        pop af
        ld a,c
        and a
        ret
.no:    pop hl
.skip:  rla                     ; the rest of this name
        ld a,(de)
        inc de
        jr nc,.skip
        dec de
        inc c
        jr .e

fn_jumps:
        dw fn_abs, fn_code + 0x8000, fn_peek, fn_rnd, fn_usr
        dw fn_chr + 0x4000, fn_str + 0x4000, fn_tl + 0xc000

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
        ld a,h
        or l
        ld a,77
        jr z,.z
        ld de,77
        call mulu
        ld a,e
.z:     ld e,a
        ld d,0
        and a
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
        ld hl,(D_FILE)
        push hl
        call ws_add
        jr fn_str.q
fn_str: ld de,(D_FILE)
        push de
        set X_STR,(iy+Y_FLAGX)
        call print_num
        res X_STR,(iy+Y_FLAGX)
.q:     ld a,C_QUOTE
        call ws_add
        pop hl
        ret
fn_tl:  ld a,(hl)
        cp C_QUOTE
        ret z
        inc hl
        ret

; HL -> a name: HL -> the byte after it, B = its length, A = that byte.
name_end:
        ld b,0
.l:     inc b
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

; CH_ADD -> a variable name. Running: DE -> an integer's or element's
; value, or a string's text; carry if there is none.
var_ref:
        ld hl,(CH_ADD)
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
        ld c,0x60
        jr find_var
.str:   djnz name_bad
        res F_NUM,(iy+Y_FLAGS)
        call run_only
        ld bc,0x0180
        jr find_var
.arr:   djnz name_bad
        push hl
        call subscript
        pop de
        call run_only
        jr elem_addr
name_bad:
        ld (CH_ADD),hl
        jr syntax_err

; (HL) = a name of length B, C = record type (an integer: $60, $40 used
; for longer names): DE -> just after the name in its record (value, text,
; or largest subscript); carry if none. Type $60 also finds a FOR record of
; the same letter.
find_var:
        ld a,(hl)
        and 0x1f
        or c
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
        ld bc,0x01a0
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
        ld a,0x60               ; one letter
        djnz .multi
        call letter_byte
        jr .val
.multi: ld a,0x40               ; longer: the letters, the last marked
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
        ld a,0x80
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
