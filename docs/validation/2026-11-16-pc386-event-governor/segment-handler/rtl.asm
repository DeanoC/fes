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
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,2
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,4
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,6
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
rtl_case_4:
 mov al,8
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,9
 out 0x80,al
rtl_case_5:
 mov al,10
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,11
 out 0x80,al
rtl_case_6:
 mov al,12
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,13
 out 0x80,al
rtl_case_7:
 mov al,14
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,15
 out 0x80,al
rtl_case_8:
 mov al,16
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,17
 out 0x80,al
rtl_case_9:
 mov al,18
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,19
 out 0x80,al
rtl_case_10:
 mov al,20
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,21
 out 0x80,al
rtl_case_11:
 mov al,22
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,23
 out 0x80,al
rtl_case_12:
 mov al,24
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,25
 out 0x80,al
rtl_case_13:
 mov al,26
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,27
 out 0x80,al
rtl_case_14:
 mov al,28
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,29
 out 0x80,al
rtl_case_15:
 mov al,30
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,31
 out 0x80,al
rtl_case_16:
 mov al,32
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  shr word [si],cl
  .fault:
  div byte [bx]
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
 mov al,33
 out 0x80,al
rtl_case_17:
 mov al,34
 out 0x80,al
 mov word [0x8014],.fault
 mov bp,1000
 mov bx,0x8004
 mov si,0x8000
 xor cx,cx
 mov word [si],0x5aa5
 mov byte [bx],0
 mov word [0x8006],0
 mov word [0x8008],0
 mov [0x800a],sp
 align 2
.inner:
  xor ax,ax
  xor dx,dx
  clc
  jmp short .prime
  .prime:
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  nop
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  shr word [si],0
  .fault:
  div byte [bx]
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
 mov al,35
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
 push es
 push cs
 pop es
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
 pop es
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
