# x87 slice 4: the divide datapath, settled by measurement before any RTL

The model is `model/divide_exactness.py`, which runs the proposed datapath at
the RTL's real register widths against exact integer division over 67,832
cases — 27,832 constructed (both endpoints of all 128 seed intervals, exact
quotients at 24, 53 and 64 significant bits, Q·B±1, the extremes of the
significand range) and 40,000 random.

## The finding that decides the design

**Goldschmidt with 64-bit truncated intermediates floors at about 2^-61
relative error, however many iterations it runs.** Measured: 2^-15.8 after
one iteration, 2^-31.6 after two, then **2^-61.4 and no better** at three,
four or five. Every intermediate is the top 64 bits of the multiplier's
product, so each step truncates, and the truncation error stops the quadratic
convergence dead at the register width.

That is enough for a 24- or 53-bit result and **not enough for the extended
format**, which needs the 64-bit significand plus a guard and a round bit —
67 bits — and, more demandingly, needs them as an *exact truncation* with a
*true* sticky bit, because that is the interface slice 3's `round_pc()`
presents. A rounded reciprocal product cannot supply either.

**So Goldschmidt needs an exact tail, and that is not a departure from option
A.** The iterations still run on slice 3's shared 9-DSP multiplier with the
seeds in M10K, exactly as Deano decided on 13 November. What the tail adds is
a bounded remainder and a handful of compare-subtract steps on slice 3's
adder — no second multiplier, no dedicated divider, and no wide product.

## The datapath, and the numbers that size it

Goldschmidt produces `qhat`. The candidate is `C0 = qhat - E` where `E` is the
*measured* error bound, so `C0 <= T` by construction and the increment
`d = T - C0` lies in `[0, 2E)`. `k = ceil(log2(2E))` restoring
compare-subtract steps find `d` exactly and leave the exact remainder — which
is the true sticky bit, for free, as a by-product of the step that was needed
anyway.

| precision | iterations | multiplies | restoring steps | remainder width | exact |
|---|---:|---:|---:|---:|:--|
| PC 24 | 2 | 6 | 2 | 66 bits | yes |
| PC 53 | 3 | 8 | 2 | 66 bits | yes |
| PC 64 | 3 | 8 | 7 | 71 bits | yes |

Zero mismatches against exact integer division on every case at those
settings, result and sticky bit both.

Three things follow, and each is measured rather than argued:

- **Early termination at 24 and 53 bits is real and it is two multiplies
  wide.** PC 24 needs two iterations where PC 53 and PC 64 need three, so the
  saving is 2 multiplies — 8 clocks on the four-clock multiplier. The bench
  will assert the cycle count by precision control, because a correct result
  at PC 24 proves nothing about iterations saved. There is no saving between
  PC 53 and PC 64 in iterations; the saving there is 5 restoring steps.
- **A fourth iteration buys nothing at any precision.** The error bound is
  identical at three, four and five iterations — 49 units at PC 64 — which is
  the truncation floor and not a sampling artefact: it is the same bound on
  constructed and random cases alike.
- **The tail needs a 71-bit remainder, against slice 3's 67-bit adder.** That
  is the one width this slice changes, and it is four bits on one existing
  path rather than a new structure. The 131-bit product a naive back-multiply
  correction would need is *not* required: the remainder is bounded by `2E·B`,
  so only its low bits ever matter.

## What the seed table costs

One M10K table, 128 words of 10 bits, indexed by the divisor's bits 62:56 —
the shape the fit spike's `spike_x87.sv` used. The reciprocal-square-root
table for FSQRT is the second. Two small tables against the 13 M10K of the
spike's 17-block budget still unspent.

## Still to model before the RTL for them is written

- **FSQRT.** The same shape: `A - Q²` is bounded, so the low bits suffice.
  Cases to construct: exact squares, square±1, and both exponent parities.
- **FPREM and FPREM1.** The partial step is `floor(A·2^d/B)` with the exact
  remainder, which is the same primitive as the tail above, so it may not
  need a loop of its own. To be checked in the model, not assumed.
- **The host-versus-Bochs partial-step comparison**, which decides how far
  each partial step reduces. Not yet run.
