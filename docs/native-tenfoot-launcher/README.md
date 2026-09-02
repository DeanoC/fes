# Native 10-foot launcher

SDL3 cover grid on Mac. It talks to the existing FogCast public host API over
HTTP. It does not own catalog, content transfer, or `/dev/MiSTer_cmd`. The
browser shell remains the default UI.

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
FOGCAST_API=http://127.0.0.1:8787 bin/fogcast-tenfoot
```

Gamepad is the intended control path (d-pad / left stick to move, South/A to
launch, East/B to back, Start to quit). Shoulders cycle the platform filter
(All, then each host platform). West/X cycles sort (title, recently added,
system). North/Y opens search; type with a keyboard, East/B clears or closes,
South/A closes the field. Keyboard is debug-only: arrows/WASD, Enter to
launch, Esc to back, Q to quit, `[` / `]` for platform, `x` for sort, `/` or
`f` for search.

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
- `GET /api/v1/games?grouped=1&availability=ready` with optional `platform`,
  `sort` (`title`, `recently_added`, `platform`), and `q`. Catalog `cover`
  handles load artwork directly; prefetched titles do not wait on presentation
  metadata. A later platform, sort, or search reload cancels the in-flight
  games request and cover artwork/presentation work for the superseded
  generation.
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
- `POST /api/v1/session/launch` with `{"game_id":"..."}`

## Planned — P1 smart collections / favorites

See [P1-COLLECTIONS.md](P1-COLLECTIONS.md). Not implemented on this tip yet; branch `feat/tenfoot-collections-p1` carries the brief for Luna SuperGrok.

## Known gaps

- GPU resources stay allocated after launch. Releasing the renderer / textures
  when a session starts is later work.
- TV safe area and overscan are later. Fullscreen is available; it is not
  calibrated for living-room overscan. Short windows such as `-height 480`
  shrink the cover cell so the row sits above the detail footer.
- Mac-first. Linux is next.
