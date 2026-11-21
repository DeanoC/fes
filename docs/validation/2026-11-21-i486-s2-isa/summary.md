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

**Result.** Each row names the commit it ran on. The code head is
`bf7afef73`; later commits change only this record.

| Gate | Result |
| --- | --- |
| Level-4 directed cases, 486SX (`I486 = 1`) | 139 of 141 agree with the oracle (101 cases and 40 `ud-` probes), on the RTL and the netlist, two runs each. The 2 stops are by design: `cpuid-1` (Bochs is a DX4, `0480h`; the identity is a 486SX, `042Bh`) and `cpuid-2` (Bochs answers leaf 2 with leaf 1; a 486 returns zeros, data book Appendix B) |
| Level-4 directed cases, 386 (`I486 = 0`) | The 112 rows of main's `i486-expected-stops.txt` are unchanged. The 29 new cases add 29 rows, each a stop at or after its probe. RTL and netlist, two runs each |
| Level-3 lockstep, 386, RTL | Passes. Its report equals main's (`ce67cbbc4`) in all 9,965 values except source and oracle-build digests: every count, trace hash and cycle figure is the same |
| Level-3 lockstep, 386, netlist | Passes; equal to main's in all 9,972 values except source, netlist and oracle-build digests. The seal's own full netlist lockstep (`--with-lockstep`) passes too |
| test386.asm, 386 | POST codes end at FFh. The report is identical to main's in all 707 values |
| Governor bench, 386 (`test_fes_i386_governor.py`) | Passes on the RTL and the netlist. The RTL report is identical to main's in all 1,178 values (`governor/`), so the regenerated governor ROM charges the 386 as before |
| Identity-off netlist | 29,548 flip-flop bits, the same as main's (`netlist/flops.txt`); cells 40,791 against 40,764; the digest differs (below) |
| DE10 seal of the 386 package | Registered producer `build_pc386_product.py --with-lockstep`, seed 1, Quartus 17.0.2, on chip-30x0-ai-dev, at `bf7afef73`: passes every analysis. BUILD_ID `2b929631b48aab40010415981dcb7e50`, package `1d09da27…`, RBF `c36accc8…`, 36,354 ALMs, setup +0.267 ns, hold +0.093 ns (`seal/`) |
| Kit run of that package | Kit B, run `k2044b`: pass. FreeDOS prompt, then Wolfenstein 3D in play; the kit was restored to its baseline ([the kit run](#the-kit-run)) |
| The head still builds the sealed design | `head_matches_seal.py` checks that the head's design identity equals the seal's |
| 486SX fit on the DE10 | At `bf7afef73`, 3 of 4 seeds close at 61.44 MHz in the seal's flow (seeds 2 to 4: setup +0.710, +0.438 and +0.337 ns). Seed 1, the producer recipe's seed, misses setup by 3 ps ([the 486SX fit](#the-486sx-fit)) |

**Review fixes after the first seal.** A review of the first sealed head
(`9aa6d0561`: seal `6de4bd49`, kit run `k2044a`, both passing) asked for
more #AC cases. Two of them found bugs:
- **MOVSD with a misaligned destination raised no #AC.** Only a st2 uop's
  first address was checked.
- **A delivery from CPL 3 onto an odd ESP0 hung.** #AC fired on the
  delivery's CPL-0 pushes.

`bf7afef73` fixes both, and adds 5 cases. Every gate above was then rerun
at `bf7afef73`: a new seal and a new kit run. The `9aa6d0561` evidence is
kept where it is still useful: `kit/kit-b-k2044a-result.json` and
`fits/9aa6d0561-*`.

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
  386 governor bench's report is unchanged. The rows have no bench at
  `I486 = 1`: `test_fes_i386_governor.py` builds the 386.
- **Lockstep.** `test_fes_i486_lockstep.py` runs both identities, on the
  RTL (`-GI486`) or with `--netlist` on the seal's netlist
  (`hierarchy -chparam`). Each identity has its own expected-stops list:
  `i486-expected-stops.txt` for the 386 and the new
  `i486-expected-stops-486sx.txt`. `fes_i486_cases.py` gains 29 cases:
  - XADD and CMPXCHG register aliasing and high-byte forms;
  - CMPXCHG to a read-only page and to a not-present page, checking that
    the flags and EAX are unchanged and that the error code has W set;
    XADD to a not-present page;
  - INVLPG on a remapped page, and on a not-present one;
  - #AC after POPFD sets AC and after it clears it, and for XADD, LFS,
    BOUND, SGDT and both of MOVSD's addresses at each alignment;
  - a delivery from CPL 3 onto an odd ESP0, which must not fault;
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
| Cells | 40,764 | 40,791 | 41,005 |
| Netlist SHA-256 | `57851047…` | `01cbcb4d…` | `b1fdb680…` |

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

| Commit | Seed | Closes | ALMs | Setup | Hold | Netlist |
| --- | --- | --- | --- | --- | --- | --- |
| `bf7afef73` (head) | 1 | **no**: setup −0.003 ns, slow 1100 mV −40 °C | 36,545 (87.2%) | −0.003 ns | — | `a029d7e1…` |
| `bf7afef73` (head) | 2 | yes, 61.44 MHz | 36,439 (86.9%) | +0.710 ns | +0.079 ns | `a029d7e1…` |
| `bf7afef73` (head) | 3 | yes, 61.44 MHz | 36,510 (87.1%) | +0.438 ns | +0.093 ns | `a029d7e1…` |
| `bf7afef73` (head) | 4 | yes, 61.44 MHz | 36,503 (87.1%) | +0.337 ns | +0.068 ns | `a029d7e1…` |
| `9aa6d0561` | 1 | yes, 61.44 MHz | 36,651 (87.5%) | +0.984 ns | +0.066 ns | `84a1f64e…` |
| `ec945043d` | 1 | yes, 61.44 MHz | 36,651 (87.5%) | +0.984 ns | +0.066 ns | `f815019f…` |
| `ec945043d` | 2 | yes, 61.44 MHz | 36,504 (87.1%) | +0.631 ns | +0.094 ns | `f815019f…` |

Each fit's summary is in `fits/`.

- **Earlier commits.** `ec945043d` differs from `9aa6d0561` only by a
  Verilator lint pragma, which moves the netlist's source line attributes.
  Seed 1 gives the same result at both.
- **At the head, after the #AC fixes, one netlist closes on seeds 2 to 4
  and misses by 3 ps on seed 1**, at one corner. The fits establish that
  and nothing more. The same seed closed by +0.984 ns on the previous
  commit's netlist.
- **What a 486SX package would need.** The pc386 recipe seals at seed 1,
  so a package built from this head would need another seed, or margin
  found elsewhere. No timing check was relaxed, and nothing was packaged.
- **The ALM comparison.** The 386 seal at head (the same flow, `I486 = 0`,
  seed 1) uses 36,354 ALMs. The 486SX fits use 36,439 to 36,545 at the
  head. Slice 1 showed that this flow cannot separate the delta from a
  netlist change's own noise, so that difference is not a measured cost.

## The kit run

Two runs, each of the then-current seal, on Kit B. Both used the
established core-only runner (`hil_s4_dos.py`, with the same claim,
baseline check and restore wrapper as the x87 slices' runs).

| Run | Seal | Package | When (EET) | Result |
| --- | --- | --- | --- | --- |
| `k2044b` | `bf7afef73`, BUILD_ID `2b929631` | `1d09da27…` | 21 Nov 12:41–12:43 | pass |
| `k2044a` | `9aa6d0561`, BUILD_ID `6de4bd49` | `11b50c98…` | 21 Nov 11:37–11:39 | pass |

**What ran.** Each package was packed as a canonical `.fcore`; its package
ID was checked on packing. It ran on the kit's baseline image `179affbb…`
and bundle `c9ae79e2…`, with no install and no reboot. It booted the
user-supplied FreeDOS drive `a6977555…` as a fresh library game.

**Result, both runs** (`kit/`).
- The FreeDOS prompt was detected in 0.6 s.
- Wolfenstein 3D reached floor 1 with graphics on screen (0.938 of the
  frame lit). After a walk, 0.562 of the frame had changed.

**Restore.** After each run the kit was back on the same image and bundle,
with the same boot ID, the agent idle and the lease free. The run's FogCast
data was removed. Both runs are logged in `kitB-STATUS.md`.

**Status-line errata.** `k2044a`'s claim line says "PR fes#2044": that is
the issue; the PR is #2045.

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
  - a kit run of a 486SX build;
  - the plan's per-slice kit gate on the 386 seal, DOOM `-timedemo demo3`
    and the acceptance games rechecked (the slice's Done list asked for
    FreeDOS and one game, which `k2044b` is).

  None of the first four was run, because no 486 package exists while the
  identity is a build parameter that every package leaves at 0.
- **The governor rows on a bench.** They are generated and checked
  (`fes_i386_governor.py --check`), but no bench runs at `I486 = 1`.
- **The 386 instruction set at `I486 = 1`** is exercised only by the 141
  directed rows. test386 and the level-3 suites ran on the 386 identity
  only, and no machine boot simulation was run at either identity.
- **Plan open questions 6 and 8.** The 386-microcode behaviours and a
  `queue32` profile stay the 386's.
