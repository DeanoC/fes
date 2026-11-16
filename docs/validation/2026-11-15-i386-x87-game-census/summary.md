# What the games ask of an FPU, and whether a reduced x87 is defensible

15–16 November 2026. Oracle-only: no RTL, no kit, no fits. Tracker:
[#1886](https://github.com/DeanoC/fes/issues/1886). Phase 6's plan is
[#1768](https://github.com/DeanoC/fes/issues/1768).

The question Deano asked: DOS games are almost always fp32 at most, so would
a reduced-precision "game" x87 — cheaper and faster than a correct one,
aimed at the actual use case — be worth building?

**The answer is no, and the deciding fact is not the one anyone expected.**

## Result

**Quake moves 80-bit extended values through memory.** Its 25-second
timedemo executes **47,161 `FLD m80` and 46,847 `FSTP m80`** — about 94,000
explicit extended loads and stores. That is not a title computing more
precisely than it needs; it is a title whose *data in memory* is in the
387's 80-bit format. A reduced-significand unit cannot round-trip those
values at all. There is no rounding argument to have, no "close enough",
and no precision-control setting that makes it work: the bytes in memory are
wider than the datapath.

Everything else that argues against a reduced unit is corroboration, not
load-bearing:

- **Coverage.** No title in the census is sufficient for a 24-bit
  significand. Quake runs 483,513 arithmetic ESCs at PC=64 beside its
  11,930,161 at PC=24, and **Screamer runs none at 24 at all** — 434,103 at
  PC=53 and 30,503 at PC=64. Screamer alone rules a 24-bit datapath out,
  and Screamer is a title that needs a 387 to boot at all.
- **Wide values in the registers.** Quake leaves a significand wider than 24
  bits in ST(0) on 165,520 steps (107,191 in `le53`, 58,329 in `le64`),
  Screamer on 131,491. Precision control bounds add, subtract, multiply,
  divide and square root; it does not bound `FILD`, `FLD m64`, the constant
  loads, `FRNDINT`, `FPREM`, `FSCALE` or the transcendentals, and the
  measurement shows those putting wider values in the file.
- **Precision control is not fp32.** PC narrows the **significand** and
  leaves the exponent 15-bit extended at every setting. So even for Quake's
  97%-at-PC=24 arithmetic, a *24-bit-significand* unit would be exact while
  an *fp32* unit would not: the exponent range, the overflow and underflow
  thresholds and the denormal handling stay at extended width. The premise
  "games are fp32, so build an fp32 FPU" does not hold even where the
  precision matches.
- **There is no fallback to degrade into.** The `i386dx+387` identity tells
  the guest a 387 is present, so there is nothing to trap to. The PS2 had
  fp32-only floating-point hardware *with* software emulation behind it, so
  default-precision code there ran slowly and could be profiled and fixed
  per title. A reduced unit here would not run Quake's extended traffic
  slowly — it would run it **wrong**, silently, wherever it is.
- **The area saving is mostly not there.** 1,110 of slice 2's 1,735 ALMs
  (64%) are measured not to shrink with the significand, and the exponent
  path inside the remainder does not shrink either.
  [area.md](area.md) has the figures and the bound.

**So there is no defensible reduced design.** Not a narrower one, not a
hybrid with a wide fallback, and not a per-title variant. The honest
statement is the blunt one: this idea is closed, and closing it cost a
census rather than a slice.

### What the earlier census could not have found, and why that matters

The 12 November census ran Quake and reported its mix in detail, but it
published its **top twelve forms by frequency**. `FLD m80` and `FSTP m80`,
at about 47,000 each, were below that cut — in a run of 32 million x87
instructions they are 0.15% each. So the single finding that settles the
question was invisible to a census that was otherwise correct and more
thorough than this one in several respects.

The lesson is worth more than the datum: **a census that reports a top-N by
frequency will miss exactly the low-frequency, high-consequence forms that
decide a design question.** 94,000 operations in 25 seconds is not rare in
absolute terms; it was rare only relative to the hot path. This record's
tables report **every** form that executed, and `census.jsonl` carries the
full histograms.

### A prediction that failed, and why the conclusion does not rest on it

Before the runs, the coordinating session and Deano registered a prediction
from the PS2's precedent ([method](method.md), "Predictions registered
before the runs"): that Quake's non-PC=24 arithmetic would be **diffuse** —
many library, startup and incidental code pages each with small counts,
because that is the shape the PS2 experience had.

**It failed.** Quake's PC=64 arithmetic is **concentrated**: 462,063
operations across 28 code pages, with **78.2% of them on one page**.

The irony is instructive and is the reason the record states it rather than
softening it. Concentrated is normally the *better* news for a hybrid — 28
sites is a set one could imagine special-casing, where a diffuse tail is
hopeless. **So the prediction failing made a hybrid look more feasible, and
the m80 traffic killed it anyway, on entirely different grounds.** The
conclusion does not depend on the prediction in either direction.

### What this census does not establish

- **Whether Quake's PC=64 code is its engine or its C runtime.** Every
  arithmetic site in the run, at both precisions, sits in the one flat
  selector `00a7`, with the PC=24 pages (`10010`–`10036`) and the PC=64 page
  (`1001a`) interleaved in a single code region. DJGPP links the engine and
  its libraries into one flat segment, so CS cannot separate them and the
  page numbers do not either. **Not established**, and a plausible
  attribution is not offered. Settling it would need Quake's own symbols, or
  a run instrumented to bracket its `Sys_LowFPPrecision` and
  `Sys_HighFPPrecision` calls.
- **What a 24-bit-significand unit would actually save.** That needs a fit
  of such a unit, and no fits are run here. [area.md](area.md) gives the
  measured floor and an upper bound instead.
- **What any title does beyond its launch window.** Each run is 20–30
  simulated seconds of launch, menus and, where it got there, play. A title
  that uses the 387 only later is undercounted, as it was in the earlier
  census.

## Secondary finding: what the mix tells slice 6

The opcode mix says which operations are worth optimising and which the
census titles never execute. Two figures were asked for by the slice 4
session while this census was running and are already in its record:

- **Divide early termination is worth taking.** Quake's divides are
  584,405 at PC=24 against 26,049 at PC=64, so the free 8-clock saving at
  low precision lands on the overwhelming majority of the hottest
  non-trivial FP operation in the corpus. Screamer's 5,839 divides are at
  PC=53, which also terminates early.
- **Square-root early termination is not.** Quake runs 758 square roots,
  **594 of them at PC=64** (78%), and Screamer's 575 are all at PC=53. The
  12 clocks it would save fall at the precisions where it saves nothing,
  and it would cost 37 bits of remainder width on every square root. The
  census confirms the decline.

More generally: transcendentals are 0.04% of Quake's x87 instructions and
1.11% of Screamer's, and FXCH is 20.09% of Quake's — the plan's instinct to
make FXCH a pointer swap rather than a data move is supported.

## The tables

Per-title: [tables.md](tables.md) — the opcode mix, precision and rounding
control with the verdict, the operand precisions, exceptions, FSQRT by
precision, and the calibration. The rows are
[census.jsonl](census.jsonl).

The verdict column is the summary script's `pc24_sufficient`, with each
failing reason and its count, so the claim is the script's output and not a
reading of the other columns. A title that only probes gets no verdict,
because sufficiency is a question about computation.

## Calibration and reproduction

Both decisive runs calibrate exactly: Σ`x87op` equals the run's D8–DF stem
total (Quake 31,997,318, Screamer 1,266,208).

**Screamer reproduces the 12 November run exactly** at 1,266,208 retired
x87 instructions, which is a useful check on both runs given the tree has
moved and the census code is a reimplementation.

**Quake does not, and the reason is known.** This run retires 31,997,318
against the earlier 45,787,636. The earlier census ran Quake from a
`bench-q` scratch copy with its own `Q.BAT`; this run uses the store's
`quakes` title on the pinned `bench` drive, which is a different launch.
The method says such a difference is reported rather than reconciled away,
and it is. Nothing in the conclusion depends on the total: the m80 counts,
the PC distribution and the verdict are all properties of the run that was
made.

## A tool fix the census needed

`machine_tb` refused Quake's launch outright — `quake +timedemo demo1
+exec mode0.cfg …` — with "cannot type that", because its typist had no
`+`. That is why the 12 November census ran Quake from a scratch copy with a
`Q.BAT` wrapper: the workaround was hiding a missing feature rather than
working around a drive problem. The typist now has the shifted characters a
DOS command line needs. `^` stays Ctrl and `~` stays Esc, so shift-6 and
shift-grave are still unreachable.

## Method

[method.md](method.md), committed before the runs, with six dated
amendments each stating what had been seen when it was made. It discloses
that the 12 November record's per-title precision table had been read before
any rule was written, so the precision result is **not** a blind
measurement; what is new here is this tree's re-measurement, the operand-
and significand-width columns, the three first-acceptance titles the earlier
census never ran, and the call-site attribution.
