; SPDX-License-Identifier: MIT
; FLOPTEST.COM: the pc386 machine's floppy drives, their CMOS configuration
; and the BIOS's change line (slice F2 of
; docs/design/pc386-removable-media.md, fes#1694).
; scripts/sim_fes_pc386.py --suite floppy runs it at the FreeDOS prompt with
; a 1.44M disk labelled DISK1 in A: and a 1.2M disk labelled BDISK in B:.
;
; Each check prints
;   FLOPPY <name> ok
; (or FAIL) to the screen and port E9h, and the run ends with
; "FLOPPY DONE ok" or "FLOPPY DONE FAIL".
;
; The program asks the bench for its own disk changes: it writes the line
; "SWAP 1" to port E9h, which the bench answers by swapping DISK2 into A:
; write-protected, and later "SWAP 2", which puts DISK1 back writable. So the
; checks never wait on a wall-clock guess.
;
;   equip      INT 11h: a floppy is fitted (bit 0) and there are two drives
;              (bits 7-6 = 01), from CMOS 14h.
;   type-a     INT 13h AH=15h for A: returns AH=02h, a drive with a change
;              line, as an AT BIOS reports a 1.44M drive.
;   type-b     the same for B:, a 1.2M drive.
;   label-a    INT 13h AH=02h reads A:'s boot sector: volume label DISK1.
;   label-b    B:'s boot sector: volume label BDISK. Its 15 sectors a track
;              are the 1.2M geometry, so the drives are told apart.
;   nochange-a INT 13h AH=16h after that read: no change, AH=00h and CF clear.
;   change-a   after SWAP 1, AH=16h reports the change: CF set and AH=06h.
;   reread-a   the next read gets the new disk: volume label DISK2.
;   cleared-a  that read stepped the head, so AH=16h is clear again.
;   wp-a       DISK2 went in write-protected: AH=03h fails with AH=03h.
;   write-b    a sector written to B:'s last track reads back; the bench
;              writes B:'s image out and the suite finds it in the file.
;   dos-a1     DOS reads the new disk: FindFirst A:\*.TXT is DISK2.TXT.
;   dos-a2     after SWAP 2, DOS sees DISK1.TXT again, which it can only do
;              by taking the change line through INT 13h AH=16h.
;
; Assemble: nasm -f bin -o FLOPTEST.COM floppy_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9
TICK       equ 0x6c                     ; 40:6Ch, the BIOS tick count
WAIT_TICKS equ 40                       ; about 2.2 s for a swap or a reread

start:  cld
        push cs
        pop es

        ; ---- equip: the CMOS equipment word
        int 0x11
        mov bx, ax
        and bx, 0xc1
        cmp bx, 0x41                    ; a floppy, two drives
        call judge_zero
        mov si, name_equip
        call check

        ; ---- type-a, type-b: INT 13h AH=15h
        xor dl, dl
        call drive_type
        cmp ah, 0x02
        call judge_zero
        mov si, name_type_a
        call check
        mov dl, 1
        call drive_type
        cmp ah, 0x02
        call judge_zero
        mov si, name_type_b
        call check

        ; ---- label-a, label-b: the boot sector of each drive
        xor dl, dl
        call read_boot
        mov di, label_disk1
        call judge_label
        mov si, name_label_a
        call check
        mov dl, 1
        call read_boot
        mov di, label_bdisk
        call judge_label
        mov si, name_label_b
        call check

        ; ---- nochange-a: the change line is clear after a read
        xor dl, dl
        mov ah, 0x16
        int 0x13
        mov al, 0
        jc .no_change_done
        cmp ah, 0x00
        jne .no_change_done
        mov al, 1
.no_change_done:
        mov si, name_nochange_a
        call check

        ; ---- change-a: the bench swaps DISK2 in, write-protected
        mov si, mark_swap1
        call mark
        call wait_change                ; AL = 1 when AH=16h reported a change
        mov si, name_change_a
        call check

        ; ---- reread-a: the next read is the new disk
        xor dl, dl
        call read_boot
        mov di, label_disk2
        call judge_label
        mov si, name_reread_a
        call check

        ; ---- cleared-a: that read stepped, so the line is clear
        xor dl, dl
        mov ah, 0x16
        int 0x13
        mov al, 0
        jc .cleared_done
        cmp ah, 0x00
        jne .cleared_done
        mov al, 1
.cleared_done:
        mov si, name_cleared_a
        call check

        ; ---- wp-a: DISK2 is write-protected
        call fill_pattern
        mov bx, sector
        mov ax, 0x0301                  ; write one sector
        mov cx, 0x0001                  ; cylinder 0, sector 1
        mov dx, 0x0100                  ; head 1, drive 0
        int 0x13
        mov al, 0
        jnc .wp_done
        cmp ah, 0x03                    ; write protected
        jne .wp_done
        mov al, 1
.wp_done:
        mov si, name_wp_a
        call check

        ; ---- write-b: the last sector of B: is written and read back
        call fill_pattern
        mov bx, sector
        mov ax, 0x0301
        mov cx, 0x4f0f                  ; cylinder 79, sector 15
        mov dx, 0x0101                  ; head 1, drive 1
        int 0x13
        mov al, 0
        jc .write_b_done
        mov di, sector
        mov cx, 512
        xor al, al
        rep stosb                       ; clear the buffer before the read-back
        mov bx, sector
        mov ax, 0x0201
        mov cx, 0x4f0f
        mov dx, 0x0101
        int 0x13
        mov al, 0
        jc .write_b_done
        call judge_pattern              ; AL = 1 when the bytes came back
.write_b_done:
        mov si, name_write_b
        call check

        ; ---- dos-a1: DOS reads the disk that is in A: now
        mov di, label_disk2
        call find_txt
        mov si, name_dos_a1
        call check

        ; ---- dos-a2: after SWAP 2, DOS sees DISK1.TXT through the change line
        mov si, mark_swap2
        call mark
        mov di, label_disk1
        call wait_txt
        mov si, name_dos_a2
        call check

        ; ---- DONE
        mov al, [failed]
        xor al, 1
        mov si, name_done
        call check
        mov ax, 0x4c00
        int 0x21

; ---- Helpers ---------------------------------------------------------------

; AH = INT 13h AH=15h's answer for drive DL. Keeps DL.
drive_type:
        push dx
        mov ah, 0x15
        int 0x13
        pop dx
        ret

; Read drive DL's boot sector into `sector`, with the usual three tries and a
; controller reset between them. AL = 1 on success.
read_boot:
        push dx
        mov byte [tries], 3
.try:   push dx
        mov bx, sector
        mov ax, 0x0201                  ; read one sector
        mov cx, 0x0001                  ; cylinder 0, sector 1
        xor dh, dh                      ; head 0
        int 0x13
        pop dx
        jnc .ok
        dec byte [tries]
        jz .fail
        push dx
        xor ah, ah                      ; reset the controller
        int 0x13
        pop dx
        jmp .try
.ok:    pop dx
        mov al, 1
        ret
.fail:  pop dx
        xor al, al
        ret

; AL = 1 when the volume label in the sector just read is the 5 characters at
; DI (offset 0x2Bh of the boot sector).
judge_label:
        push si
        push di
        push cx
        mov si, sector + 0x2b
        mov cx, 5
        repe cmpsb
        mov al, 0
        jne .done
        mov al, 1
.done:  pop cx
        pop di
        pop si
        ret

; AL = 1 when the zero flag is set (for a cmp before the call).
judge_zero:
        mov al, 0
        jnz .done
        mov al, 1
.done:  ret

; Fill `sector` with the 16-byte pattern, 32 times.
fill_pattern:
        pusha
        mov di, sector
        mov cx, 32
.next:  mov si, pattern
        push cx
        mov cx, 16
        rep movsb
        pop cx
        loop .next
        popa
        ret

; AL = 1 when `sector` holds the pattern.
judge_pattern:
        pusha
        mov si, sector
        mov bx, 32
.next:  mov di, pattern
        mov cx, 16
        repe cmpsb
        jne .bad
        dec bx
        jnz .next
        popa
        mov al, 1
        ret
.bad:   popa
        xor al, al
        ret

; Poll INT 13h AH=16h on A: until it reports a change, for WAIT_TICKS BIOS
; ticks. AL = 1 when it did.
wait_change:
        call tick_deadline
.poll:  xor dl, dl
        mov ah, 0x16
        int 0x13
        jnc .again
        cmp ah, 0x06
        jne .again
        mov al, 1
        ret
.again: call tick_expired
        jc .poll
        xor al, al
        ret

; AL = 1 when the first A:\*.TXT DOS finds starts with the 5 characters at DI.
find_txt:
        push di
        mov ah, 0x1a                    ; set the disk transfer address
        mov dx, dta
        int 0x21
        mov ah, 0x4e                    ; find first
        xor cx, cx
        mov dx, txt_pattern
        int 0x21
        pop di
        jc .fail
        push si
        push cx
        mov si, dta + 0x1e              ; the ASCIIZ name
        mov cx, 5
        repe cmpsb
        mov al, 0
        jne .done
        mov al, 1
.done:  pop cx
        pop si
        ret
.fail:  xor al, al
        ret

; Poll find_txt for the name at DI until it matches or WAIT_TICKS pass.
wait_txt:
        call tick_deadline
.poll:  push di
        call find_txt
        pop di
        cmp al, 1
        je .done
        call tick_expired
        jc .poll
        xor al, al
.done:  ret

; Remember the tick the wait started at.
tick_deadline:
        push ax
        push ds
        push 0x40
        pop ds
        mov ax, [TICK]
        pop ds
        mov [started], ax
        pop ax
        ret

; CF set while the wait still has time (elapsed ticks below WAIT_TICKS).
tick_expired:
        push ax
        push ds
        push 0x40
        pop ds
        mov ax, [TICK]
        pop ds
        sub ax, [started]
        cmp ax, WAIT_TICKS
        pop ax
        ret

; Print the line at SI, without CR or LF, to port E9h: a marker the bench acts
; on.
mark:   push ax
        push si
.byte:  lodsb
        cmp al, '$'
        je .end
        out DEBUG_PORT, al
        jmp .byte
.end:   mov al, 10
        out DEBUG_PORT, al
        pop si
        pop ax
        ret

; Print "FLOPPY <name SI> ok" when AL is 1, else FAIL (and remember it).
check:  pushad
        mov dx, msg_floppy
        call say
        mov dx, si
        call say
        test al, al
        mov dx, msg_ok
        jnz .print
        mov byte [failed], 1
        mov dx, msg_fail
.print: call say
        mov dx, msg_newline
        call say
        mov al, 10
        out DEBUG_PORT, al
        popad
        ret

; Print the $-terminated string at DX with INT 21h AH=09h and write it,
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

failed:         db 0
tries:          db 0
started:        dw 0
pattern:        db 'FES FLOPPY WRITE'
txt_pattern:    db 'A:\*.TXT', 0
label_disk1:    db 'DISK1'
label_disk2:    db 'DISK2'
label_bdisk:    db 'BDISK'
mark_swap1:     db 'SWAP 1$'
mark_swap2:     db 'SWAP 2$'
msg_floppy:     db 'FLOPPY $'
msg_ok:         db ' ok$'
msg_fail:       db ' FAIL$'
msg_newline:    db 13, 10, '$'
name_equip:     db 'equip$'
name_type_a:    db 'type-a$'
name_type_b:    db 'type-b$'
name_label_a:   db 'label-a$'
name_label_b:   db 'label-b$'
name_nochange_a: db 'nochange-a$'
name_change_a:  db 'change-a$'
name_reread_a:  db 'reread-a$'
name_cleared_a: db 'cleared-a$'
name_wp_a:      db 'wp-a$'
name_write_b:   db 'write-b$'
name_dos_a1:    db 'dos-a1$'
name_dos_a2:    db 'dos-a2$'
name_done:      db 'DONE$'

                align 4
sector:         times 512 db 0
dta:            times 128 db 0
