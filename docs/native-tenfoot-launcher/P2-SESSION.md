# Tenfoot P2 — session stop / now-playing + GPU park

Branch: `feat/tenfoot-session-p2` (off `main` @ `ef36f7c` / post-#104 P1 collections). Do **not** merge until green. Kit/MiSTer out of scope. Do **not** open the PR until acceptance is green (Luna opens it).

## Goal

Native SDL3 tenfoot can see the live host session (now-playing), stop it, and park GPU resources while a session is active — without rebuilding catalog/session plumbing. Gamepad-first. Web UI stays the default shell.

Today tenfoot already POSTs `POST /api/v1/session/launch` on South/A. It does **not** poll `GET /api/v1/session`, does **not** call `POST /api/v1/session/stop`, and keeps the SDL renderer + cover textures allocated for the whole process lifetime after launch (README known gap).

## Non-goals (later phases)

- TV safe area / overscan calibration, attract mode, library settings UI, Linux port
- Full remote-input attach/detach chrome (web has it; tenfoot may show `input` state from session JSON as read-only status text only)
- `GET /api/v1/session/events` streaming UI, development RBF upload, media preview player
- Variants UI, screenshots, video, list/wheel layouts
- Sofa collection create/rename (P1 left that to web)

## Exact host APIs to use

Contracts live in `internal/hostapi/server.go` and `internal/hostapi/session.go` (`sessionResult`, `sessionCoordinator`). Cite these paths. Web reference: `internal/hostapi/ui_app.js` `launchRequest` / `stopRequest` / `loadSession`.

### Session status (new for tenfoot)

`GET /api/v1/session` — empty body → `sessionResult`:

```json
{
  "state": "idle|active|…",
  "game_id": "…",
  "system": "…",
  "execution": "fpga_native|host_only|fpga_development|…",
  "media": "active|stopped|failed|…",
  "progress": {"stage":"…","message":"…"},
  "input": { "state": "…", "…": "…" }
}
```

Notes from `publicSession` / coordinator:

- When `state != active`, `game_id` and `system` are omitted.
- `execution` / `media` are coordinator-owned overlays (not always present when idle).
- Polling `GET /api/v1/session` is also the observation point that reaps exited host-only / media sessions (`session.go` `status`).
- Tenfoot should poll on a modest interval while the window is up (and immediately after launch/stop responses), not open a second authority model.

### Launch (already wired; keep)

`POST /api/v1/session/launch` body `{"game_id":"…"}` → same `sessionResult` shape (plus optional `progress`). Client already in `host/tenfoot/client.go` `Launch` / `LaunchResult`. Extend decoding if needed so post-launch UI can adopt `state` / `game_id` without a separate poll race (still poll afterward).

Blocked titles must still refuse to POST (`launchBlockReason`).

### Stop (new for tenfoot)

`POST /api/v1/session/stop` — **empty body** (rejectBody). → `sessionResult` (typically idle; media `"stopped"` when a media handle existed).

Mirror web: only offer Stop when the session is active (or a stop mutation is in flight). East/B or a dedicated Stop binding while now-playing is active is fine; document the choice.

### Optional / do not require in P2

| Endpoint | Notes |
| --- | --- |
| `GET /api/v1/session/events?after=` | Soft enrichment; status poll is enough. |
| `GET /api/v1/session/preview` | Media preview; out of scope. |
| `POST /api/v1/session/input/attach\|detach` | Web FPGA remote-input; read-only `input` on status is enough for P2. |
| `POST /api/v1/session/development-rbf` | Dev path; out of scope. |

### Unchanged from P0/P1 (still required)

- Platforms, games (grouped+ready), collections, favorites, presentation + artwork
- Cover grid, view cycle, platform / sort / search, detail strip

### Gaps (honest)

| Item | Severity | Notes |
| --- | --- | --- |
| Tenfoot has Launch only | soft | Add `Session()` + `Stop()` on `host/tenfoot/client.go`. |
| No now-playing chrome | soft | Poll GET session; show title/state in chrome or a compact overlay. |
| GPU stays hot after launch | soft (in-scope) | See GPU lifecycle below — client work, not a missing host API. |
| DestroyWindow mid-run on Cocoa | soft | Prefer **park**: destroy cover/label textures, skip upload/draw (or draw a minimal now-playing frame); keep window+renderer unless a clean recreate path is proven. Full teardown/recreate is stretch. |
| Session events SSE | soft | Not required. |
| Remote-input attach UX | soft / out of scope | Status field only. |
| **HARD_NEED** | **none for GET session + launch + stop + GPU park** | Host already exposes the three session endpoints; GPU park is local SDL. |

Do **not** invent endpoints. If SuperGrok hits a real missing contract, mark `HARD_NEED` in the result file and stop.

## GPU / SDL lifecycle (inventory → ship)

Current (`host/tenfoot/sdl.go` `runWindow`):

1. `SDL_Init` → `SDL_CreateWindowAndRenderer` once.
2. Cover/label `SDL_Texture` maps; `syncTextures` uploads visible/prefetch covers every frame; off-screen textures destroyed opportunistically.
3. `defer destroyTextures` + `SDL_DestroyRenderer` + `SDL_DestroyWindow` only on process exit.
4. After `App` launch succeeds (`Phase=ok` / host accepted), the frame loop **keeps** clearing, uploading, and drawing the library grid — GPU stays allocated (README known gap).

P2 must:

1. When host session becomes **active** (from launch response and/or GET session): **park GPU** — destroy cover/label textures (and clear maps), cancel or pause cover decode upload work where practical, and stop allocating new textures until idle again. Minimal now-playing chrome may use CPU-drawn text via existing label path sparingly, or a single static status without retaining the full cover atlas.
2. When session returns to **idle** (stop success, GET session idle, or media exit observed via poll): **resume** normal syncTextures / drawFrame.
3. Do **not** leak textures across park/resume. Keep gamepad able to Stop / Quit while parked.
4. Document the park policy in README (replace the “GPU resources stay allocated” stub).

## UX (gamepad-first; match web where practical)

Web: session panel with Refresh + Stop (`ui_app.js`).

**Tenfoot:**

1. After launch OK (or on poll seeing `state=active`), show now-playing: game id/title if known, `state`, optional `execution` / `media`, short status line.
2. **Stop**: gamepad binding (prefer East/B when now-playing and not in a modal; or Start-adjacent — pick one, document, do not steal Quit without a confirm if ambiguous). Keyboard debug: e.g. `Backspace` / `s` while active.
3. While launching (`Phase=launching` or host progress), keep existing “launching …” status; do not double-fire launch.
4. Quit (Start / Q) still exits the app; stopping the host session first is nice-to-have if active, not required if stop is already one button away.
5. Keep P0/P1 browse controls when idle.

## Where to hook

| Area | Path | Hook |
| --- | --- | --- |
| HTTP client | `host/tenfoot/client.go` | `Session(ctx)`, `Stop(ctx)`; align `LaunchResult` / new `SessionResult` with `sessionResult` JSON. |
| App state | `host/tenfoot/app.go` | Session snapshot + poll ticker; start/stop commands; park flag for UI; cancel cover work when parking if needed. |
| SDL loop | `host/tenfoot/sdl.go` | Honor park: `destroyTextures` on enter park; skip `syncTextures` uploads; draw now-playing chrome; resume on idle. |
| Commands / gamepad | `host/tenfoot/input.go`, `sdl.go` | CmdStop (and/or Back-as-stop when active); debug overlay hints. |
| Tests | `host/tenfoot/*_test.go` | Client session/stop decode; park/resume; stop while active; no launch when blocked. |
| README | `docs/native-tenfoot-launcher/README.md` | APIs + controls; rewrite GPU known-gap. |

## Acceptance

- Gamepad/keyboard can launch, see active session (now-playing), and stop via `POST /api/v1/session/stop`.
- `GET /api/v1/session` polling keeps chrome honest after external stops / media exit.
- On active session, cover GPU textures are released (park); after stop/idle, library drawing resumes without leaks.
- P0/P1 browse + favorites + collections still work when idle.
- `go test ./host/tenfoot/` (+ sdl3 tagged tests as applicable) and `make build-fogcast-tenfoot` green.
- `make tenfoot-smoke` still passes against live host (`http://127.0.0.1:8787`; launch may be `MISTER_UNAVAILABLE`).
- README updated (session APIs + GPU park; remove stale “GPU stays allocated” stub).
- Commit + push on `feat/tenfoot-session-p2`. **Open PR into `main` when green; do not merge.**
- Write `/tmp/fogcast-TENFOOT-P2-RESULT.txt` with `STATUS=GREEN|HARD_NEED`, `HEAD`, and PR URL when opened.

## Executor

SuperGrok / Luna on ai-dev-mac only. Not Codex. Not Grok Bot coding tokens. Prefer detached watchdog kick (prompt-file + bypassPermissions + --no-plan) if Shell classify blocks.

Do **not** commit `GROK-*.md`, `run-*.sh`, or `/tmp` results into the FogCast repo.
