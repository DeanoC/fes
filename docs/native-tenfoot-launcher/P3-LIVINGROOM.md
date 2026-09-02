# Tenfoot P3 — living-room slice (safe-area + attract; layouts/Linux phased)

Branch: `feat/tenfoot-livingroom-p3` (off `main` @ `157506d` / post-#106 P2 session). Do **not** merge until green. Kit/MiSTer out of scope. Do **not** open the PR until acceptance is green (Luna opens it). Web UI stays the default shell. Grok Bot does not write product code.

## Goal

Ship a **Mac living-room slice** on the existing SDL3 tenfoot client:

1. **TV safe-area / overscan calibration** so fullscreen cover chrome stays inside typical TV overscan.
2. **Attract mode** (idle screensaver) driven by the existing host attract playlist API, gamepad-dismissible, stills-first.

Keep P0–P2 browse / collections / favorites / session stop / GPU park when not in attract.

**Phasing inside P3** (prefer shipable Mac slice first):

| Slice | Priority | Notes |
| --- | --- | --- |
| A. Safe-area + attract (stills) | **Must ship** | Mac-first; this PR’s acceptance bar |
| B. Alternate layouts (list / shelf / wheel beyond cover grid) | Stretch | Only if A is green with budget; else document follow-up |
| C. Linux SDL3 port | Follow-up / stretch | See HARD gaps — do not block A |

## Non-goals

- Kit / MiSTer hardware work
- Parked polish #103 / #105 / #107 unless a trivial one-liner falls out of A
- Sofa settings UI for libraries / targets / preferred_regions (web owns operator settings)
- Full remote-input attach/detach, session/events SSE, development-rbf, media preview player
- Replacing the browser shell
- Requiring native **video** attract playback in the must-ship slice (stills + backdrop/cover/marquee are enough; video is stretch)

## Inventory (honest)

### Host APIs already present

Contracts: `internal/hostapi/server.go`, `internal/hostapi/library.go` (`handleAttract`, settings handlers), `fogcast/library.go` (`AttractItem`, `AttractPlaylist`), web reference `internal/hostapi/ui_app.js` (`loadAttract`, attract timer / stage).

#### Attract playlist

`GET /api/v1/library/attract?limit=N` (N clamped server-side; default/max around 24/50):

```json
{
  "items": [
    {
      "game_id": "…",
      "title": "…",
      "platform": "snes",
      "video": "handle-or-empty",
      "cover": "handle-or-empty",
      "backdrop": "handle-or-empty",
      "marquee": "handle-or-empty",
      "launchable": true
    }
  ],
  "idle_seconds": 60
}
```

Media fields are **presentation artwork handles**. Fetch bytes with existing tenfoot path:

`GET /api/v1/presentation/artwork/{handle}`

(Web uses the same via `mediaPath` → `/api/v1/presentation/artwork/…`.)

Playlist preference (host): favorites → recents → other titles with media (`AttractPlaylist`).

#### Library settings (idle only; optional for tenfoot)

- `GET /api/v1/library/settings` → includes `attract_idle_seconds` (default 60), plus web-owned fields.
- `PUT` / `PATCH /api/v1/library/settings` can update `attract_idle_seconds` (clamped; see `fogcast.MaxAttractIdleSeconds`).

Tenfoot **may** read `idle_seconds` from the attract response (preferred, matches web hydrate) and optionally PATCH idle seconds from a simple calibration/settings chrome. Do **not** build a full sofa settings panel.

#### Safe-area / overscan

**No host API** for TV insets. Calibration is **local** to the tenfoot process (CLI flags and/or a small on-disk prefs file under the user config dir). That is fine for P3 — not a HARD host gap.

### Current tenfoot state (post-P2)

- Cover **grid** only (`host/tenfoot/grid.go`, `sdl.go`).
- Artwork decode is **JPEG/PNG** covers (`host/tenfoot/artwork.go`) — **no video decode/playback**.
- Build: `make build-fogcast-tenfoot` → `-tags sdl3` with **Mac** `TENFOOT_CGO_ENV` (`MACOSX_DEPLOYMENT_TARGET`, Homebrew `sdl3` / `pkg-config`). Stub: `//go:build !sdl3` in `run_stub.go`.
- README known gaps (pre-P3): TV safe area / overscan later; Mac-first, Linux next.
- Session park (P2) must remain: attract should not fight GPU park — if host session is `active`, do not run attract; after stop/idle, attract timer may resume.

### Gaps table

| Item | Severity | Notes |
| --- | --- | --- |
| No safe-area insets in layout | soft (in-scope A) | Add margin/inset to grid + chrome; CLI `-safe-area` / percent; optional runtime calibrate UI |
| No attract client / idle timer | soft (in-scope A) | `Client.Attract`, idle timer, stage overlay, dismiss on any gamepad/key |
| Attract **video** handles | soft → stretch | Host may return `video`; tenfoot has no player. **Must-ship: stills** (prefer backdrop → cover → marquee). Video playback is stretch; if attempted and blocked, note in RESULT, do not HARD_NEED the whole P3 |
| Full library settings UI | out of scope | Idle seconds from attract JSON is enough; optional PATCH |
| Alternate layouts | stretch / follow-up | List or single-row shelf; do not block A |
| Linux port | **HARD for full ship in one PR** | Makefile/CGO Mac-locked; Cocoa/Aqua comments; no Linux SDL3 target/docs/CI. Stretch: sketch `GOOS=linux` + pkg-config notes; follow-up issue/section if not proven |
| Host TV safe-area API | none needed | Local calibration |
| **HARD_NEED for slice A** | **none** if stills attract + local safe-area | Host attract + artwork already exist |

Do **not** invent endpoints. If SuperGrok hits a real missing contract for slice A, mark `HARD_NEED` in the result file and stop.

## UX (gamepad-first)

### Safe-area

1. Default insets for fullscreen living-room (document chosen % or px; typical ~3–5% per edge is fine).
2. CLI: e.g. `-safe-area 0.05` or `-inset-pct 5` (pick one, document). Windowed debug can use 0.
3. Optional: hold a button chord or a Settings entry to nudge insets with d-pad; persist locally.
4. All chrome (grid, detail strip, now-playing, attract title) must respect insets.

### Attract

1. After `idle_seconds` with no input (and no active host session, and no modal search/view picker), enter attract.
2. Load `GET /api/v1/library/attract?limit=…`; cycle items on a short timer (match web spirit; exact ms flexible).
3. Show still artwork (backdrop preferred, else cover, else marquee) + title; use existing artwork fetch/decode path (may need larger decode bounds for backdrops — keep memory bounded).
4. **Any** gamepad activity or debug key dismisses attract and resets the idle timer.
5. South/A on a launchable attract item **may** launch (nice); East/B or any input at least dismisses. Document choice.
6. While attract is up, do not keep the full library atlas hotter than needed (reuse park patterns where practical: one stage texture is enough).
7. Disabled via env/flag (mirror web `FogCastAttractDisabled`) for smoke/CI: e.g. `-no-attract` or `FOGCAST_TENFOOT_NO_ATTRACT=1`.

### Alternate layouts (stretch only)

If attempted: one alternate (vertical list **or** single-row shelf), gamepad-cycleable, same APIs. Do not regress cover grid.

### Linux (stretch / follow-up)

If attempted: Linux `pkg-config sdl3` build path beside Mac; document packages; smoke what can run headless/dummy. Do not block Mac A.

## Where to hook

| Area | Path | Hook |
| --- | --- | --- |
| HTTP client | `host/tenfoot/client.go` | `Attract(ctx, limit)`; reuse `Artwork` |
| Options | `host/tenfoot/run.go`, `cmd/fogcast-tenfoot` | safe-area / no-attract flags |
| App state | `host/tenfoot/app.go` | Idle timer; attract playlist + index; dismiss; skip when session active |
| Layout | `host/tenfoot/grid.go`, `sdl.go` | Apply insets to draw/focus geometry |
| Attract stage | `host/tenfoot/sdl.go` (or small `attract.go`) | Fullscreen still + title inside safe-area |
| Prefs (optional) | new small file under user config | Persist inset pct |
| Tests | `host/tenfoot/*_test.go` | Attract decode; idle enter/dismiss; insets; no attract when session active |
| README | `docs/native-tenfoot-launcher/README.md` | Safe-area, attract APIs/controls; update known gaps |

## Acceptance (slice A)

- Fullscreen (and windowed) layout respects calibrated safe-area; no critical chrome in the overscan gutter.
- After configured idle with no input and idle host session, attract shows host playlist stills; input dismisses.
- Attract uses `GET /api/v1/library/attract` + artwork GETs; no invented APIs.
- P0–P2 browse / favorites / collections / launch / stop / GPU park still work when not in attract.
- `go test ./host/tenfoot/` (+ sdl3 tagged tests as applicable) and `make build-fogcast-tenfoot` green.
- `make tenfoot-smoke` still passes against live host (`http://127.0.0.1:8787`); attract disabled or idle high enough that smoke is not flaky.
- README updated (safe-area + attract; known gaps reflect Linux / layouts / video honestly).
- Commit + push on `feat/tenfoot-livingroom-p3`. **Open PR into `main` when green; do not merge.**
- Write `/tmp/fogcast-TENFOOT-P3-RESULT.txt` with `STATUS=GREEN|HARD_NEED`, `HEAD`, and PR URL when opened.

Stretch B/C: if shipped in the same PR, call them out in RESULT; if not, leave a short “Follow-up” section in README — do not fail A for missing B/C.

## Executor

SuperGrok / Luna on ai-dev-mac only. Not Codex. Not Grok Bot coding tokens. Prefer detached watchdog kick (prompt-file + bypassPermissions + --no-plan).

Do **not** commit `GROK-*.md`, `run-*.sh`, or `/tmp` results into the FogCast repo.
