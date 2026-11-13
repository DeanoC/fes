; SPDX-License-Identifier: MIT
; rtl-stops exercises both the current --cpu rtl stop inventory and the
; #1750 oracle-handoff regression. The same boot sector runs on both CPUs.
;
; The BIOS's initial SMSW is recorded as all of AX plus FLAGS. Only CR0 bits
; 5--7 lack a physical 386 measurement (#1748). A later CR0 write is checked
; with SMSW too. Two real-mode ICEBPs and one paged protected-mode ICEBP must
; hand off to the oracle and restart the RTL. LOADALL still raises #UD on the
; oracle; the unimplemented far JMP call gates stop once in protected mode
; and once under paging at an aliased linear address (#1722), whose bytes
; the bench reads through the page tables. The oracle handles each stop
; (LOADALL raises #UD) and runs protected-mode stretches until RTL can restart.
; Keep this boot sector and sim_fes_pc386.py's ordered stop inventory aligned
; as RTL instruction coverage changes.

bits 16
cpu 386

DEBUG_PORT equ 0xe9
traps equ 0x0500             ; counter, outside the boot sector
PD  equ 0x1000              ; page directory
PT0 equ 0x2000              ; first 4 MiB, identity
PT1 equ 0x3000              ; only ALIAS + 7000h maps back to 7000h
ALIAS equ 0x400000

org 0x7c00
start:
    cli
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, 0x7c00
    mov word [1 * 4], int1
    mov word [1 * 4 + 2], 0
    mov word [6 * 4], ud
    mov word [6 * 4 + 2], 0
    mov byte [traps], 0

    smsw ax                 ; preserve and report the full 16-bit result
    call result
    db 0xf1                 ; first real-mode oracle handoff
    db 0xf1                 ; second site proves the RTL restarted
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
    mov cr0, eax
    smsw ax
    call cr0_result
    db 0x0f, 0x07           ; LOADALL: #UD on Bochs at CPU level 3

    cli
    lgdt [gdtr]
    lidt [idtr]
    mov eax, cr0
    or al, 1
    mov cr0, eax
    jmp 0x08:pm
pm:
    mov ax, 0x10
    mov ds, ax
    mov es, ax
    mov ss, ax
    jmp 0x18:0              ; first far JMP through a call gate
gate1:
    mov al, 'G'
    call line

    ; Map the first 4 MiB identity, then alias this sector at 407000h.
    xor ax, ax
    mov di, PD
    mov cx, 0x1800
    rep stosw
    mov di, PT0
    mov eax, 3
.fill:
    mov [di], eax
    add eax, 0x1000
    add di, 4
    cmp di, PT0 + 0x1000
    jb .fill
    mov dword [PD], PT0 | 3
    mov dword [PD + 4], PT1 | 3
    mov dword [PT1 + 7 * 4], 0x7000 | 3
    mov eax, PD
    mov cr3, eax
    mov eax, cr0
    or eax, 0x80000000
    mov cr0, eax
    jmp 0x20:alias
alias:
    jmp 0x28:0              ; paged far JMP through a call gate
gate2:
    mov al, 'P'
    call line
    mov eax, cr0
    and eax, 0x7fffffff
    mov cr0, eax            ; the second main oracle stretch ends here

    ; Repeat the aliased fetch for ICEBP while paging is still enabled. The
    ; far jump back to CS 08h lets the RTL restart after paging is cleared.
    mov eax, cr0
    or eax, 0x80000000
    mov cr0, eax
    jmp 0x20:alias_icebp
alias_icebp:
    db 0xf1                 ; third ICEBP: paged protected-mode handoff
    jmp 0x08:after_paged
after_paged:
    mov eax, cr0
    and eax, 0x7fffffff
    mov cr0, eax
    mov eax, cr0
    and al, 0xfe
    mov cr0, eax
    jmp 0:real
real:
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov al, 'P'
    out DEBUG_PORT, al
    mov al, ' '
    out DEBUG_PORT, al
    mov al, [traps]
    add al, '0'
    out DEBUG_PORT, al
    mov al, 10
    out DEBUG_PORT, al
    mov si, done
.print:
    lodsb
    test al, al
    jz .halt
    out DEBUG_PORT, al
    jmp .print
.halt:
    cli
    hlt
    jmp .halt

; Real-mode IVT vector 1 and the protected-mode #DB gate both target here.
int1:
    inc byte [traps]
    iret

; LOADALL is undefined at Bochs CPU level 3; skip its two-byte encoding.
ud:
    push bp
    mov bp, sp
    add word [bp + 2], 2
    pop bp
    mov al, 'U'
    call line
    iret

; "R <AX> <FLAGS>" and newline. Preserve SMSW's high byte before LAHF.
result:
    push bx
    push dx
    mov bx, ax
    lahf
    mov dl, ah
    mov al, 'R'
    out DEBUG_PORT, al
    mov al, ' '
    out DEBUG_PORT, al
    mov al, bh
    call hex
    mov al, bl
    call hex
    mov al, ' '
    out DEBUG_PORT, al
    mov al, dl
    call hex
    mov al, 10
    out DEBUG_PORT, al
    pop dx
    pop bx
    ret

; "C <AX>" after the explicit CR0 write.
cr0_result:
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

; AL followed by newline.
line:
    out DEBUG_PORT, al
    mov al, 10
    out DEBUG_PORT, al
    ret

done:
    db 'DONE', 10, 0

; 08h code, 10h data, call gates 18h/28h, and 20h code at ALIAS.
align 8
gdt:
    dq 0
    dw 0xffff, 0
    db 0, 0x9a, 0, 0
    dw 0xffff, 0
    db 0, 0x92, 0, 0
    dw gate1, 0x08
    db 0, 0x84, 0, 0
    dw 0xffff, ALIAS & 0xffff
    db ALIAS >> 16, 0x9a, 0, 0
    dw gate2, 0x08
    db 0, 0x84, 0, 0
gdtr:
    dw gdtr - gdt - 1
    dd gdt

; Only the protected-mode #DB vector is used after the PE transition.
align 8
idt:
    dq 0
    dw int1, 0x08
    db 0, 0x86
    dw 0
idt_end:
idtr:
    dw idt_end - idt - 1
    dd idt

    times 510 - ($ - $$) db 0
    dw 0xaa55
