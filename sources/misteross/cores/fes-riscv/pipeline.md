# Pipelining fes_rv32_cpu

A design note, not an implementation. It says how to overlap fetch with
execute on this CPU, what that does and does not change, and why it is not
the way to buy margin at 74.25 MHz. The measured comparison is [qor.md](qor.md). Written against the CPU at
`8f5c5b598` and rebased onto main, where `fes_rv32_alu` still assigns
`sum` from `a + b` or `a - b`.

## What is slow

The core is multicycle, not short of megahertz. With a one-cycle memory an
ALU, branch or jump takes three clocks and a load or store takes five. On
the DE10-Nano harness that execute cone routed at 79.7 MHz, in the same
band as the pipelined cores. VexRiscv Min was half the logic at 82.7 MHz
and retires an ALU instruction about every cycle. The gap is cycles per
instruction.

Those three clocks are visible in `fes_rv32_cpu`:

1. `S_FETCH` issues the instruction read and holds the bus until `bus_ready`.
2. The ready cycle captures `ir` and the decode flags, and addresses the
   register file. `rs1` and `rs2` are valid the following cycle, because the
   file is a synchronous read.
3. `S_EXEC` uses those operands. The next fetch is not issued until this
   instruction finishes, and a load or store then spends `S_MEM` on the same
   bus.

The bus is one request, held until ready. The system asserts ready the cycle
after issue, so a request occupies two cycles and the next issue cannot
start until the cycle after ready. Fetch throughput on that bus is one
instruction per two cycles even if execute were free. A pipeline that keeps
this bus as the only memory port cannot reach one ALU instruction per cycle.

## The port that is already there

`fes_riscv_lane_ram` is a true dual-port M10K. The 32 KiB firmware RAM uses
port A for the CPU and ties port B to zero. The raster uses port B of the
framebuffer, not of the firmware RAM. Instruction fetch can move to port B
without a new memory and without touching the HDMI read.

Port B is a synchronous read: address this cycle, data the next. It does
not need the data-bus handshake. The data bus stays the path for loads,
stores, the framebuffer and the I/O block, including `bus_error`.

## The pipeline

Three stages. Not five. A longer pipeline would not move the adder off the
critical path, and VexRiscv Min is small because its pipeline is short.

| Stage | What it does | Holds |
| --- | --- | --- |
| F | Presents `fetch_pc` to RAM port B | the PC being fetched |
| D | Captures the instruction and the decode flags. Addresses the register file | `ir`, the `is_*` flags, the PC of that instruction |
| X | ALU, branch, jump, CSR, or a data-bus issue. Writeback. Trap or redirect | `rs1`, `rs2`, the D registers |

Schedule for an ALU instruction, no stall:

| Cycle | F | D | X |
| --- | --- | --- | --- |
| T | address of n | | |
| T+1 | address of n+1 | instruction n, regfile addressed | |
| T+2 | address of n+2 | instruction n+1 | execute n, write rd |
| T+3 | address of n+3 | instruction n+2 | execute n+1 |

That is one cycle per ALU instruction. The register-file latency is the
D-to-X register, not an extra cycle on top.

### Fetch port

Add a fetch port beside the existing bus. Do not fetch on `bus_valid`.

```text
fetch_pc[31:0]     CPU -> RAM port B
fetch_rdata[31:0]  RAM port B -> CPU
```

`fes_riscv_system` connects `fetch_pc[14:2]` to `b_addr` and `b_q` to
`fetch_rdata`. The data bus is unchanged. `bus_instr` stops being used for
fetch; the system may leave the input in place until the testbench moves.

The only executable window is the 32 KiB RAM. A fetch whose
`fetch_pc[31:15]` is not zero is an instruction access fault raised in D,
with `mtval` holding that PC. It does not need `bus_error`. A fetch of an
unmapped address must not be issued as a data-bus read.

### Redirects

Sequential fetch uses `pc + 4`. That is the prediction. Nothing else is
predicted.

| Redirect | Known in | Flush |
| --- | --- | --- |
| JAL | D, from the PC and `imm_j`. No register read | the one instruction already addressed in F |
| JALR, taken branch, MRET | X. JALR still clears bit 0. MRET still reads `mepc` | D and the instruction arriving from F |
| Trap | X, before writeback | D and F, then fetch `trap_vector` |

A taken branch or JALR costs two fetch slots. JAL costs one. Untaken
branches cost nothing beyond the ALU instruction itself. Alignment stays on
`access_low` and `pc_plus_offset[1]`, not on the 32-bit carry chain.

### Data bus

Loads and stores still use the request/ready bus, in X.

A store registers address, data and strobes and issues the bus. Fetch on
port B continues. X stalls for the ready cycle so a second load or store
cannot overlap the first. That is one bubble, not today's five clocks.

A load issues in X and stalls until `bus_ready`. Writeback and forwarding
use `load_data` in the ready cycle. A following instruction that reads the
loaded register stalls in D until that writeback. No load delay slot.

The framebuffer and the I/O block stay on this bus. They are not fetched.

### Forwarding

The register file is two synchronous read ports and one write, two M10Ks.
Today the file is never read and written at a live address in the same
cycle, so the read-during-write result is ignored on purpose. This
pipeline breaks that. In the table above, instruction n writes `rd` on the
clock edge of T+2, and instruction n+1 addresses the file in that same
cycle.

Do not depend on M10K read-during-write. Forward the X result into the
operand registers when the regfile address matches the write and the write
is to a register other than x0. x0 is still never written and still needs
no mux on the file output. A forward into an x0 read is suppressed.

A load result is forwarded the same way in the ready cycle. The consumer
stalled in D takes the forwarded value rather than the stale file read.

### Exceptions and interrupts

Precise exceptions stay as they are. The trapping instruction has no
writeback and no store. `mepc` is its PC for an exception. For an
interrupt, `mepc` is the PC of the instruction that would have executed,
which is the instruction in D, and that instruction is flushed rather than
retired.

Illegal instruction, ecall, ebreak and CSR illegal are decided from the
registered decode and the CSR unit, in X, as EXEC decides them now.
Misaligned address and access fault are decided in X from the same
`access_low` and `bus_error` rules. `trap_vector` stays a bit field. Vectored
`mtvec` does not grow an adder.

Interrupt pins are already registered in `fes_rv32_csr`, so a source raised
by a store is visible to `irq_take` on the following cycle. Take the
interrupt at the next X boundary after `irq_take`, before that instruction
has a side effect. Do not keep the multicycle extra instruction of delay.
That delay is an accident of issuing fetch only after EXEC; the lockstep
model checks that the PC and cause match when the trap is signalled, it
does not require the extra instruction. The CPU README sentence that
describes the delay is updated in the same change as the pipeline, not
before it exists.

`minstret` still counts `retired`. `mcycle` still counts clocks. `WFI`
still completes immediately. `FENCE` and `FENCE.I` are still no-operations
for memory order, except `FENCE.I` also flushes F and D and waits one cycle
so a port-A store is visible to a port-B fetch. There is no instruction
cache to invalidate.

## What this does not fix

The routed critical path is the register-file output through `sum` in
`fes_rv32_alu` and the address adder, in one cycle, into the CSR control
cone. That path is the X stage of this pipeline. Overlapping fetch does
not shorten it. [qor.md](qor.md) is still the timing note: register the
adder result if 74.25 MHz margin is the problem. Do not combine that cut
with the first pipeline measurement. Build the pipeline, re-measure, and
only then decide whether X still needs the cut.

Do not add an instruction cache. VexRiscv Lite's cache was larger and
slower on this device than VexRiscv Min without one. The firmware RAM is
already the instruction memory.

Do not add the M extension, a second data bus, or Wishbone. The data-bus
contract is what `fes_riscv_system` and the lockstep testbench already
implement.

Do not expect the core to shrink. Forwarding and flush logic sit on top of
the execute cone that is already 1,887 COMB cells. VexRiscv Min at 1,090
is evidence that a short pipeline can be small, not a size target for this
CPU. This CPU keeps Zicsr, Zicntr, three interrupts and the existing fault
rules.

## If it is built

1. Fetch port on the CPU, port B of the firmware RAM, forwarding, flush on
   redirect and trap, data bus unchanged. ALU instructions retire every
   cycle when X is not stalled.
2. The CPU testbench answers the fetch port as a one-cycle RAM and keeps
   wait states on the data bus. `make sim-fes-riscv` must still match the
   model: every retirement, register write, store, trap cause and interrupt
   entry. Update the README's interrupt-delay sentence in this change.
3. Re-run the [qor.md](qor.md) harness before calling the clock unchanged.
   The expectation is a similar fmax and a higher instruction rate, not a
   faster clock.
4. The registered-adder cut from qor.md is a separate change, after that
   measurement, and only if seed-to-seed margin at 74.25 MHz is still thin.
