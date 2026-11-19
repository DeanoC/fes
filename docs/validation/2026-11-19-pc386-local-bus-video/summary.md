# pc386 local-bus video, 19 November 2026

Issue [#2005](https://github.com/DeanoC/fes/issues/2005), part of the i486
plan (tracker [#1962](https://github.com/DeanoC/fes/issues/1962)), following
slice 3 (#1999, PR #2000). The pass rules are in [method.md](method.md),
committed with the code (`58b782b9a`) before any kit run.

## Result

**An option, with the 486 presets defaulting to local-bus video.**

`fes.pc386.machine` word 7, bits 3:2, now chooses the governor's VGA memory
charge:

| Value | Video bus |
| --- | --- |
| 0 | The preset's own: ISA for presets 1–5, local bus for 6 and 7 |
| 1 | ISA |
| 2 | Local bus |

- **The local-bus charge** is 150 ns for each 32-bit dword an access touches,
  instead of 720 ns for each 16-bit half. The source is
  [derive_cycle.py](derive_cycle.py): the median of the Video Card FAQ's
  486DX2-66 VL-Bus Vidspeed rows. Method.md section 1 says why this is an
  option and what it costs.

**On Kit A, the 486 presets now match local-bus 486s.** Against the
local-bus median, before (ISA) and after (local bus):

| Preset | DOOM full screen | PCPBench |
| --- | --- | --- |
| 486-25 | 0.74 → **1.06** | 0.98 → 1.09 |
| 486DX-33 | 0.69 → **1.08** | 0.97 → 1.10 |
| 486-50 | 0.63 → **1.08** | 0.88 → 1.04 |
| 486DX2-66 | 0.55 → **1.04** (n 25) | 0.77 → 0.93 (n 24) |

Above 1 means faster than the median. Slice 3 found these gaps growing with
the clock; they are closed.

**50 MHz is still not reached, and neither is 66.7 MHz.** Slice 3's
`reach.py`, run unchanged, marks both "not reached". More benchmarks now
fall short:

| Preset | Benchmarks not reached, before | After |
| --- | --- | --- |
| 486-50 | 1 of 6 | 4 of 6 |
| 486DX2-66 | 3 of 6 | 4 of 6 |

TopBench is "not measurable" now, because its 20 MHz hold-out failed.

**Why the reach got worse.** With the large fixed ISA time gone, the time
that scales with the clock (A) dominates every benchmark, so the core's own
shortfall shows. B is the fixed time, and its share of DOOM `MAX.CFG` at
33.3 MHz fell from 49% to 17%. The core's shortfall:
- The governor bench's `branch` and `pusha` loops are beyond the RTL at
  50 MHz.
- `int` is also beyond it at 66.7 MHz.
- The debt does not bank time, so the RTL cannot catch up on those loops
  later.

**What the gain over local-bus 486s does not mean.** At 1.04–1.08 of the
median, the presets are slightly faster than real local-bus machines on
DOOM, and on TopBench's score they are 1.15–1.5 times the database median.
That is consistent with the 486 table charging every access as a cache hit
(slice 3, "What this record does not cover"). Nothing here was tuned to
those references.

## Host gates (method.md section 4)

1. **Generated files and unit tests: pass.**
   - `fes_i386_governor.py --check`.
   - `tests/test_fes_i386_governor.py` (new local-bus cases) and
     `test_fes_i386_reference_diagnostic.py`.
   - The runtime's `make test`.
   - FogCast `protocol`, `misterruntime`, `targetclient`, `httpapi` and
     `hostapi`.
   - mister-packages `go test` and `check-fixtures`.
2. **The governor bench, RTL and netlist: PASS**
   ([governor-bench.json](governor-bench.json)).
   - Every loop is charged exactly the table's clocks.
   - Presets 1–5 reach every loop.
   - 486DX-33 and 486DX2-66 run on the local bus by default.
   - 386DX-25 on the local bus (word 7 = 2) and 486DX-33 on ISA (word 7 = 1)
     are each charged exactly.
3. **Presets 1–5 match main: PASS** ([compare-main.json](compare-main.json),
   against `origin/main` `a14663bd9`).
   - Every loop's charge is equal under `max` and presets 1–5, except `vram`.
   - `vram`'s added store accounts for exactly the difference: 3,800,
     4,800, 7,200, 9,600 and 11,600 clocks for presets 1–5.
4. **Lockstep: PASS** ([lockstep.json](lockstep.json)).
   - On the RTL: `max`, then preset 6, preset 6 on ISA and preset 3 on the
     local bus, each `--same-as` max.
   - On the netlist: the same four runs. The seal's own netlist lockstep
     also passed.
5. **test386: POST FFh on the RTL.**
6. **The storage endpoint: PASS.** 255 exchanges in the pc386 persistence
   scenario, including word 7 accepted at 8 and refused at 0Ch and 10h.
7. **Seal: pass** ([seal-summary.json](seal-summary.json)).
   - **Build.** Quartus 17.0.2, seed 1, the CUDA toolchain slot, of
     `58b782b9a`, on chip-30x0-ai-dev from a clean detached worktree.
   - **Timing.** System clock 61.44 MHz. Setup +0.544 ns, hold +0.102 ns,
     recovery +5.915 ns, removal +0.548 ns, minimum pulse width +1.122 ns.
   - **Area.** 36,381 of 41,910 ALMs.
   - **Package.** `2f8e53c3…`, build `d268d876…`.
   - **Inputs.** All 143 pinned inputs hash identically at `ed6860219`.
     The later commits touch no pinned input.

## Kit run (method.md section 5)

Run `l1` used Kit A, core-only, from 15:24 to 16:35 EET
([kit-scores.json](kit-scores.json)).

- **Software.** Baseline image `179affbb` and bundle `c9ae79e2`, with no
  install, no image change and no reboot. The run was claimed and released
  in the status file. The post-run snapshot matched the pre-run one, and the
  agent was idle with a free lease.
- **Rows.** `max`, preset 6 at word 1 = 200, 250, 0 and 500, preset 7, and
  preset 6 at word 1 = 667.
- **Settings.** Every row wrote the seven settings, so word 7 = 0 and
  presets 6 and 7 ran on the local bus.
- **Reading.** Every score was read from the named HDMI grab. Each PCPBench
  pair is byte-identical in its crop, 32 s apart. TopBench was read from the
  `-tb-final` grab. There were no reruns.

| Preset | 3DBench 1.0 | 3DBench 1.0c (shown) | PCPBench | TopBench (Total µs) | DOOM `MAX.CFG` realtics | DOOM `MIN.CFG` realtics |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `max` | 76.9 | 757 | 13.3 | 304 (182) | 2,062 | 748 |
| 486-20 (6, 200) | 18.1 | 179 | 4.0 | 97 (518) | 7,194 | 1,980 |
| 486SX/DX-25 (6, 250) | 22.2 | 220 | 5.0 | 114 (438) | 5,971 | 1,630 |
| 486DX-33 (6) | 29.4 | 286 | 6.5 | 140 (341) | 4,681 | 1,284 |
| 486DX-50 / DX2-50 (6, 500) | 41.6 | 402 | 8.8 | 179 (273) | 3,516 | 981 |
| 486DX2-66 (7) | 52.6 | 504 | 10.3 | 209 (253) | 2,911 | 858 |
| 486DX2-66 by override (6, 667) | 52.6 | 504 | 10.3 | 210 (253) | 2,912 | 857 |

**Control.** The `max` row has no governor, so it shows whether the package
is otherwise faster or slower. Against slice 3's `k2` on the same kit:
- 3DBench, 3DBench 1.0c, PCPBench and DOOM `MAX.CFG` are identical.
- TopBench is 304 here against 307; its Total is 182 µs against 181.
- DOOM `MIN.CFG` is 748 realtics against 747.

The package is otherwise the same speed. Each before/after difference on
the 486 rows is the video charge.

**Clean-up.** Run `l1`'s after-run listing failed: ssh timed out during the
banner exchange. So its status line's "0 new FogCast data paths" was wrong,
and a note in the status file corrects it. Two claimed runs followed:
- **`l2`** listed the kit: 64 new paths, the 7 launches' game records and
  media deltas, and the staged disk.
- **`l3`** removed exactly those 64 paths through `kit_hil.clean` inside its
  lease.

## Reach and references (method.md section 6)

- **Scripts.** PR #2000's `reach.py` (blob `a411434b`) and `compare.py`
  (blob `a42ec32e`), run unchanged.
- **Outputs.** [reach.json](reach.json) and [comparison.json](comparison.json),
  with slice 3's `references.json` (blob `3a61ab41`).
- **Side by side.** [before_after.py](before_after.py) lays slice 3's
  results beside these in [before-after.json](before-after.json).

| Benchmark | B's share at 33.3 MHz, before → after | 50 MHz: excess (tolerance) | 66.7 MHz: excess (tolerance) |
| --- | --- | --- | --- |
| 3DBench 1.0 | 31% → 2% | +4.90% (3.08%): not reached | +9.42% (3.82%): not reached |
| 3DBench 1.0c | 36% → 10% | +1.90% (3.05%): reached | +3.64% (3.72%): reached |
| PCPBench | 20% → 10% | +5.79% (6.68%): reached | +15.26% (9.66%): not reached |
| TopBench Total | 55% → 14% | not measurable: 20 MHz hold-out 518 against 535 ± 13 | not measurable |
| DOOM `MAX.CFG` | 49% → 17% | +3.92% (2.06%): not reached | +6.42% (2.09%): not reached |
| DOOM `MIN.CFG` | 36% → 19% | +4.82% (2.21%): not reached | +12.58% (2.33%): not reached |

The hold-out passes on the other five benchmarks. Preset 7 and preset 6 at
66.7 MHz agree on every benchmark.

**Reference ratios.** Each is the kit over the median of real machines; DOOM
uses the median over the kit. n is the number of rows.

| Preset | DOOM full screen: ISA; local bus | PCPBench: ISA; local bus | TopBench score |
| --- | --- | --- | --- |
| 486-25 | —; 0.74 → 1.06 (3) | —; 0.98 → 1.09 (3) | 1.16 → 1.50 (3) |
| 486DX-33 | 0.99 → 1.54 (5); 0.69 → 1.08 (5) | 1.04 → 1.18 (5); 0.97 → 1.10 (5) | 0.84 → 1.15 (2) |
| 486-50 | 0.67 → 1.15 (1); 0.63 → 1.08 (2) | 0.88 → 1.05 (1); 0.88 → 1.04 (2) | 0.95 → 1.40 (4) |
| 486DX2-66 | 0.81 → 1.54 (2); 0.55 → 1.04 (25) | 0.90 → 1.09 (2); 0.77 → 0.93 (24) | 0.74 → 1.16 (10) |

## What this record does not cover

- **An ISA row on the new package.** The baseline runtime refuses a non-zero
  word 7, and the run was core-only. The governor bench charges the 486 ISA
  case exactly, and slice 3's run is the ISA side.
- **Read timing.** Local-bus reads are charged as writes.
- **The VGA ports' local-bus time.** They keep the ISA charge.
- **The bus clock.** It is not modelled: 25 MHz and DX-50 boards are charged
  33.3 MHz bus time.
- **Cache misses.** The 486 table charges every access as a hit, which the
  gain over real local-bus 486s reflects.
- **The DE25.**
