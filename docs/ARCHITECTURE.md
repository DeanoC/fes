# FogCast architecture

This is the canonical description of the working system.

## Normal FPGA game launch

```text
Browser UI
  -> POST /api/v1/session/launch
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

The browser sends a game ID. The host resolves it through the catalog and
system table, uploads a cache miss, and calls the target agent. The agent
writes the MGL atomically and sends `load_core <mgl>` to `/dev/MiSTer_cmd`.
FogCast waits for the expected value in `/tmp/CORENAME`. Stop uses the same
command path with `menu.rbf` and waits for `MENU`.

## Process ownership

The host owns the catalog, UI, user intent, content selection, and host-side
media. The target agent owns its HTTP API, cache, transient MGLs, and launch
requests. The MiSTer/Main-compatible process owns FPGA programming and the
MiSTer core services.

These are simple process boundaries on a local, disposable development kit;
they are not a distributed ownership, failover, or recovery protocol.

## Agent runtime backends

The target agent defaults to the existing Main runtime. Passing
`--runtime native` explicitly selects the separate native adapter, which uses
only `/run/mister-runtime.sock` and does not inspect the Main process,
`/dev/MiSTer_cmd`, or `/tmp/CORENAME`. There is no backend detection or
fallback.

The native adapter reports ready only when `mister-runtime` reports `idle`.
An idle Stop confirms that state without calling the runtime Stop operation.
The native adapter admits Mega Drive, ordinary SNES cartridges, and the
registered ROM-less Pong profile.
Mega Drive validates an absolute staged ROM and sends one local request using
`/usr/share/mister-runtime/cores/megadrive.rbf` and media role `cartridge`.
Pong uses `/usr/share/mister-runtime/cores/pong.rbf` with `media: {}` and
`settings: {}`; supplied ROM paths and cached-content launches are rejected.
SNES uses `/usr/share/mister-runtime/cores/snes.rbf`, exactly one `cartridge`
path, and empty settings. FogCast checks the staged file path and extension;
the runtime validates cartridge bytes before hardware mutation and owns the
512-byte metadata prefix. It does not modify the host cache or content hash.
The native package index is 1; the Main MGL selector remains 0. Initial support
is bounded ordinary LoROM/HiROM; enhancement chips, external firmware, expanded
mappings remain outside this slice. Other systems remain
unsupported by this adapter. All admitted profiles reconcile lost responses
only against the requested system/core identity, without replay, and use the
ordinary Stop-to-idle lifecycle. SNES is software-tested; exact-artifact
hardware acceptance is a separate integration step.
Mega Drive remains the hardware-tested native game. The separate development
operation accepts only the existing MiSTer-compatible
ABI and has no catalogue identity. The native adapter atomically stages one
bounded upload at `/tmp/fogcast-development/core.rbf`, dispatches it once to
the runtime, and resolves ambiguous responses through Status without replay.
Development Stop uses the ordinary native Stop-to-idle path; reboot recovery
is reserved for an actual native cleanup failure. The separate `native-dev`
image packages this composition. Its idle, visible Sonic 2 launch, one-player
input, Stop, and immediate relaunch paths are hardware-tested on the designated
kit. Native development loading is hardware-tested only for the existing
MiSTer-compatible load/Stop lifecycle and subsequent game regression; exact
two-cycle evidence is in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md).
Raw development uploads still have no generic video or input guarantee.

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
backend explicitly rejects ROM-less profiles. Host discoverability does not
establish that a selected target image contains the required Pong RBF.

## Other modes

Host-emulator execution, remote input, capture, and host-to-target media are
existing optional modes. They share the host session UI but do not replace or
precede the direct FPGA launch path.

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
    players, summary, screenshot_ids, year, genre, attribution)
  -> GET /api/v1/library/attract (idle video then stills; artwork via the same presentation artwork GET)
  -> GET /api/v1/library/settings and PATCH /api/v1/library/settings (idle seconds, preferred regions, selected target, library roots)
  -> POST /api/v1/session/launch
  -> POST /api/v1/session/development-rbf (raw octet-stream from a local path OSK)
  -> GET /api/v1/session (poll; now-playing or DIAGNOSTIC development chrome)
  -> GET /api/v1/session/events?after= (poll; sofa event list)
  -> GET /api/v1/session/preview (optional MJPEG; 404/503/inactive is unavailable)
  -> POST /api/v1/session/stop
  -> GET /api/v1/health (poll; kit chrome)
  -> GET /api/v1/status (503 TARGET_UNAVAILABLE treated as kit-down)
  -> GET /v1/kit/lease on the selected target address (status-only lease strip)
  -> POST /api/v1/session/input/attach and /detach (empty body; FPGA-native now-playing)
  -> host session service
  -> existing FPGA launch path
```

TV overscan insets, sofa layout (`grid`, `shelf`, or `list`), and the local
attract on/off gate are local to the tenfoot process (CLI `-safe-area` /
`-layout` / `-no-attract` and optional `tenfoot.json` prefs). There is no host
safe-area or layout API. Host attract idle, preferred regions, selected target, library roots, and
targets use the existing public library settings endpoints. Tenfoot can add,
edit, and remove targets from the sofa settings overlay. Agent secrets are
write-only: GET exposes `agent_configured` only, the sofa never echoes a
stored agent, and PATCH sends `agent` only when the operator edited or
cleared it.

Attract prefers a playlist `video` handle when present. Darwin CGO builds
decode with AVFoundation (`host/tenfoot/attractvideo`) after streaming
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

Source entry points are `host/tenfoot/` and `cmd/fogcast-tenfoot`. UI draw
helpers use `host/tenfoot/gfx.Device` (begin/clear/present, RGBA8 textures,
textured quads, fill rects). Window, events, gamepad, and text input remain
SDL in `host/tenfoot/sdl.go`. `TENFOOT_GFX` / `Options.GFX` may select
`software` or `fpga-stub` for tests; the production sofa path stays SDL3.
linuxfb is a kit framebuffer Device, not the SDL sofa shell.

| Backend | Construction | Role |
| --- | --- | --- |
| SDL3 | `gfx.WrapSDLRenderer` (`host/tenfoot/gfx/sdl3.go`, build tag `sdl3`) | Default production path: wraps the process `SDL_Renderer` with letterbox logical presentation and VSync. |
| Software | `gfx.NewSoftware` (`host/tenfoot/gfx/software.go`) | Pure-Go RGBA8 rasterizer for tests and CI (no cgo, no SDL). Nearest blit, `Snapshot` for golden pixels. |
| FPGA stub | `gfx.NewFPGAStub` (`host/tenfoot/gfx/fpga.go`) | Placeholder for a future MiSTer custom 2D accelerator. Delegates to Software today; exposes `BackendName` / `IsStub`. Does not talk to kit, runtime, or RBF. |
| linuxfb | `gfx.OpenLinuxFB` / `gfx.NewLinuxFB` (`host/tenfoot/gfx/linuxfb.go`) | Software rasterizer whose `Present` blits RGBA8 to a 32bpp Linux framebuffer (`/dev/fb0`) with destination stride and BGRX byte order. CGO-free ARMv7 spike: `cmd/tenfoot-linuxfb-spike`, which reads evdev/joystick via `host/tenfoot/linuxinput` and moves a cursor (Start/ESC/Q quit). Sibling `cmd/tenfoot-linuxfb-grid` paints a hardcoded cover-grid on the same Present + linuxinput path (highlight, confirm, quit; no catalog). |

`gfx.Recorder` remains a call-order test double and does not draw pixels.

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

FES owns compatible source selection and release/media assembly. FogCast supplies
`cmd/fes-boot`, the target update API, and `cmd/fes-update`. Online releases replace
only a content-addressed read-only ext4 system image. The locked kernel, U-Boot,
and fixed `/linux/linux.img` bootstrap stay outside that operation. Configurations,
target identity, cache, and SNES saves remain on FAT outside every rootfs.

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

`internal/appliance` owns bounded raw-image admission, immutable publication,
cross-process locking, checksummed state, and consumed-trial selection.
`internal/applianceboot` owns fallback ordering; `internal/bootlinux` owns the
Linux mounts, loops and watchdog. The bootstrap records its selected image in
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

The active image toolchain is under `buildroot/`, `containers/target-image/`,
`internal/targetimage/`, and `scripts/*target-image*`. It produces:

- `build/output/target-image/dev/linux.img`: the fast development image.
- `build/output/target-image/prod/linux.img`: the reproducible production image.
- `build/output/target-image/native-dev/linux.img`: the reproducible native
  runtime candidate image.
- `build/output/target-image/kernel/`: the reproducible kernel artifact.

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
has no Main startup or legacy Menu-configuration helper, has no
`/dev/MiSTer_cmd` wait, and retains the same read-only root with volatile
`/run`, `/tmp`, and `/var/log`. Its build-input record identifies the runtime
commit, agent binary, and both RBFs. Its QEMU smoke proves only root filesystem
and init packaging; it does not emulate FPGA programming, prove target
readiness, or establish game or development-RBF support. Its build-input record
identifies the runtime commit, agent binary, idle RBF, and selected Mega Drive
RBF provenance. The designated-kit idle, Mega Drive launch/input/Stop/relaunch,
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

The native image build resolves a sealed `megadrive.rbf` plus its normalized
selection record before Buildroot. The selected record carries the origin,
MiSTer ABI, `megadrive` system, repository revision, artifact identity, exact
size/SHA-256, role install path, and source-built recipe/toolchain fields when
applicable. The runtime receives the same role path for either origin.

- `make target-image-native MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle`
- `make target-image-native MEGADRIVE_RBF_SOURCE=upstream`

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
`host/development_client.go`, `internal/httpapi/development.go`,
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
retries reuse the same ID. Cache transfer and status inspection do not reserve the kit. A Stop
ends the current runtime session but retains ownership for another launch.

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
`disconnected`, `connecting`, `ready`, `active`, `busy` and `recovery-required`,
separate from runtime/game state. Busy responses include the public owner label.
The browser and tenfoot target views show this state. Manual addresses remain
usable where multicast is unavailable. This path has host/fake-peer regression
coverage; physical reboot and DHCP acceptance belongs to the selected FES image.

## Native kit launcher

The native image packages `fogcast-kit`, a CGO-free controller/session adapter
with a live 4×3 catalog grid renderer. Catalog cells paint decoded box-art from
`GET /api/v1/presentation/artwork/{handle}` when `Game.Cover` is present, and
keep the system-color fallback otherwise. A paired, authenticated host listener
serves a restricted set of existing library, artwork, and session operations and a
session-bound input stream. The host keeps target and input lease ownership; the
adapter sends physical USB events through that stream to the retained virtual
pad. The native runtime enables the idle framebuffer and restores it after Stop.
The launcher only paints memory and suspends rendering during gameplay; the
grid consumes `kitlauncher.Model` and does not own those transitions.

See [kit adapter](kit-launcher.md) and [host connection contract](launcher-host.md)
for setup, controls, exact routes, timeouts and ownership. The existing browser
listener remains loopback-only. The SDL sofa layout and the kit grid are separate
renderers over the same session model and do not own physical transitions.
