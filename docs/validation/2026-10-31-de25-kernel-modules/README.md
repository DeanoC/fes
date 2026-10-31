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

A full official BSP download is proceeding from Terasic; the initial single
stream was replaced by owned parallel Range reads after measured12KB/sec
throughput. Only this task's download PID was stopped; complete initial chunks
were retained. The final assembled ZIP must pass the original lock hash before
the production extractor can consume it. Reading known module bytes from the
factory board does not substitute for that archive/source validation.

Image build, QEMU rootfs packaging and an exact-image boot/autoload check remain
required. Production AArch64 runtime admission, bounded failed-load recovery,
MENU/DMA and managed catalog flow are still incomplete. No deployment occurs
from these off-board checks.
