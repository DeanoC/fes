# RAM tester timing replay, 14 November 2026

Issue: [#1803](https://github.com/DeanoC/fes/issues/1803). These are frozen-netlist diagnostics, not production builds or sealed packages. The synthesis and constraints came from the failed #1752 build at `45c023b0edeeb130b53c5cec9841719c07212dd2`, not this worktree's base (`53681975f4d62b648ea6c49b565eb1413a53cf78`). The same authenticated nextpnr `bec82e36` binary was used for each run. `runs.jsonl` contains the full commands and environment, hashes, tool slot, backend, lease, durations and outcomes. The private Powerboat logs and outputs are at `/data/fes-ramtest-1803-campaign/`.

| Run | Seed | Weight | Critexp | Memory MHz / 100 | Pixel MHz / 74.25 | Capture MHz / 100 | Result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Retained production search | 3 | 10 | 2 | 98.87285 | 72.03573 | 314.86145 | Failed timing |
| Frozen-input replay | 3 | 10 | 2 | 98.87285 | 72.03573 | 314.86145 | Same final clocks; failed timing |
| Weight exploration | 2 | 300 | 2 | — | — | — | Stopped at campaign budget; inconclusive |

The replay used private copies of the exact synthesis, QSF and SDC, and the same nextpnr binary, seed, weight, criticality exponent and production route timeout. Input, output and private HOME paths were relocated; `gpu-run` granted an exclusive GPU and supplied its visibility and device environment. The retained production search already used `--timing-allow-fail` for diagnostic attempts; neither a signoff rule nor a seal check was changed. The replay and original final clock values match exactly. The retained failed search did not keep its routed JSON or RBF, so byte identity is not claimed.

The memory critical path in the retained seed-3 report runs from `hps_ddr.f2sdram.cmd_ready_0` through `hps_ddr.port0.slot_free` to `hps_ddr.m0_address[8].ENA`, with 7.685 ns routing of 10.114 ns total. The pixel path runs from `display.col_s[3]` through character selection to `display.sdram_ch_t.DATAIN`, with 8.558 ns routing of 13.882 ns total. Other seed reports in the former shared scratch directory can no longer be tied to these inputs because that directory has since been reused. These observations do not establish an RTL fix.

## Paired CPU-reference experiment

Using private copies of revision `45c023b0edeeb130b53c5cec9841719c07212dd2`, Yosys `3a5accbd7` and the same embedded BUILD_ID `b1649d513cdba40e08283c05e79bcbe4`, a control synthesis reproduced the retained `synth.json` SHA-256 exactly. The other synthesis replaced only `ram_display.v` with an experimental version (SHA-256 `a1d1abe1a018aa18ad48e361a4ee8435f8af5580469b6ab5ce8e000b709a6b1a`). The control and candidate were routed on Powerboat with nextpnr `bec82e36`, seed 3, weight 10 and critexp 2. Both logs confirm the CPU-reference backend; the control reproduces the earlier HIP replay's final clock values.

| Run | Memory MHz / 100 | Pixel MHz / 74.25 | Capture MHz / 100 | Route time |
| --- | ---: | ---: | ---: | ---: |
| Control | 98.87285 | 72.03573 | 314.86145 | 284.47 s |
| Display candidate | 95.31071 | 82.87064 | 343.40659 | 273.84 s |
| Display and DDR timeout lookahead | 99.52229 | 84.94012 | 244.73813 | 242.13 s |

The display candidate improves pixel timing by 10.83491 MHz but decreases memory timing by 3.56213 MHz on this seed. A subsequent combined diagnostic applied the DDR timeout-lookahead patch (SHA-256 `d12ae6cffca49477bc91d6ab1125d83820456d285d7f69498e054733f8583573`) to the original DDR file and retained the display candidate. The combined memory clock improves by 0.64944 MHz over control but remains below its requirement; pixel improves by 12.90439 MHz. The private source comparison found only the intended display and DDR files changed from control. These are single-seed diagnostics, not evidence of performance across seeds or BUILD_IDs. All three runs used `--timing-allow-fail` as in the original search; a successful process exit does not mean timing passed. The CPU-reference routes are not producer, seal or hardware acceptance. [cpu-paired-runs.jsonl](cpu-paired-runs.jsonl) records commands, exact environments, hashes and timing; the full private Powerboat outputs and receipts, including backend logs, are under `/data/fes-ramtest-1803-paired-synth/run.yfW3Sghs/`.

## Budget

The issue's four previous RAM tester builds have a conservative upper bound of 20,720 seconds: 8,290 seconds for the failed main build, 6,726 seconds for the earlier #1752 build, 2,975.831 seconds for #1788 and 2,727.529 seconds for the later #1752 build. These are full elapsed build times, including non-GPU setup and waits, not measured GPU lease time. Two old runs' lease details were not retained, so actual prior GPU use cannot be reconstructed reliably. The two runs here charged their entire GPU wrapper wall time, 375.468 and 502.533 seconds respectively. The total conservative charge is approximately 21,598 seconds against 21,600 seconds. The weight-300 run was stopped at that campaign deadline, before its normal 1,800-second route timeout, and its result is inconclusive. No further sweep was run.

At that stage no recipe change or candidate was proposed: a candidate needs at least two passing seeds out of three and a fresh production build with its own BUILD_ID, timing and seal evidence. Exact-artifact hardware acceptance would remain required for a changed RAM tester package. No kit was touched.

## Enable fanout diagnostic

A private copy of the combined CPU-reference input was routed again with the same binary, constraints, seed and placement settings, adding `--replicate-enables 4`. The pass reported zero replicas and zero ranked candidates; its eligibility checks include predicted timing and enable-net consumers, and the log does not identify which criterion excluded the guard. The final memory, pixel and capture results remained **99.52229 / 100**, **84.94012 / 74.25** and **244.73813 / 100 MHz** respectively; the timing report and diagnostic RBF hashes match the combined control. Final signoff log contains no reported hold/min-time violations. This single frozen-input diagnostic does not qualify a production recipe, seal or hardware artifact. Both original and copied input hashes matched before and after routing. [cpu-fanout-runs.jsonl](cpu-fanout-runs.jsonl) contains the exact run receipt; full private outputs are on Powerboat under `/data/fes-ramtest-1803-fanout/replicate4-_eojabnu/`. No GPU or kit was used.

## Shared guard write-data experiment

A private full-design synthesis changed the shared DDR guard so that write data updates whenever the command slot is free. This can alter idle data; it does not by itself establish the correctness of the shared handshake. The combined display and DDR timeout changes from the earlier experiment remain present. The three CPU-reference routes below use the same candidate synthesis and BUILD_ID, QSF/SDC, tool binary, weight 10 and critexp 2; only the seed changes. Each reached final signoff with no reported hold/min-time violations.

| Seed | Memory MHz / 100 | Pixel MHz / 74.25 | Capture MHz / 100 | Timing |
| ---: | ---: | ---: | ---: | --- |
| 3 | 101.15315 | 78.14942 | 257.46652 | Pass |
| 2 | 91.09128 | 77.33354 | 306.93677 | Memory fails |
| 6 | 96.93680 | 84.33125 | 350.14008 | Memory fails |

The seed-3 combined control without the shared-guard experiment reached 99.52229 / 84.94012 / 244.73813 MHz. No matched unchanged-guard routes were run for seeds 2 or 6, so those results do not establish a regression from the guard change. This candidate meets all three reported clock targets in only one of three seed diagnostics, and is not proposed for promotion. These fixed-BUILD_ID CPU-reference measurements are neither fresh production builds nor seals or kit acceptance. Full receipts are retained as separate entries in [cpu-fanout-runs.jsonl](cpu-fanout-runs.jsonl); the matching private Powerboat output directories are `/data/fes-ramtest-1803-fanout/guard-rtl-u1eolna7/`, `/data/fes-ramtest-1803-fanout/guard-seed2-3gbqj1jc/` and `/data/fes-ramtest-1803-fanout/guard-seed6-93p6gl7g/`. No GPU or kit was used.

## Banked guard and RAM tester pipeline optimization (#1832)

The next experiments use a complete source snapshot of main
`1b4601831db0399357354b3793536678df03df05`, Yosys `3a5accbd`, and the same
fixed BUILD_ID as above. They are CPU diagnostics, not production artifacts.
`cpu-structural-runs.jsonl` records complete commands, environments, source and
binary hashes, elapsed times and final analogue setup/hold results. The immutable
source snapshots and full outputs remain under
`/data/fes-ramtest-1803-structural/` on Powerboat.

The banked guard removes controller-ready from wide payload enables. A two-entry
queue preserves the original command capacity and cycle behavior while retaining
payloads in their banks. The RAM tester also decodes SDRAM display rows in
parallel, registers timeout prediction, and registers the byte-preflight to scan
boundary. Variant A retains a separate burst header. Variant C reuses the last
written bank for that header and registers latency statistic selection before
saturation and BCD conversion; this adds display preparation cycles outside the
measurement window. Variant C is selected in #1832.

| Variant | nextpnr | Seed | Weight | Memory / 100 MHz | Pixel / 74.25 MHz | Capture / 100 MHz | Result |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| A | bec82e36 | 3 | 10 | 103.60547 | 79.08890 | 254.97195 | Pass |
| A | bec82e36 | 2 | 10 | 95.63886 | 80.21819 | 254.32350 | Memory fails |
| A | bec82e36 | 6 | 10 | 100.31097 | 82.87751 | 233.64485 | Pass |
| A | bec82e36 | 3 | 300 | 82.16252 | 89.26978 | 278.86224 | Memory fails |
| C | bec82e36 | 3 | 10 | 100.54293 | 86.39309 | 299.40121 | Pass |
| C | bec82e36 | 2 | 10 | 101.45074 | 78.29627 | 201.45044 | Pass |
| C | bec82e36 | 6 | 10 | 103.57328 | 76.70477 | 302.11481 | Pass |
| C | 6162fcaa | 3 | 10 | 99.49259 | 78.09449 | 324.25421 | Memory fails |
| C | 6162fcaa | 2 | 10 | 101.48163 | 69.53619 | 278.86224 | Pixel fails |
| Unmodified main | 6162fcaa | 3 | 10 | 90.61254 | 73.42683 | 248.26218 | Memory and pixel fail |

All rows use critexp 2. The C compiler comparison uses identical synthesis,
constraints, seeds and placement settings. The newer compiler cannot qualify
2/3 after its first two failures, so its third run was stopped and excluded.
The A weight-300 probe worsens memory timing on seed 3; the remaining runs
were stopped after C qualified 3/3 with the unchanged placement settings.
The unchanged-main row is not a matched old/new compiler control. Reported
passes include positive final setup and hold slack for all three clocks.
C passes 3/3; its minimum memory margin is only 0.054 ns. This is a candidate
for production qualification, not a claim of reliability across BUILD_IDs.

Two initial banked-guard probes accidentally combined historical source with
current overlays. They were stopped after identifying that mismatch; their
intermediate timing estimates are excluded. The completed experiments above
use the full current-main snapshot. No constraint or acceptance threshold was
changed. Diagnostic routes retain `--timing-allow-fail` solely to collect the
failure reports; return code zero is not a timing pass.

Full RAM tester simulation passes for C, including injected byte-mask failures
and DDR latency/stop failures. Private Yosys state-mapped induction comparisons
of the original and candidate guard pass at 64 and 128 bits, with initially
unspecified storage set to zero and payload equality required while commands
are active. These establish modeled digital behavior, not electrical timing.
The shared consumer simulations run in PR CI; exact-artifact HIL remains due.

Deano approved up to **7,200 additional GPU-seconds** after the original
conservative incident allowance was exhausted. The CPU experiments above
consume none of that extension. Fresh authenticated production builds must
stay within that additional budget and retain their own timing and seal
receipts. No kit has been touched by this optimization campaign yet.
