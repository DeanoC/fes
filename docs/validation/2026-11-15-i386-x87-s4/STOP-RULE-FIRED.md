# The stop rule fired: slice 4's identity-on machine does not fit the device

**This is an escalation, not a report. Nothing has been sealed and no kit has
been claimed.**

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

Slice 3 was **not** the peak. It is not even a trend in the sense the question
assumed — it is a wall at slice 4.

| measurement | figure |
|---|---|
| standalone unit, merge base | 4,076 ALMs, 9 DSP, 4 M10K, +0.570 ns |
| standalone unit, slice 4 | **8,073 ALMs**, 9 DSP, 6 M10K, −3.839 ns |
| standalone delta, same flow | **+3,997 ALMs**, +0 DSP, **+2 M10K** |
| in-machine, identity on | **does not fit: 4,407 of 4,191 LABs** |

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

## The one thing worth knowing before the decision

**A material part of the +3,997 is very likely coding style, not the
algorithm**, and that is testable rather than speculative. The RTL as written
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
