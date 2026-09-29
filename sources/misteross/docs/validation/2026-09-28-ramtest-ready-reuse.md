# RAM tester: reuse the nearby enable replica, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), test the narrow
locality lead identified by the [Quartus comparison](2026-09-28-ramtest-ready-relaxed.md).
FES base is `2286fcde`; compiler base is `2c620ac6`. The diagnostic is
`f2097df2b5308853b409652e6ba46b3b9854a8cb` on
`probe/hps-ready-reuse`. The corrected retained
stack reaches 116.918037 MHz memory; 130 MHz remains unresolved.

## Bounded experiment

The worst ready-controlled sink is `skid_burstcount[5]`, BEL `(34,22,14)`.
Its original enable driver is at `(24,20)`, while an existing equivalent
replica at `(37,25)` serves four other burstcount bits. Both are ALUT3 mask
`0x32` with identical A/B/C nets and pin polarity. The target is the only
original-skid ENA user in its LAB. Reconnect that single ENA to the replica
before routing, taking the fanouts from 107/4 to 106/5.

No cells, LUT inputs or placements are added or moved. Refresh the affected
FF/control metadata and require legal occupied LAB controls. Fail closed on
unexpected logic, users, placement or pin state. Keep every other connection
unchanged. Export pre/post snapshots and absolute final setup/hold/domain
measurements for all 224 ready-controlled endpoints and all clocks. Compare
with the sealed corrected baseline; record measured regressions rather than
suppressing a rejected result.

This is a limited test of existing-replica assignment, not a recreation of
Quartus's entire bank placement. The next four original skid-address sinks
have setup slack −746 ps versus the target's −861 ps, so fixing only this
connection leaves substantial work before 130 MHz.

## Results

The fresh GPU 1 / seed 2 run finishes normally in 641.09 seconds. All six
pre-route stages exactly match the corrected retained baseline. The strict
fresh proof verifies the single ENA reconnection, all 14,927 original
placements and pin states, and complete occupied-BEL legality before routing.

| Clock | Retained baseline MHz | Reuse candidate MHz |
| --- | ---: | ---: |
| Memory | 116.918037 | 117.868927 |
| Pixel | 76.958595 | 74.688179 |
| Capture | 332.494110 | 332.494110 |

The target setup slack improves from −861 to +19 ps, an **880 ps gain**.
Its hold slack drops from 4457 to 3075 ps and remains positive. Across all
224 ready-controlled endpoints, 87 improve and 137 regress in setup; deltas
range from −407 to +880 ps. No guarded endpoint gains a new or worsened
negative hold slack. These are model-based timing results, not physical
calibration or hardware acceptance.

Pixel still meets its 74.250069 MHz constraint, but loses most of the previous
margin. The candidate fails the existing all-endpoint/all-clock nonregression
policy and is retained separately as a measured tradeoff. It is not added to
the accepted stack. The retained baseline remains **116.918037 MHz**, and
**130 MHz remains unresolved**.

The current memory critical path changes to DDR1 `idle[19]` through the
mapped timeout/control cone to `ddr1_address[13].ENA`. Its 8.484 ns effective
setup path includes five LUTs: 1.274 ns logic and 6.658 ns routing, plus
0.731 ns clock-to-Q, 0.017 ns skew and −0.196 ns setup. The worst remaining
ready-controlled sinks are skid-address FFs, at −734 ps. Fixing the selected
ready sink therefore exposes another limiting cone.

This full flow preserves placements, not routes. It changes 13,272 of the
21,761 common named routing attributes; aliases mean this is not a count of
unique physical nets. The reported gain cannot be attributed solely to the
one sink's route. Final trace/calibration checks pass with the same documented
three failed HPS recomputations and valid completed-hop observation rules.

## Decision and next experiment

The useful finding is that reassignment to equivalent existing enable logic
can remove the selected sink's violation without new LUTs or placement moves.
A fresh full route also changes other timing, so this candidate does not
establish a safe cumulative improvement. Keep its source and evidence for
comparison; do not silently replace the corrected baseline.

The Quartus solution still supports coordinated locality work. A bounded
control-bank relocation must include the local register DATAIN logic and
check its external data/clock/output boundary, not only the ready-controlled
pins. Moving banks nearer the HPS output can otherwise lengthen their data
paths. Any next experiment must also track the now-exposed DDR1 timeout
cone and the pixel clock, while keeping unrelated placements fixed.

## Verification and scope

The compiler suite passes 46 tests with one optional import test skipped.
Three focused tests cover actual LUT/input/polarity equivalence, clock guards,
local FF metadata refresh and disabled-hook neutrality. The 197-cell absent
and empty controls exactly match the parent. Ten independent checker tests
pass, including wrong logic, polarity, clock, extra rewiring, placement,
partial LAB group, illegal-site and imported connected-port corruption.

The imported preflight preserves all 14,927 cells/placements and 103,286 pin
states against the retained baseline, with every occupied BEL legal. JSON
import drops top ports, unused nets and disconnected empty ports, so its
separate comparison explicitly excludes those fields. The fresh-flow check
requires exact baseline equality. Only the moved endpoint changes in
preflight prediction: setup +610 ps, hold −610 ps, with positive hold slack;
these predictions are not routed results.

Host-only compiler research. No production RTL, recipe, compiler lock, ABI,
shared contract or hardware changes. The full-run structural proof, timing
comparison and trace verification pass. All eight derived evidence files
regenerate byte-for-byte, with 147 nested input hashes verified. The parent
consistency check is run against the committed selection before push. Raw
evidence is `out/ramtest-ready-reuse`; the adjacent JSON records source and
artifact hashes.

Frozen compiler SHA-256:
`2f79da111c33589ea7d2625ac2c5858e4ac61a5dc12b5d0d9cf6e0ea9c3955bb`.
