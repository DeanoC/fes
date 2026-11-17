; Reference-only pc386 governor discriminator. See method.md.
; Every benchmark emits completed 1000-iteration blocks in 64 BIOS ticks.
; NASM -f bin -o GOVPROBE.COM govprobe.asm
        cpu 8086
        bits 16
        org 0x100
start:  cld
        mov ax,cs
        mov ds,ax
        mov es,ax
        mov ax,0x351c
        int 0x21
        mov [oldoff],bx
        mov [oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,tickisr
        mov ax,0x251c
        int 0x21
        mov si,hdr
        call puts
%macro MEASURE 2
        mov si,%1
        call puts
        call %2
        mov ax,[blocks]
        call putu
        call crlf
%endmacro
        MEASURE msg_bd, bd
        MEASURE msg_a, test_a
        MEASURE msg_b, test_b
        MEASURE msg_c, test_c
        MEASURE msg_d, test_d
        MEASURE msg_e, test_e
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        mov ax,0x4c00
        int 0x21

t_start:
        mov word [blocks],0
        mov word [tick],0
.sync:  cmp word [tick],0
        je .sync
        mov word [tick],0
        ret
%macro FINISH 0
        inc word [blocks]
        cmp word [tick],64
        jb .block
        ret
%endmacro
bd:     call t_start
.block: mov word [dcnt],1000
        align 2
.inner: dec word [dcnt]
        jne .inner
        FINISH
test_a: call t_start
.block: mov cx,1000
        align 2
.inner: inc word [target]
        nop
        cmp word [zero],0
        loop .inner
        FINISH
test_b: call t_start
.block: mov cx,1000
        align 2
.inner: inc word [target]
        nop
        nop
        nop
        cmp word [zero],0
        loop .inner
        FINISH
test_c: call t_start
.block: mov cx,1000
        align 2
.inner: cmp word [zero],0
        nop
        inc word [target]
        loop .inner
        FINISH
test_d: call t_start
.block: mov cx,1000
        mov bx,target
        align 2
.inner: inc word [bx]
        cmp word [zero],0
        loop .inner
        FINISH
test_e: call t_start
.block: mov cx,1000
        mov bx,target
        align 2
.inner: cmp word [zero],0
        inc word [bx]
        loop .inner
        FINISH

tickisr: push ds
        push cs
        pop ds
        inc word [tick]
        pop ds
        iret
putc:   push dx
        mov dl,al
        mov ah,2
        int 0x21
        pop dx
        ret
crlf:   mov al,13
        call putc
        mov al,10
        call putc
        ret
puts:   lodsb
        or al,al
        jz .done
        call putc
        jmp puts
.done:  ret
putu:   push bx
        push cx
        push dx
        mov bx,10
        xor cx,cx
.d1:    xor dx,dx
        div bx
        push dx
        inc cx
        or ax,ax
        jnz .d1
.d2:    pop ax
        add al,'0'
        call putc
        loop .d2
        pop dx
        pop cx
        pop bx
        ret
hdr:    db 'pc386 governor probe v1',13,10,0
msg_bd: db 'BD=',0
msg_a: db 'A=',0
msg_b: db 'B=',0
msg_c: db 'C=',0
msg_d: db 'D=',0
msg_e: db 'E=',0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0
