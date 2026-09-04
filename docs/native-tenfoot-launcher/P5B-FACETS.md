# P5B — Facets and advanced catalog filters

Frontend-only sofa slice for native tenfoot. Host APIs already exist on the
FogCast public host; do not invent endpoints. Mac remains the primary sofa
proof target. Do not regress the Linux SDL3 build path from [`LINUX.md`](./LINUX.md).

## Goal

Add a gamepad-driven **sofa filter overlay** so living-room browse can narrow
the catalog by **genre**, **year**, and **region**, with optional hide toggles
for hacks / prerelease when the host query supports them. Shoulders stay
platform cycle. Filter UX should feel like the existing view / layout pickers
(list overlay, d-pad, South confirm / East back).

## Host fields (confirm before coding)

| Piece | Path / params | Notes |
|-------|---------------|-------|
| Facet lists | `GET /api/v1/library/facets` | JSON `catalog.FacetValues`: `{ "genres": string[], "years": string[] }`. Empty arrays when unavailable. **No `regions` array** on this endpoint today. |
| Games query | `GET /api/v1/games?...` | Existing `parseGameQuery` accepts `genre`, `year`, `region`, `hide_prerelease`, `hide_hacks` (plus already-used `platform`, `q`, `collection`, `sort`, `cursor`, `limit`, `grouped`, `availability`). |
| Region options | Web parity | Browser shell uses fixed dump-region tokens (`usa`, `japan`, `europe`, `world`, `brazil`, `korea`, `asia`, `australia`, `france`, `germany`, `spain`, `italy`, `canada`, `other`) — not facets. Tenfoot should mirror that vocabulary (labels like USA / Japan / …). |
| Hide toggles | `hide_prerelease=1`, `hide_hacks=1` | Host supports both via `queryFlag`. Wire both; default off (show everything) unless a prior sofa pref already exists. |

Extend `host/tenfoot` `GameListQuery` / `ListGames` to pass `genre`, `year`, `region`, and the optional `hide_*` flags. Keep `availability=ready` / `grouped=1` defaults.

## Required behavior

1. **Filter overlay.** Open/close with a dedicated gamepad control that does **not** steal: South launch, East/B Stop (session) / back, Start Quit, Guide settings, Select/View layout cycle, shoulders (L/R platforms), North favorites hold, OSK, or collection membership bindings from prior slices. Prefer a free face/menu chord consistent with tenfoot language (document the binding in README).
2. **Lists.** Overlay lists genre / year / region choices (plus clear/any). Genre and year options from facets; region from dump-region tokens. Empty facet lists still allow clear + honest empty state.
3. **Query wiring.** Changing a facet refreshes the games list with the matching query params; clearing restores unfiltered browse (other browse state — platform, collection, sort, search — stays).
4. **Hide hacks / prerelease.** Optional toggles in the same overlay when host supports `hide_*` (it does). Persist only if tenfoot already has a natural pref home in `tenfoot.json`; otherwise session-local is fine — say which in README.
5. **TV safe-area + visual language.** Fit overscan inset / layout prefs from P4. Match existing modal/list chrome (search, view picker, settings).
6. **Shoulders stay platforms.** Do not rebind L/R to filters.
7. **Darwin smoke primary.** Keep P0–P5 sofa behavior. Do not regress Linux `make build-fogcast-tenfoot` / `TENFOOT_CGO_ENV`.

## Out of scope

- Rich title detail / screenshots (shipped #127).
- Session preview / events chrome.
- `development_rbf` / kit paths.
- Library-path editor.
- Parked #125 / #130 polish unless a trivial one-liner falls out of the same touch.
- Inventing host APIs (including adding `regions` to facets).
- Kit / MiSTer, Caster, Grok Bot coding, other slices.

## Tests

Unit tests where natural:

- Client encode of `genre` / `year` / `region` / `hide_prerelease` / `hide_hacks` on `ListGames`.
- Facets decode (genres/years; nil → empty).
- Overlay selection → query refresh; clear resets those params only.
- Binding does not collide with layout / settings / OSK / launch / stop.

Acceptance commands on mini:

- `go test ./host/tenfoot/`
- `make build-fogcast-tenfoot` (Darwin)
- `make tenfoot-smoke` vs `http://127.0.0.1:8787`

## Docs

- Update [`README.md`](./README.md) controls + APIs when behavior ships.
- This brief may be dropped before merge once README matches (same pattern as P5 detail).

## Acceptance checklist

- [ ] Filter overlay lists genre/year from facets and region from dump tokens.
- [ ] Games list honors `genre` / `year` / `region` (and optional `hide_*`).
- [ ] Shoulders still cycle platforms; layout Select/View unchanged.
- [ ] TV safe-area + tenfoot visual language.
- [ ] Tests + Darwin smoke green; Linux build path not regressed.
- [ ] README matches shipped bindings/APIs.
- [ ] PR opened **only when smoke-ready**.
