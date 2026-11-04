# libmister-runtime

Development lives in the FES repository under `sources/libmister-runtime`.
The former standalone repository is archived.

The C++14 library and local `mister-runtime` daemon own FPGA programming,
physical lifecycle, media/input delivery, and recovery. FES described packages are the only product launch path. DE10 supports `fes.simple-game`,
`fes.simple-computer`, `fes.application` and `fes.computer` through `fes-gp-v1`.
DE25 selects the explicit profiles below. Protocol 2 is the only local socket protocol.

Production hardware construction selects the board before constructing physical
adapters. DE10 requires ARMv7 and the exact FES Cyclone V device-tree identity.
DE25 requires AArch64 and the exact Terasic Agilex5 model/compatible properties,
then the existing exclusive maintenance flock, matched running-kernel receipt,
root-only permanent owner and Linux DMA ABI3. Unknown or malformed identities
fail before preparation. The same production wrapper and NativeHardware lifecycle
serve both boards; DE25 constructs no Cyclone V manager, DDR window or Linux I2C
recipe. The daemon keeps its unavailable adapter on preparation failure.
AArch64 compilation does not establish physical support.

The DE25 production computer client uses resource-relative registers. Its shared
MMIO adapter adds the audited CPU resource base `0x20010300`, permits only the
seven computer-register reads and one full-word GPO write, and orders accesses.
The maintenance CLI uses that same adapter. The computer client reports protocol-2
`transport` and `request` phases so an identity failure remains readable by the
agent after the runtime restores MENU; private DE25 phase names are not wire values.
The first managed catalog trial exposed the missing address translation and
invalid error phase. The [managed native integration diagnostic](../../docs/validation/2026-11-04-de25-normal-zx81/README.md)
now passes ordinary catalog Play/Stop, HID BASIC editing and cassette reload
on the recorded ABI3 image and software bundles.

DE25 active capabilities include the selected driver's live tape-unit report,
including its limits and ready/empty state. MENU and idle report no computer
units. This uses the same driver observation as media insertion and ejection.

The shared `FesGp` client also has an explicit DE25 atomic-register transport
for the standard-core port. It consumes the same resource-relative MMIO adapter,
validates bridge identity/status/fault and retained words, and submits one word
per command. Standard HID, live cassette and SAVE behavior use the existing
`FesGpCoreDriver`. Transport uncertainty cannot be cleared by BeginSession or
Realign. The production factory selects this shared driver for the explicit
`de25-linux-computer-v1` profile. Its standard ZX81 contract requires video,
HID, controller ports, fixed PCM, cassette/SAVE, session display, shared blend
plane and allocated scanout1.1. It declares no reserved HPS DDR window. The
existing BRAM profile remains separate; there is no automatic fallback.
Software admission does not establish a sealed standard package or hardware
acceptance.

DE25 advertises application and computer ABIs in ascending identity order,
as required by the target agent's protocol-2 decoder. The native MENU fixture
exports the production serialized status for an independent agent-client test;
an unordered capability list leaves the agent unavailable even when MENU is idle.

`LinuxDe25FpgaManager` selects the overlay from the described programming
profile: `de25-hps-first-v1` is the BRAM computer, and `de25-linux-menu-v1` is the
allocated MENU application. `de25-linux-computer-v1` also selects that allocated
scanout overlay, then uses the shared computer control path. It retains the actual outgoing overlay bytes before
MENU/computer replacement; an unverified or modified overlay is refused. Firmware
comes from the retained Artifact and is closed before the kernel reads it.
Staging uses the root-only `/run/fes-de25-firmware` directory so the factory
root remains read-only. Under the existing exclusive lease, programming
temporarily selects that directory with the kernel firmware search override;
an unrelated override is refused. Successful completion and failures before
submission restore the empty override. Uncertain submission retains its lookup
path and staged bytes for a potentially late kernel request.
All standard firmware fallback locations must also lack the fixed RBF name;
a stale fallback is refused before programming.
Partial, failed or late programming poisons the context and retains the
configuration. Software deadlines cannot abort an opaque configfs/SDM call.
On DE25, TERM/INT request a normal server stop. Once socket workers have ended,
an idle MENU shutdown proves kernel drain and the end of peer mappings before
explicitly releasing that daemon's verified overlay and staged firmware. A fresh
daemon can then program its selected MENU. A running computer, pinned peer,
changed artifact or uncertain context refuses this handoff. Destruction and
unexpected process death preserve the configuration. Production restart may
abandon an exact applied allocated overlay only after its staged RBF matches an
admitted outgoing package and the permanent owner records successful completed
revocation with no exposure or uncertainty. One REAP then releases its retained
reference, and OWNER_IDLE proves the end of peer references. The outgoing package
may come from either the factory package directory or the normal agent's sealed
ID-plus-token staging publication; content identity is verified in either case.
A retained computer must also pass live identity and the existing SAVE guard, so
dirty snapshots or uncertain mailboxes remain blocked. This guard verifies live
identity without fresh-start empty-cassette discovery; a loaded tape remains
untouched and no active driver session is adopted. The existing verified release
then precedes a normal MENU load. Unknown packages, firmware-only overlays and
uncertainty remain refused; no lost guest session is resumed. The
[native lost-process proof](../../docs/validation/2026-11-04-de25-standard-zx81/README.md#native-lost-process-recovery)
records source `7688e7301`: idle and active-ZX81 runtime exits recover in the
same Linux boot, and the replacement group after one agent exit passes fresh
ZX81/UI checks. The agent's initial HTTP recorder failure remains separate.
Whole-load failure bounds and full normal-kit acceptance remain unfinished.
The [dated clean-restart diagnostic](../../docs/validation/2026-11-03-de25-watchdog/README.md#exact-normal-menu-package-and-clean-daemon-replacement)
passes two production-daemon contexts, each with 90 frames and zero underflows,
in one native boot. The later managed diagnostic also passes normal software
replacement and ordinary reboot after Stop and from an active session. Full
normal-kit acceptance remains open; recovery abandons the lost session.

The [programmer diagnostic](../../docs/validation/2026-10-31-de25-runtime-programmer/README.md)
and [shared computer lifecycle diagnostic](../../docs/validation/2026-11-01-de25-shared-lifecycle/README.md)
record their own selected kernel/runtime/core bytes, two loads and BASIC7.
`De25ComputerCoreDriver` implements the exact BRAM computer contract, selected
build identity, HID and tape insert/eject. Launch tape stays held until Start;
live replacement sends no execution/reset command. Its known image has no
tape-write capability and reports saveable=false. These dated results do not
qualify the new factory or MENU path.

`LinuxDe25Menu` supplies the existing MenuMemory and MenuDisplay interfaces from
one kernel allocation. `De25MenuCoreDriver` binds its selected build identity;
Configure waits for HDMI and confirms two complete black frames in slots1/0.
Only then does hardware sequence2 become logical initial sequence0. The
board's video backend asks the selected core driver for readiness, so MENU
never reads the computer endpoint. Subsequent publication uses coherent slots
and actual full-frame completion, without physical addresses, PL310 operations,
frame copies, a GP mailbox or invented execution commands.
On a failed grant poll, remaining deadline permitting, the error also reports
same-generation cached kernel health counters. This read performs no register
access and neither accepts the failed frame nor clears the fault/retirement gate.

The same allocation adapter also accepts an injected standard-core GP display
control. In that mode Configure/Enable/Quiesce use the existing display commands;
frames are published only through the kernel grant. Closing proves kernel drain,
the end of peer references and GP plane quiescence before another allocation.
Failed session presentation logs its original error before cleanup; failed
show/close requests also enter the existing log before a later request can
replace status. DE25 reopen and retired-counter refusals include the observed
state and counter values without changing the retirement gate. This reporting
is needed to investigate the retained post-reboot picker failure at source
`a35f3207a`; it does not establish a repair or new native acceptance.
Reopening allocates cleared storage with hardware front slot0 and restarts its
physical frame sequence, while the runtime's logical completion count continues.
Session status negotiates per-frame mappings, and its first back slot is1 after
each fresh allocation. Focus and machine execution remain in the shared lifecycle.
The shared lifecycle calls BindSession after live identity and before allocation
activation. Production construction supplies the existing GP display endpoint to
that adapter. This path has software coverage only; canonical sealed standard
package production and native acceptance remain unfinished.

The MENU package declares application1.0 with fixed video1.0, menu-display1.0
and `fes.fabric.de25-scanout`1.1. It declares no boot-latched HPS DDR window,
blitter or retained session plane. The runtime's ELF menu-interface records are
the union of its explicit DE10 and DE25 contracts. A selected startup MENU is
loaded directly; the default Start without a package uses the board's splash
recipe, which remains unsupported on DE25.

MENU grants explicitly negotiate per-frame mappings. The UI unmaps before
commit and preparation abandonment, then remaps the same allocation for its
next frame. An unaware peer is refused before fd sharing. Retirement requires
kernel drain and the final peer reference. Failed retirement stays sticky and
blocks programming; descriptor closure cannot authorize reuse. Kernel file
release can attempt its own guarded revocation and retains uncertain exposure.
The [frame agreement](../../docs/validation/2026-11-01-de25-menu-frame-mapping/README.md)
and [lifecycle tests](../../docs/validation/2026-11-01-de25-menu-lifecycle/README.md)
are software evidence at their named producers.

The [allocated DMA hardware diagnostic](../../docs/validation/2026-11-01-de25-read-ahead/README.md)
records three captured frames, full allocation unmap, software reboot and fresh
reload/repetition on its earlier ABI2 kernel. The later ABI3 factory/MENU path
is deployed in the managed native diagnostic linked above. Its catalog, software
replacement and ordinary reboot results qualify only the named artifacts and
conditions. Physical tape SAVE, broader security/reset/failure fencing and full
normal-kit acceptance remain open.

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
the real endpoint and machine RTL. That co-simulation opens no physical device.
Production DE25 construction uses the shared MMIO adapter and explicit
driver/profile described above; the maintenance CLI remains separate from
managed operation.

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

The PC/XT read-only floppy interface uses `fes.computer` 1.1 extended
capability discovery and `fes.media.pcxt-floppy` 1.0 for exact 368,640-byte
images on unit 0. Insertion and ejection preserve the running session; the
runtime checks the live limits and complete transfer before reporting ready.
This path has host software coverage; PC/XT hardware acceptance is pending.

The `native_hardware_test --session-display` selection runs the allocated MENU
and retained/fresh-slot session lifecycle regressions without unrelated core
fixtures. It uses injected hardware and is not native-board acceptance.
