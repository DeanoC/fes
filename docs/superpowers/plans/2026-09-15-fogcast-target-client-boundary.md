# FogCast Target Client Boundary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (\`- [ ]\`) syntax for tracking.

**Goal:** Move target-agent transport and lease ownership out of \`host\` into a dedicated \`targetclient\` package without changing wire behavior.

**Architecture:** \`targetclient\` owns authenticated target HTTP operations, target discovery, and kit leases. \`host\` retains catalog/config/library and host-owned remote input; its bridge starter consumes the targetclient lease through a small exported surface. All in-module consumers import the new package directly, with no compatibility aliases.

**Tech Stack:** Go, shell structural tests, existing \`go test ./...\` package and integration tests.

**Spec:** \`docs/superpowers/specs/2026-09-15-fogcast-target-client-boundary-design.md\`

## Global Constraints

- Preserve all existing target HTTP paths, headers, validation, lease semantics, and error behavior.
- Do not modify the FES parent, submodules, target images, or hardware.
- Do not leave duplicate or compatibility-copy implementations in \`host\`.
- Keep \`targetclient\` independent of \`host\`, \`ui\`, and \`internal/agent\`.
- Run the narrow structural test before each implementation step and the full Go suite before claiming completion.

---

### Task 1: Add the boundary regression test

**Files:**
- Create: \`scripts/tests/target-boundary_test.sh\`
- Modify: \`Makefile:28,43-50\`

**Interfaces:**
- Produces: \`make test-target-boundary\`, a failing test until the package move is complete.

- [ ] **Step 1: Write the failing test**

Create a POSIX shell test that requires \`targetclient/\`, rejects \`host\`
imports from \`ui/kitlauncher\`, rejects forbidden imports from \`targetclient\`, and
searches all Go sources for removed \`host.Client\`/\`host.KitLease\` symbols.

- [ ] **Step 2: Run the test to verify it fails**

Run: \`make test-target-boundary\`

Expected: FAIL because \`targetclient/\` does not exist and the current
\`ui/kitlauncher\` hostless path imports \`host\`.

- [ ] **Step 3: Add the Makefile target**

Add \`test-target-boundary\` to \`.PHONY\` and make it invoke the new script.

- [ ] **Step 4: Commit the red test**

\`\`\`sh
git add Makefile scripts/tests/target-boundary_test.sh
git commit -m "test: define FogCast target client boundary"
\`\`\`

### Task 2: Move the target client implementation and tests

**Files:**
- Create: \`targetclient/client.go\`, \`targetclient/kit_lease.go\`, \`targetclient/discovery.go\`, \`targetclient/content_client.go\`, \`targetclient/core_data_client.go\`, \`targetclient/core_package_client.go\`, \`targetclient/development_client.go\`, \`targetclient/development_media.go\`, \`targetclient/appliance_update.go\`
- Move: corresponding implementation tests from \`host/\` into \`targetclient/\` where they exercise package-private lease/discovery/update helpers.
- Delete: the moved implementation files from \`host/\`

**Interfaces:**
- Produces: \`targetclient.Client\`, \`targetclient.KitLease\`, \`targetclient.CastStatus\`, \`targetclient.KitOwnership\`, \`targetclient.ApplianceStatus\`, and \`targetclient.ResolveAppliance\` with the existing method signatures.
- Consumes: existing \`protocol\`, \`internal/core\`, \`internal/corepackage\`, \`internal/kitlease\`, and \`appliance\` packages only.

- [ ] **Step 1: Move implementation files and package-local tests**

Move the target transport files listed above. Change their package declaration
to \`targetclient\`. Move \`appliance_update_test.go\`, \`discovery_test.go\`, and
\`kit_lease_test.go\` with their implementation so package-private behavior stays
covered. Move the external target-client tests with the package so \`go test
./targetclient\` owns the client behavior.

- [ ] **Step 2: Export only the bridge-facing lease operations**

Rename the lease methods needed by \`host/target_bridge_starter.go\`:

\`\`\`go
func (l *KitLease) AuthorizeExisting(r *http.Request, token string) error
func (l *KitLease) CurrentToken() string
\`\`\`

Keep all other implementation helpers unexported.

- [ ] **Step 3: Run the moved package tests**

Run: \`go test ./targetclient\`

Expected: compilation failures only at remaining consumers or package-name
references; no targetclient behavior test should be silently removed.

### Task 3: Update host bridge/library and all consumers

**Files:**
- Modify: \`host/library.go\`, \`host/target_bridge_starter.go\`, and affected host tests.
- Modify: \`fogcast/discovery.go\`, \`fogcast/service.go\`, \`fogcast\` tests.
- Modify: \`cmd/fes-update/main.go\`, \`cmd/fogcast-api/main.go\` and tests, \`ui/kitlauncher/hostless.go\` and tests, \`internal/integration\` tests, and any remaining Go consumer found by \`rg\`.

**Interfaces:**
- \`host.Library\` stores \`*targetclient.Client\`.
- \`host.HTTPBridgeStarterConfig.KitLease\` is \`*targetclient.KitLease\`.
- FogCast service target concrete assertions use \`*targetclient.Client\` and \`*targetclient.KitLease\`.
- Cast and appliance result types come from \`targetclient\`.

- [ ] **Step 1: Update host-owned code**

Import \`targetclient\` in \`host/library.go\` and
\`host/target_bridge_starter.go\`. Replace bridge calls to the former
package-private lease helpers with \`AuthorizeExisting\` and \`CurrentToken\`.
Keep remote-input interfaces and bridge types in \`host\`.

- [ ] **Step 2: Update application and UI consumers**

Replace target-client imports and concrete types in FogCast, commands, kit
launcher, and integration tests. \`ui/kitlauncher\` must import \`targetclient\`
and \`internal/agentconfig\`, but not \`host\`.

- [ ] **Step 3: Run focused package tests**

Run: \`go test ./targetclient ./host ./fogcast ./ui/kitlauncher ./cmd/fogcast-api ./cmd/fes-update ./internal/integration\`

Expected: PASS with the target-client tests now running under \`./targetclient\`.

### Task 4: Update architecture documentation and enforce the green boundary

**Files:**
- Modify: \`README.md\` source-boundary section.
- Modify: \`docs/ARCHITECTURE.md\` source-ownership section.
- Modify: \`scripts/tests/target-boundary_test.sh\` only if the final import graph requires a narrower explicit allow-list.

- [ ] **Step 1: Document current ownership**

State that \`targetclient\` owns host-to-target transport and lease ownership,
\`host\` owns catalog/config/library and remote-input bridge code, and
\`internal/agent\` remains target-side coordination. Label no proposed future
repository split as current behavior.

- [ ] **Step 2: Run the structural test**

Run: \`make test-target-boundary\`

Expected: PASS with no forbidden imports or removed symbols.

- [ ] **Step 3: Check the diff**

Run: \`git diff --check\` and \`git status --short\`.

Expected: no whitespace errors and only the planned files changed.

- [ ] **Step 4: Commit the implementation**

\`\`\`sh
git add targetclient host fogcast cmd ui internal README.md docs/ARCHITECTURE.md Makefile scripts/tests/target-boundary_test.sh
git commit -m "refactor: separate FogCast target client"
\`\`\`

### Task 5: Verify the complete FogCast component

**Files:**
- No additional files; verification only.

- [ ] **Step 1: Run the full Go suite**

Run: \`go test ./...\`

Expected: exit code 0 and every package reports pass or no test files.

- [ ] **Step 2: Run vet**

Run: \`go vet ./...\`

Expected: exit code 0 with no diagnostics.

- [ ] **Step 3: Run the repository checks relevant to this boundary**

Run: \`make test-target-boundary test-ui-boundary\`.

Expected: both structural checks pass.

- [ ] **Step 4: Request review**

Push the branch and open a FogCast PR against \`main\` with the design document,
the boundary rationale, and exact verification output. Ask the reviewer to
focus on unchanged target wire behavior, lease behavior, and forbidden import
direction.
