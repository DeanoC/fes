; SPDX-License-Identifier: MIT
; pc386 RTL stop test disk: machine_tb --cpu rtl's stop path, through
; instructions fes_i386_cpu had not built when this was written. One boot
; sector; scripts/sim_fes_pc386.py --suite rtl-stops boots it on the oracle
; and on the RTL and compares what each writes to port E9h.
;
;   SMSW (0F 01 /4) at two sites: two stops, each run on the oracle, and the
;   RTL restarted after it with the result. The second feeds LMSW (0F 01 /6),
;   which sets PE, so the oracle runs on in protected mode with the real-mode
;   segment caches until MOV CR0 clears PE, and the RTL restarts in real mode
;
; The stops are on protected-mode instructions, which stay unbuilt until the
; CPU's protected-mode phase; the real-mode instructions it used before were
; built one by one.
;
; Each result is "R <AL> <flags>" (FLAGS' low byte, as LAHF reads it), in hex.
; "DONE" ends the run.

bits 16
cpu 386

DEBUG_PORT equ 0xe9

org 0x7c00
start:
    cli
    xor ax, ax
    mov ds, ax
    mov ss, ax
    mov sp, 0x7c00
    sti
    smsw ax                 ; CR0's low word, PE clear
    call result
    smsw ax
    or al, 1
    lmsw ax
    mov eax, cr0
    and al, 0xfe
    mov cr0, eax
    jmp short .real
.real:
    mov al, 0x55
    call result
    mov si, done
    call print
.halt:
    cli
    hlt
    jmp .halt

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

    times 510 - ($ - $$) db 0
    dw 0xaa55
