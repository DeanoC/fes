; SPDX-License-Identifier: MIT
; CPUPROF.COM: the short window of machine_tb --cpu-profile.
; scripts/sim_fes_pc386.py --suite cpu-profile types it at the FreeDOS prompt
; on the oracle; the RTL takes over at its fourth page flip and runs a short
; window (cores/fes-pc386/sim/cpu_profile.h). Each frame runs a little of
; every cause the profile sorts clocks into: taken Jcc, CALL and RET,
; indirect CALL, real-mode far CALL and RETF, a load into an address (AGI),
; a store then a load, multiply and divide, I/O, VGA text memory, then a
; page flip: a word OUT at 3D4h that selects CRTC register 0Ch and writes
; it. It never returns; the bench ends the run.
;
; Assemble: nasm -f bin -o CPUPROF.COM cpu_profile_test.asm.

        bits 16
        cpu 386
        org 0x100

SCRATCH equ 0x2000              ; stored to every pass, away from the code (no SMC restart)

start:
        cld
        mov ax, 0xb800
        mov es, ax
        mov word [chain], chain     ; a pointer to itself, for the AGI chain
        mov word [target], near_fn
        mov [far_ptr + 2], cs
frame:
        mov bp, 64
.work:
        call near_fn
        mov bx, near_fn
        call bx
        call word [target]
        call far [far_ptr]
        mov bx, chain
        mov bx, [bx]
        mov bx, [bx]
        mov [SCRATCH], bp
        mov ax, [SCRATCH]
        mov cx, 7
        mul cx
        div cx
        dec bp
        jnz .work
        in al, 0x61                 ; I/O
        mov di, 0
        mov cx, 16
        mov ax, 0x0741
        rep stosw                   ; VGA text memory
        mov dx, 0x3d4
        mov ax, 0x000c              ; CRTC 0Ch = 00h: a page flip
        out dx, ax
        jmp frame

near_fn:
        ret

far_fn:
        retf

        align 2
far_ptr:
        dw far_fn, 0                ; the segment is CS, set at start
target:
        dw 0
chain:
        dw 0
