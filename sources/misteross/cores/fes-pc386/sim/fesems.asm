; SPDX-License-Identifier: MIT
; FESEMS.SYS: a LIM EMS 4.0 driver for the pc386 machine's EMS board
; (docs/pc386-machine-design.md, "EMS board"). The board is the Lo-tech 2MB
; EMS Board's interface: four write-only page registers at consecutive ports,
; register n choosing the 16 KiB board page that page n of the 64 KiB frame
; shows, pages numbered from 0.
;
;   DEVICEHIGH=C:\FDOS\BIN\FESEMS.SYS
;
; The machine describes the board in CMOS: 63h its page count (0, no board),
; 64h the frame segment's high byte (E0h) and 65h the register base / 16
; (26h for 260h). With no board the driver does not stay resident. At load it
; unmaps the frame, writes zeros through every board page and checks a
; marker at each page's ends, so EMS never starts with an earlier run's bytes
; and two pages that alias are caught.
;
; Functions: 40h-48h and 4Bh-4Fh (LIM 3.2 and the 4.0 page map calls), 50h
; map multiple, 51h reallocate, 52h attributes (volatile only), 53h and 54h
; handle names, 57h move and exchange, 58h mappable addresses, 59h hardware
; information, 5Ah raw allocation, 5Bh with no alternate register sets, 5Ch
; and 5Dh. Anything else answers 84h and writes "FESEMS: function <AX>" to
; port E9h, the machine's debug port, so a bench run names it. Raw and
; standard pages are both 16 KiB.
;
; Assemble: nasm -f bin -o FESEMS.SYS fesems.asm. 386 code.

        bits 16
        cpu 386
        org 0

MAXH    equ 64                  ; handles, 0 the operating system's
NONE    equ 0xff                ; no page: a free page's owner, a chain's end, an unmapped register

; Caller registers in the handler's frame (pusha after DS and ES).
F_DI    equ 0
F_SI    equ 2
F_BX    equ 8
F_DX    equ 10
F_CX    equ 12
F_AX    equ 14
F_ES    equ 16
F_DS    equ 18

; ---- Device header ------------------------------------------------------
header: dd -1
        dw 0x8000               ; character device
        dw strategy
        dw interrupt
        db 'EMMXXXX0'

request: dd 0

strategy:
        mov [cs:request], bx
        mov [cs:request + 2], es
        retf

interrupt:
        push ax
        push bx
        push ds
        lds bx, [cs:request]
        cmp byte [bx + 2], 0
        jne .other
        call init
        jmp .out
.other: mov word [bx + 3], 0x0100   ; done: the device only exists to be found
.out:   pop ds
        pop bx
        pop ax
        retf

; ---- Resident data --------------------------------------------------------
pages       db 0                ; board pages
free        db 0                ; unallocated pages
frame_seg   dw 0xe000
port_base   dw 0x260
os_key      dd 0
os_given    db 0
cur_map     times 4 db NONE     ; the four registers' values
page_owner  times 256 db NONE   ; handle owning each board page
page_next   times 256 db NONE   ; next page of the owner's chain
h_used      times MAXH db 0
h_count     times MAXH db 0
h_first     times MAXH db NONE
h_saved     times MAXH db 0
h_save      times MAXH * 4 db 0
h_name      times MAXH * 8 db 0
; Function 57h's region, copied from the caller.
mv_length   dd 0
mv_src      times 7 db 0        ; type, handle, offset, segment or page
mv_dst      times 7 db 0
mv_saved    dw 0                ; frame pages 0 and 1 before the copy
mv_status   db 0                ; 0 move, 1 exchange, 2 move backward
mv_left     dd 0                ; bytes still to copy
list_ptr    dw 0                ; function 50h's next entry, in the caller's DS

; ---- INT 67h ------------------------------------------------------------
int67:
        push ds
        push es
        pusha
        mov bp, sp
        cld
        push cs
        pop ds
        mov al, ah
        sub al, 0x40
        jb .bad
        cmp al, (functions_end - functions) / 2
        jae .bad
        movzx bx, al
        add bx, bx
        mov ax, [bp + F_AX]
        mov dx, [bp + F_DX]
        call [functions + bx]
        mov [bp + F_AX + 1], ah
        popa
        pop es
        pop ds
        iret
.bad:   call unsupported
        mov byte [bp + F_AX + 1], 0x84
        popa
        pop es
        pop ds
        iret

functions:
        dw f40, f41, f42, f43, f44, f45, f46, f47, f48, unsupported, unsupported, f4b, f4c, f4d, f4e, f4f
        dw f50, f51, f52, f53, f54, unsupported, unsupported, f57, f58, f59, f5a, f5b, f5c, f5d
functions_end:

; Writes "FESEMS: function <AX>" to port E9h; answers 84h.
unsupported:
        mov si, msg_function
.text:  lodsb
        test al, al
        jz .hex
        out 0xe9, al
        jmp .text
.hex:   mov dx, [bp + F_AX]
        mov cx, 4
.digit: rol dx, 4
        mov al, dl
        and al, 15
        add al, '0'
        cmp al, '9'
        jbe .put
        add al, 'A' - '9' - 1
.put:   out 0xe9, al
        loop .digit
        mov al, 10
        out 0xe9, al
        mov ah, 0x84
        ret
msg_function: db 'FESEMS: function ', 0

; ---- Helpers (DS = CS) ----------------------------------------------------

; DX a handle: CF and AH 83h when it is not an allocated one; else SI the handle.
check_handle:
        cmp dx, MAXH
        jae .no
        mov si, dx
        cmp byte [h_used + si], 0
        je .no
        clc
        ret
.no:    mov ah, 0x83
        stc
        ret

; SI a handle, BX a logical page: AL its board page, or CF and AH 8Ah.
board_page:
        movzx cx, byte [h_count + si]
        cmp bx, cx
        jae .no
        mov al, [h_first + si]
        mov cx, bx
        jcxz .done
        movzx di, al
.walk:  mov al, [page_next + di]
        movzx di, al
        loop .walk
.done:  clc
        ret
.no:    mov ah, 0x8a
        stc
        ret

; AL a frame page 0-3, AH the register value: the register written. Any DS.
set_register:
        push dx
        push bx
        movzx bx, al
        mov [cs:cur_map + bx], ah
        mov dx, [cs:port_base]
        add dx, bx
        mov al, ah
        out dx, al
        pop bx
        pop dx
        ret

; SI a handle, BX a logical page (FFFFh unmaps), AL a frame page 0-3: map it,
; or CF and AH 8Ah or 8Bh.
map_one:
        cmp al, 4
        jae .frame
        mov dl, al
        mov ah, NONE
        cmp bx, 0xffff
        je .set
        call board_page
        jc .done
        mov ah, al
.set:   mov al, dl
        call set_register
        clc
.done:  ret
.frame: mov ah, 0x8b
        stc
        ret

; DX a segment: AL its frame page, or CF and AH 8Bh.
frame_page:
        mov ax, dx
        sub ax, [frame_seg]
        jb .no
        test ax, 0x3ff
        jnz .no
        shr ax, 10
        cmp ax, 4
        jae .no
        clc
        ret
.no:    mov ah, 0x8b
        stc
        ret

; SI a handle, CX pages to add to its chain: AH 0, or 87h/88h and nothing done.
grow:
        movzx bx, byte [pages]
        cmp cx, bx
        ja .total
        movzx ax, byte [h_count + si]
        add ax, cx
        cmp ax, bx
        ja .total
        cmp cl, [free]
        ja .free
        jcxz .ok
        ; DI the chain's last page, or NONE
        mov al, [h_first + si]
        mov di, NONE
.tail:  cmp al, NONE
        je .search
        movzx di, al
        mov al, [page_next + di]
        jmp .tail
.search:
        xor bx, bx
.find:  cmp byte [page_owner + bx], NONE
        je .take
        inc bx
        jmp .find
.take:  mov ax, si
        mov [page_owner + bx], al
        mov byte [page_next + bx], NONE
        cmp di, NONE
        jne .link
        mov [h_first + si], bl
        jmp .linked
.link:  mov [page_next + di], bl
.linked:
        mov di, bx
        inc byte [h_count + si]
        dec byte [free]
        inc bx
        loop .find
.ok:    xor ah, ah
        ret
.total: mov ah, 0x87
        ret
.free:  mov ah, 0x88
        ret

; SI a handle, CX the pages to keep: the rest of its chain freed.
shrink:
        cmp cl, [h_count + si]
        jae .done
        mov al, [h_first + si]
        jcxz .cut_first
        dec cx
        movzx di, al
        jcxz .cut
.walk:  mov al, [page_next + di]
        movzx di, al
        loop .walk
.cut:   mov al, [page_next + di]
        mov byte [page_next + di], NONE
        jmp .free
.cut_first:
        mov byte [h_first + si], NONE
.free:  cmp al, NONE
        je .done
        movzx di, al
        mov al, [page_next + di]
        mov byte [page_owner + di], NONE
        mov byte [page_next + di], NONE
        dec byte [h_count + si]
        inc byte [free]
        jmp .free
.done:  ret

; A free handle in SI (marked used, no pages, no name), or CF and AH 85h.
new_handle:
        xor si, si
.find:  cmp byte [h_used + si], 0
        je .take
        inc si
        cmp si, MAXH
        jb .find
        mov ah, 0x85
        stc
        ret
.take:  mov byte [h_used + si], 1
        mov byte [h_count + si], 0
        mov byte [h_first + si], NONE
        mov byte [h_saved + si], 0
        mov bx, si
        shl bx, 3
        xor eax, eax
        mov [h_name + bx], eax
        mov [h_name + bx + 4], eax
        clc
        ret

; ---- Functions: AL and DX the caller's, AH returned -----------------------

f40:    xor ah, ah                      ; get status
        ret

f41:    mov ax, [frame_seg]             ; get page frame address
        mov [bp + F_BX], ax
        xor ah, ah
        ret

f42:    movzx ax, byte [free]           ; get unallocated page count
        mov [bp + F_BX], ax
        mov al, [pages]
        mov [bp + F_DX], ax
        xor ah, ah
        ret

f43:    mov cx, [bp + F_BX]             ; allocate pages
        mov ah, 0x89
        jcxz .done
        jmp allocate
.done:  ret

; CX pages for a new handle: DX the handle.
allocate:
        movzx ax, byte [pages]
        cmp cx, ax
        ja .total
        cmp cl, [free]
        ja .free
        call new_handle
        jc .done
        call grow
        mov [bp + F_DX], si
.done:  ret
.total: mov ah, 0x87
        ret
.free:  mov ah, 0x88
        ret

f44:    call check_handle               ; map or unmap a page
        jc .done
        mov bx, [bp + F_BX]
        call map_one
        jc .done
        xor ah, ah
.done:  ret

f45:    call check_handle               ; deallocate pages
        jc .done
        mov ah, 0x86
        cmp byte [h_saved + si], 0
        jne .done
        xor cx, cx
        call shrink
        test si, si
        jz .ok                          ; handle 0 stays, with no pages
        mov byte [h_used + si], 0
.ok:    xor ah, ah
.done:  ret

f46:    mov al, 0x40                    ; get version: 4.0
        mov [bp + F_AX], al
        xor ah, ah
        ret

f47:    call check_handle               ; save page map
        jc .done
        mov ah, 0x8d
        cmp byte [h_saved + si], 0
        jne .done
        mov byte [h_saved + si], 1
        shl si, 2
        mov eax, [cur_map]
        mov [h_save + si], eax
        xor ah, ah
.done:  ret

f48:    call check_handle               ; restore page map
        jc .done
        mov ah, 0x8e
        cmp byte [h_saved + si], 0
        je .done
        mov byte [h_saved + si], 0
        shl si, 2
        lea si, [h_save + si]
        call load_map
        xor ah, ah
.done:  ret

; DS:SI four register values: the frame mapped from them.
load_map:
        xor bx, bx
.next:  mov ah, [si + bx]
        mov al, bl
        call set_register
        inc bx
        cmp bx, 4
        jb .next
        ret

f4b:    xor ax, ax                      ; get handle count
        xor si, si
.count: add al, [h_used + si]
        inc si
        cmp si, MAXH
        jb .count
        mov [bp + F_BX], ax
        xor ah, ah
        ret

f4c:    call check_handle               ; get handle pages
        jc .done
        movzx ax, byte [h_count + si]
        mov [bp + F_BX], ax
        xor ah, ah
.done:  ret

f4d:    mov es, [bp + F_ES]             ; get all handle pages
        mov di, [bp + F_DI]
        xor si, si
        xor bx, bx
.next:  cmp byte [h_used + si], 0
        je .skip
        mov ax, si
        stosw
        movzx ax, byte [h_count + si]
        stosw
        inc bx
.skip:  inc si
        cmp si, MAXH
        jb .next
        mov [bp + F_BX], bx
        xor ah, ah
        ret

f4e:    cmp al, 3                       ; get or set the page map
        ja .sub
        je .size
        test al, 1
        jnz .set                        ; 01h set only
        mov es, [bp + F_ES]             ; 00h get, 02h get then set
        mov di, [bp + F_DI]
        mov ecx, [cur_map]
        mov [es:di], ecx
        test al, 2
        jz .ok
.set:   push ds
        mov ds, [bp + F_DS]
        mov si, [bp + F_SI]
        mov ecx, [si]
        pop ds
        call valid_map
        jc .done
        push ecx
        mov si, sp
        push ds
        push ss
        pop ds
        call load_map
        pop ds
        pop ecx
.ok:    xor ah, ah
.done:  ret
.size:  mov byte [bp + F_AX], 4
        xor ah, ah
        ret
.sub:   mov ah, 0x8f
        ret

; ECX four register values: CF and AH A3h unless each is FFh or a board page.
valid_map:
        push ecx
        mov dx, 4
.next:  cmp cl, NONE
        je .ok
        cmp cl, [pages]
        jae .bad
.ok:    shr ecx, 8
        dec dx
        jnz .next
        pop ecx
        clc
        ret
.bad:   pop ecx
        mov ah, 0xa3
        stc
        ret

f4f:    cmp al, 2                       ; partial page map
        ja .sub
        je .size
        test al, al
        jnz .set
        ; 00h: DS:SI {count, segment...} to ES:DI {count, (segment, value)...}
        mov es, [bp + F_ES]
        mov di, [bp + F_DI]
        push ds
        mov ds, [bp + F_DS]
        mov si, [bp + F_SI]
        lodsw
        mov cx, ax
        pop ds
        mov ah, 0x8b
        cmp cx, 4
        ja .done
        mov al, cl
        stosb
        jcxz .ok
.get:   push ds
        mov ds, [bp + F_DS]
        lodsw
        pop ds
        mov dx, ax
        push ax
        call frame_page
        pop dx
        jc .done
        mov [es:di], dx
        movzx bx, al
        mov al, [cur_map + bx]
        mov [es:di + 2], al
        add di, 3
        loop .get
.ok:    xor ah, ah
.done:  ret
.set:   push ds                         ; 01h: DS:SI the array 00h made
        mov ds, [bp + F_DS]
        mov si, [bp + F_SI]
        lodsb
        movzx cx, al
        pop ds
        mov ah, 0xa3
        cmp cx, 4
        ja .done
        jcxz .ok
.put:   push ds
        mov ds, [bp + F_DS]
        mov dx, [si]
        mov bl, [si + 2]
        pop ds
        add si, 3
        call frame_page
        jc .bad
        cmp bl, NONE
        je .valid
        cmp bl, [pages]
        jae .bad
.valid: mov ah, bl
        call set_register
        loop .put
        jmp .ok
.bad:   mov ah, 0xa3
        ret
.size:  mov ax, [bp + F_BX]             ; 02h: BX pages
        cmp ax, 4
        ja .frame
        mov ah, al
        add al, al
        add al, ah
        inc al
        mov [bp + F_AX], al
        xor ah, ah
        ret
.frame: mov ah, 0x8b
        ret
.sub:   mov ah, 0x8f
        ret

f50:    cmp al, 1                       ; map or unmap multiple pages
        ja .sub
        mov di, ax                      ; DI low byte: 0 page numbers, 1 segments
        call check_handle
        jc .done
        mov cx, [bp + F_CX]
        mov ax, [bp + F_SI]
        mov [list_ptr], ax
.next:  jcxz .ok
        mov bx, [list_ptr]
        add word [list_ptr], 4
        push ds
        mov ds, [bp + F_DS]
        mov ax, [bx + 2]
        mov bx, [bx]
        pop ds
        test di, 1
        jz .number
        push bx
        mov dx, ax
        call frame_page
        pop bx
        jc .done
        jmp .map
.number:
        cmp ax, 4
        mov ah, 0x8b
        jae .done
.map:   push cx
        push si
        push di
        call map_one
        pop di
        pop si
        pop cx
        jc .done
        dec cx
        jmp .next
.ok:    xor ah, ah
.done:  ret
.sub:   mov ah, 0x8f
        ret

f51:    call check_handle               ; reallocate pages
        jc .done
        mov cx, [bp + F_BX]
        movzx ax, byte [h_count + si]
        cmp cx, ax
        jb .less
        sub cx, ax
        call grow
        test ah, ah
        jnz .done
        jmp .ok
.less:  call shrink
.ok:    movzx ax, byte [h_count + si]
        mov [bp + F_BX], ax
        xor ah, ah
.done:  ret

f52:    cmp al, 2                       ; handle attributes: volatile only
        ja .sub
        je .cap
        call check_handle
        jc .done
        test al, al
        jnz .set
        mov byte [bp + F_AX], 0
        xor ah, ah
        ret
.set:   mov ah, 0x90
        cmp byte [bp + F_BX], 1
        ja .done
        mov ah, 0x91
        je .done
        xor ah, ah
.done:  ret
.cap:   mov byte [bp + F_AX], 0
        xor ah, ah
        ret
.sub:   mov ah, 0x8f
        ret

f53:    cmp al, 1                       ; handle name
        ja .sub
        call check_handle
        jc .done
        shl si, 3
        lea si, [h_name + si]
        test al, al
        jnz .set
        mov es, [bp + F_ES]
        mov di, [bp + F_DI]
        movsd
        movsd
        xor ah, ah
.done:  ret
.set:   push ds
        mov ds, [bp + F_DS]
        mov bx, [bp + F_SI]
        mov ecx, [bx]
        mov edx, [bx + 4]
        pop ds
        mov eax, ecx
        or eax, edx
        jz .store                       ; a null name never clashes
        xor bx, bx
.clash: cmp byte [h_used + bx], 0
        je .other
        mov di, bx
        shl di, 3
        lea di, [h_name + di]
        cmp di, si
        je .other
        cmp [di], ecx
        jne .other
        cmp [di + 4], edx
        jne .other
        mov ah, 0xa1
        ret
.other: inc bx
        cmp bx, MAXH
        jb .clash
.store: mov [si], ecx
        mov [si + 4], edx
        xor ah, ah
        ret
.sub:   mov ah, 0x8f
        ret

f54:    cmp al, 1                       ; handle directory
        ja .total
        je .search
        mov es, [bp + F_ES]
        mov di, [bp + F_DI]
        xor bx, bx
        xor dl, dl
.next:  cmp byte [h_used + bx], 0
        je .skip
        mov ax, bx
        stosw
        mov si, bx
        shl si, 3
        lea si, [h_name + si]
        movsd
        movsd
        inc dl
.skip:  inc bx
        cmp bx, MAXH
        jb .next
        mov [bp + F_AX], dl
        xor ah, ah
        ret
.search:
        push ds
        mov ds, [bp + F_DS]
        mov bx, [bp + F_SI]
        mov ecx, [bx]
        mov edx, [bx + 4]
        pop ds
        mov eax, ecx
        or eax, edx
        mov ah, 0xa1
        jz .done
        xor bx, bx
.look:  cmp byte [h_used + bx], 0
        je .miss
        mov di, bx
        shl di, 3
        cmp [h_name + di], ecx
        jne .miss
        cmp [h_name + di + 4], edx
        jne .miss
        mov [bp + F_DX], bx
        xor ah, ah
        ret
.miss:  inc bx
        cmp bx, MAXH
        jb .look
        mov ah, 0xa0
.done:  ret
.total: cmp al, 2
        ja .sub
        mov word [bp + F_BX], MAXH
        xor ah, ah
        ret
.sub:   mov ah, 0x8f
        ret

f58:    cmp al, 1                       ; mappable physical address array
        ja .sub
        mov word [bp + F_CX], 4
        je .ok
        mov es, [bp + F_ES]
        mov di, [bp + F_DI]
        mov ax, [frame_seg]
        xor cx, cx
.next:  stosw
        xchg ax, cx
        stosw
        xchg ax, cx
        inc cx
        add ax, 0x400
        cmp cx, 4
        jb .next
.ok:    xor ah, ah
        ret
.sub:   mov ah, 0x8f
        ret

f59:    cmp al, 1                       ; hardware information
        ja .sub
        je .raw
        mov es, [bp + F_ES]
        mov di, [bp + F_DI]
        mov ax, 1024                    ; raw page: 16 KiB in paragraphs
        stosw
        xor ax, ax                      ; no alternate register sets
        stosw
        mov ax, 4                       ; context save area
        stosw
        xor ax, ax                      ; no DMA register sets, DMA channel operation 0
        stosw
        stosw
        xor ah, ah
        ret
.raw:   jmp f42
.sub:   mov ah, 0x8f
        ret

f5a:    cmp al, 1                       ; allocate standard or raw pages (zero allowed)
        ja .sub
        mov cx, [bp + F_BX]
        jmp allocate
.sub:   mov ah, 0x8f
        ret

f5b:    cmp al, 8                       ; alternate map register sets: none
        ja .sub
        cmp al, 2
        ja .others
        je .size
        cmp al, 1
        je .set
        mov byte [bp + F_BX], 0         ; 00h: set 0, no save area
        mov word [bp + F_DI], 0
        mov word [bp + F_ES], 0
        xor ah, ah
        ret
.set:   mov ah, 0x9c                    ; 01h: only set 0, from the caller's save area
        cmp byte [bp + F_BX], 0
        jne .done
        mov ax, [bp + F_ES]
        or ax, [bp + F_DI]
        jz .ok
        push ds
        mov ds, [bp + F_ES]
        mov si, [bp + F_DI]
        mov ecx, [si]
        pop ds
        call valid_map
        jc .done
        push ecx
        mov si, sp
        push ds
        push ss
        pop ds
        call load_map
        pop ds
        pop ecx
.ok:    xor ah, ah
.done:  ret
.size:  mov word [bp + F_DX], 4
        xor ah, ah
        ret
.others:
        cmp al, 4                       ; 03h, 05h allocate: none left; 04h, 06h-08h set 0 only
        je .zero
        cmp al, 6
        jae .zero
        mov byte [bp + F_BX], 0
        xor ah, ah
        ret
.zero:  mov ah, 0x9c
        cmp byte [bp + F_BX], 0
        jne .done
        xor ah, ah
        ret
.sub:   mov ah, 0x8f
        ret

f5c:    xor ah, ah                      ; prepare for warm boot
        ret

f5d:    cmp al, 2                       ; operating system functions
        ja .sub
        cmp byte [os_given], 0
        jne .check
        mov byte [os_given], 1
        mov dword [os_key], 0x46455345  ; first call: hand out the key
        mov word [bp + F_BX], 0x4645
        mov word [bp + F_CX], 0x5345
        xor ah, ah
        ret
.check: mov ah, 0xa4
        mov cx, [bp + F_BX]
        cmp cx, [os_key + 2]
        jne .done
        mov cx, [bp + F_CX]
        cmp cx, [os_key]
        jne .done
        xor ah, ah
        cmp al, 2
        jne .done
        mov byte [os_given], 0          ; 02h returns the key
.done:  ret
.sub:   mov ah, 0x8f
        ret

; ---- 57h: move or exchange a memory region ---------------------------------
; The region names each side as conventional memory (type 0, segment:offset)
; or expanded memory (type 1, handle, logical page and an offset below 4000h).
; Expanded memory is reached through frame page 0 (source) and 1
; (destination), which are restored after. Overlapping moves copy in the
; direction that keeps the source and answer 92h; an overlapping exchange
; answers 97h.
f57:    cmp al, 1
        ja .sub
        mov [mv_status], al             ; 0 move, 1 exchange, until the checks
        push ds
        pop es
        mov di, mv_length
        push ds
        mov ds, [bp + F_DS]
        mov si, [bp + F_SI]
        mov cx, 18
        rep movsb
        pop ds
        mov ah, 0x96
        cmp dword [mv_length], 0x100000
        ja .done
        mov bx, mv_src
        call check_side
        jc .done
        mov bx, mv_dst
        call check_side
        jc .done
        mov ax, [cur_map]
        mov [mv_saved], ax
        call overlap                    ; CF overlapping; EAX destination - source
        mov dl, 0
        jnc .go
        cmp byte [mv_status], 1
        jne .move
        mov ah, 0x97                    ; an overlapping exchange
        ret
.move:  mov dl, 0x92
        test eax, eax
        jle .go
        mov byte [mv_status], 2         ; overlapping move to a higher address: backward
.go:    push dx
        call copy
        mov bx, [mv_saved]              ; frame pages 0 and 1 as they were
        mov ah, bl
        mov al, 0
        call set_register
        mov ah, bh
        mov al, 1
        call set_register
        pop dx
        mov ah, dl
        ret
.done:  ret
.sub:   mov ah, 0x8f
        ret

; BX a side: CF and AH set unless it is a valid one for mv_length.
check_side:
        mov al, [bx]
        cmp al, 1
        ja .type
        je .ems
        movzx eax, word [bx + 5]        ; conventional: linear end within 1 MiB
        shl eax, 4
        movzx ecx, word [bx + 3]
        add eax, ecx
        add eax, [mv_length]
        cmp eax, 0x100000
        ja .wrap
        clc
        ret
.ems:   mov dx, [bx + 1]
        call check_handle
        jc .bad
        mov ah, 0x95
        cmp word [bx + 3], 0x3fff
        ja .bad
        mov ecx, [mv_length]            ; last page: page + (offset + length - 1) / 16K
        jecxz .fine
        movzx eax, word [bx + 3]
        add eax, ecx
        dec eax
        shr eax, 14
        movzx ecx, word [bx + 5]
        add eax, ecx
        movzx ecx, byte [h_count + si]
        cmp eax, ecx
        jae .long
.fine:  clc
        ret
.type:  mov ah, 0x98
.bad:   stc
        ret
.wrap:  mov ah, 0xa2
        stc
        ret
.long:  mov ah, 0x93
        stc
        ret

; A side's start as one number: conventional linear, or page * 4000h + offset.
; BX the side: EAX.
side_start:
        cmp byte [bx], 0
        movzx eax, word [bx + 5]
        jne .ems
        shl eax, 4
        jmp .add
.ems:   shl eax, 14
.add:   movzx ecx, word [bx + 3]
        add eax, ecx
        ret

; CF when the sides share memory and overlap; EAX destination start - source start.
overlap:
        mov al, [mv_src]
        cmp al, [mv_dst]
        jne .apart
        test al, al
        jz .same
        mov ax, [mv_src + 1]
        cmp ax, [mv_dst + 1]
        jne .apart
.same:  mov bx, mv_src
        call side_start
        mov edx, eax
        mov bx, mv_dst
        call side_start
        sub eax, edx
        mov ecx, eax
        jns .positive
        neg ecx
.positive:
        cmp ecx, [mv_length]
        jae .apart
        stc
        ret
.apart: xor eax, eax
        clc
        ret

; BX a side, ESI a position in the region, AL the frame page for expanded
; memory: DX:DI the byte's address; CX the bytes from it to the end of its
; 16 KiB window (forward), or from the window's start to it, inclusive
; (backward, mv_status 2).
locate:
        push bx
        cmp byte [bx], 0
        jne .ems
        movzx eax, word [bx + 5]
        shl eax, 4
        movzx ecx, word [bx + 3]
        add eax, ecx
        add eax, esi                    ; linear
        cmp byte [mv_status], 2
        je .cback
        mov edx, eax
        shr edx, 4
        mov di, ax
        and di, 15
        mov cx, 0x4000
        jmp .out
.cback: mov edx, eax
        sub edx, 0x3fff
        jnc .low
        xor edx, edx
.low:   shr edx, 4
        mov ecx, edx
        shl ecx, 4
        sub eax, ecx
        mov di, ax
        lea cx, [di + 1]
        jmp .out
.ems:   push ax
        movzx eax, word [bx + 3]
        add eax, esi
        mov ecx, eax
        shr eax, 14
        add ax, [bx + 5]                ; logical page
        and cx, 0x3fff                  ; offset in it
        push cx
        mov dx, [bx + 1]
        mov si, dx
        mov bx, ax
        call board_page
        pop di
        pop dx                          ; DL the frame page
        mov ah, al
        mov al, dl
        call set_register
        movzx dx, dl
        shl dx, 10
        add dx, [frame_seg]
        mov cx, 0x4000
        sub cx, di
        cmp byte [mv_status], 2
        jne .out
        lea cx, [di + 1]
.out:   pop bx
        ret

; Copies or exchanges mv_length bytes; mv_status 0 forward move, 1 exchange,
; 2 backward move. DS = CS.
copy:
        mov eax, [mv_length]
        mov [mv_left], eax
        xor esi, esi                    ; forward: the next position
.next:  cmp dword [mv_left], 0
        je .done
        mov edx, esi                    ; the chunk's position: forward its first byte,
        cmp byte [mv_status], 2         ; backward its last
        jne .pos
        mov edx, [mv_left]
        dec edx
.pos:   push esi
        push edx
        mov esi, edx
        mov bx, mv_src
        mov al, 0
        call locate
        mov [.src_seg], dx
        mov [.src_off], di
        mov [.count], cx
        pop edx
        push edx
        mov esi, edx
        mov bx, mv_dst
        mov al, 1
        call locate
        mov [.dst_seg], dx
        mov [.dst_off], di
        pop edx
        pop esi
        cmp cx, [.count]                ; the chunk: the shorter window, the bytes left
        jae .fit
        mov [.count], cx
.fit:   movzx ecx, word [.count]
        cmp ecx, [mv_left]
        jbe .sized
        mov ecx, [mv_left]
.sized: sub [mv_left], ecx
        cmp byte [mv_status], 2
        je .moved
        add esi, ecx
.moved: push esi
        push ds
        mov al, [mv_status]
        mov es, [.dst_seg]
        mov di, [.dst_off]
        mov ds, [.src_seg]
        mov si, [cs:.src_off]
        cmp al, 1
        je .swap
        jb .fwd
        std
.fwd:   rep movsb
        cld
        jmp .chunk
.swap:  mov al, [si]
        xchg al, [es:di]
        mov [si], al
        inc si
        inc di
        loop .swap
.chunk: pop ds
        pop esi
        jmp .next
.done:  ret
.src_seg dw 0
.src_off dw 0
.dst_seg dw 0
.dst_off dw 0
.count   dw 0

resident_end:

; ---- Initialisation (discarded) ------------------------------------------

; AL a CMOS index: AL its byte.
cmos:   out 0x70, al
        in al, 0x71
        ret

init:
        push cx
        push dx
        push si
        push di
        push es
        push ds
        mov ax, cs
        mov ds, ax
        mov al, 0x63
        call cmos
        mov [pages], al
        mov [free], al
        mov dx, msg_none
        test al, al
        jz .refuse
        mov al, 0x64
        call cmos
        mov ah, al
        xor al, al
        mov [frame_seg], ax
        mov al, 0x65
        call cmos
        movzx ax, al
        shl ax, 4
        mov [port_base], ax
        ; The frame unmapped, then every board page cleared through frame page 0
        ; with a marker at each end, then every marker checked and cleared.
        xor bx, bx
.unmap: mov al, bl
        mov ah, NONE
        call set_register
        inc bx
        cmp bx, 4
        jb .unmap
        mov es, [frame_seg]
        xor bx, bx
.clear: mov al, 0
        mov ah, bl
        call set_register
        xor di, di
        xor eax, eax
        mov cx, 0x1000
        rep stosd
        call marker
        mov [es:0], eax
        mov [es:0x3ffc], eax
        inc bl
        cmp bl, [pages]
        jb .clear
        xor bx, bx
.check: mov al, 0
        mov ah, bl
        call set_register
        call marker
        mov dx, msg_bad
        cmp [es:0], eax
        jne .refuse
        cmp [es:0x3ffc], eax
        jne .refuse
        xor eax, eax
        mov [es:0], eax
        mov [es:0x3ffc], eax
        inc bl
        cmp bl, [pages]
        jb .check
        mov al, 0
        mov ah, NONE
        call set_register
        ; Handle 0, the operating system's, with no pages.
        mov byte [h_used], 1
        ; INT 67h
        xor ax, ax
        mov es, ax
        cli
        mov word [es:0x67 * 4], int67
        mov [es:0x67 * 4 + 2], cs
        sti
        ; "FESEMS: <n>K of EMS at <frame>h, registers at <port>h"
        movzx ax, byte [pages]
        shl ax, 4
        mov di, msg_size
        call decimal
        mov ax, [frame_seg]
        mov di, msg_frame
        call hex4
        mov ax, [port_base]
        mov di, msg_port
        call hex4
        mov dx, msg_ok
        mov ah, 0x09
        int 0x21
        pop ds
        lds bx, [cs:request]
        mov word [bx + 0x0e], resident_end
        mov [bx + 0x10], cs
        mov word [bx + 3], 0x0100
        jmp .out
.refuse:
        mov ah, 0x09
        int 0x21
        pop ds
        lds bx, [cs:request]
        mov word [bx + 0x0e], 0         ; nothing stays
        mov [bx + 0x10], cs
        mov word [bx + 3], 0x810c       ; error, done: general failure
.out:   pop es
        pop di
        pop si
        pop dx
        pop cx
        ret

; BL a board page: EAX its marker.
marker: movzx eax, bl
        xor eax, 0x5a5e0000
        ret

; AX a number: five digits, leading zeros as spaces, at DS:DI.
decimal:
        add di, 4
        mov cx, 5
        mov bx, 10
.digit: xor dx, dx
        div bx
        add dl, '0'
        mov [di], dl
        dec di
        dec cx
        test ax, ax
        jnz .digit
.pad:   jcxz .done
        mov byte [di], ' '
        dec di
        loop .pad
.done:  ret

; AX: four hex digits at DS:DI.
hex4:   mov cx, 4
.digit: rol ax, 4
        mov dl, al
        and dl, 15
        add dl, '0'
        cmp dl, '9'
        jbe .put
        add dl, 'A' - '9' - 1
.put:   mov [di], dl
        inc di
        loop .digit
        ret

msg_ok:    db 'FESEMS: '
msg_size:  db '00000K of EMS at '
msg_frame: db '0000h, registers at '
msg_port:  db '0000h', 13, 10, '$'
msg_none:  db 'FESEMS: no EMS board (CMOS 63h is 0), not installed', 13, 10, '$'
msg_bad:   db 'FESEMS: board memory test failed, not installed', 13, 10, '$'
