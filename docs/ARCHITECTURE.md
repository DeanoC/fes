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
is reserved for an actual native cleanup failure. The separate `native-dev`
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
backend explicitly rejects ROM-less profiles. Host discoverability does not
establish that a selected target image contains the required Pong RBF.

## Format-2 core package inspection and staging

`internal/corepackage` is the shared, hardware-independent format-2 reader. It
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
input only for native games or active custom packages with `fes.gamepad`; raw
development RBF sessions remain input-disabled. Manual input attachment uses
the same predicate.

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

## Other modes

Host-emulator execution, remote input, capture, and host-to-target media are
existing optional modes. They share the host session UI but do not replace or
precede the direct FPGA launch path. Local session preview on Linux uses the
configured absolute V4L2 device path and the installed FFmpeg command to
produce H.264 frames for the existing MJPEG preview endpoint. macOS keeps its
native AVFoundation capture adapter; other platforms report capture as
unavailable.

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

TV overscan insets, sofa layout (`grid`, `shelf`, or `list`), the local
attract on/off gate, and the look name are local to the tenfoot process (CLI `-safe-area` /
`-layout` / `-no-attract` / `-theme` and optional `tenfoot.json` prefs). There is no host
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
textured quads, fill rects, CGO-free `DrawText` / `DrawTextWeight` with
embedded Go Regular and Go Bold, and
`DebugText` for the 8×8 HUD / FC2D opcode). Window, events, gamepad, and text input remain
SDL in `host/tenfoot/sdl.go`. `TENFOOT_GFX` / `Options.GFX` / `-gfx` may select
`software`, `fpga`, or `fpga-stub` for tests; the production sofa path stays SDL3.
linuxfb is a kit framebuffer Device, not the SDL sofa shell.

| Backend | Construction | Role |
| --- | --- | --- |
| SDL3 | `gfx.WrapSDLRenderer` (`host/tenfoot/gfx/sdl3.go`, build tag `sdl3`) | Default production path: wraps the process `SDL_Renderer` with letterbox logical presentation and VSync. |
| Software | `gfx.NewSoftware` (`host/tenfoot/gfx/software.go`) | Pure-Go RGBA8 rasterizer for tests and CI (no cgo, no SDL). Nearest blit, `Snapshot` for golden pixels. Cover/screenshot/still downscale is Catmull–Rom at decode. |
| FPGA | `gfx.NewFPGA` (`host/tenfoot/gfx/fpga_device.go`) | Records the versioned FC2D command stream (`host/tenfoot/gfx/fpga_protocol.md`) and rasters through Software. `BackendName` is `fpga`. `IsStub` is true until a programmed 2D core exists; this slice has no mailbox/RBF and is not HDMI FPGA UI. Timed still/crossfade and sprite helpers live in `host/tenfoot/anim`. |
| FPGA stub | `gfx.NewFPGAStub` (`host/tenfoot/gfx/fpga.go`) | Thin Software wrapper without a command stream, kept as `fpga-stub`. `IsStub` is true. Does not talk to kit, runtime, or RBF. |
| linuxfb | `gfx.OpenLinuxFB` / `gfx.NewLinuxFB` (`host/tenfoot/gfx/linuxfb.go`) | Software rasterizer whose `Present` blits RGBA8 to a 32bpp Linux framebuffer (`/dev/fb0`) with destination stride and BGRX byte order. CGO-free ARMv7 spike: `cmd/tenfoot-linuxfb-spike`, which reads evdev/joystick via `host/tenfoot/linuxinput` and moves a cursor (Start/ESC/Q quit). Sibling `cmd/tenfoot-linuxfb-grid` paints a hardcoded cover-grid on the same Present + linuxinput path (highlight, confirm, quit; no catalog). Shared remap and multi-device merge live in `host/tenfoot/inputmap`; linuxinput can apply a `Remapper` to gamepad records. Look tokens live in `host/tenfoot/theme` and are consumed by `fbgrid.Paint` and the sofa `Clear` sites. Kit chrome uses typography roles `title_px` / `body_px` / `caption_px` / `status_px` through `Theme.TitlePx` and siblings; when a role is unset, `header_scale` / `label_scale` / `status_scale` still map to pixel size `8*scale`. Title and chrome header use Go Bold when `title_bold` / `header_bold` are set (built-ins default true); body, caption, and status stay Regular. `DebugText` stays the FPGA/debug path. |

`gfx.Recorder` remains a call-order test double and does not draw pixels.
`gfx.Replay` / `ReplayBytes` apply a decoded FC2D stream to any Device.

Tenfoot looks are data-driven. `host/tenfoot/theme` loads colour, spacing,
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
plus the explicitly selected sealed Pong, SNES and NES RBFs when the four-system
profile is requested. FES may additionally supply one closed format-2
`fes.pong` package/selection pair. The image selector validates and copies only
`manifest.toml` and `core.rbf`, installs them beneath the exact package ID, and
retains the external producer/package selection beside the image,
has no Main startup or legacy Menu-configuration helper, has no
`/dev/MiSTer_cmd` wait, and retains the same read-only root with volatile
`/run`, `/tmp`, and `/var/log`. Its build-input record identifies the runtime
commit, agent binary, idle RBF, and selected Mega Drive RBF provenance. When the
format-2 package is selected, the record also identifies the exact selection
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
with a living-room platform wheel and a live catalog browse renderer.
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
theme `transition` token through `host/tenfoot/anim`: Classic a curtain,
Neon a glitch/static burst, Sofa Dim a wipe. Overlays settle in under
400ms and do not block pad input. `transition` `none`, `-no-transition`,
or `FOGCAST_NO_TRANSITION=1` is an honest no-op. Attract does not arm while the pane or search OSK is open. Catalog cells paint decoded box-art from
`GET /api/v1/presentation/artwork/{handle}` when a catalog `Game.Cover` or a
presentation `cover_artwork_id` is present. The focused browse tile, split hero,
and title-detail cover prefer presentation `box3d_id` (LaunchBox Box-3D, then
Cart-3D, then Box-Spine, with a `library_media` RoleBox3D overlay that wins
when present). Missing 3D art uses a cheap CPU 3/4 perspective of the 2D cover
(`tenfoot.FauxBox`); missing both keeps the placeholder. Unfocused tiles stay
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
embedded Go Regular face (no kit system fonts) and truncate with an ellipsis
when the string exceeds the chrome or cell width. A paired, authenticated host listener
serves a restricted set of existing library, artwork, and session operations and a
session-bound input stream. The host keeps target and input lease ownership; the
adapter sends physical USB events through that stream to the retained virtual
pad. Kit input discovers every eligible USB gamepad (`event*` only), merges
polls in stable device-id order through `host/tenfoot/inputmap`, and remaps
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
package ID. Catalog schema v5 associates a stable ROM-less game entry with one
explicit package ID; importing a version does not select or activate it.
The `fpga` browse platform is not a cartridge/runtime system. Library scans
exclude its logical root. See [package library API and operations](core-package-library.md).

`fogcast/core_packages.go` owns installation, inspection and checked selection.
`internal/hostapi/core_packages.go` exposes those operations. Read-only target
`POST /v1/development/core/inspect` stages and invokes native `inspect_core`
without acquiring a physical lease or replacing input. The target runtime
remains the compatibility authority. Target transitions can report busy;
failed inspection cleanup cannot report compatibility success.

Package-backed `session/launch` uses the same confirmed-package transition as
the development loader, before ordinary game launch detaches prior input/media.
The service resolves selected immutable bytes under lifecycle admission, then
records the library identity only for the exact confirmed package generation.
Changing the selection affects future launches. Runtime status stays truthful;
the host adds its explicit library association and does not infer one after a
restart. Existing ordinary cartridge launch and Stop paths remain in place.

A confirmed package activation commits its host-side ownership only after the
previous host executor stops successfully. If that cleanup fails, the service
retains the host owner and a package recovery marker, reports the observed
FPGA package with the recovery error, and blocks input attachment. Stop or a
subsequent package launch retries target recovery and host cleanup before
clearing either pending owner.
