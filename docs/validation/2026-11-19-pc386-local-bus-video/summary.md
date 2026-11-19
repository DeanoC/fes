# pc386 local-bus video, 19 November 2026

Issue [#2005](https://github.com/DeanoC/fes/issues/2005), part of the i486
plan (tracker [#1962](https://github.com/DeanoC/fes/issues/1962)), following
slice 3 (#1999, PR #2000).
- **Pass rules.** In [method.md](method.md), committed with the code
  (`58b782b9a`) before any kit run.
- **Amendment 1.** A Codex P1 found that the EMS board's frame is also MMIO,
  and it must stay on ISA timing. After the fix, every gate, the seal and
  the kit run were made again on `ea0b87f0d`. This record reports those.
- **The superseded run.** The first package's run, `l1`, is kept in
  [kit-scores-l1.json](kit-scores-l1.json).

## Result

**An option, with the 486 presets defaulting to local-bus video.**

`fes.pc386.machine` word 7, bits 3:2, now chooses the governor's VGA memory
charge:

| Value | Video bus |
| --- | --- |
| 0 | The preset's own: ISA for presets 1–5, local bus for 6 and 7 |
| 1 | ISA |
| 2 | Local bus |

- **The local-bus charge** is 150 ns for each 32-bit dword an access to
  A0000h–BFFFFh touches, instead of 720 ns for each 16-bit half. The source
  is [derive_cycle.py](derive_cycle.py): the median of the Video Card FAQ's
  486DX2-66 VL-Bus Vidspeed rows.
- **What keeps the ISA charge:** I/O, and other MMIO such as the EMS board's
  frame.
- **Why an option:** method.md section 1 says why, and what it costs.

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
| 486DX2-66 | 3 of 6 | 5 of 6 |

All six benchmarks are measurable.

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

All gates are on `ea0b87f0d`, after amendment 1.

1. **Generated files and unit tests: pass.**
   - `fes_i386_governor.py --check`.
   - `tests/test_fes_i386_governor.py` (local-bus cases) and
     `test_fes_i386_reference_diagnostic.py`.
   - The runtime's `make test`.
   - FogCast `protocol`, `misterruntime`, `targetclient`, `httpapi` and
     `hostapi`.
   - mister-packages `go test` and `check-fixtures`.
   - The DE25 combined proof, re-run because its receipt captures two
     runtime headers this PR edits (14 cases).
2. **The governor bench, RTL and netlist: PASS**
   ([governor-bench.json](governor-bench.json)).
   - Every loop is charged exactly the table's clocks.
   - Presets 1–5 reach every loop.
   - 486DX-33 and 486DX2-66 run on the local bus by default.
   - 386DX-25 on the local bus (word 7 = 2) and 486DX-33 on ISA (word 7 = 1)
     are each charged exactly.
   - The `vram` loop's store into the EMS frame is charged the ISA cycle
     under every video bus.
3. **Presets 1–5 match main: PASS** ([compare-main.json](compare-main.json),
   against `origin/main` `a14663bd9`).
   - Every loop's charge is equal under `max` and presets 1–5, except `vram`.
   - `vram`'s two added stores account for exactly the difference: 5,800,
     7,200, 10,800, 14,400 and 17,400 clocks for presets 1–5.
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
     `ea0b87f0d`, on chip-30x0-ai-dev from a clean detached worktree.
   - **Timing.** System clock 61.44 MHz. Setup +0.570 ns, hold +0.099 ns,
     recovery +4.919 ns, removal +0.271 ns, minimum pulse width +1.122 ns.
   - **Area.** 36,391 of 41,910 ALMs.
   - **Package.** `17a424bb…`, build `0086f40a…`.
   - **Inputs.** All 143 pinned inputs hash identically at the PR head.
   - **Earlier seal.** The seal of `58b782b9a`, made before the amendment,
     passed too (setup +0.544 ns, package `2f8e53c3…`) and is superseded.

## Kit run (method.md section 5)

Run `l4` used Kit A, core-only, from 18:29 to 19:40 EET
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
| `max` | 76.9 | 757 | 13.3 | 301 (179) | 2,062 | 748 |
| 486-20 (6, 200) | 18.1 | 179 | 4.0 | 96 (516) | 7,195 | 1,980 |
| 486SX/DX-25 (6, 250) | 22.2 | 220 | 5.0 | 114 (442) | 5,972 | 1,629 |
| 486DX-33 (6) | 29.4 | 286 | 6.5 | 140 (358) | 4,682 | 1,285 |
| 486DX-50 / DX2-50 (6, 500) | 41.6 | 402 | 8.8 | 180 (290) | 3,516 | 981 |
| 486DX2-66 (7) | 52.6 | 504 | 10.3 | 210 (252) | 2,912 | 858 |
| 486DX2-66 by override (6, 667) | 52.6 | 504 | 10.3 | 209 (254) | 2,913 | 859 |

**Control.** The `max` row has no governor, so it shows whether the package
is otherwise faster or slower. Against slice 3's `k2` on the same kit:
- 3DBench, 3DBench 1.0c, PCPBench and DOOM `MAX.CFG` are identical.
- TopBench is 301 against 307; its Total is 179 µs against 181.
- DOOM `MIN.CFG` is 748 realtics against 747.

The package is otherwise the same speed. Each before/after difference on
the 486 rows is the video charge.

**Repeat.** `l1` ran on the package sealed before the EMS fix, and no
benchmark loads an EMS driver.
- 3DBench, 3DBench 1.0c and PCPBench are identical.
- DOOM is within one realtic.
- TopBench's score is within one, but its Total moved by up to 6% (341
  against 358 µs at 33.3 MHz). That benchmark's Total is the noisiest
  reading on the kit, and it is the one verdict that differs between the
  two runs (below).

**Clean-up.**
- **`l4`.** Its listing found 64 new paths: the 7 launches' game records and
  media deltas, and the staged disk. Claimed run `l5` removed exactly those
  through `kit_hil.clean` inside its lease. The kit's FogCast tree then
  matched its pre-`l4` listing.
- **`l1`.** Its after-run listing failed: ssh timed out during the banner
  exchange. So its status line's "0 new paths" was wrong, and a note in the
  status file corrects it. Claimed run `l2` listed the paths, and `l3`
  removed them, the same way.

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
| TopBench Total | 55% → 29% | +6.04% (2.73%): not reached | +8.95% (3.08%): not reached |
| DOOM `MAX.CFG` | 49% → 17% | +3.89% (2.06%): not reached | +6.42% (2.09%): not reached |
| DOOM `MIN.CFG` | 36% → 19% | +4.48% (2.21%): not reached | +11.99% (2.33%): not reached |

The 20 MHz hold-out passes on all six benchmarks. Preset 7 and preset 6 at
66.7 MHz agree on every benchmark.

**`l1` came out differently on TopBench only.** There its 20 MHz hold-out
failed, so TopBench was "not measurable", and 486DX2-66 was 4 of 6. Every
other verdict is the same in both runs, and so is each preset's mark.

**Reference ratios.** Each is the kit over the median of real machines; DOOM
uses the median over the kit. n is the number of rows.

| Preset | DOOM full screen: ISA; local bus | PCPBench: ISA; local bus | TopBench score |
| --- | --- | --- | --- |
| 486-25 | —; 0.74 → 1.06 (3) | —; 0.98 → 1.09 (3) | 1.16 → 1.50 (3) |
| 486DX-33 | 0.99 → 1.54 (5); 0.69 → 1.08 (5) | 1.04 → 1.18 (5); 0.97 → 1.10 (5) | 0.84 → 1.15 (2) |
| 486-50 | 0.67 → 1.15 (1); 0.63 → 1.08 (2) | 0.88 → 1.05 (1); 0.88 → 1.04 (2) | 0.95 → 1.41 (4) |
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
- **An EMS workload on the kit.** The EMS frame's ISA charge is checked on
  the governor bench only.
- **The DE25.**

**CI.** On this PR's earlier heads, `test_fes_hip_lane`'s CUDA-slot test
failed with "build record exceeds 65536 bytes", the Spectrum record's
closure (#1995). Main fixed the test in #2011, and CI, which tests the merge
with main, passes it from `90a9ff06` on. Run locally on this branch or on
`a14663bd9` without #2011, it fails the same way.
