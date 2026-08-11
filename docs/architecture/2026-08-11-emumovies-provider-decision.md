# Provider composition decision: LaunchBox metadata and gated EmuMovies media

Date: 2026-08-11
Status: Designed; independent Vega review pending
Disposition: **CONDITIONAL-GO**

This decision is the executable architecture and security contract for replacing
the active IGDB/Twitch integration. It is subordinate to
[`docs/ARCHITECTURE.md`](../ARCHITECTURE.md),
[ADR 0001](../adr/0001-portable-target-runtime.md), and accepted evidence. It
does not change the public host/target protocol, target runtime, hardware
ownership, launch semantics, or the current audio worker.

The sole disposition applies to the provider-composition direction as a whole:

- implementation may replace IGDB with the documented LaunchBox anonymous HTTPS
  snapshot and fixed-host still-image path, using fixtures first and remaining
  disabled by default;
- no EmuMovies network adapter, downloader, FTP-family client, local-media-pack
  importer, or browser media route is authorized by this disposition; and
- release enablement, live EmuMovies work, and Phase 3 remain gated below.

## Grounding and authority

Every external-provider premise in this decision comes from the imported,
verified [provider evidence report](../research/2026-08-11-emumovies-provider-evidence.md).
The report is research plus bounded machine observation only. In particular:

- the LaunchBox founder-directed daily XML snapshot, intended third-party use,
  current anonymous HTTPS archive, exact supported platform names, and
  direct-image construction are grounded in the report's
  “LaunchBox evidence and authority weighting,” “Current LaunchBox capability
  matrix,” and machine-observed artifact manifest;
- the absence of current public LaunchBox retention, attribution, rate,
  mirroring, redistribution, and CDN policy is grounded in “Release gates and
  unresolved risks”;
- EmuMovies membership, Sync, FTP, API, platform/media, naming, reposting, and
  transport uncertainty are grounded in “EmuMovies evidence” and its route
  matrix; and
- the user-observed TLS indication on the authenticated FTP details page is only
  an operator observation. It is not accepted protocol evidence.

No endpoint, authentication flow, schema, right, platform identifier, or
transport feature may be inferred beyond that report. A later contradictory
first-party term or instruction supersedes the corresponding provider premise
and must fail closed.

| Provenance item | Exact value |
| --- | --- |
| Decision role | Sol architecture/security owner |
| Actual model / provider | `gpt-5.6-sol` / `openai-codex` |
| Fallback | none |
| Worktree branch | `wt/t_36b32cb5` |
| Authoritative base and origin/main at start | `701eb53cac0400deb772dfa4b1417bfd52df6516`; drift `0/0` |
| Imported parent commit | `b19b421bae9a5892079b24a9468bbcd6054b7d24` |
| Imported parent tree | `6999bab556e980bbef0e6f95eaed86136e07abca` |
| Imported report SHA-256 | `90c8b8a508b6ec3f755b311ff8da3e1188d4b0326f24578ec9285f1e7618bf9b` |
| Retained archive independently re-inspected for this repair | `/tmp/t_6f40d61b-citations/Metadata.zip`; SHA-256 `627e9b0c55554ec232fc32ce50272b530dc5d30c7b179cbe09f7ec58b46925dd` |
| Decoder implementation inspected for this repair | Go `1.26.5` `encoding/xml` |

The retained-archive reinspection is narrower and more complete than the
report's original field probe: it enumerated every top-level family in both
consumed XML members. It found 69,311 `GameAlternateName` records and duplicate
platform-family projections in `Metadata.xml` and `Platforms.xml`. The exact
member authority and reconciliation rules below supersede the report's
“no observed alternate-name field” observation without rewriting that
historical research artifact.

## Why this disposition is conservative and implementable

The LaunchBox path is adequately specified for a bounded adapter without
endpoint, authentication, or archive-schema guessing: the first-party founder
identifies the downloadable local-processing path and image filename contract,
and the current anonymous archive and image path were retrieved and inspected.
The implementation must inspect and version the live archive rather than assume
the historical file count.

The condition remains because no current public LaunchBox policy was located
for cache retention, attribution, request rate, CDN use, mirroring, or
redistribution. Code and fixture validation may proceed, but default enablement
and release require the live-validation gate in this decision.

EmuMovies cannot yet be implemented safely. Public material does not establish
a current independent-frontend API contract or the exact FTP-family protocol,
TLS mode, encrypted data-channel behavior, server identity, listing/resume
semantics, quotas, retention, or custom-client permission. The Windows Sync
application proves a user workflow, not a reusable protocol. Website scraping,
private-endpoint probing, historical client emulation, and guessed FTP behavior
are not substitutes.

The smallest compliant product is therefore LaunchBox metadata/still artwork
plus the existing truthful disabled/offline fallback, with the EmuMovies media
capability absent until separately unlocked. This supersedes retaining IGDB as
an active rollback provider: rollback is the disabled provider-neutral fallback
and the pre-change Git revision, not dormant Twitch/IGDB network code.

## Current source truth

The source at base `701eb53cac0400deb772dfa4b1417bfd52df6516` has these
implemented boundaries:

- `internal/metadata/types.go` defines a provider-neutral candidate/result
  surface, but has a single `ProviderIGDB` identity and an artwork reference
  containing only role plus IGDB image ID.
- `internal/metadata/provider_igdb.go` owns Twitch token exchange, IGDB queries,
  platform mapping, limits, retries, and wire translation.
- `internal/metadata/runtime.go` is materially IGDB-specific in admission,
  cache keys, attribution, provider removal, artwork suppression, and counters.
- `internal/metadata/artwork.go` hard-codes IGDB transforms and
  `images.igdb.com`, while already enforcing bounded bytes, MIME agreement,
  dimensions, pixel count, full decode, sanitizing re-encode, atomic publication,
  descriptor-relative storage, and digest verification.
- `internal/metadata/transport.go` denies ambient proxies, cookies, redirects,
  private/reserved DNS answers, non-443 ports, and non-allowlisted IGDB/Twitch
  hosts.
- `internal/metadata/cache.go` uses schema v1 and provider/versioned cache keys,
  but reads only IGDB rows, binds the cache to an IGDB credential digest, and
  owns complete descriptor-confined purge and artwork garbage collection.
- `fogcast/config.go` accepts only `provider = "igdb"`, requires client ID and
  secret, and validates the already-opened config source as a non-symlink regular
  file with exact mode `0600`.
- `cmd/fogcast-api/main.go` composes and closes metadata before the newer media
  and remote-input owners, preserving reverse-order teardown, but the current
  `metadata.Runtime` has no post-composition activation method. The replacement
  contract below adds that seam rather than permitting `Open` to start work.
- `internal/hostapi/server.go` resolves presentation only after authoritative
  catalog lookup, serves only opaque same-origin artwork handles, and accepts an
  exact one-field launch body containing `game_id`.
- `internal/hostapi/ui_app.js` keeps catalog/search/detail/selection/session and
  launch authority in the live API, rejects stale same-ID title/system
  presentation, and currently accepts only IGDB attribution.
- the current tests cover matching ambiguity, cache provenance and purge,
  descriptor and symlink attacks, lifecycle cancellation/reaping, safe errors,
  browser fallback, same-origin artwork, stale selection, and exact launch body.

This decision preserves those accepted boundaries while replacing their
provider-specific assumptions.

## Capability ownership and precedence

Provider composition is per capability, never per game authority.

| Capability | Sole authority after this change | Precedence and failure rule |
| --- | --- | --- |
| Catalog membership, ID, title, system, source state, availability, search, selection, detail | Existing FogCast catalog/service | Provider data cannot add, remove, rename, reorder, or make a game launchable. |
| Session, generation, target selection, launch, stop | Existing FogCast service/session composition | Unchanged; launch request remains exactly `{ "game_id": "<live catalog ID>" }`. |
| Summary, year, genres, studios, players | LaunchBox normalized snapshot | Empty remains empty. No EmuMovies or demo value may be labelled provider data. |
| Cover and backdrop still images | LaunchBox image records and fixed image host | Lazy, scored, integrity-checked; failure affects only that artwork role. |
| Video snaps, manuals, logos, special media | Future EmuMovies media provider | Capability is absent now. A future adapter may supplement only these fields. |
| Disabled, unconfigured, syncing, no-match, ambiguous, offline fallback | FogCast presentation layer | Explicit fallback state; it never changes live catalog or launch authority. |
| Audio worker and current host-cast media transport | Existing composition | Untouched. Library video assets are presentation media, not cast transport. |

LaunchBox does not become a catalog. EmuMovies never supplies descriptive text
unless later first-party evidence documents such a field and a new reviewed
decision assigns that capability. Provider precedence cannot be configured by
arbitrary operator strings.

## Truthful internal DTOs

Do not force a media-only provider through the current full-metadata
`Candidate` contract. Refactor internal types into two closed capabilities:

### Metadata capability

`MetadataCandidate` retains only:

- provider-scoped game ID;
- canonical name and documented alternate names;
- exact provider-local platform identities;
- optional summary, release year, genres, studios, and players;
- typed still-artwork candidates;
- source generation, provider update marker, and record checksum.

For the retained LaunchBox archive, `GameAlternateName` is a top-level record
family, not a child field of `Game`. Its exact member authority, linkage,
deduplication, and limits are specified below. `Overview`, `ReleaseYear`,
genres, developer then publisher, players, alternate names, and still-image
records map only under that accepted grammar. `ReleaseDate` is intentionally
ignored and never supplies a year. Missing or malformed optional scalar fields
remain absent as specified below.

### Media capability

A future `MediaCandidate` is separate and contains only:

- `Provider`, provider-scoped match ID, and exact platform identity;
- `Kind` from a closed set (`video_snap`, `manual`, `logo`, or a newly reviewed
  addition);
- documented region and variant labels;
- an opaque provider asset ID, never a URL or local path;
- source generation and available integrity metadata; and
- optional technical facts actually inspected after download: MIME, byte size,
  dimensions, duration, and content digest.

It has no summary, year, genre, studio, players, catalog ID, availability, or
launch fields. Before an EmuMovies contract is accepted, this interface may
exist only as a tested provider-neutral type; no EmuMovies implementation or
public media projection may exist.

### Browser projection

The current public presentation DTO remains text plus opaque cover/backdrop
handles and attribution. A future accepted media milestone may add a closed
`media` array with only `kind`, opaque same-origin `handle`, safe MIME, region,
and attribution. It must not include an upstream URL, provider path, filename,
credential, host, query, remote response, or local cache path.

## Deterministic matching and stale safety

Matching is always constrained to the exact protocol system before title
comparison:

1. Map `protocol.SystemSNES` only to `Super Nintendo Entertainment System` and
   `protocol.SystemMegaDrive` only to `Sega Genesis` for the current snapshot.
2. Compare the normalized live catalog title to a provider canonical title.
3. Compare accepted `GameAlternateName` values linked to that exact game and
   platform under the member-specific grammar below.
4. Apply only the existing reviewed decorated-title normalization for approved
   terminal decorations.
5. If no unique candidate wins the highest tier, return `ambiguous` or
   `no_match`. XML order, image order, recency, and provider ID are only stable
   tie ordering; they cannot break a semantic tie.

Fuzzy/edit-distance matching cannot auto-attach metadata or media. A later fuzzy
feature requires its own reviewed threshold, fixture corpus, false-positive
measurement, and explicit ambiguous path.

Deduplication remains provider-scoped. Two records with the same provider ID but
different canonical name, platform, source generation, or identity checksum are
an invalid response, not “first wins.” Cache publication carries a provider
epoch and source generation. A provider switch, generation replacement,
disable, purge, or removal increments the epoch before cancellation; an
in-flight old epoch cannot publish into the replacement. The UI continues to
bind presentation to exact live `(id, title, system)` and discards a late
response after any same-ID title/system refresh or selection revision.

Region affects media selection, not catalog identity. Still-artwork order is:
requested region, `World`, empty region, then lexical region/file-name order,
within the type order fixed in the evidence report. Equal top candidates are
ambiguous for that media role; they do not change metadata match outcome.

## LaunchBox acquisition and normalized index

### Fixed transport

The adapter may request only:

- anonymous HTTPS `gamesdb.launchbox-app.com/Metadata.zip`; and
- anonymous HTTPS image assets on `images.launchbox-app.com`, constructed from
  a current indexed image filename as one escaped path segment.

Origins are compiled constants, not config. HTTPS port is 443. Ambient proxies,
cookies, credentials, authentication headers, alternate schemes, userinfo,
fragments, queries, redirects, and arbitrary paths are forbidden. DNS is
resolved per new connection; every answer must be public global unicast before
dialing a validated address while preserving the fixed hostname for Host and
TLS server-name validation. TLS requires normal chain and hostname validation
and TLS 1.2 or newer. No generic URL-fetch endpoint is added.

A current index filename is eligible only if its UTF-8 bytes and Unicode scalar
values both match this immutable ASCII grammar:

```text
[A-Za-z0-9][A-Za-z0-9_-]{0,38}\.(jpg|jpeg|png|gif|webp)
```

The extension is lower-case and the complete filename is therefore at most 44
bytes and 44 runes. The allowlist is syntactic; a response still has to pass the
existing MIME agreement and full-decoder policy, so an installed decoder is not
inferred from an extension. Empty names, a 40-character stem, uppercase or
unknown extensions, non-ASCII, invalid UTF-8, slash, backslash, dot segments,
percent (including `%2f` in either case), `?`, `#`, control, space, colon, and
userinfo delimiters are rejected as `invalid_response` while building the new
generation. No rejected filename is persisted or reaches a transport.

For an eligible filename, construct `url.URL` from constants with `Path` equal
to `/` plus that filename and serialize it as exactly one escaped path segment;
do not concatenate a raw URL string and do not accept a pre-escaped filename.
Parse the serialized result again immediately before request construction and
require: non-opaque `https`; no userinfo; hostname exactly
`images.launchbox-app.com`; effective port exactly 443 with no other explicit
port; `EscapedPath()` exactly `/` plus `url.PathEscape(filename)` and no other
path prefix or segment; empty `RawQuery`, `ForceQuery`, and `Fragment`; and the
same filename grammar after path unescape. Any mismatch is `policy_blocked` and
causes zero network requests. The filename can never teach the adapter a host,
scheme, port, path prefix, query, or fragment.

#### Test-only transport seam

Production `Open` and `newLaunchBoxProvider` accept no endpoint, URL, host, port,
resolver, dialer, root-CA, HTTP-client, private-address, or allowlist override.
They construct an unexported `launchBoxTransportPolicy` only through
`newProductionLaunchBoxTransportPolicy()`, which seals the two compiled HTTPS
origins, port 443, exact archive/image paths, public-address requirement, normal
system roots, and no-proxy/no-cookie/no-redirect behavior described above. No
config field, option, interface implementation, environment variable, CLI flag,
or package export can replace that production policy.

Loopback tests use a helper compiled only from a same-package `_test.go` file:
`newLoopbackLaunchBoxTransportForTest(t, tlsServer)`. The helper creates a
separate unexported policy object bound to that one `httptest` TLS certificate,
exact loopback host, exact ephemeral port, and the same fixed archive/image path
shapes. It may replace only public-global-unicast admission with that exact
loopback peer and normal roots with the one test CA. It still requires HTTPS,
rejects redirects/proxies/cookies/userinfo/query/fragment, applies the filename
grammar and one-segment/final-URL checks, and cannot be passed to `Open` or the
production provider constructor. Tests exercise the internal transport directly
through that object and separately prove the production constructor rejects the
same loopback address, non-443 port, test root, and private DNS answer. Shipping
files contain no loopback exception or mutable production allowlist.

### Refresh lifecycle

- `metadata.Open` is an inert constructor. It may acquire and validate the
  provider root, open the last validated generation, build in-memory limiters,
  and register cleanup ownership, but it starts zero goroutines, timers, refresh
  jobs, or image jobs and performs zero network requests. Disabled or
  unconfigured configurations retain their current nil-runtime/purge behavior.
- Enabled non-nil runtimes use an internal composition-only interface:
  `type ActivatableRuntime interface { Runtime; Activate() error }`. `Open` and
  the `metadataOpener` seam return `ActivatableRuntime`; `hostapi.WithMetadata`
  receives it only as the existing `Runtime` lookup/artwork/close view. This
  avoids adding activation to the handler dependency surface. `Activate` is
  concurrency-safe and idempotent: the first call performs a bounded local
  activation transition. Successful concurrent or later calls return nil
  without another worker or request, and
  a call after `Close` returns `canceled`. It does not wait for a snapshot body.
  An activation failure is terminal for that runtime and is returned as
  `storage_failure` unless closure won the race and returns `canceled`; repeated
  calls return the same result without retrying.
- Activation has an internal commit gate. All local preflight and worker
  registration that can fail completes before the worker is released to use a
  transport. If activation fails after allocating partial worker state, it
  releases no request gate, cancels that state, and joins it within the same
  2-second ownership bound as `Close`. A join timeout transfers the still-inert
  state and root ownership to the reaper and returns `storage_failure`; it does
  not make the worker request-capable or permit a new startup. Thus failed
  activation produces zero requests and no unowned worker.
- `composeAPI` owns the runtime returned by `Open`, appends `Close` immediately,
  and supplies it to the handler as a non-owning dependency. It constructs every
  later media, target-cast, receiver, remote-input, and final handler dependency
  first. Only after the final handler exists does composition call `Activate`.
  The no-remote-input return path follows the same ordering; there is no early
  return before activation.
- If a later dependency or `Activate` fails, composition returns no handler and
  closes every acquired owner in reverse order. Activation failure closes
  remote input first when present, then media, then metadata. Cleanup failure is
  joined/redacted under the existing generic composition error and never turns
  a failed composition into success. No caller other than composition may
  activate or close the runtime.
- After successful activation, if no generation exists, exactly one
  provider-owned asynchronous bootstrap worker is admitted. Lookups remain
  explicit `syncing`/offline until promotion; a request handler never downloads
  a 100 MiB archive synchronously. If a validated generation is due, activation
  may admit the single normal conditional refresh instead. Success does not
  admit both jobs.
- If a generation exists and is due, continue serving it while exactly one
  refresh runs. Use both `If-None-Match` and `If-Modified-Since` when available.
- Automatic or manual sync may issue at most one conditional snapshot request
  per 24-hour window. A manual request inside the window reports `not_due`; it
  does not bypass the ceiling.
- There is one archive transfer and one parser/index builder globally. Lazy
  image starts are limited to two concurrent requests and no more than one new
  request per 500 ms. Respect bounded `Retry-After`; retry a transient
  connection or 502/503/504 at most once.
- Snapshot connect/TLS/header deadlines are 5 seconds; body idle deadline is 30
  seconds and whole refresh deadline is 20 minutes. Image requests retain the
  current 8-second whole-request deadline.

### Archive and XML defenses

Enforce all limits while streaming, not only from headers:

- 256 MiB compressed archive maximum;
- at most 8 members;
- exactly bounded regular-file member names, with no absolute path, directory,
  link, encryption, duplicate, `..`, separator, or unknown path structure;
- 1 GiB total uncompressed, 768 MiB per member, and 25:1 maximum member and
  aggregate compression ratio;
- require `Metadata.xml` and `Platforms.xml`; ignore `Mame.xml` and `Files.xml`
  only after validating their archive entries and limits;
- parse only `Metadata.xml` and `Platforms.xml` as UTF-8. Each must begin at byte
  zero, with no BOM or leading whitespace, with exactly one XML declaration.
  Its version is exactly `1.0`; its effective encoding is UTF-8, expressed
  either by an ASCII-case-insensitive `encoding="utf-8"` pseudo-attribute or by
  omission of `encoding` (which is accepted only as the XML UTF-8 default in the
  absence of a BOM); and `standalone`, if present, is exactly `yes`. Reject
  another version or encoding, `standalone="no"`, a second/misplaced XML
  declaration, and every other processing instruction anywhere in the member;
- keep `encoding/xml` strict and reject directives/DTD, entities, malformed
  UTF-8, duplicate required fields, oversized frames, strings, lists, and
  numeric values. Never resolve a network or filesystem entity. The bounded
  lexical reader below runs before `encoding/xml`; count limits are checked
  before allocating or persisting the next item;
- cap aggregate parsed start elements across the two consumed members at
  16,000,000; nesting depth at 8; attributes at 8 per start element and 4,096
  aggregate; element and attribute names at 64 bytes and 64 runes; each
  attribute value at 1,024 bytes and 256 runes. Unknown leaf fields are streamed
  past without persistence, but still consume depth, attribute, element,
  lexical-frame, and member-byte budgets;
- require exactly one zero-attribute `LaunchBox` root in each consumed member.
  A recognized top-level record contains only zero-attribute leaf child fields;
  nested grandchildren, attributes on schema elements, mixed-content schema
  records, and unknown top-level record families are `invalid_response`; and
- stream selected fields into a new provider-owned SQLite index. Never expand
  XML into the ROM root or an ambient temporary directory.

#### Pre-decoder lexical framing

`encoding/xml.Decoder.Token` is not itself a pre-allocation size boundary. Go
1.26.5 reads character data, comments, CDATA, processing instructions, and
quoted attribute values into an internal `bytes.Buffer` before returning a
token. Therefore caller-side `len(token)` checks are forbidden as the primary
limit.

Each consumed member must instead pass through an unexported
`framedXMLReader` before it reaches `xml.NewDecoder`. The reader is a streaming,
quote-aware finite-state lexical guard over the decompressed member. It
implements `io.Reader` and `io.ByteReader`, buffers at most one complete frame,
does not emit any byte of a frame until that frame and its terminator have been
validated, and never returns bytes from two frames in one `Read`. Its frame
classes and inclusive raw limits are:

| Lexical frame | Inclusive limit |
| --- | ---: |
| Initial XML declaration, including `<?xml` and `?>` | 256 bytes |
| Contiguous ordinary character data between markup | 131,072 bytes |
| CDATA payload | 131,072 bytes; 131,084 bytes including delimiters |
| Comment payload | 131,072 bytes; 131,079 bytes including delimiters |
| Start or empty-element tag, including delimiters | 16,384 bytes |
| End tag, including delimiters | 128 bytes |
| Element or attribute name | 64 bytes and 64 runes |
| One quoted attribute value | 1,024 raw bytes and 256 decoded runes |
| One entity or character-reference spelling | 32 raw bytes |

The guard uses one fixed 32 KiB input scratch plus the bounded current-frame
buffer and fixed counters; it never uses `io.ReadAll`, a scanner with a growing
token buffer, or a member-sized byte slice. Ordinary text and CDATA are emitted
as distinct frames. Built-in and numeric XML references cannot expand beyond
their raw spelling; custom entities are unavailable because directives are
rejected. The guard validates UTF-8 incrementally, quote/comment/CDATA
terminators across input-chunk boundaries, the declaration policy above, name,
attribute-count, attribute-value, and raw-frame limits. `<!...>` other than
`<![CDATA[...]]>` and `<!--...-->`, and every `<?...?>` other than the one
initial declaration, are rejected as soon as their discriminator is known and
are never emitted.

At a frame's maximum the guard may emit it only after seeing its valid closing
delimiter. On the next byte it returns `invalid_response` before emitting any
byte of that offending frame to the decoder. EOF in a name, tag, quote, entity,
comment, CDATA section, or processing instruction is likewise rejected by the
guard before that incomplete frame is emitted. The strict decoder behind the
guard uses `Strict = true`, no `CharsetReader`, and no custom entity map, and
still owns namespace, start/end matching, XML character-range, and document
well-formedness validation. This composition keeps decoder allocations bounded
by already validated frames while preserving one-pass streaming into the
normalized index.

Tests instrument the guard's emitted-byte count and frame-buffer high-water
mark. For every max+1 case they must prove the decoder receives zero bytes from
the offending frame and the guard never buffers more than 131,084 frame bytes
plus its fixed 32 KiB scratch. Run each case with one-byte input, every split
position around the delimiter, and 32 KiB chunks. Cover ordinary text in known
ignored and unknown leaf fields, CDATA, comments, the 256/257-byte declaration,
1,024/1,025-byte quoted attributes, 64/65-byte names,
16,384/16,385-byte start tags, entity
references, unterminated boundaries, quote-like delimiters inside other frame
classes, and adversarial alternating token shapes. A semantic decoder error or
any guard error fails the temporary generation; no partial row is published.

#### Authoritative members and record families

`Metadata.xml` is authoritative only for `Game`, `GameAlternateName`, and
`GameImage`. It also contains platform-family mirrors plus two intentionally
ignored application/configuration families. Its complete accepted top-level
grammar and inclusive per-member record budgets are:

| `Metadata.xml` family | Authority and behavior | Maximum records |
| --- | --- | ---: |
| `Game` | authoritative and persisted for supported platforms | 250,000 |
| `GameAlternateName` | authoritative optional aliases linked to `Game` | 250,000 total; 64 referring to one game |
| `GameImage` | authoritative still-image candidates linked to `Game` | 2,000,000 total; 512 referring to one game |
| `Platform` | validation mirror only; never persisted from this member | 512 |
| `PlatformAlternateName` | validation mirror only; never used for protocol mapping | 1,024 |
| `Emulator` | intentionally unsupported; leaf fields streamed past | 128 |
| `EmulatorPlatform` | intentionally unsupported; leaf fields streamed past | 1,024 |

The `Metadata.xml` top-level-record maximum implied by those family budgets is
2,502,688. `Platforms.xml` is authoritative only for at most 512 `Platform`
and 1,024 `PlatformAlternateName` records, for a 1,536-record member maximum.
The combined top-level-record maximum is 2,504,224; all child elements also
consume the 16,000,000 aggregate start-element budget. Any other top-level
family, any family over its own cap, or either member over its aggregate cap is
`invalid_response`. The family cap is checked before allocating the next record.

Within each member, the trimmed `Platform.Name` is a unique exact key. Each
`PlatformAlternateName` has the unique exact trimmed key `(Name, Alternate)`,
whose `Name` must reference a platform in that same member. After both members
parse, the exact trimmed `Platform.Name` key sets and exact trimmed
`(Name, Alternate)` key sets must match across members. Identical copies are not
double-counted: only the `Platforms.xml` rows are authoritative. A duplicate
key within either member, missing key, extra key, cross-member mismatch, or
conflicting alternate link fails the generation. Non-name `Platform` leaf
fields are explicitly ignored and do not participate in mirror equality.
Protocol mapping still admits only the two exact canonical platform names; a
platform alternate never changes protocol identity.

Each `Game.DatabaseID` is unique and positive. Every `GameAlternateName` and
`GameImage.DatabaseID` must reference exactly one parsed `Game`; forward links
are held in a bounded provider-owned table and are validated before promotion.
For aliases, the exact tuple
`(DatabaseID, trimmed AlternateName, trimmed Region)` must
be unique. A whitespace-only alternate consumes record and per-game budgets but
is intentionally ignored; a non-empty alternate is persisted. Multiple exact
aliases that normalize to the same matcher key for one game collapse after a
stable sort by normalized key, exact UTF-8 alternate, then region. The same
normalized alias on different games remains on each candidate and therefore
produces normal matcher ambiguity rather than “first wins.” Region is retained
for checksum/provenance only and never changes catalog or match identity.

Every selected XML value is accumulated through one shared byte-and-rune
limiter before trimming, parsing, list splitting, hashing, or SQLite binding.
The following are the complete selected/persisted input limits; no generic map
may retain an unlisted XML field:

| XML value | Maximum bytes / runes | Additional syntax or list limit |
| --- | ---: | --- |
| `Game.DatabaseID`, `GameAlternateName.DatabaseID`, `GameImage.DatabaseID` | 19 / 19 | ASCII decimal `1..9223372036854775807`; no sign, zero value, or leading zero |
| `Game.Name` | 1,024 / 256 | required and non-empty after trim |
| `Game.Platform`, `Platform.Name`, `PlatformAlternateName.Name`, `PlatformAlternateName.Alternate` | 256 / 128 | required and non-empty; normalized index admits only the two exact mapped canonical platform values |
| `Game.Overview` | 65,536 / 16,384 | optional; trim only |
| `Game.ReleaseYear` | 4 / 4 | optional; exactly four ASCII decimal digits when used |
| raw `Game.Genres` | 4,096 / 1,024 | at most 64 semicolon-delimited entries; each trimmed entry at most 256 / 128; dedupe preserves order |
| `Game.Developer`, `Game.Publisher` | 1,024 / 256 each | at most the two ordered, distinct, non-empty studio entries are persisted |
| raw `Game.MaxPlayers` | 32 / 32 | optional; persist only ASCII decimal `1..999`, otherwise leave absent |
| `GameAlternateName.AlternateName` | 1,024 / 256 | trim; whitespace-only is counted then ignored; non-empty values enter the deterministic alias set |
| `GameAlternateName.Region` | 128 / 64 | trim; optional provenance label; no matching authority |
| `GameImage.FileName` | 44 / 44 | exact immutable filename grammar and extension allowlist above |
| `GameImage.Type` | 128 / 64 | must equal one of the closed ordered still-image types in this decision to be eligible |
| `GameImage.Region` | 128 / 64 | optional label used only for the closed region ordering |
| `GameImage.CRC32` | 10 / 10 | ASCII decimal `0..4294967295`; required before an image is eligible |

Generated source-generation IDs, record checksums, archive digests, and object
digests are lower-case 64-byte/64-rune SHA-256 hex values; an upstream value is
never copied into those fields. Normalized genre and studio lists, image rows,
and lookup DTOs retain the same per-entry and count limits after detaching from
the parser.

`Game.ReleaseDate` is explicitly outside the selected grammar. It is streamed
past under the 131,072-byte ordinary-text/CDATA frame bound, never trimmed,
parsed, persisted, normalized, or used as a fallback. Only a syntactically
valid four-digit `Game.ReleaseYear` supplies the optional year. Tests prove
that absent, empty, valid-looking, timezone-bearing, malformed, and overlong
`ReleaseDate` values do not affect the result; an overlong lexical frame still
fails the generation as a resource violation.

All maxima are inclusive. A syntactically valid value or structure exactly at a
maximum is accepted if every other invariant passes. The next byte, rune,
attribute, element, depth level, list entry, per-game image, or record is
detected before append/bind and fails the candidate generation as
`invalid_response`; the temporary generation is removed and the prior validated
generation remains current. A required field with invalid syntax has the same
generation-failing result. An optional year/player value that is within
its byte/rune cap but fails its optional value grammar remains absent, as
specified above; exceeding a resource cap is never downgraded to absence.

The retained archive reinspection observed 10,391,773 aggregate start elements;
186,580 `Game`; 69,311 `GameAlternateName` with at most 30 per game, one
whitespace-only ignored alias, and a 151-byte/123-rune longest alternate;
1,316,025 `GameImage` with at most 249 per game; 189 `Platform` and 431
`PlatformAlternateName` in each consumed member with equal key sets; 35
`Emulator`; and 98 `EmulatorPlatform`. The limits also exceed the later
reviewed snapshot's 1,373,559 `GameImage` records. Fixture tests must recompute
every top-level family count, aggregate depth/attribute/element count,
per-game alias/image count, selected-field maximum, filename maximum,
referential link, uniqueness property, and cross-member mirror equality. The
fixture does not change the constants. Any future snapshot over a constant or
outside the accepted member grammar fails closed and requires a new reviewed
decision rather than an operator-configurable override.

Record archive SHA-256, response validators, schema fingerprint, parser version,
record counts, and generation ID. Do not log XML, game names, image filenames,
or URLs.

### Generation promotion, rollback, and retention

The descriptor-confined metadata root owns a `providers/launchbox` subtree.
Every directory is `0700`; every regular file is `0600`; no symlink, hard-link
escape, special file, or changed ancestor/child identity is accepted.

Build each generation in a random bounded temporary child. Validate schema,
counts, SQLite integrity, expected platform mapping, source/archive hashes, and
all size ceilings, then fsync files/directories as supported. Promote by atomic
rename and atomically replace a regular-file current-generation manifest; never
use a symlink as the pointer.

Retain at most current plus one previous validated generation. The previous
generation is rollback-only and is removed after 24 hours and after all reader
leases close. Remove the downloaded archive immediately after successful index
promotion. Failed temporary generations are deleted at startup only after full
preflight. Each normalized index is capped at 1 GiB; total LaunchBox generation
storage is capped at 2 GiB. A successfully conditionally validated current
index may be served for at most 7 days without another successful 200/304.
After that it remains on disk for rollback/policy purge but presentation reports
offline rather than serving unvalidated provider facts.

Still artwork retains the existing 8 MiB compressed object, 4096-by-4096,
16,777,216-pixel, full-decode, termination, sanitizing re-encode, digest,
atomic-publication, and 512 MiB aggregate LRU limits. Verify the archive CRC32
against downloaded source bytes before decode. CRC32 is source-integrity
metadata, not a security signature; the sanitized local object remains keyed by
SHA-256.

## Cache migration and purge ownership

This is a destructive migration of disposable derived state, not an in-place
reinterpretation:

1. Increment cache schema and adapter versions before any LaunchBox lookup.
2. Acquire the existing physical root lease and descriptor identities.
3. Preflight every known SQLite sidecar, artwork object, and new provider child.
   Any symlink, special file, unexpected owned-child shape, or identity change
   fails startup with `storage_failure` without following or deleting it.
4. If schema v1, IGDB settings, IGDB credential scope, IGDB rows, IGDB artwork
   refs, or IGDB provider files are present, close database/VFS owners and purge
   the complete known derived root before creating schema v2. Do not copy old
   rows or artwork into LaunchBox keys.
5. Schema v2 keys include provider, cache schema, adapter, normalizer, platform
   map policy, exact platform, normalized title, region policy, source
   generation, provider game ID/checksum where positive, and artwork selection
   policy. Negative rows also include source generation.
6. Provider switch, disable, config-table removal, explicit purge, and
   `provider_removed` cancel refresh/image work, increment epoch, close all
   generation/database/artwork readers, and purge cache rows, generation
   directories, manifests, temporary files, archives, and unreferenced artwork.
7. Purge failure is fail-closed. The physical-root lease remains held by the
   reaper until all descriptors and VFS registrations retire; a new owner cannot
   reopen or reuse the root prematurely.

The metadata root is never shared with ROM, library, staging, target cache, or
user-authored media. Purge removes only preflighted provider-owned names. It
must not use ambient path traversal or recursive deletion after a parent swap.

## Disabled, offline, startup, and close behavior

Absent `[metadata]` is `unconfigured`; `enabled = false` is `disabled`; enabled
LaunchBox with no index is `syncing` then either ready or offline. All states
leave catalog, search, detail, selection, availability, session, and launch
usable.

Inert `Open` owns the root context, image limiter, generation leases, cache, and
artwork store but no running worker. Successful `Activate` may add exactly one
refresh/bootstrap worker. Composition order remains metadata first so reverse
teardown stops remote input and media dependants before metadata. `Close` works
before, during, or after activation and is idempotent: atomically mark closed and
increment epoch, prevent/revoke the activation request gate, cancel provider
work, close network transports to unblock I/O, join activation, refresh,
bootstrap, image, and lookup leaders, close generation and artwork readers,
close SQLite/VFS, then release the physical root. `Activate` and `Close` racing
must linearize to either one active owned worker followed by cancellation or no
worker; they can never publish after close.

The caller waits at most 2 seconds for all cancellation and joins. On timeout a
reaper retains every root/transport/worker ownership token until cleanup really
completes and `Close` returns `storage_failure`; no new startup may race that
retained owner. `Close` before activation completes without starting a worker,
and a second `Close` returns the first safe result without repeating cleanup.

Refresh failure preserves the last validated generation only within its 7-day
stale ceiling. Invalid archive/image data never replaces a validated object.
Unauthorized, policy-blocked, provider-removed, storage, canceled, deadline,
rate-limited, and invalid-response conditions remain separate safe error codes.
Raw causes and response bodies never reach the browser or logs.

## EmuMovies candidate transports and stop rule

The following comparison is architecture only. It authorizes no EmuMovies code.

| Candidate | Required evidence before implementation | Current action |
| --- | --- | --- |
| Official API | Current first-party endpoint, authentication, schema, stable IDs, SNES/Genesis mapping, quotas, cache/retention/attribution, production permission, and revocation behavior | Withhold adapter. Do not guess or use historical clients. |
| Explicit FTPS | Current official host/port, AUTH TLS behavior, PBSZ/`PROT P`, passive/EPSV, certificate identity, TLS minimum, listing/resume/checksum semantics, limits, and custom-client automation permission | Withhold downloader until all facts are documented and verified without secrets. |
| Implicit FTPS | Same facts plus official implicit-TLS port and data-channel contract | Withhold; do not infer from a “TLS” label. |
| SFTP | Current official SSH host key/algorithm policy, port, path, listing/resume semantics, limits, and automation permission | Withhold; public evidence does not establish SFTP. |
| Plain FTP | None can provide confidentiality or authenticated content integrity | Forbidden in this decision. No silent downgrade. A separate explicit security decision would be required even if the vendor documents it. |
| Operator-managed local pack/mirror | Current first-party permission for custom local ingestion, allowed media/types, retention/deletion, naming/manifest/integrity rules, and no redistribution | Withhold importer. Manual download availability alone is insufficient authority. |
| Website scraping or generic URL import | Not an accepted provider contract | Permanently forbidden for this scope. |

Plain FTP exposes username/password, commands, listings, and media to network
observers and permits on-path tampering and credential/session theft across the
LAN and Internet. It cannot meet the normal secret and content-integrity
boundary merely because the operator opts in.

If a later gate accepts explicit FTPS, the smallest client is an internal,
closed-purpose protocol implementation or a separately reviewed pinned library,
not a shell call to an optional system `ftp` binary. It must use the one fixed
official hostname, validated DNS and certificate identity, encrypted control
and data channels, `AUTH TLS` plus `PBSZ 0` and `PROT P` when explicit FTPS is
documented, passive mode with EPSV preferred, and no cleartext fallback. Ignore
or reject a PASV-advertised host; data connections must use the already
validated control peer unless official documentation names a separate fixed
allowlist. Start with one control and one data connection, one sync job, finite
connect/command/idle/whole-file deadlines, atomic `.part` files, bounded resume,
size/MIME/decode/media validation, and provider-scoped promotion. Those values
must be finalized against the obtained official limits before code is allowed.

## Credential contract

LaunchBox uses no credential, cookie, or operator-configurable origin.

A future credentialed EmuMovies provider, if separately accepted, uses the
existing already-opened config source design: an operator-owned, non-symlink
regular file with exact mode `0600`, validated by descriptor identity after
open. The first cross-platform implementation must use that mechanism rather
than add an OS-specific keychain. A keychain abstraction would require a
separate cross-platform ownership, unlock, rotation, headless-service, and test
decision.

Username/password/token values are never accepted in argv, environment,
browser forms, URLs, logs, metrics, errors, reports, fixtures, source, generated
manifests, shared writable paths, or the target. Config examples may name fields
but contain no plausible secret. Secrets remain in the shortest-lived provider
memory, are not copied into cache keys or persistence, and are zeroed where the
language/runtime permits. Authentication failure returns only
`unauthorized`; formatting and wrapping cannot reveal raw causes, headers,
commands, paths, host-private details, or response bodies.

## Operator UX

The implementation milestone adds no browser-side provider mutation and no
browser credential flow. Browser behavior remains readonly and same-origin.

The host CLI may add exactly these local operations, with credentials loaded
only from the selected private config:

- `fogcast metadata status`
- `fogcast metadata sync`
- `fogcast metadata purge`

`status` reports only provider identity, enabled/disabled/syncing/offline state,
last successful validation age, current generation prefix, aggregate counts,
and categorized error. `sync` respects the 24-hour conditional-request ceiling
and reports `started`, `already_running`, or `not_due`. `purge` performs the
same complete owned purge as disable and fails closed. Provider, URL, host,
path, credential, and retention limits are not arbitrary CLI arguments.

Disable remains an operator edit to the private config followed by restart.
Removing `[metadata]` produces `unconfigured`; `enabled = false` produces
`disabled`; both purge derived state during composition. A later accepted
EmuMovies provider uses a separate `[presentation_media]` table so it cannot be
mistaken for the existing `[media]` cast transport or the LaunchBox metadata
provider. No such table is accepted by production config in this milestone.

## Observability and redaction

Allowed counters and status are coarse: provider, generation prefix, snapshot
age, 200/304, bytes, duration, parse counts, sync state, cache hit/miss,
exact/confident/ambiguous/no-match, artwork accepted/rejected, rate-limited,
offline, invalid-response, and purge result.

Never log or expose ROM title, normalized query, catalog ID, ROM path/content or
hash, provider game/image/file ID, upstream URL, remote IP, response body/XML,
image/media bytes, username, password, token, cookie, authorization header,
FTP command/reply, local cache path, or target identity. Error strings remain a
closed code such as `metadata: invalid_response`; composition labels remain the
current privacy-safe generic labels.

## IGDB/Twitch removal

The implementation must remove, not merely disable:

- `ProviderIGDB`, `IGDBConfig`, `IGDBProvider`, Twitch token constants and token
  lifecycle, IGDB query/wire structs, IGDB platform slugs, IGDB artwork URL and
  transforms, and IGDB/Twitch host allowlist entries;
- `client_id`/`client_secret` semantics from `[metadata]`, IGDB-only credential
  scope, IGDB attribution checks, and IGDB-specific cache read/purge branches;
- IGDB provider/config/runtime/cache/artwork/API/UI/browser tests and fixtures,
  replacing provider-neutral coverage and LaunchBox fixture coverage rather
  than deleting the security assertions; and
- active README/config/privacy/attribution instructions for Twitch and IGDB.

Historical research/results documents remain historical and are not rewritten.
No dormant IGDB network path, hidden config alias, compatibility flag, generic
provider URL, or fallback token flow remains. Rollback is Git plus complete
derived-state purge; it is not dual live-provider support.

## Future writable file contract

One Luna implementation worktree may write only the following existing files or
new siblings in the named directories. A narrower task may use fewer files.

### LaunchBox replacement milestone

- `internal/metadata/types.go`
- `internal/metadata/normalize.go`
- `internal/metadata/match.go`
- `internal/metadata/runtime.go`
- `internal/metadata/cache.go`
- `internal/metadata/storage_root.go`
- `internal/metadata/sqlite_vfs.go`
- `internal/metadata/artwork.go`
- `internal/metadata/transport.go`
- delete `internal/metadata/provider_igdb.go`
- new `internal/metadata/provider_launchbox.go`
- new `internal/metadata/launchbox_snapshot.go`
- new `internal/metadata/launchbox_index.go`
- matching `internal/metadata/*_test.go` files, including new LaunchBox fixture
  files only under `internal/metadata/testdata/launchbox/`
- `fogcast/config.go`, `fogcast/metadata_config_test.go`, and only if descriptor
  behavior changes, `fogcast/config_source_unix.go`,
  `fogcast/config_source_other.go`, and their tests
- `cmd/fogcast-api/main.go` and `cmd/fogcast-api/main_test.go`
- `internal/hostapi/server.go`, `internal/hostapi/ui_app.js`,
  `internal/hostapi/ui_app_test.js`, `internal/hostapi/ui_browser_test.js`, and
  provider presentation fixtures under `internal/hostapi/testdata/ui/`
- only if the CLI status/sync/purge operations are included in this milestone:
  `internal/fogcastcli/run.go`, `internal/fogcastcli/run_test.go`,
  `cmd/fogcast/main.go`, and `cmd/fogcast/main_test.go`
- `README.md`
- `go.mod` and `go.sum` only if an unavoidable dependency is separately
  approved; the LaunchBox path must prefer Go standard `archive/zip`,
  `encoding/xml`, `net/http`, and existing SQLite dependencies.

### Explicitly forbidden in this milestone

- `protocol/`, `host/` session/launch/target code, `internal/remotemedia/`,
  `internal/mediasession/`, `mister/`, `cmd/mister-*`, `buildroot/`, `deploy/`,
  target scripts/images/config, existing ADRs/architecture/roadmap, historical
  results/research, protected local docs, and generated release artifacts;
- EmuMovies API/FTP/SFTP/Sync/local-import implementation or tests that encode a
  guessed protocol;
- target access, launch, stop, deployment, reboot, HIL, or physical observation;
  and
- staging, committing, pushing, or publishing outside the separately authorized
  implementation/review/integration gate.

## TDD acceptance matrix

Implementation is test-first. Each row needs a red test, the smallest change,
and green focused plus full checks.

| Area | Required deterministic acceptance |
| --- | --- |
| Config | Absent/disabled/enabled LaunchBox states; enabled requires exact provider; credentials and unknown origin fields rejected; disabled needs no `0600`, enabled credentialless LaunchBox does not falsely require a secret; unsafe opened source still rejected where secrets exist. |
| IGDB removal | Source/docs/production fixtures contain no active IGDB/Twitch symbols, hosts, tokens, attribution, config fields, or network routes; historical docs are excluded from this scan. |
| Snapshot request | Exact HTTPS host/path/port, no proxy/cookie/credential, conditional headers, no redirects, all-DNS-answer validation, TLS minimum, timeout, 24-hour ceiling, 304 and bounded 200. |
| Archive | Compressed/member/uncompressed/ratio limits; duplicate, absolute, traversal, separator, symlink, special, encrypted, unknown-shape, truncated, trailing, and oversized entries rejected before promotion. |
| XML framing | Exercise `framedXMLReader` before `encoding/xml`: exact max/max+1 ordinary text for known ignored and unknown leaf fields, CDATA, comment, 256/257-byte declaration, 1,024/1,025-byte attribute, 64/65-byte names, 16,384/16,385-byte start tags, entities, malformed/unterminated boundaries, and adversarial alternating shapes under one-byte, delimiter-split, and 32 KiB input. Exercise framer-only depth 8/9, attributes 8/9 and aggregate 4,096/4,097. Instrument zero offending-frame bytes delivered to the decoder and a 131,084-byte frame-buffer high-water ceiling plus fixed scratch; caller-side post-`Token()` length checks alone fail acceptance. |
| XML schema | Strict streaming parse; every accepted UTF-8 declaration-policy variant and exactly one initial declaration; every other processing instruction, DTD/directive/entity/network input, malformed UTF-8, duplicate IDs/keys, invalid references/numbers rejected. Schema root/record/field attributes are zero/one rejection cases even though the framer has defense-in-depth attribute budgets. Table-drive every selected field's exact byte/rune max and max+1, depth 8/9, and elements 16,000,000/16,000,001. Exercise `Metadata.xml` family caps/max+1 for `Game` 250,000/250,001, `GameAlternateName` 250,000/250,001 and per-game 64/65, `GameImage` 2,000,000/2,000,001 and per-game 512/513, `Platform` 512/513, `PlatformAlternateName` 1,024/1,025, `Emulator` 128/129, and `EmulatorPlatform` 1,024/1,025; exercise its 2,502,688/2,502,689 aggregate. Exercise `Platforms.xml` 512/513, 1,024/1,025, and 1,536/1,537 plus the 2,504,224/2,504,225 combined aggregate. Reject unknown top-level families, cross-member missing/extra/conflicting platform keys, alias duplicates/bad links, and nested schema fields. The retained fixture must assert 69,311 aliases, both 189/431 platform copies and equality, every ignored family, one ignored blank alias, max 30 aliases/game, and all recomputed maxima. `ReleaseDate` never supplies a year; valid-looking, malformed, timezone-bearing, and overlong cases prove ignore-versus-resource-failure behavior. |
| Index | Exact SNES/Genesis mapping; observed field mapping; archive/schema/parser hashes; SQLite integrity; deterministic byte-bounded generation; crash before/after fsync/rename/pointer swap preserves old or new complete generation, never partial. |
| Matching | Exact platform first; canonical, accepted `GameAlternateName`, decorated tiers; stable same-game normalized-alias collapse; same alias across games remains ambiguous; duplicate same-ID/alias conflict; equal best tie ambiguous; no-match; no fuzzy auto-attach; provider input detached. |
| Region/media selection | Type and region order fixed; lexical tie determinism; role ambiguity affects only role; exact filename grammar/extensions and 44-byte/rune max accepted. Table-reject 40-character stem/max+1, `../`, slash, backslash, `%2f` and `%2F`, `?`, `#`, colon, space, controls, invalid UTF-8, non-ASCII, uppercase/unknown extensions, and extension/MIME disagreement. Prove one escaped segment plus final scheme/host/effective-port/path/query/fragment/userinfo revalidation and zero requests on every rejection; CRC32 checked before decode. |
| Cache migration | Schema v1/IGDB settings, rows, platform map, credential scope, SQLite sidecars, artwork refs/objects/files all purged; no old handle can open; schema v2 provider/generation keys do not collide; purge failure blocks startup. |
| Root safety | Existing ancestor/create/chmod/open/use/delete swap tests extended to provider generations, manifest, archive, index, temps, and artwork; aliases share one physical lease; no symlink/hard-link/special-file escape. |
| Artwork | Fixed LaunchBox host; no arbitrary URL; redirect/DNS/MIME/byte/dimension/pixel/decode/trailing-content/CRC failure rejected; sanitized bytes atomically published; per-object and 512 MiB LRU retained. |
| Concurrency | Inert open creates zero workers/requests. Concurrent repeated activation admits exactly one bootstrap worker and, on a successful recording transport, exactly one initial snapshot request. Activation failure releases no request gate and synchronously rolls back partial worker state. Activate/Close races, close-before-activate, blocked transport cancellation, bounded 2-second join/reaper ownership, one refresh, two image requests, 500 ms starts, independent waiter cancellation, provider-global rate limit, old epoch cannot publish, and reader lease delays old-generation deletion. Run lifecycle/security cases with `-count=20`. |
| Failure/fallback | Unconfigured, disabled, syncing, no-match, ambiguous, offline, malformed, stale-over-7-days, provider-removed, storage, and artwork-only failure preserve catalog/detail/launch. No raw cause reaches API/UI/log. |
| API | Catalog lookup precedes presentation; opaque same-origin handles only; host rejection and no-store/nosniff/CSP retained; no generic fetch/proxy/media route; exact launch body unchanged. |
| UI | LaunchBox attribution; independent empty fields remain empty; fallback is explicitly demo/offline; artwork failure is role-local; same-ID title/system and selection sequence reject stale presentation. All user strings use text APIs. |
| CLI, if included | Status/sync/purge fixed commands; no secret/provider/URL/path args; safe aggregate output; cancellation and complete close; sync ceiling and purge fail-closed. |
| Composition | Metadata opens inert without credentials. A recording transport/worker probe table covers every failure return after metadata open (missing capture dependency, target-cast construction, receiver construction, missing bridge starter, bridge-starter error, remote-input construction, and any future post-metadata branch): zero requests/workers and metadata closes exactly once. Full handler composition calls idempotent activation once and records exactly one bootstrap; activation failure returns no handler and closes remote input, media/audio, then metadata in reverse order. Normal close and activation-failure close preserve generic redacted errors and bounded cancellation/join. |

## Deterministic implementation checks

Run from a fresh implementation worktree, with no real provider credentials and
no non-loopback browser traffic:

```sh
go test -count=1 ./internal/metadata ./fogcast ./internal/hostapi ./cmd/fogcast-api
go test -race -count=20 ./internal/metadata
go test -race -count=20 ./cmd/fogcast-api -run 'Metadata|Composition|Cleanup'
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
FOGCAST_BROWSER_REQUIRED=1 node --test internal/hostapi/ui_browser_test.js
go test -race -count=1 ./...
go vet ./...
test -z "$(gofmt -l internal/metadata fogcast internal/hostapi cmd/fogcast-api internal/fogcastcli cmd/fogcast)"
go mod verify
make fmt
make test
make check
make build
git diff --check
```

Also run deterministic scans over active source/config/README (excluding
historical `docs/research`, results, Git data, test attack strings, and this
decision) for IGDB/Twitch hosts/symbols, credential-shaped values, generic
provider URLs, cleartext FTP, browser external URLs, demo text labelled as
provider data, unexpected generated files, and forbidden target scope. Record
all exclusion rules and inspect every match.

Before handoff, freeze the exact recursive tree manifest and hashes, run
`git diff --check`, and run `git diff --no-index --check /dev/null <file>` plus
SHA-256 for every untracked deliverable. Any edit invalidates the frozen review
input.

## Required Chrome/CDP scenarios

The required browser run uses the assembled production UI with a deterministic
loopback fixture. Every case must execute with zero fail/cancel/skip/todo:

1. desktop and narrow/reflow catalog load, keyboard focus, selected-card state,
   detail refresh, and reduced motion;
2. LaunchBox ready full fields, each independently empty optional field, cover,
   backdrop, and attribution;
3. unconfigured, disabled, syncing, no-match, ambiguous, offline,
   stale-expired, malformed, and artwork-role failure with retry;
4. stale search, stale detail, stale presentation after selection replacement,
   and same-ID title/system replacement;
5. launch success/failure/retry after every presentation state, preserving the
   exact selected live ID and exact one-field JSON body;
6. artwork requests only to the same loopback origin and opaque handle route,
   with no upstream filename or URL in DOM, attributes, console, or evidence;
7. zero unexpected/external browser requests, zero console errors, exact
   request/response-extra-info correlation, and complete profile/helper
   teardown.

This is developer browser observation, not live-provider, target, HIL, physical,
or acceptance evidence.

## Live-validation and release gate

After fixture implementation and independent exact-tree review, a bounded
LaunchBox live validation may run only when the coordinator records one of:

- current first-party policy or direct vendor confirmation covering independent
  application use, local normalized indexing, lazy image fetch, attribution,
  request limits, cache retention/deletion, and no redistribution; or
- an explicit user product-risk override that names the still-open policy facts
  and accepts the conservative limits in this decision.

The live check is host-only and no-target: one conditional snapshot retrieval,
stream parse/index, exact SNES/Genesis sample matches, one bounded cover and one
bounded backdrop, CRC/decode/cache/restart/offline/7-day-expiry/purge checks,
loopback browser projection, and a residue/secret/log scan. Disable and purge
afterward. Record source/archive/index/image hashes and separate fixture-tested,
live-provider, browser, and unrun target/HIL evidence.

EmuMovies requires a different prerequisite. The user must personally either:

1. use the official support path identified in the evidence report to request
   current independent-frontend API or downloader permission and documentation,
   including auth, endpoints/host, schema/listing, identifiers, limits,
   retention/deletion/attribution, automation, and SNES/Genesis media rights; or
2. inspect the authenticated FTP details page and provide only non-secret,
   first-party protocol facts and documentation: explicit versus implicit FTPS
   or SFTP, public host/port classification, encrypted data-channel requirement,
   certificate identity, passive/EPSV, resume/listing/checksum, connection/rate
   limits, and custom-client permission.

The user must not paste a username, password, token, cookie, private connection
string, or credential-bearing screenshot. An agent must not register, pay,
accept clickwrap, submit support, authenticate, or probe the service on the
user's behalf. After evidence arrives, Sol issues a new reviewed transport and
retention decision before any EmuMovies implementation card is created.

## Phase ordering, no-op rule, and rollback

Phase 3 **may not proceed now**. It may proceed only after accepted bounded live
provider evidence, or a later explicit user override that acknowledges the
precise remaining provider blocker. Fixture success, this Designed decision,
or the current machine-observed archive is insufficient.

Downstream work must no-op and stop if it is asked to implement EmuMovies,
plaintext FTP, local-pack ingestion, browser external access, a generic proxy,
or Phase 3 before the corresponding gate. It should report this decision and
the exact prerequisite rather than add a stub, guessed schema, dormant client,
or synthetic success.

Rollback for the LaunchBox milestone is: disable/remove `[metadata]`, complete
the descriptor-confined derived-state purge, and return to the last accepted Git
revision. No target rollback is involved. Provider failure never rolls back the
catalog, session, audio-worker, or launch path because those are never delegated
to provider data.

## Evidence boundary, risks, and next safe action

This document is **Designed** only. Source inspection, the byte-identical
imported research, retained-archive reinspection, and Go decoder inspection
support the contract; no production code was changed, no LaunchBox adapter was
software-tested, no EmuMovies transport was authenticated, no provider terms
were accepted, no target was contacted, and no HIL, physical, teardown, latency,
reproducibility, or acceptance claim is made.

Unresolved risks are the LaunchBox policy gap, live archive schema drift,
archive/index resource pressure, false title attachment, and all EmuMovies
access/transport/rights facts. The limits, strict parsing, ambiguity behavior,
provider epochs, and release gates contain those risks; they do not prove them
resolved.

Next safe action: Vega performs a fresh exact-tree review of the byte-identical
imported report plus this repaired decision, explicitly closing the two prior
Important findings. If accepted, the coordinator may create one Luna TDD
milestone for the LaunchBox-only replacement within the file contract above. It
must not create EmuMovies implementation or Phase 3 work until their explicit
gates pass.
