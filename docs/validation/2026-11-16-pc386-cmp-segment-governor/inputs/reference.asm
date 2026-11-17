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
        mov ax,cs
        mov es,ax
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
        MEASURE msg_4, test_4
        MEASURE msg_5, test_5
        MEASURE msg_6, test_6
        MEASURE msg_7, test_7
        MEASURE msg_8, test_8
        MEASURE msg_9, test_9
        MEASURE msg_10, test_10
        MEASURE msg_11, test_11
        MEASURE msg_12, test_12
        MEASURE msg_13, test_13
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
        cmp word [tick],64
        jb .block
        ret
%endmacro
test_0: call t_start
.block:
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
        FINISH
test_1: call t_start
.block:
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
        FINISH
test_2: call t_start
.block:
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
        FINISH
test_3: call t_start
.block:
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
        FINISH
test_4: call t_start
.block:
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
        FINISH
test_5: call t_start
.block:
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
        FINISH
test_6: call t_start
.block:
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
        FINISH
test_7: call t_start
.block:
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
        FINISH
test_8: call t_start
.block:
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
        FINISH
test_9: call t_start
.block:
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
        FINISH
test_10: call t_start
.block:
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
        FINISH
test_11: call t_start
.block:
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
        FINISH
test_12: call t_start
.block:
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
        FINISH
test_13: call t_start
.block:
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
        FINISH
restore_failed:
        mov si,restorefailmsg
        call puts
        jmp cleanup
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
msg_0: db 'CS8_INDEX=',0
msg_1: db 'CS16_INDEX=',0
msg_2: db 'SS8_DISP=',0
msg_3: db 'SS16_DISP=',0
msg_4: db 'CSCS8_DIRECT=',0
msg_5: db 'CSCS16_DIRECT=',0
msg_6: db 'CSSS8_BX=',0
msg_7: db 'CSSS16_BX=',0
msg_8: db 'SSCS8_INDEX=',0
msg_9: db 'SSCS16_INDEX=',0
msg_10: db 'SSSS8_DISP=',0
msg_11: db 'SSSS16_DISP=',0
msg_12: db 'CS8_NE=',0
msg_13: db 'SS16_NE=',0
okmsg: db 'EVENT_CONTROL_VERIFIED',13,10,0
failmsg: db 'EVENT_CONTROL_FAILED',13,10,0
restorefailmsg: db 'RESTORE_CONTROL_FAILED',13,10,0
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
