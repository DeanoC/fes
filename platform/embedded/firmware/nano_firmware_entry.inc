/* SPDX-License-Identifier: BSD-3-Clause */
/* First instructions of the selected cold entry. No stack, C call, BSS or
 * candidate-memory access. Only the initialized DDR latch is written here.
 * Test harnesses may substitute register-file addresses and the reject label;
 * the producer installs the canonical addresses and a permanent park loop. */
#ifndef FES_RAM_REJECT
#define FES_RAM_REJECT .Lfes_ram_park
#endif
    mrs x8, CurrentEL
    cmp x8, #12
    b.ne FES_RAM_REJECT
    mrs x8, sctlr_el3
    ldr x9, =0x03000005
    tst x8, x9
    b.ne FES_RAM_REJECT
    mrs x8, daif
    cmp x8, #0x3c0
    b.ne FES_RAM_REJECT
    ldr x8, =fes_nano_ram_attempt
    ldr x9, [x8]
    cbnz x9, FES_RAM_REJECT
    mov x9, #1
    str x9, [x8]
    dsb sy
    ldr x8, =FES_RAM_COOKIE
    cmp x4, x8
    b.ne FES_RAM_REJECT
    ldr x8, =FES_RAM_COLD_COOKIE
    cmp x3, x8
    b.ne FES_RAM_REJECT
    cbnz x1, FES_RAM_REJECT
    tst x0, #7
    b.ne FES_RAM_REJECT
    ldr x8, =0xbff00000
    cmp x0, x8
    b.lo FES_RAM_REJECT
    ldr x8, =0xbfffffc0
    cmp x0, x8
    b.hi FES_RAM_REJECT
    mov x8, sp
    tst x8, #15
    b.ne FES_RAM_REJECT
    ldr x9, =0x61000
    cmp x8, x9
    b.lo FES_RAM_REJECT
    ldr x9, =0x71000
    cmp x8, x9
    b.hs FES_RAM_REJECT
    tst x2, #7
    b.ne FES_RAM_REJECT
    cmp x2, #0x1000
    b.lo FES_RAM_REJECT
    ldr x8, =0x3ff20
    cmp x2, x8
    b.hi FES_RAM_REJECT
    ldr w8, [x2]
    ldr w9, =FES_RAM_COLD_MAGIC
    cmp w8, w9
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #4]
    cmp w8, #FES_RAM_COLD_VERSION
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #8]
    cmp w8, #FES_RAM_COLD_BYTES
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #204]
    cmp w8, #FES_RAM_COLD_FLAGS
    b.ne FES_RAM_REJECT
    mrs x8, mpidr_el1
    ldr x9, =0xff00ffffff
    and x8, x8, x9
    cbz x8, .Lfes_ram_core
    cmp x8, #0x200
    b.ne FES_RAM_REJECT
.Lfes_ram_core:
    lsr x8, x8, #8
    ldr w9, [x2, #200]
    cmp x8, x9
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #16]
    ldr w9, =FES_RAM_BOOT_MAGIC
    cmp w8, w9
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #20]
    cmp w8, #FES_RAM_BOOT_VERSION
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #24]
    cmp w8, #FES_RAM_BOOT_BYTES
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #28]
    cbnz w8, FES_RAM_REJECT
    ldr w8, [x2, #80]
    cmp w8, #FES_RAM_HISTORY_NEVER_ENABLED
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #84]
    cmp w8, #FES_RAM_FABRIC_ABSENT
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #88]
    cmp w8, #FES_RAM_PRODUCERS_ALL
    b.ne FES_RAM_REJECT
    ldr w8, [x2, #92]
    cbnz w8, FES_RAM_REJECT

    /* Exact existing cold-record CRC32, including a zero checksum field. */
    mov x10, #0
    mov w14, #-1
    ldr w16, =0xedb88320
.Lfes_ram_crc_byte:
    mov w12, #0
    cmp x10, #12
    b.lo .Lfes_ram_crc_read
    cmp x10, #16
    b.lo .Lfes_ram_crc_zero
.Lfes_ram_crc_read:
    ldrb w12, [x2, x10]
.Lfes_ram_crc_zero:
    eor w14, w14, w12
    mov w13, #8
.Lfes_ram_crc_bit:
    and w17, w14, #1
    neg w17, w17
    and w17, w17, w16
    eor w14, w17, w14, lsr #1
    subs w13, w13, #1
    b.ne .Lfes_ram_crc_bit
    add x10, x10, #1
    cmp x10, #FES_RAM_COLD_BYTES
    b.lo .Lfes_ram_crc_byte
    mvn w14, w14
    ldr w15, [x2, #12]
    cmp w14, w15
    b.ne FES_RAM_REJECT
    ldr x8, =fes_nano_ram_expected_chain
    ldp x9, x10, [x8]
    ldp x11, x12, [x2, #48]
    cmp x9, x11
    b.ne FES_RAM_REJECT
    cmp x10, x12
    b.ne FES_RAM_REJECT
    ldp x9, x10, [x8, #16]
    ldp x11, x12, [x2, #64]
    cmp x9, x11
    b.ne FES_RAM_REJECT
    cmp x10, x12
    b.ne FES_RAM_REJECT
    ldr x8, [x2, #40]
    cbz x8, FES_RAM_REJECT
    tbz x8, #0, FES_RAM_REJECT
    ldr x9, =FES_RAM_OWNER_SEED
    eor x9, x9, x8
    orr x9, x9, #1
    ldr x8, [x2, #32]
    cmp x8, x9
    b.ne FES_RAM_REJECT

    /* The loaded ELF must describe exactly this separate stack/BSS range. */
    ldr x8, =__NOBITS_START__
    ldr x9, =0x50000
    cmp x8, x9
    b.ne FES_RAM_REJECT
    ldr x8, =__STACKS_START__
    cmp x8, x9
    b.ne FES_RAM_REJECT
    ldr x8, =__STACKS_END__
    ldr x9, =0x58000
    cmp x8, x9
    b.ne FES_RAM_REJECT
    ldr x8, =__BSS_START__
    cmp x8, x9
    b.ne FES_RAM_REJECT
    ldr x10, =__BSS_END__
    cmp x10, x8
    b.ls FES_RAM_REJECT
    ldr x11, =__NOBITS_END__
    cmp x10, x11
    b.hi FES_RAM_REJECT
    ldr x9, =0x60000
    cmp x11, x9
    b.hi FES_RAM_REJECT

    /* Read-only live containment; never touch a reset controller's data
     * registers or release a bridge/reset. The full C provider validates
     * the saved history and repeats the live policy before any service grant. */
    dsb sy
    ldr x8, =FES_RAM_RSTMGR
    ldr w9, [x8, #0x24]
    ldr w10, =FES_RAM_MASTER_RESET
    and w9, w9, w10
    cmp w9, w10
    b.ne FES_RAM_REJECT
    ldr w9, [x8, #0x2c]
    and w9, w9, #15
    cmp w9, #15
    b.ne FES_RAM_REJECT
    ldr w17, [x2, #200]
    mov w10, #1
    lsl w11, w10, w17
    eor w11, w11, #15
    ldr w9, [x8, #0x90]
    and w9, w9, w11
    cmp w9, w11
    b.ne FES_RAM_REJECT
    ldr w9, [x8, #0x94]
    tst w9, #15
    b.ne FES_RAM_REJECT
    add w11, w17, #1
    lsl w11, w10, w11
    ldr x8, =FES_RAM_PWRMGR
    ldr w9, [x8, #0x50]
    and w9, w9, #0x1e
    cmp w9, w11
    b.ne FES_RAM_REJECT
    ldr w9, [x8, #0x54]
    and w9, w9, #0x1e
    cmp w9, w11
    b.ne FES_RAM_REJECT
    ldr w9, [x8, #0x58]
    ubfx w9, w9, #10, #2
    cmp w9, w17
    b.ne FES_RAM_REJECT
    mov x10, #0
.Lfes_ram_spares:
    cmp w10, w17
    b.eq .Lfes_ram_next_core
    add x11, x8, #0x1c
    add x11, x11, x10, lsl #3
    ldr w9, [x11]
    cbnz w9, FES_RAM_REJECT
.Lfes_ram_next_core:
    add x10, x10, #1
    cmp x10, #4
    b.lo .Lfes_ram_spares
    dsb sy
    b .Lfes_ram_checked
.Lfes_ram_park:
    wfe
    b .Lfes_ram_park
.Lfes_ram_checked:
