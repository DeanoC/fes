; Reference-only pc386 governor discriminator. See method.md.
; Every benchmark emits completed 1000-iteration blocks in 64 BIOS ticks.
; NASM -f bin -o GOVPROBE.COM govprobe.asm
        cpu 8086
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
        mov word [0x8000],1
        mov word [0x8002],0
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
        MEASURE msg_4, test_4
        MEASURE msg_5, test_5
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
        cmp word [tick],64
        jb .block
        ret
%endmacro
test_0: call t_start
.block: mov bp,1000
        mov bx,0x8000
        mov si,0x8000
        mov dx,1
        mov cx,1
        align 2
.i0:
  cmp dx,[si]
  dec bp
  jnz .i0
        FINISH
test_1: call t_start
.block: mov bp,1000
        mov bx,0x8000
        mov si,0x8000
        mov dx,1
        mov cx,1
        align 2
.i1:
  cmp dh,[si]
  dec bp
  jnz .i1
        FINISH
test_2: call t_start
.block: mov bp,1000
        mov bx,0x8000
        mov si,0x8000
        mov dx,1
        mov cx,1
        align 2
.i2:
  pushf
 pop si
  dec bp
  jnz .i2
        FINISH
test_3: call t_start
.block: mov bp,1000
        mov bx,0x8000
        mov si,0x8000
        mov dx,1
        mov cx,1
        align 2
.i3:
  jmp short .jump_done
 nop
.jump_done: nop
  dec bp
  jnz .i3
        FINISH
test_4: call t_start
.block: mov bp,1000
        mov bx,0x8000
        mov si,0x8000
        mov dx,1
        mov cx,1
        align 2
.i4:
  jmp near .near_done
 nop
.near_done: nop
  dec bp
  jnz .i4
        FINISH
test_5: call t_start
.block: mov bp,1000
        mov bx,0x8000
        mov si,0x8000
        mov dx,1
        mov cx,1
        align 2
.i5:
  call .callee
 jmp near .call_done
.callee: inc dx
 ret
.call_done: nop
  dec bp
  jnz .i5
        FINISH

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
msg_0: db 'CMP_WORD_SI=',0
msg_1: db 'CMP_BYTE_SI=',0
msg_2: db 'FLAGS_OTHER=',0
msg_3: db 'SHORT_JMP=',0
msg_4: db 'NEAR_JMP=',0
msg_5: db 'CALL_RET_NEAR=',0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0
