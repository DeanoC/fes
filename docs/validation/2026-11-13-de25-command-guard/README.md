# Retirement command ownership checks

The format-8 input diagnostic removes two duplicate whole-platform ownership
observations per retirement queue guard. The queue still samples its counter
before the ownership view and retains the original 100 ms command bound.
The view checks live retirement ownership/time before and after its unchanged
read-only register batch. The counter callback checks the current supplier
table, monotonicity and original retirement/source deadlines before returning.
There is no cached hardware permission, timer rebase or timeout increase.

The actual command queue, table validator and strong platform supplier remain
unchanged. Each slot store and control/publication write retains its existing
guards; all five 5,120-word table scans remain. The new profile cannot accept
the preceding format-7 image's metadata.

The [joined cost model](model-result.json) reduces command-phase supplier reads
from 7,996 to 5,514 (31.04%). Counter calls fall from 366 to 293. Operation counts
in all six other retirement phases remain exactly equal. The injected 16 us
read cost now completes retirement where format 7 failed; 32 us reads and a
slow counter still fail. These injected costs do not predict Nano latency.
The actual format-7 hardware failure remains failed and separate.

The [focused checks](checks.json) pass all seven retirement groups and ten
profile/observation tests. Fourteen added first-command-counter cases reject
supplier substitution, ownership loss, wrong phase/binding, clock failure,
backward time and expired retirement time before another CSR read, command
slot store or publication. The original code fails the new callback-table
substitution regression under UBSan; the repaired code passes it. Every failed
case retains memory and forbids retry, reuse and activation.

The [AArch64 cross-build](portable-arm64.json) compiles all five portable units
with the pinned compiler and strict firmware flags. Its largest individual
frame remains 368 bytes. This is not a complete BL31 or whole EL3 stack proof.
Current FPGA validators separately accept copied, byte-checked computer and
menu projects; this performs no fitting or board operation.

The broad affected check completes its parent partition with 2,649 tests and
15 skips, plus the runtime/firmware/RTL and producer integration commands.
Its 2,181-test FPGA software suite records one cleanup-test error and 88 skips:
an already exiting compiler worker disappears between the `/proc` existence
check and read, raising `ProcessLookupError`. The test now accepts that same
exited-worker condition alongside `FileNotFoundError`; all nine bounded-compiler
tests pass afterward. Compiler behavior is unchanged. The original broad-run
failure remains recorded; this is a targeted repair and recheck of its only
failing module, not a rerun or relabelling of the original result.

## Complete matched candidate

The [matched result](matched-result.json) passes the selected-program native
SDK, compressed CPU entry, complete BL31, SPL/U-Boot, FPGA composition and full
fresh native file admission. The [commands](matched-checks.json) retain their
terminal results. Its chain is
`6e09314b674d562facdc5d60c580b8fd3fb82dcbf7a5cdcdcb9d2246f611a822`,
boot ID `410c8ad262a6b3d8`, FIT SHA-256
`60e3b255ca02a4f5462d6ccc75a74750fe9efb9fdf05014f132799b09242ea33`.
The FIT is 8,514,256 bytes within the unchanged 16 MiB contract. PFG composition
runs under the shared Quartus lock and preserves the exact sealed core payload;
there is no synthesis, fitting or device access.

The [new archive](retention-result.json) independently reads back 799 new
source/artifact/receipt/evidence files, SHA-256
`2519fe474b1f1be783c4c4bb443e1dca63fdb91de68038371ac1ce7be62013e0`.
The [reused archive](reused-project-archive.json) retains both complete sealed
project trees; membership and every hash are rechecked, along with its archive
bytes. Both archives are required to reproduce this context. The complete
native file/tree snapshot still matches after retention.

The SDK and entry build at production source `5ef4a7f25`; the later cleanup-test
repair changes no selected runtime/firmware source. The complete verification
binds source `2310ff5ffceda292d1d7f1f3fde019a3e3ef883d` and rechecks all producer
source maps against the actual files. The later bounded trial checks this exact
candidate; it does not establish ordinary runtime activation or memory reuse.

## Bounded physical trial and restoration

The [physical result](hardware-result.json) independently replays procedure
`b9b5dd09388b86fc89c5462bfd260da1cd8fd4069a23581f5a913f6996fd7099`,
executed once at source `3caa3e3cfd870b109516dddba6a87c847a4c4133`
under standing human trial approval. The [procedure evidence](procedure-evidence.json)
records full candidate/baseline admissions, 72 operator fixture tests, eight real
dual-root rejection/reload checks, user/root default preflights and retained
frozen inputs. Source transfer passes all 1,708,032 bytes in two posts and two
acknowledgements, in 11,666,020 us, with the expected SHA-384.

Retirement still fails in phase 4 with command error `-3`. Four control writes
are observed, so the producer publication is reached. The command phase is
observed for 106,877 us against its unchanged 100,000 us bound; the complete
retirement observation is 286,203 us. This is progress beyond format 7's stop
before publication, but it proves neither queue completion nor translation
retirement. Source memory, tables and queues remain retained; there is no reuse
or activation grant. The two attempts do different amounts of work, so their
whole-stage times are not a matched latency comparison.

The rollback recovery programmer then reports `JTAG chain broken`. Its UART is
closed with no recovery prompt or SD write commands. The attempt stops without
retry, leaving the candidate FIT on the spare card. All three transport roles
clean up their owned processes, UART, JTAG clock and USB permissions. The
[failure archive](hardware-retention.json) independently reads back as SHA-256
`d60dc7e3249f83cfca1cadfc2fd529f903212fd335e5e1b2caea8d0d730a8e33`.
An offline collector initially used unavailable `fuser`; that failure and script
are retained. Its corrected `/proc` descriptor scan passes without rerunning
hardware.

After a separate human-confirmed physical reset, restoration-only procedure
`f5076e75e481df2ed1887bfd4703ddbff3dbe8ff8b8191404556e392d8d0cabe`
passes recovery, the read-only SMMU reset gate, exact original-FIT rollback and
17 protected-file readbacks both before and after the write. All 13 staged
sources and three installed records are preserved. The [restoration result](restoration-result.json)
independently verifies original boot, all 51 ZX81/ZX80 launch/return commands,
final Park at generation 5, raw programming/UART witnesses and complete host
cleanup. The [restoration archive](restoration-retention.json) independently reads
back as `af5ee64d9f8277fa23926009854d0e18aa95ec59e49a4c5256a7f6866acc55dd`.
Restoration scope checks also catch a copied diagnostic-only worker-context
requirement before hardware; the corrected context passes seven tests and a
fresh file admission. The preceding admission and failed offline checks remain
retained. No candidate diagnostic is retried.

TRWU8CBL is left at the original native baseline's final Park with its test card
installed and all owned handles closed. QSPI, the factory card and bottom kit
are untouched. The baseline restoration does not qualify format-8 retirement,
ordinary activation, source-memory reuse, warm recovery or general core loading.
The next firmware work is to measure and reduce repeated read-only queue-progress
observations while preserving the existing ownership/write gates and deadlines.
Consumer completion remains unproven; a new source-bound candidate and procedure
would be required for another diagnostic.

## CI boundary

[CI run 43428861389](ci-result.json) passes every job except the media partition.
`TestTrialStorageRefreshReopensHeldBackFactory` in the Linux `kitsoftware`
package fails with `bootstrap agent unexpectedly opened durable packages <nil>`.
That package is unchanged from base `daac138c7c9eb5d40009314fd8dbd797dffd2c3f`.
The failure is recorded without waiver or a flaky-test diagnosis; CI is not green.
