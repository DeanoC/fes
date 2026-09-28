# RAM tester: isolated ready-net fallback search, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), test whether the
HPS-ready candidate-scoring exclusion can be overcome without changing the
timing model or surrounding routes. FES base is `0f07cb0f`; nextpnr base is
`cfa17a49`. The control is the corrected retained stack at **116.918037 MHz**,
documented in [completed analogue observations](2026-09-28-ramtest-observation-validity.md).
The diagnostic is compiler commit `28e8d1fcc6f062f6153b7f549777ef0b6ff0ed6c`
on `probe/hps-ready-fallback`.

## Bounded experiment

The existing analogue candidate pass ignores sinks whose cached analogue
simulation failed. HPS-ready outputs lack supported input waveforms, so their
candidates are excluded even though signoff times them with calibrated scalar
fallback. This experiment adds an opt-in final pass for `cmd_ready_1` after
normal routing and analogue repair, before final bitstream signoff. It reuses
the existing candidate router and evaluates up to eight alternative trees.
`NEXTPNR_MISTRAL_READY_FALLBACK=<prefix>` enables the fixture-specific pass;
absent and empty values return before allocating identifiers.

The graph, every placement, physical pin assignments, unrelated routes,
observation map, type calibration and prior remain fixed. Each bound candidate
gets a fresh bitstream configuration and full timing analysis. Failed analogue
results remain failed; fallback values are never relabelled as analogue
observations. The existing failed-cache quad/SDF issue is unchanged.

The complete ready1 fanout is derived from the selected graph: **224 sequential
endpoints**, comprising 223 FF enable pins and one synchronous-clear pin,
through five LUTs. It includes 109 command enables, 107 original skid enables,
four existing replica users, three direct slot-free users and one skid-register
enable. Both packed and routed graphs give the same closure. All endpoints
must have one timed rising-edge memory launch/capture domain pair.

A candidate must preserve every endpoint's setup slack, avoid new or worsened
hold violations, improve the closure's worst setup slack by at least 20 ps,
and avoid regression on any clock's Fmax. Positive hold margin may shrink while
remaining nonnegative. The best eligible worst-slack result wins; ties keep
the earlier candidate, and no eligible candidate keeps the original route.

The candidate router reserves unrelated routing. Actual binding, sink
connectivity and architectural checks supplement independent tree and
occupancy checks. Each trial restores the original tree and reconfigures the
bitstream before the next one. Before/after RBFs and refreshed routed snapshots
allow comparison with the historical control and the normal final outputs.

## Matched execution and results

The fresh V2 full flow finishes normally in 600.37 seconds. All six pre-route
stages match the corrected baseline; the pre-probe and final RBFs both match
its bytes exactly. The generator returns two distinct alternatives out of
the eight-candidate cap:

| Route | Worst affected setup | Memory Fmax | Decision |
| --- | ---: | ---: | --- |
| Original | −861 ps | 116.918037 MHz | Retained |
| Candidate 0, diversity variant 2 | −1869 ps | 104.591568 MHz | Rejected |
| Candidate 1, diversity variant 3 | −2054 ps | 102.606194 MHz | Rejected |

Both alternatives bind and connect successfully, but worsen setup at all
224 endpoints and memory Fmax. Neither introduces or worsens a hold violation.
No candidate meets the acceptance policy. Final pixel timing
remains 76.958595 MHz and the capture clock 332.494110 MHz; signoff reports no
hold violations. The original ready1 scalar fallback remains 3602 ps and the
memory critical path remains 8.553 ns.

Every original placement (14,927 cells), all unrelated routes, pin assignments
and the frozen model are preserved. The final RBF SHA-256 is
`b477f3d50f1bb9355d5ca8d6564ac0a78ddc82f90e2b7e06ca0674d10957d173`.
The ready1 ROUTING string changes ordering when its original tree is rebound;
the wire/pip/strength set and RBF remain identical. The three ready arcs still
fail analogue recomputation at their first hop; no false observations are
added to the 146,023-entry map or its 256 calibration types.

The run starts from the unchanged initialized-FSM synthesis with GPU 1,
seed 2, HeAP exponent 2, weight 10, beta 0.5 and replication budget four.
Expanded timeout roots and the retained DDR1 enable copy remain enabled.
The slower timeout-refinement, ready-cut and command-cut experiments remain
disabled. No routed-JSON replay substitutes for the calibrated control:
replay does not fully restore calibration and packed clock constraints and
locks imported routing.

## Verification and next work

The first full attempt, retained in `candidate-c2`, reached the probe with a
byte-identical control RBF. It generated two alternatives with worse setup
slack and retained the original tree, then stopped at an overly broad final
placement-validity assertion. No completed final RBF or timing JSON was
produced; that attempt supplies no qualified timing result.

`lab_pre_route` inserts FF pass-through LUTs and reconnects the real DATAIN
ports, while the FF's cached `ffInfo.datain` remains in its pre-route form.
The placement-stage predicate still uses that cached input. Consequently it
can reject an unchanged routed design. The corrected diagnostic captures each
baseline cell/BEL placement-check result and requires that vector to remain
identical throughout the search, alongside actual target binding/connectivity,
context checks and exact graph/placement/pin preservation. It does not refresh
the live FF cache or change the production placement model. The first compiler,
binary and tests are preserved in `v1-compiler`.

The completed run records 20,014 bound cells. The unchanged placement-stage
predicate is false for 8,081: 4,722 report LAB input count, 2,486 both ALM and
LAB input count, and 873 ALM alone. These are per-cell checks of shared
placement structures, not a count of independently illegal physical sites.
The entire boolean/reason vector is identical before and after the search.
The real route-through fixture isolates one cache-driven cause; this experiment
does not repair or independently classify every baseline predicate failure.

Five focused compiler tests pass: guard policy, disabled identifier neutrality,
real failed-HPS fallback reevaluation without false success or learning, a
CPU candidate fixture with an unrelated reserved branch, and a real LAB
route-through fixture reproducing the stale placement-cache predicate. The
synthetic CPU fixture generates a distinct alternative and preserves the original bindings;
it is not a full-design legality proof. The backend suite passes 37 tests,
with one optional import case skipped. Both absent/empty controls preserve
the entire 197-cell fixture module exactly.

Six fanout and eleven independent probe-checker tests pass. They cover missing
endpoints, polarity/domain mismatches, frozen-model changes, conflicting or
disconnected trees, setup/hold regressions and incorrect selection. Initial
RED was the absent new API/header or checker module; it is not presented as
a behavioral reproduction of a preexisting failing test. Independent source
review finds no blocker.

The independent full-design checker confirms exact control RBF identity,
complete endpoint/domain guards, deterministic rejection/selection, preserved
placement-check results, and frozen calibration. All seven derived evidence
files regenerate byte-for-byte, with 180 nested input hashes verified. The existing retained stack remains selected: this diagnostic
adds no QoR gain and neither rejected tree is stacked onto the baseline.
**130 MHz remains unresolved.**

The next search should compare the existing route against an exact shortest
path under the same frozen calibrated pip costs and occupied graph. Scalar
fallback sums these costs and Mistral wire delay is zero, making that
comparison possible without changing the model. Any resulting path still
needs architectural binding checks and the same setup/hold guards. The
current GPU generator uses bounded diversity heuristics; rejecting its
alternatives cannot prove that no better legal route exists. A lower bound
from that comparison would guide whether to investigate candidate generation
or carefully limited rerouting of surrounding nets.

Raw artifacts and reproduction helpers are in `out/ramtest-ready-fallback`;
the adjacent JSON seals their identities. This is host-only compiler research,
with no production RTL, recipe, compiler lock, ABI, shared contract or hardware
change. No PR is opened. A host-model timing gain would not establish physical
model accuracy or silicon timing closure.

Frozen compiler SHA-256:
`27045621885879851d532674ba399ee7247f8099779432a1a1a8901d32c62a58`.
