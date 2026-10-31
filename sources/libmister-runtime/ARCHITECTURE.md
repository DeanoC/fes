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

Before loading the window driver or constructing those physical adapters, the
factory requires a Linux ARMv7 binary, raw device-tree model
`Terasic DE10-nano\0`, and compatible list
`altr,socfpga-cyclone5\0altr,socfpga\0`. This matches the existing FES boot/reset
board contract. AArch64, another board, or absent/malformed properties return
`unsupported_target` with phase `platform`. The daemon uses its existing
failure-only hardware adapter and serves the same local protocol with no
programming capabilities. The test-only factory overload supplies identity and
the driver-loader callback to this same guarded construction path; it is absent
from production archives. Identity/ordering tests establish this rejection
behavior. An AArch64 compile or execution under QEMU does not establish DE25
FPGA programming, MMIO, DDR, input or display support.

## DE25 kernel programmer

`LinuxDe25FpgaManager` implements `FpgaManager` for the internal
`de25_hps_first_v1` enum. The shared registry declares a DE25-only computer pair; the default DE10
platform and production factory remain closed to it. Cyclone V refuses that enum before its
first register/boot-layout access; the DE25 adapter refuses Cyclone V profiles.
This supplies the programming transport for the existing lifecycle, not another
runtime, daemon, profile table or network API.

The caller serializes physical transitions, quiesces its previous core and
retains the existing DE25 maintenance lock. A duplicate descriptor holds that
same lease. The adapter requires the FES owner board/kernel, root-only owner
node and an empty firmware search override. The image assembler certifies the
exact retained-programming source patch and extracts the linked kernel notes;
the runtime verifies the actual running note digest against that receipt.
An older kernel with the same release string cannot satisfy this contract.

The fixed overlay targets `/fpga-region` and carries only the fixed firmware
name and completion timeout. It creates no device, DMA grant, bridge list,
clock or topology override. Firmware is copied from the retained descriptor
using bounded-memory reads, synced, made read-only and closed before the kernel
opens it. A private overlay name and exclusive staging file refuse other
loaders and symlink/pre-existing staging collisions. The pinned configfs binary
attribute buffers writes and invokes programming on release: the adapter closes
it once, then checks deadline, `applied` and `operating`.

Within a successful context, replacement verifies the exact existing overlay
bytes/status before removing it. Any uncertainty after removal or submission
permanently poisons the context and retains files/configuration. No retry,
rollback program, destructor unload or close-as-drain operation exists. New
contexts refuse old overlays; safe restart adoption remains required before
normal platform admission. Software deadlines cannot interrupt a stalled
configfs close, secure call or shared-service mutex. Physical failure fencing
and allocated-DMA qualification remain separate open gates.

The [2026-10-31 physical programmer diagnostic](../../docs/validation/2026-10-31-de25-runtime-programmer/README.md)
records two known-core loads and BASIC7 at the selected C++/kernel bytes. It
does not admit the production factory or qualify failure/DMA recovery.

## DE25 computer driver integration

`De25ComputerCoreDriver` consumes the existing atomic client through the shared
CoreDriver lifecycle. It requires exactly computer ABI1.0, the three required
video/HID/ZX81-tape1.0 interfaces and the selected nonzero build identity before
control. Context core/build changes refuse lifecycle calls. BeginSession clears
selection only; it cannot reset sticky protocol uncertainty. Start records that
an uncertain release may have executed. Launch tape binding holds execution;
live replacement never sends an execution/reset command.

CoreDriver has optional rejecting HID/basic-media operations. FesGp overrides
preserve its implementation, and NativeHardware routes these calls through the
active driver. CoreDriver also owns optional unit-media observation/insert/eject operations;
NativeHardware consults the active driver before snapshotting or ejecting a
unit. The DE25 implementation exposes only the live limits/state proven during
identity. Its bounded BRAM-tape snapshot is read under the operation deadline;
insert never resets execution. Successful completion records ready/empty,
while transport uncertainty records loading and permits no cleanup/replay.
The BRAM contract has no tape-write bit, so its unit is not saveable. FesGp
retains its existing unit transfer/recovery behavior through overrides.

The DE25 driver now has separate de25_core_driver source/header files, leaving
the pure protocol consumer independently linkable by RTL proofs and maintenance
CLI. The production archive requires the driver object. The metadata checker and driver registry require explicit `NativePlatform::de25_nano`
selection and the generated DE25 profile/device row. The default stays DE10;
foreign profiles, including contained development, do not resolve on DE25.
One contract validator is shared by preflight and live Identify; it requires
format2, ABI1.0, selected nonzero128-bit build, and exactly the required
video/HID/tape interfaces. Production construction/startup remains closed. Scripted-register and selected-driver
regressions are software evidence; this driver has not run on the board.

## DE25 PR-control diagnostic

`De25PrControl` is a separate internal diagnostic protocol consumer. Its caller
owns bounded, ordered full-word register I/O and a validated persona
configuration callback. The controller verifies the selected 128-bit base,
uses strictly increasing command sequences, waits for positive freeze/drain
completion, and validates the base again before an identity-probed resume.
No callback runs before confirmed freeze. Read/write/configuration ambiguity,
unexpected state/sequence or deadline failure prevents further commands and
requires a fresh external recovery/identification epoch. This path is not
constructed by the production factory or exposed by the runtime daemon.

## DE25 diagnostic register client

`De25Computer` is a separately instantiated protocol consumer for the generated
`fes.fabric.de25-computer` register map. It reuses the abstract `Mmio` and
`Clock` interfaces with resource-relative offsets. There is no Linux MMIO
constructor, physical base, daemon operation or platform/profile admission for
this client. Its callers are scripted-register unit tests and the native/AArch64
DE25 ZX81 co-simulation in `misteross/boards/de25nano/zx81-hps/client_proof.py`.
That test connects the actual client to the endpoint/machine RTL, with explicit
simulation-time advances and separately labeled observation/transport faults.
The production factory and its exact ARMv7/DE10 guard above are unchanged.

The client requires an explicit nonzero 128-bit build ID, bridge identity and
version, exact `fes.computer` identity/capabilities, and live ZX81 tape limits
before controls are available. This diagnostic advertises fixed video, HID
keyboard and read-only ZX81 tape delivery; extra capabilities are refused.
Unlike the Cyclone V GP transport's old-toggle preload followed by a toggle,
each DE25 exchange performs exactly one ordered full-word GPO write. It checks
ready/fault and idle request/ACK consistency before writing, verifies the held
payload, polls coherent full-word GPI, then checks busy, held payload and fault
again before accepting completion.

Calls serialize on one client mutex. The caller must exclusively own and keep
the register backend alive; no resource lifetime or board qualification is
inferred from a successful identity. An ACK has at most 100 ms, bounded by the
caller's absolute clock deadline and a monotonic wall deadline. The complete
operation mirrors the caller's remaining deadline into wall time with a
120-second cap, so a stopped injected clock cannot extend it indefinitely.
Backward clocks, late ACKs, malformed GPI, a changed held payload, MMIO failure
or transport fault poison the client permanently. It exposes no clear/realign
or retry operation. A backend that blocks inside a bus access cannot be
interrupted by these software deadlines; bounded hardware access is a future
binding requirement. Known acknowledged command rejection remains a healthy
link, with invalid-state reported as busy, and is not retried automatically.

Hold/run return only after a zero-data success acknowledgement. HID snapshots
write all nine validated rows, including neutral state, then allow two wall
milliseconds for the keyboard mapper's nominal settling interval within the
operation deadline. Tape delivery uses generated CRC32 constants, bounded
1..512-byte chunks, little-endian packed words with zero odd-byte padding, and
requires MediaInfo ready after commit. Eject requires empty state afterward.
The caller owns stable payload bytes for the duration of the call. A failed
transfer is reported without automatic commit/eject/replay or execution release.
These checks establish software protocol behavior, not firmware/MMIO/reset or
physical keyboard/tape/display acceptance.

The separately invoked `de25-computer` maintenance CLI wraps `LinuxMmio` with
resource-relative access checks and ordered barriers. Before any register
access it requires AArch64 Linux, root, the FES DE25 rootfs/model, the exact
selected boot UUID and an explicit CPU resource base. It acquires a cooperating
exclusive lock, checks endpoint ID/version, and uses `De25Computer` for control.
The probe operation makes no control writes. This utility does not construct
production hardware or establish programming, reset fencing or package support.
Its MMIO mapping cannot interrupt a stalled bus access, so it is confined to
an exclusively owned, externally verified configured diagnostic epoch.

## Package lifecycle

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
`fes.computer` media units permit empty-drive startup unless a writable ST
ROM load explicitly requests initial library media; its complete prepared disk
transfer and ready confirmation then precede the first release.

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
Invalid state (GP error 4) on the eject-index or argument-0 attempt is
retryable busy and does not reach that begin. If clear starts with a poisoned
GP link from earlier keyboard traffic, it realigns from the live ACK and
re-identifies before sending an eject; a failed preflight remains busy because
this clear has not sent a media command. Once either eject command is sent, an
ambiguous or unstable GP response remains `io_failed` after best-effort
realignment because the core may already have accepted the eject. A hard MMIO
failure, or an invalid acknowledgement after these eject shapes, also stays
`io_failed`. Re-identify does not run for a completed rejection.
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
is admitted only alongside fixed video and HPS DDR. The only other interface a
menu package may declare is `fes.video.menu-blitter` 1.0 or 1.1 (bit 10), which
requires the menu display. The runtime grants the blitter window only when the
declared minor is 1 or later; a minor 0 declaration gets the ordinary slot-only
grant, so no socket peer can opt a shared grant into list-only commits. Live
identity must match the declared capabilities exactly. `MenuDisplayDriver`
validates fixed geometry, reads coherent counters, stages ordered submissions,
and waits for drained quiescence. `ConfigureMenuPackage` explicitly selects a described `fes.menu` package while
idle. Image startup may pass a sealed menu package selection to the daemon;
without one, startup remains splash. The interfaces `CheckMenu` admits are
listed in `src/native/menu_admission.hpp` and carried in the binary's
`.fes_menu_interfaces` section, where kit software packing reads them. Menu activation reuses ordinary package
programming, identity and DDR-port admission: zero both slots, configure,
release execution, then enable. Menu display never creates a playable active
package, gameplay generation or input worker. The existing local socket provides a bounded two-phase frame exchange; see
[menu presentation](docs/menu-display.md).

The client draws each frame straight into a slot; the runtime copies no
pixels. Before constructing production hardware, the runtime loads the
[fes-window driver](../../image/kernel/fes-window/README.md) that its software
release carries in `share/fes-window/`, beside `bin/mister-runtime`, with the
generated window base and size (`native/linux/window_driver.cpp`). A loaded
copy is kept only when its `version` and window parameters match; otherwise
the runtime unloads it, and a copy still in use is reported busy. A load
failure is logged; menu activation then fails to grant and the idle path
falls back to splash.

`MenuWindow` (`native/linux/menu_window.cpp`) owns one grant of both 4 MiB
slots at `0x30000000` per menu or session-plane activation. A menu with the
blitter's grant also covers its command lists and texture heap, 80 MiB in all.
Activation revokes any earlier grant and fills both frames black through a
separate slots-only grant, which it revokes before granting the client's.
That revoke writes back and invalidates every slot line, where a clean would
leave valid lines in the PL310. No stale slot line is then cached for a CPU
write to carry over what the blitter drew. A re-shown session plane is
granted again without clearing, so its pixels survive. `ShareMenuWindow` hands
any socket peer the grant's descriptor; `MenuBackSlot` names the slot scanout
is not showing. `PresentMenuSlot` refuses the displayed slot, writes back the
regions the client wrote (`FES_WINDOW_CLEAN`, L1 by address then the PL310)
and only then submits the slot, because fpga2sdram reads DDR past the caches.
A blitter grant takes CPU frames until the client's first list frame. From
then on only the blitter draws its slots and a CPU frame is refused, as a list
is on a grant without a blitter. A CPU frame's write-back leaves its lines
valid in the PL310; once the blitter has drawn over them, a CPU write could
make such a stale line dirty and evict it over the blitter's pixels. The next
activation's grant takes CPU frames again. For a list frame the runtime writes
back the list's commands and the heap bytes the client uploaded, checks the
blitter idle, runs it into the back slot and polls its completed count before
submitting. The completed count and busy state are separate GP reads; if the
count is still old when the state is idle, the runtime confirms the sample
again within the same deadline before rejecting a stable mismatch. A blitter
fault fails the present like any other completion fault.
Revoke ends every process's mapping and writes back and invalidates the
grant's lines. It runs before any lifecycle operation quiesces menu firmware
or programs another core, when a session plane is hidden, and on any failed
present, so nothing writes the window, and no dirty line is evicted into it,
once a core owns it. The grant does not isolate peers from one another: any
peer can already present, launch or Stop, so the slot rules bind only
cooperating clients. The runtime never reads the slots, so a write out of
turn changes only the picture. Host tests inject the driver calls and cannot
establish DDR visibility on hardware.

Menu status carries its own nonzero generation, geometry and completion
counters. `BeginMenuFrame` permits one preparation, naming the back slot,
without retaining the lifecycle lock while its caller draws. Both the
generation and runtime-created preparation identity bind `PresentMenuFrame`.
Presentation holds the existing busy fence through write-back, submission and
displayed-sequence polling. ACK alone never releases the previous slot. Only
one submission is pending. Launch, core-data, Stop, contained development
load, idle recovery, session-plane hiding and menu configure share one named
two-second wait (`kMenuFrameMutationWait`) while a frame is being drawn or
presented: from begin, whose expiry is polled because an abandoned
preparation does not notify, to the end of its commit. While any of them
waits, new menu frames cannot enter. After the frame fence drops, the
operation applies its existing busy checks and either claims the fence or
returns busy. It then revokes the window before it quiesces or programs, so a
client still drawing past the bound faults instead of writing a core's
memory. A same-thread re-entry still returns busy after the bound, because the
frame cannot finish until the caller returns. Stop reactivates an explicitly
configured menu with a fresh generation, including Stop from menu idle; that
reprograms the FPGA and blips HDMI. Splash Stop stays idempotent. If both
the menu reload and the splash reload fail, Stop ends in `reboot_required`.
A rejected pre-mutation game admission preserves the menu and its
preparation. Replacement and contained diagnostics revoke the old generation.

Menu quiesce proves drained state before execution hold. The hardware
underflow counter is cumulative and is not cleared by a GP command. It
resets when the bitstream is programmed and when port reset is asserted.
Each present records the counter before the submit and judges only the delta
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
containment leaves reboot-required. The grant remains owned by the
production hardware adapter; a new one is made after the new image is
programmed. Menu activation failures report unavailable with the error on
`menu_display` even when splash recovery succeeds. A later idle status does
not copy that menu error into top-level `status.error`; the operation that
failed assigns `status.error` itself. The runtime and the FogCast agent
decode this nested status strictly and must ship in the same image.
Capability-gated emission and tolerant decoding are not implemented. Host
tests do not establish physical scanout acceptance.

## In-session simple-computer presentation

Simple-computer `fes.video.session-display` 1.0 reuses the menu driver and
reserved DDR slots, with required `fes.memory.hps-ddr` 1.0 and its shared plane
`fes.fabric.video.display-plane` 1.0. The plane reports
pixel format 2, premultiplied RGBA, and the machine picture rectangle; the
client writes each pixel's alpha as the slot word's top byte, and status
publishes the rectangle as `menu_display.picture`. Its bit 9 and the
DDR bit 8 are included in exact declared/live identity checks; the plane
grants no bit. Admission advertises this interface and the plane only when boot
DDR layout and presentation adapters are available. Activation initializes
black slots and configures the disabled
plane once, then publishes active machine status with `session: true`, its
package and `core_generation`, but `available: false`.

`SetSessionDisplay` binds the active package/generation and serializes with the
same busy fence and bounded frame wait. It sets visibility and launcher focus
separately. Open enables the reader and grants a distinct display generation;
FPGA scanout waits for a complete submitted frame before switching HDMI pixels.
Taking launcher focus neutralizes the keyboard first. Moving focus on a shown
plane keeps its generation, grant and preparation. Close drains the plane
without execution hold and revokes the display generation and preparation.
Navigation key snapshots are suppressed while the launcher holds focus. For a
single gamepad, button snapshots are muted as well; controls observed while the
launcher has focus stay suppressed until they are released, so a later full
snapshot cannot restore them. The native adapter owns that gate: a close or
failure that cannot prove the plane drained keeps launcher focus, and status
reports it as `menu_display.focus`.
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
`fes.audio.pcm-s16-stereo-48k` and the Apple II, C64, Spectrum, ZX81 tape and
Atari ST media interfaces are independent and must be required when declared.
`fes.media.zx81-tape-write` 1.0 is admitted only with the base ZX81 tape
interface. Its snapshot capture leaves a completed SAVE frozen and dirty until
the host has durably stored the `.p` and issues a package/generation-bound
acknowledgement. Before freezing, the runtime reads the snapshot flags: an
all-clear word means that no SAVE completed, so capture returns empty and Stop
does not issue the Freeze command that requires `save_ready`. Stop and other
save-flushing transitions fail closed while a dirty snapshot remains. Host
coverage does not establish exact-artifact hardware acceptance.
Required `fes.memory.hps-ddr` 1.0 is admitted and advertised whenever the boot
core latched the port layout, including a kit without presentation adapters.
Required `fes.video.session-display` 1.0 is the in-core launcher plane: it is
rejected without DDR or without required `fes.fabric.video.display-plane` 1.0,
which is rejected without it, and a kit admits and advertises both only when
that layout and both presentation adapters are present. A shell declares at
most one
unit-0 medium. Recognized expansion buses must be
optional, unknown optional declarations are ignored and `core.system`
is rejected. Format 2 and format 3 firmware ROM packages are admitted; linked
cartridges and format 4 are not, because nothing holds execution for them.

Identity requires the registered live capability mask, including HPS DDR and session display, to equal the declared set,
then discovery reads MediaInfo for each declared unit, which must be present
and empty. Audio packets follow the declaration as for applications. Start
only releases execution: there is no media gate, no neutral write and no evdev
worker. A ROM-linked writable ST may explicitly request an initial library
floppy; native activation inserts and confirms that disk before Start. Stop holds execution first; the hold neutralizes keys and ports in the
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

The capability registry advertises `fes.computer` 1.0 and 1.1 as separate
version entries, ordered by ID, major and minor. A package's exact version
selects its matching entry. `fes.computer` 1.1 adds identity word 16,
the 17th identity word, compared like word 7 against the registered bits
16..31. Its `fes.persistence.words` registry has one layout,
`fes.pc386.machine` 1.0: the driver moves its 72 words on opcodes 24..27 with
the `fes.simple-game` freeze, begin and commit rules, and the record is keyed
by core and library game under `<data_root>/games/`. `fes.rtc.wall-clock` 1.0
takes the kit's local time from the hardware clock while held, after the
record restore and before Start. `fes.media.ata-disk` 1.0 is written into
the DDR window through short fes-window grants (`DiskWindow`) from the staged
base image and the game's `fes.ata-disk.delta` chunks, committed with the CRC
the core recomputes, and saved at Stop as content-addressed dirty chunks under
a merged manifest. Host coverage only.

Compatibility admission requires `fes.persistence.words` to pair with exactly
one registered layout before programming, including volatile developer loads
that omit a data root.

An empty ATA unit does not remove the pc386 package's persistent core-data
mode. Rejected insert/eject operations for that fixed unit leave the package's
persistence mode unchanged.

`fes.media.atari-st-floppy` 1.0 uses capability bit 7 and unit 0. Discovery
and every insertion without the geometry extension require live minimum and maximum sizes of 737,280 bytes,
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
of the immutable admitted raw ST base image. The runtime validates that source
before altering the unit, restores an existing compatible disk record, and
reports `persistence:{mode,game_id,base_media_id,revision}` on the live unit.
Raw `insert_media` remains volatile. A saved record cannot silently downgrade
to a shell lacking the write contract.

ROM operations accept an optional closed `initial_media` object with `path`,
`size`, `unit`, `data_root`, `game_id` and `base_media_id`. It requires a format-3
firmware package for `fes.atari-st`, the writable ST floppy contract, unit 0
and exactly 737,280 bytes. This object explicitly supplies library intent;
FogCast rejects it on developer requests. Its media root is independent of
any core-scoped data root. Absence preserves the existing empty-drive launch.

`PrepareInitialComputerMedia` snapshots and hashes the immutable source and
opens/checks its no-follow durable namespace and record during admission,
before outgoing save, quiescence or programming. After outgoing `FlushSave`,
`RefreshInitialComputerMedia` rereads the record and prepares its restore
snapshot, so replacement of the same namespace uses newly saved bytes.
Refresh failure restores the outgoing ownership through the existing save
failure path. The production hardware facade forwards both methods.
After programming and identity discovery, activation uploads retained bytes
and confirms the complete disk ready while execution is still held. Only then
is durable media metadata bound, and only the later Start releases execution.
Computer controller ports use `fes.gamepad.ports`, not the legacy
`fes.gamepad` input worker. Computer admission rejects required `fes.gamepad`,
so its fallible `input_.Start` path cannot run after ST execution release.
Failed upload never calls Start or replays the ambiguous media command; normal
launch failure cleanup applies. A failed Start with a bound disk is ambiguous:
the guest may already have run. Native hardware marks that result for fault-save
before idle cleanup. Runtime retains the selected package, ROM, generation and
persistent media projection privately while saving; public status keeps the
ordinary empty starting projection until either idle or retained recovery is
confirmed. Existing mailbox realignment and
identity checks precede a frozen full-disk capture; the release is never replayed.
Successful save permits idle cleanup and returns the original launch error.
Failed capture or publication leaves the FPGA and binding owned in recovery,
with Stop, replacement and generic idle recovery unable to discard them. Successful status reports the ready persistent
unit and active persistent mode. Tests compare the entire restored image and
cover preflight failures, same-namespace refresh, failed outgoing save and
failed upload. These are host software checks, with physical acceptance pending.

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

Stop and fault cleanup Freeze/drain the fixed ATA disk before capture, including
when initial dirty flags are clear. Ordered snapshot chunks read committed disk
RAM while CPU/video keep running. Publication precedes Saved; a failed capture
keeps the disk frozen until the existing save-failure recovery resumes it.
Publication rechecks each named chunk's length and content digest under the
namespace lock before replacing the manifest.
Insert, eject and explicit `save_media` reject the ATA unit while the computer
runs. Status retains the disk's durable binding but reports `saveable:false`,
so callers do not treat Stop-time persistence as a live checkpoint operation.
Periodic checkpoints remain future work. Writable Atari ST media retains its
separate eject/replacement and explicit checkpoint behavior.

Successful FPGA programming retires the former disk binding, captured bytes
and durable revision from memory. This includes Stop to the configured menu
or splash, core replacement and contained programming. The published record
stays intact. Without explicit initial library media, a new ST core reports
an empty volatile drive until library insertion binds and restores that record. Failed capture/publication
continues to retain the current disk and generation. Successful-transition
retirement follows programming, rather than the transition request. Partial
programming failures keep the existing invalidation of unsafe active-core
metadata.

Bound-disk input faults revoke asynchronous input and capture/publish before
idle programming. A persistent `fes.computer` machine record (the pc386) is
also captured on input fault, with its ATA unit bound, empty or undeclared;
Pong progress keeps Stop as its only boundary. The fault save first realigns
a mailbox the fault poisoned and reidentifies the core, as live media
operations do. A failed capture or publication retains RAM, package,
generation and binding in reboot-required recovery; it cannot program idle or
replay the fault. `recover_idle` also refuses to destroy retained bound disk
RAM, but a retained machine record does not block it: after a diskless pc386
fault whose record publish failed, `recover_idle` idles and the unsaved record
is lost, as after a failed Stop. Startup and failed activation do not save the
machine record; an ambiguous release with a bound disk still fault-saves that
disk. Volatile fault cleanup publishes nothing.
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
`RestoreInput` resumes every flushed ATA disk and machine-data snapshot before
reopening the same generation's host input. Persistent computer packages with
no host input recipe still resume their machine-data snapshot. Snapshots are
invalidated only after restoration succeeds. Unsafe resume
retains persistent package/generation metadata in `reboot_required` and leaves
the complete snapshot owned by native hardware. Successful Stop remains the
normal durability boundary; a post-start input fault also saves a bound disk
or a `fes.computer` machine record before idling. Startup and failed
activation do not save the machine record; an ambiguous release with a bound
disk still fault-saves that disk.
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
Parts admission accepts a Coleco application shell in format 2 or the factory
format-4 layout with exact 8 KiB firmware and 16 KiB cartridge ROMs, provided it
declares optional `fes.fabric.video.raster-rgb888` 1.0 or
`fes.fabric.video.native-pixels` 1.0, and optional Coleco bus 2.0. Duplicate or
simultaneous video markers are rejected. Fabric markers have no GP capability
and do not enter the runtime interface registry. The raster marker selects
layout `fes.coleco-video.parts/1` and map `fes.coleco-video.socket/1`. The native marker selects the separate
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

Required `fes.media.atari-st-floppy-geometry` 1.0 permits the 12 uniquely sized
raw ST layouts with 80–82 tracks, one/two heads, nine/ten sectors and 512-byte
sectors (368640–839680 bytes). Discovery proves capability bit 10 and exact
extended live limits. Nonlegacy immutable bases require matching BPBs; saved
records use minor 1, retain guest BPB edits, and must match the base length
before restore. Legacy 737280-byte behavior and minor-0 bytes are unchanged.
This extension has software coverage; no new hardware acceptance is claimed.
