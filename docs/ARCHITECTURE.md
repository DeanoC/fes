# FogCast architecture

This is the canonical description of the working system.

## Normal FPGA game launch

```text
Browser UI, kit launcher, or `fogcast launch|status|stop`
  -> GET /api/v1/session, POST /api/v1/session/launch, POST /api/v1/session/stop
  -> host session service
  -> target /v2/cache and /v2/launch
  -> mister-agent
  -> transient MGL
  -> /dev/MiSTer_cmd: load_core <mgl>
  -> MiSTer/Main-compatible process
  -> FPGA core and game content
```

The important source entry points are:

- `internal/hostapi/server.go`: browser-facing session endpoints.
- `internal/mediasession/`: host selection and session lifecycle.
- `internal/systems/table.go`: platform, Main RBF selector, library mapping,
  aliases, and covers. Mega Drive expected core and cartridge file index
  come from mister-packages via `internal/systems/generated/`.
- `internal/httpapi/content.go`: target cache and cached-launch endpoints.
- `internal/agent/content.go`: target-side cached content launch.
- `internal/mister/runtime.go`: MGL creation, command dispatch, core
  observation, and stop.

The browser, kit launcher, and ordinary CLI send a game ID to the same
persistent host session. `hostclient` owns the UI-independent GET
`/api/v1/session` response model and decoder, and catalog launch
eligibility (`Game.LaunchEligible`) so `ListGames` variant selection and
sofa and kit catalog admission share one predicate. Browser display and launch
dispatch use the same catalog check, matched against the Go rule by
`hostclient/testdata/launch-eligibility.json`. Available state and explicit
`launchable` and `root_online` true are required; missing flags do not grant
eligibility. Catalog eligibility does not replace session or target readiness
gates, nor the host's authoritative launch validation. Tenfoot maps those block codes to
sofa copy. Artwork handles use `hostclient.NormalizeHandle` so host
transport, kit disk cache, and UI retain share one 64-hex rule.
Shared session decoding and Kit capability checks use
`hostclient.SessionCoreInterface.IsKeyboard` for exact `fes.keyboard` 1.0
recognition. Unsupported versions remain ineligible; input readiness,
generation and session ownership checks still apply separately.
Tenfoot and the kit launcher consume that package for session
polling; the browser keeps its own `parseSession` and shares the common
success/rejection matrix in `hostclient/testdata/session-contract.json`.
That fixture is not a claim of full decoder equivalence. Launch, stop,
and input attach/detach stay on their existing endpoints. The host
resolves it through the catalog and system table, uploads a cache miss,
and calls the target agent. The agent
writes the MGL atomically and sends `load_core <mgl>` to `/dev/MiSTer_cmd`.
FogCast waits for the expected value in `/tmp/CORENAME`. Stop uses the same
command path with `menu.rbf` and waits for `MENU`. Native FPGA Stop uses the
mutation (`upload_timeout_seconds`) deadline, not the short status request
timeout; programming idle can exceed a 5s health poll.

CLI launch and Stop preserve the caller context deadline without imposing an
additional HTTP timeout that could undercut the host's configured mutation
deadline. Interrupting the CLI cancels the request; it does not replay it or
prove that a target operation was undone. Status reads retain a two-minute
HTTP bound. CLI JSON includes the public input binding (`session_id`, `source`)
and complete typed metrics from `host.RemoteInputStatus`.

`POST /api/v1/session/launch` may include `target` to bind a live FPGA session
to a configured target without rewriting `selected_target`. Omitted `target`
uses the selected configured target. A second configured target may be
launched while the first is still playing; `GET /api/v1/sessions` lists those
live plays. `GET /api/v1/session` is the foreground session (the last launch)
and is what sofa and kit attach to for input. Stop of the foreground session
leaves the other target playing. One primary host input remains on the
foreground session; a second kit uses its local pad until surfaces attach by
session id. The kit lease remains the target-side ownership authority.

## Process ownership

The host owns the catalog, UI, user intent, content selection, and host-side
media. The target agent owns its HTTP API, cache, transient MGLs, and launch
requests. The MiSTer/Main-compatible process owns FPGA programming and the
MiSTer core services.

The target agent also exposes an authenticated, read-mostly diagnostic surface
on the existing kit listener: `GET /v1/kit/debug/events` returns the bounded
target event ring, and the lease-admitted
`POST /v1/kit/debug/snapshot-before-reboot` writes a diagnostic evidence
directory before an intentional reboot. The latter records the ring window and
bounded evidence for the journal, owner, `/tmp/CORENAME`, FPGA-manager state,
the native `/run/mister-runtime.events.json` dump when present, and a FAT-side
note. The native adapter drains that dump into the same ring; it does not invent
`flight_id`, `lease_gen`, or `run_id`. It reuses the current kit lease and does not create a
third process or bypass the existing runtime path. `flight_id` is the optional
canonical host UUID v4 from #205 and is retained only when a caller already has
one; `lease_gen` and `run_id` remain opaque join strings. The target never
invents host event schema or joins through the launcher listener.

These are simple process boundaries on a local, disposable development kit;
they are not a distributed ownership, failover, or recovery protocol.

### Source ownership

Appliance release manifests and immutable image storage form the independent
Go module `github.com/DeanoC/FogCast/appliance` in `appliance/`. The store lives
in `appliance/store`; both boot selection and target update handling consume
that single implementation. The module uses the standard library and the
existing `golang.org/x/sys/unix` dependency for filesystem operations.
The root module selects it through a checked-in relative replacement.
`make test` and `make vet` explicitly check both modules because root
`go test ./...` and `go vet ./...` do not traverse nested modules.
Target update admission remains in the root module; the fixed bootstrap and its
Linux boot implementation are owned by the FES appliance.

FogCast keeps the host applications, host services, and target agent in one Go
module, but the source tree names their ownership explicitly. The ten-foot sofa
app lives under `ui/tenfoot` and the kit launcher under `ui/kitlauncher`; their
Go package names remain `tenfoot` and `kitlauncher` for compatibility. Shared
drawing, input, theme, and library helpers live under `ui/shared`, `ui/anim`,
`ui/audioreact`, `ui/fbgrid`, `ui/gfx`, `ui/inputmap`, `ui/linuxinput`, and
`ui/theme`. `ui/kitlauncher` must not import `ui/tenfoot`. `host` and
`internal/hostapi` own host catalog/config/library services and host-owned
remote-input bridges. `targetclient` owns the authenticated host-to-target
HTTP/cache/core/development transport and endpoint reconciliation. UI packages
may consume host, target-client, and public protocol contracts, but do not own
target handlers, runtime lifecycle, image assembly, or FPGA builds. The FES
parent selects the FogCast revision and owns image integration and release
evidence.

The dependency direction is host/UI/`catalog`/`internal/hostapi` -> public
contracts and `targetclient`; the target executable -> target implementation
plus those same public contracts; the runtime remains the physical owner.
`protocol` is the wire schema. `corepackage` owns core-package descriptors.
`kitlease` owns kit-lease wire types. Those public contract packages do not
import FogCast `internal/` packages. `internal/agent`, `internal/httpapi`,
`internal/mister`, `internal/misterruntime`, `internal/input`,
`internal/targetcache`, `internal/applianceupdate`, and `internal/flightdiag`
remain target-owned implementation. Host, UI, `targetclient`, `catalog`, and
`internal/hostapi` must not import those packages. A later target module or
repository split is deferred until the measured target closure excludes
host/UI-only packages.

Target `GET /v1/health` may include an `artifacts` object: the SHA-256 of the
installed `/usr/share/mister-runtime/build-inputs` record, the runtime commit,
agent revision (stamped at `make build-agent`), agent and kit digests from that record, idle and catalog core digests, the
optional format-2 package id, and on an appliance boot the bootstrap ticket
`image_sha256`. The agent does not hash live binaries on each poll. Missing
fields mean there is no sealed record (for example a Main-backend image), not
that identity was rewritten. Host `GET /api/v1/health` adds a `host` identity
(`version`, `revision`, `os`, `arch`) and forwards `target.artifacts` when the
target is reachable. These identities describe provenance, not runtime
compatibility. FES retains exact component pins, native input locks, hashes and
release receipts for reproducibility and exact-artifact acceptance.

Connection compatibility uses the target's existing `api_version` contract:
the host supports exactly `v1`. Missing, malformed or unsupported versions
fail closed before target mutation; different agent or runtime Git revisions
alone do not block the connection. Target identity, authentication, readiness,
lease ownership and recovery checks remain separate requirements.

API compatibility is not a promise that every operation is available. The
native adapter negotiates its versioned runtime protocol; package inspection
checks the runtime's ABI registry before activation. Media, input and
persistence retain their operation-specific interface versions, limits and
package/generation checks. Unsupported operations remain rejected rather than
being inferred from a Git revision or silently retried through another path.
Breaking API semantics require a new supported contract, not an arbitrary
source revision comparison. Configuration is not rewritten to hide failures.

## Agent runtime backends

The target agent defaults to the existing Main runtime. Passing
`--runtime native` explicitly selects the separate native adapter, which uses
only `/run/mister-runtime.sock` and does not inspect the Main process,
`/dev/MiSTer_cmd`, or `/tmp/CORENAME`. There is no backend detection or
fallback.

The native adapter reports ready only when `mister-runtime` reports `idle`.
An idle Stop confirms that state without calling the runtime Stop operation.
The native adapter admits Mega Drive, ordinary SNES and NES cartridges, and the
registered ROM-less Pong profile.
Mega Drive validates an absolute staged ROM and sends one local request using
`/usr/share/mister-runtime/cores/megadrive.rbf` and media role `cartridge`.
Pong uses `/usr/share/mister-runtime/cores/pong.rbf` with `media: {}` and
`settings: {}`; supplied ROM paths and cached-content launches are rejected.
SNES uses `/usr/share/mister-runtime/cores/snes.rbf`, exactly one `cartridge`
path, and empty settings. FogCast checks the staged file path and extension;
the runtime validates cartridge bytes before hardware mutation and owns the
512-byte metadata prefix. It does not modify the host cache or content hash.
NES uses `/usr/share/mister-runtime/cores/nes.rbf`, exactly one `.nes`
`cartridge` path at native filetype index `0x40`, and empty settings. The runtime validates
iNES/NES2 headers, rejects trainers and truncated payloads, and streams source
bytes unchanged. The native SNES package index remains 1; legacy Main MGL
selectors remain zero based. Initial support is bounded ordinary LoROM/HiROM and
NES iNES/NES2 cartridges; enhancement chips, external firmware, expanded
mappings, FDS/UNIF/NSF and other peripherals remain outside this slice. Other systems remain
unsupported by this adapter. All admitted profiles reconcile lost responses
only against the requested system/core identity, without replay, and use the
ordinary Stop-to-idle lifecycle. SNES and NES are software-tested;
exact-artifact hardware acceptance is a separate integration step.
Mega Drive remains the hardware-tested native game. The separate development
operation accepts only the existing MiSTer-compatible
ABI and has no catalogue identity. The native adapter atomically stages one
bounded upload at `/tmp/fogcast-development/core.rbf`, dispatches it once to
the runtime, and resolves ambiguous responses through Status without replay.
Development Stop uses the ordinary native Stop-to-idle path; reboot recovery
is reserved for an actual native cleanup failure. A `fes.simple-computer`
package with `fes.keyboard` attaches remote input without `fes.gamepad`.
Host keyboard events map through the agent onto the runtime 40-bit ZX81
matrix (`set_keyboard`); Select+Start remains the software Stop chord. The
exact `fes.coleco` package with `fes.keyboard` 1.0 maps D-pad/left-stick
Up/Right/Down/Left to keyboard bits 0..3 and A/B to P1 Fire1 bit 4/Fire2
bit 10.
The exact `fes.sms` core with verified `fes.keyboard` 1.0 reuses this
mapping for P1 Up/Right/Down/Left and A/Fire1 (bits 0..4). SMS Fire2 is
unsupported: B remains an unmapped gamepad event and does not assert keyboard
Q/bit 10. Coleco's B mapping is unchanged.
The shared state-diff path joins keyboard, D-pad and axis holds, releases on
button-up or axis neutral, replays mapped state on transport reconnect, and
clears state on source close or Stop. Other core IDs and absent/unsupported
keyboard interfaces do not enable this mapping. This is software coverage,
not physical USB/controller or SMS hardware acceptance.
A host library entry for `fes.zx81` uses `load_library_core` like other
ROM-less FPGA cores; development `core-load` stays a separate volatile
activation and does not create that entry.
The separate `native-dev`
image packages this composition. Its idle, visible Sonic 2 launch, one-player
input, Stop, and immediate relaunch paths are hardware-tested on the designated
kit. Native development loading is hardware-tested only for the existing
MiSTer-compatible load/Stop lifecycle and subsequent game regression; exact
two-cycle evidence is in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md).
Raw development uploads still have no generic video or input guarantee.
After an explicit native session Stop has released the kit lease, a later raw
development load confirms that the target remains exactly idle and does not
submit another Stop; replacing an active native game still stops it to exact
idle before reading and uploading the development RBF.

The native agent creates one `FogCast Virtual Gamepad` during startup before
runtime reconciliation. Its Linux identity is `BUS_VIRTUAL`, vendor `0x0000`,
product `0x0001`, version `0x0001`; its capabilities are the one-player D-pad,
A/B/C/X/Y/L/R/Select/Start, signed X/Y axes, and synchronized event reports consumed by the
native runtime. Authenticated input leases only gate delivery to that retained
device: detach releases held state without destroying it, and agent shutdown
destroys it once. The Main backend keeps the existing per-lease input-device
path. Exact-image physical acceptance established playable D-pad and jump
input for the one-player Mega Drive slice; it does not establish six-button,
multiplayer, remapping, or hot-plug support.

Remote input retains Select wire code 107 and MD C code 108; X/Y/L/R append
codes 109/110/111/112. Linux events use BTN_X 307, BTN_Y 308, BTN_TL 310,
BTN_TR 311, and BTN_SELECT 314. Optional masks in the active runtime profile
determine which controls the core consumes. The same retained device spans
MD and SNES leases; no per-game virtual-device churn or input coordinator is
introduced. The host API exposes lease attach/detach/status; gamepad event
producers continue using the existing host RemoteInput event interface.

## Native Stop response loss

If the runtime Stop reply is lost while the operation context remains live,
`internal/misterruntime/runtime.go` makes one read-only Status request, bounded
by the existing health timeout and remaining operation lifetime. It never
replays Stop. A clean valid idle response confirms cleanup, allowing the agent
to clear active content and a later launch to proceed. Running, malformed or
retained-error idle responses do not prove successful Stop and remain unavailable.
An explicit retryable SNES save failure retains its mapped error; an explicit
reboot-required result retains the existing recovery marker. Normal Stop replies
and the existing game/development recovery policies are unchanged. Cancellation
of the operation owner also cancels observation.

This behavior has adapter, real Unix-socket dropped-response and coordinator
Stop/relaunch regression coverage. Dated diagnostic hardware validation is in
[the session recovery report](testing/session-recovery-2026-09-07.md).

## Failed native launch recovery

A failed native launch is observed once under the coordinator's exclusive
transition. A valid native idle status (including a retained launch error, but
excluding `save_failed`) permits clearing the durable active-content record.
Only successful cleanup publishes idle. The launch still returns its original
error, also retained in `last_error`; a subsequent successful launch clears it.
This observation uses the existing health timeout and operation-owner context.
It does not replay launch or issue an automatic Stop.

An unresolved native failed state remains unready, including failure to clear
the content record. Both game and development launch admission use coordinator
readiness, so callers cannot bypass that block. Explicit Stop can retry cleanup
and restore readiness. Legacy runtimes do not opt into native idle observation.

## Native SNES battery saves

The target coordinator passes the validated game ID in `PreparedLaunch` for
both direct and cached launches. The native adapter derives a save path under
`/media/fat/fogcast/saves/snes/<sha256-game-id>/<sha256-raw-rom>.srm` and checks
that its directory is writable before dispatch. This persistent FAT directory
is separate from the evictable ROM cache and the read-only image. Renaming or
re-uploading identical ROM bytes preserves a save; a different game ID or raw
ROM revision, including a copier-header variant, selects a separate save.

`internal/misterruntime/saves.go` owns that naming policy. The agent composition
sets `WithSaveRoot`; standalone adapters without the option keep volatile
behavior. The local runtime launch request adds optional top-level `save_path`
for SNES only. Cartridge media and settings are unchanged. Main, Mega Drive and
Pong keep their existing behavior. There is no new public save API.

The runtime owns cartridge-derived battery RAM sizing, save-file admission,
restore before input, and atomic snapshot persistence before idle programming.
Admitted cartridges without battery RAM produce no save file. Successful Stop
means the snapshot has been persisted; a `save_failed` response retains the
SNES session for Stop retry and becomes a visible agent error. The coordinator
cannot publish idle or permit successful lease cleanup until that retry
succeeds. The same Stop path covers user Stop, replacement, and lease cleanup.
After failed lease cleanup, ownership remains blocked under the existing
operator takeover/retry policy.

This supports clean Stop followed by switching or reboot. Unexpected power
loss, host/cloud synchronization, save states, and enhancement-chip saves are
outside scope. Focused tests cover identities, optional request validation,
error propagation and retryable lease cleanup; physical acceptance is recorded
by FES against its selected runtime and agent artifacts.

## Built-in Pong product

Opening the host registers game ID `pong` in the ordinary SQL catalog as
`source_kind: builtin`, with no relative path, ROM, content identity or source
fingerprint. `builtin-pong` is a reserved logical collection with URI
`builtin:pong`, excluded from filesystem scans and library retirement. It uses
existing game filters, pagination, favorites, presentation and session APIs;
no UI-specific launch coordinator is introduced. The native runtime is required.

`POST /api/v1/session/launch` with `{"game_id":"pong"}` follows the existing
host direct-launch path to target `/v1/launch` with empty `rom_path`. Only the
exact registered built-in can omit media; rooted games retain their existing
source/cache admission. Configured Pong library roots are rejected. The Main
backend explicitly rejects ROM-less profiles. Catalog presence alone does not
establish that a selected target image contains the required Pong RBF.
Native health advertises versioned `native_cores` availability from readable,
nonempty regular files at the adapter's existing legacy core paths. An explicit
empty list means no installed legacy cores; an absent field is an older or
different backend, not an empty list. This is operational availability, separate
from sealed artifact provenance and from described-package compatibility.
Native Prepare and Launch also reject missing core files before calling the
runtime, so a stale catalog cannot turn a missing file into a physical failure.
The host copies this observation per configured target and uses it for the
existing catalog `launchable` projection, including built-in Pong. Browsing
does not probe the target per row. Native launch admission refreshes the
observation for the session-bound target; an unavailable core is rejected
before dispatch. Explicit host-only execution and described-package admission
remain separate. Failed observations and malformed advertised versions do not
grant native eligibility; older peers without the field retain their existing
contract rather than being mistaken for a package-only image.

## Format-2 core package inspection and staging

`corepackage` is the shared, hardware-independent format-2 reader. It
inspects exact two-file directories or restricted uncompressed ustar archives,
validates the closed typed manifest and payload bytes, and computes package
identity from the original manifest and payload. Unknown but well-formed ABIs
remain inspectable; hardware compatibility belongs to the native runtime.
`corepackage.InspectPackage` returns the package ID and closed `Descriptor`
from the same pinned read for identity-reporting consumers such as
`core-inspect`; the smaller `Inspect` wrapper returns only the descriptor.

`corepackage.Stage` accepts a caller-bounded archive stream and publishes only
validated `manifest.toml` and `core.rbf` bytes into a distinct sealed directory
beneath an absolute private root. Cancellation or validation failure removes
the incomplete directory, including cancellation observed after rename and
before ownership handoff. The caller owns the returned directory lifetime and
must release it with `Staged.Cleanup`, which reopens and verifies the retained
root and publication identities before removing the sealed directory.

The target package lifecycle uses runtime protocol 2. A read-only
`inspect_package` exchange negotiates the exact ABI registry and programming
profiles before mutation; protocol 1 fallback is permitted only after its
explicit `unsupported_protocol` response and cannot activate custom packages.
`load_core` carries a rooted staged directory and package ID. The target retains
active and in-flight `Staged` ownership, reconciles a lost mutation reply by
observing identity plus a new generation, and retries failed cleanup only at a
safe lifecycle boundary. After staging, the adapter performs a read-only runtime
inspection and checks the exact package and descriptor before it enters the
target input replacement barrier. That barrier closes the old producer, waits
for in-flight sink writes, neutralizes the retained uinput device, and prevents
new streams or attachments until the mutation and any lost-reply observation
finish. Success or an ambiguous attempted mutation retires the old lease. A
proven pre-mutation failure reconstructs the same logical lease; failure to
pause or reconstruct it is a recovery failure and leaves input gated. Startup
adopts every still-valid publication through the same opened trusted root,
selecting only the package that exactly matches the active runtime status.
After an attempted activation failure, the target publishes idle only when the
runtime confirms exact operational idle and retains the structured failure in
that status. The host returns the original failure only when the observed idle
status has matching code, phase, expected, and observed evidence; any mismatch
or ambiguous transport result remains an unavailable recovery result. Confirmed
idle failure retires the old service execution, input, and media ownership; a
host-only executor must stop successfully before its ownership is retired.

The authenticated target route `POST /v1/development/core` accepts one bounded
`application/octet-stream` archive under the normal kit lease and update
exclusion. The host route `POST /api/v1/session/development-core` passes the
same mutation through the service and session coordinator. Rejected admission
leaves the prior media and input session intact. After confirmed activation,
the coordinator retires prior input and publishes the package ID, ABI, build
ID, active interfaces, generation, and derived gamepad capability in
`GET /api/v1/session`. Status reconstruction after a host restart attaches
input only for native games or active custom packages with `fes.gamepad` or
the exact `fes.keyboard` 1.0 interface; raw development RBF sessions remain
input-disabled. Manual input attachment uses the same predicate. The shared
gamepad-to-keyboard mapping is selected only for exact `fes.coleco` or
`fes.sms` together with that verified keyboard interface; SMS consumes only
the P1 directions and Fire1 subset described above.

The kit launcher opens a stream only for a nonempty input session that is ready
or reconnecting and is native or a capable custom development package. A
same-session reconnect keeps the stream identity and re-establishes the target
transport before accepting another source; capability, session, execution, or
generation changes close the old stream before another event can be sent.
`fogcast core-inspect PATH` validates locally without opening the FogCast
service. `fogcast core-load PATH` validates and streams an archive through the
running host-owned session and reports package/build identity or the public
failure phase. Its API origin precedence is `--api`, `FOGCAST_API`, then
`http://127.0.0.1:8787`. Inspection and upload derive from one bounded immutable
archive snapshot, and the command never opens or closes a target-owning
`Service`. It accepts success only when the returned active package has the
same package, ABI, and build identities, a positive generation, and a valid
descriptor-consistent active-interface set.

`fogcast launch <game-id>`, `fogcast status`, and `fogcast stop` use that same
running host session API rather than constructing a short-lived `Service`.
Launch is `POST /api/v1/session/launch` with `{"game_id":"..."}`; status is
`GET /api/v1/session`; stop is `POST /api/v1/session/stop`. Origin precedence
matches `core-load`. JSON output is the public host session object (`id`,
`game_id`, `state`, `execution`, `core_package`, `input`, and the other public
session fields). That replaces the earlier CLI launch `{status,content}`
cached-launch envelope. A failed request does not open a new service or repeat
the mutation. `fogcast health` and catalog commands (`scan`, `games`, `search`,
and the other local library commands) still open the injected host service;
agent health is not session readiness. The host service continues to call
target `POST /v2/launch` through `targetclient.LaunchContent`.

### Development media upload

`fogcast [--api ORIGIN] core-media PATH` snapshots one regular local file of
1..16384 bytes. It reads `GET /api/v1/session` from the running host and sends
the exact bytes to `POST /api/v1/session/development-media`. Like `core-load`,
API origin precedence is `--api`, `FOGCAST_API`, then `http://127.0.0.1:8787`.
The CLI never opens a target-owning service. File extensions do not select
behavior; the bytes are opaque to FogCast. No library association or durable
media record is created.

Both the host POST and target `POST /v1/development/media` require a fixed
`Content-Length` in that range, `Content-Type: application/octet-stream`,
`X-FogCast-Package-ID` (the current 64-character lowercase package digest), and
`X-FogCast-Core-Generation` (the current positive decimal generation). The host
also requires `X-FogCast-Session-ID`, copied from the current host session's
`id`, and `X-FogCast-Target`, copied from its `target` name. When the session
reports `target_id`, send that exact value as `X-FogCast-Target-ID` too.
The service checks both target name and target ID under lifecycle admission
and the target lock: switching targets cannot reuse a colliding package and
target-local generation. Generation values remain uint64 throughout, including in the CLI.
Chunked, empty, oversized, truncated and excess bodies are rejected. A host
restart, package replacement or generation change requires a fresh session read.

The service reuses lifecycle admission and the session's bound target. It
checks artifact compatibility and active package identity before upload. The
target requires the existing kit lease, rejects the hostless owner, and uses
the existing update exclusion and lifecycle serialization. This operation
never claims a new lease. Admission requires an active, error-free described
`fes.simple-computer` 1.0 package with active `fes.media.blob` 1.0; raw RBFs,
inactive cores, recovery states and stale identities are refused before
dispatch. Rejected admission preserves existing input and session ownership.

`internal/misterruntime/development_media.go` rechecks runtime identity before
staging and immediately before mutation. It creates a unique private directory
(0700), writes a regular `media.bin` (0600), and sends exactly
`{"protocol":2,"operation":"load_media","path":"<absolute staged file>"}`
over the existing local socket. The file is removed when the operation returns;
cleanup failure is reported. No network caller supplies a target filesystem
path. The target keeps its lifecycle admission and operation owner for the
bounded runtime call (15 seconds to accommodate the runtime's 10-second
deadline), even if the uploading connection disappears after admission.

Success preserves package and generation, input and host session identity.
The native adapter serializes keyboard posts with media transfer. Posts wait
while media is in flight, preventing runtime `BUSY` from closing the keyboard
stream. After successful media delivery on a keyboard-capable core, it restores
the last held-key matrix cleared by the hold/reset sequence before admitting
queued input events. That cache is bound to the package and generation in the
last successful keyboard response. A different generation or package restores
neutral, including after failed replacement neutralization. A transfer failure does not attempt keyboard restoration
through a potentially poisoned transport.
There is no automatic retry or replay: an unchanged package status cannot
prove media delivery after a lost reply. The coordinator observes runtime
state after a transfer failure. A failed transport can leave reset held and
report `reboot_required`; use the existing leased Stop/reboot recovery path.
Stop cannot bypass a poisoned GP transport by merely quiescing it. Physical
hold/reset, byte transfer and recovery remain runtime responsibilities.

For a separately started diagnostic host on port 8797, the operator sequence
is `fogcast --api http://127.0.0.1:8797 core-load PACKAGE.fcore`, then
`fogcast --api http://127.0.0.1:8797 core-media MEDIA.rom`. Starting that host,
deploying its matching agent/runtime, and hardware validation are separate
integration operations. The FogCast native input lock selects runtime
`8c4b690964ca2af06581e4cf11df22d33f48e1e0` for reproducible image assembly;
live compatibility is determined by protocol and operation contracts.

Focused tests cover admission, existing leases, update exclusion, lifecycle
serialization, staging permissions and exact bytes, generation changes,
cross-target package/generation collisions, keyboard events during transfer,
recovery status, Unix request shape and no replay. The end-to-end test runs
CLI → real host API/service → real target client/lease/HTTP/controller → native
adapter and Unix socket, with only the hardware daemon simulated. This is
host-only evidence, not Coleco hardware acceptance.

## Other modes

Host-emulator execution, remote input, capture, and host-to-target media are
existing optional modes. They share the host session UI but do not replace or
precede the direct FPGA launch path. Local session preview on Linux uses the
configured absolute V4L2 device path and the installed FFmpeg command to
produce H.264 frames for the existing MJPEG preview endpoint. macOS keeps its
native AVFoundation capture adapter; other platforms report capture as
unavailable.

A kit-only host runs `fogcast-api` with `--launcher-config` and does not open
the SDL sofa window. Pass `--headless` so that process does not compose local
capture or the MJPEG preview pipeline; kit catalog, attract, session, and
input stay on the launcher listener. Folder-watch still polls configured
library roots every thirty seconds, but it only re-opens a source when size or
mtime changed. Unchanged rows bump `seen_generation` and do not rebuild the
search index. A long scan waits a full interval before the next poll, so the
watcher cannot run back-to-back.

The kit reconnects with the existing `launcher.json` API URL on that launcher
listener. See [the host connection contract](launcher-host.md).

## Native 10-foot launcher

`cmd/fogcast-tenfoot` is an SDL3 host-side 10-foot launcher (cover grid, shelf,
and list). It is another client of the public host API, not a second launch
path:

```text
Native SDL3 UI
  -> GET /api/v1/platforms
  -> GET /api/v1/library/collections
  -> GET /api/v1/library/facets for genre and year lists
  -> GET /api/v1/games (grouped=1, availability=ready, optional collection/platform/sort/q/genre/year/region/hide_prerelease/hide_hacks)
  -> PUT or DELETE /api/v1/library/favorites/{id} for the focused title
  -> PUT or DELETE /api/v1/library/collections/{id}/{gameId} for custom-shelf membership
  -> PUT /api/v1/library/collections/{id}?name=... and DELETE /api/v1/library/collections/{id} for custom shelves
  -> GET /api/v1/presentation/artwork/{handle} from catalog cover handles
    and focused-title screenshot handles
  -> GET /api/v1/presentation/games/{id} for the focused title (studio,
    players, summary, screenshot_ids, video_id, year, genre, attribution)
  -> GET /api/v1/library/attract (idle video then stills; artwork via the same presentation artwork GET)
  -> GET /api/v1/library/settings and PATCH /api/v1/library/settings (idle seconds, preferred regions, selected target, library roots)
  -> POST /api/v1/session/launch (optional client_ts_utc / client_mono_ms JSON or X-FogCast-Client-* headers)
  -> POST /api/v1/session/development-rbf (raw octet-stream from a local path OSK)
  -> GET /api/v1/session (poll; now-playing or DIAGNOSTIC development chrome; additive flight_id)
  -> GET /api/v1/session/events?after= (poll; sofa event list; additive flight_id plus host/client clocks)
  -> POST /api/v1/debug/ui-events and GET /api/v1/debug/ui-events?after= (sofa/tenfoot focus/nav/launch/stop stamps; not a kit mutation)
  -> GET /api/v1/session/preview (optional MJPEG; 404/503/inactive is unavailable)
  -> POST /api/v1/session/stop (empty body or optional client stamp JSON; X-FogCast-Client-* headers)
  -> GET /api/v1/health (poll; kit chrome)
  -> GET /api/v1/status (503 TARGET_UNAVAILABLE treated as kit-down)
  -> GET /v1/kit/lease on the selected target address (status-only lease strip)
  -> POST /api/v1/session/input/attach and /detach (empty body; FPGA-native now-playing)
  -> POST /api/v1/session/input/event (play-session HID; fail-closed on a foreign kit lease)
  -> host session service
  -> existing FPGA launch path
```

`GET /api/v1/session/events` keeps the existing protocol 1 event object.
The host also sets an optional `flight_id` UUID on those events: one new id
per session launch, development-RBF or described-package load, and per
orphaned stop. Related events in that flight (launch through active through
stop of that session) repeat the same id. User stop of an active session
reuses the launch id. A failed launch leaves the previous id in place. The
field is omitted until a flight has been allocated. Every event also carries
host `ts_utc` and `mono_ms`. When tenfoot or the sofa browser stamps a
launch/stop, the matching event repeats `client_ts_utc` and `client_mono_ms`.
Invalid client clocks are ignored and do not fail the mutation. The current
`flight_id` is also additive on `GET /api/v1/session` and on launch/stop
responses. Focus and nav stamps, plus a copy of launch/stop actions, go to
`POST /api/v1/debug/ui-events` (`layer=ui`, kinds `ui.launch` / `ui.stop` /
`ui.focus` / `ui.nav`); fog-flight joins those rows to host and target events
by `flight_id` when it is present. Token-like detail keys are dropped.

TV overscan insets, sofa layout (`grid`, `shelf`, or `list`), the local
attract on/off gate, the look name, and the optional debug HUD are local to the tenfoot process (CLI `-safe-area` /
`-layout` / `-no-attract` / `-theme` / `-debug-hud` and optional `tenfoot.json` prefs). There is no host
safe-area or layout API. Host attract idle, preferred regions, selected target, library roots, and
targets use the existing public library settings endpoints. Tenfoot can add,
edit, and remove targets from the sofa settings overlay. Agent secrets are
write-only: GET exposes `agent_configured` only, the sofa never echoes a
stored agent, and PATCH sends `agent` only when the operator edited or
cleared it.

Attract prefers a playlist `video` handle when present. Darwin CGO builds
decode with AVFoundation (`ui/tenfoot/attractvideo`) after streaming
`Accept: video/*` to a temp file (128 MiB cap). Linux uses the same download
when `ffmpeg` is on PATH and decodes with the ffmpeg CLI; otherwise it skips
the download and falls back to stills. Non-CGO Darwin builds also skip video.
Short clips play through, then the
playlist advances or a single-item playlist restarts from the local file
without re-fetching; clips longer than 60s are capped at 60s. Missing, failed,
or unsupported video uses backdrop, else cover, else marquee. The gfx device re-uploads
the stage texture only when `FrameSeq` changes. Hide, dismiss, park, and
process stop tear down the decoder and close any player still queued.

While a host session is `active`, tenfoot may open `GET /api/v1/session/preview`
and CPU-decode JPEG parts from `multipart/x-mixed-replace; boundary=fogcast-frame`.
The sofa labels that surface **Preview**; it is not a living-room HDMI mirror.
404 (route absent), 503 `"session preview is inactive"`, kit/decoder down, and
transport errors are graceful misses and never block Launch or Stop. Park,
unpark, Stop, app close, attract entry, and GPU-using overlays cancel the HTTP
stream, close the reader, and drop the preview texture so no background
goroutine holds the stream.

Source entry points are `ui/tenfoot/` and `cmd/fogcast-tenfoot`. UI draw
helpers use `ui/gfx.Device` (begin/clear/present, RGBA8 textures,
textured quads, fill rects, CGO-free `DrawText` / `DrawTextWeight` with
embedded Go Regular and Go Bold, and
`DebugText` for the 8×8 HUD / FC2D opcode). Window, events, gamepad, mouse, and text input remain
SDL in `ui/tenfoot/sdl.go`. USB keyboard is first-class browse/nav on that
path (`CommandFromKey` in `ui/tenfoot/keyboard.go`): arrows, Enter, Esc, and
Tab drive shelf, detail, and search without a gamepad. USB mouse/pointer is
first-class on the same path (`PointerMove` / `PointerClick` in
`ui/tenfoot/pointer.go`): hover moves focus, primary click activates
select/launch/confirm on shelf, detail, and search, and it coexists with
keyboard nav. Hints and focus ownership follow the last-used keyboard, mouse,
or gamepad (`ui/tenfoot/affinity.go`). SDL keyboard, mouse, and gamepad
add/remove events claim affinity on plug without restarting the process, and
unplug restores a remaining device. Letter shortcuts already
patterned stay (`/` or `f` search, `o` settings, `g` filters, `l` layout). While
an attached play session is live (`ForwardsPlayHID`), USB keyboard events go to
`POST /api/v1/session/input/event` instead of the sofa focus graph and do not
steal browse or ZX81/session affinity; pointer browse stays off that session.
Esc and Backspace remain session-stop chrome. Letter `s` stays a core key.
A `fes.keyboard` core maps those keys onto the ZX81 matrix. For exact
`fes.coleco`, D-pad/left-stick and A/B events are mapped onto the Coleco P1
keyboard bits while overlapping keyboard, D-pad, and axis holds remain joined.
Exact `fes.sms` reuses that path for directions and A/Fire1 only; B does not
provide SMS Fire2.
Native SNES/MD
encode USB keys as gamepad buttons (codes 100–112) so the target mux does not
route them to `set_keyboard` and reconnect replay does not treat matrix codes
as axes. Foreign kit leases and `recovery-required` connections fail closed
and drop HID. On the kit,
USB keyboards join the play-session input stream with gamepads; `fes.keyboard`
packages are eligible without `fes.gamepad`. `TENFOOT_GFX` / `Options.GFX` / `-gfx` may select
`software`, `fpga`, or `fpga-stub` for tests; the production sofa path stays SDL3.
linuxfb is a kit framebuffer Device, not the SDL sofa shell.

| Backend | Construction | Role |
| --- | --- | --- |
| SDL3 | `gfx.WrapSDLRenderer` (`ui/gfx/sdl3.go`, build tag `sdl3`) | Default production path: wraps the process `SDL_Renderer` with letterbox logical presentation and VSync. |
| Software | `gfx.NewSoftware` (`ui/gfx/software.go`) | Pure-Go RGBA8 rasterizer for tests and CI (no cgo, no SDL). Nearest blit, `Snapshot` for golden pixels. Cover/screenshot/still downscale is Catmull–Rom at decode. |
| FPGA | `gfx.NewFPGA` (`ui/gfx/fpga_device.go`) | Records the versioned FC2D command stream (`ui/gfx/fpga_protocol.md`) and rasters through Software. `BackendName` is `fpga`. `IsStub` is true until a programmed 2D core exists; this slice has no mailbox/RBF and is not HDMI FPGA UI. Timed still/crossfade and sprite helpers live in `ui/anim`. |
| FPGA stub | `gfx.NewFPGAStub` (`ui/gfx/fpga.go`) | Thin Software wrapper without a command stream, kept as `fpga-stub`. `IsStub` is true. Does not talk to kit, runtime, or RBF. |
| linuxfb | `gfx.OpenLinuxFB` / `gfx.NewLinuxFB` (`ui/gfx/linuxfb.go`) | Software rasterizer whose `Present` blits RGBA8 to a 32bpp Linux framebuffer (`/dev/fb0`) with destination stride and BGRX byte order. CGO-free ARMv7 spike: `cmd/tenfoot-linuxfb-spike`, which reads evdev/joystick via `ui/linuxinput` and moves a cursor (Start/ESC/Q quit). Sibling `cmd/tenfoot-linuxfb-grid` paints a hardcoded cover-grid on the same Present + linuxinput path (highlight, confirm, quit; no catalog). Shared remap and multi-device merge live in `ui/inputmap`; linuxinput can apply a `Remapper` to gamepad records. Look tokens live in `ui/theme` and are consumed by `fbgrid.Paint` and the sofa `Clear` sites. Kit chrome uses typography roles `title_px` / `body_px` / `caption_px` / `status_px` through `Theme.TitlePx` and siblings; when a role is unset, `header_scale` / `label_scale` / `status_scale` still map to pixel size `8*scale`. Title and chrome header use Go Bold when `title_bold` / `header_bold` are set (built-ins default true); body, caption, and status stay Regular. `DebugText` stays the FPGA/debug path. |

`gfx.Recorder` remains a call-order test double and does not draw pixels.
`gfx.Replay` / `ReplayBytes` apply a decoded FC2D stream to any Device.

Tenfoot looks are data-driven. `ui/theme` loads colour, spacing,
typography roles, cover-chrome, vignette, bezel, and cabinet tokens from a built-in name (`default`,
`arcade`, `night`) or a JSON/TOML file. Roles are explicit pixel sizes
(`title_px`, `body_px`, `caption_px`, `status_px`). Paint calls `TitlePx`,
`BodyPx`, `CaptionPx`, and `StatusPx` so fallback math stays in the theme
package: an unset role uses `gfx.ScalePx` of `header_scale` / `label_scale` /
`status_scale` (the former 8× DebugText hierarchy). Built-in `default` and
`night` use 20/13/12/14 on a 640×480 grid; `arcade` uses 22/13/12/15.
`default` still preserves the sofa/attract clear colours. Built-in themes
mark `title_bold` (and chrome `header_bold`) true so grid headers and detail
titles raster with embedded Go Bold; body/caption/status stay Regular unless
the matching `*_bold` token is set. Incomplete files inherit those defaults.
`fogcast-kit` and
`fogcast-tenfoot` share `theme.Resolve` (`-theme`, then `launcher.json` /
`tenfoot.json` `theme`, then `FOGCAST_THEME`). There is no scripted theme VM
or font-family picker; derived colours stay in Go.

The browser shell remains the default UI. Mac is the primary sofa target; Linux builds
with the same `make build-fogcast-tenfoot` target (`CGO_ENABLED=1` and
pkg-config `sdl3`). Build and run notes are in
[native-tenfoot-launcher/README.md](native-tenfoot-launcher/README.md).

Tenfoot can load a development RBF from a gamepad path OSK (type or paste a
local file path; no browser file picker and no host file-list API). That POST
is the same public `application/octet-stream` session endpoint. Sofa chrome
labels the result DIAGNOSTIC: HDMI and input may be down, and it is not a
playable game session. Stop uses the ordinary session Stop-to-idle path.

## FES appliance releases

FES owns compatible source selection, release/media assembly, and the fixed
bootstrap. FogCast supplies the public appliance module, target update API, and
`cmd/fes-update`. Online releases replace
only a content-addressed read-only ext4 system image. The locked kernel, U-Boot,
and fixed `/linux/linux.img` bootstrap stay outside that operation. Configurations,
target identity, ROM cache, launcher catalog/cover cache, and SNES saves remain
on FAT outside every rootfs. Replacing the system image does not wipe
`/media/fat/fogcast/cache` or `/media/fat/fogcast/launcher-cache`.

When leftover card capacity has already been formatted as partition 3 with
label `FESDATA3`, agent startup bind-mounts that ext4 volume over
`/media/fat/fogcast/{cache,saves,core-data,launcher-cache,evidence}`. Existing
files are copied onto p3 first and never overwritten there. Releases,
`agent.toml`, `launcher.json`, `target-id`, and known-good images stay on the
1 GiB FAT. The helper does not create, grow, or format partitions and does not
run from the fixed bootstrap. Bind is refused while `GET /v1/update` shows trial,
pending, or corrupt; the agent still starts and keeps those trees on FAT.

The kernel loop-mounts the bootstrap as before. Its PID 1 verifies the selected
image, consumes a pending trial durably, attaches another read-only loop, and uses
`pivot_root` followed by exec of the selected `/sbin/init`. The old bootstrap
remains at `/.fes-bootstrap`. Preparation failures select verified known-good or
factory; failures after starting root switching require reboot. No trial is
repeated on a later boot without another explicit activation.

For a trial, an independent bootstrap process first verifies the ARM DE10-nano
device tree and prepares Cyclone V warm reset: it marks the completed preloader
valid and disables the retained-OCRAM boot enabled by the locked U-Boot. Ordered
32-bit SYSMGR writes and matching readback are required before opening the
watchdog, so a reset reloads the same valid SD preloader instead of retained RAM
or the next preloader copy. This does not confirm the appliance trial. The process
then opens the DesignWare hardware watchdog and acknowledges arming before
candidate init executes. Its separate
mount namespace retains the bootstrap and FAT views through pivot. Its deadline
is 180 seconds. Only a durably synced confirmation matching the actual boot ID
and selected image permits magic-close. Failure, deadline, or process death
leaves reset armed. Known-good boots do not require the host to be online.

`appliance/store` owns bounded raw-image admission, immutable publication,
cross-process locking, checksummed state, and consumed-trial selection.
`internal/appliancedata` owns the optional FESDATA3 bind at agent startup.
The FES bootstrap owns fallback ordering, Linux mounts, loops, and the
watchdog. The bootstrap records its selected image in
`/media/fat/fogcast/releases/boot.json`; the native agent accepts that ticket only
for the current kernel boot ID and retained factory manifest.

`internal/applianceupdate` uses the existing kit lease and coordinator transition.
Activation drains launch/development/input/cast requests, stops the runtime so
battery saves persist, neutralizes peripherals, records pending selection, flushes
the HTTP response and requests reboot. During an unconfirmed trial these normal
operations are blocked; status, Stop, lease management and confirmation remain
available. A failed response/reboot leaves pending visible and releases admission
on the current known-good image. Confirmation requires actual native idle.

The host uploads once and activates once. It resolves response loss by reading
authenticated boot/image identity, rediscovers a changed address using the existing
target ID, waits for startup lease cleanup, obtains a new lease and confirms only
the expected trial. The operator client does not add a UI or take over another
owner. The [operator guide](appliance-updates.md) describes routes and commands.

Host tests and a real isolated ext4-to-ext4 root-switch test cover this composition.
They do not establish physical watchdog reset or exact-image kit acceptance.

## Target image

Native Buildroot and rootfs assembly live in the FES `image/` recipe.
FogCast supplies the agent, kit, extra-core selector (`cmd/target-image-lock`,
`internal/targetimage`) and `build/native-runtime.inputs.lock.toml`.
FES produces:

- `image/build/output/target-image/native-dev/linux.img`: the reproducible native
  runtime candidate image.

The working `dev` and `prod` targets boot `/media/fat/linux/linux.img`, start
the MiSTer/Main process, and then start the FAT-side FogCast agent from
`/media/fat/fogcast`. Before Main starts, the image-owned legacy boot service
atomically enforces `osd_timeout=0` and `video_off=0` in the persistent
`/media/fat/MiSTer.ini`, preserving unrelated settings and one copy of the
pre-FogCast file. This keeps the Menu HDMI output visible during unattended
capture and is idempotent across reboots. They remain the game and
development-RBF path.

The `native-dev` image instead starts image-owned `mister-runtime` and
then image-owned `mister-agent --runtime native`. It contains exactly one
locked idle RBF and one selected Mega Drive RBF under `/usr/share/mister-runtime`,
plus the explicitly selected sealed Pong, SNES and NES RBFs when the four-system
profile is requested. FES may additionally supply closed format-2
package/selection pairs for selected cores from `fes.pong`, `fes.zx81`, and
`fes.coleco`. The image selector validates and copies only `manifest.toml` and
`core.rbf` for each pair, installs them beneath their exact package IDs, and
retains the external producer/package selections beside the image,
has no Main startup or legacy Menu-configuration helper, has no
`/dev/MiSTer_cmd` wait, and retains the same read-only root with volatile
`/run`, `/tmp`, and `/var/log`. Its build-input record identifies the runtime
commit, agent binary, idle RBF, and selected Mega Drive RBF provenance. For each
format-2 package selected, the record also identifies the exact selection
digest, package and payload IDs, producer/schema revisions, and install path.
The verifier reconstructs that projection from the installed package and
external selection; it does not infer selection from cache or image contents.
The per-filesystem Buildroot copy retains the installed 0555 package directory
and 0444 member modes through image creation, then an external rootfs hook makes
only the copied directories removable when the fakeroot command exits. Before
reusing a retained Buildroot output, the image builder applies that same bounded
directory-only cleanup to its exact `target` copy. Image verification preserves
the sealed modes while checking them, then inode-binds its disposable extraction
root, rejects symlinked or mismatched package entries, makes only extracted
directories writable, and removes the tree without hiding verification or
cleanup failures.
Its QEMU
smoke proves only root filesystem and init packaging; it does not emulate FPGA
programming, prove target readiness, or establish game or development-RBF
support. The designated-kit idle, Mega Drive launch/input/Stop/relaunch,
and legacy rollback gates are
hardware-tested. The image contains no development RBF at either production
path and relies on volatile `/tmp` staging for an admitted upload. Native
development-RBF loading is hardware-tested for the existing MiSTer-compatible
lifecycle. Exact hashes and dated physical observations are in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md);
the game-only baseline remains in
[native-megadrive-baseline.md](hardware/native-megadrive-baseline.md).

### Native Mega Drive RBF selection

Source-built Mega Drive selection is the native image default; use the explicit upstream selection for fallback.

The FES native image recipe resolves a sealed `megadrive.rbf` plus its normalized
selection record before Buildroot. The selected record carries the origin,
MiSTer ABI, `megadrive` system, repository revision, artifact identity, exact
size/SHA-256, role install path, and source-built recipe/toolchain fields when
applicable. The runtime receives the same role path for either origin.

- `make build MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle`
- `make build MEGADRIVE_RBF_SOURCE=upstream`

There is no automatic fallback between the two RBF selections. A malformed or
missing source-built bundle stops the build; it cannot reuse the upstream
cache. Both selections use the MiSTer ABI; generalized/custom/non-MiSTer RBF ABI support is deferred.

This selection/provenance path is software-tested. The existing hardware
baseline covers its previously accepted image and does not by itself qualify a
new source-built RBF.

## Milestone status

```text
legacy dev/prod = current game-capable path
native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch
Milestone 2 = complete
Milestone 3 = complete for the defined one-player Mega Drive vertical slice
native development RBF = hardware-tested MiSTer-compatible load/Stop/game-regression path
Milestone 4 = complete for the defined MiSTer-compatible development lifecycle
```

## Development RBF extension

```text
Host tool
  -> POST /api/v1/session/development-rbf (raw RBF)
  -> host session service
  -> POST /v1/development/rbf (raw RBF)
  -> mister-agent atomically installs /tmp/fogcast-development/core.rbf
  -> selected backend
       Main: /dev/MiSTer_cmd load_core /tmp/fogcast-development/core.rbf
       native: /run/mister-runtime.sock load_development_rbf
         -> HDMI power-down
         -> program once and synchronize
  -> development FPGA image
```

This path intentionally has no catalog entry, MGL, game identity, manifest,
rollback store, or second programmer. The public session reports
`execution: fpga_development` with no game or system.

For the native backend, successful programming reports
`running_development` with execution `development`, a null system, and an
optional observed core name. HDMI remains powered down throughout development;
the raw upload has no video or input guarantee. Stop reloads the locked idle
RBF through the normal native lifecycle. The upload is never packaged into the
image or replayed after an ambiguous response. The Mega Drive RBF is selected
at image-build time as described above. There is no browser file picker and no
generalized RBF ABI.

If a native upload reaches the mutation boundary but cannot recover to idle,
the target preserves `stopping + development + recovery: reboot_required` and
the upload reports `MISTER_UNAVAILABLE`. A later Stop reuses that marker without
another FPGA operation so the host can retry the existing reboot handshake.

Non-MiSTer development images, including the `misteross` blinky and mailbox
experiments, do not implement the GPI signature expected by the current
MiSTer/Main process. Main exits after loading them and cannot reload Menu from
that FPGA state. Development Stop therefore uses an explicit recovery
handshake:

1. `POST /v1/stop` returns `recovery: reboot_required` without rebooting.
2. The host records the target's Linux boot ID, then sends
   `POST /v1/development/reboot`.
3. The host waits for target health to report a different boot ID and an idle
   status after boot. This proves the reboot even when polling does not sample
   the brief disconnect.
4. Only then does the public Stop return an idle session.

Normal game Stop is unchanged and continues to load `menu.rbf` through
`/dev/MiSTer_cmd` without rebooting. The relevant source entry points are
`internal/hostapi/session.go`, `fogcast/service.go`,
`targetclient/development_client.go`, `internal/httpapi/development.go`,
`internal/agent/coordinator.go`, and `internal/mister/runtime.go`.

The exact reproducible native image passed the designated two-cycle
development-to-idle-to-game acceptance and the legacy rollback gate. This is
a narrow hardware capability for the existing MiSTer-compatible development
ABI; it does not imply useful video or input for other RBFs.

### Target kit ownership

The production target agent enforces a renewable kit lease across game and
native development sessions. Authenticated clients read `GET /v1/kit/lease`,
claim with `POST /v1/kit/claim` (`request_id`, `owner`, `purpose`), and carry the
returned secret in `X-FogCast-Kit-Lease` on every hardware mutation and input
CONNECT. Request IDs are random hexadecimal strings of at least 32 characters;
retries reuse the same ID. Cache transfer, status inspection, and
`GET /v2/hostless/identity/{game_id}` do not reserve the kit. A Stop
ends the current runtime session but retains ownership for another launch.
The host session service owns lifecycle mutations; the kit launcher never
creates a second hostless owner when the configured host is unavailable.

Renew and release use empty POST bodies at `/v1/kit/renew` and
`/v1/kit/release`. The production lease lasts 90 seconds; active clients renew
before expiry. Expiry closes input streams and interrupts incomplete uploads,
then waits for admitted operations and invokes input neutralization, cast Stop,
and runtime Stop. Ownership becomes available only after successful cleanup;
failed recovery leaves it blocked. Agent startup performs the same cleanup.
The retained uinput device survives lease release.

An operator using the configured bearer credential may explicitly request
`POST /v1/kit/takeover` with `request_id`, `owner`, `purpose`,
`expected_generation`, and a nonempty `reason`. Takeover revokes the previous
lease through the same cleanup path; it never aborts FPGA programming halfway
through a transition. Clients retry a busy takeover using the same request ID.
Old lease credentials cannot stop or send input to the replacement owner.
This protects agent API operations; direct root SSH or runtime socket access
remains a maintenance escape outside the lease boundary.

## Host kit lease

The production service owns a renewable target kit lease, shared explicitly
with its game/development client and target input bridge (including CONNECT).
The first hardware mutation claims ownership; renewal runs every 20 seconds
against the target's 90-second timeout. Client expiry uses the returned
remaining duration and local monotonic time, so a kit without an RTC works;
request round-trip time counts against that duration. Status and cache transfers do not claim
hardware. Stop, input detach and reboot require an existing grant and never
claim someone else's active session. Replacement operations retain the grant.
Explicit public Stop releases its grant after input/media/hardware cleanup;
replacement Stop retains ownership for the next launch. Application shutdown
releases its grants after input/session cleanup.
Shutdown cleanup first checks local ownership: it invokes Service.Stop only for
an active host-only session or a foreground target with a held grant. Clean
idle after explicit Stop and never-owned idle skip the target Stop, while a
lost or foreign target grant fails closed. Once admitted, shutdown uses the
existing Stop timeout, retry, recovery, and error-propagation behavior.

A renewal error or expired grant invalidates local ownership and stops renewal.
The host does not automatically take over or fall back to an unguarded target
when lease endpoints are unavailable. An operator must inspect ownership and
start a fresh application session after lease loss. The target owns timeout,
revocation and serialized physical cleanup; host lease tokens stay in memory.

## Target identity and reconnection

Each named host target may have a persistent `target_id`, a lowercase UUID copied
into the target's private agent configuration during media provisioning. Legacy
`base_url`/`token` configurations may also carry a top-level `target_id`. Target
settings expose the ID without exposing the bearer credential. An explicit
`PATCH /api/v1/library/settings` with `{"prepare_target":"dev"}` creates and
atomically saves an ID only when that named target has none. Normal settings
updates and target renames preserve it. Preparing an ID requires a writable
private canonical config. The existing browser target settings provide this action.

The agent uses the configured identity, or persists one in
`/media/fat/fogcast/target-id` when no identity is configured. Authenticated
health reports it; anonymous health omits it. Advertisement runs independently
of HTTP startup, waits for an addressed multicast interface, and recreates its
listeners when interfaces or addresses change. Failed setup retries with capped
backoff. This handles the kit starting its agent before Ethernet is ready.
DNS-SD TXT contains only `target_id` and discovery protocol version. A random
service instance and hostname distinguish cloned identities on the same link;
the persistent TXT identity remains stable across reboots.

The host authenticates health at its configured or last validated endpoint. A
legacy address-only target can bind a discovery-capable agent's existing ID
through the same private write path. A bound target may resolve matching
`_fogcast._tcp.local.` DNS-SD announcements after a read-only connection failure.
Health must confirm the expected ID and API version before endpoint adoption;
HTTP redirects are rejected. One DNS-SD instance may publish multiple addresses,
but multiple distinct matching instances/endpoints are ambiguous and are not
chosen automatically. Discovered DHCP addresses stay in memory.

The service's existing lifecycle admission serializes validation with launch,
Stop and development operations. A one-second host monitor provides retry
opportunities; failed lookups back off for 1, 2, 4, 8 and then 15 seconds.
Individual health probes and multicast browse windows are bounded and cancellable.
Browse is stopped with cancel when its deadline expires, so the DNS-SD packet readers exit.
Settings changes cancel an in-progress lookup, and shutdown cancels and joins the
monitor before releasing leases. Explicit development reboot recovery uses the
same read-only validation while retaining its existing lifecycle admission.

Endpoint adoption reads public lease ownership and runtime status before reporting
ready. The shared host lease object moves game requests, lease renewal, input
attach and input CONNECT to the validated endpoint. On an unchanged boot, a
locally held grant is retained only when its live generation still matches the
agent. An address change closes the old local input stream without replaying held
input; the user can attach input again through the existing session action.
On a changed boot the host forgets its old grant and session and closes local input handles without remote Stop, release, or input cleanup. A new user
launch may claim a free lease; discovery never claims or takes over a kit, and
never replays a launch, upload, Stop, reboot, or input operation.

The existing public health response includes `target.connection`; status responses
include `connection`, including unavailable responses. Its states are
`disconnected`, `connecting`, `ready`, `active`, `busy`, `version_mismatch`
and `recovery-required`,
separate from runtime/game state. Busy responses include the public owner label.
The browser and tenfoot target views show this state. Manual addresses remain
usable where multicast is unavailable. This path has host/fake-peer regression
coverage; physical reboot and DHCP acceptance belongs to the selected FES image.

## Native kit launcher

The native image packages `fogcast-kit`, a CGO-free controller/session adapter
with a living-room platform wheel and a live catalog browse renderer.
`fogcast-kit` writes a last-good catalog snapshot and cover blobs under
`/media/fat/fogcast/launcher-cache/` (beside `launcher.json`, separate from the
ROM cache). Catalog publish stays atomic (`catalog.json` temp+rename); a host
refresh merges by identity so an unchanged list does not blank the shelf or
rewrite FAT. Cover files have a 512 MiB LRU cap on that tree and never call
into `targetcache`. Artwork and presentation prefetch is focus → visible page →
next page → strip → attract, still capped at three concurrent fetches.
`GET /api/v1/library/cache` (host and launcher listener) reports ROM cache
used/free/max from lease-free target `GET /v2/cache`; cover used/free and last
catalog sync are kit-local `DiskStore.Status()`. Games may include `rom_cached`
when the target inventory is reachable; ROM-less rows omit it. Boot paints that
shelf from disk before host games HTTP, decodes
visible covers from disk first, and labels an absent host `Offline - local library`.
Local D-pad/A still browse that snapshot. When the host is unreachable, launch
and Stop remain unavailable until the configured host API reconnects. The kit
launcher does not claim a target lease or call `/v2/launch` directly; lifecycle
mutations continue through the persistent host session API. Cache and artwork
browse state remains local and lease-free.
Kit launch admission uses full catalog state for grid, detail, strip, and
attract entries. An attract-only item without a known catalog row can still
be displayed and dismissed, but cannot launch: its platform-support flag
alone does not establish source availability. Catalog refreshes retain state
and root-online changes even when titles and artwork are unchanged.
The wheel is the top-level browse view: a horizontal clear-logo / wordmark
strip plus a hero for the focused system. Catalog rows are grouped into system
shelves (`All` plus each system present in the loaded games, typically pong,
Mega Drive, and SNES). On the wheel, D-pad, left stick, shoulder L/R, and
Select cycle platforms; A/South enters the filtered browse view for that system
(default 4×3 grid). East/B on browse returns to the wheel and closes search. Start opens catalog search on the current
shelf (from the wheel it enters that system's browse first) with the existing
gamepad OSK; Y (North) on browse
cycles Grid → Coverflow → Wall → Split → Grid; it is ignored on the wheel, title pane,
attract, and search OSK (any pad input still dismisses attract). That Y chord is the
layout switch; X (West) still cycles theme
packs Classic → Neon → Sofa Dim → Classic on the wheel, browse, strip, and
title pane; attract still dismisses on X like any pad input, and X is ignored
while the search OSK is open. Search filters the loaded shelf by a case-insensitive
title substring (clear-logo wordmark / system id when the title is empty). An empty
query restores the shelf; no matches hide tiles (including the recent strip) and paint `No matches`.
A committed query keeps the recent strip hidden so Down stays on the filtered shelf.
After Done or Start commits the OSK, D-pad and A/B match browse on the filtered results.
Closing search restores the prior focus when that title is still visible. Select
still cycles shelves; Select+Start still stops. The last pack is
stored in `launcher.json` `theme` so a kit restart (and a host reconnect of
the same process) keeps it. Coverflow is a scaled
focus row of five titles; wall is a denser 6×3 mosaic; split is a vertical
clear-logo (or title) list with a focused cover and short meta. In browse, shoulder L/R and Select
still cycle shelves as a secondary filter; the themed header shows
`FOGCAST  MEGADRIVE 12/40`, plus `SEARCH` when a query is filtering the shelf,
`FLOW`, `WALL`, or `SPLIT` when that layout is active,
and `NEON` or `DIM` when that pack is active. Classic stays untagged. The hero paints an attract still, presentation
`backdrop_artwork_id`, or representative cover when a handle exists, otherwise
a theme-tinted placeholder, with game-count chrome plus a play-count and
last-played rollup when host games already carry `play_count` /
`last_played_at` (recents order is the fallback last-played). Wheel cells use a representative
`logo_id` when presentation has one, else a bold wordmark. D-pad
and left-stick focus in the grid and wall moves in two
dimensions through `fbgrid.MoveFocus`: left/right clamp on the current row,
up/down step by the layout column count (4 on the grid, 6 on the wall), and
leaving a page changes the painted window. Coverflow uses one row of the
whole shelf so left/right walk titles and down opens the strip or title pane.
Split uses one column so up/down walk titles, left/right clamp, and last-item
down opens the strip or title pane.
Focus changes play a short `anim.Tween` / `EaseInOut` pop (highlight ring
scale ~1.06 over ~160ms); confirm eases a white pulse out over
`ConfirmFrames` ticks. Unfocused cells keep their layout origins. The kit
paints dimmed presentation `backdrop_artwork_id` (or an attract backdrop)
cover-fill behind the wheel, browse layouts, strip, and title pane when that
handle decodes; otherwise a cover-wall of visible decoded covers; otherwise
the solid theme background. Atmosphere is paint-only. Theme tokens
`vignette` / `vignette_alpha` paint a soft stage-edge darken on the wheel,
browse layouts, and title pane; `bezel` / `bezel_width` paint an optional
thin frame (Neon and Sofa Dim use 2px; Classic stays 0). A live host
session (`active` / `launching` / `stopping` / `failed`) dims that layer
and paints pause chrome (`Paused` plus Select+Start) without stealing
East/B, Start, or Guide. The kit
also loads `GET /api/v1/games` with `collection=recents` and
`collection=favorites` (best-effort; a miss hides the row) and paints a
single horizontal strip under browse when at least one title exists.
Last-row Down enters that strip; L/R move among tiles; A opens the title
pane; B or Up return to browse. Down that
cannot move focus further (last catalog row) opens a focused title pane
through `fbgrid.PaintDetail` when the strip is hidden (large cover, title at `TitlePx`, meta from
catalog plus `GET /api/v1/presentation/games/{id}` when the pane is open).
Admitted facts are platform, year, genre, studio, players, and region when
those fields are present; `summary` wraps as caption-role description and is
omitted when empty. Compact chips paint on browse tiles and the title pane
for players, rating, completion, and portable when presentation (or handheld
catalog system identity) already carries them; empty chips stay hidden rather
than inventing rating or completion. Play-count and last-played stay off the
pane body; the platform-wheel hero rolls them up from the games payload.
When `video_id` is present (library_media overlay on the same presentation
payload), the pane paints an honest motion preview: it auto-cycles
`screenshot_ids` then unique backdrop/cover posters under a VIDEO badge and
a `preview` caption. The CGO-free kit binary does not decode H.264; titles
without a video handle keep the still screenshot carousel.
Presentation `marquee_id` (LaunchBox Arcade-Marquee or Banner, with
`library_media` RoleMarquee winning when present) paints a wide strip under
the header; a missing handle hides the strip. Cover, meta, badges, and the
screenshot or video-preview slot keep their existing layout.
A/South still launches from browse. The pane's A plays the
focused title, East/B and Up return to the same shelf and focus, and
shoulder or D-pad L/R cycle `screenshot_ids` (or preview stills) when two or more are present.
When presentation `series`, `related` / `related_ids`, or `collection` names
at least one other catalog title, a Series strip paints at the bottom of the
pane (related IDs and collection membership first, then an in-catalog filter
by the admitted series string). The row hides when no sibling exists. Down enters that strip;
L/R move among tiles; A opens that title's pane (switching shelf when needed);
B or Up return to the same title. Y and X stay layout/theme and do not steal
the chord. Split Right enters the same mates in the hero when they exist;
Left/B return to the list. The short split meta line still omits series.
Meaningful scene cuts (detail open/close, attract show/hide, wheel
enter/leave, Y layout, X pack, search OSK open/close) paint a short CGO-free overlay from the
theme `transition` token through `ui/anim`: Classic a curtain,
Neon a glitch/static burst, Sofa Dim a wipe. Overlays settle in under
400ms and do not block pad input. `transition` `none`, `-no-transition`,
or `FOGCAST_NO_TRANSITION=1` is an honest no-op. Attract does not arm while the pane or search OSK is open. Catalog cells paint decoded box-art from
`GET /api/v1/presentation/artwork/{handle}` when a catalog `Game.Cover` or a
presentation `cover_artwork_id` is present. The focused browse tile, split hero,
and title-detail cover prefer presentation `box3d_id` (LaunchBox Box-3D, then
Cart-3D, then Box-Spine, with a `library_media` RoleBox3D overlay that wins
when present). Missing 3D art uses a cheap CPU 3/4 perspective of the 2D cover
(`ui/shared.FauxBox`); missing both keeps the placeholder. Unfocused tiles stay
2D covers. Theme tokens `cabinet` / `cabinet_width` paint a thin hardware bezel
around that focused art (Neon and Sofa Dim set a width; Classic stays 0).
Presentation `logo_id` (LaunchBox
Clear Logo, or a `library_media` RoleLogo overlay that wins when present)
paints on the detail title, grid label bar, and split list rows; tiles and titles without a
logo keep the existing text labels. Split paints the focused cover and
admitted short meta (platform, year, genre, studio, players, region) in the
right column; it omits summary, last-played, and play-count there. Series mates
paint as a small hero strip when at least one sibling is in catalog.
The kit prefetches
`GET /api/v1/presentation/games/{id}` for the visible browse page and a cheap
next window without blocking present; missing or failed lookups keep the placeholder.
`DecodeCover` Catmull–Rom downscales once to the cover cell so Software Draw
stays a cheap nearest blit. Missing or still-loading art paints a theme-tinted
placeholder (lettermark when missing; a distinct panel while loading) instead
of a flat system fill. After host `idle_seconds` from
`GET /api/v1/library/attract` with no pad input, the kit paints attract through
`DecodeStill` and `fbgrid.PaintAttract`. Video-only rows stay dropped because
the CGO-free kit binary does not decode H.264. A distinct attract `marquee` or
presentation `marquee_id` paints a banner strip under the header alongside the
still, motion preview, or 2×2 wall; a marquee-only row keeps the still
fallback and hides the duplicate strip. When a staged row has a video
handle plus stills (item backdrop/cover/marquee, or presentation
`screenshot_ids` when that payload is fetched), attract auto-cycles those
stills under a VIDEO badge and a `preview` caption — the same honest motion
preview as the title pane. Four or more stills-backed rows with at least one
video handle paint a 2×2 wall of neighboring stills with the staged tile
highlighted; titles without a video handle keep the stills attract. Full clip
playback is a follow-up. Any pad input returns to the same shelf and focus;
A/South may launch the current attract title. An empty playlist shows a themed
idle panel rather than a frozen grid. Attract edge chrome is off by default.
A measured 0..1 level file (`-audio-level-file` / `FOGCAST_AUDIO_LEVEL_FILE`)
drives theme-highlight bars on attract and on browse/wheel/detail; the
designated kit exposes only ALSA Dummy capture/playback, which is not FPGA
HDMI audio, and `GET /api/v1/session` has no audio level. Enabling
`-audio-chrome`, `launcher.json` `audio_chrome`, a theme `audio_chrome`
token, or `FOGCAST_AUDIO_CHROME=1` without a meter paints a quiet attract-only
`idle pulse` labeled in the footer so it is not claimed as game audio. Built-in
packs keep the token false. Aspect-fit letterbox bars mix the system colour toward
the theme label bar; focused cells add a 1px inner highlight. Header uses the title role, tile names use body, placeholder
lettermarks use caption, and the footer uses status. They rasterize the
embedded Go Regular face (no kit system fonts). Tile labels may truncate with
an ellipsis; the Kit reserves bounded multiline footer space for the focused
title and recovery instructions so the actionable text remains readable.
A paired, authenticated host listener
serves a restricted set of existing library, artwork, and session operations and a
session-bound input stream. The host keeps target and input lease ownership; the
adapter sends physical USB events through that stream to the retained virtual
pad. Kit input discovers every eligible USB gamepad (`event*` only) plus
physical USB keyboards for play-session HID, merges
polls in stable device-id order through `ui/inputmap`, and remaps
logical codes with a JSON profile (default identity). Hotplug rescan runs from
the existing 16ms poll on a one-second interval. The native runtime enables the
idle framebuffer and restores it after Stop.
The launcher only paints memory and suspends rendering during gameplay; the
grid consumes `kitlauncher.Model` and does not own those transitions. The SDL
sofa maps remapped logical codes onto the existing `tenfoot.Command` set.

See [kit adapter](kit-launcher.md) and [host connection contract](launcher-host.md)
for setup, controls, exact routes, timeouts and ownership. The existing browser
listener remains loopback-only. The SDL sofa layout and the kit browse views are separate
renderers over the same session model and do not own physical transitions.


## Installed core packages and library entries

The host package store validates and atomically publishes immutable archives by
package ID. Catalog schema v8 associates each stable game entry with an
explicit package ID and optional media role/digest. Multiple titles can use
the same core. Importing a package or media object does not select or activate it.
The `fpga` browse platform is not a cartridge/runtime system. Library scans
exclude its logical root. See [package library API and operations](core-package-library.md).

`fogcast/core_packages.go` owns installation, inspection and checked selection.
`internal/hostapi/core_packages.go` exposes those operations. Read-only target
`POST /v1/development/core/inspect` stages and invokes native `inspect_core`
without acquiring a physical lease or replacing input. The target runtime
remains the compatibility authority. Target transitions can report busy;
failed inspection cleanup cannot report compatibility success.

Package-backed `session/launch` uses the confirmed-package transition with
explicit library context, before ordinary game launch detaches prior input/media.
The service resolves selected immutable bytes under lifecycle admission, then
records the library identity only for the exact confirmed package generation.
Changing the selection affects future launches. Runtime status stays truthful;
the host adds its explicit library association and does not infer one after a
restart. Existing ordinary cartridge launch and Stop paths remain in place.

Media objects are immutable SHA-256-addressed bytes in the existing catalog
database. Host storage accepts 1 byte through 32 MiB independently of target media capacity.
Imports stream to a private temporary snapshot before taking a database writer
transaction; SHA identity, declared length and cancellation are checked before
atomic metadata/chunk publication. Schema 8 stores new objects in 64 KiB chunks,
retaining schema 7 inline objects. Reads verify contiguous chunk order, size and
digest; launch receives a verified private snapshot, never a live database
cursor. Failed imports and closed snapshots remove temporary files. Backups
still cover a single catalog database, not a second persistent asset directory.
Interrupted import reads preserve cancellation and deadline errors through the
service boundary rather than classifying them as invalid media.

`protocol/core_media.go` projects supported transport semantics from exact
package ABI/interface declarations, never core IDs. The offline
`core-media-capabilities` API/CLI separates `import_max_bytes` storage policy
from each role's `min_bytes`/`max_bytes`, format and transport. It explicitly
reports `source:declared-contract` and `compatibility:unknown`: this is not
a live target observation. Unknown versions expose no supported roles, and
optional interfaces still require active runtime support at launch.

Legacy target delivery accepts role `blob`, 1..16384 bytes, for declared
`fes.simple-computer` 1.0 and `fes.media.blob` 1.0 capabilities. A package with
both required `fes.media.blob` 1.0 and required `fes.media.blob-stream` 1.0
uses the explicit stream transport. Its offline declared safe range is
1..32768 bytes (32 KiB), not the host's 32 MiB import capacity. No core-ID
allowlist selects this behavior. Unknown stream versions do not widen legacy
delivery. The raw `core-media PATH` CLI and its host development endpoint
retain their original 16 KiB limit.

Stream delivery uses authenticated target `POST /v1/development/media-stream`
with fixed `Content-Length`, `application/octet-stream`, package/generation
headers and the existing kit lease. It shares lifecycle and update exclusion
with legacy `/v1/development/media`; it does not add a host management route
to the paired launcher listener. The adapter stages through a bounded buffer,
honors request/lease cancellation during staging, then rechecks identity and
observed limits before the single protocol-2 `load_media_stream` request:
`path`, `expected_package_id`, `expected_generation`, and `size`.
Legacy `load_media` is unchanged. No network caller supplies the staged path.

Runtime `capabilities.media_stream` and target `core_package.media_stream`
carry the exact interface version and observed `min_bytes`, `max_bytes`, and
`chunk_bytes`, associated with the active package and generation. Admission
uses the coordinator's retained copy of that observation from normal package
activation. Status snapshots deep-copy it so callers cannot mutate retained
media admission through a returned capability pointer. Admission
requires min=1, max=32768..33554432, chunk=512 and both active interfaces;
missing or invalid observation fails closed. FogCast still caps selection and
delivery at the declared 32768-byte guarantee even if an endpoint reports more.
The runtime owns the actual endpoint query and physical transaction. Generated
constants live in `protocol/internal/generated/fes_simple_computer.go`, retaining
the shared emitter's `generated` package. The runtime serializer fixture at
`internal/misterruntime/testdata/protocol-v2-media-stream-responses.jsonl`
tests the C++ JSON/Go decoder boundary, not physical hardware behavior.

`fogcast/core_media.go` rejects assets outside the selected capability's size
range before opening a verified snapshot or activating hardware. Library
storage/import and target delivery deliberately have different limits.
It validates selection and snapshots bytes before package
activation. One lifecycle admission spans package activation, media delivery,
and any Stop/recovery cleanup. Delivery binds the confirmed package ID,
generation, target and target ID; failure is not a usable launch. Kit, browser,
and CLI use the same session launch path, with no core-ID media registry.
Launch target binding occurs only after lifecycle admission. After activation,
media failure receives a separate bounded cleanup deadline, including when the
launch caller canceled or timed out.
Snapshot close failure after delivery enters that same Stop/recovery branch;
it cannot turn a running launch into an error without attempting cleanup.
For stream delivery the host uses `max(UploadTimeout, 150s)`, still bounded by
the caller's deadline/cancellation. The target retains its operation owner for
a bounded 135s call covering the runtime worker's 120s budget after staging.
Legacy delivery retains its configured host timeout and existing 15s adapter
budget. Ambiguous mutation replies are never replayed. These tests and limits
make no SMS execution, mapper, or hardware-acceptance claim.

Schema 7 seeds the historical Coleco diagnostic and binds existing Coleco
entries once, preserving game IDs and history. The licensed source remains in
`catalog/seeds` for migration only. New entries select media explicitly;
cleared selections are not restored on restart. Package/media selection affects
the next launch, not the active session. The raw `core-media` development
operation remains available. This transport is not a retail compatibility
claim or support for arbitrary new media roles.

A confirmed package activation commits its host-side ownership only after the
previous host executor stops successfully. If that cleanup fails, the service
retains the host owner and a package recovery marker, reports the observed
FPGA package with the recovery error, and blocks input attachment. Stop or a
subsequent package launch retries target recovery and host cleanup before
clearing either pending owner.

Runtime reconciliation may report `State=idle` with a retained runtime error,
such as a failed native core load. Coordinator Stop fast-paths only clean idle;
it retries runtime Stop for idle-with-error and clears the error only after a
confirmed clean idle response. A failed Stop keeps the recovery error visible.

An idle menu therefore does not by itself establish launch readiness. A new
explicit library launch can clear a retained idle legacy-launch error through
one existing leased Stop request before its single activation. Source/media
validation, target identity, API and ownership admission still apply. Cleanup
must confirm clean idle; active sessions, save failures, attributed package
failures and reboot-required states are not silently cleared. Cleanup failure
blocks activation and remains visible. This does not replay the earlier failed
launch or automatically reboot. Other recovery states still require explicit
Stop or operator attention. Kit launch diagnostics record the submitted game ID and
bounded result code, separately from the currently displayed selection, without
logging credentials or raw response bodies.

Stop errors additionally expose an allowlisted `stop_stage`, independently of
the target protocol `phase`. Admission health, ownership, status and lookup
backoff are distinguished from `target_stop` and later recovery. `target_stop`
means the client call began, not proof that the remote mutation executed. Stage
reporting preserves cancellation and protocol error identity. Explicit Stop
ignores the background lookup backoff timer, but still performs fresh bounded
health/version, identity, ownership and status admission. Polling retains its
backoff; Stop does not bypass those checks or replay a mutation. This fixes the
host-side backoff refusal, not every possible Stop failure, and is not itself a
hardware acceptance claim.


## Described-core persistent data

`fogcast/core_data.go` resolves a selected immutable library package under the
existing lifecycle admission, binds each data result to that package and the
current target, and exposes settings/progress APIs documented in
[the package library guide](core-package-library.md#persistent-settings-and-progress).
Host code consumes the runtime's `persistence_layout` metadata without a second
ABI registry. Selection rejects persistent-to-missing/different-layout changes,
including when no record has yet been written, and checks durable target data.

`targetclient/core_data_client.go` sends bounded archives and expected package IDs to
`internal/httpapi/core_data.go`; writes and library loads use the existing kit
lease. Read-only data inspection does not claim hardware. The target coordinator
serializes these calls against lifecycle operations. `internal/misterruntime/core_data.go`
privately stages/admit-checks exact bytes, supplies the fixed trusted
`/media/fat/fogcast/core-data` root, and calls protocol-2 `inspect_core_data`,
`update_core_settings`, or `load_library_core`. No network caller supplies a
filesystem path. Failed cleanup remains owned, and an ambiguous settings write
is never replayed. A successful settings-only write or definite admission/revision rejection
releases only the grant acquired for that operation; an existing session grant
stays held. Described-package Stop uses protocol 2 so retained unsafe
persistence recovery cannot trigger the legacy development autoreboot path.
Leftover staging from a failed inspect or settings cleanup does not divert a
later native-game Stop onto that protocol-2 path. Explicit library loads and
described-package Stop retain the same diagnostic dispatch events and native
event-dump import as the other runtime operations.

The runtime owns bounded record validation, revision CAS, settings/progress
semantics, atomic publication, and physical restore/capture. It refreshes a
same-core incoming record after outgoing flush, before activating the next
generation. Development `load_core` remains volatile. Active package status
reports persistence mode separately from its historical development execution
label and host library association. A safely resumed save failure preserves
its exact generation and can restore host input; unsafe persistence recovery
retains package/generation attribution in failed status and blocks input.
Existing post-activation HostOnly cleanup recovery remains unchanged.
