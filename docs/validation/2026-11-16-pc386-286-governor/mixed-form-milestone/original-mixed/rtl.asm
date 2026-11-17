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
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i0:
  mov si,0x8000
 mov di,0x9000
 mov cx,16
 rep movsw
  dec bp
  jnz .i0
 mov al,1
 out 0x80,al
 mov bp,1000
 mov al,2
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i1:
  mov di,0x9000
 mov cx,16
 mov ax,0x3939
 rep stosw
  dec bp
  jnz .i1
 mov al,3
 out 0x80,al
 mov bp,1000
 mov al,4
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i2:
  mov di,0x9000
 mov cx,16
 mov al,0xfe
 repne scasb
  dec bp
  jnz .i2
 mov al,5
 out 0x80,al
 mov bp,1000
 mov al,6
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i3:
  mov si,0x8000
 mov di,0x9000
 mov cx,16
 repe cmpsw
  dec bp
  jnz .i3
 mov al,7
 out 0x80,al
 mov bp,1000
 mov al,8
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i4:
  mov ax,[bx]
 xor ax,0x5555
 rol ax,1
 mov [bx+2],ax
 inc bx
 and bx,0x83ff
  dec bp
  jnz .i4
 mov al,9
 out 0x80,al
 mov bp,1000
 mov al,10
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i5:
  mov ax,[bx]
 add ax,[bx+2]
 adc dx,0
 mov [bx+4],ax
 add bx,2
 and bx,0x83fe
  dec bp
  jnz .i5
 mov al,11
 out 0x80,al
 mov bp,1000
 mov al,12
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i6:
  test byte [bx],1
 jz $+4
 inc dx
 inc dx
 inc bx
 and bx,0x83ff
  dec bp
  jnz .i6
 mov al,13
 out 0x80,al
 mov bp,1000
 mov al,14
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i7:
  push ax
 push bx
 call $+5
 jmp $+4
 inc dx
 ret
 pop bx
 pop ax
  dec bp
  jnz .i7
 mov al,15
 out 0x80,al
 mov bp,1000
 mov al,16
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i8:
  lodsb
 xor al,0x55
 stosb
  dec bp
  jnz .i8
 mov al,17
 out 0x80,al
 mov bp,1000
 mov al,18
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i9:
  mov cx,16
 loop $
  dec bp
  jnz .i9
 mov al,19
 out 0x80,al
 mov bp,1000
 mov al,20
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i10:
  mov ax,[bx]
 mov dx,7
 mul dx
 test ax,8
 jz $+4
 inc bx
 dec bx
  dec bp
  jnz .i10
 mov al,21
 out 0x80,al
 mov bp,1000
 mov al,22
 out 0x80,al
 mov di,0x8000
 mov ax,0x1111
 mov cx,512
 rep stosw
 mov word [0x8000],7
 mov si,0x8000
 mov di,0x9000
 mov cx,512
 rep movsw
 mov bx,0x8000
 mov dx,1
 mov cx,3
 mov si,0x8000
 mov di,0x9000
 align 2
.i11:
  enter 6,0
 mov ax,[bp-2]
 leave
  dec bp
  jnz .i11
 mov al,23
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt

fail:
 mov al,0xee
 out 0x8f,al
 hlt
