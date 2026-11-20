# x87 slice 6, step 6.0: the method, committed before any fit

Step 6.0 of the slice 6 plan, which is
[PR #2027](https://github.com/DeanoC/fes/pull/2027)'s
`docs/validation/2026-11-20-i386-x87-s6-plan/summary.md`, with Deano's
decisions of 20 November 2026. It measures and builds no RTL.
This file was committed before any fit in it was queued. The two netlists
were elaborated before it was committed, and elaboration is not a fit. A later amendment
is dated and says what had been seen by then. No band, rung or rule is
changed after a fit's output exists.

Three questions, one per part:

- **6.0a:** where current main sits against the 92% working line (D2).
- **6.0b:** where the ALMs are on Agilex 5, entity by entity. This gives
  candidates A–F of the plan a measured ranking (D8).
- **6.0c:** what clock the 387-fitted machine passes timing at on the DE25.
  This gives D3 data before de-pipelining (candidate C) is considered.

## What is fitted

- **Source:** `origin/main` at `8a162c536e8ecef245aeabbe837a856050c294a5` (#2024's merge), as a `git archive`.
- **Scratch edits:** the two edits #1907 and #1959 used, to
  `cores/fes-pc386/rtl/pc386_system.sv`: `x87_present = 1'b1`, and
  `.X87_BACKEND(1)` on the `fes_i386_cpu` instantiation. They are committed
  here as `scratch-edits.patch`, are not merged RTL, and their presence in the
  elaborated netlist is checked by reading it.
- **Fitted design:** `pc386_system` on A5EB013BB23BE4SCS, in the virtual-pin
  envelope of `scripts/fit_i386_device.py`, unmodified, with Quartus Pro 26.3
  at `/data/altera_pro/26.3` on chip-30x0-ai-dev.
- **Netlist:** the locked Yosys of the pc386 lock, through the runner's own
  `machine_netlist`, written once and reused by digest. Every fit records the
  digest of the netlist as elaborated and as fitted. **Both netlists are
  elaborated on chip-30x0-ai-dev by the pc386 lock's CUDA-slot Yosys**
  (`/data/fes-toolchain-cache`), not on powerboat as #1907's and #1959's
  were. powerboat's root filesystem was full on 20 November, and the
  runner refuses to elaborate with under 30 GiB free. So the netlist digests
  are not comparable with #1959's, which is one more reason the 6.0a
  comparison is labelled uncontrolled. The ROM is built by
  `scripts/build_pc386_firmware.py` on the same host, and its digest
  (`81562036…`) matches the pc386 seals'. `elaborate.py` in this directory
  is the command, and each elaboration's `elaboration.json` is kept. 6.0a and 6.0c share one flattened netlist, so
  between those fits only the seed and the constraint vary.
- **The pixel clock** stays at 74.25 MHz. Only the system clock moves.

## 6.0a: current main against the working line

Seeds 1 to 4 at 16.276 ns (61.44 MHz). Bands, verbatim from #1959's method,
per seed and then over the four:

1. The fitter refuses, or ALMs needed exceed the device's 46,800: **does not
   fit**.
2. Fits, and timing passes at all four corners on both clocks, on all four
   seeds: **fits and passes timing at 61.44 MHz**.
3. Fits, and timing fails at any corner on any seed: **fits but does not pass
   timing**. Not a pass. The miss is reported with the worst slack and its
   corner, and no timing check is relaxed.
4. In band 2, with ALMs needed at or above **92%** of the device (43,056) on
   any seed: **not established**. 92% is the working line Deano kept under
   D2. It is the DE10 lane's prior, not a measured Agilex 5 ceiling.

"ALMs needed" is the capacity figure. The LAB row is spread, not fullness, and
is reported without a band. The wording is "passes timing at", never
"closes", outside the ladder.

**The comparison with #1959** (42,943 / 42,956 / 42,986 / 43,010 ALMs, median
42,971, spread 67) is **uncontrolled**. Main has moved by #1998, #2003, #2022,
#2024 and other commits, so the base varies as well as the x87. The
difference of medians is reported, labelled so, and the larger of the two
spreads is printed beside it. No claim about any one PR's cost is drawn from
it.

## 6.0b: where the ALMs are

One fit, seed 1, 61.44 MHz, of a **hierarchy-preserving** netlist. It is the
runner's machine program with Yosys's `flatten` removed. The removal is
checked to happen exactly once, as `scripts/quartus_x87_spike.py --hierarchy`
does on the DE10. The record-local script `elaborate.py --hierarchy` writes the
netlist at the runner's fixed path. The runner then fits it with `--netlist`.
The per-entity table is read from Quartus Pro's own "Resource Utilization by
Entity" report in the kept project.

**Its rules, fixed now:**

- **The flat-against-hierarchy gate.** The hierarchy fit's total ALMs needed
  is compared with 6.0a's seed-1 fit of the flattened netlist. Within ±5%,
  the per-entity table is reported as the ranking. Outside ±5%, the
  hierarchy changed the design. The table is still reported, labelled
  **indicative**, and no candidate is ranked on it alone. On the DE10 the
  two flows agreed within about 2% (+1,608 flattened against +1,641
  per-entity, slice 2).
- **What it can name.** Yosys keeps module boundaries, so the table names
  `fes_i386_cpu`'s sub-entities, the machine's devices, `fes_i386_x87`,
  `fes_x87_core`, `fes_x87_trans`, `fes_i386_x87_mul` and so on. The CPU's
  own top-level logic is reported as one "own" figure: after the flattening
  it does not do, its registers carry anonymous names, so nothing inside it
  is apportioned.
- **No timing claim** is drawn from this fit. Keeping hierarchy changes
  optimisation across boundaries. Its slack is printed, not judged.
- **A ranking, not a saving.** An entity's ALMs bound what removing it could
  save. They are not the saving of any candidate. Each candidate's saving is
  measured later by its own controlled A/B (plan, 6.2).

## 6.0c: the clock ladder

The flattened netlist from 6.0a, seed 1, at these rungs of the system clock:

| Rung | MHz | Period (ns) |
| ---: | ---: | ---: |
| 1 | 61.44 | 16.276 (6.0a's seed 1; not refitted) |
| 2 | 75 | 13.333 |
| 3 | 90 | 11.111 |
| 4 | 100 | 10.000 |
| 5 | 110 | 9.091 |
| 6 | 120 | 8.333 |

The rungs come from the CPU's own Agilex 5 ladder (61.44, 75, 90, 110, 130
pass; 150 misses), capped below the CPU's 130 because the machine's
estimated worst-corner Fmax at 61.44 MHz has been 115–134 MHz (#1907,
#1959).

**The rules, fixed now:**

- A rung **passes** when the runner's timing gate passes: every corner, both
  clocks, setup, hold, recovery and removal, with each clock's own paths
  present.
- All six rungs are run at seed 1, whatever the lower rungs show. Each
  rung's ALMs needed is recorded, because a fit at a different constraint is
  a different fit with a different area.
- **The closing rung.** Take the highest rung R at which seed 1 passes and
  every lower rung also passed at seed 1. Seeds 2–4 are then run at R. If all
  three pass, the machine **closes at R** (four seeds). If any misses, seeds
  2–4 are run once more at the next lower rung, and the claim is that rung,
  if all pass there. The ladder then stops, and no further rung or seed is
  added. If seed 1 passes at a rung above a missing one, that is reported and
  is not a closure.
- 6.0c answers D3's question with data. It does not choose the clock. The
  choice stays Deano's.

## Running it

- **Host:** chip-30x0-ai-dev (16 cores, 23 GB). At most two fits run at
  once, as #1959 ran them. One fit takes about 16 minutes (#1959's seed 1:
  synthesis 3:20, fit 12:23).
- **Runtime cap:** the runner's default, 240 minutes per machine fit. A fit
  that hits it is reported as not measured within the cap.
- **Workspace:** `/data/projects/x87s6m`. The raw fit directories are kept
  there, and the record copies each fit's `fit.json`, `inputs.json`,
  `netlist.json`, `timing.json` and `timing.tsv`.
- **No GPU** is used. Quartus places and routes on the CPU. Nothing here
  touches a kit.
- **Total:** 4 (6.0a) + 1 (6.0b) + 5 (6.0c, rungs 2–6) + 3 to 6 (closure
  seeds) = 13 to 16 fits.

## Amendments

1. **20 November 2026, while the fits were queued and before any had
   finished. Nothing in a band, rung or rule changed.** In review (Codex,
   PR #2028), two things in the elaboration were corrected:
   - 6.0b named the elaborator `elaborate_hierarchy.py`. The committed
     script is `elaborate.py`, run with `--hierarchy`.
   - `elaborate.py` recorded the canonical SEAL recipe's program digest even
     when `--hierarchy` had changed the program. It now records that digest
     as `seal_recipe_program_sha256`, and the digest of the program Yosys
     actually ran (the kept `machine.ys`) as `executed_program_sha256`.
     Both netlists had already been elaborated by the first version. Their
     `elaboration.json` files have the executed digest added from the kept
     `machine.ys`, and the netlists are not re-elaborated.
