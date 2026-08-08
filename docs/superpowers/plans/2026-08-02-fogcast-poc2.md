> **Historical POC record — 2026-08-08:** This document preserves its original
> POC scope and evidence; it is not current architecture or an active plan. See
> the [canonical architecture](../../ARCHITECTURE.md) and
> [active migration roadmap](../../ROADMAP.md). Do not execute its checklists
> unless a current approved plan explicitly adopts them.

# FogCast POC 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the accepted POC 1 appliance into a Mac-owned SNES and Mega Drive library workflow that scans mounted NAS roots, lazily prepares ROM content, pushes missing bytes into a bounded persistent MiSTer cache, and launches cached games by verified content identity, including after reboot and while the NAS is offline.

**Architecture:** Keep `Main_MiSTer`, the two accepted FPGA cores, and the existing v1 launch path unchanged. Add a generated SQLite catalog and source preparer on the Mac, v2 probe/upload/launch methods to the authenticated host client, a content-addressed FAT cache owned by `mister-agent`, and a `fogcast` CLI that coordinates probe-before-read, prepare, upload, and launch. The target constructs every cache path from validated system/digest/extension fields; no host or NAS path crosses the v2 boundary.

**Tech Stack:** Go 1.26.5, `modernc.org/sqlite` v1.55.0 as the CGo-free SQLite driver, `github.com/pelletier/go-toml/v2`, Go standard-library ZIP/SHA-256/HTTP/filesystem packages, the accepted ARMv7 Buildroot 2021.02.4 image and reproduced MiSTer 5.15 kernel, synthetic ROM fixtures, shell policy tests, and operator-assisted hardware acceptance on the dedicated MiSTer Pi.

## Global Constraints

- Implement exactly Mega Drive and SNES. Retain the accepted core selectors, expected core names, file delays, file indices, and extension allowlists.
- The SuperStation One is out of scope and must not be modified. Hardware deployment targets only the dedicated complete MiSTer Pi.
- POC 1 remains recoverable at tag `POC1`, and the complete v1 API, `misterctl`, POC 1 manifest path, image verification, deployment checks, and 34-check HIL suite must continue to pass.
- Rename the Go module to `github.com/DeanoC/FogCast-POC`; the new host command is `fogcast`, while the target executable remains `mister-agent`.
- NAS roots are ordinary absolute macOS paths, normally below `/Volumes`. FogCast never mounts SMB, accepts NAS credentials, or sends a source path to the target.
- The generated SQLite catalog is the sole normal POC 2 game library. `games.toml` remains only for POC 1 regression and HIL.
- Accept raw allowlisted ROMs and ZIP files containing exactly one supported ROM member. Do not add 7z, CHD, nested archives, patches, multipart media, or additional systems.
- Do not hash ROM bodies during normal scans. Hash only during lazy preparation or target cache verification.
- Prepared content is 1 through 32 MiB inclusive. Every successful upload is verified by system, lowercase SHA-256, lowercase extension without a dot, and exact byte length before atomic promotion.
- The target cache root is fixed at `/media/fat/fogcast/cache`; only `cache_max_bytes` is configurable, with a default of 2 GiB. Cache data survives reboot and is never included in an image or Git artifact.
- Preserve cached launch while NAS roots are offline. A cache miss for an unavailable source must fail before changing the current game.
- Never log or commit bearer tokens, ROM bytes, NAS absolute paths, local SQLite/staging files, target internal cache paths, or HIL reports containing private paths.
- Keep Mac and target builds CGo-free. Every task that changes dependencies or target wiring must run the explicit `CGO_ENABLED=0` build gates listed below.
- Use synthetic test bytes created by the tests. Do not copy Sonic, Super Mario World, or any other copyrighted ROM into fixtures, build output, logs, or Git.
- Use one upload mutex on the target. Launch/stop continue to use the existing transition lock. Upload may run while a game is active, but eviction must protect the active and in-flight launch keys.
- Human CLI progress goes to stdout; under `--json`, progress goes to stderr and stdout contains exactly one final JSON value.
- Before every task commit, run its focused tests, `git diff --check`, and inspect `git status --short` so unrelated user changes are not staged.

---

## File Map

| Path | Responsibility |
|---|---|
| `protocol/content.go` | V2 content identities, probe/upload/launch payloads, limits, and new error codes |
| `protocol/validation.go` | Shared game/system/digest/extension/content validation |
| `fogcast/config.go` | Strict host configuration plus config/index/staging default paths |
| `catalog/model.go` | Library roots, source fingerprints, games, scan reports, stable IDs, and availability |
| `catalog/schema.go` | SQLite schema version and migrations |
| `catalog/store.go` | Transactional scan sessions, catalog queries, offline-root state, and digest compare-and-set |
| `catalog/scanner.go` | Recursive no-symlink discovery and ZIP central-directory classification |
| `romsource/prepare.go` | Bounded raw/ZIP streaming into exclusive local staging with SHA-256 and CRC checks |
| `host/content_client.go` | Authenticated v2 probe, one-shot upload, and content launch transport |
| `internal/targetcache/manager.go` | Target inventory, verification memo, cache resolution, and internal path construction |
| `internal/targetcache/upload.go` | Capacity reservation, deterministic eviction, bounded upload, and atomic promotion |
| `internal/targetcache/active.go` | In-memory launch pins and atomic volatile active-key record |
| `internal/agent/content.go` | Adapter joining cache operations to the existing launch coordinator |
| `internal/httpapi/content.go` | Authenticated v2 HTTP validation and response handling |
| `fogcast/service.go` | Host scan/query/control API and probe/prepare/upload/launch orchestration |
| `internal/fogcastcli/run.go` | `fogcast` command parsing, human rendering, JSON separation, and exit codes |
| `cmd/fogcast/main.go` | Mac CLI composition root |
| `cmd/mister-agent/main.go` | Target cache composition and v2 server wiring |
| `internal/integration/fogcast_content_test.go` | End-to-end synthetic POC 2 flow against fake MiSTer runtime |
| `internal/hil/poc2.go` | Operator-assisted POC 2 cache/offline/reboot acceptance sequence |
| `cmd/fogcast-hil/main.go` | Local POC 2 HIL runner and ignored report writer |
| `deploy/poc1a/agent.toml.example` | Example `cache_max_bytes` while retaining accepted target fields |
| `buildroot/board/mister-remote/rootfs-overlay/etc/init.d/S50mister-agent` | Cache-directory preflight before agent supervision |
| `scripts/tests/poc2-rootfs_test.sh` | Static and fixture policy tests for cache-enabled image wiring |
| `build/outputs.poc2.lock.toml` | Secret-free POC 2 root hashes anchored to the accepted POC 1B source/output lock |
| `internal/imagepoc/poc2.go` | Parse, validate, and record the POC 2 output lock |
| `cmd/poc2-lock/main.go` | Verify accepted inputs and record reproducible POC 2 roots |
| `scripts/install-poc2.sh` | Mac transport wrapper for the root-only POC 2 checkpoint |
| `deploy/poc2/install-target.sh` | Hash-gated, idempotent target-side root replacement |
| `scripts/restore-poc1b-sd.sh` | Offline restoration of the pre-POC 2 root while retaining the accepted kernel |
| `docs/runbooks/poc2-deploy.md` | Build, deploy, recovery, cache reset, NAS-offline, and HIL procedure |

---

### Task 1: Module Identity and V2 Protocol Contract

**Files:**
- Create: `protocol/content.go`
- Create: `protocol/content_test.go`
- Modify: `protocol/types.go`
- Modify: `protocol/validation.go`
- Modify: `protocol/validation_test.go`
- Modify: `go.mod`
- Modify: `Makefile`
- Modify: every tracked `.go` file returned by `rg -l 'github.com/clawzai2-tech/mister-remote' --glob '*.go'`

**Interfaces:**

```go
const MaxContentBytes int64 = 32 << 20

type ContentKey struct {
    SHA256    string
    Extension string
}

type ContentIdentity struct {
    SHA256    string `json:"sha256"`
    Size      int64  `json:"size"`
    Extension string `json:"extension"`
}

func (c ContentIdentity) Key() ContentKey

type CacheProbeResponse struct {
    Present bool             `json:"present"`
    System  *System          `json:"system,omitempty"`
    Content *ContentIdentity `json:"content,omitempty"`
}

type CacheUploadResult string

const (
    CacheUploadPresent CacheUploadResult = "present"
    CacheUploadCreated CacheUploadResult = "created"
)

type CacheUploadResponse struct {
    Result  CacheUploadResult `json:"result"`
    System  System            `json:"system"`
    Content ContentIdentity   `json:"content"`
}

type CachedLaunchRequest struct {
    GameID  string          `json:"game_id"`
    System  System          `json:"system"`
    Content ContentIdentity `json:"content"`
}

type CachedLaunchResponse struct {
    Status  Status          `json:"status"`
    Content ContentIdentity `json:"content"`
}

func ValidateDigest(string) error
func ValidateExtension(string) error
func ValidateContentKey(ContentKey) error
func ValidateContentIdentity(ContentIdentity) error
```

- [ ] **Step 1: Write failing protocol serialization and validation tests**

Cover exact JSON for present/absent probe, both upload results, nested v2 launch status, conversion from a full identity to a digest/extension probe key, 64-character lowercase SHA-256, extension syntax `[a-z0-9]+`, size boundaries 1 and 32 MiB, zero/negative/oversized sizes, uppercase/dotted/slashed extensions, malformed digest length/hex/case, and defensive rejection of unknown systems when validating a complete request.

- [ ] **Step 2: Run the focused tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./protocol -run 'Content|Digest|Extension' -v`

Expected: FAIL because the v2 types and validators do not exist.

- [ ] **Step 3: Implement the v2 contract without changing v1 JSON**

Add error codes `SOURCE_UNAVAILABLE`, `INVALID_ARCHIVE`, `TRANSFER_FAILED`, `DIGEST_MISMATCH`, `CONTENT_NOT_CACHED`, and `CACHE_FULL`. Keep every field and null behavior of `Health`, `Status`, and `LaunchRequest` byte-for-byte compatible. `ValidateContentIdentity` validates only generic identity syntax and limits; the target cache later checks system-specific extensions through `core.Registry`.

- [ ] **Step 4: Rename the module and imports mechanically**

Change `go.mod` to `module github.com/DeanoC/FogCast-POC`, replace the old import prefix only in tracked Go sources, and update the `LDFLAGS` version symbol in `Makefile`. Verify the old prefix is absent with:

```bash
! rg 'github.com/clawzai2-tech/mister-remote' --glob '*.go' go.mod Makefile
```

- [ ] **Step 5: Prove v1 compatibility and CGo-free builds**

Run:

```bash
mise exec go@1.26.5 -- go test -race ./protocol ./host ./internal/httpapi ./internal/agent ./internal/integration -v
CGO_ENABLED=0 mise exec go@1.26.5 -- go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- go build -o /dev/null ./cmd/mister-agent
mise exec go@1.26.5 -- make check
git diff --check
```

Expected: all existing v1 tests pass with no v1 golden JSON changes.

- [ ] **Step 6: Commit the protocol foundation**

```bash
git add go.mod Makefile protocol cmd host internal
git commit -m "feat: define FogCast v2 content protocol"
```

---

### Task 2: FogCast Host Configuration and Catalog Model

**Files:**
- Create: `fogcast/config.go`
- Create: `fogcast/config_test.go`
- Create: `catalog/model.go`
- Create: `catalog/model_test.go`
- Modify: `.gitignore`

**Interfaces:**

```go
// catalog/model.go
type Root struct {
    ID     string
    System protocol.System
    Path   string
}

type SourceKind string // raw or zip
type SourceState string // available, invalid, or missing

type Fingerprint struct {
    SourceSize       int64
    ModifiedNS       int64
    ZIPMember        string
    ZIPSize          int64
    ZIPCRC32         uint32
    ZIPEntryCount    int
}

type Content struct {
    SHA256    string
    Size      int64
    Extension string
}

type Game struct {
    ID, Title, LibraryID, RelativePath, Reason string
    System                                    protocol.System
    Kind                                      SourceKind
    State                                     SourceState
    RootOnline                                bool
    Fingerprint                               Fingerprint
    Content                                   *Content
}

func GameID(system protocol.System, libraryID, relativePath, title string) string

// fogcast/config.go
type Paths struct { Config, Index, Staging string }
func DefaultPaths() (Paths, error)
type Config struct {
    BaseURL, Token string
    RequestTimeout, UploadTimeout time.Duration
    Libraries []catalog.Root
}
func LoadConfig(path string) (Config, error)
```

- [ ] **Step 1: Write failing config/default-path/model tests**

Assert the approved TOML loads, unknown fields fail, library IDs are unique lowercase slugs, roots are absolute/clean/unique, systems are only SNES/Mega Drive, request/upload timeouts are positive and fit `time.Duration`, HTTP origins reject credentials/path/query/fragment, and defaults resolve exactly to `~/.config/fogcast/config.toml`, `~/.local/share/fogcast/library.sqlite3`, and `~/.cache/fogcast/staging/` under an injected home directory.

Test `GameID` against the exact byte sequence `<system> NUL <library-id> NUL <slash-relative-path>`, a 48-character ASCII slug cap, collapsed punctuation, empty title fallback `game`, deterministic 12-hex suffixes, and different IDs for a path or library change.

- [ ] **Step 2: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./fogcast ./catalog -v`

Expected: FAIL because both packages are absent.

- [ ] **Step 3: Implement strict configuration and stable models**

Use the existing TOML dependency with `DisallowUnknownFields`. Preserve tokens exactly after rejecting all-whitespace values. Convert configured roots with `filepath.Abs`, `filepath.Clean`, and `filepath.EvalSymlinks` only when the root currently exists; an offline root keeps its cleaned configured absolute path. Normalize catalog relative paths with slash separators and reject empty, absolute, `.` or `..`-escaping values.

- [ ] **Step 4: Exclude all local POC 2 state**

Add `*.sqlite3`, `*.sqlite3-shm`, `*.sqlite3-wal`, `/artifacts/hil/poc2*.json`, and `/local-fogcast/` to `.gitignore`. Do not add a broad `*.zip` ignore because synthetic ZIP tests are generated at runtime and no ZIP fixture is committed.

- [ ] **Step 5: Run focused and repository checks**

```bash
mise exec go@1.26.5 -- go test -race ./fogcast ./catalog -v
mise exec go@1.26.5 -- make check
git diff --check
```

- [ ] **Step 6: Commit configuration and models**

```bash
git add .gitignore fogcast catalog
git commit -m "feat: add FogCast configuration and catalog model"
```

---

### Task 3: Versioned SQLite Catalog and Transactional Scan Sessions

**Files:**
- Create: `catalog/schema.go`
- Create: `catalog/store.go`
- Create: `catalog/store_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

```go
func Open(path string) (*Store, error)
func (s *Store) Close() error
func (s *Store) BeginRootScan(context.Context, Root) (*ScanSession, error)
func (x *ScanSession) Observe(context.Context, Candidate) (Change, error)
func (x *ScanSession) Complete(context.Context) (RootReport, error)
func (x *ScanSession) Rollback() error
func (s *Store) MarkRootOffline(context.Context, Root, string) (RootReport, error)
func (s *Store) Games(context.Context) ([]Game, error)
func (s *Store) Search(context.Context, string) ([]Game, error)
func (s *Store) Game(context.Context, string) (Game, error)
func (s *Store) UpdateContent(context.Context, string, Fingerprint, Content) (bool, error)
```

Schema version 1 is:

```sql
CREATE TABLE libraries (
  id TEXT PRIMARY KEY,
  system TEXT NOT NULL,
  root TEXT NOT NULL UNIQUE,
  online INTEGER NOT NULL,
  generation INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE games (
  game_id TEXT PRIMARY KEY,
  library_id TEXT NOT NULL REFERENCES libraries(id),
  system TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  title TEXT NOT NULL,
  source_kind TEXT NOT NULL,
  source_state TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  source_size INTEGER NOT NULL,
  modified_ns INTEGER NOT NULL,
  zip_member TEXT NOT NULL DEFAULT '',
  zip_size INTEGER NOT NULL DEFAULT 0,
  zip_crc32 INTEGER NOT NULL DEFAULT 0,
  zip_entry_count INTEGER NOT NULL DEFAULT 0,
  seen_generation INTEGER NOT NULL,
  content_sha256 TEXT,
  content_size INTEGER,
  content_extension TEXT,
  UNIQUE(library_id, relative_path)
);
PRAGMA user_version = 1;
```

- [ ] **Step 1: Add failing database tests**

Use `t.TempDir()` to cover new database migration, refusal of a future `user_version`, unique roots and identities, generation increments, added/updated/unchanged counts, unchanged fingerprint preserving content, changed fingerprint clearing content, successful online reconciliation retaining removed rows as `missing`, offline marking preserving rows/content, transaction rollback preserving the previous generation, literal case-insensitive substring search across title/ID/system, deterministic ordering, unknown game errors, and fingerprint-guarded content compare-and-set.

- [ ] **Step 2: Run store tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./catalog -run 'Store|Schema|ScanSession|Search' -v`

Expected: FAIL because the store is absent.

- [ ] **Step 3: Add the CGo-free driver at its pinned version**

Run:

```bash
mise exec go@1.26.5 -- go get modernc.org/sqlite@v1.55.0
mise exec go@1.26.5 -- go mod tidy
```

Import the driver anonymously as `_ "modernc.org/sqlite"`. Configure foreign keys, a 5-second busy timeout, WAL mode, and one writer connection. Migrations run inside an exclusive transaction and reject a schema version newer than the executable.

- [ ] **Step 4: Implement scan generations and query behavior**

`Observe` upserts the current generation. It preserves content only when every fingerprint field and the source kind match; otherwise it sets all three content columns to NULL. `Complete` changes unseen rows from that library to `missing` without deleting them, marks the root online, and commits. `Rollback` is idempotent. `MarkRootOffline` changes only the library row, never game rows. Queries derive `RootOnline` from the library join and validate nullable content as an all-or-none tuple.

- [ ] **Step 5: Prove portability, concurrency safety, and migration behavior**

```bash
mise exec go@1.26.5 -- go test -race ./catalog -v
CGO_ENABLED=0 mise exec go@1.26.5 -- go test ./catalog
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go build ./catalog
mise exec go@1.26.5 -- make check
git diff --check
```

- [ ] **Step 6: Commit the catalog store**

```bash
git add go.mod go.sum catalog
git commit -m "feat: add transactional SQLite game catalog"
```

---

### Task 4: Recursive Incremental Scanner and ZIP Classification

**Files:**
- Create: `catalog/scanner.go`
- Create: `catalog/scanner_test.go`
- Modify: `catalog/model.go`
- Modify: `catalog/store.go`

**Interfaces:**

```go
type Scanner struct {
    Store         *Store
    Registry      core.Registry
    MaxZIPEntries int
}

type ScanReport struct { Roots []RootReport }
func (s Scanner) Scan(context.Context, []Root) (ScanReport, error)
```

- [ ] **Step 1: Write failing traversal and raw-source tests**

Generate a nested tree containing allowlisted mixed-case ROM extensions, unsupported files, `.DS_Store`, `._*`, directory and file symlinks, an unreadable candidate, and a disappearing file. Assert deterministic relative slash paths and game IDs, no symlink following, ignored noise, per-game invalid results without aborting the root, and scan counters for added/updated/unchanged/invalid/missing/offline.

- [ ] **Step 2: Write failing ZIP central-directory tests**

Generate ZIPs in memory for one supported member plus README, zero supported members, two supported members, nested `.zip`, empty archive, directory entries, encrypted flag, corrupt central directory, 4,096 entries, and 4,097 entries. Assert case-insensitive member extension matching, exact member name/size/CRC/entry-count fingerprint fields, no member body reads during scan, and rejection reasons that contain no absolute path.

- [ ] **Step 3: Run scanner tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./catalog -run 'Scanner|ZIP|Traversal' -v`

Expected: FAIL because scanner behavior is absent.

- [ ] **Step 4: Implement no-follow traversal and classification**

Use `filepath.WalkDir` and `os.Lstat`; skip any `ModeSymlink` entry and never call `EvalSymlinks` on candidates. Raw candidates are classified by the configured system's registry allowlist. ZIPs use `archive/zip` central-directory metadata, reject general-purpose encryption bit 0, count all entries against 4,096, and select exactly one non-directory supported member. Do not open a selected member during scanning.

- [ ] **Step 5: Integrate transactional sessions and offline roots**

Open the root directory before `BeginRootScan`. If that open fails, call `MarkRootOffline` and continue. For an online root, defer rollback until `Complete` succeeds. A database or traversal-wide failure rolls back that root and returns an error; a candidate-specific failure records an invalid candidate and continues.

- [ ] **Step 6: Run scan regressions and commit**

```bash
mise exec go@1.26.5 -- go test -race ./catalog -v
mise exec go@1.26.5 -- make check
git diff --check
git add catalog
git commit -m "feat: scan raw and ZIP game libraries"
```

---

### Task 5: Lazy Bounded ROM Source Preparation

**Files:**
- Create: `romsource/prepare.go`
- Create: `romsource/prepare_test.go`
- Create: `romsource/errors.go`

**Interfaces:**

```go
type Prepared struct {
    Path    string
    Content protocol.ContentIdentity
}
func (p *Prepared) Remove() error

type Preparer struct {
    StagingRoot string
    MaxBytes    int64
}
func (p Preparer) Prepare(context.Context, catalog.Root, catalog.Game) (*Prepared, error)
```

- [ ] **Step 1: Write failing raw preparation tests**

Cover exact SHA-256/size/normalized extension, exclusive regular staging file below the configured root, zero/32 MiB+1 rejection, context cancellation, source missing, source replaced before open, source changed during streaming, and cleanup after every failure. Assert errors expose game ID and typed code but not source absolute path.

- [ ] **Step 2: Write failing ZIP preparation tests**

Cover streaming only the recorded member, standard-library CRC failure propagation, declared uncompressed size above 32 MiB rejected before opening, streamed output over the cap, changed central-directory fingerprint, selected member disappearance, member names containing traversal syntax, and successful cleanup through `Prepared.Remove`. Assert no archive member path is ever joined to the filesystem.

- [ ] **Step 3: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./romsource -v`

Expected: FAIL because the package is absent.

- [ ] **Step 4: Implement safe staging and fingerprint revalidation**

Create the staging root with mode 0700 and files via `os.CreateTemp` plus mode 0600. Stream through `io.LimitReader(max+1)` and SHA-256; call `Sync`, close, and only return after exact limits and extension validation. For raw files compare size/mtime before and after. For ZIPs re-open the central directory, compare every recorded ZIP fingerprint field, then copy only `zip.File.Open()` output so the standard library validates CRC. Remove the staging path on every non-success return.

- [ ] **Step 5: Run focused and full checks**

```bash
mise exec go@1.26.5 -- go test -race ./romsource ./catalog -v
CGO_ENABLED=0 mise exec go@1.26.5 -- go test ./romsource
mise exec go@1.26.5 -- make check
git diff --check
```

- [ ] **Step 6: Commit source preparation**

```bash
git add romsource
git commit -m "feat: prepare bounded ROM content lazily"
```

---

### Task 6: Host V2 Cache Transport

**Files:**
- Create: `host/content_client.go`
- Create: `host/content_client_test.go`
- Modify: `host/client.go`

**Interfaces:**

```go
func (c *Client) ProbeContent(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error)
func (c *Client) UploadContent(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error)
func (c *Client) LaunchContent(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error)
```

- [ ] **Step 1: Write failing request-contract tests**

Use `httptest.Server` to assert exact methods/paths/query escaping, bearer authentication on every v2 endpoint, lowercase extension without dot, `application/octet-stream`, exact `Content-Length`, one-shot upload body, JSON content launch, bounded response decoding, typed API errors, absent probe as normal 200, and identity mismatch between request and response rejected client-side.

- [ ] **Step 2: Write failure/replay tests**

Use a counting reader and failing round tripper to prove upload is never automatically replayed, transport failure becomes `TRANSFER_FAILED`, premature body read errors propagate, and probe/launch continue to use the configured request context while a caller can provide a longer upload context.

- [ ] **Step 3: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./host -run 'Content|Upload|Probe' -v`

Expected: FAIL because v2 client methods are absent.

- [ ] **Step 4: Implement shared safe endpoint and response helpers**

Refactor URL construction and bounded error-envelope decoding from `doJSON` without changing v1 behavior. Validate all outbound content before constructing the path. Set `Request.ContentLength` explicitly and wrap the upload reader in a type that exposes only `Read`, so `http.NewRequest` cannot populate `GetBody` for transparent replay. Confirm the returned system/content exactly equal the request.

- [ ] **Step 5: Run v1 and v2 client suites and commit**

```bash
mise exec go@1.26.5 -- go test -race ./host -v
mise exec go@1.26.5 -- make check
git diff --check
git add host
git commit -m "feat: add authenticated v2 content client"
```

---

### Task 7: Target Cache Inventory, Probe, and Verified Resolution

**Files:**
- Create: `internal/targetcache/manager.go`
- Create: `internal/targetcache/manager_test.go`
- Create: `internal/targetcache/key.go`
- Create: `internal/targetcache/key_test.go`

**Interfaces:**

```go
type Config struct {
    Root         string
    ActiveRecord string
    MaxBytes     int64
}

type Resolved struct { Root, Path string }

func Open(Config, core.Registry, ...Option) (*Manager, error)
func (m *Manager) Probe(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
func (m *Manager) Resolve(context.Context, protocol.System, protocol.ContentIdentity) (Resolved, *protocol.APIError)
func (m *Manager) Usage() int64
```

- [ ] **Step 1: Write failing key/path tests**

Assert the only valid destination shape is `<root>/<system>/<64-lower-hex>.<allowlisted-extension>`, the resolved path remains beneath the resolved fixed root, SNES rejects `.md`, Mega Drive rejects `.sfc`, `.bin` is accepted for both, links and special files are rejected, and API errors never expose the resolved path.

- [ ] **Step 2: Write failing startup inventory tests**

Build fixture system directories with valid structural names, wrong systems/extensions/case/length, direct symlinks, subdirectories, special files where supported, recognized `.fogcast-*.part` files, unfamiliar files, zero/oversized entries, and digest-name/content mismatches. Assert startup removes only recognized direct stale parts, leaves every invalid/unfamiliar entry in place, counts known sizes conservatively, and never recursively traverses or follows a link.

- [ ] **Step 3: Write failing verification-memo tests**

Instrument file opens to prove the first probe after `Open` hashes a structurally valid file and returns its verified size, the second probe is memoized, a digest mismatch is quarantined in memory and stays absent, file metadata change invalidates the memo, and `Resolve` rejects a caller-declared size mismatch with `CONTENT_NOT_CACHED` without rehashing or revealing the path.

- [ ] **Step 4: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/targetcache -run 'Key|Inventory|Probe|Resolve' -v`

Expected: FAIL because the package is absent.

- [ ] **Step 5: Implement structural inventory and lazy content verification**

Use direct `os.ReadDir` calls only for the cache root and registered system directories. Use `os.Lstat`, require `Mode().IsRegular()`, and derive allowed extensions from cloned registry specs. Store structural inventory and verification memo under a mutex. Re-stat before trusting a memo. Digest mismatch and invalid entries remain on disk and in conservative accounting but are never returned as present or resolved.

- [ ] **Step 6: Run safety checks and commit**

```bash
mise exec go@1.26.5 -- go test -race ./internal/targetcache -v
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- go test -c -o /dev/null ./internal/targetcache
mise exec go@1.26.5 -- make check
git diff --check
git add internal/targetcache
git commit -m "feat: inventory and verify target content cache"
```

---

### Task 8: Target Upload, Capacity, LRU, and Active Pins

**Files:**
- Create: `internal/targetcache/upload.go`
- Create: `internal/targetcache/upload_test.go`
- Create: `internal/targetcache/active.go`
- Create: `internal/targetcache/active_test.go`
- Modify: `internal/targetcache/manager.go`

**Interfaces:**

```go
func (m *Manager) Put(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
func (m *Manager) PinForLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError
func (m *Manager) AbortLaunch(protocol.System, protocol.ContentIdentity)
func (m *Manager) CommitLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError
func (m *Manager) ClearActive() *protocol.APIError
func (m *Manager) ReconcileActive(protocol.Status)
```

- [ ] **Step 1: Write failing bounded-upload tests**

Cover zero/oversized lengths, short body, one excess byte, reader failure, digest mismatch, unsupported extension/system, destination conflict, successful same-directory exclusive part creation, `Sync`/close/atomic rename ordering, idempotent already-verified upload returning `present`, and cleanup of every failed part. Verify no target path or bytes appear in returned errors/log records.

- [ ] **Step 2: Write failing capacity and deterministic eviction tests**

Inject a `SpaceProbe` and clock. Cover cache ceiling and actual free-space checks, reservation before part creation, oldest mtime eviction, filename tie-break, touch-on-successful-launch, invalid/unfamiliar entries counted but never deleted, active/in-flight/upload entries pinned, insufficient safe victims returning `CACHE_FULL`, and no deletion outside the cache root.

- [ ] **Step 3: Write failing concurrency and active-record tests**

Prove two uploads serialize, probe remains available during upload, launch pin and current active key survive concurrent accounting, commit writes a mode-0600 JSON record atomically beneath `/run`, abort removes only the temporary pin, stop clears record/pin, agent restart reloads only a record whose system matches reconciled active core, and full reboot with no `/run` record starts unpinned.

- [ ] **Step 4: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/targetcache -run 'Put|Capacity|Evict|Active|Concurrent' -v`

Expected: FAIL because upload and active behavior are absent.

- [ ] **Step 5: Implement upload and capacity policy**

Hold one upload mutex from capacity reservation through promotion. Before creating a part, remove only recognized direct stale parts and evict verified inactive entries until both `Usage()+Content.Size <= MaxBytes` and injected/default `statfs` available bytes satisfy the reservation. Stream exactly `Size` plus a one-byte excess check while hashing. Flush, close, and rename within the system directory; update inventory only after rename. If a concurrent valid destination appears, discard the part and return `present` after verifying it.

- [ ] **Step 6: Implement pins and volatile active state**

Keep current-active and in-flight-launch keys separately so an upload cannot evict either. `CommitLaunch` atomically writes the record, promotes the in-flight key to active, drops the previous active pin, and applies `Chtimes` to the launched file. `AbortLaunch` leaves the previous active key untouched. `ReconcileActive` validates the record and cache entry, then retains it only for reconciled `StateActive` with the same system; otherwise it removes the record.

- [ ] **Step 7: Run race/portability suites and commit**

```bash
mise exec go@1.26.5 -- go test -race ./internal/targetcache -v
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- go test -c -o /dev/null ./internal/targetcache
mise exec go@1.26.5 -- make check
git diff --check
git add internal/targetcache
git commit -m "feat: bound and evict target cache safely"
```

---

### Task 9: Cache-Backed Agent Launch Coordination

**Files:**
- Create: `internal/agent/content.go`
- Create: `internal/agent/content_test.go`
- Modify: `internal/agent/coordinator.go`
- Modify: `internal/agent/coordinator_test.go`

**Interfaces:**

```go
type ContentStore interface {
    Probe(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
    Put(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
    Resolve(context.Context, protocol.System, protocol.ContentIdentity) (targetcache.Resolved, *protocol.APIError)
    PinForLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError
    AbortLaunch(protocol.System, protocol.ContentIdentity)
    CommitLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError
    ClearActive() *protocol.APIError
    ReconcileActive(protocol.Status)
}

func NewContentController(*Coordinator, ContentStore) *ContentController
func (c *ContentController) ProbeContent(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
func (c *ContentController) PutContent(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
func (c *ContentController) LaunchContent(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, *protocol.APIError)
```

- [ ] **Step 1: Write failing content-launch state tests**

Cover validation before cache access, missing/mismatched content preserving current state, target-owned `Resolved.Root` replacing only the cloned core spec's `ROMRoot`, runtime preparation and launch using the resolved path, shared launch/stop BUSY lock, pin-before-prepare, abort on prepare/launch failure, commit only after active core observation, stop clearing active only after successful Menu transition, and exact nested response content.

- [ ] **Step 2: Write failing restart reconciliation tests**

Assert `Initialize` still sets the POC 1 reconciled status, then asks the content store to reconcile its volatile record. Matching system retains the pin; Menu, unavailable, unrecognized core, or different system clears it. Ensure v1 launches remain path-based and do not create a content pin.

- [ ] **Step 3: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/agent -run 'Content|Cached|Reconcile' -v`

Expected: FAIL because content coordination is absent.

- [ ] **Step 4: Refactor one shared launch transition**

Extract an unexported method that accepts a validated game ID, cloned `core.Spec`, and target-resolved ROM path. Existing `Launch` performs the unchanged v1 validation/root selection before calling it. `LaunchContent` resolves and pins first, clones the registered spec, sets only `ROMRoot` to `Resolved.Root`, then calls the same transition. No public API accepts a target root/path.

- [ ] **Step 5: Preserve failure semantics and v1 regression**

An absent cache entry never calls runtime `Prepare` or `Launch`. A failed cached launch keeps bytes in cache. A failed stop retains the active pin. If active-record commit fails after core activation, return the active status plus `INTERNAL` while retaining the in-memory pin so eviction stays safe; test this explicitly.

- [ ] **Step 6: Run coordinator and integration checks and commit**

```bash
mise exec go@1.26.5 -- go test -race ./internal/agent ./internal/integration -v
mise exec go@1.26.5 -- make check
git diff --check
git add internal/agent
git commit -m "feat: launch verified cached content through agent"
```

---

### Task 10: Authenticated V2 HTTP Endpoints

**Files:**
- Create: `internal/httpapi/content.go`
- Create: `internal/httpapi/content_test.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`

**Interfaces:**

```go
type ContentController interface {
    ProbeContent(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
    PutContent(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
    LaunchContent(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, *protocol.APIError)
}

type Option func(*serverOptions)
func WithContent(ContentController) Option
func New(Controller, string, string, *slog.Logger, ...Option) http.Handler
```

- [ ] **Step 1: Write failing probe and launch handler tests**

Cover authentication, invalid/mixed-case system/digest/extension, duplicate/missing extension query, absent 200 response, present identity, strict one-object launch JSON, unknown fields, oversized JSON, content mismatch, controller call counts, and exact response shape. Prove no v2 route exists when `WithContent` is omitted and every v1 golden response remains unchanged.

- [ ] **Step 2: Write failing upload handler tests**

Cover missing/chunked/zero/negative/oversized `Content-Length`, wrong/missing content type including parameters, short/excess body, controller reader error, idempotent present, created, digest mismatch, capacity full, interrupted request context, and authentication before any body read. Set the test request's declared `ContentLength` independently of its reader length.

- [ ] **Step 3: Write failing status/logging tests**

Map `CONTENT_NOT_CACHED` to 404, invalid/archive/digest problems to 422, `CACHE_FULL` to 507, transfer failures to 400 unless caused by canceled context, and retain existing mappings. Assert logs include method, route, status, system, digest, size when supplied by upload/launch, and symbolic error but exclude token, request body, game source, and internal cache path.

- [ ] **Step 4: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/httpapi -run 'V2|Content|Upload|StatusMapping' -v`

Expected: FAIL because v2 handlers are absent.

- [ ] **Step 5: Implement bounded handlers and optional v2 registration**

Register exactly `GET,PUT /v2/cache/{system}/{sha256}` and `POST /v2/launch` only when content support is supplied. Validate path/query before filesystem calls. Require exact media types. Wrap upload bodies with `http.MaxBytesReader(MaxContentBytes+1)` while preserving the declared size as the identity. Continue using constant-time bearer comparison and newline-terminated JSON.

- [ ] **Step 6: Run API/security regression and commit**

```bash
mise exec go@1.26.5 -- go test -race ./internal/httpapi ./internal/agent ./host -v
mise exec go@1.26.5 -- make check
git diff --check
git add internal/httpapi
git commit -m "feat: expose authenticated v2 content endpoints"
```

---

### Task 11: FogCast Host Service and Launch State Machine

**Files:**
- Create: `fogcast/service.go`
- Create: `fogcast/service_test.go`
- Modify: `catalog/store.go`
- Modify: `catalog/scanner.go`

**Interfaces:**

```go
type Progress struct {
    Stage   string `json:"stage"`
    Message string `json:"message"`
}
type ProgressFunc func(Progress)

func Open(context.Context, Paths, *http.Client) (*Service, error)
func (s *Service) Close() error
func (s *Service) Scan(context.Context) (catalog.ScanReport, error)
func (s *Service) Games(context.Context) ([]catalog.Game, error)
func (s *Service) Search(context.Context, string) ([]catalog.Game, error)
func (s *Service) Launch(context.Context, string, ProgressFunc) (protocol.CachedLaunchResponse, error)
func (s *Service) Health(context.Context) (protocol.Health, error)
func (s *Service) Status(context.Context) (protocol.Status, error)
func (s *Service) Stop(context.Context) (protocol.Status, error)
```

- [ ] **Step 1: Write failing cache-hit/offline launch tests**

Use fake catalog/client/preparer interfaces. Assert a remembered identity probes first; a hit launches without stat/open/prepare/upload; an offline root with a hit succeeds; response identity must match; and an offline/missing/invalid source with a miss returns `SOURCE_UNAVAILABLE` or `INVALID_ARCHIVE` before prepare/upload/launch and preserves fake target state.

- [ ] **Step 2: Write failing first-transfer tests**

Assert unknown identity requires available source, prepares once, compare-and-set updates only the unchanged fingerprint, probes the computed key again, uploads only on miss, requires exact upload confirmation, launches after upload, emits ordered progress stages, and removes staging on success and every failure. A stale fingerprint triggers one catalog reload/retry and never associates bytes with the old row.

- [ ] **Step 3: Write failing failure/idempotency tests**

Cover initial probe transport failure, preparation error, second probe hit from a concurrent uploader, partial upload error followed by no blind replay, upload response mismatch, v2 launch error, unknown game ID, and POC 1 health/status/stop delegation. Assert no error or progress message contains configured root paths or token.

- [ ] **Step 4: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./fogcast -run 'Service|Launch|Offline|Transfer' -v`

Expected: FAIL because service orchestration is absent.

- [ ] **Step 5: Implement the approved nine-step launch order**

Load game; probe remembered identity; launch immediately on hit; require available valid source on miss; prepare; compare-and-set content; probe computed identity; upload only if absent; launch v2; defer staging removal from the moment preparation succeeds. Apply `RequestTimeout` to JSON/probe/control operations and `UploadTimeout` only to upload. Do not downgrade to v1 launch under any v2 error.

- [ ] **Step 6: Compose concrete catalog/scanner/preparer/client wiring**

`Open` loads strict config, creates private parent directories with mode 0700, opens SQLite, creates the staging directory, builds the default core registry/scanner/preparer, parses the base URL, and constructs the host client. Ensure failure closes any opened store and never prints the token or configured roots.

- [ ] **Step 7: Run service and repository checks and commit**

```bash
mise exec go@1.26.5 -- go test -race ./fogcast ./catalog ./romsource ./host -v
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go test ./fogcast
mise exec go@1.26.5 -- make check
git diff --check
git add fogcast catalog
git commit -m "feat: orchestrate FogCast cache-backed launches"
```

---

### Task 12: FogCast CLI and Mac Build

**Files:**
- Create: `internal/fogcastcli/run.go`
- Create: `internal/fogcastcli/run_test.go`
- Create: `cmd/fogcast/main.go`
- Create: `cmd/fogcast/main_test.go`
- Modify: `Makefile`

**Interfaces:**

```text
fogcast [--config PATH] [--json] scan
fogcast [--config PATH] [--json] games
fogcast [--config PATH] [--json] search <text>
fogcast [--config PATH] [--json] launch <game-id>
fogcast [--config PATH] [--json] health
fogcast [--config PATH] [--json] status
fogcast [--config PATH] [--json] stop
```

- [ ] **Step 1: Write failing command/usage tests**

Cover every command and arity, default and explicit config path, deterministic game table ordering, scan counters per root, literal search results, concise typed errors, health non-ready exit 1, operation errors exit 1, syntax errors exit 2, and service close on every path after successful open.

- [ ] **Step 2: Write failing output-separation tests**

For human mode, assert launch progress and final state appear on stdout. For `--json`, assert stdout contains exactly one decodable final JSON value plus one newline while progress appears only on stderr as newline-delimited JSON `Progress` objects. Test scan/games/search/control success and failure, including writers that fail.

- [ ] **Step 3: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/fogcastcli ./cmd/fogcast -v`

Expected: FAIL because the CLI packages are absent.

- [ ] **Step 4: Implement the CLI without changing `misterctl`**

Inject an `OpenService` function in tests. The real command obtains `fogcast.DefaultPaths`, replaces only `Paths.Config` for `--config`, and lets service state remain at the approved default locations. Use stable JSON structs for scan/list/search results; do not serialize internal paths/fingerprints unless the spec requires them.

- [ ] **Step 5: Add reproducible Mac build targets**

Add `build-fogcast` to `build` and compile `bin/fogcast` with `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64`, `-trimpath`, and the existing version linker flag. Keep `build-cli` producing `misterctl` and keep `build-hil` unchanged.

- [ ] **Step 6: Run CLI/build regression and commit**

```bash
mise exec go@1.26.5 -- go test -race ./internal/fogcastcli ./cmd/fogcast ./internal/cli ./cmd/misterctl -v
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go build -trimpath -o /dev/null ./cmd/fogcast
mise exec go@1.26.5 -- make build-fogcast build-cli
mise exec go@1.26.5 -- make check
git diff --check
git add internal/fogcastcli cmd/fogcast Makefile
git commit -m "feat: add FogCast host CLI"
```

---

### Task 13: Target Composition, Root Image Policy, and End-to-End Integration

**Files:**
- Create: `internal/integration/fogcast_content_test.go`
- Create: `scripts/tests/poc2-rootfs_test.sh`
- Modify: `internal/agentconfig/config.go`
- Modify: `internal/agentconfig/config_test.go`
- Modify: `cmd/mister-agent/main.go`
- Modify: `cmd/mister-agent/main_test.go`
- Modify: `deploy/poc1a/agent.toml.example`
- Modify: `buildroot/board/mister-remote/rootfs-overlay/etc/init.d/S50mister-agent`
- Modify: `buildroot/board/mister-remote/post-build.sh`
- Modify: `scripts/tests/poc1b-rootfs_test.sh`
- Modify: `Makefile`

**Configuration:**

```toml
cache_max_bytes = 2147483648
```

The cache root remains compiled/fixed as `/media/fat/fogcast/cache`; the active record remains `/run/fogcast-active.json`.

- [ ] **Step 1: Write failing configuration and composition tests**

Assert missing `cache_max_bytes` defaults to 2 GiB for POC 1 config compatibility, explicit positive values are accepted, zero/negative/overflow values fail, unknown `cache_root` is rejected, startup creates/opens the fixed cache manager, startup inventory failure aborts before listening, and `httpapi.WithContent` is present in the real handler. Decode the raw TOML field as `*int64` so omitted and explicitly zero values remain distinguishable.

- [ ] **Step 2: Write the failing synthetic end-to-end test**

Construct temporary NAS roots, raw/ZIP synthetic games, SQLite index, staging root, target cache, fake MiSTer command pipe/core-name file, real scanner/preparer/service/client/v2 HTTP/cache/coordinator, and the existing fake command writer. Prove scan -> first upload -> launch, second launch with zero uploaded bytes, agent reconstruction -> verified hit, source removal/offline cached launch, uncached offline failure preserving active core, interrupted upload cleanup/retry, and unchanged v1 launch/stop behavior.

- [ ] **Step 3: Write failing root-image policy tests**

Assert the init script creates `/media/fat/fogcast/cache/{megadrive,snes}` with mode 0700 after FAT is available, never recursively removes the cache, and starts the agent only afterward. Assert post-build still rejects ROM/archive/database/staging payloads and contains no token/path. Add `poc2-rootfs-test` to `make test`.

- [ ] **Step 4: Run focused tests and verify red**

```bash
mise exec go@1.26.5 -- go test ./internal/agentconfig ./cmd/mister-agent ./internal/integration -run 'Cache|FogCast|Content' -v
sh scripts/tests/poc2-rootfs_test.sh
```

Expected: FAIL because real target composition and policy are not wired.

- [ ] **Step 5: Wire cache-enabled `mister-agent`**

Load config, open `targetcache` with the fixed root/record and configured ceiling, construct the existing runtime/coordinator, construct `agent.ContentController`, call `Initialize` so runtime and active record reconcile, and pass `httpapi.WithContent`. Raise server `ReadTimeout` and `WriteTimeout` to 75 seconds while keeping `ReadHeaderTimeout` at 2 seconds and the JSON/launch timeouts unchanged; the upload context remains bounded by server timeout and client upload timeout.

- [ ] **Step 6: Update image policy without embedding content**

The init script creates only empty cache directories on writable FAT. Do not add them to the ext4 overlay except the existing `/media/fat` mount point. Extend post-build forbidden patterns to SQLite/WAL/staging/content cache artifacts. Rebuild both development and production root images with the new agent only; do not rebuild or alter kernel, Main, Menu, cores, or controller map.

- [ ] **Step 7: Run full software/image verification and commit**

```bash
mise exec go@1.26.5 -- go test -race ./internal/integration ./cmd/mister-agent ./internal/agentconfig -v
sh scripts/tests/poc2-rootfs_test.sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- go build -trimpath -o /dev/null ./cmd/mister-agent
mise exec go@1.26.5 -- make check
POC1B_CONTAINER_RUNTIME=docker mise exec go@1.26.5 -- make poc1b-images poc1b-verify-images poc1b-qemu-smoke
git diff --check
git add internal cmd/mister-agent deploy/poc1a buildroot scripts/tests Makefile
git commit -m "feat: wire FogCast cache into target appliance"
```

---

### Task 14: POC 2 Provenance Lock and Root-Only Deployment Checkpoint

**Files:**
- Create: `internal/imagepoc/poc2.go`
- Create: `internal/imagepoc/poc2_test.go`
- Create: `cmd/poc2-lock/main.go`
- Create: `cmd/poc2-lock/main_test.go`
- Create from verified outputs: `build/outputs.poc2.lock.toml`
- Create: `scripts/install-poc2.sh`
- Create: `deploy/poc2/install-target.sh`
- Create: `scripts/restore-poc1b-sd.sh`
- Create: `scripts/tests/install-poc2-target_test.sh`
- Create: `scripts/tests/restore-poc1b-sd_test.sh`
- Modify: `Makefile`

**Lock schema:**

| TOML key | Required value |
|---|---|
| `format` | Integer constant `1` |
| `base.poc1a_lock_sha256` | Lowercase SHA-256 of `build/sources.poc1a.lock.toml` |
| `base.poc1b_lock_sha256` | Lowercase SHA-256 of `build/sources.poc1b.lock.toml` |
| `base.accepted_dev_root_sha256` | Existing `outputs.dev_rootfs_sha256` from the accepted POC 1B lock |
| `base.accepted_kernel_sha256` | Existing `outputs.reproduced_kernel_sha256` from the accepted POC 1B lock |
| `outputs.prod_rootfs_sha256` | Lowercase SHA-256 of the twice-reproduced POC 2 production root |
| `outputs.dev_rootfs_sha256` | Lowercase SHA-256 of the twice-reproduced POC 2 development root |

- [ ] **Step 1: Write failing provenance-lock tests**

Cover strict unknown-field rejection, format/version checks, lowercase hashes, missing base/output fields, future fields, atomic stable TOML output, exact hashes of the current POC 1A and POC 1B locks, accepted dev-root/kernel values copied from the POC 1B lock, and exact POC 2 prod/dev file hashes. A changed base lock or kernel must make `verify` fail without rewriting the lock.

- [ ] **Step 2: Run lock tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/imagepoc ./cmd/poc2-lock -run 'POC2|Record|Verify' -v`

Expected: FAIL because the POC 2 output lock does not exist.

- [ ] **Step 3: Implement and record the output lock**

Add `poc2-lock record --poc1a-lock build/sources.poc1a.lock.toml --poc1b-lock build/sources.poc1b.lock.toml --prod build/output/poc1b/prod/linux.img --dev build/output/poc1b/dev/linux.img --output build/outputs.poc2.lock.toml` and a read-only `verify` command. Reuse `imagepoc.VerifyFile` and atomic-file patterns. Refuse recording unless the current kernel output matches `outputs.reproduced_kernel_sha256` in the accepted POC 1B lock; POC 2 never records a new kernel. Add `build-poc2-lock` to `Makefile` and to the aggregate `build` target.

- [ ] **Step 4: Write failing target-installer tests**

Use a rooted fixture like the existing POC 1B installer tests. Assert the installer verifies accepted Main, Menu, Mega Drive core, SNES core, controller map, current POC 1B dev root, and reproduced kernel; refuses the POC 1A root, unknown kernel, altered locks, wrong new-root hash, links, extra archive members, and unsafe paths; makes one verified `/media/fat/linux/linux.img.pre-poc2` backup plus `poc2-checkpoint.state`; atomically replaces only `linux.img`; leaves kernel/cache/FAT assets untouched; survives interruption before and after rename; and is idempotent when the POC 2 dev root is already installed.

- [ ] **Step 5: Implement the root-only transport and checkpoint**

The Mac wrapper validates `MISTER_TARGET=root@HOST`, verifies the POC 2 lock and development image locally, scans the exact package for secrets/ROMs/private paths, uploads to a fixed FAT-side temporary name, and streams the target installer over SSH. The target installer extracts into an exact non-link FAT staging directory, verifies all locks and payloads, stops the recognized agent/Main supervisors, writes/validates the one-time state, renames the root image on the FAT volume, syncs, and never modifies `zImage_dtb`, modules, Main, Menu, cores, controller maps, ROMs, saves, or FogCast cache content.

- [ ] **Step 6: Write and implement failing offline-restore tests**

Require one explicit mount argument resolving beneath `/Volumes`, the exact pre-POC 2 backup/state, matching recorded hashes, current root equal to either locked POC 2 dev root or already-restored POC 1B dev root, and unchanged reproduced kernel. Restore by verified same-volume temporary copy plus rename. Refuse broad roots, links, missing/tampered backups, unexpected kernel, or an unrelated mounted volume; do not delete the POC 2 cache or backup.

- [ ] **Step 7: Run deployment, recovery, and repository gates**

```bash
mise exec go@1.26.5 -- go test -race ./internal/imagepoc ./cmd/poc2-lock -v
sh scripts/tests/install-poc2-target_test.sh
sh scripts/tests/restore-poc1b-sd_test.sh
mise exec go@1.26.5 -- make check build-agent build-poc2-lock
bin/poc2-lock verify --lock build/outputs.poc2.lock.toml --poc1a-lock build/sources.poc1a.lock.toml --poc1b-lock build/sources.poc1b.lock.toml --prod build/output/poc1b/prod/linux.img --dev build/output/poc1b/dev/linux.img
git diff --check
```

- [ ] **Step 8: Commit the reproducible deployment checkpoint**

```bash
git add internal/imagepoc/poc2.go internal/imagepoc/poc2_test.go cmd/poc2-lock build/outputs.poc2.lock.toml scripts/install-poc2.sh scripts/restore-poc1b-sd.sh scripts/tests/install-poc2-target_test.sh scripts/tests/restore-poc1b-sd_test.sh deploy/poc2/install-target.sh Makefile
git commit -m "build: add hash-gated POC 2 deployment"
```

---

### Task 15: POC 2 HIL, Deployment Runbook, and Final Acceptance

**Files:**
- Create: `internal/hil/poc2.go`
- Create: `internal/hil/poc2_test.go`
- Create: `cmd/fogcast-hil/main.go`
- Create: `cmd/fogcast-hil/main_test.go`
- Create: `docs/runbooks/poc2-deploy.md`
- Modify: `Makefile`
- Modify: `README.md` if present at implementation time; otherwise create `README.md`

**Interfaces:**

```go
type POC2Runner struct {
    Service  POC2Service
    Prompt   Prompter
    Sabotage POC2Sabotage
    Now      func() time.Time
    Sleep    func(context.Context, time.Duration) error
}
func (r POC2Runner) Run(context.Context) (Report, error)
```

`POC2Sabotage` performs only explicit operator-confirmed test actions: target reboot, agent restart, NAS-root unmount/remount confirmation, and upload interruption. The runner never mounts/unmounts shares or reboots hardware by itself.

- [ ] **Step 1: Write failing HIL sequence tests**

Use fakes to assert this exact order: empty local index/cache confirmation; scan; first-transfer Sonic; Mega Drive video/audio/controller/playable prompts; first-transfer Mario; SNES prompts; repeat both with zero upload; target reboot and cached launches; NAS-offline cached launches; uncached offline rejection with state preserved; interrupted upload with no launchable part; alternating launches; stop/black; invalid request; agent restart reconciliation; power-cycle confirmation; POC 1 regression prompt; artifact audit. Any failed check stops unsafe dependent actions and produces a failed report.

- [ ] **Step 2: Write failing report/privacy tests**

Assert atomic mode-0600 report writing under ignored `artifacts/hil/poc2.json`, timestamps/check names/pass state, no bearer token, no absolute NAS/cache/staging paths, no ROM bytes, and refusal to overwrite through a symlink. Confirm the CLI requires explicit `--sonic-id` and `--mario-id` values rather than embedding private source names/paths.

- [ ] **Step 3: Run tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/hil ./cmd/fogcast-hil -run 'POC2|Privacy|Report' -v`

Expected: FAIL because the POC 2 HIL runner is absent.

- [ ] **Step 4: Implement the operator-assisted runner and build target**

Reuse the existing `Check`/`Report`/`Prompter` types. Add `build-fogcast-hil` for `bin/fogcast-hil` on Darwin arm64 with CGo disabled. Do not modify the existing `mister-hil` sequence or its default POC 1 report.

- [ ] **Step 5: Write deployment, recovery, and audit procedure**

The runbook must identify the dedicated MiSTer Pi; start from a verified POC 1B backup; build/verify new dev and prod roots; preserve accepted kernel/Main/Menu/core/controller hashes; deploy the dev image using the existing hash-gated procedure adapted to the newly recorded root hash; create a local FogCast config without committing it; reset only the exact FogCast index/staging/cache paths after explicit confirmation; recover by restoring the accepted POC 1 image; and execute all fourteen hardware gates from the approved design.

Document the two mounted source roots only as generic operator-supplied example paths. Never place the user's NAS addresses, share names, game filenames, target token, or live target address in the committed runbook.

- [ ] **Step 6: Run the complete pre-hardware gate**

```bash
mise exec go@1.26.5 -- make check build
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go build ./cmd/fogcast ./cmd/fogcast-hil ./cmd/misterctl ./cmd/mister-hil
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- go build -o /dev/null ./cmd/mister-agent
POC1B_CONTAINER_RUNTIME=docker mise exec go@1.26.5 -- make poc1b-verify-images poc1b-qemu-smoke poc1b-verify-kernel
rg -n -i 'token\s*=|/Volumes/|192\.168\.|\.sqlite3|\.sfc|\.smc|\.gen|\.md|\.zip' --glob '!docs/superpowers/**' --glob '!**/*_test.go' .
git diff --check
git status --short
```

Review every audit match; allow only documented example values and code extension allowlists. No live token, address, NAS path, ROM filename, database, staged content, or cache file may be tracked.

- [ ] **Step 7: Commit HIL and runbook**

```bash
git add internal/hil/poc2.go internal/hil/poc2_test.go cmd/fogcast-hil docs/runbooks/poc2-deploy.md Makefile README.md .gitignore
git commit -m "test: add FogCast POC 2 acceptance workflow"
```

- [ ] **Step 8: Execute hardware acceptance on the dedicated MiSTer Pi**

Follow `docs/runbooks/poc2-deploy.md` exactly. Record a local ignored `artifacts/hil/poc2.json`, rerun the unchanged `bin/mister-hil` for all 34 POC 1 checks, and manually confirm both games for HDMI video, HDMI audio, controller input, and playability on first transfer, repeat cache hit, post-reboot cache hit, and NAS-offline cache hit.

- [ ] **Step 9: Final verification and completion commit**

```bash
mise exec go@1.26.5 -- make check build
git diff --check
git status --short
git log --oneline --decorate -15
```

Confirm the local POC 2 report passes, the POC 1 report still has 34 passing checks, the worktree contains no untracked private material outside ignored paths, and no implementation requirement remains. Commit only any reviewed secret-free acceptance metadata; never commit either HIL JSON report.

---

## Final Definition of Done

- `fogcast scan`, `games`, `search`, `launch`, `health`, `status`, and `stop` work from the approved defaults on the Apple Silicon Mac.
- The SQLite index survives process restart, retains games across offline roots, and invalidates remembered content only when the complete source fingerprint changes.
- Raw and single-ROM ZIP sources prepare lazily into bounded, always-cleaned local staging.
- The target verifies and atomically caches content, enforces the 2 GiB default ceiling plus actual FAT free space, evicts deterministic LRU entries, and protects active/in-flight content.
- Sonic and Super Mario World each pass first-transfer, repeat cache-hit, MiSTer-reboot cache-hit, and NAS-offline cache-hit paths on the dedicated MiSTer Pi.
- Interrupted/invalid/oversized/digest-mismatched uploads never become launchable and never disturb the current game.
- The complete POC 1 software, image, deployment, and 34-check hardware suite still passes.
- Go module/imports are `github.com/DeanoC/FogCast-POC`; `fogcast`, `misterctl`, `mister-hil`, `fogcast-hil`, and ARMv7 `mister-agent` build with `CGO_ENABLED=0`.
- Git, build outputs, logs, runbooks, and reports contain no token, ROM bytes, private NAS/target paths, SQLite files, or staged/cache content.
