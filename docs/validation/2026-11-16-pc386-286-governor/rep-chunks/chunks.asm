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
        mov es,ax
        mov dx,tickisr
        mov ax,0x251c
        int 0x21
        mov si,hdr
        call puts
        mov ax,es
        mov bx,ds
        cmp ax,bx
        jne invalid_memory
        mov si,msg_ram
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
        MEASURE msg_f, test_f
        MEASURE msg_g, test_g
        mov si,dstbuf
        mov cx,64
.verify: cmp byte [si],0xa5
        jne invalid_memory
        inc si
        loop .verify
        mov si,msg_verified
        call puts
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        mov ax,0x4c00
        int 0x21

invalid_memory:
        mov si,msg_bad
        call puts
        mov ax,0x4c01
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
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,24
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_b: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,25
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_c: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,26
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_d: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,27
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_e: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,52
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_f: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,53
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_g: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,64
        mov si,srcbuf
        mov di,dstbuf
        rep movsb
        dec word [dcnt]
        jne .inner
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
hdr:    db 'pc386 governor REP RAM probe',13,10,0
msg_ram: db 'DS_ES_MATCH',13,10,0
msg_bad: db 'BAD_SEGMENTS_OR_WRITE',13,10,0
msg_verified: db 'RAM_WRITE_VERIFIED',13,10,0
msg_bd: db 'BD=',0
msg_a: db 'A=',0
msg_b: db 'B=',0
msg_c: db 'C=',0
msg_d: db 'D=',0
msg_e: db 'E=',0
msg_f: db 'F=',0
msg_g: db 'G=',0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0

srcbuf: times 256 db 0xa5
dstbuf: times 256 db 0
