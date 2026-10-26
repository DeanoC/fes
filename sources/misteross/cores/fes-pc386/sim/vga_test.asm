; SPDX-License-Identifier: MIT
; pc386 VGA test disk (docs/pc386-machine-design.md, phase M2): the modes the
; acceptance games need, set through the VGA BIOS, then programmed the way the
; games program them. scripts/sim_fes_pc386.py --suite vga assembles it with
; NASM into a disk image; machine_tb boots it, records the VGA trace and
; writes a frame at every SNAP marker.
;
; Each picture is announced on port E9h as "SNAP <name>" and then held for
; HOLD_FRAMES vertical retraces, so the bench captures it and, with the pixel
; clock running, sees it at 720p. "DONE" ends the run.
;
;   text          mode 03h: all 128 colour pairs, line-drawing characters
;                 C0h-DFh (9th dot), shading, blinking text, a block cursor,
;                 a scrolled window
;   text-font     four characters replaced through INT 10h AX=1100h
;   text-bright   blink off (INT 10h AX=1003h): bright backgrounds
;   mode13        mode 13h: a 256-colour pattern, a grey ramp in DAC 0-63
;   mode13-cycle  DAC entries 64-127 rotated in retrace (palette cycling)
;   modey         unchained 320 x 200: map-mask column writes on page 0,
;                 a block copied with write mode 1 latch copies
;   modey-flip    page 1 shown by start address with one pixel of panning,
;                 as VL_SetScreen does: start address in display, panning in
;                 retrace
;   modey-split   line compare at scan line 299 (row 150), panning compat on
;   modey-fade    the DAC faded to half over 16 retraces
;   mode12        mode 12h: write mode 2 bars, set/reset, a read mode 1
;                 colour compare written back as a byte row
;   final         mode 03h with "DONE"

bits 16
cpu 386

PROGRAM_SECTORS equ 32
HOLD_FRAMES     equ 8
DEBUG_PORT      equ 0xe9

section boot start=0 vstart=0x7c00
boot:
    cli
    xor ax, ax
    mov ds, ax
    mov ss, ax
    mov sp, 0x7c00
    sti
    mov ax, 0x1000
    mov es, ax
    xor bx, bx
    mov ax, 0x0200 | PROGRAM_SECTORS
    mov cx, 0x0002
    xor dh, dh
    int 0x13
    jc .fail
    jmp 0x1000:0
.fail:
    hlt
    jmp .fail
    times 510 - ($ - $$) db 0
    dw 0xaa55

section program start=512 vstart=0 align=1
program:
    mov ax, cs
    mov ds, ax
    mov ss, ax
    mov sp, 0xfffe
    cld

; ---- Text mode 03h -----------------------------------------------------------
    mov ax, 0x0003
    int 0x10
    mov ax, 0xb800
    mov es, ax
    ; Row 0: a title.
    xor di, di
    mov si, title
    mov ah, 0x1f
    call put_string
    ; Rows 2-17: every attribute 00h-7Fh as "Ab" pairs, eight per row.
    mov di, 2 * 160
    xor bl, bl
.attr_row:
    mov cx, 8
.attr_cell:
    mov ah, bl
    mov al, 'A'
    stosw
    mov al, 'b'
    stosw
    mov al, ' '
    stosw
    inc bl
    loop .attr_cell
    add di, 160 - 8 * 6
    cmp bl, 0x80
    jb .attr_row
    ; Row 19: line drawing C0h-DFh, then the shades B0h-B2h and DBh.
    mov di, 19 * 160
    mov al, 0xc0
    mov ah, 0x0e
.lines:
    stosw
    inc al
    cmp al, 0xe0
    jb .lines
    mov al, 0xb0
    stosw
    mov al, 0xb1
    stosw
    mov al, 0xb2
    stosw
    mov al, 0xdb
    stosw
    ; Row 20: blinking text.
    mov di, 20 * 160
    mov si, blinking
    mov ah, 0xce
    call put_string
    ; A scrolled window: rows 21-23, columns 40-70, up one line, attribute 1Fh.
    mov di, 21 * 160 + 80
    mov si, window_text
    mov ah, 0x2f
    call put_string
    mov di, 22 * 160 + 80
    mov si, window_more
    mov ah, 0x2f
    call put_string
    mov ax, 0x0601
    mov bh, 0x1f
    mov cx, 0x1528
    mov dx, 0x1746
    int 0x10
    ; A block cursor at row 24, column 10.
    mov ah, 0x01
    mov cx, 0x000f
    int 0x10
    mov ah, 0x02
    xor bh, bh
    mov dx, 0x180a
    int 0x10
    mov si, name_text
    call snap

    ; Four characters 80h-83h from our own glyphs.
    push es
    push ds
    pop es
    mov ax, 0x1100
    mov bp, glyphs
    mov cx, 4
    mov dx, 0x80
    mov bx, 0x1000
    int 0x10
    pop es
    mov di, 24 * 160 + 40
    mov ax, 0x0a80
    stosw
    inc al
    stosw
    inc al
    stosw
    inc al
    stosw
    mov si, name_font
    call snap

    ; Blink off: attribute bit 7 selects bright backgrounds.
    mov ax, 0x1003
    xor bl, bl
    int 0x10
    mov si, name_bright
    call snap

; ---- Mode 13h ------------------------------------------------------------------
    mov ax, 0x0013
    int 0x10
    mov ax, 0xa000
    mov es, ax
    ; Pixel (x, y) = x + 2y mod 256.
    xor di, di
    xor dx, dx
.m13_row:
    mov al, dl
    add al, dl
    mov cx, 320
.m13_pixel:
    stosb
    inc al
    loop .m13_pixel
    inc dx
    cmp dx, 200
    jb .m13_row
    ; DAC 0-63: a grey ramp.
    xor al, al
    mov dx, 0x3c8
    out dx, al
    inc dx
    xor bl, bl
.ramp:
    mov al, bl
    out dx, al
    out dx, al
    out dx, al
    inc bl
    cmp bl, 64
    jb .ramp
    mov si, name_13
    call snap

    ; Palette cycling: rotate DAC 64-127 by one, eight times, each in retrace.
    mov cx, 8
.cycle:
    push cx
    call wait_retrace
    call rotate_dac
    pop cx
    loop .cycle
    mov si, name_13_cycle
    call snap

; ---- Mode Y: mode 13h unchained ------------------------------------------------
    ; Sequencer memory mode: chain-4 and odd/even off.
    mov dx, 0x3c4
    mov ax, 0x0604
    out dx, ax
    ; CRTC: doubleword mode off, byte mode on.
    mov dx, 0x3d4
    mov ax, 0x0014
    out dx, ax
    mov ax, 0xe317
    out dx, ax
    ; Clear all four planes.
    mov dx, 0x3c4
    mov ax, 0x0f02
    out dx, ax
    xor di, di
    xor ax, ax
    mov cx, 0x8000
    rep stosw
    ; Page 0 (offset 0): column x gets colour x mod 256 in rows 0-199, one
    ; map mask per column, as a wall post would be drawn.
    xor bx, bx
.column:
    mov cl, bl
    and cl, 3
    mov ah, 1
    shl ah, cl
    mov al, 0x02
    mov dx, 0x3c4
    out dx, ax
    mov di, bx
    shr di, 2
    mov al, bl
    mov cx, 200
.post:
    mov [es:di], al
    add di, 80
    loop .post
    inc bx
    cmp bx, 320
    jb .column
    ; Page 1 (offset 16000): horizontal bands, all planes at once.
    mov dx, 0x3c4
    mov ax, 0x0f02
    out dx, ax
    mov di, 16000
    xor bx, bx
.band:
    mov al, bl
    shr al, 2
    add al, 0x20
    mov cx, 80
    rep stosb
    inc bx
    cmp bx, 200
    jb .band
    ; An off-screen block at offset 32000: 20 bytes x 40 rows of colour 0x0f.
    mov di, 32000
    mov al, 0x0f
    mov cx, 20 * 40
    rep stosb
    ; Latch copy (write mode 1, all planes): the block onto page 0 at
    ; byte column 30, row 80, as VL_LatchToScreen does.
    mov dx, 0x3ce
    mov ax, 0x4105
    out dx, ax
    push ds
    mov ax, es
    mov ds, ax
    mov si, 32000
    mov di, 80 * 80 + 30
    mov bx, 40
.latch_row:
    mov cx, 20
    rep movsb
    add di, 80 - 20
    dec bx
    jnz .latch_row
    pop ds
    mov dx, 0x3ce
    mov ax, 0x4005
    out dx, ax
    mov si, name_y
    call snap

    ; Page flip to page 1 with one pixel of panning: the start address while
    ; the display is on, then the panning in vertical retrace.
    mov dx, 0x3da
.display_on:
    in al, dx
    test al, 1
    jnz .display_on
    cli
    mov dx, 0x3d4
    mov ax, ((16000 >> 8) << 8) | 0x0c
    out dx, ax
    mov ax, ((16000 & 0xff) << 8) | 0x0d
    out dx, ax
    sti
    call wait_retrace
    mov dx, 0x3da
    in al, dx
    mov dx, 0x3c0
    mov al, 0x33
    out dx, al
    mov al, 2
    out dx, al
    mov si, name_flip
    call snap

    ; Split screen at scan line 299: below it, memory from offset 0 (page 0)
    ; without panning (attribute mode bit 5).
    mov dx, 0x3da
    in al, dx
    mov dx, 0x3c0
    mov al, 0x30
    out dx, al
    mov al, 0x61
    out dx, al
    mov dx, 0x3d4
    mov ax, ((299 & 0xff) << 8) | 0x18
    out dx, ax
    mov al, 0x07
    out dx, al
    inc dx
    in al, dx
    or al, 0x10
    out dx, al
    dec dx
    mov al, 0x09
    out dx, al
    inc dx
    in al, dx
    and al, 0xbf
    out dx, al
    mov si, name_split
    call snap

    ; Fade: halve DAC 0-255 over 16 retraces, one step a retrace.
    mov cx, 16
.fade:
    push cx
    call wait_retrace
    call fade_dac
    pop cx
    loop .fade
    mov si, name_fade
    call snap

; ---- Mode 12h --------------------------------------------------------------------
    mov ax, 0x0012
    int 0x10
    mov ax, 0xa000
    mov es, ax
    ; Write mode 2: 16 vertical bars of 40 pixels, bit mask per byte.
    mov dx, 0x3ce
    mov ax, 0x0205
    out dx, ax
    xor bx, bx
.bar_row:
    mov di, bx
    imul di, di, 80
    xor cx, cx
.bar_byte:
    mov ax, cx
    mov dl, 5
    div dl
    mov ah, al
    mov al, 0xff
    xchg al, ah
    mov [es:di], al
    inc di
    inc cx
    cmp cx, 80
    jb .bar_byte
    inc bx
    cmp bx, 240
    jb .bar_row
    ; Set/reset colour 0Eh through write mode 0 in rows 300-339, bit mask 3Ch.
    mov dx, 0x3ce
    mov ax, 0x0005
    out dx, ax
    mov ax, 0x0e00
    out dx, ax
    mov ax, 0x0f01
    out dx, ax
    mov ax, 0x3c08
    out dx, ax
    mov di, 300 * 80
    mov cx, 40 * 80
.setreset:
    mov al, [es:di]
    mov [es:di], al
    inc di
    loop .setreset
    mov ax, 0x0001
    out dx, ax
    mov ax, 0xff08
    out dx, ax
    ; Read mode 1, compare colour 05h: row 400 gets the compare of row 0.
    mov ax, 0x0805
    out dx, ax
    mov ax, 0x0502
    out dx, ax
    mov ax, 0x0f07
    out dx, ax
    xor si, si
    mov cx, 80
.compare:
    mov al, [es:si]
    mov [compare_row + si], al
    inc si
    loop .compare
    mov ax, 0x0005
    out dx, ax
    mov di, 400 * 80
    mov si, compare_row
    mov cx, 80
.store:
    lodsb
    mov ah, al
    mov al, [es:di]
    mov [es:di], ah
    inc di
    loop .store
    mov si, name_12
    call snap

; ---- Back to text -------------------------------------------------------------------
    mov ax, 0x0003
    int 0x10
    mov ax, 0xb800
    mov es, ax
    xor di, di
    mov si, done_text
    mov ah, 0x07
    call put_string
    mov si, name_final
    call snap
    mov si, done_text
    call debug_line
.idle:
    hlt
    jmp .idle

; ---- Subroutines ------------------------------------------------------------------
; ES:DI <- the string at DS:SI with attribute AH.
put_string:
    lodsb
    test al, al
    jz .end
    stosw
    jmp put_string
.end:
    ret

; Port E9h <- the string at DS:SI and a newline.
debug_line:
    mov dx, DEBUG_PORT
.next:
    lodsb
    test al, al
    jz .end
    out dx, al
    jmp .next
.end:
    mov al, 10
    out dx, al
    ret

; "SNAP <name>" on port E9h, then hold for HOLD_FRAMES retraces.
snap:
    push si
    mov si, snap_prefix
    mov dx, DEBUG_PORT
.prefix:
    lodsb
    test al, al
    jz .name
    out dx, al
    jmp .prefix
.name:
    pop si
    call debug_line
    mov cx, HOLD_FRAMES
.hold:
    push cx
    call wait_retrace
    pop cx
    loop .hold
    ret

; Wait for the start of the next vertical retrace.
wait_retrace:
    mov dx, 0x3da
.in_retrace:
    in al, dx
    test al, 8
    jnz .in_retrace
.before:
    in al, dx
    test al, 8
    jz .before
    ret

; DAC 64-127 rotated down by one entry.
rotate_dac:
    mov al, 64
    mov dx, 0x3c7
    out dx, al
    mov dx, 0x3c9
    mov di, dac_buffer
    mov cx, 64 * 3
.read:
    in al, dx
    mov [di], al
    inc di
    loop .read
    mov al, 64
    mov dx, 0x3c8
    out dx, al
    mov dx, 0x3c9
    mov si, dac_buffer + 3
    mov cx, 63 * 3
.write:
    lodsb
    out dx, al
    loop .write
    mov si, dac_buffer
    mov cx, 3
.wrap:
    lodsb
    out dx, al
    loop .wrap
    ret

; Every DAC component reduced by 1/32 of its value at the start of the fade
; (two each step at most): v - v / 32, rounded up.
fade_dac:
    xor al, al
    mov dx, 0x3c7
    out dx, al
    mov dx, 0x3c9
    mov di, dac_buffer
    mov cx, 256 * 3
.read:
    in al, dx
    mov ah, al
    shr ah, 5
    inc ah
    shr ah, 1
    sub al, ah
    jnc .keep
    xor al, al
.keep:
    mov [di], al
    inc di
    loop .read
    xor al, al
    mov dx, 0x3c8
    out dx, al
    mov dx, 0x3c9
    mov si, dac_buffer
    mov cx, 256 * 3
.write:
    lodsb
    out dx, al
    loop .write
    ret

title:          db "pc386 VGA test: mode 03h through the VGA BIOS", 0
blinking:       db "blinking text", 0
window_text:    db "a window scrolled up one line ", 0
window_more:    db "second line of the window     ", 0
done_text:      db "DONE", 0
snap_prefix:    db "SNAP ", 0
name_text:      db "text", 0
name_font:      db "text-font", 0
name_bright:    db "text-bright", 0
name_13:        db "mode13", 0
name_13_cycle:  db "mode13-cycle", 0
name_y:         db "modey", 0
name_flip:      db "modey-flip", 0
name_split:     db "modey-split", 0
name_fade:      db "modey-fade", 0
name_12:        db "mode12", 0
name_final:     db "final", 0
glyphs:
    ; 80h: checkerboard, 81h: frame, 82h: diagonal, 83h: solid top half.
    db 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55
    db 0xff, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0x81, 0xff
    db 0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01, 0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01
    db 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00
compare_row:    times 80 db 0
dac_buffer:     times 256 * 3 db 0
    times PROGRAM_SECTORS * 512 - ($ - $$) db 0
