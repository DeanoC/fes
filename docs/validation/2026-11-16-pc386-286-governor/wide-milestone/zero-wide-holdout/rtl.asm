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
 mov bx,0x8000
 mov word [bx],0x5aa5
 xor cx,cx
 stc
 shl word [bx],cl
 pushf
 pop dx
 and dx,0x41
 cmp dx,0x41
 jne fail
 cmp word [bx],0x5aa5
 jne fail
 mov bp,1000
 mov al,0
 out 0x80,al
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i0:
  xor cx,cx
 shl word [bx+si+5],cl
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i1:
  xor cx,cx
 shr byte [bx+di+0x1234],cl
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i2:
  shr dx,0
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i3:
  rol byte [bx],0
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i4:
  shl byte [bx],0
 sar word [bx],0
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i5:
  xor cx,cx
 ds shl word [bx],cl
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i6:
  xor cx,cx
 db 0x26,0x3e
 shr word [bx],cl
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
 mov cx,3
 mov word [bx],7
 xor si,si
 xor di,di
 align 2
.i7:
  ds shr word [bx],0
  nop
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
 xor si,si
 xor di,di
 align 2
.i8:
  xor cx,cx
 ds shl word [bx],cl
 ds shr byte [bx],cl
  nop
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
 xor si,si
 xor di,di
 align 2
.i9:
  shr word [bx],0
 mov cl,1
 shl byte [bx],cl
  nop
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
 xor si,si
 xor di,di
 align 2
.i10:
  shr word [bx],0
 jmp $+2
  nop
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
 xor si,si
 xor di,di
 align 2
.i11:
  xor cx,cx
 ds rol dx,cl
  nop
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
 xor si,si
 xor di,di
 align 2
.i12:
  mov cl,32
 sar word [bx+si+3],cl
  nop
  dec bp
  jnz .i12
 mov al,25
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt

fail:
 mov al,0xee
 out 0x8f,al
 hlt
