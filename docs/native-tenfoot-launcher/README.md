# Native 10-foot launcher

SDL3 cover grid on Mac. It talks to the existing FogCast public host API over
HTTP. It does not own catalog, content transfer, or `/dev/MiSTer_cmd`.

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
bin/fogcast-tenfoot -api http://127.0.0.1:8787
FOGCAST_API=http://127.0.0.1:8787 bin/fogcast-tenfoot
```

Gamepad is the intended control path (d-pad / left stick to move, South/A to
launch, East/B to back, Start to quit). Keyboard is debug-only: arrows/WASD,
Enter to launch, Esc to back, Q to quit.

Run from a GUI terminal for the Cocoa window. Headless agent sessions fall
back to SDL's dummy video driver; the cover grid, gamepad path, and host
launch still run.

Automated Mac proof against a live host:

```sh
make tenfoot-smoke
```

## Host API used

- `GET /api/v1/games?grouped=1`
- `GET /api/v1/presentation/games/{id}`
- `GET /api/v1/presentation/artwork/{handle}`
- `POST /api/v1/session/launch` with `{"game_id":"..."}`

## Known gaps

- GPU resources stay allocated after launch. Releasing the renderer / textures
  when a session starts is later work.
- TV safe area and overscan are later. Fullscreen is available; it is not
  calibrated for living-room overscan.
- Linux is next. This spike is Mac-first.
- The launcher does not replace `ui_shell` on `main`.
