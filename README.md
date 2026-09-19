# FogCast

FogCast is a host application and MiSTer target agent for browsing and
launching a large multi-system game library. The host owns the UI, catalog,
and content selection; the MiSTer is a small, directly controlled target.

## What works now

- [Installed FPGA core packages](docs/core-package-library.md) with explicit
  version and immutable media selection in the normal library, with multiple
  titles per core. The browser's **Manage FPGA library** panel imports packages
  and media, creates titles, and explicitly changes their selections through
  the same host API as the CLI. Package/media changes need no rebuild or host restart. Import and
  selection do not replace a running core. Library media imports stream into
  bounded storage up to the host's 32 MiB policy; package capability queries
  report the separately enforced declared core limit: 16 KiB for legacy blob
  1.0, or a safe 32 KiB guarantee for required `fes.media.blob-stream` 1.0
  alongside required blob 1.0. Stream launch also validates the runtime's
  observed endpoint limits and package generation. Importing a larger asset
  does not make it runnable; these software paths do not establish SMS hardware
  or mapper compatibility.
  Library FES Pong packages with the
  persistence interfaces retain paddle speed and best rally on the target;
  [settings/progress APIs and CLI](docs/core-package-library.md#persistent-settings-and-progress)
  expose durable data. Library FES ZX81 is a volatile `fes.simple-computer`
  entry (`fes.keyboard`, no gamepad); `POST /api/v1/session/launch` with its
  `game_id` programs the package and attaches keyboard input. Development
  package loads remain volatile.

- Development media upload for an active described `fes.simple-computer` package
  with `fes.media.blob`: `fogcast --api http://127.0.0.1:8797 core-media PATH`
  sends 1..16384 raw bytes through that running host's existing session and kit
  lease. See [development media](docs/ARCHITECTURE.md#development-media-upload)
  for admission, identity binding and failure handling. This is a development
  operation; Coleco hardware acceptance is separate. A ZX81 upload resets
  execution, so wait for the BASIC prompt before entering `LOAD`.


- Thousands of catalogued games across the systems in
  `internal/systems/table.go`.
- Real FPGA game launches on the designated MiSTer Pi.
- Target `GET /v1/health` reports sealed `artifacts` from the installed
  `build-inputs` record (and appliance boot ticket when present). Host
  `GET /api/v1/health` includes `host` OS/arch/version identity and forwards
  those target artifacts as provenance. Connection compatibility uses the
  advertised target API contract (`v1`), not Git revision equality. Missing or
  unsupported API versions refuse launch; package ABI, media, input and
  persistence support are checked by the corresponding operation. Different
  host, agent or runtime revisions alone do not require an image update.
- Target-side content caching, input, stop, and active-core observation.
- Native targets advertise installed legacy-core availability. The shared host
  marks unavailable legacy platforms browse-only and rejects direct launch
  requests before dispatch; described FPGA package entries retain their own
  compatibility checks. Missing core files are also rejected by the agent
  before runtime mutation. A new valid package-library launch can recover a
  narrowly recognized retained idle launch error with one leased Stop, then
  activate once. Save failures, foreign ownership and reboot-required states
  remain explicit recovery failures; launches are never silently replayed.
  Kit footer text wraps recovery instructions and the selected title instead
  of cutting off the action the operator needs.
- Stable target identity and local DNS-SD reconnection after reboot/address
  changes. `GET /api/v1/session` reports the host session `id` and the bound
  FPGA `target`. `POST /api/v1/session/launch` may set `target` to bind that
  session without rewriting `selected_target`. A second configured target may
  play at the same time; `GET /api/v1/sessions` lists live plays. `GET
  /api/v1/session` is the foreground session. `selected_target` is the default
  session binding, not a process identity: it can change while idle even when
  remote input or media is enabled. In browser target
  settings, choose **Prepare identity**
  before assembling new media; the existing settings API accepts
  `PATCH /api/v1/library/settings` with `{"prepare_target":"dev"}`. An already
  running agent can bind its identity through authenticated health at the
  configured address. The browser and tenfoot distinguish connection state
  from game state. See [target reconnection](docs/ARCHITECTURE.md#target-identity-and-reconnection).
- Host API loading of arbitrary development RBF files, with automatic reboot
  recovery back to Menu for non-MiSTer cores on the conventional Main backend.
- Browser UI, local media previews, and host-emulator/remote-media modes.
- Linux hosts can provide the optional local session preview from a V4L2
  capture device through FFmpeg; configure the absolute device path in the
  private `media.capture_device` setting.
- Native SDL3 10-foot launcher (`cmd/fogcast-tenfoot`) with cover-grid, shelf,
  and list layouts plus scriptable [rooms](docs/rooms.md) (sandboxed Lua
  menu screens: overworld maps, single-console rooms, tag-driven
  cross-system views), that calls the same public host API, including a
  DIAGNOSTIC development-RBF path OSK (local file path, no browser picker).
  A kit-only host is `fogcast-api --headless --launcher-config`: catalog and
  session stay up without local capture or an SDL window, and `fogcast-kit`
  reconnects to the launcher listener.
  USB keyboard is first-class browse/nav (arrows/Enter/Esc/Tab; no gamepad
  required); USB mouse/pointer hover moves focus and primary click activates
  (select/launch/confirm) without a controller; on-screen hints and focus
  follow the last-used keyboard, mouse, or gamepad, a newly plugged device
  claims affinity without restart, and unplug returns to a remaining device;
  an attached play session forwards USB keyboard HID to the core/session path
  instead of the sofa graph (ZX81 still uses the matrix; native SNES/MD encode
  as gamepad buttons; Esc/Backspace still stop); pointer browse does
  not steal that session, and a foreign or recovery-required kit lease fails closed. Mac is the primary sofa target;
  Linux uses the same Makefile target with
  system SDL3 (`pkg-config sdl3`). Draw goes through `gfx.Device`: SDL3 is
  the production backend; Software is a pure-Go rasterizer for tests/CI;
  FPGA records a versioned FC2D command stream and rasters through Software
  (`-gfx fpga` / `TENFOOT_GFX=fpga`; `IsStub` true until a programmed 2D
  core exists — not HDMI FPGA UI); FPGA stub remains the thin Software
  wrapper without a stream (`fpga-stub`); linuxfb
  rasters with Software and Present-blits onto a 32bpp Linux framebuffer
  (`make build-tenfoot-linuxfb-spike`, CGO-free ARMv7, no SDL; the spike
  also reads evdev/joystick and moves a cursor). A sibling
  `make build-tenfoot-linuxfb-grid` paints a fake cover-grid on the same
  path (d-pad/stick highlight, South/Enter confirm, Start/ESC/Q quit; no
  catalog). See
  [docs/native-tenfoot-launcher/README.md](docs/native-tenfoot-launcher/README.md).
- A reproducible target image toolchain with a development image containing
  SSH and curl.
- A separate reproducible `native-dev` image that packages the native runtime,
  native agent backend, one locked idle RBF, and the selected Mega Drive RBF
  with optional sealed Pong, SNES and NES RBFs. FES integration can also add a
  selected set of validated format-2 packages (`fes.pong`, `fes.zx81`, and
  `fes.coleco`) through the closed package selection described in [the
  development guide](docs/DEVELOPMENT.md).
  Source-built Mega Drive selection is the native image default; use the
  explicit upstream selection for fallback. Its idle path and one-player Mega
  Drive launch, input, Stop, and immediate relaunch path are hardware-tested
  on the designated kit. See the dated
  [native Mega Drive baseline](docs/hardware/native-megadrive-baseline.md).

The native FES appliance also has a release/update client and a fixed bootstrap
with watchdog-bounded trial boots. These require FES bootstrap media; ordinary
direct-root images do not expose the update API. A card with leftover capacity
already formatted as `FESDATA3` bind-mounts cache, saves, core-data,
launcher-cache and evidence from that partition at agent startup unless
`GET /v1/update` shows trial, pending, or corrupt. Releases stay on the 1 GiB
FAT. The assembler still ships the fixed 1 GiB image; expanding leftover space
is a live-card mutation. Build the operator client with
`make build-fes-update`, then use `bin/fes-update --action status` with the existing
private host configuration. See [appliance updates](docs/appliance-updates.md).
Hardware acceptance of this new boot path is tracked separately from game tests.

The normal FPGA launch path is:

1. The browser sends a game ID to `POST /api/v1/session/launch`.
2. The host resolves the catalog entry and uploads content to the target when
   the target cache does not already contain it.
3. The target agent creates a transient MGL and writes
   `load_core <mgl>` to `/dev/MiSTer_cmd`.
4. The MiSTer/Main-compatible process loads the RBF and game.
5. FogCast observes `/tmp/CORENAME` for the active core. Stopping sends
   `load_core <menu.rbf>` through the same command path.

## Source boundaries

FogCast keeps its host applications and target agent in one Go module, with
the ownership visible in the source tree. The ten-foot sofa app is under
`ui/tenfoot` and the kit launcher is under `ui/kitlauncher`; their Go package
names remain `tenfoot` and `kitlauncher`. Shared drawing, input, theme, and
library helpers live under `ui/shared`, `ui/anim`, `ui/audioreact`,
`ui/fbgrid`, `ui/gfx`, `ui/inputmap`, `ui/linuxinput`, and `ui/theme`.
`ui/kitlauncher` does not import `ui/tenfoot`. Host catalog/config/library and
host-owned input bridges live in `host` and `internal/hostapi`;
`targetclient` owns host-to-target HTTP/cache/core/development transport,
endpoint reconciliation, and kit leases; target-side HTTP/cache coordination
lives in `internal/agent`; and local MiSTer integration lives in
`internal/mister`. The UI consumes host and target-client contracts but does
not own target handlers, runtime lifecycle, image assembly, or FPGA builds. The
FES parent selects this component revision and owns image integration and
release evidence.

## Target diagnostic evidence

The target agent keeps a bounded, in-memory diagnostic ring in the same process
that owns `/v1/kit/*`; it does not add a second daemon or a second ownership
control plane. An authenticated read of
`GET http://mister.lan:8182/v1/kit/debug/events?limit=10000` returns
`{"events":[...]}` using the event shape `{ts_utc, mono_ms, flight_id?,
lease_gen?, run_id?, layer, kind, severity, detail}`. `flight_id`, when present,
is the canonical host UUID v4 from #205; lease generations and run IDs remain
opaque join strings. The ring includes lease lifecycle events and the target/runtime
hooks that can be observed locally (FIFO dispatch, descriptor open,
CORENAME/Main transitions, and ownership/program/recovery fences). The native
adapter also drains `/run/mister-runtime.events.json` from mister-runtime into
the same ring when that dump is present. Unavailable Main or `fpga_manager`
observations are left absent rather than fabricated.

Before an intentional reboot, while holding the current kit lease, persist the
window with the authenticated target-agent call:

```sh
curl -H "Authorization: Bearer $KIT_TOKEN" \
  -H 'X-FogCast-Kit-Lease: <lease-token>' \
  -H 'Content-Type: application/json' \
  -d '{"run_id":"<run-id>","lease_gen":"<current-generation>","flight_id":"<host-flight-id-if-present>"}' \
  http://mister.lan:8182/v1/kit/debug/snapshot-before-reboot
```

The response points at a durable `/media/fat/fogcast/evidence/` directory with
`evidence_class: "diagnostic"`, the ring window, and bounded copies/metadata
for the journal, owner, `/tmp/CORENAME`, `fpga_manager`, and FAT-note paths.
Only after that snapshot should the existing leased development-reboot path be
called. The snapshot request requires the current opaque `lease_gen` and never
claims or mutates the kit by itself.

The fog-flight page polls the authenticated kit dump beside its
existing `/v1/health`, `/v1/kit/lease`, and `/v1/status` polls, render the
events by `layer`/`severity`, and join to host session events only when a
`flight_id` is actually present. It should preserve `run_id` and `lease_gen`
as opaque join fields, label vault results diagnostic, and continue to use the
host events endpoint at `:8787` when available; launcher `:8789` is not a
target-event join source. Tenfoot and the sofa browser stamp launch, stop,
focus, and nav with client wall + monotonic clocks. Host session events repeat
those client clocks next to host `ts_utc`/`mono_ms`; focus/nav also land on
`GET /api/v1/debug/ui-events`. fog-flight builds a per-flight latency
waterfall from client → host → target when both ends are present and leaves
missing layers labelled missing.

## On-kit controller launcher

The native image packages the CGO-free `fogcast-kit` adapter for the kit HDMI
display and USB controller. It uses an explicitly paired host listener and the
existing session/input ownership path; Select + Start held for one second requests
Stop and returns to the library. After a successful host catalog fetch it keeps a
last-good snapshot and cover files under `/media/fat/fogcast/launcher-cache/` on
FAT, separate from the ROM cache. Catalog refresh merges in place instead of
blanking the shelf; covers use a 512MiB LRU budget on that tree and never
evict the ROM cache. Prefetch is focus, then page, then next page, then strip,
then attract. `GET /api/v1/library/cache` reports ROM used/free from a
lease-free target inventory; cover used/free and last sync are kit-local.
Host games may include `rom_cached` when that inventory is reachable.
Power-on paints that shelf and visible covers
from disk before host games HTTP; an absent host shows `Offline - local library`.
Replacing the system image does not wipe this tree. D-pad and A still browse
that local shelf. Launch and Stop remain bound to the persistent host session
API and wait for host reconnect; the kit UI does not claim a target lease or
send direct target mutations while offline. D-pad browse does not take a lease.
Its live catalog opens as a living-room platform wheel
(horizontal clear-logo / wordmark strip plus a platform hero) and drops into
a catalog browse view through `ui/fbgrid`. The default is a small
4×3 cover grid; Y (North) cycles Grid → Coverflow (scaled focus row) →
Wall (6×3 mosaic) → Split (vertical clear-logo list plus hero) → Grid
without stealing D-pad browse or the X theme cycle. X (West) cycles
theme packs Classic → Neon → Sofa Dim → Classic without stealing Y or
D-pad; the last pack is stored in `launcher.json` `theme`. Coverflow keeps the
focused title largest and paints its name at the title role (or a clear logo
when one is ready). Wall uses caption labels on denser cells. Split keeps a
logo (or title) list on the left and a large cover plus short meta on the
right; Up/Down walk the list. Empty catalogs
hide tiles and keep chrome. Shoulder L/R (and Select)
cycle platforms on the wheel and still cycle system shelves in the grid
(`All` plus each system present in the loaded catalog); the
header shows the active shelf and counts (`MEGADRIVE 12/40`), plus `SEARCH`
when a query is filtering the shelf, plus `FLOW`,
`WALL`, or `SPLIT` when that layout is active. A/South on the
wheel enters that system's browse view; East/B on the browse view returns to the wheel
and closes search. Start opens living-room search on the current shelf (from the
wheel it enters that system's browse first) and reuses the existing gamepad OSK
(`ui/shared` OSK): D-pad moves keys, A types, L/R page letters/symbols, B
clears a non-empty query or closes, and Start/Done commits. The query is a
case-insensitive substring of the title, or of the clear-logo wordmark fallback
(system id) when the title is empty. An empty query restores the full shelf; no
matches paint an honest `No matches` stage and hide tiles, including the recent
strip. A committed query keeps that strip hidden so Down stays on the filtered
shelf. After the OSK closes, D-pad and A/B match browse on the filtered results;
exiting search restores the prior focus when that title is still on the shelf.
Start does not steal Y (layout), X (theme pack), Select (shelf), or
Select+Start (stop).
The focused platform paints hardware/fanart/backdrop when attract, presentation
`backdrop_artwork_id`, or a representative cover handle exists, otherwise a
theme-tinted placeholder, with game-count chrome plus a play rollup when host
games already carry `play_count` or last-played. Wheel cells use a
representative clear logo when presentation has `logo_id`, else a bold
wordmark. D-pad and left
stick move focus in
two dimensions on the grid and wall (left/right clamp on the row; up/down by
the layout column count, paging when `Focus` leaves the visible page).
Coverflow is one row: left/right walk titles, and down that cannot move further
enters the recent strip or title pane. Split is one column: up/down walk
titles, left/right clamp, and down that cannot move further enters the strip
or title pane. A short ease-in-out pop grows the
focused tile's highlight ring (~1.06 scale, ~160ms) when focus changes;
confirm is a white pulse that eases out over `ConfirmFrames` rather than a
flat flash. Browse paints a dimmed fanart/backdrop behind chrome when presentation
`backdrop_artwork_id` or an attract backdrop exists; otherwise a soft
cover-wall of decoded covers, or the solid theme background when no art is
present. Focus rings and header/footer stay opaque. A soft theme-token
vignette darkens the stage edges on the wheel, browse grid / coverflow /
wall / split, and title pane; Neon and Sofa Dim also paint a thin bezel
frame (`bezel_width`, default off on Classic). `vignette_alpha` `0` in a
theme file turns the vignette off. When the host session is `active`,
`launching`, `stopping`, or `failed`, a dimmed pause overlay paints a
`Paused` / session badge plus the existing Select+Start stop hint; East/B,
Start, and Guide stay on the input map (Select+Start still stops; B does
not). The overlay is linuxfb menu chrome, not a fake HDMI mirror. A Recent / Favorites strip paints under the grid when host
`collection=recents` or `collection=favorites` returns at least one title
(recents first, then favorites, labeled honestly); an empty row is omitted.
Down that cannot move focus further (last catalog row) enters that strip, or
opens a focused title pane when the strip is hidden
(large cover, title, and platform/year/genre/studio/players/region when the
catalog or presentation already carries them, plus a wrapped `summary`
description). Compact chips paint on grid, coverflow, wall, and the title pane
for players, rating, completion, and portable when those presentation fields
exist (portable also uses handheld catalog systems); missing chips stay hidden.
The title pane still omits play-count and last-played as body copy; the
platform wheel rolls those up from host `play_count` / `last_played_at` (or
recents order) when admitted. D-pad L/R move among strip
tiles, A opens that title's pane, and B or Up return to the grid.
A/South still launches from the grid. The pane's A plays the title, East/B
and Up return to the same shelf and focus, and L/R (or shoulders) cycle
screenshots when `screenshot_ids` has two or more. Presentation `marquee_id`
(LaunchBox Arcade-Marquee or Banner, or a `library_media` RoleMarquee overlay
that wins when present) paints a wide strip under the header; missing handles
hide the strip rather than reserving an empty band, and cover, meta, badges,
and the screenshot/video slot keep their existing layout. When presentation
`series`, `related`, or `collection` names at least one other loaded catalog
title, a Series strip paints at the bottom of the pane (and in the Split
hero). Down (detail) or Right (split) focuses it; A opens that title; B
returns; the row hides when no sibling exists. When presentation
`video_id` (library_media video overlay) is present, the pane shows an honest
motion preview: it auto-cycles those screenshots plus backdrop/cover posters
under a VIDEO badge and a `preview` caption. The CGO-free kit path does not
decode H.264. Titles without a video handle keep today's still carousel.
Opening the pane, showing or hiding attract, entering or leaving the
platform wheel, switching layout or theme pack, and opening or closing
search play a short
theme-driven overlay: Classic a curtain, Neon a glitch/static burst,
Sofa Dim a wipe (`ui/anim`, under 400ms). `transition` `none`
in a theme file, `-no-transition`, or `FOGCAST_NO_TRANSITION=1` is an
honest no-op. Pad input is not held while the overlay paints. Attract does not arm
while the pane or search OSK is open. Missing description copy is omitted rather than
drawn as an empty box. Cells show host cover art when a catalog
`Game.Cover` or LaunchBox/IGDB presentation cover handle is available,
Catmull–Rom downscaled at decode, with a theme-tinted
placeholder (lettermark when missing, a distinct panel while loading) instead of
a flat system fill. The focused browse tile, split hero, and title-detail
cover prefer presentation `box3d_id` (LaunchBox Box-3D, Cart-3D, or Box-Spine,
or a `library_media` RoleBox3D overlay that wins when present). Missing 3D art
uses a cheap CPU perspective of the 2D cover; missing both hides the 3D look
and keeps today's placeholder. Unfocused tiles stay 2D covers. Neon and Sofa
Dim paint a thin theme-driven cabinet/bezel around that focused art; Classic
leaves the extra chrome off. Presentation `logo_id` (LaunchBox Clear Logo, or a
`library_media` RoleLogo overlay) paints on the detail title and grid label
bar; missing logos keep the existing bold/regular text labels. The visible page (12 on the grid, 5 around coverflow focus, 18 on the wall, 8 around split focus)
and a cheap next window prefetch those
handles asynchronously; missing metadata still uses the placeholder. After the host attract `idle_seconds` with no pad input, the
kit shows an attract stage (title chrome plus backdrop/cover artwork)
and returns to the same shelf and focus on any input. When the staged title has
a distinct `marquee_id` or attract `marquee` handle, a banner strip paints
under the header alongside the still or motion preview; a marquee-only row
keeps today's still fallback and hides the duplicate strip. When the staged title has
a video handle plus stills, attract auto-cycles those stills under a VIDEO badge
and a `preview` caption — the same kit-safe motion path as title-detail, not
H.264 decode. Four or more stills-backed titles with at least one video handle
paint a 2×2 wall of neighboring stills with the staged tile highlighted. Titles
without a video handle keep today's stills attract; an empty playlist uses a
themed idle panel instead of a frozen grid. Full clip playback is a follow-up.
Attract (and browse, when a measured 0..1 file is supplied) can paint a
theme-highlight edge pulse. The designated kit has only an ALSA Dummy card,
which is not FPGA HDMI audio; there is no host session level either. Chrome
stays off unless `-audio-chrome`, `launcher.json` `audio_chrome`, theme
`audio_chrome`, or `FOGCAST_AUDIO_CHROME=1`. With the gate on and no meter,
attract uses a quiet labeled `idle pulse` rather than claiming game audio.
`-audio-level-file` (or `FOGCAST_AUDIO_LEVEL_FILE`) is the measured injector.
Title-detail video handles use the screenshot/poster preview above. Header, tile names, placeholder lettermarks, and footer use the embedded Go
UI faces through `gfx.DrawText` / `gfx.DrawTextWeight` (CGO-free; no system
fonts on the kit) at theme typography roles `title_px` / `body_px` /
`caption_px` / `status_px` (legacy `header_scale` / `label_scale` /
`status_scale` still map to `8*scale` when a role is unset). Built-in themes
paint title and chrome header with Go Bold (`title_bold`, default true);
body, caption, and status stay Go Regular unless a matching `*_bold` token
is set. Paint
tokens (background, highlight, flash, system palette, header/footer chrome)
come from `ui/theme`: built-in `default` / pack **Classic** match
today's kit look, and **Neon** (`arcade`) / **Sofa Dim** (`night`) (or a
JSON/TOML file) swap colours, type roles, chrome accents, and scene
transitions without forking UI code. Select with `-theme`, `theme` in
`launcher.json`, or `FOGCAST_THEME`; on the kit, X (West) cycles the three
packs at runtime.
The
grid is a view of `kitlauncher.Model` and does not own host requests, input
leases, or FPGA transitions. Kit input opens every eligible USB pad, merges
their polls, and applies a JSON remap profile from
`ui/inputmap` (default **identity** preserves A=launch and
Select+Start=stop). The FogCast virtual pad, virtual-bus devices, and
`/dev/input/js*` duplicates stay excluded. See [kit launcher](docs/kit-launcher.md). Exact image and hardware
evidence belong to FES.

## Built-in native Pong (software integration)

Pong appears in the host catalog as game ID `pong` without adding a library or
ROM. Launch it through the existing browser/session path, or
`POST /api/v1/session/launch` with `{"game_id":"pong"}`. It requires the native
runtime and installed `/usr/share/mister-runtime/cores/pong.rbf`; the Main
backend does not support this ROM-less profile. Supplied media is rejected.
This integration is software-tested; Pong RBF/image packaging and playable
hardware acceptance is recorded separately by the FES integration task.

## Native SNES (software integration)

SNES uses the existing library, cache, and session launch path with the native
runtime and installed `/usr/share/mister-runtime/cores/snes.rbf`. FogCast passes
one unchanged cartridge path (`.sfc`, `.smc`, or `.bin`); the runtime owns format
validation, copier-header handling, and the required metadata transfer prefix.
The initial runtime contract is ordinary LoROM/HiROM up to 4 MiB; enhancement
chips, external firmware, and expanded mappings remain outside this slice.
The native agent stores ordinary SNES battery saves per game and ROM beneath
`/media/fat/fogcast/saves/snes`. The runtime restores them on launch and flushes
them before a clean Stop, including system switching and lease cleanup. Saves
survive cache eviction and target reboot; a write failure keeps Stop retryable
and prevents successful lease release. Stop before rebooting: there is no
power-loss autosaving, host synchronization, or save-state support. Cartridge
copier-header variants have separate save identities. This persistence path is
software-tested; hardware validation belongs to the selected FES integration. The native SNES package uses cartridge index 1; the native NES
package uses filetype index `0x40`. The conventional Main selectors remain
zero based (including index 0 for NES).

The retained native gamepad includes A/B/X/Y/L/R/Select/Start and the D-pad.
Existing MD C and Start codes keep their meaning. Host event normalization,
lease delivery, and disconnect/Stop neutralization use the existing input path;
this does not add a browser gamepad-capture UI. Tests use fake runtime/target
and uinput calls; exact-image SNES hardware acceptance remains separate.

## Development RBF path

`POST /api/v1/session/development-rbf` accepts one bounded
`application/octet-stream` body. On the conventional Main backend, FogCast
installs it temporarily and loads it through `/dev/MiSTer_cmd`; Stop uses the
existing reboot-required recovery handshake.

The native backend supports the same API path for the existing
MiSTer-compatible development ABI. It atomically stages the upload at
`/tmp/fogcast-development/core.rbf`, asks `mister-runtime` to power down HDMI,
program the FPGA, synchronize the core, and report development state without a
game or system identity. HDMI stays down until Stop reloads the locked idle
RBF. Raw development uploads have no video or input guarantee. The native
image packages no development RBF. Its Mega Drive RBF is selected at build
time as described below. The
exact two-cycle acceptance and legacy rollback evidence is recorded in
[native-development-rbf-baseline.md](docs/hardware/native-development-rbf-baseline.md).

Format-2 `.fcore` development packages use
`POST /api/v1/session/development-core` with a bounded
`application/octet-stream` body. FogCast negotiates runtime protocol 2,
validates package compatibility before mutation, and reports the active package
identity, ABI, build ID, interfaces, capability flags, and generation in the
session response. Input is enabled only when the active verified package
provides `fes.gamepad`; raw development RBF input stays disabled. The command
line provides `fogcast core-inspect PATH` for local inspection without a kit
connection and `fogcast core-load PATH` for a mutation owned by the running
host session. Ordinary `fogcast launch`, `status`, and `stop` use the same
persistent `/api/v1/session/*` API and origin precedence; they no longer open a
short-lived service or emit the old `{status,content}` launch envelope.
`core-load` calls the host API selected by `--api`, then
`FOGCAST_API`, then `http://127.0.0.1:8787`; it does not open an independent
target service. The archive inspection and upload use the same bounded byte
snapshot. Success requires the returned package ID, ABI, build ID, positive
generation, and required active interfaces to match that inspected package.
There is no browser file picker. Tenfoot types or pastes a local path with
the gamepad OSK and POSTs the file bytes.

## Native Mega Drive RBF selection

Source-built Mega Drive selection is the native image default; use the explicit upstream selection for fallback.

The native image resolves one sealed two-file bundle before Buildroot. The
bundle contains `megadrive.rbf` and its closed `megadrive-rbf.toml` provenance
record. Both files must be regular, sealed (no write bits), and byte-matched to
the declared source revision, recipe, toolchain, size, and SHA-256. The bundle
path is supplied by the operator and is never embedded in the image.

Build with the source-built default from a FES checkout:

- `make build MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle`

Build using the locked upstream release explicitly:

- `make build MEGADRIVE_RBF_SOURCE=upstream`

There is no automatic fallback between the two RBF selections. Both modes
install exactly one Mega Drive RBF at the same role path and keep runtime
launch, Stop, video, media, and input behavior unchanged. Both selections use the MiSTer ABI; generalized/custom/non-MiSTer RBF ABI support is deferred.

The selection and provenance path is software-tested. Qualification of the
source-built artifact on the designated kit is a separate later hardware gate;
the existing hardware baseline does not silently qualify a different RBF.

## Repository boundaries

| Repository | Owns |
| --- | --- |
| `FogCast` | Host application, browser UI, catalog, target agent, content transfer, and launch requests |
| `Main_MiSTer` | The MiSTer/Main implementation used by the target image |
| `misteross` | Quartus, Verilator, and open-source FPGA builds that produce RBF files |

Native image assembly lives in the FES `image/` recipe. FogCast keeps the
agent, kit, extra-core selector and native-runtime lock as inputs. Git
history is the archive for superseded experiments.

The conventional `dev`/`prod` image remains the broad FPGA game and
development-RBF path described above. The separate `native-dev` image supports
only registry system `megadrive` for catalogue launches: it sends the
image-owned core and staged cartridge path to `mister-runtime` and rejects
every other catalogue system. Its separate MiSTer-compatible development-RBF
path is hardware-tested for the narrow MiSTer-compatible load/Stop lifecycle
and subsequent game regression. The accepted game slice is one player with
D-pad, A/B/C, and Start. Audio, Mega Drive saves, six-button input, multiplayer,
remapping, hot-plug recovery, generalized RBF ABIs, and development video/input
remain unsupported.

## Milestone status

```text
legacy dev/prod = current game-capable path
native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch
Milestone 2 = complete
Milestone 3 = complete for the defined one-player Mega Drive vertical slice
native development RBF = hardware-tested MiSTer-compatible load/Stop/game-regression path
Milestone 4 = complete for the defined MiSTer-compatible development lifecycle
```

## Build and test

The independent `appliance/` Go module owns release manifests and image storage.
The root module uses its local source through `go.mod`; no extra checkout is
required. Use `make test` and `make vet` to check both modules. Direct root
`go test ./...` excludes the nested module; check it with
`(cd appliance && go test -race ./...)` when running Go commands manually.

```sh
make build
make test
make vet
git diff --check
```

For the target image, fixture details, deployment, and live launch checks,
read [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). The current process
boundaries and source entry points are in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

Native image assembly defaults to Mega Drive; the explicit
`NATIVE_RUNTIME_SYSTEMS="megadrive pong snes nes"` selection adds sealed
Pong/SNES/NES source bundles through the same builder and verifier. See the
[image development guide](docs/DEVELOPMENT.md#optional-pong-snes-and-nes-image-cores).

Native image assembly lives in the FES `image/` recipe. FogCast supplies the
agent, kit launcher, extra-core selector (`cmd/target-image-lock`) and
`build/native-runtime.inputs.lock.toml`. `TARGET_IMAGE_LOCK_BIN` remains an
explicit verifier override.
