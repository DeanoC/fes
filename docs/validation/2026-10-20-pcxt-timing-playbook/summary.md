# PC/XT current-main and placement-region follow-up, 20 October 2026

Timing remains open. [PR #836](https://github.com/DeanoC/fes/pull/836) stays parked and [issue #840](https://github.com/DeanoC/fes/issues/840) stays open. This is diagnostic evidence, not a source, toolchain or recipe promotion.

## Scope and frozen inputs

Deano authorized the proposed current-main comparison and placement-region investigation with “do it”. This follow-up was explicitly bounded to three baseline seeds, one evidence-led region configuration if needed, and confirmation only if qualified, with three additional charged GPU-hours. It is separate from the previous campaign's 19,118.415 seconds (5.311 hours); the historical 66 records remain unchanged.

FES checkout/base is `2e94daa66b7bfceea76edf8c3a5ae3aa2076e95b` on `feat/pcxt-board`, with functional source checkpoint `37177c9c` and merged main base `40fbe090`. Each replay copies the retained private PIC-feedback/local-ROM/126-degree candidate netlist, SHA-256 `4d8e3e719cdb53f3ac4a8a85b1275d9abc36f769cc0e32da70f525ff4120e90d`. Its actual ROM phase is 3500 ps, with sixteen fixed microcode RAM sites. Those private changes remain unadopted; the committed core still uses its previous profile. Source text, proof references, synthesis closure, constraints and replay driver are preserved in `runs.jsonl` and the previous campaign.

The private nextpnr checkout fast-forwarded from `0e2eecaf` to authoritative main `c2c01bda6d93cfe6e4d32d54e4d408995396e068`, retaining Mistral `8fcc4cb41c51f8918f1d3ad70def2febcbf20d8f`. A separate Release/HIP build supports gfx1100 and gfx1201. This is not an authenticated production toolchain slot; the core lock remains unchanged.

Relevant merged changes include [overflow routing chunks #252](https://github.com/DeanoC/nextpnr/pull/252), [QSF placement regions #263](https://github.com/DeanoC/nextpnr/pull/263) and [swapped-cell region containment #262](https://github.com/DeanoC/nextpnr/pull/262). Baseline argv changes only the compiler executable relative to the frozen parent; weight 2000, critexp 5 and seeds 1/2/3 are unchanged. All clocks and timing constraints remain unchanged: system/ROM/capture 100 MHz, pixel 74.250069 MHz. No timing-allow-fail is used.

## Native routed results

| Seed | Previous main system MHz | Current main system MHz | Pixel MHz | ROM MHz | Capture MHz | Result | Charged seconds |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| 1 | 91.208 | 89.135 | 84.367 | 102.848 | 250.376 | failed | 232.539 |
| 2 | 101.000 | 96.209 | 97.953 | 156.513 | 300.842 | failed | 220.646 |
| 3 | 88.794 | 87.773 | 109.854 | 133.772 | 270.856 | failed | 279.374 |

All current-main baseline seeds miss native system setup. Seeds 1 and 2 are limited by CPU arithmetic paths; seed 1's final path contains 3.10 ns logic and 8.12 ns routing. The previous marginal default seed 2 pass is lost. This comparison measures the combined main update; it does not isolate a particular commit as the cause. Native per-clock setup/hold summaries and analogue repair progression are retained in each record. There are no new Quartus samples or hardware acceptance claims.

## Placement-region preflight

One tentative configuration fences `system.machine.cpu` to `X24 Y1 X48 Y14` and `system.machine.board.pic_device` to `X43 Y15 X50 Y22`. The CPU rectangle includes all fixed microcode RAM sites and bounds historical CPU placement; the adjacent PIC rectangle follows the formerly passing seed 2 cluster. Current-main CPU critical paths support investigating locality, but do not prove these rectangles are optimal.

The seed 1 host-only `--no-route` preflight recognizes 2468 CPU and 283 PIC cells, then times out after 300 seconds in analytical placement without producing a legal placement checkpoint. A separate 30-second debug run of the same configuration shows repeated legalisation of framebuffer M10Ks (up to 2027 visits for one cell) and CPU registers. This is an observed failure to converge within the bound, not proof that placement is impossible or a diagnosed compiler defect. Both attempts, argv, hashes and diagnostics are retained. No region GPU route was launched and no region timing result exists. The rejected configuration was not silently widened or rerolled.

## Validation and accounting

Five analogue host tests pass; the placement-region regression and command-settings CLI test pass. The fake-broker GPU lease fixture failed under the sandbox because its inherited socket was not accepted; rerunning that same fixture outside the sandbox passed. Both logs are retained. This is focused compiler validation, not the full nextpnr suite. Parent `make check` passes (19 generated consumers and 37 fixture copies). Record validation checks five terminal records, final analogue reports, budget arithmetic and byte-identical historical evidence; `git diff --check` passes. Existing PC/XT functional tests were not rerun because no RTL, producer, lock or shared contract changed.

All routes use the shared FIFO phase-lease broker with owner `pcxt-agent`, maximum two GPUs, strict live-GPU routing and no vendor/device pin. Actual devices and queue waits are recorded. Charged work is route wall time minus only logged lease waits rounded down, retaining CPU work. The compiler's own analogue repair timer also excludes phase-lease waits. Each route reserves an 1800-second bound before admission. The host-only placement diagnostics use no GPU lease.

All 5 new records are terminal (3 routes and two host placement diagnostics). Follow-up charged work is **732.559 seconds (0.203 hours)** of its 10,800-second cap. Combined historical and follow-up usage is 19,850.974 seconds (5.514 hours). No GPU job remains running for this follow-up.

## Integration boundary

No candidate satisfies the same-recipe two-of-three timing gate. This bounded comparison stops here; unused budget is not justification for an unrelated sweep. The next useful work is diagnosing region-placement convergence or finding a proven CPU locality/logic improvement, followed by the same seed gate. Promotion still requires a fresh complete producer build and exact-artifact timing qualification. No kit was programmed, package deployed, or module/shared ABI changed.
