# The proposed bounds for the eight transcendentals

**A proposal for Deano, not a pass rule.** Written 17 Nov 2026, before any
RTL for the transcendentals existed and so before any RTL was run against a
bound. Until he agrees it, RTL results are reported as distributions and no
transcendental result gate is claimed as passed.

## What had been seen when this was written

Stated first, because it matters more here than anywhere: **the proposal was
not made blind to the design.** The transcendentals are a microprogram with
an exact integer model (`x87_trans_ucode.py`), and method.md's amendment 1
requires the RTL to equal that model bit for bit. So the model's distances
are, if that check passes, the RTL's. They had all been seen:

- the model against each host: 604,304 results a host, **0 to 2**
  representable values apart, 2 on 74 of them (Intel) and 62 (AMD), all in
  FPTAN and FPATAN;
- the model against the mathematical result: at most **1.42** units of the
  last place before its last rounding and **1.83** after;
- the two hosts against each other, each reference against QEMU's brackets,
  and the oracle against the host.

Every histogram is in [references.md](references.md), generated from the
committed reports. A bound chosen after seeing these cannot be presented as
a prediction the design then met. What it can be is a figure **derived from
the manual**, with the measurements showing how much room there is under it;
that is how it is argued below.

## The unit bench, against the host x87 and the brackets: 4

**Proposed: `--transcendental-ulp-host 4`.**

- **From the manual.** The 387's results have a relative error under 2^-62
  (rules.md G1). A unit in the last place of a 64-bit significand is between
  2^-64 and 2^-63 of the value, so 2^-62 is between **2 and 4** units
  depending on where in its binade the result falls. 4 is the widest the
  manual's bound ever is; no result the manual allows is further than that
  from the mathematical result.
- **From the references.** The host returned a neighbour of the mathematical
  result on all 4,374 bracket rows, and the two hosts never differ by more
  than 1 over 704,374 results. So a host is within 1 of the mathematical
  result wherever that could be checked. It could be checked for F2XM1,
  FYL2X, FYL2XP1 and FPATAN only: QEMU has no table for the four
  trigonometric forms, where the evidence is the two hosts' agreement and
  the model's own high-precision evaluation.
- **What 4 means.** A result more than 4 from the host is more than 3 from
  the mathematical result. That is not exactly the manual's bound (3.5 units
  can be inside 2^-62 at the top of a binade), and it is not presented as
  one: it is the manual's widest figure, applied against a reference that is
  itself within 1.
- **The room under it.** The model's largest distance from either host is 2.
  A bound of 3 would leave one value of room; 2 would leave none and would
  turn a one-in-ten-thousand rounding coincidence into a failure. 4 is
  proposed because it is the manual's number rather than the measurement's.
- **The brackets** take the same figure, applied to the distance outside the
  bracket, as method.md's amendment says.

The hosts are modern x87 units, not 387s, and this is not a 387 capture.
Whether real 387 results exist to set this bound from is still an open
question; none has been offered.

## The lockstep, against the Bochs oracle: 2^21, away from zeros and poles

**Proposed: `--transcendental-ulp-oracle 2097152`**, with the value compared
only where the oracle's own result for FSIN, FCOS, FSINCOS and FPTAN has a
magnitude in [2^-10, 2^10].

- **Why so wide.** The oracle is outside QEMU's brackets on 57 to 498 rows of
  each table, by up to about 247,000, and differs from the host by up to
  about 936,000 on FPATAN over ordinary operands
  ([references.md](references.md)). A bound has to clear the reference's own
  error or it fails correct results; 2^21 is the first power of two above
  twice the largest such distance. **It is a sanity check and is not offered
  as an accuracy check**: it catches a wrong function, a wrong sign, a wrong
  octant or a wrong exponent, and nothing finer.
- **Why the exclusion.** Beside a multiple of pi/2 the oracle and the host
  differ by up to about 8.6e9 while the two hosts agree to the bit, so no
  bound that means anything survives there. Those operands are in the unit
  bench, against the host. In lockstep such a retirement still has every
  flag, C0, C2, C3, TOP and tag compared exactly, is resynchronised, and is
  **counted** as value-unchecked in the report; it is not skipped silently.
- **What keeps its force in lockstep** is method.md's rule 1: everything but
  the value is exact, at every retirement.

## What is not proposed

- **No bound on C1.** It differs between the two hosts on tens of thousands
  of operands whose values agree, so it is reported and not gated
  (method.md, rule 5).
- **No per-instruction bounds.** One figure a reference is easier to hold to
  than eight, and the measurements do not call for more.
- **Nothing for the unmasked overflow and underflow results.** They are the
  unit's own A_RND, shared with the arithmetic, and the model does not
  compute them; the hosts and the oracle disagree with each other there
  (rules.md), which is recorded as a divergence, not bounded.
