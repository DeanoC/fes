# DE25 appliance disk construction

The shared regular-file media constructor and two-pass reconstructor now accept
an explicit DE25 selection. A board-specific layout adapter reuses the existing
kernel/DTB replacement checks, bootstrap builder, media tools and publication
checks. It preserves Terasic's U-Boot, boot script and reserved boot bytes.

Tested source: `f9b522b6c597cf66b900897f20f23d9bb7886dc1`.
The preceding `d6cf31c8a` failed the new FAT geometry check; its source and logs
remain retained. Subsequent publication changes documentation only.

| Region | Geometry and contents |
| --- | --- |
| Reserved start | Original first 1 MiB; only MBR partition geometry/CHS is derived |
| Partition 1 | Type 0x0b, start sector 2048, 4,194,304 sectors (2 GiB); FAT32 FESDATA, locked boot files and release store |
| FAT filesystem | 4,194,288 sectors, eight sectors per cluster, serial 0xde250001; exact 8 KiB zero tail |
| Partition 2 | Type 0x83, start sector 4,196,352, 65,536 sectors; shared 32 MiB FES bootstrap |
| Disk | 4,261,888 sectors, 2,182,086,656 bytes |

Capacity checks reserve the factory, Good, Previous and one staged candidate,
plus boot files and a 16 MiB margin within the filesystem. A 256 MiB factory
fits; a 512 MiB factory is refused. Partition 2 stays the vendor's Linux root
device. Its actual bytes, the full owned FAT path set, every payload hash and
the original reserved region are verified. The bootstrap's embedded factory
binding, immutable DE25 board file and static AArch64 init must match. The
owner kernel receipt supplies the kernel/DTB; the decoded boot script and FIT
remain checked through the existing boot inspector.

**This record proves construction only.** The upper release/media CLI is still
DE10-specific and needs actual factory-receipt and image-level boot-smoke
integration. No admitted factory, production release, native boot or normal-kit
acceptance is produced here. Test factory/idle payloads are explicitly marked
construction bindings. They provide no software-bundle or FPGA package seal,
and the new boot selector is not executed by these construction checks.

## Executed evidence

- The corrected DE25 suite passes in 23.1 s: four layout/admission cases and
  four real FAT/ext4 cases in the pinned container. Four host skips delegate
  those container cases. Checks include independent equal disk construction,
  foreign-board/factory-binding refusal, altered partition-2 bytes, nonzero FAT
  track padding, extra FAT files and preserving an existing output.
- The original combined focused run finishes **failed** in 40.2 s because the
  new DE25 check expected a filesystem exactly as large as its partition.
  Existing DE10 real-card and bootstrap cases pass. A direct dosfstools 4.2
  probe measures 4,194,288 filesystem sectors: the selected 255/63 geometry
  rounds down by one partial track. The correction requires that exact value
  and its zero tail, preserving the partition boundary. The first probe helper
  fails on nested string quoting in 1.5 s; its corrected continuation passes
  in 2.0 s. Both scripts/logs remain retained.
- The complete corrected affected plan passes all six commands in 281.6 s:
  1,316 parent tests with 45 skips in 275.445 s, and 40 generated consumers/42
  fixture copies. The original affected run remains **failed**, 272.9 s.
- A real-byte construction fixture passes in 16.3 s. It verifies the actual
  locked vendor boot tree and matching owner kernel, builds a fresh real
  AArch64 FES boot selector, creates two identical 32 MiB bootstraps, then
  independently constructs two identical disks. Its factory is a 256 MiB
  marker-only filesystem and its idle is text, not FPGA configuration.
- The shared reconstructor passes in 14.1 s using those same marked inputs
  with explicit `board='de25-nano'`. It invokes the ordinary inside-media entry
  point twice; both full disk hashes also match the two direct constructions.
  A foreign board in the bindings is refused before construction.
- Source/SDK/consistency checks pass in 8.8 s. All 374 canonical MENU, 59
  combined-compiler and 58 runtime-test inputs match. The actual earlier
  managed SDK authenticates against its original producer and current kernel
  policy; its old receipt is unchanged. No Quartus or CPU model is rerun.
- Independent source/archive restoration passes in 15.7 s. The exact tested
  tree is `ebef83250607ff9d2661d00305775da028ff4eb8`; 53 regular archive members
  are individually verified. Four disk files are checked against the same
  hash; the retained canonical pair is included with original logs, inputs,
  source bundle and all failures.

The real byte identities are:

| Artifact | SHA-256 |
| --- | --- |
| Vendor U-Boot FIT | `bb6a48c77d2559a2f9710153cc616a242bfe701ff891cb5a14bddba33eaf2cf7` |
| Unchanged vendor boot script | `b38747a4b43abcc04d84b8f46ea514594e9cc59c8188633e4d24ba36da3728ef` |
| Actual owner kernel Image | `28201c24220c9ed11ac5943aa2583f647904d2f694796407d66a7728c1c1456b` |
| Actual owner DTB | `d57bac64ffbb1ff4ace06dc0ba3fbb2d589055340274493efbfba687c4f244f7` |
| New AArch64 boot selector | `e6f2c62258353b114fc7829b03892d4103a3f43bcaa2f4cccae65a8af0f9b564` |
| Both bootstrap constructions | `74c15bf02fc505782562b7e29ad3da8724c4f99ed3626b8520e295f9dd58e5c8` |
| All four disk constructions | `a794f08ea14863fd4475c94f28b5c377e593fb392bfed366a7e70d011a78dda6` |

[checks.json](checks.json), [vendor-kernel-fixture.json](vendor-kernel-fixture.json),
[shared-reconstruct-fixture.json](shared-reconstruct-fixture.json) and
[source-scope.json](source-scope.json) preserve the exact scopes. The
[retention manifest](retention.json) refers to
`/home/deano/fes/out/artifacts/de25-managed-media/f9b522b6c/`.
The construction archive is 63,629,585 bytes, SHA-256
`ba79caba11abfba176ea2b09e95452ee93e228c28b3210b00e8d619d297bb182`.
Original state snapshots and failed job records remain historical evidence;
the completed checks and publication continuation are authoritative for this
milestone. Named proof containers completed and were removed, and all completed
owned job identities were checked absent.

The real-byte fixture has no release provenance sidecar or image-level boot
result. The normal release exporter was not supplied fabricated QEMU evidence.
The high-level DE25 factory/release/bootstrap/card chain still needs integration
and execution using genuinely admitted MENU and factory outputs. Persistent
data provisioning/expansion and update/watchdog recovery also need the complete
native workflow. HDMI output/reset signoff, firmware/disabled-F2H qualification,
an eligible independent RBF rebuild and all native MENU/catalog/ZX81 acceptance
in the [normal-kit scope](../../design/de25-linux-platform.md) remain open.

This milestone accesses no board UART/SSH/capture, allocator, FPGA, SD, installed
software/kernel, QSPI or power. Bottom TRWU8J8G remains claimed; top TRWU8CBL
is untouched. Last authenticated native health remains 2026-11-01 14:48 UTC on
ABI2; never run ABI3 clients against it. No baseline is restored, promoted or
released. Future QSPI/boot-configuration deployment needs fresh explicit
approval after a concrete artifact. No physical action is requested here.
