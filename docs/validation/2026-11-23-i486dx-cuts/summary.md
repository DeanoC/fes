# i486DX cuts, round 2: summary

Round 2 of the i486DX plan (plan record
`docs/validation/2026-11-22-i486dx-core-plan/`, PR #2146, tracker #2135).
It measures the cuts Deano approved on 23 November 2026, in Agilex 5 ALMs and
in DOOM, 3DBench and Quake CPI. The method was committed first, in
[method.md](method.md); the 3DBench window was fixed later by its dated
amendment 1, before any window ran. Every figure below comes from
[results.md](results.md), which [generate.py](generate.py) writes from the
committed evidence (`python3 generate.py --check`).

This record measures; it changes no RTL. The patches are scratch ablations
against main `dafabb6db`, and nothing here is a seal. The fits are
exploration fits of `fes_i386_cpu` in the virtual-pin envelope, not fits of
the DX machine.

## Decision (24 November 2026)

Deano, relayed by the coordinating session: **the DX takes `cuts`.** Slice 1
of the DX has started in its own session, so this record stays a record and
proposes no RTL.

## What was found

1. **`cuts` closes the budget first-order.** It saves 1,657 ALMs (median of
   three seeds; band 100) against the plan's 1,221 over the device, which
   leaves 436 to spare. The figure is a CPU-envelope saving set against
   machine figures, so it is first-order, and only a fit of the DX machine
   with four-seed timing settles the pass rule.
2. **The two 8 KiB L1s carry it.** `l12` saves 1,176 ALMs and 20 M20K
   (`l1i2` 553, `l1d2` 656). The x87 cuts add 136 (`x87`), and round 1's
   `lfwd` adds 360.
3. **What it costs in CPI.** The bench's memory model charges 18 clocks to
   port 0's first data against the kit's 11, so these losses are upper
   bounds:

   | Variant | DOOM h2 CPI / `base` | 3DBench CPI / `base` |
   | --- | ---: | ---: |
   | `l1i2` | 1.009 | 1.004 |
   | `l1d2` | 1.036 | 1.013 |
   | `l12` | 1.046 | 1.016 |
   | `cuts` | 1.058 | 1.016 |

   The governor bound keeps DOOM's DX2-66 and DX-33 presets for every one of
   them (`cuts`: 2,062 × 1.058 = 2,182 realtics, against 2,912).
4. **The x87 cuts are small.**
   - `xshift` saves 110 ALMs, just over its band of 100, so rule 2 calls it a
     saving. It also frees 9 DSP blocks.
   - `xdisp` saves −9, under noise. Its Fmax estimates (70.06, 63.42 and
     70.03 MHz) are not below `dx`'s lowest, 63.4.
   - `x87`, both together, saves 136.
   Quake was meant to price them in CPI and could not (point 6). The x87 unit
   bench and lockstep's x87 suites pass on all of them.
5. **`qhalf` fails rule B.** Lockstep's cycle-exact hold checks fail: three
   `after_shift` retirements and one `stack_stack` pair land 2 cycles apart
   where the checks expect 1. A Q that issues every other clock breaks them
   by construction. Every functional suite passes. `qhalf` was a CPI bound,
   never a fit, so no saving depends on it. Its CPI ratios (DOOM 1.475,
   3DBench 1.316) are reported with that label. They bound what front-end
   throughput is worth; they do not price a narrower front end, which needs a
   design.
6. **Quake is not measurable.** The `base` bring-up handed over to the RTL
   at the DOS prompt, then stopped on a stall at `00a7:0005256c`, which is
   Quake's FNINIT (`DB E3`). That was before the window, so under method C3
   every Quake cell is not measurable, and the x87 variants were not run on
   Quake.

## Not measured in this round

These cuts need a design rather than an ablation (method, "What each approved
candidate became"):

- a unified 8 KiB L1;
- a narrower front end;
- an iterative FMUL on the x87 fast lane;
- serial conversions on the x87 engine.

The divider is already radix-2, and the x87 register stack is already in
M20K. Since `cuts` closes the budget first-order, none of these is needed for
the fit, unless the DX machine's own fit says otherwise.

## Caveats

- **Exploration fits.** The netlists come from the i386 lock's Yosys at one
  thread. The Yosys fix (DeanoC/yosys#81) was not on main.
- **3DBench is not comparable with Phase 4.** It runs on the census `bench`
  drive, so only ratios within this round count, not the Phase 4 figures.
- **`cuts` and `l12` give identical 3DBench windows.** Every counter
  matches: in that real-mode window, load forwarding and the x87 change
  nothing.
- **Load forwarding and the latency bench.** No latency loop changes with it
  off, in this round or in round 1, so the bench does not exercise it.
