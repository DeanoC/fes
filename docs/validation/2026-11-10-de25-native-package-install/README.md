# Native staged package installation candidate

Implementation `4e3260456df6da10c502c47d09185d6c751dd9db` connects the existing
owned installer to actual Nano startup and Tenfoot's ordinary core-catalog
Install/Setup APIs. Its base is `aca8f8ade8b480cca6990acf7fb0b8c02c8e37a4`.
This is software/model and matched-build evidence. No kit, UART, JTAG, upload
or physical card operation occurred during this preparation.

An explicit compiled `--writable-storage-plan` and 25 MHz selection enable
the checked writable mount. The independent card identity, MBR, primary/backup
boot sectors and protected recovery files are checked through the existing
owned SD session. Writes stay within the FAT partition and exclude its boot
sectors. The installer rechecks integrity before catalogue publication and
after readback. An uncertain failure retires the store without retry; a
published record is not presumed rolled back after a later failure.

The base can start with explicit empty bootstrap lists. Before menu ownership,
it snapshots optional `FES/INSTALL.JSN` and its exactly digest-bound package,
metadata, ROM and optional tape into private RAM. The normal Install action
uses the existing input pause, menu closure, active SD window, content/record
readback and menu/input restoration. Startup does not install automatically.
Invalid optional source content is quarantined while an independently healthy
installed library remains usable; uncertain ownership still prevents startup.

All [12 selected checks](software-results.json) pass on the implementation:
1,885 parent cases plus their ancillary check, 1,595 FPGA software/model
cases, native runtime/C-dispatch integration, SD/FAT/store race checks,
host/UI, runtime, contracts, generated consumers, planner and whitespace.
Tool/fixture-dependent skips confer no physical acceptance. Focused tests
exercise the ordinary host client API, empty-library installation, actual FAT
allocation/readback, launch/return, fresh-boot discovery and second launch/return.
Failure cases cover malformed/hash-mismatched sources, foreign/running/duplicate
requests, integrity failures around publication and retired ownership.

The first U-Boot build compiled but its [FIT was rejected](rejected-fit.json)
at 8,395,432 bytes, 6,824 bytes over the unchanged 8 MiB bound. Commit
`82ad1d0e6a78f0717e222f739a0eb933fc1bb0a6` changes only host entry compression
to level 9 and adds dynamic-stream coverage to the independent C decoder tests.
All [20 entry regressions](entry-compression-results.json) pass. The native ELF
and all 2,129 recorded SDK input hashes remain unchanged. Runtime decoding,
memory layout, CRC checks and bounds remain unchanged.

The rebuilt SDK/entry/OCRAM-MMU BL31/U-Boot/FIT/HPS-first SOF are
[matched against current selected sources](publication.json). Boot identity:
`bda14309e7e758dc`. FIT size: 8,370,856 bytes, leaving 17,752 bytes. The sealed
computer/menu payloads are unchanged. A stale host-checker output path failed
before its correction; both checker runs and the rejected FIT remain retained.
No earlier physical result qualifies this installer-enabled image.

The [first-install fixture](staged-export.json) contains nine files: an empty
playable library, the selected menu, a canonical 831-byte install manifest and
the selected ZX81 package/metadata/ROM. It is prepared on the host, without a
tape or automatic installation. It has not been uploaded or read back on the
board. The [retention proof](retention-proof.json) records the complete artifact
readback/mode verification and independent source restoration/fsck/pack checks.
The capsule preserves both rejected and final candidate artifacts and all
bounded result logs; its declared base bundle remains a required dependency.
No output was deleted.

Next: add the bounded `INSTALL.JSN` destination/readback to the existing
recovery uploader, prepare a fresh top-only operator procedure and admission,
then exercise ordinary Install, launch/return and fresh-boot discovery with a
second launch/return. The spare/factory-card protections, shared Quartus lock,
first-failure stop and exact transport cleanup remain required. Physical package
installation, native Ethernet, updates/uninstall, power-loss recovery and
DE10-equivalent appliance acceptance remain open. The earlier manual visible
menu return and one-minute observation belong to their archived image.
