; SPDX-License-Identifier: MIT
; A20TEST.COM: the pc386 BIOS's A20 handling (#1386; docs/pc386-machine-design.md,
; "A20 and CPU reset"). scripts/sim_fes_pc386.py --suite a20 runs it at the
; FreeDOS prompt. Every check prints "A20 <name> ok" or "A20 <name> FAIL" and
; writes the same line to port E9h, and the run ends with "A20 DONE ok" or
; "A20 DONE FAIL". The state of A20 is always measured, by whether 100500h is
; a different byte from 500h; the first measurement is made before anything
; else runs. At the end CMOS 7Dh holds the mode ('B', 'F' or 'X'), 7Eh 0 if
; every check passed (else 1) and 7Fh A5h, for a kit run's saved record.
;
; With no XMS driver, as on the boot image (mode B):
;   boot      A20 was off when the program started: POST gated it off and
;             nothing since turned it on.
;   query     INT 15h AX=2402h reports it off.
;   query-2403  AX=2403h reports both the 8042 and port 92h (BX=3).
;   bios      AX=2401h turns A20 on and AX=2402h reports it; AX=2400h turns it
;             off again.
;   kbc       The 8042's output port turned on as HIMEM's AT method does (D1h,
;             DFh): AX=2402h reports it, and AX=2400h still turns it off.
;   port92    Port 92h bit 1 set as HIMEM's PS/2 method does: likewise.
;   move      INT 15h AH=87h copies to 110000h and back with A20 off, and
;             leaves it off.
;   move-kbc  AH=87h with A20 on through the 8042 leaves it on, and the
;             8042 alone turns it off afterwards, so the move left port 92h
;             as it was.
;   resume    A CPU reset through the 8042 (FEh) with CMOS 0Fh = 0Ah resumes
;             through 40:67h with A20 still on through the 8042, and with
;             CMOS 0Fh back at 0. machine_tb's oracle fetches the reset vector
;             at FFFFFFF0h only with A20 on, so the reset is made with A20 on.
;
; With an XMS driver and DOS in the HMA, as on the FreeDOS games image (mode
; F), the BIOS calls are left alone, since FreeDOS keeps A20 on while it runs
; there:
;   xms-query  XMS 07h agrees with memory.
;   xms-local  XMS 05h turns A20 on and 07h reports it. 06h succeeds, or
;              returns BL=94h (A20 still enabled) with A20 still on: with
;              DOS=HIGH the FreeDOS kernel holds a local enable of its own.
;              Either way 07h then agrees with memory.
; Whether HIMEMX chose a real A20 method is in its /VERBOSE banner, which the
; suite checks.
;
; "A20TEST X" (mode X) is for MS-DOS with HIMEM.SYS and DOS=HIGH. MS-DOS turns
; A20 off for each program it starts. Its first entry in the HMA, even the INT
; 2Fh call that finds HIMEM, takes an XMS local enable that it holds until the
; next program starts. So the checks run between XMS 06h calls that release
; DOS's enables and 05h calls that give them back, and are printed afterwards:
;   dos-start    A20 was off when the program started.
;   dos-locks-N  DOS held N local enables; reported, always ok.
;   dos-release  With them released, A20 is off, as memory and 07h see it.
;   xms-global   XMS 03h turns A20 on and 04h turns it off, each as memory and
;                07h see it.
;   xms-lock     XMS 05h and 06h likewise.
; and then the BIOS checks query to port92 above, behind HIMEM, which each
; leave A20 off as they found it.
;
; Assemble: nasm -f bin -o A20TEST.COM a20_test.asm.

        bits 16
        cpu 386
        org 0x100

DEBUG_PORT equ 0xe9

start:  call wrap
        mov [at_start], al
        mov si, 0x81                    ; the command tail: X selects mode X
.tail:  lodsb
        cmp al, 13
        je .tail_done
        or al, 0x20
        cmp al, 'x'
        jne .tail
        mov byte [mode], 'X'
.tail_done:
        mov ax, 0x4300
        int 0x2f
        cmp al, 0x80
        je xms_tests
        mov byte [mode], 'B'

        ; ---- Mode B: no XMS driver ----------------------------------------
        mov si, name_boot
        mov al, [at_start]
        call check_zero
        call bios_checks

        mov si, name_move
        call block_move
        call check_zero
        call state_is_off

        mov si, name_move_kbc
        mov al, 0xdf
        call kbc_output
        call block_move
        call check_zero
        call state_is_on
        mov al, 0xdd
        call kbc_output
        call wrap
        call check_zero

        mov si, name_resume
        mov al, 0xdf
        call kbc_output
        call reset_and_resume
        call wrap
        mov ah, 1
        call check
        mov al, 0x0f
        call cmos
        call check_zero
        mov ax, 0x2400
        call bios_a20
        call state_is_off
        jmp done

        ; ---- Modes F and X: an XMS driver ---------------------------------
xms_tests:
        mov ax, 0x4310
        int 0x2f
        mov [xms], bx
        mov [xms + 2], es
        push cs
        pop es
        cmp byte [mode], 'X'
        je dos_tests

        mov si, name_xms_query
        call xms_agrees

        mov si, name_xms_local
        mov ah, 0x05
        call far [xms]
        mov ah, 1
        call check
        call xms_agrees
        call wrap
        mov ah, 1
        call check
        mov ah, 0x06
        call far [xms]
        cmp ax, 1
        je .local_off
        cmp bl, 0x94
        jne .local_off                  ; AL = 0: fails below
        call wrap                       ; still enabled: A20 must be on
.local_off:
        mov ah, 1
        call check
        call xms_agrees
        jmp done

dos_tests:
        mov byte [defer], 1
        mov si, name_dos_start
        mov al, [at_start]
        call check_zero

        mov cx, 4                       ; release DOS's local enables
.release:
        mov ah, 0x07
        call far [xms]
        test ax, ax
        jz .released
        mov ah, 0x06
        call far [xms]
        cmp ax, 1
        jne .released
        inc byte [dos_locks]
        loop .release
.released:
        mov al, [dos_locks]
        add [name_dos_locks + 10], al
        mov si, name_dos_locks
        mov ah, al
        call check
        mov si, name_dos_release
        call wrap
        mov [measured], al
        mov ah, 0x07
        call far [xms]
        mov [reported], al
        mov byte [expect], 0
        call results

        mov si, name_xms_global
        mov ah, 0x03
        call xms_step_on
        mov ah, 0x04
        call xms_step_off

        mov si, name_xms_lock
        mov ah, 0x05
        call xms_step_on
        mov ah, 0x06
        call xms_step_off

        call bios_checks
        movzx cx, byte [dos_locks]       ; DOS's enables back
        jcxz .restored
.restore:
        mov ah, 0x05
        call far [xms]
        loop .restore
.restored:
        call flush

done:   mov si, name_done
        mov al, [failed]
        call check_zero
        mov al, 0x7d
        mov ah, [mode]
        call cmos_write
        mov al, 0x7e
        mov ah, [failed]
        call cmos_write
        mov al, 0x7f
        mov ah, 0xa5
        call cmos_write
        mov ax, 0x4c00
        or al, [failed]
        int 0x21

; The BIOS's A20 calls from A20 off, each leaving it off.
bios_checks:
        mov si, name_query
        mov ax, 0x2402
        call bios_a20
        call check_zero
        mov si, name_support
        mov ax, 0x2403
        int 0x15
        mov al, 1
        jc .support
        cmp bx, 3
        jne .support
        mov al, 0
.support:
        call check_zero

        mov si, name_bios
        mov ax, 0x2401
        call bios_a20
        call state_is_on
        mov ax, 0x2400
        call bios_a20
        call state_is_off

        mov si, name_kbc
        mov al, 0xdf
        call kbc_output
        call state_is_on
        mov ax, 0x2400
        call bios_a20
        call state_is_off

        mov si, name_port92
        in al, 0x92
        or al, 0x02
        out 0x92, al
        call state_is_on
        mov ax, 0x2400
        call bios_a20
        jmp state_is_off

; ---- Checks ---------------------------------------------------------------

; Both the memory and AX=2402h say A20 is on (off): checks named by SI. Both
; are read before either is printed, since under MS-DOS printing can turn A20
; on.
state_is_on:
        mov byte [expect], 1
        jmp state
state_is_off:
        mov byte [expect], 0
state:  call wrap
        mov [measured], al
        mov ax, 0x2402
        call bios_a20
        mov [reported], al
        jmp results

; XMS function AH must succeed and leave A20 on (off), as memory and 07h see
; it: checks named by SI, all read before any is printed.
xms_step_on:
        mov byte [expect], 1
        jmp xms_step
xms_step_off:
        mov byte [expect], 0
xms_step:
        call far [xms]
        cmp ax, 1
        mov ah, 1
        je .done
        mov ah, 0
.done:  mov [returned], ah
        call wrap
        mov [measured], al
        mov ah, 0x07
        call far [xms]
        mov [reported], al
        mov al, [returned]
        mov ah, 1
        call check
results:
        mov al, [measured]
        mov ah, [expect]
        call check
        mov al, [reported]
        mov ah, [expect]
        jmp check

; AL is the value 0 (a clean step's result) for the check named by SI.
check_zero:
        mov ah, 0
; AL is AH for the check named by SI; print its line, and remember a failure.
; While defer is set, the check is kept for flush to print instead.
check:  cmp al, ah
        je .deferred
        mov byte [failed], 1
.deferred:
        cmp byte [defer], 0
        je .now
        push di
        mov di, [kept_end]
        mov [di], si
        mov [di + 2], ax
        add word [kept_end], 4
        pop di
        ret
.now:   push dx
        mov dx, msg_ok
        cmp al, ah
        je .print
        mov dx, msg_fail
.print: push dx
        mov dx, msg_a20
        call say
        mov dx, si
        call say
        pop dx
        call say
        mov dx, msg_newline
        call say
        mov al, 10
        out DEBUG_PORT, al
        pop dx
        ret

; Print the checks kept while defer was set.
flush:  mov byte [defer], 0
        mov bx, kept
.next:  cmp bx, [kept_end]
        jae .done
        mov si, [bx]
        mov ax, [bx + 2]
        push bx
        call check
        pop bx
        add bx, 4
        jmp .next
.done:  ret

; XMS 07h's AX against the memory, for the check named by SI.
xms_agrees:
        mov ah, 0x07
        call far [xms]
        push ax
        call wrap
        pop bx
        mov ah, bl
        jmp check

; ---- A20 ------------------------------------------------------------------

; AL = 1 if A20 is on: 100500h (FFFF:0510h) is a different byte from 500h.
; Interrupts are off while a byte is changed, and it is put back.
wrap:   push ds
        push es
        push bx
        pushf
        cli
        xor ax, ax
        mov ds, ax
        dec ax
        mov es, ax
        mov al, 1
        mov bl, [0x500]
        mov bh, [es:0x510]
        cmp bl, bh
        jne .done
        not bh
        mov [es:0x510], bh
        cmp [0x500], bl
        not bh
        mov [es:0x510], bh
        je .done
        mov al, 0
.done:  popf
        pop bx
        pop es
        pop ds
        ret

; INT 15h AX (24xxh); AL = its AL, or FFh if it set CF or AH.
bios_a20:
        int 0x15
        jc .error
        test ah, ah
        jz .ok
.error: mov al, 0xff
.ok:    ret

; Write the 8042's output port: D1h, then AL.
kbc_output:
        push ax
        call kbc_wait
        mov al, 0xd1
        out 0x64, al
        call kbc_wait
        pop ax
        out 0x60, al
kbc_wait:
        push ax
.wait:  in al, 0x64
        test al, 0x02
        jnz .wait
        pop ax
        ret

; AL = CMOS index; returns its value in AL.
cmos:   cli
        out 0x70, al
        in al, 0x71
        sti
        ret

; AL = CMOS index, AH = the value to write.
cmos_write:
        cli
        out 0x70, al
        mov al, ah
        out 0x71, al
        sti
        ret

; INT 15h AH=87h: 16 bytes to 110000h and back into a second buffer; AL = 0
; if both moves succeeded and the bytes came back.
block_move:
        mov [check_name], si
        push es
        mov ax, ds
        mov dx, 16
        mul dx
        add ax, pattern
        adc dl, 0
        mov [gdt_source + 2], ax        ; source: pattern
        mov [gdt_source + 4], dl
        mov word [gdt_target + 2], 0x0000
        mov byte [gdt_target + 4], 0x11  ; target: 110000h
        push cs
        pop es
        mov si, gdt
        mov cx, 8
        mov ah, 0x87
        int 0x15
        jc .fail
        mov ax, ds
        mov dx, 16
        mul dx
        add ax, copy
        adc dl, 0
        mov word [gdt_source + 2], 0x0000
        mov byte [gdt_source + 4], 0x11  ; source: 110000h
        mov [gdt_target + 2], ax        ; target: copy
        mov [gdt_target + 4], dl
        mov si, gdt
        mov cx, 8
        mov ah, 0x87
        int 0x15
        jc .fail
        mov si, pattern
        mov di, copy
        mov cx, 16
        repe cmpsb
        jne .fail
        mov al, 0
        jmp .done
.fail:  mov al, 1
.done:  pop es
        mov si, [check_name]
        ret

; Reset the CPU through the 8042 with CMOS 0Fh = 0Ah, so POST resumes at
; resumed through 40:67h without an EOI.
reset_and_resume:
        mov [check_name], si
        pushf
        cli
        mov [saved_sp], sp
        mov [saved_ss], ss
        mov al, 0x0f
        out 0x70, al
        mov al, 0x0a
        out 0x71, al
        push ds
        xor ax, ax
        mov ds, ax
        mov word [0x467], resumed
        mov [0x469], cs
        pop ds
        call kbc_wait
        mov al, 0xfe
        out 0x64, al
.hang:  hlt
        jmp .hang
resumed:
        mov ax, cs
        mov ds, ax
        mov es, ax
        mov ss, [saved_ss]
        mov sp, [saved_sp]
        popf
        mov si, [check_name]
        ret

; ---- Output ---------------------------------------------------------------

; Print the $-terminated string at DX on the screen and to port E9h.
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

mode:          db 'F'
at_start:      db 0
failed:        db 0
expect:        db 0
measured:      db 0
reported:      db 0
returned:      db 0
defer:         db 0
dos_locks:     db 0
kept_end:      dw kept
xms:           dd 0
saved_sp:      dw 0
saved_ss:      dw 0
check_name:    dw 0

msg_a20:       db 'A20 $'
msg_ok:        db ' ok$'
msg_fail:      db ' FAIL$'
msg_newline:   db 13, 10, '$'
name_boot:     db 'boot$'
name_query:    db 'query$'
name_support:  db 'query-2403$'
name_bios:     db 'bios$'
name_kbc:      db 'kbc$'
name_port92:   db 'port92$'
name_move:     db 'move$'
name_move_kbc: db 'move-kbc$'
name_resume:   db 'resume$'
name_xms_query: db 'xms-query$'
name_xms_local: db 'xms-local$'
name_dos_start: db 'dos-start$'
name_dos_locks: db 'dos-locks-0$'
name_dos_release: db 'dos-release$'
name_xms_global: db 'xms-global$'
name_xms_lock: db 'xms-lock$'
name_done:     db 'DONE$'

pattern:       db 'pc386 A20 #1386!'
copy:          times 16 db 0

; The 87h descriptor table: dummy, GDT, source, target, BIOS CS, SS.
gdt:           times 16 db 0
gdt_source:    dw 0xffff, 0
               db 0, 0x93
               dw 0
gdt_target:    dw 0xffff, 0
               db 0, 0x93
               dw 0
               times 16 db 0

kept:          times 64 * 4 db 0                ; deferred checks: name, AL, AH
