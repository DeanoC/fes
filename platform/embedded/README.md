# Embedded FES base

This directory develops the Linux-free DE25-Nano base while hardware is in
transit. Its first target is emulation, not a flashable DE25 image. FES owns
the base image; FogCast owns tenfoot and its application services; the runtime
owns FPGA programming, reset, input delivery and DMA lifetime.

The initial [feasibility study](../../docs/design/de25-embedded-os.md) exercised
a small protocol probe. This work goes further: retain the actual tenfoot
application and renderer, make their platform dependencies explicit, and
exercise CPU/RAM, storage and network services independently of the board.

## Execution model

The leading native-Go experiment uses TamaGo: one boot image containing the
Go runtime, board support and application. It is not a Linux process and does
not need a Linux kernel. Tenfoot is linked into that image initially; loading
arbitrary application executables, a process model and dynamic libraries are
not prerequisites for the launcher. FPGA core packages remain separate data
and bitstream artifacts with their existing identity requirements.

TamaGo is a modified Go toolchain, not an ordinary `GOOS` supported by stock Go.
It preserves the language/runtime needed by the existing UI. Its scheduler
does not provide asynchronous goroutine preemption, so driver waits must be
bounded and CPU-heavy tasks must cooperate. One CPU is enough to establish
correctness; SMP is not part of the initial base.

The Zephyr experiment is a separate executable reference for block storage
and networking. It is not running under TamaGo or supplying services to it.
Its tested filesystem/network behavior helps specify the native base's
adapters. These two images must not be described as a single working system.

The Linux target agent and C++ hardware runtime are not hosted by the native
Go image. A native hardware port must preserve package admission, identity,
containment, media/input ownership and recovery semantics; it cannot replace
those checks with direct UI register writes. Their protocols and generated
definitions remain the authority while that port is developed.

The injected tenfoot loop proves browsing/navigation and, in its explicit model
mode, retained-ROM Play/Back with generation-bound frame and input ownership.
Its software framebuffer remains private, and model grants bracket RAMFB copying.
The Linux menu path and physical pause/drain/DMA handoff are not ported by these
software checks. Board frame grants and physical transitions still need qualified
resources before this can launch on a Nano.

## Services and their boundaries

| Service | Base responsibility | Tenfoot/application boundary | DE25-specific work |
| --- | --- | --- | --- |
| CPU, memory and time | Boot entry, RAM layout, allocator/GC, interrupts, timers, cooperative scheduling | Go runtime, contexts and timers | Firmware entry, RAM reservation, exception level, cache/MMU and interrupt ownership |
| Display | Own framebuffer lifetime, publish frames, wait for completion, revoke safely | Existing tenfoot software renderer and injected presenter | HDMI scanout, shared DDR/DMA and later FPGA display acceleration |
| Input | USB host and HID decoding; bounded normalized event delivery | Existing commands and key/gamepad semantics | DWC2 host controller, hub/enumeration and actual devices |
| Block storage | Sector reads/writes and media presence | Filesystem mounts and file APIs | Cadence SD controller, card setup, DMA and removal behavior |
| FAT32 | Parse directories/files; define durable writes separately | Assets, configuration, ROM/media and preferences | Physical SD writes and power-loss behavior |
| Network | NIC, packet buffers, IP, sockets, timeouts and entropy | Existing HTTP client and service contracts | XGMAC/PHY, address configuration and DMA |
| FPGA | Package admission, SDM configuration, bridges, reset and identity | UI requests a lifecycle operation; it never writes hardware registers | Matched shell/firmware, safe reconfiguration, DDR preservation and recovery |

The emulator's UART, interrupt controller, Ethernet and disk controller are
test backends. Their addresses, PCI identities and initialization sequences
are not board constants. A tested NVMe FAT32 mount does not validate the SD
controller; a working virtual NIC does not validate the Nano's PHY.

The native [TamaGo image](tamago/README.md) runs the actual tenfoot App and
renderer at 1280×720 with QEMU ramfb, live USB boot-keyboard input, a VirtIO
FAT32 disk and a VirtIO TCP/IP connection to a private FogCast host. The host
uses the existing scanner/catalog/API. A two-boot test changes the layout,
flushes its preference file, and verifies restoration from the same disk.
The independent [Zephyr reference](zephyr/README.md) owns separate tests.

| Path | Current result |
| --- | --- |
| Native Go runtime and tenfoot | QEMU CPU/RAM/time, actual 720p output and live navigation |
| Native persistent storage | Real VirtIO reads/writes/FLUSH and FAT32 preallocated preferences across restart |
| Native network | Real IPv4 TCP/HTTP and existing FogCast catalog service |
| Runtime admission model | Exact-package C++ compatibility decisions gate retained payloads before software lifecycle transitions; physical activation unavailable |
| Native package inspection | Existing FES format-2/3/4 validator reads and retains canonical archives from FAT32; no hardware admission |
| Separate Zephyr storage reference | NVMe FAT32 mount and boot-config read |
| Separate Zephyr TCP reference | Builds/starts; E1000 packet exchange remains unverified |
| Physical SD, USB, Ethernet, display and FPGA | Await controller implementation and board verification |

The native software renderer yields in small raster/copy chunks on TamaGo,
so network goroutines can progress during a frame. A controlled renderer-plus-
network test verifies the fix without changing pixels or extending timeouts.

## Tenfoot portability

The application already has a Go software renderer and distinct commands,
snapshots and HTTP client. Platform work should preserve that implementation
instead of replacing it with a second launcher. Keep local host/database
startup outside the UI model. An embedded caller supplies its display/event
loop and chooses its storage and service adapters explicitly.

The native image uses the existing FogCast service on a separate private host,
not an in-process HTTP fixture. The test library contains synthetic files for
browsing, and does not supply playable games. Core launch and hardware support
remain unimplemented.

TamaGo's default `os` filesystem is volatile RAM. Tenfoot's optional
`PreferenceStore` explicitly routes JSON to the FAT32 volume; a successful
write includes block-device flushes. This supports clean-restart persistence,
not atomic publication under power loss. Package/media/save lifecycle semantics
need additional work before they can use this storage path.

The separate native `packages` program now inspects real FES package archives
using the same FogCast validator and identities as the host. It checks the
shared conformance corpus and retains a payload across a source-file mutation.
This does not make existing Cyclone V packages compatible with DE25, enable
ROM linking, or connect the lifecycle model to physical programming.

## Integration sequence

1. Boot native Go and exercise memory, timer, goroutine and file APIs in QEMU.
2. Run actual tenfoot rendering/navigation through an injected platform.
3. Mount a FAT32 image and read launcher configuration and assets through a
   block-device interface; add durable write semantics as a separate step.
4. Connect the existing HTTP client to a real network stack. Keep a private
   host/catalog for tests; avoid changing shared host processes.
5. Feed normalized keyboard/controller events, then attach USB enumeration
   once an emulated and later physical controller path exists.
6. Integrate the FPGA lifecycle with an identified Agilex shell/firmware pair.
   Prove quiescence before reusing any buffer or reconfiguring the fabric.
7. Replace QEMU adapters with Nano drivers as hardware becomes available.

Steps 1–5 now have a combined emulator path, including an xHCI USB boot keyboard. The separate
[runtime-owned lifecycle model](tamago/fabricmodel/README.md) now tests command
ordering, generations and failure retention without programming hardware.
The [package admission diagnostic](tamago/README.md#runtime-admission-of-verified-packages)
now binds exact-package compatibility decisions from the production runtime to
retained bytes and full identity before model transitions. Retained format-3/4
ROM linking now supplies content-bound activation receipts through that model,
including golden-output and source-mutation checks. Next hardware-independent
work now includes retained CPU/video parts composition receipts and the real
tenfoot model-only Play/Back and failure cycle. The next board-facing step is the
[Nano driver backend](board-adaptation.md); physical
integration still needs the Nano board/firmware adapter and runtime checks against
proven board resources. Hardware addresses and DMA/interrupt contracts must be verified against
the delivered board revision.

No emulator result supplies physical boot, HDMI, USB-host, SD-card, PHY or
FPGA reconfiguration acceptance. Hardware arrival should enable those adapter
tests, rather than require starting application/platform separation from scratch.
