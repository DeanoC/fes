# RAM tester: HPS-ready timing failure and fallback, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), explain the shared
HPS-ready path that worsened during the last two Boolean-cut experiments.
FES base is `b924aba4`; compiler base is `d664a6f7`. The direct reproduction
control is the 115.021851 MHz command-cut run. The retained best remains
118.567703 MHz; this diagnostic does not promote the slower command-cut stack.

## Scope and method

Compiler `d7a186c0fe68849a707ebd34ac42fc09e5e4643e` on
`probe/hps-ready-timing-trace` adds `NEXTPNR_MISTRAL_READY_TRACE=<prefix>`.
It runs after final signoff timing and the existing analogue arc dump, without
changing mapping, placement, routing, delay arithmetic or calibration updates.
Disabled and empty-prefix modes return before allocating identifiers.

The trace records every connected HPS `cmd_ready_*` sink, its physical path,
cache and recomputation success, failure point, waveform sample counts, and
each hop's static/effective delay and observation provenance. It exports the
complete observed-pip map and all 256 calibration types, including the exact
binary32 prior. Calibration, cache, routes, placements and context checksum are
checked for equality before and after tracing.

An independent Python checker maps the logical endpoints through the actual
physical pin assignment, reconstructs the route from saved JSON, rebuilds all
calibration totals from observations, and reproduces every fallback component.
The scalar API sums each hop's maximum. Raw quad fallback sums rise/fall
components; the Context quad API then merges those with its post-override
accumulator seed. Those operations are checked separately. Successful cached analogue
results bypass the pip sums and have a separate selection check.

The full run starts from the same initialized-FSM synthesis, not an imported
routed context: Yosys `c156dd886`, Mistral `7ed06e21`, GPU 1, seed 2, HeAP
exponent 2, weight 10, beta 0.5 and replication budget four. Timeout roots,
retained DDR1 copy, timeout refinement, ready cut and command cut remain enabled.
Thirteen RTL files, generated headers and BUILD_ID remain unchanged.

## Exact full-flow reproduction

The corrected run completes in 616.52 seconds. All 12 pre-route stages match
the previous command-cut control, including 103,296 final pin states. The
complete routed module is identical: 20,016 cells, 21,763 netnames, all BELs,
routes, pin maps and settings. The 7,007,204-byte RBF is byte-identical, as are
the parsed full timing report and all 318,889 analogue dump records.

Memory remains **115.021850586 MHz**, pixel 80.951995850 MHz and capture
332.494110107 MHz. The sole warning is the memory target miss; no final hold
violations are reported. This is a diagnostic reproduction, not a speedup.
The retained best remains 118.567703247 MHz and 130 MHz is unresolved.

## Confirmed HPS-ready failure

All five connected HPS-ready arcs have a failed cached analogue result and
fail recomputation at hop zero, with reason `empty_input_wave`. Both rise and
fall input waveforms have zero samples. The first source is a GIN node in
circuit timing mode; no circuit hop completes. Mistral's input-wave builder
has no supported waveform for these HPS outputs. The earlier inference is now
confirmed on the actual routed design.

| Ready source | Sink | Scalar fallback ns | Raw fallback range ns |
| --- | --- | ---: | ---: |
| Port 0 | slot-free ALUT2 A | 3.395 | 2.845–3.395 |
| Port 1 | slot-free ALUT2 A | 4.687 | 3.859–4.687 |
| Port 1 | skid-enable root E | 5.076 | 4.135–5.076 |
| Port 1 | command-enable root B | 4.789 | 3.933–4.789 |
| Port 2 | slot-free ALUT2 A | 3.008 | 2.654–3.008 |

The checker verifies all 77 path-hop records, the complete 146,405-entry
observed-pip map and all 256 type aggregates. The prior is exactly binary32
1.25 (`0x3fa00000`). All five paths are complete; every Context scalar matches
the independently reconstructed fallback sum.

Three distinct unfinished first pips are nevertheless present in the observed
map at zero ps; port 1's three arcs share one such pip. Each has a 100 ps
static table entry. The unchanged collector merges `job.hops` without checking
`job.ok`, while the evaluator appends the zero-valued hop before attempting to
build its waveform. Thus a failed attempt can become an apparent observation.
It takes precedence over type calibration and contributes to the GIN totals.
Valid no-delay/P2P/generated hops can also be zero, so deleting all zero
observations would be incorrect. No observation filtering is changed here.

This also exposes a concrete optimization gap. `analogue_candidate_pass`
counts only sinks with a successful cached analogue result. With the recorded
state, all three ready nets have no counted sinks and are skipped by candidate
scoring even if candidates were generated. The separate broad rip/re-route
pass uses setup slack and still admits them. Per-net analogue regression
reversion also lacks counted ready sinks, although whole-design best-route
restoration remains. These statements describe the source gate and final
state, not a recorded history of every intermediate pass.

## Route topology under one timing basis

The same ready1-to-slot-free endpoints remain `GIN.51.64.18` and
`GOUT.30.26.68`, with the same cell BEL and physical pin mapping. Evaluate
all three saved paths using the captured final calibration:

| Saved path | Pips | Historical arc ps | Current full model ps | Current type-only metric ps |
| --- | ---: | ---: | ---: | ---: |
| Timeout refinement | 14 | 3473 | 3597 | 3597 |
| Ready cut | 15 | 3868 | 3986 | 3986 |
| Command cut | 17 | 4687 | 4687 | 4806 |

The longer path remains 1,090 ps slower than the refinement path with the
same complete model, versus the historical 1,214 ps difference. Most of the
trend therefore persists with fixed weights; cross-run calibration alone
does not explain it.

There is one relevant observation asymmetry. The first pip of the two older
paths is unobserved in the captured state and receives 119 ps of type-scaled
delay. The current path's unfinished first pip receives its observed zero.
The additional type-only column ignores all per-pip observations while keeping
the same type aggregates and prior. Its path difference is 1,209 ps. This is
another arithmetic metric, not a repaired model, a routed result or evidence
of physical timing accuracy. The type aggregates still include failed-hop
observations.

The static table sums are 2,679 / 2,959 / 3,537 ps. Full-model weighting and
path length both matter. Added load, physical topology and global routing
still covary across the original runs; this does not isolate electrical load.
Moreover, each shorter historical path uses five wires currently occupied by
other nets. Neither can simply replace the current path while unrelated
routes stay fixed. The occupancy check is necessary availability evidence,
not complete pip or tree legality.

## Diagnostic correction exposed a separate quad API problem

The first fresh attempt reached final signoff, then the diagnostic assertion
stopped execution: it expected Context's fallback quad to equal the sum of the
raw pip quads. No final RBF or routed JSON was produced by that attempt; its
artifacts are retained in `trace-c2-v1-aborted`.

The source explains the mismatch. `getArcDelayOverride` assigns the cached
quad to its output parameter before returning `cache.ok`. For a failed cached
analogue job, that quad is zero. `getNetinfoRouteDelayQuad` therefore loses its
initial extreme accumulator values even though the override returns false.
The subsequent union with positive fallback values keeps the minima at zero.
The scalar API uses a separate accumulator and is unaffected. The production
consumer of the quad API is SDF export; ordinary Fmax timing uses the scalar
API. This does not explain the 130 MHz miss.

The corrected diagnostic records the raw fallback, the post-override seed and
the reconstructed Context result separately. A new positive-fallback regression
fails on the original assertion, then passes with raw fallback 123 ps,
Context range 0–123 ps and scalar 123 ps. Production timing behavior is
unchanged. The first fixture's intentionally seeded all-zero delay had masked
this distinction. Versioned source, binary, test and failed-run evidence remain
available; the completed reproduction uses the corrected diagnostic.

## Verification and next work

The HIP build and 29 backend tests pass; one optional import case is skipped.
Three focused tests and the 197-cell absent/empty controls pass. Seven
independent checker methods cover 28 trace corruptions, numerical rounding,
calibration, physical pin/path mapping and the failed-cache accumulator case.
The actual trace, full-flow identity and derived measurements pass independent
review. Five derived artifacts regenerate byte-identically; frozen source,
binary and nested input hashes are checked. Parent consistency is checked on
the committed FES record.

The frozen corrected binary is `out/ramtest-ready-timing/tools/nextpnr-ready-timing`,
SHA-256 `5d1e95853fe78f46b5fa85ca92121383ca74e0550d9aa286da8d0af31a8f7520`.
Raw evidence and reproduction helpers live in `out/ramtest-ready-timing`;
the adjacent JSON seals their identities and measurements.

Next, separate correctness fixes from routing improvements. Reject unfinished
hop placeholders while preserving valid zero-delay observations, and repair
the failed-override output contract with focused tests. Any model correction
needs a new matched baseline. Then run a bounded fallback-aware ready-net
candidate search with graph, all placements, unrelated routes and calibration
fixed. Preserve all three ready loads and validate complete connectivity,
binding legality, downstream setup/hold and final RBF timing. Only a measured
improvement should be tested for stacking with the retained best.

Supported HPS waveform modelling requires characterized data; a lower estimate
alone is not a physical speedup. No equivalent-route Quartus calibration or
silicon evidence establishes model pessimism here. This work changes no
production RTL, recipe, compiler lock, ABI or shared contract. It is host-only,
with no FPGA programming or hardware acceptance, and opens no PR.
