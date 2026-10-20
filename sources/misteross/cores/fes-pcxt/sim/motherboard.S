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
    /* DMA0 refresh: auto-init single memory reads, PIT1 requests each 18
     * timer clocks. This continues throughout the interrupt diagnostic. */
    xorb %al,%al
    outb %al,$0x0d
    outb %al,$0x0c
    outb %al,$0x00
    movb $0x50,%al
    outb %al,$0x00
    movb $0xff,%al
    outb %al,$0x01
    outb %al,$0x01
    movb $0x58,%al
    outb %al,$0x0b
    xorb %al,%al
    outb %al,$0x0a
    movb $0x74,%al
    outb %al,$0x43
    movb $18,%al
    outb %al,$0x41
    xorb %al,%al
    outb %al,$0x41
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
    /* PPI mode 0; read both switch nibbles and start a PIT2 speaker tone.
     * The testbench preloads the expected high nibble at 0620 and the low
     * nibble at 0621. Observed nibbles are stored at 0619 and 0618. */
    movb $0x99,%al
    outb %al,$0x63
    movb $0x48,%al
    outb %al,$0x61
    inb $0x62,%al
    andb $0x0f,%al
    movb %al,0x619
    cmpb 0x620,%al
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
    movb %al,0x618
    cmpb 0x621,%al
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
    movb $5,0x601
    /* ISA probe -> conventional RAM, eight-byte block transfer. */
    movb $6,%al
    outb %al,$0x0a
    xorb %al,%al
    outb %al,$0x0c
    outb %al,$0x04
    movb $0x30,%al
    outb %al,$0x04
    movb $7,%al
    outb %al,$0x05
    xorb %al,%al
    outb %al,$0x05
    movb $0x86,%al
    outb %al,$0x0b
    movb $2,%al
    outb %al,$0x0a
    movw $0x305,%dx
    movb $0x6d,%al
    outb %al,%dx
    movw $0x301,%dx
    movb $2,%al
    outb %al,%dx
    movw $0x3000,%bx
    movw $8,%cx
check_dma_ram:
    cmpb $0x6d,(%bx)
    jne fail
    incw %bx
    loop check_dma_ram
    xorb %al,%al
    outb %al,%dx
    inb $8,%al
    testb $4,%al
    jz fail
    movw $0x307,%dx
    inb %dx,%al
    cmpb $1,%al
    jne fail
    /* Conventional RAM -> second ISA probe, four distinct bytes. */
    movw $0x3231,0x4000
    movw $0x3433,0x4002
    movb $7,%al
    outb %al,$0x0a
    xorb %al,%al
    outb %al,$0x0c
    outb %al,$0x06
    movb $0x40,%al
    outb %al,$0x06
    movb $3,%al
    outb %al,$0x07
    xorb %al,%al
    outb %al,$0x07
    movb $0x8b,%al
    outb %al,$0x0b
    movb $3,%al
    outb %al,$0x0a
    movw $0x309,%dx
    movb $2,%al
    outb %al,%dx
    xorb %al,%al
    outb %al,%dx
    movw $0x30d,%dx
    inb %dx,%al
    cmpb $0x34,%al
    jne fail
    incw %dx
    inb %dx,%al
    cmpb $4,%al
    jne fail
    incw %dx
    inb %dx,%al
    cmpb $1,%al
    jne fail
    /* Refresh requests compete with real LOCK-prefixed memory operations.
     * No refresh may split either locked word RMW or interrupt acknowledge. */
    movw $0,0x5100
    movw $40,%cx
locked_refresh:
    lock incw 0x5100
    loop locked_refresh
    cmpw $40,0x5100
    jne fail
    movb $4,%al
    outb %al,$0x0a
    xorb %al,%al
    outb %al,$0x0c
    inb $1,%al
    movb %al,0x624
    inb $1,%al
    movb %al,0x625
    cmpw $0xffff,0x624
    je fail
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
