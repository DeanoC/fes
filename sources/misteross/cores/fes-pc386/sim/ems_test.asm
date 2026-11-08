; SPDX-License-Identifier: MIT
; EMSTEST.COM: the EMS board and FESEMS.SYS through INT 67h
; (docs/pc386-machine-design.md, "EMS board"). scripts/sim_fes_pc386.py
; --suite ems runs it at the games image's prompt. Every check prints
; "EMS <name> ok" or "EMS <name> FAIL" and writes the same line to port E9h;
; the run ends with "EMS DONE ok" or "EMS DONE FAIL". A failure names its
; step within the check ("EMS move FAIL step 03").
;
;   detect    the EMMXXXX0 name at INT 67h's segment:000Ah; status 0; version 4.0
;   frame     41h gives E000h; 58h gives the four pages at E000h-EC00h
;   counts    42h: 128 pages, all free
;   allocate  43h 64 pages; 42h then leaves 64; 4Ch reports 64
;   pattern   each of the 64 pages filled through a frame page (16 KiB, a
;             value per page and dword), then every dword read back
;   alias     one logical page in frame pages 0 and 2 shows the same bytes
;   unmap     44h with FFFFh: the frame page reads FFh
;   save      47h, a different map, then 48h puts the first back; 4E00h and
;             4E01h do the same through a buffer
;   multiple  50h01h maps two pages by segment
;   realloc   51h to 70 then 60 pages, with 42h following
;   names     53h names the handle, 54h01h finds it
;   move      57h conventional to expanded across a page boundary, back, an
;             overlapping expanded move (92h, data correct) and an exchange
;   errors    83h for a bad handle, 8Ah for a page past the end, 87h for more
;             pages than the board, 84h for function 55h
;   os        5Dh hands out a key on its first call and 01h then refuses 59h
;             and 5Bh (A4h) until 00h with the key; 5B01h's save area comes
;             back from 5B00h with the current map in it, and one with a bad
;             map is refused (A3h) and not kept; 5B06h answers 9Eh
;             (no dedicated DMA); 02h takes the key back
;   free      45h returns every page
;
; Assemble: nasm -f bin -o EMSTEST.COM ems_test.asm. 386 code.

        bits 16
        cpu 386
        org 0x100

PAGES   equ 64

start:  cld
        mov byte [failed], 0

; ---- detect
        mov si, n_detect
        call begin
        xor ax, ax
        mov es, ax
        mov es, [es:0x67 * 4 + 2]
        mov di, 10
        mov si, emm_name
        mov cx, 8
        repe cmpsb
        jne .d_bad
        mov ah, 0x40
        int 0x67
        test ah, ah
        jnz .d_bad
        mov ah, 0x46
        int 0x67
        test ah, ah
        jnz .d_bad
        cmp al, 0x40
        jne .d_bad
        call ok
        jmp frame
.d_bad: call fail
        jmp done                        ; nothing else can run

; ---- frame
frame:  mov si, n_frame
        call begin
        mov ah, 0x41
        int 0x67
        test ah, ah
        jnz .bad
        cmp bx, 0xe000
        jne .bad
        mov [frame_seg], bx
        push ds
        pop es
        mov di, buffer
        mov ax, 0x5800
        int 0x67
        test ah, ah
        jnz .bad
        cmp cx, 4
        jne .bad
        mov si, buffer
        mov bx, 0xe000
        xor dx, dx
.entry: cmp [si], bx
        jne .bad
        cmp [si + 2], dx
        jne .bad
        add si, 4
        add bx, 0x400
        inc dx
        cmp dx, 4
        jb .entry
        call ok
        jmp counts
.bad:   call fail

; ---- counts
counts: mov si, n_counts
        call begin
        mov ah, 0x42
        int 0x67
        test ah, ah
        jnz .bad
        cmp dx, 128
        jne .bad
        cmp bx, 128
        jne .bad
        call ok
        jmp allocate
.bad:   call fail

; ---- allocate
allocate:
        mov si, n_allocate
        call begin
        mov ah, 0x43
        mov bx, PAGES
        int 0x67
        test ah, ah
        jnz .bad
        mov [handle], dx
        mov ah, 0x42
        int 0x67
        cmp bx, 128 - PAGES
        jne .bad
        mov ah, 0x4c
        mov dx, [handle]
        int 0x67
        test ah, ah
        jnz .bad
        cmp bx, PAGES
        jne .bad
        call ok
        jmp pattern
.bad:   call fail
        jmp done

; ---- pattern: page p through frame page p mod 4, value (p << 16) + dword index
pattern:
        mov si, n_pattern
        call begin
        xor bp, bp                      ; logical page
.fill:  call map_bp
        jc .bad
        call frame_es
        xor di, di
        movzx eax, bp
        shl eax, 16
        mov cx, 0x1000
.fd:    stosd
        inc eax
        loop .fd
        inc bp
        cmp bp, PAGES
        jb .fill
        xor bp, bp
.check: call map_bp
        jc .bad
        call frame_es
        xor di, di
        movzx eax, bp
        shl eax, 16
        mov cx, 0x1000
.cd:    cmp [es:di], eax
        jne .bad
        add di, 4
        inc eax
        loop .cd
        inc bp
        cmp bp, PAGES
        jb .check
        call ok
        jmp alias
.bad:   call fail

; ---- alias: logical page 5 in frame pages 0 and 2
alias:  mov si, n_alias
        call begin
        mov ax, 0x4400
        mov bx, 5
        mov dx, [handle]
        int 0x67
        mov ax, 0x4402
        int 0x67
        test ah, ah
        jnz .bad
        mov es, [frame_seg]
        mov dword [es:0x1230], 0x12345678
        mov eax, [es:0x8000 + 0x1230]
        cmp eax, 0x12345678
        jne .bad
        mov dword [es:0x1230], (5 << 16) + 0x48c   ; the pattern value again
        call ok
        jmp unmap
.bad:   call fail

; ---- unmap
unmap:  mov si, n_unmap
        call begin
        mov ax, 0x4401
        mov bx, 0xffff
        mov dx, [handle]
        int 0x67
        test ah, ah
        jnz .bad
        mov es, [frame_seg]
        cmp dword [es:0x4000], 0xffffffff
        jne .bad
        call ok
        jmp save
.bad:   call fail

; ---- save: frame page 0 on logical 1, saved; logical 2; restored shows 1
save:   mov si, n_save
        call begin
        mov dx, [handle]
        mov ax, 0x4400
        mov bx, 1
        int 0x67
        mov ah, 0x47
        int 0x67
        test ah, ah
        jnz .bad
        mov ax, 0x4400
        mov bx, 2
        int 0x67
        mov ah, 0x48
        int 0x67
        test ah, ah
        jnz .bad
        mov es, [frame_seg]
        cmp dword [es:0], 1 << 16
        jne .bad
        push ds                         ; 4E00h into the buffer, remap, 4E01h back
        pop es
        mov di, buffer
        mov ax, 0x4e00
        int 0x67
        test ah, ah
        jnz .bad
        mov ax, 0x4400
        mov bx, 3
        int 0x67
        mov si, buffer
        mov ax, 0x4e01
        int 0x67
        test ah, ah
        jnz .bad
        mov es, [frame_seg]
        cmp dword [es:0], 1 << 16
        jne .bad
        call ok
        jmp multiple
.bad:   call fail

; ---- multiple: logical 10 at E400h, logical 11 at EC00h
multiple:
        mov si, n_multiple
        call begin
        mov word [buffer], 10
        mov word [buffer + 2], 0xe400
        mov word [buffer + 4], 11
        mov word [buffer + 6], 0xec00
        mov si, buffer
        mov cx, 2
        mov dx, [handle]
        mov ax, 0x5001
        int 0x67
        test ah, ah
        jnz .bad
        mov es, [frame_seg]
        cmp dword [es:0x4000], 10 << 16
        jne .bad
        cmp dword [es:0xc000], 11 << 16
        jne .bad
        call ok
        jmp realloc
.bad:   call fail

; ---- realloc
realloc:
        mov si, n_realloc
        call begin
        mov dx, [handle]
        mov ah, 0x51
        mov bx, 70
        int 0x67
        test ah, ah
        jnz .bad
        cmp bx, 70
        jne .bad
        mov ah, 0x42
        int 0x67
        cmp bx, 128 - 70
        jne .bad
        mov dx, [handle]
        mov ah, 0x51
        mov bx, 60
        int 0x67
        test ah, ah
        jnz .bad
        mov ah, 0x42
        int 0x67
        cmp bx, 128 - 60
        jne .bad
        mov dx, [handle]                ; the kept pages still hold the pattern
        mov ax, 0x4400
        mov bx, 59
        int 0x67
        mov es, [frame_seg]
        cmp dword [es:4], (59 << 16) + 1
        jne .bad
        call ok
        jmp names
.bad:   call fail

; ---- names
names:  mov si, n_names
        call begin
        mov si, test_name
        mov dx, [handle]
        mov ax, 0x5301
        int 0x67
        test ah, ah
        jnz .bad
        mov si, test_name
        mov ax, 0x5401
        int 0x67
        test ah, ah
        jnz .bad
        cmp dx, [handle]
        jne .bad
        call ok
        jmp move
.bad:   call fail

; ---- move
move:   mov si, n_move
        call begin
        push ds
        pop es
        mov di, source                  ; conventional source: bytes 0, 1, 2, ...
        mov cx, 1000
        xor al, al
.src:   stosb
        inc al
        loop .src
        ; conventional to expanded: logical 3 offset 3E00h, crossing into logical 4
        mov dword [region], 1000
        mov byte [region + 4], 0
        mov word [region + 5], 0
        mov word [region + 7], source
        mov [region + 9], ds
        mov byte [region + 11], 1
        mov ax, [handle]
        mov [region + 12], ax
        mov word [region + 14], 0x3e00
        mov word [region + 16], 3
        mov si, region
        mov ax, 0x5700
        int 0x67
        test ah, ah
        mov byte [step], 1
        jnz .bad
        mov dx, [handle]                ; check through the frame
        mov ax, 0x4400
        mov bx, 3
        int 0x67
        mov ax, 0x4401
        mov bx, 4
        int 0x67
        mov es, [frame_seg]
        cmp byte [es:0x3e00], 0
        mov byte [step], 2
        jne .bad
        cmp byte [es:0x3fff], 0xff
        mov byte [step], 3
        jne .bad
        cmp byte [es:0x4000], 0         ; byte 512 is 0
        mov byte [step], 4
        jne .bad
        cmp byte [es:0x4000 + 487], 999 & 0xff
        mov byte [step], 5
        jne .bad
        ; expanded back to conventional
        push ds
        pop es
        mov di, target
        mov cx, 1000
        xor al, al
        rep stosb
        mov si, region
        call swap_sides
        mov word [region + 14], target  ; into target, not source
        mov si, region
        mov ax, 0x5700
        int 0x67
        test ah, ah
        mov byte [step], 6
        jnz .bad
        mov si, source
        mov di, target
        mov cx, 1000
        repe cmpsb
        mov byte [step], 7
        jne .bad
        ; overlapping expanded move, 5 bytes up: 92h, and the source arrives whole
        mov dword [region], 1000
        mov byte [region + 4], 1
        mov ax, [handle]
        mov [region + 5], ax
        mov word [region + 7], 0x3e00
        mov word [region + 9], 3
        mov byte [region + 11], 1
        mov [region + 12], ax
        mov word [region + 14], 0x3e05
        mov word [region + 16], 3
        mov si, region
        mov ax, 0x5700
        int 0x67
        cmp ah, 0x92
        mov byte [step], 8
        jne .bad
        mov dx, [handle]
        mov ax, 0x4400
        mov bx, 3
        int 0x67
        mov ax, 0x4401
        mov bx, 4
        int 0x67
        mov es, [frame_seg]
        mov di, 0x3e05
        mov si, source
        mov cx, 1000
.ov:    mov al, [si]
        cmp al, [es:di]
        mov byte [step], 9
        jne .bad
        inc si
        inc di
        loop .ov
        ; exchange conventional with expanded: 16 bytes
        push ds
        pop es
        mov di, target
        mov cx, 16
        mov al, 0xa5
        rep stosb
        mov dword [region], 16
        mov byte [region + 4], 0
        mov word [region + 5], 0
        mov word [region + 7], target
        mov [region + 9], ds
        mov byte [region + 11], 1
        mov ax, [handle]
        mov [region + 12], ax
        mov word [region + 14], 0x3e05
        mov word [region + 16], 3
        mov si, region
        mov ax, 0x5701
        int 0x67
        test ah, ah
        mov byte [step], 10
        jnz .bad
        mov si, source                  ; conventional now holds the old EMS bytes
        mov di, target
        mov cx, 16
        repe cmpsb
        mov byte [step], 11
        jne .bad
        mov es, [frame_seg]
        cmp dword [es:0x3e05], 0xa5a5a5a5
        mov byte [step], 12
        jne .bad
        call ok
        jmp errors
.bad:   call fail

; ---- errors
errors: mov si, n_errors
        call begin
        mov ax, 0x4400
        mov bx, 0
        mov dx, 63                      ; never allocated
        int 0x67
        cmp ah, 0x83
        jne .bad
        mov ax, 0x4400
        mov bx, 60
        mov dx, [handle]
        int 0x67
        cmp ah, 0x8a
        jne .bad
        mov ah, 0x43
        mov bx, 200
        int 0x67
        cmp ah, 0x87
        jne .bad
        mov ax, 0x5500
        int 0x67
        cmp ah, 0x84
        jne .bad
        call ok
        jmp os
.bad:   call fail

; ---- os
os:     mov si, n_os
        call begin
        mov byte [step], 1
        mov ax, 0x5d01                  ; first call: the key, and disabled
        int 0x67
        test ah, ah
        jnz .bad
        mov [key], bx
        mov [key + 2], cx
        mov byte [step], 2
        push ds
        pop es
        mov di, buffer
        mov ax, 0x5900
        int 0x67
        cmp ah, 0xa4
        jne .bad
        mov byte [step], 3
        mov ax, 0x5d00                  ; a wrong key is refused
        mov bx, [key]
        mov cx, [key + 2]
        inc cx
        int 0x67
        cmp ah, 0xa4
        jne .bad
        mov byte [step], 4
        mov ax, 0x5d00
        mov bx, [key]
        mov cx, [key + 2]
        int 0x67
        test ah, ah
        jnz .bad
        mov byte [step], 5
        push ds                         ; 5B01h: set 0 with a save area
        pop es
        mov di, save_area
        mov ax, 0x5b01
        xor bl, bl
        mov dword [save_area], 0xffffffff  ; every page unmapped
        int 0x67
        test ah, ah
        jnz .bad
        mov byte [step], 6
        mov dx, [handle]                ; frame page 0 on logical 2, then 5B00h saves it
        mov ax, 0x4400
        mov bx, 2
        int 0x67
        xor di, di
        mov es, di
        mov ax, 0x5b00
        int 0x67
        test ah, ah
        jnz .bad
        mov byte [step], 7
        mov ax, es
        mov cx, ds
        cmp ax, cx
        jne .bad
        cmp di, save_area
        jne .bad
        cmp byte [save_area], 0xff      ; frame page 0 is mapped now
        je .bad
        mov byte [step], 11
        push ds                         ; a save area with a bad map is refused and not kept
        pop es
        mov di, bad_area
        mov ax, 0x5b01
        xor bl, bl
        int 0x67
        cmp ah, 0xa3
        jne .bad
        mov byte [step], 12
        xor di, di
        mov es, di
        mov ax, 0x5b00
        int 0x67
        test ah, ah
        jnz .bad
        cmp di, save_area
        jne .bad
        mov byte [step], 8
        push ds                         ; back to no save area
        pop es
        xor di, di
        mov es, di
        mov ax, 0x5b01
        xor bl, bl
        int 0x67
        test ah, ah
        jnz .bad
        mov byte [step], 9
        mov ax, 0x5b06                  ; no dedicated DMA channels
        xor bl, bl
        int 0x67
        cmp ah, 0x9e
        jne .bad
        mov byte [step], 10
        mov ax, 0x5d02
        mov bx, [key]
        mov cx, [key + 2]
        int 0x67
        test ah, ah
        jnz .bad
        call ok
        jmp free
.bad:   call fail

; ---- os
free:   mov si, n_free
        call begin
        mov ah, 0x45
        mov dx, [handle]
        int 0x67
        test ah, ah
        jnz .bad
        mov ah, 0x42
        int 0x67
        cmp bx, 128
        jne .bad
        call ok
        jmp done
.bad:   call fail

done:   mov si, n_done
        call begin
        cmp byte [failed], 0
        jne .bad
        call ok
        jmp .exit
.bad:   call fail
.exit:  mov ax, 0x4c00
        int 0x21

; ---- helpers

; BP a logical page of the handle: mapped at frame page BP mod 4; CF on error.
map_bp: mov ax, bp
        and al, 3
        mov ah, 0x44
        mov bx, bp
        mov dx, [handle]
        int 0x67
        test ah, ah
        jz .ok
        stc
        ret
.ok:    clc
        ret

; ES the frame page BP mod 4.
frame_es:
        mov ax, bp
        and ax, 3
        shl ax, 10
        add ax, [frame_seg]
        mov es, ax
        ret

; The region's source and destination swapped.
swap_sides:
        mov cx, 7
        lea di, [si + 11]
        add si, 4
.next:  mov al, [si]
        xchg al, [di]
        mov [si], al
        inc si
        inc di
        loop .next
        ret

; DS:SI a check's name: "EMS <name> " printed and sent.
begin:  mov [current], si
        mov byte [step], 0
        ret
ok:     mov dx, word_ok
        jmp report
fail:   mov byte [failed], 1
        mov al, [step]                  ; the failed step within the check, as a digit pair
        aam
        add ax, '00'
        mov [fail_step], ah
        mov [fail_step + 1], al
        mov dx, word_fail
report: push dx
        mov si, prefix
        call put
        mov si, [current]
        call put
        pop si
        call put
        ret
; DS:SI a zero-ended string: to the screen and port E9h.
put:    lodsb
        test al, al
        jz .done
        out 0xe9, al
        mov dl, al
        mov ah, 0x02
        int 0x21
        jmp put
.done:  ret

emm_name:   db 'EMMXXXX0'
test_name:  db 'FESTEST', 0
prefix:     db 'EMS ', 0
word_ok:    db ' ok', 13, 10, 0
word_fail:  db ' FAIL step '
fail_step:  db '00', 13, 10, 0
n_detect:   db 'detect', 0
n_frame:    db 'frame', 0
n_counts:   db 'counts', 0
n_allocate: db 'allocate', 0
n_pattern:  db 'pattern', 0
n_alias:    db 'alias', 0
n_unmap:    db 'unmap', 0
n_save:     db 'save', 0
n_multiple: db 'multiple', 0
n_realloc:  db 'realloc', 0
n_names:    db 'names', 0
n_move:     db 'move', 0
n_errors:   db 'errors', 0
n_os:       db 'os', 0
n_free:     db 'free', 0
n_done:     db 'DONE', 0

failed:     db 0
step:       db 0
current:    dw 0
handle:     dw 0
key:        dd 0
save_area:  dd 0
bad_area:   db 0x80, 0x80, 0x80, 0x80   ; board pages past a 128-page board
frame_seg:  dw 0
region:     times 18 db 0
buffer:     times 64 db 0
source:     times 1000 db 0
target:     times 1000 db 0
