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

## The latency is fixed per precision, which is deliberate

The restoring steps are **not** an early-exit search. Each of the `k` steps is
one compare-subtract that either takes its trial or does not, and the
sequencer runs all `k` whatever the operands are, so a divide's latency is
**fixed by the precision control field alone**: 2 steps at PC 24 and PC 53, 7
at PC 64, on top of 6 or 8 multiplies. Nothing in the datapath is
data-dependent.

That is a choice and not an accident. A real 387's FDIV is quoted as a narrow
range rather than a data-proportional cost, so a data-dependent latency would
diverge from the part — and [#1837](https://github.com/DeanoC/fes/issues/1837)
is what that divergence sounds like one layer up: Ultima VI's lute is flat
because the governor's *relative* instruction costs are wrong and the game
calibrates a delay loop against the BIOS tick. A program that calibrates
against a divide loop, which FP-heavy titles do, would hear the same error. So
the governor gets a number per precision class rather than a distribution, and
that number is exact rather than a worst case.

The early termination at 24 and 53 bits is therefore a *fixed* saving selected
by PC, not an opportunistic one: 8 clocks of multiplier, deterministically.

## FPREM and FPREM1, settled on the host

`model/fprem_host.c` and `model/fprem2.c` execute FPREM (D9 F8) and FPREM1
(D9 F5) on the host's own x87 and read the status word after each step. The
host is the reference here: TestFloat's `rem` covers only FPREM1's IEEE
remainder, so FPREM would otherwise have none, and the host executes the real
instruction including its partial semantics rather than a single exact result.

What it settles:

1. **The completion boundary is exactly the plan's.** An exponent difference
   of **63 completes in one step** with C2 clear; **64 and 65 take two**. So a
   step returns a partial remainder with C2 set iff the difference is 64 or
   more, measured rather than read off the manual.
2. **A partial step consumes at most 64 quotient bits.** In steady state the
   result's exponent drops by exactly 64 a step (the diff-1000 case: 40 to
   reach alignment, then 64, 64, 64 …). The *number* of instructions a full
   reduction needs is data-dependent — 59, 119, 179, 239, 299 and 359 bits of
   difference took 1, 3, 4, 6, 9 and 10 steps — because cancellation shrinks
   the remainder unpredictably. That is architecturally visible and correct:
   software loops while C2 is set. It is not latency variation *within* an
   instruction.
3. **The quotient bits are `q = C0·4 + C3·2 + C1`**, the quotient's low three
   bits, confirmed over q = 0 to 9 for both FPREM and FPREM1. The 387 PRM's
   q2/q1/q0 naming maps to C0/C3/C1 in that order, which is easy to get
   backwards from the manual alone.
4. **FPREM and FPREM1 differ only in the quotient's rounding.** 3 mod 2 gives
   q = 1 (C1 set) for FPREM, truncating toward zero, and q = 2 (C3 set) for
   FPREM1, rounding to nearest — the 8087/287 rule against the IEEE one.
5. **The invalid and the special cases**, with the status word read directly:
   - `inf mod 2` → IE set, result the indefinite QNaN (C000…, exponent FFFF);
   - `2 mod 0` → IE set, the same indefinite QNaN;
   - `0 mod 2` → +0, **no exception at all**;
   - `2 mod inf` → **the dividend unchanged**, C2 clear, no exception. A
     divisor of infinity is not an invalid case and does not reduce.

Whether the partial step reuses the divide's tail primitive — the bounded
exact remainder and its compare-subtract steps — is the next thing to model.
It looks like the same primitive (`floor(A·2^d/B)` with the exact remainder)
and if it is, FPREM needs no loop of its own, which is a real area saving. It
will be checked in the model rather than assumed, because FPREM's exactness
and its C2 semantics are where a shared primitive would most plausibly fall
short.

## FSQRT, and the one place early termination is not worth taking

`model/sqrt_exactness.py` and `model/sqrt_per_pc.py` run the square root the
same way: Newton on the reciprocal square root (`y <- y(3 - a·y²)/2`) with a
256-word, 10-bit M10K seed indexed by the exponent's parity bit and the
radicand's top 7 bits, every intermediate the top 64 bits of the multiplier's
product, then the same bounded exact tail. The restoring step for a square
root needs no multiply at all: `(C + 2^i)² - C² = (C << (i+1)) + 2^(2i)`, two
shifts and an add on the existing adder.

39,522 cases — exact squares, square±1, both endpoints of all 128 seed
intervals, the significand extremes, both exponent parities, and 30,000
random. Zero mismatches, result and sticky.

Convergence, at the 66-bit extended target:

| iterations | multiplies | max error | restoring steps | remainder width |
|---:|---:|---:|---:|---:|
| 1 | 4 | 4.9 × 10^14 | 50 | 117 bits |
| 2 | 7 | 6.6 × 10^9 | 34 | 101 bits |
| 3 | 10 | **21** | **6** | **73 bits** |
| 4 | 13 | 11 | 5 | 72 bits |

Three iterations is the choice: the fourth costs 3 multiplies — 12 clocks — to
save one restoring step and one bit of register.

**And the per-precision grid says to take early termination on divide and not
on square root**, which is a measured trade rather than a preference:

| | PC 24 | PC 53 | PC 64 | widest remainder |
|---|---|---|---|---:|
| FDIV, iterations / steps | 2 / 2 | 3 / 2 | 3 / 7 | **71 bits** |
| FSQRT, iterations / steps | 2 / 2 | 3 / 2 | 3 / 6 | **110 bits** |

For divide, a precision-selected iteration count costs **nothing** in register
width — every precision's remainder fits the same 71 bits — and saves 2
multiplies, 8 clocks, at PC 24. Taken.

For square root it is the other way round. A square root's remainder is
`A·2^s - C²`, which lives at the *radicand's* scale and is bounded only
because `C²` tracks `A` closely. Targeting fewer result bits makes `C²` track
`A` less closely, so the bound grows: the remainder register goes from 73 bits
to **110** to save 3 multiplies at PC 24. That is 37 bits of register on a
shared path to save 12 clocks on the least common precision, with ALMs the
binding constraint on this phase — so FSQRT computes at the extended target
always and lets slice 3's `round_pc()` apply precision control, which it does
correctly because the result is an exact truncation with a true sticky bit.

So the plan's "early termination at 24 and 53 bits" is **implemented for
divide and measured-and-declined for square root**, with the number above
rather than silently omitted. It is a design choice inside the slice and the
figures are here to be overruled with.

## FPREM's partial step is the divide's primitive

Confirmed in `model/fprem_primitive.py` over 40,000 step evaluations with
exponent differences up to 300, for both the truncating and the
round-to-nearest quotient: zero violations. The remainder is exact and in
range in each of the three cases that have *different* ranges, which is the
part a single check would have got wrong — a partial step's remainder is the
running dividend in `[0, B)`, a completing FPREM's is in `[0, B)`, and only a
completing **FPREM1**'s is the IEEE `[-B/2, B/2]`. An earlier version of this
check applied FPREM1's range to partial steps too and reported 7,860
violations against a design that was correct; the violations were the
checker's.

**The measured partial quotient is up to 65 bits, not 64.** So `N × B` is not
quite one multiplier pass: it is one 64×64 product plus a single shifted add
of `B` for the top bit — the same arrangement the divide candidate already
needs for its bits above 63, not a new one.

With that, **FPREM and FPREM1 need no divider of their own.** What they add
over the divide tail is the quotient-bit capture for C0/C3/C1, the
round-to-nearest quotient for FPREM1, and the C2 decision on the exponent
difference.
