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
No new candidate has been loaded onto hardware. Physical retirement, independent
interconnect drain, source reuse and ordinary activation remain unqualified.


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

This is a complete off-board candidate. No UART, JTAG, card or network action
occurs during this change/build turn. TRWU8CBL's last verified physical state
remains original native baseline final Park after the format-9 trial's exact-FIT
rollback and all 51 baseline commands; test card installed, owned handles closed,
top-only claim held. The next bounded trial must use a fresh one-use procedure,
new complete admissions and a prerequisite replay of that latest actual
four-role restoration. Previous attempts remain consumed. Standing user trial
approval persists; a failed diagnostic permits restoration-only recovery.
Memory reuse, independent interconnect drain and ordinary activation remain open.
