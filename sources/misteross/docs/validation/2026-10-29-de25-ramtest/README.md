# DE25 three-memory RAM Tester — 2026-10-29

Producer `43c248d2dfbad7fa2d0ef4c72fa006fe21214dc9` completes Quartus Pro
26.3.0 Build 114 compilation with **zero errors and 211 warnings**. Its 46 STA
summary rows have nonnegative slack and TNS zero; all 73 clocks are constrained.
The new calibration-crossing CDC-50002 finding is cleared without a waiver.
Overall Design Assistant closure remains **Fail on eight unwaived classes**.
This is off-board compiler/software evidence. The board is absent, and no
memory, pin/electrical, boot-pairing or hardware acceptance is claimed.

The artifact is `golden_top.sof`, **2,585,054 bytes**, SHA256
`5b4bf7192dc58a2be0e8ba9b811fcac4aa082ede105c78231dcec8ffd06e1778`.
Its diagnostic build ID is `6ee668470911276a9e553d0025c7ef0b`.
The retained output is
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-ramtest/out/de25-ramtest-calibration-contract/`;
the SOF is beneath `hps/project/output_files/`. It is not checked into Git.
[Artifact identity](artifact.json), original [inputs](producer/inputs.json),
[generation](producer/generation.json) and [build receipt](producer/build.json)
retain the actual producer rather than relabeling it as a later evidence commit.

The destructive FPGA LPDDR4 B and two-chip SDRAM paths survive fitting.
The HPS endpoint's calibration/ready inputs remain tied low, and the JTAG
controller refuses its writes: the HPS destructive engine is **disabled**.
The separate read-only HPS probe and original debug paths remain present;
their presence does not establish Linux DMA ownership or global reset fencing.
The [operator guide](../../../boards/de25nano/ramtest/README.md) describes the
explicit diagnostic, not a package, runtime profile or programming action.

| Final fitted measure | Result |
| --- | --- |
| ALMs / dedicated registers | 26,966 / 51,350 |
| Block memory bits / RAM blocks | 2,106,944 / 138 |
| HPS / PLLs / DSP blocks | 1 / 3 / 0 |
| Declared B / SDRAM memory pins preserved | 57 / 40 |
| Setup / hold minimum slack | 2.320 / 0.000 ns |
| Recovery / removal minimum slack | 6.079 / 0.158 ns |
| Minimum pulse-width slack | 0.220 ns |
| Illegal / unconstrained internal clocks | 0 / 0 |
| Unconstrained input / output ports | 16 / 39 |

[Resources and fitted pins](resources-and-pins.json) retains the actual six
selected hierarchy rows, including the B PHY/controller, both local endpoints
and the custom SDRAM controller. All 97 declared memory locations and
`CLOCK1_50` match the prepared map. SDRAM pins use 1.8-V LVCMOS, including
CS0 AF1 and CS1 AE14; B uses the vendor 1.1-V standards and its additional
automatically created differential reference companion. No memory pin was
moved or omitted. The raw fitter summary says `356 / 351 (101%)` pins: the
pin table accounts for 315 FPGA-bank pins plus 41 dedicated HPS pins, while
351 is the sum of FPGA-bank capacities. Another 14 named SDM pins are separate.
All 370 named package locations are unique and every bank is within capacity;
the aggregate row is retained with that accounting, not normalized away.

[Source preservation](source-preservation.json) verifies all 26 captured Git
blobs against the retained snapshot and current source, copied RTL/SDC, 497
HPS module parameters across three IPs, all 91 original B memory/PHY parameters
and four retained vendor software files. The resource ZIP remains locked to
`f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.
Post-generation QSF changes are exactly the removed stale tool-version line
and nine IP registrations; [the exact delta](qsf-generation-delta.patch)
changes no pin, clock or SDC assignment. All compiler report, handoff and SDC
hashes still match the build receipt after the separate read-only timing query.

The B path's generated translator and master agent enable both read and write
responses and connect the actual AXI RRESP/BRESP nets. This is structural
evidence only: the actual generated width adapters, bridge behavior, EMIF
primitives and physical memory have not been functionally qualified here.
The original B recipe's 1066.667 MHz memory and 166.6667 MHz reference settings
are preserved; no measured rate or bandwidth claim follows.

The [timing review](timing-review.md) and [machine-readable timing record](timing-summary.json)
retain clock/coverage/rule/MTBF tables and paired path evidence. Four-corner
queries find 256 positive receiver data/enable paths: minimum setup/hold
8.095/10.106 ns for captured data and 7.195/9.153 ns for its enable. Calibration
stage0→stage1 remains timed at 9.542/0.129 ns; 3,033 unique stage1 fanout
endpoints remain timed, with 6.416/0.380 ns minima. The guarded exception
cuts only the independent 50 MHz registered calibration source to the 100 MHz
first-stage D pin. No broad clock cut, downstream cut or rule waiver is used.
The 73 found synchronizer chains include 30 in the MTBF model and exclude 43;
the tool's modeled `1e+09` years is not a whole-board MTBF result.

| Remaining rule | Severity | Unwaived count |
| --- | --- | ---: |
| TMC-20012 missing output delay | High | 40 |
| TMC-20011 missing input delay | High | 16 |
| RDC-50003 multiple reset chains | Medium | 2 groups |
| TMC-20021 partial min/max assignment | Medium | 1 |
| RES-50010 constant reset-chain outputs | Low | 90 |
| TMC-20602 endpoint tension | Low | 42 |
| LNT-30010 reset and enable mixing | Low | 2 |
| RES-50101 same-clock reset exception | Low | 2 |

The timing review distinguishes the preserved constant AXI-Lite calibration
outputs from actual reset-release chains and records the two deliberate reset
epoch guards. These source explanations do not waive the rows. The vendor B
SDC has a min-only HMC constraint; the fitter also ignores four vendor
350-ps Hyper Register Delay Chain assignments. Those implementation caveats
remain visible. Actual fitted B standards match despite redundant ignored
QSF IO-standard assignments. Ignored location assignments concern ten absent
camera pins, with none in either tested memory pin set.

[Software evidence](software-validation.json) separates 20 focused tests,
including five actual RTL models, from the **failed** local broad aggregate.
That aggregate's FPGA lane reports 1,470 tests, 35 skips and 12 failure records
across ten Make dry-run methods. Explicit Python/MAKEFLAGS propagation caused
the failures; the ten normal-PATH reruns pass, and all eleven relevant Make/test
files match the stack base. Parent, host, runtime and contract checks passed;
the failed aggregate is not relabeled. Consistency checks pass for 33 generated
consumers and 37 fixtures. The strengthened SDRAM model requires both chips'
extended-mode programming and a finite read-valid window; retiring live DQ or
sampling too late fails its negative controls. This is a logical capture-window
model, not an analogue transition or board-delay model.

[Five earlier candidates](candidate-history.json) retain their producer and
log/report/artifact hashes: two undefined-IP compilation failures, the stopped
EMRS/late-read candidate, its superseded direct-sampling replacement, and the
completed pre-contract candidate with the new CDC finding. None is the final
artifact. [SHA256SUMS](SHA256SUMS) covers the curated record; full compiler
reports, logs, source snapshots and SOFs remain in the named private outputs.

Physical acceptance still needs the identified SCS board and exact artifact,
real B calibration/memory tests, both SDRAM chips, and measured external timing,
PCB/skew/drive-load and electrical budgets. The fixed 50 MHz SDRAM controller's
refresh policy covers the documented ≤85°C envelope, not the hotter A2 policy.
Firmware/SOF/DT pairing and global reset/drain remain unqualified; HPS DMA and
production runtime admission stay closed. No board, kit, harness, flash,
boot image or deployed system was touched.
