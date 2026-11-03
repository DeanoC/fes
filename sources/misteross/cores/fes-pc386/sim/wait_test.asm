; SPDX-License-Identifier: MIT
; WAITTEST.COM: the pc386 BIOS's INT 15h AH=86h wait (#1150;
; docs/pc386-machine-design.md, "I/O ports"). scripts/sim_fes_pc386.py
; --suite wait runs it at the FreeDOS prompt. The BIOS waits CX:DX
; microseconds by counting port 61h bit 4's changes as 15 us each; bit 4
; toggles on each rising edge of PIT counter 1's output, which POST programs
; to 15.09 us. Each check makes one wait and prints
;   WAIT <name> <measured><unit> ok
; (or FAIL) to the screen and port E9h, and the run ends with "WAIT DONE ok"
; or "WAIT DONE FAIL". A BIOS that never programs counter 1 never returns
; from the first wait that needs a change of bit 4.
;
; The BIOS waits whole 15 us steps (CX:DX / 15, rounded down), and the first
; step ends at the first change of bit 4, up to 15.09 us after the call. The
; bounds allow that and the call's own time. Waits under 50 ms are measured
; on PIT counter 0 (mode 2, one count per 1/1,193,182 s), in microseconds:
;   zero    CX:DX = 0 returns at once.
;   15us    one step.
;   100us   six steps.
;   1ms     66 steps.
;   10ms    666 steps.
;   50ms    3,333 steps; the timer interrupt runs during it.
; The long wait is measured in BIOS timer ticks (40:6Ch, 18.2 a second) from
; the start of a tick:
;   300ms   CX:DX = 4:93E0h, 20,000 steps: 5.49 ticks, so 5 whole ticks pass
;           (6 allowed).
;
; Assemble: nasm -f bin -o WAITTEST.COM wait_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9

start:  mov bx, waits
.next:  cmp bx, waits_end
        jae .long
        push bx
        mov cx, [bx + 2]
        mov dx, [bx + 4]
        call measure                    ; EAX = microseconds
        pop bx
        mov si, [bx]
        movzx ecx, word [bx + 6]
        movzx edx, word [bx + 8]
        mov di, unit_us
        call check
        add bx, 10
        jmp .next

.long:  push ds
        push 0x40
        pop ds
        mov ax, [0x6c]
.edge:  cmp ax, [0x6c]                  ; the start of a tick
        je .edge
        mov bx, [0x6c]
        pop ds
        mov cx, 0x0004
        mov dx, 0x93e0
        mov ah, 0x86
        int 0x15
        push ds
        push 0x40
        pop ds
        mov ax, [0x6c]
        pop ds
        sub ax, bx
        movzx eax, ax
        mov si, name_300ms
        mov ecx, 5
        mov edx, 6
        mov di, unit_ticks
        call check

        mov si, name_done
        xor eax, eax
        movzx ecx, byte [failed]
        xor edx, edx
        mov di, unit_none
        call check
        mov ax, 0x4c00
        or al, [failed]
        int 0x21

; INT 15h AH=86h for CX:DX microseconds; EAX = the microseconds it took, by
; PIT counter 0.
measure:
        call pit_count
        mov [before], ax
        mov ah, 0x86
        int 0x15
        call pit_count
        mov dx, [before]
        sub dx, ax                      ; counts down, wrapping at 65536
        movzx eax, dx
        imul eax, eax, 10000
        xor edx, edx
        mov ecx, 11932
        div ecx
        ret

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

; Prints "WAIT <name SI> <EAX><unit DI> ok" if ECX <= EAX <= EDX, else FAIL.
; The DONE line passes EAX = 0 and ECX = the failure flag, so it fails after
; any failed check; unit_none prints no value. Keeps every register.
check:  pushad
        mov [value], eax
        mov [lowest], ecx
        mov [highest], edx
        mov dx, msg_wait
        call say
        mov dx, si
        call say
        cmp di, unit_none
        je .judge
        mov dx, msg_space
        call say
        call say_number
        mov dx, di
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

; Name, CX, DX, lowest and highest microseconds.
waits:  dw name_zero, 0, 0, 0, 100
        dw name_15us, 0, 15, 0, 120
        dw name_100us, 0, 100, 75, 190
        dw name_1ms, 0, 1000, 975, 1100
        dw name_10ms, 0, 10000, 10025, 10150
        dw name_50ms, 0, 50000, 50260, 50500
waits_end:

failed:        db 0
before:        dw 0
value:         dd 0
lowest:        dd 0
highest:       dd 0
number:        times 10 db 0
number_end:    db '$'
msg_wait:      db 'WAIT $'
msg_space:     db ' $'
msg_ok:        db ' ok$'
msg_fail:      db ' FAIL$'
msg_newline:   db 13, 10, '$'
unit_us:       db 'us$'
unit_ticks:    db ' ticks$'
unit_none:     db '$'
name_zero:     db 'zero$'
name_15us:     db '15us$'
name_100us:    db '100us$'
name_1ms:      db '1ms$'
name_10ms:     db '10ms$'
name_50ms:     db '50ms$'
name_300ms:    db '300ms$'
name_done:     db 'DONE$'
