; SPDX-License-Identifier: GPL-2.0-or-later
; Start-up, memory management, reports and character fetching.

MARGIN   equ 40             ; bytes kept free below the machine stack

; Size the RAM in 1 KiB steps (a step that does not keep a byte, or that
; echoes $4000, ends it), then NEW.
init:   ld a,font >> 8
        ld i,a
        im 1
        ld iy,0x4000
        ld hl,0x4000
.ram:   ld a,h
        add a,4
        ld h,a
        cp 0x80
        jr z,.top
        ld (iy+0),l
        ld (hl),a
        cp (hl)
        jr nz,.top              ; nothing there
        cp (iy+0)
        jr nz,.ram              ; not an echo of $4000
.top:   ld sp,hl
        ld hl,0x4000
        ld b,PROG & 0xff
        xor a
.clr:   ld (hl),a
        inc hl
        djnz .clr
        ld c,a
        push bc                 ; GOSUB_END: RETURN stops here
        ld de,report
        push de                 ; reports unwind to this slot
        ld (ERR_SP),sp
        ld (hl),0x80            ; no variables
        ld (VARS),hl
        inc hl
        ld (E_LINE),hl
        jr edit

; Make BC bytes of room at HL, moving everything from HL to DF_END up.
; Keeps BC and HL; NC. With X_DROP set a lack of room returns carry;
; otherwise it is report 4.
make_room:
        push hl
        call test_room
        pop hl
        ret c
        push bc
        push hl
        ld d,h
        ld e,l
        ld hl,(DF_END)
        push hl
        call pointers           ; pointers at or above HL move up
        pop hl                  ; the old end
        pop de
        push de
        and a
        sbc hl,de               ; bytes to move
        jr z,.none
        ld b,h
        ld c,l
        add hl,de
        dec hl
        ld de,(DF_END)
        dec de
        lddr
.none:  pop hl
        pop bc
        and a
        ret

; Carry if BC more bytes would not fit below the stack: with X_DROP set;
; otherwise it is report 4. Keeps A and BC.
test_room:
        ld hl,1 - MARGIN
        add hl,sp               ; (carry: the stack is never that low)
        ld de,(DF_END)
        sbc hl,de
        sbc hl,bc
        ret nc
        bit X_DROP,(iy+Y_FLAGX)
        scf
        ret nz
        rst 8
        db R_MEMORY

; Pointers at or above DE move by BC. Keeps BC, DE, HL.
pointers:
        push hl
        ld hl,ptr_list
.l:     ld a,(hl)
        inc hl
        or a
        jr z,.end
        push hl
        ld ixl,a
        ld ixh,0x40
        ld l,(ix+0)
        ld h,(ix+1)
        rst 0x28
        jr c,.keep
        add hl,bc
        ld (ix+0),l
        ld (ix+1),h
.keep:  pop hl
        jr .l
.end:   pop hl
        ret
ptr_list:
        db P_PTR & 0xff, VARS & 0xff, E_LINE & 0xff, D_FILE & 0xff
        db DF_EA & 0xff, DF_END & 0xff, CH_ADD & 0xff, DEST & 0xff, 0
                                ; (nothing moves the edit line while X_PTR
                                ; is in use)

; Clear temporary strings: the workspace after the edit line.
temp_clear:
        ld hl,(E_LINE)
        call line_skip
        ex de,hl
        ld hl,(D_FILE)
; Remove the bytes from DE up to HL.
reclaim_de:
        and a
        sbc hl,de
        ld b,h
        ld c,l
        ex de,hl
; Remove BC bytes at HL, moving everything above them down.
reclaim:
        ld a,b
        or c
        ret z
        push hl
        push bc
        ld d,h
        ld e,l
        inc de                  ; pointers above HL move down
        xor a
        sub c
        ld c,a
        sbc a,a
        sub b
        ld b,a
        call pointers
        pop bc
        pop de
        push de
        ld hl,(DF_END)
        and a
        sbc hl,de               ; bytes to move
        push hl
        ld h,d
        ld l,e
        add hl,bc               ; the first byte kept
        pop bc
        ld a,b
        or c
        jr z,.done
        ldir
.done:  pop hl
        ret


; HL -> the line after the line at HL.
next_line:
        inc hl
        inc hl
; HL -> the byte after the next NEWLINE.
line_skip:
        ld a,C_NL
.l:     cp (hl)
        inc hl
        jr nz,.l
        ret

; ---------------------------------------------------------------------
; Character fetching (RST 18 starts in hw.asm). Spaces are not significant
; outside strings and REM.

; Carry if A is a letter / a letter or digit.
is_letter:
        cp C_A
        jr is_alnum.c
is_alnum:
        cp C_0
.c:     ccf
        ret nc
        cp 0x40
        ret

; HL -> digits: DE = their value, HL -> after them; carry if over 32767.
parse_uint:
        ld de,0
.l:     ld a,(hl)
        sub C_0
        cp 10
        ret nc
        ld b,a
        ld a,d
        cp 0x0d
        ccf
        ret c                   ; 3328 or more: ten times it is too big
        push hl
        ld h,d
        ld l,e
        add hl,hl
        add hl,hl
        add hl,de
        add hl,hl
        ld e,b
        ld d,0
        add hl,de
        ex de,hl
        pop hl
        inc hl
        bit 7,d
        jr z,.l
        scf
        ret

; RST 30, continued: A = the wanted token, the caller's HL on the stack.
expect_2:
        ld b,a
        rst 0x18
        cp b
        jr nz,syntax_err
        call next_ch
        pop hl
        ret

; While checking, return from the caller (run_num also makes it numeric).
run_num:
        set F_NUM,(iy+Y_FLAGS)
run_only:
        bit F_CHECK,(iy+Y_FLAGS)
        ret z
        pop af
        ret

; K cursor if the cursor stands at this character (HL = CH_ADD). Keeps A.
k_here: ld de,(P_PTR)
        rst 0x28
        ret nz
        set F_KMODE,(iy+Y_FLAGS)
        ret
