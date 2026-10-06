# Architecture

## Ownership and production construction

`Runtime` owns one serialized lifecycle and generation-bound fault worker.
`mister-runtime` exposes protocol 2 over the local Unix socket. FogCast owns
network sessions and library context; the runtime owns physical transitions.

`CreateProductionHardware` constructs the retained artifact opener, Linux MMIO
and FPGA manager, FES GP transport/driver, ADV7513 I2C, fixed video and splash
video, Linux input, callback-only input session, and `NativeHardware`.
There is no conventional Main, MiSTer SPI, framebuffer, cartridge profile table,
or raw game launch operation.

The package inspector reads format 4's two named ROM sources and sealed map
with its separate identity domain. Physical admission still requires a linked
load before that format can be activated.
The production adapter forwards both single-source and two-source bitstream
attachment to `NativeHardware`, which validates the receipt and retains the
programmed file before any physical transition.

Package activation admission checks the exact sealed descriptor, digest,
board, programming profile, ABI and required interfaces before programming.
Owned descriptors and retained file descriptors cross the mutation boundary;
payload and composition identities are rechecked immediately before use.
Only `fes-gp-v1` packages activate. The contained profile is diagnostic-only.

Replacement stops and neutralizes old input, quiesces HDMI and the outgoing
driver, then programs once. Ambiguous quiesce is not repeated. FPGA-manager
containment and control readback remain bounded. Every program contains the
bridges and resets the SDR FPGA ports (`FPGAPORTRST` `0`) before configuration.
After user-mode readback, `fes-gp-v1` releases only the bridge reset (`0`) and
L3 remap (`0x19`). `development-contained-v1` stays contained, including
splash, idle, and raw development RBFs. The ports leave reset only for a
package that requires `fes.memory.hps-ddr` 1.0 on a boot whose U-Boot core
latched the shared layout. The FPGA manager learns that from the SDR mirrors
before its first program in a boot and records it under `/run`, and the
interface is advertised and admitted only then. After identity proves
capability bit 8 and before execution release, the FPGA manager reads the SDR
mirrors `CPORTWIDTH` through `PORTCFG` and writes `FPGAPORTRST` `0x3fff` only
when every FPGA field equals the shared layout. A mismatch fails activation as
`core_mismatch` (phase `identity`), naming the register with observed and
expected values; an MMIO failure or expired deadline is `program_failed`
(phase `programming`). Both leave the ports in reset. A programming/activation
fault receives at most one defined-idle recovery. `Stop` does not program again from
`reboot_required`. `recover_idle` is the explicit second `LoadIdle`; if that
also fails, the state stays `reboot_required` and a board reboot is the
remaining recovery. A soft board reboot after FPGA or HPS activity can wedge
the HPS network; hard power is the recovery when `recover_idle` still fails.
The agent requests that board reboot only after a development session is
already `stopping` with `reboot_required`. A readable non-empty idle file is
programmed; only an open failure returns before HDMI quiesce and FPGA
program. See FES [soft-restart Path B](../../docs/soft-restart-path-b.md).

Identity precedes video, input enablement and gameplay release. FES media
interfaces control reset-held startup and release after a successful commit;
`fes.computer` media units are the exception and never gate release.

The shared GP response sampler holds the original request after observing its
ACK, waits one microsecond using the monotonic wall clock, then requires two
identical complete GPI samples. A changing response/error payload may settle
within the original exchange deadline (at most 100 ms); sampling never writes
GPO again. Invalid signature/reserved bits, ACK reversion, MMIO failure or
continued instability past the bound poison the exchange. Re-alignment uses
the same read-only sampler and drops poison only on success. A separate wall
bound prevents a stalled runtime clock from extending response sampling.
This software sampling policy does not establish the cause of a physical
failure or qualify a package, runtime image or appliance.

`fes.simple-computer` also accepts mid-session `replace_live_media` /
`clear_media` on an active generation without holding execution reset. Tape
loader busy is GP error 4 (invalid state) and rejects with retryable busy.
Clear sends media begin at `MediaEjectIndex`. GP error 2 is invalid index:
cores sealed before that index answer it before they look at `media_busy`.
Clear then repeats control-index begin with argument 0, the eject those
cores added before the eject index. GP error 3 on that command is invalid
argument, not busy: the sealed golden mailbox rejects argument 0 because it
is below the minimum blob length, and it has no `media_busy` input. The busy
guard was added with argument-0 eject, so this core cannot reject a later
begin as invalid state. A legal begin drops `media_ready` and `media_size`
immediately. Clear waits until the runtime clock advances across the longest
`$0347` copy (one byte per CPU enable, at most 16384 bytes, 8 ms) and only
then issues control-index begin of `MediaMinBytes`, without a commit.
`SteadyClock` reports whole milliseconds, so a repeated sample is the same
millisecond and the wait continues. A clock that stays flat for that bound,
or a deadline that cannot cover it, leaves the mailbox unchanged and returns
retryable busy. The wait is not a `media_busy` sample: this bitstream does
not return one, and clear does not hold reset because that restarts the CPU.
Invalid
state (GP error 4) on the eject-index or argument-0 attempt is retryable busy
and does not reach that begin. A poisoned or unstable GP handshake after keyboard traffic
is the same retryable busy: clear realigns from the live ACK and
re-identifies before eject. Re-identify does not run for a completed
rejection. A hard MMIO failure, or an invalid acknowledgement after these
eject shapes, stays `io_failed`.
See FES
[`docs/zx81-tape-media.md`](../../docs/zx81-tape-media.md).
`NativeInputSession` requires a generation-bound driver callback and never
falls back to SPI. Splash has no core probe or HPS framebuffer; splash and
fixed-video bring-up use ADV7513 only.

Explicit `load_development_rbf` accepts the contained diagnostic profile while
idle. Package loads and raw diagnostics publish `running_development`; package
identity and persistence mode distinguish library/development context. Status,
inspection and failures always serialize protocol 2, including rejection of
retired protocol versions. Existing diagnostic event labels remain the shared
observability contract; they do not start Main processes.

The production archive contains only the sources in the Makefile manifest.
Archive regeneration removes obsolete members; archive and active-tree guards
verify construction and reject old mutation authority.

## Menu presentation primitives

The required `fes.video.menu-display` 1.0 application capability (bit 9)
is admitted only alongside fixed video and HPS DDR, with those three interfaces.
Live identity must match the declared capabilities exactly. `MenuDisplayDriver`
validates fixed geometry, reads coherent counters, stages ordered submissions,
and waits for drained quiescence. `ConfigureMenuPackage` explicitly selects a described `fes.menu` package while
idle. Image startup may pass a sealed menu package selection to the daemon;
without one, startup remains splash. Menu activation reuses ordinary package
programming, identity and DDR-port admission: zero both slots, configure,
release execution, then enable. Menu display never creates a playable active
package, gameplay generation or input worker. The existing local socket provides a bounded two-phase frame exchange; see
[menu presentation](docs/menu-display.md).

`MenuFrame` creates one exact-size sealable memfd per caller. Before mapping
staging bytes, validation requires the same device/inode, 3,686,400-byte size
and WRITE/GROW/SHRINK/SEAL seals. A client must remove writable mappings before
sealing. The runtime reads sealed staging through a private, read-only mapping;
the selected kit kernel rejects a shared mapping after `F_SEAL_WRITE`.
`MenuMemory` converts RGBA to B,G,R,0 in a cached row buffer, then writes
either fixed 4 MiB slot with naturally aligned word stores. Host builds use
volatile words. ARMv7 uses `ldmia`/`stmia` of eight words, which stays inside
one 4KB page because a row and the slot are 32-byte aligned. The mapping
stays `O_SYNC` (strongly ordered on the qualified kernel); it is not
write-combined. Frame padding remains untouched. The copy checks a cancel
flag every four rows and does not submit a partial frame. A lifecycle
operation sets that flag before it quiesces menu firmware or programs
another core, so the window is not written after teardown begins. Black fill
still runs after the new image is programmed. `MenuMemory` owns an 8 MiB
mapping at `0x30000000`,
checks the entire shared 256 MiB window against effective `/proc/iomem`
System RAM ranges, and rejects absent or redacted evidence.

Production mapping currently requires ARM Linux kernel `5.15.1-MiSTer`,
`/dev/mem` with `O_SYNC`, and an ARM `dsb sy` after writes. The selected
[kernel's mapping implementation](https://raw.githubusercontent.com/MiSTer-devel/Linux-Kernel_MiSTer/d7adb20b4ca595838289406c083fff78f004a8c3/arch/arm/mm/mmu.c)
uses noncached protection for PFNs outside Linux RAM. `O_SYNC` alone does not
establish this: boot reservation and actual target mapping qualification
remain prerequisites for physical acceptance. Host tests inject mapping
operations and cannot establish DDR visibility on hardware.

Menu status carries its own nonzero generation, geometry and completion
counters. `BeginMenuFrame` permits one preparation without retaining the
lifecycle lock while its caller fills the file. Both the generation and
runtime-created preparation identity bind `PresentMenuFrame`; immutable
validation precedes any copy. Presentation holds the existing busy fence
through copy, submission and displayed-sequence polling. ACK alone never
releases the previous slot. Only one submission is pending. Launch,
core-data, Stop, contained development load, idle recovery and menu
configure share one named two-second wait (`kMenuFrameMutationWait`) when
the only in-flight work is a menu frame. While any of them waits, new menu
frames cannot enter. After the frame fence drops, the operation applies its
existing busy checks and either claims the fence or returns busy. It does
not quiesce or program until that fence is clear, so it cannot race the
copy. A same-thread re-entry still returns busy after the bound, because the
frame cannot finish until the caller returns. Stop reactivates an explicitly
configured menu with a fresh generation, including Stop from menu idle; that
reprograms the FPGA and blips HDMI. Splash Stop stays idempotent. If both
the menu reload and the splash reload fail, Stop ends in `reboot_required`.
A rejected pre-mutation game admission preserves the menu and its
preparation. Replacement and contained diagnostics revoke the old generation.

Menu quiesce proves drained state before execution hold. The hardware
underflow counter is cumulative and is not cleared by a GP command. It
resets when the bitstream is programmed and when port reset is asserted.
Each present records the counter before the copy and judges only the delta
during that present. A delta of at most one scanline (1280 pixels) is
displayed. Three consecutive presents with any positive delta, or one delta
above that cap, fails the present. The failure keeps the configured package
and the existing idle path reprograms it, at most twice per ten minutes.
The next failure in that window drops the package, sets `menu_display.error`,
and the same idle path loads splash. A successful `configure_menu` clears
the streak and the reactivation budget. The published `underflows` value is
that per-present delta, not the cumulative counter. Activation still
requires a zero counter because programming reset it. A missing completion
or uncertain drain uses the same bounded reactivation before splash.
`menu_unsafe_` still skips a second GP quiesce on the splash path. Failed
containment leaves reboot-required. Mapping remains owned by the production
hardware adapter; new frame copies require `AllowCopies` after the new image
is programmed. Menu activation failures report unavailable with the error on
`menu_display` even when splash recovery succeeds. A later idle status does
not copy that menu error into top-level `status.error`; the operation that
failed assigns `status.error` itself. The runtime and the FogCast agent
decode this nested status strictly and must ship in the same image.
Capability-gated emission and tolerant decoding are not implemented. Host
tests do not establish physical scanout acceptance.

## In-session simple-computer presentation

Simple-computer `fes.video.session-display` 1.0 reuses the menu driver and
reserved DDR slots, with required `fes.memory.hps-ddr` 1.0. Its bit 9 and the
DDR bit 8 are included in exact declared/live identity checks. Admission
advertises this interface only when boot DDR layout and presentation adapters
are available. Activation initializes black slots and configures the disabled
plane once, then publishes active machine status with `session: true`, its
package and `core_generation`, but `available: false`.

`SetSessionDisplay` binds the active package/generation and serializes with the
same busy fence and bounded frame wait. Open neutralizes the keyboard, enables
the reader and grants a distinct display generation; FPGA scanout waits for a
complete submitted frame before switching HDMI pixels. Close drains the plane
without execution hold and revokes the display generation and preparation.
Navigation key snapshots are suppressed while session display focus is held.
Stop/replacement drain the display before their existing execution hold.

A failed session frame never enters `FinishLaunchFailure` or calls `LoadIdle`.
The native adapter disables the plane, re-identifying the same core after an
ambiguous GP toggle when necessary. It retains the error and active package,
CPU and media ownership; a disabled, drained session plane may retain a sticky
reader fault. A close whose drain is uncertain keeps input suppressed until
a successful close or ordinary lifecycle replacement. None of these paths
spends the idle menu reactivation budget. These behaviors have host software
coverage; the combined ZX81 display needs fresh exact-artifact kit acceptance.

## Composable application ABI

`fes.application` 1.0 uses the existing `fes-gp-v1` lifecycle and GP transport,
with identity tag 3 and independent video, gamepad and media capabilities.
Admission requires fixed-720p60 video; known operational declarations must be
required, while absent interfaces impose no requirement. The Coleco firmware
overlay `fes.firmware.blob` 1.0 is the exception: a package may declare it
optional so BIOS-free titles share the same bitstream. Unknown optional
interfaces are ignored. Blob-stream requires blob. `fes.memory.hps-ddr` 1.0
(capability bit 8) adds no GP command; it only gates the SDR port release
above. See [application I/O](docs/application-io.md) for the complete runtime
contract.

The existing `FesGpCoreDriver` verifies application identity and capabilities,
reuses the shared button/media codecs, and never issues keyboard commands to
an application. Video-only loads do not open the input session. Media-bearing
applications remain reset-held until successful media commit; other
applications release immediately. `load_firmware` is a separate 8192-byte
mailbox transfer that keeps execution reset-held through begin/data/commit;
cartridge `load_media` still owns release. Firmware 1.0 is software-supported
on `fes.application`; physical Coleco BIOS bind remains pending. Existing ABI
startup and persistence remain unchanged. Application stereo 48 kHz PCM audio is
software-supported through the shared ADV7513 path; physical audio acceptance
remains pending.

Two logical controller ports use required `fes.gamepad.ports` 1.0; optional
`fes.keypad.ports` 1.0 adds twelve keys per port. The former excludes the old
single-gamepad interface, and the latter requires controller ports. These
applications never open the evdev worker. Protocol-2 `set_controller` carries
`package_id`, `expected_generation`, `port` (0 or 1), `buttons` (8 bits), and
`keypad` (12 bits). Complete snapshots are validated before hardware access;
nonzero keypad state requires the declared keypad interface. The existing busy
boundary serializes delivery and rejects stale session identity. Digital and
keypad commands are sequential, not an atomic fabric update. An exchange failure
retires the generation through the existing input-fault cleanup. Start clears
both ports before release; Quiesce holds execution then explicitly clears both
ports before replacement. FogCast owns source assignment and disconnect release.
Physical controller acceptance remains pending.

## Home-computer ABI

`fes.computer` 1.0 uses the same lifecycle and GP transport with identity
tag 4. Admission requires fixed video; `fes.keyboard.hid`, `fes.gamepad.ports`,
`fes.audio.pcm-s16-stereo-48k` and the Apple II, C64, Spectrum and Atari ST
media interfaces are independent and must be required when declared. A shell
declares at most one unit-0 medium. Recognized expansion buses must be
optional, unknown optional declarations are ignored and `core.system`
is rejected. Format 2 and format 3 firmware ROM packages are admitted; linked
cartridges and format 4 are not, because nothing holds execution for them.

Identity requires registered live capability bits 0 through 7 to equal the declared set,
then discovery reads MediaInfo for each declared unit, which must be present
and empty. Audio packets follow the declaration as for applications. Start
only releases execution: there is no media gate, no neutral write and no evdev
worker. Stop holds execution first; the hold neutralizes keys and ports in the
core. `set_keyboard_hid` sends a complete nine-row USB HID snapshot and the
driver writes only changed rows, all nine after Start. `set_controller` uses
opcode 4 with a zero keypad. `insert_media` transfers into one declared unit
while the machine keeps running and confirms MediaInfo ready; any failure after
its first exchange ejects that unit once, realigning and re-identifying an
ambiguous mailbox without repeating the ambiguous request, and leaves the
generation active. Status reports `capabilities.media_units` from the latest
live exchanges. The shared golden exchanges are replayed through the driver
against an independent reference endpoint. See
[home-computer I/O](docs/computer-io.md) for the exact protocol shapes. This
path has host software coverage only.

`fes.media.atari-st-floppy` 1.0 uses capability bit 7 and unit 0. Discovery
and every insertion require live minimum and maximum sizes of 737,280 bytes,
with 512-byte chunks. A changed limit is rejected before MediaBegin. The same
transfer, ready confirmation and failure-eject path handles the full disk
while the machine runs.

The additive `fes.media.atari-st-floppy-write` 1.0 contract (capability 9)
requires the base floppy contract. WD1772 Write Sector gathers one aligned
512-byte DMA sector into staging before committing it to disk RAM. A cancelled
gather changes no disk bytes; a started commit drains through reset, drive
removal and force interrupt. DMA cursor/count changes only after the complete
sector commits. Deleted-data writes and format/write-track remain unsupported.
This gives sector atomicity, not a transaction across FAT/directory sectors.

`insert_library_media` explicitly binds unit 0 to a game ID and the SHA-256
of the immutable 737,280-byte base image. The runtime validates that source
before altering the unit, restores an existing compatible disk record, and
reports `persistence:{mode,game_id,base_media_id,revision}` on the live unit.
Raw `insert_media` remains volatile. A saved record cannot silently downgrade
to a shell lacking the write contract.

`native/media_data` retains a no-follow namespace below the fixed agent root
`/media/fat/fogcast/core-data/media`. The namespace combines core ID, game ID,
unit and base-image digest; firmware/video package revisions do not rename it.
The versioned record contains identity hashes, exact layout and payload,
checksum and full-record revision. Opening the namespace takes the same
exclusive lock and removes leftover `.record-<pid>-<sequence>` and
`.probe-<pid>-<sequence>` files before the record is read. A live save holds
that lock for the whole temporary, so only a crash or a failed cleanup leaves
a file to remove. Publication uses the existing lock, private file, file sync,
atomic rename and directory sync policy. Corrupt, incompatible
or concurrently changed records block replacement rather than resetting data.
If directory sync fails after rename, an exact visible copy of the captured
image retains its own revision for an explicit retry; the failure still reports
uncertain durability and does not authorize removal.
The shared `media-data-v1` fixture checks independent header, checksum,
revision and namespace bytes.

Stop, eject and replacement always Freeze/drain before capture, including when
initial dirty flags are clear. Ordered snapshot chunks read committed disk RAM
while CPU/video keep running. Publication precedes Saved; Saved authorizes the
next destructive Begin/Eject while still frozen. There is no Resume gap before
removal. `save_media` publishes a checkpoint and explicitly resumes. A save
failure resumes the existing disk with its binding and generation; an
unconfirmed Resume retains that ownership in reboot-required recovery. Failed
ejects reobserve the live unit without replay: confirmed empty retires the
binding, while ready retains it. Ambiguous failed replacement also compares a
frozen capture with the saved old image before resuming its namespace; different
or unresolved bytes require recovery.

Successful FPGA programming retires the former disk binding, captured bytes
and durable revision from memory. This includes Stop to the configured menu
or splash, core replacement and contained programming. The published record
stays intact. A new ST core reports an empty volatile drive until an explicit
library insertion binds and restores that record. Failed capture/publication
continues to retain the current disk and generation. Successful-transition
retirement follows programming, rather than the transition request. Partial
programming failures keep the existing invalidation of unsafe active-core
metadata.

Bound-disk input faults revoke asynchronous input and capture/publish before
idle programming. A failed capture or publication retains RAM, package,
generation and binding in reboot-required recovery; it cannot program idle or
replay the fault. `recover_idle` also refuses to destroy retained bound disk
RAM. Startup and volatile fault cleanup do not publish a disk.
These paths have host test coverage;
physical acceptance remains separate.

## Described-core persistence

The `CreateProductionHardware` facade forwards preparation, refresh, inspection,
settings updates, and programmed-bitstream attachment to its owned
`NativeHardware`, alongside the existing admission and lifecycle methods. A host
regression enters through this factory and validates durable data operations
and bitstream attachment without starting hardware; direct `NativeHardware`
tests alone do not verify production API forwarding.

`native/core_data` owns the bounded canonical record codec and retained
no-follow namespace directories for described-package persistence. `CoreDataFile::Read` reopens `record.bin` on every
read. Publication validates the current record and exact revision while holding
a namespace file lock, writes private complete bytes, syncs, renames and syncs
the directory. Library admission probes writable storage before input
retirement; read-only inspection skips this probe. Generated shared fixtures
under `tests/fixtures/core-persistence-v1` define exact record and GP bytes.

The existing Runtime busy boundary now also serializes inactive inspection and
settings compare-and-swap with lifecycle operations. `PrepareCoreData` binds
trusted library context to an admitted package; ordinary `LoadCore` has no such
binding. Following outgoing `FlushSave`, `RefreshCoreData` rereads the incoming
record before activation. Native hardware restores after the driver verifies
identity and data-info, before Start/input. Core driver methods `CaptureData`,
`RestoreData` and `ResumeData` remain bounded by the same GP exchange deadlines.
The persistence interfaces keep base ABI and transport version 1.0 unchanged.

`FlushSave` retains a complete host snapshot through publication failure.
`RestoreInput` explicitly resumes GP before reopening the same generation;
snapshots are invalidated only after input restoration succeeds. Unsafe resume
retains persistent package/generation metadata in `reboot_required` and leaves
the complete snapshot owned by native hardware. No fault/startup cleanup saves.
Legacy cartridge `SaveFile`, SNES SRAM transport and `.srm` launch handling
have been retired. Existing user save files are not deleted or migrated by
this cleanup; described packages use their explicit persistence contracts.

See [the complete local protocol and error contract](docs/core-persistence.md)
for derived layout metadata, persistence modes, downgrade rejection, CAS,
volatile development behavior and recovery envelopes. This implementation has
software coverage only; no physical support claim is added.

### Static composition admission

`NativeHardware::AdmitCoreComposition` first performs normal sealed base-package
admission, then opens expansion companions with descriptor-relative, no-follow
traversal beneath the package roots. `core_composition.cpp` accepts the fixed
canonical ZX81 and Coleco CPU-bus manifest grammar. ZX81 requires
`fes.simple-computer` 1.0 and admits optional `fes.expansion.zx81-bus`
1.0 with `fes.zx81-bus.socket/1`, or 2.0 with `fes.zx81-bus.socket/2`.
Version 2 adds CPU clock and active-low reset; its physical socket rectangle
is unchanged. A cart and shell must declare the same bus version. Coleco
requires `fes.application` 1.0. The matching
optional slot and map are checked together with the manifest-derived expansion
ID, exact base package/BUILD_ID/payload binding, cart digest and linked payload
size/digest. The composition ID uses the shared `fes-composition-v1` domain and
base/expansion/payload identities. Descriptor and GP identity remain the base's;
only the FPGA programming artifact comes from the admitted composition.
Coleco manifests may carry the canonical `fes.coleco.response-boundary/4`
patch before the cart fields. Runtime admission accepts only its two fixed
coordinates and binary values; ZX81 manifests cannot carry that patch.

A `fes.computer` shell declaring optional `fes.expansion.apple2-bus` 1.0 is
composed per slot instead: the composed operations carry an `expansions` array
of `{slot, path}` and a multi-slot tuple listing `{slot, expansion_id}` in
ascending slot order. Each card manifest adds `slot_index` (between `slot` and
`slot_major`) equal to its request slot, uses map `fes.apple2-bus.slots/1` and
binds the same frozen shell; every slot 1..7 is admitted until the shell's
physical socket set is sealed. The composition ID uses the separate
`fes-composition-v2` domain over the package, each `slot:expansion` pair and
the payload. Every card is retained and rechecked before programming, and
status reports the same multi-slot tuple. The two request shapes never compose
each other's bus; single-socket ZX81 and Coleco behavior is unchanged.

Optional `fes.expansion.atari-st-bus` 1.0 uses the slot-composition tuple with
map `fes.atari-st-bus.socket/1`, exactly one physical socket (slot 1), and at
most one card. The shared linker owns its CRAM rectangle and wide bus
boundary; runtime admission validates the map, socket and frozen shell
binding and retains every input through programming.

The network-facing target agent owns deterministic CRAM recomposition using the
shared misteross implementation. Runtime admission verifies that agent-owned
result and retains its open FD, rehashing it and the expansion files immediately
before entering the existing physical transition. It does not implement a second
linker, download paths, or reopen checked pathnames during programming. Failed
admission leaves the current generation untouched. Successful activation carries
the composition tuple in active status, and the ordinary retirement/recovery
paths clear it together with package identity.

### Presentation parts admission

Protocol-2 `inspect_parts_core` and `load_parts_core` accept a closed role list
(`video`, plus optional `expansion`) and a separate typed parts composition.
Only a format-2 Coleco application shell that explicitly declares optional
`fes.fabric.video.raster-rgb888` 1.0 or `fes.fabric.video.native-pixels` 1.0,
and optional Coleco bus 2.0 qualifies. Duplicate or simultaneous video markers
are rejected. Fabric markers have no GP capability and do not enter the runtime
interface registry. The raster marker selects layout `fes.coleco-video.parts/1`
and map `fes.coleco-video.socket/1`. The native marker selects the separate
`fes.coleco-native-video.parts/1` layout and `fes.coleco-native-video.socket/1`
map; version 1.0 fixes a 256 by 192 indexed source with four-bit TMS9918 palette
values. Parts from the two layouts cannot be interchanged. A CPU part uses
`fes.coleco-bus.socket/2` in either layout.

Native admission checks the exact shell package, payload, BUILD_ID, canonical
part manifests, role ordering, part IDs, composed digest and the
`fes-parts-composition-v1` identity. Every file is retained and rehashed before
physical mutation. The FogCast agent owns independent CRAM recomposition.
Inspection admits those companions without programming. Load uses the existing
physical lifecycle, drivers and fixed ADV7513 recipe; live GP capability and
BUILD_ID checks remain those of the sealed base package. Active status carries
layout and role identities under `active_package.composition`.

The separate `load_parts_library_core` operation requires an absolute
`data_root` and uses the same sealed admission and replacement lifecycle.
It prepares the base core's data namespace before programming, even though
this format-2 Coleco layout has no persistence contract and remains volatile.
An existing durable namespace rejects the candidate with `incompatible_data`;
selecting video cannot silently discard a previous persistence requirement.
FogCast fixes the root locally and supplies explicit library identity.
Its preceding `inspect_core_data` operation validates the sealed base package
and namespace without requiring a video composition or changing hardware.
This metadata admission also accepts native shells; it grants no activation
permission and preserves package compatibility and persistence checks.

Developer inspect/load operations have no data-root field and remain
explicitly volatile. A native shell may be inspected, but ordinary package
loads and CPU-only compositions reject it before quiescing or programming:
its video part is mandatory. Both native and raster video use the explicit
developer and normal library parts paths. Existing CPU composition operations
reject the parts request shape.
Admission has host software coverage. The earlier
[native Kit 2 diagnostic](../../docs/validation/2026-10-03-native-video-parts-kit2.md)
uses temporary diagnostic executables. The subsequent
[factory Kit 2 record](../../docs/validation/2026-10-03-native-video-factory-kit2.md)
accepts the exact `6ed1dad4` appliance image through normal library Play with
Direct/Scanlines, optional SGM, open media, captured audio and Stop/relaunch.
It does not qualify alternate timing, DDR presentation, ROM-linked parts or
general cartridge compatibility.

### Initialized bitstream

`load_initialized_core`, `load_initialized_library_core`, and
`load_initialized_composed_core` admit the sealed package first. The composed
operation also admits the recomputed cart. Each then opens a separate
programmed bitstream, hashes it, and requires the host receipt digest.
`CreateProductionHardware` forwards `AttachProgrammedBitstream` into that
check, so a library RomInit reaches native admission on the production daemon.
`LoadCore` hashes that file again immediately before programming and programs
those bytes. The sealed payload, and any linked cart payload, remain the
identity artifacts. A digest mismatch rejects admission and leaves the current
generation untouched. This path is software-covered and has no exact-artifact
hardware acceptance.

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

Linked cartridge ROM packages cannot declare media blob-stream 1.0, firmware
blob 1.0 (required or optional, for any ABI), or application 1.0 with media
blob 1.0. Stream/application-media startup and firmware delivery hold reset
until a later media commit, while a linked cartridge has already arrived in
CRAM and skips that commit.
Compatibility rejects that combination before mutation; firmware ROM packages
retain normal later cartridge/tape/disk delivery semantics. A format-3
`fes.simple-computer` cartridge may omit `fes.media.blob` when it has no later
media mailbox; format-2 and firmware-ROM computer packages still require it.
