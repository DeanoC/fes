# x87 slice 6.3: the x87-internal reductions, method committed before any fit

Slice 6.3 continues the reductions of the slice 6 plan
([PR #2027](https://github.com/DeanoC/fes/pull/2027)) after the first
milestone ([PR #2037](https://github.com/DeanoC/fes/pull/2037)). Deano's
direction on 20 November 2026, relayed by the coordinating session:

- **Order:** x87-internal reductions first, largest measured saving first.
  FBLD and FBSTP move onto the engine, then `fes_x87_core` is de-pipelined
  (candidate C, approved). Devices and the CPU come after. Multiplier sharing
  (D8) is last.
- **Cycle trades:** measured cycle trades are allowed, but only on builds
  with the 387. Builds without it keep today's timing.
  - Every reduction here is inside `fes_i386_x87`, which every 387-off build
    (the DE10, which ties `x87_present` low) folds away. So these trades
    reach the 387-fitted DE25 build only, and no per-board parameter is
    needed.
  - A later device or CPU reduction that trades cycles is selected per board
    instead, through a parameter tied to the board in the way `x87_present`
    is.
- **Area bands are one-sided.** A band fails only on growth over its
  reference, never on a shrink.

This file was committed before any fit in it was queued. A later amendment is
dated and says what had been seen by then.

## The baseline

The baseline is **#2037's four head seeds** (its record, part 5): 42,565 /
42,599 / 42,583 / 42,570 ALMs, median 42,576.5, spread 34.

- **What was fitted:** `4f1546783`, the RTL head #2037 sealed, plus
  `identity-on.patch` (#2037's record), on A5EB013BB23BE4SCS with Quartus Pro
  26.3 on chip-30x0-ai-dev.
- **Elaboration:** by the pc386 lock's CUDA-slot Yosys through step 6.0's
  `elaborate.py`.
- **Fitting:** by `fit_i386_device.py` at 16.276 ns.

Every fit below uses the same flow. A build made after main has moved
re-elaborates main with the identity patch. If that netlist's digest equals
#2037's head netlist (`elab-head`), the baseline carries over. If not, it is
refitted, four seeds, before any comparison.

## Step 1: FBLD and FBSTP, bounded by ablation

**The ablation.** `ablation-bcd.patch` changes the #2037 head two ways:

- `bcd` is tied to zero;
- the four packed-decimal states (`T_BLD_IN`, `T_BLD`, `T_BST_IN`, `T_BST`)
  are guarded out.

So every FBLD and FBSTP path, including its registers (`bl_*`, `bs_*`), is
gone and nothing replaces it. This is a measurement, never a build.

**Positive control, before any fit:** `cpu_x87_bl_acc`, `cpu_x87_bs_bcd` and
`cpu_x87_bs_bin` are named in the head's netlist (66 references) and must be
absent from the ablation's. `cpu_x87_wm` must still be present.

**Four seeds, and the rule, fixed now:**

- The bound is the baseline's median minus the ablation's, taken against the
  larger of the two spreads.
- **Bound not larger than that spread:** moving FBLD and FBSTP cannot show a
  measurable saving. It is not built, and step 2 goes ahead.
- **Larger:** the move is designed to stay under the bound, built, and given
  its own four-seed A/B. That A/B is judged one-sided: it fails only if its
  median exceeds the baseline's by more than the larger spread.

## Step 2: de-pipelining `fes_x87_core`

**Why.** `fes_x87_core` is #1892's backend, built to accept one operation a
clock at 200 MHz. The envelope keeps one ESC instruction in flight, so its
throughput is never used. The machine runs at 61.44 MHz, and 6.0c found it
closes at 120 MHz on four seeds.

**What the de-pipelining spends.** It spends that clock headroom, and the
record says so: **raising the DE25 clock later needs the pipelining back.**
The fast forms' latency should fall, because the ~15-clock pipeline response
is what #1907 measured them losing to the sequencer.

**The bound.** The first scratch form keeps the core's datapath and collapses
its pipeline registers into combinational logic where the stage boundaries
allow. It is fitted four seeds against the baseline.

- **Passing timing at 61.44 MHz on every seed is part of the bound.** A form
  that misses is not a candidate, and no timing check is relaxed.
- **The same one-sided rule applies:** build only if the saving is larger
  than the larger spread.

The built form then has its own four-seed A/B, unit bench, lockstep, seal
and kit run. Its cycle change per form is reported at the census rates, as
#2037's was.

## Reporting, for every step

- **Area:** four seeds, against the baseline, one-sided.
- **Device and shell:** the gap to the 92% line (43,056) and to the device
  itself (46,800), for two shells, each subtracting the 651 ALMs of virtual
  pins:
  - the **8,720-ALM shell** of DE25 slice 3's first real board build;
  - the 8,400 estimate.

  The 8,720 shell was fitted with a 42,655-ALM design; that figure is taken
  from #2036's record and quoted as that record states it.
- **RAM blocks:** any change, against the 58 the real board has left.
- **Cycles:** the cost per form, at the census rates.

**The engine bench's vectors** cover the five exact forms #2037 moved, which
they did not yet. This closes the gap #2037's record left in its "RTL
against its model" row.

## Amendments

1. **20 November 2026, after one seed of the FBLD/FBSTP ablation had
   finished and before any step's verdict.** The shell figure changes to
   **7,728 ALMs**. That is DE25 slice 3's controlled four-seed pair (#2036,
   `docs/validation/2026-11-20-de25-pc386-shell-s3/pair.md`): a median board
   of 42,574.0 against a virtual-pin envelope of 34,845.5, over identical
   machine bytes.
   - **Added as it stands.** The pair measured the board less the envelope,
     and the envelope's 677 virtual-pin ALMs are inside it. So 7,728 is added
     to a virtual-pin fit as it is, with **no** 651-ALM subtraction: the
     board estimate is the fit's median plus 7,728.
   - **The 8,720 seed-1 split is withdrawn.** The 8,400 estimate is quoted
     beside 7,728 only for continuity.
   - **RAM blocks.** The same pair adds 170 RAM blocks for the shell (295
     against 125). The 387-on fits use 145, so a board would hold about 315
     of 358, about 43 free. Every RAM-block change is stated against that
     figure, not the 58 relayed earlier.
   - **No rule changes.** Nothing in a band or a rule changes.
2. **21 November 2026, after the three bounds (results.md) and before any
   fit of a built form.** Deano's decision, relayed by the coordinating
   session: the first DE25 pc386 shell ships with the 387 off, and 387-on on
   the DE25 is a later step once about 3,500 ALMs are found across the shell,
   the devices and the x87. What is built, and how it is judged:
   - **FBLD alone moves onto the engine.** FBSTP stays in the envelope: the
     engine's microprogram ROM cannot hold it, and no census title runs
     either form. The FBLD-and-FBSTP bound (416.5) over-counts FBLD alone;
     the built move is judged by its own fits, not by that bound.
   - **The fast lane becomes one registered stage**, the bound's form,
     behind a `PIPELINED` parameter on `fes_x87_fast` and `fes_x87_core`.
     `fes_i386_x87` builds it with 0. `fes_x87_pipe` keeps 1, since #1892's
     standalone backend needs the throughput. This spends the clock headroom:
     a later DE25 clock raise needs the pipelining back.
   - **One milestone, one PR, one reseal, one kit run**, so the two are fitted
     together as the head, four seeds, against the baseline. FBLD alone is
     fitted four seeds as well, so each change's share can be stated: FBLD's
     is the baseline less FBLD alone; the fast lane's is FBLD alone less the
     head.
   - **The rule, unchanged in kind:** each A/B fails only if its median
     exceeds the baseline's by more than the larger spread. Every seed must
     pass timing at 61.44 MHz; no timing check is relaxed.
   - **The baseline.** Main has moved since #2037: #2039 changes
     `pc386_status.sv` and `top.sv`. Main `b206f9aaf` with
     `identity-on.patch` is elaborated and its netlist digest compared with
     `elab-head`. If they differ it is refitted, four seeds, and that refit is
     the baseline. The figures are quoted against #2037's head as well.
   - **Reported beside the area:** the remaining 387-on gap on the board,
     with the 7,728 shell and the 8,400 estimate, and each changed form's
     clocks at the census rates.
