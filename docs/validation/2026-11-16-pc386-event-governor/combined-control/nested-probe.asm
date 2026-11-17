; Private CPU-only control: a #DE handler triggers one more #DE after shifts.
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
 mov word [0],handler
 mov word [2],0
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
%macro CASE 3
 mov al,%2
 out 0x80,al
 mov bp,100
 mov word [0x8000],0x5aa5
 mov byte [0x8004],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov word [0x800a],0
 mov word [0x8014],%%fault
 mov [0x8018],sp
 align 2
%%loop:
 xor ax,ax
 xor dx,dx
 clc
 times %1 shr word [si],0
%%fault:
 div byte [bx]
 dec bp
 jnz %%loop
 cmp word [0x8006],100
 jne failed
 cmp word [0x8008],100
 jne failed
 cmp word [0x800a],0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x8018],sp
 jne failed
 mov al,%3
 out 0x80,al
%endmacro
 CASE 1,0,1
 CASE 12,2,3
 mov al,0
 out 0x8f,al
 hlt

handler:
 push bp
 mov bp,sp
 push ax
 push bx
 push cx
 push dx
 push si
 push ds
 push cs
 pop ds
 mov bx,[ss:bp+2]
 mov ax,[ss:bp+4]
 push cs
 pop dx
 cmp ax,dx
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x41
 cmp ax,0x40
 jne .bad
 cmp byte [bx],0xf6
 jne .bad
 cmp byte [bx+1],0x37
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 cmp bx,.inner_fault
 je .inner
 cmp bx,[0x8014]
 jne .bad
 inc word [0x8006]
 xor ax,ax
 xor dx,dx
 xor cx,cx
 mov bx,0x8004
 mov si,0x8000
 clc
 times 7 shr word [si],0
.inner_fault:
 div byte [bx]
 jmp .done
.inner:
 inc word [0x8008]
 jmp .done
.bad:
 mov word [0x800a],1
.done:
 add word [ss:bp+2],2
 pop ds
 pop si
 pop dx
 pop cx
 pop bx
 pop ax
 pop bp
 iret
failed:
 mov al,0xff
 out 0x8e,al
 hlt
