# Existing queue sample observations

The [format-9 physical trial](../2026-11-13-de25-command-progress/README.md)
transfers the complete source but fails the command deadline after publication.
Its success-only receipt has no final consumer value. Exact original-FIT and
all 51 baseline commands/final Park are restored; the failed diagnostic is
consumed and retained separately.

The format-10 diagnostic now retains the last successfully returned raw register
and consumer, its read position, retirement phase, queue stage and poll index.
The queue records its existing counter samples, including a later rejected
sample, separately from its last accepted clock. Eleven stages distinguish
initial snapshots, slot storage, prepublication checks, publication, progress,
both final snapshots, the final guard and completion. None of these observation
fields participates in the driver's admission or completion decisions.

`FES_NANO_RETIRE_QUEUE` follows the existing result/timing records and precedes
retained Park. Its source/build/epoch/generation/digest binding, typed bounds,
ordered framing and existing source/retirement proofs are checked by the new
reader. A raw consumer of 12, or an otherwise valid queue observation, grants
neither completion nor activation. Raw samples can precede a failed time,
ownership or pointer check; a failed supplier cannot replace the last successful
read. The record distinguishes these observations from the success-only receipt.
Previous format-7/8/9 contract metadata cannot admit this candidate. Ordinary
Linux selection is unchanged.

All 32 [focused tests](checks.json) pass, including the existing sanitized
retirement/transfer models and new observation/parser cases. The 56 raw-read
scenarios cover consumers 6/7/9/11/12, invalid values, late command/retirement
clocks, supplier failure, owner loss, both final snapshots and the final guard.
Each failure retains memory, denies activation/reuse and forbids retry. The
actual C finish record is also parsed. Initial new-fixture expectations for the
last prepublication read and nested supplier error were corrected; failed runs
are retained and are not relabelled as passes.

Sixteen [paired joined-model scenarios](model-result.json) compare the actual
controllers against source `d50144f35e446e432fe33a630bad12308bc4b4bb`.
Every existing supplier read/write/store/barrier/counter count and modeled
outcome/stopping field matches exactly, including 25,600 table words and five
control writes in the successful case. No MMIO or counter calls are added.
The command/retirement/source origins and bounds remain 100 ms/one second/
20 seconds. This is operation-count evidence, not physical latency prediction;
observation stores still execute instructions.

Six [portable AArch64 units](portable-arm64.json), including both setup profiles,
compile with pinned tools and strict firmware flags. The largest individual
frame remains 384 bytes; this is not a whole EL3 stack or matched firmware proof.
The later bounded hardware result below remains a failed diagnostic. Physical
retirement, independent interconnect drain, source reuse and ordinary activation
remain unqualified.


## Complete matched build and affected checks

The full [affected campaign](checks.json) passes on committed source
`c6b29e7358a6261e945d9c5a37f4ac866a4aea7b`: 2,660 parent tests (15 skips),
native runtime/firmware and producer integration, and 2,181 FPGA software tests
(88 skips). It completes in 1,588.816 seconds within the host cap. Generated
consistency verifies 69 consumers and 45 fixture copies. The separate focused
and cross-build results above retain their own scope.

The [matched result](matched-result.json) and [terminal build checks](matched-checks.json)
bind a complete fresh selected-program SDK, compressed CPU entry, BL31,
SPL/U-Boot and locked FPGA composition to that production source. Full native
file admission passes. Chain
`c7520b2b78fece39db6be54e4c72bed63e50cd1f0371eeba24bfbaa9480ed810`,
boot ID `6cd9cbad7f11f633`, FIT SHA-256
`ce69c0dd29856f5ec22ce20c01d8076ad88be7d2408801df4bbdf1463f56f790`.
The 8,514,256-byte FIT stays within the 16 MiB contract. The largest selected
BL31 stack-usage record remains 928 bytes (finish), not a whole-stack bound.
The sealed FPGA payloads are unchanged. Composition holds the shared Quartus
Pro lock and performs no synthesis, fitting or device access.

The [new archive](retention-result.json) preserves 1,367 files with independent
byte/hash readback, SHA-256
`b4cd8a6f46c326917e947b4250dc1bdab449d4ffeaebe7ecafd72d47db248413`.
It includes the actual failed prototype/check logs, paired baseline/controller
sources, cross-build objects, all successful software checks and source ROM.
The first retention check stops before creating an archive: its evidence list
included the still-running collector's own log. The corrected selection excludes
that live job; its predecessor manifest/helper and failed check are retained.
Neither compiled artifacts nor passing tests changed for this repair.

Both complete unchanged sealed projects remain in the
[separate verified archive](reused-project-archive.json)
`9587828cdddfd72ab618154a1bb73bb1ccaaa7b3bbcfff8ebfb369991f4247eb`;
complete project membership and hashes are checked. Both archives are required.
The [architecture notes](architecture-notes.json) refer to the primary
[Arm SMMUv3 specification](https://documentation-service.arm.com/static/608145045e70d934bc69f149)
and remain context, not device qualification or a new drain grant.

The build results above describe the off-board candidate. No UART, JTAG, card
or network action occurred during that source/build turn. TRWU8CBL's last verified physical state
remains original native baseline final Park after the format-9 trial's exact-FIT
rollback and all 51 baseline commands; test card installed, owned handles closed,
top-only claim held. The next bounded trial must use a fresh one-use procedure,
new complete admissions and a prerequisite replay of that latest actual
four-role restoration. Previous attempts remain consumed. Standing user trial
approval persists; a failed diagnostic permits restoration-only recovery.
Memory reuse, independent interconnect drain and ordinary activation remain open.


## Physical queue observation and incomplete baseline restoration

The [actual trial](physical-result.json) consumes procedure
`780506965993f29c2ca8d1d46b7d18221f1bb38ef2323bb2df1b36b9f5493321`
once at source `34b59a186dc31049622c02a840218e2deb44de7e`, under standing human
approval. The production source and matched artifacts above are unchanged.
Seventy-nine operator cases, eight real dual-root boundaries, fresh complete
candidate/baseline admissions and actual user/root default file checks pass.
The latest actual four-role restoration replays before the 30,692-file frozen
procedure reserves its single attempt. Prepared archive
`776b5e6b41819b94d05c0bb0d4d1b96143670b1b2e131e3674341f64bf535695`
has explicit verified prior-blob reuse; no older attempt is retried.

The complete 1,708,032-byte source transfers in 11,575,038 us, with two posts/two
acknowledgements, matching SHA-384, no pending buffers and configured/proof set.
Retirement then fails phase 4/error -3/four control writes. The new source-bound
queue record validates: six slots, producer published, one poll, consumer 12,
stage 7 (`final-first`), consumer last sampled in that same stage. The last
accepted queue sample is 99,714 us from its unchanged origin; the rejected
sample is 105,704 us. The observed command phase spans 107,308 us and the
retirement interval 285,687 us.

The [interpretation](physical-analysis.json) follows the exact retained control
flow: stage 7 is reached only after progress returns target 12, pointer checks
pass and its postguard succeeds. The timeout occurs within the first mandatory
final snapshot. This narrows the failure to final validation cost; it does not
supply the missing retirement receipt, final disable, independent bus drain,
reuse or activation. All source, tables and queues remain retained.

Restoration-only recovery programming passes. Exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
rollback/readback, all 17 protected-file checks before and after, and read-only
reset gates pass. The original native baseline then records 50 received
exchanges; its final Quit/Park exchange fails with `usbcontrol: uncertain or
closed USB ownership` and firmware status 4. The same-epoch read-only failure
tail saves 108 additional bytes under its one-second bound. Final Park is absent.
This baseline failure is separate from the candidate's queue deadline, and is
not attributed to the new firmware.

All four roles clean up; independent raw boot/program/queue/rollback and host
cleanup classification passes, including a root process-FD check that UART is
free, JTAG auto/15 MHz and restored USB identity/inode/permissions. The
[terminal checks](physical-checks.json) retain the failed overall result. The
[physical archive](physical-retention.json) has independent byte/hash/mode
readback, SHA-256
`33aad38f38771f032baa5b2bcad5d7233bc9963bd8d483e292b1331ffafce19c`,
with explicit prepared-archive dependencies.

Current recorded board state: exact original FIT restored on the spare test
card, native baseline failed, reboot required, resources retained, all owned
host handles closed, top claim held. The operator has been asked for a top-only
physical power cycle with the test card left installed before new
restoration-only baseline verification. No diagnostic retry, QSPI write,
factory-card change or bottom-kit operation is authorized by this result.

The next firmware proposal is an explicit private policy that keeps the original
100 ms origin/bound through validated command consumption, while retaining all
mandatory final snapshots under the unchanged enclosing one-second retirement
bound. Arm/fault profiles would retain their existing full-operation bound.
No such policy change has been implemented or qualified in this record.
