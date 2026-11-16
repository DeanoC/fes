# SPL checkpoint trial: no pre-barrier marker, console callback path identified

The new matched TRWU8CBL trial verifies its firmware and unchanged sealed
package on the spare SD card, then again stops after `FES_NANO_SPL HANDOFF`.
None of the new barrier, guard-entry or hash checkpoints appears before the
unchanged 50-second prompt limit expires. No `fesboot`, native SD write,
FPGA DDR request/drain or runtime core launch occurs. The
[experiment](experiment-result.json) remains failed. The
[independent audit](hardware-evidence.json) verifies its saved trace and cleanup.

Tested/compiled source: `eb52a604b37ba571bdfe0db18ca645653cb73f29`.
Native boot ID: `e13cbe9178827ba1`.
The preceding [final ATF trial](../2026-11-02-de25-atf-handoff/README.md) and its
source/artifacts remain unchanged.

## Ordered observations

The current `loaded-bl31-handoff-v2` descriptor requires five new observations
after SPL containment and before its final loaded-BL31 state/digest observation:
barrier before/after, guard entry, and hash before/after. The final `DSB SY`,
argument/state/digest rejection and later secure ownership checks remain in
place. Hash completion alone grants no authority. Missing, duplicate, malformed,
reordered and fragmented records cannot make the loader ready. A version-1
descriptor is rejected by the current composition/decoder contract.

[Compiled selection](selection-proof.json) checks actual literal arguments and
instruction order: pre-barrier output at `0x1c4c`, `DSB SY` at `0x1c50`,
post-barrier output at `0x1c5c`; guard-entry output at `0x1700` precedes the
`CurrentEL` read at `0x1704`, with hash outputs around the three raw SHA calls.
The compiled loaded-byte seal matches BL31. Six linked BL31 startup, setup,
console and capture functions retain their earlier instruction bytes and
addresses; vendor assembly remains unchanged. These are structural checks,
not observations of the running CPU's program counter.

The [matched build](native-build-result.json) rebuilds secure firmware,
SPL/U-Boot and the HPS-first composition. The unchanged SDK, native entry and
sealed package are reused only after [selected input verification](native-input-verification.json).
No new FPGA synthesis or timing qualification is claimed.

## Software and helper checks

All six [selected software scopes](software-result.json) pass. All 1,272
selected parent test IDs, with two skips, run in three intact-module
partitions capped at 300 seconds each. [Coverage](parent-coverage.json) verifies
exact selection, uniqueness, child exits and log hashes. The parent scope uses
that adapter to the planned aggregate command; the other five scopes retain
their planned commands. [Generated checks](make-check.log) cover 35 consumers
and 37 fixture copies.

The actual C tests inject a stop inside the barrier and each raw-hash call,
exercise argument/state/hash rejection, and check which progress records can
appear. The strong C/MMIO configuration fixture and
[recursive decoder check](atf-pipeline-verification.json) carry the actual
checked build descriptor through complete DDR decoding and failure cases.
These are host fixtures with synthetic boot/checkpoint prefixes, not hardware
acceptance. The runner adds [44 orchestration cases](experiment-fixtures.json),
[16 readback cases](readback-fixtures.json), six transport/clock/coverage checks
and helper compilation/global-name checks without opening devices.

The outer test wrapper overwrote the semantic summary with its own receipt.
Preflight rejected that missing status before opening a device. The wrapper
receipt is [retained](software-wrapper-result.json), and the
[reconstruction](software-summary-reconstruction.json) checks all six successful
child exits, exact commands, logs and complete parent coverage without rerunning
tests. A second off-board preflight rejected a stale partition-receipt filename;
its [snapshot](rejected-preflight.json) is retained. The corrected preflight and
helper checks pass before the physical run. No rejected preflight is counted as
a hardware attempt or used to bypass a gate.

## Bounded board result

The physical run lasts 11:25:07–11:27:32 UTC on 2026-11-02. One fresh qualified
recovery boot uploads and independently reads back the new 6,499,492-byte FIT.
A [fresh read-only package check](live-package.json) verifies all 1,645,568
unchanged package bytes against their selected SHA-256 after that upload.
Protected boot files and the recovery manifest remain unchanged. The package
is not rewritten. Both volatile recovery and native programming complete.

The [1,602-byte native trace](native-console.log) passes the cold CPU, DDR,
sealed FIT and containment checks and ends at the handoff line. No pre-barrier
marker is observed, so this result does not establish that execution reached
the final barrier, guard or BL31. No native command or active DDR test follows.
There is no retry on this source.

The original 15 MHz clock is restored and [read back](clock-restored-readback.log)
before the owned private JTAG service stops. Exact USB permissions are restored;
the independent audit checks a free serial port and continuing exclusive claim.
The spare and failed diagnostic remain on the top kit under the user's retention
policy. No factory restore, recovered prompt or claim release is claimed.

## Console path and next repair

The [console investigation](console-path-evidence.json) identifies a relevant
post-transmission callback in the actual pinned vendor UART driver. With this
SPL's device-model serial configuration and bulk puts disabled, the NS16550
character routine writes the byte, then calls `schedule()` when it is a newline.
The compiled routine has that direct scheduler call; SPL cyclic support is
enabled. The scheduler can run registered cyclic callbacks and read timer
state while servicing them. No live callback list was observed here, and the
watchdogs reported that autostart was disabled; a particular callback or
watchdog access must not be inferred from the transmitted line.

Five cases compile and execute the actual vendor character routine with injected
MMIO/callbacks. They verify ordinary and newline output, busy rejection, and
that an injected scheduler failure can leave the newline transmitted while the
character routine never returns. This reproduces the observation boundary in
a model; it does not prove the physical fault location.

The next candidate should isolate post-containment diagnostics from general
console scheduling/device callbacks, with bounded UART polling, formatting and
failure tests. It must preserve the final barrier, CPU/argument/digest checks
and complete ownership contract. Native DDR read/drain, trusted runtime
admission and real launch/return remain separate open gates; the full
runtime/core-load goal remains active.
