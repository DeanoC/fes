# Original FES RV32I CPU

A synthesizable 32-bit RISC-V machine-mode CPU written from the RISC-V
unprivileged and privileged specifications. It does not instantiate,
translate or include PicoRV32, VexRiscv, SERV or any other existing core.
`fes.riscv` is its first consumer. Each consumer needs its own full-system
timing and hardware qualification.

| Entry point | Behaviour | Interface |
| --- | --- | --- |
| `fes_rv32_cpu` | RV32I, Zicsr, Zicntr; machine mode only; precise exceptions; external, timer and software interrupts | One request/ready bus for instructions and data; observation ports |
| `fes_rv32_csr` | mstatus (MIE/MPIE, MPP reads 11), misa, mie, mtvec, mscratch, mepc, mcause, mtval, mip, mcycle(h), minstret(h), cycle/time/instret shadows, zero mvendorid/marchid/mimpid/mhartid | Used by the CPU; one CSR access per cycle |
| `fes_rv32_alu` | ADD/SUB, shifts, comparisons, logic | Combinational |

## Execution

The CPU is multicycle and not pipelined. FETCH issues the instruction read
and, when it completes, captures the word, its decoded class and both source
registers; EXEC performs the instruction or starts a data access; MEM waits
for that access. With a one-cycle memory an ALU, branch or jump instruction
takes three clocks and a load or store takes five. Exceptions are precise:
the trapping instruction has no side effects and `mepc` is its address.

The register file is a 32-entry synchronous-read array (two M10K blocks on
Cyclone V). Entry 0 is initialised to zero and never written, so x0 needs
no output mux; the initial values of the array are part of the bitstream.

Supported encodings: LUI, AUIPC, JAL, JALR, the six branches, LB/LH/LW/LBU/LHU,
SB/SH/SW, the OP-IMM and OP groups (including SLLI/SRLI/SRAI with the
documented funct7 checks), FENCE and FENCE.I (no-operations), ECALL, EBREAK,
MRET, WFI (completes immediately) and the six CSR forms. Every other word,
including the M, A, F and C extensions and SRET/URET, raises an illegal
instruction exception with `mtval` holding the word.

| Exception | Cause | `mtval` |
| --- | --- | --- |
| Instruction address misaligned (taken branch, JAL or JALR target with bit 1 set; bit 0 of a JALR target is cleared) | 0 | target |
| Instruction access fault (`bus_error` on a fetch) | 1 | fetch address |
| Illegal instruction (including unknown CSRs and writes to read-only CSRs) | 2 | instruction |
| Breakpoint (EBREAK) | 3 | EBREAK address |
| Load address misaligned / load access fault | 4 / 5 | data address |
| Store address misaligned / store access fault | 6 / 7 | data address |
| Environment call from M-mode | 11 | 0 |

Misaligned loads and stores are not emulated; they trap. Interrupts are
recognised at the start of a fetch when `mstatus.MIE` is set and a pending
source is enabled in `mie`, in the order external (11), software (3), timer
(7). `mepc` is then the address of the instruction that would have been
fetched. The interrupt inputs are registered once inside the CSR unit, so a
source raised by a store is recognised at the fetch after the following
instruction. `mtvec` mode 0 (direct) and 1 (vectored) are supported; a
vectored BASE is kept 64-byte aligned (WARL), so the vector is a bit field and
needs no adder. `mcycle` and `minstret` are writable; a write to either takes
effect after the writing instruction, which is not counted. `time` and
`timeh` read the 64-bit `mtime` input supplied by the system.

## Bus

```text
bus_valid, bus_instr, bus_addr[31:0], bus_wdata[31:0], bus_wstrb[3:0]   CPU -> system
bus_ready, bus_error, bus_rdata[31:0]                                   system -> CPU
```

One request is outstanding at a time. The request stays stable until the
cycle in which `bus_ready` is sampled; `bus_error` is sampled with it and
reports an access fault. `bus_wstrb` is zero for reads; stores present the
byte lanes of the addressed word with the data replicated, so a halfword
store at offset 2 drives `bus_wstrb = 4'b1100`. `bus_instr` marks fetches.
Reads return the whole aligned word; the CPU selects the byte or halfword.
Interrupt inputs and `mtime` are synchronous to `clk`. `reset` is an
active-high synchronous reset; the first fetch is from `RESET_VECTOR`.

Observation ports (`step`, `retired`, `step_pc`, `trap`, `trap_cause`,
`wb_*`, `debug_pc`) describe completions for simulation and do not affect
execution.

## Validation and synthesis

The ALU uses the exact model-generated source from AlphaMister's
`riscv-alu-timing-guidance-v1` campaign, round two proposal 0002 (SHA256
`077804dcf9fb050a55898c6479ddc11baf9cc5f0a794e9772f954637248ba7c9`).
It shares the left/right barrel shifter and bitwise selection, preserving the
adder and signed/unsigned comparisons. Exact binary-input combinational
proof covers result, eq, lt and ltu; strict integration lint passes.
Fresh final qualification passed 8/8 whole-pilot routes at the unchanged
74.25 MHz requirement, with a 74.744 MHz minimum. Area fell from 2692 to
2587 ALUTs (105 / 3.9%); flip-flops remained 755. See
[the optimizer timing-guidance change](https://github.com/DeanoC/alphamister/pull/27)
for the frozen source, compiler, prompt and routing provenance.

These are frozen zero-BuildID pilot measurements. Every production package
and other CPU consumer needs its own full-system timing and hardware
validation.

Production integration on 2026-10-11 built committed source
`d439f0d915b4e0a922c89c986167877bcaf52e4a`. The normal package
`c6c11fcdcc640420ac86da88972e85e685db4714f11f0f4014fdcf20de8206e2`
(BuildID `a711869391990efc125d016204eccc88`) closed its first GPU1 HIP
route, seed 1, at 75.188 MHz with 2623 ALUTs and 761 flip-flops. The real
BuildID and package recipe define a separate context from the pilot; the
pilot area cap and eight-route claim do not apply to this production package.
All six simulation cases passed, including the 600-frame static-video soak
(549,849,600 output pixels), as did seven producer tests and parent consistency.

Contained developer diagnostics loaded that exact package on both authorized
kits. The scene and timer-driven colour change were captured for ten seconds
per kit. All sampled banner cells matched; sampled static differences above
30 RGB levels were at most 26 capture pixels on kit A and zero on kit B.
Compression/scaling prevent exact RGB equality. Stop returned each kit to idle
and its lease was released. Gamepad movement was checked in simulation only.
[The integration record](../../../../../../docs/validation/riscv-timing-alu-2026-10-11.json)
binds the package, compiler, simulation, capture and cleanup evidence. This
does not establish factory-image or full hardware acceptance or resolve
[the intermittent video report](https://github.com/DeanoC/fes/issues/673).

From `sources/misteross`:

```sh
make sim-fes-riscv
python3 scripts/sim_fes_riscv.py --case alu
python3 scripts/sim_fes_riscv.py --case cpu --seeds 20
```

`alu` checks every operation against C++ arithmetic on edge values and two
million random operands. `cpu` runs a directed program whose expected
results are taken from the specification (immediates, sign extension, byte
lanes, every branch, link values, CSR read-modify-write forms, counters,
every exception class with its cause and `mtval`, direct and vectored
interrupts with their priority and masking) with zero and random bus wait
states, then random instruction streams compared in lockstep with an
independent instruction-level model (`cores/fes-common/sim/riscv/rv32_model.h`):
every retirement, register write, store, trap cause and interrupt entry must
match, and the final registers and memory are compared. Reads of the
cycle/time/instret counters are adopted from the RTL rather than predicted.
The default run uses six seeds. Output and build logs live under
`build/sim/fes-riscv/<case>/`. The remaining cases (`firmware`, `system`,
`board`) belong to the [fes.riscv core](../../../fes-riscv/README.md).

These are host simulations. They do not establish timing, an RBF or
hardware behaviour. The sealed `fes.riscv` producer qualifies the CPU
together with its memories and video at the 74.25 MHz pixel clock; its
result applies to that package only.

## Behaviour sources and provenance

Written independently from these specifications; existing CPU implementation
sources were excluded from the design work:

- [The RISC-V Instruction Set Manual, Volume I: Unprivileged ISA](https://riscv.org/specifications/ratified/) — RV32I base, Zicsr, Zicntr.
- [The RISC-V Instruction Set Manual, Volume II: Privileged Architecture](https://riscv.org/specifications/ratified/) — machine-mode CSRs, traps and interrupts.

The RTL and tests are original MIT-licensed work; each file carries its
SPDX identifier.
