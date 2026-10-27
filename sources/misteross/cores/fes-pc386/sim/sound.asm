; SPDX-License-Identifier: MIT
; SOUND.COM: the pc386 audio start-up exercise for the oracle-CPU machine
; (docs/pc386-machine-design.md, phase M3). It does what DOS games do at
; start-up, in order: AdLib detection by timer 1, Sound Blaster reset and
; version, a PC speaker tone from PIT channel 2, a speaker toggled in a
; CPU-timed loop (as Ultima VI's effects are), an OPL2 phrase with rhythm,
; tremolo and vibrato, then Sound Blaster single-cycle and auto-init DMA
; playback with IRQs. The Sound Blaster settings come from CMOS 60h-62h, as
; the FogCast disk builder's AUTOEXEC.BAT reads them.
;
; Writes "MARK <name>" lines to port E9h at each section so the bench can cut
; the captured audio, prints its findings and ends with "SOUND DONE".
; Assemble: nasm -f bin -o SOUND.COM sound.asm. Needs a 386 (32-bit registers).

        bits 16
        org 0x100

start:  cld
        mov si, mark_start
        call mark

        ; ---- Settings from CMOS ------------------------------------------------
        mov al, 0x60
        call cmos
        xor ah, ah
        mov cl, 4
        shl ax, cl
        mov [sb_base], ax               ; 22h -> 220h
        mov al, 0x61
        call cmos
        mov [sb_irq], al

        ; ---- AdLib detection -----------------------------------------------------
        mov ax, 0x0460
        call adlib
        mov ax, 0x0480
        call adlib
        mov dx, 0x388
        in al, dx
        and al, 0xe0
        mov bl, al
        mov ax, 0x02ff
        call adlib
        mov ax, 0x0421
        call adlib
        xor cx, cx
        mov dx, 100                      ; 100 us
        call wait_us
        mov dx, 0x388
        in al, dx
        and al, 0xe0
        mov bh, al
        mov ax, 0x0460
        call adlib
        mov ax, 0x0480
        call adlib
        mov dx, msg_adlib_fail
        cmp bx, 0xc000
        jne .adlib_report
        mov dx, msg_adlib_ok
        mov byte [adlib_ok], 1
.adlib_report:
        call print

        ; ---- Sound Blaster reset and version -------------------------------------
        call dsp_reset
        jc .sb_fail
        mov al, 0xe1
        call dsp_write
        call dsp_read
        jc .sb_fail
        mov [dsp_major], al
        call dsp_read
        jc .sb_fail
        mov [dsp_minor], al
        mov byte [sb_ok], 1
        mov dx, msg_sb
        call print
        mov al, [dsp_major]
        call print_decimal
        mov dl, '.'
        call print_char
        mov al, [dsp_minor]
        cmp al, 10
        jae .minor
        mov dl, '0'
        call print_char
        mov al, [dsp_minor]
.minor: call print_decimal
        call newline
        jmp .speaker
.sb_fail:
        mov dx, msg_sb_fail
        call print

        ; ---- PC speaker from PIT channel 2: 880 Hz for 150 ms --------------------
.speaker:
        mov si, mark_speaker
        call mark
        mov al, 0xb6
        out 0x43, al
        mov ax, 1356                     ; 1,193,182 / 880
        out 0x42, al
        mov al, ah
        out 0x42, al
        in al, 0x61
        or al, 3
        out 0x61, al
        mov cx, 2
        mov dx, 0x49f0                   ; 150,000 us
        call wait_us
        in al, 0x61
        and al, 0xfc
        out 0x61, al
        mov dx, 20000
        xor cx, cx
        call wait_us

        ; ---- Speaker toggled by the CPU: gate low holds channel 2 high ---------------
        mov si, mark_cpu_speaker
        call mark
        mov cx, 600
.toggle:
        in al, 0x61
        xor al, 2
        out 0x61, al
        push cx
        mov cx, 4000
.spin:  loop .spin
        pop cx
        loop .toggle
        in al, 0x61
        and al, 0xfc
        out 0x61, al
        mov dx, 20000
        xor cx, cx
        call wait_us

        ; ---- OPL2 -----------------------------------------------------------------
        mov si, mark_opl2
        call mark
        mov ax, 0x0120                   ; waveform select enable
        call adlib
        mov si, instrument
.patch: lodsw
        or ax, ax
        jz .patched
        call adlib
        jmp .patch
.patched:
        mov si, melody
.note:  lodsw                            ; AL = f-number low, AH = block/f-number high
        or ax, ax
        jz .rhythm
        mov bx, ax
        mov ah, 0xa0
        mov al, bl
        call adlib
        mov ah, 0xb0
        mov al, bh
        or al, 0x20                      ; key on
        call adlib
        xor cx, cx
        mov dx, 50000                    ; 100 ms: two waits
        call wait_us
        call wait_us
        mov ah, 0xb0
        mov al, bh
        call adlib                       ; key off
        xor cx, cx
        mov dx, 20000
        call wait_us
        jmp .note
.rhythm:
        mov si, drums
.drum:  lodsw
        or ax, ax
        jz .lfo
        call adlib
        xor cx, cx
        mov dx, 60000
        call wait_us
        jmp .drum
.lfo:   mov ax, 0xbdc0                   ; deep tremolo and vibrato, rhythm off
        call adlib
        mov ax, 0x21e1                   ; channel 1 modulator: AM, VIB, EGT, MULT 1
        call adlib
        mov ax, 0x24e1                   ; carrier too
        call adlib
        mov ax, 0xa1ca
        call adlib
        mov ax, 0xb131                   ; key on, block 4, f-num 1CAh
        call adlib
        mov cx, 4
        mov dx, 0x93e0                   ; 300 ms
        call wait_us
        mov ax, 0xb111
        call adlib
        mov ax, 0x4000                   ; every operator to full attenuation,
.mute:  mov al, 0x3f                     ; so release tails stay out of the
        call adlib                       ; Sound Blaster sections
        inc ah
        cmp ah, 0x56
        jb .mute
        mov dx, 50000
        xor cx, cx
        call wait_us

        ; ---- Sound Blaster DMA ---------------------------------------------------------
        cmp byte [sb_ok], 0
        je .report
        mov si, mark_sb_single
        call mark
        ; 4000 bytes of a 1 kHz sine at 8 kHz at the first 64 KiB boundary past
        ; this program's segment: memory DOS gave it, and no DMA page crossing.
        mov ax, cs
        add ax, 0x1fff
        and ax, 0xf000
        mov [buffer_segment], ax
        push es
        mov es, ax
        xor di, di
        mov cx, 500
.fill:  mov si, sine
        movsw
        movsw
        movsw
        movsw
        loop .fill
        pop es
        call hook_irq
        mov al, 0xd1                     ; speaker on
        call dsp_write
        mov al, 0x40
        call dsp_write
        mov al, 131                      ; 1,000,000 / (256 - 131) = 8000 Hz
        call dsp_write
        mov bl, 0x49                     ; single, read, channel 1
        mov cx, 3999
        call dma1
        mov al, 0x14
        call dsp_write
        mov al, 3999 & 0xff
        call dsp_write
        mov al, 3999 >> 8
        call dsp_write
        mov al, 1
        call wait_irqs

        mov si, mark_sb_auto
        call mark
        mov bl, 0x59                     ; single, auto-init, read
        mov cx, 1999
        call dma1
        mov al, 0x48
        call dsp_write
        mov al, 999 & 0xff
        call dsp_write
        mov al, 999 >> 8
        call dsp_write
        mov al, 0x1c
        call dsp_write
        mov al, 5
        call wait_irqs
        mov al, 0xda                     ; exit auto-init after this block
        call dsp_write
        mov al, 6
        call wait_irqs
        mov al, 0xd3
        call dsp_write
        call unhook_irq

        ; ---- Report -----------------------------------------------------------------------
.report:
        mov si, mark_end
        call mark
        mov dx, msg_irqs
        call print
        mov al, [irq_count]
        call print_decimal
        call newline
        mov dx, msg_done
        call print
        ret

; ---- Helpers ----------------------------------------------------------------------

; AL = index; returns the CMOS byte.
cmos:   out 0x70, al
        in al, 0x71
        ret

; AH = register, AL = value. The AdLib needs 3.3 us after the index and 23 us
; after the data; reads of the status port give the delay, as drivers do.
adlib:  push ax
        push cx
        push dx
        mov dx, 0x388
        xchg al, ah
        out dx, al
        mov cx, 6
.d1:    in al, dx
        loop .d1
        inc dx
        mov al, ah
        out dx, al
        dec dx
        mov cx, 35
.d2:    in al, dx
        loop .d2
        pop dx
        pop cx
        pop ax
        ret

; CX:DX microseconds, counted on PIT channel 0 (mode 2, one count per
; 1/1,193,182 s), as games time short waits. The Bochs BIOS's INT 15h AH=86h
; counts port 61h bit 4, which follows PIT channel 1, and that BIOS never
; programs channel 1.
wait_us:
        pushad
        mov ax, cx
        shl eax, 16
        mov ax, dx
        imul eax, eax, 1193
        xor edx, edx
        mov ebx, 1000
        div ebx
        mov ecx, eax                     ; PIT counts to wait
        call pit_count
        mov bx, ax
.next:  call pit_count
        mov dx, bx
        sub dx, ax                       ; counts since the last read
        mov bx, ax
        movzx edx, dx
        sub ecx, edx
        jg .next
        popad
        ret

; AX = PIT channel 0's current count.
pit_count:
        pushf
        cli
        xor al, al
        out 0x43, al                     ; latch channel 0
        in al, 0x40
        mov ah, al
        in al, 0x40
        xchg al, ah
        popf
        ret

dsp_write:
        push cx
        push dx
        push ax
        mov dx, [sb_base]
        add dx, 0x0c
        mov cx, 0xffff
.busy:  in al, dx
        test al, 0x80
        jz .ready
        loop .busy
.ready: pop ax
        out dx, al
        pop dx
        pop cx
        ret

; Returns AL, or CF on a timeout.
dsp_read:
        push cx
        push dx
        mov dx, [sb_base]
        add dx, 0x0e
        mov cx, 0xffff
.wait:  in al, dx
        test al, 0x80
        jnz .data
        loop .wait
        stc
        jmp .out
.data:  sub dx, 4
        in al, dx
        clc
.out:   pop dx
        pop cx
        ret

; CF set unless the DSP answers AAh.
dsp_reset:
        mov dx, [sb_base]
        add dx, 6
        mov al, 1
        out dx, al
        in al, dx                        ; 3 us
        in al, dx
        in al, dx
        in al, dx
        in al, dx
        xor al, al
        out dx, al
        call dsp_read
        jc .fail
        cmp al, 0xaa
        je .ok
.fail:  stc
        ret
.ok:    clc
        ret

; BL = mode, CX = count - 1; the buffer at buffer_segment:0000.
dma1:   mov al, 0x05
        out 0x0a, al                     ; mask channel 1
        xor al, al
        out 0x0c, al
        mov al, bl
        out 0x0b, al
        xor al, al
        out 0x02, al
        out 0x02, al
        mov ax, [buffer_segment]
        mov al, ah
        shr al, 4
        out 0x83, al                     ; page: physical address bits 23-16
        mov al, cl
        out 0x03, al
        mov al, ch
        out 0x03, al
        mov al, 0x01
        out 0x0a, al
        ret

; The IRQ's vector: 8 + IRQ, or 0Ah for IRQ 2 (the BIOS reflects IRQ 9 there).
irq_vector:
        mov al, [sb_irq]
        cmp al, 2
        jne .master
        mov al, 0x0a
        ret
.master:
        add al, 8
        ret

hook_irq:
        call irq_vector
        mov [vector], al
        mov ah, 0x35
        int 0x21
        mov [old_vector], bx
        mov [old_vector + 2], es
        push ds
        pop es
        mov al, [vector]
        mov ah, 0x25
        mov dx, sb_isr
        int 0x21
        mov byte [irq_count], 0
        mov cl, [sb_irq]
        cmp cl, 2
        jne .master
        in al, 0xa1                      ; IRQ 9 on the slave
        and al, 0xfd
        out 0xa1, al
        in al, 0x21
        and al, 0xfb
        out 0x21, al
        ret
.master:
        mov ah, 1
        shl ah, cl
        not ah
        in al, 0x21
        and al, ah
        out 0x21, al
        ret

unhook_irq:
        cli
        mov cl, [sb_irq]
        cmp cl, 2
        jne .master
        in al, 0xa1
        or al, 0x02
        out 0xa1, al
        jmp .restore
.master:
        mov ah, 1
        shl ah, cl
        in al, 0x21
        or al, ah
        out 0x21, al
.restore:
        sti
        push ds
        mov al, [vector]
        mov ah, 0x25
        mov dx, [old_vector]
        mov ds, [old_vector + 2]
        int 0x21
        pop ds
        ret

sb_isr: push ax
        push dx
        mov dx, [cs:sb_base]
        add dx, 0x0e
        in al, dx                        ; acknowledge the DSP
        inc byte [cs:irq_count]
        mov al, 0x20
        out 0x20, al
        pop dx
        pop ax
        iret

; Waits until irq_count reaches AL, at most about 3 s of timer ticks.
wait_irqs:
        mov ah, al
        mov cx, 60
        sti
.wait:  cmp [irq_count], ah
        jae .done
        hlt
        loop .wait
.done:  ret

; SI = "MARK name" text, written to port E9h with a newline.
mark:   push ax
.next:  lodsb
        or al, al
        jz .end
        out 0xe9, al
        jmp .next
.end:   mov al, 10
        out 0xe9, al
        pop ax
        ret

print:  mov ah, 0x09
        int 0x21
        ret

print_char:
        mov ah, 0x02
        int 0x21
        ret

newline:
        mov dx, msg_newline
        jmp print

; AL as decimal.
print_decimal:
        xor ah, ah
        mov bl, 10
        xor cx, cx
.digit: div bl
        push ax
        inc cx
        xor ah, ah
        or al, al
        jnz .digit
.out:   pop ax
        mov dl, ah
        add dl, '0'
        push cx
        call print_char
        pop cx
        loop .out
        ret

; ---- Data -------------------------------------------------------------------------

; Channels 0-2: register, value pairs (AH = register, AL = value), then 0.
instrument:
        dw 0x2001, 0x2301, 0x2101, 0x2401, 0x2201, 0x2501
        dw 0x4010, 0x4300, 0x4110, 0x4400, 0x4210, 0x4500
        dw 0x60f2, 0x63f2, 0x61f2, 0x64f2, 0x62f2, 0x65f2
        dw 0x8074, 0x8374, 0x8174, 0x8474, 0x8274, 0x8574
        dw 0xe002, 0xe300, 0xc00a, 0xc10a, 0xc20a
        ; Bass drum (channel 6, operators 10h/13h), snare/hi-hat (channel 7),
        ; tom/cymbal (channel 8), with their pitches.
        dw 0x3001, 0x3301, 0x5000, 0x5300, 0x70f6, 0x73f6, 0x9047, 0x9347, 0xc604
        dw 0x3101, 0x3401, 0x5100, 0x5400, 0x71f8, 0x74f8, 0x9157, 0x9457
        dw 0x3201, 0x3501, 0x5200, 0x5500, 0x72f8, 0x75f8, 0x9257, 0x9557
        dw 0xa657, 0xb609, 0xa703, 0xb70a, 0xa801, 0xb809
        dw 0

; Channel 0 notes: f-number low, then block << 2 | f-number high; key on is added.
melody: dw 0x1158, 0x11b5, 0x1203, 0x1558, 0x1203, 0x11b5, 0x1158, 0
; BDh values in turn: rhythm on, then bass drum, snare + hi-hat, tom + cymbal.
drums:  dw 0xbd20, 0xbd30, 0xbd20, 0xbd29, 0xbd20, 0xbd26, 0xbd20, 0xbd3f, 0xbd00, 0

sine:   db 128, 199, 228, 199, 128, 57, 28, 57

mark_start:       db "MARK start", 0
mark_speaker:     db "MARK speaker", 0
mark_cpu_speaker: db "MARK cpu-speaker", 0
mark_opl2:        db "MARK opl2", 0
mark_sb_single:   db "MARK sb-single", 0
mark_sb_auto:     db "MARK sb-auto", 0
mark_end:         db "MARK end", 0
msg_adlib_ok:     db "ADLIB OK", 13, 10, "$"
msg_adlib_fail:   db "ADLIB FAIL", 13, 10, "$"
msg_sb:           db "SB DSP $"
msg_sb_fail:      db "SB FAIL", 13, 10, "$"
msg_irqs:         db "SB IRQS $"
msg_done:         db "SOUND DONE", 13, 10, "$"
msg_newline:      db 13, 10, "$"

sb_base:    dw 0x220
sb_irq:     db 7
adlib_ok:   db 0
sb_ok:      db 0
dsp_major:  db 0
dsp_minor:  db 0
irq_count:  db 0
vector:     db 0
old_vector: dw 0, 0
buffer_segment: dw 0
