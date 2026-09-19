# Rooms: scriptable menu screens for the tenfoot launcher

A room is a self-contained, creator-defined screen: a Mario overworld whose
nodes launch different games on different cores, an AmigaOS-style desktop of
strategy titles, one console's library in its own colours, or every system
whose video chip descends from the TMS9918. Rooms complement the flat library
browser: the library is still there for "everything", rooms are for curated,
styled slices.

The intended explore-and-play experience is specified in
[rooms-experience.md](rooms-experience.md). Logical actions map to common
pads in [rooms-controller-bindings.md](rooms-controller-bindings.md). This
page is the authoring and API guide.

Rooms are Lua scripts plus data, run inside the launcher in a sandboxed
[gopher-lua](https://github.com/yuin/gopher-lua) VM (`ui/rooms`). A script
never touches the graphics device: each frame it records primitives into a
display list that the launcher replays through `gfx.Device`, so the same room
works on SDL3, the software rasterizer, the FC2D stream and linuxfb. Pointer
hit-testing reads the same display list.

## Installing and opening rooms

Rooms live one directory each under the rooms directory:

| Source | Default |
| --- | --- |
| `-rooms DIR` flag | |
| `FOGCAST_ROOMS` env | |
| `tenfoot.json` `rooms_dir` | |
| default | `<UserConfigDir>/FogCast/rooms` (next to `tenfoot.json`) |

The embedded examples (`ui/rooms/examples`, ids `example.*`) are always
listed. A user pack with the same id replaces an embedded one.

Open the picker with **h** / **Home** on a keyboard, or **hold B** on a
gamepad (a shortcut, not the only Home route). Settings has a **Home** row
(`library` or `rooms`, persisted as `home` in `tenfoot.json`, or `-home`)
that chooses what appears at start; **GUIDE/o** opens that system menu as a
tap. In a room, **B/Esc** goes back (closes Details first, then the parent
room, then the picker), **Y/`i`** opens Details, **GUIDE/o** still opens
settings, and safe-area nudges still work. Direction, Confirm, and the
other face/shoulder buttons are delivered to the script unless the
launcher owns Confirm/Details for a published destination. Pointer tap on
the compact selected-destination strip is Details (same path as Y / `i`)
when the destination is a game; it does not Confirm. No essential
room action is long-press-only or chord-only; see
[rooms-controller-bindings.md](rooms-controller-bindings.md).

A room that fails to compile or errors at runtime shows a Go-drawn error
panel with the message; Back returns to the picker. A room can never wedge
the launcher: each script call has a deadline, frames have an op cap, and
images have a pixel budget (see Budgets).

## Pack layout

```
mario-world/
  room.toml
  main.lua
  lib/paths.lua        -- require "lib.paths"
  assets/bg.png        -- image.load("assets/bg.png")
```

`room.toml` (unknown keys are rejected):

```toml
id = "mario-world"          # [a-z0-9][a-z0-9._-]{0,63}
title = "Mushroom Kingdom"
author = "you"
description = "shown in the picker"
version = 1
main = "main.lua"           # default main.lua
icon = "assets/icon.png"    # optional

[theme]                     # optional overrides for room.theme
background = "#5c94fc"
accent = "#ffd300"
```

Paths are relative to the pack and may not escape it (`..` and absolute
paths are rejected). Source files are capped at 2 MiB, assets at 16 MiB.

## Script lifecycle

Define any of these globals:

| callback | when |
| --- | --- |
| `load()` | once, after the file has run; start `library.*` queries here |
| `update(dt)` | every tick before `draw`; `dt` in seconds (clamped to 0.1) |
| `draw()` | every tick; the only place `gfx.*` drawing calls are allowed |
| `on_input(cmd) -> bool` | a command name (below); return `true` when consumed. Unconsumed `back` leaves the room |
| `on_hover(id)` / `on_activate(id)` | pointer over / primary click on a region registered with `gfx.hit` |
| `on_resume()` | when the launcher returns to this room after a play session or a nested room (the room instance is suspended, not reloaded, while a nested room is open) |
| `on_resize(w, h)` | when the safe content box changes (safe-area nudge); `room.width`/`room.height` are already updated |
| `unload()` | when the room closes |

Command names: `up down left right select back stop search tab tab_prev
filter_prev filter_next sort favorite view_prev view_next view_picker
layout_cycle filters details`. While a room is focused, Y / North is Details
(the launcher opens the shared game-info panel) and is not delivered as
`search`. Keyboard `i` is Details. A pointer tap on the compact destination
strip is the same Details path. `search` still arrives from `/` or `f`
when those keys are used. See [rooms-controller-bindings.md](rooms-controller-bindings.md).

## API

### `room`

`room.id`, `room.title`, `room.width`, `room.height` (logical pixels of the
safe content area; `0,0` is its top-left), `room.time` (seconds since
load), `room.theme` (hex strings: `background`, `accent`, `highlight`,
`flash`, `label`, `label_bar`, `status`, `header`, `header_bar`,
`footer_bar`, `cover_frame`).

### `gfx` (inside `draw` only, except `measure`)

Colours are `"#rgb"`, `"#rrggbb"`, `"#rrggbbaa"` or `{r, g, b[, a]}` in 0..255.

| call | effect |
| --- | --- |
| `gfx.clear(color)` | background for this frame |
| `gfx.rect(x, y, w, h, color)` | filled rectangle |
| `gfx.image(img, x, y[, w, h[, sx, sy, sw, sh]])` | blit an `image` handle (skipped until `img.ready`) |
| `gfx.text(str, x, y[, {size=18, bold=false, color=, max_w=0, align="left"}])` | text in the embedded UI face; `align` is `left`, `center` or `right` (relative to `x`, or within `max_w` when set) |
| `gfx.measure(str, size[, bold]) -> w, h` | text metrics |
| `gfx.clip(x, y, w, h)` / `gfx.unclip()` | push/pop a clip rectangle (rects and images are cropped) |
| `gfx.hit(id, x, y, w, h)` | register a pointer region; later regions win |
| `gfx.blend("alpha"|"none")` | blend mode for following fills |

There are no lines, circles or rotation: the device has none. Draw paths as
chains of small rects (see `widgets.nodemap`).

### `image`

`image.load("assets/x.png") -> img` decodes a pack PNG/JPEG on a background
goroutine. `image.cover(game_id) -> img` asks the launcher for the catalog
cover. Handles expose `ready`, `w`, `h`, `err`. Draw nothing until `ready`.

### `library` (asynchronous; callbacks run on a later tick)

| call | callback |
| --- | --- |
| `library.query({platform, collection, q, sort, genre, year, region, hide_prerelease, hide_hacks, limit}, fn(games, err))` | `games` is an array of game tables |
| `library.platforms(fn(platforms, err))` | `{id, label, game_count, online, launchable, tags}` |
| `library.collections(fn(collections, err))` | `{id, name}` |
| `library.game(id, fn(game, err))` | one game |

Game tables: `id, title, system, genre, year, region, state, series,
launchable, launch_block, favorite, play_count, last_played_at,
played, completed, collections`. `played` is FES recorded play activity
(`play_count` or `last_played_at`). `completed` is an explicit completion
record only and is false today (no household completion store). Returning
from a launch is not Completed. Game ids are derived from library path and title, so resolve
titles by search (`q`) at load rather than hard-coding ids; `pong` and core
entries are the stable exceptions.

Platform `tags` classify hardware (`cpu:z80`, `vdp:tms9918-family`,
`handheld`, ...) from `internal/systems/table.go`; they also appear in
`GET /api/v1/platforms`.

### `destination`

The selected location the launcher uses for the compact info panel, Confirm,
and Details. Loading results must not move the player’s selection; publish
the current node after a match lands, do not refocus. Lobby, Workbench, and
TMS9918 Family publish too, so the strip is present in those rooms as well
as Mushroom Kingdom.

`destination.set{kind, label, system, game_id, room_id, query, platform,
matches, resolving, missing, note, note_by}` publishes one location.
`kind` is `game`, `room`, `library`, or `unresolved`. Omit availability to
let the host classify `matches` into Checking / Missing / Needs a choice /
Unavailable / Ready. `resolving=true` is Checking. Unresolved without a
match probe is a non-game location (a platform row, an empty list): the
strip shows **Unresolved** and Confirm stays with the room. Republishing
`matches` does not overwrite host `play_count` or `last_played_at` on
catalog rows the room already cached.
`destination.classify(games, {q=})` returns that result without changing
focus. `destination.play_history(game_or_facts)`
returns `{played, completed, line}` from play facts; `completed` is true
only when the facts include an explicit completion record. Published
destinations expose the same `played` / `completed` / `history` fields.
`destination.get()` / `destination.clear()`.

Confirm never silently no-ops: Ready plays, a room destination enters,
Needs a choice opens an edition list, Missing opens the library, Checking
and Unavailable show honest copy (Unavailable also opens Details). Details
(Y / `i`, or a pointer tap on the compact strip) opens the shared
game-info panel with Play as primary; a room `note` is attributed as
“Note from <author>”. Strip taps do not steal Direction, Confirm, Back, or
the system menu. Esc/B closes Details and
keeps the room. Played vs Completed copy on the compact panel and Details
comes from `destination.play_history` / `ClassifyHistory`; returning from
a launch is not Completed.

### `session`, `rooms`, `store`, `log`

- `session.launch(game_id)` launches through the ordinary host path (the
  same admission rules as the library); `session.state()` is the current
  host session state.
- `rooms.open(id)` opens a nested room (Back returns here and calls
  `on_resume`), `rooms.back()`, `rooms.list()`, and
  `rooms.open_library{platform=, collection=, layout=}` leaves to the
  library browser with those filters applied.
- `store.get(key[, default])` / `store.set(key, value)` persist strings,
  numbers, booleans and plain tables per room in
  `<config>/FogCast/rooms/<id>.state.json`.
- `log(...)` / `print(...)` write to stderr and the debug HUD.

`require "name"` loads `stdlib` modules first, then `name` with dots as
directory separators inside the pack (`require "lib.paths"` →
`lib/paths.lua`). `os`, `io`, `debug`, `package`, `dofile`, `loadfile`,
`load`, `loadstring` and `string.dump` are not available.

## Standard library (Lua, embedded)

| module | purpose |
| --- | --- |
| `widgets.list` | focusable scrolling rows: `new{x,y,w,h,row_h,items,id}`, `input(cmd)`, `selected()`, `set_items`, `on_hover/on_activate(id)`, `draw{label=, background=, focus_color=}` |
| `widgets.grid` | cover/icon grid with the library's focus rules: `new{x,y,w,h,cell_w,cell_h,gap,items,id}`, same methods, `draw{cover=fn(item), label=fn(item), plate=, hide_labels=}` |
| `widgets.nodemap` | overworld graph: `new{nodes={{id,x,y,label,icon,played,done,color}}, edges={{a,b}}, radius}`, d-pad follows edges (falls back to the nearest node in that direction), `focused()`, `draw{edge_color, node_color, played_color, done_color, focus_color, path_width, labels_focused_only}`. `played` is recorded play; `done` is Completed only |
| `util.color` | `parse`, `rgba`, `with_alpha`, `mix`, `shade` |
| `util.ease` | easing curves, `pulse(time, period)`, `tween(from, to, duration)` |

## Examples (`ui/rooms/examples`)

| id | shows |
| --- | --- |
| `example.lobby` | every installed room in a list; publishes Library / room destinations for the compact strip |
| `example.mario-world` | nodemap overworld; nodes resolved by search across arcade/NES/GB/SNES; five availability states on the compact panel; `store` remembers the node; the island opens a nested room |
| `example.mario-sports` | nested room; client-side keyword filter over a cross-system query; publishes the focused title |
| `example.console-snes` | one platform, themed header; Tab (`search`) hands off to the library shelf; Y is Details |
| `example.tms-vdp` | platform tags → two-pane platform/game browser; unresolved platform rows and game destinations on the compact strip |
| `example.coleco-arcade` | curated Coleco arcade ports (Donkey Kong, Carnival, Zaxxon, Congo Bongo, Frogger) resolved by `library.query` search; missing titles are skipped; publishes the focused game |
| `example.workbench` | AmigaOS 1.3 desktop drawn from rects; collection with genre fallback; publishes the focused drawer title |

Run them against a host:

```sh
make build-fogcast-tenfoot
bin/fogcast-tenfoot -api http://127.0.0.1:8787 -home rooms
```

## Budgets

| limit | default |
| --- | --- |
| `load()` and the file body | 250 ms |
| `update` + `draw` per tick | 6 ms each |
| input / pointer / resume callbacks | 4 ms |
| display-list ops per frame | 4096 (also hit regions) |
| decoded image pixels per room | 16 M |
| games per `library.query` | 2000 |

Exceeding a deadline or the op cap fails the room with a message naming the
budget. Budgets are `rooms.Budget` in Go and can be tuned by the launcher.

## Testing rooms without a display

`ui/rooms` tests build packs in memory with `testing/fstest.MapFS` and drive
`Instance.Load/Step/Input`; `ui/tenfoot/room_test.go` runs a room against an
`httptest` host and asserts display-list replay with `gfx.NewRecorder()`. The
engine is pure Go, so `go test -race ./ui/rooms/ ./ui/tenfoot/` covers it
without SDL3.
