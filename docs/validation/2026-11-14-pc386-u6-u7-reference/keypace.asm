; SPDX-License-Identifier: MIT
; KEYPACE.COM: how many keystrokes a guest actually receives, and when.
;
; Written for fes#1368, to tell a measurement-rig problem from a game
; problem. The kit and the 86Box reference are driven by the same command
; file, so the same tap sequence should arrive the same way on both. This
; program reads keys through INT 16h, the way Ultima VI does, and records the
; BIOS tick count (0:046Ch, 18.2065 Hz) at each one.
;
; One tick is 54.9 ms, too coarse to time a single gap, but the span from the
; first key to the last is what the rate is taken from: over ten seconds the
; mean interval is good to about half a percent.
;
; **The first key starts the clock and is not counted.** Timing has to begin at
; a key, not at the program's start, or the span would include however long the
; driver took to get going. So a run sends one priming tap, then the sequence
; under test, and KEYS is the count *after* the primer: 60 bursts behind one
; priming tap print KEYS=60, and a ten-second hold behind one prints KEYS=1. A
; run that sends N taps with no primer prints N-1, which is the tool's own
; arithmetic and not a dropped key — send the primer and the two cannot be
; confused.
;
; Run it, let the driver send its taps, then press q. It prints
;   KEYPACE KEYS=<n, excluding the priming key> SPAN=<ticks>
;   <tick of key 1> <tick of key 2> ...        (hex, relative to key 1, 8/line)
; and stops on q, on 64 keys, or after 1092 ticks (60 s) from the first key.
; Assemble: nasm -f bin -o KEYPACE.COM keypace.asm.

        bits 16
        org 0x100

MAXKEYS equ 64
TIMEOUT equ 1092                        ; ticks from the first key

start:  cld
        mov si, txt_banner
        call puts
        xor bp, bp                      ; keys recorded, excluding the priming key below

.first: mov ah, 0x01                    ; the priming key: starts the clock, not counted
        int 0x16
        jz .first
        mov ah, 0x00
        int 0x16
        cmp al, 'q'
        je .report
        call rdtick
        mov [t0], ax
        mov [t0+2], dx

.loop:  mov ah, 0x01
        int 0x16
        jz .timeout
        mov ah, 0x00
        int 0x16
        cmp al, 'q'
        je .report
        call rdtick
        sub ax, [t0]
        sbb dx, [t0+2]
        mov di, bp
        shl di, 1
        mov [table+di], ax              ; low word is enough: 65536 ticks is an hour
        inc bp
        cmp bp, MAXKEYS
        jae .report
        jmp .loop

.timeout:
        call rdtick
        sub ax, [t0]
        sbb dx, [t0+2]
        or dx, dx
        jnz .report
        cmp ax, TIMEOUT
        jb .loop

.report:
        mov si, txt_keys
        call puts
        mov ax, bp
        call putdec
        mov si, txt_span
        call puts
        xor ax, ax
        or bp, bp
        jz .nospan
        mov di, bp
        dec di
        shl di, 1
        mov ax, [table+di]
.nospan:
        call putdec
        call crlf
        xor si, si                      ; index
.list:  cmp si, bp
        jae .end
        mov di, si
        shl di, 1
        mov ax, [table+di]
        call puthex
        mov dl, ' '
        mov ah, 0x02
        int 0x21
        inc si
        test si, 7
        jnz .list
        call crlf
        jmp .list
.end:   call crlf
        mov ax, 0x4c00
        int 0x21

; rdtick -> DX:AX, the BIOS tick count, read without tearing.
rdtick: push bx
        push es
        mov ax, 0x40
        mov es, ax
.again: mov ax, [es:0x6c]
        mov dx, [es:0x6e]
        mov bx, [es:0x6c]
        cmp ax, bx
        jne .again
        pop es
        pop bx
        ret

puts:   lodsb
        or al, al
        jz .out
        mov dl, al
        mov ah, 0x02
        int 0x21
        jmp puts
.out:   ret

crlf:   mov dl, 13
        mov ah, 0x02
        int 0x21
        mov dl, 10
        mov ah, 0x02
        int 0x21
        ret

; putdec: AX unsigned, no padding.
putdec: push bx
        push cx
        push dx
        xor cx, cx
        mov bx, 10
.div:   xor dx, dx
        div bx
        push dx
        inc cx
        or ax, ax
        jnz .div
.emit:  pop dx
        add dl, '0'
        mov ah, 0x02
        int 0x21
        loop .emit
        pop dx
        pop cx
        pop bx
        ret

; puthex: AX as four hex digits.
puthex: push cx
        push bx
        mov bx, ax
        mov cx, 4
.digit: rol bx, 4
        mov al, bl
        and al, 0x0f
        add al, '0'
        cmp al, '9'
        jbe .put
        add al, 7
.put:   mov dl, al
        mov ah, 0x02
        int 0x21
        loop .digit
        pop bx
        pop cx
        ret

txt_banner db 'KEYPACE ready, press q to finish', 13, 10, 0
txt_keys   db 'KEYPACE KEYS=', 0
txt_span   db ' SPAN=', 0

t0      dd 0
table   times MAXKEYS dw 0
