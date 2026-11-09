; SPDX-License-Identifier: MIT
; SCROLLT.COM: the VGA BIOS's mode set keeps the caller's registers (#1553),
; and INT 10h AH=07h (scroll down) of a window whose top row is 0 returns in
; the three graphics memory models (#1493; docs/pc386-machine-design.md,
; "Firmware"). scripts/sim_fes_pc386.py --suite
; scroll runs it at the FreeDOS prompt. Each check prints
;   SCR <name> <matches>/2 ok
; (or FAIL) to the screen and port E9h, and the run ends with "SCR DONE ok"
; or "SCR DONE FAIL".
;
; modeset: an IBM VGA BIOS's INT 10h AH=00h changes only AL. MicroProse's
; MGRAPHIC.EXE (Darklands) keeps the mode it asked for in DL across the call
; and compares it with INT 10h AH=0Fh's AL; the Bochs VGA BIOS returned with DX
; changed, so Darklands reported MCGA unsupported. For modes 13h, 93h (no
; clear) and 03h the check loads BX, CX, DX, SI, DI, BP and ES with known
; values, sets the mode, and counts each of those and DS kept, and AH=0Fh's AL
; equal to the mode asked for: nine matches a mode, 27 in all.
;
; Ultima VII: The Black Gate scrolls its whole mode 13h screen down one row
; through INT 10h AH=07h with CH = 0 after Iolo joins. An IBM BIOS moves the
; picture down one character row and blanks the top row with BH, then returns.
; The Bochs VGA BIOS's graphics loops count an unsigned row down to the top
; row, so with top row 0 they never end. Each mode below:
;   puts a marker byte at video offset 0 (scan line 0, the top row),
;   scrolls rows 0 to the last row down by one with BH = 0,
;   and counts two matches: the marker one character row lower, and the top
;   row's first byte 0.
;   mode13  linear, 320 bytes a line, 8-line characters: marker at 8 x 320
;   mode12  planar, 80 bytes a line, 16-line characters: marker (all four
;           planes) at 16 x 80, read through read map select
;   mode04  CGA, 80 bytes a line pair, even lines first, 8-line characters:
;           line 8 is even, at 4 x 80
; A BIOS that never returns from the scroll never prints; the bench's limit
; ends the run, which then fails.
;
; Assemble: nasm -f bin -o SCROLLT.COM scroll_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9
MARKER equ 0x5a

%macro kept 2
        cmp %1, %2
        jne %%lost
        inc byte [cs:result_modeset]
%%lost:
%endmacro

start:  mov [our_ds], ds
        mov si, modes
.mode:  mov al, [si]
        push si
        push ds
        push es
        push bp
        xor ah, ah
        mov bx, 0x1234
        mov cx, 0x2345
        mov dx, 0x3456
        mov si, 0x4567
        mov di, 0x5678
        mov bp, 0x6789
        push word 0x789a
        pop es
        int 0x10
        kept bx, 0x1234
        kept cx, 0x2345
        kept dx, 0x3456
        kept si, 0x4567
        kept di, 0x5678
        kept bp, 0x6789
        mov ax, es
        kept ax, 0x789a
        mov ax, ds
        kept ax, [cs:our_ds]
        pop bp
        pop es
        pop ds
        pop si
        mov ah, 0x0f
        int 0x10
        kept al, [si]
        inc si
        cmp si, modes_end
        jb .mode

        mov ax, 0x0013
        int 0x10
        push word 0xa000
        pop es
        mov byte [es:0], MARKER
        mov dx, 0x1827                  ; rows 0-24, columns 0-39
        call scroll
        xor bx, bx
        cmp byte [es:8 * 320], MARKER
        jne .m13
        inc bx
.m13:   cmp byte [es:0], 0
        jne .m13b
        inc bx
.m13b:  mov [result13], bl

        mov ax, 0x0012
        int 0x10
        mov byte [es:0], 0xff           ; write mode 0, all planes
        mov dx, 0x1d4f                  ; rows 0-29, columns 0-79
        call scroll
        mov dx, 0x3ce
        mov ax, 0x0004                  ; read map select: plane 0
        out dx, ax
        xor bx, bx
        cmp byte [es:16 * 80], 0xff
        jne .m12
        inc bx
.m12:   cmp byte [es:0], 0
        jne .m12b
        inc bx
.m12b:  mov [result12], bl

        mov ax, 0x0004
        int 0x10
        push word 0xb800
        pop es
        mov byte [es:0], MARKER
        mov dx, 0x1827                  ; rows 0-24, columns 0-39
        call scroll
        xor bx, bx
        cmp byte [es:4 * 80], MARKER
        jne .m04
        inc bx
.m04:   cmp byte [es:0], 0
        jne .m04b
        inc bx
.m04b:  mov [result04], bl

        mov ax, 0x0003
        int 0x10
        mov si, name_modeset
        movzx eax, byte [result_modeset]
        mov ecx, 27
        mov edx, 27
        mov di, unit_of_27
        call check_range
        mov si, name_13
        movzx eax, byte [result13]
        call check
        mov si, name_12
        movzx eax, byte [result12]
        call check
        mov si, name_04
        movzx eax, byte [result04]
        call check

        mov si, name_done
        xor eax, eax
        movzx ecx, byte [failed]
        xor edx, edx
        mov di, unit_none
        call check_range
        mov ax, 0x4c00
        or al, [failed]
        int 0x21

; Scrolls rows 0 to DH, columns 0 to DL, down one row with BH = 0.
scroll: mov ax, 0x0701
        xor bh, bh
        xor cx, cx
        int 0x10
        ret

; Checks EAX = 2 matches.
check:  mov ecx, 2
        mov edx, 2
        mov di, unit_of_two
        ; fall through

; Prints "SCR <name SI> <EAX><unit DI> ok" if ECX <= EAX <= EDX, else FAIL.
; The DONE line passes EAX = 0 and ECX = the failure flag, so it fails after
; any failed check, and unit_none prints no value. Keeps every register.
check_range:
        pushad
        mov [value], eax
        mov [lowest], ecx
        mov [highest], edx
        mov dx, msg_scr
        call say
        mov dx, si
        call say
        cmp di, unit_none
        je .judge
        mov dx, msg_space
        call say
        call say_number
        mov dx, di
        call say
.judge: mov eax, [value]
        mov dx, msg_ok
        cmp eax, [lowest]
        jb .fail
        cmp eax, [highest]
        jbe .print
.fail:  mov byte [failed], 1
        mov dx, msg_fail
.print: call say
        mov dx, msg_newline
        call say
        mov al, 10
        out DEBUG_PORT, al
        popad
        ret

; Prints [value] in decimal. Keeps every register.
say_number:
        pushad
        mov eax, [value]
        mov di, number_end
        mov ebx, 10
.digit: xor edx, edx
        div ebx
        add dl, '0'
        dec di
        mov [di], dl
        test eax, eax
        jnz .digit
        mov dx, di
        call say
        popad
        ret

; Prints the $-terminated string at DX with INT 21h AH=09h and writes it,
; without CR and LF, to port E9h.
say:    push ax
        push si
        mov ah, 0x09
        int 0x21
        mov si, dx
.e9:    lodsb
        cmp al, '$'
        je .done
        cmp al, 13
        je .e9
        cmp al, 10
        je .e9
        out DEBUG_PORT, al
        jmp .e9
.done:  pop si
        pop ax
        ret

; ---- Data -----------------------------------------------------------------

failed:        db 0
modes:         db 0x13, 0x93, 0x03
modes_end:
our_ds:        dw 0
result_modeset: db 0
result13:      db 0
result12:      db 0
result04:      db 0
value:         dd 0
lowest:        dd 0
highest:       dd 0
number:        times 10 db 0
number_end:    db '$'
msg_scr:       db 'SCR $'
msg_space:     db ' $'
msg_ok:        db ' ok$'
msg_fail:      db ' FAIL$'
msg_newline:   db 13, 10, '$'
unit_of_two:   db '/2$'
unit_of_27:    db '/27$'
unit_none:     db '$'
name_modeset:  db 'modeset$'
name_13:       db 'mode13$'
name_12:       db 'mode12$'
name_04:       db 'mode04$'
name_done:     db 'DONE$'
