# FES Target-Service Boundary Preparation Implementation Plan

Status reconciled 2026-09-17: historical implementation plan. Contract preparation integrated through FogCast #244 and FES #57. A target module/repository split was not implemented and is not currently scheduled.
Use [current structure and refactor status](../../fes-structure.md) for remaining
work. Original task checkboxes and execution constraints below are historical,
not instructions to repeat merged work or proof of unrecorded acceptance.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the FogCast host/UI and target-agent contract seams module-safe so a later target-service module or repository split is evidence-driven and does not move runtime behavior blindly.

**Architecture:** Keep the FogCast root module and current wire/API behavior intact for this slice. Publish the core-package contract and kit-lease contract that are already consumed by host/UI clients, while keeping the target HTTP server, agent, runtime, input, cache, update admission, and diagnostic manager implementation-specific. Add structural checks that make the intended dependency direction executable before deciding whether the target implementation becomes a nested module or a repository.

**Tech Stack:** Go modules, Go tests/vet, shell structural tests, FES parent pin/build checks, GitHub Actions.

**Spec:** `docs/fes-structure.md` (reviewable sequence and build/contract rules) and the selected FogCast `sources/FogCast/docs/ARCHITECTURE.md` (process ownership, source ownership, and target-agent boundaries).

## Global Constraints

- The host owns catalog, UI, user intent, and host-side media; the target agent owns target HTTP, cache, transient MGLs, and launch; the runtime owns physical transitions.
- Preserve all existing HTTP paths, JSON names, status/error semantics, kit-lease admission behavior, and runtime lifecycle behavior.
- `appliance/` remains the shared nested Go module already merged in FogCast #238 and pinned by FES #52.
- Public contract packages must not import target-only implementation packages or FogCast `internal/` packages.
- `targetclient`, host packages, and UI packages may consume shared contracts but must not import target agent, HTTP server, runtime, input, cache, update-admission, or diagnostic implementation packages.
- Do not create a second coordinator, alter the target protocol, or split repositories in this slice.
- Run root-module tests and vet plus the nested `appliance/` module checks; Go root-wide commands do not traverse that nested module automatically.
- This is a host/software boundary change. No SD-card write, FPGA programming, target reboot, or hardware acceptance is part of this slice.

## Current inventory

The measured FogCast closure at the selected post-extraction revision is:

| Entry point | Local FogCast package closure |
| --- | ---: |
| `cmd/mister-agent` | 26 |
| `internal/httpapi` | 20 |
| `internal/applianceupdate` | 13 |
| `internal/misterruntime` | 8 |
| `internal/input` | 6 |
| `targetclient` | 9 |
| `ui/kitlauncher` | 25 |
| `host` | 11 |

The important current edges are:

- `protocol` imports `internal/corepackage` for descriptor validation and response types.
- `targetclient`, host API, ten-foot UI, and kit launcher consume shared contract concepts, but `targetclient` and UI also reach `internal/core`, `internal/corepackage`, and `internal/kitlease`.
- `internal/applianceupdate` imports `internal/agent`, so update admission is currently target-agent implementation, not an independently movable service.
- `internal/httpapi` joins cache, core-package, input, kit-lease, update, and protocol behavior in the target server.
- `internal/kitlease.Manager` owns target cleanup/event admission, while host/UI clients only need lease wire types and pure ownership predicates.
- `internal/mister`, `internal/misterruntime`, and `internal/input` have no host/UI production imports and are plausible target-runtime candidates after the shared contracts are independent.

## Scope decision

Do not extract `cmd/mister-agent` or `internal/httpapi` yet. Their 26-package closure is still intentionally coupled around runtime, input, cache, update admission, and diagnostic behavior. First make the contracts they share with host/UI explicit; then re-measure the graph and decide between a nested target module and a repository split.

---

### Task 1: Add a failing target-contract boundary guard

**Files:**
- Create: `sources/FogCast/scripts/tests/target-contract-boundary_test.sh`
- Modify: `sources/FogCast/Makefile:28-53`
- Test: `sources/FogCast/scripts/tests/target-contract-boundary_test.sh`

**Interfaces:**
- Produces a non-zero structural check while `protocol` still imports `internal/corepackage` and host/UI still consume `internal/kitlease`.
- Later tasks make the same check green without changing the wire protocol.

- [ ] **Step 1: Write the failing structural assertions.**

The script must assert that `protocol/`, `corepackage/`, and the public `kitlease/` directory contain no imports matching `github.com/DeanoC/FogCast/internal/`. It must also reject target implementation imports from `targetclient/`, `host/`, `ui/`, and `catalog/` for the target-only paths `internal/agent`, `internal/httpapi`, `internal/mister`, `internal/misterruntime`, `internal/input`, `internal/targetcache`, `internal/applianceupdate`, and `internal/flightdiag`.

- [ ] **Step 2: Wire it into the component test path.**

Add `test-target-contract-boundary` to `.PHONY` and invoke it from the existing `test` target after `test-target-boundary`. Keep the existing target/UI boundary test unchanged.

- [ ] **Step 3: Run the new check and confirm RED.**

Run:

```sh
cd sources/FogCast
sh scripts/tests/target-contract-boundary_test.sh
```

Expected: failure identifying the current `protocol -> internal/corepackage` edge and the current `internal/kitlease` consumers.

- [ ] **Step 4: Commit the red guard only.**

```sh
git add scripts/tests/target-contract-boundary_test.sh Makefile
git commit -m "test: define target contract boundary"
```

---

### Task 2: Publish the core-package contract

**Files:**
- Move: `sources/FogCast/internal/corepackage/package.go` to `sources/FogCast/corepackage/package.go`
- Move: `sources/FogCast/internal/corepackage/store.go` to `sources/FogCast/corepackage/store.go`
- Move: `sources/FogCast/internal/corepackage/*_test.go` and `testdata/` with the package
- Modify: `sources/FogCast/protocol/types.go`
- Modify: `sources/FogCast/protocol/core_data.go`
- Modify: `sources/FogCast/internal/httpapi/core_data.go`
- Modify: `sources/FogCast/internal/httpapi/development.go`
- Modify: `sources/FogCast/targetclient/core_package_client.go`
- Modify: `sources/FogCast/targetclient/core_data_client.go`
- Modify: remaining `internal/corepackage` imports found by `rg`

**Interfaces:**
- Produces public package path `github.com/DeanoC/FogCast/corepackage` with the existing `Descriptor`, `ValidateDescriptor`, `MaxArchiveSize`, `Stage`, `Adopt`, and publication APIs unchanged.
- `protocol.CoreInspection.Descriptor` and `protocol.CoreDataInspection.Descriptor` change only their import path; their JSON representation and validation behavior remain byte-compatible.
- No target package gains a dependency on host or UI code.

- [ ] **Step 1: Add the red import-path assertion.**

Extend the guard from Task 1 to reject `internal/corepackage` in all production Go files after the move, while allowing the deleted path to be absent.

- [ ] **Step 2: Move the package with Git-aware renames.**

Use `git mv` for the implementation, tests, fixtures, and testdata. Change only the package import paths and the package declaration location; do not alter descriptor validation or archive bytes.

- [ ] **Step 3: Run focused package tests.**

Run:

```sh
go test ./corepackage ./protocol ./targetclient ./internal/httpapi ./internal/hostapi ./internal/fogcastcli
```

Expected: PASS with the existing JSON, descriptor, staging, HTTP, and client validation tests.

- [ ] **Step 4: Run the boundary guard and inspect imports.**

Run:

```sh
sh scripts/tests/target-contract-boundary_test.sh
rg -n "github.com/DeanoC/FogCast/internal/corepackage" --glob "*.go" .
```

Expected: the guard passes its core-package assertion and the import search returns no production or test references.

- [ ] **Step 5: Commit the contract move.**

```sh
git add corepackage internal/corepackage protocol internal/httpapi targetclient internal/hostapi internal/fogcastcli scripts/tests/target-contract-boundary_test.sh
git commit -m "refactor: publish FogCast core package contract"
```

---

### Task 3: Split the public kit-lease contract from the target manager

**Files:**
- Create: `sources/FogCast/kitlease/contract.go`
- Create: `sources/FogCast/kitlease/contract_test.go`
- Modify: `sources/FogCast/internal/kitlease/manager.go`
- Modify: `sources/FogCast/internal/kitlease/hid.go`
- Modify: `sources/FogCast/internal/kitlease/hostless.go`
- Modify: `sources/FogCast/internal/httpapi/kit_lease.go`
- Modify: `sources/FogCast/internal/httpapi/content.go`
- Modify: `sources/FogCast/internal/httpapi/diagnostic.go`
- Modify: `sources/FogCast/cmd/mister-agent/main.go`
- Modify: `sources/FogCast/targetclient/kit_lease.go`
- Modify: `sources/FogCast/ui/kitlauncher/run.go`
- Modify: `sources/FogCast/ui/kitlauncher/hostless.go`
- Modify: `sources/FogCast/ui/tenfoot/app.go`
- Modify: `sources/FogCast/internal/hostapi/session.go`
- Test: existing `internal/kitlease`, HTTP, targetclient, kitlauncher, hostapi, and UI tests

**Interfaces:**
- Public `kitlease` owns `Status`, `ClaimRequest`, `TakeoverRequest`, `Grant`, `HostlessOwner`, `HostlessPurpose`, `ForeignHID`, `HostlessSession`, and `ForeignSession`.
- `internal/kitlease` owns `Manager`, `Option`, `WithEventSink`, cleanup callbacks, timers, tokens, takeover state, and `flightdiag` integration.
- Internal aliases may preserve target-side call-site types, but host/UI/targetclient imports must resolve to public `github.com/DeanoC/FogCast/kitlease`.
- Lease JSON names, error values, owner prefixes, and state transitions remain unchanged.

- [ ] **Step 1: Add contract-package tests before moving implementation.**

Copy the pure predicate cases from `internal/kitlease/hid_test.go` and `hostless_test.go` into `kitlease/contract_test.go`. Add JSON round-trip coverage for `Status`, `ClaimRequest`, `TakeoverRequest`, and `Grant`.

- [ ] **Step 2: Run the focused contract test and confirm the expected RED state.**

Run:

```sh
go test ./kitlease
```

Expected: failure because the public package does not yet exist.

- [ ] **Step 3: Move pure contract definitions and retain the target manager internally.**

Move only the shared types/constants/predicates into `kitlease`. Keep cleanup, event sink, timer, token, takeover, and recovery logic in `internal/kitlease`. Update target code to use internal manager types and host/UI/targetclient code to use public contract types.

- [ ] **Step 4: Run the lease and boundary tests.**

Run:

```sh
go test ./kitlease ./internal/kitlease ./internal/httpapi ./targetclient ./ui/kitlauncher ./ui/tenfoot ./internal/hostapi
sh scripts/tests/target-contract-boundary_test.sh
```

Expected: PASS, with no public contract package importing `internal/` and no host/UI package importing the target manager.

- [ ] **Step 5: Commit the lease seam.**

```sh
git add kitlease internal/kitlease internal/httpapi cmd/mister-agent targetclient ui internal/hostapi scripts/tests/target-contract-boundary_test.sh
git commit -m "refactor: publish kit lease contract"
```

---

### Task 4: Prove the post-seam dependency direction and update architecture

**Files:**
- Modify: `sources/FogCast/scripts/tests/target-boundary_test.sh`
- Modify: `sources/FogCast/Makefile`
- Modify: `sources/FogCast/docs/ARCHITECTURE.md`
- Modify: `docs/fes-structure.md` only if the selected contract paths or ownership wording changes
- Test: `sources/FogCast/scripts/tests/target-boundary_test.sh`, `sources/FogCast/scripts/tests/target-contract-boundary_test.sh`

**Interfaces:**
- The structural checks document the stable direction: host/UI -> public contracts/targetclient; target executable -> target implementation plus public contracts; runtime remains the physical owner.
- The architecture document records that a future target module/repository split is deferred until the measured closure excludes host/UI-only packages and the public contracts no longer depend on `internal/`.

- [ ] **Step 1: Extend the existing target-boundary test.**

Keep its current checks for `targetclient` and kit launcher. Add checks for the new public `corepackage` and `kitlease` paths, and reject any direct host/UI import from target implementation directories.

- [ ] **Step 2: Run package graph evidence after the moves.**

Run:

```sh
for package in ./cmd/mister-agent ./internal/httpapi ./targetclient ./ui/kitlauncher ./host; do
  printf '%-24s ' "$package"
  env GOWORK=off GOPROXY=off go list -deps -f '{{.ImportPath}}' "$package" |
    rg '^github.com/DeanoC/FogCast/' | sort -u | wc -l
done
```

Record one local-package closure count for each entry point; do not use one
combined `go list` invocation because that cannot attribute a closure to its
root. Confirm that target implementation packages do not import `host`, `ui/`,
or `targetclient`.

- [ ] **Step 3: Update current architecture prose.**

Document the public contract packages and state that `internal/agent`, `internal/httpapi`, `internal/mister`, `internal/misterruntime`, `internal/input`, `internal/targetcache`, `internal/applianceupdate`, and `internal/flightdiag` remain target-owned implementation. Do not describe the future module/repository split as completed.

- [ ] **Step 4: Run component verification.**

Run:

```sh
make test
make vet
git diff --check
```

Expected: all component tests, structural checks, builds, and vet pass.

- [ ] **Step 5: Commit the boundary proof and documentation.**

```sh
git add scripts/tests/target-boundary_test.sh scripts/tests/target-contract-boundary_test.sh Makefile docs/ARCHITECTURE.md
git commit -m "docs: record target service contract boundary"
```

---

### Task 5: Integrate the reviewed FogCast seam into FES

**Files:**
- Modify: FES `sources/FogCast` gitlink only
- Modify: FES `scripts/consistency.py` and `tests/test_consistency.py` for the moved core-package fixture tree
- Modify: FES `docs/core-packages.md` to point at the public FogCast package path
- Test: FES `make check`, `make host`, focused parent tests, and nested-module checks

**Interfaces:**
- FES selects one reviewed FogCast commit; no uncommitted component tree is used by parent builds.
- Parent build receipts continue to bind the selected FogCast revision and shared `appliance` module identity.
- The parent fixture-consistency map follows the reviewed package move: `sources/mister-packages/testdata/core-bundle-v2` is copied to `sources/FogCast/corepackage/testdata/core-bundle-v2`.
- The current core-package guide names `sources/FogCast/corepackage` rather than the deleted `sources/FogCast/internal/corepackage` path.

- [ ] **Step 1: Complete the FogCast review first.**

Run the component tests, request Codex review, and resolve P1/P2 findings before changing the FES gitlink.

- [ ] **Step 2: Select the merged FogCast commit in a fresh FES worktree.**

Update only `sources/FogCast` to the reviewed commit. In the same parent
change, update the `COPIED_TREES` destination in
`scripts/consistency.py` and its test fixture expectations from
`sources/FogCast/internal/corepackage/testdata/core-bundle-v2` to
`sources/FogCast/corepackage/testdata/core-bundle-v2`. Update the
component-entrypoint row in `docs/core-packages.md` to the public package path.
Confirm no generated outputs, source pins, or unrelated submodules move.

- [ ] **Step 3: Run the parent gates.**

Run:

```sh
make check
make host
python3 -m unittest tests.test_appliance tests.test_platform tests.test_appliance_media tests.test_core_build -v
make platform-test
```

- [ ] **Step 4: Inspect provenance and dependency direction.**

Confirm host receipts contain the selected FogCast revision, platform receipts
remain path-free, the consistency test no longer references the deleted
`internal/corepackage` fixture path, the guide has no stale component path, and
the parent source tree is clean.

- [ ] **Step 5: Open the FES integration PR with matched evidence.**

Include the FogCast source commit, the fixture-map and guide updates, parent
checks, nested-module checks, and the explicit statement that no hardware
acceptance is claimed.

---

### Completion boundary

This plan is complete when the shared contract packages are independent of FogCast `internal/`, target implementation packages remain free of host/UI imports, component and parent CI are green, and the reviewed FES pin is ready to merge. Only then should a separate design decide whether the target implementation becomes a nested module or a separate repository.
