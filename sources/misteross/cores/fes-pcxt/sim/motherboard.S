/* SPDX-License-Identifier: GPL-2.0-or-later
 * Original 8088 ROM. Hardware INTA and timer IRQs come from the real chipset.
 * RAM 0600: AA pass, EE fail. RAM 0601: failing phase. */
.code16
.arch i8086
.section .text
start:
    cli
    cld
    xorw %ax,%ax
    movw %ax,%ds
    movw %ax,%es
    movw %ax,%ss
    movw $0x8000,%sp
    movb $1,0x601
    movw $timer_isr,0x20
    movw $0xf000,0x22
    movw $keyboard_isr,0x24
    movw $0xf000,0x26
    movw $card_isr,0x34
    movw $0xf000,0x36
    movw $0,0x610
    movb $0,0x612
    movw $0,0x614
    movw $0,0x616
    /* RAM top, ROM protection and expansion aperture. */
    movw $0x9000,%ax
    movw %ax,%es
    movb $0x5a,%es:0xffff
    cmpb $0x5a,%es:0xffff
    jne fail
    movw $0xf000,%ax
    movw %ax,%es
    movb %es:0xfff0,%al
    movb $0,%es:0xfff0
    cmpb %es:0xfff0,%al
    jne fail
    movw $0xb800,%ax
    movw %ax,%es
    cmpb $0xff,%es:0
    jne fail
    /* Single, edge triggered 8259, vector base 08, 8086 mode. */
    movb $0x13,%al
    outb %al,$0x20
    movb $8,%al
    outb %al,$0x21
    movb $1,%al
    outb %al,$0x21
    movb $0xff,%al
    outb %al,$0x21
    inb $0x21,%al
    cmpb $0xff,%al
    jne fail
    movb $2,0x601
    /* 100 Hz nominal timer, mode 2; PIT continues while CPU is halted. */
    movb $0x34,%al
    outb %al,$0x43
    movw $11932,%ax
    outb %al,$0x40
    movb %ah,%al
    outb %al,$0x40
    movb $0xfe,%al
    outb %al,$0x21
    sti
wait_timer:
    hlt
    cmpw $3,0x610
    jb wait_timer
    cli
    movb $3,0x601
    /* Mask timer; unmask IRQ5 from the registered ISA probe. */
    movb $0xdf,%al
    outb %al,$0x21
    movw $0x301,%dx
    movb $1,%al
    outb %al,%dx
    sti
    hlt
    cli
    cmpb $1,0x612
    jne fail
    movb $0x0b,%al
    outb %al,$0x20
    inb $0x20,%al
    testb %al,%al
    jne fail
    movb $4,0x601
    /* PPI mode 0; read both switch nibbles and start a PIT2 speaker tone. */
    movb $0x99,%al
    outb %al,$0x63
    movb $0x48,%al
    outb %al,$0x61
    inb $0x62,%al
    andb $0x0f,%al
    cmpb $2,%al
    jne fail
    movb $0xb6,%al
    outb %al,$0x43
    movw $1194,%ax
    outb %al,$0x42
    movb %ah,%al
    outb %al,$0x42
    movb $0x43,%al
    outb %al,$0x61
    inb $0x62,%al
    andb $0x0f,%al
    cmpb $13,%al
    jne fail
    movb $0xfd,%al
    outb %al,$0x21
    sti
wait_keyboard:
    hlt
    cmpw $2,0x614
    jb wait_keyboard
    cli
    cmpw $0x9e1e,0x616
    jne fail
    movb $0x40,%al
    outb %al,$0x61
    movb $0xaa,0x600
stop:
    hlt
    jmp stop
fail:
    movb $0xee,0x600
    cli
    jmp stop
timer_isr:
    pushw %ax
    incw 0x610
    movb $0x20,%al
    outb %al,$0x20
    popw %ax
    iret
card_isr:
    pushw %ax
    pushw %dx
    incb 0x612
    movw $0x301,%dx
    xorb %al,%al
    outb %al,%dx
    movb $0x20,%al
    outb %al,$0x20
    popw %dx
    popw %ax
    iret
keyboard_isr:
    pushw %ax
    pushw %bx
    movw 0x614,%bx
    inb $0x60,%al
    movb %al,0x616(%bx)
    incw 0x614
    inb $0x61,%al
    orb $0x80,%al
    outb %al,$0x61
    andb $0x7f,%al
    outb %al,$0x61
    movb $0x20,%al
    outb %al,$0x20
    popw %bx
    popw %ax
    iret
.org 0xfff0
    ljmp $0xf000,$start
.org 0xffff
    .byte 0xff
