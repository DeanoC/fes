# Native Go embedded base

This exploratory target runs Go directly on QEMU's single-core AArch64 `virt`
machine through [TamaGo](https://github.com/usbarmory/tamago). There is no guest
Linux, WebAssembly interpreter, libc or dynamically loaded executable. This
is not a DE25 board port: QEMU RAM, PL011 and the firmware entry contract are
explicitly different from the pending Nano hardware.

The `probe` program verifies counter-based time, goroutine scheduling,
allocation/GC and a volatile in-memory filesystem round trip. The `tenfoot`
program runs the existing FogCast `tenfoot.App` and software renderer, navigates
right and left, and exports three actual rendered PNGs through serial. It uses
a read-only in-process HTTP transport fixture and scripted logical input.
It is an application/runtime proof, not evidence for a physical keyboard,
network controller, display or FPGA core.

## Build inputs

The native module pins TamaGo and its modified Go compiler to **1.26.5**, the
Go version used by this FES checkout. Their exact versions and archive hash are
in [toolchain.lock.toml](toolchain.lock.toml). The standard Go compiler cannot
build `GOOS=tamago`. The nested module does not change FogCast's production
toolchain or module requirements.

From the FES worktree root, extract the verified compiler archive under
`out/embedded-base-tools/tamago`. Its archive layout includes
`usr/local/tamago-go/bin/go`. Populate a private module cache explicitly:

```sh
cd platform/embedded/tamago
GOTOOLCHAIN=local \
GOMODCACHE="$PWD/../../../out/embedded-base-tools/tamago/mod" \
../../../out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go mod download
GOTOOLCHAIN=local \
GOMODCACHE="$PWD/../../../out/embedded-base-tools/tamago/mod" \
../../../out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go mod download github.com/usbarmory/tamago
cd ../../..
```

The second command ensures the runtime-overlay module has a local source
directory before the compiler resolves `GOOSPKG`. The build runner performs
no downloads: it selects `GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local` and
`-mod=readonly`. A missing dependency fails rather than fetching implicitly.

With an AArch64 system emulator, run either program:

```sh
python3 platform/embedded/tamago/run.py probe \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-go-probe

python3 platform/embedded/tamago/run.py tenfoot \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-go-tenfoot
```

Output must be a fresh directory under this worktree's `out/`. The runner
records commands, tool identities, compiled Go source/embedded-asset hashes,
the ELF identity/hash, console and result. Tenfoot output also includes
`frame-1.png` through `frame-3.png`, with verified PNG hashes and focus sequence
`0,1,0`. The emulator is stopped by the runner after the final guest marker;
this is not guest shutdown evidence. No physical device is opened.

## Runtime contract and remaining devices

The image starts at EL1, links at `0x40010000` and uses a fixed 256 MiB RAM
window beginning at `0x40000000`. The gap below the image holds the library's
MMU tables. Its early hooks enable floating point and map memory through
TamaGo's ARM64 support. `board/qemuvirt` supplies PL011 serial, direct
architectural counter time, memory bounds and a nonblocking idle hook.
Interrupts are masked; one CPU is used. There is no GIC or DMA driver in this
board package, and EL2 entry is unsupported.

| Component | Current state | Next boundary |
| --- | --- | --- |
| CPU/RAM/Go runtime | Executes on AArch64 QEMU | Nano firmware/DDR/EL1/cache contract |
| Tenfoot UI | Existing App/renderer and navigation execute | Board display buffer and physical input adapters |
| Filesystem | TamaGo RAM filesystem only | Explicit persistent storage adapter; FAT32 and SD block I/O |
| Network | Injected read-only HTTP fixture | Native TCP/IP stack and MAC/PHY driver; `net.SocketFunc` is the runtime seam |
| USB | Absent | DWC3/xHCI host lifecycle, HID and mass storage |
| FPGA | Absent | Identified Agilex endpoint and supported configuration path |

TamaGo's default `os` filesystem is volatile. No `os.WriteFile` result here
means an SD card write. In particular, this target does not yet provide FES's
durable package/media/save semantics. The fixture library deliberately marks
titles unlaunchable. It makes no claim of programming or running a core.

The runtime has no asynchronous goroutine preemption. Device work must return
or yield promptly; a blocked loop can stall the whole single-core application.
The QEMU board currently uses a deterministic PRNG only to seed runtime hash
tables. It is unsuitable for network authentication or cryptography; replace
it with qualified entropy before enabling those services.
