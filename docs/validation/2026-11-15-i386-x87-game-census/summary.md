# What the games ask of an FPU, and whether a reduced x87 is defensible

15–16 November 2026. Oracle-only: no RTL, no kit, no fits.

**Status: the two computing titles are measured; seven runs are still in
flight, DOOM's negative control among them.** The method gates this record
on that control — if DOOM moves a real or integer memory operand, the
instrumentation is wrong and nothing here is reported. Until it has run,
every conclusion below is **provisional on it**. Tracker:
[#1886](https://github.com/DeanoC/fes/issues/1886). Phase 6's plan is
[#1768](https://github.com/DeanoC/fes/issues/1768).

The question Deano asked: DOS games are almost always fp32 at most, so would
a reduced-precision "game" x87 — cheaper and faster than a correct one,
aimed at the actual use case — be worth building?

**The answer is no, and the deciding fact is not the one anyone expected.**

## Result

**No title in this census could be reproduced by a 24-bit-significand
datapath, and the clearest case needs no interpretation: Screamer runs no
arithmetic at 24 bits at all.** Its 434,103 add, subtract, multiply, divide
and square-root operations are 424,703 at PC=53 and 1 at PC=64, with none at
PC=24 — and Screamer is a title that does not boot without a 387. A reduced
unit would be wrong for it from its first frame.

Quake is the other computing title and it fails too, though less starkly:
11,926,211 of its arithmetic operations run at PC=24 and **462,063 at
PC=64**. Both titles also leave significands wider than 24 bits in the
register file — Quake on 165,520 steps (107,191 `le53`, 58,329 `le64`),
Screamer on 131,491 — because precision control bounds add, subtract,
multiply, divide and square root and **does not** bound `FILD`, `FLD m64`,
the constant loads, `FRNDINT`, `FPREM`, `FSCALE` or the transcendentals.

Beyond coverage, three arguments stand independently of any count:

- **Precision control is not fp32.** PC narrows the **significand** and
  leaves the exponent 15-bit extended at every setting. So a
  *24-bit-significand* unit would be exact for Quake's PC=24 arithmetic,
  while an *fp32* unit would not: the exponent range, the overflow and
  underflow thresholds and the denormal handling stay at extended width.
  The premise "games are fp32, so build an fp32 FPU" does not hold even
  where the precision matches.
- **There is no fallback to degrade into.** The `i386dx+387` identity tells
  the guest a 387 is present, so there is nothing to trap to. The PS2 had
  fp32-only floating-point hardware *with* software emulation behind it, so
  default-precision code there ran slowly, and could be profiled and fixed
  per title. A reduced unit here would not run wide code slowly — it would
  run it **wrong**, silently, wherever it is.
- **The area saving is substantially smaller than the width suggests**, and
  part of what it cannot save is measured. [area.md](area.md) gives the
  figures, the one component that is isolated, and what cannot be separated
  without a fit of a narrow unit.

**So there is no defensible reduced design** — not a narrower one, not a
hybrid with a wide fallback, and not a per-title variant. The idea is
closed, and closing it cost a census rather than a slice.

### A headline this record withdraws

**Retracted, 16 November 2026, in place.** An earlier revision of this
record (commit `ac52d574d`) led with Quake's **47,161 `FLD m80` and 46,847
`FSTP m80`** and said that Quake "moves 80-bit extended values through
memory", so that a reduced significand could not round-trip data already
extended in memory. **That claim was wrong**, and the measurement that
refutes it is one this census had already collected and I had not looked at:
the significand widths of those very operations.

| Quake's m80 traffic | `FLD m80` | `FSTP m80` |
| --- | ---: | ---: |
| significand `le24` | 38,047 (80.7%) | 37,795 (80.7%) |
| significand `le53` | 2,516 | 2,543 |
| significand `le64` | 1,436 | 1,344 |
| zero significand | 5,162 | 5,165 |

**Four fifths of them carry values a 24-bit significand holds exactly.** The
near-equal load and store counts, and the width distribution, are the shape
of **register spills** — compilers of that era save x87 registers across
calls in the extended format whatever the value's precision — not of a title
relying on extended precision. A narrow unit would round-trip its *own*
spills exactly, because it would spill its own narrower values. So the m80
traffic is **not** an independent reason, and where it does carry wide values
(3,952 loads) it is the wide-significand reason restated.

The committed rule is unchanged: the method says any `FLD m80` or `FSTP m80`
makes a title not sufficient, and the script still reports it among the
verdict's reasons. **A rule is not rewritten because a result made it look
overstated.** What changes is the prose: this reason is weak and is no
longer led with, and the conclusion rests on coverage, the exponent range
and the missing fallback — each of which was already sufficient.

### The titles write the control word themselves

The brief asked whether a title ever writes the control word, and to report
the PC in force when arithmetic executes rather than the value at `FNINIT`.
Measured directly, from control-word writes rather than inferred from
values:

- **Quake writes it 148,000 times in 25 seconds**, and the dominant pair is
  `033f>0c3f` **25,735 times** with `0c3f>033f` **25,734 times** — the title
  switching **to** PC=24 and back **to** PC=64, about 2,060 round trips a
  simulated second. So Quake's PC=24 arithmetic is deliberate and bracketed,
  and its PC=64 arithmetic runs outside those brackets. It also makes
  rounding-mode changes (`033f>073f` and `033f>0b3f`, 10,644 each).
- **Screamer** writes it heavily too, consistent with the earlier census's
  61,006 `FLDCW`s, and switches rounding around its float-to-integer
  conversions.

This is what "the PC in force when arithmetic executes" means in practice:
neither title simply runs at whatever `FNINIT` left.

### A correction to this census's own classification

**Dated 16 November 2026**, after the first two runs and before the rest.
`FRNDINT` was classed as arithmetic and so entered the precision-control
distribution. It should not: `FRNDINT` rounds under **rounding** control and
**ignores** precision control (387 PRM; the plan record's "Accuracy"
section). Counting it inflated the PC=64 arithmetic figures this record
first published — Quake's to 483,513 from 462,063, and Screamer's to 30,503
from 1, the differences being exactly its 25,400 and 39,902 executions.

`FRNDINT` now has its own class, `round`: counted in the mix, absent from the
precision distribution, with a test. **Neither verdict changes** — both
titles were, and are, not sufficient. The figures in this record are the
corrected ones.

### What a full form table bought, and what a top-N census costs

The 12 November census published its **top twelve forms by frequency**.
Quake's `FLD m80` and `FSTP m80`, at about 47,000 each out of 32 million x87
instructions, are 0.15% each and fell below that cut.

The lesson is not the one the withdrawn headline suggested. What the full
histogram and the per-form significand buckets actually bought was the
ability to **test** the m80 claim and retract it. A top-N census would have
hidden the forms; a census with the forms but without their value widths
would have let a wrong and confident conclusion stand. **Reporting every
form that executed, and the width of the values each one moved, is what made
the error findable** — by me, before it reached a design decision, rather
than by a reviewer afterwards.

### A prediction that failed, and why the conclusion does not rest on it

Before the runs, the coordinating session and Deano registered a prediction
from the PS2's precedent ([method](method.md), "Predictions registered
before the runs"): that Quake's non-PC=24 arithmetic would be **diffuse** —
many library, startup and incidental code pages each with small counts,
because that is the shape the PS2 experience had.

**It failed.** Quake's PC=64 arithmetic is **concentrated**: 462,063
operations across 28 code pages, with **78.2% of them on one page**.

The irony is instructive, and is the reason the record states the failure
rather than softening it. Concentrated is normally the *better* news for a
hybrid — 28 sites, with one carrying four fifths of the traffic, is a set one
could imagine special-casing, where a diffuse tail would be hopeless. **So
the prediction failing made a hybrid look more feasible than predicted, and
the conclusion still does not change**, because what rules a hybrid out is
Screamer: a title with no PC=24 arithmetic at all, which no amount of
special-casing one hot page in a different title addresses. The conclusion
does not depend on the prediction in either direction — and it does not
depend on the m80 reading this record withdrew above.

And it should be said plainly that the PS2 precedent is still a sound
argument about *fallbacks*; it was only the distribution it predicted that
did not hold here. One failed prediction about one title's code layout is
not a reason to doubt the precedent that a reduced unit with nothing to trap
to is worse off than one with software emulation behind it.

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
