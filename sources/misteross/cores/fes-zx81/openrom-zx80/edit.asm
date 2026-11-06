; SPDX-License-Identifier: GPL-2.0-or-later
; The editor and the command loop. E_LINE holds the line being typed;
; P_PTR is the cursor, where the next key goes.

edit:   ld sp,(ERR_SP)
        ld hl,0x00ff
        ld (ERR_NR),hl          ; no report; FLAGS clear
        call eline_reset
        call edit_line
        ld hl,(E_LINE)
        call gc_store
        call parse_uint         ; none: 0
        ld (CH_ADD),hl
        ld a,d
        or e
        jr z,run_cmd            ; line 0 is a command

; Store the edit line as line DE; CH_ADD -> the text after its number
; (CH_ADD moves with the edit line). No text deletes the line.
        ld (E_PPC),de
        ld b,d
        ld c,e
        call find_line
        jr nz,.new
        push hl
        call next_line
        pop de
        call reclaim_de         ; the old line; HL is where it was
.new:   push hl
        ld hl,(CH_ADD)
        call text_len
        pop hl
        ld a,b
        or c
        jr z,edit
        inc bc
        inc bc
        inc bc
        call make_room
        ld de,(E_PPC)
        ld (hl),d
        inc hl
        ld (hl),e
        inc hl
        dec bc
        dec bc                  ; the text and its NEWLINE
        ex de,hl
        ld hl,(CH_ADD)
        ldir
        jr edit

; Obey the line at CH_ADD as a command.
run_cmd:
        rst 0x18
        cp C_NL
        jr z,edit               ; nothing to do
        call cls
        ld hl,0xfffe
        ld (PPC),hl
        jr line_go

; An empty edit line; a minimal display file after it.
eline_reset:
        ld hl,(E_LINE)
        ld (P_PTR),hl
        ld (hl),C_NL
        inc hl
        ld (D_FILE),hl
        ld (hl),C_NL
        inc hl
        ld (DF_END),hl
        ret

; Edit until a NEWLINE with no syntax error.
edit_line:
.draw:
; Check the edit line: X_PTR = the first symbol that cannot be part of a
; statement (0 if none); F_KMODE if the cursor stands where a keyword goes.
        ld hl,(ERR_SP)
        push hl
        ld hl,.fail
        push hl
        ld (ERR_SP),sp
        ld hl,0
        ld (X_PTR),hl
        ld hl,FLAGS
        res F_KMODE,(hl)
        set F_CHECK,(hl)
        ld hl,(E_LINE)
        call gc_store
        bit F_INPUT,(iy+Y_FLAGS)
        jr nz,.input
        call parse_uint         ; a line number, 9999 at most (none: 0)
        jr c,syntax_err
        push hl
        ld hl,-10000
        add hl,de
        pop hl
        jr c,syntax_err
        call gc_store
        call k_here
        cp C_NL                 ; empty, or a number alone (deletes it)
        jr nz,statement         ; a statement always ends by unwinding
.ok:    pop hl
.fail:  pop hl
        ld (ERR_SP),hl
        res F_CHECK,(iy+Y_FLAGS)
        call display
        ld (iy+Y_FLAGX),1 << X_DROP     ; a key that does not fit is lost
.key:   call wait_key
        call decode_key
        cp C_NL
        jr z,.enter
        sub K_UP
        cp K_RUBOUT - K_UP + 1
        jr nc,.ins
        ld hl,.draw
        push hl
        or a
        jr z,.up
        dec a
        jr z,.down
        dec a
        jr z,.left
        dec a
        jr z,.right
        dec a
        jr z,.home
        dec a
        jr z,edit_copy
.rub:   call .prev              ; RUBOUT
        ld bc,1
        jr reclaim
.right: ld hl,(P_PTR)
        ld a,(hl)
        cp C_NL
        ret z
        inc hl
        jr .setp
.left:  call .prev
.setp:  ld (P_PTR),hl
        ret
.prev:  ld hl,(P_PTR)           ; HL -> the byte before the cursor
        ld de,(E_LINE)
        rst 0x28
        dec hl
        ret nz
        pop af                  ; none: nothing to do
        ret
.home:  ld hl,0
        jr .set
.up:    call .find
        call line_before
        jr c,.num               ; none before: the first line
        ex de,hl
        jr .num
.down:  call .find
        call z,next_line        ; down from an existing line
.num:   call or_last
        ret c                   ; no program
        ld a,(hl)               ; its number
        inc hl
        ld l,(hl)
        ld h,a
.set:   ld (E_PPC),hl
        ret
.find:  ld bc,(E_PPC)
        jr find_line
.ins:   add a,K_UP
        call edit_insert
        jr .draw
.enter: ld hl,(X_PTR)
        ld a,h
        or l
        jr nz,.key              ; not yet a statement
        ld (iy+Y_FLAGX),h       ; running: no room is report 4 again
        ret
.input: call expr
        ld a,(FLAGS)
        and (1 << F_NUM) | (1 << F_DNUM)
        jp po,syntax_err        ; not the type INPUT wants
        rst 0x20                ; (unwinds)

; Insert A at the cursor (room is made after it, so E_LINE itself never
; moves). No room: while editing (X_DROP) nothing happens.
edit_insert:
        push af
        ld hl,(P_PTR)
        inc hl
        ld bc,1
        call make_room
        jr c,.no
        dec hl
        pop af
        ld b,(hl)               ; the byte at the cursor moves up
        ld (hl),a
        inc hl
        ld (hl),b
        ld (P_PTR),hl
        ret
.no:    pop af
        ret

; EDIT: the current line (or the next, or the last) replaces the edit
; line: its number in four columns, the cursor, its text.
edit_copy:
        ld bc,(E_PPC)
        call find_line
        call or_last
        jr c,eline_reset        ; no program: an empty line
        push hl
        call eline_reset
        pop hl
        set F_EDIT,(iy+Y_FLAGS)
        call list_number        ; typed in before the cursor
        res F_EDIT,(iy+Y_FLAGS)
        call text_len
        push de
        ld hl,(P_PTR)
        call make_room          ; (keeps BC, HL)
        pop de
        ret c
        push hl
        ex de,hl
        ldir
        pop hl
        ld (P_PTR),hl           ; the cursor stays before the text
        ret

; HL = the line at HL, or the last line if HL is past the end; carry if
; there is no program.
or_last:
        ld de,(VARS)
        rst 0x28
        ccf
        ret nc
        call line_before
        ex de,hl
        ret

; HL -> a line's text: DE = it, BC = its length before the NEWLINE.
text_len:
        push hl
        call line_skip
        pop de
        and a
        sbc hl,de
        dec hl
        ld b,h
        ld c,l
        ret

; A = key index -> the code it types in the cursor's mode.
decode_key:
        ld e,a
        ld d,0
        ld hl,key_plain         ; key_shift follows it
        add hl,de
        cp 40
        ld a,(hl)
        ret nc                  ; shifted
        bit F_KMODE,(iy+Y_FLAGS)
        ret z
        call is_letter          ; K cursor: letters type keywords, in order
        ret nc                  ; from LIST at A
        add a,T_LIST - C_A
        ret

; ---------------------------------------------------------------------
; The screen while editing: for INPUT the program's output stays and the
; line goes in the lower part; otherwise the listing above, the edit line
; at the bottom. The edit line takes the RAM it needs first.
display:
        bit F_INPUT,(iy+Y_FLAGS)
        jr nz,input_display
edit_display:
        ld (iy+Y_FLAGX),(1 << X_LOWER) | (1 << X_DROP)
        call cls
        dec (iy+Y_LINE)         ; at most 23 rows, and the blank one above
        call print_eline        ; count its rows (and drop them again)
        ld a,24
        sub (iy+Y_LINE)
        ld (iy+Y_DF_SZ),a
        ld (iy+Y_FLAGX),1 << X_DROP
        call cls_rows
        call list_prog
        ld a,(iy+Y_LINE)        ; close the listing's rows and the blank one
        sub (iy+Y_DF_SZ)
        add a,2
        ld b,a
        call fill_nl
        dec hl
        ld (DF_EA),hl
        ld a,(iy+Y_DF_SZ)
        sub 2
        ld h,a
        ld l,33
        ld (S_POSN),hl
        set X_LOWER,(iy+Y_FLAGX)
        call print_eline
        call close_all
        ld bc,(E_PPC)           ; DE = the current line (or the one before)
        call find_line
        jr z,.got
        call line_before
        ret c
        ex de,hl
.got:   ld d,(hl)
        inc hl
        ld e,(hl)
        ld hl,(DEST)
        rst 0x28
        ld hl,(S_TOP)
        jr c,.move              ; below the listing
        dec hl
        rst 0x28
        ret c                   ; shown
        inc hl
.move:  rst 0x28                ; elsewhere: it starts the listing
        ret z                   ; too long to show whole
        ld (S_TOP),de
        jr edit_display

; INPUT: the program's output (up to DF_EA) stays; a blank row, then the
; line, follow it.
input_display:
        call out_trim
        ld hl,(S_POSN)
        push hl
        ld (iy+Y_FLAGX),(1 << X_LOWER) | (1 << X_DROP)
        call next_row           ; close the output's row
        call next_row           ; and a blank one
        call print_eline
        call close_all
        pop hl
        ld (S_POSN),hl
        ret

; Remove the display file after DF_EA (an INPUT line).
out_trim:
        ld de,(DF_EA)
        ld hl,(DF_END)
        jr reclaim_de

; The edit line with its cursor (K or L) and, before the first wrong
; symbol, an inverse S.
print_eline:
        set F_SPACE,(iy+Y_FLAGS)
        ld hl,(E_LINE)
.l:     ld de,(P_PTR)
        rst 0x28
        jr nz,.s
        ld a,C_K + 0x80
        bit F_KMODE,(iy+Y_FLAGS)
        jr nz,.k
        inc a                   ; inverse L
.k:     call put_char
.s:     ld de,(X_PTR)
        rst 0x28
        jr nz,.c
        ld a,C_S + 0x80
        call put_char
.c:     ld a,(hl)
        cp C_NL
        ret z
        inc hl
        rst 0x10
        jr .l

; ---------------------------------------------------------------------
; Program lines: number (high byte first), text, NEWLINE.

; HL = the first line numbered BC or more (or VARS); Z if exactly BC.
find_line:
        ld hl,PROG
.l:     ld de,(VARS)
        rst 0x28
        jr nc,.end
        ld a,(hl)
        cp b
        jr nz,.ne
        inc hl
        ld a,(hl)
        dec hl
        cp c
        ret z
.ne:    ret nc
        call next_line
        jr .l
.end:   or h                    ; NZ
        ret

; HL -> the line after the line at HL.
next_line:
        inc hl
        inc hl
        jr line_skip

; DE = the line before the line at HL (HL kept); carry if none.
line_before:
        push hl
        ex de,hl
        ld hl,PROG
        rst 0x28
        jr nc,.none             ; it is the first line
.l:     ld b,h
        ld c,l
        call next_line
        rst 0x28
        jr c,.l
        ld d,b
        ld e,c
        pop hl
        ret
.none:  pop hl
        scf
        ret
