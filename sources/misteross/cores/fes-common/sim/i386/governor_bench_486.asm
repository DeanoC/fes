; SPDX-License-Identifier: MIT
; The governor bench's 486 program (scripts/test_fes_i386_governor.py
; --i486), which lockstep_tb's governor mode runs on the RTL with the 486
; identity (I486 = 1) at 0000:1000h, after governor_bench.asm. Each
; benchmark is a loop of the 486 identity's instructions between two marks on
; port 80h, 2n before and 2n + 1 after; BENCHES_486 in
; test_fes_i386_governor.py lists every instruction between them with its
; class (governor.toml, "The i486 identity's instructions"). Port 8Fh ends
; the program. The 386 identity raises #UD at each of these, so the 386 bench
; does not run it.

bits 16
cpu 486
org 0x1000

MARK equ 0x80
DONE equ 0x8f
DATA equ 0x8000                 ; scratch memory

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
    mov dword [DATA], 0x12345678
    mov dword [DATA + 4], 0
    mov si, DATA

    ; 0: BSWAP of four registers.
    mark 0
    mov bp, 3000
.bswap:
    bswap eax
    bswap ebx
    bswap ecx
    bswap edx
    dec bp
    jnz .bswap
    mark 1

    ; 1: XADD, register forms of 16 and 32 bits and a memory form.
    mark 2
    mov bp, 2000
.xadd:
    xadd ax, bx
    xadd ecx, edx
    xadd [si], ax
    dec bp
    jnz .xadd
    mark 3

    ; 2: CMPXCHG, registers equal and unequal, memory equal and unequal.
    ; DI and the dword at DATA + 4 are made equal to the accumulator first;
    ; CX and the dword at DATA hold other values.
    mov ecx, 0x5a5a5a5a
    mov dword [DATA], 0x5a5a5a5a
    mark 4
    mov bp, 2000
.cmpxchg:
    xor eax, eax
    mov edi, eax
    mov [si + 4], eax
    cmpxchg edi, ebx            ; equal
    cmpxchg ecx, ebx            ; unequal: EAX takes ECX
    xor eax, eax
    cmpxchg [si + 4], ebx       ; equal
    cmpxchg [si], ebx           ; unequal
    dec bp
    jnz .cmpxchg
    mark 5

    ; 3: CPUID of leaf 1 and of leaf 0.
    mark 6
    mov bp, 500
.cpuid:
    mov eax, 1
    cpuid
    xor eax, eax
    cpuid
    dec bp
    jnz .cpuid
    mark 7

    ; 4: INVD and WBINVD.
    mark 8
    mov bp, 500
.cache:
    invd
    wbinvd
    dec bp
    jnz .cache
    mark 9

    ; 5: INVLPG of a memory operand, with paging off.
    mark 10
    mov bp, 1000
.invlpg:
    invlpg [si]
    invlpg [si + 0x1000]
    dec bp
    jnz .invlpg
    mark 11

    out DONE, al
    hlt
