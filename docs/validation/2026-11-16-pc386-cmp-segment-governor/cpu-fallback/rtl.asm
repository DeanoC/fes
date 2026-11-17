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
case_0:
 mov al,0
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_1:
 mov al,2
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_2:
 mov al,4
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x64,0x3e
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_3:
 mov al,6
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x64,0x3e
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_4:
 mov al,8
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x65,0x3e
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_5:
 mov al,10
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x65,0x3e
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_6:
 mov al,12
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x66,0x3e
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_7:
 mov al,14
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [bx],eax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_8:
 mov al,16
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [ebx],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_9:
 mov al,18
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [ebx],ax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_10:
 mov al,20
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [bx+si+2],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_11:
 mov al,22
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [bx+si+2],ax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_12:
 mov al,24
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [bx],al
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
case_13:
 mov al,26
 out 0x80,al
mov bp,1000
 mov ebx,0x8004
 xor esi,esi
 mov eax,0x12341234
 mov word [0x8000],0x5aa5
 mov word [0x8004],0x1234
 mov word [0x8006],0x1234
 mov word [0x8008],0x1234
 mov [0x800a],sp
 align 2
.inner:
 db 0x3e
 cmp [bx],ax
 pushf
 pop di
 dec bp
 jnz .inner
and di,0x8d5
 cmp di,0x44
 jne failed
 cmp eax,0x12341234
 jne failed
 cmp ebx,0x8004
 jne failed
 cmp esi,0
 jne failed
 cmp bp,0
 jne failed
 cmp word [0x8000],0x5aa5
 jne failed
 cmp word [0x8004],0x1234
 jne failed
 cmp word [0x8006],0x1234
 jne failed
 cmp word [0x8008],0x1234
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
mov al,0
 out 0x8f,al
 hlt
failed:
 mov al,0xff
 out 0x8e,al
 hlt
