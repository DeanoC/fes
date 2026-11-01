; SPDX-License-Identifier: GPL-2.0-or-later
; Screen output. With 3.25 KiB of RAM or more the display file is expanded
; (24 rows of 32 characters); below that rows hold only what was printed
; and grow on demand, as on an unexpanded ZX81. S_POSN is the print
; position; DF_CC follows it on an expanded display.

FMT      equ PRBUFF+4           ; formatted-number buffer

; Carry set when the display file is collapsed.
collapsed:
        ld a,(RAMTOP+1)
        cp 0x4d
        ret

; RST 10.
print_a:
        push hl
        push de
        push bc
        call print_char
        pop bc
        pop de
        pop hl
        ret

print_char:
        cp C_NL
        jr z,print_nl
        cp 0x40
        jr c,put_char
        cp 0x43
        jr c,print_token
        cp 0x80
        jr c,.bad
        cp 0xc0
        jr c,put_char
        jr print_token
.bad:   ld a,C_QUERY

; Store character A at the print position and advance.
put_char:
        ld c,a
        ld a,(S_POSN)
        dec a
        call z,print_nl         ; column 32: wrap
        call check_line
        jr c,.done
        bit OF_MEASURE,(iy+Y_OFLAGS)
        jr nz,.adv
        push bc
        call char_addr
        pop bc
        ld (hl),c
        inc hl
        ld (DF_CC),hl
.adv:   ld hl,S_POSN
        dec (hl)
.done:  ld a,c
        and a
        res 0,(iy+Y_FLAGS)
        ret nz
        set 0,(iy+Y_FLAGS)      ; a space suppresses the next leading space
        ret

; Carry set if the character must be dropped. Errors with report 5 when
; the upper screen is full, unless OF_CLIP is set.
check_line:
        bit OF_MEASURE,(iy+Y_OFLAGS)
        ret nz
        ld a,(S_POSN+1)
        bit OF_LOWER,(iy+Y_OFLAGS)
        jr z,.upper
        cp 1
        ret
.upper: cp (iy+Y_DF_SZ)
        jr z,.full
        ret nc
.full:  bit OF_CLIP,(iy+Y_OFLAGS)
        scf
        ret nz
        rst 8
        db R_SCREEN

print_nl:
        ld hl,S_POSN
        ld (hl),33
        inc hl
        ld a,(hl)
        and a
        ret z
        dec (hl)
        set 0,(iy+Y_FLAGS)
        ret

; PRINT AT: B = line, C = column.
print_at:
        ld a,33
        sub c
        ld (S_POSN),a
        ld a,24
        sub b
        ld (S_POSN+1),a
        call collapsed
        ret c
        call char_addr
        ld (DF_CC),hl
        ret

; HL = start of the print position's row.
row_addr:
        ld a,(S_POSN+1)
        ld b,a
        ld a,24
        sub b
        ld b,a                  ; line
        ld hl,(D_FILE)
        inc hl
        ret z
        call collapsed
        ld de,33
        jr nc,.exp
        ld a,C_NL
.skip:  cp (hl)
        inc hl
        jr nz,.skip
        djnz .skip
        ret
.exp:   add hl,de
        djnz .exp
        ret

; HL = address of the print position. A collapsed row too short for it
; grows with spaces.
char_addr:
        call row_addr
        call cur_col
        ld e,a
        ld d,0
        call collapsed
        jr c,.coll
        add hl,de
        ret
.coll:  push hl                 ; row start
        push de                 ; column
        call row_len            ; BC = length, HL -> its NEWLINE
        pop de
        ld a,e
        sub c
        jr c,.in
        inc a                   ; spaces needed: column - length + 1
        push de
        ld c,a
        ld b,0
        call make_room
.fill:  ld (hl),b
        inc hl
        dec c
        jr nz,.fill
        pop de
.in:    pop hl
        add hl,de
        ret

; HL -> row: BC = its length, HL -> its NEWLINE.
row_len:
        ld bc,0
        ld a,C_NL
.l:     cp (hl)
        ret z
        inc hl
        inc bc
        jr .l

; Character at line B column C (0 past a collapsed row's end).
read_char:
        call print_at
        call row_addr
        call cur_col
        inc a
        ld b,a
.walk:  ld a,(hl)
        cp C_NL
        jr z,.none
        inc hl
        djnz .walk
        dec hl
        ld a,(hl)
        ret
.none:  xor a
        ret

; Tokens print as words; keywords get a leading space unless one was just
; printed, and a trailing space.
print_token:
        sub 0xc0
        jr nc,.idx
        add a,0xc0-0x40+64      ; RND, INKEY$, PI follow the 64 tokens
.idx:   ld b,a
        ld c,a
        ld hl,token_text
        inc b
.find:  dec b
        jr z,.found
.skip:  ld a,(hl)
        inc hl
        rla
        jr nc,.skip
        jr .find
.found: ld a,(hl)
        call is_letter
        ld b,0
        jr nc,.go
        ld a,c
        cp 64
        jr nc,.go
        inc b                   ; a keyword
        bit 0,(iy+Y_FLAGS)
        jr nz,.go
        push hl
        push bc
        xor a
        call put_char
        pop bc
        pop hl
.go:    ld a,(hl)
        inc hl
        push af
        and 0x7f
        push hl
        push bc
        call put_char
        pop bc
        pop hl
        pop af
        rla
        jr nc,.go
        dec b
        ret nz
        xor a
        jr put_char

; HL = value, C = width: print a 16-bit number, right-aligned. (Integer
; arithmetic: LIST prints a line number for every line.)
print_uint:
        ld de,0
        ld b,d                  ; B = digits
.d:     push bc
        call div10_32
        pop bc
        add a,C_0
        push af                 ; digits pile up on the stack, last first
        inc b
        dec c
        ld a,h
        or l
        jr nz,.d
.pad:   ld a,c
        dec a
        jp m,.out               ; no width left
        ld c,a
        xor a
        rst 0x10
        jr .pad
.out:   pop af
        rst 0x10
        djnz .out
        ret

; Pop and print a number (the B characters fp_format leaves in FMT).
fp_print:
        call fp_format
        ld hl,FMT
.l:     ld a,(hl)
        inc hl
        rst 0x10
        djnz .l
        ret

; Clear C rows starting at line B: blank (expanded) or empty (collapsed).
clear_rows:
        push bc
        ld c,0
        call print_at
        call row_addr
        pop bc
.row:   push bc
        push hl
        call row_len
        pop hl
        call collapsed
        jr c,.cut
.z:     ld (hl),0
        inc hl
        dec c
        jr nz,.z
        inc hl
        jr .next
.cut:   call reclaim
        inc hl
.next:  pop bc
        dec c
        jr nz,.row
        ret

clear_lower:
        ld a,24
        sub (iy+Y_DF_SZ)
        ld b,a
        ld c,(iy+Y_DF_SZ)
        jr clear_rows

; The display file must keep its 25 NEWLINEs at every moment the NMI may
; show it: rows only gain or lose characters, and SCROLL adds the new row
; before it removes the old one.

; CLS. A collapsed display empties every row; an expanded one is (re)built
; as 24 rows of 32 spaces (it only changes size while no frames run).
cls:    call collapsed
        jr nc,.exp
        ld bc,24
        call clear_rows
        jr .home
.exp:   ld hl,(VARS)
        ld de,(D_FILE)
        and a
        sbc hl,de
        dec hl                  ; bytes after the leading NEWLINE
        ld de,24*33
        and a
        sbc hl,de
        jr z,.fill
        ld b,h
        ld c,l
        ld hl,(D_FILE)
        inc hl
        jr c,.grow
        call reclaim
        jr .fill
.grow:  xor a
        sub c
        ld c,a
        sbc a,a
        sub b
        ld b,a
        call make_room
.fill:  ld hl,(D_FILE)
        ld (hl),C_NL
        ld c,24
.r:     ld b,32
.z:     inc hl
        ld (hl),0
        djnz .z
        inc hl
        ld (hl),C_NL
        dec c
        jr nz,.r
.home:  ld (iy+Y_DF_SZ),2
        ld bc,0
        jr print_at

; SCROLL the upper screen up one line; print position to its bottom line.
scroll: ld a,24
        sub (iy+Y_DF_SZ)
        ld b,a
        ld c,0
        call print_at
        call row_addr           ; the first row below the upper screen
        ld bc,1
        call collapsed
        jr c,.one
        ld c,33
.one:   call make_room          ; a blank row there
.fz:    dec c
        jr z,.nl
        ld (hl),0
        inc hl
        jr .fz
.nl:    ld (hl),C_NL
        ld hl,(D_FILE)          ; then the top row goes
        inc hl
        push hl
        call row_len
        inc bc
        pop hl
        call reclaim
        ld a,23
        sub (iy+Y_DF_SZ)
        ld b,a
        ld c,0
        jr print_at

; HL -> the next NEWLINE byte (raw, display file).
line_end_raw:
        ld a,C_NL
.l:     cp (hl)
        ret z
        inc hl
        jr .l
