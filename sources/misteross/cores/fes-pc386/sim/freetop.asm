; SPDX-License-Identifier: MIT
; FREETOP.COM: runs a program with the top of conventional memory free.
;
;   FREETOP program [arguments]      e.g. FREETOP ULTIMA7.COM
;
; FreeCOM 0.86 keeps its master environment in the last memory block (it moves
; the kernel's 128-byte environment last fit, and with kernel swap keeps a
; copy there while a program runs). A program that measures DOS memory by
; adding the last two blocks of the memory chain, as Ultima VII's MAINMENU.EXE
; does, then sees only the free memory above itself. FREETOP saves that block
; when its owner is FREETOP's parent (the shell) and it is at most 2 KiB, frees
; it, runs the program, then allocates it again last fit at the same segment
; and restores its bytes, owner and name. The shell does not use the block
; while its child runs.
;
; program is a file name with its extension, found as DOS EXEC finds it (no
; PATH search). The exit code is the program's. A message reports a program
; that cannot be run or a block that could not be put back.
; Assemble: nasm -f bin -o FREETOP.COM freetop.asm. 8086 code.

        bits 16
        cpu 8086
        org 0x100

SAVE_PARAS equ 0x80                     ; 2 KiB

start:  mov sp, stack_top
        mov ah, 0x4a                    ; keep 4 KiB (code, save area, stack)
        mov bx, 0x100
        int 0x21
        call parse
        jnc .named
        mov dx, msg_usage
        mov ah, 0x09
        int 0x21
        mov ax, 0x4c01
        int 0x21
.named: mov ah, 0x52                    ; the last block of the chain
        int 0x21
        mov ax, [es:bx-2]
.walk:  mov es, ax
        cmp byte [es:0], 'Z'
        je .last
        add ax, [es:3]
        inc ax
        jmp .walk
.last:  mov bx, [es:1]                  ; owned by our parent, the shell?
        cmp bx, [0x16]
        jne run
        mov cx, [es:3]
        cmp cx, SAVE_PARAS
        ja run
        mov [blk_owner], bx
        mov [blk_paras], cx
        mov ax, es
        inc ax
        mov [blk_seg], ax
        push ds                         ; save the MCB's name
        push es
        pop ds
        mov si, 8
        push cs
        pop es
        mov di, blk_name
        mov cx, 4
        rep movsw
        pop ds
        mov cx, [blk_paras]             ; and the block's bytes
        shl cx, 1
        shl cx, 1
        shl cx, 1
        mov ds, [blk_seg]
        xor si, si
        mov di, save
        rep movsw
        push cs
        pop ds
        mov es, [blk_seg]
        mov ah, 0x49
        int 0x21
        jc run
        mov byte [moved], 1

run:    mov ax, cs
        mov [tail+2], ax
        mov [fcb1+2], ax
        mov [fcb2+2], ax
        mov es, ax
        mov ax, 0x4b00
        mov dx, prog
        mov bx, parm
        int 0x21
        cli                             ; SS:SP may not survive EXEC on DOS 2
        mov bx, cs
        mov ss, bx
        mov sp, stack_top
        sti
        mov ds, bx
        mov es, bx
        jnc .ran
        mov dx, msg_exec
        mov ah, 0x09
        int 0x21
        mov byte [rc], 1
        jmp restore
.ran:   mov ah, 0x4d
        int 0x21
        mov [rc], al

restore:
        cmp byte [moved], 0
        je done
        mov ax, 0x5800                  ; last fit puts it back at the top
        int 0x21
        mov [strategy], ax
        mov ax, 0x5801
        mov bx, 2
        int 0x21
        mov ah, 0x48
        mov bx, [blk_paras]
        int 0x21
        mov [new_seg], ax
        pushf
        mov ax, 0x5801
        mov bx, [strategy]
        int 0x21
        popf
        jc .lost
        mov ax, [new_seg]
        cmp ax, [blk_seg]
        jne .lost
        mov es, ax
        xor di, di
        mov si, save
        mov cx, [blk_paras]
        shl cx, 1
        shl cx, 1
        shl cx, 1
        rep movsw
        dec ax
        mov es, ax
        mov ax, [blk_owner]
        mov [es:1], ax
        mov si, blk_name
        mov di, 8
        mov cx, 4
        rep movsw
        jmp done
.lost:  mov dx, msg_lost
        mov ah, 0x09
        int 0x21
        mov byte [rc], 1
done:   mov ah, 0x4c
        mov al, [rc]
        int 0x21

; The command tail: the program's name into prog, the rest into the child's
; tail (count, text, CR). CF when no name is given.
parse:  mov si, 0x81
        mov cl, [0x80]
        xor ch, ch
        cld
.skip:  jcxz .none
        lodsb
        dec cx
        cmp al, ' '
        je .skip
        cmp al, 9
        je .skip
        cmp al, 13
        je .none
        mov di, prog
.name:  stosb
        cmp di, prog + 79
        jae .none
        jcxz .end
        lodsb
        dec cx
        cmp al, ' '
        je .rest
        cmp al, 9
        je .rest
        cmp al, 13
        je .end
        jmp .name
.rest:  dec si                          ; keep the separator
        inc cx
.end:   mov byte [di], 0
        mov di, child_tail + 1
        mov [child_tail], cl
        rep movsb
        mov byte [di], 13
        clc
        ret
.none:  stc
        ret

msg_usage db 'usage: FREETOP program [arguments]', 13, 10, '$'
msg_exec  db 'FREETOP: cannot run the program', 13, 10, '$'
msg_lost  db 'FREETOP: could not put the shell', 39, 's block back', 13, 10, '$'
parm      dw 0                          ; the parent's environment
tail      dw child_tail, 0
fcb1      dw 0x5c, 0
fcb2      dw 0x6c, 0
moved     db 0
rc        db 0
strategy  dw 0
new_seg   dw 0
blk_owner dw 0
blk_paras dw 0
blk_seg   dw 0
blk_name  times 8 db 0
prog      times 80 db 0
child_tail times 129 db 0
save      times SAVE_PARAS * 16 db 0
          times 256 db 0
stack_top:
