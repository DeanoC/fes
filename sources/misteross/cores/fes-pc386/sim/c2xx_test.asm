; SPDX-License-Identifier: MIT
; C2TEST.COM: the pc386 BIOS's INT 15h C2xx mouse services with keyboard
; bytes at the 8042 (#1330; docs/pc386-machine-design.md, "PS/2 aux mouse").
; scripts/sim_fes_pc386.py --suite c2xx runs it at the FreeDOS prompt and
; types the keys in typed_keys (below) after the command, each held 8 ms and
; released for 8 ms. Every check prints "C2 <name> ... ok" or "... FAIL" and
; writes the same line to port E9h, and the run ends with "C2 DONE ok" or
; "C2 DONE FAIL". The services run with interrupts off; a BIOS that waits for
; the mouse behind a keyboard byte, or reads one as the 8042's command byte,
; never returns, and the run ends at the bench's time limit.
;
;   stale-key   A key byte in the output buffer when C205h (initialise, 3-byte
;               packets) starts: 8042 command D2h puts the set 1 make code of K
;               there with interrupts off. The reset returns, and INT 09h took
;               the byte: INT 16h AH=00h gives K (AX=256Bh).
;   stale-aux   An aux byte there (8042 command D3h, 08h): the reset returns.
;   masked-key  The K make code again with IRQ1 masked at the PIC, so no
;               handler can take it: the reset still returns, and the byte is
;               gone, so INT 16h has no key.
;   typing      C205h again and again, 3 ms apart, while the bench types: the
;               keys come through INT 16h in order up to q. It prints the
;               number of resets and the keys.
;
; Assemble: nasm -f bin -o C2TEST.COM c2xx_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9
K_MAKE     equ 0x25                     ; set 1, as the 8042 translates it
GAP_COUNTS equ 3 * 1193                 ; 3 ms of PIT counter 0
TYPING_TICKS equ 55                     ; give up after about 3 s

start:  cld
        call flush_keys

        ; ---- stale-key ----------------------------------------------------
        mov si, name_stale_key
        mov ax, 0xd2 << 8 | K_MAKE
        call stale_reset
        jc .stale_key_fail
        mov ah, 0x01
        int 0x16
        jz .stale_key_fail
        xor ah, ah
        int 0x16
        cmp ax, 0x256b
        jne .stale_key_fail
        call pass
        jmp .stale_aux
.stale_key_fail:
        call fail

        ; ---- stale-aux ----------------------------------------------------
.stale_aux:
        call flush_keys
        mov si, name_stale_aux
        mov ax, 0xd3 << 8 | 0x08
        call stale_reset
        jc .stale_aux_fail
        call pass
        jmp .masked
.stale_aux_fail:
        call fail

        ; ---- masked-key ---------------------------------------------------
.masked:
        call flush_keys
        mov si, name_masked_key
        in al, 0x21
        mov [pic_mask], al
        or al, 0x02                     ; IRQ1
        out 0x21, al
        mov ax, 0xd2 << 8 | K_MAKE
        call stale_reset
        pushf
        mov al, [pic_mask]
        out 0x21, al
        popf
        jc .masked_fail
        mov ah, 0x01
        int 0x16
        jnz .masked_fail
        call pass
        jmp typing
.masked_fail:
        call fail

        ; ---- typing -------------------------------------------------------
typing: mov di, received
        push ds
        push 0x40
        pop ds
        mov ax, [0x6c]
        pop ds
        mov [typing_start], ax
.reset: mov ax, 0xc205
        mov bh, 3
        int 0x15
        jc .typing_fail
        inc word [resets]
        call pit_count
        mov [gap_start], ax
.gap:   mov ah, 0x01
        int 0x16
        jz .no_key
        xor ah, ah
        int 0x16
        cmp di, received_end
        jae .typing_fail
        mov [di], al
        inc di
        cmp al, 'q'
        je .typed
.no_key:
        call pit_count
        mov dx, [gap_start]
        sub dx, ax
        cmp dx, GAP_COUNTS
        jb .gap
        push ds
        push 0x40
        pop ds
        mov ax, [0x6c]
        pop ds
        sub ax, [typing_start]
        cmp ax, TYPING_TICKS
        jb .reset
        jmp .typing_fail
.typed: mov byte [di], '$'
        mov si, received
        mov di, typed_keys
        mov cx, typed_keys_end - typed_keys
        repe cmpsb
        jne .typing_fail
        cmp byte [si], '$'
        jne .typing_fail
        call typing_line
        call pass
        jmp done
.typing_fail:
        mov byte [di], '$'
        call typing_line
        call fail

done:   mov si, name_done
        cmp byte [failed], 0
        jne .failed
        call pass
        jmp .exit
.failed:
        call fail
.exit:  mov ax, 0x4c00
        or al, [failed]
        int 0x21

; The typing check's name, then " <resets> resets <keys>".
typing_line:
        mov si, name_typing
        call say_name
        mov dx, msg_space
        call say
        mov ax, [resets]
        call say_number
        mov dx, msg_resets
        call say
        mov dx, received
        call say
        mov byte [named], 1
        ret

; AH = 8042 command D2h or D3h, AL = the byte it puts in the output buffer.
; With interrupts off, the byte is put there, then INT 15h C205h runs; CF as
; it returns.
stale_reset:
        cli
        push ax
.idle:  in al, 0x64
        test al, 0x02
        jnz .idle
        pop ax
        xchg al, ah
        out 0x64, al
        xchg al, ah
        out 0x60, al
.full:  in al, 0x64
        test al, 0x01
        jz .full
        mov ax, 0xc205
        mov bh, 3
        int 0x15
        sti
        ret

; Empties the BIOS keyboard buffer.
flush_keys:
        mov ah, 0x01
        int 0x16
        jz .done
        xor ah, ah
        int 0x16
        jmp flush_keys
.done:  ret

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

; Ends the line of the check named by SI with " ok" or " FAIL".
pass:   mov dx, msg_ok
        jmp verdict
fail:   mov byte [failed], 1
        mov dx, msg_fail
verdict:
        cmp byte [named], 0
        jne .named
        call say_name
.named: call say
        mov dx, msg_newline
        call say
        mov al, 10
        out DEBUG_PORT, al
        mov byte [named], 0
        ret

; "C2 <name SI>".
say_name:
        push dx
        mov dx, msg_c2
        call say
        mov dx, si
        call say
        pop dx
        ret

; Prints AX in decimal.
say_number:
        mov di, number_end
        mov bx, 10
.digit: xor dx, dx
        div bx
        add dl, '0'
        dec di
        mov [di], dl
        test ax, ax
        jnz .digit
        mov dx, di
        jmp say

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
named:         db 0
pic_mask:      db 0
resets:        dw 0
gap_start:     dw 0
typing_start:  dw 0
number:        times 6 db 0
number_end:    db '$'
; The keys the suite types after the command, ending with q.
typed_keys:    db 'abcdefghijklmnopq'
typed_keys_end:
received:      times 64 db 0
received_end:  db '$'
msg_c2:        db 'C2 $'
msg_space:     db ' $'
msg_resets:    db ' resets $'
msg_ok:        db ' ok$'
msg_fail:      db ' FAIL$'
msg_newline:   db 13, 10, '$'
name_stale_key: db 'stale-key$'
name_stale_aux: db 'stale-aux$'
name_masked_key: db 'masked-key$'
name_typing:   db 'typing$'
name_done:     db 'DONE$'
