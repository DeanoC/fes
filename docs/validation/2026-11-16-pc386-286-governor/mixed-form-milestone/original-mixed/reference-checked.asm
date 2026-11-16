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
        mov ax,cs
        mov ds,ax
        mov es,ax
        mov word [0x8000],0x5aa5
        mov word [0x9000],0
        mov si,0x8000
        mov di,0x9000
        movsw
        cmp word [0x9000],0x5aa5
        jne invalid_memory
        mov si,msg_ram
        call puts
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
        MEASURE msg_6, test_6
        MEASURE msg_7, test_7
        MEASURE msg_8, test_8
        MEASURE msg_9, test_9
        MEASURE msg_10, test_10
        MEASURE msg_11, test_11
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        mov ax,0x4c00
        int 0x21

invalid_memory:
        mov si,msg_bad
        call puts
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        mov ax,0x4c01
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
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i0:
  mov si,0x8000
 mov di,0x9000
 mov cx,16
 rep movsw
  dec bp
  jnz .i0
        FINISH
test_1: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i1:
  mov di,0x9000
 mov cx,16
 mov ax,0x3939
 rep stosw
  dec bp
  jnz .i1
        FINISH
test_2: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i2:
  mov di,0x9000
 mov cx,16
 mov al,0xfe
 repne scasb
  dec bp
  jnz .i2
        FINISH
test_3: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i3:
  mov si,0x8000
 mov di,0x9000
 mov cx,16
 repe cmpsw
  dec bp
  jnz .i3
        FINISH
test_4: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i4:
  mov ax,[bx]
 xor ax,0x5555
 rol ax,1
 mov [bx+2],ax
 inc bx
 and bx,0x83ff
  dec bp
  jnz .i4
        FINISH
test_5: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i5:
  mov ax,[bx]
 add ax,[bx+2]
 adc dx,0
 mov [bx+4],ax
 add bx,2
 and bx,0x83fe
  dec bp
  jnz .i5
        FINISH
test_6: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i6:
  test byte [bx],1
 jz $+4
 inc dx
 inc dx
 inc bx
 and bx,0x83ff
  dec bp
  jnz .i6
        FINISH
test_7: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i7:
  push ax
 push bx
 call $+5
 jmp $+4
 inc dx
 ret
 pop bx
 pop ax
  dec bp
  jnz .i7
        FINISH
test_8: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i8:
  lodsb
 xor al,0x55
 stosb
  dec bp
  jnz .i8
        FINISH
test_9: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i9:
  mov cx,16
 loop $
  dec bp
  jnz .i9
        FINISH
test_10: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i10:
  mov ax,[bx]
 mov dx,7
 mul dx
 test ax,8
 jz $+4
 inc bx
 dec bx
  dec bp
  jnz .i10
        FINISH
test_11: call t_start
.block: mov bp,1000
        mov di,0x8000
        mov ax,0x1111
        mov cx,512
        rep stosw
        mov word [0x8000],7
        mov si,0x8000
        mov di,0x9000
        mov cx,512
        rep movsw
        mov bx,0x8000
        mov dx,1
        mov cx,3
        mov si,0x8000
        mov di,0x9000
        align 2
.i11:
  enter 6,0
 mov ax,[bp-2]
 leave
  dec bp
  jnz .i11
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
msg_ram: db 'RAM_COPY_VERIFIED',13,10,0
msg_bad: db 'RAM_COPY_FAILED',13,10,0
msg_0: db 'COPY_WORDS=',0
msg_1: db 'FILL_WORDS=',0
msg_2: db 'SCAN_BYTES=',0
msg_3: db 'COMPARE_WORDS=',0
msg_4: db 'DRAW_WORDS=',0
msg_5: db 'CHECKSUM=',0
msg_6: db 'BRANCH_SCAN=',0
msg_7: db 'STACK_CALL=',0
msg_8: db 'BYTE_STREAM=',0
msg_9: db 'BUSY_WAIT=',0
msg_10: db 'ARITH_BRANCH=',0
msg_11: db 'ENTER_LEAVE=',0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0
