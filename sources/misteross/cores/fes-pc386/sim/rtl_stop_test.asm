; SPDX-License-Identifier: MIT
; pc386 RTL stop test disk: machine_tb --cpu rtl's stop path, through what
; fes_i386_cpu still stops on as not built (#1721). One boot sector;
; scripts/sim_fes_pc386.py --suite rtl-stops boots it on the oracle and on the
; RTL and compares what each writes to port E9h.
;
;   LOADALL (0F 07) in real mode: the one encoding of the 386 the RTL does
;   not build. The oracle runs it as Bochs at CPU level 3 does, as #UD, whose
;   handler here skips its two bytes; the RTL restarts in that handler.
;   A far JMP through a call gate (EA, gate 18h) at CPL 0 in protected mode,
;   a form the RTL does not build: the oracle runs the JMP, and the RTL
;   restarts at the gate's target in protected mode.
;   The same under paging (gate 28h), from a code segment whose base maps to
;   this sector through a second page table, so the stop's linear address is
;   not its physical one and the bench reads its bytes through the page
;   tables (#1722). The oracle runs on until paging is off, the stretch the
;   RTL does not restart in, and the RTL restarts in protected mode.
;
; Initial SMSW reports full AX and the LAHF flags byte, and SMSW is checked after a CR0
; write. Initial CR0 bits 5--7 remain unmeasured on a physical 386 (#1748).
; Each part writes a line: "U" from the #UD handler, "G" and "P" at the two
; gates' targets; "DONE" back in real mode ends the run. If the RTL comes to
; build LOADALL or far JMP through a call gate, this sector needs another
; stop.

bits 16
cpu 386

DEBUG_PORT equ 0xe9
PD  equ 0x1000              ; the page directory
PT0 equ 0x2000              ; the first 4 MiB, identity
PT1 equ 0x3000              ; 400000h-7FFFFFh: only ALIAS + 7000h, to 7000h
ALIAS equ 0x400000          ; code segment 20h's base

org 0x7c00
start:
    cli
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, 0x7c00
    mov word [6 * 4], ud
    mov word [6 * 4 + 2], 0
    smsw ax
    call result
    mov eax, 0x10
    mov cr0, eax
    smsw ax
    call cr0_result
    sti
    db 0x0f, 0x07           ; LOADALL: the first stop

    cli
    lgdt [gdtr]
    mov eax, cr0
    or al, 1
    mov cr0, eax
    jmp 0x08:pm
pm:
    mov ax, 0x10
    mov ds, ax
    mov es, ax
    mov ss, ax
    jmp 0x18:0              ; through call gate 18h: the second stop
gate1:
    mov al, 'G'
    call line

    ; The page tables, from zero: PT0 identity, PT1's entry 7 to 7000h.
    xor ax, ax
    mov di, PD
    mov cx, 0x1800
    rep stosw
    mov di, PT0
    mov eax, 3              ; present, writable
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
    jmp 0x20:alias          ; this code at linear ALIAS + alias
alias:
    jmp 0x28:0              ; through call gate 28h, under paging: the third stop
gate2:
    mov al, 'P'
    call line
    mov eax, cr0
    and eax, 0x7fffffff
    mov cr0, eax            ; paging off: the RTL restarts after this

    mov eax, cr0
    and al, 0xfe
    mov cr0, eax
    jmp 0:real
real:
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    sti
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

; #UD (LOADALL on the oracle): skip its two bytes.
ud:
    push bp
    mov bp, sp
    add word [bp + 2], 2
    pop bp
    mov al, 'U'
    call line
    iret

; "R <AX> <LAHF flags byte>" and newline. Preserve SMSW's high byte before LAHF.
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

; AL and a newline.
line:
    out DEBUG_PORT, al
    mov al, 10
    out DEBUG_PORT, al
    ret

done:
    db 'DONE', 10, 0

; 08h code, 10h data (16-bit, base 0, 64 KiB); 18h and 28h 286 call gates,
; DPL 0, to gate1 and gate2; 20h code, base ALIAS.
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

    times 510 - ($ - $$) db 0
    dw 0xaa55
