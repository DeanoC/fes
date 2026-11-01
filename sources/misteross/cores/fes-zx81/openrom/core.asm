; SPDX-License-Identifier: GPL-2.0-or-later
; Memory management, errors, and the line interpreter.

MR_A     equ PRBUFF             ; make_room / reclaim scratch
MR_B     equ PRBUFF+2
PW       equ FT+5               ; pointer-update scratch

; ---------------------------------------------------------------------
; NEW and power-on: everything below RAMTOP starts again.
new_entry:
        ld hl,(RAMTOP)
        push hl
        ld hl,0x4000
        ld de,0x4001
        ld bc,PROG-0x4001
        ld (hl),b
        ldir
        pop hl
        ld (RAMTOP),hl
        ld iy,0x4000
        ld a,0x1e
        ld i,a
        im 1
        call reset_stack
        dec (iy+Y_ERR_NR)       ; R_OK
        ld hl,0xfffe
        ld (PPC),hl
        dec hl
        ld (LAST_K),hl
        ld (MEM),hl
        ld (FRAMES),hl
        ld hl,PROG              ; CLS sets DF_SZ and DF_CC
        ld (D_FILE),hl
        ld b,25                 ; empty rows; CLS grows them in place
.nl:    ld (hl),C_NL
        inc hl
        djnz .nl
        ld (VARS),hl
        ld (hl),0x80
        inc hl
        ld (E_LINE),hl
        ld (hl),C_NL
        inc hl
        ld (WORKSP),hl
        call line_workspace
        call kscan
        call cls
        call slow
        jr edit_main

; Fresh machine stack: GOSUB end marker at RAMTOP-2, error slot below.
reset_stack:
        pop de
        ld hl,(RAMTOP)
        dec hl
        ld (hl),0x3e
        dec hl
        ld (hl),0
        ld sp,hl
        ld hl,main_report
        push hl
        ld (ERR_SP),sp
        ex de,hl
        jp (hl)

; ---------------------------------------------------------------------
; Errors. RST 8 is followed by the report code.
error_1:
        pop hl
        ld a,(hl)
        ld (iy+Y_ERR_NR),a
        ld hl,(CH_ADD)
        ld (X_PTR),hl
        ld sp,(ERR_SP)
        ret

; Show the report as code/line at the bottom. CONT goes back to the line
; that failed, or on past a STOP, a BREAK or the end.
main_report:
        dec sp                  ; keep the error slot
        dec sp
        ld iy,0x4000
        xor a
        ld (iy+Y_MODE),a
        ld (iy+Y_OFLAGS),1<<OF_LOWER
        ld hl,(PPC)
        bit 7,h
        jr z,.prog
        ld h,a                  ; a command: line 0
        ld l,a
        jr .show
.prog:  push hl
        ld a,(iy+Y_ERR_NR)
        cp R_OK
        jr z,.next
        cp R_STOP
        jr z,.next
        cp R_BREAK
        jr nz,.set
.next:  inc hl
.set:   ld (OLDPPC),hl
        pop hl
.show:  push hl
        ld (iy+Y_DF_SZ),2
        call clear_lower
        ld bc,0x1700
        call print_at
        ld a,(iy+Y_ERR_NR)
        inc a
        add a,C_0
        rst 0x10
        ld a,C_SLASH
        rst 0x10
        pop hl
        ld c,0
        call print_uint
        ld (iy+Y_OFLAGS),1<<OF_REPORT
        jr edit_main

; ---------------------------------------------------------------------
; Characters.
next_char:
        ld hl,(CH_ADD)
        jr gc_next
get_char:
        ld hl,(CH_ADD)
        dec hl
gc_next:
        inc hl
        ld a,(hl)
        and a                   ; spaces are skipped
        jr z,gc_next
        ld (CH_ADD),hl
        ret

; Carry set when A is a digit / letter / letter or digit.
is_digit:
        cp C_0
        ccf
        ret nc
        cp C_0+10
        ret
is_letter:
        cp C_A
        jr is_low
is_alnum:
        cp C_0
is_low: ccf
        ret nc
        cp 0x40
        ret

; Compare HL with D_FILE (HL kept): carry if HL is below it.
at_dfile:
        ld de,(D_FILE)
cp_hl_de:
        push hl
        and a
        sbc hl,de
        pop hl
        ret

expect_rparen:
        ld a,C_RPAREN
; Expect character A at CH_ADD and step past it.
expect: ld c,a
        rst 0x18
        cp c
        jr nz,nonsense
        rst 0x20
        ret
nonsense:
        rst 8
        db R_NONSENSE

; ---------------------------------------------------------------------
; Memory. All pointer system variables at or above an insertion point
; move with the bytes; WORKSP moves only when strictly above.
test_room:
        push hl
        push de
        ld hl,(STKEND)
        add hl,bc
        jr c,.full
        ld de,40
        add hl,de
        jr c,.full
        ex de,hl
        ld hl,0
        add hl,sp
        and a
        sbc hl,de
        jr c,.full
        pop de
        pop hl
        ret
.full:  rst 8
        db R_MEMORY

; Insert BC bytes before HL. Returns HL = gap, BC = size.
make_room:
        ld a,b
        or c
        ret z
        call test_room
        ld (MR_A),hl
        ld (MR_B),bc
        ex de,hl
        ld hl,(STKEND)
        and a
        sbc hl,de
        push hl
        ld hl,(STKEND)
        dec hl
        ld d,h
        ld e,l
        add hl,bc
        ex de,hl
        pop bc
        ld a,b
        or c
        jr z,.moved
        lddr
.moved: ld hl,(MR_A)
        ld bc,(MR_B)
mr_tail:
        call pointers
        ld hl,(MR_A)
        ld bc,(MR_B)
        ret

; Delete BC bytes at HL.
reclaim:
        ld a,b
        or c
        ret z
        ld (MR_A),hl
        ld (MR_B),bc
        add hl,bc
        ex de,hl
        ld hl,(STKEND)
        and a
        sbc hl,de
        ld b,h
        ld c,l
        ex de,hl
        ld de,(MR_A)
        ld a,b
        or c
        jr z,.moved
        ldir
.moved: ld hl,(MR_A)
        ld bc,(MR_B)
        xor a
        sub c
        ld c,a
        sbc a,a
        sub b
        ld b,a                  ; BC = -count: the pointers move down
        jr mr_tail

; RST 30: BC bytes at the end of the workspace. DE = first byte.
bc_spaces:
        ld hl,(STKBOT)
        call make_room
        ex de,hl
        ret

; Bit 7: the pointer moves only when strictly above the insertion point
; (VARS and WORKSP may sit exactly where a variable or workspace grows).
ptr_list:
        db D_FILE & 0xff, DF_CC & 0xff, VARS & 0xff | 0x80, DEST & 0xff
        db E_LINE & 0xff, CH_ADD & 0xff, X_PTR & 0xff, STKBOT & 0xff
        db STKEND & 0xff, NXTLIN & 0xff, WORKSP & 0xff | 0x80, 0

; Pointers at or above HL move by BC (signed); one that would fall below
; HL (inside a deleted block) becomes HL.
pointers:
        push hl
        push de
        push bc
        ld (PW),hl
        ld hl,ptr_list
.loop:  ld a,(hl)
        and a
        jr z,.done
        inc hl
        push hl
        ld l,a
        res 7,l
        ld h,0x40
        ld e,(hl)
        inc hl
        ld d,(hl)
        push hl
        ld hl,(PW)
        ex de,hl                ; HL = p, DE = where
        and a
        sbc hl,de
        jr c,.skip
        jr nz,.move
        rla
        jr c,.skip
.move:  add hl,bc
        bit 7,h
        jr z,.store
        ld hl,0
.store: add hl,de
        ex de,hl
        pop hl
        ld (hl),d
        dec hl
        ld (hl),e
        pop hl
        jr .loop
.skip:  pop hl
        pop hl
        jr .loop
.done:  pop bc
        pop de
        pop hl
        ret

; 16-bit multiply: HL = HL * DE (low 16 bits), carry if it overflowed.
mul16:  push bc
        ld b,h
        ld c,l
        ld hl,0
        ld a,16
.l:     add hl,hl
        jr c,.ov
        ex de,hl
        add hl,hl
        ex de,hl
        jr nc,.n
        add hl,bc
        jr c,.ov
.n:     dec a
        jr nz,.l
        pop bc
        and a
        ret
.ov:    pop bc
        scf
        ret

; ---------------------------------------------------------------------
; Program lines. A line is number (big-endian), length (little-endian,
; text and NEWLINE), text, NEWLINE.

; HL = first line numbered BC or above (D_FILE if none). Z if exact.
find_line:
        ld hl,PROG
.loop:  call at_dfile
        jr nc,.end
        ld a,(hl)
        cp b
        jr c,.next
        ret nz
        inc hl
        ld a,(hl)
        dec hl
        cp c
        jr c,.next
        ret
.next:  call next_line
        jr .loop
.end:   or 1
        ret

; HL = line -> HL = the line after it.
next_line:
        inc hl
        inc hl
        ld e,(hl)
        inc hl
        ld d,(hl)
        inc hl
        add hl,de
        ret

st_cont:
        ld bc,(OLDPPC)
        jr goto_line
st_goto:
        call expr_uint
goto_line:
        call find_line
        ld (NXTLIN),hl
        ret

; Move CH_ADD to the NEWLINE that ends the current statement's line.
to_line_end:
        ld hl,(NXTLIN)
        dec hl
        bit OF_RUN,(iy+Y_OFLAGS)
        jr nz,.set
        ld hl,(CH_ADD)
        call line_end
.set:   ld (CH_ADD),hl
        ret

; HL -> the NEWLINE of the text at HL, stepping over hidden numbers.
line_end:
        ld a,(hl)
        cp C_NL
        ret z
        inc hl
        cp C_NUMBER
        jr nz,line_end
        ld de,5
        add hl,de
        jr line_end

line_workspace:
        ld hl,(WORKSP)
        ld (STKBOT),hl
        ld (STKEND),hl
        ret

; ---------------------------------------------------------------------
; Run program lines from HL until the end, an error or a STOP.
run_next:
        ld hl,(NXTLIN)
run_loop:
        ld sp,(ERR_SP)
        set OF_RUN,(iy+Y_OFLAGS)
        call at_dfile
        jr nc,.end
        ld d,(hl)
        inc hl
        ld e,(hl)
        inc hl
        ld (PPC),de
        ld e,(hl)
        inc hl
        ld d,(hl)
        inc hl
        ld (CH_ADD),hl
        add hl,de
        ld (NXTLIN),hl
        call line_workspace
        call statement
        call break_1
        jr c,run_next
        rst 8
        db R_BREAK
.end:   rst 8
        db R_OK

; Execute the command in E_LINE.
run_direct:
        res OF_RUN,(iy+Y_OFLAGS)
        ld hl,0xfffe
        ld (PPC),hl
        inc hl
        inc hl                  ; 0: no jump yet
        ld (NXTLIN),hl
        ld hl,(E_LINE)
        ld (CH_ADD),hl
        call line_workspace
        call statement
        ld hl,(NXTLIN)
        ld a,h
        or l
        jr nz,run_loop
        rst 8
        db R_OK

; One statement at CH_ADD; it must end the line.
statement:
        rst 0x18
        cp T_LPRINT
        jr c,nonsense
        sub T_LPRINT
        add a,a
        ld e,a
        ld d,0
        ld hl,stmt_table
        add hl,de
        ld a,(hl)
        inc hl
        ld h,(hl)
        ld l,a
        ld de,stmt_end
        push de
        push hl
        rst 0x20
        ret
stmt_end:
        rst 0x18
        cp C_NL
        jr nz,nonsense
        ret

; LPRINT, LLIST and COPY do nothing: there is no printer.
stmt_table:
        dw to_line_end, to_line_end, st_stop, slow, fast, new_entry
        dw scroll, st_cont, st_dim, to_line_end, st_for, st_goto
        dw st_gosub, st_input, st_load, st_list, st_let, st_pause
        dw st_next, st_poke, st_print, st_plot, st_run, st_save
        dw st_rand, st_if, cls, st_unplot, clear_vars, st_return
        dw stmt_end
