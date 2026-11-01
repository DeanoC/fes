; SPDX-License-Identifier: GPL-2.0-or-later
; The line editor and the command loop. E_LINE holds the line with the
; cursor byte 0x7F at the insertion point.

TXT_OFF  equ AE_OFF
TXT_LEN  equ AE_DIMS

edit_main:
        call edit_reset
        call edit_line
        res OF_REPORT,(iy+Y_OFLAGS)
        ld hl,(E_LINE)
        ld (CH_ADD),hl
        rst 0x18
        cp C_NL
        jr z,.empty
        call is_digit
        jr c,.prog
        xor a
        call tokenize
        call cls
        jr run_direct
.prog:  ld a,(CDFLAG)            ; program lines move the display file:
        push af                 ; no frames meanwhile
        call fast
        call enter_line
        pop af
        call tape_mode
.empty: call auto_list
        jr edit_main

; E_LINE = cursor, NEWLINE; workspace and calculator stack empty.
edit_reset:
        ld hl,(E_LINE)
        ld de,(STKEND)
        ex de,hl
        and a
        sbc hl,de
        ld b,h
        ld c,l
        ex de,hl
        call reclaim
        ld bc,2
        call test_room
        ld hl,(E_LINE)
        ld (hl),C_CURSOR
        inc hl
        ld (hl),C_NL
        inc hl
        ld (WORKSP),hl
        ld (iy+Y_MODE),0
        jr line_workspace

; Edit until NEWLINE; the cursor is removed on return.
edit_line:
.draw:  bit OF_REPORT,(iy+Y_OFLAGS)
        call z,edit_draw
.key:   call get_key
        res OF_REPORT,(iy+Y_OFLAGS)
        cp C_NL
        jr z,.enter
        cp K_RUBOUT
        jr z,.rubout
        cp K_LEFT
        jr z,.left
        cp K_RIGHT
        jr z,.right
        cp K_GRAPH
        jr z,.graph
        cp K_FUNC
        jr z,.func
        bit OF_INPUT,(iy+Y_OFLAGS)
        jr nz,.ins
        cp K_EDIT
        jr z,.edit
.ins:   cp 0x70
        jr c,.put
        cp 0x80
        jr c,.draw
.put:   call edit_insert
        jr .draw
.enter: call cursor_addr
        ld bc,1
        jr reclaim
.rubout:
        call cursor_start
        jr z,.draw
        dec hl
        ld bc,1
        call reclaim
        jr .draw
.left:  call cursor_start
        jr z,.draw
        dec hl
        ld a,(hl)
        ld (hl),C_CURSOR
        inc hl
        ld (hl),a
        jr .draw
.right: call cursor_addr
        inc hl
        ld a,(hl)
        cp C_NL
        jr z,.draw
        ld (hl),C_CURSOR
        dec hl
        ld (hl),a
        jr .draw
.graph: ld a,(iy+Y_MODE)
        xor 2
        and 2
        ld (iy+Y_MODE),a
        jr .draw
.func:  ld (iy+Y_MODE),1
        jr .draw
.edit:  call edit_copy
        jr .draw

; HL -> the cursor byte; Z if it starts the line.
cursor_start:
        call cursor_addr
        ld de,(E_LINE)
        jr cp_hl_de

; HL -> the cursor byte.
cursor_addr:
        ld hl,(E_LINE)
        ld a,C_CURSOR
.l:     cp (hl)
        ret z
        inc hl
        jr .l

; Insert A before the cursor. Room is made after the cursor so E_LINE
; itself never moves.
edit_insert:
        push af
        call cursor_addr
        inc hl
        ld bc,1
        call make_room
        ld (hl),C_CURSOR
        pop af
        dec hl
        ld (hl),a
        ret

; Insert A before E_LINE's NEWLINE.
edit_append:
        push af
        ld hl,(E_LINE)
        call line_end_raw
        ld bc,1
        call make_room
        pop af
        ld (hl),a
        ret

; Carry when the cursor shows K: only a line number (and spaces) before
; it, or THEN just before it. INPUT never has K.
k_mode: and a
        bit OF_INPUT,(iy+Y_OFLAGS)
        ret nz
        ld hl,(E_LINE)
.lead:  ld a,(hl)
        cp C_CURSOR
        jr z,.k
        and a
        jr z,.ln
        call is_digit
        jr nc,.body
.ln:    inc hl
        jr .lead
.body:  ld c,0
.b:     ld a,(hl)
        cp C_CURSOR
        jr z,.end
        and a
        jr z,.sp
        ld c,a
.sp:    inc hl
        jr .b
.end:   ld a,c
        cp T_THEN
        jr z,.k
        and a
        ret
.k:     scf
        ret

; Wait for a key press (with auto-repeat) and return its code for the
; current cursor mode. The five key tables sit 39 bytes apart.
get_key:
        call get_key_raw
        call decode             ; HL -> key_plain or key_shift entry
        jr c,get_key
        ld b,0
        ld a,l
        cp key_shift & 0xff
        jr c,.u
        sub 39
        ld l,a
        inc b                   ; B = 1: SHIFT held
.u:     ld a,(iy+Y_MODE)
        dec a
        jr z,.func
        dec a
        jr z,.graph
        dec b
        jr z,.shift
        push hl
        call k_mode
        pop hl
        jr nc,.plain
        ld de,key_word-key_plain
        call .at
        and a
        ret nz
.plain: ld a,(hl)
        ret
.shift: ld de,key_shift-key_plain
.at:    push hl
        add hl,de
        ld a,(hl)
        pop hl
        ret
.func:  ld (iy+Y_MODE),a
        ld de,key_func-key_plain
        call .at
        cp 0x78
        jr z,get_key
        ret
.graph: dec b
        jr nz,.ginv
        ld de,key_graph-key_plain
        call .at
        and a
        ret nz
        jr .shift
.ginv:  ld a,(hl)
        cp 0x40
        ret nc
        or 0x80
        ret

; BC = a new key-down state from the frame keyboard reads. A held key
; repeats after 24 frames, then every 4.
get_key_raw:
        call wait_frame
        ld bc,(LAST_K)
        ld hl,(MEM)
        ld (MEM),bc
        ld a,c
        inc a
        jr z,get_key_raw        ; no key down
        and a
        sbc hl,bc
        ld a,24
        jr nz,.new
        dec (iy+DEBOUNCE-0x4000)
        jr nz,get_key_raw
        ld a,4
.new:   ld (iy+DEBOUNCE-0x4000),a
        ret

; Draw E_LINE at the bottom of the screen, growing the lower part as the
; line needs.
edit_draw:
        set OF_LOWER,(iy+Y_OFLAGS)
        set OF_MEASURE,(iy+Y_OFLAGS)
        ld bc,0
        call print_at
        call print_eline
        res OF_MEASURE,(iy+Y_OFLAGS)
        ld a,(S_POSN+1)
        ld b,a
        ld a,25
        sub b                   ; rows used (at least one)
        ld b,a
        inc a
        cp 25
        jr c,.sz2
        ld a,24
.sz2:   ld (iy+Y_DF_SZ),a
        push bc
        call clear_lower
        pop bc
        ld a,24
        sub b
        ld b,a
        ld c,0
        call print_at
        call print_eline
        res OF_LOWER,(iy+Y_OFLAGS)
        ret

; E_LINE moves when a collapsed display row grows: walk it through X_PTR.
; (INPUT keeps its target on the stack meanwhile.)
print_eline:
        set 0,(iy+Y_FLAGS)
        ld hl,(E_LINE)
.l:     ld a,(hl)
        cp C_NL
        ret z
        inc hl
        ld (X_PTR),hl
        cp C_NUMBER
        jr z,.num
        cp C_CURSOR
        jr nz,.ch
        call cursor_char
        call put_char
        jr .next
.ch:    rst 0x10
.next:  ld hl,(X_PTR)
        jr .l
.num:   ld de,5
        add hl,de
        jr .l

cursor_char:
        ld c,(iy+Y_MODE)
        ld a,C_F+0x80
        dec c
        ret z
        ld a,C_G+0x80
        dec c
        ret z
        call k_mode
        ld a,C_K+0x80
        ret c
        inc a
        ret

; ---------------------------------------------------------------------
; Store E_LINE as a program line (no text: delete the line).
enter_line:
        ld hl,(E_LINE)
        ld a,1
        call tokenize
        ld hl,(E_LINE)
        call dec_to_fp          ; the line number (left as plain digits)
        push hl
        call get_uint_pop
        pop hl
        ld a,b
        or c
        jr z,nonsense
        ld a,b                  ; 1 to 10239: below the GOSUB marker
        cp 0x28
        jr nc,nonsense
        ld (E_PPC),bc
        ld (CH_ADD),hl
        rst 0x18                ; past spaces
        cp C_NL
        push af                 ; Z: no text, delete the line
        ld de,(E_LINE)
        and a
        sbc hl,de
        ld (TXT_OFF),hl
        add hl,de
        push hl
        call line_end
        pop de
        and a
        sbc hl,de
        inc hl
        ld (TXT_LEN),hl
        ld bc,(E_PPC)
        call find_line
        jr nz,.noold
        push hl
        call next_line
        pop de
        and a
        sbc hl,de
        ld b,h
        ld c,l
        ex de,hl
        call reclaim
.noold: pop af
        ret z
        ld bc,(E_PPC)
        call find_line
        ld bc,(TXT_LEN)
        inc bc
        inc bc
        inc bc
        inc bc
        call make_room
        ld bc,(E_PPC)
        ld (hl),b
        inc hl
        ld (hl),c
        inc hl
        ld bc,(TXT_LEN)
        ld (hl),c
        inc hl
        ld (hl),b
        inc hl
        ex de,hl
        ld hl,(E_LINE)
        push bc
        ld bc,(TXT_OFF)
        add hl,bc
        pop bc
        ldir
        ret

; Clear the screen and list the program around the current line.
auto_list:
        ld hl,(E_PPC)
        ld de,(S_TOP)
        and a
        sbc hl,de
        jr nc,.top
        add hl,de
        ld (S_TOP),hl
.top:   call .list
        call at_dfile
        ret nc
        ld b,(hl)
        inc hl
        ld c,(hl)
        ld hl,(E_PPC)
        and a
        sbc hl,bc
        ret c
        add hl,bc
        ld (S_TOP),hl
.list:  ld bc,(S_TOP)
        jr list_at

; EDIT: bring the current line down, cursor after its number.
edit_copy:
        ld bc,(E_PPC)
        call find_line
        ret nz
        push hl
        call edit_reset
        pop hl
        ld b,(hl)
        inc hl
        ld c,(hl)
        inc hl
        push hl
        call fp_from_bc
        call fp_format
        ld hl,FMT
.d:     ld a,(hl)
        inc hl
        push hl
        push bc
        call edit_insert
        pop bc
        pop hl
        djnz .d
        pop hl
        ld c,(hl)
        inc hl
        ld b,(hl)
        inc hl
        dec bc
.t:     ld a,b
        or c
        ret z
        ld a,(hl)
        inc hl
        dec bc
        cp C_NUMBER
        jr nz,.app
        ld de,5
        add hl,de
        dec bc
        dec bc
        dec bc
        dec bc
        dec bc
        jr .t
.app:   push hl
        push bc
        call edit_append
        pop bc
        pop hl
        jr .t
