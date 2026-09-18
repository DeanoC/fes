# Architecture

## Current boundary

This repository has one runtime implementation split across three narrow
roles:

- `libmister-runtime.a` owns the public lifecycle API, state, validation, and
  profile model.
- `mister-runtime` owns the local Unix-socket protocol and delegates every
  hardware-changing request to that lifecycle API.
- `src/native` and `src/linux` contain the Linux hardware primitives and the
  production construction boundary. `CreateProductionHardware` owns
  `PosixArtifactOpener`, `LinuxMmio`, `SteadyClock`, `LinuxFpgaManager`,
  `LinuxSpi`, `CoreLoader`, `MisterCoreDriver`, `LinuxI2c`, `LinuxFramebuffer`, `MenuVideoBringup`,
  `FixedVideoBringup`, `LinuxInput`, `NativeInputSession`, and
  `NativeHardware`. The dependency graph is:

  ```text
  LinuxMmio + SteadyClock -> LinuxFpgaManager + LinuxSpi
  LinuxSpi -> CoreLoader
  LinuxMmio + CoreLoader + SteadyClock -> MisterCoreDriver
  LinuxMmio + SteadyClock -> FesGp -> FesGpCoreDriver
  SteadyClock -> LinuxI2c
  CoreLoader + LinuxSpi + LinuxI2c + LinuxFramebuffer + SteadyClock + LogSink + fixed recipe
    -> MenuVideoBringup
  LinuxSpi + LinuxI2c + SteadyClock + LogSink + fixed recipe
    -> FixedVideoBringup
  SteadyClock -> LinuxInput
  LinuxInput + LinuxSpi + SteadyClock + bounded delivery timeout
    -> NativeInputSession
  all of the above -> NativeHardware
  ```

  Its installed idle path is `/usr/share/mister-runtime/idle.rbf`. The
  production profiles are `megadrive`, `pong`, `snes` and `nes`, with image-owned paths
  `/usr/share/mister-runtime/cores/megadrive.rbf` and
  `/usr/share/mister-runtime/cores/pong.rbf` and
  `/usr/share/mister-runtime/cores/snes.rbf` and
  `/usr/share/mister-runtime/cores/nes.rbf`.

FPGA-manager and SPI MMIO constants are checked-in generated C++14 text
from mister-packages (`src/native/generated/de10_nano.hpp`). Production
Mega Drive, Pong, SNES and NES profile fields come from
`src/native/generated/megadrive.hpp`, `src/native/generated/pong.hpp`,
`src/native/generated/snes.hpp` and `src/native/generated/nes.hpp`. The image still owns the absolute
RBF directory prefix.

mister-packages is host software. The generated headers are target
text for the ARMv7 Linux HPS runtime (Arm GNU C++14). The Pi / cross
build does not run Go and does not use a development-host compiler.
Regenerate those headers in mister-packages and replace the checked-in
copies. Do not hand-edit them.

Format-2 package preflight is a read-only native boundary. `OpenCorePackage`
opens an exact `manifest.toml`/`core.rbf` directory through one retained
directory descriptor, parses at most 65,536 manifest bytes, verifies the
retained payload descriptor's size and SHA-256, and computes the length-prefixed
package identity from the original bytes. The returned `OpenedCorePackage`
retains that payload descriptor for later programming, so a pathname
replacement after preflight cannot redirect activation. Structural admission
does not select hardware: `CheckCoreCompatibility` separately checks the
compiled target/profile/ABI/interface registry.

Production package admission and inspection first traverse one of the fixed
roots `/tmp/fogcast-development/core-packages` or
`/usr/share/mister-runtime/core-packages`. Each relative component is opened
from a retained parent descriptor with `O_NOFOLLOW`; empty components,
traversal, the root itself, and symlink components fail before package bytes
are read. This policy is part of `NativeHardware`, so direct library calls and
daemon requests use the same boundary. Tests and embedders can inject their
own trusted roots when constructing that hardware boundary.

`Runtime::LoadCore(directory, expected_package_id)` calls
`Hardware::AdmitCorePackage` while the current Status and session remain
unchanged. Admission returns an owned opaque `AdmittedCorePackage` only after
exact identity, compatibility, optional MiSTer system recipe, and
registered-driver availability all succeed. The runtime flushes any active
save before publishing `starting`, assigns a new generation, and passes the
retained object to `Hardware::LoadCore`; the package path is not reopened at
the mutation boundary. Native activation rechecks the
descriptor/profile/driver pairing, stops and joins any outgoing game input
session, and only then quiesces the outgoing driver and programs the package.
`Runtime::InspectCore(directory, expected_package_id, output)` uses the same
rooted byte and registry checks without consuming a generation, changing
status, or invoking FPGA, input, video, or driver operations. A structurally
valid unsupported package returns a successful inspection with a structured
compatibility error; malformed bytes or the wrong claimed package identity
fail inspection.

`CoreDriver` owns outgoing protocol quiesce and destination identity, button,
and start operations. `MisterCoreDriver` is the production MiSTer adapter.
`FesGpCoreDriver` is the production `fes-gp-v1` adapter over the bounded GPO/GPI
transport. Package admission requires that registered driver before save,
input, state, video, or FPGA changes.

After programming a FES package, activation resets the transport session and
reads all 16 identity words under a two-second deadline, with each exchange
bounded to 100 ms. It sends no destination control until the descriptor ABI,
capabilities, and build ID match. The fixed custom video path configures and
validates the ADV7513 entirely over I2C, without issuing a MiSTer SPI timing or
audio command. It then sends a neutral normalized button map, releases
gameplay, and starts the input worker with the activation generation. Stop
retires and joins that worker, sends its final neutral map, and only then asks
the outgoing driver to hold gameplay reset. Opposite directions resolve to a
neutral pair, and a retired generation cannot deliver to a replacement core.

The package registry consumes checked-in generated C++14 headers
`src/native/generated/fes_gp.hpp`,
`src/native/generated/fes_simple_computer.hpp`, and
`src/native/generated/de10_nano_programming.hpp`.
The programming table lists `fes.simple-game` 1.0 and `fes.simple-computer` 1.0
on `fes-gp-v1`. `CheckCoreCompatibility` and `FesGpCoreDriver` identify both
ABIs over the same GP transport: tag 1 with gamepad plus fixed 720p60 for
Pong, tag 2 with keyboard, fixed 720p60 and media blob for ZX81. Gamepad
input stays disabled for the computer ABI. After video bring-up the driver
writes eight neutral keyboard rows. A verified stream-media session explicitly
holds execution reset and completes activation without releasing: a fresh
endpoint has no committed media yet. Its later media transaction releases only
after successful Commit. Non-stream computer startup still releases execution
as before; the decision uses verified stream support, not a core ID or a bare
capability bit. Keyboard or hold failures fail activation without release.
Live `set_keyboard`
uses a 40-bit active-low matrix (five bits per ULA row); `load_media` reads a
1..16384-byte regular file into a bounded snapshot before touching the core.
The blob ABI is filename-independent: ZX81 `.p` and Coleco cartridge bytes use
the same operation. Final symlinks, FIFOs, empty and oversized files are
rejected. The driver holds execution reset, sends begin/data/commit, and only
releases reset after a successful commit. The core must keep execution held
until its committed blob is ready for consumption. A transfer failure after
acknowledged hold leaves reset held; the caller can retry a rejected command.
A poisoned transport can prevent Stop from quiescing the core and require the
existing reboot recovery path. ZX81 uploads now reset the machine; wait for
BASIC before issuing `LOAD ""` against the retained blob.
The runtime owns reset ordering; FogCast owns upload staging and session/lease
admission. These development loads remain volatile. Advertised ABIs are sorted by
id so FogCast protocol-2 inspect can admit ZX81. The programming-profile header retains the reviewed mister-packages tree
`85a7771470ef0ff872e7a27d9fbf87d102e4a30f`. The GP header and persistence
fixtures are generated from mister-packages
`bfc4b2bc8232c93d67f88bd452223986768bfe4f`; target builds do not run Go.
The language-neutral conformance corpus under
`tests/fixtures/core-bundle-v2/` is an exact copy of the reviewed Task-1 corpus;
its sorted file-digest-list SHA-256 is
`2fcc3812f3c77cd2b65893f61fd9b9f8f91f3d4ad931f2096fc5914fb1c4b004`.
The vendored standard-library-only toml11 v3.8.1 headers, MIT license and file
digest receipt live under `third_party/toml11/`.

ADV7513 programming lives in `src/native/adv7513.hpp`. Register addresses,
bitfields, and CEA-861 AVI/VIC values are named from the public ADV7513
datasheet and Programming Guide. Analog Devices "must be set" bytes stay
under `adv7513::adi_required` because those internals are not documented.
`Menu720p60Recipe()` composes those named writes; the I2C byte sequence is
unchanged from the known-working Main table.

The library does not own a network API, catalogue, transfer cache, or host
session. A future target agent integration belongs outside this repository
and will call the daemon over `/run/mister-runtime.sock`.

## Diagnostic event ring

The production daemon keeps one bounded in-memory diagnostic ring and publishes
the same window to `/run/mister-runtime.events.json` using the FogCast
mister-agent #206 event shape `{ts_utc, mono_ms, flight_id?, lease_gen?,
run_id?, layer, kind, severity, detail}`. `flight_id` is copied only when a
caller supplies the canonical host UUID v4; `lease_gen` and `run_id` remain
opaque join strings. The runtime never invents those fields.

Native sites emit the Main/FIFO/fpga_manager/CORENAME phases the agent ring
already names:

- `fifo.consume` when the daemon handles `launch`, `load_core`,
  `load_library_core`, `update_core_settings`, `load_development_rbf`, or `stop`.
  Each event records completion success or failure; read-only package/data
  inspection does not emit a mutation-completion event
- `cap.fd.open` when an artifact descriptor is admitted or rejected
- `fpga_manager.state` on MMIO preflight/reset/configuration/initialization/user
  transitions, plus sysfs `state`/`status` only when those files are present
- `corename.change` from the SPI core-name probe (native authority; `/tmp/CORENAME`
  is attached as `file_observed` only when readable)
- `main.start` / `main.exit` / `main.app_restart` for the `mister-runtime`
  process
- typed fences `fence.ownership`, `fence.handoff`, `fence.program`,
  `fence.abi`, and `fence.recovery`

Unavailable Main FIFO or sysfs observations stay absent. Optional CORENAME
and sysfs files are opened `O_NOFOLLOW|O_NONBLOCK` and omitted unless they are
regular files, so a FIFO or symlink cannot stall Probe or FPGA programming.
Dump publish uses the same flags on its temporary file. The dump is
read-mostly and never bypasses the lifecycle or FPGA ownership path.

## Lifecycle and ownership

The lifecycle states are `idle`, `starting`, `running_game`,
`running_development`, and `reboot_required`. Starting the runtime deliberately
asks the hardware boundary to establish idle. A game launch validates the
entire request against the one runtime-owned profile table before mutation; a
development launch validates its absolute RBF path without inventing a system
profile. Stop flushes an active ordinary SNES battery save, then returns the hardware
to idle.

The FPGA manager takes one explicit `ProgrammingProfile`. It reads only board
status/control during preflight, contains the system-manager interface, SDRAM
ports, bridges and L3 remap, asserts `nCONFIG`, and waits for configuration
reset before writing any destination GPO value. `mister-v1` then initializes a
MiSTer reset value and, after verified configuration, releases the established
SDRAM/bridge/remap recipe and core-normal value. `fes-gp-v1` initializes GPO to
zero while reset is asserted and leaves bridges and SDRAM contained.
`development-contained-v1` performs no ABI write and also remains contained.
The active driver performs outgoing protocol reset before this sequence;
startup has no active driver and sends no guessed word to unknown fabric.

Native idle admission performs this exact sequence:

```text
open locked idle RBF
  -> read-modify-write ADV7513 main power-down to present a clean link loss
  -> program FPGA and release bridges/core hardware reset
  -> toggle the FPGA core-ID strobe and sample GPI
  -> assert menu-core software reset over user-I/O SPI
  -> probe and require core identity MENU
  -> locate ADV7513 main map 0x39 (PD/AD low, Programming Guide 0x72) on /dev/i2c-0 through /dev/i2c-2
  -> apply the fixed ADV7513 initialization
  -> send the fixed 1280x720@60 timing and PLL words
  -> apply the fixed 720p ADV7513 mode registers
  -> release menu-core software reset
  -> emit the bounded ADV7513 wake edge and neutral core-input packet
  -> configure and validate the Linux framebuffer, enable HPS framebuffer over SPI
  -> require ADV7513 HPD and monitor-sense status
  -> publish idle
```

When a game input session is open, `LoadIdle()` first prevents further input,
joins its worker, attempts the session's final neutral packet, and closes its
descriptors. At process startup there is no input session, so the same idle
path safely begins at the locked idle-RBF open. One absolute deadline bounds
each native stage. An idle failure enters `reboot_required`; it does not retry,
fall back, or reboot automatically.

Mega Drive launch performs this exact sequence:

```text
validate the complete profile request
  -> resolve and open exactly one FogCast Virtual Gamepad
  -> open and validate the locked Mega Drive RBF and every media artifact
  -> sort opened media by profile-owned index
  -> read-modify-write ADV7513 main power-down to present a clean link loss
  -> program the FPGA
  -> toggle the FPGA core-ID strobe and sample GPI
  -> assert profile-owned core reset
  -> probe and require core identity MegaDrive
  -> apply the profile-owned initial status
  -> attach cartridge at file index 1 using little-endian byte pairs
  -> apply the fixed 1280x720@60 ADV7513 path and wake the transmitter
  -> initialize the generic core buttons/switches word to neutral
  -> require link status
  -> send a neutral player-one map
  -> release core reset
  -> start the owned input worker with the new runtime generation
  -> publish running_game
```

MiSTer-compatible development RBF loading performs this software sequence:

```text
open and validate the complete staged RBF
  -> read-modify-write ADV7513 main power-down to present a clean link loss
  -> program the FPGA exactly once from the opened artifact
  -> toggle the FPGA core-ID strobe and sample GPI
  -> optionally observe the MiSTer core identity
  -> publish running_development with no system identity and HDMI down
```

The observation is reported as core identity only; it is not a system or
catalogue claim. Development loading does not run fixed video bring-up, media,
reset, status, or input operations. Its physical-hardware acceptance remains
pending.

Format-2 MiSTer packages use the same explicit-development semantics. A
declared `core.system` must resolve in the compiled Profiles table and selects
its expected MiSTer identity and recipe metadata; omission remains valid for
an explicit development package. Package loading never infers media, launches
a game, opens input, or fabricates live build capabilities. Status records the
verified package ID and declared core metadata (including the optional system)
inside `active_package`, plus the separately observed MiSTer identity. The
top-level system remains absent for every development execution, preserving
the protocol-1 state shape. Existing raw `LoadDevelopmentRBF` remains a `mister-v1`
compatibility adapter. Native-only explicit contained loading selects
`development-contained-v1` and performs no identity or controller operation.

Input resolution and all artifact opens complete before the transmitter is
quiesced and FPGA programming begins. Quiescing preserves every other ADV7513
power-register bit and uses the existing bounded video deadline; failure stops
before FPGA mutation. The subsequent fixed bring-up reinitializes the ADV7513
and powers the output back up after the new core is programmed.
Neutralization succeeds before reset release, and the generation-bound input
worker starts before `running_game` becomes observable. Stop retires the
active generation while save/input shutdown runs, restores it when save
persistence fails, joins and neutralizes input through `LoadIdle()`, performs
the existing Menu idle bring-up once, and publishes `idle` only on success. A
second launch repeats preflight and creates a new generation, descriptor
session, and worker.

At most one hardware-changing operation is admitted. A concurrent external
lifecycle mutation is rejected as `busy`; external operations are not queued.
The one-shot hardware-fault notification is only deferred until the active
mutation releases that same boundary. After a mutation has begun, a failed
launch gets exactly one cleanup attempt. Cleanup success returns to `idle` with
the original error; cleanup failure returns `reboot_required`.
An outgoing-driver quiesce failure that attempted protocol mutation retires
that driver identity before cleanup, so the containment/Menu recovery does not
repeat an ambiguously completed protocol command. A failure known to precede
protocol mutation retains the driver and lets the single cleanup attempt make
its ordinary quiesce decision.
FES GP discovery authorizes that cleanup command only after all identity words
arrive through a stable transport and the protocol header, ABI, and required
capabilities match. A later build-ID mismatch retains the programmed driver's
context for one hold-reset command before Menu programming. A transport,
header, ABI, or required-capability failure retires it, so recovery sends no
command to an unverified fabric.
There is no retry loop, failover path, recovery coordinator, or second
ownership database.

`Runtime::Impl` is the sole `HardwareFaultSink`. `Hardware::SetFaultSink`
installs it before startup idle, and each admitted game or package hardware
launch gets a strictly increasing generation. The input worker callback only enqueues its
generation-tagged error and returns. Enqueuing an active-generation fault also
reserves that generation under the runtime mutex, so Stop is rejected as busy
until the drain owns cleanup. A private runtime drain thread admits the fault
through the same mutation boundary, then invokes `LoadIdle()` exactly once; it
never joins input from the input worker itself. Before that blocking recovery
call, it publishes fresh `starting` status with no retired generation, package,
system/core identity, or active interfaces.
Cleanup success preserves the direct input fault in idle status, cleanup
failure publishes `reboot_required`, and stale generations perform no work.

Profiles own core identity, semantic media roles and indices, core and input
recipes, and allowed settings. Callers own selection and staging of absolute
paths. The production Mega Drive row is filled from the generated system
table plus the image-owned cores directory. Test profiles are private
fixtures and cannot be selected by the production daemon. The canonical
software-versus-physical record is the [support matrix](docs/support-matrix.md).

The MiSTer-compatible development RBF path is software-implemented with
physical-hardware status pending. It does not promise development video.
Native audio, save RAM, save states, six-button X/Y/Z/Mode input, multiplayer,
remapping, and hot-plug recovery are outside this slice. FES currently admits
the basic NES slice described below; every other game system remains
unsupported. The runtime does not preserve a running game
across restart and does not add conventional Main, transient MGLs, or automatic
legacy fallback.

## Protocol

Computer stream media uses the same owned runtime busy boundary, with exact
active package/generation binding before hardware access. Observed stream Info
is retained separately from the driver registry. An unlinked, bounded-buffer
snapshot supplies CRC-checked chunk delivery; no live source or host-sized byte
array is retained during GP transfer. A poisoned mailbox is never replayed.
See [stream media](docs/media-stream.md) for the protocol-2 extension, optional
observed capability, timeout budgets and terminal cleanup-failure semantics.

Protocol 1 accepts exactly four operations over a local Unix socket, one JSON
request and one JSON response per connection:

- `status`
- `launch`
- `load_development_rbf`
- `stop`

Requests reject unknown fields. `launch` accepts a stable system ID, an
absolute RBF path, semantic media paths, profile-declared settings, and an
optional SNES-only absolute `save_path`.
Responses report `ok`, lifecycle state, execution type, system/core identity
when present, a direct error when present, and the runtime version.

Protocol 2 accepts `status`, `inspect_core`, `load_core`,
`load_development_rbf`, and `stop`; it deliberately has no game `launch`.
Package requests carry one absolute package path and the exact 64-character
lowercase package ID. Raw diagnostic loading requires the literal
`development-contained-v1` profile and never advertises an ABI, package, video,
or input capability. Each v2 response always has twelve closed top-level
fields. In addition to the v1 lifecycle fields it reports the sorted installed
profile/ABI registry, currently active interfaces, the confirmed active package
and observed ABI/build identity, the positive active generation or null, and a
separate successful inspection result or null. MiSTer package observation has
null ABI and build identity because its probe proves neither. Errors add a
bounded phase and omit unavailable expected/observed strings. The active
interface list is the sorted exact descriptor/installed-registry intersection
verified by the live driver. Save, quiesce, programming, transport, identity,
video, input, and recovery producers retain their concrete phase; identity
mismatches include bounded safe expected/observed evidence. Protocol-2-only
error codes project to `invalid_request` when old protocol-1 clients reconcile
the same lifecycle error; no v2 fields or error members enter a v1 response.

The maximum request frame is 65,536 bytes including its newline terminator.
Decoded path strings reject embedded NUL bytes at the protocol, lifecycle,
profile, and POSIX artifact boundaries so the path validated by the runtime is
the path presented to the operating system.

`status` is the sole reconciliation mechanism for a response lost after
dispatch. The caller observes daemon state instead of guessing whether a
mutation happened or consulting another authority.

## Build and link closure

The canonical host build produces one production archive and one daemon. The
daemon links the whole archive so unresolved or accidentally omitted native
members fail at the final link. Acceptance guards audit the archive manifest,
exclude fake and historic symbols, exercise test and production header
dependency invalidation, verify incremental version embedding, and compare two
clean archive hashes for determinism. Host and 32-bit Arm production builds
use 64-bit file offsets for high-address MMIO mappings.

## ROM-less Pong software integration

Pong uses protocol-v1 `launch` with system `pong`, its profile-owned RBF path,
`media: {}` and `settings: {}`. Extra media and a different RBF path are rejected
before hardware mutation. The same lifecycle preflights input and the RBF,
quiesces HDMI, programs/synchronizes, asserts reset, checks identity `Pong`,
applies initial status, configures video, neutralizes input, releases reset and
starts input. There is no media transfer. Stop restores the existing idle path.

The wrapper contract uses reset assert/initial status 1 and release 0, joystick
command 0x02, Up 0x08, Down 0x04 and Start 0x80. Existing Left/Right/A/B/C bits
remain reserved in the packet and are ignored by Pong. No second protocol or
hardware construction path is introduced. Software tests cover this ordering
and relaunch; physical video/input/audio acceptance is pending. Game bringup writes MiSTer audio attenuation zero after HDMI link verification
and before returning to input neutralization/reset release.

## Game audio attenuation

The pinned Pong Template framework (`3ea1134cf05d62c2b1db30362277a823d739ced2`)
and Mega Drive framework (`7365a137cfd8fa6f041e964d8b953159c0ec42d9`)
initialize `sys/sys_top.v` volume attenuation to 0x1f and accept user-I/O
command 0x26 with the low five payload bits. Bit 4 mutes their audio output.
`FixedVideoBringup` sends `{0x0026, 0x0000}` after HDMI link readiness, under
the existing absolute video deadline, while the game remains held in reset.
The `audio_volume` phase reports failure before running state; ordinary launch
failure cleanup reloads idle. Menu bringup and raw development loading do not
unmute. Stop reprograms the menu, restoring its default muted attenuation.

This enables the existing framework audio stream at zero attenuation. It adds
no mixer or audio service and is software-tested only; audible HDMI output
still requires physical acceptance for each exact core/runtime/image pair.

## SNES content and input

`MediaTransform` describes content separately from `FileWireFormat` byte-pair
encoding. Generated media rules select `raw` or `snes_cartridge`; preparation
carries that choice into `OpenLaunchArtifacts`. The latter retains the opened
ROM descriptor and validates its header before quiescing HDMI or programming.
`MediaContentPlan` stores only a source window and 512 bytes of metadata.
`CoreLoader::Attach` streams that prefix/window through the existing bounded
4096-byte transfer buffer, using the retained descriptor. Short reads abort
without sending completion; ordinary post-program failure cleanup reloads idle.

The bounded SNES slice admits exactly one coherent LoROM (mapper 0x20/0x30,
header 0x7fc0) or HiROM (0x21/0x31, header 0xffc0) candidate with valid reset
vector/opcode, nonzero complementary checksum pair and matching declared ROM
size. Payloads must be powers of two from 32 KiB to 4 MiB; HiROM needs at least
64 KiB. An optional copier header is exactly 512 bytes. Types 0–2 are ordinary
cartridges; RAM exponent is at most 7. Optional ordinary battery RAM persistence is
described below. Special
mappings, BS-X/Sufami signatures, ambiguous candidates and enhancement types
are rejected. No mirroring, patching or whole-ROM allocation is performed.
Metadata fields follow Main_MiSTer `915ca339` and SNES_MiSTer `93d359e6`;
source bytes stay unchanged. Maximum source admission includes the copier
header (0x400200 bytes).

The common required input masks are directions, A/B and Start. C and the
appended X/Y/L/R/Select masks may be zero; nonzero masks must be unique single
bits. An event mapped to zero is ignored without sending a packet or faulting.
SNES uses bits 0–3 for Right/Left/Down/Up, bits 4–9 for A/B/X/Y/L/R, bit 10
Select and bit 11 Start; C is unused. Linux BTN_X/BTN_Y/BTN_TL/BTN_TR/BTN_SELECT
are decoded into the new controls. Existing Mega Drive C and Pong Start retain
their original masks. All profiles use the same input worker and neutral Stop.

## NES content and input

`MediaTransform::nes_cartridge` validates the retained file before HDMI
quiesce or FPGA programming. The 16-byte header must carry `NES\x1a`, clear the
trainer flag, and declare a nonzero PRG payload. Both iNES 1.0 page counts and
NES2 linear or exponent/multiplier sizes are decoded with checked arithmetic;
the declared payload must fit the file and the 32 MiB profile bound. The
artifact plan has no prefix or save data, so `CoreLoader::Attach` streams the
original bytes unchanged at native filetype index `0x40`. The NES core encodes
the first `FS,NESFDSNSF` entry as `0x40` (NES type in bits 7:6 and slot zero in
bits 5:0); the legacy Main selector remains a separate zero-based value.
Because this core instantiates `hps_io` without `WIDE`, each cartridge byte is
sent in the low eight bits of its own 16-bit SPI word. Wide cores use the
little-endian byte-pair format instead.
Mapper selection remains in the
upstream core. The production row uses the generated NES input masks for one
standard controller; FDS, UNIF/UNF, NSF, trainers, saves, cheats and extra
peripherals are not admitted.

## SNES save lifecycle

`MediaContentPlan::battery_ram_size` is derived only for type-2 battery
cartridges with nonzero RAM exponent. `OpenLaunchArtifacts` prepares a retained
save directory, exact original bytes and writable temporary-file admission
before programming. It never creates a final save for nonbattery cartridges.

`CoreLoader::Attach` mounts slot zero with 0x1d/0x1c after the complete cartridge
payload but before download completion. Zero image size represents a missing
save and preserves the core's 0xff RAM initialization. Existing images trigger
automatic restore after download completion. `RestoreSave` handles exact,
sequential 512-byte sectors through commands 0x16 and 0x17 before reset release
and input start. This is the existing pinned SNES_MiSTer 93d359e6 contract.

The native hardware retains the save only after successful launch. Ordinary
`Runtime::Stop` admits `Hardware::FlushSave` under the existing busy boundary
while retaining running-game identity. It invalidates the input generation
before the flush, so a late input fault cannot discard a retained snapshot after
a save failure. It stops/joins
input before SPI, asserts reset, pulses status bit13 and reads all sectors
through 0x18. Each request must identify modern protocol, slot zero, one
512-byte block, the expected direction and sequential LBA. One absolute
core-I/O deadline bounds polling and transfer. A prior interrupted snapshot is
drained under reset before a fresh snapshot; partial bytes are never published.

`SaveFile::Persist` writes a sibling exclusive temporary file, fsyncs it,
renames it atomically over the destination and fsyncs the retained directory.
Failure preserves the complete snapshot for a later Stop retry. A failed flush
returns `save_failed`, records that error in status and releases the busy flag,
but retains the game session and does not load idle. Input remains stopped and
the game may be frozen. Successful retry clears the error through ordinary idle
completion. An explicit Stop while already idle acknowledges and clears any
historical admission/launch error without touching hardware. Generic `LoadIdle` discards persistence state without flushing, so
startup, failed launch and input-fault cleanup cannot replace a good save with
partial or uninitialized SRAM. This deliberately excludes autosave, crash or
power-loss capture, save states and host/cloud save synchronization.

## Idle framebuffer ownership

`LinuxFramebuffer`, injected through `Framebuffer` into `MenuVideoBringup`,
writes `8888 1 640 480 2560` to the MiSTer_fb module mode parameter on each
idle bring-up. It reads fixed/variable framebuffer ioctls, requires packed
truecolor 32-bit BGRX, no panning, 640×480 visible and virtual geometry, stride
2560, sufficient backing bytes, and the reserved physical address 0x22001000.
No fallback framebuffer or separately allocated DDR buffer is admitted.

After neutral input, Menu bring-up sends user-I/O command 0x2f with enable/format
0x8016, the validated address and geometry, scaling bounds (0,1279,0,719), and
stride. A zero capability response is rejected. The already released Menu status remains zero. Main's framebuffer status
helper shifts and masks its argument, leaving bits[8:5] zero; the apparent
0x160 call-site argument is not a raw status word. All SPI operations share the existing absolute video deadline; Linux
configuration checks that deadline before and after device operations. Device
syscalls have no additional userspace interruption mechanism.

Stop already reloads Menu through this same path, so it restores framebuffer
selection without a second display authority. Game video bring-up is unchanged.
The launcher must pause presentation while a core owns HDMI and reopen/recheck
its framebuffer mapping on confirmed return to idle. These words are derived
from Main_MiSTer video_fb_enable and remain hardware-unaccepted until a dated
exact-artifact diagnostic validates the selected Menu core and kernel.

## Described-core persistence

The `CreateProductionHardware` facade forwards preparation, refresh, inspection,
and settings updates to its owned `NativeHardware`, alongside the existing
admission and lifecycle methods. A host regression enters through this factory
and validates durable data operations without starting hardware; direct
`NativeHardware` tests alone do not verify production API forwarding.

`native/core_data` owns the bounded canonical record codec and retained
no-follow namespace directories, separate from cartridge `SaveFile` and its
power-of-two SRAM constraints. `CoreDataFile::Read` reopens `record.bin` on every
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
The existing SNES transport, `.srm` identity and byte format are unchanged.

See [the complete local protocol and error contract](docs/core-persistence.md)
for derived layout metadata, persistence modes, downgrade rejection, CAS,
volatile development behavior and recovery envelopes. This implementation has
software coverage only; no physical support claim is added.
