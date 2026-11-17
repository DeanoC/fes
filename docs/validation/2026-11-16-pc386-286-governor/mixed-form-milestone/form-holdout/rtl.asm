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
 mov word [0x8000],1
 mov word [0x8002],0
 mov word [0x8000],0x5aa5
 mov word [0x9000],0
 mov si,0x8000
 mov di,0x9000
 movsw
 cmp word [0x9000],0x5aa5
 jne fail
 mov bp,1000
 mov al,0
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i0:
  lodsb
  dec bp
  jnz .i0
 mov al,1
 out 0x80,al
 mov bp,1000
 mov al,2
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i1:
  lodsw
  dec bp
  jnz .i1
 mov al,3
 out 0x80,al
 mov bp,1000
 mov al,4
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i2:
  stosb
  dec bp
  jnz .i2
 mov al,5
 out 0x80,al
 mov bp,1000
 mov al,6
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i3:
  stosw
  dec bp
  jnz .i3
 mov al,7
 out 0x80,al
 mov bp,1000
 mov al,8
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i4:
  lodsb
 stosb
  dec bp
  jnz .i4
 mov al,9
 out 0x80,al
 mov bp,1000
 mov al,10
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i5:
  lodsb
 xor al,0x55
 stosb
  dec bp
  jnz .i5
 mov al,11
 out 0x80,al
 mov bp,1000
 mov al,12
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i6:
  test ax,8
  dec bp
  jnz .i6
 mov al,13
 out 0x80,al
 mov bp,1000
 mov al,14
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i7:
  test al,8
  dec bp
  jnz .i7
 mov al,15
 out 0x80,al
 mov bp,1000
 mov al,16
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i8:
  mov ax,[bx]
 mov dx,7
 mul dx
 test ax,8
  dec bp
  jnz .i8
 mov al,17
 out 0x80,al
 mov bp,1000
 mov al,18
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i9:
  mov ax,[bx]
 mov dx,7
 mul dx
  dec bp
  jnz .i9
 mov al,19
 out 0x80,al
 mov bp,1000
 mov al,20
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i10:
  enter 0,0
 leave
  dec bp
  jnz .i10
 mov al,21
 out 0x80,al
 mov bp,1000
 mov al,22
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i11:
  enter 6,0
 leave
  dec bp
  jnz .i11
 mov al,23
 out 0x80,al
 mov bp,1000
 mov al,24
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i12:
  enter 6,0
 mov sp,bp
 pop bp
  dec bp
  jnz .i12
 mov al,25
 out 0x80,al
 mov bp,1000
 mov al,26
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i13:
  push bp
 mov bp,sp
 leave
  dec bp
  jnz .i13
 mov al,27
 out 0x80,al
 mov bp,1000
 mov al,28
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i14:
  enter 6,1
 leave
  dec bp
  jnz .i14
 mov al,29
 out 0x80,al
 mov bp,1000
 mov al,30
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 mov si,0x8000
 mov di,0x9000
 mov ax,0x5aa5
 align 2
.i15:
  mov cx,2
 rep stosb
  dec bp
  jnz .i15
 mov al,31
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt

fail:
 mov al,0xee
 out 0x8f,al
 hlt
