# Recovery PHY preparation and UART bootstrap prototype

**The recovery PHY ordering repair, matched bundles and UART bootstrap prototype
pass their off-board checks. One kit test stopped at the old-loader version check
before the UART probe, SD writes or native boot.** The prior
[recovery-network failure](../2026-11-03-de25-returned-sd/README.md) remains a
failed trial; returned-core SD access and ordinary Nano admission remain open.

The implementation is `20ebc7cf58dcec7b21957da020f9e096d3e9efb5`, followed by
the actual-banner binding fix `1bf9fe81404f130e914b4ff801e836a2864760a0`.
Base: `93905fca93b14d18dcfd2efb57a43a9f29e16770`.

## Resulting recovery behavior

The vendor's first XGMAC start calls `phy_connect`, which resets the PHY after
the old uploader's readiness samples. The separate recovery build now provides
`fesphy`: it checks the exact PHY address/ID, resets/configures once and retains
failed preparation. Later calls check the same object without another reset.
The uploader invokes it after probing Ethernet and before checking identity and
two consecutive link/negotiation-ready samples. XGMAC start requires the prepared
object and no longer performs PHY connection/configuration.

The vendor negotiation timeout remains 4,000 ms, link readiness remains bounded
at 20 seconds and no retry of failed preparation is added. The repair uses a
private vendor source copy. Its transformer locks the complete driver/header/
Makefile inputs and reconstructs them when validating the prepared source.
Packaging checks the actual linked preparation function/command and version
banner. The final profile explicitly identifies pinned Terasic U-Boot
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413` in that banner. An off-board audit
caught an earlier private-build banner mismatch before any hardware use; those
artifacts/checks remain retained.

The paired C test executes the original and transformed first-start code:
the original resets an already ready PHY and loses readiness; the changed path
prepares first and starts twice without another reset/configuration. Fourteen
changed-path success/failure cases cover identity, address, reset, configuration
and later scope loss. They model registers and do not qualify the physical PHY.

## Builds and software checks

The [software summary](software-summary.json) retains ten affected scopes at
the implementation and six scopes for the later banner fix. Both selections
complete 1,330 parent test IDs in three intact-module partitions capped at 300
seconds each. Two environment skips remain explicit. The implementation also
passes 1,577 FPGA software tests; unchanged integration scopes were retained.
`make check` passes on the final source.

The [recovery build selection](phy-build-selection.json) binds the actual
prepared sources. The matched recovery FIT SHA-256 is
`c27efed4828f73ad000fe78649ae01d665e12452f457c3e569d25b20b5b031dd`,
and SOF SHA-256 is
`ce143f50e05c9895381047f921be4a0c38d9a7eacfa75b3c0709be4ff7eafe75`.
An [independent literal decoder](recovery-spl-identity.json) matches all 254,835
SPL bytes and preserves eleven other reference sections. It does not verify
SOF security/configuration CRC or establish programming/boot acceptance.

The matched native warm-SD candidate is also built: boot ID
`a1cd2bc64ed5dbcf`, FIT
`4d75bc093df13146bd29c8cc7a8f4e1c0b5958efef78946253764810a25c8be5`, SOF
`239a882d67177185ea748570a0cf3a7e241b0b61e46fe91671586584b9aa763e`.
The [selected-input audit](selected-inputs.json) verifies 115 Git blobs and
1,298 pinned module/SDK files, including the actual CPU/translation instructions.
The Go SDK was built at the implementation commit; all its selected Git bytes
also match the final commit. The later changes affect recovery/banner selection,
and their exact uncompiled difference is recorded. Matched native startup,
relocation and contained-console checks pass. No FPGA compile ran.

## UART bootstrap evidence and remaining hardware work

The qualified old recovery loader includes Kermit `loadb`. A private host
prototype uses it to prepare the corrected spare card without another physical
swap. The [actual sender/receiver check](kermit-model.json) transfers 1 MiB
containing every byte value through Debian G-Kermit 2.01 and the exact pinned
U-Boot receiver. This uses host pipes. Six
[controller cases](uart-controller-model.json) use those real endpoints over a
modeled UART and reject changed readiness, size, acknowledgement, source and
ownership. They do not establish physical 230,400-baud operation.

The upgrade first verifies the qualified old loader/card and reserves RAM by
reading the already verified `FES/UPLOAD.BIN`. A small UART RAM probe must pass
before any SD write. It then preserves the old FIT/manifest in
`FES/R1103.ITB` / `FES/R1103.JSN`, writes the new FAT boot FIT and bounded raw FIT
mirror, and publishes the new recovery manifest last. Every write gets an
independent readback; the protected native/package files and rollback copies
are checked again. Failure consumes the attempt and cannot activate the result.
Fifteen [persistent card/LMB model cases](serial-upgrade-model.json) pass,
including partial/unknown writes, readback failures and rejection of replay.

The completed compound controller passes 248 actual success/failure cases,
18 real UploadSession/card cases, eleven transport/phase checks and four
SDK/entry selection checks. Fresh source/claim/USB/UART preflight passed on
validation checkout `6cfd241e63331a40241eecf342650b3f06c8f15a`, with unchanged
tested/build code `1bf9fe81404f130e914b4ff801e836a2864760a0`.

The [bounded TRWU8CBL trial](hardware-evidence.json) on 2026-11-03 booted the
qualified old recovery loader, then stopped after its single `version` command.
The vendor appends `socfpga_agilex5` directly after the build date; the prototype
and public uploader had omitted that exact suffix from their checks. There was
no RAM probe, SD write, corrected-recovery boot, Ethernet operation or native
attempt. Independent saved-byte and cleanup checks pass: 15 MHz read back before
stopping the owned private JTAG service, exact USB permissions restored, UART
free, same exclusive claim/spare and factory card safe. The failed sequence was
not retried. Frozen controllers/input plan and the complete stopped trace were
separately archived and read back.

The follow-up repair requires the complete profile/date/board banner and checks
`CONFIG_IDENT_STRING="socfpga_agilex5"` in the selected configuration. A
[captured vendor response](../../../tests/fixtures/nano-recovery-version.txt)
now exercises the uploader through successful admission using the new profile
name; the actual old profile, missing/foreign/extended/duplicate/prefixed banners
reject before card commands. All 41 focused recovery/upload tests pass. Full
changed-input checks and a fresh recovery build are the next off-board gates;
this parser repair has no physical acceptance yet.

The next hardware sequence still requires a new frozen controller/preflight:
one qualified old boot, UART RAM probe/verified spare upgrade, one corrected
recovery boot and owned Ethernet uploads/readbacks, then the native warm-SD
two-load/return test. Prompt 50 seconds, initial SD 100 seconds, each core
observer 45 seconds, returned SD 100 seconds and each configuration 20 seconds
remain unchanged. No native/core-load acceptance was gained by the failed test.

Artifacts, failed off-board checks, exact controller source and restartable
state are under `out/nano-phy-prepare-20261103/`; software results are under
`out/nano-phy-prepare-checks-20261103/` and
`out/nano-phy-prepare-checks-final-20261103/`. A complete tested-source bundle is
retained under `/home/deano/fes-retained/de25-phy-prepare-20261103/`.
Future 3090 Quartus jobs must hold the existing
`/tmp/lander-quartus-3090.lock` throughout. This milestone grants no normal
runtime admission or DE10-equivalent core-load acceptance; the full goal remains
active.

The [complete-banner follow-up](../2026-11-03-de25-recovery-version-uart/README.md)
passes its new software checks and version gate on the board, then stops at
the RAM-only serial probe. It makes no SD write and restores the qualified
old recovery prompt; new PHY/native warm-SD acceptance remains open.
