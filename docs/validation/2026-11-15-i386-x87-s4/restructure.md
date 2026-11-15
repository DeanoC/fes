# Building option A's form, and what it costs

The first fit measured the datapath **as first written**, and that number is
kept rather than replaced: [the first reading](area-first-reading.md) is a
real measurement of real RTL, and the pair of fits is what answers "did the
shifters matter" later.

| fit | ALMs | DSP | M10K | worst setup |
|---|---:|---:|---:|---:|
| standalone, merge base | 4,076 | 9 | 4 | +0.570 ns |
| standalone, as first written | 8,073 | 9 | 6 | −3.839 ns |
| in-machine, identity on, as first written | **does not fit: 4,407 of 4,191 LABs** | | | |
| in-machine, identity **off** — what ships and seals | **36,416** | 40 | 282 | **+0.459 ns** |

**The identity-off package fits and closes.** That is the configuration that
ships and that the seal is taken on, so slice 4's own seal and kit run can
proceed as a regression run exactly as slice 3's did: only the identity-**on**
configuration is blocked. The +24 ALMs against slice 3's 36,392 are the
decode and format table entries, which exist whatever the identity does, and
the two fits are through different Quartus hosts so the difference is not a
within-slot delta.

## Why that number is not the algorithm's cost

**Option A's defining choice is that the unit has no barrel shifter** — its
own header says shifts are done as multiplies. The RTL as first written had
about ten *general* variable shifts, so 8,073 ALMs measured shifters as much
as arithmetic, and the design argument that Goldschmidt reuses slice 3's
machinery could not be tested from it. Removing them is conformance to the
specification, not trimming to fit; leaving them in was the deviation.

The coordinating session authorised the work on that reasoning, and **the stop
rule stays in force on the re-fit, unchanged**: above +1,400 ALMs or a no-fit,
escalate again before the seal. The rule is about the measurement, not about
the explanation for it.

## The four shifters, and the exact form of each

### The tail's subtrahend, held and shifted one place a step

The largest. Forming `B << i` for the divide and
`(C << (i+1)) + 2^(2i)` for the square root from a step counter put **two
96-bit variable shifters** on the critical path. Both recurrences are exact,
so the subtrahend is held in a register instead:

- **divide**: `S(i) = B · 2^i`, so `S(i−1) = S(i) / 2`.
- **square root**: with `P(i) = 2^(2i)`,
  `S(i−1) = S/2 + P/2 + P/4` when the step is taken and
  `S/2 − P/2 + P/4` when it is not, because taking the step moves `C` by
  `2^i`. `P` halves twice a step.

Every shift is by one or two places. The step count is still fixed by
precision control, so the latency does not move. **The RTL carries a sim-only
assertion comparing the recurrence against the closed form it replaces at
every step**, so a drift is caught by a check rather than by a wrong result —
which is the only reason a recurrence is safe to substitute for a formula
that 740 bench cases already agree with.

### Still to do

- the three precision drops as a `case` on `wprec` rather than a register fed
  to a general shift;
- FPREM's shift by `n` through the existing `S_SHIFT`/`S_MUL` multiply path,
  which is where option A puts a genuinely variable shift;
- the square root's normalisations (`sh`, `lz`, the estimate's exponent) as
  two-way muxes with assertions on their range, which is one or two bits.
