# DE25 inputs and shared factory wiring

Tested source: `164d3fe49317bc8c2fbda014058bd5f95f1c35fd`.
The production change is in `38faedcb5ccb282482056ae5c280c942cbda674b`;
the following commit adds the CPU-selection/hash-preservation regression.
Publication changes documentation only. This work produces no managed
Buildroot image, complete SD card, sealed MENU or native boot acceptance.
Neither board, capture device, installed software, SD, FPGA nor QSPI is touched.

## Change

The existing artifact pack/check/stage path has an explicit `de25-nano` context.
It checks board/architecture metadata, every executable's AArch64 ELF target,
static base helpers, the MENU target and runtime interfaces before staging.
The default context remains DE10. New packed sets record board/architecture;
DE25 cannot be selected by an uploaded manifest. AArch64 staging preserves
the selected provenance and the six normal binary names with their CPU suffix.

The shared rootfs and factory post-build scripts accept
`FES_TARGET_BOARD=de25-nano`, require its exact image identity and select those
AArch64 binaries. Idle must match the selected MENU firmware. Existing package
selection, bundle packing, base-library closure and interface checks still run.
DE25 never builds or carries `fes-window`, and refuses the DE10 Bluetooth module
archive. The Buildroot prebuilt-runtime recipe selects from its actual ARMv7 or
AArch64 configuration and checks the selected hash before replacing any binary.

DE25 uses the existing per-card MAC script before shared DHCP, replacing the
DE10 U-Boot-text initializer. DHCP brings up loopback, uses the configured
hostname and stays available to renew its lease. The image points Dropbear at
`/media/fat/fogcast/ssh`; startup requires the exact writable p1 FAT mount and
refuses directory symlinks before creating persistent key storage. It generates
no shared keys in an image. Bluetooth modules are sought under the running
kernel release instead of a hard-coded Cyclone V release.

The top-level `make image`, source-image export and media workflow still select
DE10. These APIs and shared scripts are prerequisites for a managed DE25
Buildroot profile, not a substitute for building and booting that profile.

## Executed checks

The focused driver passes all three commands in 7.3 seconds:

- 22 Python tests pass in 0.303 seconds, including AArch64 ELF/metadata and
  cross-board refusal, staging names, MENU target mismatch, Make CPU selection,
  preservation of an installed binary after a failed hash, SSH mount/symlink
  refusal, retained key bytes, and DHCP hostname/start/stop/renewal settings.
- The complete rootfs shell fixture passes in 6.385 seconds. It exercises the
  existing DE10 expanded package tree, factory/update bundles and warm-tree
  reuse, plus a DE25 MENU-only factory with AArch64 names, persistent SSH wiring,
  the selected idle and absence of the Cyclone V module. The Go bundle packer
  is real; **readelf, package selection and executable/RBF payloads are explicit
  fixture stand-ins**. This establishes shared assembly wiring, not ELF execution,
  timing seal, native startup or hardware behavior.
- The existing DE10 `fes-window` build/post-build fixture passes in 0.292 seconds.

The artifact tests use synthetic ELF headers/sections to exercise architecture
and interface validation. No new QEMU or physical CPU test is claimed. Full
affected results are retained separately in `affected.json`; CI/review on the
publication head remains a separate gate.

The complete affected plan passes all six commands in 373.6 seconds: 1,288
parent tests with 41 skips (364.379 seconds), the planner/whitespace checks and
generated consistency for 40 consumers/42 fixture copies. Host/UI, C++ runtime,
contracts and FPGA lanes are not selected by these parent/image changes.

## Real retained-base audit

All 33 direct `*.so*` library-directory entries (including symlinks and the
debugger helper) match the retained ABI3 headless rootfs tar byte-for-byte.
The assembler source and four actual AArch64 binaries also match their earlier
`199604662ea19a526e1884ad202fbca48e1c84d7` proof. Its successful ELF compatibility
result is reused after these checks; unchanged CPU cases are not rerun. This
does not test the later agent's update behavior or declare a current managed
software installation.

The audit identifies required factory paths absent from that headless layout:
image/Bluetooth libraries, D-Bus/BlueZ services and factory/base manifests.
`libstdc++` is already available under `/usr/lib`; the shared factory's `/lib`
path also needs the managed filesystem layout. The base's compatible ELF ABI
does not establish its startup or service completeness. Exact entries, hashes,
the prior receipt and its original source identity are in `retained-base-audit.json`.

The prepared owner kernel has USB HID built in, but Bluetooth and BTUSB are
modules and its image ships no matching module set. `kernel-driver-audit.json`
retains the actual config/image identities. The managed kernel needs those
drivers built in or modules from that exact owner-kernel build; neither vendor
nor DE10 modules may be substituted.

## Remaining

Build the actual managed AArch64 base with the shared libraries, read-only
skeleton, services, owner/kernel receipt and software helpers. Integrate its
validated artifact inputs and required sealed MENU, then assemble the complete
capacity-checked DE25 card through the existing builders. Native bootstrap/FAT/
root switching, stable SSH/DHCP across reboots, watchdog recovery, MENU/catalog
ZX81 launch-stop-relaunch, input, SAVE/persistence, update/restore and the other
[normal-kit requirements](../../design/de25-linux-platform.md) remain open.
HDMI output/reset signoff, firmware/native F2H qualification and the eligible
independent RBF rebuild are unchanged. No new timing exception, PCB delay,
clock change, kernel/SMMU policy or native acceptance is introduced.

Private evidence is retained at
`/home/deano/fes/out/artifacts/de25-factory-inputs/164d3fe49/`. The independent
archive read verifies 16 regular proof/driver/log files: 6,678 bytes, SHA-256
`ae07aed6694aed52bf4bc4587fb350dc765cd06ee9025d9111415b8de4f31c08`.
The base audit, kernel audit and full affected logs are additional hashed
sidecars. The copied source-bundle chain restores the exact tested tree;
374 canonical MENU, 59 combined-compiler and 58 runtime-test input hashes
remain unchanged. No unchanged FPGA/ZX81 compilation or CPU case is rerun.
