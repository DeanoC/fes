; Private CPU-only diagnostic fallback controls; MMIO is read, never written.
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
 ; Cross 9ffffh RAM to a0000h emulated MMIO within DS:000f.
 mov al,0
 out 0x80,al
 mov bp,100
 mov ax,0x9fff
 mov ds,ax
 mov si,0x000f
 xor ax,ax
.inner_cross:
 db 0x3e
 cmp word [si],ax
 pushf
 pop di
 dec bp
 jnz .inner_cross
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp bp,0
 jne failed
 cmp si,0x000f
 jne failed
 cmp word [ss:0x8000],0
 jne failed
 cmp sp,0x7000
 jne failed
 xor ax,ax
 mov ds,ax
 mov al,1
 out 0x80,al
 lgdt [gdtr]
 mov eax,cr0
 or al,1
 mov cr0,eax
 jmp 0x08:pm32
bits 32
pm32:
 mov ax,0x10
 mov ds,ax
 mov es,ax
 mov ss,ax
 mov esp,0x7000
 mov al,2
 out 0x80,al
 mov ebp,100
 mov ebx,0x8004
 mov eax,0x1234
 mov word [0x8004],0x1234
.inner_32:
 ; 66+67 => effective 16-bit operand/address, DS override; code default is 32.
 db 0x66,0x67,0x3e,0x39,0x07
 pushfd
 pop edi
 dec ebp
 jnz .inner_32
 and edi,0x8d5
 cmp edi,0x44
 jne failed32
 cmp ebx,0x8004
 jne failed32
 cmp eax,0x1234
 jne failed32
 cmp esp,0x7000
 jne failed32
 cmp word [0x8004],0x1234
 jne failed32
 mov al,3
 out 0x80,al
 jmp 0x18:pm16
bits 16
pm16:
 mov al,4
 out 0x80,al
 mov bp,100
 mov bx,0x8004
 mov ax,0x1234
.inner_16:
 db 0x3e
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner_16
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp bx,0x8004
 jne failed
 cmp ax,0x1234
 jne failed
 cmp esp,0x7000
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 mov al,5
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
failed:
 mov al,0xff
 out 0x8e,al
 hlt
bits 32
failed32:
 mov al,0xff
 out 0x8e,al
 hlt
align 8
gdt:
 dq 0
 dw 0xffff,0
 db 0,0x9a,0x40,0
 dw 0xffff,0
 db 0,0x92,0x40,0
 dw 0xffff,0
 db 0,0x9a,0x00,0
gdt_end:
gdtr: dw gdt_end-gdt-1
 dd gdt
