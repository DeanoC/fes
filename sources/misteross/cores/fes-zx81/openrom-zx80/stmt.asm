; SPDX-License-Identifier: GPL-2.0-or-later
; Statements and the run loop. Each handler parses its operands and calls
; stmt_end: while checking, that ends the check; when running, it returns
; so the statement can act. One statement per line, as on the ZX80.

stmt_table:
        dw st_list, st_return, st_cls, st_dim, st_print.end, st_for, st_goto
        dw st_poke, st_input, st_rand, st_let, syntax_err, syntax_err
        dw st_next, st_print, syntax_err, st_new, st_run, st_stop
        dw st_cont, st_if, st_gosub, st_load, st_clear, stmt_end.go
        dw syntax_err

; A one-letter name; DEST = it, A = the character after it.
one_letter:
        call name_at
        call name_end
        cp C_DOLLAR
        jr z,syntax_err
        dec b
        jr nz,syntax_err
        jr gc_store

; (HL) = record type A ^ $20 with DEST's letter ($20 + 0-31) in its low
; bits; HL + 1. Keeps BC, DE.
letter_byte:
        push de
        ld de,(DEST)
        ex de,hl
        xor (hl)
        ex de,hl
        pop de
        ld (hl),a
        inc hl
        ret

; ---------------------------------------------------------------------
; The run loop. ERR_SP is the stack each line starts with; its slot holds
; the report routine, and GOSUB records lie above it.

; Obey the line at CH_ADD as a command.
run_cmd:
        rst 0x18
        cp C_NL
        jr z,edit               ; nothing to do
        call cls
        ld hl,0xfffe
        ld (PPC),hl
line_go:
        ld sp,(ERR_SP)
        call temp_clear
        call statement
line_end:
        ld a,(PPC + 1)
        inc a
        jr z,unwind             ; a command has finished: 0
        ld hl,(CH_ADD)
        call line_skip
.at:    ld de,(VARS)
        rst 0x28
        jr nc,unwind            ; no line: 0 and the last or wanted line
        ld d,(hl)
        inc hl
        ld e,(hl)
        inc hl
        ld (PPC),de
        ld (CH_ADD),hl
break_key:                      ; NC if SHIFT and SPACE (BREAK) are both down
        ld a,0x7f
        in a,(0xfe)
        rra
        jr c,.up
        ld a,0xfe
        in a,(0xfe)
        rra
.up:    out (0xff),a            ; the reads started vertical sync
break_got:                      ; (the fast harness gives carry and comes in here)
        jr c,line_go
        jr unwind               ; BREAK: 0 and the next line

; Show report m/n at the bottom of the screen, wait for a key, edit.
; After a report other than 0, CONTINUE goes back to n (n + 1 after STOP).
report: dec sp                  ; keep the slot
        dec sp
        ld a,(ERR_NR)
        inc a
        push af
        jr z,.cmd
        ld hl,(PPC)
        cp R_STOP + 1
        jr nz,.o
        inc hl
.o:     ld (OLDPPC),hl
.cmd:   ld (iy+Y_FLAGX),(1 << X_LOWER) | (1 << X_DROP)
        ld (iy+Y_DF_SZ),2
        ld a,(iy+Y_LINE)        ; the output stays; the report goes on the
        cp 2                    ; bottom row
        call c,cls
.nl:    call next_row           ; down to the bottom row
        ld a,(iy+Y_LINE)
        or a
        jr nz,.nl
        pop af
        add a,C_0
        rst 0x10
        ld a,0x15               ; /
        rst 0x10
        ld hl,(PPC)
        call print_num
        call close_all
        call wait_key
        jr edit

; ---------------------------------------------------------------------

st_list:
        call opt_num
        rst 0x20
        ld (E_PPC),hl
        jr edit

st_return:
        rst 0x20
        ld sp,(ERR_SP)
        pop de                  ; the slot
        pop hl
        ld a,h
        or l
        jr nz,.ok
        push hl                 ; none: put it back
.ok:    push de
        jr nz,new_sp
        rst 8
        db R_RETURN

st_cont:
        rst 0x20
        ld hl,(OLDPPC)
        jr jump_hl

st_run: call opt_num
        rst 0x20
        ld b,h
        ld c,l
        call clear_vars
        ld sp,(ERR_SP)          ; GOSUB records go
        pop de
.r:     pop hl
        ld a,h
        or l
        jr nz,.r
        push hl
        push de
        ld h,b
        ld l,c
        jr new_sp

st_gosub:
        ld bc,72                ; room for the record, and to show report 4
        call test_room          ; and type RUN or NEW (editing drops it)
        call expr_num
        rst 0x20
        ld de,(PPC)
        inc de                  ; RETURN comes back to the next line
        ld sp,(ERR_SP)
        pop bc
        push de
        push bc
new_sp: ld (ERR_SP),sp          ; (RETURN and RUN come in here too)
        jr jump_hl

st_goto:
        call expr_num
        rst 0x20
; Jump to line HL.
jump_hl:
        ld (PPC),hl
        ld b,h
        ld c,l
        call find_line
        jr line_end.at

st_clear:
        rst 0x20
clear_vars:
        push bc
        ld hl,(E_LINE)
        dec hl                  ; the end marker stays
        ld de,(VARS)
        call reclaim_de
        pop bc
        ret

st_if:  call expr_num
        rst 0x30
        db T_THEN
        bit F_CHECK,(iy+Y_FLAGS)
        jr nz,statement
        ld a,h
        or l
        jr nz,statement
        ret

st_poke:
        call expr_num
        push hl
        rst 0x30
        db T_COMMA
        call expr_num
        pop de
        rst 0x20
        ld a,l
        ld (de),a
        ret

st_rand:
        call opt_num
        rst 0x20
        ld a,h
        or l
        jr nz,.s
        ld hl,(FRAMES)          ; none or 0: the frame count
.s:     ld (SEED),hl
        ret

; INPUT: edit a line in the lower part (an expression, given quotes for a
; string) and assign its value.
st_input:
        call dest
        rst 0x20
        push de                 ; the variables do not move while it is edited
        ld a,(PPC + 1)          ; not as a command
        inc a
        jr nz,.prog
        rst 8
        db R_INPUT
.prog:  ld hl,(D_FILE)          ; the workspace becomes an empty edit line
        dec hl
        ld de,(E_LINE)
        call reclaim_de         ; HL = E_LINE
        ld (hl),C_NL
        ld (P_PTR),hl
        ld hl,FLAGS             ; F_DNUM = the destination's type
        res F_DNUM,(hl)
        set F_INPUT,(hl)
        bit F_NUM,(hl)
        jr z,.str
        set F_DNUM,(hl)
        jr .go
.str:   ld a,C_QUOTE            ; a string: the cursor inside quotes
        call edit_insert
        call edit_insert
        call edit_line.left
.go:    ld hl,(DF_END)
        ld (DF_EA),hl           ; where the output ends
        call edit_line
        res F_INPUT,(iy+Y_FLAGS)
        call out_trim           ; the line leaves the screen
        ld hl,(E_LINE)
        ld (CH_ADD),hl
        call expr
        pop de
        push hl
        ld hl,(DEST)            ; line_end goes on from the INPUT line
        ld (CH_ADD),hl
        pop hl
        jr store

st_for: call one_letter
        rst 0x30
        db T_EQ
        call expr_num
        push hl
        rst 0x30
        db T_TO
        call expr_num
        rst 0x20
        ex (sp),hl              ; limit under start
        push hl
        ld hl,(DEST)
        ld bc,0x0160 ^ 0x20
        call find_var           ; any integer of that letter goes
        ex de,hl
        call nc,del_var
        ld bc,7
        call new_var
        ld a,0xe0 ^ 0x20
        call letter_byte
        pop de
        call put_de
        pop de
        call put_de
        ld de,(PPC)
        inc de                  ; NEXT goes to the line after the FOR
; (HL) = DE; HL + 2.
put_de: ld (hl),e
        inc hl
        ld (hl),d
        inc hl
        ret


st_next:
        call one_letter
        rst 0x20
        ld hl,(DEST)
        ld bc,0x01e0 ^ 0x20
        call find_var
        jr nc,.got
        rst 8
        db R_NEXT
.got:   ex de,hl
        ld e,(hl)
        inc hl
        ld d,(hl)
        inc de                  ; the value + 1
        ld a,d
        xor 0x80
        or e
        jr z,overflow
        ld (hl),d
        dec hl
        ld (hl),e
        inc hl
        inc hl
        ld a,(hl)               ; limit - value: negative ends the loop
        sub e
        inc hl
        ld a,(hl)
        sbc a,d                 ; (its high byte gives S and P/V)
        inc hl
        jp po,.nv
        cpl
.nv:    rla
        ret c
        call peek_word
        jr jump_hl

st_dim: call one_letter
        cp T_LPAREN
        jr nz,syntax_err
        call subscript
        rst 0x20
        inc h
        dec h
        jr nz,elem_addr.range   ; 0 to 255 only
        push hl                 ; L = the largest subscript
        inc hl
        add hl,hl
        inc hl
        inc hl
        ld b,h
        ld c,l
        call new_var
        ld a,0xa0 ^ 0x20
        call letter_byte
        pop de
        ld (hl),e
        ld b,e
        inc b                   ; the elements (256 for 255)
        xor a
.z:     inc hl
        ld (hl),a
        inc hl
        ld (hl),a
        djnz .z
        ret

st_print:
        cp C_NL
        jr z,.nl
.item:  sub T_SEMI              ; 0 for ;, 1 for ,
        cp 2
        jr c,.sep
        call expr
        call .val
        rst 0x18
        cp C_NL
        jr z,.nl
        sub T_SEMI
        cp 2
        jr nc,syntax_err
.sep:   dec a
        call z,.tab
        call next_ch
        cp C_NL
        jr nz,.item
.end:   rst 0x20             ; a final ; or , : no NEWLINE (SAVE comes
        ret                     ; here: the FES core keeps no ZX80 tape output)
.nl:    rst 0x20
        jr print_nl
.tab:   call run_only
        ld a,(iy+Y_COL)         ; comma: on to column 8, 16 or 24, else the
        cp 10                   ; next row (COL is 33 - column)
        jr c,.row               ; column 24 or more
        sub 2
        and 7
        inc a                   ; spaces to the next multiple of 8
        ld b,a
.sp:    call space
        djnz .sp
        ret
.row:   ld (iy+Y_COL),1
        ret
.val:   call run_only
        bit F_NUM,(iy+Y_FLAGS)
        jr z,print_str
; Signed decimal HL.
print_num:
        bit 7,h
        jr z,print_uint
        ld a,C_MINUS
        rst 0x10
        call neg_hl
; Unsigned decimal HL, no leading zeros.
print_uint:
        ld de,10
        call divu
        push de
        ld a,h
        or l
        call nz,print_uint
        pop de
        ld a,e
        add a,C_0
        rst 0x10
        ret

; LOAD through the FES core: it copies the mounted image over $4000
; onwards and comes back at $0203. E_LINE is cleared first, so an image
; that arrived is recognised by its own E_LINE.
st_load:
        rst 0x20
        ld hl,(E_LINE)
        push hl
        xor a
        ld (E_LINE + 1),a       ; no image would have E_LINE below $0100
        jr load_entry
load_done:
        pop de
        pop hl                  ; the image overwrote ERR_SP: the stack
        ld (ERR_SP),sp          ; is still ours
        push hl
        ld hl,(E_LINE)
        ld a,h
        or a
        jr nz,.got
        ld (E_LINE),de          ; nothing came: carry on
        ret
.got:   dec hl
        ld a,(hl)
        cp 0x80
        jr nz,init              ; not a program image
                                ; (edit follows, in edit.asm)
