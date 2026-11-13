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
