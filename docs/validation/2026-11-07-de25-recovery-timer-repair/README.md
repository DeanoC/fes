# Exact installed-image repair for the recovery timeout correction

Implementation: `0f870243530414e9f0ca246d929a61167924f83d`. This is host/model evidence. No device was opened,
card written or native candidate booted by this milestone.

The new `recovery_trace_upgrade.TimerRepair` accepts only the exact UART1 FIT,
manifest and protected prefix left by the completed earlier repair. Its current
destination is the separately validated [100-ms recovery correction](../2026-11-07-de25-recovery-timer-unit-fix/README.md).
The original `Upgrade` and UART0 `Repair` retain their earlier source limits.
The new controller uses their existing claim, journal, memory-map, session,
UART, fixed-write, readback and permanent-retirement checks. It snapshots every
caller-owned pair, authenticates both historical rollback generations and
reads/hashes both before and after the write sequence. It backs up the current
UART1 pair under fresh R1107C names. Existing R1107C files stop before writes.
R1107A and R1107B are never write destinations. It never activates the result.

All 13 focused tests pass across the new and earlier controllers, including
wrong host/live identities, missing or damaged files in either rollback pair,
post-write damage, occupied backup names, claim/journal/UART failures, uncertain
writes, failed readbacks and attempts to reuse retired wire entry points.
Caller mutation cannot change admitted bytes. The initial new fixture supplied
an extra prefix field as a rollback pair and was correctly rejected by the
strict pair shape; that fixture was corrected and its failure log retained.

Ten additional cases execute the actual controller with the actual artifact
bytes and unmodified production pins on the stateful card/UART model. Only the
success case accepts. The three staged UART payloads, five writes and all four
preserved rollback files are listed in `uart-plan.json`. A regular-file prefix
comparison proves that only the padded FIT mirror changes. Modeled write
counts are not physical writes or physical qualification.

Affected software checks pass: 1613 parent tests, three skips, plus the
selected planner/generated/whitespace checks. All 2,205 native compiled inputs
remain unchanged; the preceding normal native gate remains applicable. The
host controller change requires no native or FPGA rebuild.

After the [reported power interruption](../2026-11-07-de25-power-interruption/README.md),
the fresh serial version/nonce/prompt check passed. It did not establish a
fresh boot or current card integrity. The continuing claim is held; the old
failed upload operator remains consumed. The corrected loader remains off-board.
The next milestone is a newly frozen bounded UART-only old-image boot/repair/
new-image boot with live spare, protected-file and rollback verification before
any write. A separate fresh full-upload/native launch/return test follows.
Factory-card restoration remains waived by Deano; the factory card and bottom
kit stay untouched. General core-loading acceptance, display/input/media/native
network behavior and the four FPGA signoff findings remain open.

Raw evidence: `out/nano-recovery-timer-repair-20261107`.
Retention: `/home/deano/fes-retained/de25-recovery-timer-repair-20261107-r1`, layered on the built-candidate archive.
