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
        mov si,dstbuf
        mov cx,28
.verify_first_movs:
        cmp byte [si],0xa5
        jne invalid_memory
        inc si
        loop .verify_first_movs
        cmp byte [dstbuf+28],0
        jne invalid_memory
        mov si,msg_movs_verified
        call puts
        MEASURE msg_b, test_b
        MEASURE msg_c, test_c
        MEASURE msg_d, test_d
        mov di,dstbuf
        mov cx,64
        xor ax,ax
        rep stosb
        MEASURE msg_e, test_e
        cmp byte [dstbuf],0xa5
        jne invalid_memory
        cmp byte [dstbuf+1],0
        jne invalid_memory
        mov si,msg_stos_verified
        call puts
        MEASURE msg_f, test_f
        MEASURE msg_g, test_g
        MEASURE msg_h, test_h
        MEASURE msg_i, test_i
        MEASURE msg_j, test_j
        MEASURE msg_k, test_k
        MEASURE msg_l, test_l
        MEASURE msg_m, test_m
        MEASURE msg_n, test_n
        MEASURE msg_o, test_o
        MEASURE msg_p, test_p
        mov si,dstbuf
        mov cx,32
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
.inner: mov cx,28
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_b: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,40
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_c: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,54
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_d: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,80
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep movsb
        dec word [dcnt]
        jne .inner
        FINISH
test_e: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,1
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep stosb
        dec word [dcnt]
        jne .inner
        FINISH
test_f: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,8
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep stosb
        dec word [dcnt]
        jne .inner
        FINISH
test_g: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,22
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep stosb
        dec word [dcnt]
        jne .inner
        FINISH
test_h: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,40
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep stosb
        dec word [dcnt]
        jne .inner
        FINISH
test_i: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,1
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep lodsb
        dec word [dcnt]
        jne .inner
        FINISH
test_j: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,8
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep lodsb
        dec word [dcnt]
        jne .inner
        FINISH
test_k: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,22
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep lodsb
        dec word [dcnt]
        jne .inner
        FINISH
test_l: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,40
        mov si,srcbuf
        mov di,dstbuf
        mov al,165
        rep lodsb
        dec word [dcnt]
        jne .inner
        FINISH
test_m: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,1
        mov si,srcbuf
        mov di,dstbuf
        mov al,0
        repne scasb
        dec word [dcnt]
        jne .inner
        FINISH
test_n: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,8
        mov si,srcbuf
        mov di,dstbuf
        mov al,0
        repne scasb
        dec word [dcnt]
        jne .inner
        FINISH
test_o: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,14
        mov si,srcbuf
        mov di,dstbuf
        mov al,0
        repne scasb
        dec word [dcnt]
        jne .inner
        FINISH
test_p: call t_start
.block: mov word [dcnt],1000
        align 2
.inner: mov cx,28
        mov si,srcbuf
        mov di,dstbuf
        mov al,0
        repne scasb
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
msg_movs_verified: db 'FIRST_MOVS_WRITE_VERIFIED',13,10,0
msg_stos_verified: db 'FIRST_STOS_WRITE_VERIFIED',13,10,0
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
msg_h: db 'H=',0
msg_i: db 'I=',0
msg_j: db 'J=',0
msg_k: db 'K=',0
msg_l: db 'L=',0
msg_m: db 'M=',0
msg_n: db 'N=',0
msg_o: db 'O=',0
msg_p: db 'P=',0
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
