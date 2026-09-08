# Native kit launcher adapter

`cmd/fogcast-kit` is the CGO-free ARMv7 controller/session shell. It uses
`kitlauncher.Run` with a renderer callback and physical controller factory.
The callback receives `kitlauncher.Model`: the loaded catalog, the active system
shelf, the filtered games list, selected index, connection and
session status, controller presence and a readable message. This boundary lets
the renderer use the shared `host/tenfoot/fbgrid` primitive without owning
network, input leases, or FPGA transitions. The kit view pages the live catalog
as a 4×3 grid; the standalone `tenfoot-linuxfb-grid` command remains a hardcoded
paint/input fixture for framebuffer tests. Existing SDL rendering files are
unchanged.

Build with `make build-fogcast-kit`. The native image installs the command and
supervises it after runtime and agent startup. Its default configuration is
`/media/fat/fogcast/launcher.json`; FES generates and embeds that file through
its launcher setup/media workflow. Missing configuration causes a readable
service error and retry; late host/network startup stays on the connecting screen.
See [the host connection contract](launcher-host.md) for listener configuration
and exact HTTP/input-stream schemas. The host remains required for browsing and
launch. Host endpoint configuration is explicit; target discovery is separate.

## Physical controls

Every eligible physical evdev gamepad is opened; polls merge in stable device
path order into one `remoteinput.Event` stream. The virtual FogCast device
(`BUS_VIRTUAL` / name `FogCast Virtual Gamepad`), other virtual-bus nodes, devices
without gamepad buttons, and duplicate `/dev/input/js*` joystick interfaces are
excluded. Standard Linux gamepad buttons and the kit's 081f:e401 USB pad are
normalized. Axis range comes from EVIOCGABS, including unsigned 0–255 fixture
axes. Buttons held on opening are suppressed until release. Unplug drops that
pad and keeps any remaining pads; a one-second rescan from the 16ms loop picks
up a newly plugged pad without blocking present. When the last pad disconnects,
the launcher reopens as before.

Remapping lives in `host/tenfoot/inputmap`. The default **identity** profile
leaves codes unchanged, so A still launches and Select+Start still stops.
`-input-profile` (then optional `input_profile` in `launcher.json`, then
`FOGCAST_INPUT_PROFILE`) selects a built-in name (`identity`, `swap-ab`) or a
JSON file:

```json
{
  "name": "swap-ab",
  "description": "Swap A and B after device normalization.",
  "bindings": { "a": "b", "b": "a" }
}
```

Bindings are a single lookup of logical names (`a`, `b`, `start`, `select`,
`dpad-up`, `left-x`, …) or raw `evdev:<code>` / `js:<n>` keys onto those logical
names. The same remapper is what linuxinput-derived paths and the tenfoot
`CommandFromLogical` adapter apply. There is no on-screen editor in this slice.

Look tokens live in `host/tenfoot/theme`. Built-in **default** keeps the current
kit pixels (highlight BGRX 0,220,255,0, flash white, system palette). **arcade**
and **night** are named alternates. `-theme` (then optional `theme` in
`launcher.json`, then `FOGCAST_THEME`) selects a built-in name or a JSON/TOML
file. `fbgrid.Paint` and the system-color fallback consume those tokens (fills,
highlight, flash, chrome, spacing). Header, tile names, and footer/status draw
through `gfx.DrawText` with the embedded Go Regular face (`golang.org/x/image/font/gofont/goregular`);
the kit does not read system fonts. Typography roles `title_px` / `body_px` /
`caption_px` / `status_px` are explicit UI-face pixel sizes (header, tile name,
placeholder lettermark, footer). When a role is omitted, `header_scale` /
`label_scale` / `status_scale` still map to pixel size `8*scale` (the former
DebugText glyph height) so existing JSON keeps the same hierarchy. Paint uses
`theme.TitlePx` and siblings rather than repeating that fallback. Overlong
chrome and tile labels truncate with an ellipsis. `DebugText` remains the 8×8
HUD path for FPGA protocol and spikes. There is no second face or font-family
picker in this slice. The catalog, input, and launch path stay the same. There is no
on-screen theme picker in this slice.

Focus changes play a short ease-in-out pop: the focused highlight ring
grows about its cell (~1.06 scale, ~160ms via `anim.Tween`) while unfocused
tiles keep their `CellOrigin` layout. South/A confirm is a white pulse that
eases out over `ConfirmFrames` (launch path unchanged). The present loop
paints while pop or a title-pane open fade is active instead of waiting for
the usual 100ms same-key skip.

The grid uses the D-pad and left stick in two dimensions to select, Shoulder L/R
(or Select) to cycle system shelves, and A to launch. East/B opens a focused
title pane (large cover from CoverCache/DecodeCover, title at the theme title
role, meta from catalog year/genre plus presentation studio/players when the
host `GET /api/v1/presentation/games/{id}` succeeds). Down that cannot move
focus further (last catalog row, including analog down) also opens the pane.
A stays launch on the grid and is not used to enter the pane. The pane closes
on East/B or Up and restores the same shelf and focus. A on the pane launches
the focused title through the same session path as the grid. Shoulder L/R and
D-pad L/R cycle presentation `screenshot_ids` when two or more handles are
present; otherwise those controls do nothing in the pane (shelf cycling stays
on the grid). Attract does not arm while the pane is open; opening it notes
activity so idle does not fire underneath. The pane uses a short
fade-from-black overlay (`DetailFadeDuration`) that settles to the existing
paint. Missing cover art uses the same
placeholder path as the grid. The idle footer hint is
`A play | B detail | L/R shelf`; the pane footer is `A play | B back`
(or `A play | B back | L/R shots` when screenshots can cycle). Shelves are `All` plus
each system present in the loaded catalog. Changing shelf filters the 4×3 page
and keeps focus when that game is still visible; otherwise focus lands on the
first launchable title. The last shelf is stored in `launcher.json` when that
file was loaded from disk. Left/right move one cell and clamp at the ends of
the current row; up/down move by four cells (one row of the 4×3 page) and clamp
at the first and last catalog rows. Crossing a page of 12 updates the painted
page because `Model.Focus` stays an index into the visible shelf. Stick motion
steps on the rising edge only; holding a deflection does not repeat. During
native play, events flow through the authenticated host stream into the existing
leased virtual pad. Hold Select + Start together for one second to request
ordinary Stop; both must release before rearming. Individual Start and Select
remain game controls while a session can stop; B does not stop gameplay.
Stop/save errors retain the retry operation. After the host `idle_seconds` from
`GET /api/v1/library/attract` (default 60s; 1s is allowed) with no pad input,
no busy/session transition, and a ready host, the kit leaves the grid for a
stills attract: backdrop, then cover, then marquee, decoded with `DecodeStill`
and cycled with `anim` fade-through-black. Title chrome uses the theme
`AttractBackground` and title/status roles. A/South on a launchable still
launches that title when a game id is present; any other pad input dismisses
and returns to the same shelf and focus. Kit attract is stills-only; video
handles are ignored. An empty playlist (or video-only rows) still enters a
themed idle panel (`Idle` / `No attract stills`) so the grid is not frozen;
any input returns to the grid.

The host source stream releases controls on close/timeout, and an attachment ID
prevents old input affecting a new session. Its source is exclusive; the launcher
cannot steal desktop input. Local USB input currently travels via the host and
back to the kit, so its responsiveness depends on the LAN. Report measured
transport values separately from end-to-end button-to-photon latency.

## Display and verification

The native runtime enables the MiSTer HPS framebuffer on Menu bring-up and every
successful return to idle. The launcher only renders memory; it never issues SPI,
programs the FPGA, or claims a kit lease. Rendering pauses while a game is active.
The connecting/library screen uses the existing pure-Go linuxfb backend and the
shared `fbgrid` paint path. Tiles are a bounded page of live catalog rows. When
`Game.Cover` is present, the kit fetches `GET /api/v1/presentation/artwork/{handle}`
on the paired listener, decodes it with `DecodeCover` (Catmull–Rom downscale to
the cover cell; Software Draw stays nearest), and aspect-fits the RGBA into the
cell over theme-tinted letterbox bars. Missing or failed art paints a
theme-tinted placeholder with a lettermark; still-loading art uses a distinct
panel without a letter. Fetching is asynchronous and does not block the present
loop. The title pane paints the focused cover (and current screenshot, when
present) through the same cache. After idle, attract stills use the same artwork GET with `DecodeStill`
(Catmull–Rom to a 720p-class stage) and `fbgrid.PaintAttract`. Empty playlists
paint a themed idle panel instead of hanging on the grid.

Run `go test -race ./kitlauncher/... ./host/tenfoot/inputmap ./host/tenfoot/theme ./host/tenfoot/fbgrid ./host/tenfoot/gfx ./host/tenfoot/anim ./cmd/fogcast-kit` for adapter tests. They
exercise real HTTP transports with isolated servers and never open real input or
framebuffer devices. On the kit, `fogcast-kit -selftest-nav` paints a 25-title
catalog, moves focus right/down across a 4×3 page, samples the highlight, and
exits without talking to the host. `fogcast-kit -selftest-shelf` paints a mixed
pong/Mega Drive/SNES catalog, cycles shelves with L/R and Select, checks the
header counts and visible set, and samples the highlight. `fogcast-kit -selftest-theme` paints **default**
then **arcade**, samples highlight and background, requires the pixels to
differ, and checks that title/body/caption/status pixel roles change the
painted `DrawText` sizes (including a scale-only fallback versus a px
override). `fogcast-kit -selftest-text` paints UI-face header/tile/footer chrome,
requires the header pixels to differ from a DebugText-only baseline, checks
themed glyph ink, and re-runs nav plus shelf. `fogcast-kit -selftest-cover`
decodes a cover, paints missing and loading placeholders, samples the art and
panel pixels, and re-runs text (which re-runs nav plus shelf). `fogcast-kit -selftest-attract`
arms a short idle, paints a decoded still plus an empty idle panel, dismisses on
pad input with shelf and focus unchanged, and re-runs cover (which re-runs text,
nav, and shelf). `fogcast-kit -selftest-detail` opens and closes the title pane
(East/B, last-row Down, Up), paints a large cover plus title ink at `TitlePx`,
launches from the pane, holds attract while open, and re-runs attract (which
re-runs cover, text, nav, and shelf). `fogcast-kit -selftest-motion` moves
focus, ticks a mid-pop, samples a gap pixel that the highlight ring grows
into, confirms and samples a mid-pulse interior that is neither full flash
nor the tile fill, and re-runs detail (which re-runs attract, cover, text,
nav, and shelf). `fogcast-kit -selftest-fpga` records a timed attract still/crossfade
and sprite move through `gfx.NewFPGA` (FC2D software-replay, `IsStub` true,
`HW=not-yet`) and, when `/dev/fb0` opens, Replays the stream onto linuxfb.
It does not claim a programmed 2D core. `fogcast-kit -selftest-pads` opens every
eligible USB pad under the identity profile, prints device id/name, and exits.
Runtime and host tests cover their respective boundaries.
Use FES for selected image assembly and exact-artifact evidence. A diagnostic
binary or modified image does not establish reproducible-image acceptance.
