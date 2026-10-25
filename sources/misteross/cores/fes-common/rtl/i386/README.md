# fes_i386_cpu RTL

The first-party 386-compatible CPU proposed in
[the i386 CPU design](../../../../docs/i386-cpu-design.md). This directory
holds Phase 1's first slice: the uop format, the front end up to Q, and the
AG address unit. There is no `fes_i386_cpu` top, decode, cache or execute yet.

| File | Content |
| --- | --- |
| `fes_i386_pkg.sv` | The uop (the Decode table's fields), the prefix record, and `fetched_t`, the instruction Q hands D |
| `fes_i386_oplen.sv` | Length of an unprefixed instruction for each operand and address size, from its opcode, ModRM and SIB |
| `fes_i386_lendec.sv` | The 16 per-byte length decoders: one prefix, the 0Fh escape, lengths for both sizes of each, and the prefix-run flag |
| `fes_i386_fetchbuf.sv` | F1, F2 and the 32-byte fetch buffer, filled 16 bytes per cycle from a memory port |
| `fes_i386_qalign.sv` | Q: length select, prefix accumulator and aligner |
| `fes_i386_frontend.sv` | The fetch buffer and Q together |
| `fes_i386_agu.sv` | AG's linear and offset adders with 4:2 compression and the 16-bit-wrap high-half select |

## Front end

Fetch is sequential from the last redirect; there is no L1I or prediction yet.
The memory port behaves as an M10K with address stall: `mem_data` is the block
at the address registered on the last edge with `mem_load`. A redirect flushes
the buffer and Q; a change of CS.D (`code32`) needs one.

The buffer is two 16-byte halves. Buffer byte j holds linear byte j mod 32 of
the block in its half. As F2 writes a block, the 16 decoders see the other
half's last four bytes and then the new block, so a block's bytes 0-11 are
decoded on arrival and bytes 12-15 when the next sequential block arrives.
F2 refills a half on the cycle after Q leaves it. In that cycle Q may already
use the incoming block's bytes, which the aligner takes straight from the
memory port.

Q keeps a start position and a prefix accumulator. Each cycle it either
consumes one prefix that another prefix follows, or issues the instruction at
start, whose last prefix is part of the per-byte decode. Every per-position
quantity Q's loop needs is computed beside its position from registers, so
the loop is one 32:1 select. Measured in simulation, with D always ready:

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

## Synthesis notes

The repository's Yosys parses a subset of SystemVerilog. This RTL stays inside
it:

- no `import`: ports name `fes_i386_pkg::` types, and a module may alias one
  with `typedef fes_i386_pkg::x x;`;
- no `return`, and no assignment patterns such as `'{a: ...}`;
- no packed arrays of structs (Yosys sizes them wrongly without an error), so
  the per-byte decode is a set of plain arrays;
- package functions do not use package constants.

## Checks

`scripts/test_fes_i386_frontend.py` runs the Verilator checks in
[`sim/i386`](../../sim/i386/README.md#front-end-checks); `tests/test_fes_i386_frontend.py`
runs them from the unit-test suite when Verilator and the pinned oracle are
present. `scripts/benchmark_fes_i386.py` routes the timing spikes.
