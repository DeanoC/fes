; SPDX-License-Identifier: MIT
; The latency bench's program (scripts/test_fes_i386_latency.py): one loop per
; row of the latency table that CPU Phase 4 changes (docs/i386-cpu-design.md,
; "Phase 4: speed"). lockstep_tb's governor mode runs it on the RTL alone at
; 0000:1000h under max, in the governor bench's form: each loop runs between
; two marks on port 80h, 2n before and 2n + 1 after, and counts BP down. Apart
; from the first, which is the bare DEC BP and JNZ loop, the three after
; call_alt's and V86 mode's INT, which run their case once, each iteration runs its
; case eight times. The last two run in V86 mode. Port 8Fh ends the program.

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

    ; 14: one indirect CALL through a register and RET an iteration, the
    ; CALL alone in its 16-byte block but for the loop's DEC and JNZ.
    mov bx, near_return
    mark 28
    mov bp, 8000
    align 16, nop
.call_ind1:
    call bx
    dec bp
    jnz .call_ind1
    mark 29

    ; 15: one indirect JMP through a register an iteration, to a block that
    ; counts down and jumps back.
    mov bx, jmp_target
    mark 30
    mov bp, 8000
    align 16, nop
.jmp_ind1:
    jmp bx
.jmp_done:
    mark 31

    ; 16: as 14, but the CALL's target changes every iteration, between two
    ; RETs (XOR BX, SI swaps them).
    mov bx, near_return
    mov si, (near_return - $$ + 0x1000) ^ (near_return2 - $$ + 0x1000)
    mark 32
    mov bp, 8000
    align 16, nop
.call_alt:
    call bx
    xor bx, si
    dec bp
    jnz .call_alt
    mark 33

    jmp v86_section             ; 3 bytes, as OUT DONE, AL and HLT were: the loops above keep their layout

    align 16, nop
jmp_target:
    dec bp
    jnz more.jmp_ind1
    jmp more.jmp_done

    align 16, nop
near_return2:
    ret

v86_section:
    ; 17-18: V86 mode (Phase 5's slice 3). Into protected mode, TR the TSS
    ; below (its I/O permission bitmap all clear, ring-0 stack 10h:7000h),
    ; then IRETD to V86 mode at IOPL 3: CS 0100h (this code's base, 1000h),
    ; SS 0 and SP 6000h. The marks and the end go through the bitmap.
    lgdt [gdt_ptr]
    lidt [idt_ptr]
    mov eax, cr0
    or al, 1
    mov cr0, eax
    jmp 0x08:.pm16
.pm16:
    mov ax, 0x10
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov ax, 0x20
    ltr ax
    push dword 0                ; GS
    push dword 0                ; FS
    push dword 0                ; DS
    push dword 0                ; ES
    push dword 0                ; SS
    push dword 0x6000           ; ESP
    push dword 0x00023002       ; EFLAGS: VM, IOPL 3
    push dword 0x0100           ; CS
    push dword v86_code - 0x1000  ; IP
    iretd

v86_code:
    ; 17: INT 30h from V86 mode through a DPL-3 386 interrupt gate to an
    ; IRETD in 32-bit code at CPL 0, back to V86 mode: a delivery from V86
    ; mode and the IRETD to it, one an iteration.
    mark 34
    mov bp, 1000
.v86_int:
    int 0x30
    dec bp
    jnz .v86_int
    mark 35

    ; 18: IN in V86 mode, which reads the TSS's I/O permission bitmap.
    mark 36
    mov bp, 1000
.v86_in:
%rep UNROLL
    in al, 0x61
%endrep
    dec bp
    jnz .v86_in
    mark 37

    out DONE, al
    hlt

    align 16, nop
v86_handler32:                  ; 18h:v86_handler32, 32-bit code
    db 0xcf                     ; IRETD (no 66h in 32-bit code)

    align 8, db 0
gdt:
    dq 0
    dq 0x00009b000000ffff       ; 08h: 16-bit code, base 0, 64 KiB
    dq 0x000093000000ffff       ; 10h: data, base 0, 64 KiB
    dq 0x00cf9b000000ffff       ; 18h: 32-bit code, flat
    dw 0x007f, tss, 0x8900, 0   ; 20h: a 386 TSS of 128 bytes at tss (below 64 KiB)
gdt_end:
gdt_ptr:
    dw gdt_end - gdt - 1
    dd gdt
idt_ptr:
    dw 0x31 * 8 - 1
    dd idt
    align 8, db 0
idt:
    times 0x30 dq 0
    dw v86_handler32, 0x18, 0xee00, 0  ; 30h: a 386 interrupt gate at DPL 3
    align 8, db 0
tss:
    dd 0, 0x7000, 0x10          ; back link, ESP0, SS0
    times 22 dd 0
    dw 0, 0x68                  ; 64h: the T bit; 66h: the bitmap's base
    times 0x80 - 0x68 db 0      ; the bitmap: ports 0-BFh allowed
