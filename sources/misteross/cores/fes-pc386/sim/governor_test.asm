; SPDX-License-Identifier: MIT
; pc386 governor test disk: one boot sector that times the CPU against the
; BIOS timer tick (scripts/sim_fes_pc386.py --suite governor on pc386_system,
; and the governor's kit run, which launch it under max and presets).
;
;   1. Turbo Pascal's CRT unit calibrates Delay at start: it waits for the
;      timer tick (the byte at 0040:006Ch) to change, counts passes of its
;      delay loop until it changes again, and divides by 55 for passes a
;      millisecond (DelayCnt). This is an equivalent loop, not Borland's
;      code: DX:AX counts down from FFFFFFFFh while the byte stays as BL
;      holds it, with SUB, SBB, JC, CMP and JE.
;   2. The same loop over the next 18 ticks, about a second.
;
; It prints "GOV TP=<passes> DC=<DelayCnt> C18=<passes>", in hex, on the
; screen and to port E9h, and keeps the three in CMOS 70h-79h, low byte
; first (the BIOS uses nothing above 3Fh but 5Bh-5Dh, and the machine
; 60h-62h), with a boot count in 7Ah and A5h in 7Bh once they are there. A
; kit run reads them from the fes.pc386.machine record that Stop saves. Last
; it prints "GOV DONE" and halts with interrupts on.

bits 16
cpu 386
org 0x7c00

DEBUG_PORT equ 0xe9
TICK equ 0x046c

start:
    cli
    xor ax, ax
    mov ds, ax
    mov es, ax
    mov ss, ax
    mov sp, 0x7c00
    cld
    sti
    mov ax, 0x0003              ; 80 x 25 text, cleared
    int 0x10
    mov al, 0x7a                ; count the boot, and clear the finished flag
    call cmos_read
    inc al
    mov ah, al
    mov al, 0x7a
    call cmos_write
    mov ax, 0x007b
    call cmos_write

    ; 1. The calibration: the passes in one tick.
    mov di, TICK
    mov bl, [es:di]
.edge:
    cmp bl, [es:di]
    je .edge
    call passes
    mov [tp], ax
    mov [tp + 2], dx
    mov cx, 55
    div cx
    mov [dc], ax

    ; 2. The passes in the next 18 ticks.
    mov cx, 18
.ticks:
    call passes
    add [c18], ax
    adc [c18 + 2], dx
    loop .ticks

    mov si, tp                  ; tp, dc and c18 into CMOS 70h-79h
    mov bx, 0x0a70
.save:
    mov al, bl
    mov ah, [si]
    call cmos_write
    inc si
    inc bl
    dec bh
    jnz .save
    mov ax, 0xa57b
    call cmos_write

    mov si, text_tp
    call print
    mov ax, [tp + 2]
    call hex
    mov ax, [tp]
    call hex
    mov si, text_dc
    call print
    mov ax, [dc]
    call hex
    mov si, text_c18
    call print
    mov ax, [c18 + 2]
    call hex
    mov ax, [c18]
    call hex
    mov si, text_done
    call print
.halt:
    hlt
    jmp .halt

; The delay loop's passes from now until the tick changes, in DX:AX.
passes:
    mov bl, [es:di]
    mov ax, 0xffff
    mov dx, ax
.pass:
    sub ax, 1
    sbb dx, 0
    jc .out
    cmp bl, [es:di]
    je .pass
.out:
    not ax
    not dx
    ret

; CMOS byte AL into AL; AH into CMOS byte AL.
cmos_read:
    out 0x70, al
    in al, 0x71
    ret
cmos_write:
    out 0x70, al
    mov al, ah
    out 0x71, al
    ret

; AX in four hex digits.
hex:
    mov cx, 4
.digit:
    rol ax, 4
    push ax
    and al, 0x0f
    add al, '0'
    cmp al, '9'
    jbe .put
    add al, 'a' - '9' - 1
.put:
    call put
    pop ax
    loop .digit
    ret

; The zero-terminated string at DS:SI.
print:
    lodsb
    test al, al
    jz .done
    call put
    jmp print
.done:
    ret

; AL on the screen (INT 10h teletype) and on port E9h, which takes no CR.
put:
    push ax
    push bx
    cmp al, 13
    je .screen
    out DEBUG_PORT, al
.screen:
    mov ah, 0x0e
    mov bx, 0x0007
    int 0x10
    pop bx
    pop ax
    ret

text_tp:
    db 'GOV TP=', 0
text_dc:
    db ' DC=', 0
text_c18:
    db ' C18=', 0
text_done:
    db 13, 10, 'GOV DONE', 13, 10, 0

tp: dd 0
dc: dw 0
c18: dd 0

    times 510 - ($ - $$) db 0
    dw 0xaa55
