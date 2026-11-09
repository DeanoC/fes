# Embedded FES base

This directory develops the Linux-free DE25-Nano base. QEMU supplies combined
launcher integration, while bounded Nano CPU/SD/package diagnostics now run on
the top kit. Playable-core readiness remains open. FES owns
the base image; FogCast owns tenfoot and its application services; the runtime
owns FPGA programming, reset, input delivery and DMA lifetime.

The native [SD PIO driver and owned session](tamago/sdhci/README.md) also have an
explicit write-mode prototype. Host tests cover sector writes, uncertain-write
retirement, stop/resume and FAT settings persistence with independent readback.
Current Nano board builds still select read-only access; enabling and physically
qualifying writes remains separate from the accepted recovery-loader uploads.
The [FAT32 allocation path](tamago/fat/README.md) now creates files and directories
without preallocation, with independent mtools readback and real package-store
retention tests. The [owned in-process installer](tamago/nativefabric/README.md)
now validates uploads in RAM, writes verified content through the active SD
lease, publishes the catalogue last and discovers it on a modeled fresh boot.
Real HTTP/FAT-image tests now cover download of an existing Nano export,
compiled admission, installation, launch/return and modeled fresh-boot discovery.
The Linux-free QEMU `programfetch` diagnostic separately verifies package/ROM/
tape identities over VirtIO HTTP and leaves its FAT disk unchanged. A native
network upload service, NIC containment and hardware write qualification remain
separate work; Nano networking and SD writes stay disabled in the board profile.

The current [queued-menu hardware trial](../../docs/validation/2026-11-09-de25-menu-queue-trial/README.md)
passes ZX81 (+917-byte tape) → menu → ZX80 → menu → ZX81 (+same tape) → menu
and final Park on TRWU8CBL. All eight App exchanges replay from the saved serial
trace; seven FPGA configurations and four menu sessions complete. Both tape
loads take about 1.206 seconds using size-dependent transfer budgets. Independent
programming-connection cleanup and full evidence/source restore checks pass.
This is a bounded diagnostic: physical display/input, ordinary appliance loading,
general USB reliability, native Ethernet and whole-board timing remain open.
The new native keyboard return chord has software coverage and still requires
its own rebuilt matched image and physical test.

The [paired menu/core/menu hardware trial](../../docs/validation/2026-11-08-de25-verified-files-board-trial/hardware.md)
now passes ZX81 → menu → ZX80 → menu → ZX81 → menu and final Park on TRWU8CBL:
three launches/returns, four menu sessions and seven configurations. The native
collector completes in 428.67 seconds within its original 450-second limit.
This remains exact retained serial evidence; visible transitions and ordinary
appliance admission remain open. The [next game-tape path](../../docs/validation/2026-11-08-de25-native-game-tape/README.md)
retains selected SD media and delivers it before CPU release through the existing
driver, with software tests; its changed native image has no board acceptance.

The [standard-package native boot path](../../docs/validation/2026-11-06-de25-computer-runtime/README.md)
now connects the shared Nano `fes.computer` contract to the retained-ROM loader,
Tenfoot backend and existing owned firmware controls. Off-board tests cover
launch, return and failures, and a matched SDK/entry/BL31/SPL/FIT/SOF candidate is
prepared. The refreshed [physical serial launch/return test](../../docs/validation/2026-11-07-de25-timer-launch-trial/README.md)
passes eight verified uploads and three native ZX81/alternate-ROM/ZX81 cycles,
including master stop, shared USB pause/resume and owned Quit. This is serial
runtime evidence; general package admission, full core/video conformance,
physical display/input/media/native networking and FPGA timing remain open.

The [distinct-package physical test](../../docs/validation/2026-11-07-de25-distinct-observer-trial/README.md)
now passes ZX81 → ZX80 → ZX81 with three complete native launch/return cycles,
exact selected package/build/ROM identities, nine verified uploads/fresh reads
and owned Quit. Launches take about 55 seconds. This remains serial evidence;
visible menu/core transitions and broader admission remain open.
The [owned menu video prototype](tamago/scanout/README.md#owned-menu-video-prototype)
now has an explicit 8 MiB two-slot native binding. Its
[bounded physical test](../../docs/validation/2026-11-07-de25-native-cpu-feature-trial/README.md)
passes three Tenfoot frame acknowledgements in slots 0/1/0 with zero reported
underflows, local drain and independent firmware master-stop/bridge closure on
TRWU8CBL. The opt-in native CPU feature-discovery overlay reduces presentation
from about 3.8 to 1.3 seconds per frame; interactive throughput still needs work. This is serial reader evidence; physical HDMI, full menu/core/menu
transitions and ordinary package admission remain open.

The [follow-on timing repair](../../docs/validation/2026-11-06-de25-timing-repair/README.md)
now clears the inferred capture reset-polarity conflict and two obsolete JTAG
filter queries in a freshly routed package. Four signoff rules remain open.
The new package passes native retained-ROM preflight without firmware access;
its matched boot chain and physical acceptance remain pending.

The experimental [native computer driver](tamago/computer/README.md) now checks
the captured DE25 ZX81 Avalon/GP interface through injected platform I/O. Its
explicit host model tests cover release, input, tape transfer, hold, relaunch
and faults against the actual captured core RTL. A matched
[persistent native service diagnostic](../../docs/validation/2026-11-03-de25-service-hardware/README.md)
now completes three same-package ZX81 load/Run/Hold/return sessions on TRWU8CBL. Physical
input/video and ordinary runtime core admission remain open.

The [two-package board test](../../docs/validation/2026-11-03-de25-distinct-hardware/README.md)
now passes ZX81 → ZX80 → ZX81 with six verified Ethernet uploads, fresh native
SD retention and request-driven Run/Hold/return. Both sealed payloads use the
same contained platform. The [current-payload DDR test](../../docs/validation/2026-11-03-de25-computer-ddr/README.md)
now also passes one fixed 32-byte FPGA read, reader/master drain and bridge
reclosure. Native tenfoot/input/video, general DDR use and ordinary FES admission
remain open.

The [full-width window hardware test](../../docs/validation/2026-11-04-de25-fullwidth-window/README.md)
passes one complete 1280×720 tenfoot frame on TRWU8CBL. The new 256-bit DDR
boundary delivers all 3,686,400 bytes with the native CPU publication's CRC,
then completes reader/master drain and bridge reclosure. Its 4 MiB reservation
stays pinned until reboot. The later [repeated-frame hardware diagnostic](../../docs/validation/2026-11-04-de25-buffered-window/README.md)
passes twelve changing native frames, a local reader drain before each arena
update, and final firmware master stop and bridge closure. Full-arena hashes
use private CPU scratch storage with unchanged coverage and ownership checks.
This qualifies bounded updates within that fixed source and reader scope.
Physical HDMI, USB hub input, native networking and ordinary package admission
remain open.

The [complete-command-handler board test](../../docs/validation/2026-11-04-de25-transfer-hash/README.md)
now passes native boot, retained SD package, twelve changing frames and owned
return within the unchanged 45-second observer bound. Transfer-side DMA hashes
use fresh reads through private 32 KiB scratch; firmware checks are unchanged.
The normal UI loop, menu/core transitions and ordinary admission remain open.

The [two-load window diagnostic](../../docs/validation/2026-11-04-de25-window-two-loads/README.md)
now passes twelve frames and owned return, fresh SD retention/containment,
then a second configuration and two frames with a new drain/closure on the
same boot-pinned arena. Both configuration activations are checked. This
remains a bounded diagnostic; the persistent native frontend, physical HDMI,
USB hub input and ordinary package admission remain open.

The [persistent window-service board test](../../docs/validation/2026-11-04-de25-window-service-hardware/README.md)
now passes three request-driven sessions, six changing full frames, two fresh
SD cycles, six expected stale-request rejections and final Park on TRWU8CBL.
The real Tenfoot renderer uses the same fixed-package native owner throughout;
independent firmware closures are observed at generations 7, 13 and 19.
This qualifies that diagnostic service. The full Tenfoot App/backend,
ordinary package admission, HDMI, USB hub input and native networking remain open.

The [contained firmware/RTL prototype](firmware/README.md) prepares a pinned
TF-A source variant and an FPGA identity/read-drain endpoint. The experimental
[native endpoint](tamago/nativefabric/README.md) now connects their protocols
to the existing lifecycle and tenfoot model actor through injected hooks. Host
tests use the actual firmware functions and RTL with simulated platform services;
the native image's Nano discovery remains read-only until board routing, reset
and memory are qualified.

The opt-in [owned bridge SiP binding](../../docs/validation/2026-10-31-de25-owned-sip/README.md)
now cross-builds inside BL31 and connects to the native runtime through the
shared wire contract. The matched cold-boot diagnostic now supplies trusted
ownership for its closed-bridge, read-only SD subset. Active-fabric launch/return
and failure tests still use injected platform state and require further board
qualification.

The experimental [native boot entry](entry/README.md) uses the vendor U-Boot
source in QEMU to cleanly hand a validated ELF to the Go runtime at EL1. A Nano
candidate uses an explicit firmware profile; bounded EL2/EL1 handoff and
read-only SD/package retention now pass on TRWU8CBL.

The [Nano serial-probe recipe](nano/README.md) now assembles a source-derived
memory/device gate and matched SPL/FIT/SOF/test-SD files. The bounded serial
earlier trials stopped at the firmware bridge gate. The subsequent
[KSZ9031 recovery and native package trial](../../docs/validation/2026-11-01-de25-ksz9031-packages/README.md)
passes its narrow CPU/SD path; other native peripherals remain unqualified.
The [HPS-first native composition tool](nano/README.md#compose-the-hps-first-fpga-diagnostic-with-native-spl)
now pairs the sealed FPGA diagnostic with the matched native SPL as regular
files. It checks the embedded SPL and unchanged canonical core payload, while
keeping live boot assertions and physical programming admission disabled.
The [bounded HPS-first loader observation](../../docs/validation/2026-11-01-de25-native-hps-pair/README.md)
passed DDR setup, sealed FIT verification and the matched loader prompt on
TRWU8CBL. That observation stops before native EL1 entry and runtime FPGA
configuration.

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
| Physical CPU and read-only SD | Bounded native boot, FAT32/package retention and secure SD stop/resume pass |
| Physical contained FPGA loading | Persistent same-package load/Run/Hold/return across three sessions, with fresh retention and guarded control; diagnostic profile only |
| Physical USB, native Ethernet and display | Require board verification and qualified lifecycle integration |

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

The Nano's optional [boot-owned scanout](tamago/scanout/README.md) now shares
pixel-copy code with QEMU RAMFB. It requires compatible preinitialized firmware
and a dedicated profile reservation; no Nano bitstream or physical drain is
provided. The reservation remains pinned until reboot, so this does not enable
physical FPGA replacement or core launch.
