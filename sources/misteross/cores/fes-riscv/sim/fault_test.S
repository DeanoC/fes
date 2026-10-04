# Bus-fault contract test for fes_riscv_system. Every access outside the
# mapped RAM, framebuffer and ten I/O words must fault; the handler counts
# and skips it. Results are left in mtimecmp: low = faults taken, high = the
# identification word read from the I/O block. Also exercises a CSR address
# above 0x7ff (the assembler regression).
.equ MMIO,  0x20000000
.equ COUNT, 0x7f00

_start:
    j start
.org 0x40
trap:
    li t1, COUNT
    lw t0, 0(t1)
    addi t0, t0, 1
    sw t0, 0(t1)
    csrr t0, mepc
    addi t0, t0, 4
    csrw mepc, t0
    mret
start:
    la t0, trap
    csrw mtvec, t0
    li s0, COUNT
    sw zero, 0(s0)
    csrr s3, cycle                 # CSR 0xc00
    li t0, 0x20000028
    lw t1, 0(t0)                   # fault 1: first unused I/O word
    li t0, 0x2000003c
    lw t1, 0(t0)                   # fault 2: last unused I/O word
    li t0, 0x20000040
    sw zero, 0(t0)                 # fault 3: next 64-byte block
    li t0, 0x20000028
    sw zero, 0(t0)                 # fault 4: write to an unused word
    li t0, 0x10004b00
    lw t1, 0(t0)                   # fault 5: first word past the framebuffer
    li t0, 0x30000000
    lw t1, 0(t0)                   # fault 6: nothing mapped
    li t0, 0x20000020
    lw s1, 0(t0)                   # identification: must not fault
    li t0, 0x10004afc
    lw t1, 0(t0)                   # last framebuffer word: must not fault
    li t0, 0x00007ffc
    lw t1, 0(t0)                   # last RAM word: must not fault
    lw s2, 0(s0)                   # faults taken
    li t0, MMIO
    sw s1, 0x14(t0)
    sw s2, 0x10(t0)
done:
    j done
