# RAM tester: exact ready-net shortest-path comparison, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), compare the retained
ready1 route with an exact shortest path under the same occupied routing and
frozen calibrated costs. FES base is `a18455c3`; nextpnr base is `28e8d1fc`.
The compiler candidate is `7e4130f6fa2644a1f51e47706d2fcb2acffb53a8` on
`probe/hps-ready-shortest`. The [preceding candidate experiment](2026-09-28-ramtest-ready-fallback.md)
rejected two slower heuristic routes and retained **116.918037 MHz**.

## Experiment

The opt-in diagnostic runs after normal routing and analogue repair.
`NEXTPNR_MISTRAL_READY_FALLBACK=<prefix>` together with
`NEXTPNR_MISTRAL_READY_SHORTEST=1` selects exact search; absent or empty
shortest mode retains the prior heuristic path. Dijkstra searches the architecture's directed pip graph from ready1's HPS source to
its sole slot-free LUT sink. It excludes resources owned by unrelated nets,
permits resources already owned by ready1, and uses the frozen calibrated
scalar pip delays. Mistral wire delay is zero. The search does not change
calibration or manufacture a supported HPS analogue waveform.

The original graph, placements, pins and unrelated routes stay fixed. Any
shortest candidate must bind and connect successfully, preserve setup on all
224 affected endpoints, avoid new or worse hold violations, improve worst
setup by at least 20 ps and avoid regression on any clock. The existing
routed-baseline placement-check vector is preserved. Failure of the guard
retains the original tree.

A recorded shortest path is not alone an optimality proof. The accompanying
certificate records explored distances and edges so an independent checker
can validate the cost arithmetic, frontier bound and returned path. The
checker independently compares every examined physical outgoing edge
with `sx120f-r.txt.xz`, reconstructs calibrated costs and checks baseline
ownership. Generated-wire adjacency and architectural availability predicates
remain source-reviewed compiler operations; a graph optimum only becomes an
accepted route after binding and full timing checks.

## Results

The fresh GPU 1, seed 2 full flow finishes normally in 597.85 seconds. All
six pre-route stages match the corrected retained baseline. The pre-probe and
final RBFs match it exactly, as does the final timing JSON.

Dijkstra returns **3602 ps**, the same scalar delay as the original ready1
route. Its candidate is the same 15-wire/pip/strength tree. It settles
657,601 nodes, expands 657,600, and records 7,548,058 examined edges before
settling the sink. The independent certificate check passes: 817,828 discovered
nodes, 7,203,098 physical edges checked against the device database, and
344,960 generated edges covered by source review. The captured graph includes
7,406,169 allowed edges, 141,376 blocked by other nets' wires and 513 rejected
by architectural pip availability.

| Measurement | Original | Exact candidate / final |
| --- | ---: | ---: |
| Ready1 scalar fallback | 3602 ps | 3602 ps |
| Worst affected setup | −861 ps | −861 ps |
| Memory Fmax | 116.918037 MHz | 116.918037 MHz |
| Pixel Fmax | 76.958595 MHz | 76.958595 MHz |
| Capture Fmax | 332.494110 MHz | 332.494110 MHz |

The candidate binds successfully, but its zero gain fails the 20 ps minimum.
All 224 endpoint setup/hold pairs are unchanged. The original remains selected;
no optimization is added to the retained stack. Final signoff reports no hold
violations. **130 MHz remains unresolved.**

The final RBF SHA-256 remains
`b477f3d50f1bb9355d5ca8d6564ac0a78ddc82f90e2b7e06ca0674d10957d173`.
Rebinding changes the ready1 ROUTING string's entry order only; its parsed
wire/pip/strength set stays identical. No unrelated routing, placements or
calibration changes.

## Verification and interpretation

Ten focused compiler tests pass: five new exact-search tests and five prior
fallback tests. The new tests cover weighted optimum, obstacles, zero-cost
cycles, deterministic ties, unreachable sinks, invalid costs, actual backend
binding and disabled-mode identifier neutrality. The backend fixture finds
a 9 ps route versus the original 40 ps. Absent, empty and mode-only controls
preserve the entire 197-cell fixture module against the parent compiler.
The initial RED build fails on the missing new API; it is not a reproduction
of a preexisting heuristic-router defect.

The full backend suite passes 42 tests, with one optional import case skipped.
Twenty-two independent checker tests pass. The actual full-design certificate,
224 endpoint/domain guards, baseline identity and frozen-model checks pass.
All seven derived evidence files regenerate byte-for-byte; 186 nested input
hashes verify. Independent source and artifact review find no blocker.

This closes one specific hypothesis: the previous ready1 route was not slower
because the candidate generator missed a cheaper path in this fixed occupied
graph. The existing route is a minimum scalar-cost path under the captured
calibration. Although the GPU search uses a weighted estimate and bounded
diversity, replacing it with exact search brings no gain for this net and
configuration. This does not prove global timing optimality or physical model
accuracy.

The next useful comparison is a read-only relaxed-occupancy shortest path,
with architectural restrictions and costs unchanged. Its lower bound would
show whether moving other nets could help this ready arc, and its path would
identify the blocking nets. Any negotiated reroute would then need to preserve
the displaced nets' timing as well as the 224 ready endpoints. If relaxing
occupancy offers little improvement, the evidence instead points toward
placement/control-cone changes or a separately justified timing-model
investigation. No competing-net reroute is adopted in this stage.

This is host-only compiler research. It changes no production RTL, recipe,
compiler lock, ABI or shared contract and performs no hardware operation.
An improvement in the calibrated host model does not prove physical timing
accuracy or silicon closure. Raw evidence lives in `out/ramtest-ready-shortest`;
the adjacent JSON records its hashes.

Frozen compiler SHA-256:
`048fc91a3e8f308b0c49b811752d8d1a40f53797581a40ef390a551ea32ed1be`.
