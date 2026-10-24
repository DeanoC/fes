# Whole-board display and boot timing candidates, 2026-10-23

Owner: misteross. Base: `547f0c20a2a66dfdf87d551399c9b9e2524b146e`.
No shared definitions, toolchain lock, clock frequency, pad contract or kit change.
Evidence is host simulation and diagnostic routing, not hardware acceptance.

## Display-only route: rejected for timing

Frozen source `0f84601bdf0b6dbb70c4397c9115bdba00972bd0`, unchanged
corrected diagnostic compiler and seed-1 recipe from the CPU checkpoint.
System 100 MHz, SDRAM capture +180 degrees, pixel 74.25 MHz.

| Complete final routed inventory | CPU checkpoint | Display pipeline |
| --- | ---: | ---: |
| Rows | 17709 | 17742 |
| Setup failures | 72 | 229 |
| Worst setup, ps | -1575 | -2658 |
| Hold failures | 0 | 0 |
| Minimum hold, ps | 789 | 778 |
| Unsupported records | 105 | 105 |

The CPU still passes all emitted CPU rows, but its setup margin falls from
934 to 27 ps. PIC has 7 failures, display 90, and other logic 132. The worst
capture is DMA transfer-address enable. Thus the structural renderer pipeline
and prepared VRAM writes are not a qualified timing improvement. Keep the
passing CPU checkpoint as the comparison baseline; do not promote this profile.

The collector completed with native timing-miss return code 1 and all input
bindings intact. Complete setup/hold endpoint union, both unsupported inventories,
clock equality and all output artifact hashes are retained under
`out/dev/pcxt-board/out/pcxt-whole-board-closure-20261023/`.

## Boot-ROM banking: routed, whole-board closure still open

Frozen source `1860f14260e150937b0fdb372f8d59b250b14bf1` adds eight
bank-local read-address registers. The same generated firmware is emitted as
eight contiguous 8 KiB files; reconstructing them is byte-for-byte equal to the
previous 64 KiB image (SHA-256
`afe432903f7911042397c6d645471e32bdece8e4c06fb38193dc0e870d27b85b`).
Mapped actual board synthesis retains 104 distinct system-clocked local address
FFs and reduces boot M10Ks from 64 to 14, board M10Ks from 318 to 268.
Board FFs are 3952. This is a resource/structure result, not routed timing.

All 35 focused tests pass with the pinned Yosys, and all 16 PC/XT simulation
suites pass. An independent old/new loader comparison covers 132529 accepted
bytes, two complete images and a one-cycle mid-load reset. `make check` passes
after committing module bytes. The intermediate parent/component `make test-changed` run is incomplete;
its retained partial results do not constitute a completed check.

The frozen combined boot/display route completes with all input bindings intact:
17128 rows, 17123 setup/hold-timed rows, 85 setup failures, worst setup -1467 ps,
zero hold failures, minimum hold +738 ps, 105 unsupported records and
`complete=false`. All surviving clocks are exactly equal to the CPU baseline.
All 345 emitted boot-loader rows pass (minimum setup +556 ps, hold +1236 ps).
CPU has zero setup failures / +512 ps minimum; PIC has 7 / -1439 ps,
display 35 / -1111 ps, other logic 43 / -1467 ps. The worst captures are DMA
terminal-count and transfer-address enables. The result improves the rejected
display route but does not close the board; the baseline's 72 failing rows
remain fewer than this candidate's 85. Complete endpoint union and unsupported
details are retained in `boot-route-comparison.json` and its companion files.

All 842 selected parent tests pass. The intermediate component run was
intentionally stopped when the next focused fixes were ready, retaining its
partial log and interruption record. It is not a completed `make test-changed`
pass. The earlier direct 16-suite simulation has passed. A DMA grant-snapshot prototype proves all 457 state/qualified-
transaction obligations with an active-service snapshot invariant. Both
address and terminal-count mutations reject. Standalone mapping keeps 357 FFs,
adds two LUTs (679 to 681), and reduces terminal-enable cell depth from seven
to two. This is a structural candidate, not routed timing acceptance.
 No winning
profile, two-of-three seed gate, fresh producer, physical IO/reset qualification
or kit acceptance has been obtained. The 105 unsupported records remain open.
A separate private DMA decode-order prototype passes all 453 state/interface
equivalence points; it has not been adopted into RTL or routed.

## DMA grant snapshots and exact dot-enable prediction: source frozen

Commit `b8f144d8` moves the address/terminal snapshot before service validation
at the same grant edge. Inactive TC may differ; the actual motherboard latches
TC only with a valid DMA request. All other observable outputs/state and active
snapshots retain the proved relation. No new DMA cycle is added.

The fractional-dot predictor computes the next phase's wrap flag in a FF,
removing the wide compare from CRTC enables. Reset establishes the phase range
and flag identity; one-step induction proves that both CGA and MDA keep their
exact original pulse cycles. The arithmetic proof still compares against the
original full-width sum/carry definition for every phase under that flag
relation. Threshold mutations reject for both rates.

All 40 focused tests pass with pinned Yosys, including address/TC mutation
rejection. The DMA and complete-frame display simulations pass. All 16 simulation suites and `make check` pass. Frozen board synthesis has
3954 FFs and 268 M10Ks, both dot-enable FFs on the system clock and all 104
independent boot-address FFs. Actual DMA payload enables have only state,
grant, guest pulse and the registered reset as launch sources.

The complete final route has 17132 rows (17127 setup/hold-timed), **one setup
failure / -305 ps**, zero hold failures / +728 ps minimum, 105 unsupported
records and `complete=false`. CPU, PIC and display all pass their emitted
rows, with minimum setup +133, +689 and +19 ps respectively. The remaining
capture is `system.machine.board.state_MISTRAL_FF_Q_3/DATAIN`: DMA state feeds
the RAM address decode through a wide carry chain before state dispatch.
All clock contracts are unchanged and all collector bindings pass.
The full three-profile endpoint union and paired reports are retained in
`grant-dot-route-comparison.json` and its companion artifacts. This is still
a timing miss, not whole-board closure or physical IO/reset/kit acceptance.
Required broad integration checks will be completed after the candidate is
stable; no fresh parent/component pass is claimed for this commit yet.

## Explicit aligned memory regions: seed qualification in progress

Frozen commit `e24b88ed` decodes RAM with A19:16 below `A` and firmware at
`F`, in both the motherboard router and defensive storage backend. This is
symbolically identical to the old full-width bounds for every address, kind
and write direction. The ISA-hole admission mutation rejects. All 42 focused
tests, motherboard/memory simulations, the full 16-suite simulation and
`make check` pass. Mapped arithmetic cells fall from 1418 to 1376; no
arithmetic cell is attributed to the former RAM comparison. FFs remain 3954,
M10Ks 268, and all CPU, dot-predictor and local-ROM boundaries pass.

Seed 1 completes with all collector bindings intact: 17132 rows, 17127 timed,
6 setup failures / -274 ps, zero hold failures / +776 ps, 105 unsupported
records and `complete=false`. The motherboard dispatch miss is gone. CPU,
PIC and other logic all pass (minimum setup +335 / +263 / +253 ps); the six
remaining misses are CGA VRAM address captures. This is a targeted path repair,
not a qualified whole-board timing winner. The source, compiler, full repair
recipe and 100/74.25 MHz clock contract are held fixed for seeds 1/2/3.
Seeds 2 and 3 complete with zero reported setup or hold failures. Seed 2 has
minimum setup/hold +168/+788 ps; seed 3 +49/+773 ps. Each has the same 17132
rows, 17127 timed rows and 105 unsupported identities. The strict audit verifies
all artifact hashes, final analogue/cache/calibration state, exact source,
compiler, environment, clock contracts and commands except the seed. The full
17132-row three-seed union is retained. The **reported fabric setup/hold
2-of-3 gate passes**; physical whole-board qualification remains false.

The selected replay seed is 2. A fresh synthesis regenerates byte-identical
ROM banks, microcode and mapped JSON from the frozen committed module bytes.
The fresh diagnostic seed-2 route then reproduces all five artifacts byte for
byte: `diagnostic.rbf`, `routed.json`, `timing.json`, `endpoints.json` and
`timing-state.json`. Its native return code is zero and all collector bindings
pass. `region-fresh-byte-comparison.json` retains the five exact hashes.
This is a fresh diagnostic replay, not authenticated production admission. The compiler
remains the diagnostic b85677cf branch binary, not a production pin adoption.
A live comparison with reviewed nextpnr main shows divergence (112 commits on
the diagnostic side, 11 on main). These results do not authorize adopting that
research branch or reopening closed #266. Authenticated production-toolchain
admission, external IO/reset modeling and exact-artifact hardware acceptance
remain separate requirements.

A separate private CRTC address-register prototype passes all 252 state and
interface equivalence points against the combinational row-plus-column MA.
It maintains MA at the same CRTC edge without adding a memory-read cycle.
The horizontal-wrap mutation rejects, including live horizontal-total changes.
It is not adopted into RTL and has no routed timing result.

## Stable-source broad validation and reviewed-main comparison

The complete stable-source `make test-changed --base 547f0c20` passes:
842 parent tests, 1283 component tests (one optional host oracle skipped),
generated-consumer consistency and all 16 PC/XT RTL simulation suites. Its log
is `stable-region-test-changed.log`. The earlier intentionally stopped run
remains separate and is not counted as a pass.

An unmodified checkout of reviewed nextpnr main
`b9ffde2d360ef3fc56c67074bfed2ddfe7933535` builds with merged Mistral
`9ab6522077948fc3dd2e074338874f92e848fc44`. Its standard producer profile
uses the same frozen RTL, clocks, HeAP weight2000/critexp5 and GPU router,
without the research branch's hard-block, LUT-swap or final-move options.
All three final reports preserve every one of the 17127 reference timed
endpoint identities; setup and hold are both checked and clock contracts are
exact. Reference physical IO/reset obligations remain unqualified.

| Reviewed-main standard profile | Setup failures | Minimum setup ps | Hold failures | Minimum hold ps |
| --- | ---: | ---: | ---: | ---: |
| Seed 1 | 0 | 86 | 0 | 703 |
| Seed 2 | 3 | -189 | 0 | 789 |
| Seed 3 | 9 | -756 | 0 | 785 |

Thus this standard-profile matrix **fails 2-of-3 qualification** despite seed 1
passing (system101.52MHz, pixel74.73MHz). The actual core lock stays unchanged.
The failures are DMA/motherboard data/state and one pixel bank register, not
the CRTC VRAM paths. The private CRTC prototype is therefore not adopted.
A separate reviewed-main budget32 critical-cohort profile is being tested;
it is an existing default-off compiler feature, with no timing waiver or
research-branch code imported. A temporary lock proposal provisions the
reviewed pair through the official immutable cache builder, without changing
the selected core lock. Compiler tests remain pending; the interrupted initial
run with missing common test binaries is retained separately from the full
rerun after all targets were built.

## Route accounting

Receipts 89-91 add exactly 99.68203672394156, 174.6483835540712, 177.7076671998948
charged seconds. All 91 unique receipt objects, hashes and Decimal charges
verify, preserving the original 88-row journal prefix. Cumulative charge
including the pre-journal amount is 95040.37256684945837 seconds.
User authorization remains unlimited route budget. Journal SHA-256:
`d8c48302660404b4ba490b2353805b1318aa97e36e90858a9cec31f8fbdff478`.

### Reviewed cohort placement trial

Reviewed nextpnr main with its existing cohort budget 32 also failed on the frozen region-decode source, seed 2: nine setup violations, -458 ps worst setup, no hold failures and +784 ps minimum hold. Full 17127 timed identities and unchanged clock contracts were audited. This profile is rejected; it is not combined with other profiles to claim a seed gate. Receipt 92 retains the failed run and its 185.2980316360481-second charge, bringing the exact cumulative charge to 95225.67059848550647 seconds.

The reviewed-main monolithic host test was stopped after growing to approximately 45 GiB resident plus 37 GiB swapped; that full-suite run is not passing evidence. Separate investigation is tracked by DeanoC/nextpnr#288.

### Configuration predecode and local read staging

Commit 46cd75db adds twelve per-channel DMA mode-decode configuration bits. All 481 state/interface equivalence points pass against the original live mode-byte decode; wrong reset and programmed-channel mutations reject. The seven focused DMA proof tests and native DMA suite pass. This is a functional/proof result, not a new routed timing pass.

The motherboard read pipeline samples local peripherals at the original phase-8 edge into an independent byte register and transfers that byte into the shared return register at phase 17. It preserves the existing eight active and nine release clocks. An original-behavior ghost return register and the local-phase bound are proved together by temporal induction over the actual motherboard/peripheral RTL. The phase bound is proved, not assumed. The native motherboard suite passes the same 378 transactions in 17245445 clocks. CPU and DMA consume the payload on completion; its inactive intermediate value is not a public read sample.

Initial whole-module equivalence attempts retained 2900 proved points but could not prove the return-data invariant after cutting state/phase relationships; they are retained as failed proof attempts. The single-design temporal proof retains those relationships and passes. Negative tests change the peripheral sample edge or the completed byte and require an actual failed proof, not a parser error. Initial negative-test assertions used a different Yosys diagnostic spelling; the proof rejection was present, and the assertion was corrected. No FPGA or kit ran.

### Reviewed-main fabric gate and authenticated replay

Frozen RTL checkpoint `e799cd7a97f6a536160ea3ba275df68628565a75` passes the fixed three-seed **reported fabric** gate with reviewed nextpnr `b9ffde2d360ef3fc56c67074bfed2ddfe7933535` and Mistral `9ab6522077948fc3dd2e074338874f92e848fc44`. Only the seed changes; source, compiler, environment, input bindings and native recipe are identical. System/capture targets remain 100 MHz and pixel 74.25 MHz. All three runs have the exact same complete 17187 timed identities, every reported row has paired setup/hold checks, and all 20 new FFs have DATAIN/ENA/SCLR coverage.

| Seed | Setup failures | Worst setup | Hold failures | Minimum hold |
| --- | ---: | ---: | ---: | ---: |
| 1 | 2 | -297 ps | 0 | +771 ps |
| 2 | 0 | +22 ps | 0 | +767 ps |
| 3 | 0 | +55 ps | 0 | +764 ps |

The DMA-only seed-3 comparison has four display setup failures, worst -465 ps, and no hold failures (+714 ps minimum). It is retained as a failed intermediate route, not mixed into the accepted recipe. Cross-source private endpoint renames are retained in full unions; the three-seed gate compares the entire identical new identity set, not an intersection.

The official published cache, selected using an explicit temporary candidate lock, reproduces seed 2's RBF, timing and model reports byte for byte. Its routed JSON differs only in `/creator`: the two builds encode the same reviewed source with different version labels. The RBF SHA256 is `d004b81c8cfbe5714ae76843885acddc7ab50b04bbab1404b5849f513126206b`. This is an authenticated replay of the frozen synthesized input, **not** a fresh producer build.

Mapped structure has 3974 FFs and 268 M10Ks. Immutable source-span witnesses identify the twelve DMA predecode FFs and eight read-snapshot FFs even when Yosys removes or inverts their aliases; all are system-clocked. The shared return DATAIN cones exclude direct local-peripheral launches. All 104 independent boot-address FFs, fourteen boot M10Ks, registered dot enables, aligned region decode and shallow DMA snapshot enables retain their checked structure.

The stable checkpoint passes `make check`, all 842 parent tests, 1289 component tests (one optional oracle skipped), all 16 PC/XT simulation suites and generated-consumer consistency. The new native motherboard check retains 378 transactions in 17245445 clocks. These are host checks and routed fabric evidence; no kit ran.

All 105 prior unqualified physical-boundary roles remain accounted for in the new route: 28 HDMI, 21 registered SDRAM/clock, two byte-mask, 32 bidirectional data/OE, sixteen SDRAM capture, four reset-clear and two HPS I2C endpoints. The four reset roles are rebound by immutable RTL source spans, not guessed private aliases. The five reference untimed rows are also retained. Physical whole-board timing, external board/chip budgets, reset/CDC qualification, exact-artifact Quartus checks and hardware acceptance are still open. The full compiler host suite is not claimed as passing evidence.

The diagnostic producer now selects reviewed main and the merged Mistral timing-line correction through its private core lock; other core/global pins are unchanged. It defaults to seed 3, exposes unsigned-64-bit `--seed` for reproduction, and records every generated `.mem`, `.hex` and facts `.vh` file. A fresh ordinary producer run is the next check. No factory/package/runtime contract or hardware acceptance is added.

Receipts 93-97 retain all five new routes, including the failed DMA-only and seed-1 runs. The original 92-receipt prefix is exact; all 97 receipt objects, hashes and Decimal charges verify. Exact cumulative charge: 96582.15624318644347 seconds.

### Fresh ordinary producer and final producer checks

The ordinary producer from clean committed `bc588c5ef93f64876c5cc9071426077907a1d611` completes with its actual private lock, authenticated published tools and default seed 3. It uses no temporary lock or command override. All fourteen generated ROM/facts files and all source/tool bindings verify before capture; the source working tree is clean. Its RBF is byte-identical to the frozen passing seed-3 artifact: SHA256 `dee3a8db392c9a3a22502f6e975134c61a256a95d7bd99e923d3ef8ccaf03ddd`.

The complete 17187-row native inventory has zero setup and hold failures, +55 ps worst setup and +764 ps minimum hold. All twenty added configuration/snapshot FFs have paired DATAIN/ENA/SCLR coverage. The native report uses the producer's normal 64-path diagnostic limit; the complete detailed endpoint inventory remains present. The full synthesized JSON matches the frozen design under explicit source/toolchain/generated-root prefix renaming only; no graph fields or parameters are omitted. A first comparison that removed only source attributes still found path-bearing generated identifiers, so the exact prefix bijection is recorded separately.

The producer/pin delta passes `make check`, its four focused producer tests, 842 parent tests, 1290 component tests (one optional oracle skipped), all sixteen XT simulation suites and generated-consumer consistency. These checks use the authenticated selected Yosys. This remains routed/host diagnostic evidence, not physical whole-board or hardware acceptance. No kit was claimed or touched and no shared/runtime/factory contract changed.

### Rejected further IO staging experiment

An isolated immutable proposal `ac8bdeeaf58585c6e86556c9e17a033be1abae5f` captures all four peripheral read bytes independently at phase 8 and fully predecodes display byte lanes and CRTC write kinds during endpoint preparation. It preserves phase-17 completion and the original ISA commit cycles. It passes eight focused proof tests (including negative mutations), native motherboard transactions and complete CGA/MDA frame checks. Mapped size is 4002 FFs and 268 M10Ks; all 52 pipeline/configuration FFs have source-owned clock and paired endpoint coverage.

Despite those structural/proof results, its unchanged-clock ordinary recipe fails both tested seeds: seed 2 has nine setup failures / -778 ps worst setup, seed 3 thirteen / -582 ps; neither has hold failures (minimum +765/+701 ps). Both complete native inventories contain 17263 paired timed rows. With two failures the two-of-three gate cannot pass, so seed 1 is not run. The proposal is rejected and not adopted or pushed; the passing ordinary producer remains unchanged. Failed captures and the original source archive are retained.

Receipts 98-100 cover the ordinary producer and both failed proposal routes. The original 97-receipt prefix is exact; every one of the 100 receipt objects, hashes and Decimal charges verifies. Exact cumulative charge is 97306.21721360552997 seconds.

### Remaining physical board inputs

The user's targets are Kit A (MiSTer Pi) and Kit B (SuperStation One), using standard MiSTer SDRAM interfaces. [Retro Remake's SuperStation specifications](https://retroremake.co/pages/superstation%E1%B5%92%E2%81%BF%E1%B5%89) advertise 128 MB SDRAM but do not supply the fitted chip timing grade or routed flight/load bounds. The [MiSTer SDRAM assembly reference](https://github.com/MiSTer-devel/Main_MiSTer/wiki/SDRAM-Board-Assembly-%28DIY%29) covers multiple board/chip variants and warns that its BOM may be stale. A common bus interface does not establish interchangeable physical budgets. No chip timing or trace delay is inferred from those descriptions.

RAM Tester's existing budget helper is specifically the EM63B165 -6 model, and its example board intervals are explicitly unmeasured. It also requires an actual final analogue forwarded-clock report with both edges. The next bounded diagnostic is a clock-pad probe using the existing characterized 30 pF reference envelope only; zero output delays instrument arrival extraction and are not asserted board requirements. No board flights, chip timings or acceptance margins are invented. Full physical IO/reset/CDC, clock uncertainty, whole-design exact-artifact Quartus, retention and kit acceptance remain open.
