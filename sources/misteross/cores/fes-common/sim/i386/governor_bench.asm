; SPDX-License-Identifier: MIT
; The governor bench's program (scripts/test_fes_i386_governor.py), which
; lockstep_tb's governor mode runs on the RTL at 0000:1000h. Each benchmark
; is a loop between two marks on port 80h, 2n before and 2n + 1 after;
; test_fes_i386_governor.py lists every instruction between them with its
; class, which gives the reference clocks the governor must charge and the
; time the loop must take at a preset. Last, the interrupt latency run: it
; marks 40h, and the bench raises INTR (vector 20h) now and then while a loop
; of REP MOVSB, DIV, PUSH and POP runs; the handler marks port 81h first.
; Port 8Fh ends the program.

bits 16
cpu 386
org 0x1000

MARK equ 0x80
IRQ_MARK equ 0x81
DONE equ 0x8f
DATA equ 0x8000                 ; scratch memory
COPY equ 0xa000

%macro mark 1
    mov al, %1
    out MARK, al
%endmacro

start:
    cli
    cld
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, 0x7000
    mov word [0x20 * 4], irq20
    mov word [0x20 * 4 + 2], 0
    mov word [0x30 * 4], int30
    mov word [0x30 * 4 + 2], 0
    mov word [DATA], 0x1234
    mov word [DATA + 2], 0
    mov si, DATA
    mov di, DATA + 0x1000
    mov bx, 0x0100

    ; 0: one-uop ALU operations.
    mark 0
    mov bp, 4000
.alu:
    add ax, bx
    xor dx, ax
    inc si
    dec di
    dec bp
    jnz .alu
    mark 1

    ; 1: loads, a load-op, a store, a read-modify-write and a compare.
    mov si, DATA
    mov di, DATA + 0x1000
    mov bx, 0x0100
    mark 2
    mov bp, 3000
.memory:
    mov ax, [si]
    add ax, [bx + si + 4]
    mov [di], ax
    add [di + 2], ax
    cmp ax, [si + 2]
    dec bp
    jnz .memory
    mark 3

    ; 2: 32-bit operands (66h), whose accesses are dwords.
    mark 4
    mov bp, 3000
.dword:
    mov eax, [si]
    add eax, ebx
    mov [di], eax
    shl eax, 3
    dec bp
    jnz .dword
    mark 5

    ; 3: branches: one not taken, a jump, a call and its return.
    mark 6
    mov bp, 3000
.branch:
    cmp ax, ax
    jne .branch_out
    jmp short .branch_on
.branch_on:
    call near_return
    dec bp
    jnz .branch
.branch_out:
    mark 7

    ; 4: the stack.
    mark 8
    mov bp, 3000
.stack:
    push ax
    push bx
    pop cx
    pop dx
    pushf
    popf
    dec bp
    jnz .stack
    mark 9

    ; 5: multiply and divide.
    mov bx, 7
    mov cx, 13
    mark 10
    mov bp, 1000
.muldiv:
    mov ax, 1234
    mul bx
    div cx
    imul ax, ax, 3
    mul bl
    dec bp
    jnz .muldiv
    mark 11

    ; 6: strings, REP and single.
    mark 12
    mov bp, 200
.string:
    mov si, DATA
    mov di, COPY
    mov cx, 32
    rep movsw
    mov cx, 16
    rep stosb
    movsb
    lodsw
    dec bp
    jnz .string
    mark 13

    ; 7: shifts and rotates.
    mov cl, 3
    mov di, DATA + 0x1000
    mark 14
    mov bp, 3000
.shift:
    shl ax, 1
    shr bx, cl
    rol dx, 4
    rcl si, 1
    sar word [di], 1
    dec bp
    jnz .shift
    mark 15

    ; 8: Turbo Pascal's CRT delay loop: count DX:AX down while the byte at
    ; ES:DI (the timer tick's, on a PC) stays as BL holds it; here nothing
    ; changes it, so the count ends it, after 5000 passes.
    mov di, 0x046c
    mov bl, [es:di]
    mark 16
    mov ax, 4999
    xor dx, dx
.crt:
    sub ax, 1
    sbb dx, 0
    jc .crt_out
    cmp bl, [es:di]
    je .crt
.crt_out:
    mark 17

    ; 9: I/O, as a speaker loop toggles port 61h.
    mark 18
    mov bp, 2000
.io:
    in al, 0x61
    xor al, 2
    out 0x61, al
    dec bp
    jnz .io
    mark 19

    ; 10: INT and IRET.
    mark 20
    mov bp, 500
.int:
    int 0x30
    dec bp
    jnz .int
    mark 21

    ; 11: a far pointer load, XCHG, LEA and the flag operations.
    mov si, DATA
    mark 22
    mov bp, 2000
.misc:
    les bx, [si]
    xchg ax, dx
    lea di, [bx + si + 8]
    clc
    cmc
    dec bp
    jnz .misc
    mark 23

    ; 12: LOOP.
    mark 24
    mov cx, 3000
.loop:
    nop
    loop .loop
    mark 25

    ; 13: PUSHA and POPA.
    mark 26
    mov bp, 500
.pusha:
    pusha
    popa
    dec bp
    jnz .pusha
    mark 27

    ; 14: 386 instructions.
    mov si, DATA
    mark 28
    mov bp, 2000
.i386:
    movzx ax, byte [si]
    setz al
    bt ax, 3
    dec bp
    jnz .i386
    mark 29

    ; 15: direct-address global load and store (A1/A3, not a ModRM EA).
    mark 30
    mov bp, 3000
.direct:
    mov ax, [0x0040]
    mov [0x0042], ax
    dec bp
    jnz .direct
    mark 31

    ; The interrupt latency run.
    mov si, DATA
    mov di, COPY
    mov bx, 7
    mark 0x40
    sti
    mov bp, 1000
.irq:
    mov cx, 64
    rep movsb
    sub si, 64
    sub di, 64
    mov ax, 5000
    xor dx, dx
    div bx
    push ax
    pop ax
    dec bp
    jnz .irq
    cli
    out DONE, al
    hlt

near_return:
    ret

int30:
    iret

irq20:
    out IRQ_MARK, al
    iret
