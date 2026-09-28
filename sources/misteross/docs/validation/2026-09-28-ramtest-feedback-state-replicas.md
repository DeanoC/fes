# RAM tester: Quartus feedback paths and a guard-state copy trial

For [FES #264](https://github.com/DeanoC/fes/issues/264), examine the
feedback/state boundary left by the earlier 387- and 655-cell placement moves.
The retained routed OSS baseline is 116.918037 MHz memory. The 117.868927 MHz
enable-reuse route remains a separate tradeoff. FES base is `82a4f08c`.

## Saved Quartus fit

A new TimeQuest query uses a private copy of the previously hash-matched
Quartus fit. It performs no synthesis or fitting. It selects exactly one
source and destination register for each path:

| Quartus path at 130 MHz | Logic levels | Data delay | Routing | Setup slack |
| --- | ---: | ---: | ---: | ---: |
| Port 1 `skid → owed_one` | 4 | 4.474 ns | 3.088 ns | +2.982 ns |
| DDR1 `idle[22] → write_cycles[12]` | 3 | 4.766 ns | 2.716 ns | +2.272 ns |

The fitted `skid → owed_one` path runs through two `enter_write` LUTs and two
`Equal1` LUTs. The first source and its `owed_one` sink are within LABs
(35,62) and (34,64). The `idle → write_cycles` path uses three LUTs from
(33,59) to (24,59). These are timed routes in the Quartus fit, not a calibrated
prediction for our different mapped netlist.

Quartus has both original and `~DUPLICATE` flip-flops for port 1 `draining`
and `finishing`. Each pair occupies adjacent FF sites in the same LAB at
(34,60) or (35,60). The duplicate outputs feed the local `enter_write` logic.
This is a precise local fanout/placement clue; the saved fit does not show a
state copy relocated to a distant bank.

TimeQuest reports two existing SDC warnings about the second PLL clock name.
Both queried paths launch and capture on the measured memory clock 0, so that
warning does not remove either relationship.

## Controlled OSS trial

The private mapped-netlist transform copies only the port 1 `draining` and
`finishing` MISTRAL_FF cells. Each copy has identical input, clock and control
connections and the same default-low power-up. Four existing LUT input pins
that feed `enter_read` and DDR1 `write` use the new outputs; all other original
users stay on the originals. One structural test checks every existing cell,
port and net except those four named pins, and rejects altered FF controls.

The original and transformed netlists use the same source RTL, existing
nextpnr binary, seed 2, HeAP weight 10, critical exponent 2, enable replication
budget 4, expanded timeout roots, constraints and placement hooks. This is
an opt-in private synthesized-netlist test, not an RTL or production recipe
change. Placement and timing predictions are compared before deciding whether
to run a full route.

## Result

The structural transform passes its real-netlist test: two new FFs, four exact
rewires, and no other changed source cell, port or net. The saved Quartus fit
matches 173 input files in the private project copy. TimeQuest regenerated two
timing caches in the private copy, which are identified separately.

| Placement prediction, before the retained-enable hook | Original | Two copies |
| --- | ---: | ---: |
| Memory | 77.04 MHz | 72.30 MHz |
| Pixel | 84.99 MHz | 89.17 MHz |
| Capture | 397.57 MHz | 297.49 MHz |

The original control completes and reproduces the previous 77.04 MHz placement
prediction. The copied-FF trial reaches placement and timeout partitioning,
then stops at a retained-enable assertion. The selected `burst_end` FF moved
from LAB (37,23) into a carry-containing LAB at (4,9). The diagnostic's
conservative ordinary-LAB precondition is false; this does not establish an
illegal BEL or a complete routed result. The copied trial also selects zero
automatic enable replicas versus one in the original control.

The global placement changed substantially: original `draining` and
`finishing` shift from x39/y32 and x39/y27 to x2/y9 and x2/y6; their copies
land at x2/y9. The skid FF moves from x30/y26 to x3/y14. Quartus instead
places the two original/copy pairs adjacent in LABs (34,60) and (35,60).
The same seed does not isolate the copies' local delay effect when the placer
reshuffles the entire design.

The trial is rejected as a standalone route candidate: its placement memory
prediction is worse, and it never completes the accepted retained-enable
stage. The original RTL and accepted routed 116.918037 MHz result stay in
place. No gain is stacked, and 130 MHz remains unresolved.

The next physical test should preserve the accepted placement while measuring
the complete timing boundary of any local state copies or feedback-cone move.
It must reject a copy that improves a control input by making its own D/control
path, DDR1 idle/state path, or pixel/capture path critical. A full route is
justified only after that fixed-placement check passes.

## Scope

Host-only compiler investigation. No hardware operation, production RTL,
recipe, lock, ABI or shared contract change. Raw evidence is
`out/ramtest-feedback-cone`; the adjacent JSON records identities and
measurements. The real-netlist structural test passes. Re-running the analysis
regenerates three derived records byte-for-byte, verifies 24 recorded input
hashes and compares 173 copied fit files. The published summary seals 219 raw
artifacts and 20 external source inputs. The parent
consistency check is run against the committed FES report.
