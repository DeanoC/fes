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

The selected-program native SDK and CPU entry now build from the committed
production sources. Complete BL31, SPL/U-Boot, composition and native file
admission remain the next gates.
TRWU8CBL remains at its separately restored original baseline Park. This change
has no physical retirement acceptance and grants no ordinary activation or
source-memory reuse.
