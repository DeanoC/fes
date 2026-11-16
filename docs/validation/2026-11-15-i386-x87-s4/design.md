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
2. **A partial step reduces `n = (expDiff & 31) | 32` quotient bits — 32 to
   63, not 64.** `model/fprem_step_size.c` prints the exponent difference
   before and after every step: differences of 119, 179, 239, 299 and 359
   reduce by 55, 54, 48, 43 and 42 and then settle on 32, where a fixed
   64-bit step would have reduced 64 each time and finished in half the
   instructions. **Bochs's `cpu/fpu/fprem.cc` at the pinned revision computes
   exactly that quantity** — `if (expDiff >= 64) { int n = (expDiff & 0x1f) |
   0x20; ... }` — so the host and the oracle agree bit for bit and lockstep
   can be exact on partial steps with no divergence to record.

   The exponent *drop* is `n` plus whatever the subtraction cancels, which is
   what made a fixed 64 look plausible: at difference 999 the drops are 40
   then 64, 64, 64, and those are `n` = 39 then 63, 63, 63 with one bit of
   cancellation each. Reading the drop as the step size is the mistake, and
   this record made it before the per-step probe was written.

   **An earlier version of this section claimed 64 bits a step and a 65-bit
   partial quotient needing a shifted add of `B`. Both are withdrawn.** The
   quotient is at most 63 bits, so `N × B` is one 64×64 product and nothing
   else; the 65 bits were an artefact of the first model shifting by a fixed
   64 instead of by `n`.

   The number of *instructions* a full reduction needs is still
   data-dependent — 59, 119, 179, 239, 299 and 359 bits of difference took 1,
   3, 4, 6, 9 and 10 steps — because cancellation shrinks the remainder
   unpredictably. That is architecturally visible and correct: software loops
   while C2 is set. It is not latency variation *within* an instruction.

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

## FSQRT

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

| precision | iterations | multiplies | restoring steps | remainder width |
|---|---:|---:|---:|---:|
| PC 24 | 2 | 7 | 2 | 28 bits |
| PC 53 | 3 | 10 | 2 | 57 bits |
| PC 64 | 3 | 10 | 6 | **73 bits** |

A fourth iteration costs 3 multiplies — 12 clocks — to save one restoring step
and one bit of register, so three is the choice at PC 53 and PC 64 and two at
PC 24.

### A withdrawn section, and why it was wrong

**An earlier version of this record declined early termination for FSQRT, on
the ground that it widened the shared remainder register from 73 bits to 110
to save 12 clocks. That measurement was wrong and the decline is withdrawn.**
Early termination is taken for the square root as well as the divide.

The error was in the model, not the hardware. It took the PC target as
`isqrt(A·2^s) >> drop` and then measured the remainder against the
**full-width** square, so the remainder it reported was the full-precision
one. `isqrt(n) >> d` is not `isqrt(n >> 2d)`: the right target is
`isqrt((A·2^s) >> 2·drop)`, with the sticky bit taken as the remainder being
non-zero **or** the radicand bits shifted out being non-zero. Scaled that
way, a lower precision makes the remainder *narrower* — 28 bits at PC 24
against 73 at PC 64 — which is the opposite of what the record claimed, and
obvious in hindsight: a shorter result is compared against a shorter radicand.

So the widest remainder this slice needs is 73 bits, set by PC 64, and
precision-selected iteration counts cost **nothing** in register width for
either operation. The decline, and the whole ledger of 37 bits against 12
clocks that justified it, are withdrawn.

Two things were reported to the coordinating session on the strength of that
wrong number, and both are corrected here: that FSQRT would compute at the
extended target always, and that slice 6 might reconsider it. There is
nothing to reconsider.

**A figure this record quoted and has now withdrawn.** It said "164 of
68,626 square roots are at PC 24 — 0.24%, with Chris's 3D Benchmark's 67,292
all at PC 64". **That figure is not derivable from anything in this tree.**
Codex could not reproduce it; a repo-wide search finds `68,626` only in this
file. It came to this session as a figure in a message, from a later census
run whose artifact is not committed here — which is a claim resting on
evidence nobody else can see, and exactly what this record spends a section
warning about.

**Re-derived from the committed census instead**, by
`model/census_sqrt_pc.py` over
`docs/validation/2026-11-12-i386-x87-plan/census.jsonl`, so these are checker
output rather than recollection:

| class | total | PC 24 | PC 53 | PC 64 | PC 24 share |
|---|---:|---:|---:|---:|---:|
| square root | 69,214 | **328** | 576 | 68,310 | **0.47%** |
| divide | 1,067,615 | **809,340** | 5,843 | 252,432 | **75.81%** |

So the shape of the conclusion survives and the numbers change: square roots
at PC 24 are **0.47%** of the corpus, not 0.24%, and about one in two hundred
rather than one in four hundred.

**A later census exists and is not in this tree.** #1887, the x87 game
census, is a separate and later run with different totals, recorded at
`docs/validation/2026-11-15-i386-x87-game-census/`. If a reader wants the
newer figures they belong there and should be cited there; this record cites
only what it can re-derive from what it ships beside.

The premise the figure was originally gathered to test is **gone** either way:
the decline was withdrawn because early termination for the square root costs
nothing, not because the saving was judged too small. So neither 0.24% nor
0.47% justifies a decline — both measure how little the early termination this
slice **did** take for FSQRT is worth in practice.

### What the census does make load-bearing

The same census, re-derived the same way, reports **Quake's 809,340 of
835,649 divides at PC 24 — 96.8%**, which is the 97% this record has been
quoting and is derivable. Corpus-wide the divide is 75.81% PC 24. So the
divide's two-iteration path — 8 clocks saved, free in area — lands on roughly
810,000 divides in one title.

**So the two early terminations this slice implements are at opposite extremes
of usefulness, and both were taken because both are free.** Divide's lands on
96.8% of Quake's divides and 75.8% of the corpus's; the square root's lands on
0.47% of its square roots. Neither was decided on that basis, and that is the
point: deciding them on area made the measurement unnecessary, where deciding
them on predicted usage would have got the square root wrong in the direction
that *felt* safe.

Worth recording as a prediction rather than hindsight: Quake running its
divides at PC 24 and its square roots mostly at PC 64 in the same program is
what you would expect if the engine sets PC 24 for its own fast math while the
C runtime's `sqrt()` runs at a control word the engine never reset. The census
is breaking the PC 64 call sites down by engine against library next. If that
comes back "library", the picture is consistent.

## FPREM's partial step is the divide's primitive

`model/fprem_primitive.py` runs the datapath **as it was first built** at the
step's variable scale — the Goldschmidt estimate, the fixed error bound and
the restoring tail, with `n = (expDiff & 31) | 32` — and compares against
exact arithmetic computed independently of it. 120,000 step evaluations,
exponent differences 0 to 399, both the truncating and the round-to-nearest
quotient: **zero mismatches**. Widest quotient 64 bits, widest remainder 64
bits, and 10 multiplier passes for one step.

**That is not the landed datapath, and this section describes the model rather
than the RTL.** The restructure replaced it: FPREM's quotient is never used
beyond its low three bits, so there is nothing to estimate, and `A_OP` reduces
the dividend once while `P_SH` takes `n` doublings with a conditional
subtract — no seed, no iterations, no candidate, no error bound, no divide
tail and no multiplier passes at all.
[The restructure](restructure.md#why-that-number-is-not-the-algorithms-cost)
has it. The model's run is kept because it is what established that the
remainders needed no divider *before* the better answer was found, and because
its 10-pass figure is the thing the landed path improves on.

An earlier version of this section called the model's form "the actual
datapath" after the RTL had moved. Codex found it.

## Clocks per form, derived and to be measured

The multiplier takes **4 clocks** a pass and a restoring step **1**. From the
pass counts above, for normal operands:

| form | precision | multiplier passes | steps | clocks (datapath) |
|---|---|---:|---:|---:|
| FDIV, FDIVR, FIDIV, FIDIVR | PC 24 | 7 | 3 | 31 |
| | PC 53 | 9 | 3 | 39 |
| | PC 64 | 9 | 8 | 44 |
| FSQRT | PC 24 | 8 | 3 | 35 |
| | PC 53 | 11 | 3 | 47 |
| | PC 64 | 11 | 7 | 51 |
| FPREM, FPREM1 | per step | 10 | 8 | 48 |

**These are derived from the model's pass counts, not measured from the RTL,
and they exclude the classify, operand-read, rounding and apply cycles the
unit already spends on every operation.** Two caveats that matter for slice 6:
a denormal operand adds pre-normalisation passes, so "fixed by precision
control alone" holds for normal operands; and FPREM's figure is per
*instruction*, with the instruction count for a full reduction
data-dependent. The unit bench measures clocks per form, precision and operand
class from the RTL, and those measurements supersede this table — which is
here so slice 6 has something to compare against the 387's published figures
rather than a derivation of its own.

## The constants are fixed, and validated on cases that did not choose them

Everything above measures `E`, the error bound, and `k`, the step count, as
maxima **over the same cases that then verify them**. That makes "zero
mismatches" partly true by construction: in the RTL `E` and `k` are
constants, and a case outside the sample that exceeded `E` would be a wrong
result, not a larger measurement.

So `model/fixed_constants.py` fixes them as the RTL will, rounded up to a
power of two, and validates on an independent seed:

| operation | precision | iterations | E | k | observed max \|T − q̂\| | headroom |
|---|---|---:|---:|---:|---:|---:|
| FDIV | PC 24 | 2 | 4 | 3 | 1 | 3 |
| FDIV | PC 53 | 3 | 4 | 3 | 1 | 3 |
| FDIV | PC 64 | 3 | 128 | 8 | 50 | 78 |
| FSQRT | PC 24 | 2 | 4 | 3 | 1 | 3 |
| FSQRT | PC 53 | 3 | 4 | 3 | 1 | 3 |
| FSQRT | PC 64 | 3 | 64 | 7 | 22 | 42 |

3,000,000 independent cases, 500,000 per operation and precision, on a seed no
fitting run used. **Zero mismatches**, result and sticky bit, and the worst
observed error stays well inside the bound everywhere — the tightest case is
PC 64 divide at 50 against 128.

**The widest remainder register is 74 bits**, set by FSQRT at PC 64 with these
constants, against 72 for the divide. That supersedes the 71 and 73 quoted
earlier in this record, which were computed from fitted bounds rather than the
constants the RTL will carry. One 74-bit register serves both operations.

The headroom is empirical and not a proof, so the RTL carries a **simulation-only
assertion** that the remainder after the tail is in range, and the unit bench
counts the cases that exercise it. That is what catches the RTL diverging from
this model — the model's own value class normalises more freely than the RTL's
registers will, which is exactly the kind of difference an assertion finds and
a comparison of results does not.

## How the remainder is actually formed, which the model does not show

The model computes `rem = A·2^(65+s) - C0·B` in Python's unbounded integers.
Both terms are about 2^129; only their difference is small. The RTL cannot
materialise a 131-bit subtract, and it does not have to: because
`|rem| < 2·E·B`, the remainder is **exactly represented by the low bits** of a
modular subtraction, with the sign in the top bit.

So the RTL keeps a **96-bit** remainder and forms it modularly:

- `A·2^(65+s) mod 2^96` is `A[30-s:0] << (65+s)` — only A's low 31 bits reach
  below bit 96, so the big shift is a wire selection and not a shifter;
- `C0·B mod 2^96` is `mul_p[95:0]` plus `((C0[66:64] · B)[31:0] << 64)`, since
  `C0` is 67 bits: the 64×64 product the multiplier already computes, plus
  three shifted adds of `B[31:0]` for the candidate's bits above 63;
- the restoring steps subtract `B << i` for `i < k`, at most 71 bits, which
  fits;
- the sticky bit is `rem != 0` over the whole 96 bits, which is exact because
  the true remainder is in range.

**So the register is 96 bits in the RTL, not the 74 the model's value bound
gives.** That is an inferred figure from the modular construction, not a
measured one, and the fit is what will price it. It is recorded here because
the 74 is the *value's* width and the 96 is the *representation's*, and
quoting the model's number as the hardware's would be the same mistake this
record has already made twice. The 74 remains the right figure for how wide
the remainder's value ever gets; nothing in the slice needs a 131-bit adder.

## What is in the branch

All three operations are built and exact: FDIV and FDIVR in every ST(i),
integer, memory and popping form, FSQRT, and FPREM and FPREM1. **740
unit-bench cases pass against the host's own x87 on the RTL and 740 on the
locked-Yosys netlist**, with the tail's remainder assertions armed.

**The decode landed per operation with the unit that runs it**, not in one
commit, and it was written and reverted once on purpose. Adding `X87_SQRT` or
`X87_PREM` to `x87_decode` un-stops the instruction — `fes_i386_decode` gates
on `!= X87_UNIMPL` — so a branch with the decode and without the unit's
handling would accept FSQRT with nothing to execute it and break lockstep
rather than leave it cleanly not-built. This is worth stating because it is
the kind of thing a later reader tidies into a single decode commit.

`x87_decode` is also not the only table a form has to be in. **`x87_format`
has to map it too**: `fes_i386_decode` takes the format first and turns the
operation back into `X87_UNIMPL` where it is `X87F_NONE`, so a memory divide
accepted by one table and missing from the other raises
`FAULT_UNIMPLEMENTED` on the real CPU. The unit's bench cannot see that,
because it supplies the format itself. Codex found it on PR #1889, and the
`div-mem` lockstep program is what catches it now.

**The seal and the kit run are done, and QEMU's fprem test is ported and
passes.** This section said otherwise in the present tense long after
[the results table](summary.md#results) recorded all three — which is the
first failure mode in this record's own taxonomy, in this record, for the
second time. The table is the single place those states are kept; this
section defers to it rather than restating it.

What is genuinely outstanding: the identity-on seed sweep, which has nothing
to sweep because the fitter stopped before placement, and
[the escalation](STOP-RULE-FIRED.md), which is **resolved** as Deano's phase
decision rather than open.

## Three more census results, which bear on what this slice built

From the completed nine-title census, recorded here because they change how a
later slice should read this one:

- **71 of the 162 x87 forms were executed by no title at all.** They must be
  **correct** but need not be **fast** — so the per-form clock table above is
  not uniformly load-bearing, and slice 6 should know which rows a title
  actually reaches before it spends anything making one quicker.
- **Three titles run with exceptions unmasked** — Chris's 3D Benchmark over
  15,236,298 instructions, Wolfenstein 3D over 13,598, Quake over 139 — and
  **none raised one and none set ES.** So no title *relies* on the exception
  path, though it is live state throughout.
- That makes this slice's two new denormal rules — that a denormal operand
  does not raise DE beside the operation's own divide-by-zero or invalid,
  whether the denormal came from an operand class or from the memory
  conversion — **correctness work rather than performance-critical.** Which is
  the right reason to have got them exactly right rather than approximately:
  nothing would have caught them being wrong, because nothing exercises them.
