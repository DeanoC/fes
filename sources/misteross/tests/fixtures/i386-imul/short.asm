bits 16
cpu 286
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
 mov es,ax
 mov ss,ax
 mov sp,0x7000
 mov word [0x8000],7
 mov al,0
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i0:
 db 0x69,0xc0,0x01,0x00
 dec bp
 jnz .i0
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,1
 out 0x80,al
 mov al,2
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i1:
 db 0x6b,0xc0,0x01
 dec bp
 jnz .i1
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,3
 out 0x80,al
 mov al,4
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i2:
 db 0x69,0x07,0x01,0x00
 dec bp
 jnz .i2
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,5
 out 0x80,al
 mov al,6
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i3:
 db 0x6b,0x07,0x01
 dec bp
 jnz .i3
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,7
 out 0x80,al
 mov al,8
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i4:
 db 0x69,0xc0,0x01,0x00,0x90,0x90,0x90
 dec bp
 jnz .i4
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,9
 out 0x80,al
 mov al,10
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i5:
 db 0x6b,0xc0,0x01,0x90,0x90,0x90
 dec bp
 jnz .i5
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,11
 out 0x80,al
 mov al,12
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i6:
 db 0x69,0x07,0x01,0x00,0x90,0x90,0x90
 dec bp
 jnz .i6
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,13
 out 0x80,al
 mov al,14
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov ax,7
 align 2
.i7:
 db 0x6b,0x07,0x01,0x90,0x90,0x90
 dec bp
 jnz .i7
 cmp ax,7
 jne fail
 cmp word [0x8000],7
 jne fail
 mov al,15
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
fail: mov al,1
 out 0x8f,al
 hlt
