; SPDX-License-Identifier: MIT
; SPKTEST.COM: PIT counter 2 as POST leaves it, and the speaker carrier a
; game gets by writing only a count (#1457; docs/pc386-machine-design.md,
; "I/O ports"). scripts/sim_fes_pc386.py --suite speaker runs it at the
; FreeDOS prompt. Each check prints
;   SPK <name> <measured><unit> ok
; (or FAIL) to the screen and port E9h, and the run ends with "SPK DONE ok"
; or "SPK DONE FAIL".
;
; An AT BIOS's POST beep leaves counter 2 in mode 3 with LSB-then-MSB counts,
; and games such as Ultima VI then write only a count to port 42h and toggle
; port 61h bits 0 (gate) and 1 (speaker) together. The checks:
;   post     the read-back status of counter 2 before this program touches
;            it: bits 5-0 must be 36h (LSB then MSB, mode 3, binary).
;   rise     with count 60 written as 3Ch, 00h and no control word, 64 times
;            the gate goes low, then high with the speaker on; port 61h bit 5
;            (counter 2's output) must read 1 right after each rise. In mode
;            3 a low gate drives the output high and a rising gate reloads
;            the count with the output still high, for 30 counts (25 us).
;   carrier  with the gate held high, the output must be count 60's square
;            wave: the time on PIT counter 0 from one change of bit 5 to the
;            80th after it, 40 periods, gives its frequency, 1,193,182 / 60 =
;            19,886 Hz.
;
; Assemble: nasm -f bin -o SPKTEST.COM speaker_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9
RISES equ 64
CHANGES equ 80                          ; 40 periods

start:  mov al, 0xe8                    ; read-back: status only, counter 2
        out 0x43, al
        in al, 0x42
        and eax, 0x3f
        mov si, name_post
        mov ecx, 0x36
        mov edx, 0x36
        mov di, unit_hex
        call check

        mov al, 0x3c                    ; count 60, LSB then MSB
        out 0x42, al
        xor al, al
        out 0x42, al
        xor bx, bx                      ; rises that read the output high
        mov cx, RISES
.rise:  in al, 0x61
        and al, 0xfc
        out 0x61, al
        in al, 0x61
        or al, 3
        out 0x61, al
        in al, 0x61
        test al, 0x20
        jz .low
        inc bx
.low:   loop .rise
        movzx eax, bx
        mov si, name_rise
        mov ecx, RISES
        mov edx, RISES
        mov di, unit_of_rises
        call check

        cli                             ; the gate is still high
        in al, 0x61
        and al, 0x20
        mov ah, al
        mov bx, -1                      ; changes after the first
        mov ecx, 400000                 ; reads before giving up
.read:  in al, 0x61
        and al, 0x20
        cmp al, ah
        je .same
        mov ah, al
        inc bx
        jnz .count
        push ax                         ; the first change starts the time
        call pit_count
        mov [before], ax
        pop ax
        jmp .same
.count: cmp bx, CHANGES
        je .timed
.same:  dec ecx
        jnz .read
        sti
        xor eax, eax                    ; no carrier
        jmp .judge
.timed: call pit_count
        sti
        mov dx, [before]
        sub dx, ax                      ; counts down, wrapping at 65536
        movzx ecx, dx
        mov eax, 1193182 * CHANGES / 2
        xor edx, edx
        div ecx                         ; Hz
.judge: mov si, name_carrier
        mov ecx, 19400
        mov edx, 20400
        mov di, unit_hz
        call check
        in al, 0x61
        and al, 0xfc
        out 0x61, al

        mov si, name_done
        xor eax, eax
        movzx ecx, byte [failed]
        xor edx, edx
        mov di, unit_none
        call check
        mov ax, 0x4c00
        or al, [failed]
        int 0x21

; AX = PIT counter 0's count.
pit_count:
        pushf
        cli
        xor al, al
        out 0x43, al                    ; latch counter 0
        in al, 0x40
        mov ah, al
        in al, 0x40
        xchg al, ah
        popf
        ret

; Prints "SPK <name SI> <EAX><unit DI> ok" if ECX <= EAX <= EDX, else FAIL.
; unit_hex prints EAX as two hex digits and an h; the DONE line passes EAX =
; 0 and ECX = the failure flag, so it fails after any failed check, and
; unit_none prints no value. Keeps every register.
check:  pushad
        mov [value], eax
        mov [lowest], ecx
        mov [highest], edx
        mov dx, msg_spk
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

; Prints the low byte of [value] as two hex digits. Keeps every register.
say_hex:
        pushad
        mov al, [value]
        mov ah, al
        shr al, 4
        and ah, 0x0f
        mov bx, hex_digits
        xlat
        xchg al, ah
        xlat
        mov [number_end - 2], ah
        mov [number_end - 1], al
        mov dx, number_end - 2
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

failed:        db 0
before:        dw 0
value:         dd 0
lowest:        dd 0
highest:       dd 0
number:        times 10 db 0
number_end:    db '$'
hex_digits:    db '0123456789ABCDEF'
msg_spk:       db 'SPK $'
msg_space:     db ' $'
msg_ok:        db ' ok$'
msg_fail:      db ' FAIL$'
msg_newline:   db 13, 10, '$'
unit_hex:      db 'h$'
unit_of_rises: db '/64$'
unit_hz:       db 'Hz$'
unit_none:     db '$'
name_post:     db 'post$'
name_rise:     db 'rise$'
name_carrier:  db 'carrier$'
name_done:     db 'DONE$'
