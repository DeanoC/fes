# x87 slice 6, step 6.2, first milestone: the method, committed before any fit

The first 6.2 milestone of the slice 6 plan
([PR #2027](https://github.com/DeanoC/fes/pull/2027)), as Deano directed on
20 November 2026 (relayed by the coordinating session):

- move FPREM, FPREM1, FRNDINT, FSCALE and FXTRACT onto the DE25 backend
  (D5);
- then delete `X87_BACKEND = 0`, the sequencer arithmetic, with no shim
  (D6);
- judge the result on area, by
  [step 6.0's method](https://github.com/DeanoC/fes/pull/2028).

This file was committed before any fit in it was queued. A later amendment
is dated and says what had been seen by then.

## What the RTL says before anything is measured

At `X87_BACKEND = 1` the five forms do **not** use the deleted sequencer
arithmetic (the `A_LOAD`/`A_SUM`/`A_PROD`/`A_NORM`, `D_*` and `Q_*` arms, all
guarded out). They run on small arms of their own:

- **FPREM and FPREM1:** a doubling loop with a conditional subtract, one
  clock per quotient bit (`P_SH`, `P_FIN`, `P_ZERO`, `P_PACK`).
- **FRNDINT:** one shift, then a round and a pack (`A_INT`, `A_INT2`,
  `A_PACK`).
- **FSCALE:** one shift to truncate the scale, then an exponent add (`A_SC`).
- **FXTRACT:** one shift to normalise the exponent, then a pack (`A_XT`).

Each shift goes through `S_SHIFT`, the 9-DSP multiplier used as a shifter.
Loads, stores and the conversions use that path as well, so **moving these
five forms does not remove the multiplier**. The most a move can delete is:

- those nine states and their four `A_OP` branches;
- the registers only they write (`pr_*`, `xt_e`, and at backend 1
  `dv_rem`).

It then adds back whatever the backend needs to run the forms. The special
cases (zeros, infinities, NaNs, denormals, stack faults) stay in the
envelope, as they do for the transcendentals.

6.0b put the envelope's own logic at 4,408 ALMs, but it does not say how
much of that is these arms. So the first step bounds the saving before
anything is built.

## Part 1: bound the move by ablation

**The ablation.** One scratch edit on top of 6.0's
(`ablation.patch`, to `fes_i386_x87.sv`): `localparam bit S6_ABL =
X87_BACKEND == 1`. It guards the nine states and the four `A_OP` branches
named above with `!S6_ABL`, so at backend 1 the five forms' datapath arms
are gone and nothing replaces them. This is a measurement, never a
build: the five forms would hang.

**What is fitted.** It is 6.0's design exactly, plus `ablation.patch`:

- `origin/main` at `8a162c536`, with 6.0's `scratch-edits.patch`;
- elaborated on chip-30x0-ai-dev by the same CUDA-slot Yosys, through
  6.0's `elaborate.py`;
- fitted with `fit_i386_device.py` and Quartus Pro 26.3, at 16.276 ns,
  seeds 1 to 4.

**The baseline.** 6.0a's four seeds (43,008 / 43,038 / 43,059 / 43,080 ALMs,
median 43,048.5, spread 72) are the controlled baseline. The tree, the
host, the Yosys, the harness and the seeds are the same, and the
ablation patch is the only difference. The patch is diffed against 6.0's
tree before elaboration.

**Positive control, checked in the elaborated netlist before any fit:**

- `cpu_x87_pr_q`, `cpu_x87_pr_n`, `cpu_x87_pr_part`, `cpu_x87_pr_one`,
  `cpu_x87_xt_e` and `cpu_x87_dv_rem` are absent. In 6.0's flat netlist
  each is declared.
- `cpu_x87_wm` and `cpu_x87_sh_src` are still present.

If either check fails, the ablation removed something other than what it
claims. It is not fitted.

**The rule, fixed now.** Take the difference of medians (6.0a minus the
ablation) and the larger of the two arms' spreads.

- **Not larger than that spread:** the move cannot show a measurable
  saving even before its own cost is added. It is **not built**, and the
  coordinating session is asked once, with the number, because that
  departs from an explicit instruction.
- **Larger:** the difference is the move's ceiling. The move is built, its
  own controlled A/B is measured against 6.0a, and the move's design is
  chosen to stay under that ceiling.

## Part 2: whether the DE10 still folds the unit with backend 0 gone

Deleting `X87_BACKEND = 0` leaves one arithmetic backend: the DE25 one. The
DE10 package ships identity-off (`x87_present = 1'b0`) and has relied on
the unit folding away. **Whether the backend-1 logic folds as completely as
the sequencer did is not known.** `fes_x87_core` declares
`(* preserve, dont_retime *)` on its dispatch register (`fes_x87_core.sv:59`),
and the netlist recipe keeps attributes. So this is measured before
anything is deleted.

**Two fits**, both through `fit_i386_device.py --device de10` with the
locked Quartus 17.0.2, seed 1, 61.44 MHz, in the virtual-pin envelope, with
identity off (`x87_present = 1'b0`, as main ships):

- **D0:** main as it is, `X87_BACKEND = 0`.
- **D1:** main with `.X87_BACKEND(1)` on the `fes_i386_cpu` instantiation,
  and nothing else changed.

**The rule, fixed now.** D1 **folds** if all three hold:

1. its DSP and M10K counts equal D0's;
2. its ALMs needed are within ±200 of D0's (this lane's established
   fit-to-fit noise);
3. it passes timing at every corner.

If D1 does not fold, the deletion is **blocked**: it would grow the DE10
identity-off package. This is reported and nothing is deleted.

**On the DE25 the deletion is zero by construction.** Every arm it removes
is already guarded out at `X87_BACKEND = 1`. A null result there is the
expected result, not a failed reduction.

## Part 3: the gates for whatever is built

If Part 1 says build and Part 2 says fold, the move and the deletion land in
**one PR**, so the DE10 reseal and the kit run happen once. Its gates are
#1768's:

- unit bench and lockstep against the Bochs oracle, on the RTL and on the
  locked-Yosys netlist;
- test386 to POST FFh;
- a Quartus 17.0.2 identity-off seal of the DE10 package that passes every
  analysis, and a kit run on that seal;
- the DE25 four-seed fit by 6.0's method, against 6.0a.

**The census:** FSCALE executes in the census titles, and FRNDINT's
executions are in the census tables. For those two, each per-form cycle
count before and after is reported. FPREM, FPREM1 and FXTRACT execute in no
title and may be slow.

## Running it

The fits run on chip-30x0-ai-dev in `/data/projects/x87s6m`, the 6.0
workspace. The tree is a copy, `tree-abl`, with `ablation.patch` applied,
and the outputs are `abl-s1`..`abl-s4`. The DE10 fits come from two more
copies of the tree, `tree-d0` and `tree-d1`. All are sequential behind the
runner's host-wide Quartus lease.
