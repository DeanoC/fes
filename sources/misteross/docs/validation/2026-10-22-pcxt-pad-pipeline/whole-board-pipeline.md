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
