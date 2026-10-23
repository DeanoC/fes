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


The completed budget32 result now has an
[immutable compiler capture](https://github.com/DeanoC/nextpnr/tree/f3549d6c/docs/validation/2026-10-23-pcxt-lut-permutation-budget32),
archive SHA-256
`5c8771558434c8713f8ba79cd1afce75c6398315012058f27c96df4d0d2b3b02`.
Root fresh extraction verifies all 284 members and complete 245/100-member
private-node/combined dependencies, reproducing every portable proof and the
independent review. It runs no historical routes or native timing.

## Preserved source exits reach native timing but retain no new gain

Compiler `1ba2e289f5bf4e584a8539f185d6f15383650c26`, binary
`04c5ade4b55f8b7b48a4e79d8cf85e38d08a08bfd2819f1dbf9ec6a9c40cb868`,
adds the explicit default-off `--critical-lut-permute-retain-source-exits`.
It retains only original directly rooted COMB-output exit tuples with complete
native driver, owner, half, wire/PIP and strength identities. It preserves the
stationary LAB/FF configuration guard and every setup/hold/coverage predicate.
Host admission passed 57 native tests and eight CLI tests, plus exact default
and explicit-zero controls. The isolated low-level probes are structural
rollback evidence; the fresh routes below provide the timing evidence.

Both raw seed1 routes retain the original 100 MHz system/CPU/microcode and
74.25 MHz pixel constraints, two CPU LUT6 discovery rounds and permutation32.
The option-off control reproduces the preceding budget32 artifacts apart from
the recorded JSON creator. Each route attempts 23 permutations and keeps the
same two previously accepted CGA/MDA permutations.

The intended CGA D/E, D/C and D/B trials now pass the previously blocking
stationary-selector check, then reject on native setup regression at
`system.machine.isa.rgbi_MISTRAL_FF_Q_1.DATAIN`: -26 ps becomes -39, -37 and
-37 ps respectively. The D/E trial improves whole-clock setup from -434 to
-369 ps, but that aggregate improvement does not override the individual loss.
No rejected candidate is retained.

All four final artifacts (RBF, routed JSON, timing report and endpoint inventory)
are byte-identical between option-off and option-on. All 17,199 endpoint
identities/windows/flags/metadata and all 17,194 setup and hold margins match.
Eight setup failures remain: two CPU, three CGA, two MDA and one PIC. CPU worst
setup is -25 ps; whole-system worst is -434 ps. Related hold has zero failures
and minimum +822 ps. Each complete 12,397-cell/15,546-alias graph is proved
separately against the common CPU2 graph using its own complete accepted ledger.

Fresh route charges are 1304.8248345819302 seconds for the control and
1298.359501537867 seconds for retention, total 2603.1843361197972 seconds.
The 52-receipt journal cumulative charge is 53488.10083870170587 seconds.
No hardware ran. The 105 unsupported boundaries and complete=false remain;
there is no timing closure, same-recipe seed qualification, seal or hardware
acceptance. No RTL, clocks, constraints, toolchain lock or shared contracts changed.
The next routing experiment uses the option-off baseline because this option
provides no retained benefit on this recipe.

Native saved-tree readbacks independently decode each common-CPU2 comparison:
ten nets, eighteen stationary users and 65 retained original wire tuples.
Fourteen complete graph/ledger/parser/topology/timing outputs replay exactly;
independent review SHA-256 is
`45516c79bd4c4febdb063ed202b46d3d2d68e1f022241a7c5b0919620649c030`.
The initial replay stopped because a parser-control output had not been generated;
that failure is retained, the existing parser controls then pass, and the complete
replay succeeds. This is saved native topology evidence, not a new STA run.
Evidence is under `out/cga-source-exit-retention-20261023/`.

The source-exit result now has an
[immutable compiler capture](https://github.com/DeanoC/nextpnr/tree/e6ea2a6cf40a2236c4166b07591ca55e709393e2/docs/validation/2026-10-23-pcxt-source-exit-retention),
archive SHA-256 `bb41b70758c9b2a2b09069ddcc7c3f5bde829a40f082dcf208310d1a5422f221`.
Root fresh extraction verifies 418 members and complete 284/100-member
budget32/combined dependencies. Separate portable checker copies change only
three explicit path lookups; original live proofs remain unchanged. Only declared
checker-digest provenance differs, and all substantive proof results are exact.
No new native execution or historical route is claimed.

## Final route rediscovery finds no improving proposal

Compiler `626e368b0529c4516aa3a7a4bb4bcdd034011e30`, binary
`52dda57ca0a9da4c57f9f5836764e791aced3e9efab9da9b1c64543c51a417f5`,
adds the separate default-zero final-route budget. Its 31 native and nine CLI
tests and exact default/zero controls pass. Two fresh routes retain the same raw
synthesis, original 100 MHz CPU/system/microcode and 74.25 MHz pixel constraints,
route32, two LUT6 rounds and permutation32. Source-exit retention stays disabled.
Only the candidate adds `--critical-final-route-budget 32`.

After the preceding repairs, fresh timing identifies 24 failing ordinary sink
rows and selects 21 nets. Two bounded proposal generations log 17 candidates;
every native gain is negative, with best -20 ps. None reaches the +20 ps trial
threshold, so zero full trials run and nothing is retained. This bounded result
does not establish that no legal improving route exists.

RBF, timing and endpoints are byte-identical between the two results. The full
12,397-cell/15,546-alias graph has no canonical tree or other state changes;
routed JSON differs only in 15 routing-attribute serialization orders. No new
native reader execution is required. All 17,199 endpoint identities and metadata,
17,194 setup and hold margins, clock categories and constraints are exact.
Eight setup failures remain, CPU worst -25 ps and overall worst -434 ps; related
hold has no failures and minimum +822 ps. The control also reproduces the prior
fixed result, with only its routed JSON creator label changed.

Root's complete comparisons and the independent eight-output replay pass.
The review includes 54 graph/log negatives, 24 topology negatives and three
preserved checker corrections; SHA-256 is
`110ee76bd47952b2b812f32ee3e2a9f7376d4a933a5cca741bd876b8ac931a89`. Evidence is under
`out/final-critical-route-rediscovery-20261023/`.
The two route charges are 1071.680669224821 and 1186.9478577878326 seconds;
the 54-receipt cumulative charge is **55746.72936571435947 seconds**.

An isolated display packet pipeline is the next full-board RTL diagnostic.
Formal checks preserve the original dot sample and same-cycle ISA/MDA status
while delaying the complete exported tuple one system edge. The three unchanged
display integration suites pass. Ten additional consumer runs check 260 output
frames and 192 HOLD edges, including queued publication; complete-frame skips
or repeats remain allowed. Synthesis/path inspection must include the new
packet-renderer-to-MDA-status path before routing. These tests are host evidence,
not timing or hardware qualification; shared RTL is unchanged.

Both PRs remain draft. The 105 unsupported boundaries and `complete=false`,
final timing, same-recipe seed, fresh producer and exact-artifact hardware gates
remain open. No clocks, constraints, toolchain lock or shared contracts changed.

## Display packet clears its timed paths; CPU remains dominant

The preceding final-route no-gain evidence is now available as an
[immutable capture](https://github.com/DeanoC/nextpnr/tree/6a467579/docs/validation/2026-10-23-pcxt-final-route-rediscovery).
Fresh extraction checks 294 members plus the exact 418-member source-exit
capture and reproduces all eight proof outputs. Root independently obtains the
same readback hash `6be0566ed9eecc3feba42d978a9600064a6e37b5654d4588d24e64b4b5d7a356`.

The isolated display packet now has a completed full-board route comparison.
Both routes use frozen compiler `626e368b`, binary `52dda57c`, seed 1 and the
original 100/74.25 MHz constraints, with route32, two LUT6 rounds and
permutation32. The candidate uses literal synthesized JSON `8962f247`; the
source-verified original uses `1d31765e`. No net names or source paths are
rewritten. The old named 17-input CPU reduction root is absent in the new
mapping, so that transform is removed from both cases. The balanced eight-failure
recipe remains a separate reference; these are independent new placements.

| Final native result | Original RTL, fresh control | Display packet |
| --- | ---: | ---: |
| Setup failures | 82 | 59 |
| Worst setup margin | -967 ps | -1196 ps |
| Related hold failures | 0 | 0 |
| Minimum related hold | +781 ps | +785 ps |
| Complete endpoint rows | 17199 | 17369 |
| Untimed rows | 5 | 5 |
| Unsupported obligations | 105 | 105 |

The candidate has 45 CPU and 14 other failing rows; CGA, MDA and PIC attributed
rows have none. Source-derived packet queries find every mapped capture:
CGA RAM-to-packet minimum +2245 ps, MDA RAM-to-packet +1699 ps,
CGA packet-to-RGBI +3864 ps and the exact MDA packet-to-ISA status capture
+2237 ps (hold +2648 ps). The positive MDA status row has no bounded reported
path witness, which is retained as a reporting limit. Other display paths are
less comfortable: CGA CRTC enable minimum is only +4 ps and MDA minimum +185 ps.

All 59 failing candidate rows have reported path witnesses. Thirty-six launch
from CPU `eu_register_r0_MISTRAL_FF_Q_8`; the worst path contains 6.780 ns of
routing and 3.880 ns of logic. Eleven launch from the ISA bridge response
register. The display change therefore warrants further fixed-seed diagnostics,
but this result does not improve the retained eight-failure whole-board recipe.

The full comparison enumerates 1606 added and 1436 removed endpoint identities
and every common row, clock, native report and graph inventory. All ten clock
constraints and physical-obligation records are exact. Unsupported count stays
105 but serialized identities change, so equal counts are not a coverage or
semantic-equivalence proof. No fixed-placement attribution is made.

Local evidence is in `out/display-packet-route-20261023/`, with host admission
in the separate packet, consumer and synthesis folders. The two completed
charges are 1044.5264138458297 and 925.1706512258388 seconds, total
1969.6970650716685. The journal now has 56 subsequent receipts, preserving the
exact prior 54-entry prefix, and cumulative charge **57716.42643078602797 seconds**.
The source change remains isolated: shared core RTL, contracts, lock, clocks
and constraints are unchanged. No hardware ran and both PRs remain draft.

Independent actual review
`a84e9f195dc24df3ccc319b23a41b5a73328faaec4d946402333a624a36e7c0e`
reproduces all 24 complete analysis outputs and verifies the new receipt charges
and exact journal prefix. Twenty focused comparator controls pass. Fixed seeds
2 and 3 of the identical packet recipe are now running under selection
`38e6fa4ddf96cf03a9dd5a3e002dc0776f6ad01059ba11e753b33f8843d1b28b`;
only the seed changes. They are not yet charged or reported as completed.
If the same CPU launch cone remains dominant, that cone is the next repair target.

## Display packet seed comparison does not qualify the recipe

Seeds 2 and 3 of the exact packet recipe are complete. The raw mapped input,
compiler, clocks and all route options match packet seed 1; only the seed changes.
All three retain 17,369 endpoint rows, 17,364 checked setup/hold rows, five untimed
rows and 105 unsupported obligations. No setup pass is obtained.

| Final native result | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Setup failures | 59 | 131 | 92 |
| Worst setup margin | -1196 ps | -996 ps | -1043 ps |
| CPU failures | 45 | 77 | 40 |
| CGA / MDA / PIC failures | 0 / 0 / 0 | 2 / 7 / 1 | 0 / 5 / 0 |
| Other failures | 14 | 44 | 47 |
| Related hold failures | 0 | 0 | 0 |
| Minimum related hold | +785 ps | +788 ps | +719 ps |
| Exact MDA status setup | +2237 ps | +2514 ps | +2407 ps |

The packet pipeline does not improve the retained balanced eight-failure board
recipe and remains isolated. Complete pairwise inventories preserve all endpoint
identities and clock/contract categories; physical details and positive margins
change with placement and are reported explicitly. Equal inventory size is not
physical acceptance. The packet seed-1 CPU launch that reaches 36 failing rows
is absent from the failing reported launches in seeds 2 and 3. Generated names
also cannot identify RTL bits across resynthesis: the packet's r0_Q_8 is r0[1],
whereas that suffix in the original mapping identifies r0[9]. Any next repair must
use real connectivity and source aliases rather than those suffixes.

Evidence is under `out/display-packet-seeds23-20261023/`. Independent review
`415707250609700bc1fa12859239c00defdb777fca9f4328bb72ccd743cb12fa` reproduces all 29 analysis outputs, including the summary and checks
receipt identity and accounting. Fresh charges are 1030.1297092940658 and
938.7390288948081 seconds, total 1968.8687381888739. The 58-receipt journal
preserves the exact 56-entry prefix and has cumulative charge
**59685.29516897490187 seconds**.

The next paired diagnostic runs the original balanced raw input with default-off
compiler repair `--critical-final-route-buf-users`. Both routes use final-route
budget 32 and compiler `e1a23c3b324febad36f80a3db9e042ae90777988`, binary
`477dd65034559a13019a18b1b99e27bf60407feb350bd38146548a1a97a26204`.
The candidate alone admits verified native transparent buffers feeding private
register inputs. Host admission has 47 native and 12 CLI tests plus exact
whole-RBF structural reconstruction; those are not final timing evidence.
The root-owned matched pair is running under shared GPU phase leases and is
not yet charged as complete. Source, runner and selection are frozen.

These are host diagnostics. Both PRs remain draft. CPU/system/microcode clocks
remain 100 MHz and pixel 74.25 MHz; shared RTL, toolchain lock, constraints and
shared contracts are unchanged. The 105 unsupported obligations, complete=false,
same-recipe timing qualification, fresh producer and exact-artifact hardware
acceptance gates remain open. No hardware ran.

## Native buffer admission reaches the ALU net but retains no route

Compiler `e1a23c3b324febad36f80a3db9e042ae90777988`, frozen binary
`477dd65034559a13019a18b1b99e27bf60407feb350bd38146548a1a97a26204`,
completes a matched original-RTL seed-1 pair. Both use the retained balanced raw
input, route32, two LUT6 rounds, permutation32 and final-route32 at the original
100/74.25 MHz clocks. Only the candidate enables
`--critical-final-route-buf-users`. Source-exit retention remains disabled.

The control selects 21 nets from 24 failing ordinary sink rows and measures
17 proposals. Buffer admission selects 22 nets from 26 rows and measures 20.
The added net is actual CPU ALUout[3], including its fixed transparent buffers
feeding private register inputs. Its three new native gains are -568, -519 and
-533 ps. The best proposal overall remains -20 ps, below the unchanged +20 ps
trial threshold. Both cases perform zero final-route trials and retain nothing.

The complete saved graphs, native ownership, LAB configuration and pin maps are equal;
all 12,397 cells and 15,546 alias classes remain intact. The candidate differs
only in one ALUout routing-tuple serialization order. RBF, timing and endpoint
files are byte-identical. All 17,199 endpoint identities/windows/flags/metadata
and all 17,194 checked setup/hold margins match. Eight setup failures remain:
two CPU, three CGA, two MDA and one PIC. CPU worst is -25 ps and whole-system
worst -434 ps. Related hold has no failures and minimum +822 ps.

Independent complete review `3008f6a621d87ee9a5dff8a4ce437f3cac2ded3a34a3f60e7ab026f4cd9716c1` reproduces the prepared
analysis and exact accounting. Since neither canonical route changes, the
matching-object native reader is not executed; no reload timing is claimed.
The corrected reader and graph/timing controls remain available with the capture.
The earlier host admission passed 47 native and 12 CLI checks plus whole-RBF
structural reconstruction. These support the bounded implementation, not a
routed timing improvement.

Fresh charges are 1101.456457699649 and 1110.3485453659669 seconds, total
2211.8050030656159. The 60-receipt journal preserves the exact 58-entry prefix
and has cumulative charge **61897.10017204051777 seconds**. Full local evidence
is under `out/final-route-buf-users-20261023/`.

The original eight-failure recipe remains the reference with buffer admission
disabled. An isolated host synthesis experiment now keeps the CPU module through
mapping to test whether a display edit can preserve its complete mapped graph;
this is not recovery of the old mapping or fresh timing evidence. A separate
native placement preview checks legal empty destinations under the existing
ordinary-LUT rules, without changing routes or claiming timing gains.

Two fresh original-netlist placement diagnostics are now running with timing
weights 4000 and 8000 versus the completed weight-2000 control. Seed 1, exponent
5, all repair budgets and original clocks remain fixed; buffer admission is off.
The independently reviewed runner changes only that weight and uses shared GPU
phase leases. These routes are not yet charged as completed.

No shared RTL, clock, constraint, toolchain lock or shared contract changes.
Both PRs remain draft; no hardware ran. The 105 unsupported obligations,
complete=false, same-recipe timing, fresh producer and exact-artifact hardware
acceptance gates remain open.

## Higher placement weights regress the original board

Two fresh routes retain the exact original balanced input, frozen compiler
`e1a23c3b` / binary `477dd650...26204`, seed 1, exponent 5, all repair options
and original clocks. Only placement timing weight changes. Both complete with
failed timing; the completed weight-2000 reference remains the best result.

| Placement timing weight | Setup failures | Worst setup | Hold failures | Minimum hold |
| --- | ---: | ---: | ---: | ---: |
| 2000 reference | 8 | -434 ps | 0 | +822 ps |
| 4000 | 354 | -2190 ps | 0 | +778 ps |
| 8000 | 375 | -1887 ps | 0 | +773 ps |

All 17,199 endpoint/domain identities, windows and flags remain present, with
17,194 timed setup/hold rows and 105 unsupported boundaries. Complete timing,
clock and metadata unions and lossless raw-graph differences are retained;
independent placement changes are not a Boolean-equivalence proof. The reviewed
comparator has 36 focused controls, including empty endpoint identities and
complete reconstruction of changed graph objects. No larger-weight or unchanged
seed sweep follows these regressions.

Independent actual review
`481e5070dcacd85429ac14c3369d885d2f5b0e569e683cef1b5fdff87760252d`
reproduces all six complete comparison outputs and verifies both lossless graph
patches and exact receipt accounting.

The pair charges 1155.411436055787 and 1180.2349809180014 seconds, total
2335.6464169737884. The 62-entry journal preserves its exact prior 60-entry prefix
and cumulative charge is **64232.74658901430617 seconds**. Evidence is under
`out/higher-timing-weights-20261023/`.

## Preserved CPU synthesis boundary enters a fresh paired diagnostic

An isolated recipe keeps `pcxt_cpu` through the existing pinned mapping stages,
then removes that added hierarchy attribute and flattens. Original and display
packet builds have matching complete CPU graphs: 2,574 cells, 1,498 aliases,
19 ports, 1,185 FFs, 223 arithmetic cells and 16 initialized M10Ks. Generated
names differ; the proof compares full functions, state and connectivity.
Independent replay verifies complete flatten/cleanup, state and path inventories,
including all 318 RAM initialization/parameter records and 94 IO/PLL/clock
records. The boundary changes mapping from the older flattened builds; it does
not recover the old best-eight CPU placement or establish board equivalence.

The actual CPU operand/carry/writeback paths remain, including the 17-stage carry
chain. CGA packet capture splits the old 7–8-LUT RAM-to-RGBI path into direct
RAM-to-packet FF connections and five-LUT packet-to-RGBI paths. MDA packet-to-ISA
status still has six LUTs. Its unused exported RGBI outputs are optimized away
in this CGA build; zero selected captures do not imply timing clearance.

The bounded overflow reduction is rediscovered from actual DX/AX register
aliases in both designs, checked over all 131,072 input rows, and accepted by
native planning and packing into 6+6+5 inputs. Independent runner review
`99d608dae3ba7ad4c558897da3e4896d3c4f45afc0caef9ef604be06cff788ea`
verifies the 3,864-file host manifest and fourteen admission controls. Root
selection `0d8cefef4dc6f886d880d86ddfbe0a54f6fb8377d825c9256a95ddde8a44c226`
admits two fresh whole-board diagnostics using the original best recipe. Only
literal mapped input and the proven reduction root change. They are running
under the existing two-route shared GPU limit and are not yet charged as complete.

Shared RTL, synthesis recipes, constraints, toolchain locks and shared contracts
remain unchanged. Both PRs stay draft; no hardware ran. Complete native coverage,
same-recipe timing qualification, fresh producer and exact-artifact hardware
acceptance remain open.

## CPU boundary pair completes without a timing improvement

The admitted original/packet routes complete with the same frozen compiler,
100/74.25 MHz clocks and best-reference recipe. Preserving the CPU synthesis
boundary provides a stable logical CPU graph across the display edit, but does
not preserve placement or recover the eight-failure reference.

| Final native result | Best reference | Original with CPU boundary | Packet with CPU boundary |
| --- | ---: | ---: | ---: |
| Setup failures | 8 | 69 | 108 |
| Worst setup | -434 ps | -1575 ps | -1285 ps |
| Endpoint rows | 17199 | 17207 | 17377 |
| Hold failures | 0 | 0 | 0 |
| Minimum related hold | +822 ps | +791 ps | +683 ps |
| Unsupported boundaries | 105 | 105 | 105 |

Both remain `complete=false`. The previously reviewed 49-control comparator
completes against the actual artifacts: all added/removed endpoint identities,
full timing/clock/metadata categories and lossless raw/routed graph differences
are retained. Generated names are descriptive keys, not semantic equivalence
across mappings. Root independently checks the emitted row counts and margins;
a separate independent replay of these actual comparison outputs is still
pending. No recipe or packet RTL is adopted.

The original route charges 1611.6099411519244 seconds and the packet route
1383.5658514136449 seconds, total 2995.1757925655693. The 64-entry journal retains
its exact prior 62-entry prefix and cumulative charge is
**67227.92238157987547 seconds**. Evidence and the complete comparison are under
`out/cpu-synthesis-boundary-20261023/`.

The separate final ordinary-LUT move proposal remains OUT-only, with host
validation unfinished. No shared RTL/compiler source, constraints, locks,
contracts or hardware changed during this result capture. Both PRs remain draft
and timing/coverage/producer/exact-artifact acceptance gates remain open.


## Final ordinary-LUT move diagnostic retains no move

The default-zero final move stage is committed in nextpnr `46b763ba`, based on
`e1a23c3b`, with frozen binary
`7633292517393d08a71b46303d5a9448189c73a77130e8b6055709ebf5952293`.
Host checks pass 44 native LAB/MLAB/both-half cases and seven CLI controls.
The separately reviewed selector boundary permits only selected-Q-owned direct
COMBOUT output muxes with exact native NLUT/REG settings; stationary FF input
selectors, controls, masks, routes and full setup/hold checks remain protected.

One reviewed fresh matched pair retains original b4 synthesis, QSF/SDC,
100/74.25 MHz clocks, seed1, placement weight2000/exponent5 and all best-reference
repair budgets. Candidate alone enables `--critical-final-move-budget 16`.
Both complete at eight setup failures / -434 ps, zero hold failures / +822 ps
minimum hold, 17,199 endpoint rows and 105 unsupported boundaries,
`complete=false`. Candidate discovers nine eligible cells but tries only four
legal shortlisted destinations, all for one CGA ALUT5. Every trial rejects
`stationary mate LUT mask changed`; no move is retained.

The complete matched RBF, routed JSON, endpoints and timing reports are byte
identical. Full reference comparisons, all endpoint/clock/report categories and
lossless graph patches are retained. Independent actual replay
`a60de3d1c81d51a83119e7cc44ad265a4d4aa54ebf627c1111b67a6aa6e6be4f` reproduces every output and verifies complete accounting.
Checker preparation passes 53 inventory/graph/recipe controls and fourteen
native endpoint/clock regression controls. No row intersection substitutes for
complete inventory, and no reference-free final inventory is separately emitted.

The baseline charges 955.9725542096421 seconds and candidate 999.028345788829,
total 1955.0008999984711. The 66-entry journal preserves its exact first64 prefix;
cumulative charge is **69182.92328157834657 seconds**. Evidence is under
`out/final-ordinary-lut-move-route-20261023/`.

Final-move remains disabled in the retained reference. The rejected source ALM
contains only the moved LUT; canonical empty-ALM mask/mode handling needs a
separate native diagnostic before changing any guard. Geometric possibilities
do not establish live shortlist eligibility or timing gain. Shared FES RTL,
constraints, locks and contracts remain unchanged. Both PRs remain draft;
no hardware ran and timing/coverage/producer/exact-artifact gates remain open.

### Canonical empty-ALM move: retained 165 ps CGA gain

Compiler `c93d81a7eff027c94e879d713ffcb8f03492c140` (binary
`d9e25b4dd46eaf5ee946496812e2647fb01a624afa2662b993e05422e143c562`)
adds bounded proofs for canonical empty-ALM mask/mode transitions and direct
incident COMBOUT output-selector ownership. All six LUT/FF BELs and internal
observers must be absent in the empty frame. Complete native fields, moved
truth, stationary state and exact rollback remain guarded. Original malformed
selectors and empty-target fields reject before placement previews. Indexed
ownership avoids repeated full-netlist scans. Default zero remains disabled;
legacy callers and timing/coverage guards are unchanged.

Host evidence passes 72 native LAB/MLAB/both-half cases and seven CLI controls,
with exact parent/default/zero empty-design CPU preparation bytes. Four actual
board-graph native trials reach forced-false callbacks and restore exactly.
Eight final negative controls reject four released remote-output equal-value
faults and four foreign-owned NLUT-to-REG faults. Earlier fixture-selection
failures remain retained; the final eight do not claim acquisition coverage.
Independent LOW source, fixture, host and runner reviews pass. All 3,393 frozen
source files match the committed tree and pinned submodule blobs.

A fresh matched pair keeps the original b4 synthesis, seed 1, weight 2,000,
exponent 5, 100 MHz system/CPU/microcode, 74.25 MHz pixel, QSF/SDC and earlier
repair recipe. The candidate alone adds `--critical-final-move-budget 16`.
Nine cells are eligible; four destinations are tried. Three MLAB targets reject
native timing/clock margin checks. One CGA ALUT5 moves from
`MISTRAL_COMB.18.9.43` to `MISTRAL_COMB.17.9.13`, retaining its type and pinmap.
Its failing RGBI capture improves from −434 to −269 ps, a 165 ps gain. Whole-board
WNS improves from −434 to −270 ps. Eight setup failures remain; both runs retain
17,199 rows (17,194 timed), zero hold failures, +822 ps minimum hold, 105
unsupported boundaries and `complete=false`.

Complete emitted endpoint and clock nonregression passes. The full logical
nonattribute inventory is identical; one cell BEL attribute and six routing
aliases change. Independent replay reproduces every comparison output,
including lossless graph patches, endpoint/domain unions, metadata, clock
reports and failing witnesses. The retained actual RBF reconstructs exactly
from its saved routed graph in a separate zero-trial native readback. This
readback establishes neither STA reproduction nor hardware acceptance.

This becomes the research timing reference, with PIC −270 ps, CPU −25/−25 ps,
and display −26/−269/−234/−265/−266 ps failures still open. No production recipe,
lock or seal is selected. Original constraints, FES RTL and shared contracts
remain unchanged; both PRs remain draft. Same-recipe seed, complete physical
coverage, fresh producer and exact-artifact hardware gates remain open.

Evidence is in `out/empty-alm-final-move-route-20261023/` and
`out/empty-alm-move-proposal-20261023/`. Accounting now covers 68 unique
whole-route receipts, cumulative `71226.12229338567657` seconds; this pair adds
`2043.199011807330` seconds. The prior 66-entry prefix is preserved exactly,
and independent replay verifies the decimal sum. No hardware ran.


## 23 October: bounded final-move rediscovery removes one setup failure

Compiler `b85677cfc9708b994b29fadd12debdd73171bfb4` (tree
`e2e5146b345363de8976277aff65904c469a038d`), based on `c93d81a7`, adds the
separate runtime-only, default-false `--critical-final-move-rediscover` option.
It requires the existing positive final-move budget and fresh full GPU route.
After an actual retained move, one additional discovery rebuilds both timing
frames, the live endpoint inventory and geometry from current BELs. The same
16-trial total ceiling, 32-cell/four-destination caps, native transactions,
selector ownership, coverage and positive setup/hold guards remain. There is no
budget reset or third discovery.

All 3,393 frozen source blobs match the committed tree and recursively pinned
Gitlinks. A full isolated ABI rebuild passes, followed by 84 native tests
(original 72 plus 12 real orchestration cases across LAB/MLAB and both halves)
and ten CLI controls. Default/explicit-zero bytes match within the new binary;
the parent/default complete empty-design graph differs only in its recorded
creator label. The binary SHA256 is
`bbe8222b1aa041d1b199dc7ddbee500fe0ad3a5e87e93a2145abf52677566cad`;
its base-derived build label is distinguished from the verified commit.

A fresh matched pair preserves the original b4 synthesis, QSF/SDC, seed 1,
weight 2,000, exponent 5, all earlier repair settings and 100 MHz
system/CPU/microcode plus 74.25 MHz pixel clocks. Both enable final-move16;
only the candidate adds rediscovery. The baseline exactly reproduces the prior
c93 RBF, timing and endpoint bytes: eight setup failures and WNS -270 ps.
Its routed-JSON differences remain fully recorded.

The first pass retains the prior CGA ALUT5 move. The second discovery finds
seven destinations and retains a CGA ALUT2 move from `MISTRAL_COMB.18.9.6` to
`MISTRAL_COMB.17.9.42`. The RGBI capture
`system.machine.isa.rgbi_MISTRAL_FF_Q_2.DATAIN` improves from -269 to +89 ps,
a further 358 ps gain. Two moves are kept across 11 of the shared 16 trials.
Setup failures fall from eight to seven; global WNS stays -270 ps. Every other
emitted setup margin and every emitted hold margin remain exact, including
positive margins. Both runs retain 17,199 rows (17,194 timed), zero hold
failures, +822 ps minimum hold, 105 unsupported records and `complete=false`.

Complete endpoint/domain, window/flag, metadata and clock comparisons pass.
The raw synthesis graph is identical. The complete logical/nonattribute graph,
ports, cell types, parameters and pin maps remain exact; one cell BEL attribute
and eight routing aliases change. Lossless patches reconstruct every complete
raw and routed graph. Independent LOW replay reproduces all 15 proof outputs
byte for byte, with 58 inventory/graph/recipe controls and 14 native timing
negative controls passing. Thirteen negative actual-preflight controls also
pass. Both saved actual RBFs reconstruct exactly in separate current-ABI,
zero-trial native legality/connectivity readbacks. This does not reproduce STA;
the internal reference-free final inventory remains guarded in-process but is
not separately emitted.

This seven-failure result is the new research reference. Remaining rows are
PIC -270 ps, CPU -25/-25 ps and display -26/-234/-265/-266 ps. The next timing
work targets these paths and the complete PIC source relationships before any
reset-source change. Original RTL, clocks, shared contracts and production locks
remain unchanged. Both PRs stay draft. Same-recipe seed, complete physical
coverage, fresh producer and exact-artifact hardware gates remain open.

Evidence is in `out/final-move-rediscovery-route-20261023/` and
`out/final-move-rediscovery-proposal-20261023/`. The two full routes add exactly
`5014.9869625419379` seconds. Accounting now contains 70 unique receipts and
`76241.10925592761447` cumulative seconds, preserving the complete prior
68-entry prefix with SHA256
`ddad46d0d729d52b98b594a1df0babef7bdb1b78dbc6aadb3288de4708530649`.
The new journal SHA256 is
`b84824339c422fcafee3657ab54e14bd244f098af1830a2ac95f7e3b291c4601`.
Evidence class: host-only fresh GPU diagnostics and native checks. No hardware ran.

### 23 October: reset/HOLD separation proofs and rejected resynthesis pair

A separate initialized register can drive video HOLD while the original reset
register drives execution/storage. Both sample the identical original decode on
the same system edge. The isolated default-off `nomerge` attribute prototype
preserves those FF identities without `keep` semantics. The reviewed change is
[Yosys draft #31](https://github.com/DeanoC/yosys/pull/31), commit
`230e9a58e988eb990a290ef2dc2897e8eb23b3d8`, based on `a75fe777`.
The pinned `3a5accbd` runtime passes 22 focused, four baseline, four threaded and
six integrated regression cases; the unpatched binary rejects a marked case.
GitHub Linux and macOS builds pass for the delivery commit. No toolchain lock
or production RTL changes accompany this diagnostic.

Original/candidate synthesis succeeds with frozen identical inputs. A reviewed
mapped compositional proof retains all 4,865 interface obligations and adds
3,076 state equalities as outputs, with zero assumptions. Its explicit initialized
base and first induction step pass; a wrong-init mutation produces an actual
counterexample. Native logic/FF semantics remain real; matched opaque primitive
composition requires identical semantics and initialization. Independent clock
transitions are preserved. This is digital equivalence, not CDC qualification.

Resynthesis removes the candidate's exact old NOR17 root identity. A separate
matched diagnostic therefore omits reduction balancing on **both** variants;
it is not the seven-failure recipe. Completed routes report:

| Variant | Endpoint rows | Setup failures | Worst setup | Hold failures / minimum |
| --- | ---: | ---: | ---: | ---: |
| Resynthesized original | 17,199 | 88 | -1,271 ps | 0 / +675 ps |
| Resynthesized reset/HOLD copy | 17,200 | 205 | -1,561 ps | 0 / +709 ps |

Both exit with timing failure and retain 105 unsupported records / `complete=false`.
The candidate is rejected. The complete literal union retains 15,758 common,
1,441 original-only and 1,442 candidate-only rows without inventing endpoint
correspondence. Independent replay reproduces all four comparison outputs
(gzip compared after decompression); full 50,276 raw and 85,964 routed graph
patches reconstruct the exact graphs. Both zero-trial native readbacks reconstruct
their whole saved RBFs exactly. These checks do not independently reproduce STA.

This pair adds exactly `4835.1470302860254` seconds. Accounting now has 72 unique
full-route receipts and `81076.25628621363987` cumulative seconds. The complete
70-entry prefix with SHA256
`b84824339c422fcafee3657ab54e14bd244f098af1830a2ac95f7e3b291c4601`
is preserved. The resulting journal SHA256 is
`ba1d82f309778e805019552131e4c0cd096d355151391b2c2e63c56ac6f2bb62`.
Proofs are in `out/reset-hold-nomerge-20261023/`; completed route evidence and
independent accounting review are in `out/reset-hold-route-20261023/`.

A smaller host-only prototype instead copies one native FF and inverter, adds
two nets and rewires four verified consumers of the frozen b4 graph. Every other
raw object and the old NOR17 root remain exact. Initialized native base/induction,
equal-substitution context checks, three semantic counterexamples and five
structural negative controls pass. A complete 1,944-branch / 5,527-terminal clock
census puts the sole pixel capture inside those four edges; 1,065 source-unknown
terminals remain explicitly unknown. Paired native packing succeeds and preserves
the copy and NOR17 transform. Packed constant-control evidence is source-derived,
since serialized JSON omits `pin_data`. Fresh routing of this separately reviewed
prototype is recorded below; packing alone conferred no timing or native-readback
acceptance.

The seven-failure / -270 ps b4 research reference remains unchanged. Both FES
and nextpnr PRs remain draft. Physical coverage, seed, producer and exact-artifact
hardware gates remain open. Evidence class: host diagnostics only; no hardware ran.


### Exact-b4 mapped reset copy: rejected (23 October)

The matched seed-1 pair retains the original NOR17 transform and latest b856
rediscovery recipe at 100 MHz / 74.25 MHz. The fresh baseline RBF, timing and
endpoint reports reproduce the retained seven-failure reference byte for byte.

| Variant | Endpoint rows | Setup failures | Worst setup | Hold failures / minimum |
| --- | ---: | ---: | ---: | ---: |
| Exact-b4 baseline | 17,199 | 7 | -270 ps | 0 / +822 ps |
| Minimal mapped reset copy | 17,200 | 216 | -1,461 ps | 0 / +747 ps |

Both runs exit with timing failure and retain 105 unsupported records /
`complete=false`. The copy is rejected. The complete literal endpoint union
retains all 17,199 common rows and one copy-only row. Independent replay
reproduces five comparison outputs (gzip compared after decompression), and
all eight raw / 92,652 routed graph differences reconstruct the exact graphs.

The baseline native whole-RBF readback passes. The copy readback and unchanged
source audit both reject the restored model at the native legality / FF-cache
check, before producing source hints. The failure is retained; no copy source
guard or native legality success is claimed. A separate printing-only helper
diagnostic reports 31 illegal BEL cells, all in LAB 33,15, and zero FF-cache
mismatches. A second diagnostic isolates the conservative LAB input-count check:
47 exceeds its limit of 42; all ALM, control-set and MLAB checks pass. Carry-chain
sharing metadata restoration remains under investigation. The original rejection
is retained; live-route legality versus incomplete restoration is unresolved.
The baseline source audit exhausts all 28 walks with zero enumeration issues,
but all 28 production guards remain false.

This pair adds exactly `3668.7970947101712` seconds. Accounting now has 74 unique
full-route receipts and `84745.05338092381107` cumulative seconds. The complete
72-entry prefix with SHA256
`ba1d82f309778e805019552131e4c0cd096d355151391b2c2e63c56ac6f2bb62`
is preserved. The resulting journal SHA256 is
`3951c26be9bb9c6641cbafd706639daa0cfb9b52db487ffb6094fcd57f6e7d8b`.
Evidence is retained in `out/mapped-reset-replication-route-20261023/` and
`out/mapped-reset-replication-source-audit-20261023/`. No production RTL, lock,
clock, module/shared contract or hardware transition changes. The seven-failure
reference and all seed, coverage, producer and hardware gates remain unchanged.


### Source-guard isolation confirmed by observed carry restoration (23 October)

The original readback failures remain retained. A separate host observer captures
actual carry metadata immediately after the unchanged native pack/check flow.
Both cases produce packed JSON byte-for-byte identical to their frozen pack
outputs, including all 1,349 arithmetic cells and 119 roots. JSON omits cluster
offsets; the frontend also omits the 119 unconnected root-CI ports serialized as
empty input connections. These losses explain the earlier reconstruction checks.

The reviewed host helper restores only the observed cluster metadata, after strict
typed parameter/attribute, connectivity, root/child and native geometry checks.
Its empty-root-CI rule requires both observed null net/driver/users and the actual
routed JSON input/empty-connection record. Connected and other missing ports
remain rejected. The original 42-input limit, native legality, cache, whole-RBF
and source-coverage guards are unchanged. No production compiler changes result.

The restored baseline reproduces its whole RBF and all 28 previous source-audit
results exactly. Eleven mutation controls reject at their reviewed gates. The
copy then reconstructs its whole RBF exactly and passes read-only graph, pin and
route checks. All 28 walks exhaust with zero enumeration issues on both cases.
The existing production source guard passes **0/28 baseline versus 28/28 copy**.
The full 1,920-hint identity union retains 28 changed records with no missing or
added identities; the copy removes the upstream reset-to-pixel null-window
relationship from these PIC source cones. Maximum diagnostic work is 2,535 versus
2,532; enumeration limits do not replace production guard checks.

This confirms the source-guard obstruction and its isolation, not whole timing
coverage, physical CDC qualification or adoption. The copy still has 216 setup
failures / -1,461 ps and remains rejected; the seven-failure reference is unchanged.
Evidence is in `out/mapped-reset-replication-source-audit-20261023/`, especially
`carry-pack-observer-v2/` and `carry-observed-restore-v4/`. All of these builds,
packs, readbacks and controls are host diagnostics; route accounting remains 74.


### Compiler research PR disposition (23 October)

At the user's request, nextpnr PR #266 is closed, unmerged. Its head
`b85677cfc9708b994b29fadd12debdd73171bfb4`, branch and all evidence are retained.
The mixed research bundle is not a focused review/merge unit and has no qualifying
timing recipe. Demonstrated diagnostic gains are not discarded, but any proposed
compiler delivery must extract an independently justified, focused change.
No compiler merge, lock change or timing acceptance follows from closure.


### Native observer feasibility and forward census (23 October)

A bounded read-only native observer reproduces the RBF and unchanged design,
and its two raw observations match. Its bidirectional logical closure reaches
10,620 nets / 10,437 cells and reports 5,143 issues, including 140 unmeasured
analogue arcs and 24 multiple-sink cases. It therefore establishes neither
complete electrical coverage nor stability evidence. Its 572 tree warnings are
unused arithmetic outputs with zero users, no public ports and empty routing;
they are not evidence of broken live routes. Full failures remain retained.

A separate forward logical census preserves every invalid hint and terminal,
uses native port/clock identities, and recomputes all reconvergent path unions.
The bounded baseline run passes in 8.8 seconds with exact whole-RBF, read-only
and original 28 source-audit results. Each frame has one invalid register root,
14 invalid hint records, 5,527 terminals and exactly one matching invalid capture
relationship, with zero census issues. Work is 49,232 below the 65,536 cap. The
complete connecting logical path spans two nets and ends at the video hold
synchronizer's DATAIN. Other terminals remain in the inventory.

This identifies a diagnostic observation target; it does not prove electrical
independence. All physical fanout and off-path model loads still need accounting.
No source guard, clock, production toolchain or timing acceptance changes.
Artifacts are in `out/mapped-reset-replication-source-audit-20261023/`, under
`native-invariant-observer-proposal-v2b/` and `forward-terminal-census-proposal/`.
These host runs add no full-route charge; accounting remains 74.


### Mistral line-load indexing correction (23 October)

The actual electrical trace exposed a reachable load-index mismatch in Mistral's
RC line generator. At one receiver-to-fixed-capacitor transition it selected
counter pair `(3, 1)` instead of `(4, 0)`. The loop classified the next position
after advancing, also reading past the final position. Separately, split-span
initialization repeatedly classified one position instead of each preceding
entry. The audit found no actual initial-prefix mismatch among 946 observed
signatures; the general prefix error is covered by the new regression.

[Mistral PR #19](https://github.com/DeanoC/mistral/pull/19), commit
`9ab6522077948fc3dd2e074338874f92e848fc44` on master base
`0a4ff598f4a64d331ad8351231178233d6633d95`, corrects both indices. It changes no
model tables, solver, public API or bitstream configuration. All nine Mistral
host tests pass. The new real-generator regression verifies 6,152 RC load maps,
including all receiver/fixed-cap sequences through six entries, every split,
both edges and loads on the split. AddressSanitizer reproduces the old final
position overread; the corrected generator passes all 6,152 cases.

A diagnostic linked with only the corrected traced `cv-rmux.cc.o` replacing
its previous archive member, keeping all 83 nextpnr dependencies unchanged,
replays the saved XT RBF and routes in 37.37 seconds. All 1,945 selected arcs
have three repeated native-delay measurements. 1,906 intervals change, all
increasing or equal component by component, with a maximum increase of 213 ps.
Whole-RBF reconstruction, read-only design, source guards, logical census and
phase inventories match the original. The original arc-0/pass-1 witness changes
from 16 counter mismatches to zero across all 326 logged line steps.

These are host-only measurements on two selected nets, not whole-design setup
and hold or physical acceptance. The three existing zero-circuit diagnostics
(one dedicated arc in each pass) remain reported; no unsupported clock or source
coverage is admitted. The initial comparison script incorrectly expected detail-
off circuit counts to equal detailed counts; its failure remains retained. A
separate offline comparison checks the existing per-pass count convention and
all native delay repeats without rerunning the native process.

Artifacts are in `out/mistral-line-index-fix-20261023/`. The PR is open for review;
there is no compiler merge, production lock/RTL change or hardware action. Route
accounting remains 74. The previously retained seven-failure / -270 ps route is
an old-model result: remeasure complete setup/hold with the corrected model
before using it for another timing decision, then resume same-recipe routing.


### Corrected-model full route and timing replay (23 October)

An old-model reload of the retained route reconstructed the RBF and all clock
constraints but failed the complete timing reproduction gate: 3 setup failures
instead of the saved 7. Its 152 changed endpoint entries are not an improvement.
The corrected-model comparison was not started on that basis. A read-only
observer reproduced that reload byte for byte and found all 92 differing
same-path routing segments used declined native overrides. The fresh reload
had no retained PIP observations and calibration disabled; the original router
had accumulated calibration during routing. Historical calibration values are
not present in the old capture and were not inferred from margins. Separately,
all 374 unsupported-contract identity differences are observed empty ports that
the JSON frontend omits. The failed reproduction and full differences remain
in `out/mistral-line-index-sta-20261023/`.

A fresh corrected-model route therefore uses the exact b4 mapped input, QSF,
SDC, seed 1, weight 2000, exponent 5 and retained rediscovery recipe. Clocks
remain 100/74.25 MHz. It uses frozen nextpnr source `b85677cf` and Mistral's
index correction `9ab6522` (review head `932c6d1` only extends tests). This is
a diagnostic based on retained compiler research, not adoption of closed #266.
The only additional nextpnr code is a local read-only capture after final
bitstream timing: calibrated-table enable/prior, every retained PIP observation,
all 256 type-calibration records and the native success/failure inventory.
Exact source, object, binary, input and execution identities are retained.

| Final routed metric | Retained old-model route | Fresh corrected-model route |
| --- | ---: | ---: |
| Endpoint rows / timed setup rows | 17,199 / 17,194 | 17,199 / 17,194 |
| Setup failures | 7 | 33 |
| Setup WNS | -270 ps | -482 ps |
| Related hold failures / minimum margin | 0 / +822 ps | 0 / +822 ps |
| Unsupported endpoints | 105 | 105 |

The corrected route is rejected for timing, not promoted. The complete union
has 17,199 common rows, no missing or new rows, and unchanged clock constraints
and unsupported identities. It has 27 CPU setup failures, five display failures
and one PIC failure. The worst CPU writeback path contains 6,701 ps of routing,
3,263 ps of logic, 731 ps clock-to-Q, -196 ps setup and -17 ps clock skew. Its
largest wire alone takes 2,417 ps between a carry-control LUT and arithmetic
input. CPU carry-control/writeback placement is the next focused target; these
figures do not establish a new recipe or justify reducing the clock.

The fresh route captures 138,405 retained PIP observations, calibration prior
1.25, 46,580 native arcs and 1,839 declined native arcs, with no missing cache
entries. A separate host diagnostic restores that exact captured calibration
and only the explicitly saved empty-port identities, then runs native final
analogue STA. It reconstructs the complete RBF and reproduces **both full
report files byte for byte**, including all endpoint identities, setup/hold
margins, clocks, contracts, critical paths and detailed timing. The fresh native
success/failure inventory also matches exactly. This fixes the reproduction
boundary for this diagnostic; it is not electrical completeness or hardware
acceptance, and no general compiler checkpoint change is shipped here.

The leased route finished with timing-failure exit 1 in 824.1652118018828 seconds;
all frozen inputs and dependencies pass postflight identity checks. Its host
readback took 12.01 seconds and adds no route charge. Accounting appends exactly
one receipt, retaining the original 74-row prefix and verifying every stored
receipt and exact Decimal charge: **75 completed route receipts**, cumulative
**85,569.21859272569387 seconds**, including the preserved pre-journal charge.
The user's unlimited route-budget authorization remains in force. No hardware,
production lock, RTL, shared contract, clock constraint or timing waiver changed.
Artifacts and the separate complete-union/report/receipt checks are in
`out/mistral-line-index-route-20261023/`.


## Corrected-model checkpoint searches and architectural next step

The extra final-placement pass uses the exact corrected checkpoint, original
clocks and captured calibration. Before admission it reproduces the complete
RBF and both full timing reports byte for byte. Fresh discovery finds 24 eligible
cells and seven candidate moves; all seven reject under the unchanged native
clock/endpoint-margin or 20 ps gain guards. No move is retained. The output RBF,
full timing report and complete endpoint report remain byte-identical, with all
17,199 rows, 33 setup failures / -482 ps, zero hold failures / +822 ps and 105
unsupported boundaries. Complete calibration values and native success/failure
identity sets also match. This is a derived-checkpoint search, not another
same-recipe full route or seed qualification.

The worst CPU control LUT is excluded by the ordinary move footprint: its
output feeds an arithmetic B input, and an input net has a fixed BUF.A user.
Read-only native checks show its complete upstream launch coverage passes in
both skew frames. The downstream check passes only through the existing explicit
proof of unobserved arithmetic SO/CO leaves; all directly selected-net sinks
have successful native analogue arcs. Ordinary caller admission remains unchanged.

An OUT-only selected-net reroute diagnostic keeps all cells, parameters, bindings
and pin maps fixed. It uses those native source/downstream proofs, the existing
route transaction and complete both-frame clock/endpoint guards. Two distinct
native measurements give -149 ps and +16 ps worst-sink gain. Neither reaches the
unchanged 20 ps admission threshold; no route is retained. Exact full RBF/report
rollback passes, and every cell connection matches by its complete net/alias
identity (serialized integer bit numbers are local to each JSON export).

The existing unmodified private LUT-to-arithmetic copy pass is also screened
without routing or applying a copy. A paired fresh-placement report/listing
checks 20 estimated sites and qualifies none; both placement JSON and timing
report bytes remain exact. A separate host diagnostic first reconstructs the
actual corrected checkpoint exactly, then removes only routes in memory while
keeping its placement and original unfiltered timing report. This discovers the
actual worst control edge, but qualifies no placement site. The listing restores
its complete unrouted graph, pins and route stores byte for byte. Initial CLI
rejection and a diagnostic-helper compile error are retained separately; neither
ran a route. No compiler admission or implementation is changed by these screens.

The two charged searches take 103.66652875812724 and 39.32369616813958 seconds.
Accounting now contains **77 route/search receipts**, cumulative
**85,712.20881765196069 seconds**, preserving each prior prefix and verifying all
77 receipt objects, file hashes and exact Decimal charges. Host-only placement,
readback and listing checks add no route charge. Artifacts are under
`out/corrected-checkpoint-eight-trials-20261023/`,
`out/corrected-cpu-control-audit-20261023/`,
`out/corrected-cpu-control-reroute-20261023/`,
`out/corrected-cpu-driver-copy-listing-20261023/` and
`out/corrected-checkpoint-copy-listing-20261023/`.

Mistral #19 is merged, including the model correction and review tests. The
current CPU path still combines operand selection, addition and writeback in
one cycle; one-hot controls, ADD source specialization and dedicated carry
mapping already exist, and the prior forced ADD0 map regressed matched routes.
The next architectural proposal is a 100 MHz two-phase EU with registered
operands before ALU/writeback. This needs coordinated sequencer, ROM lead,
jump-flush, flag and BIU handling plus a measured guest-cycle throughput budget.
It is a proposal, not an RTL change or accepted timing result. Display/PIC and
physical endpoint qualification remain separate work. No production lock,
shared contract, clock, timing waiver or hardware state changes here.
