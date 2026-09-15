# Shared Appliance Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the boot selector and target updater one independently buildable appliance schema/store module.

**Architecture:** Keep the release schema at its existing import path, introduce a nested Go module at `FogCast/appliance`, and move the image store into its `store` package. Existing boot and agent commands continue to build in FogCast for this slice.

**Tech Stack:** Go 1.26.5, existing Go tests, GitHub Actions and FES consistency checks.

**Spec:** [FES structure](../../fes-structure.md), shared-module slice only.

## Global constraints

- Keep manifest bytes, closed schema, boot ABI, filesystem layout, update admission and HTTP contracts unchanged.
- Preserve a single store implementation; no forwarding copies or second schemas.
- Keep canonical `sources/` clean and use a separate FogCast worktree.
- No hardware deployment is needed for this module-only slice.

## Task 1: Move the store into the shared module

**Files:** Create `appliance/go.mod`; move `internal/appliance/*.go` to
`appliance/store/`; modify root `go.mod` and all imports of the old store path.
Keep `appliance/release.go` and its tests in place.

**Interfaces:** Existing exported manifest types/functions retain
`github.com/DeanoC/FogCast/appliance`; store types retain their behavior and
package name under `github.com/DeanoC/FogCast/appliance/store`.

- [ ] Record the focused baseline from the selected FogCast revision:

  ```sh
  go test ./appliance ./internal/appliance ./internal/applianceboot ./internal/bootlinux ./internal/applianceupdate ./cmd/fes-boot ./cmd/fes-update
  rg -n 'github.com/DeanoC/FogCast/internal/appliance"' --glob '*.go' .
  ```

- [ ] Create the nested module with the current Go floor:

  ```go
  module github.com/DeanoC/FogCast/appliance

  go 1.26.5
  ```

- [ ] Move all store source/tests with Git, preserve their package declarations,
  and replace only the complete quoted old import path. This excludes the
  separate `internal/applianceboot` and `internal/applianceupdate` packages.
  Add this root module dependency and replacement:

  ```go
  require github.com/DeanoC/FogCast/appliance v0.0.0
  replace github.com/DeanoC/FogCast/appliance => ./appliance
  ```

  The local version is resolved by the checked-in relative replacement for
  this standalone checkout. FES boot extraction must select a published
  immutable module revision before it can consume this module independently.

- [ ] Check the module imports. If moved production code imports any other
  FogCast package, resolve that dependency explicitly before proceeding; the
  shared module may not depend on the enclosing host/agent module.

  ```sh
  cd appliance
  go test ./...
  go list -deps ./...
  ```

- [ ] From FogCast root run existing behavioral tests and cross-build the boot
  command to a temporary output directory:

  ```sh
  go test ./internal/applianceboot ./internal/bootlinux ./internal/applianceupdate ./cmd/fes-boot ./cmd/fes-update
  boot_test_dir=$(mktemp -d)
  CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o "$boot_test_dir/fes-boot" ./cmd/fes-boot
  git diff --check
  ```

## Task 2: Make the new boundary visible to builds and CI

**Files:** FogCast `Makefile`, existing workflows under `.github/workflows/`,
`README.md`, `docs/ARCHITECTURE.md`; FES component pin and ownership guides
when integrating the merged result.

**Interfaces:** Existing `make test` and CI must execute both modules. Consumers
continue to build the same command paths and expose the same runtime APIs.

- [ ] Locate every root test/vet entry point before editing:

  ```sh
  rg -n 'go (test|vet)|make (test|vet)' Makefile .github/workflows
  ```

- [ ] Add explicit nested-module execution wherever root-wide tests/vet are
  intended, using the same flags as that entry point. The required additional
  commands are:

  ```sh
  (cd appliance && go test ./...)
  (cd appliance && go vet ./...)
  ```

- [ ] Update current architecture/build documentation to name the shared
  schema/store module and explain that root Go traversal excludes it.
- [ ] Run `make test`, `make vet`, the normal agent build, and `git diff --check`.
  Inspect CI on the exact PR revision to confirm nested-module tests ran.
- [ ] Have an independent reviewer check import closure, unchanged storage
  behavior and test inclusion. Publish the component result before selecting
  it in FES.
- [ ] Integrate the merged FogCast revision in a parent worktree; run
  `make check`, `make test` and `make host`. Record that this is source/build
  boundary verification and does not replace exact-image hardware evidence.

## Completion boundary

This plan ends with a tested shared module and selected compatible parent pin.
The boot binary still resides in FogCast. FES boot relocation, its immutable
dependency selection and changed provenance require the next plan and PR.
