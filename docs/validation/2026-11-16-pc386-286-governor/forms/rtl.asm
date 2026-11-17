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
 mov si,0x8000
 mov dx,1
 mov cx,1
 align 2
.i0:
  cmp dx,[si]
  dec bp
  jnz .i0
 mov al,1
 out 0x80,al
 mov bp,1000
 mov al,2
 out 0x80,al
 mov bx,0x8000
 mov si,0x8000
 mov dx,1
 mov cx,1
 align 2
.i1:
  cmp dh,[si]
  dec bp
  jnz .i1
 mov al,3
 out 0x80,al
 mov bp,1000
 mov al,4
 out 0x80,al
 mov bx,0x8000
 mov si,0x8000
 mov dx,1
 mov cx,1
 align 2
.i2:
  pushf
 pop si
  dec bp
  jnz .i2
 mov al,5
 out 0x80,al
 mov bp,1000
 mov al,6
 out 0x80,al
 mov bx,0x8000
 mov si,0x8000
 mov dx,1
 mov cx,1
 align 2
.i3:
  jmp short .jump_done
 nop
.jump_done: nop
  dec bp
  jnz .i3
 mov al,7
 out 0x80,al
 mov bp,1000
 mov al,8
 out 0x80,al
 mov bx,0x8000
 mov si,0x8000
 mov dx,1
 mov cx,1
 align 2
.i4:
  jmp near .near_done
 nop
.near_done: nop
  dec bp
  jnz .i4
 mov al,9
 out 0x80,al
 mov bp,1000
 mov al,10
 out 0x80,al
 mov bx,0x8000
 mov si,0x8000
 mov dx,1
 mov cx,1
 align 2
.i5:
  call .callee
 jmp near .call_done
.callee: inc dx
 ret
.call_done: nop
  dec bp
  jnz .i5
 mov al,11
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
