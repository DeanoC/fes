; Reference-only pc386 governor discriminator. See method.md.
; Every benchmark emits completed 1000-iteration blocks in 64 BIOS ticks.
; NASM -f bin -o GOVPROBE.COM govprobe.asm
        cpu 286
        bits 16
        org 0x100
start:  cld
        mov ax,cs
        mov ds,ax
        mov es,ax
        mov ax,0x351c
        int 0x21
        mov [oldoff],bx
        mov [oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,tickisr
        mov ax,0x251c
        int 0x21
        mov ax,0x3500
        int 0x21
        mov [de_oldoff],bx
        mov [de_oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,handler_de
        mov ax,0x2500
        int 0x21
        mov ax,0x3501
        int 0x21
        mov [db_oldoff],bx
        mov [db_oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,handler_db
        mov ax,0x2501
        int 0x21
        call control_shifts
        mov si,hdr
        call puts
%macro MEASURE 2
        mov si,%1
        call puts
        call %2
        mov ax,[blocks]
        call putu
        call crlf
%endmacro
        MEASURE msg_0, test_0
        MEASURE msg_1, test_1
        MEASURE msg_2, test_2
        MEASURE msg_3, test_3
        mov si,okmsg
        call puts
cleanup:
        mov dx,[de_oldoff]
        mov ax,[de_oldseg]
        mov ds,ax
        mov ax,0x2500
        int 0x21
        mov ax,cs
        mov ds,ax
        mov dx,[db_oldoff]
        mov ax,[db_oldseg]
        mov ds,ax
        mov ax,0x2501
        int 0x21
        mov ax,cs
        mov ds,ax
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        mov ax,0x4c00
        int 0x21

t_start:
        mov word [blocks],0
        mov word [tick],0
.sync:  cmp word [tick],0
        je .sync
        mov word [tick],0
        ret
%macro FINISH 0
        inc word [blocks]
        cmp word [tick],256
        jb .block
        ret
%endmacro
test_0: call t_start
.block:
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
        FINISH
test_1: call t_start
.block:
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
        FINISH
test_2: call t_start
.block:
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
        FINISH
test_3: call t_start
.block:
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
        FINISH
failed:
        mov si,failmsg
        call puts
        jmp cleanup

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

tickisr: push ds
        push cs
        pop ds
        inc word [tick]
        pop ds
        iret
putc:   push dx
        mov dl,al
        mov ah,2
        int 0x21
        pop dx
        ret
crlf:   mov al,13
        call putc
        mov al,10
        call putc
        ret
puts:   lodsb
        or al,al
        jz .done
        call putc
        jmp puts
.done:  ret
putu:   push bx
        push cx
        push dx
        mov bx,10
        xor cx,cx
.d1:    xor dx,dx
        div bx
        push dx
        inc cx
        or ax,ax
        jnz .d1
.d2:    pop ax
        add al,'0'
        call putc
        loop .d2
        pop dx
        pop cx
        pop bx
        ret
hdr:    db 'pc386 governor probe v1',13,10,0
msg_0: db 'EMPTY_7_CL=',0
msg_1: db 'EMPTY_7_IMM=',0
msg_2: db 'FILLED_7_CL=',0
msg_3: db 'FILLED_7_IMM=',0
okmsg: db 'EVENT_CONTROL_VERIFIED',13,10,0
failmsg: db 'EVENT_CONTROL_FAILED',13,10,0
de_oldoff: dw 0
de_oldseg: dw 0
db_oldoff: dw 0
db_oldseg: dw 0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0
