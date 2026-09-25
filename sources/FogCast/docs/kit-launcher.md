# Native kit launcher adapter

`cmd/fogcast-kit` is the CGO-free ARMv7 controller/session shell. It uses
`kitlauncher.Run` with a renderer callback and physical controller factory.
The callback receives `kitlauncher.Model`: the loaded catalog, the active system
shelf, the filtered games list, selected index, connection and
session status, controller presence and a readable message. This boundary lets
the renderer use the shared `ui/fbgrid` primitive without owning
network, input leases, or FPGA transitions. The kit view pages the live catalog
as a 4×3 grid, a coverflow focus row, a 6×3 cover wall, or a split list plus
hero; the standalone `tenfoot-linuxfb-grid` command remains a hardcoded
paint/input fixture for framebuffer tests. Existing SDL rendering files are
unchanged.

Build with `make build-fogcast-kit`. The native image installs the command and
supervises it after runtime and agent startup. Its default configuration is
`/media/fat/fogcast/launcher.json`; FES generates and embeds that file through
its launcher setup/media workflow. Missing configuration causes a readable
service error and retry. A successful host catalog fetch also writes
`/media/fat/fogcast/launcher-cache/` (catalog snapshot plus cover files keyed by
artwork handle). That tree lives on FAT beside `launcher.json` and the ROM cache;
replacing the system image does not wipe it. Catalog.json is still published
atomically; a later host fetch merges in place when titles/covers are unchanged
so the grid does not blank. Covers use a 512 MiB LRU budget under
`launcher-cache/covers/` and never evict the ROM cache. Prefetch order is
focus → page → next page → strip → attract (three concurrent fetches).
`GET /api/v1/library/cache` returns ROM used/free from the target; the kit store
reports cover used/free and last sync. `rom_cached` on a games row is a host
probe of that inventory while the host is up — not a hostless launch grant.
Boot paints the last-good shelf from
disk before host games HTTP and decodes visible covers from disk first. An absent
host shows `Offline - local library` rather than an endless reconnect. Local D-pad
and A still browse that snapshot. Launch and Stop remain disabled or unavailable
until the configured host API reconnects; the launcher never claims a target
lease or sends a direct target launch while offline. See [the host connection
contract](launcher-host.md) for listener configuration and the session HTTP
schema. Play input from a pad on the kit uses the local socket described below.
`fogcast-kit` supplies the socket path and the runtime core-bound probe.
Host endpoint configuration is explicit; target discovery is separate.

## Physical controls

Every eligible physical evdev gamepad is opened; polls merge in stable device
path order into one `remoteinput.Event` stream. Physical USB keyboards are
opened on the same hub so play-session HID can reach the kit-local input
socket without stealing browse focus. The virtual FogCast device
(`BUS_VIRTUAL` / name `FogCast Virtual Gamepad`), other virtual-bus nodes, devices
without gamepad buttons, and duplicate `/dev/input/js*` joystick interfaces are
excluded. Standard Linux gamepad buttons and the kit's 081f:e401 USB pad are
normalized. Axis range comes from EVIOCGABS, including unsigned 0–255 fixture
axes. Buttons held on opening are suppressed until release. Unplug drops that
pad and keeps any remaining pads; a one-second rescan from the 16ms loop picks
up a newly plugged pad without blocking present. When the last pad disconnects,
the launcher reopens as before.

Remapping lives in `ui/inputmap`. The default **identity** profile
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

Look tokens live in `ui/theme`. Built-in **default** / pack **Classic**
keep the current kit pixels (highlight BGRX 0,220,255,0, flash white, system
palette). **Neon** (`arcade`) and **Sofa Dim** (`night`) are named living-room
packs: colours, type roles, chrome accents, and a scene `transition`
(curtain / glitch / wipe). `-theme` (then optional `theme` in
`launcher.json`, then `FOGCAST_THEME`) selects a pack or builtin name, or a
JSON/TOML file. X (West) cycles Classic → Neon → Sofa Dim → Classic on the
wheel, browse, strip, and title pane without stealing D-pad or Y; the last
pack is written to `launcher.json` `theme`. Classic stays untagged in the
header; Neon paints `NEON` and Sofa Dim paints `DIM`. `fbgrid.Paint` and the
system-color fallback consume those tokens (fills,
highlight, flash, chrome, spacing). Header, tile names, and footer/status draw
through `gfx.DrawText` / `gfx.DrawTextWeight` with the embedded Go Regular and
Go Bold faces (`golang.org/x/image/font/gofont/goregular` and `gobold`);
the kit does not read system fonts. Typography roles `title_px` / `body_px` /
`caption_px` / `status_px` are explicit UI-face pixel sizes (header, tile name,
placeholder lettermark, footer). When a role is omitted, `header_scale` /
`label_scale` / `status_scale` still map to pixel size `8*scale` (the former
DebugText glyph height) so existing JSON keeps the same hierarchy. Paint uses
`theme.TitlePx` and siblings rather than repeating that fallback. Built-in
themes set `title_bold` (and chrome `header_bold`) true so grid headers and
detail titles use Bold; body, caption, and status stay Regular unless a
matching `*_bold` token is set. Incomplete themes inherit those defaults.
Overlong
chrome and tile labels truncate with an ellipsis. `DebugText` remains the 8×8
HUD path for FPGA protocol and spikes. There is no italic, medium, or
font-family picker in this slice. The catalog, input, and launch path stay the same. There is no
on-screen theme file picker in this slice; X cycles the three packs.

Focus changes play a short ease-in-out pop: the focused highlight ring
grows about its cell (~1.06 scale, ~160ms via `anim.Tween`) while unfocused
tiles keep their `CellOrigin` layout. South/A confirm is a white pulse that
eases out over `ConfirmFrames` (launch path unchanged). The present loop
paints while pop or a scene overlay is active instead of waiting for
the usual 100ms same-key skip.

Browse starts on a platform wheel: a horizontal clear-logo / wordmark strip
and a hero for the focused system. D-pad, left stick, Shoulder L/R, and Select
cycle platforms; A/South enters the filtered catalog browse (default 4×3 grid).
East/B on browse returns to the wheel and closes search. Start is the search
chord on browse (and from the wheel or title pane): it opens the existing
gamepad OSK from `ui/shared` and filters the current shelf by a
case-insensitive substring of the title, or of the clear-logo wordmark fallback
(system id) when the title is empty. D-pad moves keys while the OSK is open; A
types; L/R page letters and symbols; B clears a non-empty query or closes; Start
or Done commits and leaves the filter on the shelf. An empty query restores the
full shelf; no matches hide tiles (including the recent strip) and paint `No
matches`. A committed query keeps the recent strip hidden so Down stays on the
filtered shelf. After the OSK closes, D-pad and A/B match browse on the filtered
results; closing search restores the prior focus when that title is still
visible. Y, X, and Select are ignored while the OSK is open so they
cannot steal layout, pack, or shelf. Y (North) is the layout switch chord
on browse: Grid → Coverflow → Wall → Split → Grid. Coverflow is a scaled focus row of five titles
with the focused cover largest and its name (or ready clear logo) at the title
role; wall is a denser 6×3 mosaic with caption labels; split is a vertical
clear-logo (or title) list with a focused cover and short meta
(platform/year/genre/studio/players/region when those fields exist). Y is ignored on the
wheel, title pane, and attract. X (West) cycles theme packs Classic → Neon →
Sofa Dim → Classic on the wheel, browse, strip, and title pane; attract still
dismisses on X. The last pack is stored in `launcher.json`. An empty catalog hides tiles and keeps chrome. The hero paints an attract still for that system,
then presentation `backdrop_artwork_id`, then a representative catalog/presentation
cover; missing art uses the same theme-tinted placeholder as the grid. Light
stats chrome is the shelf game count, a play-count rollup when host games
carry `play_count`, and last-played (or a representative title) when that
exists. Wheel cells use a representative `logo_id` when
presentation has one, else a bold text label. Browse uses the D-pad and left
stick to select, Shoulder L/R
(or Select) to cycle system shelves as a secondary filter, and A to launch.
The wheel, browse layouts, recent strip, and title pane paint a dimmed
fanart/backdrop behind chrome when presentation `backdrop_artwork_id` or an
attract backdrop handle exists (`DecodeStill` through the existing still
cache). Missing fanart uses a soft cover-wall of decoded covers already on
the page; missing that art keeps the solid theme background. Atmosphere does
not change focus, launch, or stop. A soft `vignette` / `vignette_alpha`
edge darken paints on the wheel, browse layouts, and title pane after
atmosphere so tiles and focus rings stay inside the pad; Neon and Sofa Dim
add a 2px `bezel` frame and Classic leaves `bezel_width` at 0.
`vignette_alpha` `0` disables the edge darken. When host `GET /api/v1/session`
reports `active`, `launching`, `stopping`, or `failed`, `fbgrid` dims that
same stage and paints a `Paused` (or Starting / Stopping / Retry Stop)
badge with the catalog title and `Select+Start stop`. East/B, Start, and
Guide are not remapped; Select+Start still requests Stop. The overlay is
kit linuxfb chrome, not captured HDMI. When host `GET /api/v1/games?collection=recents` or `collection=favorites`
returns titles, a single horizontal Recent / Favorites row paints under the
browse view (covers and clear logos when those handles exist). Recents win on
duplicates; the caption is `Recent`, `Favorites`, or `Recent / Favorites`.
An empty result hides the row and keeps today's full stage height. Down that
cannot move focus further enters that strip; L/R move among its tiles; A
opens the title pane for the strip game; B or Up return to the same browse
cell. Last-row Down still opens a focused
title pane when the strip is hidden (large cover from CoverCache/DecodeCover, title at the theme title
role, meta from catalog year/genre/region plus presentation studio/players
when the host `GET /api/v1/presentation/games/{id}` succeeds, and wrapped
`summary` prose at the caption role). Empty summary draws no description
block. A ready `marquee_id` paints a banner strip under the header; missing
art hides that strip. Compact chips paint on grid, coverflow, wall, and the pane for
players, rating, completion, and portable when those presentation fields
exist (portable also uses handheld catalog systems); missing chips stay
hidden. Play-count and last-played stay off the pane body; the wheel hero
rolls them up from host `play_count` / `last_played_at` when admitted.
A stays launch on the grid and is not used to enter the pane. The pane closes
on East/B or Up and restores the same shelf and focus. A on the pane launches
the focused title through the same session path as the grid. Shoulder L/R and
D-pad L/R cycle presentation `screenshot_ids` when two or more handles are
present; otherwise those controls do nothing in the pane (shelf cycling stays
on the grid). When presentation `series`, `related` / `related_ids`, or
`collection` admits at least one other loaded catalog title, a Series strip
paints at the bottom of the pane. Down focuses it; L/R move; A opens that title's pane
(and its system shelf when the mate lives elsewhere); B or Up return to the
title. The row hides when no sibling exists. Split Right focuses the same
mates in the hero; Left/B return to the list. Y and X stay layout and theme.
When presentation `video_id` is present, the screenshot slot
becomes a kit-safe motion preview: it auto-cycles screenshots then unique
backdrop/cover posters every two seconds, paints a VIDEO badge, and captions
the slot `preview` (or `preview N / M`). That path does not decode H.264 on
the CGO-free ARMv7 binary; full clip playback is a follow-up. Titles without
a video handle keep today's still carousel. Attract does not arm while the pane is open; opening it notes
activity so idle does not fire underneath. Meaningful scene cuts (detail
open/close, attract show/hide, wheel enter/leave, Y layout, X pack, search
OSK open/close) paint a
short overlay from the active theme `transition` token: Classic **curtain**,
Neon **glitch**, Sofa Dim **wipe**. Attract does not arm while the search OSK
is open. Durations stay under 400ms and do not
block pad input. `transition` `none`, `-no-transition`, or
`FOGCAST_NO_TRANSITION=1` skips the overlay. Attract edge chrome is off by
default. `-audio-chrome`, `launcher.json` `audio_chrome`, a theme file
`audio_chrome` token, or `FOGCAST_AUDIO_CHROME=1` paints theme-highlight edge
bars from a measured 0..1 file (`-audio-level-file` /
`FOGCAST_AUDIO_LEVEL_FILE`) on attract and browse. The kit ALSA Dummy card
is not a game-audio meter and is not treated as one. With the gate on and no
file, attract uses a labeled `idle pulse` instead of claiming HDMI or FPGA
audio. `-selftest-audio-chrome` injects fake levels and samples the edge.
Missing cover art uses the same
placeholder path as the grid. The wheel footer hint is
`A open | L/R platform | X neon` (X names the next pack: `neon`, `dim`, or
`classic`); the search OSK footer is
`A type | B clear | L/R abc | START done`; the browse footer after entering from the wheel is
`A play | B platforms | L/R | Y flow` (Y names the next layout: `flow`, `wall`,
`split`, or `grid`); a browse view that never used the wheel (selftests)
keeps `A play | B detail | L/R | Y flow`; the strip footer is
`A detail | B grid | L/R`; the pane footer is `A play | B back`
(or `A play | B back | L/R shots` when screenshots can cycle, or
`A play | B back | L/R preview` when a video preview can cycle, plus
`Down series` when mates exist; the focused series row is
`A open | B title | L/R`). Shelves are `All` plus
each system present in the loaded catalog. Changing shelf filters the browse page
and keeps focus when that game is still visible; otherwise focus lands on the
first launchable title. The last shelf is stored in `launcher.json` when that
file was loaded from disk. Left/right move one cell and clamp at the ends of
the current row; up/down move by the layout column count (four on the grid, six
on the wall, one row of titles on coverflow, one column of titles on split) and clamp
at the first and last catalog rows. Crossing a page updates the painted
window because `Model.Focus` stays an index into the visible shelf. Coverflow
down that cannot change rows enters the recent strip or title pane. Split
left/right clamp; last-item down enters the strip or title pane. Stick motion
steps on the rising edge only; holding a deflection does not repeat. During
native play, while the local runtime has a core bound, pad and USB keyboard
events are raw input frames on `/run/fogcast/local-input.sock`. mister-agent
delivers them into the running core. The kit does not post that play input to
the host, and it does not wait for launcher.json reachability or the host
session's input.ready. A pad on the kit still drives the core when another
host holds the lease. The player index on the frame is the one the kit
assigned. If the socket is not listening, the kit reports `Local input
unavailable`, waits one second before dialing again, and does not fall back
to the host input route. Hold Select + Start together for one second to request
ordinary Stop; both must release before rearming. Individual Start and Select
remain game controls while a session can stop; B does not stop gameplay.
Stop/save errors retain the retry operation. After the host `idle_seconds` from
`GET /api/v1/library/attract` (default 60s; 1s is allowed) with no pad input,
no busy/session transition, and a ready host, the kit leaves the grid for an
attract stage: backdrop, then cover, then marquee, decoded with `DecodeStill`
and cycled with `anim` fade-through-black. A distinct marquee or presentation
`marquee_id` also paints a banner strip under the header alongside that still,
motion preview, or 2×2 wall; a marquee-only row keeps the still fallback and
hides the duplicate strip. When the staged row has a video
handle plus stills, attract auto-cycles those stills (and presentation
`screenshot_ids` when fetched) every two seconds under a VIDEO badge and a
`preview` caption — the same kit-safe motion path as the title pane. Four or
more stills-backed rows with at least one video handle paint a 2×2 wall of
neighboring stills with the staged tile highlighted. Title chrome uses the theme
`AttractBackground` and title/status roles. A/South on a launchable still
launches that title when a game id is present; any other pad input dismisses
and returns to the same shelf and focus. The CGO-free kit path does not decode
H.264; full clip playback is a follow-up. Titles without a video handle keep
today's stills attract and hide the VIDEO chrome. An empty playlist (or
video-only rows) still enters a themed idle panel (`Idle` / `No attract stills`)
so the grid is not frozen; any input returns to the grid.

Closing the kit's connection to `/run/fogcast/local-input.sock` releases only
that local source. The host input stream remains the path for a pad that is
not plugged into this kit. Local play input does not cross the LAN. Report
measured transport values separately from end-to-end button-to-photon latency.

## Display and verification

The launcher only renders memory; it never issues SPI, programs the FPGA, or
claims a kit lease. Rendering pauses while a game is active. After confirmed
idle it also skips `present` when that idle has no HPS framebuffer (SPI
`0x002f` omitted, the splash contract), logs once, and leaves FPGA splash
pixels in place. A missing linuxfb device does not stop the service. When
the launcher config or the session sets `hps_framebuffer`, the
connecting/retry/last-good shelf may use the existing pure-Go linuxfb backend
and the shared `fbgrid` paint path as a temporary overlay. That overlay is
not Menu's file browser, not rooms, and not a permanent catalog. Tiles are a
bounded page of live catalog rows. Cover handles come from catalog
`Game.Cover` when present, otherwise from `GET /api/v1/presentation/games/{id}`
(`cover_artwork_id`) for the visible page and the cheap next page. The same
presentation payload may include `logo_id` from LaunchBox Clear Logo art, with
`library_media` RoleLogo winning when that overlay is present. The kit
fetches `GET /api/v1/presentation/artwork/{handle}` on the paired listener,
decodes it with `DecodeCover` (Catmull–Rom downscale to
the cover cell; Software Draw stays nearest), and aspect-fits the RGBA into the
cell over theme-tinted letterbox bars. The focused tile, split hero, and title
pane prefer `box3d_id` when that handle is ready (LaunchBox Box-3D / Cart-3D /
Box-Spine, or `library_media` files named `box3d` / `cart-3d` / `*_3d`, or a
`Box - 3D` folder next to the ROM stem). Missing 3D art skews the 2D cover;
missing both hides the 3D look. Unfocused tiles keep 2D covers. Neon and Sofa
Dim add a thin cabinet bezel around focus (`cabinet_width`); Classic leaves it
off. Missing or failed art paints a
theme-tinted placeholder with a lettermark; still-loading art uses a distinct
panel without a letter. Ready logos replace the grid label-bar text and the
detail title; missing or still-loading logos keep today's text labels.
Presentation and artwork fetching are asynchronous and
do not block the present loop. The title pane paints the focused cover (and current screenshot or
video-preview still, when present) through the same cache. Presentation
`marquee_id` paints a wide strip under the header when that handle is ready;
missing or still-loading marquees hide the strip and leave cover, meta,
badges, and the screenshot/preview slot on today's layout. Video bytes are
not fetched on kit; the preview uses already-admitted still artwork. After idle, attract stills use the same artwork GET with `DecodeStill`
(Catmull–Rom to a 720p-class stage) and `fbgrid.PaintAttract`. A video handle
on the staged row cycles those stills as an honest motion preview; four or more
stills-backed rows with a video handle paint a 2×2 wall. Empty playlists
paint a themed idle panel instead of hanging on the grid. Video bytes are not
fetched on kit.

Run `go test -race ./ui/kitlauncher/... ./ui/shared ./ui/inputmap ./ui/theme ./ui/fbgrid ./ui/gfx ./ui/anim ./cmd/fogcast-kit` for adapter tests. They
exercise real HTTP transports with isolated servers and never open real input or
framebuffer devices. On the kit, `fogcast-kit -selftest-boxes` paints a focused 3D box over a 2D
cover, a cover perspective when the 3D handle is missing, hides both when no
art exists, samples Neon cabinet chrome (Classic stays off), paints the same
3D art on the title pane and split hero, keeps Y/X/A, and re-runs search
(which re-runs transition, packs, layouts, atmosphere, strip, wheel, motion,
detail, attract, cover, text, nav, and shelf). On the kit, `fogcast-kit -selftest-bezel` paints the
soft vignette, an Arcade bezel frame, and pause chrome over an active
session, proves East/B and Start do not stop, keeps split layout, and
re-runs search (which re-runs transition, packs, layouts, atmosphere,
strip, wheel, motion, detail, attract, cover, text, nav, and shelf).
On the kit, `fogcast-kit -selftest-search` opens Start search,
types through the gamepad OSK, filters the current shelf, paints an honest empty
miss, restores prior focus, keeps Y/X/Select, and re-runs transition (which re-runs
packs, layouts, atmosphere, strip, wheel, motion, detail, attract, cover, text, nav,
and shelf). On the kit, `fogcast-kit -selftest-marquee` paints a banner
strip on attract beside a still and a motion preview, hides the strip when the
handle is absent or is the only still, paints the same strip on the title pane
without crushing cover, meta, badges, or VIDEO preview, keeps Y/X/A, and
re-runs transition (which re-runs packs, layouts, atmosphere, strip, wheel,
motion, detail, attract, cover, text, nav, and shelf). On the kit, `fogcast-kit -selftest-transition` paints curtain, wipe, and glitch
overlays on a 4×3 grid, proves `none` is a no-op, keeps A launch during a
detail cut, and re-runs packs (which re-runs layouts, atmosphere, strip,
wheel, motion, detail, attract, cover, text, nav, and shelf). On the kit, `fogcast-kit -selftest-badges` paints players/rating/completion/portable
chips on grid, coverflow, wall, and the title pane, hides empty chips, samples
theme-pack highlight fills so covers stay readable, paints wheel play-stats,
keeps Y/X/A, and re-runs packs (which re-runs layouts, atmosphere, strip,
wheel, motion, detail, attract, cover, text, nav, and shelf). On the kit, `fogcast-kit -selftest-packs` paints Classic, cycles X to Neon then
Sofa Dim then Classic, samples highlight and background so the three looks
differ, keeps Y layout cycle and D-pad browse, paints a Neon wheel hint, keeps
A launch, and re-runs layouts (which re-runs atmosphere, strip, wheel, motion,
detail, attract, cover, text, nav, and shelf). On the kit, `fogcast-kit -selftest-layouts` paints the 4×3 grid, cycles Y to
coverflow then wall then split, samples a larger focused coverflow tile, a denser wall
cell, and a split list-plus-hero with short meta, hides empty coverflow and split,
keeps A launch, and re-runs atmosphere (which
re-runs strip, wheel, motion, detail, attract, cover, text, nav, and shelf).
On the kit, `fogcast-kit -selftest-atmosphere` paints dimmed fanart behind the
grid, proves a missing-art stage stays the theme background, paints a
cover-wall from decoded covers, dims the wheel stage around a bright hero,
dims the title pane around a bright cover, and re-runs strip (which re-runs
wheel, motion, detail, attract, cover, text, nav, and shelf).
On the kit, `fogcast-kit -selftest-strip` paints a Recent row under the grid, enters it
from last-row Down, moves L/R, opens detail on A, returns on B, hides the
row when empty, and re-runs wheel (which re-runs motion, detail, attract,
cover, text, nav, and shelf). `fogcast-kit -selftest-wheel` paints the platform
wheel and hero, cycles systems, enters the Mega Drive grid, returns on B, and
re-runs motion (which re-runs detail, attract, cover, text, nav, and shelf).
`fogcast-kit -selftest-nav` paints a 25-title
catalog, moves focus right/down across a 4×3 page, samples the highlight, and
exits without talking to the host. `fogcast-kit -selftest-shelf` paints a mixed
pong/Mega Drive/SNES catalog, cycles shelves with L/R and Select, checks the
header counts and visible set, and samples the highlight. `fogcast-kit -selftest-theme` paints **default**
then **arcade**, samples highlight and background, requires the pixels to
differ, and checks that title/body/caption/status pixel roles change the
painted `DrawText` sizes (including a scale-only fallback versus a px
override). `fogcast-kit -selftest-text` paints UI-face header/tile/footer chrome,
requires the header pixels to differ from a DebugText-only baseline, checks
themed glyph ink, proves the header uses Bold (and differs from a Regular
paint of the same chrome), and re-runs nav plus shelf. `fogcast-kit -selftest-bold`
proves gobold rasters differ from goregular at the same size, checks detail
title ink uses Bold, and re-runs detail (which re-runs attract, cover, text,
nav, and shelf). `fogcast-kit -selftest-cover`
decodes a cover, paints missing and loading placeholders, samples the art and
panel pixels, and re-runs text (which re-runs nav plus shelf). `fogcast-kit -selftest-attract`
arms a short idle, paints a decoded still plus an empty idle panel, paints a
kit-safe VIDEO motion preview and a stills-only neighbour without that chrome,
paints a 2×2 wall when four titles include a video handle, dismisses on
pad input with shelf and focus unchanged, and re-runs cover (which re-runs text,
nav, and shelf). `fogcast-kit -selftest-detail` opens and closes the title pane
(East/B, last-row Down, Up), paints a large cover plus title ink at `TitlePx`,
paints admitted meta and wrapped description (and omits empty description),
paints a VIDEO preview badge and `preview` caption when `video_id` is present
(and keeps a neighbour still-only carousel without that badge),
launches from the pane, holds attract while open, and re-runs attract (which
re-runs cover, text, nav, and shelf). `fogcast-kit -selftest-series` paints
series mates on the title pane, enters the row from Down, jumps with A, hides
an empty row, enters split-hero mates from Right, keeps Y/X, and re-runs
detail. `fogcast-kit -selftest-motion` moves
focus, ticks a mid-pop, samples a gap pixel that the highlight ring grows
into, confirms and samples a mid-pulse interior that is neither full flash
nor the tile fill, and re-runs series (which re-runs detail, attract, cover, text,
nav, and shelf). `fogcast-kit -selftest-fpga` records a timed attract still/crossfade
and sprite move through `gfx.NewFPGA` (FC2D software-replay, `IsStub` true,
`HW=not-yet`) and, when `/dev/fb0` opens, Replays the stream onto linuxfb.
It does not claim a programmed 2D core. `fogcast-kit -selftest-pads` opens every
eligible USB pad under the identity profile, prints device id/name, and exits.
Runtime and host tests cover their respective boundaries.
Use FES for selected image assembly and exact-artifact evidence. A diagnostic
binary or modified image does not establish reproducible-image acceptance.

## HOW_TO_RUN: LaunchBox covers on the kit host

The paired kit host (`fogcast-api` with `--launcher-config`) needs the same
opt-in `[metadata]` section the sofa host already uses. LaunchBox does not take
credentials. On the kit host:

1. Place official `Metadata.zip` at a private path (copy the sofa archive, or
   download `https://gamesdb.launchbox-app.com/Metadata.zip`).
2. Add to the host `config.toml` (mode `0600`; no `client_id` / `client_secret`):

```toml
[metadata]
enabled = true
provider = "launchbox"
archive = "/absolute/path/to/Metadata.zip"
```

3. Restart the kit-host API so it reloads metadata. Cover files land under
   `~/.cache/fogcast/metadata/launchbox-covers`.
4. Rebuild and run `fogcast-kit` (`make build-fogcast-kit`, CGO-free ARMv7).
   Visible tiles prefetch presentation then artwork; titles without a match keep
   the existing placeholder. Clear logos use the same artwork GET via `logo_id`
   and fall back to text labels when the handle is missing. Arcade-Marquee and
Banner files use the same artwork GET via `marquee_id` and hide when absent.
   Box-3D, Cart-3D, and Box-Spine files use the same artwork GET via `box3d_id`
   on the focused tile; local `library_media` RoleBox3D (including a `Box - 3D`
   folder or `*_3d` still) wins when present. Missing 3D handles hide rather
   than invent geometry.

Do not put provider secrets in launcher JSON, logs, or pull requests. Local
`library_media` covers still win when `Game.Cover` is set.
