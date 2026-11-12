; SPDX-License-Identifier: MIT
; X87TEST.COM: a 387's unmasked exception on the AT error path (#1768 slice
; 0; docs/validation/2026-11-13-i386-x87-s0/summary.md). scripts/sim_fes_pc386.py
; --suite x87 runs it at the FreeDOS prompt on the oracle, whose 387 is wired
; as on an AT (Oracle::set_at_coprocessor). Each check prints
;   X87 <name> <measured> ok
; (or FAIL) to the screen and port E9h, and the run ends with "X87 DONE ok"
; or "X87 DONE FAIL".
;
; On an AT the 387's ERROR# drives IRQ 13 and latches BUSY# until a write to
; port F0h; the CPU's own ERROR# input is tied inactive, so no exception 16
; (80387 Programmer's Reference Manual 231917-001, Appendix F; IBM PC AT
; Technical Reference 6183355, "NMI and Coprocessor Controls" and Section 2).
; Every check divides 1 by 0 with divide-by-zero unmasked (control word
; 037Bh), which sets the status word's ES and ZE:
;   bios    the BIOS's INT 75h handler writes F0h, sends EOI and runs INT 02h,
;           which this program hooks: it must run once and see ES and ZE.
;   raw     with this program's INT 75h handler, which writes F0h and sends
;           EOI, the interrupt comes after FDIVP and its frame must return to
;           the FSTP after it; that FSTP then runs with ES still set, which
;           FNSTSW AX must show.
;   held    with IRQ 13 masked, FSTP after FDIVP holds on the BUSY# latch; the
;           timer interrupt is taken while it holds, and its frame must
;           return to that FSTP. The hooked INT 08h writes F0h and FSTP runs.
;   f1      OUT F1h resets the 387: FNSTCW must give 037Eh and FNSTSW AX
;           0081h, a 387's state after RESET, ERROR# active.
;
; Assemble: nasm -f bin -o X87TEST.COM x87_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9

; FNINIT, unmask divide-by-zero, and divide 1 by 0: ES and ZE set, ERROR#.
; Inline, so that an interrupt after FDIVP returns to the next instruction.
%macro divide 0
        fninit
        fldcw [cw_zm]
        fld1
        fldz
        fdivp st1, st0
%endmacro

start:  mov ax, 0x3502                  ; keep INT 02h, 08h and 75h
        int 0x21
        mov [old02], bx
        mov [old02 + 2], es
        mov ax, 0x3508
        int 0x21
        mov [old08], bx
        mov [old08 + 2], es
        mov ax, 0x3575
        int 0x21
        mov [old75], bx
        mov [old75 + 2], es

        ; ---- bios: the BIOS's INT 75h, then INT 02h ----
        mov ax, 0x2502
        mov dx, nmi
        int 0x21
        sti
        divide
        fstp dword [scratch]
        fnclex
        movzx eax, word [nmi_calls]
        mov si, name_bios
        mov ecx, 1
        mov edx, 1
        mov di, unit_calls
        call check
        movzx eax, word [nmi_sw]
        and eax, 0x84                   ; ES and ZE
        mov si, name_bios_sw
        mov ecx, 0x84
        mov edx, 0x84
        mov di, unit_hex
        call check

        ; ---- raw: this program's INT 75h ----
        mov ax, 0x2575
        mov dx, irq13
        int 0x21
        mov ax, [raw_calls]
        mov [raw_before], ax
        divide
raw_fstp:
        fstp dword [scratch]
        fnstsw ax
        mov [raw_sw], ax
        fnclex
        mov ax, [raw_calls]
        sub ax, [raw_before]
        movzx eax, ax
        mov si, name_raw
        mov ecx, 1
        mov edx, 1
        mov di, unit_calls
        call check
        movzx eax, word [raw_ip]
        mov si, name_raw_ip
        mov ecx, raw_fstp
        mov edx, raw_fstp
        mov di, unit_hex
        call check
        movzx eax, word [raw_sw]
        and eax, 0x84
        mov si, name_raw_sw
        mov ecx, 0x84
        mov edx, 0x84
        mov di, unit_hex
        call check

        ; ---- held: the timer while FSTP holds ----
        in al, 0xa1                     ; mask IRQ 13
        or al, 0x20
        out 0xa1, al
        mov ax, 0x2508
        mov dx, timer
        int 0x21
        divide
        mov byte [armed], 1
held_fstp:
        fstp dword [scratch]
        fnclex
        mov ax, 0x2508                  ; the BIOS's INT 08h again
        push ds
        lds dx, [old08]
        int 0x21
        pop ds
        movzx eax, word [held_ip]
        mov si, name_held
        mov ecx, held_fstp
        mov edx, held_fstp
        mov di, unit_hex
        call check

        ; ---- f1: a 387 RESET ----
        out 0xf1, al
        fnstcw [f1_cw]
        fnstsw ax
        mov [f1_sw], ax
        fninit
        movzx eax, word [f1_cw]
        mov si, name_f1_cw
        mov ecx, 0x037e
        mov edx, 0x037e
        mov di, unit_hex
        call check
        movzx eax, word [f1_sw]
        mov si, name_f1_sw
        mov ecx, 0x0081
        mov edx, 0x0081
        mov di, unit_hex
        call check
        in al, 0xa1                     ; unmask IRQ 13
        and al, 0xdf
        out 0xa1, al

        push ds                         ; restore INT 02h and 75h
        mov ax, 0x2502
        lds dx, [old02]
        int 0x21
        pop ds
        push ds
        mov ax, 0x2575
        lds dx, [old75]
        int 0x21
        pop ds

        mov si, name_done
        xor eax, eax
        movzx ecx, byte [failed]
        xor edx, edx
        mov di, unit_none
        call check
        mov ax, 0x4c00
        or al, [failed]
        int 0x21

; INT 02h (from the BIOS's INT 75h): count, keep the status word, clear it.
nmi:    inc word [cs:nmi_calls]
        fnstsw [cs:nmi_sw]
        fnclex
        iret

; INT 75h (IRQ 13): count, keep the frame's IP, clear the latch, EOI.
irq13:  push bp
        mov bp, sp
        push ax
        mov ax, [bp + 2]
        mov [cs:raw_ip], ax
        inc word [cs:raw_calls]
        out 0xf0, al
        mov al, 0x20
        out 0xa0, al
        out 0x20, al
        pop ax
        pop bp
        iret

; INT 08h while armed: keep the frame's IP, clear the latch, then the BIOS's.
timer:  cmp byte [cs:armed], 0
        je .chain
        push bp
        mov bp, sp
        push ax
        mov ax, [bp + 2]
        mov [cs:held_ip], ax
        mov byte [cs:armed], 0
        out 0xf0, al
        pop ax
        pop bp
.chain: jmp far [cs:old08]

; Prints "X87 <name SI> <EAX><unit DI> ok" if ECX <= EAX <= EDX, else FAIL.
; unit_hex prints EAX as four hex digits and an h; the DONE line passes EAX =
; 0 and ECX = the failure flag, so it fails after any failed check, and
; unit_none prints no value. Keeps every register.
check:  pushad
        mov [value], eax
        mov [lowest], ecx
        mov [highest], edx
        mov dx, msg_x87
        call say
        mov dx, si
        call say
        cmp di, unit_none
        je .judge
        mov dx, msg_space
        call say
        cmp di, unit_hex
        je .hex
        call say_number
        jmp .unit
.hex:   call say_hex
.unit:  mov dx, di
        call say
.judge: mov eax, [value]
        mov dx, msg_ok
        cmp eax, [lowest]
        jb .fail
        cmp eax, [highest]
        jbe .print
.fail:  mov byte [failed], 1
        mov dx, msg_fail
.print: call say
        mov dx, msg_newline
        call say
        mov al, 10
        out DEBUG_PORT, al
        popad
        ret

; Prints [value] in decimal. Keeps every register.
say_number:
        pushad
        mov eax, [value]
        mov di, number_end
        mov ebx, 10
.digit: xor edx, edx
        div ebx
        add dl, '0'
        dec di
        mov [di], dl
        test eax, eax
        jnz .digit
        mov dx, di
        call say
        popad
        ret

; Prints the low word of [value] as four hex digits. Keeps every register.
say_hex:
        pushad
        mov dx, [value]
        mov di, number_end - 4
        mov cx, 4
.nibble:
        rol dx, 4
        mov bx, dx
        and bx, 0x0f
        mov al, [hex_digits + bx]
        mov [di], al
        inc di
        loop .nibble
        mov dx, number_end - 4
        call say
        popad
        ret

; Prints the $-terminated string at DX with INT 21h AH=09h and writes it,
; without CR and LF, to port E9h.
say:    push ax
        push si
        mov ah, 0x09
        int 0x21
        mov si, dx
.e9:    lodsb
        cmp al, '$'
        je .done
        cmp al, 13
        je .e9
        cmp al, 10
        je .e9
        out DEBUG_PORT, al
        jmp .e9
.done:  pop si
        pop ax
        ret

; ---- Data -----------------------------------------------------------------

cw_zm:         dw 0x037b                ; all masked but divide-by-zero
failed:        db 0
armed:         db 0
nmi_calls:     dw 0
nmi_sw:        dw 0
raw_calls:     dw 0
raw_before:    dw 0
raw_ip:        dw 0
raw_sw:        dw 0
held_ip:       dw 0
f1_cw:         dw 0
f1_sw:         dw 0
old02:         dd 0
old08:         dd 0
old75:         dd 0
scratch:       dd 0
value:         dd 0
lowest:        dd 0
highest:       dd 0
number:        times 10 db 0
number_end:    db '$'
hex_digits:    db '0123456789ABCDEF'
msg_x87:       db 'X87 $'
msg_space:     db ' $'
msg_ok:        db ' ok$'
msg_fail:      db ' FAIL$'
msg_newline:   db 13, 10, '$'
unit_hex:      db 'h$'
unit_calls:    db ' calls$'
unit_none:     db '$'
name_bios:     db 'bios$'
name_bios_sw:  db 'bios-sw$'
name_raw:      db 'raw$'
name_raw_ip:   db 'raw-ip$'
name_raw_sw:   db 'raw-sw$'
name_held:     db 'held$'
name_f1_cw:    db 'f1-cw$'
name_f1_sw:    db 'f1-sw$'
name_done:     db 'DONE$'
