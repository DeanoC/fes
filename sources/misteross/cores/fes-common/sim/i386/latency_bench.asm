; SPDX-License-Identifier: MIT
; The latency bench's program (scripts/test_fes_i386_latency.py): one loop per
; row of the latency table that CPU Phase 4 changes (docs/i386-cpu-design.md,
; "Phase 4: speed"). lockstep_tb's governor mode runs it on the RTL alone at
; 0000:1000h under max, in the governor bench's form: each loop runs between
; two marks on port 80h, 2n before and 2n + 1 after, and counts BP down. Apart
; from the first, which is the bare DEC BP and JNZ loop, each iteration runs
; its case eight times. Port 8Fh ends the program.

bits 16
cpu 386
org 0x1000

MARK equ 0x80
DONE equ 0x8f
DATA equ 0x8000                 ; scratch memory
UNROLL equ 8

%macro mark 1
    mov al, %1
    out MARK, al
%endmacro

; A taken JE over a NOP.
%macro je_over 0
    je %%over
    nop
%%over:
%endmacro

start:
    cli
    cld
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, 0x7000
    mov word [DATA], DATA       ; a pointer to itself, for the AGI chain
    mov word [DATA + 2], near_return
    mov word [DATA + 4], 0

    ; 0: a taken Jcc loop, DEC BP and JNZ.
    mark 0
    mov bp, 4000
.jcc_loop:
    dec bp
    jnz .jcc_loop
    mark 1

    ; 1: taken Jcc, each over one byte.
    mark 2
    mov bp, 1000
.jcc_taken:
    cmp ax, ax
%rep UNROLL
    je_over
%endrep
    dec bp
    jnz .jcc_taken
    mark 3

    ; 2: CALL rel and RET.
    mark 4
    mov bp, 1000
.call_ret:
%rep UNROLL
    call near_return
%endrep
    dec bp
    jnz .call_ret
    mark 5

    ; 3: indirect CALL through a register.
    mov bx, near_return
    mark 6
    mov bp, 1000
.call_reg:
%rep UNROLL
    call bx
%endrep
    dec bp
    jnz .call_reg
    mark 7

    ; 4: indirect CALL through memory.
    mark 8
    mov bp, 1000
.call_mem:
%rep UNROLL
    call word [DATA + 2]
%endrep
    dec bp
    jnz .call_mem
    mark 9

    ; 5: real-mode far CALL and RETF.
    mark 10
    mov bp, 1000
.far_call:
%rep UNROLL
    call 0:far_return
%endrep
    dec bp
    jnz .far_call
    mark 11

    ; 6: LOOP, taken.
    mark 12
    mov bp, 1000
.loop:
    mov cx, UNROLL
.loop_inner:
    loop .loop_inner
    dec bp
    jnz .loop
    mark 13

    ; 7: an AGI chain: each load's result is the next one's address.
    mark 14
    mov bp, 1000
.agi_load:
    mov bx, DATA
%rep UNROLL
    mov bx, [bx]
%endrep
    dec bp
    jnz .agi_load
    mark 15

    ; 8: an ALU result into the next instruction's address.
    mark 16
    mov bp, 1000
.agi_alu:
    mov si, DATA
%rep UNROLL
    add si, 2
    mov ax, [si]
%endrep
    dec bp
    jnz .agi_alu
    mark 17

    ; 9: a store, then a load from the same address.
    mov di, DATA + 0x100
    mark 18
    mov bp, 1000
.store_load_same:
%rep UNROLL
    mov [di], ax
    mov cx, [di]
%endrep
    dec bp
    jnz .store_load_same
    mark 19

    ; 10: a store, then a load from another line.
    mov si, DATA + 0x200
    mark 20
    mov bp, 1000
.store_load_other:
%rep UNROLL
    mov [di], ax
    mov cx, [si]
%endrep
    dec bp
    jnz .store_load_other
    mark 21

    ; The later loops run after the subroutines, so the code above keeps its
    ; addresses: a JMP takes the three bytes OUT and HLT took.
    jmp near more

near_return:
    ret

far_return:
    retf

    ; Zeros, as past the program's end before these loops: the wrong path
    ; after RETF fetches them.
    times 64 db 0

more:
    ; 11: a store, then a load from the next dword of its line.
    mark 22
    mov bp, 1000
.store_load_line:
%rep UNROLL
    mov [di], ax
    mov cx, [di + 4]
%endrep
    dec bp
    jnz .store_load_line
    mark 23

    ; 12: a word store, then a byte load from its second byte.
    mark 24
    mov bp, 1000
.store_load_part:
%rep UNROLL
    mov [di], ax
    mov cl, [di + 1]
%endrep
    dec bp
    jnz .store_load_part
    mark 25

    ; 13: a store, then a load 4 KiB on, which agrees with it in address bits
    ; 11:0.
    mark 26
    mov bp, 1000
.store_load_alias:
%rep UNROLL
    mov [di], ax
    mov cx, [di + 0x1000]
%endrep
    dec bp
    jnz .store_load_alias
    mark 27

    out DONE, al
    hlt
