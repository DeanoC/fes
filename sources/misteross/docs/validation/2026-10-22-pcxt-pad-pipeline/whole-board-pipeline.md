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

### Clock-pad probe and DQ packing compatibility

The final reference clock-pad probe uses the unchanged ordinary producer input and an isolated QSF/SDC overlay. Only SDRAM_CLK declares the characterized 30 pF reference profile. Zero external delays instrument clock arrival extraction; they are not board requirements. Both forwarded edges have complete final setup/hold records. The rising pad transfer is launched by the falling system-clock edge and has a 1.244..6.810 ns arrival interval; the falling transfer is launched by the rising edge and has a 1.244..6.830 ns interval. Independently combining these bounds gives high/low pulse lower bounds of -0.566/-0.586 ns. This model cannot prove a positive pulse width; it does not measure a negative hardware waveform. The diagnostic route fails timing and is retained without replacing the passing baseline.

The existing RAM Tester corner audit already distinguishes conservative independent envelopes from correlated fitted transfers. Its surrogate clock routes and assumed board flights cannot supply correlation credit for this XT route. The native registered-pad model uses zero conservative early bounds rather than promoting observed fitted minima into guaranteed silicon limits. No phase shift, narrowed envelope or timing waiver is adopted.

A separate host-only pack probe adds fast input/data/output-enable register hints to the sixteen SDRAM DQ pads. All sixteen become MISTRAL_SDRIO with IOREG_IN/OUT/OE enabled. Input capture remains on clocks[1]; data and OE remain on clocks[0]. All sixteen original fabric input-capture registers are absorbed. This establishes compatibility of the existing RTL with IO packing; it supplies no routed timing or hardware result. The probe does not enable a pad profile, does not change the producer constraints and is not an accepted variant. Dynamic DQM masks, board/chip bounds, bus turnaround and capture-to-controller timing still require their own checks.

Receipt 101 records the clock-pad route and its 185.74247418902814-second charge. The original 100-receipt prefix is preserved; cumulative charge is 97491.95968779455811 seconds. Journal SHA256 is `18404c836a33f41ba8bcad3f071bf8e4ac6f75d977ba34be0383ec8fbbc5614c`. The passing functional checkpoint and normal producer remain unchanged. No kit was claimed or touched.

### Local Quartus SDRAM boundary comparison

The build host is Powerboat. Quartus Prime Lite 17.0.2 Build 602 is installed at `/home/deano/intelFPGA_lite/17.0/quartus`; local TimeQuest runs successfully. The earlier SSH-to-self authentication failure was incorrectly treated as a remote tooling blocker and supplies no evidence that Quartus is unavailable.

An isolated whole-RTL comparison first requires three loop-variable declarations to be hoisted for the Quartus 17 parser, the vendor-supported FALSE spelling for disabled bidirectional bus hold, and a behavioral tri-state equivalent of MISTRAL_IO for HPS I2C. These adaptations remain in ignored comparison copies. The frozen source and passing ordinary producer are unchanged. Quartus synthesis then expands the two display VRAMs into 163840 storage registers; the whole design reports 167512 registers. The owned fit is stopped and all attempts retained. This unsuitable mapping is not a timing result or a failure of the OSS block-memory implementation.

A narrower comparison instantiates the exact frozen `pcxt_memory` and `sdram_addon_port` RTL, copies the board's memory PLL and DQ wrapper, and makes the host request interface virtual. It retains the original 100 MHz system clock and 180-degree capture clock. Quartus fits 237 registers and one PLL. Source and generated-input hashes are checked; the only postfit QSF edit allowed by that audit is Quartus's exact LAST_QUARTUS_VERSION metadata suffix. No external input/output requirements or false paths are asserted. PLL uncertainty is derived. Both the initial unspecified-load fit and a separate explicit 30 pF reference-load fit complete; the load is a declared characterization condition, not measured kit loading.

The 30 pF fit has all sixteen capture sources checked for both setup and hold at every corner. The capture handoff has a 5 ns launch/capture relationship. Forwarded-clock reports verify the physical rising pad edge is launched by the falling system-clock edge and vice versa. Synthetic zero delays expose arrivals only and fail their arbitrary output requirement; those failures are not converted into board acceptance. Same-corner pulse arithmetic removes the ideal launch edge, retains the fitted clock prefix, and is invariant under a common prefix translation.

| Quartus reference corner | Capture handoff setup | Capture handoff hold | Fitted high lower bound | Fitted low lower bound |
| --- | ---: | ---: | ---: | ---: |
| Slow 100 C | +1.673 ns | +4.340 ns | 2.287 ns | 2.889 ns |
| Slow -40 C | +1.838 ns | +4.419 ns | 2.613 ns | 2.947 ns |
| Fast 100 C | +3.020 ns | +4.691 ns | 3.677 ns | 4.267 ns |
| Fast -40 C | +3.339 ns | +4.703 ns | 3.835 ns | 4.210 ns |

These values describe different Quartus-fitted wiring. They do not grant correlation credit to the OSS route, establish a chip pulse-width requirement, or qualify a native model. The complete unconstrained-path reports are retained: neither the virtual host interface nor external SDRAM setup/hold has an asserted physical budget. The initial unspecified-load fit also passes all sixteen handoffs, but it is not a matched 30 pF comparison. No capture phase, latency, clock rate, producer constraint or timing bound changes follow from either fit.

Evidence is retained under `out/pcxt-whole-board-closure-20261023/quartus-whole-rtl-oracle`, `quartus-sdram-oracle` and `quartus-sdram-reference30`, with per-corner reports, input receipts and machine-readable summaries. These are CPU-only comparison fits, with no new GPU route receipt; the 101-entry route journal is unchanged. No hardware ran. The next physical step remains a qualified correlated model for the actual OSS clock/pad paths and verified chip/board requirements, followed by external-channel and exact-artifact checks.

### Actual OSS clock-route comparison

The reference fit above is not an exact clock-resource match. Its generic Quartus wrapper chooses the top PLL with counters C6/C5 and G4/G7, and needs an explicit `PLL_COMPENSATION_MODE DIRECT` assignment to match the requested direct mode. The actual OSS source uses FPLL `(0,14)`, C6/C7 and G2/G3. Those reference differences are retained and no timing credit is transferred from the earlier fit.

A topology audit now decodes the actual ordinary producer RBF and checks its physical clock bindings against its own routed JSON. All 4107 system-clock and 48 capture-clock physical pip bindings match. Mistral's existing active-route export omits default-selected DCMUX/TCLK and SCLK links, hiding two used edges in the SDRAM clock branch. An isolated diagnostic decoder retains raw selected links; each branch must also be present in the actual source bindings, because a default selection alone does not prove use. Missing-binding and wrong-branch mutations reject. All decoder timing fields are -1 placeholders; this audit measures topology only. No compiler, library pin, producer or timing bound is changed.

Two forced-location attempts use the wrong top-PLL counter coordinates and fail placement; they are retained without inferring illegal native clock connectivity. The checked-in PLL quad Quartus oracle establishes native `(0,14)` C6/C7 at physical counter Y20/Y21. Correcting those locations and the explicit DIRECT assignment produces a successful fit on G2/G3. Its local pad branch still differs, so a further isolated RCF changes only that branch's row-clock selection. The resulting Quartus RBF decodes to the exact same nine-edge GCLK-to-SDRAM_CLK branch as the OSS RBF. Input mux selections for G2/G3 also agree. Other clock branches and fabric placement remain the reference fit.

All sixteen capture-to-controller sources retain paired setup/hold checks at every corner. The final exact-branch comparison reports:

| Corner | Handoff setup | Handoff hold | Fitted high lower bound | Fitted low lower bound |
| --- | ---: | ---: | ---: | ---: |
| Slow 100 C | +1.595 ns | +4.402 ns | 3.191 ns | 3.774 ns |
| Slow -40 C | +1.757 ns | +4.478 ns | 3.513 ns | 3.849 ns |
| Fast 100 C | +2.997 ns | +4.677 ns | 4.035 ns | 4.633 ns |
| Fast -40 C | +3.321 ns | +4.683 ns | 4.196 ns | 4.572 ns |

The machine-readable [clock-route comparison](clock-route-comparison.json) binds the source and comparison RBF hashes, all ten nodes and nine edges, decoder identity, input bindings and qualification limits. Raw captures are under `out/pcxt-whole-board-closure-20261023/quartus-sdram-clock-match` and `quartus-sdram-exact-clock-branch`; failed attempts and prior reference fits remain separate. The ordinary source RBF stays `dee3a8db392c9a3a22502f6e975134c61a256a95d7bd99e923d3ef8ccaf03ddd`.

This is a matched forwarded-clock-branch reference, not a whole-artifact Quartus reconstruction. The 30 pF condition is declared, not measured. Common clock side branches, complete electrical/corner correlation, chip/board setup/hold and pulse-width requirements, DQM, bus turnaround, consumption-cycle identity, reset/CDC and whole-design uncertainty still require qualification. No positive pulse or handoff number overrides those missing checks. GPU route receipts remain at 101; these are CPU-only oracle/decoding runs. No hardware ran.

### Explicit setup/hold uncertainty screen

The local Quartus reference now includes both source PLL configurations, at their native physical sites/counters/global roots, with the exact SDRAM forwarded-clock branch retained. A preserved eight-bit pixel counter supplies same-clock pixel paths for uncertainty extraction; it is not the actual pixel fabric. Native video FPLL `(0,31)` C6 maps to Quartus physical counter Y33, as the checked-in two-PLL fixture establishes. An initial Y37 mapping attempt fails and is retained without inferring illegal native clock connectivity.

`scripts/screen_fes_pcxt_uncertainty.py` checks the producer's embedded report and RBF hash against the branch reference, verifies retained reference summaries, extracts explicit setup/hold uncertainty increments for each clock edge pair at all four corners, and screens the complete native inventory. It rejects changed evidence, incomplete corners, missing or ambiguous increments, negative/nonfinite budgets, duplicate endpoints and relaxed clock targets. No native model, source clock, SDC or producer behavior changes. Nine focused host tests pass, including setup/hold failures and substitution/coverage rejection cases.

The [machine-readable uncertainty screen](uncertainty-screen.json) retains all 17196 endpoint rows: 17187 paired rows have both reference budgets, and nine unpaired rows remain explicit. Per-pair worst reference setup/hold budgets are 100/0 ps for system-to-system, 90/70 ps for capture-to-system, and 200/0 ps for pixel-to-pixel. Zero is an explicitly reported TimeQuest hold increment, not an assumed missing value. These remain reference conditions; they are not measured oscillator/board bounds or a complete physical model.

The source's reported fabric gate is unchanged, but its setup headroom fails this uncertainty screen: two system-clock enables have adjusted margins -45 ps (CRTC register enable) and -2 ps (VRAM enable). All screened hold margins stay positive. No failures or unpaired rows are waived, and the tool returns nonzero. No hardware ran. The restored private pin reproduces the baseline synthesis; the CRTC trial and separate slower-memory feasibility work are recorded below.

### Rejected write-select trial and physical read boundary

The unchanged private toolchain was restored through its authenticated bootstrap. Re-synthesizing the frozen baseline reproduces the entire previous JSON after normalizing only the recorded generated/tool-cache root prefixes; no fields are omitted. The programmed CRTC write-select trial passes full-state sequential equivalence, including all 128 programmed register bits; wrong-reset and wrong-mask variants fail that proof. Its fixed-recipe seeds 2 and 3 nevertheless fail native setup: 317 paths at -2.813 ns and 18 paths at -1.233 ns respectively. Both have positive reported hold margins. The trial is rejected without running seed 1 or changing the selected RTL. The complete route journal retains 106 receipts, including both failed trials and their measured charges.

The [manufacturer's MiSTer Pi SDRAM schematic](https://github.com/Retro-Remake/Hardware_MiSTER-Pi/blob/787c33cd60fe0c1f3df9a369744b675022676c2d/Hardware/SDRAM/SDRAM.pdf) specifies two AS4C32M16SB-6TIN devices, shared address-pin byte masks, tied-high CKE and an inverter for the second chip select. The controller already drives masks on A11/A12. Its separate CKE/DQML/DQMH shell outputs are diagnostic mirrors, not separate signals on that published module connector; they are not silently waived from the native inventory. This establishes a published reference, not either kit's fitted revision or trace bounds. Kit B's integrated memory remains unverified.

The [chip manufacturer's revision 1.4 datasheet](https://www.alliancememory.com/wp-content/uploads/AllianceMemory_512M_SDRAM_Bdie_AS4C32M16SB-7TXN-6TIN-7BIN__Rev1.4_June2024NK.pdf), tables 16/17, specifies the -6 device's CL2 minimum period of 10 ns, maximum access of 6 ns, minimum output hold of 2.5 ns, and input setup/hold of 1.5/0.8 ns under its stated electrical conditions. The published clock high/low minima are 2 ns. Those conditions are not measured kit loading, edge rates or flight times.

A zero-flight chip-pin diagnostic applies those explicit CL2 requirements to the fitted reference at all four corners. It checks the actual address/command/data connector nets, with masks on A11/A12. This is an optimistic conditional scenario, not a consumption-cycle proof, native-model adoption or kit acceptance. The earlier fabric-DQ reference misses slow-100C input setup by 9.557 ns and output setup by 6.195 ns.

A separate reference keeps the exact nine-edge forwarded-clock branch and packs all sixteen existing DQ input, data and output-enable registers into IO cells, with explicit zero input-delay selectors. No sample edge or RTL latency changes. Decoder and fitter audits verify the branch and every packed pad. At every corner all 51 output channels pass setup/hold; worst margins are +1.978/+2.766 ns. Input setup improves but all sixteen channels still fail: worst -3.949 ns. All input holds remain positive, minimum +4.856 ns. Three IO-to-fabric capture handoffs fail setup at the slow corners, worst -1.434 ns; all handoff holds pass. The new reference is not a replacement for the ordinary producer artifact.

A hypothetical common capture-clock translation would need more than 3.949 ns to repair input setup and less than 4.856 ns to retain input hold under these reference conditions: only a 0.907 ns open interval before board bounds. Such a shift also reduces the following system-edge handoff budget. This algebra does not qualify a new PLL phase or justify a multicycle exception. It identifies the read-capture and transfer boundary as an architectural obstacle; packing or additional route seeds alone does not solve it. The next implementation needs an explicit valid-word/capture-cycle contract and transfer pipeline, followed by fresh physical setup/hold checks. System/pixel/source clocks remain unchanged.

The whole-RTL Quartus comparison also retains a local trial separating video RAM read processes. It still expands the display RAM and maps 167512 registers, so its unsuitable fit was stopped. No read-during-write waiver or RTL change is adopted. Raw comparisons, rejected trials, source/tool identities and reports are retained under `out/pcxt-whole-board-closure-20261023`. All evidence here is host-only; no kit was claimed or programmed.

### Separate 50 MHz SDRAM feasibility profile

An isolated default-off prototype retains the 100 MHz CPU/frontend and 74.25 MHz pixel clocks, while running the SDRAM controller at 50 MHz with a 180-degree 50 MHz capture clock from the same PLL. The existing held-request/completion interface crosses related clocks; it does not add an asynchronous request queue. The controller input stage follows the slower clock. Its explicit refresh threshold is 349, giving a 350-clock nominal interval of 7 us with room for command retirement; the tests bound observed refresh gaps to 390 slow clocks. A slow-domain reset stretcher retains one-fast-cycle cold resets, and initialization remains gated during that reset. No new mode is adopted by the selected producer.

The private source is `7950b95c4c29c2a400f72c5757d4ca48b5212cd3`. The unchanged default memory implementation passes a 464-point sequential-equivalence proof. The slower variant passes all three mask-delay cases with 10507 reads, 8491 writes, 160 warm-reset and 80 cold-reset phases, plus short cold-reset pulses on both divider phases. Removing reset stretching or using the late refresh threshold fails the corresponding check. The complete CGA Direct system simulation also passes all three mask-delay cases through cold and warm boots, DMA, refresh and 4950000 checked active pixels per case. These are digital-model results, not physical chip timing evidence.

A Quartus reference now contains the actual 100 MHz frontend and 50 MHz controller/capture clocks, with the same prototype memory RTL and exact nine-edge forwarded-clock branch decoded from native seed 2. The native memory uses C5/G1 while the CPU remains C6/G2 and capture C7/G3; the pixel global root is G0. The comparison preserves those requested physical sites/counters/roots and uses a declared 30 pF condition. An initial fit retaining stale MLAB placements fails; a separate fit removes all remaining fabric placements and succeeds. Other placement and clock side branches differ from the native board.

Across all four corners the reference checks all sixteen inputs, 51 output channels and sixteen capture handoffs. Every setup/hold check passes under the explicit zero-flight chip-pin scenario:

| Reference boundary | Worst setup | Worst hold |
| --- | ---: | ---: |
| Chip data to capture | +6.038 ns | +4.861 ns |
| Data/OE/address/command outputs | +6.989 ns | +7.753 ns |
| Capture to controller fabric | +2.751 ns | +10.605 ns |

Each corner also reports 270 register paths, including 68 frontend-to-controller and 46 controller-to-frontend paths. All pass setup and hold; worst register-only margins across corners are +2.751/+0.144 ns. The separate synthetic zero-delay clock-observation row is excluded from those register figures. These are reference paths, not bounds transferred into the native model. Kit revision/chip identity, electrical conditions, board delays, turnaround, consumption-cycle correspondence and reset/CDC qualification remain open.

The three fresh native routes retain the original placement recipe and complete reports:

| Seed | Legacy gate exit | Setup failures | Worst setup | Hold failures | Minimum hold |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 8 | -433 ps | 0 | +787 ps |
| 2 | 0 | 0 | +307 ps | 0 | +797 ps |
| 3 | 1 | 22 | -362 ps | 0 | +784 ps |

This is not a qualified gate. Each report has 17165 paired and 274 unpaired rows, and the compiler only recognizes equal-period declared phase relations. The 100/50 MHz crossings need explicit compiler timing support. In addition, the legacy detailed report assigns its first launch event to an entire net, obscuring mixed launch identities; [nextpnr #294](https://github.com/DeanoC/nextpnr/pull/294) adds an actual launch event per endpoint, with all fourteen timing-report tests passing. An additional default-off harmonic-clock implementation is being checked against independent edge enumeration, without changing the selected toolchain. No missing crossing, uncertainty or physical IO requirement is waived.

The unchanged selected source at `d09ae1105` passes `make check` and the complete `make test-changed --base 5f33d986c1d377946b447a92e1a704d988eda238` run: 842 parent tests, 1299 component tests (one optional oracle skipped), all sixteen XT suites, and every other RTL lane selected by that command. The first sandboxed attempt fails because socket fixtures cannot bind; the successful host rerun and both logs are retained. [Machine-readable follow-up evidence](closure-followup.json) records source identities, reference summaries and trial accounting. The 106-entry journal verifies every full receipt, hash and decimal charge, with cumulative charged wall time 98433.87671378627359 seconds. This includes failures and lease/placement/route wall time; it is not a GPU-seconds claim. No kit ran, no shared/runtime contract changed, and no package was promoted.

## CPU100 / motherboard50 diagnostic candidate

The default-off `HALF_RATE_BOARD` candidate keeps the CPU and byte adapter at
100 MHz and moves the motherboard, ISA/storage, firmware loader and framebuffer
capture to 50 MHz. Pixel output remains 74.25 MHz. A held request/response bridge
uses CPU falling-edge transfer registers and preserves guest tick events.
Display dot enables and speaker cadence/gain follow the board clock. The
keyboard serializer remains in its existing 100 MHz fixture. Coordinated reset
cancels both sides; the memory reset stretcher preserves short cold resets and
the 50 MHz refresh threshold retains the physical refresh interval.

Frozen source `b03528b33bc734b4015e89d6902293c15deeb727`, using nextpnr
`62d792547097358e21b9d35ad7fcd52223c2a8f6` and Mistral
`9ab6522077948fc3dd2e074338874f92e848fc44`, passes all three fresh native routes
with the same HeAP weight2000/critexp5, GPU router and zero cohort budget.
The harmonic-clock opt-in checks the new related-clock crossings. All three
reports contain exactly the same 17709 paired sink-port/launch/capture
identities; physical driver nets may differ because of native FF routing.

| Seed | Raw setup minimum ns | Hold minimum ns | Setup after reference uncertainty ns |
| --- | ---: | ---: | ---: |
| 1 | 1.253 | 0.669 | 1.053 |
| 2 | 1.060 | 0.681 | 0.970 |
| 3 | 1.665 | 0.732 | 1.585 |

No paired setup or hold failures remain in these reports. The uncertainty
screen requires explicit four-corner budgets for all eleven event pairs,
including CPU rising/falling transfers; it supplies no missing-pair default.
All three native RBF clock trees match their recorded bindings, and their
nine-edge forwarded SDRAM clock branches match the fresh Quartus reference.
Missing-binding and wrong-branch negative controls are rejected. The screen
still exits unsuccessfully because nine unrelated/asynchronous rows remain
unpaired; those rows are retained and are not waived.

The separate fitted board50 memory reference uses the published
AS4C32M16SB-6TIN CL2 requirements, declared 30pF and zero board flight. All four
corners pass the reported sixteen input checks, fifty-one output checks and
sixteen capture handoffs. Worst setup/hold margins are respectively
6.038/4.861 ns, 6.989/7.753 ns and 2.458/10.598 ns. The reference has matched PLL
sites/counters and forwarded branch, with different remaining fabric and clock
sidebranches. It is not exact-artifact whole-board STA or a fitted-kit model.

All sixteen existing simulation suites pass on the frozen candidate's default
profile. Permanent regressions additionally cover the randomized bridge
(23184 accepts, 23108 completions, 99 reset offsets), both speaker clock rates,
and the complete CGA Direct and MDA Scanlines board50 systems. Both system
cases pass all three mask delays, both boots, 4.95 million checked active pixels
per mask case, DMA and the tighter 390-controller-cycle refresh bound.
The extracted dot/counter proof fixtures now declare the selected clock and
cover both board rates; the initial missing-parameter fixture failures are
retained. [Candidate evidence](board50-candidate.json) binds source bytes,
raw reports, reference budgets, topology and host logs.

The integrated RTL/QSF bytes match the routed candidate, with only the private
QSF filename renamed to `constraints-board50.qsf`. The public
`--half-rate-board` option requires `--synth-only`: routing remains closed while
the selected compiler lock lacks the reviewed harmonic-clock support. No
ordinary producer result, core lock, runtime/shared contract, package or
hardware acceptance changes. A fresh normal producer and exact-artifact
qualification are still required after toolchain integration.

Receipts 107-111 retain the completed harmonic-memory and rejected bank-enable
trials. Receipts 112-116 retain two sandbox lease-directory failures before
routing and all three successful board50 native routes. The original
106-entry prefix is exact. Cumulative charged wall time, including the
pre-journal amount, is 99665.06616608286217741 seconds; this is not GPU time.
Journal SHA256 is `02f1a133cd95f81ddd779632dd6948686760af750ad6b2d782f22ef8c8757ade`.
The remaining closure work is complete external IO/OE/feedback and reset
coverage, qualified clock waveforms, and evidenced physical board/load/flight
and CS2 steering bounds. No kit was claimed or programmed.

### Stable source verification

Committed source `959bc4f7b230e921258b55b8cb0db987117d4e5f` passes
`make check` and `make test-changed --base a17b334f0109ad90a9d152b45ce8c358c735f90b`:
842 parent tests, 1301 component cases and all 21 XT suites. The initial
component environment skips 49 compiler/oracle checks; all 48 Yosys-dependent
cases subsequently pass with the pinned compiler. Seven selected cases first
fail imports from the wrong test cwd; only those seven are retried at the
module root and pass. One optional host oracle remains skipped. Original
logs and both targeted attempts are retained.

The fresh authenticated `--half-rate-board --synth-only` producer at that
commit reproduces the frozen candidate's entire JSON after only source/output
path-prefix normalization, with identical generated ROMs. The first invocation
uses the cache parent rather than the slot root and fails before synthesis;
the corrected cache-root invocation and comparison pass. No fields are dropped.

The new artifact's structural inventory identifies all 70 top-level pads and
all 16 registered DQ input/data/OE modes. Of 629 nonconstant FF asynchronous-clear
connections, 625 have native paired rows and 4 PLL-driven clears remain unpaired.
Mistral currently treats those ACLR checks as synchronous setup/hold: they do
not supply recovery/removal or MTBF qualification. The other 5 unpaired native
rows are video-domain transfers. The four-corner fitted forwarded-clock
reference has minimum high/low bound 8.187 ns under its declared conditions,
versus the published chip's 2 ns requirement; native fanout, board loads/flight
and jitter remain unqualified. [Verification evidence](board50-verification.json)
retains the exact scope, test results and hashes. No hardware ran.


### Stricter FF clear reference: separate fresh cohort

Nextpnr [#296](https://github.com/DeanoC/nextpnr/pull/296), source
`fdf0462df26d18bfb308248249e1798a0b58667c`, adds an explicit default-off
`--mistral-ff-reset-reference`. Its local clear recovery reference is 170 ps;
the historical 270 ps hold floor remains. Three Quartus fixture families give
1648 observations across four corners: maximum recovery 164 ps and removal
75 ps. These bounds do not qualify reset pulse width, slew, routing fast
corners, PLL relock or MTBF. Inactive, disconnected constant-high clears keep
the ordinary calibration model; missing signal clears still reject. No timing
exceptions or failure overrides are added.

Three fresh native routes use this stricter model and the same frozen board50
source/synthesis, harmonic-clock option, placement recipe and Mistral source.
This is a separate recipe from the earlier ordinary-clear cohort.

| Seed | Raw setup ns | Hold ns | Reference-screened setup ns |
| --- | ---: | ---: | ---: |
| 1 | +0.784 | +0.779 | +0.704 |
| 2 | +0.881 | +0.783 | +0.801 |
| 3 | +0.565 | +0.750 | +0.365 |

All three have identical 17709 paired logical endpoint identities, including
625 clear checks under the stricter reference. Clock topology/RBF audits pass.
Nine unpaired rows remain, so the overall reference screen still fails closed.
All 63 compiler timing/IO tests, seven parser cases and the CLI/checkpoint test
pass at the committed compiler source. Portable Quartus refits also pass.
Two earlier captures fail on inactive-clear calibration after routing; their
source, binaries, receipts and artifacts are retained separately.

[Reset-reference evidence](board50-reset-reference.json) records hashes and
limits. Receipts 117-121 retain those two failures and three successful fresh
routes. The previous 116-entry prefix is exact. Cumulative charged wall time
is 100144.64048435585514741 seconds (not GPU time); journal SHA256 is
`340d7fb18e6722180da2c4cd37035bb36f2ef3cd0360aff55596314eb4da4050`.
Whole-board physical qualification remains open: external IO/OE/feedback,
reset/CDC, actual board load/flight/CS2 and waveform obligations remain.
No hardware ran, compiler pin changed, or runtime/shared contract changed.

### Registered SDRAM IO coverage and chip-reference replay

The current board50 source no longer has the old thirteen dynamic CEOUT pad
configurations: all 37 registered SDRAM output pads have constant enables.
The existing compiler `fdf0462d` therefore supports their pad references.
A private dynamic-CE port was built and its retained reference/corruption
checks pass, but it is unnecessary here and remains unpublished. No route
was launched with that port.

A host-only replay adds only explicit profile/30 pF load attributes to those
37 pads. All 70 external channels are timed: 16 input captures, 36 output
data, 16 DQ enables and both forwarded-clock edges. Synthetic zero external
requirements first establish coverage. A separate replay uses the published
AS4C32M16SB-6TIN CL2 6 ns access, 2.5 ns output hold, 1.5 ns setup and
0.8 ns input hold reference. Constraints include the actual reported forwarded
clock rise interval [1.259, 6.797] ns exactly once. Board flight, uncertainty,
margin and CS2 inverter delay are zero assumptions, not qualified bounds.
The resulting report reproduces the same clock interval.

| Reference boundary | Channels | Setup minimum ns | Hold minimum ns |
| --- | ---: | ---: | ---: |
| DQ input capture | 16 | +6.506 | +2.568 |
| Output data and OE | 52 | +2.817 | +3.507 |

Both replay RBFs are byte-identical to the original successful seed-1 RBF.
All original 17718 logical endpoint identities remain. Reload recalculates
analogue minimum delays and slacks, so replay timing does not replace the
fresh-route reports and supplies no new route-cohort pass. No exceptions or
failure overrides are used. The route journal remains at 121 receipts.

The two separate DQML/DQMH mirror pins are fabric-register driven, not
constant. Standard MiSTer chip masks use shared A11/A12; actual fitted-board
connectivity is still a physical obligation. IO instrumentation additionally
exposes 29 unconstrained pad-output rows: with the existing four PLL clears
and five video CDC rows, 38 rows remain unpaired in this report. No rows are
waived. Complete pad models/contracts, measured board/load/flight/CS2,
waveforms and reset/CDC qualification remain open. No hardware ran.
[Replay evidence](board50-io-reference-replay.json) binds the artifacts and
records this host-only boundary separately from the native route cohort.

### Routed synchronizer structure and interstage timing

`scripts/audit_fes_pcxt_cdc.py` audits the actual CPU100/board50 checkpoints
against their successful route receipts and final analogue timing reports.
All three retained routes have the same twelve marked FFs in six disjoint
two-stage chains: three reset chains and three video synchronizers. The audit
checks effective native clock, enable, reset and load pins, permits only
uninverted native identity buffers between stages, requires exclusive head
fanout, and retains paired setup/hold checks bound to each actual driver.
It grants no timing exception and does not qualify downstream consumers.

| Seed | Minimum interstage setup ns | Minimum interstage hold ns |
| --- | ---: | ---: |
| 1 | +12.288 | +0.837 |
| 2 | +12.324 | +0.820 |
| 3 | +12.267 | +0.858 |

Twelve focused tests use an excerpt of the actual native seed-1 cells and
reports. Corrupting fanout, buffer polarity, stage clock, enable, reset
polarity, stage marking, pin metadata, checked timing flags, edge identity,
negative/nonfinite margins or final-model status rejects the audit.
The affected-check planner selects host/component checks without rebuilding
unchanged RTL for this diagnostic. No source RTL, constraints, toolchain lock,
RBF or runtime/shared contract changes.

The four PLL-driven clear rows, three asynchronous video synchronizer entries
and two bundled bank transfers remain open timing/physical obligations.
Synchronizer tails also feed additional reset controls; their consumer lists
are retained, without claiming downstream safety. Pulse/slew/relock, settling
and MTBF, bundled-data stability and actual board interfaces still require
qualification. No kit ran and no route was added: the journal remains at 121.
[Audit evidence](board50-reset-cdc-audit.json) records every chain identity,
source/report/receipt hashes, per-chain margins and the exact qualification
boundary.

Stable source `a7a30a603c918e8170cf2a1a5bc43bc923977299` passes
`make check`. The affected run initially fails in its sandboxed parent lane:
990 cases include 10 failures and 136 errors (116 unique test IDs), with
one skip; local sockets and build/cache access are denied. The unchanged
116 failed cases all pass on a targeted host rerun. The unstarted component
lane then passes 1313 cases on the host, including all twelve new audit tests,
with 49 existing optional-tool/oracle skips. The original failure, exact retry
IDs and host logs remain bound in the audit evidence; no broad pass replaces
the failed capture. A corrupted report-digest CLI control also rejects before
producing an audit. No source, constraints or hardware changes accompany
this validation record.

### Video ownership proof and reset paths to the next registers

`tests/test_pcxt_video_mailbox.py` proves digital safety on the real CGA/MDA
ownership logic with arbitrary source/pixel clock ordering and arbitrary extra
first-stage sampling stalls. Frame-tail position and output SOF are
independent inputs, preserving the actual eligibility/queue guards; RAM
outputs are arbitrary. Both logical domains reset together. The projection
contains every post-reset ownership trace under that contract, without
simulating entire frames.

ABC PDR proves bank identifier stability while pending, valid/separate bank
selection, source/display ownership after acknowledgement, adoption-only
front changes, and two prior pixel observations of the identifier and toggle
before adoption. A 24-step SAT witness covers queuing, publication, adoption,
acknowledgement and republication. Early pending-identifier modification and
a shortened synchronizer produce counterexamples in frames 3 and 4.
Initial bounded induction is inconclusive under arbitrary stalls; the final
PDR safety proof is unbounded. No production RTL changes.

This proof does not establish framebuffer contents or write-commit timing,
physical bundled-data delay/skew, analogue metastability/MTBF, independent
reset safety, or physical reset timing. Reachability is not a liveness proof.
It grants no timing exception to the five asynchronous video rows.

The routed audit now follows each reset tail through combinational logic to
the next register inputs. Every supported terminal must retain its effective
native pin identity, same clock and edge, actual routed driver, and positive
paired setup/hold checks. Combinational cycles, unsupported terminals,
missing rows, changed clocks and cut checks reject. All three routes have
the same 919 distinct consumer ports. Their 1021 cone/terminal checks overlap;
they are not 1021 distinct paths.

| Seed | Raw consumer setup minimum ns | Raw consumer hold minimum ns |
| --- | ---: | ---: |
| 1 | +6.578 | +0.931 |
| 2 | +5.180 | +1.048 |
| 3 | +6.723 | +0.934 |

Two related asynchronous clear consumers remain explicit pulse/control
obligations even though their native paired checks pass. Consumer policy,
exceptions and physical qualification remain false. Eighteen structural
corruption tests and four formal test methods pass with the pinned Yosys/ABC;
CGA and MDA are separate positive proof cases. The expanded native excerpt
retains the complete reset consumer cones and relevant rows.
[Control evidence](board50-cdc-control-proof.json) binds all solver artifacts,
source/tool hashes and the three native audits. No fresh routes, kit operation,
core lock, runtime/shared contract or hardware acceptance changes; the route
journal remains at 121 receipts. Fitted SDRAM revision/chip identification is
still needed to replace the optimistic physical IO assumptions.

### Recovered module photos and earlier chip-select launch

The operator pointed back to RetroRemake's product photographs. The retained
[manufacturer photo](https://cdn.shopify.com/s/files/1/0713/7145/6535/files/ram1to1.png?v=1725021808)
shows two Etron EM63B165TS-6SG devices; the product is described as based on
XS-DS v3.0. The separately published MiSTer Pi schematic uses Alliance parts.
These are different published populations. The Etron -6 CL2 values match the
prior reference's 6/2.5 ns access/output hold and 1.5/0.8 ns input setup/hold.
The [photo record](retroremake-sdram-photo-reference.json) binds the original
images, primary datasheets and exact distinction from fitted-kit inspection.

The published SN74LVC1G04 inverter reference has maximum propagation 4.2 ns
at 3.3 V +/-0.3 V, CL 50 pF and -40..85 C. Adding that bound to the former zero-flight
chip reference makes CS setup fail at -1.095 ns, versus +3.105 ns with no inverter.
The failed host replay is retained, with its final analogue report and unchanged
RBF; no failure override is used. Actual fitted inverter/load/slew is unverified.

The experimental top now launches CS 2 ns earlier through a fourth 50 MHz PLL
counter at phase 18 ns. Other board outputs remain at phase 0; CPU 100 MHz,
board 50 MHz, capture 50 MHz/phase 10 ns and pixel 74.25 MHz targets are unchanged. The caller owns
only the final CS register; the shared controller's default-zero parameter
retains the original IO output stage. No command state-machine or SDRAM
consumption cycle changes. A proof using the actual IO stage preserves all
default samples, proves equal chip-capture values with the earlier CS stage,
and rejects omitting that final register.

Three fresh native routes use one fixed recipe on frozen source `320dbc51`,
nextpnr `fdf0462d` and Mistral `9ab65220`, with harmonic clocks and the stricter
clear reference. All 17709 paired identities match across this separate cohort;
the same 9 unpaired rows remain. This does not combine the earlier recipe gates.

| Seed | Raw native setup ns | Raw native hold ns | Conditional chip CS setup/hold ns |
| --- | ---: | ---: | ---: |
| 1 | +1.762 | +0.794 | +0.962 / +1.508 |
| 2 | +0.685 | +0.761 | +0.962 / +1.508 |
| 3 | +0.605 | +0.796 | +0.962 / +1.508 |

Each conditional host replay covers 70 registered IO channels, including the
inverter bound, and emits the identical native RBF. It declares FPGA load 30 pF,
inverter load 50 pF and zero board flight/uncertainty. DQ capture setup/hold is
+6.506/+2.568 ns. Reloaded timing remains separate from the fresh native reports.
There is no fresh four-corner uncertainty reference for this four-counter
variant, fitted board acceptance, or physical reset/CDC/HDMI/HPS qualification.
The normal experimental producer route remains gated on compiler integration.
[Candidate evidence](board50-cslead-candidate.json) binds the recipe and all
receipts. Journal entries 122-124 add 264.30493893800304 charged seconds; cumulative
charged wall time is 100408.94542329385818741 seconds. The 121-entry prefix remains
exact. The outer tool session reported 143 after the retained completion line;
all three child receipts independently record exit 0 and immutable artifacts.
No kit ran or runtime/shared contract changed.

### Committed early-CS source and fresh four-counter reference

Commit `695036ff48` integrates the four RTL files byte for byte from frozen
source `320dbc51`. The routed cohort selects CGA Direct; MDA/scanline variants
inherit no routed acceptance. A fresh ordinary authenticated `--half-rate-board --synth-only`
producer at that committed revision reproduces the complete frozen synthesis
JSON after normalizing only source/output directory prefixes. Generated ROMs
also match; no cells, parameters, nets, source lines or other fields are dropped.

All five native clock trees match the selected edges decoded from each exact
RBF. Raw memory-PLL queries include default fields: M=20, N=2, VCO=500 MHz;
C6/C7/C5/C8 preserve CPU/capture/board/CS period and phase. C8 has high/low
counts 5/5, preset 10 and phase mux 0, encoding period 20 ns and phase 18 ns.
The final CS pad is the only register clocked by that output. Missing CS
branch bindings and an incorrect phase preset are rejected. This is structural
encoding evidence, without an analogue waveform or metastability claim.

A new Quartus 17.0.2 reference uses the committed PLL/CS blocks and actual
memory frontend/controller bytes. The queried raw memory-PLL fields match all
three native RBFs, and the nine-edge forwarded SDRAM clock branch matches.
The fitted CS clock sidebranch and other fabric differ from the native full
board; these reports do not calibrate the native IO delays. The reference's
virtual probes are not board interfaces.

Every paired native row has fresh explicit four-corner uncertainty budgets,
including the board-to-CS transfer. All 17709 paired identities match across
the cohort and remain positive under this partial screen:

| Seed | Setup after fresh reference uncertainty ns | Hold after fresh reference uncertainty ns |
| --- | ---: | ---: |
| 1 | +1.562 | +0.794 |
| 2 | +0.605 | +0.761 |
| 3 | +0.405 | +0.796 |

The screen still exits 1 because the same nine unpaired rows remain. It does
not assert native fast-corner, external IO, reset or CDC acceptance. The bounded
Quartus chip reference separately includes Etron -6 CL2 and the conditional
4.2 ns CS2 inverter delay, declared FPGA 30 pF, and zero board flight. All four
corners pass their reported paths. Worst reference setup/hold is +6.048/+4.861 ns
for the 16 inputs, +5.183/+5.389 ns for the 51 output paths, and +2.495/+10.635 ns
for the 16 capture handoffs. Separate shell CKE/DQML/DQMH mirrors remain outside
those chip-pin output requirements.

The new native routes also pass the same six-chain structural/interstage
checks and all 919 reset consumer ports. Minimum consumer setup/hold is
+6.553/+1.000 ns, +6.146/+0.870 ns and +6.612/+0.931 ns for seeds 1/2/3.
The 1021 cone-terminal checks overlap; two asynchronous clear consumers retain
pulse/control obligations. The committed final-stage proof now reads the
actual top-level CS register as well as the shared IO stage; all three proof
cases pass after both final stages have loaded, including rejection of the
missing-stage variant. Power-up behavior before that warmup is outside this lemma.

[Fresh reference](board50-cslead-quartus-reference.json) retains the complete
corner summaries, source/constraint/report identities, branch comparison and
partial native screens. [Verification](board50-cslead-verification.json)
retains producer, proof and audit identities. No further native route ran;
the journal remains at 124 receipts. Whole-board physical qualification and
exact-artifact hardware acceptance remain open.

Validation of committed code `695036ff48` passes `make check` and the full
`make test-changed TEST_CHANGED_ARGS='--base 485165b3ec'` run: 843 parent cases,
1326 component cases, all 21 PC/XT suites, and the full selected Atari ST and
RAM Tester simulations. The component run skips 56 existing/optional tool
cases, including the four mailbox methods and three new CS methods. All three
CS methods separately pass with the pinned Yosys/ABC; the unchanged mailbox
and reset-audit source retains its explicit 22-method pass at `485165b3ec`.
The final verification commit changes documentation/evidence only.

### Reviewed compiler main and HDMI receiver phase

The task-owned nextpnr clear-reference branch now merges reviewed main at
`5b6b5a9f`, producing `e9ff47f7`; PR #296 targets main. The merged main includes
the endpoint launch identities and harmonic-clock support from #294/#295 and
the device-model lifetime repair. All 64 focused compiler cases and the reset
parser/CLI checks pass. Three fresh early-CS routes reproduce each previous
same-seed RBF and complete timing JSON exactly. This integration changes no
timing requirement or toolchain lock. [Compiler evidence](board50-compiler-main-integration.json)
retains the identities and comparisons.

A conditional HDMI contract exposes a higher-level hold problem: the original
common forwarded clock has +5.235 ns setup but -3.598 ns hold, with all 27
data/control channels failing hold. The contract pairs clock/data pad tails
within each of four PVT corners, considers independent 0/30 pF clock/data load
endpoints, and uses the actual native clock-route interval. It includes the
ADV7513's 1.8/1.3 ns setup/hold requirements for clock delay none, explicit
300/210 ps reference uncertainty, a 20 ps guard and outward 10 ps rounding.
Board flight/skew is zero; these declared conditions are not fitted-kit bounds.

The experimental profile gives HDMI its own 74.25 MHz video-PLL output at
phase 8979 ps, about 4.489 ns before the next pixel edge. Pixel state stays at
phase zero. An explicit global buffer is required because this output drives
only the unregistered clock pad. The default profile keeps its single video
output. A proof projects the actual top-level pin assignments and preserves
every 27-bit pixel bundle in order after warmup; wrong-clock and corrupt-pixel
controls produce counterexamples. It proves digital ordering, without a
propagation, power-up or physical waveform claim.

Fresh two-output Quartus fits at both load endpoints match all 28 native
egress boundaries and pad/inversion settings. All 896 corner/edge/early/late
observations identify the same GOUT boundaries as the native routes. The
queried M/N/fraction, divider and phase-preset fields also match both reference
RBFs and all three native RBFs: C6/C7 divide by six, and C7 preset five encodes
approximately 8978.676 ps. TimeQuest reports period 13.464 ns while the raw
programmed fields imply approximately 13.468013 ns. Pad-tail extraction excludes
upstream routing; this quantization difference remains explicit.

| Seed | Native setup / hold ns | Conditional HDMI setup / hold ns |
| --- | ---: | ---: |
| 1 | +1.343 / +0.708 | +1.183 / +0.788 |
| 2 | +1.816 / +0.772 | +1.004 / +0.783 |
| 3 | +1.650 / +0.783 | +0.842 / +0.855 |

Each native report retains 17709 paired and nine unpaired rows. Separate
combined host replays pass all paired rows and preserve the exact native RBF;
the 70 SDRAM channels retain +0.962/+1.508 ns CS and +6.506/+2.568 ns DQ read
setup/hold. The combined reports retain eleven unpaired rows, including the two
mask mirrors. HPS OE/feedback still lacks qualified hard-IP launch/capture bounds.
No source clock, exception or failure override is invented for those paths.

All six clock-tree bindings match each exact RBF. The six-chain audit retains
all 919 distinct reset consumer ports and its asynchronous pulse/control
obligations. Its alternative pixel-clock identity is explicit and must bind to
video PLL output zero through the native clock buffer; ordinary callers keep
their original identity. All 26 focused phase/audit tests pass. The
[candidate evidence](board50-hdmi-phase-candidate.json) retains the complete
conditional contracts, reference tails, identities and limitations. Ordinary
committed-source producer and affected-regression verification follows separately.

Journal entries 125-130 retain all six new native searches, including the
compiler comparison cohort. There are 130 receipts and cumulative charged wall
time 101129.89851958258019741 seconds. The prior prefixes and all new receipt,
artifact and input hashes verify. Failed pack/reference attempts and the
original failing HDMI contract remain retained as diagnostics. No kit ran;
native fast-corner calibration, board/load/flight bounds, reset/CDC/HPS physical
qualification and exact-artifact acceptance remain open. MDA/Scanlines inherit
no routed acceptance from this CGA Direct cohort.

The full affected run at `f925f35eeb` passes 843 parent tests, 1334 component
tests and all 21 PC/XT suites. Its ordinary authenticated synthesis reproduces
the complete frozen timing-input JSON after directory-prefix normalization
only; generated ROMs match. The four committed-source pixel-order proof cases
and all three reset-consumer audits also pass.

Follow-up review catches a default-profile packing regression: the second
video output is unused there, and the native PLL validator requires its
frequency and phase to be zero. Those two parameters now select zero when
`HALF_RATE_BOARD=0`; the experimental settings remain 74.25 MHz/8979 ps. Native
pack checks lower the actual clock block in both modes and reject deliberate
nonzero-unused-frequency and nonzero-unused-phase mutations. The old and fixed
experimental clock-block synthesis JSONs are entirely identical. Final-source
producer and affected-regression verification follows this correction.

### Final committed-source verification and HPS configuration readback

Code `f24a255ad4` passes `make check` and the full selected
`make test-changed TEST_CHANGED_ARGS='--base f925f35eeb'` run: 806 parent tests,
1338 component tests and all 21 PC/XT suites. The component run has 64 optional
tool-dependent skips. Thirty focused tests separately pass with pinned tools,
including both native clock-block pack modes and the two rejected unused-output
mutations. The four committed-source pixel proof cases and three reset audits
also pass. The ordinary authenticated producer again reproduces the complete
frozen timing-input JSON after directory-prefix normalization only, with all
generated ROMs equal. The default-profile correction leaves the experimental
mapped JSON unchanged. No further native searches run; the journal remains at
130 receipts.

A fresh two-output pixel-counter reference supplies explicit four-corner
pixel-self uncertainty, with queried programmed PLL fields equal to the native
cohort. Memory clock-event pairs retain the matching four-counter reference.
All 17709 paired rows remain positive under the partial screen:

| Seed | Adjusted setup minimum ns | Adjusted hold minimum ns |
| --- | ---: | ---: |
| 1 | +1.253 | +0.706 |
| 2 | +1.736 | +0.772 |
| 3 | +1.560 | +0.783 |

Each screen still exits 1 on the nine unpaired rows. These reference budgets
do not calibrate native fast-corner delays or grant physical acceptance.
[Final verification](board50-hdmi-phase-verification.json) binds the source,
tests, ordinary synthesis, proof/audit artifacts and reference screens.

A later leased read-only Kit A diagnostic captures its live device-tree and
HPS clock/controller programming. The peripheral PLL is nominally 1 GHz from
the live 25 MHz reference; its base divider five and L4 SP divider two select
100 MHz. This clock retains an independent phase from the FPGA clocks.
Physical controller `0xffc06000` is Linux adapter `i2c-1`; its device tree
requests 100 kHz. Standard high/low counts are 427/499 and TX SDA hold is one
cycle, nominally 10 ns before hard-IP/pad/board effects. These are observed
configuration values, not qualified source or external setup/hold bounds.

[Configuration evidence](board50-hps-config-readback.json) retains the raw
registers, live DTB digest, decode sources and limits. Clock debug files and
the optional health identity query are unavailable, so running appliance,
bundle and exact kernel-source binding remains incomplete. No PC/XT RBF is
loaded and no I2C bus transaction, FPGA/software/image/boot/host change is
performed. The target lease is confirmed free after the diagnostic.

Next integration requires review of nextpnr #296, then its selected pin/recipe
and a fresh ordinary route producer. Whole-board acceptance still requires
HPS source/OE/pad/feedback bounds, native fast-corner and board/load/flight/skew
qualification, reset/relock/pulse/MTBF and bundled-data checks, followed by
exact-artifact hardware acceptance. No runtime/shared protocol or package is
promoted by these results.

### Merged compiler and ordinary three-seed producer, 25 October

Base `a907e1ec98`; implementation `47b21bc4bb`. The core lock now selects
the canonical nextpnr #296 merge `2fa5ec4c0ef6f6a34eafe0a311cf63f866b73c99`.
Mistral, Yosys and the other tool pins are unchanged. A fresh immutable shared
cache authenticates the selected binaries. The ordinary `--half-rate-board`
producer now routes with `constraints-board50.qsf`, explicit harmonic and
FF-clear reference options, and cohort budget zero. It retains every failed
attempt and adds no failure override. The default common-clock profile keeps
its previous pad and timing choices. No RTL, clock frequency, runtime/shared
contract, package or kit changes accompany this integration.

All three fresh ordinary CGA Direct producers succeed from clean committed
sources with one fixed recipe, using normal shared GPU phase leases and two
active jobs/phases per owner. Every complete mapped JSON equals the frozen
candidate after normalizing only source/output/tool-install directory prefixes.
No fields are dropped. Generated ROMs match. Every same-seed final native
timing JSON is equal under those same path substitutions, and every RBF is
byte-for-byte identical to the retained candidate.

| Seed | Native setup / hold minimum ns | Conditional HDMI setup / hold minimum ns |
| --- | ---: | ---: |
| 1 | +1.343 / +0.708 | +1.183 / +0.788 |
| 2 | +1.816 / +0.772 | +1.004 / +0.783 |
| 3 | +1.650 / +0.783 | +0.842 / +0.855 |

Each native result has 17,709 positive paired rows and the same nine explicit
unpaired CDC/reset rows. Fresh reset audits retain six chains, 919 distinct
consumer ports and 1,021 overlapping cone-terminal checks. Separate strict host
replays reuse the existing conditional SDRAM/HDMI contracts only after exact
native RBF and complete timing equality. Their complete final timing JSONs
match the retained same-seed replays. All 17,806 paired rows pass, including
70 SDRAM and 27 HDMI channels, with eleven unpaired rows retained. Each replay
preserves its fresh native RBF exactly. These are conditional host checks,
not new Quartus characterization, native fast-corner or fitted-board acceptance.

The merged #296 reference also exposes a real regeneration gap: its unrestricted
Quartus removal query selects falling CLRN assertion, which the strict rising
deassertion validator rejects. The failed unmodified fit and reports are
retained. [nextpnr #299](https://github.com/DeanoC/nextpnr/pull/299) restricts
recovery/removal queries to rising reset-source edges and regenerates all three
fixtures and four corners. All 1,648 observations now validate the RTL clear
source and sink deassertion transition; all fitted recovery/removal checks pass.
The maxima are 164 ps recovery and 43 ps removal, within the unchanged
170/270 ps backend limits. Eight parser/archive tests and two characterizer
failure-publication tests pass. A model review reports no issues, and #299 is
merged as `bec82e36d436acec7cbeb248725acfde72b8f220`, with a tree identical
to reviewed `2bc7d90beb1e31b0902557e54107d10b78beb190`. It changes only
reference fixtures, tests, archives and documentation. These native routes stay
bound to the selected #296 binary; their original incomplete-reference warning
is retained as launch metadata. The fresh merged reference validates its
unchanged limits, without granting physical reset acceptance.

`make check` passes 21 generated consumers/37 fixture copies/zero copied source
pins. The full selected affected run for `47b21bc4bb` passes 843 parent tests,
1,341 component tests and all 21 PC/XT suites. Nine focused producer tests,
four explicit profile-pack tests using the newly authenticated tools, and the
merged parser/characterizer/CLI checks also pass.

[Integration verification](board50-merged-producer-verification.json) binds
the code, tool/cache identities, complete native comparisons, separate IO
replays, reset audits and new reference. The append-only journal now retains
133 route-search receipts. Its first 130 entries are byte-for-byte unchanged;
the three native runner wall charges total 1,533.7742970571853 seconds,
including GPU lease waits and excluding synthesis. Full producer wall times
are retained separately. No additional native searches were omitted.

The compiler/ordinary-producer integration step is complete. Whole-board
acceptance still requires HPS hard-IP source/OE/pad/feedback contracts and
immutable running-artifact binding; native fast-corner and fitted board/load/
flight/skew/inverter-slew bounds; reset/relock/pulse/MTBF and bundled-data
qualification; and exact-artifact hardware acceptance. HPS and fabric clocks
retain no declared phase relation. This CGA Direct cohort supplies no MDA or
Scanlines native routed acceptance. No hardware ran in this integration step.

### HPS enable/feedback inventory and reference, 25 October

Base `e9ee608e02`; implementation `c0a3e0d0e7`. The HPS auditor binds all
four enable/feedback boundaries to each immutable ordinary producer receipt,
complete routed JSON and exact RBF. It discovers roles through external ports
and validates controller 2 at native site 52.60.0, SCL U10, SDA AA4,
constant-low data, unregistered IO, pin maps, exclusive enables and uncrossed
feedback. All three frozen seeds pass this structural check.

Read-only extraction finds two supported feedback routes and two unsupported
OE routes per seed. Mistral's waveform builder has no HPS output case. Its
unavailable OE zero arrays become unknown bounds in the audit. The helpers use
preserved `fdf0462d` objects and Mistral `9ab65220`, independently of the
selected producer. They restore native pin maps and read the original RBFs.
An explicit four-corner routing helper reproduces all slow100 controls exactly.
The two OEs remain unsupported at every corner. Seed 1 SDA falling feedback
changes from 3.586--3.639 ns at slow100 to 1.290--1.313 ns at minimum/-40 C;
these values use the unrepaired legacy voltage model and are retained as
diagnostics, not trusted route bounds. They also exclude pad and hard-IP
capture timing.

The committed characterizer produces fresh Quartus 17.0.2 Build 602 final-model
fits at declared 0 and 30 pF loads. All 128 paths cover both pins, directions,
transitions, early/late analysis and four corners. Every path retains its
IOOBUF OE or IOIBUF boundary and HPS site. `AUTO_OPEN_DRAIN_PINS OFF` is
essential: the default optimization uses dedicated open-drain DATAOUT,
bypassing the separate OE boundary used by the native bitstreams. The final
references retain separate OE routes and the native delay-selector values.
Earlier dedicated-open-drain and failed fixture attempts remain retained.
The zero source row is an HPS analysis boundary, not characterized clock-to-Q.
These own-route references do not supply native-route, measured board-load,
continuous-load, hard-IP launch/capture or HPS/fabric phase bounds.

The minimum-corner investigation identifies a simulator supply bug:
`add_gnd_vdd` does not update the fixed 1 V threshold setting.
[Mistral #22](https://github.com/DeanoC/mistral/pull/22), code `d210ec5d9a`,
corrects it and adds an independent analytical RC voltage-invariance test.
The actual parent fails and the corrected source passes. A clean build passes
all ten CTests. Three host controls linked to the actual corrected library
reproduce every complete original final native timing JSON and RBF exactly;
the native producer uses the unchanged 1 V slow100 corner. Other corner
voltage/model corrections remain under investigation. The toolchain lock is
unchanged, and this correction grants no fast-corner or physical acceptance.

Two briefly leased, read-only Kit B captures observe nominal 100 MHz L4 SP,
standard-mode counts 427/499, requested 100 kHz bus rate and one TX hold cycle.
The boot ticket and health agree on image `4db74864`. Running agent/runtime
hashes match active bundle `35d58489` and its declared `022481f4` provenance;
the different base-image agent digest is recorded separately. The image
manifest's kernel digest matches that source revision's locked artifact.
Actual loaded-kernel and root-image backing bytes were not hashed, so full
kernel-source binding remains open. No PC/XT RBF load, I2C transaction,
software/image/boot or shared-host change occurs. Both captures finish
lease-free; the current status file governs later kit use.

[HPS evidence](board50-hps-i2c-contract.json) binds code, native receipts,
128 fitted paths, configuration snapshots and regressions. `make check` passes
21 generated consumers/37 fixture copies. The full selected affected run
passes 843 parent tests, 1,351 component tests and all 21 PC/XT simulation
suites; 64 optional component checks are skipped. Ten focused HPS tests and
the fresh 128-path characterizer also pass. No native route
search runs; the 133-entry journal is unchanged. No runtime/shared contract
or package changes. Physical HPS/OE/pad/board bounds, complete native
fast-corner cell/clock/data coverage, reset/CDC qualification and observable
exact-artifact PC/XT hardware acceptance remain open.

### Minimum-model investigation, 26 October

Base `7217d83031`. [Mistral #22](https://github.com/DeanoC/mistral/pull/22)
now includes current master at `c57686002d`, retaining its newer configured
receiver-load tests; all ten CTests pass. The merge changes no library source.
[Mistral #24](https://github.com/DeanoC/mistral/pull/24), `d8b0745098`,
adds supply-normalized driver-table coordinates and derivatives, preserves the
physical three-dimensional gate origin, removes the extra supply factor in
LAB/MLAB source waves, and orders finite signed derating intervals. It depends
on #22. All thirteen CTests pass, including 192 actual source waveforms and
an actual powered-buffer negative-delay regression. Actual parent libraries
fail the waveform and signed-interval controls. Both PRs are ready for review;
neither changes this FES toolchain lock.

Fifteen host replays use the actual committed corner library: three seeds at
four route corners, plus three explicitly diagnostic zero-FF-min controls.
The later master merge changes no library bytes or library source tree.
Every RBF is byte-identical to its frozen original. Each slow100 control
reproduces the complete original native timing JSON. All twelve ordinary
corner replays complete with 17,709 positive paired rows and nine explicit
unpaired rows, without analogue simulation declines. Cell/PLL/pad calibration
and HPS sources remain incomplete, so these are numerical diagnostics, not
native fast-corner signoff. The artificial zero-FF-min controls report
70/63/70 failing holds and minima -0.165/-0.140/-0.130 ns. They do not establish
physical RTL failures.

Re-analysis of the immutable, deassertion-corrected Quartus FF fits retains
3,200 clock-to-Q and 3,200 data-pin observations. It covers every FF slot in
two LABs with constant/positive enables and three LABs adding inverted enables, both data
transitions, early/late analysis and four corners. The committed fixture
sources match, allowing only the reference reader's declared Quartus version
bookkeeping normalization. Original fits remain unchanged; no new fit runs.
Synthetic IO clocks expose the local arcs and supply no board clock contract.
At minimum/-40 C, clock-anchor-to-Q observations range from 168 to 173 ps;
the native diagnostic still uses 731 ps for both minimum and maximum.

Data observations retain the distinct `d` and `asdata` pins. Their internal
data/clock-to-storage requirements differ, so neither bare FF pin constants
nor a clock-to-Q-only correction supplies a complete native FF model. Native
virtual data/control endpoints and the actual PKREG input selection must be
matched before transferring these observations. Actual fitted RBF PKREG bits
agree with all 3,200 retained `d`/`asdata` identities. A separate reader uses
nextpnr's own placed BEL metadata and selected routing edges to check all
4,067 native FFs per seed against their original RBF input-selector bits;
every check passes. It also finds 1,008/928/932 MLAB FFs, which these LAB-only
references do not characterize. This selection proof does not establish
equivalence of the virtual timing boundaries. The next repair remains matched
LAB/MLAB data/control characterization and a source-bound min/max model,
followed by complete frozen-route replays.

[The model screen](board50-minimum-model-screen.json) binds compiler commits,
tests, all fifteen replays, fitted source/report hashes and the two retained
analysis Tcl scripts. The accompanying compressed archive contains all 6,400
FF observations. Evidence is host-only. No runtime/shared contract, package,
clock rate or timing exception changes; no kit or GPU is used in this step.
The native search journal remains unchanged at 133 entries. HPS/pad/board,
reset/CDC and observable exact-artifact hardware qualification remain open.

### Complete FF references and sensitivity, 26 October

Base `9aec310170`. [nextpnr #305](https://github.com/DeanoC/nextpnr/pull/305),
`a718d09f06` on main `03f39e460c`, adds a reference-only inventory of 101,120
local FF observations. Sixteen fixed-placement cases cover LAB/MLAB, all four
FF positions in ten ALMs per block, all three local clock/enable selectors,
100/50/74.25 MHz clocks, both clock polarities, constant/positive/inverted
enables and synchronous clear active/inactive. Four cases also retain clear
deassertion. Sources, geometry, clock and data/control identities, RBFs and
report hashes are bound in its manifest. Synthetic IO/reset fixture violations
remain signed; these fixtures are not accepted board or reset designs.

The configuration reader builds through CMake. Its CTest passes ten Python
regressions, including complete RBF decoding and changed-control rejection.
A fresh Quartus 17.0.2 Build 602 rebuild from that committed source reproduces
all sixteen RBFs byte for byte and every local timing value exactly. Across
the covered modes, clock-anchor-to-Q spans 158 ps at fast/-40 C to 860 ps at
slow/100 C. The original producer still uses 731 ps for both bounds. The new
reference leaves the timing engine and this FES source lock unchanged.

A separate structural reader checks all 4,067 native FFs per seed against
the actual RBF selectors and routed architecture objects. All clock suffixes
have two virtual edges back to physical CLKIN0. DATAIN resolves either to the
local LUT output or, for 1,222/1,066/1,079 FFs, a selected same-ALM E/F pin.
All 3,088 enables, 2,579 synchronous clears and 629 asynchronous clears per
seed resolve to their architecture-owned LAB inputs. Enable and synchronous
clear activation/inversion bits and selected E/F inputs also agree. The
default control-set allocator deliberately routes common CLKA through virtual
CLK0, including when physical CLK/ENA1 is selected; model selection must read
the actual RBF index rather than infer it from that virtual wire. No native FF combines
an active asynchronous clear with an inverted clock. This proves structural
cut identities; it does not yet qualify the numerical delay at those cuts.

Fifteen further host replays use the complete reference's outward-rounded
local pin ranges with the actual corrected Mistral corner library. Three
unchanged-model slow100 controls reproduce the complete original timing JSON.
The twelve FF-reference sensitivities cover all four route corners. Every
RBF stays byte-identical, every run retains 17,709 paired/nine unpaired rows,
and no analogue simulation declines occur. The smallest setup margin is
+0.718 ns. Seeds 2 and 3 retain positive setup/hold at all diagnostic corners;
seed 1 fast/-40 C reaches zero hold on the fabric-FF-to-SDRAM-DQ-output-register
input and is correctly rejected by the native timer. A zero margin is a failed
case, even though the count of strictly negative rows is zero.

[The complete-reference screen](board50-ff-complete-reference-screen.json)
binds the fresh fits, source and tool identities, native cut inventories and
all fifteen replay receipts. These remain pin-reference sensitivities:
numerical pin-to-virtual equivalence, other primitive/PLL/pad models and HPS
sources remain unqualified. No RTL repair follows from the zero-hold result.
The next compiler step is a source-bound FF model matched to the native cuts,
followed by complete corner/endpoint checks. Whole-board and exact-artifact
hardware acceptance remain open. No clock, timing exception, runtime/shared
contract, package, kit or GPU changes; the search journal still has 133 entries.
