# fes_i386_cpu RTL

The first-party 386-compatible CPU proposed in
[the i386 CPU design](../../../../docs/i386-cpu-design.md). This directory
holds Phase 1's slices so far: the uop format, the front end from F1 to Q,
and `fes_i386_cpu`, which runs the one-uop register and immediate forms in
real mode from D to WB. There is no data memory, cache, microcode or
exception delivery yet.

| File | Stage | Content |
| --- | --- | --- |
| `fes_i386_pkg.sv` | — | The uop (the Decode table's fields), the prefix record, and `fetched_t`, the instruction Q hands D |
| `fes_i386_fetchbuf.sv` | F1, F2, L1, L2 | Block address and memory port; F2's register; L1's register; L2 writing the 32-byte buffer; each byte's availability for Q |
| `fes_i386_lendec.sv`, `fes_i386_oplen.sv` | L1 | The 16 per-byte decoders: one prefix, the 0Fh escape, the prefix-run flag, and the length classes (opcode and fixed-immediate bytes, ModRM bytes per address size, size-dependent immediates) |
| `fes_i386_lstage.sv` | L2 | Each byte's length, end and next-half bit for every prefix-accumulator variant |
| `fes_i386_qalign.sv` | Q | Next-start select, prefix accumulator and aligner |
| `fes_i386_frontend.sv` | F1-Q | The fetch side and Q together |
| `fes_i386_amode.sv` | DC | The address registers and scale a ModRM and SIB name |
| `fes_i386_dfwd.sv` | D | The forwarding table written a cycle ahead, register file read, index scaling, the late operand and its stalls |
| `fes_i386_agu.sv` | AG | The linear and offset adders: 3:2 rows, one carry chain each, the 16-bit-wrap and address-size select |
| `fes_i386_decode.sv` | DC | The instruction to one uop, its #UD and #GP(0) checks, and LEA's displacement |
| `fes_i386_dops.sv` | D | Data operands: the second forwarding table, pending operands and how far ahead their producers are |
| `fes_i386_shift.sv` | EX | The 64-bit funnel shifter and the 386EX's shift flags |
| `fes_i386_exctl.sv` | M2 | EX's controls, decoded a stage ahead into registered one-hot selects |
| `fes_i386_ex.sv` | EX | The late ALU (one adder, ZF from the operands), the result's merge into its register, flags and branch resolution |
| `fes_i386_cpu.sv` | F1-WB | The top: front end, DC's decoded buffer, D, AG, M1, M2, EX, WB, the register file, segment registers, reset state and retirement trace |

## Front end

Fetch is sequential from the last redirect; there is no L1I or prediction yet.
The memory port stands for the L1I's way-selected output, as an M10K with
address stall: `mem_data` is the block at the address registered on the last
edge with `mem_load`, and `mem_valid` says that response is available. F2
holds its address and does not advance until `mem_valid`; it requests the next
block on the edge that accepts the current response. Nothing decodes M10K data
in F2 (design rule 7). A redirect flushes F2, L1, L2, the buffer and Q; a
change of CS.D (`code32`) needs one.

Length decode takes two stages. L1 runs the 16 decoders on F2's register and
registers what each byte's instruction is made of: its prefix and run flags,
its opcode and fixed-immediate bytes, its ModRM, SIB and displacement bytes
for each address size, and whether a 2- or 4-byte immediate or offset
follows. The decoders see the previous block's last four bytes and then the
block, so a block's bytes 0-11 are decoded as it passes L1 and bytes 12-15
when the next sequential block does. Those four bytes come from L2's
register, since the previous block may not have reached the buffer yet. L2
adds the classes up for each of the four prefix-accumulator variants (66h
seen, 67h seen), with CS.D and the byte's own 66h or 67h folded in, into each
byte's length, end and next-half bit. L2 writes block and decode into its half
of the buffer once Q has released that half, and in that cycle Q may already
use the block's bytes from L2's register. The second stage costs one cycle
on every redirect: the first instruction issues 5 cycles after a redirect
instead of 4.

The buffer also holds, per byte and variant, whether Q may use the
instruction there: its half is valid, its bytes 12-15 are decoded if it starts
there, and the next half is valid or being filled if it ends past its own
half. It is registered from next-cycle state. A release on that edge is
ignored for the byte's own half, since Q's start has just left it; the next
half's term assumes a release whenever Q is in that half now.

Q keeps a start position and a prefix accumulator. Each cycle it either
consumes one prefix that another prefix follows, or issues the instruction at
start, whose last prefix is part of the per-byte decode. Its loop picks the
stored availability and next start by the accumulator's two flags and then by
start: a select, with no add. Measured in simulation, with D always ready:

- one instruction per cycle on every prefix-free stream, once the buffer is
  full, including streams of 8- to 11-byte instructions;
- an instruction with k prefixes issues max(1, k) cycles after the one before
  it;
- an instruction longer than 15 bytes, or a run of prefixes that reaches a
  16th byte, issues with `too_long` set, for D to raise #GP(0).

Lengths follow the oracle's decoder for every encoding it defines. The i386
raises #UD for the rest, so their length is not architectural, and D decides
#UD. MOV to and from TR6 and TR7 (0F 24, 0F 26), which the design accepts and
the oracle does not, take the CR/DR rule: ModRM is always a register form.

## D's address operands and AG

D decodes the address registers from ModRM and SIB. A SIB index of 100b with a
nonzero scale scales the base, as the 386EX does (Phase 0 divergence
`invalid-sib-scales-base`). D reads the flip-flop register file, takes each
register's youngest in-flight value from the M1, M2, EX or WB result
register, scales the index and registers AG's operands.

D's forwarding selects are worked out a cycle ahead, per GPR. Each cycle a
table records, for every GPR, where its youngest value will be next cycle
among the uops then in M1, M2, EX and WB: in a result register, late from EX
or WB, further off, or only in the register file. It is written from the
destination tags of the uops now in AG, M1, M2 and EX. Beside it go the GPR
the uop in D writes, when that result is ready, and whether the uop left D:
next cycle that uop is in AG, younger than everything in the table. D's cycle
applies each GPR's registered select to the result registers beside the
addressing decode and picks base and index by register number, with no tag
compare. If the stages behind D ever hold, the table must be written from the
held producers.

A value completing in the cycle the uop leaves D arrives late, from the
register it lands in when the uop is in AG: the previous uop's early-ALU
result (M1), load data from M2 (EX) or a late-ALU result from EX (WB). At most
one late value reaches AG, entering its last 3:2 row as a base or an unscaled
index. D stalls the uop when the late value would be a scaled index, when
both operands would be late, or when a value is further off.

AG adds the segment term, D's first-row operand and the displacement in a 3:2
row from registers, then the last-row operand (D's or the late value) in a
second, then one carry chain per sum. With a 16-bit address size every operand
is zero-extended; linear bits 31:16 come from the segment's high half or that
plus one, by the carry that bits 17:16 of the two sums leave once the wrap is
removed, so only the address-size select follows the 32-bit chain.

## D to WB

`fes_i386_cpu` puts DC, D, AG, M1, M2, EX and WB behind the front end. M1
and M2 carry uops through; the memory slice fills them.

**DC** decodes Q's output register into a two-entry buffer of decoded
instructions: `dc` holds the instruction D issues, and `dc_skid` takes Q's
instruction when `dc` cannot. Q may load its output whenever `dc_skid` is
empty, so D's stall never reaches Q's loop as `d_ready`, and everything D's
issue waits on starts from a register. The decode costs one cycle on every
redirect.

DC decodes the instruction to one uop (`fes_i386_decode`): group 1, INC,
DEC, NEG, NOT, TEST, MOV (to and from a segment register in real mode, and
immediates), LEA, the shifts and rotates, SHLD and SHRD, SETcc, MOVZX and
MOVSX, CBW/CWDE, CWD/CDQ, SAHF, LAHF, the flag set and clear instructions,
NOP, HLT, and the near JMP, Jcc, LOOP, LOOPE, LOOPNE and JCXZ, register
forms only (LEA's memory forms access nothing). LOCK on any of them, MOV to
CS or to segment register 6 or 7, LEA of a register and C6h/C7h with reg != 0
raise #UD; over 15 bytes raises #GP(0). Every other encoding decodes as
`FAULT_UNIMPLEMENTED`, which stops the CPU at WB, so no check can pass over
one. DC also registers the address registers and scale a memory ModRM names
(`fes_i386_amode`).

**D** keeps the instruction's EIP with an incrementer; in 16-bit code an
instruction whose next EIP passes FFFFh is also unimplemented, since fetch
would have to wrap and the CS-limit check is not built. D applies that
fault, since only D knows the EIP.

The uop's src1 is always its destination's register: a byte or word result
merges into the old value, an implicit source. src2 is the second register
or the immediate; a CL count is a third source. D reads all three from the
register file or the M1, M2, EX and WB result registers (`fes_i386_dops`),
with selects worked out a cycle ahead per GPR as `fes_i386_dfwd` does for
addresses. A data operand never stalls D: if its producer has not computed it,
the operand is pending and records how far ahead the producer is (1-4
stages). No stage behind D holds, so the producer reaches WB when the uop is
that many stages before WB, and the uop takes the WB result register there:
AG, M1 or M2 into its next pipeline register, or EX beside its operand, which
is EX's only forwarding mux. A late result reaches the next uop's ALU with no
bubble. LEA's offset is early when the destination is 32-bit and comes from
AG; a 16-bit LEA merges at EX like any other result. `fes_i386_dfwd` gives
LEA's address operands and its stalls as before. A selector read (MOV r/m,
Sreg) waits in D while an older uop writes that segment register.

**AG** runs `fes_i386_agu` for LEA, or for a branch's target: CS.base + EIP +
length + displacement in one carry chain, wrapped by operand size.

**EX** (`fes_i386_ex`) takes its selects registered: M2 decodes them from
the uop (`fes_i386_exctl`), so no opcode compare sits on EX's data paths. It
has one adder for ADD, ADC, SUB, SBB, CMP, INC, DEC,
NEG and LOOP's count, with ZF from the operands; the logic unit; the funnel
shifter (`fes_i386_shift`); and the result's merge into the destination's old
value, so WB writes whole registers. It keeps a running EFLAGS: every uop that
passes EX updates it, so Jcc, SETcc, ADC, SBB and RCL/RCR read it with no
forwarding. Undefined flags follow the 386EX (the Phase 0 divergence list):
logic ops clear AF, shifts by a nonzero count set it, and the shift OF and
wide-count CF rules are in `fes_i386_shift`.

**Branches** are predicted not taken. EX resolves them; a taken one redirects
F1 from WB, where every younger uop is flushed, and clears D's forwarding
tables. A fault, an unimplemented encoding or HLT stops D issuing after it;
at WB the fault stops the CPU with nothing retired (delivery waits for the
microcode slice), and HLT retires and stops it.

**WB** commits in order: the GPR file and segment registers in flip-flops,
EFLAGS and EIP. A real-mode segment load sets the base to selector * 16 and
keeps the limit and attributes.

**Reset and trace.** `init_*` is the architectural state reset loads; a
machine ties it to the 386 reset state, and the lockstep harness to a test's
state. `trace_*` and `arch_*` show each retirement.

## Synthesis notes

The i386 RTL has its own toolchain lock, [`toolchain.lock`](toolchain.lock)
here: the DeanoC Yosys fork's main at `238eac9a49`, with nextpnr `bd7a1c0d`
and Mistral `8fcc4cb4`. The timing spikes default to it. The repository's
shared lock stays at Yosys `3a5accbd`, which reads this RTL wrongly, so check
and route the i386 RTL with this lock's Yosys. Each later Yosys fix is a
small bump of this lock, the removal of its one workaround, the `--yosys`
check and a re-measure.

Synthesis runs `proc -norom` first, by choice: it keeps case tables in
logic. Without it the length decoders' 0Fh opcode tables become M10K ROMs
whose registered read takes in F2's byte register, which puts an M10K read in
front of L1's decode, the shape [rule 7](../../../../docs/i386-cpu-design.md#platform-constraints)
keeps out of the pipeline. Measured on the corrected model, F2 fell from
86-91 to 73-77 MHz in the front-end spike. A `rom_style = "logic"` attribute
on the table's case statement does not keep it in LUTs.

Narrow sums (lengths, ends, counts) mark their `+` with `(* no_carry *)`, so
the locked Yosys builds them from LUTs rather than an ALM carry chain
([DeanoC/yosys#44](https://github.com/DeanoC/yosys/pull/44); the attribute
is opt-in and Verilator ignores it).

The locked Yosys still gets some SystemVerilog wrong. This RTL stays inside
what it reads correctly:

- no assignment patterns such as `'{a: ...}` ([#38](https://github.com/DeanoC/yosys/issues/38));
- logic replicated to cut fanout merges back into one copy even on
  `(* keep *)` wires, so each replica needs its own kept input register
  ([#51](https://github.com/DeanoC/yosys/issues/51));
- read two or more fields of one variable-indexed record through a single
  whole-record copy (`at_start = dec[start]`), not as `dec[start].a` and
  `dec[start].b`: the selects share their `start * width` multiply, which
  then maps to a DSP multiplier and a carry chain
  ([#59](https://github.com/DeanoC/yosys/issues/59)). A constant index, as
  in a generate loop, is fine.

`dist` is a SystemVerilog keyword (the constraint operator); Verilator rejects
it as a signal name, so the forwarding modules spell the distance `dist_a` or
`ahead`.

`scripts/test_fes_i386_frontend.py --yosys <yosys>` and
`scripts/test_fes_i386_lockstep.py --yosys <yosys>` run every check on the
netlist Yosys writes from this RTL, which is what the spikes route. Run them
after any RTL change. The front-end runner also synthesizes Q's aligner on its
own and fails if any multiplier or carry cell appears there: Q has no wide sum,
and the front-end spike does not show #59's multiplier.

## Checks

`scripts/test_fes_i386_frontend.py` runs the Verilator checks in
[`sim/i386`](../../sim/i386/README.md#front-end-and-address-checks), and
`scripts/test_fes_i386_lockstep.py` the
[whole-CPU lockstep](../../sim/i386/README.md#whole-cpu-lockstep);
`tests/test_fes_i386_frontend.py` and `tests/test_fes_i386_lockstep.py` run
them from the unit-test suite when Verilator, the pinned oracle and (for the
lockstep) the pinned corpus are present. `scripts/benchmark_fes_i386.py`
routes the timing spikes.
