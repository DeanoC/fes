# RAM tester: fixed analytical HPS anchor, 2026-09-28

Follow-up to [analytical HPS pin geometry](2026-09-27-ramtest-hps-pin-geometry.md)
for [FES #264](https://github.com/DeanoC/fes/issues/264), based on FES `d58283c0`
and nextpnr `979039a9`. This host-only experiment tests whether keeping the
single-site HPS block fixed during analytical solving helps the unchanged
RAM-test design. Both candidates regress: fixed-only reaches 90.80 MHz memory,
fixed plus pin offsets reaches 95.01 MHz. The best remains 116.44 MHz;
130 MHz is still unresolved.

## Paired experiment

FPGA2SDRAM has one legal BEL, at (52,53), but ordinary HeAP treats its virtual
position as a variable during some solve passes. Legalization subsequently
returns it to the physical site. The preceding experiment changed the pin
locations relative to that virtual position and regressed memory from
116.44 to 96.52 MHz. That result did not establish whether the movable
analytical position contributed to the loss.

Both new modes hold FPGA2SDRAM at its physical site during analytical placement:

- **Fixed:** keep the existing cell-based endpoint coordinates.
- **Fixed plus pins:** also apply the previously verified physical pin offsets
  to the 673 eligible HPS/fabric connections.

The diagnostic uses HeAP's existing seed-locking mechanism, preserving initial
BEL enumeration and shuffling. It rejects unexpected prebound, constrained or
clustered HPS cells. Phase evidence checks that HPS remains locked at (52,53)
and absent from the analytical solve rows. Before refinement, its binding
strength returns to the ordinary WEAK value. This preserves the baseline
binding contract; STRONG is not categorically excluded from SA refinement.

Timing prediction, refinement algorithms, routing, final delay models, RTL,
synthesis and source constraints are unchanged. The existing enable-replication
policy and budget of four remain enabled; different placements may select
different copies. Complete-flow timing comparisons must account for that
interaction.

## Measurement and evidence

Both fresh flows use the same initialized-FSM synthesis and BUILD_ID, seed 2,
HeAP exponent 2, beta 0.5, timing weight 10, original QSF/SDC and GPU 1.
The best measured candidate remains 116.4415 MHz memory / 77.9059 MHz pixel.

Before/after-refinement placement tables allow independent comparison of
physical HPS-to-fabric distances at both phases. These distances are geometric
measurements, not routed delay or the complete HeAP objective. Separate checks
require packed logic, initialization, connectivity, settings and all pin states
to remain unchanged; BEL moves are allowed. Replication is checked separately.

Raw evidence is under `out/ramtest-hps-fixed-anchor/`. No producer lock, shared
contract, PR or hardware operation is part of this diagnostic.

## Fixed-only result

The fixed-only run completed with a legal route, RBF and final analogue timing:
90.8018 MHz memory, 76.3884 MHz pixel and 333.6889 MHz capture. Memory regresses
against the 116.4415 MHz baseline. The limiting memory path is now
`ddr2_test.idle[23]` to `ddr2_address[25].ENA`: eight LUT arcs, 1.604 ns logic,
8.947 ns routing, 0.731 ns clock-to-Q, -0.073 ns skew and -0.196 ns setup,
for 11.013 ns effective setup. This differs from the baseline HPS-ready path;
it is not a matched-path comparison.

All 14,890 original packed cells and 103,057 pin states retain their logic,
initialization and connectivity; 14,812 BEL assignments change. The independent
anchor proof covers 236 phase observations, including 60 solve-row setups,
56 solve/spread/legalize cycles and the refinement boundaries. The HPS remains
fixed and unsolved, and WEAK is restored before refinement. The post-refinement
BEL table exactly matches the pre-replication snapshot. After-refinement
analytical coordinates are retained HeAP state; the actual binding and current
BEL table establish physical placement at that boundary.

The physical distance metric over 673 HPS/fabric arcs falls from the baseline's
53,763 to 30,283 before refinement and 28,763 after it. All three ready sinks
are at (50,52), giving distance 25 under this metric. Thus the final loss cannot
be explained as refinement undoing this particular geometric improvement.
It also shows why that distance alone is an insufficient optimization target.

The unchanged replication policy chooses one different copy: DDR0 `cmd_end`
enable ALUT3, from (43,41) to (47,35), serving address bits 16–18 in one LAB.
The snapshot checker verifies identical LUT/polarity, whole-LAB rewiring and
preserved original placements and pin states across replication. This differs
from the baseline's port1 enable copy and limits causal attribution to anchoring
alone at the final-route level.

## Combined result

Fixed plus pin offsets completes with a legal route, RBF and final analogue
timing of 95.0119 MHz memory, 74.7049 MHz pixel and 356.1786 MHz capture. It also
regresses against the best baseline. Its limiting memory path is
`ddr1_test.idle[14]` to `ddr1_test.beats_left[2].ENA`: seven LUT arcs, 1.397 ns
logic, 8.561 ns routing, 0.731 ns clock-to-Q, 0.032 ns skew and -0.196 ns setup,
for 10.525 ns effective setup. Signal names are resolved from routed Q aliases;
cell-name suffixes do not reliably identify the RTL bit.

Neither run reports hold violations in final signoff; each reports the memory
130 MHz setup miss. The pixel clock passes in both runs.

| Flow | Memory MHz | Pixel MHz | Capture MHz |
| --- | ---: | ---: | ---: |
| Previous best: generic replication | 116.4415 | 77.9059 | 332.4941 |
| Prior movable HPS + pin offsets | 96.5158 | 75.0694 | 240.1278 |
| Fixed HPS | 90.8018 | 76.3884 | 333.6889 |
| Fixed HPS + pin offsets | 95.0119 | 74.7049 | 356.1786 |

These are complete-flow results for one design and seed, including differing
replication decisions. Neither anchoring candidate is adopted.

### Placement evidence

Fixed anchoring plus physical pin offsets preserves the same packed logic and
103,057 pin states; 14,812 original BEL assignments change relative to the
baseline. Its anchor proof covers 220 observations, including 56 row setups
and 52 solve/spread/legalize cycles. All 1,178 connected HPS pins independently
match the Mistral database: 673 offsets apply; 430 constant inputs, 11 clocks
and 64 unused outputs are excluded. No enable replica qualifies in this run.

The physical metric falls to 18,137 before refinement and rises to 19,614 after
it. Refinement therefore gives back some geometric improvement in this mode,
but the final value remains substantially below the baseline's 53,763. Ready0,
ready1 and ready2 sink positions change from (50,59)/(50,57)/(50,55) before
refinement to (48,54)/(50,51)/(50,53) afterwards. These are placement observations,
not predictions of final timing.

## Validation and reproduction

Compiler diagnostic: `probe/ramtest-hps-fixed-anchor`, commit
`623070998f3e23e2ad7acd5eb3311e4827fbbd17`, based on `979039a9`.
The separate earlier branches remain intact. The environment variable
`NEXTPNR_MISTRAL_HPS_FIXED_ANCHOR=<prefix>` enables anchoring and its phase/BEL
sidecars. Combined mode additionally sets `NEXTPNR_MISTRAL_HPS_PIN_GEOMETRY`
to a distinct prefix. Both are disabled by default.

- Five focused real-backend tests pass. Three initially failed with the no-op
  implementation; the two disabled controls already passed.
- All 27 backend tests pass. Small absent/empty controls exactly reproduce the
  complete 197-cell reference module.
- A fresh full-design disabled placement reproduces baseline before/after
  modules, all pin states and the replica manifest exactly. This is placement
  identity evidence; baseline routed timing is retained from its earlier run.
- Independent packed-placement checks preserve all logic, initialization,
  connectivity, constraints and settings, allowing only BEL moves. Their
  identity/move positives and six deliberate corruptions pass.
- Independent anchor checks correlate phase traces, strength transitions, all
  cell placements and the packed snapshot. One positive transition and 14
  deliberate corruptions pass. Replication snapshots are checked separately.
- Independent source review found no blockers. The frozen source hashes match
  the compiler commit. The binary retains a cached version string, so identify
  it by source and SHA256, not that string.

Frozen binary: `out/ramtest-hps-fixed-anchor/tools/nextpnr-hps-fixed-anchor`,
SHA256 `cb9ebf7dca81c0f7ceec330f76475ecf58b05013fe7be9c4f0a60a817438f4ba`.
The adjacent JSON records commands, source/input/output hashes, final timing,
phase proofs, geometry comparisons and test results. Raw helpers reproduce the
proofs and summary. Both actual timing runs use fresh synthesis input, not a
routed-JSON replay.


## Decision and next comparison

Keeping the unique-site HPS fixed does improve the measured physical locality,
and the phase traces verify that this happens as intended. It does not close
timing: long timeout/control cones become limiting. Further HPS-distance
optimization is not justified by these results alone. No claim is made that
Mistral's delay model is pessimistic or that any route is calibrated against
an equivalent Quartus route.

The user requested comparison with the Quartus implementation that meets
130 MHz. The saved same-RTL, hash-matched Quartus fit reaches 139.30 MHz memory;
the next step is to inspect its fitted enable and timeout cones against the
best 116.44 MHz OSS candidate: Boolean mapping, state decode/duplication,
fanout, ALM placement and routed path segments. This should identify a concrete
compiler transformation before another broad placement change.

A secondary read-only bound is preserved in `next-lead.md`/`next_lead.py` under
the raw directory. For the best baseline's remaining port1 enable users in
LAB (27,14), keeping every input delay nonregressing permits at most 205 ps
predicted output improvement, below the generic pass's 250 ps admission rule.
Increasing copy budget alone cannot reach that group; the pass also allows
only one copy per original driver/input-load set. This is predictor arithmetic,
not a legal-site or timing improvement. No relaxed-threshold copy is adopted.
