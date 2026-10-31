# DE25 kernel module packaging checks

FES image scope, base d6d90ef7262e8a290e0ab985514fb3eee55de1c5. The selected
vendor kernel remains unchanged. Four module identities are the exact hashes
from the earlier physical Linux-load proof, now captured in the image recipe.
No runtime/shared definition/FPGA source changes or hardware admission.

Read-only recovery from the factory top board TRWU8CBL verifies all four real
module hashes, relocatable AArch64 ELF identities, vermagic and the complete
dependency closure. [module-info.json](module-info.json) and
[modules.dep](modules.dep) retain those observations. Boot epoch was
34322962-bd96-4fed-a4d5-424327fffebf. No target upload, driver load, service,
FPGA, reboot, flash, SD or boot change occurred. Serial logged out/closed and
SSH reads completed; the top board is idle/released on its factory baseline.
The bottom board stays on the verified persistent bootstrap baseline.

Seven module tests pass in the existing image-builder container, including a
real regular-file ext3/ZIP extraction, authenticated source rejection, module
hash/architecture/kernel/dependency rejection and the actual unchanged S15fpga
script under an isolated chroot. The latter checks wrong-kernel refusal before
modprobe, successful dependency entry points and failed-load reporting. These
are software tests with injected commands, not physical kernel autoload.
Ten existing image-builder tests and four rootfs tests pass.

The full official BSP and resource ZIPs have now passed their original locked
SHA256 hashes. The unchanged production extractor from source
`0da63d1044cc7ecf9908efba52af17b3e55e6271` also passed against the actual BSP:
it authenticated the entire decompressed vendor SD image, recovered all four
modules from its regular-file partition and verified their bytes, AArch64 ELF
identity, kernel release and dependency closure. See
[official-archive-proof.json](official-archive-proof.json). No card was mounted
or written. The original slow stream was replaced by this task's parallel
HTTP Range downloader; the original archive hashes remained required.

The approved baseline's actual AArch64 BusyBox also resolves the generated
`modules.dep` under QEMU in an isolated chroot. Its `modprobe -D` dependency
listing selects bridge, region and OF region in that order, plus the separate
manager. [busybox-dryrun.json](busybox-dryrun.json) binds the BusyBox bytes;
[dependency](modprobe-dependencies-chroot.log) and
[manager](modprobe-manager-chroot.log) logs retain the output. `-D` lists the
operations without executing `insmod`. This is userspace emulation, not a
physical module load or boot-autoload test.

The complete image and C++ SDK build passed at source
`0da63d1044cc7ecf9908efba52af17b3e55e6271`. The
[image receipt](image.json) binds every artifact. Filesystem checks verify the
exact four modules, metadata, dependencies and executable startup script in
both ext4 and tar; see [verify.json](verify.json). The
[QEMU smoke](smoke.json) passes AArch64 BusyBox execution and the Dropbear SSH
banner. It neither runs target init nor tests module insertion, board boot or
SSH authentication. Image SHA256 is
`0d703468eeec8bddec2f591d7acd4cd3c674f94878ec589bca56a3fda22a4171`.
The image, rootfs, SDK and vendor boot artifacts are retained under
`/home/deano/fes/out/artifacts/de25-kernel-modules/0da63d1044cc7ecf9908efba52af17b3e55e6271/linux-image/`.
This independent, read-only artifact copy passes the same receipt checks.
Later evidence-only commits do not change the producer identity.

The newly produced SDK also cross-builds the actual runtime archive, daemon and
agent. Seven C++ unit binaries pass under AArch64 QEMU, including production
board refusal and DE25 computer controls; five agent protocol/readiness tests
and its maintenance-contract command pass. [runtime.json](runtime.json) binds
source `6cbbcacb7` and SDK hash
`7912076c08d93c8d8c28c0ef81bd406abf6b1acb9e01ccac37d44307e39759a7`;
[checks.json](checks.json) lists the cases and compiler. The source difference
from the image producer is evidence only. Logs are retained alongside these
receipts, and binaries independently under the retained artifact set's
`runtime/` directory. This is userspace-only SDK validation: the production
ARMv7/DE10 guard remains closed on AArch64, and no daemon was started on a kit.

An exact-image boot/autoload check remains required. Production AArch64 runtime
admission, bounded failed-load recovery, MENU/DMA and managed catalog flow are
still incomplete. No deployment occurs from these off-board checks.
