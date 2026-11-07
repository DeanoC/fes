# Explicit repair of the consumed UART0 trace image

The [corrected UART1 build](../2026-11-07-de25-recovery-console-fix/README.md)
now has an explicit UART-only repair transition from the exact image last
verified on TRWU8CBL's spare card. This is software evidence. No UART/JTAG,
SD-card or board operation was performed in this milestone.

Implementation: `03b8ec56d01cbf5ea64184c1987058228fa0ddf6`.
Compiled diagnostic source: `ca87b32780372567507f17d9b765087fecdde69d`.

## Contract and preservation

`recovery_trace_upgrade.Repair` shares the existing one-shot controller's
ownership, boot/session, LMB, UART, write/readback and terminal-retirement
checks. It separately pins the consumed v1 FIT, complete manifest and protected
prefix. The original `Upgrade` still accepts only the original PHY-prepare
baseline; changing its baseline pin is not part of this repair.

Repair requires the exact original R1107A FIT/manifest pair. It reads and hashes
both before any write and again after completion. Its five fixed writes use
fresh R1107B backup names followed by the corrected FAT FIT, raw FIT mirror and
manifest. Existing R1107A rollback files are outside its write allowlist.
The current package and experimental FIT remain protected and are checked at
completion. Caller-owned input bytes/maps and rollback bytes are snapshotted.
Any error retires the controller. No automatic activation, reset, retry or
rollback activation is supplied. An uncertain partial write remains uncertain.

The new FIT SHA-256 is `ccb6a597052c69f982713c327eeaab02adda4bd8ef21905deed2db7e7d47f353`. The prepared plan freezes three
actual UART payloads, all five target sizes/hashes, original rollback identities,
the 8,198,704-byte scratch owner and unchanged 150/5/330-second transfer/control/
repair bounds. Its regular-file comparison permits only the fixed FIT mirror
to differ in the protected prefix. The plan has `execute=false` and grants no
live-card or programming admission.

## Evidence

All 32 focused recovery tests pass, including the actual repair controller over
the stateful card/memory model. Cases cover preserved/missing/corrupt rollback,
wrong transition and prefix, existing repair backup, caller mutation, journal
and ownership loss, wrong UART receipt, uncertain/short writes and readbacks,
and retirement after success or failure. The original updater still rejects a
trace image. These tests model hardware I/O.

A separate run uses the actual consumed FIT/manifest, corrected typed package,
original rollback, protected package and experimental FIT bytes with the real
controller. All five modeled writes/readbacks pass; both rollback generations
and protected data remain exact. UART is modeled; its sender is not executed.

The composed repair receipt checker keeps the prior qualified receipt checker
byte-identical and adds exact old identities, fresh backup targets, preservation
order and original rollback requirements. A deliberate synthetic boot/wire
fixture is accepted; 26 mutations reject. Those additional
boot/wire fields are parser fixtures, never physical observations.

The selected affected run passes 1,606 parent tests (three skips), planner,
whitespace and generated-consistency checks. No firmware rebuild was needed:
the previously validated corrected image's producer/profile inputs are unchanged.

## Next physical boundary

The consumed earlier operator remains terminal. A new operator must bind these
sources and exact inputs, consume one new fuse, hold the continuing claim and
shared JTAG lock, enforce existing phase/output bounds and restore host clock/
permissions on exit. After the reported power loss it must perform a fresh
bounded bootstrap and independently verify card geometry, old prefix, FIT,
manifest, protected files and original rollback before the first repair write.
The installed v1 image is eligible only for that exact UART bootstrap/repair;
the current trace package loader continues to reject it.

Only after independently accepted physical repair/readbacks can the corrected
image be booted for a bounded traced TFTP test. The original intermittent
network stall is still unexplained. Native/core loads, ordinary Tenfoot launch/
return, HDMI, USB/media/network acceptance and timing remain open. The original
factory card and bottom kit are untouched; no restoration or card swap is needed
to prepare this step. TRWU8CBL's continuing claim remains held.

Raw evidence: `out/nano-recovery-console-repair-20261107`.
Retention: `/home/deano/fes-retained/de25-recovery-console-repair-20261107-r1`, layered on the corrected-build archive.
