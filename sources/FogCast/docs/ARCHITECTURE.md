# FogCast architecture

This is the canonical description of the working system.

## Normal FPGA game launch

```text
Browser UI, kit launcher, or `fogcast launch|status|stop`
  -> GET /api/v1/session, POST /api/v1/session/launch, POST /api/v1/session/stop
  -> host session service
  -> target described-package library endpoints
  -> mister-agent session/transfer coordination
  -> /run/mister-runtime.sock, protocol 2
  -> libmister-runtime physical lifecycle
  -> FPGA package and declared media/input contracts
```

The entry points are `internal/hostapi`, `fogcast`, `internal/agent`,
`internal/httpapi`, and `internal/misterruntime`. `internal/systems/table.go`
retains library platform metadata; it does not describe raw-core launch recipes.

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
and input attach/detach stay on their existing endpoints. The host resolves installed package entries and explicitly binds library
persistence. The runtime validates the package and declared interfaces before
programming. Bare legacy game records remain browseable but unlaunchable.
Installed FPGA `core_package` rows, and any row on the `fpga` catalog
platform, stay `launchable` even when a cartridge platform such as Coleco
has no host-emulator mapping. Rooms treat those titles as Ready once
composition flags such as `firmware_ready` pass; raw Coleco library carts
remain browse-only.
Native FPGA Stop uses the
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
media. The target agent owns its HTTP API, transfers and session requests.
libmister-runtime owns FPGA programming, media/input delivery and recovery.

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
optional sealed package ID, and on an appliance boot the bootstrap ticket
`image_sha256`. The agent does not hash live binaries on each poll. Missing
fields mean there is no sealed record (for example an unsealed diagnostic image), not
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

## Agent runtime

The agent uses only local runtime protocol 2 through `/run/mister-runtime.sock`.
There is no backend selector, Main process probing, MGL dispatch or protocol-1
fallback. Normal FPGA launches require described FES packages; bare catalog
records and old raw-core profiles are rejected before stopping an active package.
Contained raw-RBF loads remain an explicit diagnostic with no media/input ABI.
Their generation and state are reconciled without replay; Stop restores idle.

A `fes.simple-computer`
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
activation and does not create that entry. The sealed shell's machine ROM
is empty. Launch requires `[zx81_machine_rom]` with `script`, `image`, and
`mistral_cv`, each a clean absolute path, and refuses the launch when that
section is absent. The host splices the 8 KiB BASIC image after package
identity is fixed, keeps that package id, and records `image_sha256` on the
launch status after firmware and media binding. A selected cart is composed
first and stays on the launch source, so the activated composition matches
the selection. The ROM splice is last.
Mid-session `.p` tape select/load while the core stays running is a
separate product path from that ROM splice; see the FES design lock
[ZX81 tape media](../../../docs/zx81-tape-media.md).
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
destroys it once. Package interface and generation checks gate delivery.
Exact-artifact hardware acceptance remains a separate integration gate.

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

## FPGA product admission

Only installed described FES packages are FPGA products. Historical bare Pong,
SNES, NES and Mega Drive records do not grant launch eligibility. Existing
catalog rows, ROM caches and old saves are preserved; the host does not seed a
raw Pong product. Package persistence uses the described-core records below.

## Core package inspection and staging

`corepackage` is the shared, hardware-independent format-2/3 reader. It
inspects closed directories or restricted uncompressed ustar archives,
validates the closed typed manifest and payload bytes, and computes package
identity from the original manifest and payload. Unknown but well-formed ABIs
remain inspectable; hardware compatibility belongs to the native runtime.
`corepackage.InspectPackage` returns the package ID and closed `Descriptor`
from the same pinned read for identity-reporting consumers such as
`core-inspect`; the smaller `Inspect` wrapper returns only the descriptor.

`corepackage.Stage` accepts a caller-bounded archive stream and publishes only
validated `manifest.toml`, `core.rbf` and, for format 3, `rom-map.json` bytes
into a distinct sealed directory
beneath an absolute private root. Cancellation or validation failure removes
the incomplete directory, including cancellation observed after rename and
before ownership handoff. The caller owns the returned directory lifetime and
must release it with `Staged.Cleanup`, which reopens and verifies the retained
root and publication identities before removing the sealed directory.

The target package lifecycle uses runtime protocol 2. A read-only
`inspect_package` exchange negotiates the exact ABI registry and programming
profiles before mutation. Protocol 1 is rejected without mutation; there is
no negotiation fallback.
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
agent health is not session readiness. The host service activates installed
packages through the target described-package library endpoints.

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
report `reboot_required`; the leased recovery path programs idle again
(`recover_idle`) and reboots the board when that LoadIdle fails or the
runtime rejects `recover_idle` as an unknown operation. If recovery fails
closed without arming a reboot, Stop returns that error instead of waiting
for a new boot id.
Stop cannot bypass a poisoned GP transport by merely quiescing it. Physical
hold/reset, byte transfer and recovery remain runtime responsibilities.

For a separately started diagnostic host on port 8797, the operator sequence
is `fogcast --api http://127.0.0.1:8797 core-load PACKAGE.fcore`, then
`fogcast --api http://127.0.0.1:8797 core-media MEDIA.rom`. Starting that host,
deploying its matching agent/runtime, and hardware validation are separate
integration operations. FES selects the runtime revision and generates the concrete assembly lock;
live compatibility is determined by protocol and operation contracts.

Focused tests cover admission, existing leases, update exclusion, lifecycle
serialization, staging permissions and exact bytes, generation changes,
cross-target package/generation collisions, keyboard events during transfer,
recovery status, Unix request shape and no replay. The end-to-end test runs
CLI → real host API/service → real target client/lease/HTTP/controller → native
adapter and Unix socket, with only the hardware daemon simulated. This is
host-only evidence, not Coleco hardware acceptance.

### Session live media (ZX81 change-tape)

Mid-session tape arming for an active `fes.simple-computer` generation uses the
runtime `replace_live_media` / `clear_media` path (no hold-reset soft-reboot).
It is distinct from launch-time `POST /api/v1/session/development-media`
(`load_media`). See the FES lock
[ZX81 tape media](../../../docs/zx81-tape-media.md).

Operator/diagnostics CLI:

```text
fogcast --api http://127.0.0.1:8787 --json change-tape MEDIA_ID_OR_.p_PATH
fogcast --api http://127.0.0.1:8787 --json eject-tape
```

`change-tape` arms a household core-media object into the active session. A
`.p` / `.P` path is imported through `POST /api/v1/core-media` first; a 64-hex
media id uses the already stored object. Admission requires 1..16384 bytes and
a `.p` / `.P` name. The host call is `POST /api/v1/session/live-media` with JSON
`{"media_id","name"}` and the same session/package/generation/target binding
headers as development media. `eject-tape` posts
`POST /api/v1/session/live-media/clear` so the next empty `LOAD ""` is `0/0`.
The target agent uses `POST /v1/development/live-media` and
`POST /v1/development/clear-media`. Busy-while-LOAD maps to retryable `BUSY`.
A poisoned or unstable GP handshake is the same retryable `BUSY`. A hard MMIO
failure or an invalid clear acknowledgement stays `MISTER_UNAVAILABLE`, even
when the runtime phase is `input`. The host retries phase-`input` busy and
does not relabel that hard failure as loader contention. Unavailable eject
does not replace the active session. The host does not inject BASIC `LOAD ""`
keys; the user types that on the ZX81 keyboard after the tape is armed. Sofa
tenfoot Load-tape chrome (Y / north while the session is active and live-media
capable) opens a local `.p` picker, imports via `POST /api/v1/core-media` when
needed, then arms with `POST /api/v1/session/live-media`. The same overlay can
eject through `POST /api/v1/session/live-media/clear`. It retries loader `BUSY`
and leaves the session active when eject reports unavailable.

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
  -> GET /api/v1/library/edition-preferences and PUT /api/v1/library/edition-preferences for household room edition choice
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
  -> POST /api/v1/session/stop (empty body releases the kit lease; optional client stamp JSON; retain_lease true keeps it; release_idle drops idle grants without stopping a surviving play; X-FogCast-Client-* headers)
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
attract on/off gate, reduced motion, the look name, the optional debug HUD, and Home
(`home` start screen plus `pinned_rooms`) are local to the tenfoot process (CLI `-safe-area` /
`-layout` / `-no-attract` / `-theme` / `-debug-hud` / `-home` and optional `tenfoot.json` prefs). The Home overlay lists pinned rooms, recently played games, installed rooms, and the library. There is no host
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
Household Coleco BIOS import is a separate overlay: rooms/library Confirm on
a firmware-required title opens a local file picker and posts
`POST /api/v1/core-media` plus `PUT /api/v1/library/firmware`. That is not a
development RBF load. Mid-session ZX81 Load-tape is another overlay: while an
active `fes.simple-computer` session advertises `fes.media.blob`, Y opens a
`.p` picker that arms or ejects through the session live-media API without
relaunch or hold-reset `load_media`.

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
`internal/targetimage`) while FES owns external-artifact policy in
`image/build/native-inputs.toml`. FES adds its selected runtime revision to a
disposable assembly copy;
FogCast has no expected-runtime SHA constant. The explicit hardware diagnostic accepts
that concrete copy through `NATIVE_RUNTIME_INPUT_LOCK`.
FES produces:

- `image/build/output/target-image/native-dev/linux.img`: the reproducible native
  runtime candidate image.

The only image variant is `native-dev`, which starts image-owned
`mister-runtime` and `mister-agent`. It installs the locked splash/idle artifact
and the selected closed FES package set (`fes.pong`, `fes.zx81`, `fes.coleco`).
The image selector validates and copies the closed `manifest.toml` and
`core.rbf` set, including `rom-map.json` for format 3, beneath exact package IDs. It
retains the external producer/package selections beside the image,
has no Main startup or legacy Menu-configuration helper, has no
`/dev/MiSTer_cmd` wait, and retains the same read-only root with volatile
`/run`, `/tmp`, and `/var/log`. Its build-input record identifies the runtime
commit, agent binary and idle RBF provenance. For each
format-2/3 package selected, the record also identifies the exact selection
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

## Contained development RBF diagnostic

`POST /api/v1/session/development-rbf` sends one bounded raw upload through the
agent to protocol-2 `load_development_rbf` with `development-contained-v1`.
The runtime contains the FPGA, programs once and publishes a generation with
no core/system/package identity. It supplies no package ABI, media, video or
input guarantee. Stop restores the locked idle artifact. Ambiguous mutations
are observed once and never replayed. An actual recovery failure retains
`reboot_required`; a separately requested reboot is verified by changed boot
identity and fresh idle. On the native agent that marker is HTTP 200
`state=stopping`, `development=true`, and `recovery=reboot_required`, and only
when the session was already development and runtime Stop returned
`reboot_required`. A Stop reply that is still `starting`, or a lost Stop,
stays `failed` with `development` set and no `recovery`, and does not arm
`POST /v1/development/reboot`. The decision table is the FES
[soft-restart Path B](../../../docs/soft-restart-path-b.md) note. Development uploads remain volatile.

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
Explicit public Stop (empty body, or `retain_lease` false) releases its grant
after input/media/hardware cleanup. Sofa Soft-stop sets `retain_lease` true on
that same route and keeps the grant after idle cleanup. An idle selected-target
change may drop that client; the retained grant stays reachable for a later
explicit Stop. An already-idle explicit Stop still attempts that release when
the newly selected target's probe or Stop fails. A Soft-stopped legacy
non-package fpga_native session keeps its execution label and is still
already idle for that release. Soft-stop `retain_lease` still keeps the grants.
Invalidating one target removes only that client's retained grant. A release
that fails leaves that grant retained so the next explicit Stop can retry it. Replacement Stop retains ownership for the next launch.
Explicit release leaves a retained grant held when that lease still backs a
remaining play, so Soft-stop, relaunch, and an explicit stop of another target
do not revoke the live session. A failed explicit Stop of that surviving play
still releases the other idle grants. The tenfoot shell's rooms Soft-stop is
the retain request and keeps the grant while the shell stays up. Start, Q, or
closing the window waits for an in-flight Soft-stop, then releases that idle
grant. A confirmed idle service gets an empty-body Stop. When a play still
survives, the shell posts `release_idle` so idle grants drop without stopping
that play. A failed release is retried before the shell exits.
Application shutdown releases its grants after input/session cleanup.
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
DNS-SD TXT keeps `protocol` and `target_id`. It also carries `node_id` (the
same stable id; the agent does not mint a second one), `mesh` (`major.minor`,
currently `1.0`), and `cap` (a capability bag). The kit bag advertises Execute
`fpga_native` and DisplaySink, plus InputSource for the kit's local pad path.
ABI or package-family suffixes are included only when the advertiser knows
them; the agent omits them because it does not inventory packages before
announcing, and an empty list is not a claim that any RBF runs. DisplaySink
means the node can present. It is not HDMI or ADV liveness, and a host preview
is not this capability. Catalog, Content, Shell, and Coordinator are omitted.
The advertisement carries no credentials, title list, or lease secret. A `ttl`
key is parsed when a peer sends one and is not emitted here; the protocol
strawman leaves the seconds unsigned. Parsed TTL silence is absence for a
future placement choice only and does not release the kit lease. Phase 0
announcements that omit `mesh` stay directly bindable. The host collects those
announcements into an in-memory node inventory (`node_id`, mesh version, and
the `cap` bag) and serves it at `GET /api/v1/mesh/nodes`. The inventory does
not adopt an endpoint, claim a lease, or make a title Ready. A later browse
that no longer sees a node, including a node that omitted `ttl`, drops that
row only. A mesh major other than 1 does not remove that direct bind; a
session that needs the mesh contract fails closed on that major. A random service instance and hostname distinguish
cloned identities on the same link; the persistent TXT identity remains stable
across reboots.

Host-side content identity lives in `internal/meshcontent`.
`fogcast.ProjectMeshLibrary` projects the host library already stored
into that catalog shape: described package id and ABI, the household
firmware digest when that slot is required, the selected primary-media
digest, and named expansion digests. Stored SHA-256 strings pass
through `FromSHA256`. Primary media uses `PrimarySourceID`: the
format-3 source `MediaID`, which the executor records as
`SourceSHA256`. That id is not the post-link `ProgrammedSHA256`.
`MeshExpansion.Digest` is the slot-bytes digest, SHA-256 of the
expansion slot's own bytes (`expansion.Manifest.CartSHA256`). It is
not `Asset.ID`, not the archive `media_id`, and not
`ProgrammedSHA256`. `ExpansionSlotBytesID` names that digest. The
projection does not hash files again and does not link expansion bytes.
A title that cannot be named is skipped. A catalog entry names the
title id (a catalog game id: lowercase ASCII slug), one execute kind,
and the required slots. A launchable `fpga_native` entry requires a
package/ABI slot. A launchable `native_emu` entry requires primary
media and carries no package/ABI slot; BIOS and expansion content-ids
are optional. A content-id is the unsigned strawman `sha256:` plus 64
lowercase hex of that slot's bytes. Deano has not locked the algorithm.
The package/ABI slot is the described package id and ABI, not a
content-id of an RBF. `PackageABI.Major` is that ABI's major, not the
mesh protocol major. `ReadyHere` requires that package id and an
eligible ABI id and major before it reports Ready. Package id alone is
not eligibility.

`meshcontent.Ensure` is the host ensure step. It takes one projected
entry and the executor the session is already bound to. Each required
content-id comes back Present, Checking (mid-pull), or Missing. A
required id that is missing and has no source is
`ErrContentMissingNoSource`. A Checking slot stays Checking.
`Result.Execute` stays false while any required slot is Checking or
the executor's ABI id and major are not eligible, and Launch returns
before the existing execute path. Expansion content-ids stay separate
and are linked by `Executor.LinkExpansion` on that executor. The host
does not pre-link them. Ensure does not choose a node, does not pull
onto a node the session did not bind, and does not release or change a
lease. When a mesh session is installed, `LaunchOn` captures the target
once, before Ensure, and bind uses that same name and node id. Bind
keeps the captured client and returns `ErrUnboundNode` when that name's
address or TargetID no longer matches the capture. An implicit target
with an empty TargetID is the bound node only when its name is that
node. Otherwise Ensure returns `ErrUnboundNode` and does not pull. With
the seam off, Launch leaves the target live: bind resolves the selected
target under the target lock at bind time, so a settings change that
selects another target or replaces its client is the endpoint that
launches. Before execute, and after lifecycle admission, Launch compares
the ensured catalog row with the row now selected. The core path and the
host-only path both do this, and both refuse a changed package, media,
firmware, ROM, or expansion composition. Host-only mesh play
stays on the installed session node. A launchable FPGA entry that the
foreign-kit check would deny is rejected before Ensure. The executor
is an interface. Tests pass a fake. The kit store that pulls bytes
through the target agent is not in this slice.

The projection is not a host route. JSON tags stay on the host catalog
shape. Ensure results have no JSON tags. Rooms Ready,
`GET /api/v1/games`, and `GET /api/v1/mesh/nodes` do not call
`ReadyHere` or `Ensure`. `POST /api/v1/session/launch` calls Ensure
only when `SetMeshExecuteSession` installed a session; otherwise Phase
0 and Phase 1 launch are unchanged. Phase 1 Ready remains composition
against the bound executor.

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
monitor before releasing leases. Development recovery first programs idle on
the current boot. It starts a board reboot only when that idle program fails,
then uses the same read-only validation while retaining its existing lifecycle
admission. A soft reboot after FPGA or HPS activity can leave the kit
unreachable; if idle recovery still reports `reboot_required`, use a hard
power cycle.

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
Busy means another session holds the kit lease. Tenfoot rooms show a Ready
FPGA title aimed at that kit as Unavailable, with the copy "This executor is
in use." Confirm explains and does not launch. A host-only title stays Ready
and Play reaches the host executor. `POST /api/v1/session/launch` returns the
existing lease denial, without claiming, when that launch would use the busy
kit. A host-emulator title and a launch aimed at a different target still
proceed. That named target is the client used for the load, including while a
host-only session is still the active execution. The shell that holds the grant,
including after Soft-stop, stays ready and keeps Phase 0 Play. Generation
takeover remains `POST /v1/kit/takeover`.
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
when the target inventory is reachable; ROM-less rows omit it. When the idle
enables the HPS framebuffer, boot may paint that shelf from disk before host
games HTTP as a temporary linuxfb overlay. It decodes visible covers from disk
first and labels an absent host `Offline - local library`. Confirmed idle
without an HPS framebuffer (SPI `0x002f` omitted) does not present, so FPGA
splash pixels stay on HDMI, and a missing linuxfb device does not stop the
service.
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

Coleco firmware and optional ZX81 expansion-bus carts use the normal library launch
path. A title may require a household firmware object; rooms/catalog **Ready**
follows that fill, and `session/launch` binds firmware before cartridge media
and reset release. Tenfoot Confirm imports an 8192-byte BIOS through the
existing media/firmware APIs. Expansion selection binds an independently linked
pack to the exact shell package. Later removable media work remains proposed
in [launch composition](launch-composition.md).

Library list, detail and variant responses report expansion selection and
readiness independently of firmware requirements, including firmware-free ZX81
shells. Expansion admission distinguishes missing/invalid packs from catalog
failures: missing titles retain not-found responses, concurrent choices retain
conflict responses, and unexpected storage failures remain internal errors.
Malformed archives or incompatible compositions are rejected as admission
errors.

`fes.application` 1.0 packages compose fixed 720p60 video with optional presence
of normalized gamepad and raw blob/stream media interfaces. Each implemented
operational interface is declared required; omitting input creates an autonomous
demo. The runtime remains the compatibility authority through its negotiated ABI
registry. Host session input attachment follows observed gamepad/keyboard
capabilities, so an application without input does not attach a controller and
an application gamepad does not pass through legacy Coleco/SMS keyboard mapping.
The host recognizes the application media transport by ABI and interface version,
never core ID. Library launches requiring application blob media reject a missing
selection before package activation; entry creation and development package load
remain possible before choosing/uploading media. Development loads with media
remain held until commit. Existing simple-game/computer behavior is unchanged.
These software contracts do not establish exact-image hardware acceptance or
general keyboard/mouse support. Shared controller ports are described below.

### Shared controller ports

An observed `fes.gamepad.ports` 1.0 interface attaches the ordinary host session
input stream and reports `core_package.gamepad: true`. It has two digital ports;
optional `fes.keypad.ports` 1.0 adds a twelve-key mask on each port. Attachment
uses exact observed interface versions, not a core-ID allowlist. A migrated
Coleco package omits `fes.keyboard`, so its pad does not pass through the old
controller-to-ZX81-matrix translation. Existing simple-computer Coleco and SMS
packages retain that translation.

`remoteinput.Event.Player` and the existing binary frame's `Player` carry port
0 or 1. Omitting the JSON field preserves port 0. The public
`POST /api/v1/session/input/event` and paired launcher's existing NDJSON stream
carry the same event. For example, this presses player 2 Right; changing
`Action` to 0 releases it:

```json
{"event":{"Player":1,"Device":1,"Kind":1,"Action":1,"Code":103,"Value":0}}
```

Gamepad codes 100–107 remain Up, Down, Left, Right, A, B, Start, Select.
Keypad codes 120–129 mean digits 0–9; 130 means `*` and 131 means `#`.
These are semantic gamepad-button events, independent of a keyboard layout.
Input profiles can bind spare physical controls to `keypad-0` through
`keypad-9`, `keypad-star`, and `keypad-hash`. Keypad events require the observed
keypad interface. Host snapshots and reconnect replay retain each player's
buttons and axes separately. Axis directions use an 8000 deadzone and combine
with held digital directions, so centering a stick cannot release a held D-pad.

`ui/kitlauncher/controller.Hub` assigns the lowest free port in stable device-ID
order, never renumbers a surviving controller, and emits releases and zero axes
for an unplugged pad before recycling its port. At most two pads contribute.
The kit keeps Select+Start stop chords separate per controller. When the active
core has only the legacy single-pad contract, the kit retains the prior merged
port-0 behavior, including input from a surviving second physical pad. Browser
and SDL physical-device assignment are unchanged; clients can submit explicit
players through the shared API without room or renderer changes.

The native target controller selects `controllerPortsSink` from a fresh runtime
capability observation under its existing input lifecycle lock. For ports cores
it bypasses uinput and sends the local protocol-2 `set_controller` full snapshot:
`package_id`, `expected_generation`, `port`, `buttons`, and `keypad`. Digital
bits are Up, Down, Left, Right, A, B, Select, Start; keypad bits are 0–9, `*`, `#`.
The runtime validates the complete request and owns the physical GP writes.
There is no additional network input endpoint or virtual-device discovery rule.
Other cores retain the single virtual gamepad and keyboard sink.

Disconnect, detach, source handoff and Stop neutralize both dirty ports. A failed
write remains dirty because delivery may have happened. Cleanup attempts both
ports and propagates failures. A reconnect cannot publish its replay until the
old stream finishes releasing input. Core replacement uses the existing input
barrier; every cleanup retains its original package and generation. A failed
zero request may retire old local state only after a fresh runtime observation
proves clean idle or a different active generation. Same-generation failures,
unavailable observations and reboot-required states remain errors; cleanup never
zeros a replacement core. New generations begin with empty host and target state.

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
Replacing a recognized-ABI native package session with a cartridge or host-only
title stops the package-load target and clears package ownership first. True
Diagnostic sessions still require an explicit Stop.

Library titles whose installed package has a recognized play ABI
(`fes.simple-computer` 1.0, `fes.simple-game` 1.0, or `fes.application` 1.0)
resolve to `execution: fpga_native`. `fpga_development` remains the no-ABI
fallback and the explicit LoadDevelopmentRBF / development-core path. Target
status may still report `development: true` for the package-load transport;
the host session and catalog labels follow the ABI policy, not that flag.
After a host restart, an in-progress recognized-ABI package session is
reconstructed as `fpga_native` from that CorePackage ABI. A raw development
RBF or unknown ABI still reconstructs as `fpga_development`.

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
`fes.simple-computer` 1.0 or `fes.application` 1.0 and `fes.media.blob` 1.0
capabilities. Application controller ports and keypads compose with either
media transport; they do not change the selected transport or its size limit.
A package with
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


## ROM-bearing package inspection

`corepackage` accepts closed format-2 and format-3 packages during the ROM
transition. Format 3 carries exactly `manifest.toml`, `core.rbf`, and
`rom-map.json`. The required `rom` manifest table names one ROM requirement
(id, firmware/cartridge role and exact binary source size) and binds the map's
length and SHA256. Package identity uses the format-3 domain and hashes all
three exact members. The maximum archive/import size is 65 MiB; format-2
payload limits and identities remain unchanged.

Import, private staging, inspection and restart adoption validate and preserve
the map. The Go reader checks its closed JSON shape, unique destinations and
source ranges, device/encoding, and binding to the manifest's payload digest
and source size through `expansion.ParseROMMap`. It does not trust a map
supplied separately by a media upload. Expansion composition retains the whole
sealed shell package, including the map, and its package identity.

Format-3 library entries select one exact-size binary through the named ROM
selection API. The host sends a source-only `rom-link.json` envelope containing
the sealed package, ROM bytes and optional expansion asset. It does not run
Python or build the programmed RBF. The target negotiates `rom_linking: 1`,
validates the package/map/source bindings, composes any expansion, then applies
the ROM map using the Go linker before entering the input replacement barrier.

The runtime accepts `load_rom_core`, `load_rom_library_core`, and
`load_rom_composed_core` with the retained programmed artifact and a `rom_link`
identity binding the named ROM, map digest, source digest/size, and programmed
digest/size. It validates and rechecks the artifact before hardware mutation.
Bare format-3 and old initialized format-3 loads are rejected. Active status,
lost-reply reconciliation and restart adoption retain this tuple; adoption
relinks retained source inputs and compares the programmed bytes and identity.
Two ROM selections on the same core package are distinct active instances.

Production ZX81 exports use format 3. Existing format-2 packages retain their
launch behavior, including the older ZX81 Python prototype during transition.
The image selector preserves all three sealed format-3 members. Host-side tests
do not establish hardware acceptance or kit performance; those remain evidence
for the exact tested package and software.
