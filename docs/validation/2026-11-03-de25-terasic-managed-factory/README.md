# Terasic-based DE25 managed factory and card

The ordinary FES source and appliance commands now produce a complete DE25
factory, release, boot selector and reproducible card. The actual selected
kernel and root filesystem pass the image packaging boot on AArch64 QEMU.
This establishes image production and boot smoke; managed MENU/catalog ZX81,
software activation, updates and recovery still need the physical kit.

The [evidence summary](evidence.json) records exact producers, artifacts and
test scopes. Private card images and provisioning credentials are retained
locally and are not repository artifacts.

## Vendor baseline and FPGA evidence

The kernel derives from Terasic's `linux-socfpga` revision
`d7d192a9ddd955fe206aa350afed8f82e368a74c` (6.12.11), with the existing FES
owner/DMA changes. FPGA admission uses the exact locked rev-B resource archive,
its board pins, selected video PLL and HDMI/I2C setup. It checks the actual
core-specific PLL project path for MENU and ZX81.

The normal MENU and ZX81 producers each complete independent paired builds
with identical payloads within their pair. Timing reports retain the two
approved asynchronous HDMI-input findings and thirty output findings reviewed
against the vendor reference. There are no Quartus rule waivers. This is vendor
reference acceptance; independent PCB electrical/timing qualification remains
unclaimed. Other clock, reset, crossing and routed timing checks remain required.

The Powerboat MENU selected by the factory has build ID
`93eb139a58a2516b4776de5d4a08d208`. Its thirteen board, ten transport and
seventy-two startup model cases pass. Both fitted copies pass all four corners:
280 board paths and 3,096 FIFO paths per fit, minimum slack 0.102 ns. The
original projects remain unchanged. These checks do not establish native DMA,
metastability or managed display acceptance.

## Integration repairs found by actual commands

The normal Go archive reader initially rejected DE25 packages. It now accepts
the exact DE25/SCS platform/device pair alongside the existing DE10 pair;
crossed and unknown pairs still fail before publication. The source-built host
imports and reads back actual MENU and ZX81 packages, and a fresh process reads
the retained catalog without reimporting them.

The source image path omitted `fes-kit-data` and `fes-kit-software`. It now uses
the existing explicit AArch64 builders and packs all six required executables.
Its factory call retains the outer parent lock and acquires the media lease,
avoiding the earlier attempt to take its own parent lock twice. Tests probe
both real locks while the producer callback runs.

The first full OS assembly then detected a runtime byte mismatch. Buildroot
removed 104 bytes of compiler metadata after the selected binary was sealed.
Applying the original compiler's existing final strip policy to a separate
copy produced exactly the installed hash. The source path now applies that
policy before sealing, using the same authenticated SDK/container. The original
failed image, selected binaries and controlled comparison remain retained;
the executable identity check is unchanged.

## Actual image and public commands

The successful software/factory producer is
`a95b51c493793b4af8bf4994e68b5d3907810a5a`. The subsequent test-only commit
`40f2953b9ebc1d130fe6d110907e3728b8750992` checks admission ordering before
and inside the media lease; it changes no producer or deployed source.

| Command or check | Result | Scope |
| --- | --- | --- |
| `make host BOARD=de25-nano` | PASS | Real host binary; package import/readback and retained catalog |
| `make dev BOARD=de25-nano` with verified managed SDK/base and matching kernel/compiler | PASS, 453.6 s | Real AArch64 programs, sealed MENU, full managed factory and actual image boot |
| Factory verifier | PASS | Bundle/base/MENU contracts, executable identity, aliases and 37 critical tar/ext4 files |
| Kernel/rootfs QEMU boot | PASS, 45 s observation | Read-only root, writable volatile mounts, live D-Bus and ready marker |
| Production runtime loader, QEMU A53/A76 | PASS | Actual factory loader/libraries reach the intentional argument guard before hardware construction |
| `make release APPLIANCE_BOARD=de25-nano` | PASS, 9.9 s | Reverified actual factory; release `0.2.0-de25.a95b51c49379` |
| `make bootstrap APPLIANCE_BOARD=de25-nano` | PASS, 12.7 s | Actual static AArch64 boot selector and factory binding |
| `make appliance-media APPLIANCE_BOARD=de25-nano` | PASS, 49.3 s | Two independent 2,182,086,656-byte constructions compare exactly |

Factory production records one actual OS build pass. The two card constructions
prove disk assembly reproducibility and do not promote that to two independent
OS builds. The card preserves the checked Terasic boot dependencies and uses
the selected owner kernel/DTB, 2 GiB FAT storage and partition-2 bootstrap.

The Go reader repair passes its four focused packages and full affected host/UI
checks. The helper repair passes 1,436 extended parent tests. The lock repair's
extended run fails only the old single-admission-call assertion; the test-only
ordering repair passes all 32 artifact tests and its routine affected checks.
The runtime normalization has focused workflow and actual factory evidence;
its extended pre-test-fix run retains that same obsolete assertion separately.

## Remaining native acceptance

The new factory/card has not booted on a board at this milestone. Earlier
three-frame DDR/display and software-reboot evidence belongs to its exact older
maintenance artifact and is not reused as acceptance of this factory's MENU.
Next are the physical managed boot, agent/runtime identity, captured MENU,
normal catalog ZX81 input and BASIC load/run/reload/persistence, return to MENU,
relaunch, software activation and bounded recovery in the
[normal-kit requirements](../../design/de25-linux-platform.md).
