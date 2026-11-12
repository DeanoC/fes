; SPDX-License-Identifier: MIT
; pc386 RTL stop test disk: the machine bench runs defined-but-unimplemented
; ICEBP on Bochs and restarts the RTL. Two real-mode ICEBPs use an INT1
; handler and require two returns. A third runs with paging and a valid 16-bit
; protected-mode gate; the oracle stays in charge until paging is cleared.
; The same boot sector also runs on the oracle alone, and both emit port E9h
; markers for the handler counts and final result.
;
; The first SMSW logs "R <AL> <flags>" at boot. Its reserved CR0 bits 5--7
; differ between current RTL and Bochs and need a real-386 measurement (#1748); the
; runner records both values and compares the other bits and flags. After a
; CR0 write, SMSW logs "C <CR0>". "DONE" ends the run.

bits 16
cpu 386

DEBUG_PORT equ 0xe9
traps equ 0x0500             ; scratch, away from the boot sector's code page

org 0x7c00
start:
    cli
    xor ax, ax
    mov ds, ax
    mov ss, ax
    mov sp, 0x7c00
    ; ICEBP is defined by the 386 but not yet built in the RTL. Point the
    ; real-mode debug trap at this sector so Bochs can deliver and IRET it.
    mov word [0x0004], int1
    mov word [0x0006], 0
    mov byte [traps], 0
    smsw ax                 ; CR0's low word, PE clear
    call result
    db 0xf1                 ; ICEBP: first real-mode unimplemented stop
    db 0xf1                 ; second site proves restart after the first
    mov al, 'X'
    out DEBUG_PORT, al
    mov al, ' '
    out DEBUG_PORT, al
    mov al, [traps]
    add al, '0'
    out DEBUG_PORT, al
    mov al, 10
    out DEBUG_PORT, al
    mov eax, 0x10
    mov cr0, eax             ; the 386 sets reserved CR0 bits on this write
    smsw ax
    push bx
    mov bx, ax
    mov al, 'C'
    out DEBUG_PORT, al
    mov al, ' '
    out DEBUG_PORT, al
    mov al, bh
    call hex
    mov al, bl
    call hex
    mov al, 10
    out DEBUG_PORT, al
    pop bx
    ; Give #DB a valid 16-bit protected-mode gate before enabling paging.
    lgdt [gdtr]
    lidt [idtr]
    mov eax, cr0
    or al, 1
    mov cr0, eax
    jmp 0x08:.protected
.protected:
    mov ax, 0x10
    mov ds, ax
    mov es, ax
    mov ss, ax
    ; Identity-map the first 4 MiB. The running program, exception stack and
    ; both paging levels all fit in it. The CPU reaches ICEBP with PG set,
    ; so its oracle fallback remains active until paging has been cleared.
    xor eax, eax
    mov di, 0x1000
    mov cx, 4096
    rep stosw
    mov dword [0x1000], 0x2003
    mov di, 0x2000
    mov eax, 3
    mov cx, 1024
.pages:
    stosd
    add eax, 0x1000
    loop .pages
    mov eax, 0x1000
    mov cr3, eax
    mov eax, cr0
    or eax, 0x80000000
    mov cr0, eax
    db 0xf1                 ; ICEBP: paged protected-mode oracle stretch
    mov eax, cr0
    and eax, 0x7ffffffe     ; disable paging and return to real mode
    mov cr0, eax
    jmp 0:.real
.real:
    mov al, 'P'
    out DEBUG_PORT, al
    mov al, ' '
    out DEBUG_PORT, al
    mov al, [traps]
    add al, '0'
    out DEBUG_PORT, al
    mov al, 10
    out DEBUG_PORT, al
    mov al, 0x55
    call result
    mov si, done
    call print
.halt:
    cli
    hlt
    jmp .halt

int1:
    inc byte [traps]
    iret

; "R <AL> <flags>" and a newline.
result:
    lahf
    push ax
    mov al, 'R'
    out DEBUG_PORT, al
    mov al, ' '
    out DEBUG_PORT, al
    pop ax
    push ax
    call hex
    mov al, ' '
    out DEBUG_PORT, al
    pop ax
    mov al, ah
    call hex
    mov al, 10
    out DEBUG_PORT, al
    ret

; AL as two hex digits.
hex:
    push ax
    shr al, 4
    call digit
    pop ax
    and al, 0x0f
digit:
    add al, '0'
    cmp al, '9'
    jbe .out
    add al, 'a' - '9' - 1
.out:
    out DEBUG_PORT, al
    ret

; The zero-terminated string at DS:SI.
print:
    lodsb
    test al, al
    jz .done
    out DEBUG_PORT, al
    jmp print
.done:
    ret

done:
    db 'DONE', 10, 0

align 8
gdt:
    dq 0
    dq 0x00009a000000ffff   ; 16-bit ring-0 code, base 0
    dq 0x000092000000ffff   ; 16-bit ring-0 data, base 0
gdt_end:
gdtr:
    dw gdt_end - gdt - 1
    dd gdt
idt:
    dq 0
    dw int1, 0x08
    db 0, 0x86              ; present 286 interrupt gate for #DB
    dw 0
idt_end:
idtr:
    dw idt_end - idt - 1
    dd idt

    times 510 - ($ - $$) db 0
    dw 0xaa55
