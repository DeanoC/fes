# PC/XT timing playbook, 19 October 2026

The `6df7e982` CGA Direct artifact was rejected by the [Quartus cross-check](https://github.com/DeanoC/fes/pull/836#issuecomment-6261235615).
[#836](https://github.com/DeanoC/fes/pull/836) remains parked and [#840](https://github.com/DeanoC/fes/issues/840) remains open. No row below is timing acceptance.

Baseline rows replay the original frozen netlist, SHA-256
`e6c5bf772f0bb6609b9b2da72d5a951228e3da95be76e347962b682ba640df5e`,
synthesized by Yosys `5391eeb1`. Named probes are private RTL or netlist
experiments; their source/transform hashes are in the records. The fresh
producer row selects committed source and its own authenticated toolchain.
The initial 62 records use nextpnr `552ba2464a8fce6ef0bc66189ef6d9f5b732ef3b`
and Mistral `8fcc4cb4`. The fresh source uses Yosys `3a5accbd`.
All keep system/ROM frequencies at 100 MHz, pixel at 74.250069 MHz and the
original QSF/SDC. The named p90/local-ROM probes change the actual ROM PLL
output phase to 2500 ps; they give address capture 2.5 ns and ROM return
7.5 ns. The private `rom-cluster-p126` probe changes the actual phase to
3500 ps, giving the address and return paths 3.5 ns and 6.5 ns respectively. This is a new unqualified clock profile, not inherited acceptance.

All GPU work uses `gpu-run`, owner `pcxt-agent`, at most two physical
leases, FIFO, without preemption. The incident budget is six GPU-hours.
1800-second route bounds are reserved before admission. Baseline times
include the entire leased process. After a phase-leased run ends, its budget
upper bound subtracts only logged lease waits, rounded down, from route wall
time; all CPU work remains charged. Each raw receipt SHA and calculation is
recorded without modifying the raw receipt. Synthesis is outside GPU leases.
All recorded routes are terminal; no GPU work remains running.

| Candidate | Seed | Weight | Critexp | Flags | Status | System MHz | Pixel MHz | ROM MHz | Capture MHz | Budget seconds |
| --- | ---: | ---: | ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 1 | 2000 | 5 | default | failed | 98.04 | 93.65 | 104.98 | — | 117.1 |
| baseline | 2 | 2000 | 5 | default | failed | 89.73 | 74.45 | 100.18 | — | 297.4 |
| baseline | 3 | 2000 | 5 | default | failed | 98.39 | 83.38 | 130.41 | — | 116.8 |
| baseline | 1 | 2000 | 2 | default | failed | 93.02 | 101.43 | 110.08 | — | 238.1 |
| baseline | 2 | 2000 | 2 | default | failed | 95.40 | 97.40 | 142.94 | — | 260.0 |
| baseline | 3 | 2000 | 2 | default | failed | 86.53 | 78.53 | 110.94 | — | 326.4 |
| baseline | 1 | 300 | 5 | default | failed | 94.13 | 73.66 | 107.69 | — | 309.7 |
| baseline | 2 | 300 | 5 | default | failed | 94.34 | 84.46 | 122.19 | — | 306.5 |
| baseline | 3 | 300 | 5 | default | completed | 100.47 | 76.80 | 106.95 | — | 123.0 |
| baseline | 1 | 300 | 2 | default | failed | 94.22 | 99.57 | 136.65 | — | 340.0 |
| baseline | 2 | 300 | 2 | default | failed | 96.26 | 101.44 | 131.96 | — | 332.2 |
| logic | 1 | 2000 | 5 | default | failed | 96.98 | 92.96 | 116.77 | — | 232.3 |
| baseline | 3 | 300 | 2 | default | failed | 94.42 | 82.39 | 110.42 | — | 236.1 |
| decode | 1 | 2000 | 5 | default | failed | 94.44 | 81.64 | 89.96 | — | 181.1 |
| baseline | 1 | 1000 | 5 | default | failed | 90.16 | 76.85 | 111.01 | — | 274.6 |
| baseline | 2 | 1000 | 5 | default | failed | 95.99 | 84.57 | 98.70 | — | 282.0 |
| decode-p90 | 1 | 2000 | 5 | default | failed | 52.09 | 77.69 | 137.06 | 261.64 | 338.9 |
| baseline | 3 | 1000 | 5 | default | failed | 98.42 | 79.38 | 114.31 | — | 216.5 |
| baseline | 1 | 1000 | 2 | default | failed | 90.66 | 86.42 | 134.19 | — | 229.0 |
| baseline | 2 | 1000 | 2 | default | failed | 91.56 | 95.79 | 134.48 | — | 451.2 |
| baseline | 3 | 1000 | 2 | default | failed | 96.36 | 102.26 | 122.58 | — | 426.4 |
| baseline | 2 | 300 | 5 | arrival-pin-assignment | failed | 97.45 | 81.61 | 122.79 | — | 233.5 |
| baseline | 1 | 300 | 5 | arrival-pin-assignment | failed | 95.17 | 81.71 | 107.39 | — | 287.4 |
| baseline | 3 | 300 | 5 | arrival-pin-assignment | completed | 101.76 | 79.42 | 106.70 | — | 95.5 |
| baseline | 1 | 300 | 5 | isolated-lut-placement | failed | 99.76 | 81.35 | 107.37 | — | 253.1 |
| baseline | 2 | 300 | 5 | isolated-lut-placement | failed | 95.69 | 79.31 | 120.22 | — | 296.0 |
| baseline | 3 | 300 | 5 | isolated-lut-placement | failed | 95.93 | 75.12 | 117.21 | — | 521.5 |
| baseline | 1 | 300 | 5 | arrival-pin-assignment,isolated-lut-placement | failed | 95.97 | 81.63 | 106.47 | — | 240.2 |
| baseline | 2 | 300 | 5 | arrival-pin-assignment,isolated-lut-placement | failed | 98.13 | 79.84 | 119.45 | — | 221.0 |
| baseline | 3 | 300 | 5 | arrival-pin-assignment,isolated-lut-placement | failed | 94.56 | 72.26 | 104.06 | — | 508.9 |
| baseline | 1 | 2000 | 5 | arrival-pin-assignment | failed | 98.41 | 99.89 | 106.41 | — | 147.8 |
| baseline | 2 | 2000 | 5 | arrival-pin-assignment | failed | 90.49 | 73.04 | 85.22 | — | 299.5 |
| baseline | 3 | 2000 | 5 | arrival-pin-assignment | failed | 99.74 | 80.28 | 125.41 | — | 169.6 |
| baseline | 1 | 2000 | 5 | isolated-lut-placement | failed | 99.24 | 73.29 | 104.78 | — | 185.5 |
| rom-cluster | 1 | 2000 | 5 | default | operator-stopped-unqualified | — | — | — | — | 482.3 |
| baseline | 2 | 2000 | 5 | isolated-lut-placement | failed | 90.89 | 89.81 | 118.37 | — | 255.7 |
| baseline | 3 | 2000 | 5 | isolated-lut-placement | failed | 92.07 | 80.08 | 110.16 | — | 240.6 |
| baseline | 1 | 2000 | 5 | arrival-pin-assignment,isolated-lut-placement | failed | 98.87 | 78.87 | 109.43 | — | 180.0 |
| rom-cluster-p126 | 1 | 2000 | 5 | default | failed | 91.83 | 103.70 | 150.60 | 301.02 | 354.8 |
| baseline | 2 | 2000 | 5 | arrival-pin-assignment,isolated-lut-placement | failed | 90.57 | 90.59 | 117.37 | — | 360.1 |
| baseline | 3 | 2000 | 5 | arrival-pin-assignment,isolated-lut-placement | failed | 93.32 | 86.42 | 122.28 | — | 384.2 |
| pic-feedback-cluster-p126 | 1 | 2000 | 5 | default | failed | 91.21 | 84.95 | 137.80 | 250.38 | 344.4 |
| pic-feedback-cluster-p126 | 2 | 2000 | 5 | default | completed | 100.03 | 98.02 | 150.22 | 300.84 | 230.9 |
| pic-feedback-cluster-p126 | 3 | 2000 | 5 | default | failed | 88.79 | 112.50 | 152.19 | 270.86 | 382.4 |
| pic-feedback-cluster-p126 | 2 | 300 | 5 | default | failed | 92.30 | 79.69 | 167.57 | 297.09 | 426.5 |
| pic-feedback-cluster-p126 | 3 | 300 | 5 | default | failed | 96.19 | 78.27 | 149.32 | 288.85 | 429.3 |
| pic-feedback-cluster-p126 | 1 | 300 | 5 | default | failed | 97.06 | 74.79 | 165.23 | 426.99 | 469.2 |
| pic-register-anchor-p126 | 1 | 2000 | 5 | default | failed | 88.38 | 103.52 | 136.07 | 250.25 | 276.7 |
| pic-feedback-cluster-p126 | 2 | 1000 | 5 | default | failed | 93.70 | 100.08 | 162.22 | 243.31 | 410.6 |
| pic-feedback-cluster-p126 | 1 | 1000 | 5 | default | failed | 99.58 | 107.61 | 153.48 | 401.61 | 415.5 |
| pic-feedback-cluster-p126 | 3 | 1000 | 5 | default | failed | 98.59 | 83.16 | 141.86 | 277.32 | 410.1 |
| pic-feedback-cluster-p126 | 1 | 2000 | 5 | arrival-pin-assignment | failed | 90.16 | 89.77 | 127.43 | 250.38 | 382.3 |
| pic-feedback-cluster-p126 | 2 | 2000 | 5 | arrival-pin-assignment | failed | 93.58 | 95.59 | 152.47 | 300.84 | 422.9 |
| pic-feedback-cluster-p126 | 3 | 2000 | 5 | arrival-pin-assignment | failed | 90.01 | 101.53 | 148.57 | 270.86 | 389.9 |
| pic-feedback-cluster-p126 | 1 | 2000 | 5 | isolated-lut-placement | completed | 100.26 | 105.13 | 148.03 | 326.58 | 126.3 |
| pic-feedback-cluster-p126 | 2 | 2000 | 5 | isolated-lut-placement | failed | 88.45 | 103.49 | 135.22 | 258.26 | 213.8 |
| pic-feedback-cluster-p126 | 3 | 2000 | 5 | isolated-lut-placement | failed | 93.38 | 103.19 | 143.08 | 317.86 | 156.4 |
| pic-carry-cluster-p126 | 1 | 2000 | 5 | default | failed | 85.08 | 81.89 | 157.65 | 401.61 | 266.9 |
| pic-carry-cluster-p126 | 2 | 2000 | 5 | default | failed | 97.67 | 76.09 | 152.15 | 278.40 | 262.5 |
| pic-mixed-cluster-p126 | 1 | 2000 | 5 | default | failed | 90.84 | 73.63 | 128.33 | 462.53 | 317.2 |
| pic-mixed-cluster-p126 | 2 | 2000 | 5 | default | failed | 92.23 | 95.27 | 120.73 | 247.89 | 284.3 |
| fresh-37177c9c | 1 | 2000 | 5 | default | operator-stopped-unqualified | — | — | — | — | 531.7 |

Final conservative charged budget: 5.139 GPU-hours.

## Quartus checks

Both baseline native passes (weight 300, critexp 5, seed 3, default and
arrival pin assignment) fail the sampled Quartus ROM return check. No
combination passed two of three seeds. TimeQuest launch/setup cells
and its uncertainty replace the native cells; native clock skew is retained.
These are sampled-path upper bounds, not whole-design STA. Quartus hold is
not checked. Decoded replica data-hop chains match the native routes.

| Run | Path | Replica | Corner | MHz including uncertainty | Slack ns including uncertainty |
| --- | ---: | --- | --- | ---: | ---: |
| 20261019T065925Z-s3-w300-c5 | 0 | quartus-path-0 | slow_1100mv_100c | 75.31 | -1.639 |
| 20261019T065925Z-s3-w300-c5 | 0 | quartus-path-0 | slow_1100mv_-40c | 76.88 | -1.504 |
| 20261019T065925Z-s3-w300-c5 | 0 | quartus-path-0-clock1 | slow_1100mv_100c | 75.45 | -1.627 |
| 20261019T065925Z-s3-w300-c5 | 0 | quartus-path-0-clock1 | slow_1100mv_-40c | 77.03 | -1.491 |
| 20261019T065925Z-s3-w300-c5 | 1 | quartus-path-1 | slow_1100mv_100c | 73.53 | -0.132 |
| 20261019T065925Z-s3-w300-c5 | 1 | quartus-path-1 | slow_1100mv_-40c | 75.19 | +0.168 |
| 20261019T065925Z-s3-w300-c5 | 2 | quartus-path-2 | slow_1100mv_100c | 100.84 | +0.083 |
| 20261019T065925Z-s3-w300-c5 | 2 | quartus-path-2 | slow_1100mv_-40c | 105.69 | +0.538 |
| 20261019T074627Z-s3-w300-c5-arrival | 0 | quartus-path-0 | slow_1100mv_100c | 75.31 | -1.639 |
| 20261019T074627Z-s3-w300-c5-arrival | 0 | quartus-path-0 | slow_1100mv_-40c | 76.88 | -1.504 |
| 20261019T085125Z-probe-pic-feedback-cluster-p126-s2-w2000-c5 | 0 | quartus-path-0-clock1-ff-a3 | slow_1100mv_100c | 109.41 | +0.559 |
| 20261019T085125Z-probe-pic-feedback-cluster-p126-s2-w2000-c5 | 0 | quartus-path-0-clock1-ff-a3 | slow_1100mv_-40c | 110.00 | +0.591 |
| 20261019T085125Z-probe-pic-feedback-cluster-p126-s2-w2000-c5 | 2 | quartus-path-2 | slow_1100mv_100c | 98.39 | +3.304 |
| 20261019T085125Z-probe-pic-feedback-cluster-p126-s2-w2000-c5 | 2 | quartus-path-2 | slow_1100mv_-40c | 104.61 | +3.909 |
| 20261019T085125Z-probe-pic-feedback-cluster-p126-s2-w2000-c5 | 3 | quartus-path-3 | slow_1100mv_100c | 99.76 | -0.024 |
| 20261019T085125Z-probe-pic-feedback-cluster-p126-s2-w2000-c5 | 3 | quartus-path-3 | slow_1100mv_-40c | 105.57 | +0.528 |
| 20261019T094042Z-probe-pic-feedback-cluster-p126-s1-w2000-c5-isolated | 0 | quartus-path-0-clock1-tree | slow_1100mv_100c | 112.98 | +0.747 |
| 20261019T094042Z-probe-pic-feedback-cluster-p126-s1-w2000-c5-isolated | 0 | quartus-path-0-clock1-tree | slow_1100mv_-40c | 114.36 | +0.816 |
| 20261019T094042Z-probe-pic-feedback-cluster-p126-s1-w2000-c5-isolated | 2 | quartus-path-2 | slow_1100mv_100c | 106.96 | +4.119 |
| 20261019T094042Z-probe-pic-feedback-cluster-p126-s1-w2000-c5-isolated | 2 | quartus-path-2 | slow_1100mv_-40c | 113.13 | +4.629 |
| 20261019T094042Z-probe-pic-feedback-cluster-p126-s1-w2000-c5-isolated | 3 | quartus-path-3-carry | slow_1100mv_100c | 100.57 | +0.057 |
| 20261019T094042Z-probe-pic-feedback-cluster-p126-s1-w2000-c5-isolated | 3 | quartus-path-3-carry | slow_1100mv_-40c | 104.69 | +0.448 |

The native dual-port M10K read clock uses internal clock1. A separate replica
with independent write and read input clocks prevents Quartus merging them;
it retains the same data chains. Its Slow 100 C launch is 2.576 ns versus
2.581 ns with clock0 in the original helper, so this selection does not
explain the native timer's roughly 1.004 ns launch estimate.

A real M10K capture replica of the private `decode-p90` address path preserves
both data chains and explicitly uses clock1. With TimeQuest cells and
uncertainty, the sampled path misses its 2.5 ns window by 2.385 ns at Slow
100 C. The original helper's FF replacement is not used for this endpoint.
The first two capture attempts remain in the local diagnostic directory;
the successful third attempt and chain checks are embedded in `runs.jsonl`.

The unclustered fresh producer and the superseded p90 cluster were explicitly
operator-stopped after their analogue checks remained below target. Their
progress logs are retained; they have no final timing or bitstream acceptance.

No hardware was programmed, no package was sealed, and no acceptance was
inherited from the rejected artifact. Raw diagnostic directories are local;
the committed JSONL preserves commands, source/tool/artifact hashes, terminal
states, per-clock results and the extracted Quartus checks.

## Initial campaign stop, before the requested main comparison

**Timing is not closed.** There are 62 terminal route records. Conservative
charged work is 18,502.155 seconds (5.140 GPU-hours), leaving 3,097.845 seconds
under the six-hour incident cap. No candidate meets the required two-of-three
seed gate. The remaining budget cannot reserve even two 1800-second route
bounds, before a fresh producer confirmation. Search stops here under root
`AGENTS.md`, timing playbook rules 4, 6 and 7. Further sweeping needs Deano's
explicit authorization; no production recipe or private candidate is promoted.
The committed diagnostic lock is experimental and has no timing acceptance.

The best private structure combines the proven PIC service shortcut, sixteen
local ROM placement constraints and an actual 3500 ps ROM PLL phase. With
isolated LUT placement, seed 1 passes native internal setup/hold and sampled
Quartus system/pixel/ROM checks at both slow corners. Slow 100 C sampled slacks,
including uncertainty, are +0.057 / +4.119 / +0.747 ns. Seeds 2 and 3 fail
native system timing, so this is **one useful artifact, not a qualified recipe**.
The default variant's seed 2 passes native timing but misses sampled Quartus
system timing by 0.024 ns. All-carry and mixed PIC variants each fail their
first two seeds; their third seeds are deliberately omitted because two passes
are then impossible. No additional candidate RTL or 3500 ps profile is adopted.

Quartus replicas with malformed duplicate RCF labels were rejected despite
successful compilation. Only chain-verified samples above count as evidence.
For the valid carry sample, the decoded replica also confirms each constrained
carry ALM is configured as ADDER. A full-RBF analogue decode timed out; its
partial output is unused. The filtered decoder keeps the entire active routing
network and loads and filters only reported destinations; two selected rows
match an earlier complete decode byte for byte. All samples retain native clock
skew, do not establish whole-design STA and do not check Quartus hold.

## Source and functional validation

The functional checkpoint is `37177c9c92f9a1e08277c98a161a8e1ad8c88ab8`,
with main through `40fbe0907e8ffa8ea7326eef7bc6a98eb84c2ffb`.
The final follow-up changes only this evidence and the core README's stale
timing claim. Existing main module/shared contracts and guest cycle latency
are unchanged by the follow-up. No hardware, sealed artifact, physical ISA,
BIOS/DOS or game acceptance is claimed.

- All 15 PC/XT host simulation suites pass, covering CGA/MDA, Direct/Scanlines,
  DQM delays 0/1/2, DMA, refresh, reset and HOLD.
- Eight focused CPU checks pass, including immutable microcode reconstruction,
  operand/jump bindings, prefetch equivalence, 66-bit PC/stack/stall equivalence,
  local address invariance and registered read ordering at 90/180/270 degrees.
  These are compositional checks, not whole-CPU equivalence.
- All 22 focused locked-tool checks, 15 common lease regressions and 1,238 full
  producer host tests pass (27 optional skips). `make check` passes for 19
  consumers and 37 fixtures.
- All 33 commands selected by `make test-changed --base 40fbe090` pass across
  the initial run and unchanged retry/continuation. The original command
  stopped on a runtime ST geometry media insertion assertion; the runtime
  and shared sources are identical to the base. The unchanged unit retry,
  full runtime suite retry, and remaining 25 canonical selected commands pass.
  This is not a claim that the original command exited successfully.

The first JSONL record embeds a restart bundle of the private candidate,
proof, replay, accounting and Quartus helper text with hashes. Each relevant
record includes private candidate RTL and exact Quartus replica input text.
These are diagnostic data, never automatically installed or executed.
Raw netlists, bitstreams and full logs remain in the recorded local paths;
the hashes alone do not make those large artifacts remotely available.
To resume elsewhere, rebuild the source checkpoint with its authenticated lock,
recover private input text into a separate directory, and adjust recorded local
paths deliberately. Do not replay a different netlist as the original artifact.
A new search needs an authorized incident budget and must achieve two-of-three
passes, then a fresh full producer build and exact-artifact Quartus qualification
at unchanged frequencies. Hardware acceptance remains a later leased-kit step.

## Requested nextpnr main comparison

After the initial stop, Deano requested incorporating recent nextpnr main
improvements. The private clean compiler checkout fast-forwarded from
`552ba246` to `0e2eecaff44533c31fbaea5220ca2ecaf1107279`, retaining
Mistral `8fcc4cb4`. The new compiler is a private Release/HIP diagnostic build,
not a promoted authenticated toolchain slot. The core lock remains unchanged.
[nextpnr #234](https://github.com/DeanoC/nextpnr/pull/234) adds analogue
improvement after native signoff has passed; it leaves failing routes alone.
The other new commit affects GateMate CI only.

Each replay copies its retained `synth.json` byte for byte and changes only the
compiler executable in argv. No RTL, placement, phase, clock constraint, seed or
router setting is changed. Five focused host tests pass, including all three
new analogue-improvement tests. The full nextpnr test suite was not rerun.

| Recipe / seed | Native system MHz, previous → main | Main pixel / ROM / capture MHz | Main result | RBF identical to previous |
| --- | ---: | --- | --- | --- |
| default / 2 | 100.030 → 101.000 | 98.020 / 150.220 / 300.842 | completed | False |
| isolated / 1 | 100.261 → 100.261 | 105.130 / 148.030 / 326.584 | completed | False |
| default / 1 | 91.208 → 91.208 | 84.955 / 137.799 / 250.376 | failed | True |
| default / 3 | 88.794 → 88.794 | 112.499 / 152.189 / 270.856 | failed | True |

Default seed 2 gains native system setup margin from 3 ps to 99 ps. Its
sampled Quartus system path improves from −24 ps to **+46 ps** at Slow 100 C,
including uncertainty; pixel and ROM remain +3.304 ns and +0.559 ns.
All three samples also pass Slow −40 C. Isolated seed 1 retains its native
frequencies but changes its routing. Fresh chain-verified Quartus replicas of
those new bytes pass too: system/pixel/ROM are +0.057 / +4.119 / +0.747 ns at
Slow 100 C, and all three pass Slow −40 C. Its carry ALM modes and sites are
also verified. These are sampled paths retaining native clock skew, not
whole-design STA; Quartus hold is not checked.

The default recipe still passes **only one of three seeds**. Its failed seeds
1 and 3 produce bit-identical RBFs to the previous compiler and never enter
the improvement stage. The isolated result is a different recipe, so its
passing seed cannot be combined with default seed 2 to satisfy the gate.
No private recipe or source is promoted, and no fresh producer qualification
or hardware run is claimed. #836 remains parked and #840 remains open.

All 66 records are terminal. Total conservative charged work is 19,118.415 seconds (5.311 GPU-hours), leaving 2,481.585 seconds under the original six-hour cap. This follow-up did not increase the cap. The next integration step remains a repeatable structural/recipe candidate followed by a fresh producer build and exact-artifact qualification.
