# Core package library implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development, with component integration reviews.

**Goal:** Install versioned FPGA packages and launch explicitly selected ROM-less entries through the normal library.

**Architecture:** Host-owned immutable archives and catalog associations; read-only target compatibility through existing runtime inspection; existing package session transitions reused by normal library launch.

**Tech Stack:** Existing Go, SQLite catalog, format-2 parser and native runtime protocol 2.

**Spec:** `docs/superpowers/specs/2026-09-09-core-package-library-design.md`.

## Global constraints

- No UI layout/theme edits, new ABI/compiler/FPGA design or inferred cartridge media.
- Preserve exact package identity, existing admission/recovery and input ownership.
- Use isolated FogCast worker `/home/deano/fes/out/dev/core-package-library/FogCast`.
- Root owns `fogcast/`, `internal/hostapi/`, CLI wiring, docs, FES integration and kit.
- Workers own only their named directories; agree any shared-file change first.

## Task 1: Immutable store and catalog associations

Files: new `internal/corepackage/store.go` and tests; new `catalog/core_entries.go`
and tests; catalog schema/platform/model/store changes required by that feature.

Interfaces: store accepts bounded archive bytes and returns validated
`corepackage.Inspection`; opens package by ID as validated same-read archive
bytes. Catalog exposes creation, selection with expected current ID, selection
lookup and reference listing. `SourceKindCorePackage` and browse-only platform
`fpga` identify reserved entries. Worker hands exact Go signatures to integrator
before service integration.

- [x] Add tests using real archive fixtures for idempotent/concurrent imports,
  corrupt/truncated archives, symlinks, cancellation, reopening and selection.
- [x] Run focused tests and capture expected failure.
- [x] Implement atomic store publication and authenticated reads.
- [x] Add schema migration, reserved logical library, entry association and CAS.
  Check same core ID, stable game identity, unique group key and scan protection.
- [x] Run `go test ./internal/corepackage ./catalog` and affected race tests.
- [x] Review isolated diff and document exact interfaces.

## Task 2: Read-only compatibility transport

Files: new inspection helpers/tests under `internal/misterruntime/`,
`internal/agent/`, `internal/httpapi/`, `host/`; narrowly scoped shared protocol
response type. This worker must not edit root-owned hostapi/service/CLI files.

Interfaces: authenticated target read-only archive inspection, no lease mutation,
no input barrier/programming. Host client method returns exact inspected package
identity/descriptor and compatible/incompatible outcome; transport failures are
errors. Agree exported signatures with integrator before integration.

- [x] Add regression tests asserting zero LoadCore/Stop/barrier calls, including
  active sessions, invalid archives, unsupported ABI/backend and cancellation.
- [x] Run focused tests and observe expected failure.
- [x] Factor stage/inspect/cleanup from existing native admission, preserving
  identity verification and cleanup ownership.
- [x] Expose bounded authenticated target endpoint and read-only host client.
- [x] Run affected tests, race tests and vet; review exact diff.

## Task 3: Host inventory, selection and normal launch

Files: new `fogcast/core_packages.go`, tests; `fogcast/config.go`,
`fogcast/service.go`; new `internal/hostapi/core_packages.go`, tests;
`internal/hostapi/session.go`, `server.go`; existing CLI command files and tests.

- [x] Test actual store/catalog import and entry APIs. Import must not contact
  hardware; selection requires compatibility and expected-current CAS.
- [x] Wire persistent store through `Paths`, inventory and entry operations.
- [x] Add normal library package launch tests proving prior input/media survive
  pre-mutation rejection and active IDs reflect the loaded selection.
- [x] Factor package service/coordinator transition helpers to avoid nested locks.
  Resolve package kind before ordinary coordinator input detachment.
- [x] Project host game association only for an explicit library launch and its
  exact observed package; after restart leave association unknown/stoppable.
- [x] Add CLI install/list/check/create/select and API contract examples.
- [x] Run affected suites, race tests, vet and complete independent review.

## Task 4: Integration and acceptance

- [x] Update canonical component architecture and FES usage guide.
- [x] Commit reviewed component change and select it in FES; run consistency and
  host build, preserving existing component pins and compiler caches.
- [x] Lease kit and use isolated host for bounded diagnostics: import original
  Pong, create entry, launch via ordinary session route, observe input and Stop.
- [x] Import metadata-version variant with same RBF/build ID; inspect and select,
  launch exact new package, select old package and relaunch.
- [x] Verify incompatible selection and launch preserve prior selection/input;
  restart host and verify inventory/selection persistence.
- [x] Build/verify stabilized exact image when target inputs change; distinguish
  diagnostic and exact-artifact hardware evidence. Restore pairing/free lease.
- Publish reviewable commits/PRs within existing user authorization; no merge
  without the user requesting it. Final handoff lists evidence and limitations.
