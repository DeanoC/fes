bits 16
cpu 286
org 0x1000
cli
xor ax,ax
mov ss,ax
mov sp,0x7000

 mov ax,0x0
 mov ds,ax
 mov bx,0x8000
 mov cx,3
 mov bp,100
 mov al,0
 out 0x80,al
.i0:
 test word [bx],0x1234
 dec bp
 jnz .i0
 mov al,1
 out 0x80,al

 mov ax,0xa000
 mov ds,ax
 mov bx,0x0
 mov cx,3
 mov bp,100
 mov al,2
 out 0x80,al
.i1:
 test word [bx],0x1234
 dec bp
 jnz .i1
 mov al,3
 out 0x80,al

 mov ax,0x9fff
 mov ds,ax
 mov bx,0xf
 mov cx,3
 mov bp,100
 mov al,4
 out 0x80,al
.i2:
 test word [bx],0x1234
 dec bp
 jnz .i2
 mov al,5
 out 0x80,al

 mov ax,0x0
 mov ds,ax
 mov bx,0x8000
 mov cx,3
 mov bp,100
 mov al,6
 out 0x80,al
.i3:
 mul word [bx]
 dec bp
 jnz .i3
 mov al,7
 out 0x80,al

 mov ax,0xa000
 mov ds,ax
 mov bx,0x0
 mov cx,3
 mov bp,100
 mov al,8
 out 0x80,al
.i4:
 mul word [bx]
 dec bp
 jnz .i4
 mov al,9
 out 0x80,al

 mov ax,0x9fff
 mov ds,ax
 mov bx,0xf
 mov cx,3
 mov bp,100
 mov al,10
 out 0x80,al
.i5:
 mul word [bx]
 dec bp
 jnz .i5
 mov al,11
 out 0x80,al

 mov ax,0x0
 mov ds,ax
 mov bx,0x8000
 mov cx,3
 mov bp,100
 mov al,12
 out 0x80,al
.i6:
 shl word [bx],cl
 dec bp
 jnz .i6
 mov al,13
 out 0x80,al

 mov ax,0xa000
 mov ds,ax
 mov bx,0x0
 mov cx,3
 mov bp,100
 mov al,14
 out 0x80,al
.i7:
 shl word [bx],cl
 dec bp
 jnz .i7
 mov al,15
 out 0x80,al

 mov ax,0x9fff
 mov ds,ax
 mov bx,0xf
 mov cx,3
 mov bp,100
 mov al,16
 out 0x80,al
.i8:
 shl word [bx],cl
 dec bp
 jnz .i8
 mov al,17
 out 0x80,al

 mov ax,0x0
 mov ss,ax
 mov sp,0x7000
 mov cx,3
 mov bp,100
 mov al,18
 out 0x80,al
.i9:
 pusha
 add sp,16
 dec bp
 jnz .i9
 mov al,19
 out 0x80,al

 mov ax,0xa000
 mov ss,ax
 mov sp,0x1000
 mov cx,3
 mov bp,100
 mov al,20
 out 0x80,al
.i10:
 pusha
 add sp,16
 dec bp
 jnz .i10
 mov al,21
 out 0x80,al

 mov ax,0x9fff
 mov ss,ax
 mov sp,0x18
 mov cx,3
 mov bp,100
 mov al,22
 out 0x80,al
.i11:
 pusha
 add sp,16
 dec bp
 jnz .i11
 mov al,23
 out 0x80,al

 xor al,al
 out 0x8f,al
 hlt
