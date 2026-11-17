bits 16
cpu 386
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
 mov fs,ax
 mov es,ax
 mov ss,ax
 mov sp,0x7000
 mov word [0x8000],1
 mov word [0x8002],0
 mov bp,1000
 mov al,0
 out 0x80,al
 xor ebx,ebx
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i0:
  test byte [bx],0x55
  nop
  dec bp
  jnz .i0
 mov al,1
 out 0x80,al
 mov bp,1000
 mov al,2
 out 0x80,al
 xor ebx,ebx
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i1:
  a32 test byte [ebx],0x55
  nop
  dec bp
  jnz .i1
 mov al,3
 out 0x80,al
 mov bp,1000
 mov al,4
 out 0x80,al
 xor ebx,ebx
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i2:
  db 0x66
 test byte [bx],0x55
  nop
  dec bp
  jnz .i2
 mov al,5
 out 0x80,al
 mov bp,1000
 mov al,6
 out 0x80,al
 xor ebx,ebx
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i3:
  fs test byte [bx],0x55
  nop
  dec bp
  jnz .i3
 mov al,7
 out 0x80,al
 mov bp,1000
 mov al,8
 out 0x80,al
 xor ebx,ebx
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i4:
  db 0x64,0x3e
 test byte [bx],0x55
  nop
  dec bp
  jnz .i4
 mov al,9
 out 0x80,al
 mov bp,1000
 mov al,10
 out 0x80,al
 xor ebx,ebx
 mov bx,0x8000
 mov dx,1
 mov cx,1
 xor si,si
 align 2
.i5:
  ds test byte [bx],0x55
  nop
  dec bp
  jnz .i5
 mov al,11
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
