; SPDX-License-Identifier: MIT
; FESENV.COM: prints the BLASTER line for the machine's Sound Blaster setting
; (docs/pc386-machine-design.md, "FreeDOS in the FogCast disk builder"). The
; machine mirrors the latched setting into CMOS 60h-62h at every Execution
; release: base port high byte (22h or 24h), IRQ (2, 3, 5 or 7) and DMA
; channel (1). AUTOEXEC.BAT runs FESENV into a batch file and calls it, so
; BLASTER follows the setting at every boot without editing the disk:
;
;   FESENV > C:\FDOS\FESENV.BAT
;   CALL C:\FDOS\FESENV.BAT
;
; A value outside the setting's set prints a REM line instead, which leaves
; BLASTER unset. Assemble: nasm -f bin -o FESENV.COM fesenv.asm. 8086 code.

        bits 16
        cpu 8086
        org 0x100

start:  mov dx, msg_none
        mov al, 0x60
        call cmos
        cmp al, 0x22
        je .base
        cmp al, 0x24
        jne .print
.base:  and al, 0x0f
        add al, '0'
        mov [digit_base], al
        mov al, 0x61
        call cmos
        cmp al, 2
        je .irq
        cmp al, 3
        je .irq
        cmp al, 5
        je .irq
        cmp al, 7
        jne .print
.irq:   add al, '0'
        mov [digit_irq], al
        mov al, 0x62
        call cmos
        cmp al, 1
        jne .print
        add al, '0'
        mov [digit_dma], al
        mov dx, msg_blaster
.print: mov ah, 0x09
        int 0x21
        mov ax, 0x4c00
        int 0x21

; AL = CMOS index; returns its value in AL. Bit 7 of the index stays clear,
; so NMI stays enabled.
cmos:   cli
        out 0x70, al
        in al, 0x71
        sti
        ret

msg_blaster:    db 'SET BLASTER=A2'
digit_base:     db '2', '0 I'
digit_irq:      db '7', ' D'
digit_dma:      db '1', ' T3', 13, 10, '$'
msg_none:       db 'REM FESENV: CMOS 60h-62h hold no Sound Blaster setting', 13, 10, '$'
