/* SPDX-License-Identifier: GPL-2.0-or-later
 * Original workloads for measuring EU throughput against the unchanged guest
 * pin clock. Port 80h marks intervals; these are measurements, not 8088 timing
 * conformance limits. Every workload also checks its architectural result.
 */
.code16
.arch i8086
.section .text
start:
    cli
    cld
    xorw %ax, %ax
    movw %ax, %ds
    movw %ax, %es
    movw %ax, %ss
    movw $0x8000, %sp
    movb $0x50, %al
    outb %al, $0x80
    .rept 128
    nop
    .endr
    movb $0x51, %al
    outb %al, $0x80

    movb $0x52, %al
    outb %al, $0x80
    xorw %dx, %dx
    movw $1, %cx
    .rept 64
    addw %cx, %dx
    .endr
    stc
    .rept 32
    adcw $0xffff, %dx
    .endr
    jnc fail
    cmpw $64, %dx
    jne fail
    movb $0x53, %al
    outb %al, $0x80

    movb $0x54, %al
    outb %al, $0x80
    xorw %dx, %dx
    movw $128, %cx
again:
    incw %dx
    loop again
    cmpw $128, %dx
    jne fail
    movb $0x55, %al
    outb %al, $0x80

    movw $0x1000, %di
    movw $0xa55a, %ax
    movw $256, %cx
    rep stosw
    movw $0x1000, %si
    movw $0x2000, %di
    movw $256, %cx
    movb $0x56, %al
    outb %al, $0x80
    rep movsw
    movb $0x57, %al
    outb %al, $0x80
    cmpw $0xa55a, 0x2000
    jne fail
    cmpw $0xa55a, 0x21fe
    jne fail
    movb $0xaa, %al
    outb %al, $0x80
    hlt
fail:
    movb $0x0e, 0x201
    movb $0xee, %al
    outb %al, $0x80
    hlt
.org 0xfff0
    ljmp $0xf000, $start
.org 65535
    .byte 0xff
