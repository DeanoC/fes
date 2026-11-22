; SPDX-License-Identifier: MIT
; SHRREPRO.COM: the minimal repro for fes#1777, with SHARE loaded. Each step
; prints "R <step> <AX> <CF>" to the screen and to port E9h (the pc386
; bench's debug port; on other machines nothing listens there).
;
;   create   INT 21h AH=3Ch C:\SHRREPRO.TMP, attributes 0
;   close    INT 21h AH=3Eh on that handle
;   rename   INT 21h AH=56h C:\SHRREPRO.TMP to C:\SHRREPR2.TMP; no handle is open
;   delete   INT 21h AH=41h C:\SHRREPR2.TMP (C:\SHRREPRO.TMP if the rename failed)
;   absent   INT 21h AH=41h C:\SHRABSNT.TMP, a file that does not exist
;   is_open  INT 2Fh AX=10A6h DS:SI -> C:\SHRABSNT.TMP, the call kernel 2046
;            makes before a delete or rename; prints the AX it returns
;
; MS-DOS 6.22 with SHARE.EXE: rename and delete succeed (CF 0), absent
; returns 0002h (file not found), and is_open returns 0001h with CF set (not
; supported). FreeDOS kernel 2046 with SHARE 08/2006a: rename, delete and
; absent all return 0005h (access denied), and is_open returns 0001h with CF
; set. SHARE 08/2006a does not handle 10A6h, so the call goes on down the
; INT 2Fh chain and fails with AX=1. The kernel's caller ignores CF and
; reads AX=1 as "open".
;
; Assemble: nasm -f bin -o SHRREPRO.COM shrrepro.asm

        bits 16
        org 0x100

DEBUG_PORT equ 0xe9

%macro STEP 1
        call report
        db %1, 0
%endmacro

start:  mov ah, 0x3c
        xor cx, cx
        mov dx, name1
        int 21h
        STEP 'create'
        mov bx, ax
        mov ah, 0x3e
        int 21h
        STEP 'close'
        mov ah, 0x56
        mov dx, name1
        mov di, name2
        int 21h
        STEP 'rename'
        mov dx, name2
        jnc .delete
        mov dx, name1
.delete: mov ah, 0x41
        int 21h
        STEP 'delete'
        mov ah, 0x41
        mov dx, absent
        int 21h
        STEP 'absent'
        mov ax, 0x10a6
        mov si, absent
        int 2fh
        STEP 'is_open'
        call report
        db 'DONE', 0
        mov ax, 0x4c00
        int 21h

; report: prints "R <name> <AX> <CF>" for the caller's AX and CF; the
; name's bytes follow the call, and it returns past them.
report: pushf
        push ax
        mov [saved_ax], ax
        pushf
        pop ax
        and al, 1
        add al, '0'
        mov [saved_cf], al
        mov si, prefix
        call puts
        push bp
        mov bp, sp
        mov si, [bp + 6]
        call puts
        inc si
        mov [bp + 6], si
        pop bp
        mov al, ' '
        call putc
        mov ax, [saved_ax]
        call hex4
        mov al, ' '
        call putc
        mov al, [saved_cf]
        call putc
        mov al, 13
        call putc
        mov al, 10
        call putc
        pop ax
        popf
        ret

puts:   lodsb
        test al, al
        jz .end
        call putc
        jmp puts
.end:   dec si
        ret

putc:   push ax
        push dx
        cmp al, 13
        je .screen
        out DEBUG_PORT, al
.screen: mov dl, al
        mov ah, 2
        int 21h
        pop dx
        pop ax
        ret

hex4:   push ax
        mov al, ah
        call hex2
        pop ax
hex2:   push ax
        shr al, 4
        call hex1
        pop ax
hex1:   and al, 15
        add al, '0'
        cmp al, '9'
        jbe .digit
        add al, 7
.digit: jmp putc

prefix  db 'R ', 0
name1   db 'C:\SHRREPRO.TMP', 0
name2   db 'C:\SHRREPR2.TMP', 0
absent  db 'C:\SHRABSNT.TMP', 0
saved_ax dw 0
saved_cf db 0
