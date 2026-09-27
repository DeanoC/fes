# RAM tester: paired placed-LUT composition, 2026-09-27

Follow-up to [generic enable replication](2026-09-27-ramtest-enable-replication.md)
for [FES #264](https://github.com/DeanoC/fes/issues/264), based on FES `c35e6d3d`
and nextpnr `71c513e1`. **Neither candidate improves the 116.44 MHz baseline.**
This host-only experiment separates moving the remaining port-1 enable LUT
from combining its two logic stages. The RTL, producer lock
and shared contracts are unchanged.

## Paired experiment

The 116.4415 MHz candidate is limited by HPS `cmd_ready_1` through a shared
ALUT2 and the original ALUT3 enable driver to `skid_address[27].ENA`.
The earlier pre-placement composition experiment also changed placement across
the design, so its regression did not isolate the effect of combining LUTs.

Keeping a combined ALUT4 at the original site is illegal: LAB (24,20) already
uses all 42 input resources, and the extra LUT input requires 43. A backend
preflight checks this directly. Instead, both modes perform the same bounded
legal-site search and select `MISTRAL_COMB.24.21.19`, one LAB away from
`MISTRAL_COMB.24.20.36`. Search prioritizes distance, then a geometric
maximum-input-plus-maximum-output estimate, then BEL name. The estimate is a
selection heuristic, not routed timing.

- **Relocate:** move the original ALUT3 without changing its function or wiring.
- **Compose:** place an ALUT4 at exactly the same site, absorbing the upstream
  ALUT2 function into this consumer. Keep the shared ALUT2 for its other users.

The original function is `!slot_free && (enter_read || enter_write)`, with
`slot_free = cmd_ready_1 || !cmd_valid_1`. The combined ALUT4 has input order
read, ready, valid, write and mask `0x3020`. All 16 input combinations match
an independent Boolean oracle. The original enable net retains 107 FF users;
the existing replica and its four FF users are unchanged. The shared ALUT2
retains seven users in relocation and six in composition.

This is a fixture-specific diagnostic, committed as
[`d5b8aa56`](https://github.com/DeanoC/nextpnr/commit/d5b8aa56709bcac3bdf8beeccfbb432d43ca8209)
on branch `probe/ramtest-placed-composition`, not a general compiler optimization. Enable it with
`NEXTPNR_MISTRAL_PLACED_COMPOSITION=<snapshot-prefix>` and
`NEXTPNR_MISTRAL_PLACED_COMPOSITION_MODE=relocate|compose`. With no prefix, it
performs no identifier allocation or context mutation.

## Controlled result

| Candidate | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Generic enable replication baseline | **116.4415** | **77.9059** |
| Relocation only | 114.1553 | 75.1371 |
| Composition at the same site | 112.5366 | 73.6214 |

Both fresh flows use the same initialized-FSM synthesis,
BUILD_ID, seed 2, HeAP exponent 2, beta 0.5, timing weight 10, enable-replication
budget four, original constraints, GPU 1 and frozen compiler binary.
Both complete legally, emit RBFs, and finish signoff timing without reported
hold violations. Capture-clock Fmax is 332.4941 MHz in both. Relocation still
passes the 74.25 MHz pixel target; composition fails it. Neither reaches the
130 MHz memory target.

Both actual before snapshots exactly match the measured baseline module and
its 103,061 pin states. Both select `MISTRAL_COMB.24.21.19`; the other 14,890
placements and all original pin states remain unchanged. The only added pin
in composition is the ordinary, uninverted ALUT4 D input.

The relocation result's worst memory path starts at `ddr1_test.idle[22]` and
ends at `ddr1_test.burst_end_MISTRAL_FF_Q.ENA`: six LUT arcs, 1.412 ns logic
and 6.776 ns routing, totaling 8.760 ns with clock-to-Q, skew and setup.
Composition's worst path starts at the same idle bit and ends at
`ddr1_test.write_cycles[12].ENA`: five LUT arcs, 1.315 ns logic and 7.041 ns
routing, totaling 8.886 ns. These are different endpoints, not a matched-path
logic-delay comparison.

This paired result rejects this particular composition/site combination.
It does not show that LUT composition is generally harmful: full-design
routing changes, and the chosen site is a bounded heuristic, not an exhaustive
optimum. Retain the 116.44 MHz candidate. The next controlled compiler target
is HPS pin-aware placement geometry, described below, with the remaining
idle/control distribution as another measured limit.

## Verification and limits

The HIP build passes 19 backend tests, with the optional real-snapshot test
skipped in the ordinary suite and passed separately. The paired synthetic
fixture checks function, site, physical input mapping, preserved placements
and indexed net-user stores. Default and mode-only flows reproduce the entire
197-cell reference module. The independent checker accepts both modes and
rejects ten corrupted snapshots, including wrong masks, connections, placements,
missing pins and changed polarity.

Before/after snapshots include pin-state TSV sidecars because ordinary JSON
omits folded constants and polarities. The independent checker constrains all
changes to the selected cell and proves the truth table. A separate pair check
compares each before snapshot with the measured generic-replication baseline
and checks that both actual runs select the same destination. Whole-design
rerouting remains enabled, so changing this cell can alter timing in other
cones even though their placements are fixed.

The optional preflight imports a saved placement only to check legality. Its
test importer restores pin states and the known second PLL output connection:
ordinary JSON drops mixed scalar/indexed PLL output information. It is not a
route restart or a substitute for the in-process legality assertions in each
fresh full flow.

Independent source and evidence reviews found no blocking issues. All 76
artifact hashes and ten external-input hashes verify; evidence regenerates
byte-identically. The adjacent [JSON record](2026-09-27-ramtest-placed-composition.json)
contains the full measurements and compiler provenance.

Raw artifacts, frozen compiler, tests, snapshots and regeneration helpers are
under `out/ramtest-placed-composition/`. No PR or hardware operation is included.

## Separate HPS endpoint lead

The measured 116 MHz route starts `cmd_ready_0/1/2` at physical routing nodes
`GIN.51.64.19/.18/.17`, while their common HPS BEL is at (52,53). The placement
predictor and HeAP equations use the BEL/cell location for these pins. This is
an endpoint-geometry approximation worth isolating in a subsequent experiment;
changing prediction alone would leave HeAP's geometric anchor unchanged.

For ready-1 to the LUT at (30,26), substituting physical-pin coordinates into
the current prediction formula changes 4.370 ns to 5.435 ns. This illustrates
the geometry difference, not improved accuracy: routed delay is 3.578 ns.

All three ready signals are absent from the analogue arc dump. Source inspection
strongly supports unsupported initial-wave generation for HPS pins as the reason:
the waveform builder covers LAB/MLAB, GPIO and certain clock nodes, but not HPS.
A failed analogue override falls back to pip/wire table delays, which may have
calibration adjustments. The exact failure return was not instrumented. Thus
full analogue signoff does not imply direct analogue simulation of this HPS
arc; neither model pessimism nor Fmax causality has been established. These
paired runs change neither the predictor nor the delay model.
