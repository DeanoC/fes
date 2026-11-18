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
 mov word [0],handler_a
 mov word [2],0
 mov word [4],handler_db
 mov word [6],0
 call control_shifts
rtl_case_0:
 mov word [0],handler_a
 mov al,0
 out 0x80,al
 mov word [0x8014],.resume
 mov byte [0x8016],77
 mov byte [0x8017],117
 mov byte [0x8018],0
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov word [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
 mov si,.resume
 mov di,handler_a
.inner:
 xor ax,ax
 xor dx,dx
 clc
 pushf
 push cs
 push si
 jmp di
.resume:
 dec bp
 jnz .inner
 cmp word [0x8006],1000
 jne failed
 cmp word [0x8008],0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 mov al,1
 out 0x80,al
rtl_case_1:
 mov word [0],handler_c
 mov al,2
 out 0x80,al
 mov word [0x8014],.resume
 mov byte [0x8016],77
 mov byte [0x8017],117
 mov byte [0x8018],0
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov word [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
 mov si,.resume
 mov di,handler_c
.inner:
 xor ax,ax
 xor dx,dx
 clc
 pushf
 push cs
 push si
 jmp di
.resume:
 dec bp
 jnz .inner
 cmp word [0x8006],1000
 jne failed
 cmp word [0x8008],0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 mov al,3
 out 0x80,al
rtl_case_2:
 mov word [0],handler_b
 mov al,4
 out 0x80,al
 mov word [0x8014],.resume
 mov byte [0x8016],77
 mov byte [0x8017],117
 mov byte [0x8018],0
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov word [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
 mov si,.resume
 mov di,handler_b
.inner:
 xor ax,ax
 xor dx,dx
 clc
 pushf
 push cs
 push si
 jmp di
.resume:
 dec bp
 jnz .inner
 cmp word [0x8006],1000
 jne failed
 cmp word [0x8008],0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 mov al,5
 out 0x80,al
rtl_case_3:
 mov word [0],handler_d
 mov al,6
 out 0x80,al
 mov word [0x8014],.resume
 mov byte [0x8016],77
 mov byte [0x8017],117
 mov byte [0x8018],0
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov word [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
 mov si,.resume
 mov di,handler_d
.inner:
 xor ax,ax
 xor dx,dx
 clc
 pushf
 push cs
 push si
 jmp di
.resume:
 dec bp
 jnz .inner
 cmp word [0x8006],1000
 jne failed
 cmp word [0x8008],0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 mov al,7
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
failed:
 mov al,0xff
 out 0x8e,al
 hlt

control_shifts:
 mov si,0x8000
 mov word [si],0x5aa5
 xor cx,cx
 xor dx,dx
 stc
 shr word [si],cl
 pushf
 pop ax
 and ax,0x41
 cmp ax,0x41
 jne failed
 cmp word [si],0x5aa5
 jne failed
 xor dx,dx
 stc
 shr word [si],0
 pushf
 pop ax
 and ax,0x41
 cmp ax,0x41
 jne failed
 cmp word [si],0x5aa5
 jne failed
 ret
handler_a:
 push bp
 mov bp,sp
 push ax
 push bx
 push ds
 push cs
 pop ds
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 mov al,[0x8016]
 cmp [bx],al
 jne .bad
 mov al,[0x8017]
 cmp [bx+1],al
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x41
 cmp ax,0x40
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 xor ax,ax
 mov al,[0x8018]
 add [ss:bp+2],ax
 pop ds
 pop bx
 pop ax
 pop bp
 iret
handler_b:
 times 8 nop
 push bp
 mov bp,sp
 push ax
 push bx
 push ds
 push cs
 pop ds
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 mov al,[0x8016]
 cmp [bx],al
 jne .bad
 mov al,[0x8017]
 cmp [bx+1],al
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x41
 cmp ax,0x40
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 xor ax,ax
 mov al,[0x8018]
 add [ss:bp+2],ax
 pop ds
 pop bx
 pop ax
 pop bp
 iret
handler_c:
 push bp
 mov bp,sp
 push ax
 push bx
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 mov al,[0x8016]
 cmp [bx],al
 jne .bad
 mov al,[0x8017]
 cmp [bx+1],al
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x41
 cmp ax,0x40
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 xor ax,ax
 mov al,[0x8018]
 add [ss:bp+2],ax
 pop bx
 pop ax
 pop bp
 iret
handler_d:
 times 8 nop
 push bp
 mov bp,sp
 push ax
 push bx
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 mov al,[0x8016]
 cmp [bx],al
 jne .bad
 mov al,[0x8017]
 cmp [bx+1],al
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x41
 cmp ax,0x40
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 xor ax,ax
 mov al,[0x8018]
 add [ss:bp+2],ax
 pop bx
 pop ax
 pop bp
 iret
handler_db:
 push bp
 mov bp,sp
 push ax
 push bx
 push ds
 push cs
 pop ds
 mov bx,[ss:bp+2]
 cmp bx,[0x8014]
 jne .bad
 cmp byte [bx],0x4d
 jne .bad
 mov ax,cs
 cmp [ss:bp+4],ax
 jne .bad
 mov ax,[ss:bp+6]
 and ax,0x141
 cmp ax,0x140
 jne .bad
 cmp word [0x8000],0x5aa5
 jne .bad
 jmp .done
.bad:
 mov word [0x8008],1
.done:
 inc word [0x8006]
 and word [ss:bp+6],0xfeff
 pop ds
 pop bx
 pop ax
 pop bp
 iret
