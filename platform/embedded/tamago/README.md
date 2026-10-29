# Native Go embedded base

This exploratory image runs the existing tenfoot application directly on
QEMU's single-core AArch64 `virt` machine through
[TamaGo](https://github.com/usbarmory/tamago). There is no guest Linux, libc,
WebAssembly interpreter or dynamically loaded executable. It is an emulator
integration prototype, not a flashable DE25-Nano image.

The `tenfoot` program mounts a real FAT32 disk through VirtIO, reads its API
configuration and preferences, connects to a private FogCast host through a
real VirtIO NIC and TCP stack, and draws the existing software renderer into
QEMU ramfb at 1280×720. A real xHCI USB boot keyboard supplies live navigation
and settings commands; PL011 remains an explicit diagnostic input option.
The host runs the normal scanner/catalog/API against a private library. Boot
configuration supplies the gateway URL and loopback Host header expected by
the existing private API policy. The runner's sample files are synthetic browsing
data, not
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
loading, sends live USB keyboard navigation and a layout change, captures QEMU's
actual display, independently reads the saved JSON, then checks that a fresh
boot restores the changed layout. The report records tool/ELF/source hashes,
commands, disk identity, logs and screenshots. This proves a clean restart;
it is not abrupt-power-loss or hardware acceptance. Use the runner's
`--help` for interactive operation and host binary selection. USB is the
default; `--input uart` selects the independent serial diagnostic backend.

For manual use, add `--interactive` (USB keys in the QEMU window, Ctrl-] in
the terminal to exit) and choose
`--display sdl` or a supported QEMU display. Use `--disk out/my-boot.img` to retain
preferences between invocations; a per-disk lock covers preparation and both
boots. The runner refreshes boot connection/input configuration on an existing image
without replacing its saved preferences.

`probe` uses the same compiler/QEMU arguments without storage or networking.
All output directories must be fresh and beneath the current worktree's `out/`.

## Platform contract

The image enters at EL1 and links at `0x40010000`. Go owns 256 MiB beginning
at `0x40000000`. The combined profile provides 512 MiB physical RAM: a separate
64 MiB DMA arena starts at `0x50000000`, followed by an 8 MiB framebuffer arena.
`board/qemuvirt.InitDevices` walks the installed page tables and maps the device
arenas as Normal noncacheable, non-executable memory. They never overlap the
Go heap. Disk, network, USB and display retain DMA ownership until completion;
ambiguous device failure retains buffers and requires reboot.

One CPU is used; interrupts are masked. Drivers poll with deadlines and yield
cooperatively. The runtime has no asynchronous goroutine preemption. The
software renderer
and scanout now yield every four rows and split large copies into 32 KiB chunks.
The actual renderer/network stress test passes 30 concurrent 192 KiB responses
with identical pixels and unchanged HTTP timeouts. Other CPU-heavy work still
needs cooperative chunks; this is not a general latency guarantee. No QEMU
address or firmware assumption is a DE25 board constant.

| Component | Implemented here | Still required for DE25 |
| --- | --- | --- |
| CPU/RAM/time | AArch64 runtime, counter, heap/GC, isolated DMA memory | Nano firmware/DDR/EL1/cache and IRQ contract |
| Display | QEMU ramfb via fw_cfg DMA, software RGBA rendering | HDMI/FPGA display plane and generation-bound handoff |
| Input | QEMU xHCI, real USB boot-keyboard control/interrupt transfers; optional PL011 | DWC3/PHY/reset/power integration, hubs and gamepads |
| Storage | VirtIO block reads/writes/FLUSH; FAT32 directories and files | SD controller, card lifecycle and removal |
| Preferences | Existing preallocated FAT32 JSON file; data and metadata flushes | Allocation/update strategy and power-loss-safe publication |
| Network | VirtIO, IPv4 TCP, static address, normal Go HTTP client | MAC/PHY, IRQs, address setup and qualified entropy |
| FPGA | Separate software lifecycle model; no actual FPGA programming | Runtime-owned configuration, identity, reset and DMA revocation |

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

The [fabric lifecycle model](fabricmodel/README.md) runs separately through
`run.py fabric`. It checks ownership and recovery order with software-only
images. It neither replaces the production runtime nor enables tenfoot Play.

## Package inspection without a host OS

`run.py packages` builds a separate native diagnostic, creates a fresh FAT32
image from the existing shared format-2/3/4 conformance corpus, and inspects
its archives through `corepackage.RetainArchive`. The validator is shared with
normal FogCast staging: canonical archive shape, manifest rules, payload/map
hashes and package identity are unchanged. Reads allocate within the declared
archive bound and reject short, extra or non-progressing input. The caller's
reader must enforce deadlines for blocking I/O; cancellation is checked between
reads and validation phases.

```sh
python3 platform/embedded/tamago/run.py packages \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-package-inspection
```

This run needs no remote host, display or USB device. It verifies package IDs
and payloads, corrupts one runner-owned source file, proves the retained payload
is unchanged and a fresh read fails, then restores the exact original FAT32
image. Existing `--disk` images are refused by this diagnostic. Generated
archives are synthetic test fixtures, never deployable bitstreams.

The retained object keeps private bytes in RAM; returned metadata and payload
read cursors cannot mutate it. This is not durable package installation or
hardware admission. A syntactically valid unknown ABI remains valid for
inspection, with compatibility left to the runtime. Format-3/4 ROM maps are
validated, but linking and their activation receipts are not implemented here.
The native FAT reader currently limits individual files to 64 MiB, below the
shared 65 MiB archive cap. Maximum-size memory/latency behavior is unqualified.
No FPGA is programmed and tenfoot Play remains unavailable.

## Runtime admission of verified packages

`run.py admission` connects retained archives to the software fabric lifecycle.
It builds a host-only [oracle](../runtime/admission_oracle.cpp) from the existing
runtime's `OpenCorePackage` and `CheckCoreCompatibility` implementation. This
keeps profile/ABI pairings and interface semantics in their current owner,
libmister-runtime. The helper constructs no physical backend. Its restricted
model-driver snapshot uses generated runtime contract identities, and its
per-package decisions bind the exact verified package ID. The oracle binary
hash identifies the policy producer in the receipt.

```sh
python3 platform/embedded/tamago/run.py admission \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-runtime-admission
```

The runner also needs a host C++14 compiler. It records compiler, oracle,
compiler-reported source/header dependencies, native ELF and FAT32 hashes.
It downloads nothing and refuses an existing disk. `RUNTIME.JSON` is a trusted,
runner-owned **test input**, not an authenticated admission receipt. Editing it
cannot authorize physical programming: this gate has only `LoadModel` and
accepts only the explicit `qemu-model-only` scope. A production backend must
perform its own runtime-owned checks against retained bytes and live hardware.
The C++ checker itself has not been ported into native Go.

The native gate checks the expected package ID, target, programming profile,
ABI, required interfaces and exact-package compatibility decision before calling
`Controller.Load`. Missing policy or decisions fail closed. Successful loads
copy the retained payload into the software model and bind the complete package,
core, ABI version and build identity to the identify/release sequence. Rejection
leaves the current core, input generation and prepared display frame intact.
An accepted replacement still saves the old owner and waits for its frame to
finish; identity failure recovers to idle before release.

The diagnostic runs all eleven structurally valid shared corpus entries plus
six compatibility cases: missing mandatory gamepad, newer ABI, diagnostic-only
profile, unknown required interface, unknown optional interface, and an invalid
optional persistence declaration. The last case requires the C++ semantic check;
matching required capabilities alone would miss it. An unknown optional extension
can pass. Default/absent policy, absent exact-package decisions and wrong expected
IDs are also rejected without changing ownership. A full build-identity mismatch
exercises recovery to idle.

This backend models only simple-game 1.0 with gamepad and fixed video, with a
1 MiB payload bound. It deliberately rejects format-3/4 activation because it
cannot supply linked ROM receipts. Existing DE10-Nano corpus targets remain
DE10-Nano targets; none become DE25-compatible. Production driver resolution,
HPS DDR layout, native display composition and physical lifecycle checks remain
outside this prototype. There is no FPGA programming, live identity proof,
physical hardware acceptance or tenfoot Play support.

The same corpus can run on the host with the race detector after a native run:

```sh
FES_ADMISSION_FIXTURES="$PWD/out/native-runtime-admission/package-fixtures" \
  GOWORK=off go -C platform/embedded/tamago test -race \
  ./runtimeadmission ./admissioncheck ./fabricmodel
```
