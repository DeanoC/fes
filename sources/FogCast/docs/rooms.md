# Rooms: scriptable menu screens for the tenfoot launcher

A room is a self-contained tenfoot screen with its own presentation and local
navigation. Built-in Home and Systems lead to the library and model-specific
setup pages. Independently installed rooms can provide curated collections,
maps or other presentations through the same sandboxed API.

The intended explore-and-play experience is specified in
[rooms-experience.md](rooms-experience.md). Logical actions map to common
pads in [rooms-controller-bindings.md](rooms-controller-bindings.md). This
page is the authoring and API guide.

The embedded [Sinclair ZX81 hardware workbench](../../../docs/hardware-rooms.md)
adds an illustrated view of host-owned setups, exact-package expansion
selection, and the existing live tape picker during play.

Rooms are Lua scripts plus data, run inside the launcher in a sandboxed
[gopher-lua](https://github.com/yuin/gopher-lua) VM (`ui/rooms`). A script
never touches the graphics device: each frame it records primitives into a
display list that the launcher replays through `gfx.Device`, so the same room
works on SDL3, the software rasterizer, the FC2D stream and linuxfb. Pointer
hit-testing reads the same display list.

## Installing and opening rooms

Rooms live one directory each under the rooms directory:

The directory precedence is `-rooms DIR`, `tenfoot.json` `rooms_dir`,
`FOGCAST_ROOMS`, then `<UserConfigDir>/FogCast/rooms` (next to
`tenfoot.json`). On a kit the default is `/media/fat/fogcast/state/FogCast/rooms`.

The built-in rooms (`ui/rooms/examples`, ids `example.*`) are always
listed. A user pack with the same id replaces an embedded one.

First-party source packs declare the same `example.*` ids as the built-in
rooms, so installing one updates that room instead of creating a second entry.
Use the already installed launcher binary, with the room source directory
available on the same machine:

```sh
fogcast-tenfoot room-install --rooms /path/to/rooms /path/to/fes-cores
```

To distribute one file instead of a source directory, export a validated pack:

```sh
fogcast-tenfoot room-export --output home.room.zip /path/to/fes-cores
fogcast-tenfoot room-install --rooms /path/to/rooms home.room.zip
```

The ZIP contains `room.toml`, Lua and assets directly at its root, without a
wrapper directory. Export validates through the ordinary installer and publishes
atomically; a failed export preserves an existing output. Unchanged source
bytes produce the same archive bytes. Install accepts either a directory or a
portable archive and uses the same installed revision and rollback format.
Archives are extracted privately before selection; links, duplicate/unsafe
paths, corrupt/truncated files, more than 2,048 entries, files over 16 MiB and
expanded contents over 64 MiB are rejected. Compressed input is capped at 72 MiB.
No archive permissions or executable bits are carried into the installed room.
Transfer the file to the launcher's machine before installing it; remote catalog
transport is separate from this local operation.

On a kit, omit `--rooms` to use its configured room directory. Reopen Rooms
and select Home (or the updated room) to trial the new Lua and artwork in the
running launcher. No Go compilation, agent restart, MENU build or OS rebuild
is needed. The current room stays pinned to its old snapshot until reopened.
To undo an update:

```sh
fogcast-tenfoot room-rollback --rooms /path/to/rooms example.fes-cores
```

Reopen the room again. The previous installed snapshot returns; rolling back
a first install reveals the built-in room. Invalid Lua is rejected before
selection, and a failed runtime trial restores the previous selection.
Built-in rooms remain the offline recovery defaults; refreshing those binary
snapshots is a software update, separate from normal room-pack delivery.

Open **Home** with **h** / **Home** on a keyboard, or **hold B** on a
gamepad (a shortcut, not the only Home route). When `home` is `rooms`
and `home_room` names a pack that can be opened, start and Home open
that room as the root and clear the nested stack. Home does not toggle
the picker in that case; if that root room is already showing, Home
stays there. Otherwise the Menu lists pinned rooms, recently played games, Home, Systems,
installed presentation rooms and the full library. Selecting a room opens it
(first visit uses the authored start; later visits restore the last
valid location). Settings has a **Home** row: Left/Right chooses the
start screen (`library` or `rooms`, persisted as `home` in
`tenfoot.json`, or `-home`); Confirm goes Home now. There is no Settings
row for `home_room`. Details / **Y** / `i` pins or unpins the focused
room (`pinned_rooms` in `tenfoot.json`). **GUIDE/o** opens that system
menu as a tap.

`home_room` is a room id. Precedence is `-home-room`, then `tenfoot.json`
`home_room`, then `FOGCAST_HOME_ROOM`. The id must match the pack id
rule (`[a-z0-9][a-z0-9._-]{0,63}`). It applies only when `home` is
`rooms`; `library` is unchanged. If the id is malformed, the pack is
missing or invalid, or the room cannot be created, the Home picker is
shown. The id and reason are written to stderr and posted once per
start or Home attempt as `ui.home_room_fallback` (`room`, `reason`).
They are not shown on the TV. Back inside a nested room pops as usual.
Back at the home-room root opens Menu. While a session is
active, Home keeps the playing-view path and does not substitute
`home_room`.

In a room, **B/Esc** goes back (closes Details first, then the parent
room, then Home). Rooms cannot suppress **Back** or the **system menu**.
**Y/`i`** opens Details, **GUIDE/o** still opens
settings, and safe-area nudges still work. Direction, Confirm, and the
other face/shoulder buttons are delivered to the script unless the
launcher owns Confirm/Details for a published destination. Confirm on a
Ready game shows a FES launch overlay immediately; a second Confirm
does not POST again while that launch is in flight. Launch failure keeps
the selected location and offers Retry or Back to room. Pointer tap on
the compact selected-destination strip is Details (same path as Y / `i`)
when the destination is a game; it does not Confirm. After Stop (and any
required saving) the same room, selected location, and nested parent stack
return. A failed save stays on failed-save chrome and is not shown as a
successful completion. No essential room action is long-press-only or
chord-only; see
[rooms-controller-bindings.md](rooms-controller-bindings.md).

A room that fails to compile or errors at runtime shows a Go-drawn error
panel with the message; Back returns to Home. A room can never wedge
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
id = "my-collection"          # [a-z0-9][a-z0-9._-]{0,63}
title = "My collection"
author = "you"
description = "shown in the picker"
version = 1
main = "main.lua"           # default main.lua
icon = "assets/icon.png"    # optional
destination_panel = "strip" # optional: "strip" (default) or "hero"

[theme]                     # optional overrides for room.theme
background = "#5c94fc"
accent = "#ffd300"
```

Paths are relative to the pack and may not escape it (`..` and absolute
paths are rejected). Source files are capped at 2 MiB, assets at 16 MiB.

`destination_panel` places the FES selected-destination panel. `strip` is
the compact strip along the bottom of the room. `hero` is the Cartridge
Lounge layout used by kit Home (`example.fes-cores`): FES draws the
selected item's title, required inputs, readiness, destination and actions
in a large text column near the top, and replaces the debug-font footer
with a full-size status and prompt line. The room draws its own card,
artwork, shelf and header around it, using `room.hero` for the shared
geometry. The hero uses the light seed roles in
[the FES visual language](../../../docs/ux/visual-language.md); the room
should paint the card in the same surface colour (`#FFFCF5`). A room too
small for the hero (under 480×360) keeps the strip. Kit Home
(`example.fes-cores`) still draws the Library, Systems and Settings routes
and the visible cores above that strip, with pointer hits and a focus mark.
When those routes do not fit on one row they stack, and Direction follows
that column into the shelf. A row that fits keeps Left and Right, as the
hero does.

## Script lifecycle

Define any of these globals:

| callback | when |
| --- | --- |
| `load()` | once, after the file has run; start `library.*` queries here |
| `update(dt)` | every tick before `draw`; `dt` in seconds (clamped to 0.1) |
| `draw()` | every tick; the only place `gfx.*` drawing calls are allowed |
| `on_input(cmd) -> bool` | a command name (below); return `true` when consumed. `settings` and `home` are never delivered. A room can still offer Settings by publishing an allowlisted action destination (below). FES owns **Back** after overlays (Details / choice / launch); unconsumed `back` is not required for leave |
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
load), `room.reduced_motion` (boolean; the launcher reduced-motion
preference), `room.overlay` (boolean; true while the room is drawn over a
running machine on the kit's HDMI session plane with launcher focus, false
on host windows; a room that reads it can clear translucently so the machine
stays visible, and must include it in any reused-picture key), `room.theme`
(hex strings: `background`, `accent`, `highlight`,
`flash`, `label`, `label_bar`, `status`, `header`, `header_bar`,
`footer_bar`, `cover_frame`). A `hero` room also gets `room.hero`, with
`header`, `card`, `text`, `art`, `shelf` and `footer` rectangles
(`{x, y, w, h}`); FES draws over `text` and the footer band. It is
recomputed before `on_resize`.

### `gfx` (inside `draw` only, except `measure`)

Colours are `"#rgb"`, `"#rrggbb"`, `"#rrggbbaa"` or `{r, g, b[, a]}` in 0..255,
with straight (not premultiplied) alpha. A translucent clear, or a fill under
`gfx.blend("none")`, stores its premultiplied form. Over the kit's session
plane that is what lets the running machine show through.

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
| `gfx.paint(commands)` | record a whole frame in one call. Each command is `{"c", color}`, `{"r", x, y, w, h, color}`, `{"t", text, x, y, size, color, max_w, bold, align}`, `{"i", image, x, y, w, h}` or `{"h", id, x, y, w, h}` |
| `gfx.extend(commands)` | append those commands to the frame already presented, after any drawing recorded earlier in this draw. A clear already recorded in this draw replaces the presented clear |
| `gfx.reuse()` | keep the frame already presented and return true. Returns false until one has been presented. Call it before other drawing |

`gfx.paint` records the frame in one call. Include a clear command (`{"c", color}`). `gfx.extend` copies the presented frame, keeps ops and hits already recorded in this draw after that copy, then appends its commands. A clear recorded before the call replaces the presented clear, and a clear inside the commands replaces that. A second `gfx.extend` in the same draw appends more commands and does not copy the presented frame again, so a picture can be filled in over several draws. A finished paint or extend that misses only the soft budget is still presented, and `room.presented` becomes true, so the next unchanged draw can call `gfx.reuse` and return. Eight consecutive slow paints still retire the room. A draw that does not paint, and misses the soft budget, keeps the previous frame and sets `room.presented` false. The 4× hard deadline still retires the room on that callback.

There are no lines, circles or rotation: the device has none. Draw paths as
chains of small rects (see `widgets.nodemap`).

### `image`

`image.load("assets/x.png") -> img` decodes a pack PNG/JPEG on a background
goroutine. `image.cover(game_id) -> img` asks the launcher for the catalog
cover. Handles expose `ready`, `w`, `h`, `err`. Draw nothing until `ready`.

### `library` (asynchronous; callbacks run on a later tick)

| call | callback |
| --- | --- |
| `library.query({platform, core_ids, collection, q, sort, genre, year, region, hide_prerelease, hide_hacks, limit}, fn(games, err))` | `games` is an array of game tables. The query returns matching catalog rows, including titles that are not ready. `core_ids` restricts results to prepared core-package entries with those core IDs. |
| `library.platforms(fn(platforms, err))` | `{id, label, game_count, online, launchable, tags}` |
| `library.collections(fn(collections, err))` | `{id, name}` |
| `library.game(id, fn(game, err))` | one game. A cached list row is returned immediately. When `variant_count` is greater than one and that row has no `variants`, the call loads the detail row so grouped editions are present. |

Game tables: `id, title, system, core_id, package_id, media_id, media_role, genre, year, region, state, series,
launchable, launch_block, favorite, play_count, last_played_at,
played, completed, collections, variant_count, expansion_id, expansion_ready,
slot_expansions, variants`. `core_id` is set for an installed core-package
row, along with that entry's `package_id` and, when the entry saved media,
`media_id` and `media_role`. `variant_count` is the grouped edition count. `variants` lists those
editions as sibling rows. `expansion_id` and `slot_expansions` (`slot`,
`expansion_id`, `ready`) are the composition the library already stored.
`played` is FES recorded play activity
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
the current destination after a match lands, without moving focus. Built-in
Home publishes its selected machine to the hero panel; installed authored
rooms can use the compact destination panel.

`destination.set{kind, label, system, game_id, room_id, action, package_id,
core_id, launchable, block, needs, inputs, query, platform, matches,
resolving, missing, status, note, note_by}` publishes one location.
`inputs` lists required input devices (`keyboard`, `mouse`; anything else
is ignored) so FES can say so before Play. A pad is not listed: its
presence does not establish a mapping. With `resolving=true`, `status`
replaces the generic Checking sentence (for example "Looking for this
kit's machines…"); Confirm still waits.
`kind` is `game`, `room`, `library`, `action`, `core`, or `unresolved`. Omit availability to
let the host classify `matches` into Checking / Missing / Needs a choice /
Unavailable / Ready. `resolving=true` is Checking. Unresolved without a
match probe is a non-game location (a platform row, an empty list): the
strip shows **Unresolved** and Confirm stays with the room. Republishing
`matches` does not overwrite host `play_count` or `last_played_at` on
catalog rows the room already cached.

`kind = "action"` is a launcher-internal operation, not a shell command.
The only allowlisted id is `settings`:

```lua
destination.set{ kind = "action", action = "settings", label = "Settings" }
```

Rooms are dynamic Lua, so the check runs on every `destination.set`
call; that API boundary is the validation. A missing id, or any id other
than `settings`, raises a Lua error (`destination.set: unknown launcher
action "..."` or `destination.set: launcher action required`) and is not
stored. The room's existing error panel shows it. An accepted action is
Ready. The host may still refuse it at Confirm — Settings does not open
while a session is active or launching, a stop is in progress, the GPU
is parked, or a development load is running — and then shows a short
status instead of doing nothing. Confirm runs the action. Details
(Y / `i`, or a pointer tap on the compact strip) shows the status line
and does not run it. `destination.get()` keeps `action` as the panel
copy and reports the id as `launcher_action`. There is no `rooms.action`
call.

`kind = "core"` is an installed package on this kit, not a library row:

```lua
destination.set{
  kind = "core",
  package_id = row.package_id,
  core_id = row.core_id,
  label = row.name,
  launchable = row.launchable,
  block = row.block,
  needs = row.needs,
  inputs = row.inputs,
}
```

`package_id` is required and must be a lowercase SHA-256 hex id. A missing
or unsafe id raises a Lua error and is not stored. An unknown `kind` is
still dropped. `core_id`, when set, must be a safe token (`fes.pong`).
A non-empty `block`, or `launchable = false`, does not launch; the block
line is the status ("Needs a cartridge", "Needs firmware"). The hero
panel turns `needs` into a consequence and the remedy available from this
screen ("Display setup incomplete" for `parts`). Details (Y / `i`, or A
on a blocked core) shows the core id, a short package id and the verbatim
block inside the hero; Back or moving focus closes it. With a pointer, the
hero's Play and Details buttons do what they say; the rest of the hero is
Details. While an ambiguous earlier local launch is still being
reconciled, a launchable core is Checking rather than Ready. Activating a
launchable core calls the injected local-control client's Launch. With no
client that activation returns an error and does not panic. `in_use` and
`blocked` from the socket are returned the same way and do not fail the
room. Tenfoot connects the socket when it is started with `-gfx menu-display`. A host session leaves it unset. Kit Home says "Installed machines appear here on an FES kit." when no client is present and focuses its Library route.

`destination.classify(games, {q=})` returns that result without changing
focus. `matches` is the viable play rows when any can play. `candidates`
is the full set: a blocked sibling stays Unavailable with its `reason`
and is not a choice. Rooms that keep the classify result should publish
`candidates` so a refresh can reclassify a sibling
that becomes viable. `destination.play_history(game_or_facts)`
returns `{played, completed, line}` from play facts; `completed` is true
only when the facts include an explicit completion record. Published
destinations expose the same `played` / `completed` / `history` fields.
`destination.get()` / `destination.clear()`.

Confirm never silently no-ops: Ready plays, a room destination enters,
and an allowlisted action runs or explains why it cannot. Needs a choice
opens an edition list unless a household edition preference
is saved for that query and platform, Missing opens the library, Checking
and Unavailable show honest copy (Unavailable also opens Details). Confirm
during Checking is acknowledged ("Still checking. Press A again once it
has finished.") and is not queued: a queued Confirm could start whatever the
finished list focuses, which the player has not seen. The acknowledgement
clears when the destination stops Checking. A kit
lease held by another session is applied to each candidate before viable
choices are counted. A leased FPGA beside a playable host emulator leaves
that emulator Ready, with no choice. A lone leased option is Unavailable
with the copy "In use" / "Someone else is playing on this machine. You can play
when they're done." (or "{name} is playing {title}" when a friendly name
exists). There is no take-over option. (The "executor in use" wording is
only for logs and host diagnostics.) Confirm explains and does not launch
or take that lease. The shell that already holds the grant, including after
Soft-stop, stays Ready. A saved
preference that still matches makes Confirm Play and Details open the shared
game panel; a stale preference still forces a choice. Details
(Y / `i`, or a pointer tap on the compact strip) opens the shared
game-info panel with Play as primary; a room `note` is attributed as
“Note from <author>”. Strip taps do not steal Direction, Confirm, Back, or
the system menu. Esc/B closes Details and
keeps the room. Played vs Completed copy on the compact panel and Details
comes from `destination.play_history` / `ClassifyHistory`; returning from
a launch is not Completed.

### `kit` (only when a local-control client is injected)

On a host launcher `kit` is nil. When the launcher injects a client,
`kit.cores(function(rows, err) end)` lists installed cores on a later tick.
Each row is `{package_id, core_id, name, needs, launchable, block, inputs}`.
`name` is the manifest name. `needs` is `none`, `media`, `firmware`, `parts`
or `unsupported`. `block` is empty when `launchable` is true. `inputs` lists
`keyboard` when the package requires `fes.keyboard.hid`, or when it requires
plain `fes.keyboard` 1.0 together with `fes.video.session-display` 1.0 and
has no cartridge ROM. That pair is the ZX81 bring-up computer. The same
matrix on Master System, SG-1000 and the earlier ColecoVision package is the
joystick and is not listed. `mouse` is a required `fes.mouse*` interface.
An older agent omits `inputs`.

### `session`, `rooms`, `store`, `log`

- `session.launch(game_id)` launches through the ordinary host path (the
  same admission rules as the library). The optional
  `session.launch(game_id, package_id, expected_package_id)` first saves that
  package for the entry with compare-and-swap, then launches it; the host still
  checks package, media and persistence compatibility. `session.state()` is the current
  host session state.
- `session.stop(snapshot.session)` requests the ordinary Stop/save path;
  `session.open_tape(snapshot.session)`
  opens the live tape or disk picker only for an eligible active session.
  A Commodore 64 session uses it for the 1541 `.d64` unit.
  `session.resume()` returns from a room to the running session. They never
  replace a live tape swap with a launch. Both carry the exact observed play
  identity, so a stale room cannot act on another foreground play. The
  launcher rejects Launch while
  a session is active. A script must explain Stop's memory consequences.
- `rooms.home()` opens the home room when `home_room` is valid, and
  otherwise the FES Home picker. In a room visited during play,
  Back returns to the playing view without stopping it. The visible Hardware
  room button, physical keyboard Home and controller Select/View return to
  the active system workbench (or the Sinclair specialist workbench); held computer keys are released before menu input.
- `rooms.open(id, {game_id=..., core_id=...})` opens a nested room (Back returns here and calls
  `on_resume`). The optional selection is exposed as `room.initial_game_id` and
  `room.initial_core_id`, `rooms.back()`, `rooms.list()`, and
  `rooms.open_library{platform=, collection=, layout=}` leaves to the
  library browser with those filters applied.
- `store.get(key[, default])` / `store.set(key, value)` persist strings,
  numbers, booleans and plain tables per room in
  `<config>/FogCast/rooms/<id>.state.json`. On a kit, config is
  `/media/fat/fogcast/state` (`XDG_CONFIG_HOME`, or that directory when
  `HOME` cannot hold a config directory). A config directory that already
  holds readable state or a room pack is still loaded when it is read-only.
  A failed write is logged once and the room keeps running.

### `hardware` (optional host services)

`hardware.read(function(snapshot, err) ... end)` asynchronously returns
`machines` and `session`, plus `session_error` when the running state is
unavailable. `expansion_selection` is present when the host advertises
whether this listener accepts the single-entry next-start save. It is
separate from socket and card readiness. False means inspect only. An
omitted field is an older host: a paired kit projects false, and an
unpaired host keeps the existing save. Machines expose `game_id`, `title`, `core_id`, `package_id`,
`package_ready`, `firmware_ready`, `ready`, `unavailable_reason`,
`draft_expansion_id`, `socket = {id, label, supported}`, and
`choices = {{expansion_id, label, description, ready, unavailable_reason}}`.
The `machines` projection contains Sinclair specialist setups. The separate
`computers` list contains prepared playable core entries for Home, with
`game_id`, `title`, `core_id`, `package_id`, `ready`, and `unavailable_reason`.
Session fields are
`id`, `game_id`, `state`, `target`, `target_id`, `flight_id`, `package_id`,
`expansion_id`, `generation` (a decimal string preserving all 64 bits),
`hardware_known`, `tape_available`, and `removable_available`. The last three
prevent a missing receipt from being mistaken for an empty connector or a
supported tape or disk path. `removable_available` is true for a running
package that reports a supported unit, including a Commodore 64 `.d64`.
`hardware_known` requires a composition receipt naming the active package;
a missing or mismatched receipt is displayed as unavailable hardware.

`hardware.select_expansion({game_id=..., package_id=...,
expected_expansion_id=..., expansion_id=...}, function(selection, err) ... end)`
saves through the existing host API. Both expansion fields are required;
empty strings explicitly mean an empty connector. The callback receives
`{game_id, expansion_id}` on success. Read again after success or failure,
and never replay an ambiguous mutation automatically. One hardware read and
one save may be outstanding per room. Unsupported launchers report an error
to the callback instead of failing the room.

`hardware.setup({core_id=...})` opens guided installed-package setup for a
system. With no argument it opens the Sinclair family selector.
`hardware.open_media({game_id=..., core_id=..., package_id=...,
expected_media_id=...})` opens the next-start picker for the exact displayed
entry. An empty expected media ID is explicit. The native picker reads the
package's media capabilities and saves with compare-and-swap.
`hardware.open_expansions({game_id=..., core_id=..., package_id=...})` opens
manifest-derived sockets and exact-package cards. Importing and fitting are
separate actions. These setup actions require an idle machine; live media uses
`session.open_tape` and its observed session binding.

Keep configuration out of `store`: it is presentation state, not the host
catalogue. Room code may display compatibility results but must not infer
them from labels, art or hardware family names.
- `log(...)` / `print(...)` write to stderr and the debug HUD.

`require "name"` loads `stdlib` modules first, then `name` with dots as
directory separators inside the pack (`require "lib.paths"` →
`lib/paths.lua`). `os`, `io`, `debug`, `package`, `dofile`, `loadfile`,
`load`, `loadstring` and `string.dump` are not available.

## Standard library (Lua, embedded)

| module | purpose |
| --- | --- |
| `systems` | Registered model labels and family navigation (`families`, `family_by_core`); package manifests determine admission |
| `widgets.system_workbench` | Shared system workbench; call the returned function with an optional fixed `core_id` |
| `widgets.list` | focusable scrolling rows: `new{x,y,w,h,row_h,items,id}`, `input(cmd)`, `selected()`, `set_items`, `on_hover/on_activate(id)`, `draw{label=, background=, focus_color=}` |
| `widgets.grid` | cover/icon grid with the library's focus rules: `new{x,y,w,h,cell_w,cell_h,gap,items,id}`, same methods, `draw{cover=fn(item), label=fn(item), plate=, hide_labels=}` |
| `widgets.nodemap` | overworld graph: `new{nodes={{id,x,y,label,icon,played,done,color}}, edges={{a,b}}, radius}`, d-pad follows edges (falls back to the nearest node in that direction), `focused()`, `draw{edge_color, node_color, played_color, done_color, focus_color, path_width, labels_focused_only}`. `played` is recorded play; `done` is Completed only. Edges are navigation, not unlock gates |
| `util.color` | `parse`, `rgba`, `with_alpha`, `mix`, `shade` |
| `util.ease` | easing curves, `pulse(time, period)`, `tween(from, to, duration)` |

Nodemap `edges` (the drawn paths between locations) express relationships
and navigation. They do not lock or unlock destinations. A path on the map
is not a gate unless the room script adds that mechanic itself. See
[rooms-experience.md](rooms-experience.md) §2.

## Built-in rooms (`ui/rooms/examples`)

The launcher ships two navigation screens and two internal setup templates.
Old demo rooms and the separate C64 wrapper are removed. Independently installed
room packs still use the same authoring, install, rollback and nested-room APIs.

| id | purpose |
| --- | --- |
| `example.fes-cores` | **Home**: installed machines and prepared systems, with Library / Systems / Settings routes, readable readiness and direct Play / Details actions. Machine names use the selected core's plain model name; raw package names and build tags stay metadata. Selection persists by package ID. |
| `example.computers` | **Systems**: prepared systems, models and editions. Confirm launches; Set up opens the selected model's setup. Add system stays available with an empty library. |
| `example.system` | Internal **System setup** template for 12 families and their hardware variants. Next system changes family; Variant chooses the model. Entry, edition, media and expansion selections keep exact core/package identities. |
| `example.hardware` | Internal **Sinclair setup** template with the rear-connector shelf and cassette controls for ZX81, ZX80, TS1000 and TS1500. |

Only Home, Systems and independently installed presentation rooms appear in the
room/default-start pickers. Setup templates open from Systems or the active
session and cannot be selected as a startup screen. A saved reference to a
removed room follows the normal missing-room fallback; no legacy alias is kept.

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
