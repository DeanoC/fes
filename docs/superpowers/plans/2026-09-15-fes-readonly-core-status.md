# Read-only native core status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Make the native FES launcher show truthful, read-only status for the selected fes.pong, fes.zx81, and fes.coleco packages without changing package selection or lifecycle APIs.

**Architecture:** Add a read-only join in the existing ten-foot HTTP client, then carry the normalized status separately through the kitlauncher model so it is not serialized into the offline catalog cache. Refresh it with the existing catalog poll, clear it on read failure, and render compact status copy in the existing tile/detail metadata surfaces.

**Tech Stack:** Go, existing FogCast HTTP client, kitlauncher model, linuxfb fbgrid, Go tests, shell package-runtime smoke fixture.

**Spec:** docs/superpowers/specs/2026-09-15-readonly-core-status-design.md

## Global Constraints

- Only read GET /api/v1/library/core-entries and GET /api/v1/core-packages from the launcher; do not add a selection/install action.
- Preserve the existing fes.pong, fes.zx81, and fes.coleco launch/stop path and the generic catalog path for non-FPGA games.
- Preserve compatibility:"unknown" as unknown; never label it hardware-ready.
- Clear stale core status when the read model cannot be refreshed; ordinary catalog browsing remains available.
- Do not persist core status in launcher-cache/catalog.json.
- This slice uses software tests and the existing fixture smoke only; no full image, FPGA synthesis, or physical-display acceptance.

---

### Task 1: Add the ten-foot core-library read model

Files:
- Modify: ui/tenfoot/client.go
- Test: ui/tenfoot/client_test.go

Interfaces:
- Consumes: Existing Client.getJSON and the two documented GET endpoints.
- Produces:
  - type CoreEntry struct { GameID, Title, CoreID, PackageID string }
  - type CorePackage struct { PackageID, CoreID, Name, Version, Compatibility string }
  - type CoreLibrary struct { Entries []CoreEntry; Packages []CorePackage }
  - type CoreAvailability struct { GameID, Title, CoreID, PackageID, PackageCoreID, PackageName, PackageVersion, Compatibility, State string }
  - func (c *Client) CoreLibrary(context.Context) (CoreLibrary, error)
  - func (l CoreLibrary) Availability() []CoreAvailability
  - func (s CoreAvailability) Label() string

- [ ] Step 1: Write failing client and join tests

Add a table-driven httptest.Server case to ui/tenfoot/client_test.go that serves the documented JSON shape for all three entries. Return one installed package per core, one extra installed package, and a second case where the selected package is missing. Assert that CoreLibrary requests exactly /api/v1/library/core-entries and /api/v1/core-packages, decodes descriptor core ID/name/version and compatibility, and that Availability() returns the deterministic states installed, missing, mismatch, and incompatible for representative rows.

Use package IDs made from repeated hex characters so the test does not depend on generated values:

~~~go
entries := {"entries":[
  {"game_id":"fpga-pong","title":"Pong","core_id":"fes.pong","package_id":"AAA"},
  {"game_id":"fpga-zx81","title":"ZX81","core_id":"fes.zx81","package_id":"BBB"},
  {"game_id":"fpga-coleco","title":"Coleco","core_id":"fes.coleco","package_id":"CCC"}
]}
~~~

- [ ] Step 2: Run the focused test to verify it fails

Run from sources/FogCast:

~~~sh
go test ./ui/tenfoot -run 'TestClientCoreLibrary|TestCoreAvailability' -count=1
~~~

Expected: compile or test failure because the new read-model types and client method do not exist.

- [ ] Step 3: Implement the minimal wire types and GET method

Decode the package response through private wire structs because the public client model should not import server-internal fogcast or corepackage types. CoreLibrary must normalize nil slices to empty slices. Fetch the two endpoints without changing the existing host API or issuing a mutation.

The method must return the first endpoint error and must not silently convert a non-2xx response into an empty inventory.

- [ ] Step 4: Implement the deterministic availability join

Index installed packages by exact PackageID, iterate entries in entry order, and assign:

~~~go
if packageID is absent { state = "missing" }
else if package.CoreID != entry.CoreID { state = "mismatch" }
else if strings.EqualFold(package.Compatibility, "incompatible") { state = "incompatible" }
else { state = "installed" }
~~~

Retain the raw compatibility string. Label() must include the core ID and state, and when present include the package version and only a short package-ID prefix; it must render unknown as unknown rather than ready.

- [ ] Step 5: Run the focused test to verify it passes

Run:

~~~sh
go test ./ui/tenfoot -run 'TestClientCoreLibrary|TestCoreAvailability' -count=1
~~~

Expected: PASS, including the endpoint-path and all join-state assertions.

- [ ] Step 6: Commit the client slice

~~~sh
git add ui/tenfoot/client.go ui/tenfoot/client_test.go
git commit -m "feat: expose read-only core package status"
~~~

### Task 2: Carry core status through kitlauncher refreshes without stale cache data

Files:
- Modify: ui/kitlauncher/model.go
- Modify: ui/kitlauncher/run.go
- Test: ui/kitlauncher/model_test.go
- Test: ui/kitlauncher/run_test.go

Interfaces:
- Consumes: tenfoot.CoreLibrary, tenfoot.CoreAvailability, and the existing Run observation/poll loop.
- Produces:
  - Model.CoreStatuses []tenfoot.CoreAvailability
  - Model.CoreStatusUnavailable bool
  - Model.CoreStatusRevision uint64
  - func (m *Model) ApplyCoreStatuses([]tenfoot.CoreAvailability)
  - func (m *Model) ClearCoreStatuses(unavailable bool)
  - func (m Model) CoreStatusForGame(string) (tenfoot.CoreAvailability, bool)

- [ ] Step 1: Write failing model tests

Add tests that apply the three FES statuses to a model containing three FPGA games and assert lookup by GameID, revision advancement, and that ClearCoreStatuses(true) removes every old status while marking the inventory unavailable. Add a test proving an ordinary catalog refresh does not serialize or inherit core statuses through CatalogSnapshot.

Example assertion shape:

~~~go
m := Model{}
m.ApplyCoreStatuses([]tenfoot.CoreAvailability{{GameID: "fpga-pong", CoreID: "fes.pong", State: "installed"}})
got, ok := m.CoreStatusForGame("fpga-pong")
if !ok || got.State != "installed" { t.Fatalf("status = %#v, ok=%v", got, ok) }
~~~

- [ ] Step 2: Run the focused model tests to verify they fail

Run:

~~~sh
go test ./ui/kitlauncher -run 'TestCoreStatus|TestCatalogSnapshot' -count=1
~~~

Expected: compile failure because the model fields and methods do not exist.

- [ ] Step 3: Add status state to Model

Store status separately from Catalog, Games, Strip, and tenfoot.Game. Copy the input slice, increment CoreStatusRevision only when the normalized status changes, and use exact GameID lookup. ClearCoreStatuses must drop the old slice and set the requested unavailable bit. Do not modify CatalogSnapshot or the JSON representation of tenfoot.Game.

- [ ] Step 4: Add the refresh observation

Extend the existing observation with a successful status payload and a status-error flag. When the normal catalog refresh is requested, call c.Library.CoreLibrary(ctx) after the catalog fetch. A core-status error must not discard a successful catalog: clear old statuses and mark them unavailable. On a successful response apply the new statuses and clear the unavailable flag. When the host is absent, clear statuses so an offline boot cannot display stale package claims.

- [ ] Step 5: Verify the run-loop behavior

Extend the existing fake-host run test with the two read endpoints and assert that a Model observation contains fes.pong, fes.zx81, and fes.coleco. Add a failure response for one endpoint and assert that ordinary games still arrive while CoreStatusUnavailable is true and no previous status remains.

Run:

~~~sh
go test ./ui/kitlauncher -run 'TestCoreStatus|TestRun.*Catalog|TestCatalogSnapshot' -count=1
~~~

Expected: PASS.

- [ ] Step 6: Commit the refresh slice

~~~sh
git add ui/kitlauncher/model.go ui/kitlauncher/run.go ui/kitlauncher/model_test.go ui/kitlauncher/run_test.go
git commit -m "feat: refresh core package status with launcher catalog"
~~~

### Task 3: Render truthful status in the native kit UI

Files:
- Modify: cmd/fogcast-kit/main.go
- Test: cmd/fogcast-kit/main_test.go

Interfaces:
- Consumes: Model.CoreStatusForGame, Model.CoreStatusRevision, and tenfoot.CoreAvailability.Label.
- Produces: status copy in FPGA tile metadata and focused detail metadata, plus renderer invalidation when only status changes.

- [ ] Step 1: Write failing render-model tests

Add tests that build a Pong tile and focused detail with an installed status and assert the metadata contains fes.pong and installed. Add missing and incompatible cases and assert the exact state words are preserved. Add a renderer-key test showing different CoreStatusRevision values produce different keys, while a non-FPGA game remains unchanged.

- [ ] Step 2: Run the focused render tests to verify they fail

Run:

~~~sh
go test ./cmd/fogcast-kit -run 'Test.*CoreStatus|TestModelRenderKey' -count=1
~~~

Expected: FAIL because the current tile/detail builders do not consume core status and the render key has no status revision.

- [ ] Step 3: Add status-aware tile/detail metadata

Keep the existing gameTile helper as a no-status wrapper for existing tests and add a status-aware helper used by modelGrid, strip, series, and detail builders. Append the compact CoreAvailability.Label() to the existing metadata with the current ASCII normalization. Do not alter launch eligibility or input handling.

- [ ] Step 4: Add status refresh invalidation and unavailable footer copy

Include CoreStatusRevision in renderKey. When the model has FPGA games and the read model is unavailable, use the existing footer surface to show Core package status unavailable unless a stronger transient/launch/stop message is already active. Do not replace the existing offline message while the host is disconnected.

- [ ] Step 5: Run the focused and package tests

Run:

~~~sh
go test ./cmd/fogcast-kit -run 'Test.*CoreStatus|TestModelRenderKey' -count=1
go test ./ui/tenfoot ./ui/kitlauncher ./cmd/fogcast-kit -count=1
~~~

Expected: PASS.

- [ ] Step 6: Commit the UI slice

~~~sh
git add cmd/fogcast-kit/main.go cmd/fogcast-kit/main_test.go
git commit -m "feat: show native core package status"
~~~

### Task 4: Verify the three-core contract and document the boundary

Files:
- Modify: sources/FogCast/docs/core-package-library.md
- Test: sources/FogCast/scripts/tests/package-runtime-smoke_test.sh (only if the existing fixture lacks an assertion needed to retain all three cores)

Interfaces:
- Consumes: The existing package-runtime smoke contract and the approved read-only design.
- Produces: Documentation that the native launcher is status-read-only and a final verification record.

- [ ] Step 1: Add the client/UI boundary to the package-library documentation

Document that the native launcher reads core entries and installed package inventory, shows selected-package status, and does not change selection. State explicitly that package compatibility unknown is not hardware acceptance and that package replacement remains core-select/API work for a later UI slice.

- [ ] Step 2: Preserve or extend the three-core smoke assertion

Run the existing fixture first. Only add assertions if necessary to retain the three declared selection records and their launch/stop requests for fes.pong, fes.zx81, and fes.coleco; do not convert the fixture into a hardware test.

- [ ] Step 3: Run the complete bounded verification

Run from sources/FogCast:

~~~sh
go test ./ui/tenfoot ./ui/kitlauncher ./cmd/fogcast-kit -count=1
bash scripts/tests/package-runtime-smoke_test.sh
~~~

Expected: all Go tests pass and the shell fixture reports PASS for all three FES package launch/stop cycles.

- [ ] Step 4: Inspect the final diff and status

Run:

~~~sh
git diff --check
git status --short
git log --oneline --decorate -8
~~~

Expected: only the planned client/model/UI/documentation files are changed, with no generated image, FPGA, cache, or local credential artifacts.

- [ ] Step 5: Commit the documentation and verification slice

~~~sh
git add docs/core-package-library.md
git commit -m "docs: define read-only native core status boundary"
~~~

- [ ] Step 6: Prepare review evidence

Record the focused test commands, complete bounded verification output, and the explicit limitation that hardware/display acceptance remains separate. The PR must not claim package compatibility or physical target acceptance from these tests.
