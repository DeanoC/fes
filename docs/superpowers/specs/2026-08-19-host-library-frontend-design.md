# Host library frontend for large collections

**Status:** Designed on 2026-08-19.

**Role:** Sol architecture/security owner. This specification is the executable
host-only contract for catalog platforms, paginated library queries, user
library state, operator-owned local media, and host-display attract. It does
not change the public host/target protocol, FPGA ownership, codecs, or POC6
claims.

This document does not amend
[the 2026-08-09 frontend metadata adapter design](2026-08-09-frontend-metadata-adapter-demo-data-design.md).
Catalog identity remains authoritative; presentation and local media only
enrich.

A short architecture note lives at
[`docs/architecture/2026-08-19-host-library-frontend.md`](../../architecture/2026-08-19-host-library-frontend.md).

## Purpose

Make the Mac loopback launcher a useful controller for a large personal
library: many platforms, tens of thousands of titles, favorites, operator-owned
still and video media, and idle attract on the host display. The MiSTer stays
UI-free. Evidence for this work is **Software-tested** only.

## Non-goals

- Expanding `protocol.System` or adding FPGA cores / host-target launch
  systems.
- EmuMovies, any network media provider, pack importer, FTP, or browser
  third-party fetches.
- A distinct 10-foot/gamepad-first app, Electron, or a Node SPA.
- Attract or library UI on the target.
- HIL, latency, or controller-symmetry claims.

## Decision 1 — Host catalog platforms vs protocol systems

`protocol.System` remains the launch/target vocabulary and stays `megadrive`
and `snes` until a separate Sol decision expands it.

The host catalog gains a **platform registry**: id, display label, ROM
extensions, and optional launch mapping onto `protocol.System` or a configured
host emulator system. Configured `[[libraries]]` validate against this
registry, not `protocol.ValidateSystem`. Game identity continues to include the
platform id string; existing Mega Drive and SNES `game_id` values stay stable.

Scanner extension lookup uses the host platform registry. `internal/core`
remains the FPGA core comparison registry and is not the source of browse
platforms.

Launch is capability-gated in the host service **before** any target probe,
upload, or launch:

- mapped FPGA or configured host-emulator platforms may launch through the
  existing session body `{ "game_id": "..." }`;
- unmapped platforms are browse-only; Launch is disabled in the UI and the
  host API returns `UNSUPPORTED_SYSTEM` without a target round-trip.

## Decision 2 — Query plane and user-library contract

Catalog schema version 2 adds FTS5 over folded title/id/platform text and
`QueryGames` with opaque cursors. Schema version 3 recreates
`games_system_title_id` as `(system, lower(title), game_id)` so title-ordered
queries can use the index, and rewrites `search_text` with the same Go Unicode
fold used at query time.

Loopback `GET /api/v1/games` accepts `q`, `platform`, `collection`, `sort`,
`cursor`, and `limit`. Default `limit` is 100; maximum is 200. The browser
must not dump the full catalog. CLI `fogcast games` / `search` may still read
the unbounded store APIs.

List items stay privacy-safe and **must not** perform live metadata `Lookup`.
Cover on a list row comes only from the local-media index or an already-cached
metadata artwork handle. Extra list fields (`favorite`, `cover`, `launchable`,
`platform`) are additive; existing identity fields remain so current launcher
parsers keep working.

User state lives in a **separate** SQLite file
(`library-user.sqlite3`) so ROM rescans never rewrite favorites:

- `favorite` / `favorited_at`
- `last_played_at` / `play_count` recorded by the host session coordinator
  when a launch becomes `active`, not by the UI
- `collection=favorites|recents` on the games query
- `PUT` / `DELETE /api/v1/library/favorites/{id}`

A catalog miss hides the title from browse/favorites UI and keeps the user row
until explicit cleanup. Paths never enter user-state rows or public JSON.

`GET /api/v1/platforms` lists configured catalog platforms with counts, online
flags, and `launchable`.

## Decision 3 — Operator-owned local media

Config uses `[[library_media]]` roots. The existing `[media]` table remains
the RTP/cast transport and must not be overloaded.

Matching is host-side only, first hit wins:

1. `{platform}/{game_id}/{role}.{ext}`
2. `{platform}/{rom_stem}/{role}.{ext}` (stem from `relative_path`, never
   exposed)

Roles: `cover`, `logo`, `marquee`, `screenshot`, `backdrop`, `video`.

Scan uses `os.OpenRoot`, rejects symlinks, and never lists directories to the
browser. Stills (JPEG/PNG, ≤ 8 MiB) are decoded and re-encoded into a private
cache, then served as opaque 64-hex handles. Video (`video/mp4`, `video/webm`,
≤ 128 MiB) is magic-byte checked and Range-served from the confined root
without transcode.

`GET /api/v1/presentation/games/{id}` overlays local stills on provider stills
when both exist. Text metadata still comes from the existing provider.
`GET /api/v1/presentation/media/{handle}` serves local stills and video with
the same loopback, `nosniff`, and no-store (or short private cache) pattern as
artwork. Missing or invalid media is non-fatal.

This is not an EmuMovies pack importer: no upstream folder schema, credentials,
or network client.

## Decision 4 — Launcher shell and attract

Keep the Go-embed, loopback, vanilla document. Playnite-style layout: platform
rail, virtualized cover wall, rich detail. Search hits the paginated API.
Attract is a host-display overlay after idle (default 60s) using
`GET /api/v1/library/attract?limit=` (favorites with media, else recents with
media, else a bounded sample of titles that have media — never the full
catalog). Prefer muted video; `prefers-reduced-motion` forces stills. Pointer,
keyboard, or focus exits immediately. Attract may run while a TV session is
active. Choosing another title uses existing replace-session. Attract must not
start a target session by itself.

Catalog-wins merge, safe DOM, reduced-motion, and same-origin rules from the
2026-08-09 metadata design remain in force.

## Verification

Software-tested: catalog FTS/cursors, ≥10k synthetic rows, favorites across
rescan, media confinement (symlink, escape, oversize, wrong MIME, Range),
unchanged launch body, UI virtualization and attract enter/exit, no path
strings in public JSON or DOM. Independent Vega review of this contract and of
media serving plus the host API. No hardware claims.

## Rollback

Revert the host branch. Catalog v2/v3 migrate is additive; a rollback binary that
only understands schema v1 will refuse newer databases (existing catalog
guard). User-library and media indexes are separate files and can be deleted
without dropping the ROM catalog.
