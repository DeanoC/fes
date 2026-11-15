# The stop rule fired: slice 4's identity-on machine does not fit the device

**This is an escalation, not a report. Nothing has been sealed and no kit has
been claimed with the identity on.**

**The rule has now fired twice: once on the datapath as first written, and
again on the datapath rebuilt in the form option A specifies.** The second
firing is the one that answers the question, because the first number was
confounded by shifters option A does not have.

| in-machine, identity on | LABs required | of 4,191 | over by |
|---|---:|---:|---:|
| as first written | 4,407 | 105.2% | 216 |
| **option A's form** | **4,292** | **102.4%** | **101** |

**The restructure recovered 115 LABs — just over half the excess — and left
101.** So the shifters were real and worth removing, and they were *not* the
reason it does not fit. On the measurement that matters, **slice 4 does not
fit the DE10-Nano with the `i386dx+387` identity on.**

Neither fit placed, so there is no ALM figure, no timing and no seed sweep
from either: the demand figures above are the fitter's own.

[The scope reading](scope.md), written before any fit existed, set the rule:

> **Above +1,400 ALMs, or the identity-on fit failing to fit the device at
> all:** escalate to Deano and the coordinating session before the seal. I
> will not narrow this rule after seeing the number, and I will not trim
> area, change architecture to fit, or propose a new ceiling.

**The second trigger has fired.** The in-machine fit with the `i386dx+387`
identity on, at the seal's settings, seed 1, Quartus 17.0 on Powerboat:

```
Error (170012): Fitter requires 4407 LABs to implement the design,
                but the device contains only 4191 LABs
Error (11802): Can't fit design in device.
```

4,191 LABs is the 5CSEBA6's 41,910 ALMs. The design asks for **4,407 LABs —
about 105% of the device** — so the fitter stopped before placement and there
is no ALM figure, no timing and no seed sweep to report: **there is nothing
to sweep, because nothing placed.**

## The peak-or-trend answer

Slice 3 was **not** the peak. It is not a trend in the sense the question
assumed either — it is a wall at slice 4, and the wall survives building the
datapath the way option A specifies.

**And the design argument is refuted, measured.** Goldschmidt was expected to
be cheap because it reuses slice 3's multiplier, adder, rounder, shifts and
exponent arithmetic. In option A's form the arithmetic alone costs **+2,847
ALMs against the merge base — about 70% of slice 3's entire 4,076-ALM unit**.
Reuse did not make it cheap. That argument is one this record's own author and
the coordinating session talked themselves into, and it should be read as
plainly refuted as it was asserted.

| measurement | figure |
|---|---|
| standalone unit, merge base | 4,076 ALMs, 9 DSP, 4 M10K, +0.570 ns |
| standalone unit, slice 4 | **8,073 ALMs**, 9 DSP, 6 M10K, −3.839 ns |
| standalone delta, same flow | **+3,997 ALMs**, +0 DSP, **+2 M10K** |
| standalone unit, option A's form | **6,923 ALMs**, 9 DSP, 6 M10K, −1.222 ns |
| **standalone delta, arithmetic alone** | **+2,847 ALMs**, +0 DSP, +2 M10K |
| in-machine, identity on, as first written | does not fit: 4,407 of 4,191 LABs |
| **in-machine, identity on, option A's form** | **does not fit: 4,292 of 4,191 LABs** |
| in-machine, identity **off** — what ships and seals | **36,416 ALMs, +0.459 ns: fits and closes** |

Slice 3's identity-on machine fit was 39,682 ALMs at 94.68% of the device,
with 2,228 ALMs left. A slice costing about +4,000 cannot fit in 2,228, and
the fitter agrees.

**The M10K half of the budget held exactly as predicted**: 4 → 6 blocks, the
two seed tables, against the 13 of the spike's 17-block forecast still
unspent. The pressure was never memory. It is ALMs, and this slice spent
about 2.9 times what remained.

## What is not blocked

**The identity ships off**, so nothing a kit runs today depends on this. The
identity-off machine fit is running and is the configuration that seals; if it
fits and closes, slice 4's own seal and kit run can still proceed as slice
3's did, as a regression run that exercises none of this arithmetic.

## What I am not doing, and why

My own rule says escalate rather than narrow, so I am **not**:

- trimming area to make it fit;
- changing the architecture to fit;
- proposing a new ceiling figure;
- sealing or claiming a kit while the RTL may be about to change.

## The thing that was worth knowing, now measured

**A material part of the first +3,997 was coding style rather than the
algorithm — 1,150 ALMs of it, measured — and it was not enough.** What
follows is the reasoning as it stood before the restructure; it was right
about the cause and wrong about the size. The RTL as written
has about ten **general variable shifts** in a unit whose own header says
option A has no barrel shifter:

- the tail shifts a 96-bit subtrahend by 0 to 8 every step;
- `dv_dr` is a *register*, so the candidate's drop, the remainder's modular
  placement and the final placement became general shifts where the precision
  makes them one of three constants;
- the square root's normalisations (`sh`, `lz`, the estimate's exponent) are
  general where their range is one or two bits.

The restructures are standard and each has a cheaper exact form: a `case` on
the precision for the three drops, two-way muxes for the square root's
normalisations with assertions on their range, the FPREM shift by `n` through
the existing `S_SHIFT`/`S_MUL` path, and a held subtrahend shifted one place a
step in the tail. A standalone fit takes minutes and the 738-case bench is the
net.

**This is building what option A specifies, not trimming to fit** — but it is
a change to RTL that currently passes 738 cases on the RTL and on the netlist,
and under my own rule it is not mine to decide. The design argument that
Goldschmidt reuses slice 3's machinery has *not* been tested yet, because
8,073 ALMs measures shifters as much as it measures arithmetic.

So the decision in front of Deano is not "is slice 4 affordable" — on the
measurement it is not — but **"is slice 4 affordable once it is built the way
option A specifies"**, and that is a few hours of work and two standalone fits
away from being a measured answer rather than an argument.

## The decision this leaves, now that the measurement is unconfounded

The question is no longer "is slice 4 affordable once it is built the way
option A specifies". It is built that way, and it is not affordable: **101
LABs over, about 1,010 ALM-equivalents, with nothing left for routing even if
it had fitted.**

What this record can say about the options, without choosing between them —
they are Deano's:

- **Two DE10 core versions**, which Deano already put on the table on 15
  November in response to slice 3's seed sweep. The identity-off package fits
  and closes at 36,416 ALMs and +0.459 ns, so the no-387 core is unaffected;
  the 387 core is the one with no room.
- **The whole-system area reduction** Deano deferred on 14 November and made
  slice 6's charter. 101 LABs is within reach of the area audit's own
  deferred wins — the keyboard redesign alone was about −650 ALMs — so slice 6
  landing *before* slice 5 rather than after would change this answer.
- **The DE25**, whose hard floating-point DSP blocks could make the 387 far
  cheaper in fabric, which Deano raised on the same day.

What this record does **not** recommend is trimming slice 4's arithmetic: it
is exact against real hardware on 740 bench cases and the locked-Yosys
netlist, every lockstep bucket, test386 and QEMU's grid, and the one
simplification available was already taken — the remainders need no
Goldschmidt at all, which removed a seed, an iteration count, a candidate and
an error bound.
