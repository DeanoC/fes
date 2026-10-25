# fes_i386_cpu RTL

The first-party 386-compatible CPU proposed in
[the i386 CPU design](../../../../docs/i386-cpu-design.md). This directory
holds Phase 1's slices so far: the uop format, the front end from F1 to Q, and
D's address operands with AG. There is no `fes_i386_cpu` top, instruction
decode, cache or execute yet.

| File | Stage | Content |
| --- | --- | --- |
| `fes_i386_pkg.sv` | — | The uop (the Decode table's fields), the prefix record, and `fetched_t`, the instruction Q hands D |
| `fes_i386_fetchbuf.sv` | F1, F2, L1, L2 | Block address and memory port; F2's register; L1's register; L2 writing the 32-byte buffer; each byte's availability for Q |
| `fes_i386_lendec.sv`, `fes_i386_oplen.sv` | L1 | The 16 per-byte decoders: one prefix, the 0Fh escape, the prefix-run flag, and the length classes (opcode and fixed-immediate bytes, ModRM bytes per address size, size-dependent immediates) |
| `fes_i386_lstage.sv` | L2 | Each byte's length, end and next-half bit for every prefix-accumulator variant |
| `fes_i386_qalign.sv` | Q | Next-start select, prefix accumulator and aligner |
| `fes_i386_frontend.sv` | F1-Q | The fetch side and Q together |
| `fes_i386_amode.sv` | D | The address registers and scale a ModRM and SIB name |
| `fes_i386_dfwd.sv` | D | The forwarding table written a cycle ahead, register file read, index scaling, the late operand and its stalls |
| `fes_i386_agu.sv` | AG | The linear and offset adders: 3:2 rows, one carry chain each, the 16-bit-wrap and address-size select |

## Front end

Fetch is sequential from the last redirect; there is no L1I or prediction yet.
The memory port stands for the L1I's way-selected output, as an M10K with
address stall: `mem_data` is the block at the address registered on the last
edge with `mem_load`, and `mem_ready` says that response is available. F2 holds
its address and does not advance until `mem_ready`; it requests the next block
on the edge that accepts the current response. Nothing decodes M10K data in F2
(design rule 7). A redirect flushes F2, L1, L2, the buffer and Q; a change of
CS.D (`code32`) needs one.

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

## Synthesis notes

The i386 RTL has its own toolchain lock, [`toolchain.lock`](toolchain.lock)
here: the DeanoC Yosys fork's main at `d4ac300d9d`, with nextpnr `bd7a1c0d`
and Mistral `8fcc4cb4`. The timing spikes default to it. The repository's
shared lock stays at Yosys `3a5accbd`, which reads this RTL's per-byte
records wrongly, so check and route the i386 RTL with this lock's Yosys.
Each later Yosys fix is a small bump of this lock, the removal of its one
workaround, the `--yosys` check and a re-measure.

The locked Yosys still parses a subset of SystemVerilog, and gets some of it
wrong without an error. This RTL stays inside what it reads correctly:

- no `import`: ports name `fes_i386_pkg::` types, and a module may alias one
  with `typedef fes_i386_pkg::x x;` ([DeanoC/yosys#36](https://github.com/DeanoC/yosys/issues/36));
- no `return` ([#37](https://github.com/DeanoC/yosys/issues/37)), and no
  assignment patterns such as `'{a: ...}` ([#38](https://github.com/DeanoC/yosys/issues/38));
- no function returning a struct called from another function, which Yosys
  reads as empty ([#33](https://github.com/DeanoC/yosys/issues/33));
- a field of an array element, `arr[i].field`, is read and written through a
  whole-record copy: Yosys takes `arr[i]` for a generate block, which is an
  error with a variable index and a silent, undriven wire with a constant one
  ([#52](https://github.com/DeanoC/yosys/issues/52));
- package functions do not use package constants ([#35](https://github.com/DeanoC/yosys/issues/35));
- small sums are built from gates (`fes_i386_pkg::small_add`), since every `+`
  becomes an ALM carry chain ([#39](https://github.com/DeanoC/yosys/issues/39));
- synthesis runs `proc -norom` first, or the length decoders' opcode tables
  become an M10K ROM that nextpnr-mistral rejects ([#40](https://github.com/DeanoC/yosys/issues/40));
- logic replicated to cut fanout merges back into one copy even on
  `(* keep *)` wires, so each replica needs its own kept input register
  ([#51](https://github.com/DeanoC/yosys/issues/51)).

`scripts/test_fes_i386_frontend.py --yosys <yosys>` runs every check on the
netlist Yosys writes from this RTL, which is what the spikes route. Run it
after any RTL change.

## Checks

`scripts/test_fes_i386_frontend.py` runs the Verilator checks in
[`sim/i386`](../../sim/i386/README.md#front-end-and-address-checks);
`tests/test_fes_i386_frontend.py` runs them from the unit-test suite when
Verilator and the pinned oracle are present. `scripts/benchmark_fes_i386.py`
routes the timing spikes.
