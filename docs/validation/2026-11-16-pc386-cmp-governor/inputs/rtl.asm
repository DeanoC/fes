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
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,1
 out 0x80,al
rtl_case_1:
 mov al,2
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,3
 out 0x80,al
rtl_case_2:
 mov al,4
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp al,[bx]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,5
 out 0x80,al
rtl_case_3:
 mov al,6
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp ax,[bx]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,7
 out 0x80,al
rtl_case_4:
 mov al,8
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx+2],al
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,9
 out 0x80,al
rtl_case_5:
 mov al,10
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx+si],ax
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,11
 out 0x80,al
rtl_case_6:
 mov al,12
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp al,[0x8004]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,13
 out 0x80,al
rtl_case_7:
 mov al,14
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp ax,[bx+2]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,15
 out 0x80,al
rtl_case_8:
 mov al,16
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,17
 out 0x80,al
rtl_case_9:
 mov al,18
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x26
 cmp [bx+si],ax
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,19
 out 0x80,al
rtl_case_10:
 mov al,20
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp al,[bx+2]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,21
 out 0x80,al
rtl_case_11:
 mov al,22
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x26
 cmp ax,[0x8004]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,23
 out 0x80,al
rtl_case_12:
 mov al,24
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx],al
 pushf
 pop di
 loop .inner
 cmp bp,1000
 jne failed
 cmp cx,0
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,25
 out 0x80,al
rtl_case_13:
 mov al,26
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx],ax
 pushf
 pop di
 loop .inner
 cmp bp,1000
 jne failed
 cmp cx,0
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,27
 out 0x80,al
rtl_case_14:
 mov al,28
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp al,[bx]
 pushf
 pop di
 loop .inner
 cmp bp,1000
 jne failed
 cmp cx,0
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,29
 out 0x80,al
rtl_case_15:
 mov al,30
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp ax,[bx]
 pushf
 pop di
 loop .inner
 cmp bp,1000
 jne failed
 cmp cx,0
 jne failed
 and di,0x8d5
 cmp di,0x44
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,31
 out 0x80,al
rtl_case_16:
 mov al,32
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1235
 mov word [0x8006],0x1235
 mov [0x800a],sp
 align 2
.inner:
 nop
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1235
 jne failed
 cmp word [0x8006],0x1235
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,33
 out 0x80,al
rtl_case_17:
 mov al,34
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1235
 mov word [0x8006],0x1235
 mov [0x800a],sp
 align 2
.inner:
 nop
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1235
 jne failed
 cmp word [0x8006],0x1235
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,35
 out 0x80,al
rtl_case_18:
 mov al,36
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1235
 mov word [0x8006],0x1235
 mov [0x800a],sp
 align 2
.inner:
 nop
 cmp al,[bx]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x95
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1235
 jne failed
 cmp word [0x8006],0x1235
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,37
 out 0x80,al
rtl_case_19:
 mov al,38
 out 0x80,al
 mov word [0x8014],.inner
 mov bp,1000
 mov cx,1000
 mov bx,0x8004
 xor si,si
 mov ax,0x1234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1235
 mov word [0x8006],0x1235
 mov [0x800a],sp
 align 2
.inner:
 nop
 cmp ax,[bx]
 pushf
 pop di
 dec bp
 jnz .inner
 cmp bp,0
 jne failed
 cmp cx,1000
 jne failed
 and di,0x8d5
 cmp di,0x95
 jne failed
 cmp ax,0x1234
 jne failed
 cmp bx,0x8004
 jne failed
 cmp si,0
 jne failed
 cmp word [0x8004],0x1235
 jne failed
 cmp word [0x8006],0x1235
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp [0x800a],sp
 jne failed
 push ds
 pop ax
 mov dx,cs
 cmp ax,dx
 jne failed
 push es
 pop ax
 cmp ax,dx
 jne failed
 mov al,39
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
