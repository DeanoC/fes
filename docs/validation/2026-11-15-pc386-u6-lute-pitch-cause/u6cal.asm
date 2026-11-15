; u6cal.asm -- fes#1837 probe: what sets Ultima VI's lute pitch.
;
; Measures, on any PC, the four quantities the game's own code makes the
; note's pitch out of (see the record's method.md):
;
;   C   the calibration count: passes of the game's INT 1Ch-timed loop
;       `inc word [m]; cmp word [tick],0; je` in exactly one BIOS tick.
;       The game stores floor(18C/750) at [2FC6h] and uses
;       N = floor([2FC6h]/24) inner delay iterations per lute pass.
;   BD  1000-iteration blocks of the game's inner delay loop
;       `dec word [m]; jne` completed in 64 ticks.
;   BP  the same for that loop with five NOPs added: the same memory
;       accesses, five more bytes to fetch (the instruction-fetch probe).
;   P0  passes of the game's lute loop with N = 0, in 64 ticks / 1000.
;   PN  passes of the game's lute loop with N as computed above, in
;       64 ticks / 100. The note's pitch is proportional to this.
;
; Both loop bodies are byte-identical to GAME.EXE's (FF 06 / 83 3E / 74,
; and FF 0E / 75) and keep its loop-start address parity: the calibration
; loop starts at an odd linear address, the delay loop at an even one.
;
; No game code and no game data are reproduced here: the two loops are
; three and two generic instructions, and the lute pass is rebuilt from the
; published description of the routine, with its own argument block.
;
; nasm -f bin -o U6CAL.COM u6cal.asm
;
; SPDX-License-Identifier: MIT

        cpu 8086
        bits 16
        org 0x100

start:
        cld
        mov     ax, cs
        mov     ds, ax
        mov     es, ax

        ; hook INT 1Ch
        mov     ax, 0x351c
        int     0x21
        mov     [old_off], bx
        mov     [old_seg], es
        mov     ax, cs
        mov     ds, ax
        mov     dx, tickisr
        mov     ax, 0x251c
        int     0x21

        ; ---- M0: a 256-tick marker, so a video capture gives the tick period
        mov     si, msg_t0
        call    puts
        mov     cx, 256
.t0:    mov     word [tick], 0
.t0w:   cmp     word [tick], 0
        je      .t0w
        loop    .t0
        mov     si, msg_t1
        call    puts

        ; ---- M1: C, five repeats
        mov     si, msg_c
        call    puts
        mov     cx, 5
.crep:  push    cx
        call    calib
        mov     ax, [ctr]
        call    putu
        mov     al, ' '
        call    putc
        pop     cx
        loop    .crep
        call    crlf

        ; N = floor(floor(18*C/750)/24) from the last repeat, the game's way
        mov     ax, [ctr]
        xor     dx, dx
        mov     cx, 18
        mul     cx
        mov     cx, 750
        div     cx
        mov     [calval], ax
        cmp     ax, 100
        jge     .div24
        xor     ax, ax
        jmp     .haven
.div24: mov     cl, 24
        div     cl
        mov     ah, 0
.haven: mov     [nunits], ax
        mov     si, msg_cv
        call    puts
        mov     ax, [calval]
        call    putu
        mov     si, msg_n
        call    puts
        mov     ax, [nunits]
        call    putu
        call    crlf

        ; ---- M2: BD
        mov     si, msg_bd
        call    puts
        call    blk_delay
        mov     ax, [blocks]
        call    putu
        call    crlf

        ; ---- M3: BP
        mov     si, msg_bp
        call    puts
        call    blk_delay_pad
        mov     ax, [blocks]
        call    putu
        call    crlf

        ; ---- M4: P0  (lute pass, N = 0)
        mov     si, msg_p0
        call    puts
        mov     word [arg_n], 0
        mov     word [divisor], 1000
        call    blk_pass
        mov     ax, [blocks]
        call    putu
        call    crlf

        ; ---- M5: PN  (lute pass, N as calibrated)
        mov     si, msg_pn
        call    puts
        mov     ax, [nunits]
        mov     [arg_n], ax
        mov     word [divisor], 100
        call    blk_pass
        mov     ax, [blocks]
        call    putu
        call    crlf

        ; speaker off, restore the vector
        in      al, 0x61
        and     al, 0xfc
        out     0x61, al
        mov     dx, [old_off]
        mov     ax, [old_seg]
        mov     ds, ax
        mov     ax, 0x251c
        int     0x21
        mov     ax, cs
        mov     ds, ax
        mov     si, msg_done
        call    puts
        mov     ax, 0x4c00
        int     0x21

; ---------------------------------------------------------------- M1
; The game's calibration, byte for byte: sync to a tick, then count passes
; of `inc word [ctr]; cmp word [tick],0; je` until the next tick.
calib:
        mov     word [ctr], 0
        mov     word [tick], 0
.sync:  cmp     word [tick], 0
        je      .sync
        mov     word [tick], 0
        align   2
        nop                             ; the loop body starts odd, as the game's does
.loop:  db      0xff, 0x06
        dw      ctr                     ; inc word [ctr]
        db      0x83, 0x3e
        dw      tick
        db      0x00                    ; cmp word [tick], 0
        db      0x74, 0xf5              ; je .loop
        ret

; ---------------------------------------------------------------- M2/M3
; 1000-iteration blocks of the game's inner delay loop, in 64 ticks.
blk_delay:
        call    t_start
.blk:   mov     word [dcnt], 1000
        align   2
.loop:  db      0xff, 0x0e
        dw      dcnt                    ; dec word [dcnt]
        db      0x75, 0xfa              ; jne .loop
        inc     word [blocks]
        cmp     word [tick], 64
        jb      .blk
        ret

blk_delay_pad:
        call    t_start
.blk:   mov     word [dcnt], 1000
        align   2
.loop:  db      0xff, 0x0e
        dw      dcnt                    ; dec word [dcnt]
        nop
        nop
        nop
        nop
        nop                             ; five more bytes to fetch
        db      0x75, 0xf5              ; jne .loop
        inc     word [blocks]
        cmp     word [tick], 64
        jb      .blk
        ret

; ---------------------------------------------------------------- M4/M5
; The game's lute pass: the phase accumulator, the port 61h gate, and an
; N-iteration delay, [divisor] passes to a counted block, in 64 ticks.
blk_pass:
        call    t_start
        mov     bp, args
        xor     dx, dx                  ; the phase accumulator
        mov     bx, 0                   ; the threshold ramp
.blk:   mov     cx, [divisor]
.pass:  mov     ax, [bp + 0x0e]         ; note step
        add     dx, ax
        push    ax
        push    cx
        cmp     dx, bx
        ja      .high
        in      al, 0x61
        and     al, 0xfc
        out     0x61, al
        jmp     .after
.high:  in      al, 0x61
        or      al, 3
        out     0x61, al
.after: add     bx, [bp + 0x06]         ; threshold step
        mov     ax, [bp + 0x0a]         ; N
        or      ax, ax
        je      .nodelay
        mov     cx, [bp + 0x0c]         ; outer multiplier
        mov     [douter], cx
.rel:   mov     [dcnt], ax
.inner: dec     word [dcnt]
        jne     .inner
        dec     word [douter]
        jne     .rel
.nodelay:
        pop     cx
        pop     ax
        loop    .pass
        inc     word [blocks]
        cmp     word [tick], 64
        jb      .blk
        ret

t_start:
        mov     word [blocks], 0
        mov     word [tick], 0
.sync:  cmp     word [tick], 0
        je      .sync
        mov     word [tick], 0
        ret

; ---------------------------------------------------------------- the tick
tickisr:
        push    ds
        push    cs
        pop     ds
        inc     word [tick]
        pop     ds
        iret

; ---------------------------------------------------------------- output
putc:   push    dx
        mov     dl, al
        mov     ah, 2
        int     0x21
        pop     dx
        ret
crlf:   mov     al, 13
        call    putc
        mov     al, 10
        call    putc
        ret
puts:   lodsb
        or      al, al
        jz      .d
        call    putc
        jmp     puts
.d:     ret
putu:   ; print AX in decimal
        push    bx
        push    cx
        push    dx
        mov     bx, 10
        xor     cx, cx
.d1:    xor     dx, dx
        div     bx
        push    dx
        inc     cx
        or      ax, ax
        jnz     .d1
.d2:    pop     ax
        add     al, '0'
        call    putc
        loop    .d2
        pop     dx
        pop     cx
        pop     bx
        ret

msg_t0: db 'u6cal fes1837', 13, 10, 'T0', 13, 10, 0
msg_t1: db 'T1 (256 ticks)', 13, 10, 0
msg_c:  db 'C= ', 0
msg_cv: db 'CAL=', 0
msg_n:  db ' N=', 0
msg_bd: db 'BD=', 0
msg_bp: db 'BP=', 0
msg_p0: db 'P0=', 0
msg_pn: db 'PN=', 0
msg_done: db 'END', 13, 10, 0

        align 2
old_off:  dw 0
old_seg:  dw 0
tick:     dw 0
ctr:      dw 0
calval:   dw 0
nunits:   dw 0
blocks:   dw 0
divisor:  dw 0
dcnt:     dw 0
douter:   dw 0
; the lute case's argument block, as the routine's caller lays it out:
; [bp+6] threshold step, [bp+0A] N, [bp+0C] outer multiplier, [bp+0E] note step
args:     dw 0, 0, 0                    ; bp+0 .. bp+4
arg_thr:  dw 5270                       ; bp+6  = 2 * the note step: a 50% gate duty
          dw 0                          ; bp+8
arg_n:    dw 0                          ; bp+0A
          dw 1                          ; bp+0C the lute's delay multiplier
arg_step: dw 2635                       ; bp+0E
