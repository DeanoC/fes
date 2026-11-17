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
 align 2
.i0:
  xchg al,[bx]
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
 align 2
.i1:
  xchg ax,[bx]
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
 align 2
.i2:
  test byte [bx],0x55
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
 align 2
.i3:
  test word [bx],0x1234
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
 align 2
.i4:
  not byte [bx]
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
 align 2
.i5:
  neg word [bx]
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
 align 2
.i6:
  inc byte [bx]
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
 align 2
.i7:
  dec word [bx]
  dec bp
  jnz .i7
 mov al,15
 out 0x80,al
 mov bp,1000
 mov al,16
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 align 2
.i8:
  add byte [bx],3
  dec bp
  jnz .i8
 mov al,17
 out 0x80,al
 mov bp,1000
 mov al,18
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 align 2
.i9:
  add word [bx],3
  dec bp
  jnz .i9
 mov al,19
 out 0x80,al
 mov bp,1000
 mov al,20
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 align 2
.i10:
  adc ax,[bx]
  dec bp
  jnz .i10
 mov al,21
 out 0x80,al
 mov bp,1000
 mov al,22
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 align 2
.i11:
  sub [bx],dx
  dec bp
  jnz .i11
 mov al,23
 out 0x80,al
 mov bp,1000
 mov al,24
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 align 2
.i12:
  push word [bx]
 pop si
  dec bp
  jnz .i12
 mov al,25
 out 0x80,al
 mov bp,1000
 mov al,26
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,1
 align 2
.i13:
  push dx
 pop word [bx]
  dec bp
  jnz .i13
 mov al,27
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
