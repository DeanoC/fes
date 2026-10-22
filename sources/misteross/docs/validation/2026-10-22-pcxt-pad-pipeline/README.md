# PC/XT SDRAM output pipeline diagnostic

This candidate improves internal command/address pad-register paths but is
**not qualified for promotion**. CPU setup still fails, failing setup rows
increase, and microcode has only 1 ps of setup margin. No hardware ran;
external SDRAM timing and the unsupported boundaries remain open.

Base: `612f907d` (completed RTL baseline `be5f2253`). Candidate: `9eca8583`.
The sole changed synthesis input is `pcxt_memory.sv`, enabling the existing
controller's `IO_OUTPUT_REGISTERS` parameter. Commands, addresses, write data
and output enable gain one aligned stage; read capture adjusts by the same
cycle. CPU clocks, SDC/QSF, compiler binary, seed 1, placement weight 2000 and
exponent 5 are unchanged. Packing verifies all 20 command/address outputs are
MISTRAL_SDROUT without dynamic CEOUT or ACLR.

| Final routed metric | Completed baseline | Output-pipeline candidate |
| --- | ---: | ---: |
| Worst command/address pad-register setup | -2.555 ns | +5.021 ns |
| Worst command/address pad-register hold | +2.192 ns | +1.136 ns |
| CPU/system setup WNS | -2.555 ns | -1.602 ns |
| CPU/system reported Fmax | 76.406 MHz | 64.004 MHz |
| Memory setup WNS | +3.175 ns | +2.854 ns |
| Microcode setup WNS | +0.285 ns | +0.001 ns |
| Pixel setup WNS | +1.471 ns | +0.675 ns |
| Failing setup rows | 257 | 369 |
| Failing related hold rows | 0 | 0 |
| Unsupported boundaries | 105 | 105 |

The candidate's worst reported setup path is last ALU result through jump logic
into the next microcode address register (-1.602 ns). Last-result writeback
also fails (worst -0.729 ns), so adding a zero reduction there is not an
established solution. Further work needs CPU placement and logic analysis.

The first pipeline simulation exposed a model assumption: FPGA cold reset
immediately marked SDRAM as deinitialized. SDRAM has no FPGA reset input;
an already-issued command can retire before CKE falls. The model now starts
reinitialization checks at the observed CKE-low boundary and retains bank
recovery/RAM state. A regression permits the queued ACTIVATE but rejects a
new ACTIVATE before reinitialization. The reset sweep also requires that the
CKE boundary actually occurred.

Validation passed: the model regression, all 80 warm/40 cold-reset phases,
memory-backed CGA/MDA boots, CGA Direct and MDA Scanlines system boots at each
zero/one/two-cycle model DQM delay, parent consistency, and `make test-changed`
(805 parent tests, 1269 component tests with 30 skips, complete PC/XT simulation
sweep). Yosys-dependent Python tests were skipped in this repeat because Yosys
is not on PATH; diagnostic synthesis explicitly used the same locked Yosys as
the baseline. No CPU or shared controller RTL changed.

The candidate route completed with exit 1 after 224.164795 wall seconds, within
its 600-second active cap, using automatically leased HIP with Router1 cleanup.
No GPU lease remains. Conservative incident charge includes lease waits:
**607.674011 seconds remain**, down from 831.838806 seconds. Compiler source:
`45476abd07a8881ebeee1e5d061be6b5fe123d71`. Binary/input hashes, argv and environment
are in `runs.jsonl`. The baseline is the completed candidate from
`../2026-10-22-pcxt-commit-timing/`, not its timed-out baseline.

Full source is recoverable from the committed candidate. The frozen netlist,
constraints, generated inputs, packing checks, routed RBF/graph, reports and
both comparison reports are in `capture.tar.gz`. Member hashes are in
`manifest.json`; `comparison.json` retains all 20 pad rows and native clock
metrics. The compiler binary is not embedded. `test-changed.log.gz` retains
the test run. No toolchain lock, seal, shared protocol or physical acceptance
changed. This remains part of draft #979 and open timing issue #840.


## CPU locality follow-up

On base `930f2fbe`, two host-only fresh synthesis-to-placement replays tested
existing `--critical-cohort-budget 16` repair with the same frozen inputs,
compiler, seed and placement weights. The first used the complete final routed
report as guidance. The second selected its 49 same-clock CPU paths ending at
microcode read-address registers. This selection only directs proposals; all
full-design clock, setup endpoint, related-hold and legality guards remain on.
Neither replay routes or writes an RBF.

Both exited successfully and rejected every move: 16 STA trials each, zero
retained. The complete-guidance search attempted 16 geometries; address-focused
search attempted 21. Their output modules, including placement and connectivity,
are identical. This bounded negative result does not prove that all placements
fail. It provides no new final-routed or hardware evidence.

The worst routed address path spends 7.978 ns in wiring and 3.045 ns in logic.
Its first two wires alone total 3.780 ns, travelling from last-result at (10,37)
to reduction logic at (12,27), then back to (8,37). The existing hard-IP capture
locality pass is inapplicable to this combinational CPU path. Adding a cached
zero flag is also unjustified while last-result writeback already fails by
0.729 ns. The next compiler investigation should address coordinated placement
of the reduction and address-select cone, with writeback and microcode capture
protected, before another route is spent.

`cpu-locality-host.tar.gz` retains exact commands, scripts, logs, input hashes,
results and original report indices to reproduce the selected guidance. No RTL,
compiler implementation, clock, lock, seal or shared contract changed. No GPU
route time was spent: the incident allowance remains 607.674011 seconds.

## Atomic connected-LUT pairs

Compiler follow-up [d133140a](https://github.com/DeanoC/nextpnr/commit/d133140a)
adds an explicit default-off paired-LUT experiment to draft nextpnr #266. It
moves two connected ordinary LUTs and their displaced occupants atomically,
retaining all legality, clock, endpoint and hold guards. Registers, carry chains
and logic functions remain unchanged.

On this retained PC/XT placement, 32 STA trials (2080 geometry checks) retain
zero moves. The first blocking setup rows reveal competing consumers of shared
next-address logic: a CPU microcode-PC row goes from +0.264 ns to -2.370 ns in
one rejected trial. These are placement-model margins. Selective replication
of the combinational address-selection logic is the next candidate to investigate;
this is not evidence that replication will close timing.

The original compiler, new disabled mode, and rejected pair search produce
byte-identical placement and timing JSON. All 49 existing native cohort tests,
three new corrected pair tests, 16 CLI tests and 25 routed-retention tests pass.
The initial new fixture coordinate and a build/test executable race were corrected;
only the final successful checks count as validation. Full commands, hashes,
logs and replay inputs are retained in the compiler commit's
`docs/validation/2026-10-22-pcxt-pair-swaps/capture.tar.gz` (SHA256
`ed82d9bab6a2574d7d8d874f650d25f4cb0515fdfdd978a2a9913fee0e52d792`).

No route or hardware ran. The incident route allowance remains 607.674011 seconds.
FES RTL, recipes, toolchain lock, seals and shared contracts remain unchanged.

## Exact shared-data LUT replication

Compiler [c8d4a1d3](https://github.com/DeanoC/nextpnr/commit/c8d4a1d3)
adds default-off selective replication of ordinary shared data LUTs. With this
same placed checkpoint, 32 native STA trials (135 geometries) retain three
copies. Independent whole-netlist substitution verifies exact LUT functions,
inputs and polarities, all 10,448 original cells and original nets, including
unchanged registers and initial state. Worst predicted clock slack is unchanged.
All 55 native cohort tests and 17 CLI tests pass.

The subsequent route experiment does **not** establish a timing improvement.
The baseline completes with CPU/system setup WNS -1.638 ns, microcode -0.358 ns,
pixel -0.228 ns, 343 failing setup rows and 105 unsupported boundaries. The
candidate times out at 270 seconds during analogue repair, with no final timing
or RBF. Its interim results do not count as a final comparison.

Both attempts were accidentally unleased: the experiment driver renamed the
binary `compiler`, bypassing FES's `nextpnr-mistral` basename recognition. They
are not managed-GPU qualification evidence. Compiler repair
[f6a234dd](https://github.com/DeanoC/nextpnr/commit/f6a234dd) preserves that name,
rejects unrecognised commands before dispatch and requires grant evidence for
completed GPU receipts. All 28 retention/runner tests pass, including three new
host dispatch regressions; the actual FES recognition helpers also pass a host
check. The fixed runner has not yet had a live leased route.

All spent time remains charged: 467.207891 seconds, leaving **140.466120 seconds**
in the incident allowance. No further route was attempted. Full failed captures,
source identity, replay inputs, proof and tests are retained in nextpnr's
`docs/validation/2026-10-22-pcxt-replication/capture.tar.gz`, SHA256
`29689164ccf7593855d90331f8df3cee5d29a0a7e20a542cfd513aa393ffba05`.
No hardware ran. FES RTL, recipes, clocks, toolchain lock, seals and shared
contracts remain unchanged. Draft #979 and timing issue #840 remain open.

## Leased comparisons after budget reset

The user renewed the six-hour incident allowance. The repaired runner completed
properly leased baseline, three-copy and four-copy routes, with grant evidence
and unchanged constraints. The new baseline reproduces every prior baseline
RBF/report/graph byte. The three-copy candidate improves CPU setup WNS from
-1.638 to -1.516 ns and pixel from -0.228 to +0.269 ns, but introduces 18 new
setup failures; total failing rows decrease from 343 to 252. The four-copy
candidate reaches CPU -1.532 ns, microcode -0.080 ns and pixel -0.035 ns, with
59 new failures and 337 total failing rows. Neither is accepted.

All inventories retain 17,199 matching endpoint identities and 105 unsupported
boundaries, with no related-hold failures. Positive-margin regressions also
remain disqualifying. No coverage or timing gate was relaxed. The four-copy
host result passes exact netlist substitution; it provides no hardware proof.

A CPU-reference baseline times out at 600 seconds. The CPU-reference three-copy
candidate completes and exactly matches the live GPU RBF, timing and endpoint
reports. Routed graphs match after bijective bit renumbering and the backend
setting are accounted for. No hardware ran; RTL, locks, recipes, clocks, seals
and shared contracts are unchanged.

Compiler evidence is in
`docs/validation/2026-10-22-pcxt-replication/leased.md`; the accompanying
`leased-capture.tar.gz` SHA256 is
`3c4c87f5542d406f6c7570515b29f009ffce54f246640022f38f99331903f403`.
The renewed ledger charges 2703.542323 seconds, including queue time and CPU
reference attempts, leaving **18896.457677 seconds**. Baseline reuse is not
charged twice. Older incident captures and charges remain preserved.

The remaining CPU path still spends 8.413 ns in routing. These trials support
investigating local route preservation with full-design rollback guards, rather
than simply raising replication count. That repair is not yet implemented.
Draft #979 and timing issue #840 remain open.

## Route-preserving compiler trials

Compiler [5d35f171](https://github.com/DeanoC/nextpnr/commit/5d35f171)
implements default-off bounded post-route data-net repair. Other nets stay bound;
each candidate is checked against full native setup/hold, clock and coverage
contracts, with physical route/cache rollback on rejection. The diagnostic
follow-up [0c590466](https://github.com/DeanoC/nextpnr/commit/0c590466)
reports the rejection reason without changing those acceptance predicates.

A properly leased baseline/candidate comparison completes with eight proposals
and zero accepted changes. All 17,199 endpoint rows retain identical margins.
Independent graph comparison verifies all 12,605 cells and 15,736 nets, with
no changed routes or other content after bijective bit renumbering. The baseline
also reproduces all four prior baseline artifact bytes. CPU setup WNS remains
-1.638 ns, microcode -0.358 ns and pixel -0.228 ns, with 343 failing setup rows,
zero failing related-hold rows and 105 unsupported boundaries. This does not
close timing or coverage.

An eight-trial CPU-reference replay also rejects every proposal and reproduces
the RBF, timing and endpoint report bytes; routed graph content matches after
bit renumbering and backend normalization. A 32-budget CPU attempt times out at
600 seconds after 17 rejected trials, with no final artifacts. The compiler
passes 62 focused native, 18 CLI and 29 retention/runner tests; both native
guard/rollback tests also pass on the diagnostic follow-up.

The diagnostic replay identifies CPU/system setup regression as the reason for
all eight rejections. Its final RBF and reports again match the baseline exactly.
This points the next investigation at analogue-aware candidate generation and
branch load/delay, rather than increasing the trial count.

[Compiler evidence and reproduction](https://github.com/DeanoC/nextpnr/blob/3e518286/docs/validation/2026-10-22-pcxt-route-preservation/README.md)
include every completed and failed capture. The 61-member archive was verified
by SHA256 readback; its SHA256 is
`b027aa5717877e9b8c215c9dc02ef785badb2da8ae2427a8f05f4c0499c0cb62`.
The renewed ledger now charges 5332.881248 seconds and leaves
**16267.118752 seconds** (about 4 h 31 min), including queue waits
and all CPU-reference attempts.

No hardware ran. RTL, clocks, toolchain locks, recipes, seals and shared contracts
are unchanged. Draft #979 and timing issue #840 remain open. Production
integration still requires a passing full routed comparison, complete coverage,
the unchanged multi-seed gate, a fresh producer and exact-artifact kit acceptance.

## Native-ranked route generation and bounded feedback

Compiler [0c02e81a](https://github.com/DeanoC/nextpnr/commit/0c02e81a)
reserves whole-tree candidate slots and ranks proposals using native delay at
every sink, including branch loading and terminal LUT input delay. The opt-in
pass requires a 20 ps predicted worst-sink gain before full-design STA; existing
setup/hold, clock, coverage and rollback guards remain mandatory. The first
same-binary CPU-reference comparison generates 12 alternatives, all worse by
99–2017 ps. No distinct whole-tree alternative is emitted on this cohort, and
nothing is accepted.

Compiler [687ebf55](https://github.com/DeanoC/nextpnr/commit/687ebf55)
adds one bounded feedback generation using native pip delays measured in rejected
routes. The original fallback timing table is restored before evaluation and
signoff; learned costs guide search only. A fresh same-binary comparison learns
92 additional pip observations and generates one additional distinct route,
which still loses 1202 ps. All 13 distinct proposals fail the native prefilter;
none reaches full-design STA and none is retained.

Both baselines reproduce prior RBF/timing/endpoint bytes and normalized routed
graphs. The final feedback candidate reproduces its baseline's RBF/timing/endpoint
bytes. Independent comparison verifies all 12,605 cells and 15,736 nets unchanged,
with identical margins at all 17,199 endpoint rows. CPU setup WNS remains
-1.638 ns, microcode -0.358 ns and pixel -0.228 ns, with 343 failing setup rows
and 105 unsupported boundaries. This is a completed negative timing result.

Twelve focused ranking/route/rollback tests and 29 retention/runner tests pass;
the three focused ranking/guard/rollback tests pass again on the feedback build.
[Compiler evidence](https://github.com/DeanoC/nextpnr/blob/b79bd884/docs/validation/2026-10-22-pcxt-native-ranking/README.md)
contains both verified captures, commands, inputs, source identities, tests and
comparison scripts. The final feedback archive SHA256 is
`ad5ca04daed70ed9a7efd44022475924a978cacc71a66dd9d6075ecead4e3bc3`.
These comparisons charge 1221.163890 seconds, leaving **15045.954862 seconds**
(about 4 h 11 min) in the renewed allowance. All earlier charges remain preserved.

Evidence is CPU-reference host diagnostic only. No hardware ran; RTL, clocks,
locks, recipes, seals and shared contracts remain unchanged. These bounded trials
do not justify a larger route-only search on this placement. The next useful
experiment is moving and rerouting the critical CPU result-to-microcode cone
together while preserving unrelated routes and the same final timing guards.
Draft #979 and timing issue #840 remain open; production integration still needs
complete routed coverage, the unchanged seed gate, a fresh producer and exact
artifact hardware qualification.

## Bounded placement and incident-net rerouting

Compiler [43960506](https://github.com/DeanoC/nextpnr/commit/43960506)
adds default-off `--critical-move-route-budget` (0..16). It moves one ordinary
LUT to an empty legal site and reroutes only its incident data nets, keeping
unrelated routes bound. Routing has a shared 500,000-node limit. Full native
setup/hold, clock and endpoint guards remain mandatory, with transactional
placement, route and physical mux rollback.

The same-binary eight-trial CPU-reference comparison accepts zero moves.
Four trials route but fail the clock setup/hold guard; four cannot complete
bounded routing. Candidate RBF, routed JSON, timing and endpoint reports are
byte-identical to baseline. Independent checking verifies all 12,605 cells,
15,736 nets and 17,199 endpoint rows unchanged. CPU setup WNS stays -1.638 ns,
microcode -0.358 ns and pixel -0.228 ns; 343 setup failures and 105 unsupported
boundaries remain. Default-off RBF/timing/endpoint bytes reproduce prior evidence.

Two focused native tests, the CLI boundary test and all 30 retention/runner
tests pass. Tests cover inverted-input routing, successful connectivity,
rejection/exception/exhaustion rollback, protected cells, occupied targets and
candidate-only dispatch. The independent graph checker also rejects a deliberate
cell-function mutation.

[Compiler capture and reproduction](https://github.com/DeanoC/nextpnr/blob/f956382e/docs/validation/2026-10-22-pcxt-move-route/README.md)
retain inputs, commands, receipts, test logs and comparisons. The 35-member
archive passes SHA256 readback; archive SHA256 is
`a256bc1a29201c83d97d40134c8989e3da15a3e7e85554bea651dd3de7e78906`.
This comparison charges 582.528400 seconds, leaving **14463.426462 seconds**
(about 4 h 1 min) of the renewed allowance.

This is a bounded single-LUT experiment, not simultaneous placement of the
entire critical cone. Only four of 32 examined cells yield positive geometric
gain proposals. Next, identify the exact regressing clock and margin per trial
before choosing a coordinated placement footprint; this capture does not justify
simply increasing trial counts. No hardware ran. RTL, clocks, locks, recipes,
seals and shared contracts are unchanged. Draft #979 and timing issue #840 stay
open; complete routed coverage, the seed gate, a fresh producer and exact-artifact
hardware acceptance remain required.

## Exact clock regressions for rejected moves

Compiler [180bacd2](https://github.com/DeanoC/nextpnr/commit/180bacd2)
adds per-trial native setup/hold margin diagnostics without changing candidate
generation, routing, acceptance or rollback. The same-input comparison repeats
eight trials. Four fail local routing; every routable move regresses only
CPU/system setup: trials 1, 3, 5 and 7 lose 0.907, 0.807, 0.908 and 0.819 ns
respectively. Other clock setup margins and all hold margins remain unchanged.
No clock coverage changes or margin improvements occur. This identifies a CPU
data-path setup regression rather than a hold/other-clock tradeoff.

The new baseline matches all four previous artifact files byte for byte,
including routed JSON. The candidate reproduces its baseline exactly after
rollback. Independent checks verify 12,605 cells, 15,736 nets and all 17,199
endpoint rows; the margin parser validates all 32 native diagnostic rows.
The two focused native guard and physical rollback tests pass. Final timing
and unsupported coverage remain unchanged and incomplete.

[Immutable compiler evidence](https://github.com/DeanoC/nextpnr/tree/100521cb/docs/validation/2026-10-22-pcxt-move-margins)
contains commands, inputs, source identity, build/test logs, receipts and
comparisons. The 33-member capture passes SHA256 readback; archive SHA256 is
`8a2738d0e086be6cfdf99d02a1b91120444b313c8643ff932d063e2a58902a3a`.
This comparison charges 457.853168 seconds, leaving **14005.573293 seconds**
(about 3 h 53 min) of the renewed allowance.

Connectivity inspection finds shared incident inputs with up to 31 sinks
(14 on the address LUT). Whole-net rerouting can alter stationary consumers;
these clock totals do not prove which branch causes the regression. The next
bounded repair should preserve stationary sink branches and reconnect moved
sink branches before expanding to coordinated LUT moves. Keep full native
acceptance and exact rollback checks; do not increase whole-net trial counts.

Evidence is CPU-reference host diagnostic only. No hardware ran and no RTL,
clocks, locks, recipes, seals or shared contracts changed. Compiler #266 and FES
#979 remain draft; timing #840 stays open with the existing coverage, multi-seed,
producer and exact-artifact hardware gates.

## Continued compiler closure experiments

The user subsequently removed the total route cap. All earlier charges remain
recorded; shared GPU phase leases and native search bounds still apply. The
former remaining-time figures above describe earlier checkpoints, not a current
limit. CPU/system and microcode remain 100 MHz and pixel remains 74.25 MHz.

Compiler `66c97e49` preserves stationary sink branches during local LUT moves,
checks physical ALM halves and refreshes changed ALM modes. All eight trials now
route, versus four previously, but none passes the unchanged native retention
guards: six regress clock setup and two reduce positive hold margins. The
retained final artifact bytes equal the immutable baseline. Three native guard,
half/mode and rollback tests pass; subsequent independent physical encoding
regressions cover emitted RBFs. This fixes a search limitation without claiming
a timing winner.

Compiler `692a1c37` adds exact two-LUT factor reassociation. The first PC/XT trial
improves system setup WNS from -1.638 to -1.301 ns and setup failures from 343 to
295, while preserving complete endpoint identities and proving the selected
consumer truth table over all 1024 assignments. Two further cumulative factors
perform worse. Native pure/backend/CLI tests, complete graph proofs and mutation
checks pass; positive hold-margin reductions and 105 unsupported boundaries
still prevent acceptance.

Separate native-qualified placement diagnostics move two or fourteen microcode
RAMs. Fourteen RAM moves reduce setup failures to 217, but still miss system,
microcode-return and pixel setup. Combining them with the first factor has 247
failures. Full graph checks preserve all 12,605 cells, 15,736 named nets and
17,199 endpoint identities; geometry alone is not used as timing proof. These
experiments start from the same frozen placed checkpoint. A fresh raw-synthesis
baseline is a different execution model and is reported separately.

[Branch, factor and RAM evidence](https://github.com/DeanoC/nextpnr/tree/cb76e894/docs/validation)
retains commands, source/binary identities, inputs, failures, RBFs, timing and
endpoint reports, proofs and SHA256-verified archives. Cumulative renewed route
accounting through those runs and the fresh baseline is 9940.813688 seconds.

Compiler `cbb1e1f1` expands the existing explicitly selected, private four-LUT
reduction rewrite from sixteen literals to thirteen through eighteen, keeping
the sixteen-literal result unchanged. Eight focused native tests and the CLI
regression pass. Independent complete unpacked-graph proofs exhaust the actual
17-bit overflow and 16-bit last-result predicates, preserving all outside cells,
connections, aliases, polarity and metadata. Old/new sixteen-literal exports are
identical with the feature disabled and enabled.

| Fresh raw-synthesis seed 1 | Baseline | Overflow only | Both reductions |
| --- | ---: | ---: | ---: |
| System setup WNS, ns | -1.602 | -1.232 | -1.349 |
| CPU subtree setup WNS, ns | -1.602 | -1.232 | -1.126 |
| CPU-to-ROM address WNS, ns | -1.406 | -0.598 | -0.760 |
| Microcode-return WNS, ns | +0.001 | +0.397 | -0.046 |
| Pixel setup WNS, ns | +0.675 | +0.370 | -0.664 |
| Setup failures | 369 | 150 | 318 |
| Related hold failures | 0 | 0 | 0 |
| Unsupported boundaries | 105 | 105 | 105 |

The combined rewrite's worst system path is CGA, not CPU. The overflow-only
rewrite gives the best overall seed-1 result. The disabled fresh baseline
reproduces the earlier fresh producer candidate's RBF, timing, endpoint and
routed JSON bytes exactly. Complete timed endpoint identities and constraints
match, but static/constant/unsupported physical records and route-through
inventories change with fresh placement. The unpacked Boolean proof is not a
complete final routed or physical encoding proof.

Compiler `36b9e372` adds debug-only placement tracing. Four parent/new runs with
tracing on/off produce byte-identical placed JSON. The selected HeAP iteration
solves the sixteen ROMs near column 14, then spreads all sixteen to column 41;
the strict legalizer largely follows those displaced targets. Related-clock
phase and M10K setup timing are present. Legalizer priority alone would not
address the observed spreading stage.

[Reduction and placement-stage evidence](https://github.com/DeanoC/nextpnr/tree/eab8b15f/docs/validation)
retains the three complete routes, exhaustive proofs, compatibility exports and
debug trace. The route charge is 634.416549 seconds, taking cumulative renewed
accounting to 10575.230237 seconds through these three runs. Placement-only
tracing has no route charge.

Source review identifies two concrete cut-spreader indexing defects: small
partitions use an absolute vector index instead of their local rank, and a
rightward pivot transfer charges the previous cell's bucket. Compiler
`be4c5d34` corrects both behind default-off `--placer-heap-correct-spread`.
All ten focused tests pass; restoring either defect makes regressions fail.
The existing control-set/congestion and new spreader CTest targets pass.
Native placement with the option absent preserves the entire previous module,
with only the top-level build-version label differing. Further full-route and
seed comparisons are required; the correction is not a timing-closure claim.

No FPGA kit was contacted. These are host diagnostics; no production RTL,
recipe, clock, toolchain lock, seal or shared contract changes. Draft compiler
#266 and FES #979 stay open with timing #840. The next integration step remains
a passing same-recipe multi-seed result, complete timing/physical contracts,
a fresh producer and exact-artifact hardware acceptance.

## Additional seed, spreader and paired-path results

The six additional fresh routes complete the baseline, overflow-only and
both-reduction recipes at seeds 1, 2 and 3. All nine fail setup; no recipe passes
the unchanged two-of-three gate. Overflow-only seed 3 has the fewest failures
(89), but still misses system setup by 1.201 ns and pixel setup by 0.564 ns.
Every route retains 17,199 endpoint rows, zero reported related-hold failures
and 105 unsupported boundaries. Native launch/capture identities and windows
match; changed physical coverage records remain explicitly recorded.

The corrected spreader also completes its frozen seed-1 route comparison:

| Native routed setup | Default | Corrected spreading | Corrected + overflow |
| --- | ---: | ---: | ---: |
| System WNS, ns | -1.602 | -1.402 | -1.487 |
| CPU-to-ROM address WNS, ns | -1.406 | -0.583 | -0.704 |
| Microcode-return WNS, ns | +0.001 | +0.652 | +0.605 |
| Pixel WNS, ns | +0.675 | -0.544 | -1.333 |
| Setup failures | 369 | 115 | 125 |

The disabled route reproduces the prior fresh RBF, timing and endpoint bytes;
its routed JSON differs only by the creator label. Explicit-zero preparation
preserves all placements and complete graph correspondence after an independently
checked numeric-bit bijection. These checks do not establish final routed
logical or physical equivalence for the enabled fresh placements.

A separate native-qualified local move carries two microcode RAMs and sixteen
private decoders together. Selected address failures fall from 24 to 6, selected
return failures from one to zero, and total setup failures from 343 to 299.
Unmoved address paths still fail by 1.542 ns. Complete graph and native physical
decoder-mask checks pass before and after routing, including rollback and
corruption tests. Those checks do not decode or qualify the emitted RBF.

An exact AX15 factor promotion is compared against its own unchanged serialized
reload control. The original AX15-to-address path improves by 629 ps, but the
worst path moves to ALU operand selection: overall system WNS improves only
from -1.143 to -1.114 ns. Pixel setup still fails. Complete final logical graph
proof accounts independently for every route-through helper and all original
functions, connections, aliases and logical pin states; seven deliberate
corruptions reject. Physical encoding and device behavior remain separate.

[Complete seed, spreader, RAM/decoder and AX15 captures](https://github.com/DeanoC/nextpnr/tree/5383f232/docs/validation)
retain all commands, source and binary identities, paired artifacts, proofs,
failures, receipts and full archive readback. The six seed routes charge
1838.679211 seconds, RAM/decoder route 257.685019 seconds, spreader comparison
750.604719 seconds and paired AX15 routes 475.896628 seconds. Concurrent operand
driver routes charge another 519.622055 seconds. Including those completed
parallel routes, cumulative accounting is **14417.717868 seconds**; unfinished
density routes are excluded until their receipts complete. There is no total
route cap. Native per-run bounds and shared GPU leases remain in force.

All results remain host diagnostics. No kit, RTL, clock constraint, core lock,
production recipe, seal or shared contract changed in this continuation. Timing
closure, complete physical coverage, the same-recipe seed gate and fresh
producer/exact-artifact hardware qualification remain required.

## Native-window RAM placement and rejected ADD mapping

The default-off compiler policy at nextpnr `a361eac0` assigns the complete
eligible RAM bucket using native launch/capture windows and a bounded global
matching. It preserves ordinary placement when disabled or when the complete
bucket fails eligibility. All four seed-1 runs below use identical raw input,
100 MHz system target, weight 2000 and exponent 5. The overflow variant uses
the independently proved private four-LUT reduction.

| Recipe | Setup failures | Worst setup | Microcode address failures / worst |
| --- | ---: | ---: | ---: |
| Default | 369 | -1.602 ns | 148 / -1.406 ns |
| Native windows | 247 | -1.307 ns | 100 / -0.674 ns |
| Windows and corrected spreading | 110 | -0.967 ns | 3 / -0.199 ns |
| Windows, corrected spreading and overflow balancing | 14 | -0.434 ns | 0 / +0.124 ns |

The last route's 14 failures comprise seven CPU, three CGA, two MDA and two
PIC endpoints. CPU destination WNS is -0.173 ns; microcode return is +1.257 ns
and pixel setup is +2.005 ns. Native reported system Fmax is 95.8405 MHz.
The same recipe fails with 358 endpoints at seed 2 and 56 at seed 3; zero of
three seeds qualifies. Every route retains all 17,199 native endpoint/clock
tuples, unchanged windows/contracts and 105 unsupported boundaries. Complete
per-artifact graph and helper checks pass, including mutation rejections.
Neither matching identities nor positive related-hold margins supplies the
missing physical coverage.

The ADD operand-0 mapping in FES `53b8fc1a` was tested and withdrawn by
`d2e19b13`. It replaces the actual three-level cone with two LUT levels and
passes the actual emitted truth-table, selector-state and CPU regression
checks. Fresh synthesis changes placement enough to regress both matched
route comparisons:

| Pair | Baseline setup failures / system WNS | ADD candidate failures / system WNS |
| --- | ---: | ---: |
| Windows and corrected spreading | 111 / -0.967 ns | 366 / -2.227 ns |
| Same, with each build's independently proved overflow root | 14 / -0.434 ns | 211 / -1.637 ns |

The source pair is `2a1cedf4` versus `53b8fc1a`, with identical tool, firmware,
ROM, QSF, SDC and PLL configuration. Complete endpoint sets and changed
identities are retained; no intersection stands in for qualification. All
original functions, aliases, pin polarities and private route helpers are
checked separately against each run's exact native preparation. Whole-board
sequential equivalence, physical encoding and hardware acceptance are not
claimed. The committed experiment passed `make check` and `make test-changed`
(842 parent tests, 1,275 component tests with one skip, the initial 18/26-test
lanes and all PC/XT simulation suites). These functional results do not
override its timing regression. After withdrawal, every misteross source byte
matches `2a1cedf4`; parent consistency passes again.

Adding only `--arrival-pin-assignment` to the best frozen recipe also fails:
14 setup failures remain, system WNS becomes -0.497 ns and CPU WNS -0.358 ns;
two microcode address rows now fail by up to 0.108 ns. All complete native
identities/windows/contracts and 105 unsupported rows are unchanged. This
does not replace the original recipe. The separate density and reciprocal
RAM-swap captures also retain their failed whole-design outcomes, including
the beta-0.75 route that exits 125 before producing final artifacts.

A separate default-off hard-block cost adds native criticality and fanout to
the phase-window weight. Its own formula, rollback and complete preparation
tests pass, but its full route regresses to 295 setup failures: system WNS
-1.655 ns, CPU WNS -1.621 ns, 33 address failures and two return failures.
All five previously failing display endpoints become worse. The complete
17,199-row identity/window/flag inventory and contracts remain preserved.
This option is excluded from the best working diagnostic recipe.

An immutable source experiment moves the microcode read onto the system edge,
retaining the registered output cadence, PLL parameters and ROM contents.
The source transport, CPU regressions, 192 actual mapped address functions,
90 registered outputs and removed 48 local address copies pass their stated
proofs. This is conditional state correspondence, not whole-board sequential
or physical equivalence. The actual new overflow cone has five LUTs and is
correctly rejected by the existing bounded four-LUT transform; no guard is
relaxed. These routes therefore use the unbalanced 111-failure reference.
With the original window cost, the new source has 407 setup failures, system
WNS -1.990 ns and 191 failing ROM-address rows. Adding the new criticality cost
increases failures to 515 and system WNS to -2.556 ns; 145 address rows and one
return row fail. Neither route has a related hold failure; both retain 105
unsupported boundaries. Full graph proofs and deliberate corruption checks
pass independently for each route. No tracked RTL adopts the retiming. The [complete same-edge captures](rom-same-edge/README.md)
preserve both source snapshots, proofs and failed full routes.

[Compiler captures through `36617f23`](https://github.com/DeanoC/nextpnr/tree/36617f23/docs/validation)
include the complete native-window four-way comparison, fixed-recipe seeds,
density, RAM swaps, bare ADD, balanced ADD, arrival-pin, failed cost and parameter-matrix results. Archives
retain original failures and exact input/source/tool/command identities with
full readback. `continued-runs.jsonl` preserves 27 subsequent completed run
receipts and per-clock results. Their charges bring the campaign total to
**31584.30252136522837 seconds**. This includes all five placement
weight/exponent sweep cases and both completed local repair tests.
Only completed receipts are charged. The earlier sandbox-denied sweep launch
never started nextpnr: its five wrapper failures add zero GPU/full-route charge;
0.5072072022594512 seconds of wrapper elapsed time is retained separately. Synthesis, compiler builds and CPU-only preparation add
no full-route charges. There is no total route cap; native iteration bounds
and shared GPU leases remain.

The completed sweep keeps raw synthesis, compiler, seed 1, native windows,
corrected spreading and overflow balancing frozen. Weight/exponent cases
300/2, 300/5, 1000/2, 1000/5 and 2000/2 produce 537, 148, 48, 91 and 309 setup
failures respectively; every case is worse than the existing 2000/5 reference.
Their complete 17,199-row identity/window/flag inventories match. Changed
native physical details are retained, not presumed equivalent. Related hold
has no failing row and coverage remains incomplete in every case.

Adding `--critical-route-budget 32` to the frozen best recipe accepts three
native-guarded route changes. Failures fall from 14 to 13: one PIC row improves
from -289 to +186 ps; the two failing MDA rows improve to -327/-328 ps.
Exactly five setup rows improve, every other setup row and every hold row is
unchanged, and all non-endpoint snapshot fields match. System WNS remains
-434 ps and CPU WNS -173 ps. The emitted RBF, native endpoint report and timing
report change, but the old compiler's saved routed JSON is byte-identical to
the reference: its cached ROUTING attributes predate the repairs. This is a
confirmed final-native timing gain with an explicit checkpoint provenance gap,
not proof of the final serialized routing. The compiler serialization fix and
fresh fixed-tool replay are required before using that JSON as final evidence.

The separate `--critical-move-route-budget 16` test keeps no move and retains
14 failures; it considers 32 cells but finds no eligible target for a trial.
The old compiler also copies cached physical attributes into move snapshots;
that provenance is being repaired before further retained-move claims.
The strict timing/coverage guards remain unchanged. Separate out-only work
evaluates exact PIC event decode, while a default-off private two-LUT display
decomposition is being prepared with placement, native legality and rollback
guards. CPU/system remains 100 MHz, pixel remains 74.25 MHz, no shared/module
contract or production lock changes, and no hardware ran. Both PRs remain
draft and #840 remains open.


## PIC source-equivalence and board mapping rejection

Two isolated PIC event-decode rewrites pass the complete arbitrary-state
module transition/reset/output proof, including invalid state encodings.
Actual source mutations for invalid-state handling, poll edge and reset are
rejected. The smaller acknowledge-plus-poll rewrite also passes the standalone
all-eight-service-bit depth screen. Neither retains that benefit after full
board synthesis against immutable source `2a1cedf4`.

For service bits 0 through 7, actual baseline reset-to-DATAIN LUT depths are
`[7,8,7,7,7,7,7,7]`. Changing all three event expressions gives
`[7,7,7,8,8,7,8,8]`; changing acknowledge and poll only gives
`[8,8,8,8,8,8,8,8]`. The all-three rewrite deepens the failing bit 3 path and
does not improve failing bit 0; the smaller rewrite deepens seven paths.
All 318 RAM parameter/INIT records and both PLL parameter/clock-role records
remain exact, with unchanged FF and arithmetic-LUT counts. Complete mapped
cell-name unions and all 93 source-provenance PIC FF input cones are retained.
This is structural mapping evidence, not a candidate routed measurement or
whole mapped-board equivalence proof.

Both candidates are rejected before routing. [The complete PIC captures](pic-direct-events/README.md)
retain source snapshots, proofs, negative witnesses, synthesis commands and
outputs, full comparisons and independently verified archive manifests.
The host syntheses take 361.68976250104606 and 400.9227558299899 seconds;
GPU/full-route charge is zero. No tracked PIC RTL or production contract changes.

## Final post-route checkpoint consistency

Compiler `bd87c530` refreshes physical architecture attributes after explicitly
enabled post-route repairs. Move guards also use a separate exception-safe live
metadata snapshot; the complete timing/contract acceptance predicate is
unchanged. The disabled and explicit-zero paths preserve the earlier complete
preparation bytes apart from the recorded creator label. Sixteen native tests,
the strengthened metadata/exception test and both repair/settings CLI suites
pass. This source repair changes neither ordinary routing nor timing models.

A fresh leased full route with the frozen fixed binary reproduces every byte
of the earlier thirteen-failure RBF, endpoint inventory and timing report.
System WNS remains -434 ps, CPU WNS -173 ps, microcode address +124 ps,
return +1257 ps and pixel +2005 ps. Exactly five setup rows improve against
the original fourteen-failure route; every other setup and every hold row is
unchanged, with all complete native metadata preserved. Coverage remains
incomplete at 105 unsupported boundaries.

The final saved graph now includes the three accepted route-tree changes.
Complete canonical wire/pip/strength maps distinguish them from eleven
serialization-order-only changes. All other cell, net, pin and LAB metadata
is exact apart from the creator label. Independent native interpretation
checks each changed tree's actual source, every pip endpoint and owner,
acyclic connected structure and every physical sink. Missing-source/sink
mutations reject. This closes the old JSON provenance gap; it is not proof
of device electrical behavior or complete physical timing acceptance.

The fresh replay charges 702.9260488403961 seconds, bringing the 28 completed
subsequent receipts and campaign accounting to **32287.22857020562447 seconds**.
Earlier route/move captures are preserved and are not charged a second time.
No kit ran. The 100 MHz system/CPU and 74.25 MHz pixel requirements, original
seed gate, production lock, seal and shared contracts remain unchanged.
The later private two-LUT MDA experiment and its rejected full-route result
are recorded below.

The [committed compiler capture](https://github.com/DeanoC/nextpnr/tree/27ff77c0/docs/validation/2026-10-22-pcxt-post-route-checkpoint)
retains all original and repaired artifacts, complete comparisons and native
tree checks. Independent archive readback verifies every one of 395 members;
fresh extraction and the offline comparison replay pass. The capture SHA-256
is `abc3a736d38b920ee7c6753c89aa614d3b06f6d2ef3009cfb72ef687beea4581`.


## Private MDA decomposition: reject the routed candidate

Compiler `40097b8d` adds an explicitly selected, bounded private ALUT3-to-ALUT5
partition experiment. Its seven distinct essential inputs are composed over all
128 assignments; cells, output nets, BELs and all eight input-use slots remain.
The independent complete graph proof covers 10,448 cells, 13,597 named aliases
and 19 top ports, with five actual mutation negatives. Four pure and 44 native
tests plus seven CLI checks pass, including physical mask readback in both ALM
halves and LAB/MLAB tiles. The initial disconnected memory-enable test fixture
failure and its test-only correction are retained separately.

Default and listing preparations are byte-identical. The one selected MDA
partition improves four emitted predicted setup rows by 851 ps and two hold
rows by 168/363 ps, with no emitted-row regression. These are placement
estimates, not routed evidence. The final native guard remains unchanged.

A matched pair of fresh full routes uses the same frozen compiler, original
synthesis/constraints, seed 1, hard-block windows, corrected spreading,
overflow-17 balancing and post-route budget 32. The enabled case adds only
private-pair candidate 0 and its immutable prepared timing report.

| Full route | Setup failures | System WNS | CPU failures / WNS | Result |
| --- | ---: | ---: | --- | --- |
| Disabled control | 13 | -434 ps | 7 / -173 ps | Reproduces prior RBF and native report bytes |
| Private MDA candidate 0 | 82 | -829 ps | 69 / -829 ps | Reject |

All 17,199 endpoint/domain identities, windows and flags and every native
metadata category match. All thirteen old negative rows remain negative;
sixty-nine new failures appear. MDA-derived Q5/Q7 worsen from -327/-328 ps to
-655/-656 ps: their dominant source shifts to a sibling VRAM branch with a
3.112 ns first route. Microcode address stays +124 ps; return drops from
+1257 to +1165 ps and pixel from +2005 to +1201 ps. No hold failure appears,
but 6,824 previously positive setup rows and 5,809 positive hold rows regress.
The complete comparison retains all 6,835 setup regressions and every margin;
unchanged per-cohort hold minima do not hide those regressions.
The disabled control is the retained recipe; the MDA option is not adopted.

Both final graphs account for all original cells plus 1,949 individually
proved private routing helpers. Every raw physical difference is retained,
including an enabled-run-only serialized `gpurouter/localInputEscape` false
setting inserted by the native router's default read; this is not a changed
user constraint. Complete graph mutation checks reject nine corruptions.
No checkpoint reload substitutes for either route.

The completed charges are 920.9708376382478 and 947.4481055433862 seconds.
The journal now contains 30 subsequent receipts and a total campaign charge of
**34155.64751338725847 seconds**. Coverage remains incomplete at 105 unsupported
boundaries. There is no timing closure, seed qualification, production seal or
hardware acceptance, and no tracked RTL, clock, lock or shared contract changes.
The subsequent bounded compiler experiment swaps symmetric LUT6 inputs after
routing, using actual failing paths while preserving unrelated routes. Its
completed paired result is recorded below.

The [committed compiler capture](https://github.com/DeanoC/nextpnr/tree/4b787dc2/docs/validation/2026-10-22-pcxt-private-pair)
contains all 121 hash-verified members. The archive SHA-256 is
`18c8074b7fedba429cabec99d2a337a64b827506a12a0f7d2b06d1d43ae7b237`.
Independent fresh replays reproduce graph and repair-ledger bytes. The report
comparison exposed only Python-set dictionary-key rendering order; the retained
initial failure and strict supplement enumerate every permutation while checking
all values, types, keys and array order. Six checks, including three explicit hash
seeds, pass on both independent extractions with equal result receipts.


## Routed symmetric LUT6 pair: no eligible trial

Compiler `461ce654` adds an explicit `--critical-lut6-swap-budget` of 0..16,
default off. It discovers symmetric input pairs from actual failing native
paths, proves all 64 truth-table assignments, and reroutes only the two
single-sink trees. Native legality, full setup/hold inventory and all existing
coverage contracts remain required. Thirty-three native, three pure-policy
and six CLI tests pass. Default and explicit-zero preparations match exactly;
the prior preparation differs only in its creator label.

The fresh paired routes use the same compiler, original synthesis/constraints,
seed 1, hard-block windows, corrected spreading, overflow-17 balancing and
post-route budget 32. The selected case adds only LUT6 swap budget 16. Both
produce byte-identical RBF, final routed JSON, timing and endpoint files:
13 setup failures, system worst slack -434 ps, CPU seven failures/-173 ps,
zero hold failures and minimum checked hold slack +822 ps. All 17,199 native
rows, metadata, 12,397 cells and 15,546 aliases match, including all 17,194
checked setup and hold margins.

The selected pass finds all thirteen negative native path witnesses and
28 LUT6 arcs. Of fifty filtered pairs, 41 are asymmetric, seven fail original
LAB legality, one fails the solitary-LUT6 condition and one is multisink.
There are zero eligible pairs and zero trials. This is an eligibility blocker;
it does not measure the timing benefit of the intended symmetric swaps.

The completed charges are 854.6584242181852 and 859.5982504962012 seconds.
The journal contains 32 subsequent receipts and a total campaign charge of
**35869.90418810164487 seconds**. The retained recipe remains the route-only
13-failure diagnostic. Coverage is still incomplete at 105 unsupported
boundaries. No RTL, clocks, production lock or shared contract changes; no
timing closure, seed qualification, production seal or hardware acceptance.

The [committed compiler capture](https://github.com/DeanoC/nextpnr/tree/8987a69b/docs/validation/2026-10-22-pcxt-routed-lut6-swap)
retains the complete pair and frozen source/tests. Independent readback verifies
all 103 members and reruns five offline checks from a fresh extraction,
reproducing all proof JSON bytes. Archive SHA-256:
`1b9642742873e2c6d4c14aba4d234fdddea2bd9e0b3f1b1ee030cbfd5230d4f6`.

## Route-through cache correction: host validation

The LUT6 eligibility rejection exposed stale native FF input metadata. Inserting
1,949 route-through BUFs rewires the FF DATAIN ports but leaves their old cached
net pointers. Native route preparation then reports 309 illegal LABs, 411 illegal
ALMs and 188 overfull cached LAB counts. The intended CPU OR6 LAB rises from
42 to 47 inputs; its five stale FF caches account for the complete discrepancy.

Compiler `972d8ea6` refreshes each changed FF cache before helper binding recounts
the ALM. The full-board native probe then has zero stale entries or illegal
LABs/ALMs, and its complete observed snapshot equals the old diagnostic cache
refresh. All three previously blocked CPU LABs remain at 42 inputs. Legality
rules, physical pins and control semantics are unchanged.

Eight new regressions fail with the old code and pass with the correction.
The same frozen compiler passes the 33-case LUT6/coverage suite, 77-case
private-pair suite and both six-case CLI suites. Suite counts include overlapping
coverage tests. Default/zero placements are byte-identical and match the prior
complete graph and predicted timing apart from the creator label. The separate
post-route private-pair implementation is `7b8556ad`, compiled but disabled in
these controls. No full route has run with this build.

A further native counterexample exposes an existing LUT6 admission gap: an
essential missing-clock or undriven upstream input can disappear from propagated
timing tags, leaving the same complete-looking downstream row as a fully known
control. Enabled LUT6 routes are held until an explicit bounded upstream-source
check rejects these omitted origins. This is separate from the confirmed cache
correction; no coverage or timing guard is waived.

The [compiler host capture](https://github.com/DeanoC/nextpnr/tree/6d02e632/docs/validation/2026-10-22-pcxt-routethrough-cache)
retains 85 members with SHA-256
`121dd4407e48945bd49ed19a9bfdfee791d05d6483ae8548f5ec5b119d2e6caa`.
Fresh readback verifies every retained hash, source identity, test-log count,
complete native snapshot and placement control. Executables and build objects
are recorded by identity and omitted from the archive. This is host-only
structural/model evidence with zero added route charge. The retained timing
result remains 13 setup failures/-434 ps; CPU remains seven failures/-173 ps.
There is no timing closure, seed qualification, production seal or hardware
acceptance, and no RTL, clock, lock or shared contract changes.


## Source-complete routed CPU swap: 13 to 10 setup failures

Compiler `243c829c` closes the omitted-origin admission gap with a bounded
structural walk through characterized LUT, buffer and arithmetic arcs to actual
FF/M10K launches. It checks complete clock/edge identities against native
relationships, before and after each trial in both timing frames. Six new
source-coverage tests, 33 native regressions and six CLI tests pass. All three
actual CPU candidates pass source/downstream coverage and native legality;
default and explicit-zero complete placement/report controls match. Independent
replay confirms every source origin and the complete native probe bytes.

The fresh paired routes use one frozen compiler, the original synthesis and
constraints, seed 1, the existing placement recipe and route-repair budget 32.
The selected case adds only LUT6 swap budget 16. The cache correction makes
seven input pairs on three CPU LUT6s eligible. Three trials run: ALU E/D cannot
route its first input within the bounded transaction; the lane-10 operand OR6
B/A swap is retained; the other ALU D/E swap fails the required 20 ps gain.
Remaining proposals on the already changed OR6 are skipped by the one-swap-per-
cell rule.

| Final routed metric | Matched default | Retained CPU swap |
| --- | ---: | ---: |
| Setup failures | 13 | 10 |
| CPU setup failures | 7 | 4 |
| CPU worst setup | -173 ps | -72 ps |
| Whole-system worst setup | -434 ps | -434 ps |
| Hold failures | 0 | 0 |
| Minimum checked hold | +822 ps | +822 ps |
| Unsupported boundaries | 105 | 105 |

The complete 17,199-row inventory, flags and metadata are preserved. Sixteen
checked setup margins improve and 17,178 remain equal; all 17,194 checked hold
margins remain equal. Both complete clock comparisons pass. The default RBF,
timing and endpoint files match the previous 13-failure route byte for byte.

The full graph proof covers all 12,397 cells and 15,546 aliases. Only the logged
OR6 input transposition and its two canonical route trees change; all 64 Boolean
rows match. Independent native interpretation validates each saved pip's source
and destination, ownership, driver root and every physical sink. A second root
replay reproduces both native topology outputs byte for byte. These checks do
not reload timing as a substitute for the original fresh-route reports.

The [immutable compiler capture](https://github.com/DeanoC/nextpnr/tree/7635c002/docs/validation/2026-10-22-pcxt-lut6-source-coverage)
contains 494 verified members with archive SHA-256
`2b2240ddd347411dc22a695cb5db0e53544b873e75183acf299ebc230101c8f1`.
Independent extraction reruns all six offline checks and reproduces every proof
JSON byte for byte. Native execution and offline evidence replay remain distinct.

Completed route charges are 715.8551598787308 and 769.3148755207658 seconds.
The journal now contains 34 subsequent receipts and a cumulative charge of
**37355.07422350114147 seconds**. This becomes the retained local diagnostic
improvement. Four CPU, three CGA, two MDA and one PIC-capture setup failures
remain. Coverage is still incomplete, and the same-recipe seed and physical
acceptance gates remain open. There is no hardware evidence, production seal,
clock reduction, timing waiver, RTL change, lock change or shared-contract change.


## Post-route private pair: unused arithmetic-output boundary

A separate fresh route with the same frozen `243c829c` compiler adds only
private-pair budget 4 to the matched default recipe; LUT6 swaps remain disabled
for this isolated comparison. The six private-pair CLI checks pass on this exact
binary. The 77 native tests retain their original `972d8ea6` provenance, with
all private-pair implementation and test files proven unchanged.

Five producer witnesses are found. The intended MDA pair rejects because its
source fanout reaches an untimed or unused output boundary; the other four
witnesses fail the existing consumer, direct-source or single-consumer guards.
There are zero eligible groups and zero trials. The RBF, complete routed graph,
timing and endpoint files all match the fresh default byte for byte, retaining
13 setup failures and no hold failures. This isolated result does not replace
the improved 10-failure CPU recipe.

The route charge is 492.9281931621954 seconds. The already charged matched
default is not charged again. The journal contains 35 subsequent receipts and
cumulative charge **37848.00241666333687 seconds**. A separate complete graph trace and
native structural probe identify two internal arithmetic SO/CO outputs with
zero users and route wires, valid driver backreferences and no public, global
or clock observer. The live branches still reach exactly 64 saved FF captures.
The corresponding CGA input fanout has six such unused arithmetic outputs and
66 FF captures. This corrects an earlier structural audit that silently stopped
at empty branches. No coverage rule has changed and no private-pair or CGA
permutation timing gain is established.


## Preserved frontier evidence and ordinary-LUT host validation

The [private-pair/frontier capture](https://github.com/DeanoC/nextpnr/tree/df90eb41/docs/validation/2026-10-22-pcxt-private-pair-frontiers)
retains 308 members with archive SHA-256
`4c82c3d307ad1148abc7dfffdd1d1c0efac58dcb93b419cfac3f9fb02cafba91`.
Its explicit hash-bound dependency is the CPU capture above. Independent root
extraction verifies both complete inventories and reproduces the complete
no-trial comparison and both frontier proofs byte for byte. The older incomplete
structural audit and original native probe compile failure remain preserved.

Compiler `a17e0ce3` adds a separate default-off ordinary ALUT2–5 input-permutation
trial with fixed BELs, exact Boolean and native allocation checks, stationary
route retention, complete source/capture coverage and full setup/hold guards.
All 45 new native tests, 39 retained source/LUT6 tests and two six-case CLI suites
pass; default/zero complete preparation controls match. The initial eight test
oracle failures are preserved and corrected separately; production binary
`7107f9fd57ee7a096c7f1153a989c105ea4ccaa9525998eac1fdc2cd1a56195b`
remained unchanged. Root verifies all 93 host evidence files and 26 exact
committed source blobs. The [immutable host capture](https://github.com/DeanoC/nextpnr/tree/550ba271/docs/validation/2026-10-22-pcxt-lut-permutation-host)
has 105 members with archive SHA-256
`3a9da14c90439588794b1c301df1357a28c15718ea8c729723fb7130e6c8d130`.
Independent root extraction verifies its explicit CPU/frontier dependencies
and reproduces both offline proofs. These are host checks with zero new route charge.

The intended CGA target remains excluded by the current unused-output guard,
so no board route is admitted from this host result. A separate narrowly scoped
opt-in for proven unused arithmetic leaves is under validation. The retained
board result remains ten setup failures, worst -434 ps, with all physical,
seed and hardware gates open. No RTL, clocks, contracts or locks change here.


## Unused arithmetic coverage and a retained CGA margin gain

Compiler `18663701` adds the explicit default-false
`--critical-unobserved-arith-outputs` option for the ordinary-LUT and private-pair
repairs. It admits only characterized, internal ARITH SO/CO leaves with no
users, routed wires or external observer. A complete live-port scan rejects
missing indexed observers; every live branch still needs full coverage and a
real capture. The LUT6 repair keeps its strict wrapper. All 173 native and 24
CLI checks pass, with exact default/zero preparation controls and independent
actual-target host replays. These are admission checks, not routed timing.

The [immutable host capture](https://github.com/DeanoC/nextpnr/tree/7c270647/docs/validation/2026-10-22-pcxt-unobserved-arithmetic-host)
has 173 members and SHA-256
`bb96052b78d487b127e105a326be221c7c6e5e1b0082d68a2418b89af0c4cce1`.
Root independently checks all members, all 907 dependency members and exact
proof reproduction. The original compile error and checker revision remain
recorded separately. All 29 source blobs match the committed compiler.

Fresh routes use frozen binary
`7faa854df524d0dd3a69cf90622b589692d2db078c031a36ac2c7a5d0deadf12`
and the unchanged original synthesis, constraints, seed and clocks. The CPU16
control reproduces the prior ten-failure RBF, timing and endpoint files exactly;
the complete routed serialization differs only in creator. Adding permutation
budget 4 and the unused-output option finds eight eligible groups and runs four
trials. One CGA ALUT5 D/E transposition is retained:

| Final routed metric | Fresh CPU16 control | Plus permutation4 |
| --- | ---: | ---: |
| CGA `rgbi_MISTRAL_FF_Q_3/DATAIN` setup | -369 ps | -234 ps |
| All setup failures | 10 | 10 |
| Whole-system worst setup | -434 ps | -434 ps |
| CPU worst setup | -72 ps | -72 ps |
| Hold failures | 0 | 0 |
| Minimum checked hold | +822 ps | +822 ps |

Exactly one checked setup margin improves by 135 ps; the other 17,193 checked
setup margins and all 17,194 checked hold margins are exact. All 17,199 endpoint
identities, windows, flags, metadata and complete clock guards pass. The other
trials reject changed stationary selector state, a 4 ps regression on an already
positive CGA endpoint, or a native clock-margin regression. No guard is waived.

The independent complete graph proof covers all 12,397 cells and 15,546 aliases,
including all 32 function rows of the changed ALUT5. Only the logged input/mask
permutation and canonical route trees 54479/54480 change. Native readback checks
all six selected input/output trees and preserves all 16 stationary users' paths
and strengths. Root reruns both proof-checker suites: 40 ordinary-LUT and 38
private-pair corruption cases reject. These checks do not substitute restored
checkpoint timing for the original fresh-route reports.

The completed control and candidate charges are 875.8208838431165 and
908.4442155929282 seconds. The journal contains 37 subsequent receipts and
cumulative charge **39632.26751609938157 seconds**. The separate private-pair
route is still running and is not charged or claimed complete here. Local
receipts and complete comparisons are under
`out/unobserved-arith-coverage-20261022/`.

The 135 ps gain is retained as a local diagnostic improvement, with ten setup
failures, 105 unsupported boundaries and `complete=false` still open. There is
no same-recipe seed qualification, production seal or hardware acceptance. No
FES RTL, constraints, clocks, toolchain lock or module/shared contract changes.

The private-pair trial has now completed on the same frozen compiler. It admits
one MDA group and attempts one candidate, then rejects local routing of
`mda_font_address[11]`. The present error combines unreachable routing and node
budget exhaustion, so this result does not identify which caused the rejection.
No decomposition is retained. The RBF, complete timing and endpoint files are
byte-identical to CPU16. Routed JSON differs in six route-attribute orderings;
all canonical wire/PIP/strength maps, cells, aliases and LAB state remain exact.
All 17,194 setup and hold margins are unchanged. The independent private-pair
full graph proof passes and requires no changed-tree native readback.

This completed route costs 608.085521012079 seconds. The shared CPU16 control
is charged once. The journal now has 38 subsequent receipts and cumulative
charge **40240.35303711146057 seconds**. The CGA 135 ps improvement remains the
best retained local result; this rejected private-pair trial adds no gain.


## Second CPU LUT6 repair pass

Compiler `5d566b0d0b93cbcb3773e4a2bd6094bbccd80353` adds an explicit
`--critical-lut6-swap-rounds 2` option. Default one-pass behavior is unchanged.
Both rounds share the existing sixteen-trial cap, and a new round requires a
retained improvement in the previous one. Candidate discovery is refreshed
from the current native timing state; every existing transaction, coverage,
setup and hold guard remains in force. The focused six source-coverage,
33 native LUT6 and nine CLI checks pass. Default/explicit-one preparation
controls reproduce the prior raw placed bytes and predicted reports.

Fresh seed1 routes at the original clocks compare default one round with two
rounds using frozen binary
`e1f208fc6aa72910b639f503c0b5fec5140c15e613c715f3e5476ab7c4d706b7`.
The one-round control reproduces the previous CPU16 result. The second round
retains the operand1 lane10 B/F transposition after rejecting three other
trials. The complete graph proof checks all 12,397 cells and 15,546 aliases;
only canonical input trees 43272/43277 change.

| Final routed metric | One round | Two rounds |
| --- | ---: | ---: |
| All setup failures | 10 | 8 |
| CPU setup failures | 4 | 2 |
| CPU worst setup | -72 ps | -25 ps |
| BP register bit13 setup | -72 ps | +174 ps |
| CX register bit14 setup | -35 ps | +211 ps |
| Whole-system worst setup | -434 ps | -434 ps |
| Hold failures | 0 | 0 |
| Minimum checked hold | +822 ps | +822 ps |

Exactly sixteen setup margins improve, with a maximum gain of 246 ps. All
17,178 other checked setup margins and all 17,194 checked hold margins are
unchanged. Complete endpoint identities, windows, flags, metadata and emitted
native clock guards pass. Two CPU endpoints remain at -25 ps. The six other
failures are three CGA, two MDA and one PIC endpoint. The -434 ps worst path
is in CGA. This recipe does not include the separate retained CGA permutation;
combining those gains requires a new full route.

The completed route charges are 714.176197880879 and 690.2115861573257 seconds.
The journal now contains forty subsequent receipts and cumulative charge
**41644.74082114966527 seconds**. Complete local artifacts and comparisons are
under `out/lut6-rounds-20261022/`. Native route-tree readback covers all seven selected input/output nets; six
stationary users preserve thirty original wire/PIP/strength tuples. Independent
root native executions reproduce both readback outputs byte for byte. Independent
review reproduces all six final proof files, including all 64 truth-table rows,
and confirms the complete graph/timing comparison. The native reader uses one
frozen source/object snapshot with an explicit structural compatibility receipt;
it performs no routing, bitstream configuration or reload timing.
There remain 105 unsupported boundaries and `complete=false`; physical,
same-recipe seed, production-seal and hardware qualification gates stay open.
No FES RTL, constraints, clocks, toolchain lock or module/shared contract changed.

The earlier CGA gain and rejected MDA private-pair route are now preserved in
an [immutable compiler capture](https://github.com/DeanoC/nextpnr/tree/1da6a49f/docs/validation/2026-10-22-pcxt-routed-lut-permutation).
Its archive SHA-256 is
`f5528953a89346e18cd10c1bc30afcf9e7d0b3f7da63d3628b04e8a7f5d6c855`.
Independent root extraction verifies all 261 members, all 667 explicit
dependency members and exact complete proof reproduction. The initial
extraction into the full shared `/tmp` filesystem stopped before replay;
that infrastructure failure is retained separately from the passing replay
on the task disk. Archive verification adds zero route charge.


## Expanded ordinary-LUT search and MDA margin gain

Compiler `02ffa3aa6a74b71debbb23da24e5332b541cf742` extends the explicit
ordinary-LUT permutation budget from four to sixteen; default zero, candidate
ordering and every transaction/coverage/timing guard remain unchanged. All
45 native and six CLI checks pass, with byte-exact default/zero preparation
controls. The native transaction executable omits the unused orchestration
through LTO; retained compile/link evidence distinguishes those transaction
checks from CLI admission and the fresh route execution below.

Fresh CPU16-plus-permutation routes with frozen binary
`ff2174157ae425d4b8c106bb22120be740230ac02b28e8a198010124f3b433e3`
compare budgets four and sixteen. The control reproduces the earlier CGA
135 ps improvement. The first four trials and preceding repairs match exactly.
One additional MDA ALUT3 A/C permutation is retained at trial six. Four setup
margins improve by 62 ps; the two failing MDA outputs improve -327 to -265 ps
and -328 to -266 ps. All other 17,190 checked setup margins and all 17,194 hold
margins are exact. There remain ten setup failures, whole-system worst -434 ps,
zero hold failures and minimum hold +822 ps. The CPU D/C and D/B alternatives
reject native clock-margin regressions; D/A was outside these sixteen trials.

The complete graph proof covers all 12,397 cells and 15,546 aliases and all
eight raw truth rows of the MDA ALUT3. Only canonical input trees 57715/57717
change. An initial offline checker rejected the M10K vector output: the exact
native frontend names both bits of a width-two B1DATA port with brackets,
including index zero. The corrected selection bridge follows the pinned parser,
requires a complete one-to-one name mapping and the actual saved physical map,
and retains the raw driver/user identities. Original scalar checker bytes and
failure evidence are retained. Two actual indexed outputs, scalar-name
preservation and eight malformed cases pass; an independent review verifies
that all other graph/function logic is unchanged.

The unchanged frozen native reader verifies the actual memory bit-port IDs,
source wires and complete four-net frontier. Two stationary users preserve
nineteen original wire/PIP/strength tuples. Independent root execution reproduces
both native outputs byte for byte. Independent review reproduces eight complete
proof outputs, including all 17,199 endpoint rows, clock guards and graph/parser
corruption controls. This does not infer timing from reloaded checkpoints.

Completed route charges are 768.3349105152301 and 845.7753356420435 seconds.
The journal now has 42 subsequent receipts and cumulative charge
**43258.85106730693887 seconds**. Complete local artifacts are under
`out/lut-permutation-budget16-20261022/`. A fresh experiment combining the
second CPU round with these CGA/MDA repairs is running separately; its results
are not inferred from the separate gains. The best completed eight-failure CPU
recipe remains distinct from this ten-failure display-improvement recipe.
No RTL, constraints, clocks, locks, shared contracts or hardware changed.

The second CPU round now also has an [immutable capture](https://github.com/DeanoC/nextpnr/tree/bfa6904b/docs/validation/2026-10-22-pcxt-lut6-rounds)
with archive SHA-256
`86105f2089992e257032b32b10c818b14e59c74a72bca14ba871c5cad890f5a8`.
Root independently extracts all 234 members, verifies all 261 dependency
members and reproduces the complete proofs. All physical, same-recipe seed,
production and exact-artifact hardware gates remain open.


## Combined second CPU pass and display permutations

A fresh pair using the same frozen `02ffa3aa` compiler and binary
`ff2174157ae425d4b8c106bb22120be740230ac02b28e8a198010124f3b433e3`
now measures both changes together. The CPU-two-round control reproduces the
previous two-round RBF, timing and endpoint reports byte for byte. The candidate
adds the sixteen-trial ordinary-LUT pass and retains both the CGA D/E and MDA
A/C permutations. One CGA setup margin improves 135 ps and four MDA margins
improve 62 ps; all other 17,189 setup margins and all 17,194 hold margins are
unchanged. Eight setup failures remain: two CPU, three CGA, two MDA and one PIC.
CPU worst setup stays -25 ps and whole-system worst stays -434 ps. There are
zero hold failures and minimum checked hold is +822 ps.

The complete 17,199 endpoint identities, windows, flags, metadata and native
clock guards pass. Complete graph proof covers 12,397 cells, 15,546 aliases,
all 32 CGA and eight MDA truth rows, and only four changed canonical input
trees (54479, 54480, 57715, 57717). The frozen native reader verifies ten selected
nets and all 65 original wire/PIP/strength tuples for eighteen stationary users.
Independent root executions reproduce both native readbacks byte for byte;
independent review reproduces the complete graph and timing proof outputs.

Completed route charges are 839.973974572029 and 904.7418550127186 seconds.
The journal contains 44 subsequent receipts and cumulative charge
**45003.56689689168647 seconds**. Artifacts are under
`out/cpu2-permutation16-20261022/`. These improvements are now measured in one
recipe; closure remains open with 105 unsupported boundaries and `complete=false`.
Physical, same-recipe seed, production-seal and exact-artifact hardware gates
remain open. No RTL, constraints, clocks, lock or shared contracts changed.

Separately, compiler `f5c6b90df4b8a36a6a8f631998d52ef160c2044b` adds an
explicit bounded private-pair local-routing node budget. Default 500,000 nodes
and all acceptance guards are unchanged; the optional maximum is 2,000,000.
The 81 native and nine CLI checks pass, and default/zero preparation controls
reproduce the parent exactly. Fresh default/2,000,000 route trials are running;
host validation is not a claim that the rewrite improves final routed timing.


## Private-pair local search bound: routing succeeds, timing rejects

Fresh default-500,000 and explicit-2,000,000 node routes using compiler
`f5c6b90df4b8a36a6a8f631998d52ef160c2044b`, binary
`d1abff2e8eae7021a1bc291634b45d7bb290e4391b87d6380542c14e6120ce80`,
now complete. Both keep the original one-round CPU16/private4 recipe and clocks;
this pair does not include the separate second CPU round or display permutations.
The default control reproduces the earlier private4 route exactly and rejects
local routing of `mda_font_address[11]` at the original search bound. The larger
search reaches full native timing for the same MDA rewrite, then rejects a hold
margin regression at `dot_MISTRAL_FF_Q` DATAIN, from +942 to +936 ps. The existing
nonregression guard remains unchanged; neither trial retains a decomposition.
The rejected trial's complete live timing inventory was not emitted, so this
result does not establish its other setup/hold margins or a path-level cause.

Complete final routed JSON, RBF, timing and endpoint files are byte-identical
between the two runs. The complete 12,397-cell/15,546-alias graph proof has no
changed route tree or route-attribute ordering. All 17,199 endpoint identities,
windows, flags and metadata match; all 17,194 checked setup and hold margins are
exact. Both retain the baseline ten setup failures, worst -434 ps, zero hold
failures and minimum hold +822 ps. No changed-tree native readback is needed. Independent review reproduces all
seven full proof outputs, including 38 graph/log and eleven node-ledger
corruption cases, and independently compares the four final artifact bytes.
The eight-failure combined CPU/display result remains the best retained recipe.

Completed route charges are 899.2859137370251 and 913.594029861968 seconds.
The journal now contains 46 subsequent receipts and cumulative charge
**46816.44684049067957 seconds**. Artifacts are under
`out/private-pair-node-budget-20261022/`. This is a completed rejected host
experiment with exact rollback, not timing closure or hardware acceptance.
All physical/coverage, same-recipe seed, production and hardware gates remain
open. No RTL, constraints, clocks, lock, shared contracts or hardware changed.

The combined CPU/display route now has its own
[immutable compiler capture](https://github.com/DeanoC/nextpnr/tree/b2ae8ec3/docs/validation/2026-10-23-pcxt-cpu-display-combination),
archive SHA-256
`3333e2524c928aec6704c3fd8374b45208cc4964ff3598c5fdfac4604971d22b`.
Independent root extraction verifies all 100 members, all 256/234 members of
the two explicit dependencies, and exact full proof and review reproduction.
Archive replay incurs no route charge.


## Combined-recipe placement seeds remain diagnostic

Two fresh routes now apply the same CPU-two-round/permutation16 recipe to
seeds 2 and 3, using the frozen `02ffa3aa6a74b71debbb23da24e5332b541cf742`
compiler and `ff2174157ae425d4b8c106bb22120be740230ac02b28e8a198010124f3b433e3`
binary. Only the seed differs from the measured eight-failure seed-1 reference;
all original clocks, raw synthesis and constraints are unchanged.

| Seed | Setup failures | Worst setup | CPU setup failures | CPU worst | Hold failures | Minimum hold |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 (prior reference) | 8 | -434 ps | 2 | -25 ps | 0 | +822 ps |
| 2 | 356 | -1734 ps | 302 | -1734 ps | 0 | +719 ps |
| 3 | 50 | -384 ps | 42 | -384 ps | 0 | +781 ps |

The complete comparison preserves all 17,199 endpoint/domain rows, row windows
and timing flags, schema-7 clock contracts and native clock constraints.
All changed rows and every non-endpoint metadata difference are retained.
These independent placements can improve some margins and regress others;
no cross-seed nonregression or graph equivalence is inferred. Both new seeds
retain 105 unsupported obligations and `complete=false`. Neither closes timing
or establishes a same-recipe two-of-three pass. The eight-failure seed-1 result
remains the selected diagnostic recipe.

The two completed route charges are 1188.7011790256947 and
1083.278275907971 seconds. The journal now contains 48 subsequent receipts and
cumulative charge **49088.42629542434527 seconds**. The original seed-1 reference
is not recharged. Complete evidence is under `out/combined-seeds23-20261023/`;
comparison SHA-256 is
`d3c7a4c049710c771d38095bac4ebaf16d86df5632063891c00ef38d3fdcf61f`.
Independent replay reproduces the comparison bytes and recomputes every changed
row, metadata difference and journal total; review SHA-256 is
`daf038b765daf06e7d8893782ea46bf767ac0ac8323edc7334e4f2645aa15444`.
This is host diagnostic evidence. No RTL, clocks, constraints, toolchain lock,
shared contracts or hardware changed.

The completed private-pair rejection has an
[immutable compiler capture](https://github.com/DeanoC/nextpnr/tree/d2548af0/docs/validation/2026-10-23-pcxt-private-pair-node-budget),
archive SHA-256
`26dea41e116f9ae89bba40c68e7caacaeb47f31a0c2673c8d7ce66371e1540d7`.
Root fresh extraction verifies all 245 members and both complete 261/256-member
dependencies, reproduces all seven proof outputs and the independent review,
and confirms all four final artifacts remain byte-identical. Replay incurs no
route charge.


## Expanded ordinary-LUT search exhausts eligible trials without another gain

Compiler `134b858165a4dd0c00c8b5d5371fbf90fdac63a3`, binary
`aba33a3cc6b3a322f98c22853efc0d1d9fe4d781707932c6249640cd5395662c`,
raises the explicit ordinary-permutation ceiling to 32, preserving default zero,
ordering and every native acceptance guard. All 45 native and six CLI tests pass;
default/zero preparation remains exact apart from the recorded creator label.

The fresh budget16 control reproduces the previous combined CPU/display result.
The budget32 candidate exhausts all 23 eligible trials. Its first sixteen complete
decisions match the control; all seven additional trials reject at the native
clock margin/coverage guard. Neither the extra budget nor the rejected trials
produce a retained gain. Eight setup failures remain, CPU worst -25 ps and
whole-system worst -434 ps, with zero hold failures and minimum hold +822 ps.

Every one of the 17,199 endpoint identities, windows, flags and metadata entries
is accounted for. All 17,194 setup margins and all 17,194 hold margins are exact,
as are the native clock summaries. Complete graph proof covers all 12,397 cells
and 15,546 aliases, with no changed canonical route tree. RBF, timing and endpoint
files are byte-identical. Routed JSON differs only in five routing-attribute
serialization orders; every source, sink, wire, PIP, strength and other JSON
field is unchanged. No native tree-reader execution is needed. Independent
replay reproduces eight complete proof/control outputs; review SHA-256 is
`fe19edc97c03cb03ed8d0cc1a52c3a9bc8f446063681fe0cc7e94bff5fa7290a`.

Completed route charges are 862.5985456877388 and 933.8916614698246 seconds.
The journal contains 50 subsequent receipts and cumulative charge
**50884.91650258190867 seconds**. Evidence is under
`out/lut-permutation-budget32-20261023/`. This completes the bounded host
experiment with no additional improvement; a larger trial ceiling alone has
no further candidates in this discovery. Timing closure, the 105 unsupported
boundaries, same-recipe seed, production and hardware gates remain open.
No RTL, clocks, constraints, lock, shared contracts or hardware changed.
