# Tenfoot P1 — smart collections / favorites

Branch: `feat/tenfoot-collections-p1` (off `main` @ `5a345b5` / post-#100 P0 browse). Do **not** merge until green. Kit/MiSTer out of scope. Do **not** open the PR until acceptance is green (Luna opens it).

## Goal

Native SDL3 tenfoot browses the same smart collections and favorites the web UI already exposes, and can star/unstar the focused title — without rebuilding catalog/session plumbing. Gamepad-first. Web UI stays the default shell.

## Non-goals (later phases)

- Session stop / now-playing chrome, GPU release on launch, TV safe area, attract mode, library settings UI, Linux port
- Full collection editor UX (create / rename / delete custom collections) — host APIs exist; P1 may **browse** customs but must not block on sofa-friendly create/rename flows
- User tags / tag facets (no public host API)
- Separate playlist product (custom collections + attract cover this; attract is P2/P3)
- Variants UI, screenshots, video, list/wheel layouts

## Exact host APIs to use

There is no standalone OpenAPI file; contracts live in `internal/hostapi/server.go`, `internal/hostapi/library.go`, and hostapi/UI tests. Cite these paths.

### Browse (extend P0 catalog client)

`GET /api/v1/games` — `internal/hostapi/library.go` `parseGameQuery` / `handleGamesList`

| Query | Notes |
| --- | --- |
| `collection` | Smart or custom id (see below). Empty = full library (P0). |
| `grouped` | Keep `1` (web + P0 default). |
| `availability` | Tenfoot sofa default stays `ready` (P0). Web home rails omit this; empty collections vs web may differ — document in README, do not invent a new availability mode in P1. |
| `platform`, `sort`, `q`, `cursor`, `limit` | Same as P0. Keep working **inside** a collection when the host honors them. |
| `sort` values | `title`, `recently_added`, `platform`/`system`, `year`. **Do not** send web-only `sort=recents` — UI omits it; `collection=recents` already returns last-played order (`fogcast/library.go` `queryRecents`). |

**Smart / reserved `collection` values** (`fogcast/service.go` `QueryGames` + `libraryuser/collections.go` reserved ids):

| `collection=` | Behavior |
| --- | --- |
| `favorites` | Restrict to favorited game ids (`FavoriteIDs`, favorited_at DESC). |
| `recents` | Last-played order via `queryRecents` (paginated). |
| `continue` | At most **one** preferred recent playable title (`queryContinue`). |
| `unplayed` | Exclude played ids. |
| `recently_added` | Forces `sort=recently_added` when sort was empty/title. |
| other slug | Custom collection membership (`queryCustomCollection`); unknown/reserved misuse → bad request. |

Each game in the list may include:

```json
{
  "id": "...",
  "title": "...",
  "favorite": true,
  "collections": ["weekend-queue"],
  "cover": "...",
  "launchable": true
}
```

(`gameResult` in `internal/hostapi/server.go`.) Tenfoot’s `Game` struct today does **not** decode `favorite` / `collections` — extend it in P1.

### Favorites mutate

| Method | Path | Body | Response |
| --- | --- | --- | --- |
| `PUT` | `/api/v1/library/favorites/{id}` | empty | `{"id":"...","favorite":true}` |
| `DELETE` | `/api/v1/library/favorites/{id}` | empty | `{"id":"...","favorite":false}` |

Handlers: `handleFavorite` in `internal/hostapi/library.go`. No dedicated `GET /library/favorites` list — browse with `games?collection=favorites`.

### Custom collections

| Method | Path | Notes |
| --- | --- | --- |
| `GET` | `/api/v1/library/collections` | `{"collections":[{"id","name","created_at"}]}` |
| `PUT` | `/api/v1/library/collections/{id}?name=...` | Upsert; reserved ids (`favorites`, `recents`, …) rejected |
| `DELETE` | `/api/v1/library/collections/{id}` | Delete shelf |
| `PUT` | `/api/v1/library/collections/{id}/{gameId}` | Add member → `{"id","collection","member":true}` |
| `DELETE` | `/api/v1/library/collections/{id}/{gameId}` | Remove member |

P1 **must** use `GET` + `games?collection={id}` for browse. Membership toggle / CRUD is optional stretch; if shipped, mirror web (`ui_app.js` `toggleCollectionMember` / `createCollection`).

### Unchanged from P0 (still required)

- `GET /api/v1/platforms`
- `GET /api/v1/presentation/games/{id}` + artwork
- `POST /api/v1/session/launch`

### Gaps (honest)

| Item | Severity | Notes |
| --- | --- | --- |
| No `GET /api/v1/library/favorites` | soft | Use `games?collection=favorites`. |
| No user-tags HTTP API | soft / out of scope | Dump/catalog tags ≠ user tags. |
| No separate playlist API | soft | Customs + `GET /api/v1/library/attract` (attract = later phase). |
| `continue` returns ≤1 game | soft | Match web; empty is OK. |
| Sofa create/rename collection | soft | APIs exist; keyboard name entry is poor on tenfoot — leave CRUD to web unless trivial. |
| **HARD_NEED** | **none for browse + favorite toggle** | Host already supports smart collections + favorites mutate. |

Do **not** invent endpoints. If SuperGrok hits a real missing contract, mark `HARD_NEED` in the result file and stop.

## UX (gamepad-first; match web where practical)

Web reference: `internal/hostapi/ui_shell.html` library nav + `ui_app.js` `HOME_SMART_RAILS`:

1. Continue  
2. Favorites  
3. Recent (`recents`)  
4. Unplayed  
5. Recently added (`recently_added`)  
6. Custom collections from `GET /api/v1/library/collections` (name order as returned)

**Tenfoot library view cycle** (propose):

- Views: `All` (empty collection, P0 catalog) → smart rails in web order → each custom collection → wrap to `All`.
- Chrome status (and/or a short label) shows the active view name next to platform / sort / search (extend `chromeLine` / `libraryStatusLocked` in `host/tenfoot/app.go` + `sdl.go`).
- Bind **CmdViewPrev / CmdViewNext** (or one CmdViewCycle): keyboard `c` (and optionally `Shift+c` / `[`+modifier). Gamepad: prefer a free face/shoulder combo that does **not** steal P0 shoulders (platform), West (sort), North (search). Reasonable default: **Select long-press (≥450ms) opens a small on-screen view list**; d-pad moves; South confirms; East cancels. Short Select still launches.
- Changing view reloads the grid via `ListGames` / `FetchLibrary` with `collection=` set (extend `GameListQuery` in `host/tenfoot/client.go`). Cancel in-flight catalog/cover/presentation work the same way P0 reloads do.

**Favorite toggle:**

- Focused title: toggle via **North long-press** or a dedicated CmdFavorite (keyboard `v` / `*` — pick one and document). Optimistic UI: flip local `favorite` then `PUT`/`DELETE`; on failure revert + status text.
- Detail strip: show a compact favorited mark when `favorite` is true (text or glyph — no new asset pipeline required).
- When view is Favorites and the user unfavorites the focused row, reload or drop the row consistently (mirror web reconcile; simplest: reload collection).

**Keep from P0:** cover grid, async covers, platform / sort / search, focus detail strip, grouped+ready default, launch on South.

## Where to hook (P0 map)

| Area | Path | Hook |
| --- | --- | --- |
| HTTP client | `host/tenfoot/client.go` | Add `Collection` to `GameListQuery`; decode `Favorite` / `Collections` on `Game`; `Platforms`-style helpers for `GET /library/collections`, `PUT`/`DELETE` favorites. |
| App state / reload | `host/tenfoot/app.go` | View id + label; `currentQueryLocked`; `HandleCommand`; reload/cancel generation; optional collections prefetch like `loadPlatforms`. |
| Commands / gamepad | `host/tenfoot/input.go`, `sdl.go` | New commands + long-press timing; debug overlay hint string. |
| Chrome / detail | `host/tenfoot/sdl.go`, detail helpers in `app.go` | View name in chrome; favorite mark on strip. |
| Grid | `host/tenfoot/grid.go` | Unchanged layout; focus restore across reloads already in P0 — keep it. |
| Tests | `host/tenfoot/*_test.go` | Client query encoding, view cycle, favorite toggle + error revert, empty continue/favorites. |

## Acceptance

- Gamepad/keyboard can cycle All + smart collections (+ customs if listed) and see the grid update from host `collection=`.
- Focused title can favorite / unfavorite via host `PUT`/`DELETE`; Favorites view reflects membership.
- P0 platform / sort / search / detail strip still work (including inside a collection where the API allows).
- `go test ./host/tenfoot/` (+ sdl3 tagged tests as applicable) and `make build-fogcast-tenfoot` green.
- `make tenfoot-smoke` still passes against live host (`http://127.0.0.1:8787`; launch may be `MISTER_UNAVAILABLE`).
- `docs/native-tenfoot-launcher/README.md` updated for new controls / APIs.
- Commit + push on `feat/tenfoot-collections-p1`. **Open PR into `main` when green; do not merge.**
- Write `/tmp/fogcast-TENFOOT-P1-RESULT.txt` with `STATUS=GREEN|HARD_NEED`, `HEAD`, and PR URL when opened.

## Executor

SuperGrok / Luna on ai-dev-mac only. Not Codex. Not Grok Bot coding tokens. Prefer detached watchdog kick (prompt-file + bypassPermissions + --no-plan) if Shell classify blocks.

Do **not** commit `GROK-*.md`, `run-*.sh`, or `/tmp` results into the FogCast repo.
