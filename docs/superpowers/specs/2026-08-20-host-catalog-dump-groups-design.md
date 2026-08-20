# Host catalog dump groups, query facets, and host-only play

**Status:** Designed on 2026-08-20.

**Role:** Sol architecture/security owner. This specification is the executable
host-only contract for dump identity, grouped browse, server-side library
filters, continue/unplayed/recently-added collections, a cached-presentation
facet index, per-platform host-emulator cores, cue/gdi set identity, and
host-only large-content launch. It does not change the public host/target
protocol, FPGA ownership, codecs, or POC6 claims.

This document does not amend
[the 2026-08-19 host library frontend design](2026-08-19-host-library-frontend-design.md)
or
[the 2026-08-09 frontend metadata adapter design](2026-08-09-frontend-metadata-adapter-demo-data-design.md).
Catalog file identity remains authoritative. Grouping, presentation, and local
media only enrich browse. Launch still sends `{ "game_id": "<file id>" }`.

A short architecture note lives at
[`docs/architecture/2026-08-20-host-catalog-dump-groups.md`](../../architecture/2026-08-20-host-catalog-dump-groups.md).

## Purpose

Make a tens-of-thousands dump library usable: one card per title, honest
server-side region/flag/availability/genre filters, a preferred dump for
Launch, and host-emulator play for browse platforms without expanding
`protocol.System`. Evidence for this work is **Software-tested** only.

## Non-goals

- Expanding `protocol.System` beyond `megadrive` and `snes`, adding FPGA cores,
  or raising `protocol.MaxContentBytes` for target cache/upload.
- EmuMovies, Screenscraper, TheGamesDB, DAT hash matching, arcade parent/clone
  sets, pack importers, FTP, or browser third-party fetches.
- A 10-foot/gamepad-first app, Electron, Node SPA, or library UI on the target.
- Host-side SRAM/save-state sync. Continue means last-played `game_id` only.
- Watch folders or a true incremental/partial-tree scan.
- HIL, latency, or controller-symmetry claims.

## Decision 1 — Dump identity is catalog-owned; file identity is unchanged

`game_id` stays `GameID(system, libraryID, relativePath, filenameStem)`. Rescans
must not rewrite IDs when only decorations are parsed.

At scan time the host parser reads trailing balanced `( )` / `[ ]` tags from
the filename stem and persists:

- `canonical_title`: stem with dump decorations removed (display and group)
- `region`: one of `usa`, `europe`, `japan`, `world`, `brazil`, `korea`,
  `asia`, `australia`, `france`, `germany`, `spain`, `italy`, `canada`,
  `other`, or empty
- `revision`: `rev` token if present (`a`, `1`, `2`, …), else empty
- `dump_flags`: sorted comma-separated tokens from
  `beta`, `proto`, `sample`, `demo`, `hack`, `unl`
- `group_key`: `system` + unit separator + Unicode-folded `canonical_title`
- `region_rank`: integer from the operator preferred-region list (lower is
  better); unknown/empty ranks after configured regions
- `first_seen_ns`: set once on insert; never rewritten on later observes

`internal/metadata.DecoratedTitle` stays the conservative provider-matching
form (USA/Europe/Japan/World and `Rev X` only). Catalog dump parsing is the
browse authority and may understand the broader tag set above. Catalog must not
import `internal/metadata`.

Grouping is a query overlay. Arcade MAME parent/clone and hashed DAT matching
wait.

## Decision 2 — Grouped query plane

Catalog schema version 4 is additive. Loopback `GET /api/v1/games` keeps
`q`, `platform`, `collection`, `sort`, `cursor`, `limit` and adds:

- `region`
- `hide_prerelease=1` (drops `beta`, `proto`, `sample`, `demo`)
- `hide_hacks=1` (drops `hack`, `unl`)
- `availability=ready|offline|all` (default `all`; `ready` is available and
  online)
- `genre`, `year` (from the facet columns; empty matches only empty)
- `grouped=1` (default for the browser) or `grouped=0`
- `collection=favorites|recents|continue|unplayed|recently_added`
- `sort=title|platform|year|recently_added`

Default `limit` remains 100; maximum 200. The browser must not dump the full
catalog.

When `grouped=1`, each page row is the **preferred child** of a `group_key`
among rows that survive the same filters. `variant_count` is the number of
surviving siblings. Cursor identity is `(sort, platform, canonical_title,
group_key)` so pagination is over groups, not files.

When `grouped=0`, each file is a row (CLI, version lists, tests).

List JSON stays privacy-safe: no paths, library IDs, or digests. Additive
fields: `canonical_title`, `region`, `revision`, `dump_flags`, `group_key`,
`variant_count`, `genre`, `year`. Existing identity fields remain.

`GET /api/v1/games/{id}` returns the file record plus a bounded `variants`
array (same group, same filters as an ungrouped group query, cap 50) so the
detail panel can pick a dump. Launch still uses the selected file `game_id`.

Preferred region comes from `[library].preferred_regions` (default
`usa`, `world`, `europe`, `japan`). Tie-break: higher `revision` lexical, then
`game_id`. The UI must send region/flag/availability/genre/year/sort as query
parameters and must not client-filter the loaded page for those axes. The
Platform dropdown uses the same `platform=` parameter as the rail.

`GET /api/v1/library/facets` returns distinct non-empty `genre` and `year`
values from the catalog facet columns (bounded, sorted). No live metadata
`Lookup` on list or facet endpoints.

## Decision 3 — Continue, unplayed, recently added

User state remains in `library-user.sqlite3`. Play recording stays host-session
authority when a launch becomes `active`.

- `collection=continue`: most recently played surviving catalog row (grouped
  preferred child of that `game_id`'s group). Same store as recents; the UI
  may pin it.
- `collection=unplayed`: catalog rows whose `game_id` has no
  `last_played_at`. Implementation may exclude played IDs from the user store
  without attaching that database to catalog files.
- `collection=recently_added`: `ORDER BY first_seen_ns DESC, game_id` on
  catalog rows (grouped by default).

`collection=recents` and `favorites` keep their current meanings. A catalog
miss still hides the title and keeps the user row.

## Decision 4 — Cached presentation facet index

List rows must not live-`Lookup`. Schema v4 adds `genre`, `year`, and
`search_aliases` on `games`. `search_text` / FTS include folded canonical
title and aliases.

A host sync copies **already-cached** provider presentation (exact or
confident outcomes only) onto matching catalog rows by folded canonical title
and platform. It must not perform network fetches. The metadata cache cap
(10,000 records) remains; uncovered titles keep empty facets. Operator CLI
`fogcast facets-sync` runs this copy. The presentation detail path may write
the same columns when it already served a cache hit. Empty or fallback demo
metadata must not overwrite catalog facets.

No new network provider.

## Decision 5 — Per-platform host emulator cores

`protocol.System` stays `megadrive` and `snes`. Host-emulator launchability is
still a service policy in front of any target round-trip.

`[host_emulator]` gains per-platform cores:

```toml
[host_emulator]
binary = "/Applications/RetroArch.app/Contents/MacOS/RetroArch"

[[host_emulator.cores]]
platform = "nes"
core = "/cores/nestopia_libretro.dylib"

[[host_emulator.cores]]
platform = "gba"
core = "/cores/mgba_libretro.dylib"
```

The existing `core` plus `systems = [...]` form remains valid and means every
listed platform shares that one core. Mixing a legacy `core`/`systems` pair
with `[[host_emulator.cores]]` is rejected. Each platform appears at most once.
Binary and each core path must be clean absolute paths. Unmapped platforms stay
browse-only (`UNSUPPORTED_SYSTEM`, no target I/O).

## Decision 6 — Cue/gdi sets and host-only large content

Scanner: a `.cue` or `.gdi` is one catalog row. Referenced `.bin`/`.img`/
`.iso` members in the same directory are not separate games. Standalone
`.chd` remains one row. `source_kind` is `raw` for those primary files.

Host-only launch of a cue/gdi/chd, or of any available file larger than
`protocol.MaxContentBytes`, uses a **confined library path** inside the
already-authorized `os.OpenRoot` for that catalog root. RetroArch is started
with that path and the platform core. The host must not copy the set through
the 32 MiB prepare snapshot, must not upload it to the target cache, and must
not put filesystem paths in public JSON, logs, or the host/target protocol.

FPGA/target launch is unchanged: prepare remains raw or single-ROM ZIP bounded
by `protocol.MaxContentBytes`. Cue/gdi/chd and oversized files on FPGA-mapped
platforms return a privacy-safe unsupported/invalid content error rather than
raising the protocol cap.

The launcher enables Launch for available, launchable, online titles even when
`content_prepared` is false. The service already prepares (or path-launches)
on demand. `content_prepared` remains an informational list field.

## Verification

Software-tested: dump parse fixtures; schema v4 migrate; grouped vs ungrouped
cursors; ≥10k synthetic dumps with grouping and region filter staying bounded;
preferred-region pick stable across rescan; launch body unchanged; no path
strings in public JSON; host-emulator per-platform core selection; cue/gdi
sibling skip; host-only oversized/cue launch does not call target upload;
unplayed/continue/recently_added; facet copy from cache only; UI sends filter
query params and does not client-filter region/genre/year. Independent Vega
review of this contract, grouping identity, media/path confinement, and the
host API. No hardware claims.

## Rollback

Revert the host branch. Schema v4 is additive; a v3-only binary refuses the
newer database (existing catalog guard). User-library and media indexes are
unchanged files. Preferred-region config is host-local.
