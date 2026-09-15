# FES Boot Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the stable FES boot selector and its Linux-only helpers from FogCast into an FES-owned `platform` Go module while preserving the boot ABI, image layout, update admission, and source-bound bootstrap evidence.

**Architecture:** FogCast keeps the public `github.com/DeanoC/FogCast/appliance` schema/store module and target update code. FES `platform` owns `fes-boot`, fallback policy, and Linux mount/watchdog mechanisms. FES builds the platform module against the selected FogCast appliance module through a temporary `go.work`; no checked-in developer-path replacement is used.

**Tech Stack:** Go 1.26.5, Python 3, FES appliance/reproducibility scripts, Git submodules, GitHub Actions.

**Spec:** `docs/fes-structure.md`, especially the source-ownership table, build/contract rules, and reviewable sequence.

## Global Constraints

- Keep manifest bytes, closed schema, boot ABI, filesystem layout, update admission and HTTP contracts unchanged.
- Keep one appliance schema/store implementation in FogCast; FES consumes the selected immutable module source.
- No developer worktree path may be persisted in published build inputs or bootstrap evidence.
- Boot receipts must identify FES platform source, selected appliance-module content, Go toolchain and build flags.
- A platform/module/lock change must invalidate bootstrap output; unchanged inputs must remain reusable.
- FES image assembly builds boot from `platform/`; FogCast no longer owns `cmd/fes-boot` or its private boot packages.
- No physical deployment or hardware acceptance is part of this software boundary migration.

---

### Task 1: Remove stable boot ownership from FogCast

**Files:**
- Delete `sources/FogCast/cmd/fes-boot/main_linux.go` in the FogCast component worktree.
- Delete all source and test files under `sources/FogCast/internal/applianceboot/` and `sources/FogCast/internal/bootlinux/`.
- Modify FogCast `Makefile` to remove the `build-fes-boot` target and its phony declaration.
- Modify FogCast Go CI/docs to remove stale boot-package build/test references while retaining appliance schema/store and target-update coverage.

**Interfaces:**
- The public `github.com/DeanoC/FogCast/appliance` and `github.com/DeanoC/FogCast/appliance/store` import paths remain unchanged.
- FogCast `cmd/mister-agent`, `cmd/fes-update`, target update APIs, manifest bytes and `fes-bootstrap-v1` remain unchanged.
- FES Task 2 copies the boot implementation into its own module before this task's deleted files are considered complete.

- [ ] Record the clean FogCast base and run `go test ./...`, `go vet ./...`, and the existing focused boot-package tests before deletion.
- [ ] Remove only the stable boot command/private packages and direct build references; do not remove `appliance/store` or target update code.
- [ ] Run `go test ./...`, `go vet ./...`, and the relevant FogCast build targets after deletion.
- [ ] Confirm `rg -n 'cmd/fes-boot|internal/applianceboot|internal/bootlinux|build-fes-boot' Makefile .github README.md docs cmd internal ui targetclient` has no stale ownership references.
- [ ] Commit the component change as `refactor: move stable boot ownership to FES`.

### Task 2: Add the FES platform module and boot implementation

**Files:**
- Create `platform/go.mod` and `platform/go.sum`.
- Create `platform/cmd/fes-boot/main_linux.go`.
- Create `platform/internal/applianceboot/boot.go` and its tests.
- Create `platform/internal/bootlinux/` with the Linux implementation and tests currently owned by FogCast.
- Add a focused FES platform test/build entry point if needed by Task 3.

**Interfaces:**
- Module path: `github.com/DeanoC/fes/platform`.
- Go floor: `go 1.26.5`.
- Required shared module: `github.com/DeanoC/FogCast/appliance v0.0.0`, supplied locally by the temporary workspace from the selected FogCast checkout.
- Required Linux dependency: `golang.org/x/sys v0.46.0`.
- Internal imports use `github.com/DeanoC/fes/platform/internal/applianceboot` and `github.com/DeanoC/fes/platform/internal/bootlinux`.
- Runtime behavior, constants, boot ticket bytes, watchdog arguments and fallback ordering remain byte/behavior compatible with the removed FogCast implementation.

- [ ] Add the module files and run the platform tests in a temporary `go.work` containing only `platform` and the selected FogCast `appliance` module; verify the baseline fails until the copied implementation is present.
- [ ] Copy the boot command, policy and Linux helper implementation with only module-path ownership changes; preserve package APIs and tests.
- [ ] Run `go test ./...`, `go vet ./...`, and `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' ./cmd/fes-boot` through the temporary workspace.
- [ ] Confirm the resulting binary is static ARM EABI5 and that no platform production package imports FogCast host, agent, UI, runtime or target-service packages.
- [ ] Commit the platform module as `feat: own stable appliance boot in FES platform`.

### Task 3: Route FES bootstrap assembly through platform and bind provenance

**Files:**
- Create or modify `scripts/platform.py` for temporary Go workspace creation, selected-module identity, and static ARM platform build.
- Modify `scripts/appliance.py` and `scripts/appliance_media.py` to use the shared platform builder instead of `FogCast/cmd/fes-boot`.
- Modify `tests/test_appliance.py` and add focused platform/build-input tests.
- Modify `Makefile`, `docs/project-map.md`, `docs/image-assembly.md`, and current appliance documentation to describe the FES platform source.

**Interfaces:**
- The platform builder accepts the FES root, selected FogCast checkout, output path, and existing build environment; it returns the static-binary digest plus a canonical input record.
- The input record contains the FES platform tree identity, FogCast appliance-module tree identity, selected FogCast revision, Go version/toolchain identity, and exact build flags.
- `assemble_bootstrap` records that input record in evidence and includes it in the immutable bootstrap identity.
- Retained bootstrap verification checks the selected FES revision's `platform/` tree and rejects a stale or mismatched platform/module identity.
- Existing release manifest fields, `evidence.json` format compatibility, media layout, and hardware classification remain unchanged except for the additive boot-source provenance fields.

- [ ] Write failing unit tests for temporary workspace input selection, appliance-module identity mismatch, build-flag/toolchain binding, and bootstrap identity invalidation when platform source changes.
- [ ] Implement the temporary `go.work` build without storing an absolute checkout path in receipts, manifests, or published artifacts.
- [ ] Replace both bootstrap build call sites and pass the canonical platform build record through assembly and media reconstruction.
- [ ] Run `python3 -m unittest tests.test_appliance -v`, relevant media tests, `git diff --check`, and focused platform Go tests.
- [ ] Run the static ARM build from the selected clean inputs and inspect evidence to confirm platform and appliance-module identities are present.
- [ ] Commit as `build: bind appliance bootstrap to FES platform sources`.

### Task 4: Integrate and verify the component handoff

**Files:**
- Modify the FES parent gitlink `sources/FogCast` only after the FogCast ownership-removal commit is reviewed and published.
- Modify FES consistency/build docs or pins only where required to select the matching FogCast revision.
- Add parent tests only for the new cross-repository source/provenance contract.

**Interfaces:**
- The selected FogCast revision must contain the public appliance module and no stable boot implementation.
- `make check`, `make host`, and the focused appliance/platform checks must pass against one matching component selection.
- Full cold image/release and physical target acceptance remain separate gates.

- [ ] Confirm the FogCast component commit is available from its remote and select it in an isolated FES parent worktree.
- [ ] Run `make check`, `make host`, focused appliance/platform tests, and the static ARM boot build.
- [ ] Inspect generated receipts for selected FES/FogCast/module identities and absence of absolute worktree paths.
- [ ] Record hardware status as host-only/software-boundary verification.
- [ ] Commit the parent selection as `build: select FES-owned appliance boot`.
