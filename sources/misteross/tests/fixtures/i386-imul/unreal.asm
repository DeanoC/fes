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
 mov word [0x8102],9
 mov ax,0x1000
 mov es,ax
 mov word [es:0x8002],7
 xor ax,ax
 mov es,ax
 jmp short real_start
real_start:
 mov al,0
 out 0x80,al
 mov bp,1000
 mov ax,7
 align 2
.real_loop:
 imul dx,word [0x8002],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .real_loop
 cmp dx,0xffeb
 jne fail
 mov al,1
 out 0x80,al
 lgdt [gdtr]
 mov eax,cr0
 or eax,1
 mov cr0,eax
 jmp 0x08:protected
protected:
 mov ax,0x10
 mov ds,ax
 mov eax,cr0
 and eax,0xfffffffe
 mov cr0,eax
 jmp 0:unreal
unreal:
 mov ax,ds
 cmp ax,0x10
 jne fail
 jmp short unreal_start
unreal_start:
 mov al,2
 out 0x80,al
 mov bp,1000
 mov ax,7
 align 2
.unreal_loop:
 imul dx,word [0x8002],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .unreal_loop
 cmp dx,0xffeb
 jne fail
 cmp word [0x8002],7
 jne fail
 cmp word [es:0x8102],9
 jne fail
 mov al,3
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
fail:
 mov al,1
 out 0x8f,al
 hlt
align 8, db 0
gdt:
 dq 0
 dq 0x00009b000000ffff
 dq 0x000093010000ffff
gdt_end:
gdtr:
 dw gdt_end-gdt-1
 dd gdt
