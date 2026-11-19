bits 16
cpu 386
org 0x1000
%macro CASE 3
 mov al,%1
 out 0x80,al
 mov bp,100
%%loop:
 mov dx,0x300
 mov cx,2
 mov si,0x8100
 mov di,0x8200
 db %3
 dec bp
 jnz %%loop
 mov al,%2
 out 0x80,al
%endmacro
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
 mov es,ax
 mov ss,ax
 mov sp,0x7000
 mov word [0x8100],0x5555
 CASE 0,1,{0xf3,0x6e}
 lgdt [cs:gdtr]
 mov eax,cr0
 or eax,1
 mov cr0,eax
 jmp 0x08:protected_ds
protected_ds:
 mov ax,0x10
 mov ds,ax
 mov eax,cr0
 and eax,0xfffffffe
 mov cr0,eax
 jmp 0:unreal_ds
unreal_ds:
 mov word [0x8100],0x5555
 CASE 2,3,{0xf3,0x6e}
 xor ax,ax
 mov ds,ax
 CASE 4,5,{0xf3,0x6c}
 lgdt [cs:gdtr]
 mov eax,cr0
 or eax,1
 mov cr0,eax
 jmp 0x08:protected_es
protected_es:
 mov ax,0x10
 mov es,ax
 mov eax,cr0
 and eax,0xfffffffe
 mov cr0,eax
 jmp 0:unreal_es
unreal_es:
 CASE 6,7,{0xf3,0x6c}
 mov al,0x55
 out 0x82,al
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
