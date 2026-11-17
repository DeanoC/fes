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
 mov bp,1000
 mov al,0
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i0:
  test [bx],al
  nop
  dec bp
  jnz .i0
 mov al,1
 out 0x80,al
 mov bp,1000
 mov al,2
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i1:
  test [bx],ax
  nop
  dec bp
  jnz .i1
 mov al,3
 out 0x80,al
 mov bp,1000
 mov al,4
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i2:
  test byte [bx+si],0x5a
  nop
  dec bp
  jnz .i2
 mov al,5
 out 0x80,al
 mov bp,1000
 mov al,6
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i3:
  test word [bx+si],0x5678
  nop
  dec bp
  jnz .i3
 mov al,7
 out 0x80,al
 mov bp,1000
 mov al,8
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i4:
  test byte [0x8000],0x5a
  nop
  dec bp
  jnz .i4
 mov al,9
 out 0x80,al
 mov bp,1000
 mov al,10
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i5:
  test word [0x8000],0x5678
  nop
  dec bp
  jnz .i5
 mov al,11
 out 0x80,al
 mov bp,1000
 mov al,12
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i6:
  test [bx+si],dl
  nop
  dec bp
  jnz .i6
 mov al,13
 out 0x80,al
 mov bp,1000
 mov al,14
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i7:
  test [bx+si],dx
  nop
  dec bp
  jnz .i7
 mov al,15
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
