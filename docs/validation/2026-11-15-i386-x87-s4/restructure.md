# Building option A's form, and what it costs

The first fit measured the datapath **as first written**, and that number is
kept rather than replaced: [the first reading](area-first-reading.md) is a
real measurement of real RTL, and the pair of fits is what answers "did the
shifters matter" later.

| fit | ALMs | DSP | M10K | worst setup |
|---|---:|---:|---:|---:|
| standalone, merge base | 4,076 | 9 | 4 | +0.570 ns |
| standalone, as first written | 8,073 | 9 | 6 | −3.839 ns |
| **standalone, option A's form** | **6,923** | 9 | 6 | **−1.222 ns** |
| in-machine, identity on, as first written | **does not fit: 4,407 of 4,191 LABs** | | | |
| in-machine, identity **off** — what ships and seals | **36,416** | 40 | 282 | **+0.459 ns** |

**The identity-off package fits and closes.** That is the configuration that
ships and that the seal is taken on, so slice 4's own seal and kit run can
proceed as a regression run exactly as slice 3's did: only the identity-**on**
configuration is blocked. The +24 ALMs against slice 3's 36,392 are the
decode and format table entries, which exist whatever the identity does, and
the two fits are through different Quartus hosts so the difference is not a
within-slot delta.

## What the shifters cost, measured

**The restructure is worth 1,150 ALMs and 2.6 ns.** The datapath in option
A's form is **6,923 ALMs** against **8,073** as first written, and the
standalone envelope's worst setup goes from −3.839 ns to −1.222 ns.

So **14% of the as-written figure was shifters that option A does not have**,
and the timing improvement is the larger result: a general shift on a
critical path costs delay out of proportion to its area.

**And the arithmetic's own cost is now measurable: +2,847 ALMs against the
merge base**, in the same flow and seed, which is what the peak-or-trend
question was actually asking. The design argument that Goldschmidt reuses
slice 3's machinery is **not** borne out at that figure — slice 3's whole unit
is 4,076 ALMs, so slice 4's arithmetic costs about 70% of everything before
it — but it is now an answer about arithmetic rather than about coding style.

The in-machine identity-on fit at this form decides whether the stop rule
fires a second time.

## Why the first number was not the algorithm's cost

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

## The per-entity fit, and what it can and cannot attribute

The brief asked for a fit with `flatten` removed so the report lists ALMs by
entity. Standalone unit, option A's form, `--hierarchy`, seed 1, same slot:

| entity | ALMs (own) | registers (own) |
|---|---:|---:|
| `spike_x87_unit` (the bench envelope) | 6,899.2 (130.5) | 7,287.6 (136.3) |
| `fes_i386_x87:dut` | 6,722.7 (**6,546.8**) | 7,084.0 (6,882.0) |
| `fes_i386_x87_mul:mul` | **175.9** (175.9) | 202.0 (202.0) |
| `altsyncram:rcp_tab_rtl_0` — the reciprocal seeds | **0.0** | 0.0 |
| `altsyncram:rsq_tab_rtl_0` — the reciprocal-square-root seeds | **0.0** | 0.0 |
| `altsyncram:rf_a`, `rf_b` — the register stack | **0.0** | 0.0 |
| `spike_stimulus:stimulus` (the bench's driver, not the unit) | 46.0 | 67.3 |

Dropping `flatten` is a different flow — it fits at 6,899 ALMs against the
flattened 6,923 — so these figures are not interchangeable with the flattened
ones, the same caveat slice 3's record makes about its own pair.

### What it establishes

**Both seed tables cost 0 ALMs**, read from the fitter's own hierarchy rather
than inferred from a block count. That is the M10K claim verified: the
tables are in memory, and the 2 extra M10K blocks are their whole cost. The
register stack is likewise 0, as slice 2 built it.

**The 9-DSP multiplier's glue is 175.9 ALMs**, and the DSP blocks themselves
are counted separately — so the multiplier is almost free in logic, which is
what sharing it was for.

### What it cannot attribute, and this is the honest limit

**6,546.8 of the unit's 6,722.7 ALMs are flat logic inside
`fes_i386_x87.sv` itself**, because slices 2, 3 and 4 all live in that one
module. So **the per-entity fit cannot apportion slice 4's +2,847 ALMs within
the unit** — it can only say that the cost is in the unit's own logic rather
than in its memories, its multiplier or the bench envelope.

That is worth stating rather than glossing, because a per-entity fit is
normally the tool for "which part cost what" and here it answers a narrower
question. Attributing within the unit would need the logic split into modules
per slice, which is a refactor nobody has asked for and which would change
the fit it was measuring. **The +2,847 figure stands on the whole-unit delta
against the merge base in the same flow, which is the measurement that does
not depend on module boundaries.**
