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
        call control_0
        mov al,0
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i0:
        enter 0,0
        leave
        dec si
        jnz .i0
        mov al,1
        out 0x80,al
        call control_1
        mov al,2
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i1:
        enter 0,1
        leave
        dec si
        jnz .i1
        mov al,3
        out 0x80,al
        call control_2
        mov al,4
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i2:
        enter 0,2
        leave
        dec si
        jnz .i2
        mov al,5
        out 0x80,al
        call control_3
        mov al,6
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i3:
        enter 0,3
        leave
        dec si
        jnz .i3
        mov al,7
        out 0x80,al
        call control_4
        mov al,8
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i4:
        enter 0,4
        leave
        dec si
        jnz .i4
        mov al,9
        out 0x80,al
        call control_5
        mov al,10
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i5:
        enter 6,1
        leave
        dec si
        jnz .i5
        mov al,11
        out 0x80,al
        call control_6
        mov al,12
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i6:
        enter 12,2
        leave
        dec si
        jnz .i6
        mov al,13
        out 0x80,al
        call control_7
        mov al,14
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i7:
        enter 32,3
        leave
        dec si
        jnz .i7
        mov al,15
        out 0x80,al
        call control_8
        mov al,16
        out 0x80,al
        mov bp,0x6000
        mov si,1000
        align 2
.i8:
        enter 256,4
        leave
        dec si
        jnz .i8
        mov al,17
        out 0x80,al
        call control_9
        mov al,18
        out 0x80,al
        mov bp,0x6000
        mov cx,1000
        align 2
.i9:
        enter 0,2
        leave
        loop .i9
        mov al,19
        out 0x80,al
        cli
        xor al,al
        out 0x8f,al
        hlt
invalid_memory:
        cli
        mov al,0xee
        out 0x8f,al
        hlt
control_0:
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
        ret
control_1:
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
        ret
control_2:
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
        ret
control_3:
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
        ret
control_4:
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
        ret
control_5:
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
        ret
control_6:
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
        ret
control_7:
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
        ret
control_8:
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
        ret
control_9:
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
        ret
saved_test_sp: dw 0
