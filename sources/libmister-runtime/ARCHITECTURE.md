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

Package activation admission checks the exact sealed descriptor, digest,
board, programming profile, ABI and required interfaces before programming.
Owned descriptors and retained file descriptors cross the mutation boundary;
payload and composition identities are rechecked immediately before use.
Only `fes-gp-v1` packages activate. The contained profile is diagnostic-only.

Replacement stops and neutralizes old input, quiesces HDMI and the outgoing
driver, then programs once. Ambiguous quiesce is not repeated. FPGA-manager
containment and control readback remain bounded. Every program contains the
bridges before configuration. After user-mode readback, `fes-gp-v1` releases
the SDR FPGA ports (`0x3fff`), bridge reset (`0`), and L3 remap (`0x19`).
`development-contained-v1` stays contained, including splash, idle, and raw
development RBFs. A programming/activation fault receives
at most one defined-idle recovery. `Stop` does not program again from
`reboot_required`. `recover_idle` is the explicit second `LoadIdle`; if that
also fails, the state stays `reboot_required` and a board reboot is the
remaining recovery. A soft board reboot after FPGA or HPS activity can wedge
the HPS network; hard power is the recovery when `recover_idle` still fails.
The agent requests that board reboot only after a development session is
already `stopping` with `reboot_required`. A readable non-empty idle file is
programmed; only an open failure returns before HDMI quiesce and FPGA
program. See FES [soft-restart Path B](../../docs/soft-restart-path-b.md).

Identity precedes video, input enablement and gameplay release. FES media
interfaces control reset-held startup and release after a successful commit.
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

## Composable application ABI

`fes.application` 1.0 uses the existing `fes-gp-v1` lifecycle and GP transport,
with identity tag 3 and independent video, gamepad and media capabilities.
Admission requires fixed-720p60 video; known operational declarations must be
required, while absent interfaces impose no requirement. The Coleco firmware
overlay `fes.firmware.blob` 1.0 is the exception: a package may declare it
optional so BIOS-free titles share the same bitstream. Unknown optional
interfaces are ignored. Blob-stream requires blob. See
[application I/O](docs/application-io.md) for the complete runtime contract.

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
canonical ZX81 expansion-bus manifest grammar and verifies its manifest-derived expansion
ID, exact base package/BUILD_ID/payload binding, cart digest and linked payload
size/digest. The composition ID uses the shared `fes-composition-v1` domain and
base/expansion/payload identities. Descriptor and GP identity remain the base's;
only the FPGA programming artifact comes from the admitted composition.

The network-facing target agent owns deterministic CRAM recomposition using the
shared misteross implementation. Runtime admission verifies that agent-owned
result and retains its open FD, rehashing it and the expansion files immediately
before entering the existing physical transition. It does not implement a second
linker, download paths, or reopen checked pathnames during programming. Failed
admission leaves the current generation untouched. Successful activation carries
the composition tuple in active status, and the ordinary retirement/recovery
paths clear it together with package identity.

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
retain normal later cartridge/tape/disk delivery semantics.
