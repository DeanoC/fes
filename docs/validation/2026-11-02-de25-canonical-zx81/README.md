# Canonical DE25 ZX81 source producer, 2026-11-02

This dated record binds producer `6008eb094e53c0275cbff6b850c7826d0d35b152`.
It records off-board compilation and model checks. Neither candidate is an
admitted package or a native development-kit acceptance result.

The explicit `fes.zx81` / `de25-nano` registry row selects the new canonical
format-2 BRAM producer. All 22 existing recipe rows remain unchanged, including
the DE10 defaults and DE25 MENU. The package uses the existing computer 1.0,
fixed 720p, HID keyboard and ZX81 tape contract; it supplies no allocated
in-play display plane. Normal source production uses the public source
materializer, registered recipe and authenticated installed-tool API.

The managed BRAM profile removes the inherited DDR probe, fabric/JTAG masters,
ACE5-Lite translator and unused memory adapter. The complete generated graph
permits only the two HPS CPU bridges and peripheral relay. Both fabric memory
interfaces use documented Unused settings; the warm-reset conduit remains,
with its acknowledgement inactive. Exactly three original HPS settings may
change. Every other HPS/EMIF setting and boot byte remains checked. Firmware
pairing and native reset/load behaviour remain unqualified.

| Candidate | BUILD_ID | RBF SHA256 | Reported timing | Failed signoff categories |
| --- | --- | --- | --- | --- |
| ZX81 | `8a7006d64591ac8bbb836f4c1e9bfc56` | `e75c793d337f5b44f6130a2f6923351b3b31e5ec7f603872d60801d821248c87` | 41 pass | 5 raw, 4 unreviewed |
| MENU | `7e71288038fa6c0de4e50ac633bf7a8e` | `9ad935eb52a7707fc939b73a49b29d4dee9e008a895bfe86bd7094293deee9b3` | 69 pass | 3 raw, 2 unreviewed |

Both normal-source producer jobs complete in 611.1 seconds, including real
Pro 26.3 generation, fitting and Linux phase conversion. Each RBF matches the
independent earlier source-clone candidate byte for byte. The 19 ZX81 and 21
MENU retained SDC files also match across their respective attempts. This is
candidate reproducibility; package export remains blocked. Both producers
retain all 85 signoff rules and zero waivers. Only the two previously approved
HDMI input findings receive a fresh fitted review.

ZX81 retains TMC-20012:30, TMC-20011:2, RDC-50003:2, LNT-30023:1 and
RES-50101:3. MENU retains TMC-20012:30, TMC-20011:2 and RDC-50003:2. The
ZX81 intra-clock paths run from the top reset release into two generated
controllers, and from peripheral reset into video system release. The polarity
finding names the opposing synchronous clears of capture/arm state driven by
vblank. These require investigation and repair; no extra exception is added.
No additional PCB/trace files are available. Electrical/output/reset timing
remains open, without invented delays or lower clocks.

The shared report parser now recognises Quartus's conditional Setup Data Delay
family. It checks complete rows, duplicates and finite nonnegative slack/TNS.
All seven common timing families remain required. Negative, non-finite,
incomplete, duplicate and unknown checks remain rejected. Sixteen focused
parser tests pass. Actual generation also exposed and corrected a stale DDR
probe expectation and the exact Qsys QSF update: one duplicate old version
line retired, three declared IP references added. Raw preparation is retained
and all other project changes remain rejected.

Actual generated MENU startup (72 cases) and transport (10 cases) pass on this
source. Their selected generated RTL, QIP, input and generation bytes are bound
to these new candidates. The older 2,048-case reset study remains evidence for
its own producer; it is not new hardware or current-source qualification.

The fresh internal-RAM proof passes all 20 cases in 1,321.6 seconds;
the combined empty/16-KiB-persona proof passes all 14 cases in 1,694.9 seconds.
Each uses actual C++ native and static AArch64 clients, the latter under QEMU
Cortex-A76, against actual endpoint/machine RTL with behavioral memory.
Both clients type `1 PRINT 7`, observe ROM SAVE, load and run it, add `2 REM`,
then reload and run without reset. Both runs print 7, return to the editor,
and reload removes the edit. Each architecture records two loads, two runs,
24 editor returns and the same 923-byte SAVE image, SHA256
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
The internal session makes 1,475 writes and observes 637 complete frames.
The combined session performs three persona callbacks, interrupts the third
LOAD after nine bytes and returns to 1-KiB BASIC. Persona selection is a mux
model; no SDM programming is modeled or performed.

All 104 internal captured source files match current committed bytes; 57
also match the canonical FPGA source closure. The combined proof binds 65
compiler and 58 test/runtime files. The branch-local proof fixture is refreshed
from this actual run, and its original source-freshness unit check passes.
The original receipt remains retained, without rewriting its hashes.

Both longer Powerboat attempts reached their existing 1,800-second session
wall limit. The successful 3090 retries retain those session/guest limits.
The internal model explicitly selects `MAKEFLAGS=OPT_FAST=-O3`, matching the
combined proof, because Verilator's later `-Os` otherwise overrides its `-O2`.
Actual compiler commands verify the final `-O3`; this changes host model
compilation, without changing guest clocks, source or bounds. Native client
compilation remains `-O2`. The imported container's actual identity is recorded;
all seven filesystem layers, execution settings, both SDK compiler hashes and
QEMU compare with the original image. Rejected image-identity imports remain
preserved. No production image/toolchain policy is weakened.

The final normal parent source materialization, registry selection and
installed-tool API completes successfully in 63.6 seconds. Its entire 577-input
record equals the normal producer record, SHA256
`ab4c1edc2c09b051085b369ef59a8cd359ed5d7db43e7f86722e295f2fe2f343`.
Initial origin/path/URI comparison failures remain distinct from this pass.

[Compilation](compilation.json), [internal proof](internal-proof.json),
[combined proof](combined-proof.json), [source API](source-api.json) and
[software accounting](software.json) retain the exact identities and results.


The original 1,784-case FPGA suite is retained as failed: one stale combined
proof receipt and one six-minute machine-model timeout, with 76 skips. The
same actual timed-out test passes alone on the off-board 3090 in 498.4 seconds
with the unchanged 600-second inner bound. All 106 captured production,
test and runtime inputs compare byte for byte. The complete ordered test
inventory matches the original log. The current inventory has 1,785 cases,
adding only the conditional Setup Data Delay case. Passing unchanged cases
are reused explicitly, rather than relabelling the original aggregate.

The parent lane passes 1,385 tests with eight skips, 62 planner cases, working
generated-consumer checks and committed-source checks (40 consumers / 42
fixture copies). The final parser-only delta receives its 16 focused checks. The exact current
1,785-case inventory is accounted for as 1,709 passes and 76 skips, using the
verified unchanged cases and the two actual replacement results. This is a
source-checked union, not a fresh single aggregate pass.

Original failed attempts, generated projects, reports, fitted databases,
compiler inputs, models and source bundles are retained. The canonical archive
restores 13,469 files and passes the actual public source, graph, identity,
PLL, timing and approved-input readers. The complete proof archive restores a further 536 files, rechecks all 34
scenario results with the public readers and validates all four SAVE images,
static AArch64 binaries and SDK hashes. All 22,959 retained files are verified.
[Retention](retention.json) records the complete archive and restore identities.
All 577 ZX81 and 377 MENU source
inputs verify against an independent restored source repository. Earlier
869/775 attempts restore 2,029/6,263 files; the Powerboat software/proof
attempts restore 505 files, and the isolated model replay restores 157 files.

The ABI3 kernel/base/runtime and these new FPGA images remain uninstalled.
This increment performs no kit, capture, SD, QSPI or baseline operation. The
last recorded native health is ABI2 on 2026-11-01 at 14:48 UTC; it is not a
fresh health check. Firmware, electrical and whole-reset qualification,
admitted factory/card construction, native MENU/catalog ZX81 lifecycle,
HID/BASIC/persistence and recovery remain part of the full normal-kit goal.

The ordinary one-core `core-dev prepare` command still has no board selector;
its default remains DE10. The registered DE25 image/package API is exercised
above. Extending that developer command is the next software integration step,
alongside the remaining reset and electrical qualification.
