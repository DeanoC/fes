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
        mov [saved_orig_sp],sp
        mov [saved_orig_ss],ss
        cli
        mov ax,cs
        mov ss,ax
        mov sp,0x7000
        sti
        mov ax,0x351c
        int 0x21
        mov [oldoff],bx
        mov [oldseg],es
        mov ax,cs
        mov ds,ax
        mov dx,tickisr
        mov ax,0x251c
        int 0x21
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
        mov dx,[oldoff]
        mov ax,[oldseg]
        mov ds,ax
        mov ax,0x251c
        int 0x21
        cli
        mov ss,[saved_orig_ss]
        mov sp,[saved_orig_sp]
        sti
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
        mov ax,cs
        mov ds,ax
        cli
        mov ss,[saved_orig_ss]
        mov sp,[saved_orig_sp]
        sti
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
test_0:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 0,0
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,2
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i0:
        enter 0,0
        leave
        dec si
        jnz .i0
        FINISH
test_1:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 0,1
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,4
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i1:
        enter 0,1
        leave
        dec si
        jnz .i1
        FINISH
test_2:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 0,2
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,6
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i2:
        enter 0,2
        leave
        dec si
        jnz .i2
        FINISH
test_3:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 0,3
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,8
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],0x3344
        jne invalid_memory
        cmp word [ss:bx-6],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i3:
        enter 0,3
        leave
        dec si
        jnz .i3
        FINISH
test_4:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 0,4
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,10
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],0x3344
        jne invalid_memory
        cmp word [ss:bx-6],0x5566
        jne invalid_memory
        cmp word [ss:bx-8],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i4:
        enter 0,4
        leave
        dec si
        jnz .i4
        FINISH
test_5:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 6,1
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,10
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i5:
        enter 6,1
        leave
        dec si
        jnz .i5
        FINISH
test_6:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 12,2
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,18
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i6:
        enter 12,2
        leave
        dec si
        jnz .i6
        FINISH
test_7:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 32,3
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,40
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],0x3344
        jne invalid_memory
        cmp word [ss:bx-6],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i7:
        enter 32,3
        leave
        dec si
        jnz .i7
        FINISH
test_8:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 256,4
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,266
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],0x3344
        jne invalid_memory
        cmp word [ss:bx-6],0x5566
        jne invalid_memory
        cmp word [ss:bx-8],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov si,1000
        align 2
.i8:
        enter 256,4
        leave
        dec si
        jnz .i8
        FINISH
test_9:
        mov bp,0x6000
        mov word [0x5ffe],0x1122
        mov word [0x5ffc],0x3344
        mov word [0x5ffa],0x5566
        mov [saved_test_sp],sp
        cli
        stc
        pushf
        pop dx
        enter 0,2
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        mov ax,[saved_test_sp]
        sub ax,6
        cmp sp,ax
        jne invalid_memory
        mov bx,[saved_test_sp]
        sub bx,2
        cmp bp,bx
        jne invalid_memory
        cmp word [ss:bx],0x6000
        jne invalid_memory
        cmp word [ss:bx-2],0x1122
        jne invalid_memory
        cmp word [ss:bx-4],bx
        jne invalid_memory
        stc
        pushf
        pop dx
        leave
        pushf
        pop ax
        cmp ax,dx
        jne invalid_memory
        cmp sp,[saved_test_sp]
        jne invalid_memory
        cmp bp,0x6000
        jne invalid_memory
        sti
        call t_start
.block:
        mov bp,0x6000
        mov cx,1000
        align 2
.i9:
        enter 0,2
        leave
        loop .i9
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
saved_orig_sp: dw 0
saved_orig_ss: dw 0
saved_test_sp: dw 0
msg_bad: db 'ENTER_CONTROL_FAILED',13,10,0
hdr:    db 'pc386 governor probe v1',13,10,0
msg_0: db 'L0_A0_DEC=',0
msg_1: db 'L1_A0_DEC=',0
msg_2: db 'L2_A0_DEC=',0
msg_3: db 'L3_A0_DEC=',0
msg_4: db 'L4_A0_DEC=',0
msg_5: db 'L1_A6_DEC=',0
msg_6: db 'L2_A12_DEC=',0
msg_7: db 'L3_A32_DEC=',0
msg_8: db 'L4_A256_DEC=',0
msg_9: db 'L2_A0_LOOP=',0
        align 2
oldoff: dw 0
oldseg: dw 0
tick:   dw 0
blocks: dw 0
dcnt:   dw 0
target: dw 0
zero:   dw 0
