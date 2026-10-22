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
