; SPDX-License-Identifier: GPL-2.0-or-later
; Output to the display file: D_FILE holds a NEWLINE, then rows of up to 32
; characters, each ending in a NEWLINE. Output goes on the end (DF_END);
; the open row has no NEWLINE yet. Shown, the file has all 24 rows.
; S_LINE counts rows up from the bottom (23 is the top); rows below DF_SZ
; are the lower part.

; RST 10: print character, token or NEWLINE A. Keeps BC, DE, HL.
print_a:
        cp C_NL
        jr z,print_nl
        bit 6,a                 ; codes 64-127 and 192-211 print as ?
        jr z,.ch
        ld a,C_QUERY
.ch:    res F_SPACE,(iy+Y_FLAGS)

; Character A (0-63 or 128-191) at the print position. Keeps F_SPACE.
put_char:
        push hl
        push de
        push bc
        bit X_STR,(iy+Y_FLAGX)
        jr nz,.ws
        bit F_EDIT,(iy+Y_FLAGS)
        jr nz,.ed
        push af
        ld a,(iy+Y_COL)
        cp 2
        call c,next_row         ; row full or NEWLINE due
        pop bc                  ; B = the character
        jr c,.out
        bit X_MEASURE,(iy+Y_FLAGX)
        jr nz,.count
        ld a,b
        call append
        jr c,.out
.count: dec (iy+Y_COL)
.out:   pop bc
        pop de
        pop hl
        ret
.ws:    call ws_add
        jr .out
.ed:    call edit_insert
        jr .out

; NEWLINE: the next character starts a new row. A second NEWLINE leaves
; an empty row.
print_nl:
        push hl
        push de
        push bc
        ld a,(iy+Y_COL)
        or a
        call z,next_row
        ld (iy+Y_COL),0
        set F_SPACE,(iy+Y_FLAGS)
        jr put_char.out

; Close the open row and open the next. Past the bottom of its part: carry
; when dropping output, else report 5.
next_row:
        ld a,(iy+Y_LINE)
        dec a
        jp m,.full
        bit X_LOWER,(iy+Y_FLAGX)
        jr nz,.ok
        cp (iy+Y_DF_SZ)
        jr nc,.ok
.full:  set X_FULL,(iy+Y_FLAGX)
        bit X_DROP,(iy+Y_FLAGX)
        scf
        ret nz
        rst 8
        db R_SCREEN
.ok:    ld (iy+Y_LINE),a
        ld (iy+Y_COL),33
        and a
        bit X_MEASURE,(iy+Y_FLAGX)
        ret nz
        ld a,C_NL
; A on the end of the display file. No room: with X_DROP carry (and
; X_FULL), else report 4.
append: push af
        ld bc,1
        call test_room
        jr c,.no
        pop af
        ld hl,(DF_END)          ; nothing follows: only DF_END moves
        ld (hl),a
        inc hl
        ld (DF_END),hl
        ret
.no:    pop af
        set X_FULL,(iy+Y_FLAGX)
        scf
        ret

; Token A. Keywords and THEN, TO, NOT, AND, OR have a space each side; the
; leading one is left out after another keyword's space or at the start
; of a line.
print_token:
        push hl
        push bc
        sub T_QUOTE
        ld b,a
        inc b
        ld hl,tokens
        jr .find
.skip:  bit 7,(hl)
        inc hl
        jr z,.skip
.find:  djnz .skip
        ld a,(hl)
        and 0x7f
        call is_letter
        jr nc,.sym
        bit F_SPACE,(iy+Y_FLAGS)
        call z,space
        call .text
        call space
        set F_SPACE,(iy+Y_FLAGS)
        jr .done
.sym:   res F_SPACE,(iy+Y_FLAGS)
        call .text
.done:  pop bc
        pop hl
        ret
.text:  ld a,(hl)
        and 0x7f
        call put_char
        bit 7,(hl)
        inc hl
        jr z,.text
        ret
space:  xor a
        jr put_char

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

; The string at HL, up to its closing quote.
print_str:
        ld a,(hl)
        cp C_QUOTE
        ret z
        rst 0x10
        inc hl
        jr print_str

; ---------------------------------------------------------------------
; Display file layout.

; CLS: no rows yet; two rows of lower part.
cls:    ld hl,(D_FILE)
        inc hl
        ld (DF_END),hl
        ld (iy+Y_DF_SZ),2
home:   ld (iy+Y_COL),33
        ld (iy+Y_LINE),23
        ret

; Close the open row and the rows below it: the display file is whole.
; (MARGIN keeps room for these below the stack.)
close_all:
        ld b,(iy+Y_LINE)
        inc b
; B NEWLINEs on the end of the display file; HL = the new end.
fill_nl:
        ld hl,(DF_END)
.l:     ld (hl),C_NL
        inc hl
        djnz .l
        ld (DF_END),hl
        ret

; ---------------------------------------------------------------------
; Listing.

; The program line at HL: number right-aligned in four columns, '>' in
; inverse after the current line's number, then the text. HL -> next line.
list_line:
        call list_number
        push hl
        ld hl,(E_PPC)
        and a
        sbc hl,bc
        ld a,C_SPACE
        jr nz,.mark
        ld a,C_GT + 0x80
.mark:  call put_char
        set F_SPACE,(iy+Y_FLAGS)
        pop hl
.text:  ld a,(hl)
        inc hl
        cp C_NL
        jr z,print_nl
        rst 0x10
        jr .text

; The number of the line at HL in four columns; BC = it, HL -> its text.
list_number:
        ld b,(hl)
        inc hl
        ld c,(hl)
        inc hl
        push hl
        push bc
        ld h,b
        ld l,c
        ld de,1000
        call .pad
        ld de,100
        call .pad
        ld de,10
        call .pad
        call print_uint
        pop bc
        pop hl
        ret
.pad:   rst 0x28
        ret nc
        jr space

; List from S_TOP to the bottom of the upper part. DEST = the last line
; shown whole (0 if none).
list_prog:
        ld hl,0
        ld (DEST),hl
        res X_FULL,(iy+Y_FLAGX)
        ld bc,(S_TOP)
        call find_line
.l:     ld de,(VARS)
        rst 0x28
        ret nc
        push hl
        call list_line
        pop de
        bit X_FULL,(iy+Y_FLAGX)
        ret nz
        ex de,hl
        call peek_be
        ld (DEST),hl
        ex de,hl
        jr .l

; HL = the line number (high byte first) at HL.
peek_be:
        ld a,(hl)
        inc hl
        ld l,(hl)
        ld h,a
        ret
