bits 16
cpu 386
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
 mov es,ax
 mov ss,ax
 mov sp,0x7000
 mov word [0x8002],7
 mov word [0x8011],7
 jmp short ram_start
ram_start:
 mov al,0
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov bx,0x8002
 align 2
.loop:
 imul dx,word [bx],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .loop
 cmp dx,0xffeb
 jne fail
 mov al,1
 out 0x80,al
 jmp short odd_start
odd_start:
 mov al,2
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov bx,0x8011
 align 2
.loop:
 imul dx,word [bx],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .loop
 cmp dx,0xffeb
 jne fail
 mov al,3
 out 0x80,al
 mov ax,0xa000
 mov ds,ax
 xor bx,bx
 mov word [bx],7
 jmp short mmio_start
mmio_start:
 mov al,4
 out 0x80,al
 mov bp,1000
 mov ax,7
 xor bx,bx
 align 2
.loop:
 imul dx,word [bx],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .loop
 cmp dx,0xffeb
 jne fail
 mov al,5
 out 0x80,al
 mov ax,0x9001
 mov ds,ax
 mov bx,0xffef
 mov byte [bx],7
 mov byte [bx+1],0
 jmp short cross_start
cross_start:
 mov al,6
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov bx,0xffef
 align 2
.loop:
 imul dx,word [bx],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .loop
 cmp dx,0xffeb
 jne fail
 mov al,7
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
fail:
 mov al,1
 out 0x8f,al
 hlt
