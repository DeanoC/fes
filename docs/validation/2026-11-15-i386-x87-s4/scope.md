# x87 slice 4: scope reading, checked before any RTL

Checked against [#1768](https://github.com/DeanoC/fes/issues/1768) (body, the
13 November option-A decision, and the 15 November plan change
`#issuecomment-6899342397`), the plan record
`docs/validation/2026-11-12-i386-x87-plan/summary.md` (slice list at lines
586-597, FPREM semantics at 405-414, accuracy at 395-404), the fit spike
record (line 264 and `sim/i386/spike/spike_x87.sv` lines 23-25), and slice
3's record and PR #1879.

Slice 3's session was handed a brief wrong in three places; this is the same
check, and the brief for slice 4 is **right on scope**. Two things in its
wording do not match the sources and one of them may be a real decision.

## What the sources agree on, and the brief has right

- **Operations.** FDIV/FDIVR with every ST(i), integer (FIDIV/FIDIVR) and
  popping form; FSQRT; FPREM; FPREM1. Plan lines 589-590, #1768 bullet 4.
- **Datapath.** Goldschmidt divide and square root over slice 3's shared
  9-DSP multiplier, 67-bit adder, PC-aware rounder, multiply-based shifts and
  exponent arithmetic, with reciprocal and reciprocal-square-root seeds in
  M10K. Deano's 13 November option-A decision; the spike's `DIVSQRT=1`.
- **Early termination at 24 and 53 bits.** #1768 bullet 4.
- **Exceptions.** Divide-by-zero is pre-operation (slice 3 rule 1): unmasked,
  it leaves the operands and TOP alone and reports nothing else. The invalid
  cases are 0/0, inf/inf, FSQRT of a negative (and of an unnormal or a
  pseudo-value, plan 415-418), and FPREM/FPREM1 with an infinite dividend or
  a zero divisor. Flags are derived on the commit edge, never from the
  registered status word.
- **Gates.** TestFloat div and sqrt, and `rem` for FPREM1; directed FPREM and
  FPREM1 cases over large exponent differences checking each partial step's
  C2 and the final C0, C3 and C1; QEMU's `fprem` test. Plan 590-593.
- **Slice boundary.** Transcendentals, FBLD and FBSTP are slice 5's and stay
  stopped as not built. The 15 November change narrowed slice 5 to those
  alone; the identity flip is slice 7's.
- **The identity ships off.** So slice 4's identity-on fit and seed sweep are
  evidence for slice 6, not gates on this slice.

## The one wording difference that may be a decision: FPREM's style

- Deano's **13 November** option-A decision and the fit spike (line 264) both
  say "transcendentals, **FPREM** and BCD in microcode", listing FPREM
  *apart* from "Goldschmidt divide and square root".
- The **15 November** plan change says slice 4 is "FDIV/FDIVR, FSQRT, FPREM,
  FPREM1 **on Goldschmidt** with M10K seed tables".
- The **brief** says FPREM/FPREM1 go "on that datapath — **not microcode**".

**My reading is that these do not conflict architecturally, and that the
brief and the 15 November comment are both satisfiable.** The spike's own
words for `DIVSQRT=1` are "Goldschmidt/Newton **in microcode on the
multiplier**" (`spike_x87.sv:24`) — so in the spike's language "in microcode"
means *sequenced over the shared multiplier rather than given a dedicated
hardware block*, which is exactly what the brief calls "on the datapath".
`fes_i386_x87.sv` has no microcode engine to put anything in: it has one
sequencer with `S_*` states, and slice 3's multiply, FSCALE and FRNDINT
already run as multi-pass sequences through `S_SHIFT`/`S_MUL`. Slice 4 adds
states to that sequencer. There is no third option on the table.

**What is genuinely not interchangeable is Goldschmidt for the remainder's
result.** FPREM and FPREM1 are **exact** and **ignore both rounding and
precision control** (plan 405-408). A Goldschmidt quotient is a rounded
reciprocal product, so it cannot produce the remainder; it can only estimate
the quotient to pick the scale for each reduction step. So the plan reads to
me as: FDIV/FDIVR and FSQRT get the Goldschmidt iteration and the seed
tables; FPREM/FPREM1 get a scaled-subtract partial-remainder loop on the same
adder and multiply-based shifts, using the exponent difference (and, if it
earns its area, the same seed) to choose each step's scale, with the quotient
bits accumulated only for C0/C3/C1. **I will build it that way unless told
otherwise**, and the record will say so plainly.

## The second difference: early termination does not apply to the remainders

The brief groups "early termination at 24 and 53 bits under precision
control" with everything in the slice. It can only apply to **FDIV/FDIVR and
FSQRT**: FPREM and FPREM1 ignore precision control entirely (plan 405-408),
so there is nothing to terminate early. The gate for early termination is
therefore on divide and square root only, and the bench will assert the
**cycle count** by precision control so that "early" is observed rather than
asserted — a correct result at PC 24 proves nothing about iterations saved.

## Threshold the plan fixes that the brief leaves as "large"

The partial remainder is not a vague large-difference case: when the
operands' exponents differ by **64 or more**, a step returns a partial
remainder with **C2 set** for software to loop on; a complete reduction
clears C2 and reports the quotient's low three bits in C0, C3 and C1 (plan
409-412). The directed cases are built on that boundary — at, just below and
far above it.

## Two things to say now rather than at gate time

- **TestFloat `rem` covers FPREM1 only** (plan 412-414), so FPREM has no
  reference vector set and rests on directed cases plus the host. Slice 3
  could not build TestFloat at all — its session's permission classifier
  refused it — so TestFloat may well come back unmet again. If it does I will
  report it unmet and name the host bench as coverage, not as the plan's
  reference, exactly as slice 3's record does.
- **This slice's kit run will again be a regression run.** The identity's
  record word is slice 7's, so a kit can only run the package with the
  identity off, which exercises none of slice 4's arithmetic. The bench and
  the lockstep are the gate; the kit run is a no-regression check. Slice 3's
  record says the same of its own.

## Stop rule, set before any fit exists

Precommitted, on the identity-**on** machine fit at the seal's settings
through one CUDA slot, seed 1, against this branch's merge base through that
same slot:

- **Below +800 ALMs:** the design argument holds; slice 3 was the peak.
  Report and carry on.
- **+800 to +1,400 ALMs:** the trend is real but slices 5-7 still have room
  only with slice 6's reduction landing first. Report, finish the slice, and
  say so in the record and to the coordinating session.
- **Above +1,400 ALMs, or the identity-on fit failing to fit the device at
  all:** escalate to Deano and the coordinating session before the seal. I
  will not narrow this rule after seeing the number, and I will not trim
  area, change architecture to fit, or propose a new ceiling — Deano's
  standing instruction of 14 November is to report the number.

The seed sweep is reported in full, every seed including misses, whatever it
shows.
