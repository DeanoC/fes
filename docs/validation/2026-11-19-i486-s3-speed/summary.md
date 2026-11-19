# i486 slice 3: 486 speed, 19 November 2026

Slice 3 of the [i486 plan](../2026-11-17-i486-plan/summary.md)
(tracker [#1962](https://github.com/DeanoC/fes/issues/1962), issue
[#1999](https://github.com/DeanoC/fes/issues/1999)). The pass rules are in
[method.md](method.md), which was committed (`1f68b68d5`) before any kit run.

**Result:**
- **No named preset is added.** A 486 at 25 or 50 MHz is preset 6 with the
  machine record's frequency override (word 1). On the RTL, preset 6 at
  word 1 = 667 runs every governor-bench loop in exactly preset 7's cycles and
  charges. On the kit the two agree within 1.8% on every benchmark. The slice
  changes no RTL, ABI or FogCast code.
- **Reach on the DE10 core (Kit A, seal of main `0c7af0728`), from
  [`reach.py`](reach.py):**
  - **486SX/DX-25 and 486DX-33:** these are the calibration points. The
    20 MHz hold-out passes on all six benchmarks, so the model they fit holds.
  - **486DX-50 / DX2-50:** marked **not reached**, on one benchmark of six:
    DOOM at `MIN.CFG` is 2.39% late against a 2.16% tolerance. The other five
    are reached.
  - **486DX2-66:** marked **not reached**, on three of six:
    - 3DBench 1.0: +5.30%, against a 3.94% tolerance;
    - PCPBench: +12.18%, against 9.89%;
    - DOOM at `MIN.CFG`: +7.26%, against 2.23%.
- **Against real 486s:**
  - **486DX-33 against ISA-VGA DX-33s:** PCPBench and full-screen DOOM are
    within 4%.
  - **Against local-bus (VLB or PCI) machines:** the presets are slower, and
    the gap grows with the clock. Full-screen DOOM is at 0.74 of the local-bus
    median at 25 MHz, 0.69 at 33 MHz, 0.63 at 50 MHz and 0.55 at 66 MHz.
  - **Why:** the governor charges ISA bus time for VGA memory under every
    preset. That fixed time is about half of the 486DX-33 DOOM run (B below).
- **3DBench 1.0c** shows three digits with no decimal point, and its
  reference column has one. How they relate is not settled, so that
  comparison is reported both ways and not judged.

## Override or named presets

[`gov486.py`](gov486.py) runs the governor bench's 19 loops on the RTL at
61.44 MHz ([governor-bench-486.json](governor-bench-486.json), PASS):

| Preset | Loops beyond the CPU | Worst error on the other loops |
| --- | --- | ---: |
| 6, word 1 = 200 (20 MHz) | none | 0.24% |
| 6, word 1 = 250 (25 MHz) | none | 0.26% |
| 6 (33.3 MHz) | none | 0.29% |
| 6, word 1 = 500 (50 MHz) | `branch`, `pusha` | 0.28% |
| 7 (66.7 MHz) | `branch`, `int`, `pusha` | 0.32% |
| 6, word 1 = 667 | the same as preset 7, cycle for cycle | — |

Every loop is charged exactly what the table gives. All eight preset IDs are
in use, so a named 486-25 or 486-50 would have widened the 3-bit field in the
RTL, the ABI and FogCast to add what word 1 already gives. Nothing is
superseded, so nothing is deleted.

## Package

[seal-summary.json](seal-summary.json):
- **Seal.** Quartus 17.0.2, seed 1, of main
  `0c7af07286508942e455dd2575997f8bc7a76250`. It was made on
  chip-30x0-ai-dev from a clean detached worktree, after a netlist lockstep
  run that passed.
- **Package.** `63fd394c…`, build `dfd9266d…`.
- **Timing.** System clock 61.44 MHz; setup +0.478 ns, hold +0.077 ns,
  recovery +6.466 ns, removal +0.422 ns, minimum pulse width +1.122 ns.
- **Area.** 36,450 of 41,910 ALMs.
- **Inputs.** At this PR's head, all 142 pinned inputs in its build summary
  hash identically. The PR changes only this directory.

## Kit scores

Run `k2` used Kit A, core-only, from 09:56 to 11:14 EET
([kit-scores.json](kit-scores.json)):
- **Kit.** Baseline image `179affbb` and bundle `c9ae79e2`, with no install,
  no image change and no reboot. The kit was claimed and released in its
  status file, and its post-run snapshot matched the pre-run one.
- **Disk.** `f82cc8ba…` (method.md). Sound Blaster at 220h, IRQ 7, DMA 1.
- **Reading.** Every score was read from the HDMI grab named in the JSON.
- **Run `k1` was stopped** during its first benchmark, before any reading.
  Its finish detector needed a 2% match against an older finished 3DBench
  screen and got 2.9%, so it would have polled for 40 minutes. The detector
  was fixed, and `k2` reran everything; nothing from `k1` is used.
- **Clean-up.** Run `k3` removed both runs' FogCast data (17 trees, 72 paths)
  inside its own lease, through `kit_hil.clean`. The find diff was exact, and
  the kit was logged FREE.

DOOM is the pack's shareware 1.9 `-timedemo demo3`: 2,134 gametics, with
fps = 74,690 / realtics.

| Preset | 3DBench 1.0 (fps) | 3DBench 1.0c (as shown) | PCPBench | TopBench (Total µs) | DOOM `MAX.CFG`, realtics (fps) | DOOM `MIN.CFG`, realtics (fps) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `max` | 76.9 | 757 | 13.3 | 307 (181) | 2,062 (36.22) | 747 (100.0) |
| 486-20 (6, 200) | 14.4 | 144 | 3.7 | 78 (645) | 9,713 (7.69) | 2,285 (32.7) |
| 486SX/DX-25 (6, 250) | 16.9 | 168 | 4.5 | 88 (563) | 8,533 (8.75) | 1,939 (38.5) |
| 486DX-33 (6) | 20.8 | 204 | 5.7 | 102 (490) | 7,290 (10.25) | 1,599 (46.7) |
| 486DX-50 / DX2-50 (6, 500) | 26.3 | 258 | 7.4 | 122 (403) | 6,074 (12.30) | 1,287 (58.0) |
| 486DX2-66 (7) | 30.3 | 295 | 8.5 | 134 (380) | 5,510 (13.56) | 1,165 (64.1) |
| 486DX2-66 by override (6, 667) | 30.3 | 295 | 8.5 | 134 (387) | 5,509 (13.56) | 1,167 (64.0) |

**Against the last run.** The 486DX-33 row is within 4% of the
[Phase 4 baseline](../2026-11-05-i386-phase4-baseline/summary.md)'s 486DX-33
row (Kit A, 5 November, an older seal before Phase 4's branch prediction):
- 3DBench 20.4, +2.0% here;
- PCPBench 5.5, +3.6%;
- TopBench 102, unchanged;
- DOOM 7,364 and 1,650 realtics, −1.0% and −3.1%.

## Reach

[reach.json](reach.json), produced by [`reach.py`](reach.py) exactly as
method.md fixed it. Codex review found the first version modelling presets 6
and 7 at 100/3 and 200/3 MHz, where the governor runs them at 33.3 and 66.7 MHz
(record units 333 and 667, as method.md states). The fix aligns the code with
method.md, and it changes no verdict. T is 1/fps, TopBench's Total µs, or realtics. A and B are
fitted from 25 and 33.3 MHz, with T(f) = A/f + B:

| Benchmark | B's share of T at 33.3 MHz | 20 MHz hold-out (measured, predicted, tolerance) | 7 against 6, 667 | 50 MHz: excess (tolerance) | 66.7 MHz: excess (tolerance) |
| --- | ---: | --- | --- | --- | --- |
| 3DBench 1.0 | 31% | 0.0694, 0.0703, ±0.0021: pass | pass | +3.00% (3.30%): reached | **+5.30% (3.94%): not reached** |
| 3DBench 1.0c | 36% | 0.0694, 0.0701, ±0.0021: pass | pass | +0.80% (3.29%): reached | +2.18% (3.89%): reached |
| PCPBench | 20% | 0.270, 0.269, ±0.016: pass | pass | +5.27% (7.05%): reached | **+12.18% (9.89%): not reached** |
| TopBench Total | 55% | 645, 636, ±15: pass | pass | −3.26% (2.48%): reached | +0.03% (2.66%): reached |
| DOOM `MAX.CFG` | 49% | 9,713, 9,780, ±198: pass | pass | +0.57% (2.03%): reached | +1.75% (2.05%): reached |
| DOOM `MIN.CFG` | 36% | 2,285, 2,280, ±48: pass | pass | **+2.39% (2.16%): not reached** | **+7.26% (2.23%): not reached** |

How to read the table:
- **Excess** is how much longer the preset took than a core that keeps up
  everywhere would. A positive excess beyond the tolerance means the core fell
  behind the 486 table somewhere in that benchmark.
- **The rule bounds only lateness.** TopBench at 50 MHz ran 3.26% *faster*
  than the model. The rule counts that as reached, and nothing here explains
  it.
- **B is the time that does not scale with the clock,** mostly the ISA bus
  time the governor charges for VGA memory and I/O. It is 49% of the DOOM
  `MAX.CFG` run at 33 MHz and 55% of TopBench's Total, where VidMem alone is
  about 250 µs at every 486 preset.

**Marks:**

| Preset | DE10 core | By benchmark |
| --- | --- | --- |
| 486SX/DX-25 | calibration point; hold-out passes | — |
| 486DX-33 | calibration point; hold-out passes | — |
| 486DX-50 / DX2-50 | **not reached** | DOOM `MIN.CFG` 2.39% late against 2.16%; five of six reached |
| 486DX2-66 | **not reached** | 3DBench 1.0, PCPBench and DOOM `MIN.CFG` late; three of six reached |

**What the marks agree with:**
- The 486-50 miss is narrow: 0.23 percentage points over the tolerance, on
  one benchmark.
- The governor bench's loops agree with both marks: at 50 MHz `branch` and
  `pusha` are beyond the CPU, and at 66.7 MHz `int` is as well.
- DOOM at `MIN.CFG` has the least screen to draw and the most CPU-bound
  share of the game loop, and it misses first.
- So does the
  [plan record's](../2026-11-17-i486-plan/summary.md#can-the-core-reach-a-486)
  derived ceiling of 40.9 MHz on `branch`.

## Against real 486s

[comparison.json](comparison.json), from [`compare.py`](compare.py) over
[references.json](references.json):
- [`refs.py`](refs.py) extracted the reference rows mechanically, with the
  source hashes recorded in the JSON.
- Each ratio is the kit over the median of real machines with the same CPU
  and clock. Above 1 means faster.
- DOOM compares realtics, so its ratio is median over kit.
- n is the number of rows in the median.

| Preset | PCPBench: ISA (n), local bus (n) | DOOM full screen: ISA (n), local bus (n) | TopBench score (n) |
| --- | --- | --- | --- |
| 486SX/DX-25 | — ; 0.98 (3) | — ; 0.74 (3) | 1.16 (3: 71, 76, 98) |
| 486DX-33 | 1.04 (5); 0.97 (5) | 0.99 (5); 0.69 (5) | 0.84 (2: 114, 130) |
| 486DX-50 / DX2-50 | 0.88 (1); 0.88 (2) | 0.67 (1); 0.63 (2) | 0.95 (4: 103–141) |
| 486DX2-66 | 0.90 (2); 0.77 (24) | 0.81 (2); 0.55 (25) | 0.74 (10: 109–218) |

Notes on the sources:
- **Ertl's DOOM list** is run with the screen two steps below maximum, which
  is not `MAX.CFG`. Its 486 rows are kept separate in the JSON:
  - 486-50 ISA: median 4,830 realtics, n 2, kit ratio 0.80;
  - 486-66 ISA: 4,668, n 1, kit ratio 0.85;
  - 486-66 local bus: 2,590, n 11, kit ratio 0.47;
  - 486-66 EISA: 3,023, n 1, kit ratio 0.55.
- **3DBench 1.0c** is reported both ways in the JSON and not judged. For
  example, at 486DX-33 the kit shows 204, and the reference medians are 17.4
  (ISA, n 5) and 24.8 (local bus, n 5).
  - The kit shows three digits with no decimal point. The database column
    has one.
  - On one launch, 3DBench 1.0 showed 14.4 where 1.0c showed 144, which fits
    a ×10 display.
  - No source or documentation for the program was found that says so.
- **No published 486 figure** was found in these sources for 3DBench 1.0 or
  DOOM at `MIN.CFG`.
- **The reference rows vary widely.** Full-screen DOOM on a DX2-66 runs from
  2,549 to 9,561 realtics across 29 rows, with L2 cache, chipset and the VGA
  card behind the spread. A ratio to the median says where the preset sits,
  not that it matches a particular machine.

**What it shows:**
- **At 33 MHz the preset is an ISA-VGA 486DX-33:** PCPBench at 1.04 and
  DOOM at 0.99 of the ISA median.
- **It is not a local-bus 486 at any clock,** and the gap grows with the
  clock. That is consistent with B: a fixed ISA charge that local-bus boards
  did not pay. The
  [plan record](../2026-11-17-i486-plan/summary.md#can-the-core-reach-a-486)
  named this effect; this record measures it on whole benchmarks.
- Whether a 486 preset should charge local-bus VGA time is a design question
  for Deano. It is not decided here.

## What this record does not cover

- **The DE25.** No DE25 build of pc386 exists.
- **The i386 latency bench.** It runs the RTL alone under `max`, has no
  preset, and has no real-486 counterpart.
- **Registered DOOM v1.9 `-timedemo demo3`.** No published real-486 figure
  was found.
- **Cache and queue effects of a real 486.** The 486 table charges every
  access as a cache hit (plan record, "What exists").
- **Any change to the governor, the presets or ISA charging.** None is made
  here.
