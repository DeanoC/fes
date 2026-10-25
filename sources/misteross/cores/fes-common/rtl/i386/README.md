# fes_i386_cpu RTL

The first-party 386-compatible CPU proposed in
[the i386 CPU design](../../../../docs/i386-cpu-design.md). This directory
holds Phase 1's slices so far: the uop format, the front end from F1 to Q, and
D's address operands with AG. There is no `fes_i386_cpu` top, instruction
decode, cache or execute yet.

| File | Stage | Content |
| --- | --- | --- |
| `fes_i386_pkg.sv` | — | The uop (the Decode table's fields), the prefix record, and `fetched_t`, the instruction Q hands D |
| `fes_i386_fetchbuf.sv` | F1, F2, L | Block address and memory port; F2's register; L writing the 32-byte buffer |
| `fes_i386_lstage.sv` | L | The 16 per-byte length decoders and each byte's next start for every prefix-accumulator variant |
| `fes_i386_lendec.sv`, `fes_i386_oplen.sv` | L | The decoders: one prefix, the 0Fh escape, a length for each operand and address size, the prefix-run flag |
| `fes_i386_qalign.sv` | Q | Next-start select, prefix accumulator and aligner |
| `fes_i386_frontend.sv` | F1-Q | The fetch side and Q together |
| `fes_i386_amode.sv` | D | The address registers and scale a ModRM and SIB name |
| `fes_i386_dfwd.sv` | D | Register file read, forwarding by tag, index scaling, the late operand and its stalls |
| `fes_i386_agu.sv` | AG | The linear and offset adders: 3:2 rows, one carry chain each, the 16-bit-wrap and address-size select |

## Front end

Fetch is sequential from the last redirect; there is no L1I or prediction yet.
The memory port stands for the L1I's way-selected output, as an M10K with
address stall: `mem_data` is the block at the address registered on the last
edge with `mem_load`. F2 registers that block. Nothing decodes M10K data in
F2 (design rule 7). A redirect flushes F2, L, the buffer and Q; a change of
CS.D (`code32`) needs one.

L runs the 16 decoders on the registered block. They see the other half's
last four bytes and then the block, so a block's bytes 0-11 are decoded as it
passes L and bytes 12-15 when the next sequential block does. For each of the
four prefix-accumulator variants (66h seen, 67h seen) L also adds the length
to the byte's position. CS.D and the byte's own 66h or 67h are folded in, so
every buffer byte carries a next start per variant. L writes block and decode
into its half of the buffer once Q has released that half, and in that cycle
Q may already use the block's bytes from L's register.

Q keeps a start position and a prefix accumulator. Each cycle it either
consumes one prefix that another prefix follows, or issues the instruction at
start, whose last prefix is part of the per-byte decode. Its loop picks the
stored next start by the accumulator's two flags and then by start: a
select, with no add. Measured in simulation, with D always ready:

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
register's youngest in-flight value from the M1, M2, EX or WB result register
by comparing destination tags, scales the index and registers AG's operands.

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

The repository's Yosys parses a subset of SystemVerilog, and gets some of it
wrong without an error. This RTL stays inside what it reads correctly:

- no `import`: ports name `fes_i386_pkg::` types, and a module may alias one
  with `typedef fes_i386_pkg::x x;` ([DeanoC/yosys#36](https://github.com/DeanoC/yosys/issues/36));
- no `return` ([#37](https://github.com/DeanoC/yosys/issues/37)), and no
  assignment patterns such as `'{a: ...}` ([#38](https://github.com/DeanoC/yosys/issues/38));
- no packed arrays of structs, which Yosys sizes wrongly ([#34](https://github.com/DeanoC/yosys/issues/34));
- no function returning a struct called from another function, which Yosys
  reads as empty ([#33](https://github.com/DeanoC/yosys/issues/33));
- package functions do not use package constants ([#35](https://github.com/DeanoC/yosys/issues/35));
- multi-dimensional packed arrays are declared at module scope, not inside
  generate blocks ([#41](https://github.com/DeanoC/yosys/issues/41));
- small sums are built from gates (`fes_i386_pkg::small_add`), since every `+`
  becomes an ALM carry chain ([#39](https://github.com/DeanoC/yosys/issues/39));
- synthesis runs `proc -norom` first, or the length decoders' opcode tables
  become an M10K ROM that nextpnr-mistral rejects ([#40](https://github.com/DeanoC/yosys/issues/40)).

`scripts/test_fes_i386_frontend.py --yosys <yosys>` runs every check on the
netlist Yosys writes from this RTL, which is what the spikes route. Run it
after any RTL change.

## Checks

`scripts/test_fes_i386_frontend.py` runs the Verilator checks in
[`sim/i386`](../../sim/i386/README.md#front-end-and-address-checks);
`tests/test_fes_i386_frontend.py` runs them from the unit-test suite when
Verilator and the pinned oracle are present. `scripts/benchmark_fes_i386.py`
routes the timing spikes.
