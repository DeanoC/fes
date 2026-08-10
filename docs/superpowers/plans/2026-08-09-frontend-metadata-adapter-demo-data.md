# Frontend Metadata Adapter and Demo Data Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the minimal FogCast catalog page with a polished,
self-contained launcher that enriches live catalog records through a
deterministic frontend metadata adapter without changing any backend or
host/target contract.

**Architecture:** Keep `internal/hostapi.UIHandler` as the only HTTP boundary,
but assemble its HTML response from Go-embedded shell, CSS, metadata-adapter,
and launcher-app assets. The adapter is a dependency-free JavaScript module
that exports pure normalization and merge functions for Node tests and exposes
the same API to the browser. The launcher app renders only normalized view
models with DOM text APIs and always uses the original live `game.id` for
detail and launch requests.

**Tech Stack:** Go 1.26.5 standard library (`embed`, `net/http`), browser-native
HTML/CSS/JavaScript, Node.js 26 built-in test runner (`node:test`), existing Go
test suite.

## Global Constraints

- This is frontend-only: do not change catalog JSON, the public host/target
  protocol, launch semantics, target lifecycle, or hardware ownership.
- The live catalog remains authoritative for ID, title, system, source state,
  source availability, prepared-content state, and live launch state. The
  catalog `execution` field is unreliable compatibility data in this milestone:
  hide/defer it and never use it for eligibility, authorization, launch
  identity, metadata merge, or user-facing execution claims. Actual launch
  resolution remains backend authority; backend semantic correction is deferred
  to a separately scoped follow-up.
- The browser makes no third-party requests and uses no remote fonts, remote
  images, API keys, tracking, scraping, ROM contents, local paths, target
  identities, randomness, or current time.
- All artwork is visibly synthetic demo presentation built from static CSS
  treatments and a fixed allowlist of palette tokens.
- Every metadata result is normalized to complete safe defaults; metadata
  failure must never block browsing or launch.
- User-controlled values are rendered with `textContent`/`createTextNode` and
  are never assigned to `innerHTML`, `outerHTML`, or HTML-bearing insertion
  APIs.
- Search membership comes only from `GET /api/v1/games?q=...`; the frontend
  does not invent or locally filter games.
- Launch requests remain exactly
  `POST /api/v1/session/launch` with JSON `{ "game_id": <live catalog ID> }`.
- Preserve `text/html; charset=utf-8`, loopback-only serving, and `no-store`
  behavior.
- Evidence from this plan is Software-tested only; it makes no HIL claim.
- Do not stage, commit, push, deploy, or operate hardware without separate
  explicit user authorization.

## Ownership, Workspace, and Review

- **Implementation owner:** Luna (`gpt-5.6-luna`; use
  `gpt-5.6-terra` only if Luna is unavailable and record the fallback).
- **Writable worktree:** create one isolated worktree at execution time using
  `superpowers:using-git-worktrees`; the owner has exclusive write access to
  all files listed below.
- **Independent reviewer:** Vega, read-only, normally `gpt-5.6-sol`, reviews
  the exact implementation diff after Tasks 1 and 2 and again after final
  integration. Record the model, diff inspected, and Critical/Important
  dispositions.
- **Governing decision:**
  `docs/superpowers/specs/2026-08-09-frontend-metadata-adapter-demo-data-design.md`.
- **Rollback implications:** source-only frontend changes; rollback is removal
  of the new embedded assets/tests and restoration of `internal/hostapi/ui.go`
  and `internal/hostapi/ui_test.go`. No persisted data, API migration, target
  state, or hardware rollback is involved.

## File Map

- Create `internal/hostapi/ui_shell.html`: semantic launcher document with
  explicit insertion markers for embedded styles and scripts.
- Create `internal/hostapi/ui.css`: responsive launcher layout, synthetic
  artwork treatments, focus states, and loading/error/empty presentation.
- Create `internal/hostapi/ui_metadata.js`: pure `GameMetadataAdapter`, curated
  records, deterministic fallback, normalization, and catalog-first merge.
- Create `internal/hostapi/ui_metadata_test.js`: dependency-free executable
  adapter tests using `node:test`.
- Create `internal/hostapi/ui_app.js`: API calls, state transitions, safe DOM
  rendering, search, detail refresh, retry, and launch behavior.
- Create `internal/hostapi/ui_app_test.js`: dependency-free tests for pure app
  helpers, especially request paths and launch payload identity.
- Modify `internal/hostapi/ui.go`: embed and assemble the assets into one HTML
  response while preserving the handler API and response headers.
- Modify `internal/hostapi/ui_test.go`: verify self-containment, asset
  assembly, safe DOM operations, required UI states, and absence of external
  origins.
- Modify `Makefile`: add the two Node test files to the normal test target so
  adapter/contract coverage cannot silently drop out of CI.

---

### Task 1: Executable Metadata Adapter Boundary

**Owner:** Luna

**Files:**

- Create: `internal/hostapi/ui_metadata.js`
- Create: `internal/hostapi/ui_metadata_test.js`

**Interfaces:**

- Consumes catalog-shaped objects with `id`, `title`, `system`, `state`,
  `root_online`, `content_prepared`, and the unreliable compatibility `execution`
  field. The adapter treats `execution` as untouched wire data, never as a
  frontend policy input.
- Produces global/CommonJS `FogCastMetadata` with:
  `metadataFor(game) -> GamePresentationMetadata`,
  `normalizeMetadata(value, game) -> GamePresentationMetadata`, and
  `toLauncherGame(game, adapter?) -> LauncherGame`.
- `GamePresentationMetadata` contains `cover`, `backdrop`, `summary`, `year`,
  `genre`, `studio`, `players`, and `isFallback`. `cover` and `backdrop` contain
  only allowlisted presentation tokens, never raw CSS from catalog data.
- `LauncherGame` copies all catalog fields unchanged and adds a `presentation`
  property. No presentation field can overwrite a catalog field.

- [ ] **Step 1: Write failing adapter tests**

Create `internal/hostapi/ui_metadata_test.js` with Node built-ins only. Cover
these behaviors with concrete catalog fixtures:

```javascript
const test = require('node:test');
const assert = require('node:assert/strict');
const {
  metadataFor,
  normalizeMetadata,
  toLauncherGame,
} = require('./ui_metadata.js');

const sonic = Object.freeze({
  id: 'megadrive-sonic-test',
  title: 'Sonic the Hedgehog',
  system: 'megadrive',
  state: 'available',
  root_online: true,
  content_prepared: true,
  execution: 'fpga_native',
});

test('metadataFor returns the same complete value for the same game', () => {
  assert.deepEqual(metadataFor(sonic), metadataFor(sonic));
  for (const field of ['cover', 'backdrop', 'summary', 'year', 'genre', 'studio', 'players']) {
    assert.ok(metadataFor(sonic)[field], `missing ${field}`);
  }
});

test('curated metadata requires both the intended system and normalized title', () => {
  const curated = metadataFor(sonic);
  const wrongSystem = metadataFor({ ...sonic, id: 'snes-sonic', system: 'snes' });
  assert.equal(curated.isFallback, false);
  assert.equal(wrongSystem.isFallback, true);
});

test('unmatched catalog games always receive complete fallback metadata', () => {
  const result = metadataFor({ id: 'unknown-1', title: 'Unknown', system: 'snes' });
  assert.equal(result.isFallback, true);
  assert.match(result.summary, /demo/i);
  assert.ok(result.cover.palette);
  assert.ok(result.backdrop.palette);
});

test('normalization recovers from missing and throwing provider data', () => {
  const missing = normalizeMetadata(undefined, sonic);
  const throwing = toLauncherGame(sonic, { metadataFor() { throw new Error('offline'); } });
  assert.equal(missing.isFallback, true);
  assert.equal(throwing.presentation.isFallback, true);
});

test('catalog identity and operational fields win during merge', () => {
  const poisoned = {
    metadataFor() {
      return {
        id: 'replacement', title: 'Replacement', system: 'other',
        state: 'offline', execution: 'host_cast', summary: 'Presentation only',
      };
    },
  };
  const merged = toLauncherGame(sonic, poisoned);
  for (const field of ['id', 'title', 'system', 'state', 'root_online', 'content_prepared', 'execution']) {
    assert.equal(merged[field], sonic[field]);
  }
  assert.equal(merged.presentation.summary, 'Presentation only');
});
```

Add a palette-boundary assertion that every generated `cover.palette` and
`backdrop.palette` belongs to an exported frozen allowlist. Add a fallback test
using strings containing `<`, `>`, quotes, and CSS-like content to prove those
strings are retained only as text fields and never become artwork tokens.

- [ ] **Step 2: Run the adapter test and verify RED**

Run:

```sh
node --test internal/hostapi/ui_metadata_test.js
```

Expected: FAIL because `ui_metadata.js` does not exist.

- [ ] **Step 3: Implement the minimal adapter module**

Create `internal/hostapi/ui_metadata.js` as an IIFE that assigns a frozen API
to `globalThis.FogCastMetadata` and `module.exports` when CommonJS is present.
Implement:

- a frozen palette allowlist with semantic token names such as `ember`,
  `lagoon`, `violet`, `sunset`, and `forest`;
- a curated map keyed by normalized `system + "\0" + title`, initially for a
  small set of recognizable SNES and Mega Drive titles;
- a stable 32-bit FNV-1a-style hash over `String(game.id) + "\0" +
  String(game.system)`;
- fallback palette/treatment selection by modulo into fixed arrays;
- system-label defaults from a fixed map, falling back to `Unknown system`;
- normalization that converts optional display values to bounded strings,
  supplies every missing field, replaces invalid artwork tokens with fallback
  tokens, and marks recovery as `isFallback: true`; and
- a catalog-first merge implemented as `{ ...game, presentation }`, never as a
  spread of presentation fields over catalog fields.

Do not call `Math.random`, `Date`, `fetch`, storage APIs, or browser location
APIs in this module.

- [ ] **Step 4: Run the adapter tests and verify GREEN**

Run:

```sh
node --test internal/hostapi/ui_metadata_test.js
```

Expected: PASS with no skipped tests or warnings.

- [ ] **Step 5: Refactor without changing behavior**

Freeze exported tables and returned presentation objects, name the hash and
normalization helpers by responsibility, and keep provider recovery confined
to `toLauncherGame`. Re-run Step 4 after refactoring.

- [ ] **Step 6: Review checkpoint; do not commit**

Run `git diff -- internal/hostapi/ui_metadata.js
internal/hostapi/ui_metadata_test.js`, then request the required read-only Vega
review of that exact diff. Resolve all Critical and Important findings. Do not
stage or commit without explicit user authorization.

---

### Task 2: Self-Contained Launcher Rendering and State Machine

**Owner:** Luna

**Files:**

- Create: `internal/hostapi/ui_shell.html`
- Create: `internal/hostapi/ui.css`
- Create: `internal/hostapi/ui_app.js`
- Create: `internal/hostapi/ui_app_test.js`
- Modify: `internal/hostapi/ui.go`
- Modify: `internal/hostapi/ui_test.go`

**Interfaces:**

- Consumes `globalThis.FogCastMetadata.toLauncherGame`, the existing
  `/api/v1/games`, `/api/v1/games/{id}`, and `/api/v1/session/launch`
  endpoints, and normalized privacy-safe API errors.
- Produces the unchanged `UIHandler() http.Handler` and
  `UIHTMLForTest() string` Go functions.
- Produces global/CommonJS `FogCastApp` pure helpers:
  `gamesPath(query) -> string`, `gameDetailPath(id) -> string`, and
  `launchRequest(game) -> {path, options}` for executable contract tests.
- Browser rendering owns explicit `loading`, `populated`, `empty`,
  `no_matches`, `catalog_error`, `metadata_fallback`, `launching`,
  `launch_success`, and `launch_error` states.

- [ ] **Step 1: Write failing pure app contract tests**

Create `internal/hostapi/ui_app_test.js` with Node built-ins and import
`FogCastApp` from `ui_app.js`. Assert:

```javascript
const test = require('node:test');
const assert = require('node:assert/strict');
const { gamesPath, gameDetailPath, launchRequest } = require('./ui_app.js');

test('gamesPath delegates search membership to the live API', () => {
  assert.equal(gamesPath(''), '/api/v1/games');
  assert.equal(gamesPath('sonic & tails'), '/api/v1/games?q=sonic%20%26%20tails');
});

test('gameDetailPath safely encodes the live catalog ID', () => {
  assert.equal(gameDetailPath('game/id'), '/api/v1/games/game%2Fid');
});

test('launchRequest preserves the exact live game ID contract', () => {
  const request = launchRequest({
    id: 'live-id',
    presentation: { id: 'fake-id' },
  });
  assert.equal(request.path, '/api/v1/session/launch');
  assert.equal(request.options.method, 'POST');
  assert.equal(request.options.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(request.options.body), { game_id: 'live-id' });
});
```

- [ ] **Step 2: Expand Go tests before changing production assembly**

In `internal/hostapi/ui_test.go`, keep the existing response/content-type test
and add focused tests that inspect the fully assembled `UIHTMLForTest()`:

- exactly one inline `<style>` and the expected inline scripts are present;
- no `<script src>`, `<link rel="stylesheet">`, `http://`, `https://`,
  protocol-relative origin, remote font directive, or external image origin is
  present;
- the document contains labels or explicit state identifiers for loading,
  empty catalog, no matches, catalog failure with retry, metadata fallback,
  launch progress, success, and failure;
- `ui_app.js` uses `textContent`, `createElement`, and `replaceChildren`, and
  the assembled scripts contain none of `.innerHTML`, `.outerHTML`,
  `insertAdjacentHTML`, `document.write`, or `eval(`;
- the assembled app references only the three existing API endpoint families;
  and
- the shell includes accessible labels, a status live region, a main heading,
  keyboard-focusable controls, and mobile viewport metadata.

Make each assertion report the forbidden or missing token so failures are
diagnosable.

- [ ] **Step 3: Run the focused tests and verify RED**

Run:

```sh
node --test internal/hostapi/ui_app_test.js
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go test ./internal/hostapi -run 'TestUI' -v
```

Expected: Node FAIL because `ui_app.js` does not exist; Go FAIL on the new
self-containment/state/safe-DOM assertions against the current minimal page.

- [ ] **Step 4: Build the semantic shell and responsive visual system**

Create `ui_shell.html` with placeholders `{{FOGCAST_STYLES}}`,
`{{FOGCAST_METADATA}}`, and `{{FOGCAST_APP}}`. Include:

- a compact brand/header and privacy-safe host status;
- a labeled search input and explicit refresh button;
- a catalog region with `aria-busy`, an `aria-live="polite"` status element,
  and retry action container;
- a detail region with an initial selection prompt and launch-action area;
- no inline event attributes and no external resources; and
- demo-art labeling near generated cover/backdrop treatments.

Create `ui.css` with a two-column desktop launcher and a one-column mobile
layout, fixed palette classes/tokens, visible `:focus-visible` outlines,
reduced-motion handling, disabled/loading button states, selected-card state,
and synthetic cover/backdrop geometry. Keep every image effect in local CSS;
do not use `url(...)`.

- [ ] **Step 5: Assemble embedded assets in Go**

In `internal/hostapi/ui.go`, replace the monolithic `const uiHTML` with
`//go:embed` strings for the shell, CSS, metadata module, and app module. Build
the final document once during package initialization by replacing each unique
placeholder. Panic at initialization if a placeholder is missing or appears
more than once so a broken asset cannot silently ship.

Keep:

```go
func UIHTMLForTest() string
func UIHandler() http.Handler
```

and preserve `Content-Type: text/html; charset=utf-8`. Do not modify
`server.go`, API result types, routing, or `noStore`.

- [ ] **Step 6: Implement pure request helpers and browser state rendering**

Create `ui_app.js` with the same browser/CommonJS export pattern as the adapter.
Guard browser startup behind `typeof document !== 'undefined'` so Node can
test pure helpers. Implement:

- `gamesPath`, `gameDetailPath`, and `launchRequest` exactly as specified;
- a single state object holding query, games, selected live game, request
  sequence number, catalog state, metadata fallback count, and launch state;
- request sequence checks so a slow earlier search response cannot replace a
  newer result;
- safe element builders that set classes from fixed literals and user-facing
  strings only through `textContent`;
- list rendering from `games.map(game =>
  FogCastMetadata.toLauncherGame(game))`;
- distinct empty vs. no-search-match views based on whether the query is empty;
- actionable retry for catalog and detail errors;
- detail refresh through `GET /api/v1/games/{live id}` followed by the same
  metadata merge;
- non-fatal metadata fallback labeling on affected cards/details; and
- launch progress, success, and privacy-safe failure rendering using
  `launchRequest(selectedLiveGame)`.

Selection must track the original live `id`; artwork availability and metadata
fields must not participate in selection, search membership, or launch
eligibility.

- [ ] **Step 7: Run focused JavaScript and Go tests and verify GREEN**

Run:

```sh
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go test ./internal/hostapi -run 'TestUI' -v
```

Expected: PASS, no skipped tests, no warnings.

- [ ] **Step 8: Manually inspect the assembled page source**

Run a temporary local host using the repository's existing development command
and inspect the loopback page in a browser. Confirm responsive layout,
keyboard navigation, loading/empty/error/retry visuals, curated and fallback
cards, detail refresh, and launch progress/error behavior. Record this as
developer observation only, not HIL evidence. Do not deploy or connect to
target hardware.

- [ ] **Step 9: Review checkpoint; do not commit**

Inspect the exact Task 2 diff and request read-only Vega review. Resolve all
Critical and Important findings, rerun Step 7, and record reviewer model and
dispositions. Do not stage or commit without explicit user authorization.

---

### Task 3: Integrate Tests and Complete Software Verification

**Owner:** Luna for the Makefile change; root coordinator for final evidence

**Files:**

- Modify: `Makefile`
- Verify: all files from Tasks 1 and 2
- Verify: `docs/superpowers/specs/2026-08-09-frontend-metadata-adapter-demo-data-design.md`
- Verify: `docs/superpowers/plans/2026-08-09-frontend-metadata-adapter-demo-data.md`

**Interfaces:**

- Produces a normal repository test path that runs both JavaScript test files
  before the existing Go race suite and shell checks.
- Produces final Software-tested evidence and an independent review record;
  it does not publish artifacts or make an acceptance claim.

- [ ] **Step 1: Add a failing Makefile integration check**

Add a `.PHONY` `test-ui` target that runs:

```make
test-ui:
	node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
	go test ./internal/hostapi -run 'TestUI' -v
```

Make the existing `test` target depend on `test-ui` as well as `build-agent`.
Before editing, temporarily move or rename one test fixture locally and show
that `make test-ui` fails because the JavaScript suite is genuinely wired in;
restore it immediately afterward without recording the temporary rename in the
diff.

- [ ] **Step 2: Run the narrow integrated target**

Run:

```sh
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make test-ui
```

Expected: PASS for both Node files and focused Go UI tests.

- [ ] **Step 3: Format and run the broader relevant suite**

Run:

```sh
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- gofmt -w internal/hostapi/ui.go internal/hostapi/ui_test.go
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go test -race ./internal/hostapi
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go test ./...
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go vet ./...
```

Expected: every command exits 0. If an unrelated baseline failure appears,
record the exact command/output and stop rather than changing unrelated code.

- [ ] **Step 4: Verify source and privacy boundaries explicitly**

Run:

```sh
rg -n 'https?://|//[^ ]+\.(png|jpe?g|webp|gif)|@import|url\(' internal/hostapi/ui_shell.html internal/hostapi/ui.css internal/hostapi/ui_metadata.js internal/hostapi/ui_app.js
rg -n 'innerHTML|outerHTML|insertAdjacentHTML|document\.write|eval\(' internal/hostapi/ui_*.js
rg -n 'Math\.random|new Date|Date\.now|localStorage|sessionStorage|fetch\(' internal/hostapi/ui_metadata.js
```

Expected: no matches. The app module may contain `fetch(` for the existing
local API; the metadata module must not.

- [ ] **Step 5: Run tracked and untracked whitespace/provenance checks**

Run:

```sh
git diff --check
git status --short
```

For every untracked deliverable reported by status, run the repository-required
full-file check and then hash the full untracked set:

```sh
for f in <exact-untracked-deliverable-paths>; do
  code=0
  result=$(git diff --no-index --check /dev/null "$f" 2>&1) || code=$?
  { test "$code" -eq 1 && test -z "$result"; } || exit 1
done
shasum -a 256 <exact-untracked-deliverable-paths>
```

Expected: no whitespace errors; record SHA-256 values as artifact provenance.

- [ ] **Step 6: Final independent review**

Ask Vega (`gpt-5.6-sol`, read-only) to inspect the exact full diff from base
commit `b0f4e9c`, including every untracked deliverable supplied with full-file
content or no-index diff. The review must check spec coverage, safe DOM use,
catalog authority, launch identity, external origins, deterministic fallback,
test strength, and scope boundaries. Resolve every Critical and Important
finding and rerun affected checks.

- [ ] **Step 7: Handoff; do not commit**

Report:

- base commit and isolated worktree/branch;
- governing approved spec;
- files changed and SHA-256 hashes for untracked deliverables;
- exact commands and results, labeled Software-tested;
- browser observations separately labeled developer observations;
- reviewer role/model/fallback, exact diff, and finding dispositions;
- unresolved risks (notably Node 26 availability in CI and absence of HIL,
  which is intentionally out of scope); and
- next safe action: request explicit authorization before any commit, push,
  deployment, or hardware operation.

## Plan Self-Review

- **Spec coverage:** Tasks 1-3 cover adapter/normalization, curated and fallback
  demo data, safe local artwork, catalog-first merge, all required UI states,
  server-backed search, unchanged launch identity, self-contained serving,
  privacy constraints, narrow and broad software verification, and independent
  review.
- **Boundary check:** No server API, protocol, lifecycle, target, hardware,
  catalog schema, scraper, remote provider, credential, or persistence file is
  modified.
- **Type consistency:** `metadataFor`, `normalizeMetadata`, `toLauncherGame`,
  `gamesPath`, `gameDetailPath`, and `launchRequest` have one spelling and one
  signature throughout.
- **Placeholder check:** The only placeholders are deliberate assembly markers
  named explicitly in Task 2 and the execution-time list of untracked files,
  which must be replaced with exact paths derived from `git status` before the
  command runs. There are no deferred implementation requirements.
