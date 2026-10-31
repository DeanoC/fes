# DE25 PR consolidation

Deano requested that the open DE25 development chain be consolidated into one
or two coherent PRs. The resulting stack has depth two and retains
`lane:deano` ownership:

1. [#1297](https://github.com/DeanoC/fes/pull/1297), based on main: board
   diagnostics, headless AArch64 Linux, ZX81/BASIC proofs, physical-memory
   records, HPS-first artifacts, persistent bootstrap, matching kernel modules
   and the authenticated software ABI/SDK.
2. [#1312](https://github.com/DeanoC/fes/pull/1312), based on #1297: explicitly
   selected Linux DMA scanout profile, isolated nonsecure stream, bounded
   control guard and contiguous allocation foundations. The production kernel
   probe remains closed with `EOPNOTSUPP`.

This consolidation changes no board configuration, SD contents or flash and
grants no merge or deployment approval. Normal DE10-equivalent kit operation remains the
goal, including managed MENU/catalog/runtime and core session integration.

## Preserved history and conflict decisions

The head of each superseded PR was checked as an ancestor of bring-up source
`2243c34a250867b324d6b26146fd32acdb778882`, then its GitHub metadata was fetched
again before retirement. `preserved-heads.json` records those identities.
No branch was rebased, force-pushed or deleted. Original remote branches,
dated records and review discussions remain available. A verified complete
pre-consolidation Git bundle is also retained independently at
`out/artifacts/de25-pr-consolidation/2026-10-31/pre-consolidation.bundle`, with
SHA-256 `e3b22c4093be2c45a8ef1dfc3d7bbf91372027ad2430e0e61532eb6412fec902`.

All eleven superseded heads are included: #1213, #1229, #1249, #1254, #1283,
#1288, #1292, #1303, #1305, #1309 and #1307. They are superseded by the two
PRs above; older closed DE25 PRs were not reopened.

Normal merges preserve the later external reset-domain architecture and
continuous HDMI raster. Obsolete internal pixel-reset interfaces and their
source-shape tests were not restored. The current HDMI visibility gate remains
registered and timed, with ten reviewed clear pins. The distinct Quartus-native
CDC identification and first-stage ingress constraint were retained.

The later U-Boot command interpreter and rootfs identity validation supersede
the older literal-script parser. Physical-memory evidence corrections were
retained in the dedicated evidence tests, with the combined README's checksum
updated. Runtime computer ABI minor zero remains explicit. The KUnit SDK lock
matches the packaged C++ SDK, and runtime archive accounting includes both DE25
diagnostic objects plus main's ATA object (25 members).

## Kernel refusal regression

The first combined-tree KUnit runs passed their individual assertions but were
correctly rejected by the raw kernel-log gate. The inherited refusal hook had
cleared `VM_PFNMAP` after the real DMA mapper installed special PTEs. Error-path
unmapping consequently reported `BUG: Bad page map`.

Commit `7b67bbf4711f2c11a64e20fb62381a73baad2da4` changes only this test hook:
it injects `EOPNOTSUPP` without corrupting the installed VMA/PTE semantics.
The real mapping-shape checks and new-mm-reference cleanup remain strict.
The platform branch incorporates it at
`cb7b307ef4c3baad531adf8399f0fac101a7c2eb`.
Both reruns pass the assertions and raw-log gate: 17 bring-up cases and 18
platform cases, including the failed-mapping reference regression. These are
AArch64 QEMU kernel tests with a scripted register backend, not physical DMA
acceptance. Failed and passing logs are retained with their source identities.

## Composition proof freshness

The platform's initial affected run selected 1,690 FPGA unit tests: 1,614
passed, 75 skipped and one failed. That case treated the dated 2026-10-30
combined proof as current-source evidence. Both its compiler and runtime
closures have changed since producer
`75abf6dfa9912e6c3c45c825a53f4ae1a26f2d01`. The original receipt's hashes were
verified against that producer's Git blobs; the receipt is retained unchanged.
The bring-up run likewise selected 1,678 FPGA unit tests: 1,602 passed, 75
skipped and the same source-freshness case failed. Its parent, host, runtime,
contract and four native-video/socket/session commands passed before that
failure. The six unstarted core simulations are resumed separately, preserving
the initial failed run rather than replacing its log with an aggregate pass.

The current-source test now uses the branch-local
`sources/misteross/tests/fixtures/de25-combined-proof.json` fixture. It still
requires exact equality with the live compiler and runtime source closures,
all fourteen passing results and both native and AArch64 architectures.
Independent refreshed proofs run against the consolidated bring-up and platform
producers; their dated records remain separate from the original receipt.
Both complete all fourteen cases, with identical semantic results between
native and AArch64 clients. Bring-up producer `7b67bbf47` passes in 1752.4
seconds; platform producer `cb7b307ef` passes in 1762.1 seconds. Both perform
actual ROM SAVE, two LOAD/RUN cycles producing 7, an intervening edit removed by
reload, interrupted LOAD/freeze and empty/16K/empty selection. The original
guest phase bounds and 1800-second session watchdog are unchanged.
Bring-up receipts, per-architecture logs and saved images are in
`combined-proof/`. Complete source snapshots, client/model binaries and logs
are independently retained under
`out/artifacts/de25-pr-consolidation/2026-10-31/combined-proof/`.
The combined RTL uses a selection-mux model for persona changes and behavioral
memory, so these proofs do not establish physical partial reconfiguration.

## Evidence boundaries

Initial Quartus Pro 26.3 builds on the preferred 3090 builder completed with the
original constraints and no waivers:

| Diagnostic | Producer | Build seconds | Reported timing rows | Failed signoff rules |
| --- | --- | ---: | ---: | ---: |
| Default FPGA-first scanout | `2243c34a2` | 570.1 | 74 | 7 |
| HPS-first ZX81 | `2243c34a2` | 589.2 | 46 | 5 |
| Opt-in Linux DMA (child PR) | `70f8fee9c` | 538.7 | 74 | 6 |

All reported rows have nonnegative slack and zero TNS; this does not close the
missing I/O constraints or CDC/reset signoff findings. Timing remains
**not qualified**. The per-build receipts verify all 19 report hashes and the
captured handoff/SDC hashes, and the HPS-first records verify the SOF/JIC/RBF
phase artifacts. Independently retained complete artifacts and reports are at
`out/artifacts/de25-pr-consolidation/2026-10-31/fpga/`; this directory includes
selected compressed reports and metadata rather than the programming binaries.
The build manifest in each subdirectory describes that independent complete
retention, not just the files copied into this documentation directory.

The default and Linux DMA generated-interconnect simulations both pass their
source-hash and transaction checks, using modeled M20K storage. Their receipts
name the exact generated sources. Neither is a physical DDR/SMMU/firewall test.

Concurrent commit `af83211cbb40f5207c2fdaf7f32a0462e2fd2da5` registers the
visibility request and releases its clear in the pixel domain. It is preserved
by normal merges `535631a10` and `21f50a8e2`, without rewriting either owner's
history. The existing default scanout build at that exact producer was retained
and independently verified: 528.3 seconds, 19 report hashes, 445 handoff/SDC
hashes, 74 nonnegative reported timing rows and **five failed signoff rules**.
`CDC-50001` and `RES-50001` now pass. Missing I/O delays, `RDC-50003`,
`LNT-30023` and `RES-50101` remain open. Hold rounded to zero is not positive
margin. `scanout-af832/` contains the original owner's job receipt, normalization
record, reports and source-equivalence check: only the receipt test and its
fixture differ in the FPGA module tree at the checked merge.

The opt-in profile was rebuilt separately at `21f50a8e2`: 485.6 seconds,
19 report hashes, 431 handoff/SDC hashes, 74 nonnegative reported timing rows
and **four failed signoff rules**. The updated default and opt-in generated
interconnects pass seven and eight cases respectively. Both consolidated
branches also pass the source-matched composition gate, scanout construction/
constraint checks and actual scanout RTL simulations after the merge; their
receipts are under `visibility-delta/` and the child PR's dated record.
These newer scanout results supersede the initial scanout compilation evidence
for current input bytes. The ZX81 compiler input closure is unchanged.

Review readiness is separate from merge acceptance. The broader compatibility
runner is still active on frozen bring-up source `2243c34a2`, resuming the six
unstarted core targets without repeating successful parent/host/runtime checks.
Its full C64 aggregate includes many benches and has not yet completed. The
current progress snapshot is not a passing result. CI, clean review, timing
closure and exact-artifact board acceptance are not claimed by this record.

The initial frozen consolidation producers are bring-up
`2243c34a250867b324d6b26146fd32acdb778882` (base main
`808a7b464105be5936ced07062d971e4684704e8`) and platform
`70f8fee9c6d90c2bc338eef0be38829883f63cf8` (base bring-up). The later kernel
test-hook fix changes no FPGA or shared-package source bytes. Validation
receipts identify the actual producer, rather than assigning historical
results to a later documentation or merge commit.

Earlier board records remain evidence only for the named artifacts: memory
diagnostics, Linux FPGA loading and BASIC, the persistent HPS-first bootstrap
and software reboot trials. They do not qualify the consolidated bitstreams.
Compilation and modeled interconnect tests do not establish timing closure,
physical DDR/SMMU/firewall operation or normal FES kit acceptance.
[#1284](https://github.com/DeanoC/fes/issues/1284) remains open. The new profile
still requires real device/domain lifetime protection, serialized loader and
epoch handling, managed AArch64 runtime admission and exact-artifact board
load/run/reload/stop/recovery checks.
