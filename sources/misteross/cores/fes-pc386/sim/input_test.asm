; SPDX-License-Identifier: MIT
; INPUT.COM: the pc386 keyboard and mouse exercise the system bench runs at
; the FreeDOS prompt (docs/pc386-machine-design.md, "Keyboard model" and
; "PS/2 aux mouse"). Through the BIOS's PS/2 pointing-device services
; (INT 15h C205h, C207h, C200h) it resets the aux mouse and enables it with a
; far handler that sums every packet's X and Y, in PS/2 counts with Y up, and
; counts each button's presses and releases. It writes "MOUSE READY" to port
; E9h, then reads keys with INT 16h AH=10h, keeping up to eight AX values,
; until q. Then it removes the handler and writes
;   MOUSE X=<signed> Y=<signed> LEFT=<presses>/<releases> RIGHT=<p>/<r> PACKETS=<n>
;   KEYS <AX in hex>...
; to port E9h and the screen.
; Assemble: nasm -f bin -o INPUT.COM input_test.asm.

        bits 16
        org 0x100

start:  cld
        mov ax, 0xc205                  ; initialise: 3-byte packets, reset
        mov bh, 3
        int 0x15
        jc .init_failed
        push cs
        pop es
        mov bx, handler
        mov ax, 0xc207
        int 0x15
        jc .init_failed
        mov ax, 0xc200                  ; enable
        mov bh, 1
        int 0x15
        jc .enable_failed
        mov di, line
        mov si, txt_ready
        call put_string
        call emit_line

.key:   mov ah, 0x10
        int 0x16
        cmp al, 'q'
        je .done
        mov bx, [key_count]
        cmp bx, 8
        jae .key
        shl bx, 1
        mov [keys + bx], ax
        inc word [key_count]
        jmp .key

.done:  mov ax, 0xc200                  ; disable, then remove the handler
        xor bh, bh
        int 0x15
        xor bx, bx
        mov es, bx
        mov ax, 0xc207
        int 0x15
        push cs                         ; the report's stosb writes through ES
        pop es

        mov di, line
        mov si, txt_x
        call put_string
        mov ax, [sum_x]
        call put_signed
        mov si, txt_y
        call put_string
        mov ax, [sum_y]
        call put_signed
        mov si, txt_left
        call put_string
        mov ax, [left_down]
        call put_decimal
        mov al, '/'
        stosb
        mov ax, [left_up]
        call put_decimal
        mov si, txt_right
        call put_string
        mov ax, [right_down]
        call put_decimal
        mov al, '/'
        stosb
        mov ax, [right_up]
        call put_decimal
        mov si, txt_packets
        call put_string
        mov ax, [packets]
        call put_decimal
        call emit_line

        mov di, line
        mov si, txt_keys
        call put_string
        mov cx, [key_count]
        mov bx, keys
.next:  jcxz .keys_done
        mov al, ' '
        stosb
        mov ax, [bx]
        add bx, 2
        call put_hex
        dec cx
        jmp .next
.keys_done:
        call emit_line
        mov ax, 0x4c00
        int 0x21

.init_failed:
        mov si, txt_init_failed
        jmp .failed
.enable_failed:
        mov si, txt_enable_failed
.failed:
        mov di, line
        call put_string
        call emit_line
        mov ax, 0x4c01
        int 0x21

; The BIOS calls this far, with interrupts on, after each packet: the status
; byte, X and Y words and a zero Z word on the stack above the return address.
handler:
        push bp
        mov bp, sp
        push ax
        push bx
        push ds
        push cs
        pop ds
        mov bl, [bp + 12]               ; status: buttons, X sign 10h, Y sign 20h
        mov al, [bp + 10]
        xor ah, ah
        test bl, 0x10
        jz .x
        mov ah, 0xff
.x:     add [sum_x], ax
        mov al, [bp + 8]
        xor ah, ah
        test bl, 0x20
        jz .y
        mov ah, 0xff
.y:     add [sum_y], ax
        inc word [packets]
        mov al, [buttons]
        xor al, bl                      ; the buttons that changed
        mov ah, bl
        and ah, 3
        mov [buttons], ah
        test al, 1
        jz .right
        test bl, 1
        jz .left_up
        inc word [left_down]
        jmp .right
.left_up:
        inc word [left_up]
.right: test al, 2
        jz .out
        test bl, 2
        jz .right_up
        inc word [right_down]
        jmp .out
.right_up:
        inc word [right_up]
.out:   pop ds
        pop bx
        pop ax
        pop bp
        retf

; SI, zero-terminated, to DI.
put_string:
        lodsb
        or al, al
        jz .end
        stosb
        jmp put_string
.end:   ret

; AX signed, with its sign, to DI.
put_signed:
        mov byte [di], '+'
        test ax, ax
        jns .positive
        mov byte [di], '-'
        neg ax
.positive:
        inc di
        ; falls through

; AX unsigned, in decimal, to DI.
put_decimal:
        push bx
        push cx
        push dx
        mov bx, 10
        xor cx, cx
.digit: xor dx, dx
        div bx
        push dx
        inc cx
        test ax, ax
        jnz .digit
.out:   pop ax
        add al, '0'
        stosb
        loop .out
        pop dx
        pop cx
        pop bx
        ret

; AX in four hex digits to DI.
put_hex:
        push cx
        mov cx, 4
.digit: rol ax, 4
        push ax
        and al, 0x0f
        add al, '0'
        cmp al, '9'
        jbe .store
        add al, 'A' - '9' - 1
.store: stosb
        pop ax
        loop .digit
        pop cx
        ret

; The line from `line` to DI: to port E9h with a newline, then to the screen.
emit_line:
        mov cx, di
        sub cx, line
        mov si, line
.port:  lodsb
        out 0xe9, al
        cmp si, di
        jb .port
        mov al, 10
        out 0xe9, al
        mov al, 13
        stosb
        mov al, 10
        stosb
        mov ah, 0x40                    ; write to standard output
        mov bx, 1
        add cx, 2
        mov dx, line
        int 0x21
        ret

txt_ready:         db "MOUSE READY", 0
txt_x:             db "MOUSE X=", 0
txt_y:             db " Y=", 0
txt_left:          db " LEFT=", 0
txt_right:         db " RIGHT=", 0
txt_packets:       db " PACKETS=", 0
txt_keys:          db "KEYS", 0
txt_init_failed:   db "MOUSE INIT FAILED", 0
txt_enable_failed: db "MOUSE ENABLE FAILED", 0

sum_x:      dw 0
sum_y:      dw 0
packets:    dw 0
left_down:  dw 0
left_up:    dw 0
right_down: dw 0
right_up:   dw 0
buttons:    db 0
key_count:  dw 0
keys:       times 8 dw 0
line:       times 96 db 0
