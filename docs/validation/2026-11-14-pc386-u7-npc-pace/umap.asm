; SPDX-License-Identifier: MIT
; UMAP.COM: what the guest sees in the upper memory area, block by block.
;
; Written for fes#1853. The Black Gate will not start on a reference 386
; because the games image's UMB provider finds no RAM at C800, and the fix is
; a configuration change on the reference. A claim about a memory map needs a
; reading of that map from inside the guest, on both machines, before and
; after the change -- not an inference from an emulator's board list.
;
; It walks A000:0000 to F800:0000 in 4 KiB blocks and prints one character per
; block, sixteen to a row, each row labelled with its segment:
;
;   R  the block starts with the AA55 option-ROM signature
;   W  writable: a complemented byte reads back changed, and is restored
;   r  read-only and not uniform -- ROM body, or a shadowed region
;   .  read-only and reads FF everywhere sampled -- open bus
;   -  read-only and reads 00 everywhere sampled
;   =  read-only and uniform at some other value -- not open bus, not zeroes
;
; Four offsets are sampled for the uniformity test (0, 1, 800h, FFFh). The
; write test writes one byte and puts the original back, so a writable block
; is left as it was found.
;
; Assemble: nasm -f bin -o UMAP.COM umap.asm

        bits 16
        org 0x100

start:
        mov dx, banner
        mov ah, 9
        int 0x21
        mov word [curseg], 0xa000
.row:
        mov ax, [curseg]
        call hex16
        mov dl, ' '
        call putc
        mov cx, 16
.blk:
        push cx
        call probe
        mov dl, al
        call putc
        add word [curseg], 0x100
        pop cx
        loop .blk
        call crlf
        cmp word [curseg], 0
        jne .row
        mov ax, 0x4c00
        int 0x21

; AL = the character for the block at [curseg]
probe:
        push bx
        push cx
        push es
        mov es, [curseg]
        cmp word [es:0], 0xaa55
        je .rom
        mov al, [es:0]
        mov bl, al
        not al
        mov [es:0], al
        mov cl, [es:0]
        mov [es:0], bl
        cmp cl, al
        je .ram
        mov al, [es:0]
        cmp al, [es:1]
        jne .ro
        cmp al, [es:0x800]
        jne .ro
        cmp al, [es:0xfff]
        jne .ro
        cmp al, 0xff
        je .open
        or al, al
        jz .zero
        mov al, '='
        jmp .done
.zero:  mov al, '-'
        jmp .done
.open:  mov al, '.'
        jmp .done
.ro:    mov al, 'r'
        jmp .done
.ram:   mov al, 'W'
        jmp .done
.rom:   mov al, 'R'
.done:
        pop es
        pop cx
        pop bx
        ret

hex16:
        push ax
        mov al, ah
        call hex8
        pop ax
        call hex8
        ret
hex8:
        push ax
        shr al, 4
        call hexnib
        pop ax
        and al, 0x0f
        call hexnib
        ret
hexnib:
        add al, '0'
        cmp al, '9'
        jbe .p
        add al, 7
.p:     mov dl, al
        call putc
        ret
putc:
        push ax
        mov ah, 2
        int 0x21
        pop ax
        ret
crlf:
        mov dl, 13
        call putc
        mov dl, 10
        call putc
        ret

banner: db 'UMAP A000-FFFF 4K R=rom W=ram r=ro .=ff -=00 ==same', 13, 10, '$'
curseg: dw 0
