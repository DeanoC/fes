# pc386 local-bus video, 19–20 November 2026

Issue [#2005](https://github.com/DeanoC/fes/issues/2005), part of the i486
plan (tracker [#1962](https://github.com/DeanoC/fes/issues/1962)), following
slice 3 (#1999, PR #2000). The pass rules are in [method.md](method.md),
committed with the code (`58b782b9a`) before any kit run, and in its three
amendments. Each amendment was written before the reruns it called for.

## Result

**The fallback: the 486 presets charge local-bus video, and the 286 and 386
presets keep ISA.** Each preset's video bus is fixed in `governor.toml`:

| Presets | Video | VGA memory charge |
| --- | --- | --- |
| 1–5 (286-12 to 386DX-40) | ISA, unchanged | 720 ns per 16-bit half |
| 6 and 7 (486DX-33, 486DX2-66), and their frequency overrides | local bus | 150 ns per 32-bit dword |

Both charges are less the 2-clock bus cycle the instruction table already
holds. I/O, including the VGA ports, and MMIO outside A0000h–BFFFFh, such
as the EMS board's frame, keep the ISA charge.

**Where 150 ns comes from.** It is the median of the Video Card FAQ's
486DX2-66 VL-Bus Vidspeed rows, computed in [derive_cycle.py](derive_cycle.py)
from period measurements. It was not fitted to any benchmark.

**Why there is no per-game option.** Deano's brief was "an option if
possible, if not charge local-bus video". The option was built and then
withdrawn (amendment 3):
- It persisted the choice in `fes.pc386.machine` word 7.
- An older runtime refuses such a game record after a software rollback,
  and kit HIL rolls back every run.
- A layout version bump would make that refusal clean, but the game still
  would not launch on the older runtime.

A per-game choice is left as a possible follow-up, with a layout minor bump.
Nothing in this change persists anything or touches the runtime, FogCast or
the ABI.

**On Kit A, the 486 presets now match local-bus 486s.** The kit over the
local-bus median, before (ISA) and after (local bus):

| Preset | DOOM full screen | PCPBench | TopBench score |
| --- | --- | --- | --- |
| 486-25 | 0.74 → **1.06** (n 3) | 0.98 → 1.09 (3) | 1.16 → 1.50 (3) |
| 486DX-33 | 0.69 → **1.08** (5) | 0.97 → 1.10 (5) | 0.84 → 1.15 (2) |
| 486-50 | 0.63 → **1.08** (2) | 0.88 → 1.04 (2) | 0.95 → 1.41 (4) |
| 486DX2-66 | 0.55 → **1.04** (25) | 0.77 → 0.93 (24) | 0.74 → 1.16 (10) |

- **How to read it.** Above 1 means faster than the median. TopBench's
  database does not split by bus.
- **What changed.** Slice 3 found the gap to local-bus machines growing with
  the clock; it is now closed.
- **Benchmarks without a reference.** Slice 3's references have no
  real-486 figures for Wolfenstein 3D, 3DBench 1.0 or DOOM at `MIN.CFG`, so
  those have no ratio.

**50 MHz is still not reached, and neither is 66.7 MHz.** Slice 3's
`reach.py`, run unchanged:

| Preset | Before (ISA) | After (local bus) |
| --- | --- | --- |
| 486-50 | not reached: 1 of 6 | not reached: 3 of 6, and TopBench not measurable |
| 486DX2-66 | not reached: 3 of 6 | not reached: 4 of 6, and TopBench not measurable |

**Why the reach got worse.** With the large fixed ISA time gone, the time
that scales with the clock (A) dominates every benchmark, so the core's own
shortfall shows. B is the fixed time, and its share of DOOM `MAX.CFG` at
33.3 MHz fell from 49% to 17%. The core's shortfall:
- The governor bench's `branch` and `pusha` loops are beyond the RTL at
  50 MHz.
- `int` is also beyond it at 66.7 MHz.
- The governor's debt does not bank time, so the RTL cannot catch up on
  those loops later.

**What the gain over local-bus 486s does not mean.** Being 1.04–1.08 of the
median on DOOM fits the 486 table charging every access as a cache hit
(slice 3, "What this record does not cover").

## Host gates (method.md section 4)

These run on the RTL of `1ff32d6dd`, which is also the PR head's RTL.

1. **Generated files and unit tests: pass.**
   - `fes_i386_governor.py --check`.
   - `tests/test_fes_i386_governor.py` (the local-bus cycle, dword counts,
     each preset's video) and `test_fes_i386_reference_diagnostic.py`.
2. **The governor bench, RTL and netlist: PASS**
   ([governor-bench.json](governor-bench.json)).
   - Every loop is charged exactly the table's clocks.
   - Presets 1–5 reach every loop.
   - 486DX-33 and 486DX2-66 charge VGA memory the local-bus cycle.
   - Every preset charges the EMS-frame store the ISA cycle.
3. **Presets 1–5 match main: PASS** ([compare-main.json](compare-main.json),
   against `origin/main` `a14663bd9`).
   - Every loop's charge is equal under `max` and presets 1–5, except `vram`.
   - `vram`'s two added stores account for exactly the difference: 5,800,
     7,200, 10,800, 14,400 and 17,400 clocks.
4. **Lockstep: PASS** ([lockstep.json](lockstep.json)).
   - On the RTL and on the netlist: `max`, then preset 6 (local) and preset 3
     (ISA), each `--same-as` max, on clean trees.
   - The seal's own netlist lockstep also passed.
5. **test386: POST FFh on the RTL.**
6. **Seal: pass** ([seal-summary.json](seal-summary.json)).
   - **Build.** Quartus 17.0.2, seed 1, the CUDA toolchain slot, of
     `1ff32d6dd`, on chip-30x0-ai-dev from a clean detached worktree.
   - **Timing.** System clock 61.44 MHz. Setup +0.345 ns, hold +0.069 ns,
     recovery +7.044 ns, removal +0.267 ns, minimum pulse width +1.122 ns.
   - **Area.** 36,395 of 41,910 ALMs.
   - **Package.** `ebe13bb8…`, build `828c86b0…`.
   - **Inputs.** All 143 pinned inputs hash identically at the PR head.
   - **Earlier seals.** The seal summary lists every earlier seal of this
     branch. One failed: the first main merge, at −0.198 ns on this PR's own
     governor path, which amendment 2 fixed. Every later one passed.

## Kit run (method.md section 5)

Run `l8` used Kit A, core-only, from 01:07 to 02:18 EET on 20 November
([kit-scores.json](kit-scores.json)).

- **Software.** Baseline image `179affbb` and bundle `c9ae79e2`, with no
  install, no image change and no reboot. The run was claimed and released
  in the status file. The post-run snapshot matched the pre-run one, and the
  agent was idle with a free lease.
- **Rows.** `max`, preset 6 at word 1 = 200, 250, 0 and 500, preset 7, and
  preset 6 at word 1 = 667.
- **Reading.** Every score was read from the named HDMI grab. Each PCPBench
  pair is byte-identical in its crop, 32 s apart. TopBench was read from the
  `-tb-final` grab. There were no reruns.

| Preset | 3DBench 1.0 | 3DBench 1.0c (shown) | PCPBench | TopBench (Total µs) | DOOM `MAX.CFG` realtics | DOOM `MIN.CFG` realtics |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `max` | 76.9 | 757 | 13.3 | 307 (187) | 2,062 | 747 |
| 486-20 (6, 200) | 18.1 | 179 | 4.0 | 97 (521) | 7,194 | 1,980 |
| 486SX/DX-25 (6, 250) | 22.2 | 220 | 5.0 | 114 (443) | 5,972 | 1,630 |
| 486DX-33 (6) | 29.4 | 286 | 6.5 | 140 (353) | 4,682 | 1,285 |
| 486DX-50 / DX2-50 (6, 500) | 41.6 | 402 | 8.8 | 180 (288) | 3,517 | 982 |
| 486DX2-66 (7) | 52.6 | 504 | 10.3 | 210 (248) | 2,912 | 859 |
| 486DX2-66 by override (6, 667) | 52.6 | 504 | 10.3 | 209 (257) | 2,912 | 859 |

**Control.** The `max` row has no governor, so it shows whether the package
is otherwise faster or slower. Against slice 3's `k2` on the same kit:
- 3DBench, 3DBench 1.0c, PCPBench, TopBench's score and DOOM `MAX.CFG` are
  identical.
- DOOM `MIN.CFG` is identical too, at 747 realtics.
- TopBench's Total is 187 µs against 181.

**Repeats.** Runs `l1`, `l4` and `l6` ran on earlier packages of this
change (the persisted option, with word 7 at 0). Their charges are the same,
and they are kept in `kit-scores-l1.json`, `-l4.json` and `-l6.json`. Across
all four runs:
- 3DBench, 3DBench 1.0c and PCPBench are identical.
- DOOM is within one realtic.
- TopBench's score is within one.
- TopBench's Total moves by up to 6%, which is why its `reach.py` verdict
  changes from run to run (below).

**Clean-up.** `l8`'s listing found 64 new paths: the 7 launches' game
records and media deltas, and the staged disk. Claimed run `l9` removed
exactly those through `kit_hil.clean` inside its lease, and the kit's
FogCast tree then matched its pre-`l8` listing. The earlier runs were
cleaned the same way, by `l2`/`l3`, `l5` and `l7`. `l1`'s after-run listing
failed (an ssh timeout), and the status file carries a correction.

## Reach and references (method.md section 6)

- **Scripts.** Slice 3's `reach.py` and `compare.py`, as merged with #2000
  (blobs `a411434b` and `a42ec32e`), run unchanged.
- **Outputs.** [reach.json](reach.json) and [comparison.json](comparison.json),
  with slice 3's `references.json` (blob `3a61ab41`).
- **Side by side.** [before_after.py](before_after.py) lays slice 3's
  results beside these in [before-after.json](before-after.json).

| Benchmark | B's share at 33.3 MHz, before → after | 50 MHz: excess (tolerance) | 66.7 MHz: excess (tolerance) |
| --- | --- | --- | --- |
| 3DBench 1.0 | 31% → 2% | +4.90% (3.08%): not reached | +9.42% (3.82%): not reached |
| 3DBench 1.0c | 36% → 10% | +1.90% (3.05%): reached | +3.64% (3.72%): reached |
| PCPBench | 20% → 10% | +5.79% (6.68%): reached | +15.26% (9.66%): not reached |
| TopBench Total | 55% → 23% | not measurable (repeat) | not measurable (repeat) |
| DOOM `MAX.CFG` | 49% → 17% | +3.92% (2.06%): not reached | +6.42% (2.09%): not reached |
| DOOM `MIN.CFG` | 36% → 19% | +4.70% (2.21%): not reached | +12.34% (2.33%): not reached |

**The checks behind those verdicts.**
- The 20 MHz hold-out passes on all six benchmarks.
- Preset 7 and preset 6 at 66.7 MHz agree on every benchmark but TopBench,
  whose Totals (248 and 257 µs) differ by more than the repeat rule allows.
  So TopBench is "not measurable" in this run.
- In runs `l4` and `l6` TopBench was measurable and "not reached" at both
  clocks. In `l1` it was "not measurable" through its hold-out.
- Each preset's mark is "not reached" in every run.

## What this record does not cover

- **A per-game choice of video bus.** It is the possible follow-up named
  above.
- **Read timing.** Local-bus reads are charged as writes.
- **The VGA ports' local-bus time.** They keep the ISA charge.
- **The bus clock.** It is not modelled: 25 MHz and DX-50 boards are charged
  33.3 MHz bus time.
- **Cache misses.** The 486 table charges every access as a hit, which the
  gain over real local-bus 486s reflects.
- **An EMS workload on the kit.** The EMS frame's ISA charge is checked on
  the governor bench only.
- **A Wolfenstein 3D benchmark.** No real-486 reference exists in slice 3's
  set.
- **The DE25.**
