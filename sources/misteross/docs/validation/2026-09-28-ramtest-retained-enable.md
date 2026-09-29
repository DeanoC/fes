# RAM tester: retained DDR1 enable distribution, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), investigate the
remaining shared enable net identified by the
[expanded timeout-region experiment](2026-09-28-ramtest-timeout-regions.md).
FES base is `f039f822`; the private compiler starts from `7a375fc8`.
This host-only diagnostic retains the accumulated best-stack fixes and the
116.33 MHz timeout candidate. It changes no production lock or RTL.

## Why the generic pass skips this net

The prior final critical path ends at DDR1 `burst_end.ENA`. Its final ALUT4
at (24,16) drives 106 FF enable pins in 21 LAB groups. The critical FF is
at (37,23); the last two routes travel from (37,21) to (24,16), then back to
(37,23), contributing 1.748 and 2.299 ns.

The existing replication pass rejects the whole net before ranking it if any
sink lies in a protected LAB. Here 11 users share LAB (24,12) with carry cells
and 17 share LAB (24,13). The critical singleton group at (37,23), source
LUT and all four input drivers are outside protected carry LABs. All 106
users are FF ENA pins with the same clock and polarity. Simply rerunning the
unchanged generic pass would still reject the source. It would also reset
its per-invocation input-load ledger and copy count, so repeated invocation
is not used as a shortcut.

## Bounded experiment

A separate opt-in diagnostic selects this one source and critical endpoint
from the previous measured routed path. It can add at most one copy, searched
within radius three of the critical FF's actual BEL. This selection is based
on the recorded routed failure, not a claim that the generic predictor ranked
it as critical. The existing generic pass remains unchanged.

The source continues driving every protected sink group. Only complete,
eligible LAB groups can transfer to the copy, and the measured critical sink
must transfer. Each moved output requires at least 250 ps predicted delay
improvement; every clone input must be nonregressing. The diagnostic retains
source/input boundary checks, the same-clock FF-only enable restriction,
all original placements and pin states, full occupied-BEL legality and fresh
predicted-hold checks. It preserves the accepted HPS replica and prevents
sharing its input-load budget. The copy must fit within the configured total
replication budget.

Only the targeted users' ENA connections and the copy's added input loads may
change. This does not relax carry-chain protection in the production pass,
move existing logic, change register initialization, or alter any clock/reset
or D-versus-ENA boundary. A fresh full route determines the actual outcome.

## Preflight and compiler identity

Private nextpnr branch `probe/ramtest-retained-enable` is committed at
`bb5a3ba5dd32cb84e8f77528fcb6cf7aa6e0a713`, based on `7a375fc8`.
The opt-in hook is `NEXTPNR_MISTRAL_RETAINED_ENABLE=<snapshot-prefix>`,
after the timeout rewrite with `NEXTPNR_MISTRAL_PLACED_TIMEOUT_REGION=roots`.
The generic pass is still enabled with budget four. Its existing HPS copy
plus this diagnostic copy consume two slots, with disjoint input-load sets.

The frozen binary is `out/ramtest-retained-enable/tools/nextpnr-retained-enable`,
SHA256 `7a22bd513be77011b74676faeadb181bf1511d4724eb2e720159034901c33cfc`.
`compiler-build.json` records the committed production file hashes. The binary
was built before the final commit; its embedded version is not the identity.

The corrected saved-context preflight selects `MISTRAL_COMB.37.23.0`, moves
only `ddr1_test.burst_end_MISTRAL_FF_Q.ENA`, and leaves 105 sinks on the
original ALUT4, including all 28 protected sinks. It preserves all 14,926
original cells and 103,281 pin states. The four input predictions change
D/C/B/A from 1855/2225/1900/1855 ps to 1100/1070/1655/1100 ps. The output
prediction improves by 1755 ps. These are geometric estimates, not routed
measurements.

Imported JSON does not restore the memory clock constraint or all derived
clock metadata. Its zero predicted hold violations therefore do not establish
fresh-flow timing equivalence. The production hook requires the actual memory
clock constraint and repeats the hold comparison in the fresh run. Carry
clusters and indexed pin states are reconstructed as described in the preceding
region report; preflight is a structural/site check, not a complete replay.

## Validation coverage

The HIP build passes the backend CTest target: 24 registered cases, 23 passed
and the optional actual-design preflight skipped there. Seven focused cases,
including that preflight, pass separately. The policy tests were written first; their initial build failed on the missing
policy header, then passed with the implementation. That initial failure is
a compile-stage check, not an observed runtime policy failure. Absent and empty diagnostic environment
settings reproduce the complete 197-cell default fixture. The saved timeout
control reproduces its prior module, indexed pins, sites and manifest.

The independent copy checker passes a positive fixture and rejects 13
mutations, including protected-group movement, prior-copy/input-load/budget
violations and changed original cells. The budget guard has a separate
recorded failing-to-passing check. On the actual preflight it checks the LUT
mask and input polarity, complete LAB groups, all old cell/pin states, the
prior HPS copy, exact protected-sink retention and independently calculated
geometric input/output delays. JSON omits some internal constraints: runtime
protection evidence is checked against independently identifiable carry,
MLAB, strong-placement and keep/dont-touch exclusions rather than treated as
a full serialization of the internal context.

## Fresh-flow isolation

The fresh run starts from the same initialized-FSM synthesis JSON
(`5cf5205362505abbd3a35791462c66853b37dde02b85f060bb25998d47c2f76c`),
13 unchanged RTL files, generated headers and BUILD_ID as the previous trials.
GPU 1, seed 2, HeAP exponent 2, timing weight 10, beta 0.5 and replication
budget four are unchanged. All other experimental environment switches are
cleared before enabling this timeout/copy combination.

The fresh pre-timeout module and indexed pins match the best 116.44 MHz
baseline with its accepted replica. The post-timeout module, pins and all 41
replacement sites then match the measured 116.33 MHz control exactly. The
new diagnostic's before snapshot is that same module. Its selected copy,
moved user, protected LAB set, per-input/output predictions and final indexed
pin sidecar match preflight. Both the original timeout-equivalence bridge
and the independent copy proof pass on these fresh snapshots.

The real memory period is 7692 ps. Immediately before routing, predicted
setup slack at the selected endpoint changes from -1440 to +1070 ps, with
zero predicted hold violations before and after. These fresh values supersede
the imported-context timing numbers for qualification. They are still
pre-route predictions; final analogue timing decides whole-design QoR.

## Final measured outcome

| Candidate | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Previous best: generic replication | 116.441544 | 77.905884 |
| Expanded-region timeout control | 116.333183 | 79.929665 |
| Timeout control plus retained enable copy | **118.567703** | **76.958595** |

This is a new measured experimental memory best, 1.83% above 116.44 MHz
and about 16.3% above the original 101.94 MHz OSS result. Pixel still passes
74.25 MHz; memory still misses 130 MHz. Capture remains 332.494110 MHz.
The fresh run finishes normally in 575.85 seconds, writes a legal
7,007,204-byte RBF, and reports no final hold violations. Its sole warning is
the memory frequency miss. Every one of the 14,927 post-copy placements
survives routing; the router adds 5,087 ordinary route-through buffers.

At the selected burst-end ENA, maximum propagated arrival falls from
10.074 ns in the timeout control to 7.613 ns, a 2.461 ns improvement.
Across the 106 original sinks, five arrivals improve and 101 regress;
the worst arrival nevertheless falls from 10.074 to 9.119 ns. The improvement
is therefore concentrated rather than uniform. These are matched endpoint
arrivals from any memory launch, not setup slack or fixed-launch path delays.
The added input load and fresh routing can affect other paths even though
all original placements stay fixed.

The new limiting path is `ddr1_test.idle[19]` → `ddr1_address[13].ENA`.
It traverses five timeout replacement LUTs, with 1.274 ns logic, 6.608 ns
routing, 0.731 ns clock-to-Q, 0.017 ns clock skew and -0.196 ns setup,
for 8.434 ns effective setup. The sites are:

```text
FF (42,18) → partition (35,17) → reduce (34,18) → control3 (37,20)
           → control9 (37,26) → control0 (33,21) → address FF (23,16)
```

The final three routing hops cost 0.971, 1.284 and 1.797 ns. This is now a
replacement-logic placement/mapping target, rather than the retained burst-end
driver tested here. The next controlled step is timing-aware placement or
factoring of this DDR1 address-enable chain while preserving the surviving
original logic and both accepted copies. A broad placement restart would
lose the isolation established by this combination.

## Retained stack and limits

Keep this combination as the best measured experimental stack: initialized
FSM support, GPU convergence fix, corrected HeAP options, generic HPS enable
replication, equivalent placed timeout rewrite with expanded regions, and the
new retained-enable copy. The earlier 116.33 MHz timeout candidate was retained
for this combination despite not winning alone. Rejected HPS geometry and
composition configurations remain excluded. `stack.json` records the lineage
and required opt-in switches.

The last two transformations remain fixture-specific diagnostics. One design
and seed do not qualify a general compiler policy or a production lock update.
In particular, this result supports testing selective retention of protected
sink groups; it does not establish that relaxing the generic pass's exclusions
will improve every design. The diagnostic aborts on a failed guard before
routing; it is not a production rollback transaction.

All generated measurements regenerate identically; frozen compiler source,
binary, proof inputs, raw artifacts and external input hashes are verified in
the adjacent JSON. Raw artifacts and helpers live in
`out/ramtest-retained-enable/`. Source, fresh snapshots, measurements and report
receive independent review. No RTL, recipe, compiler lock, ABI/shared contract
or hardware state changes. No PR is opened. This is host-only timing evidence,
not hardware acceptance.
