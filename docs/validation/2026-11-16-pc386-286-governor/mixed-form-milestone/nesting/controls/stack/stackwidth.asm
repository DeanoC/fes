; Whole-CPU, diagnostic governor scope control. 16-bit CS, SS.B changes.
; Ports 80h/8fh are the private governor bench's markers, not real hardware.
bits 16
cpu 386
org 0x1000
start:
    cli
    cld
    xor ax,ax
    mov ds,ax
    lgdt [gdtr]
    mov eax,cr0
    or al,1
    mov cr0,eax
    jmp 0x08:pm
pm:
    mov ax,0x18             ; flat 32-bit data descriptor, 16-bit code
    mov ds,ax
    mov es,ax
    mov ss,ax
    mov esp,0x00018000

; The taken branch and NOPs put each marked window behind the same queue
; flush. The SS write is immediately followed by the instruction under test.
%macro PREP 3
    mov ax,%1              ; initial SS selector
    mov ss,ax
    mov esp,0x00018000
    mov ebp,%3
    mov word [0x7ffe],0x1357
    mov word [dword 0x17ffe],0x2468
    mov si,%2              ; destination SS selector
    jmp %%clean
%%clean:
    nop
    nop
    nop
    nop
%endmacro
%macro MARK 1
    mov al,%1
    out 0x80,al
%endmacro
%macro ENTER_CASE 4
    PREP %1,%2,0x00013456
    MARK %3
    mov ss,si
    enter 0x20,0
    MARK %4
    cmp esp,0x00017fde
    jne fail
    cmp ebp,0x00017ffe
    jne fail
    %if %2 == 0x10
        cmp word [0x7ffe],0x3456
        jne fail
        cmp word [dword 0x17ffe],0x2468
        jne fail
    %else
        cmp word [dword 0x17ffe],0x3456
        jne fail
        cmp word [0x7ffe],0x1357
        jne fail
    %endif
%endmacro
%macro LEAVE_CASE 4
    PREP %1,%2,0x00017ffe
    MARK %3
    mov ss,si
    leave
    MARK %4
    cmp esp,0x00018000
    jne fail
    %if %2 == 0x10
        cmp ebp,0x00011357
    %else
        cmp ebp,0x00012468
    %endif
    jne fail
    cmp word [0x7ffe],0x1357
    jne fail
    cmp word [dword 0x17ffe],0x2468
    jne fail
%endmacro

    ENTER_CASE 0x10,0x18,0,1 ; SS.B 0 -> 1
    ENTER_CASE 0x18,0x10,2,3 ; SS.B 1 -> 0
    LEAVE_CASE 0x10,0x18,4,5 ; SS.B 0 -> 1
    LEAVE_CASE 0x18,0x10,6,7 ; SS.B 1 -> 0
    xor al,al
    out 0x8f,al
    hlt
fail:
    mov al,0xee
    out 0x8f,al
    hlt
align 8, db 0
gdt:
    dq 0
    dq 0x00009b000000ffff     ; 08: 16-bit code, base zero, 64 KiB
    dq 0x000093000000ffff     ; 10: 16-bit stack, base zero, 64 KiB
    dq 0x00cf93000000ffff     ; 18: 32-bit stack/data, flat 4 GiB
gdt_end:
gdtr:
    dw gdt_end-gdt-1
    dd gdt
