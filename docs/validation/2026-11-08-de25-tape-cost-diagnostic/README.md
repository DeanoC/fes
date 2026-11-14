# Secure tape cost and failure-reset diagnostic

Boot `4489a0cd784922b0`, compiled from `ad38a355f7a7d3cea0a31ce2065b402952aca9a4`,
is a newly matched diagnostic chain. Its native runtime/entry bytes reproduce
the previous build; firmware, cold-chain identity, SPL/FIT and composed boot files
change. The previous `69a03065693442b5` image remains consumed1/max1 after the
[retained tape deadline failure](../2026-11-08-de25-input-action-board-trial/README.md).
The new image is unused0/max1 and has no physical programming admission.

The strong tape supplier counts callback calls and samples ownership, CSR read,
write and barrier costs once per64 calls; complete frame guards, image copies
and SHA callbacks sample every call. These diagnostic samples add overhead
inside the original1s limit. They neither grant access nor qualify physical
performance. All six original guarded callback bodies and the complete tape
engine/service are preserved. The protocol, private512-byte frames, source
hashes, before/after guards, single-toggle writes and failure retirement remain.

After failure containment, cached reset status and seven cost records precede
the terminal failure marker, so the collector can retain them before stopping.
Reporting performs no endpoint access, emits once and is silent on successful
loads. Reset status0 records only the existing direct control-reset callback's
readback conditions. It does not establish fabric acknowledgement, producer drain
or a normal return. Normal bridge access is retired afterward. The old hardware
trace cannot establish that reset readback retrospectively.

Final software checks pass:1,734 parent tests(two skips),1,595 core-project
tests(79 skips), actual native/producer/Avalon/firmware-RTL integration, and the
actual two-package retained-ROM/tape preflight with zero firmware calls. The
strong-provider tests cover complete/maximal transfers, original deadline
failure, stuck control reset, unexpected open data bridge, ownership/source/USB
faults and no replay. Earlier mixed-format test failures are retained; final
runs use frozen sources.

The SDK, entry, BL31, U-Boot and shared-lock-protected packaging pass. Exact ELF,
source/artifact, embedded-SPL and unchanged FPGA payload checks pass. Original
OCRAM has7,488 bytes remaining; the8MiB FIT limit has79,312 bytes remaining.
The first overall build timed out during U-Boot preparation; its partial files
and exact owned-process cleanup are retained. Only U-Boot was rebuilt in a fresh
directory with8 compile jobs; each phase kept its existing limit.

This is preparation and injected-test evidence. No new board programming, card
write, native boot, tape consumption, physical input/HDMI or ordinary core-load
acceptance is claimed. Top TRWU8CBL's last observed state remains the failed
native launch; recovery precedes another native test. Its claim and spare card
remain reserved, factory restoration is waived unless needed, and the bottom
kit is untouched. Fresh operator/source/file admission is still needed before
the next single bounded diagnostic. Its measurements will guide the tape
performance repair without widening the1s deadline.

The archive retains original vendor build-tree symlink targets and verifies them
without extraction. Absolute build links point inside this worktree; restoring
under a different root requires adjusting those links or rebuilding from the
retained source bundle and recipes. The first archive policy refusal is retained
separately; it caused no device operation.
