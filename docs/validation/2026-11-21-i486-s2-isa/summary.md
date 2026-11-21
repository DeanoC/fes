# i486 slice 2: the 486SX instruction set behind the I486 build parameter, 21 November 2026

This record is slice 2 of the [i486 plan](../2026-11-17-i486-plan/summary.md)
(tracker [#1962](https://github.com/DeanoC/fes/issues/1962), slice issue
[#2044](https://github.com/DeanoC/fes/issues/2044)). It adds the 486SX
instruction set to `fes_i386_cpu` behind a build parameter, `I486`, with
default 0. Every package still builds 0. The target is Deano's choice of
18 November: a later-stepping 486 with CPUID and without VME, so there is
no CR4. What the identity is and does is in the design doc,
[`i386-cpu-design.md`, "The i486 identity"](../../../sources/misteross/docs/i386-cpu-design.md#the-i486-identity).

The RTL starts from the slice 1 fit spike (`deano/i486-s1-spike-rtl`,
arm b), with these changes:
- the spike's reset-latched input (a strap) is replaced by the parameter;
- everything is gated so that `I486 = 0` folds back to the 386;
- three of the spike's shortcuts are closed: XADD's and CMPXCHG's memory
  loads are now checked as writes, CPUID returns zeros above leaf 1, and a
  real-mode delivery clears AC.

**Result.** Each row names the commit it ran on. The head is `9aa6d0561`.

| Gate | Result |
| --- | --- |
| Level-4 directed cases, 486SX (`I486 = 1`) | 134 of 136 agree with the oracle (96 cases and 40 `ud-` probes), on the RTL and the netlist, two runs each, at `9aa6d0561`. The 2 stops are by design: `cpuid-1` (Bochs is a DX4, `0480h`; the identity is a 486SX, `042Bh`) and `cpuid-2` (Bochs answers leaf 2 with leaf 1; a 486 returns zeros, data book Appendix B) |
| Level-4 directed cases, 386 (`I486 = 0`) | The 112 rows of main's `i486-expected-stops.txt` are unchanged. The 24 new cases add 24 rows, each a stop at or after its probe. RTL and netlist, two runs each |
| Level-3 lockstep, 386, RTL | Passes at `9aa6d0561`. Its report equals main's (`ce67cbbc4`) in all 9,965 values except source and oracle-build digests: every count, trace hash and cycle figure is the same |
| Level-3 lockstep, 386, netlist | Passes; equal to main's in all 9,972 values except source, netlist and oracle-build digests. The seal's own full netlist lockstep (`--with-lockstep`) passes too |
| test386.asm, 386 | POST codes end at FFh. The report is identical to main's in all 707 values |
| Identity-off netlist | 29,548 flip-flop bits, the same as main's (`netlist/flops.txt`); cells 40,791 against 40,764; the digest differs (below) |
| DE10 seal of the 386 package | Registered producer `build_pc386_product.py --with-lockstep`, seed 1, Quartus 17.0.2, on chip-30x0-ai-dev, at `9aa6d0561`: passes every analysis. BUILD_ID `6de4bd49354bf70b1a1bdd96c10a7248`, package `11b50c98…`, RBF `3c28aa32…`, 36,416 ALMs, setup +0.511 ns, hold +0.101 ns (`seal/`) |
| Kit run of that package | Kit B, run `k2044a`: see [the kit run](#the-kit-run) |
| 486SX fit on the DE10 | Closes at 61.44 MHz in the seal's flow at every seed fitted: see [the 486SX fit](#the-486sx-fit) |

## What changed

- **RTL** (`cores/fes-common/rtl/i386/`, `cores/fes-pc386/rtl/pc386_system.sv`).
  `I486` is a parameter on `fes_i386_cpu`, `fes_i386_decode`,
  `fes_i386_seq`, `fes_i386_ex`, `fes_i386_exctl` and `pc386_system`.
  - Each 486 decode item is `if (I486) … else undefined = 1'b1`, the 0Fh
    map's default.
  - Each 486 sequence is empty at 0, which is the sequencer's default.
  - EX's three new results, the privileged-opcode additions, the INVLPG
    flush, TR3's write and #AC's vector and error code are all gated by
    `I486`.
  - The two EFLAGS registers are `I486 ? 22 : 18` bits wide, read through
    a mask, so at 0 they are the 386's 18 bits. Masking alone left 13
    dead flip-flop bits in the seal's netlist (below).
- **Governor** (`governor.toml`, regenerated package and ROM). Each new
  instruction gets the i486 data book's Table 13-15 count on every table.
  CMPXCHG to memory (7 or 10 clocks) and CPUID (14 or 9) take the middle,
  rounded up. `0F01 /7` is split: the register form stays in `group7_ud`.
  A #UD never retires, so on the 386 the new rows are never charged. The
  rows have no governor bench at `I486 = 1`: `test_fes_i386_governor.py`
  builds the 386.
- **Lockstep.** `test_fes_i486_lockstep.py` runs both identities, on the
  RTL (`-GI486`) or with `--netlist` on the seal's netlist
  (`hierarchy -chparam`). Each identity has its own expected-stops list:
  `i486-expected-stops.txt` for the 386 and the new
  `i486-expected-stops-486sx.txt`. `fes_i486_cases.py` gains 24 cases:
  - XADD and CMPXCHG register aliasing and high-byte forms;
  - CMPXCHG to a read-only page and to a not-present page, checking that
    the flags and EAX are unchanged and that the error code has W set;
    XADD to a not-present page;
  - INVLPG on a remapped page, and on a not-present one;
  - #AC after POPFD, and for XADD, LFS, BOUND and SGDT at each alignment;
  - AC across a real-mode and a protected-mode delivery;
  - BSWAP with a 16-bit operand;
  - CPUID leaf 2.

  No lockstep testbench or oracle file changed.

## The sources behind three choices

These were read for this slice from the Intel documents on bitsavers: the
1992 PRM, data book 241731-002 and AP-485 revisions.
- **#AC before #PF.** PRM §9.4, Table 9-2, ranks "Alignment Faults for
  memory operands" above "[Lowest] Page Faults for memory operands". So the
  check stays at M1, before the walk, as the spike had it. The fit spike's
  record listed this as a shortcut. It is the 486's order.
- **The signature, `042Bh`.** The data book's Table 9-9 gives "Intel486 SX
  04 2x". AP-485 says older SX steppings have no CPUID and lists none that
  do. The stepping tables at datasheets.chipdb.org and ardent-tool.com (a
  secondary source) mark `0427h`, the spike's value, as without it, and
  `042Ah` and `042Bh` (aA0 to aC0) as with it. `042Bh` is the latest of
  those. Leaf 1's EDX is zero for an SX: the data book's bit 0 is "FPU
  on-chip", and no document gives an SX's value.
- **Above leaf 1.** The data book's Appendix B, item 3, gives zeros in all
  four registers. AP-485's Table 1 says "undefined". The RTL returns zeros.

Two behaviours have no 486 document. Both follow Bochs, as plan open
question 6 allows when the 386 has nothing to keep:
- **BSWAP r16.** The PRM says "undefined". The RTL writes a zero low word
  and keeps the high word. Bochs does the same, and so did later Intel and
  AMD parts in a published measurement (gynvael.coldwind.pl, 2009). No
  measurement on a 486 was found.
- **AC on a real-mode delivery.** The 1992 PRM's INT pseudocode clears
  only IF and TF. The SDM's adds "AC ← 0", and Bochs clears AC. The RTL
  clears it. A protected-mode delivery keeps AC in every source.

## The identity-off netlist

`netlist/elaborate.py` runs the CPU through the seal's Yosys program, using
the pc386 lock's HIP slot on Powerboat, at main (`ce67cbbc4`) and at head
with `I486` set to 0 and to 1. `netlist/flop_widths.py` sums flip-flop
widths after `proc; opt -fast`.

| | Main | Head, `I486 = 0` | Head, `I486 = 1` |
| --- | --- | --- | --- |
| Flip-flop bits | 29,548 | 29,548 | 29,599 |
| Cells | 40,764 | 40,791 | 40,993 |
| Netlist SHA-256 | `57851047…` | `6e10f559…` | `e356212d…` |

- **Flip-flops.** At 0 the 486 state is gone: TR3, CR0's five bits, and
  AC and ID in each EFLAGS copy. Only the indices of `pipe_t` and
  `exctl_t` fields shift, because both structs widened.
- **Cells.** The 27 extra cells come from the governor's new classes and
  from restructured terms.
- **Digest.** It is not main's, so the 386 package needed the normal
  reseal and kit run, which this slice did. Its behaviour is shown equal
  by the level-3 report comparisons above, not by the netlist.

The first version gated only the writes. It left 13 dead flip-flop bits,
because a sync-reset EFLAGS register kept bits 18 to 21 through an `x`
branch. Sizing the registers by the identity removed them.

## The 486SX fit

`fit_486sx.py` elaborates the seal's machine program with `I486 = 1` added
to the sealed guest-rate parameters. It fits the result in the producer's
own Quartus project (`pc386_quartus.write_project`), with BUILD_ID zero,
and applies the seal's timing check (`validate_timing_report`). It ran on
chip-30x0-ai-dev with the CUDA slot. Nothing is packaged.

| Commit | Seed | Closes | ALMs | Setup | Hold |
| --- | --- | --- | --- | --- | --- |
| `ec945043d` | 1 | yes, 61.44 MHz | 36,651 (87.5%) | +0.984 ns | +0.066 ns |
| `ec945043d` | 2 | yes, 61.44 MHz | 36,504 (87.1%) | +0.631 ns | +0.094 ns |

`ec945043d` differs from the head only by a Verilator lint pragma and its
comment in `fes_i386_pkg.sv`. The 386 seal at head, the same flow with
`I486 = 0` at seed 1, uses 36,416 ALMs. The 486SX's 235 more ALMs at seed 1
are one seed's difference. Slice 1 showed that this flow's seed spread is
about 55 ALMs and that a netlist change moves ALMs by more than that, so
this is not a measured cost of the delta.

## The kit run

Pending at the time of writing.

## Not in this slice

- **The BIOS clearing CD and NW on a 486 identity** (plan, slice 2). With a
  build parameter it is a per-identity ROM. With a strap it is a runtime
  test in the shared ROM. So it waits for Deano's strap-or-parameter
  choice. The 386 ROM and its `rom_hex` pin are unchanged. On this core,
  CD and NW have no effect.
- **The plan's other slice-2 gates:**
  - Duke Nukem 3D past its BSWAP on the RTL and on a kit;
  - the census rerun with the identity on;
  - random programs against the level-4 oracle (the testbench's level-4
    mode runs only the directed cases);
  - a kit run of a 486SX build.

  None was run, because no 486 package exists while the identity is a
  build parameter that every package leaves at 0.
- **The governor rows on a bench.** They are generated and checked
  (`fes_i386_governor.py --check`), but no bench runs at `I486 = 1`.
- **Plan open questions 6 and 8.** The 386-microcode behaviours and a
  `queue32` profile stay the 386's.
