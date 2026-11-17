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
 align 2
.i0:
  xor cx,cx
 rol dl,cl
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
 align 2
.i1:
  mov cl,32
 sar dx,cl
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
 align 2
.i2:
  xor cx,cx
 shr byte [bx],cl
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
 align 2
.i3:
  mov cl,32
 rcr word [bx],cl
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
 align 2
.i4:
  xor cx,cx
 shr word [bx],cl
 sar byte [bx],cl
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
 align 2
.i5:
  xor cx,cx
 shl word [bx],cl
 inc cx
 shr byte [bx],cl
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
 align 2
.i6:
  mov cl,1
 rol byte [bx],cl
 dec cx
 sar word [bx],cl
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
 align 2
.i7:
  xor cx,cx
 shl word [bx],cl
 nop
 nop
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
 align 2
.i8:
  xor cx,cx
 shl word [bx+1],cl
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
 align 2
.i9:
  xor cx,cx
 rcl byte [bx],cl
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
 align 2
.i10:
  shr word [bx],0
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
 align 2
.i11:
  xor cx,cx
 shr word [bx+si+3],cl
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
 align 2
.i12:
  xor cx,cx
 ds shl word [bx],cl
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
