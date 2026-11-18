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
 mov word [0x8002],7
 mov word [0x8007],7
 mov al,0
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i0:
 imul dx,si, strict word -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i0
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,1
 out 0x80,al
 mov al,2
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i1:
 imul dx,si, strict byte -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i1
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,3
 out 0x80,al
 mov al,4
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i2:
 imul dx,si, strict word 255
 times 24 add ax, strict word 0
 dec bp
 jnz short .i2
 cmp dx,0x06f9
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,5
 out 0x80,al
 mov al,6
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i3:
 imul dx,si, strict byte -128
 times 24 add ax, strict word 0
 dec bp
 jnz short .i3
 cmp dx,0xfc80
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,7
 out 0x80,al
 mov al,8
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i4:
 imul dx,si, strict word -32768
 times 24 add ax, strict word 0
 dec bp
 jnz short .i4
 cmp dx,0x8000
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,9
 out 0x80,al
 mov al,10
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i5:
 imul dx,si, strict byte 127
 times 24 add ax, strict word 0
 dec bp
 jnz short .i5
 cmp dx,0x0379
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,11
 out 0x80,al
 mov al,12
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,2
 mov ax,7
 xor dx,dx
 align 2
.i6:
 imul dx,word [bx+si], strict word -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i6
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,13
 out 0x80,al
 mov al,14
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,2
 mov ax,7
 xor dx,dx
 align 2
.i7:
 imul dx,word [bx+si], strict byte -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i7
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,15
 out 0x80,al
 mov al,16
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,2
 mov ax,7
 xor dx,dx
 align 2
.i8:
 imul dx,word [bx+si+5], strict word -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i8
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,17
 out 0x80,al
 mov al,18
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,2
 mov ax,7
 xor dx,dx
 align 2
.i9:
 imul dx,word [bx+si+5], strict byte -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i9
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,19
 out 0x80,al
 mov al,20
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,2
 mov ax,7
 xor dx,dx
 align 2
.i10:
 imul dx,word [0x8002], strict byte -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i10
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,21
 out 0x80,al
 mov al,22
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,7
 mov ax,7
 xor dx,dx
 align 2
.i11:
 es imul dx,si, strict byte -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i11
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,23
 out 0x80,al
 mov al,24
 out 0x80,al
 mov bp,1000
 mov bx,0x8000
 mov si,2
 mov ax,7
 xor dx,dx
 align 2
.i12:
 es imul dx,word [bx+si], strict byte -3
 times 24 add ax, strict word 0
 dec bp
 jnz short .i12
 cmp dx,0xffeb
 jne fail
 cmp ax,7
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [0x8007],7
 jne fail
 mov al,25
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
fail: mov al,1
 out 0x8f,al
 hlt
