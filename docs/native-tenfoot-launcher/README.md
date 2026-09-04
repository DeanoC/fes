# Native 10-foot launcher

SDL3 10-foot launcher (Mac primary; Linux build path documented). It talks to
the existing FogCast public host API over HTTP. It does not own catalog,
content transfer, or `/dev/MiSTer_cmd`. The browser shell remains the default
UI. Fullscreen living-room use applies a local TV overscan inset and idle
stills-or-video attract from the host playlist. Browse layouts are cover grid
(default), shelf/carousel, and list.

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
FOGCAST_API=http://127.0.0.1:8787 bin/fogcast-tenfoot
FOGCAST_TENFOOT_NO_ATTRACT=1 bin/fogcast-tenfoot
FOGCAST_TENFOOT_SAFE_AREA=0 bin/fogcast-tenfoot
```

Default overscan inset is **5% of each edge** (`-safe-area 0.05`). Windowed debug
can pass `-safe-area 0`. `-` / `=` nudge the inset by 0.5 percentage points
(clamped 0–20%) and persist to `tenfoot.json` under the user config dir
(`~/Library/Application Support/FogCast/` on Mac;
`$XDG_CONFIG_HOME/FogCast/` or `~/.config/FogCast/` on Linux). Inner cover
padding is unchanged and sits inside that gutter.

Sofa layout defaults to **grid**. `-layout shelf` or `-layout list` override
the saved pref for that run; gamepad **Select/View** (SDL Back) and debug
keyboard `l` cycle grid → shelf → list → grid. The choice is stored
in the same `tenfoot.json` as `layout`. Shelf is one row of larger covers:
Left/Right move one title, Up/Down jump by a visible page of covers. List is
vertical rows with a thumb, title, and system: Up/Down move one row, Left/Right
jump by a visible page of rows. Launch, stop, browse, favorites, search, views,
sort, platform, detail pane, GPU park, attract, and safe-area keep working in
every layout.

Attract mode starts after the host `idle_seconds` from
`GET /api/v1/library/attract` (default 60s) with no input, no modal search/view
picker or settings overlay, and no active host session. The client hydrates that
idle threshold before arming the timer. A sofa settings PATCH of
`attract_idle_seconds` updates the same idle timer. Playlist rows with a `video` handle play first (Darwin
AVFoundation, including the track `preferredTransform`). Short clips play
through, then the playlist advances; a single-item playlist restarts from the
already-fetched local file and keeps the last frame on screen. Clips longer
than 60s are capped at 60s. Missing, failed, or unsupported video falls back
to stills (backdrop, else cover, else marquee) for that row or the next
playable row. Video-only rows play when decode works. Stills cycle about every
12s. The renderer re-uploads only when a new video frame is ready. Any
gamepad activity or debug key dismisses, tears down the decoder (including
queued open results), and resets the idle timer. South/A on a launchable
attract item dismisses and launches. A South/North hold that began before
attract does not launch the attract title on release. `-smoke` implies
`-no-attract`. Linux SDL3 builds skip the video download and keep the stills
fallback; native video decode there is NEED.

Gamepad is the intended control path (d-pad / left stick to move, South/A to
launch, East/B to back, Start to quit, Select/View to cycle layout, Guide to
open the sofa settings overlay). Down from the last row (or last shelf/list
page) opens the focused title's detail pane; Up or East/B returns to browse.
Left/Right in the pane cycle screenshots. South/A still launches, East/B still
stops a live session, Start still quits, Guide still opens settings, North/Y
still opens search, and Select/View still cycles layout.
Shoulders cycle the platform filter
(All, then each host platform). West/X cycles sort (title, recently added,
system) while browsing. While an FPGA-native session is now-playing, West/X
attaches or detaches remote input; East/B remains Stop, Start remains Quit,
and Guide remains settings. On Recent, West/X cycles last played, title, and
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
manage/confirm, name OSK, search OSK, settings overlay, or an in-flight
remote-input attach/detach is open. Hold
North/Y on the grid to favorite or unfavorite the focused title. While a host session is active,
East/B stops it (`POST /api/v1/session/stop`); Start still quits the app.
West/X attaches or detaches remote input when the session is `active` with
`execution=fpga_native` and input is not `starting` or `reconnecting`. Browse
header and now-playing chrome prefix `host unreachable`, `kit unreachable`, or
`kit not ready` from `GET /api/v1/health` (and `GET /api/v1/status` 503
`TARGET_UNAVAILABLE`) so a down kit is obvious inside the TV safe-area.
Keyboard is debug-only: arrows/WASD (S is stop, not down),
Enter to launch (or confirm search), Esc/Backspace to back (or stop while a session is active),
Q to quit, `[` / `]` for platform, `x` for sort (or add/remove on a custom
shelf while the view picker is open; attach/detach while now-playing), `/` or `f` for search (or manage a
custom shelf while the picker is open), `c` / Shift+`c` to cycle views,
`v` to favorite, `l` to cycle layout,
`o` to open settings, `-` / `=` to nudge the overscan inset. Down arrow still
moves focus when idle, and opens the detail pane from the last row.

## Sofa settings

Guide (debug keyboard `o`) opens a gamepad-first overlay inside the TV
safe-area. Up/Down move rows, Left/Right change the focused row, South/A
confirms, East/B closes. Attract does not arm while the overlay is open.
Select/View still cycles layout and `-` / `=` still nudge overscan, including
while the overlay is closed.

Local prefs write immediately to `tenfoot.json`:

- Layout (`grid` / `shelf` / `list`)
- Safe-area inset
- Attract on/off (local gate only). `-no-attract` and
  `FOGCAST_TENFOOT_NO_ATTRACT` still force attract off for debug and smoke.

Host fields load from `GET /api/v1/library/settings` and save with
`PATCH /api/v1/library/settings` on confirm: attract idle seconds, preferred
regions (usa / world / europe / japan), and selected target (from the GET
target list). Library paths are shown as a count only; the path editor stays
in the browser. A failed PATCH keeps the previous values and reports a short
status line. Changing `selected_target` can fail while a session is active.

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
  Favorites unfavorite.
- `PUT /api/v1/library/collections/{id}?name=...` with an empty body creates
  or renames a custom shelf. The sofa derives a lowercase ASCII slug from
  the OSK name (web `uniqueCollectionID`) and does not send reserved ids
  (`all`, `favorites`, `recents`, `continue`, `unplayed`, `recently_added`,
  `recently-added`).
- `DELETE /api/v1/library/collections/{id}` removes a custom shelf.
- `GET /api/v1/games?grouped=1&availability=ready` with optional `collection`,
  `platform`, `sort` (`title`, `recently_added`, `platform`), and `q`. Tenfoot
  keeps `availability=ready` even when the web home rails omit it, so an empty
  sofa collection can still list titles in the browser. Recent (`collection=recents`)
  omits `sort` for last-played order; Title and System send `sort=title` /
  `sort=platform`. Recently added defaults to `sort=recently_added` (the host
  rewrites empty/title to that order) and still accepts System. Catalog `cover`
  handles load artwork directly; prefetched titles do not wait on presentation
  metadata. A later view, platform, sort, or search reload cancels the in-flight
  games request and cover artwork/presentation work for the superseded
  generation. Each game may include `favorite` and `collections`.
- `PUT` / `DELETE /api/v1/library/favorites/{id}` toggles the focused title.
  Unfavoriting while the Favorites view is active reloads that collection.
- `GET /api/v1/presentation/games/{id}` for the focused title's detail pane
  (title, platform, year, genre, studio, players, summary, screenshot handles,
  and provider attribution). Empty studio, players, and summary are omitted.
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
  missing. The SDL cover texture is keyed by game ID and is replaced when
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
  are shown in now-playing chrome when present. Stop failures and live session
  progress replace a completed launch acknowledgement in the status line.
  Tenfoot does not call session/events, preview, or development-rbf.
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
- `POST /api/v1/session/stop` with an empty body. Offered only while the session
  is active or a stop is already in flight (East/B, Esc/Backspace, or `s`).
- `GET /api/v1/library/attract?limit=24` after idle. `idle_seconds` sets the
  client timer. On Darwin, video handles stream with `Accept: video/*` to a
  temp file, then AVFoundation pulls frames. Linux and non-CGO builds do not
  fetch video bytes. Stills still load with
  `GET /api/v1/presentation/artwork/{handle}` and `Accept: image/*`. Attract
  does not run while the host session is `active`; dismiss/park/stop tears
  down the decoder and any queued player.
- `GET /api/v1/library/settings` hydrates sofa settings (idle seconds,
  preferred regions, selected target, read-only targets / systems /
  library count). `PATCH /api/v1/library/settings` writes only the field the
  operator confirmed. Tenfoot does not send `libraries` or target CRUD.

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
parks GPU cover work: it destroys cover and label textures, drops decoded
cover bitmaps, and does not upload a cover atlas until the session is idle
again. Now-playing chrome is a few CPU-rasterized status labels, not the
library view. Stop and Quit still work while parked. On idle (stop success,
poll, or media exit observed through the status poll) the current layout
resumes and textures are uploaded again for the visible/prefetch window.
Attract does not run while parked; after idle it may start again.

## Known gaps / next

- Attract **video** on Linux: Darwin plays host `video` handles in attract
  (AVFoundation). Linux SDL3 builds compile without extra video libraries,
  skip the video download, and fall back to stills. GUI video smoke on Linux
  is NEED.
- **Linux GUI / localhost smoke:** Makefile Linux `TENFOOT_CGO_ENV` is
  `CGO_ENABLED=1` with pkg-config `sdl3`. debian:sid `libsdl3-dev` compiled
  an aarch64 binary on the mini (container). Windowed/fullscreen smoke and
  `-smoke` against a loopback host API still need a Linux box (see
  [`LINUX.md`](./LINUX.md)). No Linux CI job.
- Library-path / roots editor and target address/agent CRUD stay in the
  browser. Sofa settings can pick `selected_target` and show a read-only
  target list.

Still out of tenfoot scope (web / later): library/target settings editor,
session/events stream UI, development-rbf, media preview player.
