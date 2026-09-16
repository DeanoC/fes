# Package-only acceptance implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Exercise one immutable sealed package through existing host APIs without rebuilding or deploying a system image.

**Architecture:** Add a separate FES operator script beside the existing three-core acceptance runner. Use existing package import, compatibility, catalog compare-and-swap selection and persistent session APIs; keep image acceptance unchanged. Tests use local HTTP fixtures, never a kit.

**Tech Stack:** Python 3.11 standard library and unittest; existing FogCast HTTP contracts.

**Spec:** User-approved package-only workflow: seal artifact, host install, compatibility check, explicit selection, launch/test/Stop. Freeze package and platform identities; cover failed uploads, insufficient space, incompatibility, stale selection and active-session preservation.

## Global constraints

- No kit access, deployment, HIL, resizing, restore, reboot, image builds or FPGA builds during implementation.
- No component changes, pin updates, commits, pushes or PRs. Preserve the existing staged FogCast pin.
- No durable target store, new protocol, resumable upload or automatic software updates.
- Reuse /home/deano/fes/out/dev/library-client/fes; implementation owns only new package_acceptance script/tests. Coordinator owns docs/Makefile. Reviewer is read-only.
- Runtime invocation is explicitly hardware-mutating and must require operator opt-in; tests only use loopback fixtures.

### Task 1: Package-only runner and host tests

**Files:** Create scripts/package_acceptance.py and tests/test_package_acceptance.py. Read scripts/target_acceptance.py and the pinned FogCast API handlers for exact contracts.

**Interfaces:** CLI requires archive, exact expected package/core IDs, expected host/agent/runtime revisions, an existing game ID plus expected selected package (or explicit new-entry title), and an execution opt-in. A JSON receipt records frozen identities and success only after owned-session Stop. Core input/media diagnostics are optional explicit recipes, never inferred from a new core name.

- [ ] Write failing loopback HTTP tests. Call the new main entry with a temporary archive and explicit identities; assert exit status and fixture-observed durable selection/session state. Success must import exact bytes, check compatible, perform expected selection, launch exact package, Stop its own session and emit receipt.
- [ ] Run `python3 -m unittest discover -s tests -p test_package_acceptance.py -v`; record expected missing-runner failures before implementation.
- [ ] Implement bounded artifact read and existing HTTP contract orchestration. Refuse active/non-idle session before any mutation; require exact platform identity. Abort upload failure, changed returned identity, incompatible descriptor, stale selection and revision changes before launch. Use CAS for existing entries. Never stop a pre-existing or foreign session; ambiguous launch transport failure requires inspection, not blind Stop.
- [ ] Add cases for HTTP ENOSPC, interrupted upload, incompatible package, stale selection, active session, wrong returned package and platform change. Test behavior through the script, not copied implementation logic. No selection/launch after import failure; no selection after incompatibility; no Stop on unrelated active session.
- [ ] Run new and existing acceptance tests. Hand back uncommitted diff and exact red/green commands.

### Task 2: Documentation, entrypoint and independent review

**Files:** Modify Makefile and docs/development.md; add docs/package-acceptance.md.

**Interfaces:** `make package-acceptance PACKAGE_ACCEPTANCE_ARGS='...'` invokes only the runner, with no build prerequisites. Documentation distinguishes package-only diagnostic acceptance from image/HIL acceptance and explains persistent explicit selection, operator exclusivity and manual rollback.

- [ ] Validate the exact CLI with `python3 scripts/package_acceptance.py --help` before documenting commands.
- [ ] Add Makefile entry and operator guide including no automatic image update and safe retry boundaries.
- [ ] Independent reviewer checks API fidelity, freeze checks, failure preservation, session ownership and receipt truthfulness. Fix actionable findings with regression tests first.
- [ ] Run `make test`, `make check`, and `git diff --check`; verify staged pin unchanged. No live acceptance command.
- [ ] Return host-only results and remaining hardware gate, leaving the diff uncommitted.

## Execution record

- Implemented in the existing isolated parent worktree, base `89f99e36d2c9dcab9f01f7ddea0aad50ce525f6e`; no new worktree, branch, component edit or pin change.
- Herdr feature-worker owned the runner/tests; coordinator owned Makefile/docs; reviewer independently reviewed contracts and code. rtl-worker verified the existing real store/catalog tests.
- Ruling: this first lane is lifecycle-only. No media, controller or HDMI behavior is inferred for unfamiliar cores; receipts explicitly record an empty diagnostics list.
- Ruling: HTTP fixtures validate orchestration, not the package parser or real filesystem failures. The existing real Go `corepackage` and `catalog` tests passed separately. Crash/power-loss and real ENOSPC injection remain outside this slice.
- Focused TDD progressed from missing-runner failures through four safety regressions and five interruption/response-validation regressions to 25 passing package tests. Existing five three-core acceptance tests passed unchanged.
- `make test` completed successfully: 296 parent tests (36 delegated-container cases skipped at the outer level), platform tests and image shell/fixture tests. `make check`, CLI help and `git diff --check` passed. Final test-only fixture tightening was followed by another focused run.
- The implementation phase made no live host or target requests, kit operations, FPGA builds, system-image deployment, commits, pushes or PRs. The pre-existing staged FogCast gitlink `b5a1eb6` to `eccc042` was preserved and remains held.

## Subsequently authorized hardware and PR phase

- The user separately authorized hardware acceptance followed by a PR, and then a metadata producer fix discovered by the real preflight.
- Strict identity validation exposed bare `verify-package ... verified` lines in the existing diagnostic image's build-inputs record. The image producer now sends its two validation passes to stderr, retaining canonical `--print-inputs` stdout and all verification failures. The noisy-selector regression failed before this two-line fix and passed afterward; the independent reviewer approved it.
- A disposable copy of the existing diagnostic image had only those six stray record lines removed. Original image and binaries were preserved; this was not a reproducible release rebuild. Fresh boot health now exposes the expected runtime revision and corrected record digest.
- After the producer fix, `make test` passed 296 parent tests (36 outer delegated-container skips), delegated suites and image shell tests; `make check` passed. No toolchain, FPGA, or four-pack factory-image rebuild was needed.
- SG-1000 passed two actual package-only runs with generations 1 and 2, distinct flight IDs, unchanged package/platform/image and clean Stop after each. See the recorded run and evidence boundaries in `docs/package-acceptance.md`. No HDMI/controller, new-entry creation, changed-package CAS or three-core regression is claimed. PR publication was authorized; the pre-existing FogCast pin remains excluded.
