# Recovery version repair and stopped UART RAM probe

**The complete Terasic version check is fixed and its software gates pass.
The next TRWU8CBL test passed that check but stopped at the 1 KiB UART RAM
probe, before any SD write, Ethernet operation or native boot.** The qualified
old recovery prompt was subsequently restored and independently verified.

Tested source: `35f4d552971f724235249da965e92423dda245d1`.
Base: `6cfd241e63331a40241eecf342650b3f06c8f15a`.
The unchanged native candidate was built at
`1bf9fe81404f130e914b4ff801e836a2864760a0`, with SDK selected at
`20ebc7cf58dcec7b21957da020f9e096d3e9efb5` and sealed FPGA source
`c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`. It remains unbooted.

## Repair and off-board checks

Pinned `common/version.c` appends `CONFIG_IDENT_STRING` directly after the
build date. The public uploader now checks the complete profile/date/board
banner and requires `CONFIG_IDENT_STRING="socfpga_agilex5"` in the selected
configuration. The [captured response](../../../tests/fixtures/nano-recovery-version.txt)
regresses the actual vendor format. The old profile, missing/foreign board,
extended profile, duplicate banner and prefixed banner reject before card I/O.
The separate UART bootstrap also checks its explicit old-loader banner.

All 41 focused tests, [six affected software scopes](software-summary.json),
1,331 exact parent IDs in three intact-module partitions capped at 300 seconds,
and `make check` pass. Two environment skips remain explicit. Earlier PHY/RTL
and integration checks are retained for their unchanged inputs. The final
private controllers pass 248 outer cases, 18 real UploadSession/card cases,
18 UART upgrade/card cases, eleven transport/phase checks and four unchanged
native-selection rejection cases.

A fresh recovery build/package passes. Its FIT remains
`c27efed4828f73ad000fe78649ae01d665e12452f457c3e569d25b20b5b031dd`.
The [independent SOF inspection](recovery-spl-identity.json) matches all 254,835
SPL bytes and eleven preserved sections; it supplies no security/configuration
CRC or hardware acceptance. The [native source audit](native-source-context.json)
checks all 115 selected Git blobs and 1,298 module/SDK files against the current
checkout and the earlier SDK commit. No native rebuild or FPGA compile ran.

## Hardware boundary

The [saved-byte and cleanup audit](hardware-evidence.json) verifies one failed
sequence on the repaired implementation. The old recovery boot, complete version
check, original card/prefix/protected-file checks and real LMB reservation pass.
The 230,400-baud switch/ready messages were readable. The binary sender then
reported `Kermit sender failed or left unsent bytes`; its quiet stderr is empty.
The saved first implementation does not distinguish a sender exit failure from
pending transmit bytes, so neither cause is established.

The RAM probe is unverified. No rollback file, boot FIT, raw mirror or recovery
manifest was written. There was no corrected-recovery boot, network operation,
new PHY preparation or native attempt. The prior version-check rejection is
retained separately in the [earlier record](../2026-11-03-de25-recovery-phy-prepare/README.md).
Neither failure was replayed. The failed attempt's UART receiver state was
unknown; one separate bounded JTAG recovery only reloaded the qualified old
image and verified its prompt. Both operations restored 15 MHz and exact USB
permissions before stopping their own private services. UART is free, the same
exclusive claim/spare are retained, and the factory card remains safe.

Off-board follow-up repeats the actual 1 KiB probe through the real sender and
pinned C receiver under [normal](uart-user-model.json) and
[elevated](uart-root-model.json) privileges. Six success/failure cases pass in
each context. A diagnostic copy records sender status and queued-byte counts;
it has not run on the board and does not repair or qualify the serial path.

## Retained state and next step

Exact artifacts, controllers, failed traces, recovery and checks are under
`out/nano-version-suffix-20261103/`,
`out/nano-version-suffix-checks-20261103/`,
`out/nano-uart-diagnostics-20261103/` and the preceding
`out/nano-phy-prepare-20261103/`. Source is independently restored/fsck under
`/home/deano/fes-retained/de25-version-suffix-20261103/`; controller archives
have independent readback proofs. The final session archive receipt records
its complete readback separately. Attempted controller directories are consumed
and cannot be reused.

Next: a separately frozen, bounded RAM-only serial diagnostic with saved sender
status and wire/queue observations, preserving the failed implementation's HIL
failure count. It must not expose any SD write or activation path. Physical
binary-transfer acceptance precedes another recovery-card upgrade. The existing
prompt 50 s, initial SD 100 s, core observers 45 s, returned SD 100 s and each
configuration 20 s bounds remain unchanged. Warm SD, ordinary runtime admission,
arbitrary package loading and Tenfoot hardware acceptance remain open.

All future Quartus jobs on `fes-build-3090` must hold the existing shared
`/tmp/lander-quartus-3090.lock` throughout. This step used local PFG packaging
and no 3090 job. Ownership stays `lane:deano`; the full core-load goal is active.
