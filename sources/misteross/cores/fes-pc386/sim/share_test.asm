; SPDX-License-Identifier: MIT
; SHRTEST.COM: delete and rename under SHARE (fes#1777). scripts/sim_fes_pc386.py
; --suite share runs it at the prompt of the games image's boot entry 4
; (SHARE loaded). Kernel 2046 asks SHARE (INT 2Fh AX=10A6h) before every
; delete and rename. With FreeDOS 1.4's SHARE 08/2006a, which lacks that call,
; every delete and rename failed with error 5. With a SHARE that answers it,
; the release kernel's stub returned with SI clobbered and the rename used a
; garbage path (FDOS/kernel#276). Each check prints
;   SHARE <name> ok
; (or FAIL with AX) to the screen and port E9h, and the run ends with
; "SHARE DONE ok" or "SHARE DONE FAIL":
;   create      INT 21h AH=3Ch C:\SHRTEST.TMP succeeds; the handle stays open
;   del-open    INT 21h AH=41h on the open file fails (SHARE: it is open)
;   ren-open    INT 21h AH=56h on the open file fails
;   close       INT 21h AH=3Eh
;   rename      AH=56h C:\SHRTEST.TMP to C:\SHRTEST2.TMP succeeds
;   delete      AH=41h C:\SHRTEST2.TMP succeeds
;   gone        AH=41h C:\SHRTEST2.TMP again returns 0002h (file not found)
;
; Assemble: nasm -f bin -o SHRTEST.COM share_test.asm.

        bits 16
        org 0x100

DEBUG_PORT equ 0xe9

start:  mov ah, 0x3c
        xor cx, cx
        mov dx, name1
        int 21h
        mov [handle], ax
        mov si, n_create
        call expect_ok

        mov ah, 0x41
        mov dx, name1
        int 21h
        mov si, n_delopen
        call expect_fail

        mov ah, 0x56
        mov dx, name1
        mov di, name2
        int 21h
        mov si, n_renopen
        call expect_fail

        mov ah, 0x3e
        mov bx, [handle]
        int 21h
        mov si, n_close
        call expect_ok

        mov ah, 0x56
        mov dx, name1
        mov di, name2
        int 21h
        mov si, n_rename
        call expect_ok

        mov ah, 0x41
        mov dx, name2
        int 21h
        mov si, n_delete
        call expect_ok

        mov ah, 0x41
        mov dx, name2
        int 21h
        mov si, n_gone
        jnc .gone_bad
        cmp ax, 2
        jne .gone_bad
        call pass
        jmp .done
.gone_bad:
        call fail

.done:  mov si, prefix
        call puts
        mov si, n_done
        call puts
        mov si, s_ok
        cmp byte [failed], 0
        je .last
        mov si, s_fail
.last:  call puts
        call newline
        mov ax, 0x4c00
        int 21h

; expect_ok / expect_fail: CF from the call just made; SI names the check.
expect_ok:
        jc fail
        jmp pass
expect_fail:
        jnc fail
        jmp pass

pass:   push si
        mov si, prefix
        call puts
        pop si
        call puts
        mov si, s_ok
        call puts
        jmp newline

fail:   mov byte [failed], 1
        push ax
        push si
        mov si, prefix
        call puts
        pop si
        call puts
        mov si, s_failax
        call puts
        pop ax
        call hex4
        jmp newline

newline:
        mov al, 13
        call putc
        mov al, 10
        jmp putc

puts:   lodsb
        test al, al
        jz .end
        call putc
        jmp puts
.end:   ret

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

prefix    db 'SHARE ', 0
n_create  db 'create', 0
n_delopen db 'del-open', 0
n_renopen db 'ren-open', 0
n_close   db 'close', 0
n_rename  db 'rename', 0
n_delete  db 'delete', 0
n_gone    db 'gone', 0
n_done    db 'DONE', 0
s_ok      db ' ok', 0
s_fail    db ' FAIL', 0
s_failax  db ' FAIL AX=', 0
name1     db 'C:\SHRTEST.TMP', 0
name2     db 'C:\SHRTEST2.TMP', 0
handle    dw 0
failed    db 0
