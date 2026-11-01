# DE25 AArch64 appliance bootstrap source and image proof

This adds the DE25 startup branch to the existing FES `fes-boot` PID1 and
bootstrap builder. Tested source is
`e06d5e3a1f32170cf6458c172288259baf04aae0`, following initial source
`b07487a8b713607810bdff80b3581130621c25a8`. Publication changes documentation
only. No board, capture, SD card, installed kernel/software, FPGA or QSPI is
accessed. This is not managed-kit or native boot acceptance.

## Resulting behavior

- Build the same platform package for an explicitly selected ARMv7 or
  AArch64 CPU, with exact environment/source/tool provenance. Clear inherited
  CPU flags before probing Go as well as before compiling.
- Bake the selected `/etc/fes-board`, static executable and factory manifest
  into the deterministic bootstrap ext4 image. Default APIs remain DE10-only;
  the DE25 factory and executable require matching explicit selections, and a
  source-bound build record must match that selected CPU.
- Before release-store access, PID1 requires its immutable board file to
  match the executing CPU. The codec/store pins the factory and later release
  choices to that selection and kernel, using the existing shared implementation.
- For DE25, admit the exact Terasic model/SoC and complete ext4
  `/dev/mmcblk0p2` root. `/dev/root` is accepted only when its mounted device
  number matches that partition's sysfs identity. Refuse another filesystem,
  device, root subdirectory, duplicate root or existing FAT mount. Check p1's
  block-device identity, make the bootstrap root read-only, then mount its FAT
  at `/media/fat` with `nosuid,nodev,noexec` and owner-only file/directory masks.
  The existing root switch transfers this mount to the selected system.
- Preserve Terasic's boot-script contract; no boot script or kernel is modified.
  The new bootstrap image is not installed in partition 2 during this proof.
- Refuse DE25 trial watchdog startup. The Cyclone V register preparation
  remains restricted to DE10; DE25 native watchdog/reset recovery is unqualified.

The existing builder APIs now accept
`build_static_arm(..., architecture='aarch64')` and
`assemble_bootstrap(..., board='de25-nano')`. The ordinary release/card CLI and
Make workflow remain DE10-specific. This increment does not assemble a complete
DE25 card or managed factory.

## Actual checks

The original source passes 58 native boot test/subtest events with one skipped
privileged root-switch integration test. Two actual static AArch64 test
executables run under QEMU Cortex-A53 and pass 17 test/subtest events: selected
store/factory refusal before directory mutation, exact image/CPU agreement,
DE25 partition admission and unqualified trial refusal before guard startup.
These are CPU/filesystem-policy checks, not execution of the DE25 mount
syscalls or a board reboot.

The Python bootstrap/platform suite passes 40 host tests with three skips,
plus three actual pinned-container image tests. It checks both supported ELF
targets, wrong board/build-environment refusal, immutable output and source
bindings, device inodes, selected board contents and reproducibility.

The first combined proof remains **failed** after 7.6 seconds: all preceding
CPU checks pass, but an intentionally invalid inherited `GOARM64` reaches the
toolchain probe before compilation. The corrected source normalizes that probe
environment. Its new regression and focused continuation pass in 26.8 seconds.
The complete platform/appliance source trees match the executed CPU tests, so
those passing native/QEMU cases are reused without resimulation. An earlier
ad-hoc Python invocation also remains failed: the normal scripts import path
was omitted before the platform test could load.

The continuation builds real static ARMv7 and AArch64 bootstrap executables
using Go 1.26.5 with invalid inherited ARM flags. Both pass ELF/provenance
validation. The AArch64 executable runs under QEMU and refuses non-PID1 startup
with the expected diagnostic, before any mount or device access. Two independent
32 MiB ext4 assemblies match, and extracting init, the board file and factory
manifest reproduces their exact input bytes.

| Artifact | SHA-256 |
| --- | --- |
| ARMv7 `fes-boot` | `e1f463cb48af74e48c1444a9d733bd28409e5813746c460bcf609a1e4e39d3cc` |
| AArch64 `fes-boot` | `e6f2c62258353b114fc7829b03892d4103a3f43bcaa2f4cccae65a8af0f9b564` |
| DE25 bootstrap ext4 | `11fde77d2c03fffa6979c6c5e1a757420acb054011a140260ebcdb3e51d105cd` |

The factory manifest is a **binding fixture** naming the unchanged retained
128 MiB ABI3 headless rootfs (`d88380ff…`) and kernel (`0a429367…`). No release
is exported from it. That rootfs is not a managed appliance factory and this
bootstrap is not a bootable card. Its manifest/source bindings prove construction,
not that the fixture has the required appliance root layout or services.

The authenticated prepared kernel config has built-in loop, ext4, FAT/VFAT,
codepage 437, ISO-8859-1, devtmpfs, tmpfs and namespace support. Built-in watchdog
drivers establish availability only; they do not qualify DE25 recovery.

The full `make platform-test` passes all six test-bearing Go packages in
37.6 seconds, with the Bluetooth command reporting no tests. The full affected
parent/image-tooling plan passes all six commands in 302.4 seconds, including
1,280 parent tests with 41 skips (294.878 seconds) and generated consistency
for 40 consumers/42 fixture copies. Its exact results are in `affected.json`.
Host/UI, C++ runtime, contracts and FPGA lanes are not selected by this change.
These local checks do not assert exact-head GitHub CI or independent review.

## Retention and remaining work

Private artifacts are retained at
`/home/deano/fes/out/artifacts/de25-appliance-bootstrap/e06d5e3a1/`.
The independently read archive verifies 37 regular files, including both
actual AArch64 test executables, QEMU, original failed receipts/logs, the built
bootstrap executables/image and actual construction evidence. Its size is
13,202,484 bytes and SHA-256 is
`2338be14d90fb32b279833e82e4876baeaeff9ec0e4fcd7470393a06f13b3947`.
The copied source-bundle chain restores the exact tested tree. All 374 canonical
MENU inputs and all 59 combined-compiler/58 runtime-test inputs match the
existing FPGA/ZX81 receipts; none of those compiles or simulations is repeated.

Complete the managed DE25 rootfs and factory/software wiring, a capacity-checked
card layout and native PID1/FAT/root-switch boot, then qualify watchdog/reboot
recovery and update/restore. MENU, catalog ZX81 lifecycle, persistence, allocated
DDR/SID/security and the remaining FPGA timing/reset gates retain the full
[normal-kit acceptance scope](../../design/de25-linux-platform.md). No board
baseline is promoted or released. New boot-configuration/QSPI deployment still
needs explicit approval after a concrete artifact is prepared.
