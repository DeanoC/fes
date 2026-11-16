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
        MEASURE msg_o1, order1
        MEASURE msg_o2, order2
        MEASURE msg_p0, prefix0
        MEASURE msg_p1, prefix1
        MEASURE msg_p3, prefix3
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
order1: call t_start
.block: mov cx,1000
        align 2
.inner: inc word [target]
        cmp word [zero],0
        loop .inner
        FINISH
order2: call t_start
.block: mov cx,1000
        align 2
.inner: cmp word [zero],0
        inc word [target]
        loop .inner
        FINISH
%macro PREFIX_TEST 2
%1:     call t_start
.block: mov word [dcnt],1000
        align 2
.inner:
%rep %2
        db 0x3e
%endrep
        cmp word [zero],0
        dec word [dcnt]
        jne .inner
        FINISH
%endmacro
        PREFIX_TEST prefix0,0
        PREFIX_TEST prefix1,1
        PREFIX_TEST prefix3,3

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
msg_o1: db 'O1=',0
msg_o2: db 'O2=',0
msg_p0: db 'P0=',0
msg_p1: db 'P1=',0
msg_p3: db 'P3=',0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0
