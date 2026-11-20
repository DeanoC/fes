# i486 slice 3, 486 speed: method

19 November 2026. Slice 3 of the [i486 plan](../2026-11-17-i486-plan/summary.md)
(tracker [#1962](https://github.com/DeanoC/fes/issues/1962)). This file is
committed before any kit run. Its rules do not change after the runs; a run
that cannot meet them is reported as failed or "not measurable", not re-ruled.

The gate, from the tracker: each 486 preset's scores are recorded against a
real-486 reference, and the presets the DE10 core cannot reach are marked.

## 1. Override or named presets

This was decided before the kit runs, from a host simulation.

`fes.pc386.machine` word 1 already overrides a preset's frequency, from 1 to
100 MHz in 100 kHz steps. The ABI defines it (`fes_computer.yaml`), FogCast's
`machine.frequency` carries it, and the governor applies it. The 3-bit preset
field has all eight IDs in use, so a named 486-25 or 486-50 preset would widen
the field across the RTL, the ABI and FogCast.

[`gov486.py`](gov486.py) runs the governor bench's 19 loops on the RTL at the
package's clock, under preset 6 (486DX-33) with word 1 at 0, 200, 250, 500 and
667, and under preset 7 (486DX2-66). It found:

- Preset 6 with word 1 = 667 runs every loop in exactly preset 7's cycles and
  charges. A 486 preset at an overridden frequency is therefore the same
  machine a named preset would be.
- Each loop is charged exactly what the table gives at every frequency.
- Loops beyond the CPU, at the RTL's `max`:
  - 20, 25 and 33.3 MHz: none of 19.
  - 50 MHz: 2, `branch` and `pusha`.
  - 66.7 MHz: 3, `branch`, `int` and `pusha`.

The report is [governor-bench-486.json](governor-bench-486.json).

**Decision:** 486 speeds other than 33.3 and 66.7 MHz use preset 6 with
word 1, and no named preset is added. The change set is this record; no RTL,
ABI or FogCast change is needed. It supersedes nothing, so nothing is deleted.

## 2. Presets measured

| Label | Record words 0, 1 | Governor | Role |
| --- | --- | --- | --- |
| `max` | 0, 0 | none | the core's own speed |
| 486-20 | 6, 200 | 486 table, 20 MHz | hold-out for the reach model |
| 486SX/DX-25 | 6, 250 | 486 table, 25 MHz | calibration point; reference class 486-25 |
| 486DX-33 | 6, 0 | 486 table, 33.3 MHz | calibration point; reference class 486-33 |
| 486DX-50 / DX2-50 | 6, 500 | 486 table, 50 MHz | reach verdict; reference class 486-50 |
| 486DX2-66 | 7, 0 | 486 table, 66.7 MHz | reach verdict; reference class 486-66 |
| 486DX2-66 by override | 6, 667 | the same as preset 7 | repeat check |

The governor charges integer time only, which is the same for a 486SX and a
486DX, so 486SX-25 and 486DX-25 are one preset. A DX-50 and a DX2-50 are also
one preset, because the governor does not model the bus clock.

## 3. Package

- **Seal.** Quartus 17.0.2 at seed 1, with the CUDA toolchain slot (which only
  selects the slot), made on chip-30x0-ai-dev from a clean detached worktree
  of main `0c7af07286508942e455dd2575997f8bc7a76250`. The netlist lockstep runs
  first, because the sealer requires it.
- **Timing.** The seal must pass the sealer's own gates. A miss is reported
  and stops the slice. No check is relaxed.
- **Inputs.** This PR changes only `docs/validation/`. Before the record is
  final, every input the seal pinned (`build-inputs.json`) is re-hashed at the
  PR head and must match. If one differs, the kit evidence does not count and
  the seal is redone.

## 4. Kit runs

- **Kit.** Either free kit (standing OK for pc386 work), following CLAUDE.md:
  - claim only when the status file's last entry logs it FREE;
  - check that it is on the image and bundle that entry names, and that the
    agent is idle with a free lease;
  - record owner, PR and start time in the status file;
  - run core-only, with no install, no image or boot change and no reboot;
  - check the kit afterwards against its pre-run snapshot;
  - log FREE only if the check passes; otherwise halt and leave the kit
    IN USE.
- **Clean-up.** The run's FogCast records and disk deltas are removed
  afterwards, under a new claim, and only those paths.
- **Disk.** `f82cc8badd90805b045e1055c917e21a4b69760955b9cc3a3e03a39015b0f554`
  (80/16/63), staged from Powerboat and not committed:
  - the `bench` game drive `e69a4cb6…` (`build_pc386_game_drive.py`, FreeDOS
    games base);
  - plus the DOS Benchmark Pack v1.5's `MARKS/3DBENCH2` and, from
    `MARKS/DOOMS`, the files `DOOM.EXE`, `DOOM1.WAD`, `MAX.CFG`, `MIN.CFG`
    and `DEFAULT.CFG`.

  The executables hash as in the
  [Phase 4 baseline](../2026-11-05-i386-phase4-baseline/kit-scores.json).
  `3DBENCH2.EXE` is `bd68a100…`. Guest memory is zeroed on load (#1499), so
  no scrub launch is made.
- **Launch.** One library launch per preset, with record words preset, word 1,
  0, 0, 220h, IRQ 7, DMA 1 (Sound Blaster as in the Phase 4 baseline).
- **Benchmarks, in order:**
  1. `3dbench`
  2. `3dbench2` (3DBench 1.0c)
  3. `pcpbench /vgamode`
  4. TopBench's Benchmarking screen
  5. `doom -config max.cfg -timedemo demo3`
  6. `doom -config min.cfg -timedemo demo3`

  Steps 5 and 6 are the pack's shareware DOOM 1.9: 2,134 gametics, with
  fps = 74,690 / realtics. `MAX.CFG` is full screen at high detail, the
  setting of the reference database's DOOM column.
- **Reading scores.** Every score is read from a kept HDMI grab of its
  finished screen, and the grab is named in `kit-scores.json`.
  - 3DBench: the value in its box.
  - PCPBench: two grabs at least 30 s apart showing the same value.
  - TopBench: SCORE and Total µs from a grab 15 s after its result screen
    appears.
  - DOOM: realtics.
- **Reruns.** A benchmark whose finished screen was not captured, or whose
  reading a missed Esc spoiled, is rerun once, in a new launch of the same
  preset. If it fails again it is recorded as "not run". A valid reading is
  never discarded or replaced, and no preset is rerun for its value.
- **Not run per preset, and why:**
  - The i386 latency bench (`test_fes_i386_latency.py`) runs the RTL alone
    under `max`. It has no preset and no real-486 counterpart.
  - Registered DOOM v1.9 `-timedemo demo3` has no published real-486 figure in
    the sources below.

## 5. Reach

Reach is measured on the kit and computed by [`reach.py`](reach.py) from
`kit-scores.json` alone. A summary sentence may not override its output.

- **Model.** Each benchmark's time-like value is one of:
  - 1/fps (3DBench, 3DBench 1.0c, PCPBench);
  - TopBench's Total µs;
  - DOOM realtics.

  A preset the core keeps up with throughout runs in T(f) = A/f + B. A is the
  table-charged work. B is the time that does not scale with the CPU clock:
  the ISA I/O and VGA memory time the governor charges as fixed time, and the
  timer.
- **Fit.** A and B are fitted from 25 and 33.3 MHz. The governor bench reaches
  every loop at both.
- **Tolerance.** tol(f) is the sum of three terms:
  - 2% of the prediction;
  - the prediction's worst-case error when each fit point moves by half its
    display quantum;
  - the measured value's half quantum.
- **Validity, per benchmark.** Each of these must hold:
  - the 20 MHz hold-out is within tol(20) of the prediction;
  - preset 7 and preset 6 at word 1 = 667 agree within 2% plus both half
    quanta;
  - A > 0;
  - none of those scores is missing.

  If any of these fails, the benchmark's verdicts are "not measurable".
- **Verdicts at 50 and 66.7 MHz (preset 7).** A preset is "reached" if
  T − prediction ≤ tol(f), and "not reached" otherwise.
- **A preset's mark.**
  - "Not reached" (the DE10 core cannot reach it) if any benchmark says not
    reached.
  - "Reached" if every benchmark says reached.
  - "Not measurable" otherwise.

  25 and 33.3 MHz are calibration points, and their only check is the
  hold-out. If the hold-out fails, the record says that 33.3 MHz may not be
  reached, and it does not claim either way.
- **Checked before any run.** Synthetic scores were tested before this file
  was committed:
  - a core that keeps up everywhere gives "reached";
  - a core that stalls at a 45 MHz-equivalent floor on DOOM gives "not
    reached" at 50 and 66.7 MHz;
  - a floor at 30 MHz fails DOOM's hold-out and gives "not measurable".

## 6. References

[`refs.py`](refs.py) extracts [references.json](references.json) mechanically
from three sources:
- the community DOS benchmark database
  ([CSV](https://docs.google.com/spreadsheets/d/0Aigi3Xn2U-_vdGtKeFpIU2pKWTl2N0J6UFh6Z0E2TWc/export?format=csv)),
  which has 3DBench 1.0c, PCPBench and DOOM at full screen;
- Anton Ertl's [DOOM list](https://www.complang.tuwien.ac.at/misc/doombench.html),
  run with the screen two steps below maximum, so it is not the same setting
  as `MAX.CFG`;
- the pack's own TopBench `DATABASE.INI`.

Which rows it takes:
- Intel and AMD 486SX, DX and DX2 at 25, 33, 50 and 66 MHz.
- Each row keeps its board, chipset, L2 cache and VGA bus.
- 3DBench 1.0 and DOOM at `MIN.CFG` have no published 486 rows in these
  sources. Those cells are reported as having no reference.

The comparison is reported, not gated. The governor charges ISA bus time for
VGA memory under every preset. Most of the 486 reference rows have local-bus
(VLB or PCI) VGA, so the record splits ISA rows from local-bus rows and gives
the kit score's ratio to each group's median. It also gives the row count and
range.

## 7. Outputs

- `kit-scores.json`: per preset, every score, its grab, the run and the kit.
- `reach.json`: the output of `reach.py`.
- `seal-summary.json`: the seal.
- `summary.md`: the table of scores against references, with each preset's
  reach mark.
