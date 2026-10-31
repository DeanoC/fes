# DE25 owner userspace check preparation

Producer `d64ced1eabf3e514626cd8563e6cb68dba68e3d0`, based on platform
`7700de82ac3561e18a0d7dc6c9a2dd4f9cb65b11`.

The standalone [userspace check](../../../image/kernel/de25-platform/owner_unconfigured.c)
is ready for an exclusively claimed, fresh owner-candidate board boot before
any FPGA overlay is applied. It uses the exact shared kernel UAPI and actual
open/ioctl/fork/capset syscalls. It checks kernel, board/rootfs and selected boot
identity, root-only character-device metadata/sysfs device number, and the
control descriptor's CLOEXEC bit. With the existing computer maintenance lock
held, unknown requests and nonzero arguments must be refused, and allocation
must return ENODEV while the owner is unconfigured. A child drops every
capability and must receive EPERM. The parent rechecks unconfigured refusal
and boot identity after the child exits.

No frame is published and no MMIO is mapped. If allocation unexpectedly returns
a new grant, the check fails and attempts to reap only that newly acquired
grant before closing it. EBUSY never triggers a reap of another client's
allocation. Run only before FPGA configuration under a whole-board claim; the
userspace maintenance lock does not serialize arbitrary privileged loaders.

Native compile with C11/Wall/Wextra/Werror and all three CLI/host-refusal cases
pass. The automated kernel-platform suite passes five cases, including the new
compiled native check. The locked SDK builds a static AArch64 binary (835,304
bytes, SHA256 `b5db0fc187290073086714b94ccbdd6729011cd20c781e4e7730028f931d0032`);
readelf confirms AArch64 and no interpreter or dynamic dependency. The actual
binary under qemu-aarch64-static passes the same three refusal cases. The
complete build runner passes in 7.9 seconds. The original attempt used the
absent qemu-aarch64 name; its failed receipt/log remain retained. The existing
builder's qemu-aarch64-static resolves that wrapper error without changing the
candidate kernel or image.

These are host-only compilation/refusal checks, not a successful owner open,
permission syscall, grant allocation, DMA or physical board test. A hardware
pass marker still requires execution on the identified candidate boot. Binary,
SDK identity, source/header hashes, readelf output and reproducible private build
scripts are retained in out/owner-client-build-d64ced1ea-v2. All six affected
commands pass against the exact producer in 241.7 seconds, including the real
media suites; job and result receipts are retained here.

A claimed UART preflight before card preparation confirmed the bottom board's
original boot `e8963972-c3e9-405c-a2ef-93eb09f691e6`, kernel
`6.12.11-gd7d192a9ddd9`, SD CID `275048534436344760702d33e0017c00` and Ethernet
address 192.168.10.111. FPGA modules were unloaded; that observation is not a
new FPGA-load or video test. The session logged out and released without
changing the board. The later separately logged SD trial is not covered by
that preflight's unchanged-baseline statement.
