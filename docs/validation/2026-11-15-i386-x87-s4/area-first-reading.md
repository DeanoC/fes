# Slice 4's first area reading, and what it is not

**Standalone unit fit, same flow, same seed, Powerboat's Quartus 17.0 — not
the chip host's CUDA slot:**

| fit | ALMs | DSP | M10K | worst setup |
|---|---:|---:|---:|---:|
| merge base (slice 3's unit) | 4,076 | 9 | 4 | **+0.570 ns** |
| slice 4, before the DSP fix | 7,964 | 13 | 6 | **−3.029 ns** |

**+3,888 ALMs standalone — the slice roughly doubled the unit** — and the
standalone envelope stopped closing by about 3 ns.

**M10K went 4 → 6, so the two seed tables landed in memory as the budget
predicted.** That is the one part of the forecast that held.

## What this is not

- **It is not the in-machine identity-on fit the stop rule is written
  against.** [The scope reading](scope.md) sets that rule on the machine fit
  at the seal's settings through one CUDA slot. This is the standalone
  envelope, which the fit spike used as a diagnostic.
- **It is not within-slot comparable to slice 3's published numbers.** Those
  were measured through the chip host's CUDA slot and these through
  Powerboat's Quartus, and slice 3's record is explicit that the two slots'
  Yosys binaries give different net numbering and Quartus fits them
  differently. The only valid comparison here is against the merge base
  measured **beside it in the same flow**, which is what the table does.
- **It is not the number after the DSP fix.** 13 DSP blocks against the nine
  option A allows was a design violation, not a measurement: the candidate's
  bits above 63 times a 64-bit value was written `a * b` and Quartus mapped
  each to a DSP block. The multiplicand is three bits, so it is three
  conditional shifts and about sixty ALMs (`mul3`). That is fixed and
  re-fitting.

## What it does say

On this evidence **slice 3 was not the peak**, and a straight reading puts
slice 4 well past the +1,400 ALM escalation threshold the scope reading set
before any fit existed. The machine fit has to run before the rule formally
fires, and it has to run on the chip host.

## The part that is probably coding style rather than algorithm

The RTL as written has about ten **general variable shifts** — a 96-bit shift
by 0 to 8 in the tail, the remainder's modular placement, the candidate's
drop by precision, the square root's normalisations — in a unit whose own
header says option A has no barrel shifter. A 96-bit shifter with a
three-bit amount is roughly 300 ALMs on its own, and there are several.

The restructure that removes the worst of them is standard and known: in the
tail, **shift the remainder left one bit a step against a fixed divisor**
rather than shifting the divisor and the candidate. That turns a 96-bit
variable shifter into a 65-bit register and a fixed subtrahend.

It has not been done, deliberately. It changes RTL that currently passes 730
bench cases, and the honest measured number of the design as written is worth
more to the phase's decision than an optimised number from a version this
slice broke at the last minute. It is **not** trimming to fit — it is
building what option A specifies — but it is not free risk either, and it is
the coordinating session's and Deano's call whether this slice spends it.
