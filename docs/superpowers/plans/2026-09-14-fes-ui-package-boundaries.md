# FES-first UI Package Boundaries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move FogCast's UI packages under an explicit `ui/` namespace and pin the reviewed component revision in FES without changing runtime behavior.

**Architecture:** Keep FogCast as one Go module for this slice, but make UI ownership explicit through `ui/tenfoot` and `ui/kitlauncher`. The FES parent continues to select the FogCast revision and own image integration; no repository split or API redesign is included.

**Tech Stack:** Go 1.26.0/toolchain 1.26.5, shell regression checks, FES Python/unittest parent checks, Git submodules.

**Spec:** `docs/superpowers/specs/2026-09-14-fes-ui-package-boundaries-design.md`

## Global Constraints

- Work only in the isolated FES parent worktree `/home/deano/fes/out/dev/fes-ui-boundaries` and the separate FogCast component worktree created beneath it.
- Keep the canonical `/home/deano/fes` worktree and unrelated user worktrees untouched.
- Preserve Go package names, executable names, command flags, routes, configuration keys, sockets, image integration, and target lifecycle behavior.
- Do not run or claim physical hardware acceptance; that is a separate user-at-machine gate.
- Use a component PR followed by a parent FES pin/docs PR. Do not merge either PR in this overnight pass.

## Task 1: Record the boundary contract and add a failing structural check

**Files:** `sources/FogCast/scripts/tests/ui-boundary_test.sh`, `sources/FogCast/Makefile`

- [x] Create `ui-boundary_test.sh` that requires `ui/tenfoot` and `ui/kitlauncher`, rejects `host/tenfoot` and `kitlauncher`, and rejects old Go import paths.
- [x] Run the new script before moving the directories and capture the expected red result.
- [x] Add a `test-ui-boundary` target and invoke it from `test-ui` so the invariant is part of the normal component test path.

## Task 2: Move the UI packages with no behavior change

**Files:** `sources/FogCast/ui/tenfoot/**`, `sources/FogCast/ui/kitlauncher/**`, affected Go importers

- [x] Create a FogCast worktree from the pinned component revision `b3df1d7b938e383eabc413fafacff9234b06e788`.
- [x] Move `host/tenfoot` to `ui/tenfoot` and `kitlauncher` to `ui/kitlauncher` with Git-aware renames.
- [x] Update repository-qualified Go imports and any source-local references to the new paths; retain package declarations and public API names.
- [x] Run `gofmt` on changed Go files.
- [x] Run the structural test and focused UI/command tests:

  ```sh
  go test ./ui/tenfoot/... ./ui/kitlauncher/... ./cmd/fogcast-tenfoot ./cmd/fogcast-kit
  ```

- [x] Run the complete component verification:

  ```sh
  go test ./...
  go vet ./...
  make build-fogcast build-fogcast-api build-fogcast-kit build-agent
  ```

## Task 3: Update active documentation and enforce the new vocabulary

**Files:** `sources/FogCast/README.md`, `sources/FogCast/docs/ARCHITECTURE.md`, relevant scripts/docs

- [x] Update active source paths from `host/tenfoot` to `ui/tenfoot` and from the root `kitlauncher` path to `ui/kitlauncher`.
- [x] Keep prose package names such as `tenfoot` and `kitlauncher` where they describe Go packages rather than filesystem paths.
- [x] Add a concise boundary note explaining that UI packages consume host contracts but do not own target handlers, runtime lifecycle, image assembly, or FPGA builds.
- [x] Scan active component source/docs for legacy paths and confirm only intentional historical references remain.

## Task 4: Verify, review, and publish the FogCast component PR

- [x] Re-run the component structural test, focused tests, full tests, vet, and build targets after documentation changes.
- [x] Run `git diff --check` and inspect the complete diff for accidental behavior changes.
- [x] Commit the component change as `refactor: make FogCast UI package ownership explicit` (`9425041`).
- [x] Push `refactor/fes-ui-package-boundaries` and open [FogCast PR #235](https://github.com/DeanoC/FogCast/pull/235) against the pinned base.
- [x] Request the configured Codex reviewer with `@codex review`; resolve any P1/P2 comments before merge.

## Task 5: Pin the reviewed component in FES

**Files:** FES gitlink and `docs/component-boundaries.md`

- [x] After the component commit is available, update only `sources/FogCast` in the isolated FES parent worktree to that commit.
- [x] Update the parent boundary document to name `sources/FogCast/ui/tenfoot` and `sources/FogCast/ui/kitlauncher` as the application UI locations.
- [x] Confirm the parent diff has no unrelated submodule movement or generated artifacts.
- [ ] Run the parent checks:

  ```sh
  make test
  make check
  git diff --check
  ```

- [ ] Commit as `build: select FogCast UI package boundaries`, push `refactor/fes-ui-boundaries`, and open the FES PR.
- [ ] Request Codex review and leave the PR ready to merge, with physical target/image acceptance explicitly recorded as pending.

## Task 6: Queue the next architectural slices

- [ ] Capture follow-on issues for host-service ownership, target-agent ownership, appliance/package integration, and longer-term repo consolidation/splitting.
- [ ] Do not start those moves in this slice; use the merged boundary as the stable baseline for the next PR.
