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

Package inspection/admission checks the exact format-2 descriptor, digest,
board, programming profile, ABI and required interfaces before programming.
Owned descriptors and retained file descriptors cross the mutation boundary;
payload and composition identities are rechecked immediately before use.
Only `fes-gp-v1` packages activate. The contained profile is diagnostic-only.

Replacement stops and neutralizes old input, quiesces HDMI and the outgoing
driver, then programs once. Ambiguous quiesce is not repeated. FPGA-manager
containment and control readback remain bounded; neither retained profile
releases the old MiSTer SDRAM bridges. A programming/activation fault receives
at most one defined-idle recovery. Failed recovery requires reboot.

Identity precedes video, input enablement and gameplay release. FES media
interfaces control reset-held startup and release after a successful commit.
`fes.simple-computer` also accepts mid-session `replace_live_media` /
`clear_media` on an active generation without holding execution reset; tape
loader busy rejects with retryable busy. See FES
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
the complete snapshot owned by native hardware. `Stop` from `reboot_required`
returns that retained error and does not call hardware again. `RecoverIdle`
retries `LoadIdle` from `reboot_required` only, or succeeds immediately when
already idle. It does not reboot the board. A running session is rejected so
save and input retirement stay on `Stop`. Failed `RecoverIdle` stays in
`reboot_required` and keeps package metadata that was already retained. No fault/startup cleanup saves.
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
