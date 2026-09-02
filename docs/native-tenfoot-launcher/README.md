# Native 10-foot launcher

SDL3 cover grid on Mac. It talks to the existing FogCast public host API over
HTTP. It does not own catalog, content transfer, or `/dev/MiSTer_cmd`. The
browser shell remains the default UI.

Phase briefs (implement in order; each links host APIs + acceptance):

- [P0 browse](P0-BROWSE.md) — merged (#100)
- [P1 collections / favorites](P1-COLLECTIONS.md) — merged (#104)
- [P2 session / GPU park](P2-SESSION.md) — merged (#106)
- [P3 living-room](P3-LIVINGROOM.md) — **active (slice A)**: TV safe area + stills attract. Alternate layouts and Linux are follow-up.

## Build

Homebrew `sdl3` and `pkg-config` are required.

```sh
make build-fogcast-tenfoot
```

That builds `bin/fogcast-tenfoot` with `-tags sdl3`.

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
bin/fogcast-tenfoot -no-attract
FOGCAST_API=http://127.0.0.1:8787 bin/fogcast-tenfoot
FOGCAST_TENFOOT_NO_ATTRACT=1 bin/fogcast-tenfoot
FOGCAST_TENFOOT_SAFE_AREA=0 bin/fogcast-tenfoot
```

Default overscan inset is **5% of each edge** (`-safe-area 0.05`). Windowed debug
can pass `-safe-area 0`. `-` / `=` nudge the inset by 0.5 percentage points
(clamped 0–20%) and persist to `$HOME/Library/Application Support/FogCast/tenfoot.json`
on Mac. Inner cover padding is unchanged and sits inside that gutter.

Attract mode starts after the host `idle_seconds` from
`GET /api/v1/library/attract` (default 60s) with no input, no modal search/view
picker, and no active host session. It cycles stills (backdrop, else cover, else
marquee) about every 12s. Any gamepad activity or debug key dismisses and resets
the idle timer. South/A on a launchable attract item dismisses and launches.
Video handles are ignored; tenfoot does not play attract video. `-smoke` implies
`-no-attract`.

Gamepad is the intended control path (d-pad / left stick to move, South/A to
launch, East/B to back, Start to quit). Shoulders cycle the platform filter
(All, then each host platform). West/X cycles sort (title, recently added,
system). On Recent, West/X cycles last played, title, and system; on Recently
added it cycles recently added and system, matching the host. North/Y opens
search; type with a keyboard, East/B clears or closes, South/A closes the
field. Hold South/A (≥450ms) to open the library view list (All, Continue,
Favorites, Recent, Unplayed, Recently added, then custom
shelves); d-pad moves, South confirms, East cancels. Hold North/Y to
favorite or unfavorite the focused title. While a host session is active,
East/B stops it (`POST /api/v1/session/stop`); Start still quits the app.
Keyboard is debug-only: arrows/WASD (S is stop, not down),
Enter to launch, Esc/Backspace to back (or stop while a session is active),
Q to quit, `[` / `]` for platform, `x` for sort, `/` or `f` for search,
`c` / Shift+`c` to cycle views, `v` to favorite, `-` / `=` to nudge the
overscan inset. Down arrow still moves focus when idle.

Run from a GUI terminal for the Cocoa window. Headless agent sessions fall
back to SDL's dummy video driver; the cover grid, gamepad path, and host
launch still run.

Automated Mac proof against a live host:

```sh
make tenfoot-smoke
```

## Host API used

- `GET /api/v1/platforms`, retried with backoff if the first fetch fails.
  The chrome status reports `platform list failed` until a list arrives;
  shoulders retry immediately while that error is set.
- `GET /api/v1/library/collections` for custom shelves. Smart rails are
  `continue`, `favorites`, `recents`, `unplayed`, and `recently_added`.
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
- `GET /api/v1/presentation/games/{id}` for the focused title's detail strip
  (title, platform, year, genre, summary, and provider attribution). HTTP 200
  with `state: "offline"` is a temporary provider failure: details are not
  cached, and the focused title retries with backoff. A local-media overlay of
  offline arrives as `ready` without attribution and retries the same way.
  `disabled`, `unconfigured`, `no_match`, and `ambiguous` are complete even
  when local cover or backdrop media is present.
- `GET /api/v1/presentation/artwork/{handle}`. A failed GET or decode is
  terminal for that cover slot; a persistent 404 or corrupt image is not
  re-requested every frame, including while focused presentation details
  retry for the same cover handle. A later presentation response that
  replaces or removes the cover handle is adopted even when the previous
  cover has already decoded, so the slot can fetch the new artwork or go
  missing. The SDL cover texture is keyed by game ID and is replaced when
  that decoded image changes.
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
  Tenfoot does not call session/events, preview, input attach/detach, or
  development-rbf.
- `POST /api/v1/session/stop` with an empty body. Offered only while the session
  is active or a stop is already in flight (East/B, Esc/Backspace, or `s`).
- `GET /api/v1/library/attract?limit=24` after idle. `idle_seconds` sets the
  client timer. Stills load with `GET /api/v1/presentation/artwork/{handle}`.
  Attract does not run while the host session is `active`.

## Library views

The cover grid cycles All and the web smart rails (Continue, Favorites, Recent,
Unplayed, Recently added), then any custom collections from the host. Sofa
create/rename of custom collections stays in the browser shell.

## GPU park

The SDL window and renderer stay up for the process lifetime. When the host
session becomes `active` (launch response or `GET /api/v1/session`), tenfoot
parks GPU cover work: it destroys cover and label textures, drops decoded
cover bitmaps, and does not upload a cover atlas until the session is idle
again. Now-playing chrome is a few CPU-rasterized status labels, not the
library grid. Stop and Quit still work while parked. On idle (stop success,
poll, or media exit observed through the status poll) the cover grid resumes
and textures are uploaded again for the visible/prefetch window. Attract does
not run while parked; after idle it may start again.

## Known gaps / next

Slice A of [P3-LIVINGROOM.md](P3-LIVINGROOM.md) is in this tree (safe-area +
stills attract). Follow-up:

- Alternate sofa layouts (list or wheel) beyond the cover grid.
- Attract **video** playback (host may return a `video` handle; tenfoot shows
  stills only).
- Linux SDL3 build: `Makefile` `TENFOOT_CGO_ENV` is Darwin-oriented today;
  `host/tenfoot/sdl.go` is `//go:build sdl3` with a `!sdl3` stub. No Linux
  pkg-config target or CI yet.
- Full sofa settings UI (libraries / targets / preferred_regions stay in the
  browser). Idle seconds come from the attract JSON.

Still out of tenfoot scope (web / later): sofa collection create/rename,
library/target settings editor, session/events stream UI, development-rbf,
media preview player, remote-input attach/detach chrome.
