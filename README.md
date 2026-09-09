# libmister-runtime

`libmister-runtime` is the standalone C++14 lifecycle library and local control
daemon for the native MiSTer hardware-control direction. The repository keeps
the lifecycle API, native Linux primitives, profile model, and
`mister-runtime` daemon behind one build and one ownership boundary.

## Current status

Production native construction includes Mega Drive, ROM-less Pong, basic SNES and native NES profiles, fixed
1280x720@60 game and menu video paths, and one exact FogCast virtual-gamepad
input session. Host tests cover complete launch, Stop, idle cleanup,
asynchronous input-fault cleanup, and immediate relaunch. Dated exact-image
physical acceptance covers the same launch/input/Stop/relaunch slice.
Hardware-supported systems: 1.

Mega Drive is software- and hardware-supported on the designated FogCast kit.
The accepted slice proves visible HDMI and playable one-player D-pad and jump
input on a physical MiSTer. Native audio, save RAM, save states, six-button
X/Y/Z/Mode input, multiplayer, remapping, hot-plug recovery, and
development-RBF loading/video acceptance remain outside this slice. Pong, SNES
and NES are software-supported while their exact-image hardware acceptance is
pending. The remaining rows are unchanged; every other production system remain unsupported.
The native path does not
preserve a running game across restart and does not start conventional Main,
transient MGLs, or an automatic legacy fallback. Fakes under `tests/` verify
software contracts only and are not physical evidence. See the
[dated FogCast hardware baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md).

Pong has software-tested profile admission and ROM-less launch/Stop/relaunch
through the ordinary native lifecycle. It uses the image-owned
`/usr/share/mister-runtime/cores/pong.rbf`, core identity `Pong`, and empty
media/settings objects. Up/Down/Start use the existing one-player packet;
Left/Right/A/B/C are reserved and ignored by the wrapper. A matching core must
be installed by image assembly. Pong video, input and audio have no physical
acceptance; native audio remains unaccepted for Mega Drive as well. Game
bringup now clears the MiSTer framework mute after HDMI link verification;
software tests cover its command, deadline and cleanup, not audible output.

HPS MMIO constants and the Mega Drive/Pong/SNES/NES profile tables are generated C++14
headers checked in under `src/native/generated/`. They are target text
for the ARMv7 Linux HPS, not host objects. The target build does not run
Go.

The library API exposes `Runtime::InspectCore(directory,
expected_package_id, output)` for read-only package identity, descriptor and
compatibility inspection, and `Runtime::LoadCore(directory,
expected_package_id)` for activation. Hardware implementations
admit the exact format-2 package into an owned opaque `AdmittedCorePackage`
before the runtime flushes save data or changes visible state, then consume
that retained object with a new generation at the mutation boundary. Packaged
MiSTer cores run as explicit development loads: an optional declared system is
checked against the compiled Profiles table, while media and input are never
inferred; it remains only in the package descriptor and the top-level
development system stays null for protocol-1 compatibility. FES GP package
activation is software-tested through the production
MMIO driver, fixed ADV7513-only video path, and generation-bound normalized
gamepad sink. It verifies all 16 identity/build words before controls, then
brings up video, sends neutral input, releases gameplay, and starts input. The
Protocol 2 on the local socket exposes `status`, `inspect_core`, `load_core`,
explicit contained diagnostic RBF loading, and `stop`. Every response reports
the actual profile/ABI registry, active package identity and lifecycle
generation. Active interfaces are the sorted exact intersection of the
admitted descriptor and installed ABI registry. Concrete operation failures
retain their phase and safe expected/observed identity evidence; protocol 1
retains its original request and eight-field response
contract. Package paths are accepted only below the production roots
`/tmp/fogcast-development/core-packages` and
`/usr/share/mister-runtime/core-packages`, with descriptor-relative no-follow
traversal. The raw diagnostic path is
`/tmp/fogcast-development/core.rbf`. Replacing a running game
with a package first joins and
neutralizes the old input session; an ambiguously failed outgoing-driver
quiesce is not repeated during the one bounded Menu recovery.

Native launches keep short deadlines for core control, video, and input setup,
then give each cartridge transfer its own 120-second deadline. The HPS SPI
bridge performs an MMIO handshake for every 16-bit media word, so using the
control deadline for a multi-megabyte cartridge would reject valid content
before the core can start. The media bound is still finite; a transfer that
stalls leaves the ordinary failure and idle-recovery path.

## Described-core settings and progress

Protocol 2 adds `load_library_core`, `inspect_core_data`, and
`update_core_settings` for exact admitted packages. FES Pong library launches
restore and durably publish its paddle-speed setting and best-rally record at
successful Stop/replacement. Development `load_core` remains explicitly
volatile. Status reports `active_package.persistence_mode`; data inspection
returns durable values and a record revision. See the
[local API and recovery contract](docs/core-persistence.md). This capability is
software-tested; hardware acceptance is pending.

## Build and test

```sh
make all
make test
```

The host build produces `build/libmister-runtime.a` and
`build/mister-runtime`. See the [support matrix](docs/support-matrix.md) for
the single canonical support record, [architecture](ARCHITECTURE.md) for the
runtime boundaries, and [development guide](DEVELOPMENT.md) for full local and
cross-build checks.

## Basic SNES software integration

The `snes` profile uses `/usr/share/mister-runtime/cores/snes.rbf`, core identity
`SNES`, and required cartridge media at index 1. Artifact preflight accepts a
32 KiB–4 MiB power-of-two LoROM/HiROM payload, optionally preceded by a 512-byte
copier header. It rejects malformed/ambiguous headers, enhancement cartridges,
special mappings and unsupported sizes before FPGA mutation. The runtime
streams a synthesized 512-byte core metadata prefix followed by the retained
ROM file window; the host supplies the original ROM and never builds metadata.
Ordinary cartridge types 0–2 and RAM exponents up to 7 are admitted. RAM defaults
to volatile. Optional SNES `save_path` persists ordinary type-2
battery cartridges on clean Stop; see below. Enhancement chips and full-library
compatibility are excluded.

The retained gamepad now accepts X/Y/L/R/Select in addition to existing controls.
Zero-mask controls have no effect on Mega Drive/Pong and do not end sessions.
Tests cover individual SNES masks and Stop neutralization. SNES physical
video/input/audio acceptance is pending; this software profile is not evidence
of a working hardware system.

## Basic NES software integration

The `nes` profile uses `/usr/share/mister-runtime/cores/nes.rbf`, core identity
`NES`, and required `.nes` cartridge media at native filetype index `0x40`.
The index encodes the first `FS,NESFDSNSF` entry (NES type in bits 7:6, slot
zero in bits 5:0); it is separate from the legacy Main selector. Artifact preflight accepts
iNES 1.0 and NES2 headers with a nonzero PRG payload, no trainer, and a declared
payload that fits the supplied file and 32 MiB bound. Source bytes are retained
and streamed unchanged through the narrow low-byte loader; no
mapper or battery interpretation is added here. FDS, UNIF/UNF, NSF, trainers,
cheats, saves, special peripherals and four-player accessories remain outside
this slice. NES video/input acceptance is pending for the exact assembled image.

## SNES battery RAM saves

Protocol-1 SNES launches may include an absolute `save_path`. FogCast owns the
per-game path; the runtime derives eligibility and size from the retained ROM.
Type-2 battery cartridges with RAM exponent 1–7 use 2–128 KiB files. Omit the
field for volatile operation; nonbattery and zero-RAM cartridges create no file.
The parent directory must already exist. Existing saves must be regular files
of exactly the derived size; admission precedes FPGA mutation.

The runtime mounts and restores SRAM through the existing SNES virtual SD
backup interface before input starts. Clean Stop stops input, freezes the core,
snapshots SRAM and atomically replaces the save before programming idle.
A `save_failed` Stop leaves the session retained and frozen for Stop retry;
a complete captured snapshot is retained if file publication fails. Fix the
storage problem and retry Stop. Failed launch, startup and asynchronous fault
cleanup do not publish SRAM. Sudden power loss, process crashes and forced
reboot do not save current progress. This is software-tested behavior; hardware
save acceptance is pending. It adds no save states or host synchronization.

## Idle launcher display

Native Menu bring-up now enables the MiSTer HPS framebuffer for a 640×480
32-bit launcher surface scaled to the existing 1280×720 HDMI mode. The runtime
configures and validates the Linux framebuffer and owns the SPI enable sequence;
a launcher only writes pixels. Startup, Stop and failed-launch idle cleanup all
repeat this setup. A configuration/enable failure prevents idle publication.
This new display path is software-tested; exact-image hardware acceptance is
pending and does not inherit the previous Mega Drive acceptance.
