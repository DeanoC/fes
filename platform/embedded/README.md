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

The injected tenfoot loop currently proves browsing and navigation. Its
software framebuffer is private; the Linux menu path's generation-bound
pause/drain handoff is not implemented in this loop. Physical frame grants
and core transitions need that lifecycle before this can launch on a board.

## Services and their boundaries

| Service | Base responsibility | Tenfoot/application boundary | DE25-specific work |
| --- | --- | --- | --- |
| CPU, memory and time | Boot entry, RAM layout, allocator/GC, interrupts, timers, cooperative scheduling | Go runtime, contexts and timers | Firmware entry, RAM reservation, exception level, cache/MMU and interrupt ownership |
| Display | Own framebuffer lifetime, publish frames, wait for completion, revoke safely | Existing tenfoot software renderer and injected presenter | HDMI scanout, shared DDR/DMA and later FPGA display acceleration |
| Input | USB host and HID decoding; bounded normalized event delivery | Existing commands and key/gamepad semantics | DWC3 host controller, hub/enumeration and actual devices |
| Block storage | Sector reads/writes and media presence | Filesystem mounts and file APIs | Cadence SD controller, card setup, DMA and removal behavior |
| FAT32 | Parse directories/files; define durable writes separately | Assets, configuration, ROM/media and preferences | Physical SD writes and power-loss behavior |
| Network | NIC, packet buffers, IP, sockets, timeouts and entropy | Existing HTTP client and service contracts | XGMAC/PHY, address configuration and DMA |
| FPGA | Package admission, SDM configuration, bridges, reset and identity | UI requests a lifecycle operation; it never writes hardware registers | Matched shell/firmware, safe reconfiguration, DDR preservation and recovery |

The emulator's UART, interrupt controller, Ethernet and disk controller are
test backends. Their addresses, PCI identities and initialization sequences
are not board constants. A tested NVMe FAT32 mount does not validate the SD
controller; a working virtual NIC does not validate the Nano's PHY.

The native [TamaGo image](tamago/README.md) runs the actual tenfoot App and
renderer at 1280×720 in QEMU. Its three captured frames verify selection
moving right and back left. The service data and input are fixtures, and the
frame is exported over the diagnostic serial channel rather than scanned out
to HDMI. The independent [Zephyr reference](zephyr/README.md) owns its own
filesystem/network tests.

Current evidence is deliberately split:

| Path | Current result |
| --- | --- |
| Native Go runtime and tenfoot | QEMU boot, memory/timer checks and real 720p UI navigation pass. |
| Separate Zephyr storage reference | Real NVMe block I/O, FAT32 mount and boot-config read pass. |
| Separate Zephyr TCP reference | Builds/starts, but packet exchange remains unverified because of the QEMU E1000 driver path. |
| Native-Go persistent storage and network | Adapters are not connected yet; the UI uses a fixture and volatile files. |
| Physical SD, USB, Ethernet, display and FPGA | Await controller implementation and later board verification. |

## Tenfoot portability

The application already has a Go software renderer and distinct commands,
snapshots and HTTP client. Platform work should preserve that implementation
instead of replacing it with a second launcher. Keep local host/database
startup outside the UI model. An embedded caller supplies its display/event
loop and chooses its storage and service adapters explicitly.

For early execution, a fixture-backed service can exercise real rendering and
navigation. Such a service is not the FogCast library server and cannot grant
hardware support. Actual catalog/session operations, authentication and
persistence need their normal contracts before being enabled.

TamaGo's default `os` filesystem is volatile RAM. A successful `os.WriteFile`
is not proof of a persistent SD write. A FAT-backed VFS or explicit staging and
durable publication boundary must be implemented before claiming persistent
settings, installed packages or saves. Likewise, Go's socket hook needs a real
network stack and NIC adapter before ordinary HTTP can reach another machine.

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

Steps 3–5 can advance before delivery: use an emulated block controller and
NIC with the native Go base, then connect real file/HTTP clients and normalized
input. The native runtime's `net.SocketFunc` hook is the network seam; its
volatile filesystem needs an explicit persistent-storage integration. USB
descriptor/HID and FPGA protocol tests can run against models, with physical
host-controller and configuration support kept distinct.

No emulator result supplies physical boot, HDMI, USB-host, SD-card, PHY or
FPGA reconfiguration acceptance. Hardware arrival should enable those adapter
tests, rather than require starting application/platform separation from scratch.
