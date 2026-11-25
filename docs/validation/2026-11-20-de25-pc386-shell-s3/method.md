# Slice 3: the shell's cost and seed stability, method (committed before the fits)

This follows the method #2030 fixed in "The shell's cost", with Deano's
standing rule of 20 November 2026: area, fold and resource bands are
one-sided. They fail only on growth over the reference, never on a ±N window.

## The two arms

Both arms fit the same netlist bytes: the board build's
`fes_mailbox/pc386_system.v`, the derived machine as
`pc386/build.py` prepares it. Its SHA-256 is recorded with each fit.

- **Board.** The generated board project of a committed revision, compiled
  once per seed with only `SEED` changed in `golden_top.qsf`.
- **Envelope.** `scripts/fit_i386_device.py --design machine --device de25
  --netlist <those bytes> --netlist-source <revision>`, the #1899 virtual-pin
  envelope, at 61.437908 MHz (`--period-ns 16.276`), once per seed.

The settings are the same on both arms: Quartus Pro 26.3 on chip-30x0-ai-dev,
seeds 1, 2, 3 and 4, no other change.

## The quantities

- **ALMs.** Quartus's `ALMs needed [=A-B+C]`, the capacity row. The LAB row
  is not used.
- **RAM blocks and DSP blocks**, from the fit summary.
- **Timing.** Worst setup, hold, recovery, removal and minimum pulse width at
  every published corner, from the timing summary.

## What is reported

- **The shell's cost**: the median of the four board seeds' ALMs minus the
  median of the four envelope seeds' ALMs, with each arm's spread (max − min)
  beside it. The difference counts as *measured* only if it is larger than
  the larger spread; otherwise it is reported as within the spread. This rule
  decides measurability only. It is not a pass band.
- **The envelope's virtual-pin cells**, reported separately, because they are
  what the shell replaces.

## Bands (one-sided)

- **Board seed stability (the pass rule).** All four board seeds fit, and
  every clock meets setup, hold, recovery, removal and minimum pulse width
  with non-negative slack at every corner. A seed that misses is a fail for
  the arm, not a candidate. Three of four is a fail.
- **Capacity.** The board arm passes on ALMs if every seed's ALMs needed is at
  most 46,800, the device. No lower bound.
- **Against the working line.** Each board seed's ALMs is compared with 43,056
  (92%, #2027 D2) and reported as "under" or "over". Over is not a failure of
  this slice, which is identity-off; it is the input D2 asks for.

No band changes after a fit has run. A rule that has to change is reported as
a failed rule with a new one beside it, not substituted.
