# Emulated FES base services

This independent Zephyr image exercises a small set of Linux-free base
services before DE25-Nano hardware is available. It is a driver/service
reference for the [embedded study](../../../docs/design/de25-embedded-os.md),
not a DE25 board image or a native-Go application host.

The image uses real Zephyr kernel, interrupt, PCI, NVMe, FAT filesystem,
Ethernet and TCP implementations against QEMU devices. FAT32 is mounted
read-only and `FESBOOT.CFG` is read through the filesystem API. The config
names tenfoot as the intended launcher; this image does **not** load or
execute tenfoot. It does not share a kernel with the separate Go experiment.

| Service | Implemented/testable boundary | Not established |
| --- | --- | --- |
| CPU/RAM | AArch64 kernel boot, static service buffers, threads/timers | Nano boot, LPDDR4 configuration or memory qualification |
| Storage | QEMU PCI NVMe block I/O, real FAT32 mount and config read | Nano SD controller, writable persistence or power-loss behavior |
| Network | QEMU E1000 driver and IPv4 TCP listener compile/start | Packet exchange is currently blocked; no network acceptance |
| Input | Bounded normalized button state with monotonic sequence | USB controller, HID enumeration or input delivery to an FPGA |
| FPGA | Explicit unavailable response | Programming, bridge ownership, DMA or a playable core |

Real NVMe/FAT32/config reads have completed. E1000 packet exchange has not:
the full network verifier fails its TCP readiness deadline. Use the explicit
storage-only check below for the established evidence.

The QEMU devices are deliberate substitutes at the OS driver boundary. NVMe
is not called an SD driver; E1000 is not called the Nano Ethernet controller.
The configured pseudorandom generator is for this isolated test image only.

## Build and verify

Prerequisites are CMake, Ninja, Python 3.11+, mtools (`mformat`, `mcopy`,
`mdel`), Zephyr SDK 0.17.4 with its AArch64 compiler and host QEMU, and the
Python packages used by Zephyr. The earlier protocol experiment's SDK and
virtual environment can be reused read-only. No test invokes a download.

The pinned inputs are:

- Zephyr v4.2.0: `413b789deb391d3a37d06b463288a5fe765ee57e`
- FatFS module: `16245c7c41d2b79e74984f49b5202551786b8a9b`
- SDK: 0.17.4; archive hashes remain in
  [the experiment lock](../../../examples/de25-embedded-os/toolchain.lock.toml).

Prepare the sole extra Zephyr module in a private output directory:

```sh
mkdir -p out/embedded-base-tools
git clone https://github.com/zephyrproject-rtos/fatfs.git out/embedded-base-tools/fatfs
git -C out/embedded-base-tools/fatfs checkout 16245c7c41d2b79e74984f49b5202551786b8a9b
```

Then from the FES worktree root:

```sh
python3 platform/embedded/zephyr/verify.py \
  --zephyr-base out/de25-os-tools/zephyr \
  --sdk out/de25-os-tools/zephyr-sdk-0.17.4 \
  --fatfs out/embedded-base-tools/fatfs \
  --python out/de25-os-tools/venv/bin/python \
  --storage-only \
  --output out/embedded-base-verify
```

The output directory must be new and under the worktree's `out/`. The helper
creates regular disk-image files only, launches only its own QEMU processes,
and binds forwarding to a selected loopback port. It stops those processes
after the scenarios. No TAP device, physical device or external network
service is required.

The storage-only scenarios assert FAT32/config readiness through the guest
console. Separate images have a missing or malformed config, or an otherwise
valid config followed by a NUL and unexpected bytes. FAT32 must still mount
while config readiness remains false; config admission compares the exact
byte count and complete contents. Each run verifies the read-only
image bytes did not change. Logs,
commands, source hashes, device responses and image/ELF identities are kept in
`result.json` and neighboring files. These are emulated-device checks.

Omitting `--storage-only` additionally requires real TCP status/input and
rejection checks. That path is implemented but currently fails; do not describe
a storage-only pass as network support or as tenfoot running.

## Small diagnostic TCP protocol

Port 4242 accepts one ASCII request terminated by LF, then returns one JSON
line and closes. The request buffer is 128 bytes, replies have a 768-byte
buffer, and every client has a bounded receive/send observation window. This
is not the FogCast public API or a substitute implementation of its leases.

- `STATUS` reports actual storage/config status and diagnostic input state.
- `INPUT <sequence> <buttons>` records a uint32 sequence greater than the last
  accepted sequence and a uint16 button mask. It returns
  `"route":"diagnostic-only"`; no FPGA or operating-system input is injected.
- `USB`, `SDCARD`, and `FPGA` return `"error":"unavailable"`.
- Other requests are refused without changing input state.

The boot config is exactly:

```text
format=fes-base-v1
launcher=tenfoot
```

The service does not format an unknown disk or silently create a missing
config. The helper constructs a separate, known fixture image using mtools.

## Narrow NVMe source adaptation and remaining network blocker

Zephyr 4.2.0's `drivers/disk/nvme/nvme_controller.c` includes an unused
`<soc.h>`, but the Cortex-A53 QEMU platform supplies no such header. CMake
checks the original SHA-256
`35b50200a5d391860096b2ae340e60f6e8dc82f12e8c34b6a12a9bb3e5b573e6`
and exactly one occurrence of that include, then writes a build-local source
copy replacing that include with a comment. It also enables PCI memory decode
and bus mastering before the first controller read: the pinned NVMe driver
assumes boot firmware already enabled those bits, but QEMU's direct kernel
entry has no such firmware. It compiles the copy in the
same driver target. The immutable upstream checkout is not edited; there is
no empty-header shim. A changed upstream input fails configuration for review.

NVMe uses GICv3 ITS/MSI-X, so this profile enables dynamic interrupts and ITS
explicitly. Its 1 MiB kernel heap accommodates the 64 KiB-aligned GIC LPI tables;
an initial 64 KiB heap could not do so, causing a null-memory access during
early GIC initialization. Those mechanisms are exercised by storage requests, not inferred
from a successful compiler run. Any future Nano profile needs its own
controller, DMA and interrupt integration.

E1000 reaches interface-up with the configured IPv4 address. Packet capture
shows host ARP broadcasts with no successful guest reply; its pinned driver
uses a single RX/TX descriptor and enables multicast acceptance but not
broadcast acceptance. A bounded broadcast-bit experiment still failed, with
QEMU reporting Slirp send errors, and was removed. The upstream E1000 source
is unmodified. Further driver work is separate from the proven storage slice.
