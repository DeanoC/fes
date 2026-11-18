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
 mov word [0x8012],7
 mov word [0x8102],9
 mov ax,0x1000
 mov es,ax
 mov word [es:0x8002],7
 xor ax,ax
 mov es,ax
 mov bx,0x8002
 mov si,7

 ; Older canonical DS reload followed immediately by a selected-memory IMUL.
 mov al,0
 out 0x80,al
 mov ax,1
 mov ds,ax
 imul dx,word [bx],strict byte -3
 cmp dx,0xffeb
 jne fail
 mov al,1
 out 0x80,al

 ; Adjacent DS reload and register-only IMUL; memory-segment hazard differs.
 mov al,2
 out 0x80,al
 xor ax,ax
 mov ds,ax
 imul dx,si,strict byte -3
 cmp dx,0xffeb
 jne fail
 mov al,3
 out 0x80,al

 ; Create DS=0010h with hidden base 10000h, then reload the same selector in
 ; real mode before a memory IMUL: expected 9 comes from canonical 00100h base.
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
 cmp word [0x8002],7
 jne fail
 cmp word [es:0x8102],9
 jne fail
 mov al,4
 out 0x80,al
 ; Old noncanonical selected DS precedes its same-selector reload.  In-order
 ; retirement should complete this read before the reload becomes visible.
 imul dx,word [bx],strict byte -3
 mov ax,0x10
 mov ds,ax
 cmp dx,0xffeb
 jne fail
 mov al,5
 out 0x80,al
 mov al,6
 out 0x80,al
 imul dx,word [bx],strict byte -3
 cmp dx,0xffe5
 jne fail
 cmp word [es:0x8102],9
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
 dq 0x000093010000ffff
gdt_end:
gdtr:
 dw gdt_end-gdt-1
 dd gdt
