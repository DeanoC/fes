/* SPDX-License-Identifier: GPL-2.0-or-later
 * Original 8088 diagnostic: direct ISA register/VRAM accesses, no BIOS calls.
 */
.code16
.arch i8086
.global _start
_start:
    cli
    cld
    xorw %ax,%ax
    movw %ax,%ss
    movw $0x8000,%sp
    movw %ax,%ds
    movb $1,0x601
    movw $0x3d8,%dx
    outb %al,%dx
    movw $cga_regs,%si
    movw $0x3d4,%dx
    call program_crtc
    movw $0xb800,%ax
    movw %ax,%es
    xorw %di,%di
    movw $0x55aa,%ax
    movw $8192,%cx
    rep stosw
    cmpw %ax,%es:0x3ffe
    jne fail
    cmpw %ax,%es:0x7ffe  /* second aperture mirror */
    jne fail
    movw $0x3d9,%dx
    movb $15,%al
    outb %al,%dx
    decw %dx
    movb $0x1a,%al
    outb %al,%dx
    movb $2,0x601
    movw $0x3b8,%dx
    movb $1,%al
    outb %al,%dx
    movw $mda_regs,%si
    movw $0x3b4,%dx
    call program_crtc
    movw $0xb000,%ax
    movw %ax,%es
    xorw %di,%di
    movw $0x0fc4,%ax
    movw $2048,%cx
    rep stosw
    cmpw %ax,%es:0x0ffe
    jne fail
    cmpw %ax,%es:0x7ffe
    jne fail
    movw $0x3b8,%dx
    movb $9,%al
    outb %al,%dx
    movb $3,0x601
    /* Real status polling; no host status interception. */
    movw $0x3da,%dx
wait_vblank_end:
    inb %dx,%al
    testb $8,%al
    jnz wait_vblank_end
wait_vblank:
    inb %dx,%al
    testb $8,%al
    jz wait_vblank
    movw $0x3ba,%dx
wait_hblank_end:
    inb %dx,%al
    testb $1,%al
    jnz wait_hblank_end
wait_hblank:
    inb %dx,%al
    testb $1,%al
    jz wait_hblank
    movb $0xaa,0x600
stop:
    hlt
    jmp stop
fail:
    movb $0xee,0x600
    jmp stop
program_crtc:
    xorw %bx,%bx
    movw $16,%cx
reg_loop:
    movb %bl,%al
    movb %cs:(%si),%ah
    outw %ax,%dx  /* 8088 splits index/data into adjacent ISA byte cycles */
    incw %si
    incw %bx
    loop reg_loop
    ret
cga_regs:
    .byte 56,40,45,10,127,6,100,112,2,1,0x20,7,0,0,0,0
mda_regs:
    .byte 97,80,82,15,25,6,25,25,2,13,0x20,12,0,0,0,0
.org 0xfff0
    ljmp $0xf000,$_start
.org 0x10000
