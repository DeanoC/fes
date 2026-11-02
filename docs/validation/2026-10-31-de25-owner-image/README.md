# DE25 matching owner-kernel SD candidate

Producer `829cf0353d1ea7a479f95bdbdf3d72e0391ab810`, based on published
platform `3b078d511c8fa05ac72d9eab782c1df57e37c339`.

The separate [offline assembler](../../../scripts/de25_owner_image.py) now
combines the verified headless userspace from
`0da63d1044cc7ecf9908efba52af17b3e55e6271` with the candidate board kernel/DTB
from `e6bdd35973cec91ee3ef93b92862680de3b7988f`. It preserves the base SDK,
libraries and tools; removes the incompatible vendor module tree; and replaces
only the release marker, FPGA startup check and module manifest in the rootfs.
The startup check verifies the built-in FPGA suppliers, owner device and
protected bindings without loading a module or configuring the FPGA.

The assembler authenticates the shared pinned media tools independently of
DE10 disk geometry. Their image ID is recorded in tools.json. All assembly uses
unprivileged tools on regular files; no filesystem mount, block-device access,
board session or deployment occurs. Input directories are read-only container
mounts. The normal headless image selection and runtime admission are unchanged.

Two independent full assemblies have identical image, ext4, tar and FAT-prefix
hashes. The complete image is 3,984,588,800 bytes, SHA-256
`de2a9a4ca35214ab75230d5807a0a8900d99f7869e1346c83f4e41bce0853138`.
The complete runner passed in 38.3 seconds; the named assembly container passed
in 27.3 seconds with its existing 30-second heartbeat and 1,800-second limit.

Verification checks:

- Every regular archive file matches its ext4 bytes; changed configuration has
  root ownership, and the vendor module tree is absent. Each patched filesystem
  passes e2fsck. Archive rewriting retains unrelated data, modes, ownership and
  symlink targets, including BusyBox's `[` and `[[` commands.
- The exact selected Image and DTB replace only their FAT payloads. Original
  MBR, partition geometry, U-Boot and boot script stay locked. Independent FAT
  readback and the shared full-image verifier check partition contents and zero
  padding. The existing FIT/script/DT parser confirms the serial and Ethernet
  descriptions and effective `/dev/mmcblk0p2` root selection.
- The supported first-boot expectation format binds the new kernel release,
  seven userspace configuration hashes, image receipt and rootfs archive. It is
  preparation for a later read-only board collector, not a board observation.
- Independent positive re-verification passes. Seven altered receipts are
  refused: open runtime admission, wrong recipe, empty/inconsistent assembly
  hashes, wrong selected kernel, changed partition and changed FAT alias.
  The retained check script changes private receipt copies and leaves the
  candidate bytes untouched.

The original failed attempts remain recorded: `84cd76638` checked a missing
output parent, `b56153bcb` refused legitimate BusyBox bracket paths, and
`199c96348` could not replace a read-only boot payload copy. The final recipe
checks free space at the nearest existing output ancestor, permits bracket
filenames while refusing traversal/command characters, and makes only its
private replacement payload/prefix copies writable. Base inputs were not
modified. The earlier full affected run at `b56153bcb` passed in 247.9 seconds;
make check at `199c96348`, final focused archive checks, docs and whitespace
checks pass. All six final-producer affected commands pass in 250.5 seconds, including the
real media suites; their job and result receipts are retained alongside this
record. The complete raw test log remains in the named local job directory.

Complete artifacts, both assembly passes and input/source receipts are retained
at `out/artifacts/de25-owner-image/829cf0353/candidate/`; artifact-inventory.json
binds their exact sizes and hashes. No prior kernel or image evidence is
relabeled as this producer. Factory JIC/SOF files in boot/factory-boot are
references required by the shared verifier, not an instruction to replace the
kit's approved HPS-first QSPI baseline.

This is host-only structural/reproducibility evidence. The candidate has not
booted. Actual board supplier/domain admission, SDM firmware loading, userspace
control syscalls and concurrent loader behavior, physical SID/SMMU/firewall/DDR,
DMA/HDMI acceptance, timing signoff, managed AArch64 MENU/catalog/runtime and
normal ZX81 lifecycle/recovery remain open. Normal runtime admission stays
closed. No SD, flash, FPGA, reset or baseline change was performed.
