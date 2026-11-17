bits 16
cpu 286
org 0x1000
cli
xor ax,ax
mov ss,ax
mov sp,0x7000
mov ax,0
mov ds,ax
mov bx,0x8000
mov bp,1000
mov al,0
out 0x80,al
.ram:
xor cx,cx
shl word [bx],cl
dec bp
jnz .ram
mov al,1
out 0x80,al
mov ax,0xa000
mov ds,ax
xor bx,bx
mov bp,1000
mov al,2
out 0x80,al
.mmio:
xor cx,cx
shl word [bx],cl
dec bp
jnz .mmio
mov al,3
out 0x80,al
mov ax,0x9fff
mov ds,ax
mov bx,0xf
mov bp,1000
mov al,4
out 0x80,al
.cross:
xor cx,cx
shl word [bx],cl
dec bp
jnz .cross
mov al,5
out 0x80,al
xor al,al
out 0x8f,al
hlt
