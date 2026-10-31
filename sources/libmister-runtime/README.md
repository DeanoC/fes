# libmister-runtime

Development lives in the FES repository under `sources/libmister-runtime`.
The former standalone repository is archived.

The C++14 library and local `mister-runtime` daemon own FPGA programming,
physical lifecycle, media/input delivery, and recovery. FES described packages are the only product launch path. The installed driver supports
`fes.simple-game`, `fes.simple-computer`, `fes.application`, and
`fes.computer` through `fes-gp-v1`. Protocol 2 is the only local socket protocol.

Production hardware construction requires an ARMv7 Linux binary and the exact
DE10-nano device-tree identity used by FES boot/reset preparation. AArch64,
DE25-Nano, unknown boards and missing or malformed identity are rejected before
kernel-module loading or construction of MMIO/I2C adapters. The daemon retains
its existing unavailable-hardware adapter and local protocol after refusal;
it advertises no programming capabilities. An AArch64 compile or QEMU software
test does not add DE25 hardware support.

The internal `LinuxDe25FpgaManager` uses the existing retained-Artifact and
programming-result interfaces for kernel configfs/SDM programming. Its
`de25_hps_first_v1` enum maps to the DE25-only shared `de25-hps-first-v1` row.
The default platform and production factory still reject DE25. An explicit
DE25 metadata/driver-registry selection permits only the exact BRAM computer
contract; it does not construct the physical lifecycle. It retains the caller's
existing maintenance lease, binds the actual running kernel notes to the matched-image
receipt, stages firmware exclusively, and checks completion after closing the
configfs binary attribute. No Cyclone V register or DDR-release recipe is used.
Unsuccessful, partial or late programming poisons that context and preserves
configuration/firmware; destruction never unloads it. A new context refuses an
existing runtime overlay, including after a late successful kernel callback.
Restart adoption and bounded whole-load recovery remain unfinished. Software
unit coverage is separate from physical adapter acceptance; the [dated record](../../docs/validation/2026-10-31-de25-runtime-programmer/README.md)
identifies the exact kernel, adapter and known BRAM core tested on the board.

The internal `De25ComputerCoreDriver` now implements the shared driver lifecycle
and optional HID/tape calls through that atomic client. It accepts only the
exact BRAM computer ABI/interface/build contract, keeps launch media held until
Start, replaces live tape without reset, and never clears a poisoned transport
at a session boundary. NativeHardware dispatches HID and basic tape operations
through its selected CoreDriver rather than a DE10 concrete-class cast. This
new driver has a [dated two-load hardware diagnostic](../../docs/validation/2026-11-01-de25-shared-lifecycle/README.md); its DE25 registry binding requires
explicit platform selection and is not constructed by the production factory and has no new physical acceptance claim. Unit-media limits/state are observed during identity, insert/eject use retained
snapshots through the selected driver, and uncertain transfer preserves an
unready state without cleanup commands. The known BRAM image advertises no
tape-write capability, so this driver reports saveable=false.

Explicit DE25 construction of the shared `NativeHardware` now binds package
compatibility, driver selection and advertised capabilities to that platform.
Its managed core lifecycle programs, identifies, observes FPGA-owned video
readiness and calls the selected driver Start. A DE10 video backend and foreign
packages are refused before programming. The DE25 video backend performs no
Linux I2C setup; HOLD belongs to the core driver and does not promise black HDMI.
The Cyclone V splash-idle recipe is refused on DE25. The production factory,
managed DE25 idle/MENU startup, safe process restart and actual tape-save support
remain unfinished. The dated shared-lifecycle diagnostic programs the known package twice, observes
video readiness, loads/runs BASIC7 through driver tape/HID calls and verifies
a software reboot. Production startup, managed idle/MENU and restart adoption
remain closed.

The internal `De25PrControl` diagnostic consumes the generated DE25 PR-control
register contract through an exclusive injected `Mmio` backend and an injected
persona-configuration operation. It validates the selected base identity,
freezes and drains before configuration, and resumes only after the expected
persona's identity response. Uncertain completion permanently poisons the
controller. Native/AArch64 tests and actual-RTL co-simulation exercise this
protocol; they add no device opener, daemon operation, programming profile or
production hardware admission. See the
[shared control contract](../mister-packages/docs/de25-pr-control.md).

The separate native `De25Computer` diagnostic client selects the endpoint's
exact `fes.computer` ABI 1.0, independently of the newest generated family minor.
It uses resource-relative,
ordered 32-bit register operations for `fes.fabric.de25-computer`. It requires
the selected nonzero build ID and exact ZX81 capabilities before hold/run,
full HID state or CRC/chunked tape delivery. Each command has one atomic GPO
write; an uncertain completion permanently poisons the client without replay.
Scripted-register unit tests and the DE25 ZX81 client/RTL co-simulation construct
it. The latter executes the real client natively and under AArch64 QEMU against
the real endpoint and machine RTL. It has no physical MMIO backend, daemon
command, programming profile or production factory path;
the ARMv7/DE10 guard above remains the production gate.

`make de25-computer` builds an explicit maintenance CLI for an already loaded
ZX81-HPS image on the FES DE25 rootfs. It uses the existing Linux MMIO adapter
with a seven-register read allowlist and only one full-word GPO write address.
The caller supplies the selected boot UUID, full core build ID and audited CPU
resource base. It refuses other architectures, rootfs/model identities and
boot epochs, and holds an exclusive local maintenance lock. `probe` reads only
endpoint ID/version; all controls additionally identify the selected computer.
It neither programs an FPGA nor enables the production daemon. Software
deadlines cannot interrupt a hardware bus stall; the bridge and loaded image
must be qualified and exclusively owned before this maintenance command runs.

Raw RBF loading is an explicit, idle-only hardware diagnostic using
`development-contained-v1`; it does not infer a game, ABI, media, or persistence.
Startup, Stop, and bounded failure recovery load the sealed splash through the
contained programming profile and ADV7513 video path. A `fes-gp-v1` package
load releases the HPS bridges after the FPGA enters user mode. The SDR FPGA
ports stay in reset unless the package requires `fes.memory.hps-ddr` 1.0,
the boot core latched the shared layout, and the package's verified identity
and live SDR mirrors match that layout. The
runtime contains no Main launcher, conventional game profiles, MiSTer SPI
driver, or general framebuffer service.

The Atari ST home-computer contract uses required `fes.media.atari-st-floppy`
1.0 for exact 737,280-byte `.st` disks on media unit 0 by default. Required
`fes.media.atari-st-floppy-geometry` 1.0 opts into the bounded raw disk layouts
described below. Insert and eject keep
execution running. Optional `fes.expansion.atari-st-bus` 1.0 composes one card
in physical socket 1 through the existing slot-composition path. These paths
have host test coverage; Atari ST hardware acceptance is pending.

Menu-display GP, CPU granted-slot presentation, and blitter command-list
presentation through the fes-window driver have host software coverage;
exact-artifact hardware acceptance is pending. Explicit idle-menu activation
and generation-bound presentation use the existing local socket, which hands
the client one descriptor for the granted slots; the runtime copies no frame.
Required simple-computer, computer and application session-display plus HPS
DDR capabilities reuse that frame path with a separate display generation.
Opening/closing controls the plane without resetting the machine; a session
frame fault disables the plane and retains the active core instead of loading
idle. Launcher focus releases controller ports and neutralizes/gates a required
single-gamepad worker. These paths have host coverage; combined ZX81 and
application exact-artifact hardware acceptance remain pending.
Default startup remains splash unless the image passes an explicitly selected
menu package to the daemon. That menu returns after Stop with a fresh
presentation generation.

Package admission retains validated artifacts before mutation. Activation
verifies live ABI/build identity before input or media controls. Initialized
loads admit that sealed package, and a recomputed cart when one is selected,
then program a separate bitstream whose digest matches the host receipt. The
sealed package id stays the admitted package. Library loads
explicitly bind durable core data; development loads remain volatile. Failed
save publication retains session ownership and captured data for retry.

Developer Coleco parts admission distinguishes raster RGB and native indexed
video sockets by their sealed marker, map and layout. The native 1.0 socket
fixes a 256 by 192 four-bit TMS9918 palette source and requires a video part;
inspection alone does not authorize an uncomposed package load. These admission
and lifecycle guards have host test coverage. The [native factory Kit 2 record](../../docs/validation/2026-10-03-native-video-factory-kit2.md)
accepts the named `6ed1dad4` image, Direct/Scanlines parts and open ROMs through
normal library Play, including optional SGM, captured audio and Stop/relaunch.

Hardware-supported package paths: 0.
General package qualification remains pending; the bounded Coleco acceptance
above covers only its named artifacts and operations. The historical
[Mega Drive baseline](../FogCast/docs/hardware/native-megadrive-baseline.md)
records a retired path and does not validate the current package artifacts.

Build and validate with `make all` and `make test`. See
[architecture](ARCHITECTURE.md), [support matrix](docs/support-matrix.md),
[menu presentation](docs/menu-display.md),
[development](DEVELOPMENT.md), [application I/O](docs/application-io.md),
[home-computer I/O](docs/computer-io.md), [stream media](docs/media-stream.md), and
[core persistence](docs/core-persistence.md).

The default build uses `-O2`, including appliance builds. Package admission
hashes every sealed member; unoptimized hashing can exceed the target's bounded
activation deadline for large ROM maps. An explicit `CXXFLAGS` still overrides
the default for diagnostic builds.

Format-3 package inspection validates the closed manifest/RBF/ROM-map file set,
retains the map descriptor, and binds all three files to the package identity.
The required ROM slot metadata is exposed in inspection. Map semantics and CRAM
linking belong to the target agent; C++ does not implement another linker.
Format-3 activation uses `load_rom_core`, `load_rom_library_core`, or
`load_rom_composed_core`. Each carries `programmed_path` and a closed `rom_link`
receipt: `rom_id`, `map_sha256`, `source_sha256`, `source_size`,
`programmed_sha256`, and `programmed_size`. The runtime binds the receipt to the
sealed ROM descriptor, retains the programmed FD, and rechecks its bytes and
size before retiring input or quiescing hardware. The local agent owns linking
and source-ROM validation. Ordinary and initialized operations reject format 3;
ROM operations reject format 2. Native capabilities advertise `rom_linking: 1`.
Active status retains the receipt until retirement; library ROM loads preserve
core-scoped persistence. This path has host software coverage only and adds no
hardware acceptance claim.

Atari ST may additionally require `fes.mouse.relative` and
`fes.media.atari-st-floppy-write` 1.0. Mouse packets split signed host movement
without replay after an uncertain ACK. Library disk inserts bind explicit game,
unit and immutable base-image identities; the runtime freezes/captures the
complete disk and publishes the [shared record](../mister-packages/docs/media-data.md)
under the existing durable core-data tree. Raw inserts remain volatile. A busy
guest writer rejects insert/eject; a rejected first insert header preserves the
current disk without a cleanup eject. ST
raster video parts may coexist with the format-3 firmware and CPU expansion,
with separate composition and final ROM-link receipts.

A format-3 writable Atari ST ROM load may carry an explicit initial library
disk. The runtime validates and retains its source and saved record before
programming, restores it while execution is held, and releases only after the
complete disk reports ready. Developer requests cannot supply this library
binding; ordinary ROM loads retain their empty-drive startup. This path has
host software coverage; physical acceptance remains pending.

Required `fes.media.atari-st-floppy-geometry` 1.0 permits the 12 uniquely sized
raw ST layouts with 80–82 tracks, one/two heads, nine/ten sectors and 512-byte
sectors (368640–839680 bytes). Discovery proves capability bit 10 and exact
extended live limits. Nonlegacy immutable bases require matching BPBs; saved
records use minor 1, retain guest BPB edits, and must match the base length
before restore. Legacy 737280-byte behavior and minor-0 bytes are unchanged.
This extension has software coverage; no new hardware acceptance is claimed.

The production library includes the existing JSON parser shared by the daemon
and DE25 kernel-receipt checks. Linking the library does not require a separate
daemon parser object. The exact archive and raw-I/O ownership audits include
the DE25 driver/programmer and retain missing-member negative checks.

The DE25 managed Start waits for the exact fabric1.1 video-ready register
before releasing execution. Missing readiness, reserved bits or a failed read
send no Run command. The board owns HDMI initialization; this read-only gate
adds no Linux I2C recipe. New RTL/driver bytes have software coverage only
until their matching bitstream and board acceptance are complete.
