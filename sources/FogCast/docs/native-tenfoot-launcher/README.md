# Native 10-foot launcher

SDL3 10-foot launcher (Mac primary; Linux build path documented). It talks to
the existing FogCast public host API over HTTP. It does not own catalog,
content transfer, or `/dev/MiSTer_cmd`. The browser shell remains the default
UI. Fullscreen living-room use applies a local TV overscan inset and idle
stills-or-video attract from the host playlist. Browse layouts are cover grid
(default), shelf/carousel, and list. Proposed composition readiness (a core
package is not by itself Ready) is in
[`launch-composition.md`](../launch-composition.md).

Linux SDL3 packages, native-on-Linux build/run, and the Mac-vs-Linux proof
split are in [`LINUX.md`](./LINUX.md).

## Build

**Mac:** Homebrew `sdl3` and `pkg-config` are required.

```sh
make build-fogcast-tenfoot
```

That builds `bin/fogcast-tenfoot` with `-tags sdl3`. `TENFOOT_CGO_ENV` on
Darwin includes `MACOSX_DEPLOYMENT_TARGET=11.0` and `-mmacosx-version-min=11.0`.

**Linux:** install a distro SDL3 development package so `pkg-config --modversion sdl3`
succeeds (`libsdl3-dev`, `SDL3-devel`, or `sdl3` — see [`LINUX.md`](./LINUX.md)),
then the same target:

```sh
pkg-config --modversion sdl3
make tenfoot-cgo-env    # expect CGO_ENABLED=1, no Darwin flags
make build-fogcast-tenfoot
```

Do not cross-compile the SDL3 binary from macOS (`GOOS=linux` + cgo needs a
Linux sysroot). Native-on-Linux is the supported path.

## 2D graphics device

Cover grid, labels, attract, now-playing, and session preview draw through
`ui/gfx.Device`, a small 2D bitmap API:

- frame lifecycle: `BeginFrame` / `Clear` / `Present`
- textures: create/update/destroy from `*image.RGBA` (RGBA8), opaque handles
- draw: textured quad (dst rect, optional src rect), solid fill rect, CGO-free `DrawText` / `DrawTextWeight` (embedded Go Regular and Go Bold), and `DebugText` (8×8 HUD)
- letterbox logical size and VSync are backend concerns
- GPU park destroys textures individually (preview is the parked exception)

UI helpers in `ui/tenfoot/draw.go` do not call `SDL_Render*` or
`SDL_CreateTexture`. Window creation, events, gamepad, mouse, and text input stay
in `ui/tenfoot/sdl.go` until a later slice.

| Backend | Construction | Role |
| --- | --- | --- |
| SDL3 | `gfx.WrapSDLRenderer` (`sdl3.go`, `-tags sdl3`) | Default production path. Wraps the process `SDL_Renderer` with `SDL_LOGICAL_PRESENTATION_LETTERBOX` and VSync. |
| Software | `gfx.NewSoftware` (`software.go`) | Pure-Go RGBA8 rasterizer for tests and CI (no cgo, no SDL). Nearest-neighbour blit; `Snapshot` for golden pixels. Cover/screenshot/still downscale is Catmull–Rom at decode, not in Draw. |
| FPGA | `gfx.NewFPGA` (`fpga_device.go`) | Records the versioned FC2D command stream (`fpga_protocol.md`) and rasters through Software. `BackendName` is `fpga`. `IsStub` stays true; this is not HDMI FPGA UI. Attract still/crossfade and sprite helpers: `ui/anim`. |
| FPGA stub | `gfx.NewFPGAStub` (`fpga.go`) | Thin Software wrapper without a command stream (`fpga-stub`). `IsStub` is true. Does not talk to kit, runtime, or RBF. |
| linuxfb | `gfx.OpenLinuxFB` / `gfx.NewLinuxFB` (`linuxfb.go`) | Software rasterizer; `Present` blits onto a 32bpp Linux framebuffer (`/dev/fb0`) with stride and BGRX. Kit spike: `make build-tenfoot-linuxfb-spike` (`CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`, no SDL3 tag). The spike reads `/dev/input/event*` and `js*` through `ui/linuxinput` (pure Go evdev/js) and moves a cursor; Start/ESC/Q (JS button 7/9) quits. Fake cover-grid: `make build-tenfoot-linuxfb-grid` (`cmd/tenfoot-linuxfb-grid`) on the same path with hardcoded tiles; d-pad/stick moves highlight, South/Enter/JS 0 confirms, Start/ESC/Q quits; `-theme` selects the shared look tokens. |

`gfx.Recorder` is a call-order test double and does not draw pixels. Optional
`TENFOOT_GFX=software|sdl|fpga|fpga-stub` (or `Options.GFX` / `-gfx`) selects a
Device inside the SDL window shell; unset keeps WrapSDLRenderer. `fpga` is
software-replay of the FC2D stream, not a programmed 2D core. `linuxfb` is not
opened from that shell; run `cmd/tenfoot-linuxfb-spike` or
`cmd/tenfoot-linuxfb-grid` on the kit.

## Run

The host API must already be listening. Default base URL:

```text
http://127.0.0.1:8787
```

```sh
bin/fogcast-tenfoot
bin/fogcast-tenfoot -fullscreen
bin/fogcast-tenfoot -height 480
bin/fogcast-tenfoot -api http://127.0.0.1:8787
bin/fogcast-tenfoot -safe-area 0.05
bin/fogcast-tenfoot -safe-area 0
bin/fogcast-tenfoot -layout shelf
bin/fogcast-tenfoot -layout list
bin/fogcast-tenfoot -no-attract
bin/fogcast-tenfoot -smoke -api http://host.docker.internal:8787
FOGCAST_API=http://127.0.0.1:8787 bin/fogcast-tenfoot
FOGCAST_API_HOST=127.0.0.1:8787 bin/fogcast-tenfoot -api http://host.docker.internal:8787
FOGCAST_TENFOOT_NO_ATTRACT=1 bin/fogcast-tenfoot
FOGCAST_TENFOOT_SAFE_AREA=0 bin/fogcast-tenfoot
bin/fogcast-tenfoot -input-profile identity
FOGCAST_INPUT_PROFILE=swap-ab bin/fogcast-tenfoot
bin/fogcast-tenfoot -theme arcade
FOGCAST_THEME=night bin/fogcast-tenfoot
bin/fogcast-tenfoot -debug-hud
FOGCAST_DEBUG_HUD=1 bin/fogcast-tenfoot
bin/fogcast-tenfoot -gfx fpga
TENFOOT_GFX=fpga-stub bin/fogcast-tenfoot
```

`-debug-hud` (or `FOGCAST_DEBUG_HUD=1`, or `tenfoot.json` `debug_hud`) paints
an optional corner overlay with the current `flight_id`, kit lease generation
and TTL, and last launch/stop error. It is off by default and uses the active
theme's status/caption tokens. Launch, stop, focus, and nav actions send
client wall + monotonic clocks on the existing host session path and to
`POST /api/v1/debug/ui-events`.

Look tokens (`ui/theme`) are shared with the kit grid. `-theme`
selects a built-in or pack name (`default`/`classic`, `arcade`/`neon`,
`night`/`sofa-dim`) or a JSON/TOML file;
`tenfoot.json` may store `theme`, and `FOGCAST_THEME` is the env fallback.
`default` / Classic keeps the sofa and attract clear colours. This slice applies the
loaded theme to those `Clear` sites; kit `fbgrid.Paint` consumes the full
token set, including typography roles (`title_px` / `body_px` / `caption_px` /
`status_px`, with `*_scale` fallback) and title/header Bold (`title_bold`,
default true on built-ins). Packs also select a kit scene overlay
(`transition`: Classic curtain, Neon glitch, Sofa Dim wipe). On the kit, X (West) cycles the three packs at
runtime and writes the last pack to `launcher.json`.

Default overscan inset is **5% of each edge** (`-safe-area 0.05`). Windowed debug
can pass `-safe-area 0`. `-` / `=` nudge the inset by 0.5 percentage points
(clamped 0–20%) and persist to `tenfoot.json` under the user config dir
(`~/Library/Application Support/FogCast/` on Mac;
`$XDG_CONFIG_HOME/FogCast/` or `~/.config/FogCast/` on Linux). Inner cover
padding is unchanged and sits inside that gutter.

Sofa layout defaults to **grid**. `-layout shelf` or `-layout list` override
the saved pref for that run; gamepad **Select/View** (SDL Back) and
keyboard `l` cycle grid → shelf → list → grid. The choice is stored
in the same `tenfoot.json` as `layout`. Shelf is one row of larger covers:
Left/Right move one title, Up/Down jump by a visible page of covers. List is
vertical rows with a thumb, title, and system: Up/Down move one row, Left/Right
jump by a visible page of rows. Launch, stop, browse, favorites, search, views,
sort, platform, detail pane, GPU park, attract, and safe-area keep working in
every layout.

Attract mode starts after the host `idle_seconds` from
`GET /api/v1/library/attract` (default 60s) with no input, no modal search/view
picker, filter overlay, or settings overlay, and no active host session. The client hydrates that
idle threshold before arming the timer. A sofa settings PATCH of
`attract_idle_seconds` updates the same idle timer. Playlist rows with a `video` handle play first (Darwin
AVFoundation, including the track `preferredTransform`; Linux optional ffmpeg
CLI when `ffmpeg` is on PATH). Short clips play through, then the playlist advances; a single-item playlist restarts from the
already-fetched local file and keeps the last frame on screen. Clips longer
than 60s are capped at 60s. Missing, failed, or unsupported video falls back
to stills (backdrop, else cover, else marquee) for that row or the next
playable row. Video-only rows play when decode works. Stills cycle about every
12s. The renderer re-uploads only when a new video frame is ready. Any
gamepad activity or debug key dismisses, tears down the decoder (including
queued open results), and resets the idle timer. South/A on a launchable
attract item dismisses and launches. A South/North hold that began before
attract does not launch the attract title on release. `-smoke` implies
`-no-attract`. Linux without `ffmpeg` skips the video download and keeps the
stills fallback. GUI video smoke on a Linux display is NEED. The on-kit
`fogcast-kit` adapter arms the same host idle and paints stills, or a kit-safe
screenshot/poster motion preview when the staged title has a video handle
(no H.264 decode; full clip playback is a follow-up). Four or more stills-backed
titles with a video handle paint a 2×2 attract wall. Any pad input returns to
the platform wheel or catalog grid. The kit
opens on a platform wheel (`fbgrid.PaintWheel`) and A enters catalog browse
(default 4×3 grid); East/B on browse returns to the wheel. Y cycles Grid →
Coverflow → Wall → Split → Grid. X (West) cycles theme packs Classic → Neon → Sofa Dim
without stealing Y or D-pad; the last pack is stored in `launcher.json`.
Detail, attract, wheel, layout, and pack cuts play that pack's short overlay
(curtain, wipe, or glitch) without holding pad input; `transition` `none`
disables it. Kit attract can paint a theme-highlight edge pulse when
`-audio-chrome` / `audio_chrome` is on. Without a measured 0..1 level file
that pulse is an attract-only `idle pulse` (the kit Dummy ALSA device is not
FPGA HDMI audio). Built-in packs leave it off.
A Recent / Favorites strip paints under the grid when those host collections
return titles, and hides when they are empty. Last-row Down enters the
strip; A opens the title pane; B or Up return to the grid. The kit title pane is a
sibling `fbgrid.PaintDetail` over the same catalog focus: last-row
Down opens it when the strip is hidden, East/B and Up return, and A still launches. The pane
shows admitted genre/year/players/region/studio plus wrapped `summary`
description when those fields exist; it omits empty copy. Compact chips
paint on kit tiles and the pane for players, rating, completion, and
portable when presentation (or a handheld catalog system) already carries
them. Play-count and last-played stay off the pane body; the platform
wheel rolls them up from host games when those fields are admitted. A presentation `video_id`
paints a VIDEO badge and cycles screenshot/poster stills as an honest
motion preview (no H.264 decode on the CGO-free kit).

USB keyboard is first-class browse/nav on the SDL path (no gamepad required):
arrows move focus, Enter launches or confirms, Esc backs out, Tab opens search
(or confirms an open search; Shift+Tab opens filters, or pages an OSK). In the
detail pane Tab cycles screenshots when more than one is present, otherwise it
launches; Shift+Tab steps back. USB mouse/pointer is first-class on the same
path: hover moves focus, and primary click activates (launch on the shelf or
detail pane, type/confirm on the search OSK). Clicking empty space does not
launch the previously focused title. Keyboard and mouse coexist; a gamepad is
not required. Letter shortcuts already patterned stay. On-screen hints and
focus ownership follow the last-used keyboard, mouse, or gamepad. Plugging a
keyboard, mouse, or gamepad claims affinity without restarting tenfoot; unplug
returns hints to a remaining device. While
an attached play session is live, sofa keys forward to
`POST /api/v1/session/input/event` instead of the focus graph (ZX81 still uses
the matrix), pointer browse does not steal that session, and a foreign kit
lease fails closed.

Gamepad remains a supported control path (d-pad / left stick to move, South/A to
launch, East/B to back, Start to quit, Select/View to cycle layout, Guide to
open the sofa settings overlay). `-input-profile identity|swap-ab|/path.json`
applies the shared `ui/inputmap` remapper after SDL button
normalization (`CommandFromLogical`); empty is identity. Kit multi-device
merge is the HIL proof; sofa still opens every SDL gamepad. Down from the last row (or last shelf/list
page) opens the focused title's detail pane; Up or East/B returns to browse.
Left/Right in the pane cycle screenshots. South/A still launches, East/B still
stops a live session, Start still quits, Guide still opens settings, North/Y
still opens search, and Select/View still cycles layout.
Shoulders cycle the platform filter
(All, then each host platform). West/X tap cycles sort (title, recently added,
system) while browsing. Hold West/X opens the sofa filter overlay (genre,
year, region, hide prerelease, hide hacks). Keyboard `g` (or Shift+Tab)
toggles the same overlay. D-pad moves rows, South/A confirms, East/B backs out of a list
or closes. Genre and year options come from `GET /api/v1/library/facets`;
region uses the web dump-region tokens (USA, Japan, Europe, …, Other). Empty
facet lists still offer Any plus an honest empty hint. Changing a facet
reloads the catalog; Clear restores unfiltered browse and keeps platform,
collection, sort, and search. Hide toggles are session-local (not stored in
`tenfoot.json`) and default off. Shoulders stay platforms; they are not
rebound to filters. Select/View still cycles layout, Guide still opens
settings, North/Y still opens search, and hold North still favorites.
While an FPGA-native session is now-playing, West/X
attaches or detaches remote input; East/B remains Stop, Start remains Quit,
and Guide remains settings. Hold West does not open filters while now-playing. On Recent, West/X cycles last played, title, and
system; on Recently
added it cycles recently added and system, matching the host. North/Y opens
search with a gamepad on-screen keyboard (letters/digits, space, backspace,
clear, done). D-pad moves keys, South/A types the focused key, shoulders
switch letters/symbols, East/B clears a non-empty query or closes, Done or
physical Enter applies the pending query. A physical keyboard still types in
parallel. Select/View layout cycle and overscan nudge are ignored while the
OSK is open; Guide dismisses it and opens settings. Hold South/A (≥450ms) to
open the library view list (All, Continue, Favorites, Recent, Unplayed,
Recently added, then custom
shelves, then **New collection...**); d-pad moves, South/A opens the
highlighted view or starts a collection name OSK on New collection, East
cancels. On a **custom** shelf, West/X adds or removes the focused title
(optimistic; a failed host call reverts and shows a short status line).
North/Y opens manage (Rename / Delete). Rename uses the same gamepad OSK,
prefilled. Delete asks for South confirm / East cancel. Favorites and other
smart rails are not renamed or deleted. After deleting the active custom
shelf, the sofa returns to All. Attract does not arm while the view picker,
manage/confirm, name OSK, search OSK, filter overlay, settings overlay, or an in-flight
remote-input attach/detach is open. Hold
North/Y on the grid to favorite or unfavorite the focused title. While a host session is active,
East/B stops it (`POST /api/v1/session/stop` with `retain_lease: true`); Start still quits the app.
West/X attaches or detaches remote input when the session is `active` with
`execution=fpga_native` and input is not `starting` or `reconnecting`. Browse
header and now-playing chrome prefix `host unreachable`, `kit unreachable`, or
`kit not ready` from `GET /api/v1/health` (and `GET /api/v1/status` 503
`TARGET_UNAVAILABLE`) so a down kit is obvious inside the TV safe-area.
USB keyboard browse/nav: arrows/WASD (S is stop, not down),
Enter to launch (or confirm search), Esc/Backspace to back (or stop while a session is active, including while play HID is attached; letter `s` stays a ZX81/core key on that path),
Tab to open search (Shift+Tab opens filters; Tab confirms an open search),
Q to quit, `[` / `]` for platform, `x` for sort (or add/remove on a custom
shelf while the view picker is open; attach/detach while now-playing), `/` or `f` for search (or manage a
custom shelf while the picker is open), `c` / Shift+`c` to cycle views,
`v` to favorite, `l` to cycle layout,
`o` to open settings, `g` to open the filter overlay, `-` / `=` to nudge the overscan inset. Down arrow still
moves focus when idle, and opens the detail pane from the last row. USB mouse
browse/nav: move the pointer over a cover, list row, overlay row, or OSK key to
focus it; primary click launches or confirms. Mac tenfoot
is bring-up; the target product path is Pi kit tenfoot with a real USB keyboard
and mouse.

## Sofa settings

Guide (keyboard `o`) opens a gamepad-first overlay inside the TV
safe-area. Up/Down move rows, Left/Right change the focused row, South/A
confirms, East/B closes. Attract does not arm while the overlay is open.
Select/View still cycles layout and `-` / `=` still nudge overscan, including
while the overlay is closed.

Local prefs write immediately to `tenfoot.json`:

- Layout (`grid` / `shelf` / `list`)
- Safe-area inset
- Attract on/off (local gate only). `-no-attract` and
  `FOGCAST_TENFOOT_NO_ATTRACT` still force attract off for debug and smoke.
- Reduced motion on/off. `FOGCAST_TENFOOT_REDUCED_MOTION` /
  `FOGCAST_REDUCED_MOTION` honour an existing preference and freeze
  decorative room animation (focus pulse, tweens, drifting art).

Host fields load from `GET /api/v1/library/settings` and save with
`PATCH /api/v1/library/settings` on confirm: attract idle seconds, preferred
regions (usa / world / europe / japan), selected target, target list, and
library roots. The overlay lists each target as name, address, enabled, and
agent status (`stored` / `not set` / `will set` / `will clear`). South/A
edits name or address with the gamepad OSK, toggles enabled, or opens a
password-style agent OSK (masked while typing; never shown after commit).
Left/Right on agent marks a stored secret for clear (`agent: ""`). West/X
removes a draft row. Add target appends a disabled row; Save targets PATCHes
`targets` as a full-array replace and includes `selected_target` only when
that name changed. PATCH omits `agent` unless the operator edited or cleared
it. GET never returns the secret (`agent_configured` only). Removing or
disabling the selected target while a session is active is blocked in the
sofa so an active session is not stranded. The overlay still lists each
library root as system label plus path. South/A opens the gamepad OSK to
edit a path, Left/Right cycle the GET `systems[]` list, West/X removes a
draft row, Add library appends a row, and Save libraries PATCHes only
`libraries` (it does not send `targets`). **DIAGNOSTIC RBF** opens a path
OSK; tenfoot reads that local file and POSTs `/api/v1/session/development-rbf`.
That load is not a game session, and HDMI/input may be down. A successful libraries save
reloads the catalog. A failed PATCH keeps the previous values and reports a
short status line. Changing `selected_target` can fail while a session is
active.

On Mac, run from a GUI terminal for the Cocoa window. On Linux, use a session
with X11 or Wayland for windowed/fullscreen. Headless agent sessions fall
back to SDL's dummy video driver; the cover grid, gamepad path, and host
launch still run.

Automated proof against a live host (Mac mini; same flags on Linux):

```sh
make tenfoot-smoke
```

## Host API used

- `GET /api/v1/platforms`, retried with backoff if the first fetch fails.
  The chrome status reports `platform list failed` until a list arrives;
  shoulders retry immediately while that error is set.
- `GET /api/v1/library/collections` for custom shelves. Smart rails are
  `continue`, `favorites`, `recents`, `unplayed`, and `recently_added`.
- `PUT` / `DELETE /api/v1/library/collections/{id}/{gameId}` with an empty
  body adds or removes the focused title on a custom shelf. Unmembership
  while that shelf is the active view reloads the catalog, matching
  Favorites after a successful add or remove.
- `PUT /api/v1/library/collections/{id}?name=...` with an empty body creates
  or renames a custom shelf. The sofa derives a lowercase ASCII slug from
  the OSK name (web `uniqueCollectionID`) and does not send reserved ids
  (`all`, `favorites`, `recents`, `continue`, `unplayed`, `recently_added`,
  `recently-added`).
- `DELETE /api/v1/library/collections/{id}` removes a custom shelf.
- `GET /api/v1/library/facets` for genre and year lists (`{genres, years}`).
  Empty or missing arrays are treated as empty. The endpoint does not return
  regions; tenfoot uses the same dump-region tokens as the browser shell
  (`usa`, `japan`, `europe`, `world`, `brazil`, `korea`, `asia`,
  `australia`, `france`, `germany`, `spain`, `italy`, `canada`, `other`).
- `GET /api/v1/games?grouped=1&availability=ready` with optional `collection`,
  `platform`, `sort` (`title`, `recently_added`, `platform`), `q`, `genre`,
  `year`, `region`, `hide_prerelease=1`, and `hide_hacks=1`. Tenfoot
  keeps `availability=ready` even when the web home rails omit it, so an empty
  sofa collection can still list titles in the browser. Recent (`collection=recents`)
  omits `sort` for last-played order; Title and System send `sort=title` /
  `sort=platform`. Recently added defaults to `sort=recently_added` (the host
  rewrites empty/title to that order) and still accepts System. Catalog `cover`
  handles load artwork directly; prefetched titles do not wait on presentation
  metadata. A later view, platform, sort, search, or filter reload cancels the in-flight
  games request and cover artwork/presentation work for the superseded
  generation. Each game may include `favorite` and `collections`.
- `PUT` / `DELETE /api/v1/library/favorites/{id}` toggles the focused title.
  Unfavoriting while the Favorites view is active reloads that collection.
- `GET /api/v1/library/edition-preferences` hydrates household room edition
  choices (`{preferences:[{query,platform,game_id,chosen_at},…]}`) from
  `libraryuser`. `PUT /api/v1/library/edition-preferences` with
  `{query,platform,game_id}` remembers Confirm/Details edition choice so a
  later visit does not re-ask while that `game_id` is still a match.
- `GET /api/v1/presentation/games/{id}` for the focused title's detail pane
  (title, platform, year, genre, studio, players, summary, screenshot handles,
  and provider attribution). Empty studio, players, and summary are omitted.
  Catalog `region` from `GET /api/v1/games` joins the meta line when present.
  Optional `marquee_id`, `series`, `related` / `related_ids`, and `collection`
  decode when present. A ready `marquee_id` paints under the header; the kit
  filters the loaded catalog for siblings and hides the Series row at the
  bottom of the pane when none exist. Last-played and play-count stay off this pane.
  HTTP 200 with `state: "offline"` is a temporary provider failure: details are
  not cached, and the focused title retries with backoff. A local-media overlay of
  offline arrives as `ready` without attribution and retries the same way.
  `disabled`, `unconfigured`, `no_match`, and `ambiguous` are complete even
  when local cover or backdrop media is present. `screenshot_ids` are normalized
  64-hex handles, de-duplicated, and capped at 8 (same as the browser shell).
- `GET /api/v1/presentation/artwork/{handle}`. A failed GET or decode is
  terminal for that cover slot; a persistent 404 or corrupt image is not
  re-requested every frame, including while focused presentation details
  retry for the same cover handle. A later presentation response that
  replaces or removes the cover handle is adopted even when the previous
  cover has already decoded, so the slot can fetch the new artwork or go
  missing. The cover texture is keyed by game ID and is replaced when
  that decoded image changes. Screenshot handles from the focused title use
  the same artwork GET. Failed or missing screenshot handles are skipped in
  the carousel and are not retried every frame; the pane stays interactive
  while a handle is in flight.
- `POST /api/v1/session/launch` with `{"game_id":"..."}`. The launch JSON
  (`state`, `game_id`, optional `execution` / `media` / `progress`) is adopted
  immediately; tenfoot still polls afterward.
- `GET /api/v1/session` polled about once a second while the window is up, and
  again right after launch or stop. A poll that started before a launch or stop
  is discarded when that mutation finishes, so a stale idle or active snapshot
  cannot unpark or re-park the GPU. That is the same observation point the host
  uses to reap exited host-only / media sessions (`internal/hostapi/session.go`).
  When `state` is not `active`, `game_id` and `system` are omitted. An active
  session that omits `game_id` keeps that omission, including when another
  client replaces a launched game with a development RBF; tenfoot copies an ID
  from the local launch response only, not from later polls. `execution`, `media`, `progress`, and `input.state`
  are shown in now-playing chrome when present. Sofa chrome maps the live
  session to idle, active, stopping, failed, or development (`execution=fpga_development`).
  When `execution=fpga_development`, or when `development` /
  `development_active` / `development_session_state` are present on the
  session JSON, sofa chrome is DIAGNOSTIC (not Now playing): HDMI/input may
  be down and this is not a game session. Stop failures and live session
  progress replace a completed launch acknowledgement in the status line.
- `GET /api/v1/session/events?after=` polled with the session poll. The sofa
  shows a short readable list (event, state, game/system when present), not a
  raw dump, and advances `after` from the last seen `sequence`. A `save_failed`
  event or a Stop error that retains the session or kit lease shows **retry
  Stop**, keeps launch/replace locked, and does not auto-takeover. Successful
  Stop is the only clear path.
- `GET /v1/kit/lease` on the selected target address (status-only: owner,
  purpose, generation, remaining expiry, blocked+reason). Tenfoot does not add
  a host lease proxy and does not offer claim/renew/takeover. A transport
  failure is **kit unreachable**, matching the P4d kit-down chrome. A blocked
  lease refuses DIAGNOSTIC RBF load and does not open a takeover panel.
- `GET /api/v1/session/preview` while the host session is `active`. The host
  registers this route only with a media decoder (`WithMediaPreview`). Tenfoot
  consumes `multipart/x-mixed-replace; boundary=fogcast-frame` JPEG parts with
  CPU `image/jpeg` decode and one SDL texture upload (same spirit as attract
  stills). The sofa labels it **Preview**; it does not claim full living-room
  HDMI mirror quality. HTTP 404 (no route), 503 `"session preview is inactive"`,
  kit/decoder down, and network errors are graceful unavailable: no panic, and
  Launch/Stop never wait on preview. Park/unpark, session Stop, app close,
  attract entry, and overlays that need GPU cancel the in-flight GET, close the
  reader, and drop the texture so no goroutine holds the stream after teardown.
- `POST /api/v1/session/development-rbf` with `Content-Type:
  application/octet-stream` and `Content-Length` set. Settings overlay row
  **DIAGNOSTIC RBF** opens a gamepad path OSK (symbols page; type or paste a
  local host-reachable file path). Tenfoot stats the path, rejects empty and
  >32MiB files before upload, then POSTs the file bytes. There is no browser
  file picker and no host file-list API. Load is refused while retry-Stop
  lockout is set, while a session is already active, or while the kit lease
  is blocked. Successful load parks GPU and shows DIAGNOSTIC chrome. East/B
  Stop-to-idle still works. HDMI/input may be down; this is not a playable
  session.
- `GET /api/v1/health` polled about once a second with the session poll. Host
  `ready` is the local API process. `target.reachable` / `target.ready` drive
  kit chrome. A transport failure is **host unreachable**, distinct from
  **kit unreachable** (`target.reachable=false`) and **kit not ready**.
- `GET /api/v1/status` when health does not yield a kit snapshot. HTTP 503
  `TARGET_UNAVAILABLE` is kit/target unavailable. Tenfoot does not draw a
  full operator status panel.
- `POST /api/v1/session/input/attach` / `/detach` with empty bodies. Input
  state is the `input` object on `GET /api/v1/session` (the same
  `RemoteInputStatus` shape as `GET /api/v1/session/input`). Attach/detach
  are offered only while the session is `active` and `execution=fpga_native`.
  Busy while `starting` / `reconnecting` or while a mutation is in flight. A
  failed call keeps the prior `input.state` and shows a short status line
  without host path text. Attract does not arm during an in-flight
  attach/detach.
- `POST /api/v1/session/input/event` while a play session is attached. USB
  keyboard HID uses this path; a foreign or recovery-required kit lease is
  fail-closed. `fes.keyboard` posts ZX81 matrix codes; native SNES/MD post
  gamepad buttons so reconnect replay cannot treat those keys as axes.
- `POST /api/v1/session/stop` with `{"retain_lease":true}` so idle cleanup
  keeps the kit lease (rooms Soft-stop). Offered while the session is
  active, a stop is in flight, or retry-Stop lockout is set (East/B,
  Esc/Backspace, or `s`). Esc/Backspace still stop while play HID is attached;
  letter `s` stays a ZX81/core key on that path. SNES `save_failed` and other Stop errors that keep
  the session or lease retain launch lockout until a successful Stop.
- `GET /api/v1/library/attract?limit=24` after idle. `idle_seconds` sets the
  client timer. On Darwin, video handles stream with `Accept: video/*` to a
  temp file, then AVFoundation pulls frames. Linux does the same fetch when
  `ffmpeg` is on PATH and decodes with the ffmpeg CLI (no libav cgo). Non-CGO
  Darwin and Linux without ffmpeg do not fetch video bytes. Stills still load with
  `GET /api/v1/presentation/artwork/{handle}` and `Accept: image/*`. Attract
  does not run while the host session is `active`; dismiss/park/stop tears
  down the decoder and any queued player.
- `GET /api/v1/library/settings` hydrates sofa settings (idle seconds,
  preferred regions, selected target, targets with `agent_configured` and
  never an agent secret, systems, library roots). `PATCH /api/v1/library/settings`
  writes only the field the operator confirmed. A targets save sends
  `{targets:[{name, original_name?, address, enabled, agent?},…]}` as a
  full-array replace, omits `agent` when untouched, sends `agent: ""` to
  clear, and may include `selected_target`. A libraries save sends
  `{libraries:[{id,system,root},…]}` as a full-array replace and does not
  send `targets`.

## Rooms

Rooms are creator-defined, scripted menu screens (a Mario overworld, a
console room, an AmigaOS desktop) that sit beside the library browser.
Press **h**/Home or **hold B** for Home (hold B is a shortcut; GUIDE
or `o` opens Settings, whose Home row Confirm goes Home now). Home lists
pinned rooms, recently played games, installed rooms, and the full
library; `-home rooms` (or Settings › Home Left/Right) starts there.
When several editions match, Confirm and Details force a choice unless a
household preference is already saved (`libraryuser` via the edition-preferences
host API). A firmware-required title with an empty household BIOS slot is
**Unavailable**; Confirm opens a pad-friendly file picker that imports an
8192-byte Coleco BIOS through `POST /api/v1/core-media` and
`PUT /api/v1/library/firmware`. Back closes the picker without moving the
selected location. Room logical-action bindings live in
[rooms-controller-bindings.md](../rooms-controller-bindings.md). Packs are
one directory each under `-rooms DIR`, `FOGCAST_ROOMS`, `tenfoot.json`
`rooms_dir`, or `<config>/FogCast/rooms`; the embedded `example.*` rooms
are always available. Scripts are sandboxed Lua that record a display list
replayed through `gfx.Device`, so rooms work on every backend. See
[rooms](../rooms.md) for the pack format, Lua API, stdlib widgets and
budgets.

## Library views

Library views cycle All and the web smart rails (Continue, Favorites, Recent,
Unplayed, Recently added), then any custom collections from the host. Hold A
opens that list plus **New collection...**. Custom shelves can be created,
renamed, and deleted from the sofa with the gamepad OSK; Favorites and other
reserved smart ids stay non-renamable and non-deletable. The active layout
only changes how that list is drawn and moved, not which titles load.

## GPU park

The SDL window and renderer stay up for the process lifetime. When the host
session becomes `active` (launch response or `GET /api/v1/session`), tenfoot
parks GPU cover work through `gfx.Device`: it destroys cover and label
textures, drops decoded cover bitmaps, cancels in-flight presentation and
artwork work (advancing the cover generation so pre-park completions cannot
apply after resume), and does not upload a cover atlas until the session is
idle again. The **Preview** texture is the parked exception and is destroyed
on unpark, attract entry, Stop, and app close. Now-playing chrome is a few
CPU-rasterized status labels, not the library view, plus the recent session
events list, kit lease strip, and an optional **Preview** MJPEG surface.
Preview uses CPU JPEG decode and one texture; a park or unpark transition
cancels any in-flight preview stream so it cannot fight the cover atlas or
leak GPU after teardown. Stop
and Quit still work while parked. Retry-Stop lockout after `save_failed` or a
failed Stop keeps the GPU parked and launch locked until Stop succeeds. On
idle (stop success, poll, or media exit observed through the status poll) the
current layout resumes and textures are uploaded again for the visible/prefetch
window. Attract does not run while parked; after idle it may start again.
Preview stops on unpark, session Stop, app close, attract entry, and any
overlay that needs GPU.

## Known gaps / next

- Attract **video** on Linux: Darwin plays host `video` handles in attract
  (AVFoundation). Linux uses optional `ffmpeg` on PATH (CLI, no extra cgo).
  Missing ffmpeg skips the video download and falls back to stills. GUI video
  smoke on a Linux display is NEED.
- **Linux GUI / localhost smoke:** Makefile Linux `TENFOOT_CGO_ENV` is
  `CGO_ENABLED=1` with pkg-config `sdl3`. debian:sid `libsdl3-dev` compiled
  an aarch64 binary on the mini (container). Container `-smoke` against
  `http://host.docker.internal:8787` now sends `Host: 127.0.0.1:8787` and
  reaches the loopback API without `403 HOST_NOT_ALLOWED`. Windowed/fullscreen
  X11/Wayland still needs a Linux box with a display (see
  [`LINUX.md`](./LINUX.md)). No Linux CI job.

Still out of tenfoot scope (web / later): save-management / save browser, a
host lease proxy, and a full kit claim/renew/takeover operator panel.
Development RBF load uses the settings path OSK only; there is no file picker.
Session preview is optional host MJPEG, not a new encoder and not a full
HDMI mirror.
