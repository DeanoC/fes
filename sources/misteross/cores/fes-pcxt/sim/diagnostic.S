/* SPDX-License-Identifier: GPL-2.0-or-later
 * Original 8088 bring-up ROM. GNU as rejects later ISA instructions.
 * Port 80h is a testbench-only progress sink, not an XT motherboard device.
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
    movb $0, 0x500
    movb $0, 0x501
    pushf
    popw %ax
    movw %ax, 0x200
    andw $0xf000, %ax
    cmpw $0xf000, %ax
    jne fail_flags
    movw $0x1234, %ax
    movw $0x0011, %bx
    mulw %bx
    cmpw $0x3574, %ax
    jne fail_alu
    cmpw $1, %dx
    jne fail_alu
    divw %bx
    cmpw $0x1234, %ax
    jne fail_alu
    orw %dx, %dx
    jne fail_alu
    call subroutine
    cmpw $0x1235, %ax
    jne fail_alu

    movw $0x300, %di
    movw $0xa55a, %ax
    movw $8, %cx
    rep stosw
    movw $0x300, %si
    movw $0x401, %di
    movw $8, %cx
    rep movsw
    movw 0x40f, %ax
    cmpw $0xa55a, %ax
    jne fail_memory
    /* Word at offset FFFF wraps the second byte within the segment. */
    movw $0x7654, 0xffff
    cmpb $0x54, 0xffff
    jne fail_memory
    cmpb $0x76, 0
    jne fail_memory
    /* Physical segment:offset addition also wraps at 20 bits. */
    movw $0xffff, %ax
    movw %ax, %es
    movb $0x91, %es:0x10
    cmpb $0x91, 0
    jne fail_memory
    xorw %ax, %ax
    movw %ax, %es
    movw $0x400, %bx
    lock incw (%bx)

    movw $0x304, %dx
    movb $160, %al
    outb %al, %dx
    movw $0x300, %dx
    movw $0x035a, %ax
    outw %ax, %dx
    inw %dx, %ax
    cmpw $0x035a, %ax
    jne fail_isa
    movw $0x700, %dx
    inb %dx, %al
    cmpb $0x5a, %al
    jne fail_isa
    movw $0x302, %dx
    inb %dx, %al
    cmpb $3, %al
    jne fail_isa
    incw %dx
    inb %dx, %al
    cmpb $3, %al
    jne fail_isa
    movw $0xc800, %ax
    movw %ax, %es
    movb $0x7e, %es:0
    movb %es:0, %al
    cmpb $0x7e, %al
    jne fail_isa
    movw $0x308, %dx
    movb $0x43, %al
    outb %al, %dx
    inb %dx, %al
    cmpb $0x43, %al
    jne fail_isa
    movw $0x2f0, %dx
    inb %dx, %al
    cmpb $0xff, %al
    jne fail_isa
    /* Word I/O at FFFF must wrap the second port to 0000. */
    movw $0xffff, %dx
    movw $0x9876, %ax
    outw %ax, %dx

    movw $irq_handler, 0x180
    movw $0xf000, 0x182
    movw $nmi_handler, 8
    movw $0xf000, 10
    int $0x60
    cmpb $1, 0x500
    jne fail_interrupt
    /* Bench raises INTR after HLT; it supplies vector 60h on two INTA cycles. */
    movb $0x40, %al
    outb %al, $0x80
    sti
    hlt
    cli
    cmpb $2, 0x500
    jne fail_interrupt
    movb $0x41, %al
    outb %al, $0x80
    hlt
    cmpb $1, 0x501
    jne fail_interrupt
    movb $0xaa, %al
    outb %al, $0x80
stop:
    cli
    hlt
    jmp stop
subroutine:
    incw %ax
    ret
irq_handler:
    incb 0x500
    iret
nmi_handler:
    incb 0x501
    iret
fail_flags:
    movb $1, %al
    jmp fail
fail_alu:
    movb $2, %al
    jmp fail
fail_memory:
    movb $3, %al
    jmp fail
fail_isa:
    movb $4, %al
    jmp fail
fail_interrupt:
    movb $5, %al
fail:
    movb %al, 0x201
    movb $0xee, %al
    outb %al, $0x80
    jmp stop
.org 0xfff0, 0xff
    ljmp $0xf000, $start
.org 0x10000, 0xff
