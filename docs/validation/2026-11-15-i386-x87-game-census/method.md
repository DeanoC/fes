# Method: what the games ask of an FPU, 15 November 2026

Committed **before** the runs this record reports. Tracker: the census issue
this record closes. The question: would a reduced-precision "game" x87 —
narrower than the 387's 64-bit significand — be worth building for the
titles pc386 actually runs?

This is an oracle-only measurement. No RTL, no kit, no fits.

## What I had already seen before writing this

Full disclosure, because this lane's rule is that a method is committed
before measurement and amendments are dated:

- The [12 November plan record](../2026-11-12-i386-x87-plan/summary.md) and
  its `census.jsonl`, measured on tree `2d895034c` with an uncommitted
  patch. I read its per-title table in full, including Quake at "24-bit
  97%, 64-bit 3%", Screamer at "53-bit 100%", Chris's benchmark at 64-bit,
  the exception-mask figures (3Fh almost everywhere, 3Ah for Chris's
  benchmark, 30h for Norton SI 8) and **DOOM's 11 retired ESCs**.
- Slice 2's [area record](../2026-11-14-i386-x87-s2/summary.md), including
  the ~967-ALM floor and its ablation table.

So the precision-control result is **not** a blind measurement. What is new
here is: a re-measurement on today's tree with the committed
`census.h`, the operand-width and significand-width columns the earlier
census did not have, the three first-acceptance titles it never ran
(Wolfenstein 3D, Ultima VI, Ultima VII: The Black Gate), and the call-site
attribution of Quake's non-PC=24 arithmetic.

## The negative control, restated

The brief asks for DOOM as a negative control that "should show no x87 at
all". **That is not the committed rule**, because the 12 November record
already shows DOOM retiring 11 ESCs with a 387 fitted — the
FNINIT/FNSTCW/FNSTSW detection probe, which a fixed-point engine still
runs. The committed control is:

> **DOOM must retire zero load, store, FXCH, arithmetic and transcendental
> forms.** Control forms (FNINIT, FNCLEX, FNSTCW, FLDCW, FNSTSW) and the
> 287/387 infinity test are expected and do not fail the control. Any
> arithmetic or any memory load or store of a real operand in DOOM's window
> means the instrumentation is wrong and nothing else in this record is
> reported.

This departs from the brief's wording and the departure is deliberate.

## What is counted

Every count comes from `machine_tb --census --x87 1` through
`pc386_census_summary.py`; no new oracle hooks beyond the keys listed
below. The census framework (`cores/fes-pc386/sim/census.h`) is already
committed and already counts ESC forms by mode, the control word by
arithmetic class, exception flags raised and unmasked, ES, #NM/#MF, WAIT,
the emulator fixup interrupts and the AT coprocessor ports. This record
adds, as committed instrumentation in the same header:

1. `x87cwform:<form>:<cw>` — the control word every retired ESC form ran
   under, not only the arithmetic classes. This is what gives precision
   control **and** rounding control per form, including rounding on the
   integer stores where it matters.
2. `x87cwsec:<second>:<cw>` — the control word by whole simulated second,
   which is "over time" as the brief asks.
3. `x87cwset:<form>:<old>><new>` — control-word writes, from
   `after87.control`, for FLDCW, FNINIT, FLDENV and FRSTOR, **including
   writes that leave the value unchanged**, so "does the title ever write
   the control word" is answerable directly rather than inferred from the
   value.
4. `x87sig:<form>:<bucket>` — the significand width in use. For each
   retired ESC, `64 - ctz(significand)` of ST(0) after the step, bucketed
   `le24`, `le53`, `le64`, and `zero` for a zero significand. This is the
   measurement that distinguishes "runs at PC=24" from "holds only values a
   24-bit significand can represent".
5. `x87pcsite:<pc>:<cs>:<page>` — for each retired arithmetic ESC, its
   precision control and the code page (CS and EIP>>12) it ran on, so a
   title's non-PC=24 arithmetic can be attributed to the engine or to a
   library or DPMI host.

Operand precision (m32 / m64 / m80) is **not** new instrumentation: it is
determined by the opcode and the ModRM reg field, which the existing
`x87op:<form>` keys already carry. The classification lives in
`pc386_census_summary.py` so that it is reviewable and testable:

| Form | Operand |
| --- | --- |
| D9.m0, D9.m2, D9.m3 | real m32 (FLD, FST, FSTP) |
| DD.m0, DD.m2, DD.m3 | real m64 |
| DB.m5, DB.m7 | **real m80** (FLD, FSTP) — the only explicit extended forms |
| DB.m0/m2/m3, DF.m0/m2/m3 | integer m32 / m16 |
| DF.m5, DF.m7 | integer m64 |
| DF.m4, DF.m6 | BCD m80 |
| DD.m4, DD.m6, D9.m4, D9.m6 | FRSTOR / FNSAVE / FLDENV / FNSTENV images |

FNSAVE and FRSTOR images are reported **separately** from explicit m80
operands, because a DPMI host's lazy x87 context switch produces them and
they are not the title relying on extended precision.

## The window

Each title runs its drive record's own launch and keys, on the same drive
the earlier census and the game-drive census used, for the drive record's
limit (20 s default; the earlier census's longer windows are kept where it
used them: Quake 25 s, the `fpu` bucket 30 s). Every claim in this record
is scoped **to that window**, and each row says what the window reached
(prompt, menu, or play), because a title that uses the 387 only in play is
undercounted by a launch window. That limitation is the earlier census's
too and is not fixed here.

## Calibration

Σ`x87op:*` must equal the D8–DF stem total in the same census file. I do
**not** commit equality with the 12 November run's 45,787,636 for Quake:
the tree has moved since `2d895034c`, and the committed `census.h` classes
DA's register forms and FPREM differently from that run's scratch patch, so
the totals are not required to agree. A difference is reported, not
treated as a failure.

## How a title is classified

For each title, from its own window's counts:

- **PC class**: `24`, `53`, `64`, or `mixed`, from `x87cwform` over the
  arithmetic forms (add, subtract, multiply, divide, square root — the
  forms precision control acts on). `mixed` whenever more than one PC value
  appears over those forms, with each value's count and its code pages.
- **Sufficiency of a 24-bit significand**: a title is **sufficient** only
  if *all* of these hold in its window: every arithmetic ESC ran at PC=24;
  no `x87sig` bucket above `le24` is non-zero; and no m80 load or store
  (DB.m5, DB.m7) executed. **Any arithmetic at PC above 24 with a count
  above zero, or any significand wider than 24 bits, makes the title not
  sufficient** — a small percentage is not sufficiency, and it is reported
  with its count and its sites rather than rounded away.
- **Relies on exceptions**: ES set at least once (`x87es`), or IRQ 13
  delivered, in the window. Unmasked-but-never-fired is reported as
  "unmasked, none fired", which is not reliance.
- **Probe only**: zero load, store, FXCH, arithmetic and transcendental
  forms.

A rule this method does not have, and will not acquire after the fact: there
is no threshold below which non-PC=24 arithmetic is ignored.

## A limit of this census, stated in advance

**Precision control is not fp32, and PC=24 sufficiency is about the
significand only.** Precision control narrows the significand to 24 or 53
bits; the x87's exponent field stays 15-bit extended at every PC setting,
and the register file stays 80-bit. So a title measured "sufficient" above
would be reproduced by a **24-bit-significand, 15-bit-exponent** unit, not
by an fp32 unit: an fp32 datapath would still diverge on any intermediate
outside fp32's exponent range, on overflow, on underflow and on denormals.
This census does not measure the exponent range of the values the titles
compute, and so **cannot** say whether an fp32 unit would reproduce any
title. Settling that would need the exponents of every intermediate
recorded, which this instrumentation does not do.

Likewise, PC=24 bounds the results of add, subtract, multiply, divide and
square root only. FILD, FLD m64, the constant loads, FRNDINT, FPREM,
FSCALE, FXTRACT and the transcendentals put values in registers under no
precision control at all. The `x87sig` buckets are what catch that, and
they are a measurement of the values observed in the window, not a proof
about values the title could produce.

## Amendments

None yet. Any amendment is dated here and says what had already been seen
when it was made.
