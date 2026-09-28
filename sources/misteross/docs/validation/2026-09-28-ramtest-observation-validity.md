# RAM tester: completed analogue observations, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), correct the invalid
zero-delay observations identified by the
[HPS-ready trace](2026-09-28-ramtest-ready-timing.md), then establish fresh
timing baselines. FES base is `323949ec`; nextpnr base is `d7a186c0`.
The correction is compiler commit `cfa17a490df916ab64579d630070d328da189e06`
on `fix/mistral-completed-observations`.

## Correction and scope

`mistral/delay.cc` previously appended a zero-valued observation before a
hop completed. An unsupported HPS input waveform could therefore leave an
unfinished hop in the calibration map as an observed zero. This suppressed
that pip's fallback delay and affected its routing-type totals.

The evaluator now constructs a local hop and appends it only after completion:
immediately for generated, P2P and no-delay hops, or after both circuit edges
are simulated. Completed prefixes remain available when a later hop fails.
The collector still merges each pip's maximum across samples and rebuilds
type totals from the map. Delay arithmetic, cached-override behavior and
candidate scoring are unchanged. This prevents new invalid observations; it
does not clean a preexisting imported map.

This is a correctness correction, not evidence of faster physical routing.
Calibration influences both reported delay and subsequent routing decisions,
so historical and corrected full-flow MHz values are different model baselines.
The failed cached-override quad/SDF bug remains a separate task. HPS-ready
waveforms remain unsupported, and analogue candidate scoring still excludes
these failed arcs.

## Tests and matched configurations

Regression tests reproduce the real HPS failure through
`compute_analogue_arcs(true)`: no observation or type count may be emitted.
A synthetic path exercises completed generated/no-delay/P2P zero prefixes
before that physical failure, including repeated aggregation and maximum
retention. Its connectivity is a test construction, not a route-legality claim.
A configured real LAB circuit hop preserves the pre-fix measurements:
63/83 ps rise/fall observations and 60/63/79/83 ps delay quad.

Two full routes start from the original initialized-FSM synthesis, with GPU 1,
seed 2, HeAP exponent 2, weight 10, beta 0.5 and replication budget four.
Both enable expanded timeout roots and the retained DDR1 enable copy.
The command-cut control additionally enables timeout refinement, ready cut
and command cut. The retained-best configuration excludes those three
experiments. Thirteen RTL files, generated headers and BUILD_ID are unchanged;
Yosys is `c156dd886`, Mistral `7ed06e21`.

All pre-route modules and physical pin states are compared against their
respective historical configurations. Final routes may differ because the
calibration changes. Independent checks reconstruct every recorded HPS-ready
path, all per-pip observations and 256 type totals, then reproduce the exact
scalar and quad fallback arithmetic. A separate fresh-map assertion rejects
any observed pip sourced by the three failed HPS-ready GIN roots, including
branches left from earlier reroutes.

## Full-flow results

| Configuration | Historical memory MHz | Corrected memory MHz | Corrected pixel MHz |
| --- | ---: | ---: | ---: |
| Command-cut control | 115.021851 | 113.688049 | 80.951996 |
| Retained-best stack | 118.567703 | 116.918037 | 76.958595 |

The corrected retained-best stack remains the stronger of these two
configurations. **116.918037 MHz is the new qualified baseline**, with the
118.567703 MHz result retained as historical evidence under the old model.
The correction stacks with the retained compiler optimizations. It does not
promote the command-cut/refinement experiments, and neither run closes 130 MHz.

The command-cut run completes in 605.63 seconds. All 12 pre-route snapshots
match the historical control. All 14,928 placed cells retain their BELs, with
5,088 route-through buffers added. The RBF differs; 13,289 of 21,763 common
named-net `ROUTING` attributes differ. This counts serialized route attributes,
not necessarily distinct electrical nets.

The command control's critical memory path is now DDR2 `idle[21]` to
`write_cycles[31]` ENA, from FF `(39,19,10)` to FF `(10,36,44)`, through five LUTs.
Its 8.796 ns total comprises 7.052 ns routing, 1.203 ns logic, 0.731 ns clock
to Q, 0.006 ns skew and -0.196 ns setup. The old HPS-ready path is no longer
the single reported memory critical path. The memory target still fails;
pixel and capture clocks pass, and no final hold violations are reported.

All five ready arcs still fail at their first hop because the HPS waveform is
unsupported. The corrected scalar fallbacks are 3.516 ns for port 0,
4.609/5.606/5.081 ns for port 1's slot/skid/command branches, and 3.129 ns for
port 2. None of the three failed ready roots contributes an observed pip,
including earlier reroute branches. The checker verifies 79 path hops,
146,045 observations and all 256 type totals. There are no GIN zero entries
in this fresh run; 32,629 other zero observations remain. This does not imply
that every historical GIN zero was independently proven invalid.

The retained-best run completes in 580.21 seconds. Its six pre-route stages
match exactly; all 14,927 placed cells retain their BELs, with 5,087 added
route-through buffers. The RBF differs, and 13,287 of 21,761 common named-net
route attributes differ. Pixel and capture timing are unchanged from their
historical values; memory is the sole warning, with no final hold violations
reported.

Its critical path is now HPS `cmd_ready_1` through the slot-free and skid-enable
LUTs to `hps_ddr.port1.skid_burstcount[5].ENA` at FF `(34,22,14)`.
The 8.553 ns total contains 6.561 ns routing, 0.800 ns logic, 1.177 ns clock
to Q, 0.211 ns skew and -0.196 ns setup. The three routing arcs are
3.602 / 1.407 / 1.552 ns. The skid-enable LUT is the original 107-user ALUT3,
not the four-user replica. HPS-ready routing therefore remains a relevant
optimization target on the corrected retained stack.

The best-stack trace verifies three failed ready arcs, 39 path hops,
146,023 observations and 256 type totals. Ready0/1/2 scalar fallbacks are
3.417 / 3.602 / 3.028 ns. None of the failed ready roots contributes an
observation. This run also has no GIN zero entries and retains 32,586 other
zero observations. The existing failed-cache output behavior still clamps
Context quad minima to zero; this is independently checked and left unchanged.

## Verification and next work

Six focused tests pass, including three behaviorally failing pre-fix cases.
The full backend suite passes 32 tests; one optional full-design import is
skipped. Absent/empty diagnostic controls each preserve all 197 fixture cells
and the complete top module. Independent source review finds no blocker.
Nine independent checker methods include inherited trace corruptions and new
fresh-map negatives; the old production trace is correctly rejected.

Both actual traces and timing results pass independent review. Derived
structural proofs, trace checks and measurements are regenerated exactly;
frozen source, binary and nested input hashes are checked. Parent consistency
is checked on the committed FES record.

Keep the failed-override output fix separate, with focused scalar/quad and SDF
tests. For further timing work, use the corrected retained stack as the primary
baseline and the command-cut graph as a diagnostic control. A bounded
fallback-aware ready-net candidate search must preserve the selected graph,
placements, unrelated routes and calibration, and validate all affected setup
and hold endpoints. Derive the fanout guard from the selected configuration;
do not assume the command-cut experiment's 720-pin guard describes the best
stack. Compare under the same corrected model before promoting an improvement.

Unsupported HPS waveforms still require characterized data. Neither these
correctness tests nor the changed MHz values establish physical model accuracy
or explain away the remaining Quartus gap.

Raw artifacts and reproduction helpers are in
`out/ramtest-observation-validity`; the adjacent JSON records their identities.
The frozen compiler SHA-256 is
`47ae7e0e939199866adefb85c1f2735738d659df4307c7ba8bfa191fd11c47f2`.
This work changes no production RTL, recipe, compiler lock, ABI or shared
contract. It is host-only, without FPGA programming, hardware acceptance or PR.
