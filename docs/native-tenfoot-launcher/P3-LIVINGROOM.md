# Tenfoot P3 — living-room polish (safe area, attract, layouts, Linux)

Branch: `feat/tenfoot-livingroom-p3` (off `main` @ `157506d` / post-#106 P2 session). Do **not** merge until green. Kit/MiSTer out of scope. Do **not** open the PR until acceptance is green (Luna opens it).

## Goal

Ship the living-room gap list that P0–P2 left explicit in
`docs/native-tenfoot-launcher/README.md`:

1. **TV safe area / overscan** — keep chrome and the cover grid inside a
   calibrated inset when fullscreen (or near-fullscreen) on a real TV.
2. **Attract mode** — idle screensaver using the host attract playlist.
3. **Alternate layouts** — at least one non-grid layout (list or wheel) the
   sofa can switch to without leaving tenfoot.
4. **Linux port** — `make build-fogcast-tenfoot` (or a documented sibling
   target) builds and runs the SDL3 tenfoot client on Linux with the same
   `-tags sdl3` surface Mac already uses.

Gamepad-first. Web UI stays the default shell. Host catalog / session /
MiSTer paths stay unchanged.

## Non-goals

- Sofa create/rename of custom collections (web owns that; P1 left it).
- Full settings editor for libraries / targets / preferred regions (web).
- Writing `attract_idle_seconds` from tenfoot (read is enough; PATCH/PUT
  settings stays web unless a tiny local override flag is trivial).
- `GET /api/v1/session/events`, development-rbf, media preview player,
  remote-input attach/detach UX (P2 left those out; keep them out).
- New host endpoints for safe-area / overscan (none exist; do not invent).
- Variants UI, screenshots gallery, video scrubber beyond attract playback.
- Changing the FPGA launch path or kit images.

## Exact host APIs to use

Contracts live in `internal/hostapi/server.go` and
`internal/hostapi/library.go` (`handleAttract`, `publicLibrarySettings`,
`AttractItem` in `fogcast/library.go`). Web reference:
`internal/hostapi/ui_app.js` attract + settings hydrate.

### Attract playlist (new for tenfoot)

`GET /api/v1/library/attract?limit=N` (default limit 24; host clamps
`1…50`, invalid → `400 BAD_REQUEST`) →

```json
{
  "items": [
    {
      "game_id": "…",
      "title": "…",
      "platform": "snes",
      "video": "…",
      "cover": "…",
      "backdrop": "…",
      "marquee": "…",
      "launchable": true
    }
  ],
  "idle_seconds": 60
}
```

Notes from `AttractPlaylist` / `handleAttract`:

- Playlist prefers favorites, then recents, then games-with-media; skips
  titles with no cover/backdrop/marquee/video.
- `idle_seconds` mirrors `AttractIdleSeconds()` (settings overlay;
  default 60; clamped by `fogcast.MaxAttractIdleSeconds`).
- If the service does not implement attract, host still returns
  `{"items":[],"idle_seconds":60}`.
- Media handles are the same presentation handles used elsewhere; resolve
  artwork via existing `GET /api/v1/presentation/artwork/{handle}` (and
  video/media only if tenfoot already has a safe path — still images are
  enough for v1 attract if video decode is hard under SDL).

### Library settings (read for idle; optional)

`GET /api/v1/library/settings` → public JSON including
`attract_idle_seconds`, `preferred_regions`, `libraries`, `targets`,
`selected_target`, `systems`.

`PUT` / `PATCH /api/v1/library/settings` exist (web edits
`attract_idle_seconds` etc.). **P3 does not require tenfoot to write
settings.** Hydrate idle from attract response and/or GET settings; a
local CLI flag / env override for idle seconds is fine for sofa testing.

### Safe area / overscan — **no host API**

There is **no** `safe-area` / `overscan` field on library settings or
elsewhere. Calibration is **client-local**:

- CLI flags and/or a small on-disk prefs file under the user config dir
  (e.g. percent inset on each edge, or a single uniform percent).
- Defaults must be conservative enough for typical consumer overscan
  (document the default; allow 0% for PC monitors).
- Fullscreen already exists (`-fullscreen`); P3 makes fullscreen
  living-room-safe, not just borderless.

### Unchanged from P0–P2 (still required)

- Platforms, games (grouped+ready), collections, favorites, presentation +
  artwork
- Session poll / launch / stop + GPU park while active
- Cover grid when the grid layout is selected

### Gaps (honest)

| Item | Severity | Notes |
| --- | --- | --- |
| No tenfoot attract client | soft | Add `Attract(ctx, limit)` on `host/tenfoot/client.go`. |
| Idle timer / attract overlay | soft | Mirror web: timer resets on input; any gamepad/key exits attract. |
| TV inset | soft | Local prefs + layout math; **not** a missing host API. |
| Alternate layout | soft | List **or** wheel (pick one primary; document). Grid remains default. |
| Linux SDL3 build | soft | Makefile `TENFOOT_CGO_ENV` is Darwin-only today; `sdl.go` is `//go:build sdl3`, stub is `!sdl3`. Linux needs pkg-config `sdl3` + non-Darwin CGO env. |
| Attract video under SDL | soft | Prefer stills (cover/backdrop/marquee) first; video is stretch. |
| Settings write from sofa | out of scope | Web. |
| **HARD_NEED** | **none for attract GET + local safe-area + layout + Linux build** | Host already exposes attract + settings read; safe-area is local. |

Do **not** invent endpoints. If SuperGrok hits a real missing contract, mark
`HARD_NEED` in the result file and stop.

## UX (gamepad-first)

### Safe area

1. Apply inset to **all** drawn chrome: cover row, detail strip, status,
   now-playing, attract, layout chrome.
2. Provide a debug/calibration path (keyboard and/or hold-button chord)
   that grows/shrinks the inset live and persists it. Document bindings.
3. `-fullscreen` on a TV with default inset must not clip focus rings or
   labels. Windowed short heights (`-height 480`) keep today’s fit behavior.

### Attract

1. After `idle_seconds` with no input (and no active host session, no modal
   picker/search, settings-like overlay closed), enter attract.
2. Fetch `GET /api/v1/library/attract?limit=24` (or hydrate idle earlier).
3. Cycle items (~8–12s, slower if reduced-motion preference is detectable;
   still OK to hardcode ~12s on native). Show title + best available still;
   video optional.
4. Any gamepad / keyboard / quit input **exits** attract and resets the
   idle timer. South/A on a launchable attract item may launch (nice); not
   required if exit-only is cleaner — document the choice.
5. While a host session is active (P2 park), **do not** enter attract.
6. Empty playlist → stay on the library; do not flash an empty overlay.

### Alternate layouts

1. Add a layout cycle (grid ↔ list **or** grid ↔ wheel). Bind to a free
   control (document; do not steal Stop / Quit / search / view-picker).
2. Focus, launch, favorite, view/platform/sort/search must keep working in
   the alternate layout.
3. GPU park (P2) still destroys layout textures on active session.

### Linux

1. Document packages (`libsdl3-dev` / distro equivalent + `pkg-config`).
2. Make `build-fogcast-tenfoot` (or `GOOS=linux` sibling) work without
   Darwin-only `MACOSX_DEPLOYMENT_TARGET` flags when `uname` is not Darwin.
3. Smoke: binary starts, talks to `http://127.0.0.1:8787`, `-smoke` path
   green on Linux CI or a documented manual Linux check. Mac
   `make tenfoot-smoke` must stay green.

## Where to hook

| Area | Path | Hook |
| --- | --- | --- |
| HTTP client | `host/tenfoot/client.go` | `Attract(ctx, limit)`; optional `LibrarySettings(ctx)`. |
| App state | `host/tenfoot/app.go` | Idle timer, attract playlist + index, layout enum, safe-area inset, prefs load/save. |
| SDL loop | `host/tenfoot/sdl.go` | Apply inset to layout rects; draw attract overlay; draw list/wheel; park still honored. |
| Input | `host/tenfoot/input.go`, `sdl.go` | Reset idle on input; exit attract; layout cycle; inset calibrate. |
| Makefile | `Makefile` | Non-Darwin `TENFOOT_CGO_ENV`; keep `-tags sdl3`. |
| Build tags | `host/tenfoot/sdl.go`, `run_stub.go` | Stay `sdl3` / `!sdl3`; no Darwin-only build tag required for Linux SDL. |
| Tests | `host/tenfoot/*_test.go` | Attract decode; idle enter/exit; inset math; layout focus; stub Linux build flags where testable. |
| README | `docs/native-tenfoot-launcher/README.md` | APIs + controls; replace safe-area / Linux known-gaps with what shipped. |

## Acceptance

- Fullscreen (and documented default inset) keeps all chrome inside the safe
  area; inset is adjustable and persisted.
- Idle → attract from `GET /api/v1/library/attract`; input exits; no attract
  during active session.
- User can switch grid ↔ alternate layout; launch / stop / browse / favorites
  still work.
- Linux: documented build with `-tags sdl3` produces a runnable
  `fogcast-tenfoot` (smoke against live host or documented equivalent).
- Mac: `go test ./host/tenfoot/` (+ sdl3 tagged tests as applicable) and
  `make build-fogcast-tenfoot` green; `make tenfoot-smoke` vs
  `http://127.0.0.1:8787` still passes.
- P0–P2 browse / collections / session / GPU park still work.
- README updated (attract + settings read, safe-area prefs, layout binding,
  Linux build notes; known gaps refreshed).
- Commit + push on `feat/tenfoot-livingroom-p3`. **Open PR into `main` when
  green; do not merge.**
- Write `/tmp/fogcast-TENFOOT-P3-RESULT.txt` with `STATUS=GREEN|HARD_NEED`,
  `HEAD`, and PR URL when opened.

## Executor

SuperGrok / Luna on ai-dev-mac only. Not Codex. Not Grok Bot coding tokens.
Prefer detached watchdog kick (prompt-file + bypassPermissions + --no-plan).

Follow `/Users/clawzai/Developer/FOGCAST-PR-REVIEW-PLAYBOOK.md`: one wave
session owns tip fixes (`grok --continue`); after polish pushes re-kick
**Codex-only** (`@codex review`), not dual cursor review every push. Caster
HOLD. Do not mill R1…Rn processes.

Do **not** commit `GROK-*.md`, `run-*.sh`, or `/tmp` results into the FogCast
repo.
