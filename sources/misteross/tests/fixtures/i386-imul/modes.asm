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

 jmp short real_start
real_start:
 mov al,0
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov esi,7
 mov ebx,0x8002
 xor dx,dx
 align 2
.real_loop:
 imul dx,si,strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .real_loop
 cmp dx,0xffeb
 jne fail
 mov al,1
 out 0x80,al

 jmp short o32_start
o32_start:
 mov al,2
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov esi,7
 mov ebx,0x8002
 xor edx,edx
 align 2
.o32_loop:
 imul edx,esi,strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .o32_loop
 cmp edx,0xffffffeb
 jne fail
 mov al,3
 out 0x80,al

 jmp short a32_start
a32_start:
 mov al,4
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov esi,7
 mov ebx,0x8002
 xor dx,dx
 align 2
.a32_loop:
 a32 imul dx,word [ebx],strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .a32_loop
 cmp dx,0xffeb
 jne fail
 mov al,5
 out 0x80,al

 lgdt [gdtr]
 mov eax,cr0
 or eax,1
 mov cr0,eax
 jmp 0x08:protected
protected:
 mov ax,0x10
 mov ds,ax
 mov es,ax
 mov ss,ax
 mov sp,0x7000
 jmp short protected_start
protected_start:
 mov al,6
 out 0x80,al
 mov bp,1000
 mov ax,7
 mov esi,7
 mov ebx,0x8002
 xor dx,dx
 align 2
.protected_loop:
 imul dx,si,strict byte -3
 times 24 add ax,strict word 0
 dec bp
 jnz short .protected_loop
 cmp dx,0xffeb
 jne fail
 cmp word [0x8002],7
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
align 8, db 0
gdt:
 dq 0
 dq 0x00009b000000ffff
 dq 0x000093000000ffff
gdt_end:
gdtr:
 dw gdt_end-gdt-1
 dd gdt
