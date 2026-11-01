; SPDX-License-Identifier: GPL-2.0-or-later
; Fixed ABI: restarts, display interrupts, keyboard tables and tape entry
; points at the addresses ZX81 software and the FES core use.

; Display timing. One scan line is 207 T-states; the INT that ends a line
; comes from R bit 6, so R is loaded to fire after the row's NOP run.
ROW_R    equ 0xDD
ROW_DELAY equ 6                 ; 13 * 6 + 2 + 9 T-states before the rows

        org 0x0000
start:  out (0xfd),a            ; NMI generator off
        jp init

        org 0x0008
rst_err: jp error_1             ; RST 8: report code byte follows the call

        org 0x0010
rst_print: jp print_a           ; RST 10: print character A

        org 0x0018
rst_get: jp get_char            ; RST 18: current character

        org 0x0020
rst_next: jp next_char          ; RST 20: advance and get character

        org 0x0028
rst_calc: jp fp_calc            ; RST 28: calculator (not yet provided)

        org 0x0030
rst_room: jp bc_spaces          ; RST 30: BC bytes of workspace

; INT ends each scan line of a character row. Both paths take 58 T-states
; so every line has the same length; B counts rows, C scan lines in a row.
        org 0x0038
int_row: dec c
        jp nz,int_same
        pop hl                  ; interrupted after HALT: next row
        dec b
        ret z                   ; last row drawn
        set 3,c                 ; eight scan lines
int_go: ld r,a
        ei
        jp (hl)                 ; run the row from its upper-memory echo
int_same:
        pop de                  ; same row again
        ret z                   ; never taken; balances the other path
        jr int_go

; NMI counts border lines in A'. When the count reaches zero the next NMI
; ends a HALT exactly on a line boundary and the IX routine takes over.
        org 0x0066
nmi:    ex af,af'
        inc a
        jp m,nmi_out
        jr z,nmi_sync
nmi_out: ex af,af'
        ret
nmi_sync:
        ex af,af'
        push af
        push bc
        push de
        push hl
        halt                    ; resumes on the next line's NMI
        out (0xfd),a
        jp (ix)

; Key tables, indexed row * 5 + column - 1 (SHIFT has no entry). Rows are
; address lines A8..A15; columns are data bits 0..4.
        org 0x007e
key_plain:
        db 0x3f,0x3d,0x28,0x3b                  ; Z X C V
        db 0x26,0x38,0x29,0x2b,0x2c             ; A S D F G
        db 0x36,0x3c,0x2a,0x37,0x39             ; Q W E R T
        db 0x1d,0x1e,0x1f,0x20,0x21             ; 1 2 3 4 5
        db 0x1c,0x25,0x24,0x23,0x22             ; 0 9 8 7 6
        db 0x35,0x34,0x2e,0x3a,0x3e             ; P O I U Y
        db 0x76,0x31,0x30,0x2f,0x2d             ; NEWLINE L K J H
        db 0x00,0x1b,0x32,0x33,0x27             ; SPACE . M N B
        assert $ == 0x00a5
key_shift:
        db 0x0e,0x19,0x0f,0x18                  ; : ; ? /
        db 0xe3,0xe1,0xe4,0xe5,0xe2             ; STOP LPRINT SLOW FAST LLIST
        db 0xc0,0xd9,0xe0,0xdb,0xdd             ; "" OR STEP <= <>
        db 0x75,0xda,0xde,0xdf,0x72             ; EDIT AND THEN TO left
        db 0x77,0x74,0x73,0x70,0x71             ; RUBOUT GRAPHICS right up down
        db 0x0b,0x11,0x10,0x0d,0xdc             ; " ) ( $ >=
        db 0x79,0x14,0x15,0x16,0xd8             ; FUNCTION = + - **
        db 0x0c,0x1a,0x12,0x13,0x17             ; £ , > < *
        assert $ == 0x00cc
key_func:                                       ; 0x78 = no function
        db 0xcd,0xce,0xc1,0x78                  ; LN EXP AT -
        db 0xca,0xcb,0xcc,0xd1,0xd2             ; ASN ACS ATN SGN ABS
        db 0xc7,0xc8,0xc9,0xcf,0x40             ; SIN COS TAN INT RND
        db 0x78,0x78,0x78,0x78,0x78
        db 0x78,0x78,0x78,0x78,0x78
        db 0xc2,0xd3,0xc4,0xd6,0xd5             ; TAB PEEK CODE CHR$ STR$
        db 0x78,0xd4,0xc6,0xc5,0xd0             ; - USR LEN VAL SQR
        db 0x78,0x78,0x42,0xd7,0x41             ; - - PI NOT INKEY$
        assert $ == 0x00f3
key_graph:                                      ; 0 = use the shifted key
        db 0x00,0x00,0x00,0x00                  ; Z X C V
        db 0x09,0x0a,0x89,0x8a,0x00             ; A S D F G
        db 0x81,0x82,0x83,0x84,0x85             ; Q W E R T
        db 0x01,0x02,0x03,0x04,0x05             ; 1 2 3 4 5
        db 0x00,0x00,0x08,0x07,0x06             ; 0 9 8 7 6
        db 0x00,0x88,0x87,0x86,0x80             ; P O I U Y
        db 0x00,0x00,0x00,0x00,0x00             ; NEWLINE L K J H
        db 0x00,0x00,0x00,0x00,0x00             ; SPACE . M N B
        assert $ == 0x011a
key_word:                                       ; K-mode keywords, 0 = plain
        db 0xff,0xfd,0xe8,0xfb                  ; COPY CLEAR CONT CLS
        db 0xe6,0xf8,0xe9,0xeb,0xec             ; NEW SAVE DIM FOR GOTO
        db 0xf6,0xfc,0xea,0xf7,0xf9             ; PLOT UNPLOT REM RUN RAND
        db 0x00,0x00,0x00,0x00,0x00
        db 0x00,0x00,0x00,0x00,0x00
        db 0xf5,0xf4,0xee,0xfa,0xfe             ; PRINT POKE INPUT IF RETURN
        db 0x00,0xf1,0xf0,0xef,0xed             ; - LET LIST LOAD GOSUB
        db 0x00,0x00,0xf2,0xf3,0xe7             ; - - PAUSE NEXT SCROLL

; ---------------------------------------------------------------------
; Fixed entry points.

; LOAD and SAVE finish by jumping here (the core patches these paths);
; the caller pushed its continuation.
        org 0x0207
tape_done:
        ret

; IX routine after the top border: draw the rows, then count the bottom
; border.
        org 0x0281
display_ix:
        call draw_rows
        ld ix,border_ix
        ld a,(MARGIN)
        neg
        ex af,af'
        out (0xfe),a
nmi_pop:
        pop hl
        pop de
        pop bc
        pop af
        ret

        org 0x02bb
keyboard: jp kscan              ; KEYBOARD

        org 0x02e7
set_fast: jp fast               ; SET-FAST

; SAVE: the core arms its fast path on the M1 at $02FB, then turns
; $02FC into JP $0313 and $031F into RET, recording E at each $031F.
        org 0x02fb
save_arm: nop
        jp save_body
        org 0x0313
save_body:
        ld hl,VERSN
        jp save_loop
        org 0x031e
save_byte:
        ld e,(hl)
        ret

; LOAD: the core replaces these bytes while it copies the mounted .p into
; RAM, then sets carry and leaves through $0207. Without the core the
; same bytes return at once.
        org 0x0347
load_entry:
        xor a
        scf
        jr nc,load_entry
        jp tape_done

        org 0x07bd
findchr: jp decode              ; DECODE (FINDCHR)
        org 0x08f5
print_at_entry: jp print_at     ; PRINT-AT
        org 0x0a2a
cls_entry: jp cls               ; CLS
        org 0x0f23
fast_entry: jp fast             ; FAST
        org 0x0f2b
slow_entry: jp slow             ; SLOW
        org 0x0f46
break_entry: jp break_1         ; BREAK-1

; ---------------------------------------------------------------------
; Frame engine (placed by the assembler).
        autofloat

; Draw the 24 character rows. The first HALT (D_FILE itself) waits for the
; first line INT; the INT routine returns here after the last row.
draw_rows:
        ld b,ROW_DELAY          ; picture phase: the output raster locks to
.d:     djnz .d                 ; the line timing at vertical sync
        ld a,i
        ld hl,(D_FILE)
        set 7,h
        ld bc,0x1901
        ld a,ROW_R
        jp int_go

; IX routine after the bottom border: vertical sync and keyboard, then the
; next top border unless FAST mode was selected.
border_ix:
        call frame_tasks
        bit 7,(iy+Y_CDFLAG)
        jr z,.stop
        call start_frames       ; ends sync, NMI on
        jp nmi_pop
.stop:  out (0xfd),a            ; ends sync, NMI stays off
        jp nmi_pop

; Keyboard read (starts vertical sync while the NMI generator is off),
; frame counter.
frame_tasks:
        call kscan
        ld (LAST_K),hl
        ld hl,(FRAMES)
        dec hl
        ld (FRAMES),hl
        ld b,VSYNC_PAD          ; hold vertical sync for a few lines
.pad:   djnz .pad
        ret
VSYNC_PAD equ 25

; Wait until another frame has been shown and the keyboard read. In FAST
; mode one frame is generated here.
wait_frame:
        ld a,(FRAMES)
        ld e,a
        bit 7,(iy+Y_CDFLAG)
        call z,start_frames
frame_spin:
        ld a,(FRAMES)
        cp e
        jr z,frame_spin
        ret

start_frames:
        ld a,(MARGIN)           ; top border: MARGIN - 2 lines
        neg
        add a,2
        ex af,af'
        ld ix,display_ix
        out (0xfe),a            ; NMI on: the top border starts
        ret

; SLOW: keep generating frames from the NMI. CDFLAG bit 6 asks for SLOW
; (it is saved with a program), bit 7 says the frames are running.
slow:   set 6,(iy+Y_CDFLAG)
        bit 7,(iy+Y_CDFLAG)
        ret nz
        set 7,(iy+Y_CDFLAG)
        jr start_frames

; FAST: stop after the frame in progress. Keep bit 6, the saved SLOW request;
; bit 7 alone says whether frames are currently running.
fast:   bit 7,(iy+Y_CDFLAG)
        ret z
        ld a,(FRAMES)
        ld e,a
        res 7,(iy+Y_CDFLAG)
        jr frame_spin

; Keyboard scan. Returns HL in the ZX81 KEYBOARD format: L has a zero bit
; for each address line whose row has a key down, H bits 1-5 are the
; pressed columns (low = down) and H bit 0 is low while SHIFT is down.
; Also sets MARGIN from the 50/60 Hz link.
kscan:  ld hl,0xffff
        ld bc,0xfefe
.row:   in a,(c)
        or 0xe0
        bit 0,b
        jr nz,.keys
        or 0x01                 ; SHIFT is not a key
.keys:  cp 0xff
        jr z,.next
        and h
        ld h,a
        ld a,l
        and b
        ld l,a
.next:  rlc b
        jr c,.row
        ld a,0xfe
        in a,(0xfe)
        rra
        rl h
        and 0x20                ; bit 6 before the shift: high on 50 Hz
        ld a,31
        jr z,.hz
        ld a,55
.hz:    ld (MARGIN),a
        ret

; BREAK-1: carry clear while SPACE is down.
break_1:
        ld a,0x7f
        in a,(0xfe)
        rra
        ret

; DECODE: BC = KEYBOARD result. Returns HL -> the key's character in
; key_plain or key_shift; carry set if no single key is down.
decode: ld a,c
        ld d,0
.r:     rra
        jr nc,.rf
        inc d
        bit 3,d
        jr z,.r
        and a                   ; no single row: carry clear
        ret
.rf:    ld a,b
        rra
        ld e,0
.c:     rra
        jr nc,.cf
        inc e
        bit 3,e
        jr z,.c
        and a                   ; no single column: carry clear
        ret
.cf:    ld a,d
        add a,a
        add a,a
        add a,d
        add a,e
        dec a
        ld e,a
        ld d,0
        ld hl,key_plain
        bit 0,b
        jr nz,.plain
        ld l,key_shift & 0xff   ; same page
.plain: add hl,de
        scf                     ; a key was decoded successfully
        ret

save_loop:
        call save_byte
        inc hl
        ex de,hl
        ld hl,(E_LINE)
        and a
        sbc hl,de
        ex de,hl
        jr nz,save_loop
        jp tape_done

        include "init.asm"
        endautofloat
