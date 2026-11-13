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
