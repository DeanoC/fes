# Native package inspection and retention

Exploratory result based on `d563b7cd2d60a5efdd51f24f9d680d8e6d1b143f`.
This is QEMU-only software/storage evidence. No FPGA is programmed, no Cyclone V
package becomes DE25-compatible, and no physical board or shared kit is used.

## Working path

The native `packages` program boots the existing AArch64 TamaGo base, mounts a
fresh FAT32 image through VirtIO, and opens real FES canonical archives through
`corepackage.RetainArchive`. The same reader/validator now serves the normal
FogCast `Stage` path before its existing private rooted publication. The work
does not add another validator, identity domain, compatibility registry, or
programming path.

The API reads an exact bounded length and retains private bytes. It validates
canonical archive layout, manifest shape, payload/map digests and ROM-map
structure through the existing functions. Inspection metadata is deep-copied;
payload readers copy into caller buffers and expose no direct mutable slice.
Closing or changing the source does not change the retained object. A broken
reader that repeatedly makes no progress is rejected. Cancellation is checked
between reads and validation phases; a blocking reader needs its own deadlines.

## Evidence

[The complete receipt](result.json) binds compiler, native ELF, module versions,
compiled source, corpus files, fixture construction, generated archives, disk
and QEMU commands. The [console](console.log) records all case results:

- All 88 shared format-2/3/4 cases match: 11 accepted, 77 rejected.
- Accepted package IDs and payload hashes match the shared corpus exactly.
- Four additional malformed containers reject header corruption, nonzero
  padding, trailing bytes and truncated headers.
- After retention, the probe corrupts a runner-owned package on FAT32. The
  retained payload remains unchanged; a fresh inspection rejects that source.
- The source is restored and flushed. The entire disk hash before and after
  the test is identical.

The diagnostic reports `HeapSys=12419072` for this small synthetic corpus.
That is one runtime observation, not a production package-memory or timing
qualification. The images and RBF bytes in the corpus are test fixtures and
must never be deployed.

Host tests cover all shared cases, independent payload cursors, caller-buffer
and returned-descriptor mutation, exact lengths, cancellation and invalid or
non-progressing readers. [Focused race checks](reader-race.log) pass. The
existing corepackage suite also passes after moving Stage onto the shared API.
An unchanged [USB tenfoot control](tenfoot-control.json) still boots twice,
browses the real private catalog, changes the layout and restores preferences.

## Limits and next boundary

This verifies archive contents, not runtime admission. Structurally valid
unknown ABIs still inspect successfully, as before. The physical runtime must
check target/ABI/interfaces, linked ROM/composition receipts, retained programmed
bytes and live hardware identity before release. Format-3/4 ROM linking and
activation are not performed here. The lifecycle model remains separate.

The native FAT reader caps individual files at 64 MiB; the shared archive cap
is 65 MiB. The corpus does not exercise maximum-size production maps or prove
large-package latency. Retention is RAM ownership, not durable installation or
power-loss-safe publication. No new shared contract or producer pin changes.

Reproduce with the [native run guide](../../../platform/embedded/tamago/README.md):
use `run.py packages` with a fresh output directory. This mode deliberately
refuses an existing disk image, holds its disk lock for the complete operation,
and starts no remote host, USB device or display service.

## Repository checks

[The selected checks](repository-checks.json) pass: 1,106 parent tests,
generated-consumer consistency, host Go/UI checks and dead-code analysis.
The initial host run hit the unchanged 48 ms room-script budget in
`TestHomeKeepsWorkingWhenStateDirIsUnwritable`: elapsed 52.36 ms versus 10.14 ms
thread CPU time. The [failure excerpt](initial-room-budget-failure.log) is
retained; this result is consistent with scheduling contention and is not a
package-validation failure. The exact test passes twice with the race detector
on both the [current source](tenfoot-isolated-current.log) and a
[base-source overlay](tenfoot-isolated-base.log). The overlay restores the old
corepackage implementation from the base commit; tenfoot source is unchanged.
The complete host lane then passed on retry. No timeout or script budget was
changed, and the successful parent/native checks were not rerun.
