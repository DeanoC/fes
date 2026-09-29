# RAM tester: analytical HPS pin geometry, 2026-09-27

Follow-up to the [paired composition experiment](2026-09-27-ramtest-placed-composition.md)
for [FES #264](https://github.com/DeanoC/fes/issues/264), based on FES `7e16a063`
and the best nextpnr candidate, `71c513e1`. **The analytical-only change regresses
memory to 96.52 MHz and is rejected.** This host-only diagnostic tests
physical pin locations in analytical placement. RTL, synthesis, constraints,
producer locks and shared contracts are unchanged.

## Question and boundary

The HPS FPGA-to-DDR block has one legal BEL at (52,53), but its connected
physical pins lie at column 51 across rows 53–68. In particular, ready-0/1/2
exit at `GIN.51.64.19/.18/.17`. The original HeAP equations give every pin the
cell's location. The HPS cell itself participates in analytical solving and is
later legalized to its sole BEL; treating all its pins as fixed coordinates
would therefore be an incorrect implementation of offsets.

The diagnostic adds a relative per-port offset to HeAP's net extrema, distance
weights, equation terms and total half-perimeter wirelength. For a spring
between `(x_i + o_i)` and `(x_j + o_j)`, the right-hand-side correction is
`-w*o_i + w*o_j`. This applies whether the HPS cell is solved in the current
bucket pass or contributes as a fixed term. Offset endpoints are not clamped
to the device boundary during analytical solving.

Mistral caches eligible FPGA-to-DDR pin offsets from the unique physical BEL
before HeAP runs, independently of temporary placement bindings. Ordinary
logic cells retain zero offsets. The diagnostic does not change the timing
predictor, simulated-annealing refinement, GPU routing or final timing model.
It therefore tests analytical geometry alone, not complete pin-aware placement
or calibrated delay prediction.

An independent parser of Mistral's `data/sx120f-p2r.txt` maps all 1,178 connected
baseline HPS pins, including folded constants and unused outputs. All map to
column 51, rows 53–68. Physical mapping does not itself establish timing
relevance. The actual compiler manifest independently matches all mappings
and applies 673 offsets, excluding 430 constant-driven inputs, 11 clock inputs
and 64 unused outputs. Constant-driven HPS inputs retain `PIN_SIG`, so the
exclusion checks their `MISTRAL_CONST` drivers as well as folded pin states.

The compiler diagnostic is committed as
[`979039a9`](https://github.com/DeanoC/nextpnr/commit/979039a95910f388cb0b7d69d7e9f16980e29df3)
on `probe/ramtest-hps-pin-geometry`. It is disabled by default.

Set `NEXTPNR_MISTRAL_HPS_PIN_GEOMETRY=<prefix>` to enable the diagnostic;
`<prefix>.pins.tsv` records every considered pin and exclusion reason, and
`<prefix>.scope.json` records its phase boundary. Absent or empty values leave
it disabled without allocating identifiers. An enabled run with no applicable
pins is rejected. The implementation requires ordinary full-design HeAP.

## Controlled measurement

The fresh flow uses unchanged initialized-FSM synthesis,
BUILD_ID, seed 2, HeAP exponent 2, beta 0.5, timing weight 10, enable-replication
budget four and GPU 1. The measured baseline is 116.4415 MHz memory /
77.9059 MHz pixel. The full disabled placement control exactly reproduces both
before/after replication modules, every pin state and the replica manifest.
The rejected composition probe is not part of this branch.

| Candidate | Memory MHz | Pixel MHz | Capture MHz |
| --- | ---: | ---: | ---: |
| Effective exponent 2, before enable replication | 111.0864 | 75.3239 | 332.4941 |
| Best generic-replication baseline | **116.4415** | **77.9059** | **332.4941** |
| Analytical HPS pin offsets, same replication budget | 96.5158 | 75.0694 | 240.1278 |

The enabled run completes legally, emits an RBF and finishes signoff timing
without reported hold violations. Pixel still passes 74.25 MHz and capture
passes 130 MHz; memory remains below 130 MHz. The new worst memory path is
`ddr0_test.idle[14]` to `ddr0_test.beats_left[2].ENA`: seven LUT arcs,
1.397 ns logic and 8.351 ns routing. With 0.731 ns clock-to-Q, 0.078 ns skew
and −0.196 ns setup, it totals 10.361 ns.

The result rejects this analytical-only configuration on this design and seed;
it does not establish that physical pin coordinates are irrelevant. The old
predictor and refinement costs remain active, the entire placement changes,
and the replication selector finds no qualifying copy. The timing comparison
measures the complete flow's response, not offsets on an identical final
netlist. Even relative to the 111.09 MHz pre-replication control, this result
regresses.

An additional geometric comparison after refinement finds that the sum of
`abs(dx) + 2*abs(dy)` across the 673 eligible HPS/fabric arcs increases from
53,763 to 69,547 tile units. This metric has no timing-criticality weights
and is neither HeAP's full objective nor a delay estimate. Ready-1's
first LUT moves from (30,26) to (34,30), reducing that one weighted distance
from 97 to 85; ready-0 and ready-2 distances worsen. Thus the final placement
does not even improve aggregate HPS proximity under this metric. This is not
a before/after analysis of HeAP alone, because refinement has already run.

Retain the 116.44 MHz candidate. A useful next isolation is to keep the
single-site HPS block anchored during analytical solving, comparing that alone
with anchored per-pin offsets. The current HeAP solution lets its virtual
center move before legalization snaps it back to the unique physical site.
That observation motivates another controlled experiment; it does not prove
this freedom caused the regression. Consistency with refinement and timing
prediction also remains untested.

## Verification and limits

All 22 backend tests pass. Five focused tests cover fixed, solved and jointly
solved equation offsets; unclamped endpoint coordinates; zero-offset arithmetic;
physical pin mappings independent of current binding; excluded constants,
clocks, unused outputs and missing mappings; enabled no-op rejection; and
disabled identifier neutrality. Three initial tests fail before implementation;
two later coverage failures exercise constant/unused-output exclusions and
no-op rejection, with one repeated test. Absent and empty-prefix placement
flows reproduce the entire 197-cell reference module.

Independent checker tests accept the baseline and a placement-only change,
and reject six logic/state/settings corruptions. The silicon-manifest checker
rejects six mapping, sign, coverage and eligibility corruptions. Independent
snapshot checks require the packed module and pin states to match the baseline exactly,
allowing BEL positions to differ. Enable replication is checked separately
because a new placement may change which copies qualify. The enabled placement
moves 14,809 of 14,890 packed cells; all packed logic, settings, connectivity
and 103,057 pin states remain identical. No enable copy qualifies at the new
placement, despite the unchanged budget of four. Both the old and new selectors
use the same replication policy; this is an interaction with placement.

Independent source and evidence reviews found no blocking issues. All 65
artifact hashes and 11 input hashes verify; the evidence record regenerates
byte-identically. The adjacent [JSON record](2026-09-27-ramtest-hps-pin-geometry.json)
contains the measurements and exact compiler provenance.

Raw evidence and regeneration helpers are under `out/ramtest-hps-pin-geometry/`.
A first control-launch attempt used unsupported CLI flags and exited before
packing; its `control-c2-place-invalid-cli` artifacts supply no measurement.
The corrected placement-only control uses `--no-route`.
No PR or hardware operation is included. The previous HPS analogue-waveform
fallback observation remains a separate, uninstrumented source-based inference;
this experiment does not modify or calibrate that timing path.
