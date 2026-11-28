# FES SH-4 integer RTL

The first slice is an independently written, single-issue SH-4 integer core
targeting the DE25-Nano's Agilex 5. It separates fetch, decode and operand
capture, execution, and retirement with registers. Only one instruction is in
flight. With the diagnostic memory, register instructions retire in five
clocks. The bus operates on aligned 32-bit words, which allows a later cache
front end to use synchronous block RAM without a combinational memory read in
the CPU.

The [Renesas SH-4 software manual](https://www.renesas.com/en/document/mas/sh-4-software-manual?r=1055166)
defines instruction encodings and behavior. The implemented slice includes:

- Register and immediate MOV and ADD, SUB, ADDC, SUBC, AND, OR, XOR, NOT, TST,
  integer compares, single-bit logical and arithmetic shifts, and T-bit access.
- Byte, word, and longword MOV through a register, including predecrement
  stores and postincrement loads, base-plus-displacement forms, PC-relative
  literal loads, and MOVA.
- BT, BF, BT/S, BF/S, BRA, BSR, JMP, JSR, RTS, and NOP. The branch decision and
  target are captured before the delay slot executes.

FPU, remaining integer instructions, privileged state, exception entry,
interrupts, caches, MMU and board buses are later work. The core exposes sticky
diagnostic faults until reset. Fault codes 1 through 4 indicate an unsupported
opcode, a prohibited delay-slot instruction, an unaligned fetch, and an
unaligned data access. `fault_pc` identifies the offending instruction address,
or the attempted fetch address for an unaligned fetch. General registers have
no defined reset values. Software must initialize them before reading them.

## Memory contract

`mem_valid && mem_ready` accepts one aligned byte-addressed request.
`mem_wstrb == 0` selects a read. A write completes when accepted, with one strobe
per little-endian byte lane. A read returns exactly one `mem_rvalid` pulse at
least one cycle after acceptance. The core holds its request while stalled and
has at most one outstanding read. The surrounding memory interface must flush
any outstanding read on reset. The core presents architectural addresses as
issued, including the default reset address `0xa0000000`.

The diagnostic top resets to address zero and exposes a host-side word loader
while reset is asserted. It uses four synchronous byte banks for a 2 KiB
unified memory. In this top, out-of-range reads return zero and out-of-range
stores are discarded.

## Validation

From the repository root:

```sh
make -C sources/misteross sim-fes-sh4
make -C sources/misteross/cores/fes-common/rtl/sh4 fit QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus
```

The Verilator test runs hand-encoded integer code against the synchronous RAM
and a memory model that stalls requests and delays read responses. It also
checks fault retirement and restart. The Quartus Pro build uses the shared
builder lease and leaves reports under `out/sh4/fit/output_files`. It targets
the DE25-Nano part at 200 MHz. The clock is the only physical port in the
virtual-pin project. The fit and timing reports cover internal logic and RAM.
Pinout, programming and on-board behavior require a board-level design.
