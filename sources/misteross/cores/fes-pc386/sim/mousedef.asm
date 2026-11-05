; SPDX-License-Identifier: MIT
; MOUSEDEF.COM: gives the INT 33h driver a default event handler that is not
; 0:0, with an empty event mask (docs/pc386-machine-design.md, "FreeDOS in
; the FogCast disk builder"). AUTOEXEC.BAT runs it right after CuteMouse.
;
; CuteMouse starts with no handler, 0:0, and its reset (function 00h) clears
; only the mask. Microsoft's driver and DOSBox's start with a non-null one
; (DOSBox: 6362:0000, mask 0). Ultima VII's MAINMENU.EXE swaps its handler in
; with function 14h and keeps the old one only while its saved copy is 0:0,
; so after CuteMouse's 0:0 it saves its own handler instead, puts that back as
; it exits, and the next program on the same load address is called at a
; stale address on every mouse event (fes#1485). CuteMouse also keeps the
; handler and mask that guest RAM held before it loaded, so a kit load that
; follows another run can start with a stale handler armed.
;
; The handler set here is the first RETF byte in the system BIOS ROM, so a
; call through it would return at once; with mask 0 nothing calls it. A
; machine with no INT 33h vector is left alone. Assemble:
; nasm -f bin -o MOUSEDEF.COM mousedef.asm. 8086 code.

        bits 16
        cpu 8086
        org 0x100

start:  xor ax, ax
        mov es, ax
        mov ax, [es:0x33 * 4]
        or ax, [es:0x33 * 4 + 2]
        jz .done
        mov ax, 0xf000
        mov es, ax
        xor di, di
        mov cx, 0xffff
        mov al, 0xcb
        cld
        repne scasb
        jne .done
        dec di
        mov dx, di
        mov ax, 0x0014
        xor cx, cx
        int 0x33
.done:  mov ax, 0x4c00
        int 0x21
