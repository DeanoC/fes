# DE25 physical owner binding, userspace control and software reboot

Kernel producer `8e7479d48d87a8b1977cc4040438cf378d35746c`; final image/rootfs
producer `8281d03a1a7c91004af34461bb3a1b300bccc7c8`; actual userspace checker
producer `d64ced1eabf3e514626cd8563e6cb68dba68e3d0`. These diagnostics extend
[the initial physical boot](../2026-10-31-de25-owner-first-boot/README.md).
Normal FES runtime admission remains closed.

The first board kernel booted Linux but correctly refused to bind its owner:
the SMMU's parent soc@0 is bound to simple-pm-bus and still exposed unbind.
The source-bound default-off supplier patch now protects that ancestor too.
No ownership/domain/drain check is weakened. The locked kernel builds this bus
with CONFIG_OF and has no SIMPLE_PM_BUS option; the first repair's incorrect
configuration assertion failed before compile and is retained. The corrected
recipe requires built-in OF and the linked simple_pm_bus_probe symbol.

The corrected complete kernel runner passes in 288.1 seconds (280.2 seconds in
the container). Its Image SHA256 is
01357da8a2f9971cb3bf9e7f99bd176bea46a893554d7b1c5364339310fe9b9b.
The DTB, kernel configuration and symbol-version export are byte-identical to
the earlier kernel. The configuration keeps the KUnit hooks disabled. Exact
supplier before/after hashes, the patch hash, compiler/SDK and source receipts
are retained. Normal vendor image selection and the shared kernel UAPI do not
change.

The first corrected boot binds the owner and starts its built-in suppliers,
but mdev changes its character-node mode to 660. The final assembler prefixes
an explicit root:root mode-600 rule, with chmod for an existing devtmpfs node,
while preserving every byte of the other device rules. A duplicate owner rule
in the base is refused. The preserved base rules are hashed and reverified
against the candidate archive and ext4 contents. An intermediate installer
tried to use stat, which this headless rootfs lacks; its error path restored
the old files. The final source removes that dependency and uses the static
C check's fstat. The failed installation, automatic restoration and subsequent
successful update are separately retained.

The final image independently assembles twice with identical image, rootfs,
archive and FAT-prefix hashes. Its SHA256 is
4e54546fd81a2c5f054a14eac635900c56dea5baa2891434d19cb43bbbfafe37.
The complete runner takes 65.4 seconds; the assembly container takes 61.3.
The bus-repair affected run passes all six commands at f7c05a8b7 in 336.6
seconds. The mdev affected run passes all six commands at c778626e5 in 231.7
seconds. The final source differs from the latter by removing the unsupported
startup stat call and explaining that choice; its actual assembly, four
archive/device-rule cases and physical startup/control checks pass. Five
kernel-platform/native CLI cases also pass. These are not new FPGA timing or
production-runtime results.

## Physical maintenance delivery and two software reboots

The identified bottom board remains exclusively claimed. The corrected
kernel and matching metadata were delivered over pinned-key SSH against the
exact selected boot UUID and SD CID, avoiding another physical card swap.
The old and new payload hashes, unmounted FAT, initial root metadata and
absent owner node were checked first. The old Image and three metadata files
were copied and hash-verified in FAT fes-owner-before-bus before replacement.
The boot DTB did not change. The new Image and root metadata were verified and
flushed, and FAT unmounted before the separate software reboot request.

| Boot | Kernel/root state | Observation |
| --- | --- | --- |
| b989f017-f9f3-45dd-811b-0482b1b65421 | Initial candidate | Linux/SSH pass; owner refuses unprotected ancestor |
| aef6b113-efe8-4d0c-b164-efbf302bfd70 | Corrected kernel, first software reboot | Owner binds, singleton DMA group and protected suppliers; initial mdev mode 660; persistent rule installed and actual userspace check then passes |
| 1cdac9ba-52eb-4268-87c0-d6533ec445f6 | Same corrected kernel, second software reboot | Mode 600 survives startup; actual userspace check passes again; automatic DHCP address 192.168.10.129 and unchanged pinned host key |

The retained C binary is static AArch64, SHA256
b5db0fc187290073086714b94ccbdd6729011cd20c781e4e7730028f931d0032.
It enters actual open/ioctl/fork/capset syscalls on the real misc device:
root ownership/mode 600, sysfs device number and control-fd CLOEXEC agree;
unknown commands return ENOTTY, nonzero arguments EINVAL and unconfigured
allocation ENODEV; the child drops every capability and receives EPERM for
all requests. The parent still sees ENODEV afterward. It publishes no frame,
maps no MMIO and obtains no grant. These are real preconfiguration syscall
checks, not the KUnit callback/private-FD-table model.

After the second reboot, a read-only supplement verifies all six named
supplier drivers exist as built-ins without unbind/module entries, the singleton
owner group reports DMA, and the three installed metadata/device-rule hashes
match the selected image. A read-only FAT mount verifies the selected Image,
unchanged DTB and retained recovery Image hashes. The 84-byte /sys/kernel/notes
hash matches the note range extracted from the selected compiled Image using
its linked System.map. That identifies the running build in addition to the
shared release string; it is not secure measured-boot attestation.

The first generic collection connection after the last reboot timed out before
SSH established. Its partial log is retained without a success receipt. The
subsequent authenticated control check and a fresh read-only collection pass;
offline receipt verification passes. No cause is asserted for the transient
connect failure, and no retry framework or network policy was added. Serial
also shows automatic DHCP; the MAC/IP are not claimed stable across reboots.
Stock mdev still emits a missing-module-directory message because the candidate
has no vendor modules; normal device-driver packaging remains future work.

This was a file-level maintenance update after the initial full SD write.
The live writable SD includes normal first-boot changes and local recovery
files, so it is not claimed byte-identical to the final offline image. Neither
software reboot required JTAG or a physical power cycle. The HPS-first QSPI
was not changed. The complete original pretrial SD extent remains privately
backed up under kit-status with SHA256
9daa6788aff7e07857e3e7a0c349b56b1ae8af1793437837c16169942f63ccfb.

No actual FPGA/SDM load, allocated grant, DDR/SMMU/firewall transfer, HDMI frame
publication, active-DMA reset/drain or production timing signoff is established
by this record. Managed AArch64 MENU/catalog/runtime and normal ZX81
load/run/reload/stop/recovery remain required for DE10-equivalent normal-kit
acceptance. The current candidate is left powered on and idle for that next
integration phase. UART/SSH test clients are closed, no grant was allocated,
and the kit claim is retained; it is not logged as restored or baseline-promoted.

Exact exported kernel/image hashes, operation scripts, raw observations and
failed attempts are retained here and independently under
out/artifacts/de25-owner-binding/20261031. Existing prior artifacts keep their
original producer identity and acceptance limits.
