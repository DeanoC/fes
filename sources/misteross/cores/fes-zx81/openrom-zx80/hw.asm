; SPDX-License-Identifier: GPL-2.0-or-later
; Fixed ABI: restarts, the display interrupt, the FES core's tape entry
; points, and the frame engine.

; Display timing. One scan line is 207 T-states; the INT that ends a line
; comes from R bit 6, so R is loaded to fire after the row's NOP run.
ROW_R    equ 0xDD
TOP_LINES equ 55                ; border lines above the first row
BOT_LINES equ 55                ; border lines below the last row
VSYNC_PAD equ 50                ; holds vertical sync for six lines

        org 0x0000
start:  di
        jp init
st_new: rst 0x20                ; (statements that fit between the restarts)
        jp init

; The restarts hold the start of their routines.
        org 0x0008
rst_err: pop hl                 ; RST 8: report code byte follows the call
        ld a,(hl)
        ld (ERR_NR),a
        jr unwind

        org 0x0010
rst_print:                      ; RST 10: print character or token A
        cp T_QUOTE
        jr nc,print_token
        jr print_a

        org 0x0018
rst_get: ld hl,(CH_ADD)         ; RST 18: current character
        jr gc_look

        org 0x0020
rst_end: push hl                ; RST 20: the statement must end here
        rst 0x18
        pop hl
        jp stmt_end

        org 0x0028
cp_hl_de: and a                 ; RST 28: compare HL with DE
        sbc hl,de
        add hl,de
        ret
st_stop:
        rst 0x20
        rst 8
        db R_STOP

        org 0x0030
rst_expect:                     ; RST 30: the token that follows must come next
        ex (sp),hl
        ld a,(hl)
        inc hl
        ex (sp),hl
        push hl
        jr expect_2

; INT ends each scan line. Both paths take 58 T-states so every line has
; the same length; B counts rows, C scan lines in a row.
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

; LOAD: the core replaces $0207-$020D while it copies the mounted image to
; $4000, then sets carry and leaves through $0203. Without the core the
; same bytes return at once.
        org 0x0203
load_back: jp load_done
        org 0x0207
load_entry:
        xor a
        scf
        jr nc,$-1
        jp load_back

; ---------------------------------------------------------------------
; Frame engine (placed by the assembler).
        autofloat

; Show one frame. Vertical sync carries the keyboard read; then the top
; border, the 24 rows and the bottom border, all counted by the line INT.
; The time is the same whatever is pressed. Returns, in the alternate DE,
; a fingerprint of the keys down (any one key changing changes it).
; Interrupts must be off.
frame:  ld hl,0
        ld bc,0xfefe
.row:   in a,(c)                ; the first read starts vertical sync
        or 0xe0
        add a,l                 ; a weighted sum: a key down or up always
                                ; changes it (XOR let two keys cancel)
        ld l,a
        add hl,hl
        rlc b
        jr c,.row
        ex de,hl
        exx                     ; (the line interrupt uses DE: keep it aside)
        ld hl,(FRAMES)
        inc hl
        ld (FRAMES),hl
        ld b,VSYNC_PAD
.pad:   djnz .pad
        out (0xff),a            ; vertical sync ends
        ld hl,(D_FILE)
        set 7,h
        push hl
        ld bc,0x1900 + TOP_LINES ; the first HALT for the border, then 24 rows
        call .rows
        pop hl
        ld bc,0x0100 + BOT_LINES
.rows:  ld a,ROW_R
        jr int_go

; Wait for a new key press; the display runs meanwhile. Returns A = key
; index. A held key does not repeat.
wait_key:
        call frame
        exx                     ; DE = the keys' fingerprint again
frame_got:                      ; (the fast harness gives DE and comes in here)
        ld hl,(LAST_K)
        ld (LAST_K),de
        rst 0x28
        jr z,wait_key           ; unchanged: same time every frame
key_scan:                       ; the key down: row * 5 + column, +40 with SHIFT
        ld bc,0xfefe
        in a,(c)
        rra                     ; carry clear: SHIFT is down
        sbc a,a
        cpl
        and 40
        ld d,a                  ; index of the row's first key
        ld e,0xff
        in a,(c)
        or 1                    ; SHIFT itself is not a key
.row:   ld l,d
        ld h,5
.bit:   rra
        jr c,.up
        ld e,l
.up:    inc l
        dec h
        jr nz,.bit
        ld d,l                  ; the next row's first key
        rlc b
        in a,(c)
        jr c,.row
        ld a,e
key_got:                        ; (the fast harness gives A and comes in here)
        inc a
        jr z,wait_key           ; none: released
        dec a
        ret

        endautofloat
