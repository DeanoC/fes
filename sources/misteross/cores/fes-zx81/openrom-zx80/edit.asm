; SPDX-License-Identifier: GPL-2.0-or-later
; The editor and the command loop. E_LINE holds the line being typed;
; P_PTR is the cursor, where the next key goes.

edit:   ld sp,(ERR_SP)
        ld hl,0x00ff
        ld (ERR_NR),hl          ; no report; FLAGS clear
        ld (iy+Y_FLAGX),h
        call eline_reset
        call edit_line
        ld hl,(E_LINE)
        ld (CH_ADD),hl
        rst 0x18
        call is_digit
        jr nc,run_cmd
        call parse_uint
        ld (CH_ADD),hl
        ld a,d
        or e
        jr z,run_cmd            ; line 0 is a command
        call enter_line
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
.draw:  call check_eline
        call display
.key:   call wait_key
        call decode_key
        cp C_NL
        jr z,.enter
        cp K_UP
        jr c,.ins
        cp K_RUBOUT + 1
        jr nc,.ins
        ld hl,.draw
        push hl
        sub K_UP
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
        call peek_be
.set:   ld (E_PPC),hl
        ret
.find:  ld bc,(E_PPC)
        jr find_line
.ins:   call edit_insert
        jr .draw
.enter: ld hl,(X_PTR)
        ld a,h
        or l
        ret z
        jr .key                 ; not yet a statement

; Insert A at the cursor (room is made after it, so E_LINE itself never
; moves). No room: nothing happens.
edit_insert:
        push af
        ld hl,(P_PTR)
        inc hl
        ld bc,1
        call room_drop
        jr c,.no
        dec hl
        ld a,(hl)
        inc hl
        ld (hl),a
        dec hl
        pop af
        ld (hl),a
        inc hl
        ld (P_PTR),hl
        ret
.no:    pop af
        ret

; make_room, but carry instead of a report when there is no room.
room_drop:
        set X_DROP,(iy+Y_FLAGX)
        call make_room
        res X_DROP,(iy+Y_FLAGX)
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
        push bc
        ld hl,(P_PTR)
        call room_drop
        pop bc
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
        ld hl,key_shift - 40
        cp 40
        jr nc,.t
        ld hl,key_plain
        add hl,de
        ld a,(hl)
        bit F_KMODE,(iy+Y_FLAGS)
        ret z
        call is_letter          ; K cursor: letters type keywords, in order
        ret nc                  ; from LIST at A
        add a,T_LIST - C_A
        ret
.t:     add hl,de
        ld a,(hl)
        ret

; ---------------------------------------------------------------------
; Check the edit line: X_PTR = the first symbol that cannot be part of a
; statement (0 if none); F_KMODE if the cursor stands where a keyword goes.
check_eline:
        ld hl,(ERR_SP)
        push hl
        ld hl,.fail
        push hl
        ld (ERR_SP),sp
        ld hl,0
        ld (X_PTR),hl
        res F_KMODE,(iy+Y_FLAGS)
        set F_CHECK,(iy+Y_FLAGS)
        ld hl,(E_LINE)
        ld (CH_ADD),hl
        bit F_INPUT,(iy+Y_FLAGS)
        jr nz,.input
        rst 0x18
        call k_here
        call is_digit
        jr nc,.stmt
        call parse_uint         ; a line number, 9999 at most
        jr c,syntax_err
        push hl
        ld hl,-10000
        add hl,de
        pop hl
        jr c,syntax_err
        ld (CH_ADD),hl
        rst 0x18
        call k_here
.stmt:  cp C_NL                 ; empty, or a number alone (deletes it)
        call nz,statement
        jr .ok
.input: call expr
        ld a,(FLAGS)
        and (1 << F_NUM) | (1 << F_DNUM)
        jp po,syntax_err        ; not the type INPUT wants
        rst 0x20
        ret
.ok:    pop hl
.fail:  pop hl
        ld (ERR_SP),hl
        res F_CHECK,(iy+Y_FLAGS)
        ret

; The screen while editing: for INPUT the program's output stays and the
; line goes in the lower part; otherwise the listing above, the edit line
; at the bottom. The edit line takes the RAM it needs first.
display:
        bit F_INPUT,(iy+Y_FLAGS)
        jr nz,input_display
edit_display:
        ld (iy+Y_FLAGX),(1 << X_LOWER) | (1 << X_MEASURE) | (1 << X_DROP)
        call cls
        call print_eline        ; count its rows
        ld a,25
        sub (iy+Y_LINE)         ; rows + 1 for the blank row above
        cp 25
        jr c,.sz
        ld a,24
.sz:    ld (iy+Y_DF_SZ),a
        ld (iy+Y_FLAGX),1 << X_DROP
        call home
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
        ld (iy+Y_LINE),a
        ld (iy+Y_COL),33
        set X_LOWER,(iy+Y_FLAGX)
        call print_eline
        call close_all
        ld (iy+Y_FLAGX),0
        call target             ; the current line (or the one before)
        ret c
        ld hl,(DEST)
        and a
        sbc hl,bc
        jr c,.move              ; below the listing
        ld hl,(S_TOP)
        dec hl
        and a
        sbc hl,bc
        ret c                   ; shown
.move:  ld hl,(S_TOP)           ; elsewhere: it starts the listing
        and a
        sbc hl,bc
        ret z                   ; too long to show whole
        ld (S_TOP),bc
        jr edit_display

; BC = the current line's number, or the one before it; carry if neither.
target: ld bc,(E_PPC)
        call find_line
        jr z,.got
        call line_before
        ret c
        ex de,hl
.got:   ld b,(hl)
        inc hl
        ld c,(hl)
        and a
        ret

; INPUT: the program's output (up to DF_EA) stays; a blank row, then the
; line, follow it.
input_display:
        ld de,(DF_EA)
        ld hl,(DF_END)
        call reclaim_de
        ld hl,(S_POSN)
        push hl
        ld (iy+Y_FLAGX),(1 << X_LOWER) | (1 << X_DROP)
        call next_row           ; close the output's row
        call next_row           ; and a blank one
        call print_eline
        call close_all
        pop hl
        ld (S_POSN),hl
        ld (iy+Y_FLAGX),0
        ret

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

; Store the edit line as line DE; CH_ADD -> the text after its number
; (CH_ADD moves with the edit line). No text deletes the line.
enter_line:
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
        ret z
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
        ret

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
        and a
        ret
.none:  pop hl
        scf
        ret
