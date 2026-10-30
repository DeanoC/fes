# DE25 ZX81 computer with PR RAM personas, 2026-10-29

The actual HPS computer mailbox, NMOS ZX81, HID, CRC tape delivery and coherent
720p renderer now share the registered empty/16 KiB PR RAM bus. Both endpoints
report one source-bound build ID. CPU execution requires healthy transport,
mailbox RUN and PR RUNNING. HOLD/freeze registers an idle CPU bus before the
existing drain/settle acknowledgement. Selection resets/reboots BASIC.

Owner: misteross. Current base: `809e1b44b91093179b6e5e3d033ecad47c752410`
(PR #1269). The task started at `c81f37d681bc0e4d684d0ce770e471dd7462bd68`.
Normal dependency merges `d70e42a266` and `d38319471b` combine #1272/#1260 and
absorb the updated RAM-persona parent, including its 16,375-byte tape bound.
Final compiler and client producer: `c7a991110c993f69ad1a94af504679289aae4c9e`.
The actual C++ controllers, shared definitions and 24-member runtime archive
are unchanged from the current parent; runtime differences are documentation.

## Actual-client and RTL evidence

[Fourteen native/static-AArch64 cases](client-proof.json) execute both actual
C++ controllers against the actual combined endpoint/machine/persona RTL.
AArch64 runs under QEMU Cortex-A76 using the pinned SDK image and read-only
volume recorded in the receipt; the executable is static ELF64 AArch64.
Both architectures produce identical semantic results.

Each full session selects empty/RAM/fresh RAM/empty, observes CPU-written
RAMTOP 0x4400/0x8000/0x8000/0x4400, and performs a real 910-byte BASIC LOAD.
All bytes must reach the actual persona RAM with the expected address/data.
A second LOAD is frozen after **9 of 910 bytes**; writes stay stopped afterward.
Per-edge monitors bound write drain to three system edges after RUNNING falls
and require stable Frozen response/retirement outside the configuration callback.
Independent checks prove mailbox RUN cannot bypass PR freeze and PR RUNNING
cannot bypass mailbox HOLD. Selection/control/tape upload and CPU progress also
work with the pixel clock stopped.

Wrong build, wrong persona, programmer failure, uncertain register write,
missing response and later common reset must leave execution held and prevent
retry. The uncertain-write injection concerns an applied PR argument write;
it does not claim command-phase acceptance. The separate controller tests
retain their wider protocol coverage.

[Source matching](client-source-match.json) binds all 58 production source files
to the compiler selection and checks the final test/runtime sources.
[Prepared-input comparison](rtl-source-match.json) additionally matches 24
HDL/header files and all 16,384 decoded selected ROM values between the model
and generated Quartus component. The model reads the padded project-root HEX;
an unused component HEX copy retains the original unreachable FF tail.

## Compiler, timing and composition

The [compiler receipt](compiler-build.json), [descriptor](compiler-artifacts.json)
and [input matching](compiler-source-match.json) record three fresh, successful
Quartus Pro 26.3.0 Build 114 flows. All 58 captured sources and 63 prepared inputs
match. IP-XACT and generated HDL select EXTERNAL_RAM=1. Both endpoints use
`9e73d6d51866d75050e13fc8f7773680`. The exact exported root QDB remains unchanged
through both personas, and vendor-decoded POF0 region/hash matches the base.
All 46 request and 20 response registers remain protected, without inversion.
The static 1 KiB RAM, padded ROM, 177,120-bit renderer, 131,072-bit tape store
and separate 16 KiB persona map to M20K as required.

All 41 reported timing rows per revision are nonnegative and all 41 clocks are
constrained. Global minima are 1.410 ns setup, displayed 0.000 ns hold,
6.989 ns recovery, 0.090 ns removal and 0.220 ns pulse width. The rounded zero
is not positive physical margin. Registered PR boundary checks pass at all
four published corners. [Additional frame/readiness audits](frame-timing.json)
cover **136 rows per revision**, including setup/hold and local readiness-reset
recovery/removal. Metadata flight is at most 4.412 ns within its 10 ns bound.
The focused minimum hold slack is 0.005 ns; local readiness reset minima are
12.853 ns recovery and 0.167 ns removal. These are tool-model results.

Readiness now has a registered system-domain source, two pixel-domain data
stages and locally synchronized reset release. Common reset still asserts
with both clocks stopped. CDC-50001, CDC-50003, CDC-50011 and RES-50001 have
zero violations; there are no waivers. Internal Design Assistant failures now
stop the base build before either persona. A [real-report control](real-drc-gate-control.json)
rejects the earlier failing base and accepts the corrected base.

**Overall Design Closure remains FAIL**: two input and 30 output ports lack
external delays (9 input and 2,420 output path pairs). Unwaived findings also
remain: two LNT-30023, two RDC-50003, three RES-50101, and one LNT-30010 where
subsystem reset drives asynchronous clears and the PR first_seen enable.
MTBF finds 46 chains and excludes 16. No clock, seed, region or timing requirement
was relaxed. External IO, physical CDC/reset and whole-board timing remain open.

[Composition](composition-proof.json) executes the actual new base/persona RBFs
on native and emulated AArch64. Repeats and both architectures produce identical
bundle bytes for each selection, with distinct composition IDs. All 12 identity
or damaged-payload controls reject without publishing an output. The target
composer does not invoke Quartus. POF/root matching does not prove SDM execution.

## Software and retained controls

The [affected software campaign](software-checks.json) passes: parent Python,
generated consistency, host Go/UI, runtime and 1,503 FPGA producer cases
(35 environment skips); parent Python reports 1,128 cases with two skips.
That campaign started at the earlier compiler source while test-only edits
continued; it is not a frozen publication-head run. Later dependency and CDC
changes use focused checks. Eighteen HPS cases at the dependency-refresh source
`8ece36c4aa` cover the maximum LOAD ending at $7FFF. The final source uses five
combined-profile cases, 23 link-builder cases, the corrected endpoint reset and
readiness regression, native controller units, archive audit and the final
14-case combined native/AArch64 proof. `make generate` changes zero files;
`make check` verifies 37 consumers/37 fixture copies. Documentation links pass.
Raw logs identify their source and scope; final-head CI is reported on the PR.

The initial parameter inspector expected old Qsys rather than IP-XACT; the next
inspector expected a pipe rather than Quartus's generate-scope dot. Neither
candidate routed. Initial fixture checks incorrectly demanded stable persona
responses during the configuration callback; only that callback window is now
exempted. Several early client runs were deliberately stopped to bind actual
build identity and strengthen gate/in-flight assertions. Partial runs are not
passing evidence.

The first complete compiler/client candidate passed before the parent refresh;
[its receipts](pre-parent-refresh-evidence.tar.gz) and [relocated artifact identity](pre-parent-artifact-archive.json)
are retained. Its final fitter stalled in IPC after successful reports; a
verified owned worker was terminated and vendor `-resume` succeeded without
changing the fit. The current three flows complete normally without that resume.

Merging the current parent exposed the readiness/renderer integration error.
The next candidate passed all 14 digital cases but had two internal high DRC
findings: [rejection record](cdc-rejected-candidate.json) and
[raw digital/compiler evidence](cdc-rejected-candidate.tar.gz). It was stopped
before completing the RAM persona. The final registered-source/local-reset
repair clears both findings and keeps the new early rejection gate.

An added post-LOAD editor-return expectation also fails on the unchanged #1272
internal-RAM baseline: [control receipt](postload-baseline-control.json),
[follow-up #1274](https://github.com/DeanoC/fes/issues/1274). The final proof
makes no editor-return claim for that captured NEW snapshot and boots a fresh
RAM selection before its second LOAD. The separate fixture question is open.
Compressed archives retain compiler, software, composition and rejected-run logs.

## Hardware boundary and next integration step

[Archive identity](artifact-archive.json) records the local 23.4 MB artifact
archive `out/de25-zx81-pr-computer-artifacts.tar.gz` in this task worktree. It
contains the base, both personas, HPS-enriched SOF, exact root QDB, captured
source and producer receipts. Complete private outputs remain under `out/`.
The interface is `fes.de25-zx81-computer-empty-ram16k.v1`.

Evidence is off-board. RAM is behavioral in simulation; persona selection is a
mux model. The client fixture does not simulate the generated HPS interconnect
or SDM. Physical MMIO/device binding, production construction/admission, common
reset fencing, SDM execution and exact-board HDMI/DDR acceptance remain open.
The separate RAM Tester must qualify the board's other memory channels.
No kit, image, boot media, FPGA, QSPI, deployment or harness was touched.

Strict [classification relative to the refreshed PR base](hil-plan.json) is
`NO_DEPLOY_CHANGE`. The [older-baseline plan](hil-plan-older-baseline.json)
selected the inherited runtime overlay. Review and dependency integration remain
required. Board bring-up starts with the separate headless image's
serial/DHCP/SSH/reboot checks, followed by bounded physical MMIO/reset/SDM work
before this combined artifact can receive empty/16 KiB BASIC, tape and HDMI
acceptance. Existing DE10 acceptance does not transfer to DE25.
