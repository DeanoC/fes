; Read one documented 86Box v6.0 chipset configuration register and restore
; its index latch. Build with INDEX_PORT, DATA_PORT and REGISTER defined.
bits 16
org 0x100
    in al, INDEX_PORT
    mov cl, al
    mov al, REGISTER
    out INDEX_PORT, al
    in al, DATA_PORT
    mov bl, al
    mov al, cl
    out INDEX_PORT, al
    mov al, bl
    shr al, 4
    call hex_digit
    mov [result + 4], al
    mov al, bl
    and al, 15
    call hex_digit
    mov [result + 5], al
    mov dx, result
    mov ah, 9
    int 0x21
    mov ax, 0x4c00
    int 0x21
hex_digit:
    add al, '0'
    cmp al, '9'
    jbe .done
    add al, 7
.done:
    ret
result db 'REG=00', 13, 10, '$'
