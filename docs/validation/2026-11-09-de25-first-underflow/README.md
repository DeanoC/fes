# DE25 first-underflow diagnostic integration

This change prepares a measurement for
[#1594](https://github.com/DeanoC/fes/issues/1594). It does not fix or identify
the physical cause of the intermittent MENU underflow. The settled FES screen
and ordinary ZX81 picture passed their earlier controls; a separate bounded
navigation trial still recorded an underflow. A correct picture does not erase
that failed trial.

## Contract and lifetime

The source starts from `53009e4ab17d64de21ae51778278c7d60f4fd2bc`.
[`fes.fabric.de25-scanout`1.2](../../../sources/mister-packages/docs/de25-scanout.md)
uses the three previously unused words at `0x74`, `0x78` and `0x7c` for the
first missed pixel's state, reader occupancy and maximum interface gaps.
Existing snapshot words and command, display-completion, ownership and drain
rules are unchanged. Legal drained CLEAR or configuration reset clears the
record; STOP, late responses, illegal CLEAR and subsequent misses cannot replace
it. The observations distinguish controlled command stalls from controlled
response stalls without diagnosing the actual native memory path.

The kernel authenticates and caches the record before a failed POLL marks the
grant uncertain. A separate version1, 48-byte DIAGNOSTICS ioctl copies only that
cache. Allocation ABI3 and the existing 104-byte INFO/POLL and 24-byte PUBLISH
layouts remain unchanged. Known transport identity loss invalidates the cache;
generic I/O failure or timeout may retain the last authenticated observation.
No diagnostic query reads MMIO or releases a reference. The runtime verifies
generation, build identity and packed fields before appending the record to the
original error. Missing, malformed or late diagnostics cannot authorize a
presentation or change recovery policy.

The producer manifests, runtime admission/capabilities, software-interface
records and agent session-display predicate select scanout1.2 together. Matching
kernel, MENU and ordinary ZX81 artifacts must be rebuilt. Old physical results
do not qualify these changed bytes.

## Focused software evidence

All evidence below is off-board. Private source bindings retain file hashes,
commands, tool identities and logs beneath
`/data/fes-de25-main-integration-20261105/`.

| Check | Result | Retained evidence |
| --- | --- | --- |
| Actual endpoint/board/guard/session/frame-ack RTL | Five simulation cases pass; endpoint includes eight diagnostic/lifecycle scenarios | `partial-start-investigation-20261108/first-fault-production-53009e4ab/` |
| Shared MENU RTL | Eleven cases pass, including per-fetch gap reset and controlled stalls | Same directory |
| First-only record across repeated misses | Separate final frame-ack pass with nonzero FIFO and pixel lane1 | Same directory |
| Pinned AArch64 kernel KUnit | 57/57 pass; no compiler or kernel warning, oops or panic | `first-fault-kernel-53009e4ab-transport-stale-fixed/` |
| Runtime unit tests | Existing lifecycle coverage and 23 new diagnostic variants pass on native Linux and pinned-SDK AArch64/QEMU | `partial-start-investigation-20261108/first-fault-integration-53009e4ab/runtime/` |

The new kernel regression failed before the focused identity-loss fix. Its nine
controlled combinations cover POLL/PUBLISH/revoke with I/O failure, timeout and
known stale transport; subsequent cache queries do not increase backend access
counts. The regression validates diagnostic invalidation, not failed-DMA
containment or physical reset safety.

An initial RTL run used the checkout's pre-existing link to a retained compiled
simulation cache. Its affected output paths and logs are recorded separately;
those compiled outputs must not be treated as unchanged retained artifacts.
Final passing suites use isolated output directories, with source hashes and
actual compile paths recorded. No source evidence or physical kit was modified
by that initial run.

## Remaining gates

The broader affected integration checks are recorded separately in
`partial-start-investigation-20261108/first-fault-integration-53009e4ab/`.
This record does not claim their result before their receipt exists.

Fresh source-bound Quartus generation, numerical timing, approved scoped
reference checks, an independent rebuild and physical acceptance remain
required. A new kernel/image trial must be prepared against exact artifacts
before changing the running kit. This work performs no board, boot FIT, QSPI,
JTAG or SD operation. Physical persona support remains default-off under the
existing #1568 waiver; #1298 containment/reset-fence qualification and #1594
MENU stability remain open.
