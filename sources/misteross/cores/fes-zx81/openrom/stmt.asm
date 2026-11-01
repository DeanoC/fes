; SPDX-License-Identifier: GPL-2.0-or-later
; BASIC statements. Each handler starts with CH_ADD after its keyword and
; leaves CH_ADD at the line's NEWLINE.

; Assignment target (parse_target) -> assign.
TG_KIND  equ T_ADDR             ; 1 number, 2 new number, 3 string, 4 fixed
TG_LEN   equ T_ADDR+1           ; name length

st_stop:
        rst 8
        db R_STOP

; Optional number (0 when absent) as BC.
opt_uint:
        ld bc,0
        rst 0x18
        cp C_NL
        ret z
        jr expr_uint

expect_comma:
        ld a,C_COMMA
        jr expect

; A = column of the print position.
cur_col:
        ld a,(S_POSN)
        ld b,a
        ld a,33
        sub b
        ret

; BC bytes for a new entry at the end of VARS: HL -> it, A = the name's
; letter (X_PTR) & 0x1F.
var_room:
        ld hl,(E_LINE)
        dec hl
        call make_room
        ld de,(X_PTR)
        ld a,(de)
        and 0x1f
        ret

; Delete the variable entry at HL.
var_delete:
        push hl
        call var_skip
        pop de
        and a
        sbc hl,de
        ld b,h
        ld c,l
        ex de,hl
        jr reclaim


; GOSUB entries sit above the error slot. The line loop starts every line
; with SP at ERR_SP, so GOSUB and RETURN rebuild the stack from there and
; carry on with the loop.
st_gosub:
        call expr_uint
        ld hl,(PPC)
        inc hl                  ; RETURN goes to the line after
        ld sp,(ERR_SP)
        pop de
        push hl
        push de
        ld (ERR_SP),sp
        call goto_line
        jr run_next

st_return:
        ld sp,(ERR_SP)
        pop de
        pop bc
        ld a,b
        cp 0x3e                 ; the end marker: no GOSUB
        jr z,.none
        push de
        ld (ERR_SP),sp
        call goto_line
        jr run_next
.none:  rst 8
        db R_RETURN

st_run: call opt_uint
        push bc
        call clear_vars
        call cls
        pop bc
        call reset_stack
        call find_line
        jr run_loop

clear_vars:
        ld hl,(E_LINE)
        ld de,(VARS)
        and a
        sbc hl,de
        dec hl
        ld b,h
        ld c,l
        ex de,hl
        jr reclaim

st_rand:
        call opt_uint
        ld a,b
        or c
        jr nz,.s
        ld bc,(FRAMES)
.s:     ld (SEED),bc
        ret

st_pause:
        call expr_uint
        ld a,b
        or c
        ret z
.l:     push bc
        call wait_frame
        pop bc
        ld a,(LAST_K)
        inc a
        ret nz
        bit 7,b
        jr nz,.l
        dec bc
        ld a,b
        or c
        jr nz,.l
        ret

st_poke:
        call expr_uint
        push bc
        call expect_comma
        call expr_byte          ; 0 to 255
        pop bc
        ld (bc),a
        ret

range_error:
        rst 8
        db R_RANGE

st_if:  call expr_num
        ld a,T_THEN
        call expect
        call fp_pop_zero
        jr z,to_line_end
        jr statement

; ---------------------------------------------------------------------
st_print:
        rst 0x18
        cp C_NL
        jr z,.nl
.item:  cp C_SEMI
        jr z,.next
        cp C_COMMA
        jr z,.comma
        cp T_AT
        jr z,.at
        cp T_TAB
        jr z,.tab
        call expr
        call print_value
.after: rst 0x18
        cp C_NL
        jr z,.nl
        cp C_SEMI
        jr z,.next
        cp C_COMMA
        jr nz,nonsense
.comma: ld a,(S_POSN)           ; to column 16, or the next line
        cp 18
        jr nc,.cp
        ld a,C_NL
        rst 0x10
        jr .next
.cp:    ld a,(S_POSN)
        cp 17
        jr z,.next
        xor a
        rst 0x10
        jr .cp
.next:  rst 0x20
        cp C_NL
        jr nz,.item
        ret
.nl:    ld a,C_NL
        rst 0x10
        ret
.at:    rst 0x20
        call expr_byte
        push af
        call expect_comma
        call expr_byte
        pop bc
        cp 32
        jr nc,range_error
        ld c,a
        ld a,23
        sub (iy+Y_DF_SZ)
        cp b
        jr c,range_error
        call print_at
        jr .after
.tab:   rst 0x20
        call expr_byte
        and 0x1f
        ld c,a
        call cur_col
        cp c
        jr z,.after
        ld a,C_NL
        jr nc,.tpad
.tsp:   call cur_col
        cp c
        jr z,.after
        xor a
.tpad:  rst 0x10
        jr .tsp

; Print the value on the stack.
print_value:
        bit 6,(iy+Y_FLAGS)
        jr nz,fp_print
        call stk_pop_str        ; X_PTR follows the text if a collapsed
        ld (X_PTR),de           ; display row grows under it
.s:     ld a,b
        or c
        ret z
        ld hl,(X_PTR)
        ld a,(hl)
        inc hl
        ld (X_PTR),hl
        rst 0x10
        dec bc
        jr .s

; ---------------------------------------------------------------------
st_plot:
        ld a,1
        jr plot_common
st_unplot:
        xor a
plot_common:
        push af
        call expr_byte
        cp 64
        jr nc,range_error
        push af
        call expect_comma
        call expr_byte
        cp 44
        jr nc,range_error
        ld d,a                  ; D = y, E = x
        pop af
        ld e,a
        ld (COORDS),de
        ld a,d
        srl a
        ld b,a
        ld a,21
        sub b
        ld b,a                  ; row
        ld a,e
        srl a
        ld c,a                  ; column
        ld a,1                  ; quadrant: TL 1, TR 2, BL 4, BR 8
        bit 0,e
        jr z,.l
        add a,a
.l:     bit 0,d
        jr nz,.t
        add a,a
        add a,a
.t:     ld (AE_N),a
        push bc
        call read_char
        ld c,0                  ; quadrants of the existing character
        cp 8
        jr c,.q
        sub 0x80
        cp 8
        jr nc,.q0
        xor 0x0f                ; 128-135 are the complements of 0-7
.q:     ld c,a
.q0:    ld a,(AE_N)
        ld b,a
        pop hl                  ; row / column
        pop af                  ; PLOT or UNPLOT
        and a
        ld a,b
        jr z,.un
        or c
        jr .char
.un:    cpl
        and c
.char:  bit 3,a
        jr z,.store
        xor 0x8f
.store: push af
        ld b,h
        ld c,l
        call print_at
        pop af
        jr put_char

; ---------------------------------------------------------------------
st_list:
        call opt_uint
        ld (E_PPC),bc
; CLS, then list from the first line numbered BC or above. HL -> the first
; line that did not fit.
list_at:
        push bc
        call cls
        pop bc
        call find_line
        set OF_CLIP,(iy+Y_OFLAGS)
        call list_from
        res OF_CLIP,(iy+Y_OFLAGS)
        ret

; List lines from HL until the upper screen is full. HL -> first unlisted.
list_from:
        call at_dfile
        ret nc
        ld a,(S_POSN+1)
        cp (iy+Y_DF_SZ)
        ret z
        ret c
        call list_line
        jr list_from

; Print the line at HL. HL -> next line.
list_line:
        ld b,(hl)
        inc hl
        ld c,(hl)
        inc hl
        push hl
        push bc
        ld h,b
        ld l,c
        ld c,4
        call print_uint
        pop bc
        ld hl,(E_PPC)
        and a
        sbc hl,bc
        jr nz,.text
        ld a,C_GT               ; the current line
        rst 0x10
        set 0,(iy+Y_FLAGS)
.text:  pop hl
        ld c,(hl)
        inc hl
        ld b,(hl)
        inc hl
        dec bc
.ch:    ld a,b
        or c
        jr z,.end
        ld a,(hl)
        inc hl
        dec bc
        cp C_NUMBER
        jr nz,.out
        ld de,5
        add hl,de
        dec bc
        dec bc
        dec bc
        dec bc
        dec bc
        jr .ch
.out:   rst 0x10
        jr .ch
.end:   inc hl
        ld a,C_NL
        rst 0x10
        ret

; ---------------------------------------------------------------------
st_let: call parse_target
        call target_save
        ld a,C_EQ
        call expect
        call expr
        call target_restore
        jr assign

; Parse a variable to assign (see op_variable).
parse_target:
        rst 0x18
        call is_letter
        jr nc,nonsense
        set OF_LVAL,(iy+Y_OFLAGS)
        jr op_variable

; Keep the assignment target on the machine stack while a value is
; evaluated (variables in it reuse these system variables).
target_save:
        pop hl
        ld de,(DEST)
        push de
        ld de,(X_PTR)
        push de
        ld de,(STRLEN)
        push de
        ld de,(T_ADDR)
        push de
        jp (hl)
target_restore:
        pop hl
        pop de
        ld (T_ADDR),de
        pop de
        ld (STRLEN),de
        pop de
        ld (X_PTR),de
        pop de
        ld (DEST),de
        jp (hl)

; Store the value on the stack in the parse_target destination.
assign: ld a,(TG_KIND)
        cp 3
        jr nc,.str
        call need_num
        cp 2
        ld de,(DEST)
        jr nz,.copy5
        ld a,(TG_LEN)           ; a new number: name, then the value
        add a,5
        ld c,a
        ld b,0
        call var_room
        ld b,(iy+TG_LEN-0x4000)
        djnz .long
        or 0x60
        jr .hdr
.long:  or 0xa0
.lc:    ld (hl),a
        inc hl
        inc de
        ld a,(de)
        djnz .lc
        or 0x80                 ; last character of a long name
.hdr:   ld (hl),a
        inc hl
        ex de,hl
.copy5: push de
        call stk_drop
        pop de
        ld bc,5
        ldir
        ret
.str:   call need_str
        cp 4
        jr z,.fixed
        call stk_pop_str
        ld (AE_OFF),de
        ld (AE_DIMS),bc
        ld hl,3
        add hl,bc
        ld b,h
        ld c,l
        call var_room
        push af
        ex de,hl                ; DE = gap
        ld hl,(AE_OFF)          ; the source moved if it was above the gap
        and a
        sbc hl,de
        jr c,.src
        ld hl,(AE_OFF)
        add hl,bc
        ld (AE_OFF),hl
.src:   ex de,hl
        pop af
        or 0x40
        ld (hl),a
        inc hl
        ld bc,(AE_DIMS)
        ld (hl),c
        inc hl
        ld (hl),b
        inc hl
        ex de,hl
        ld hl,(AE_OFF)
        ld a,b
        or c
        jr z,.old
        ldir
.old:   ld hl,(DEST)
        ld a,h
        or l
        ret z
        inc hl
        ld c,(hl)
        inc hl
        ld b,(hl)
        inc bc
        inc bc
        inc bc
        ld hl,(DEST)
        jr reclaim
.fixed: call stk_pop_str
        ld (AE_OFF),de
        ld hl,(STRLEN)
        ld de,(DEST)
.cp:    ld a,h
        or l
        ret z
        ld a,b
        or c
        jr z,.pad
        push hl
        ld hl,(AE_OFF)
        ld a,(hl)
        inc hl
        ld (AE_OFF),hl
        pop hl
        dec bc
        jr .st
.pad:   xor a
.st:    ld (de),a
        inc de
        dec hl
        jr .cp

; ---------------------------------------------------------------------
; FOR entry: letter | 0xE0, value, limit, step, line (18 bytes). The name
; is held on the stack: variables in the expressions reuse X_PTR.
st_for: call one_letter
        push de
        ld a,C_EQ
        call expect
        call expr_num
        ld a,T_TO
        call expect
        call expr_num
        rst 0x18
        cp T_STEP
        jr nz,.one
        rst 0x20
        call expr_num
        jr .have
.one:   ld hl,const_one
        call stk_push
.have:  pop de
        ld (X_PTR),de
        ld bc,0x0100
        call find_var
        call z,var_delete
        ld bc,18
        call var_room
        or 0xe0
        ld (hl),a
        inc hl
        push hl
        ld hl,(STKEND)
        ld de,-15
        add hl,de
        ld (STKEND),hl
        pop de
        ld bc,15
        ldir
        ld hl,(PPC)
        ex de,hl
        ld (hl),e
        inc hl
        ld (hl),d
        ret

; A one-letter variable name at CH_ADD: DE -> it.
one_letter:
        rst 0x18
        call is_letter
        jr nc,nonsense
        call scan_name
        dec b
        ret z
        jr nonsense

; HL -> FOR entry. Carry if the value has passed the limit.
for_test:
        inc hl
        call stk_push           ; value
        call stk_push           ; limit
        inc hl
        ld a,(hl)               ; sign of the step
        push af
        call fp_compare
        pop bc
        jr z,.go
        bit 7,b
        ret nz                  ; counting down: done below the limit
        ccf                     ; counting up: done above it
        ret
.go:    and a
        ret

st_next:
        call one_letter
        ld bc,0x0100
        call find_var
        jr nz,.none
        ld a,(hl)
        and 0xe0
        cp 0xe0
        jr z,.found
.none:  rst 8
        db R_NEXT
.found: ld (DEST),hl
        inc hl
        push hl
        call stk_push           ; value
        ld de,5
        add hl,de
        call stk_push           ; step
        call fp_add
        call stk_drop
        pop de
        ld bc,5
        ldir
        ld hl,(DEST)
        call for_test
        ret c
        ld hl,(DEST)
        ld de,16
        add hl,de
        ld c,(hl)
        inc hl
        ld b,(hl)
        inc bc
        jr goto_line

; ---------------------------------------------------------------------
; DIM: numbers start at zero, strings at spaces (also zero bytes). The
; dimensions collect on the machine stack, first one on top.
; Numeric arrays only for now.
st_dim: call one_letter
        push de
        cp C_LPAREN
        jr nz,nonsense
        call subscripts
        ld (iy+TG_LEN-0x4000),a
        ld (AE_N),a
        pop de
        ld (X_PTR),de
        ld bc,0x0102            ; a number array of that name
        call find_var
        call z,var_delete
        ld hl,1
        ld (AE_OFF),hl
.p:     call get_uint_pop
        ld a,b
        or c
        jr z,subscript_error
        push bc
        ld d,b
        ld e,c
        ld hl,(AE_OFF)
        call mul16
        jr c,.mem
        ld (AE_OFF),hl
        ld hl,AE_N
        dec (hl)
        jr nz,.p
        ld hl,(AE_OFF)
        ld d,h
        ld e,l
        add hl,hl
        jr c,.mem
        add hl,hl
        jr c,.mem
        add hl,de
        jr c,.mem
        ld a,(TG_LEN)
        ld e,a
        ld d,0
        add hl,de
        add hl,de
        inc hl
        jr c,.mem
        ld (AE_DIMS),hl         ; length field
        inc hl
        inc hl
        inc hl
        ld b,h
        ld c,l
        call var_room
        ld e,a
        push hl
        push de
        ld d,h
        ld e,l
        inc de
        dec bc
        ld (hl),0
        ldir
        pop de
        pop hl
        ld a,e
        or 0x80
        ld (hl),a
        inc hl
        ld de,(AE_DIMS)
        ld (hl),e
        inc hl
        ld (hl),d
        inc hl
        ld a,(TG_LEN)
        ld (hl),a
        ld b,a
.w:     inc hl
        pop de
        ld (hl),e
        inc hl
        ld (hl),d
        djnz .w
        ret
.mem:   rst 8
        db R_MEMORY

; ---------------------------------------------------------------------
st_input:
        bit OF_RUN,(iy+Y_OFLAGS)
        jr nz,.ok
        rst 8
        db R_INPUT
.ok:    call parse_target
        call target_save
        ld hl,(CH_ADD)
        push hl
        ld hl,(S_POSN)          ; the upper screen keeps its print position
        push hl
        set OF_INPUT,(iy+Y_OFLAGS)
        call edit_reset
        call edit_line
        res OF_INPUT,(iy+Y_OFLAGS)
        call clear_lower
        ld (iy+Y_DF_SZ),2
        pop hl
        ld (S_POSN),hl
        ld hl,(E_LINE)          ; the text typed, as a string
        push hl
        call line_end_raw
        pop de
        and a
        sbc hl,de
        ld b,h
        ld c,l
        call stk_push_str
        ld a,(TG_KIND)
        cp 3
        call c,fn_val           ; a number: evaluate it like VAL
        pop hl
        ld (CH_ADD),hl
        call target_restore
        jr assign

; ---------------------------------------------------------------------
; SAVE and LOAD go through the core's tape traps. The name is accepted
; and ignored: the core saves to, and loads from, the mounted tape.
; Ignore an optional name, remember the display mode and go FAST.
tape_start:
        rst 0x18
        cp C_NL
        jr z,.go
        call expr_str           ; (the name is not used)
.go:    pop hl
        ld a,(CDFLAG)
        push af
        push hl
        jr fast

; Back to SLOW if it was on before the tape operation (A = old CDFLAG).
tape_mode:
        rla
        ret nc
        jr slow

st_save:
        call tape_start
        ld hl,tape_end
        push hl
        jp save_arm
tape_end:
        pop af
        jr tape_mode

st_load:
        call tape_start
        ld a,0xff
        ld (VERSN),a
        ld hl,.done
        push hl
        jp load_entry
.done:  ld a,(VERSN)
        and a
        jr z,after_load
        xor a
        ld (VERSN),a
        jr tape_end

; A .p image replaced everything from VERSN to E_LINE. Rebuild the rest,
; then run on from NXTLIN when it was saved by a running program.
after_load:
        call reset_stack
        ld hl,(E_LINE)
        ld (hl),C_NL
        inc hl
        ld (WORKSP),hl
        call line_workspace
        ld (iy+Y_OFLAGS),0
        ld (iy+Y_DF_SZ),2
        ld a,(iy+Y_CDFLAG)
        res 7,(iy+Y_CDFLAG)
        push af
        call check_dfile
        pop af
        add a,a                 ; the mode the program asked for: bit 6
        call tape_mode
        ld hl,(NXTLIN)          ; autostart: NXTLIN inside the program
        ld de,PROG
        call cp_hl_de
        jr c,.stop
        call at_dfile
        jr c,run_loop
.stop:  rst 8
        db R_OK

; Rebuild an expanded display file that is not exactly 24 full rows. (A
; collapsed one is kept: it came from a machine with this little RAM.)
check_dfile:
        call collapsed
        ret c
        ld hl,(VARS)
        ld de,(D_FILE)
        and a
        sbc hl,de
        ld de,24*33+1
        sbc hl,de
        ret z
        jr cls
