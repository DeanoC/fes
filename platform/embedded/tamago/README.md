# Native Go embedded base

This exploratory image runs the existing tenfoot application directly on
QEMU's single-core AArch64 `virt` machine through
[TamaGo](https://github.com/usbarmory/tamago). There is no guest Linux, libc,
WebAssembly interpreter or dynamically loaded executable. It is an emulator
integration prototype, not a flashable DE25-Nano image.

The `tenfoot` program mounts a real FAT32 disk through VirtIO, reads its API
configuration and preferences, connects to a private FogCast host through a
real VirtIO NIC and TCP stack, and draws the existing software renderer into
QEMU ramfb at 1280×720. PL011 serial input supplies live navigation and
settings commands. The host runs the normal scanner/catalog/API against a
private library. Boot configuration supplies both the gateway URL and the
loopback Host header expected by the existing private API policy. The runner's sample files are synthetic browsing data, not
playable ROMs. No core launch or FPGA programming is implemented.

The separate `probe` program checks time, goroutine scheduling, allocation/GC
and TamaGo's volatile in-memory filesystem.

## Build and run

The native module pins TamaGo and its modified Go compiler to **1.26.5**,
matching FES. Exact compiler, runtime and network pins are in
[toolchain.lock.toml](toolchain.lock.toml). Stock Go cannot build `GOOS=tamago`.
Extract the verified compiler archive under `out/embedded-base-tools/tamago`,
then populate the private cache explicitly:

```sh
cd platform/embedded/tamago
GOTOOLCHAIN=local \
GOMODCACHE="$PWD/../../../out/embedded-base-tools/tamago/mod" \
../../../out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go mod download
cd ../../..
(cd sources/FogCast && GOWORK=off go build \
  -o ../../out/embedded-base-tools/fogcast-host ./cmd/fogcast-embedded-host)
```

The runner requires Python 3, QEMU AArch64 with ramfb, and mtools. It creates
only ordinary files under a fresh output directory; it never opens a physical
block device. Compiler builds use `GOPROXY=off`, `GOSUMDB=off`,
`GOTOOLCHAIN=local` and `-mod=readonly`.

```sh
python3 platform/embedded/tamago/run.py tenfoot \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-go-system
```

The default test boots twice on the same FAT32 image. It verifies catalog
loading, sends live serial navigation and a layout change, captures QEMU's
actual display, independently reads the saved JSON, then checks that a fresh
boot restores the changed layout. The report records tool/ELF/source hashes,
commands, disk identity, logs and screenshots. This proves a clean restart;
it is not abrupt-power-loss or hardware acceptance. Use the runner's
`--help` for interactive operation and host binary selection.

For manual use, add `--interactive` (terminal keys, Ctrl-] to exit) and choose
`--display sdl` or a supported QEMU display. Use `--disk out/my-boot.img` to retain
preferences between invocations; a per-disk lock covers preparation and both
boots. The runner refreshes only the boot API address on an existing image.

`probe` uses the same compiler/QEMU arguments without storage or networking.
All output directories must be fresh and beneath the current worktree's `out/`.

## Platform contract

The image enters at EL1 and links at `0x40010000`. Go owns 256 MiB beginning
at `0x40000000`. The combined profile provides 512 MiB physical RAM: a separate
64 MiB DMA arena starts at `0x50000000`, followed by an 8 MiB framebuffer arena.
`board/qemuvirt.InitDevices` walks the installed page tables and maps the device
arenas as Normal noncacheable, non-executable memory. They never overlap the
Go heap. Disk, network and display retain DMA ownership until completion;
ambiguous device failure retains buffers and requires reboot.

One CPU is used; interrupts are masked. Drivers poll with deadlines and yield
cooperatively. The runtime has no asynchronous goroutine preemption. Large
CPU-bound work still requires smaller cooperative chunks: a controlled 180 ms
busy-loop test starved TCP retries, while 2 ms chunks passed. The working
launcher flow does not qualify future artwork decode or latency guarantees. No QEMU
address or firmware assumption is a DE25 board constant.

| Component | Implemented here | Still required for DE25 |
| --- | --- | --- |
| CPU/RAM/time | AArch64 runtime, counter, heap/GC, isolated DMA memory | Nano firmware/DDR/EL1/cache and IRQ contract |
| Display | QEMU ramfb via fw_cfg DMA, software RGBA rendering | HDMI/FPGA display plane and generation-bound handoff |
| Input | Live PL011 terminal keys | USB host enumeration, HID and gamepads |
| Storage | VirtIO block reads/writes/FLUSH; FAT32 directories and files | SD controller, card lifecycle and removal |
| Preferences | Existing preallocated FAT32 JSON file; data and metadata flushes | Allocation/update strategy and power-loss-safe publication |
| Network | VirtIO, IPv4 TCP, static address, normal Go HTTP client | MAC/PHY, IRQs, address setup and qualified entropy |
| FPGA | Not implemented in this image | Runtime-owned configuration, identity, reset and DMA revocation |

FAT32 writes update existing preallocated files only; they cannot create,
allocate, rename or atomically replace them. The preference record reserves
4096 bytes. TamaGo's default `os` filesystem remains volatile; only the explicit
`tenfoot.PreferenceStore` backed by this volume persists settings. Native
Linux tenfoot keeps its existing file-based implementation.

The board's deterministic entropy source only seeds runtime hash tables.
Authentication and TLS are not qualified. The private test API uses numeric
IPv4 HTTP addresses; DHCP, DNS configuration, UDP, IPv6, network recovery and
throughput qualification remain outside this prototype. The separate
[Zephyr reference](../zephyr/README.md) is not part of this boot image.
