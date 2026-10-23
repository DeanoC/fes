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
rejection. The DMA and complete-frame display simulations pass. The complete
16-suite rerun and frozen whole-board synthesis/route are pending. These are
host proofs and simulations, not timing, physical IO/reset or kit acceptance.
Required broad integration checks will be completed after the candidate is
stable; no fresh parent/component pass is claimed for this commit yet.

## Route accounting

Receipt 83 adds exactly 1251.239453698974 charged seconds. All 83
unique receipt objects, hashes and Decimal charges verify, preserving the
original 82-row journal prefix. Cumulative charge including the pre-journal
amount is 93753.29497616877719 seconds. User authorization remains
unlimited route budget. Journal SHA-256: `f6503930973e624ba58b40e257912c471921aacb423fa37a794d67239236d577`.
