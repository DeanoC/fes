# Matched command-snapshot boot candidate

The selected-program native SDK, compressed CPU entry, complete BL31,
SPL/U-Boot and HPS-first FPGA composition build from the selected source at
`412bc2cc226ac03fd5f0c85d93f0c6c1b71ced31`. Subsequent documentation corrections
do not change their selected producer sources. All producer source maps and
cross-component metadata are checked, including a complete fresh native file
admission. This is off-board preparation for the format-7 retirement test;
it does not qualify ordinary core activation or memory reuse.

The [matched result](matched-result.json) records chain
`9a3ce056ac63f788433e8c691459049ddd67b414fb44075079735e0932b34db9`
and boot ID `c0365a0ef029c687`. The FIT is 8,497,872 bytes, leaving 8,279,344 bytes
under the explicit 16 MiB boot-FIT contract. That limit is independent of the
FPGA configuration-package bound. The composition checks that the sealed
computer FPGA payload remains unchanged; it binds the separately sealed menu,
held ROM, shared boot keyboard, initial tape, owned SD and writable installer
selections. The historical spare-card plan is a build input, not fresh live
card admission or write permission.

The [producer commands](commands.json) use the pinned TamaGo compiler, GNU
AArch64 toolchain and clean Terasic TF-A/U-Boot revisions. The brief local
Quartus Pro 26.3 PFG composition runs under the shared
`/tmp/lander-quartus-3090.lock`; it performs no synthesis or fitting. The
existing sealed projects are copied with complete before/after digest checks
and independently accepted by the current FPGA producer validators.

The [retention record](retention-result.json) identifies an archive containing
3,711 source, input, receipt, artifact and evidence files. Every archived member
is independently read back and hashed. External Quartus binaries are recorded
by digest rather than copied into the archive. The native file-admission
snapshot still matches the current complete file/tree closure after retention;
its record explicitly grants no live authority.

## Warm-return evidence correction

The [saved-trace analysis](warm-return-analysis.json) checks the exact previous
failed baseline console SHA-256. Its last master stop reports status 0 and
complete 1, followed by reader generation/request/ack 1, matching cookie 1,
status 4 and pending 0. Firmware then reports generation 18, reset f and a
successful independent hardware-drained mask of 8 before the native panic.

The original failure summary's missing-fabric-acknowledgement diagnosis is not
established by this trace. The zero fabric-handshake flag is expected for this
profile. A failed local menu stop can still retire the native service after
successful independent firmware closure, and later native source/arena checks
can also fail. The final panic preserves only the generic retired-service
sentinel, so the actual native error is unproven. The attempt remains failed;
its later physical-reset baseline restoration remains separate evidence.

Next, preserve the cached native failure cause through frontend retirement so
a subsequent bounded stop test can distinguish local polling failure from
native receipt/source rejection. Then rebuild the affected matched chain and
prepare a trial with fresh kit, transport, card and artifact admission. No
board was programmed, rebooted or written during this preparation.

## Rebuilt candidate with preserved failure cause

The [updated result](matched-with-cause-result.json) binds the complete rebuilt
SDK/entry/BL31/SPL/U-Boot/FPGA chain to selected source at
`16e8cbde849ea2557e16c4dbd0d75a9928b5e06d`, including the cached native
failure-cause repair. Its chain is
`78d9472a2d7218dbdfbfbc3bc8aea3ea432f789c6436937fef649ca5a28b1c20`,
boot ID `bc40ecb6064165fa`, and FIT SHA-256
`5176ea7b074dd15e1414df0a39296f3b3c4579507b9a71f08e465b0584cb2486`.
The FIT remains 8,497,872 bytes. Complete producer source maps, component
metadata and a new full native file admission pass. The entry guide now states
the actual explicit 16 MiB FIT contract.

The [updated commands](commands-with-cause.json) retain the same pinned tools
and shared PFG lock. The [updated archive](retention-with-cause-result.json)
independently reads back 793 new source/artifact/receipt/evidence files. The
unchanged complete sealed project trees remain in the
[preceding verified archive](reused-project-archive.json); their complete current
membership and hashes are rechecked, and that archive's bytes are hashed again.
Both archives are required to recover this build context. External Quartus
binaries remain digest-only dependencies.

The [subsequent physical trial](../2026-11-13-de25-command-snapshot-physical/README.md)
boots this rebuilt candidate and completes full input transfer. Retirement
again fails closed in phase 4 before queue publication, retaining all source
memory without activation/reuse. The original FIT is restored exactly; the
warm baseline then fails with a USB packet error and no final Park. The
candidate has no physical retirement acceptance. Its consumed procedure must
not be reused. Later main integration changes require fresh source admission
and an affected rebuild before another candidate trial.
