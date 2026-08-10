# Frontend Metadata Adapter and Demo Data Design

**Status:** Approved on 2026-08-09.

## Purpose

Build the polished FogCast launcher against an explicit frontend metadata
boundary before a real metadata provider or scraper exists. The launcher must
remain useful with the current host catalog, show deterministic artwork and
descriptive demo content, and avoid coupling presentation work to scraping,
third-party services, or target-runtime contracts.

This is a frontend-only phase. It does not change the public host/target
protocol, catalog identity, launch semantics, target lifecycle, or hardware
ownership.

## Current Context

The local FogCast page is served by `internal/hostapi.UIHandler` as a
self-contained document. It reads the existing local application endpoints:

- `GET /api/v1/games` and `GET /api/v1/games/{id}` provide catalog identity,
  title, system, source state, source availability, and prepared-content state.
  Their `execution` field is retained only as unreliable compatibility data for
  this milestone and is not a frontend policy input.
- `POST /api/v1/session/launch` accepts the selected catalog `game_id`.
- Health and session endpoints remain the authority for operational state and
  actual launch resolution.

The current API intentionally has no artwork or rich descriptive metadata.
Adding demo presentation data to that API would blur the catalog and
presentation boundaries and would create a backend contract before a real
metadata source has been designed.

## Decision

Introduce a documented frontend `GameMetadataAdapter` boundary. The first
implementation is a deterministic demo adapter. The live FogCast catalog stays
authoritative for ID, title, system, source state, source availability,
prepared-content state, and live launch state; adapter results are presentation
only. Catalog `execution` remains unreliable compatibility data: the launcher
hides/defers it and never uses it for eligibility, authorization, launch
identity, metadata merge, or user-facing execution claims. Backend semantic
correction is deferred to a separately scoped follow-up.

The launcher combines the catalog record and adapter result into a view model:

```text
live catalog game ──┐
                    ├── launcher game view model ──> rendered launcher
metadata adapter ───┘
```

The merge has one direction: metadata enriches a catalog game but cannot
replace its ID, title, system, availability, prepared-content state, or launch
state. The unreliable catalog `execution` value may remain untouched in the
wire-shaped record for compatibility, but it is not consumed by the metadata
merge, eligibility, authorization, rendering, or launch identity. Launching
continues to send the unmodified live catalog `game_id`; the backend remains
the authority for launch resolution.

## Frontend Interfaces

The conceptual adapter interface is:

```text
GameMetadataAdapter.metadataFor(game) -> GamePresentationMetadata
```

`game` contains only the public catalog fields already returned to the page.
`GamePresentationMetadata` contains:

- cover artwork;
- backdrop artwork or visual treatment;
- short summary;
- release year;
- genre;
- developer or publisher label; and
- player-count label.

Artwork values are renderable frontend resources, not filesystem paths or
target identifiers. The demo implementation uses only bundled or locally
generated resources. The interface must remain compatible with a future
implementation that receives normalized metadata from a host-side provider,
but this design does not specify that provider or authorize browser-side
scraping.

The launcher view model holds the original catalog fields alongside normalized
presentation fields. Normalization supplies safe defaults for every optional
metadata value so rendering does not need provider-specific branches.

## Deterministic Demo Data

The demo adapter has two layers:

1. A small curated table provides richer sample metadata for recognized
   system-and-title combinations.
2. Every other real catalog entry receives deterministic fallback metadata
   derived from its stable game ID and system.

Fallback generation selects a stable palette, graphic treatment, and
system-appropriate labels. The same catalog record therefore renders the same
way across refreshes and test runs. It must not use randomness, current time,
network access, ROM contents, local paths, or private target information.

Generated artwork is intentionally identifiable as demo presentation rather
than scraped or authoritative box art. User-controlled strings are inserted
through safe DOM text operations or explicit escaping; they are never treated
as markup.

## Data Flow and UI States

On load or search, the launcher requests live games from the existing local
API. Each returned game is passed to the metadata adapter and normalized into a
launcher view model before list rendering. Selecting a game may refresh its
live detail record, after which the same metadata merge is reapplied. Launching
uses only the selected live game ID.

The UI distinguishes:

- initial loading;
- a populated catalog;
- an empty catalog;
- no search matches;
- catalog/API failure;
- metadata fallback; and
- launch progress, success, and failure.

Metadata failure is non-fatal. The affected game receives deterministic
fallback presentation while catalog browsing and launch controls continue to
work. Catalog or launch failures retain the existing privacy-safe API error
messages and provide an actionable retry path.

Search remains backed by `GET /api/v1/games?q=...`; the demo adapter does not
invent games or alter search membership. UI filtering, selection, and launch
state must not depend on artwork availability.

## Privacy, Security, and Portability

The browser makes no third-party requests. The phase introduces no API keys,
tracking, remote fonts, browser-side scraping, or external image origins. It
does not expose catalog source paths, ROM fingerprints, target identities, or
target-private implementation details.

The existing loopback-only host policy and no-store response behavior remain
unchanged. Presentation metadata is not added to the versioned host/target
protocol. A future metadata backend must be designed separately and preserve
these boundaries.

## Testing and Verification

Focused tests will cover:

- the page remains self-contained and served with the existing content type;
- metadata mapping is deterministic for the same game;
- curated metadata is selected only for its intended system/title match;
- unmatched games always receive complete fallback presentation;
- catalog identity and approved operational fields win during the merge, while
  unreliable catalog `execution` is never used as frontend policy;
- rendered catalog and metadata strings follow an explicit safe-text path;
- empty, no-match, catalog-error, and metadata-fallback states are present;
- launch requests retain the exact `{ "game_id": <live catalog ID> }`
  contract; and
- no external metadata or artwork origin is introduced.

Run the narrow `internal/hostapi` tests first, followed by the applicable Go
test suite, formatting, static checks, `git diff --check`, and repository status
inspection. These checks provide software evidence only and make no HIL claim.

## Scope Boundaries

Included:

- the frontend metadata interface and normalization boundary;
- deterministic curated and fallback demo metadata;
- demo artwork suitable for the launcher;
- integration with the launcher list/detail presentation; and
- focused frontend-serving and contract tests.

Excluded:

- browser-side scraping;
- a server-side scraper or metadata database;
- third-party credentials or network dependencies;
- catalog schema or public API expansion;
- changes to session, media, input, target lifecycle, or hardware behavior;
- claims that demo descriptions or artwork are authoritative; and
- committing, pushing, deploying, or operating hardware without separate
  authorization.

## Replacement Path

When real metadata work begins, it should implement the same normalized
frontend boundary, preferably from a privacy-reviewed host-side source. The
launcher consumes that replacement without changing catalog identity,
selection, search membership, or launch behavior. Demo fallback remains useful
for missing provider records and offline development unless a later approved
design replaces it.
