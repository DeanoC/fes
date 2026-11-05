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
    /* Keep both reset-blanked adapters disabled until real SDRAM/DMA checks
     * pass. A failed or stalled cold boot must not display the success pattern. */
    call storage_dma
    xorb %al,%al
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
    /* Visible completion signature, only after both adapters and status polls
     * pass: a solid 16-dot by 8-line block at the CGA viewport's top left.
     * The surrounding 55AA stripes deliberately remain static after HLT. */
    movw $0xb800,%ax
    movw %ax,%es
    movw $0xffff,%ax
    movw %ax,%es:0
    movw %ax,%es:0x2000
    movw %ax,%es:80
    movw %ax,%es:0x2050
    movw %ax,%es:160
    movw %ax,%es:0x20a0
    movw %ax,%es:240
    movw %ax,%es:0x20f0
    movb $0xaa,0x600
    /* Only the media-enabled board claims the FDC.  Other board profiles
     * retain the original halted diagnostic. */
    movw $0x3f4,%dx
    inb %dx,%al
    cmpb $0x80,%al
    jne stop
    call floppy_diagnostic
stop:
    hlt
    jmp stop

/* Exercise the real guest FDC, DMA2 and SDRAM-backed floppy after a live
 * insertion. The first sector of the diagnostic image is the 512-byte
 * sequence (11 + 37 * byte_index) modulo 256. A matching read paints a
 * second solid block in the CGA viewport; an empty or different disk leaves
 * the original completion pattern in place. The rest of the image is not
 * interpreted. This routine is diagnostic, not a DOS firmware path. */
floppy_diagnostic:
    movw $0x3f2,%dx
    movb $0x08,%al
    outb %al,%dx
    movb $0x1c,%al
    outb %al,%dx
    movw $0x3f5,%dx
    movb $0x03,%al         /* Specify, DMA mode */
    outb %al,%dx
    movb $0xdf,%al
    outb %al,%dx
    movb $0x02,%al
    outb %al,%dx
floppy_wait:
    movb $0x04,%al         /* Sense drive status */
    outb %al,%dx
    xorb %al,%al
    outb %al,%dx
    inb %dx,%al
    testb $0x20,%al        /* Motor spun up and medium committed */
    jz floppy_clear
    /* Poison the destination with the inverse expected bytes. A stale or
     * partial DMA transfer can never reuse a preceding successful sector. */
    movw $0x7000,%bx
    movw $512,%cx
    movb $11,%al
floppy_poison:
    notb %al
    movb %al,(%bx)
    notb %al
    addb $37,%al
    incw %bx
    loop floppy_poison
    /* DMA channel 2, peripheral -> 0000:7000, one 512-byte sector. */
    movb $6,%al
    outb %al,$0x0a
    xorb %al,%al
    outb %al,$0x0c
    outb %al,$0x04
    movb $0x70,%al
    outb %al,$0x04
    movb $0xff,%al
    outb %al,$0x05
    movb $1,%al
    outb %al,$0x05
    xorb %al,%al
    outb %al,$0x81
    movb $0x46,%al
    outb %al,$0x0b
    movb $2,%al
    outb %al,$0x0a
    movb $0x46,%al         /* READ DATA, MFM, C0/H0/R1/N2 */
    outb %al,%dx
    xorb %al,%al
    outb %al,%dx
    outb %al,%dx
    outb %al,%dx
    movb $1,%al
    outb %al,%dx
    movb $2,%al
    outb %al,%dx
    movb $9,%al
    outb %al,%dx
    movb $0x2a,%al
    outb %al,%dx
    movb $0xff,%al
    outb %al,%dx
    decw %dx                /* Wait for the seven-byte result phase */
floppy_result_wait:
    inb %dx,%al
    andb $0xd0,%al
    cmpb $0xd0,%al
    jne floppy_result_wait
    incw %dx
    inb %dx,%al             /* ST0 */
    testb $0xc0,%al
    jnz floppy_failed
    inb %dx,%al             /* ST1 */
    testb %al,%al
    jnz floppy_failed_6
    inb %dx,%al             /* ST2 */
    testb %al,%al
    jnz floppy_failed_5
    movb $4,%cl            /* C, H, R and N */
floppy_consume_result:
    inb %dx,%al
    decb %cl
    jnz floppy_consume_result
    movw $0x7000,%bx
    movw $512,%cx
    movb $11,%al
floppy_compare:
    cmpb %al,(%bx)
    jne floppy_clear
    addb $37,%al
    incw %bx
    loop floppy_compare
    /* 64 by 32 white pixels at x=64, distinct from the original marker. */
    movw $0xffff,%ax
    call floppy_mark
    jmp floppy_wait
floppy_clear:
    movw $0x55aa,%ax
    call floppy_mark
    jmp floppy_wait
floppy_mark:
    pushw %ax
    movw $0xb800,%ax
    movw %ax,%es
    popw %ax
    movw $8,%di
    movw $16,%bx
floppy_mark_row:
    movw $4,%cx
    rep stosw
    subw $8,%di
    addw $0x2000,%di
    movw $4,%cx
    rep stosw
    subw $0x2008,%di
    addw $80,%di
    decw %bx
    jnz floppy_mark_row
    ret
floppy_failed:
    movb $6,%cl
    jmp floppy_drain
floppy_failed_6:
    movb $5,%cl
    jmp floppy_drain
floppy_failed_5:
    movb $4,%cl
floppy_drain:
    inb %dx,%al
    decb %cl
    jnz floppy_drain
    jmp floppy_clear
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
storage_dma:
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
    /* The FDC and the synthetic ISA probe both own DMA2, so the assembled
     * machine selects only one. Keep refresh enabled in either profile, but
     * exercise probe transfers only when the FDC port is unclaimed. */
    movw $0x3f4,%dx
    inb %dx,%al
    cmpb $0x80,%al
    je storage_done
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
    /* Conventional RAM -> ISA probe, four distinct bytes. */
    movw $0x3231,0x4000
    movw $0x3433,0x4002
    movb $6,%al
    outb %al,$0x0a
    xorb %al,%al
    outb %al,$0x0c
    outb %al,$0x04
    movb $0x40,%al
    outb %al,$0x04
    movb $3,%al
    outb %al,$0x05
    xorb %al,%al
    outb %al,$0x05
    movb $0x8a,%al
    outb %al,$0x0b
    movb $2,%al
    outb %al,$0x0a
    movw $0x301,%dx
    movb $2,%al
    outb %al,%dx
    xorb %al,%al
    outb %al,%dx
    movw $0x305,%dx
    inb %dx,%al
    cmpb $0x34,%al
    jne fail
    incw %dx
    inb %dx,%al
    cmpb $12,%al
    jne fail
    incw %dx
    inb %dx,%al
    cmpb $2,%al
    jne fail
storage_done:
    ret
.org 0xfff0
    ljmp $0xf000,$_start
.org 0x10000
