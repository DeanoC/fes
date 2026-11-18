bits 16
cpu 386
org 0x1000
start:
 cli
 cld
 xor ax,ax
 mov ds,ax
 mov ss,ax
 mov sp,0x7000
 mov dword [0x8000],0x00009003
 mov di,0x9000
 xor eax,eax
 mov cx,16
.setup:
 mov edx,eax
 or edx,3
 mov [di],edx
 add eax,0x1000
 add di,4
 loop .setup
 mov dword [0x9010],0x0000c003
 mov dword [0x9014],0x0000e003
 mov byte [0xcfff],0xcd
 mov byte [0xe000],0xab
 lgdt [gdtr]
 lidt [idtr]
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
 mov eax,0x8000
 mov cr3,eax
 mov eax,cr0
 or eax,0x80000000
 mov cr0,eax
 jmp short .paged
.paged:
 jmp short .clean1
.clean1:
 mov al,0
 out 0x80,al
 imul dx,word [0x4fff],strict byte -3
 mov al,1
 out 0x80,al
 cmp dx,0xfc99
 jne fail
 mov word [0x4ffe],0xabcd
 jmp short .clean2
.clean2:
 mov al,2
 out 0x80,al
 imul dx,word [0x4ffe],strict byte -3
 mov al,3
 out 0x80,al
 cmp dx,0xfc99
 jne fail
 mov byte [0x4fff],0xcd
 mov byte [0x5000],0xab
 mov dword [0x9010],0
 mov eax,cr3
 mov cr3,eax
 jmp short .clean3
.clean3:
 mov al,4
 out 0x80,al
 imul dx,word [0x4fff],strict byte -3
 mov al,5
 out 0x80,al
 cmp dx,0xfc99
 jne fail
 mov al,0
 out 0x8f,al
 hlt
pf_handler:
 push eax
 mov eax,cr2
 cmp eax,0x4fff
 jne fail
 mov dword [0x9010],0x0000c003
 mov eax,cr3
 mov cr3,eax
 mov al,0x55
 out 0x81,al
 pop eax
 add sp,2
 iret
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
align 8, db 0
idt:
 times 14 dq 0
 dw pf_handler,0x0008
 db 0,0x86
 dw 0
idt_end:
idtr:
 dw idt_end-idt-1
 dd idt
