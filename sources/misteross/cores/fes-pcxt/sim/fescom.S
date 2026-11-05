/* SPDX-License-Identifier: MIT */
/* An 8086 DOS COM executable: prove DOS loading, keyboard and return. */
.code16
.section .text
.global _start
_start:
    mov $ready, %dx
    mov $0x09, %ah
    int $0x21
    mov $0x01, %ah
    int $0x21
    cmp $'k', %al
    jne failed
    mov $passed, %dx
    mov $0x09, %ah
    int $0x21
    mov $0x4c00, %ax
    int $0x21
failed:
    mov $failure, %dx
    mov $0x09, %ah
    int $0x21
    mov $0x4c01, %ax
    int $0x21
ready:   .ascii "\r\nFES COM READY (PRESS K)\r\n$"
passed:  .ascii "\r\nFES COM PASS\r\n$"
failure: .ascii "\r\nFES COM FAIL\r\n$"
