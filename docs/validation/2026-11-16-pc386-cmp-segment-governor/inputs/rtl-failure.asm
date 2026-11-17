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
 mov word [0],handler_de
 mov word [2],0
 mov word [4],handler_db
 mov word [6],0
 call control_shifts
rtl_case_0:
 mov al,0
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x2e
 cmp [bx+si],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jmp .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,1
 out 0x80,al
rtl_case_1:
 mov al,2
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x2e
 cmp [bx+si],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,3
 out 0x80,al
rtl_case_2:
 mov al,4
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x36
 cmp [bx+2],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,5
 out 0x80,al
rtl_case_3:
 mov al,6
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x36
 cmp [bx+2],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,7
 out 0x80,al
rtl_case_4:
 mov al,8
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x2e,0x2e
 cmp [0x8004],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,9
 out 0x80,al
rtl_case_5:
 mov al,10
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x2e,0x2e
 cmp [0x8004],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,11
 out 0x80,al
rtl_case_6:
 mov al,12
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x2e,0x36
 cmp [bx],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,13
 out 0x80,al
rtl_case_7:
 mov al,14
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x2e,0x36
 cmp [bx],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,15
 out 0x80,al
rtl_case_8:
 mov al,16
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x36,0x2e
 cmp [bx+si],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,17
 out 0x80,al
rtl_case_9:
 mov al,18
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x36,0x2e
 cmp [bx+si],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,19
 out 0x80,al
rtl_case_10:
 mov al,20
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x36,0x36
 cmp [bx+2],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,21
 out 0x80,al
rtl_case_11:
 mov al,22
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 db 0x36,0x36
 cmp [bx+2],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0x44
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1234
 jne .case_failed
 cmp word [0x8006],0x1234
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,23
 out 0x80,al
rtl_case_12:
 mov al,24
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1235
 mov word [0x8006],0x1235
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 nop
 db 0x2e
 cmp [bx],al
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1235
 jne .case_failed
 cmp word [0x8006],0x1235
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,25
 out 0x80,al
rtl_case_13:
 mov al,26
 out 0x80,al
 mov word [0x8014],.inner
 pushf
 pop word [0x8024]
 mov ax,ss
 mov [0x8020],ax
 mov [0x8022],sp
 cli
 mov ax,cs
 mov ss,ax
 mov sp,0x7000
 push word [0x8024]
 popf
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1235
 mov word [0x8006],0x1235
 mov word [0x800c],0
 mov [0x800a],sp
 align 2
.inner:
 nop
 db 0x36
 cmp [bx],ax
 pushf
 pop di
 cmp word [0x800c],0
 loop .inner
 push ax
 mov ax,ss
 mov dx,cs
 cmp ax,dx
 pop ax
 jne .case_failed
 cmp word [0x800c],0
 jne .case_failed
 cmp bp,1000
 jne .case_failed
 cmp cx,0
 jne .case_failed
 and di,0x8d5
 cmp di,0
 jne .case_failed
 cmp ax,0x1234
 jne .case_failed
 cmp bx,0x8004
 jne .case_failed
 cmp si,0
 jne .case_failed
 cmp word [0x8004],0x1235
 jne .case_failed
 cmp word [0x8006],0x1235
 jne .case_failed
 cmp word [0x8000],0x5aa5
 jne .case_failed
 cmp [0x800a],sp
 jne .case_failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne .case_failed
 push es
 pop ax
 cmp ax,dx
 jne .case_failed
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp .case_ok
.case_failed:
 cli
 mov ax,[0x8020]
 mov ss,ax
 mov sp,[0x8022]
 push word [0x8024]
 popf
 pushf
 pop dx
 xor dx,[0x8024]
 test dx,0x200
 jnz restore_failed
 mov dx,ss
 cmp dx,[0x8020]
 jne restore_failed
 cmp sp,[0x8022]
 jne restore_failed
 jmp failed
.case_ok:
 mov al,27
 out 0x80,al
 mov al,0
 out 0x8f,al
 hlt
restore_failed:
 mov al,0xfe
 out 0x8e,al
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
handler_de:
 nop
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
 cmp byte [bx],0xf6
 jne .bad
 cmp byte [bx+1],0x37
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
 add word [ss:bp+2],2
 pop ds
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
