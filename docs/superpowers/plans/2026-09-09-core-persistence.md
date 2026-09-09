# Core Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Track steps with checkboxes.

**Goal:** Persist standalone Pong paddle speed and best rally across clean exit, restart and compatible package changes.

**Architecture:** Required format-2 interfaces identify a bounded GP data transport and Pong layout. The runtime owns atomic records and physical save/restore; FogCast supplies trusted library context and typed APIs. Existing SNES saves and volatile development loads retain their behavior.

**Tech Stack:** Go, C++14/ARMv7, Verilog/SystemVerilog, Python, existing Buildroot/Mistral toolchain.

**Spec:** `docs/superpowers/specs/2026-09-09-core-persistence-design.md` (approved by user).

## Global Constraints

- Keep format 2, `fes.simple-game` ABI 1.0 and GP transport 1.0.
- Required interfaces: `fes.persistence.words` 1.0 bit 2 and `fes.pong.progress` 1.0 bit 3.
- Opcodes 4 control, 5 read, 6 write, 7 info; control arguments 0 freeze, 1 begin, 2 commit, 3 resume; invalid-state error 4.
- Pong payload is two u16 words: speed 0/1/2 (2/4/6 pixels/frame), best rally saturating at 65535. Default [1,0]; clamp paddle to [0,208].
- Canonical record is exactly the approved `FESDATA1` envelope, maximum 688 bytes. Revision hashes the complete record; missing revision is `absent`.
- Target namespace is SHA-256(core.id), fixed `record.bin`, rooted at `/media/fat/fogcast/core-data/`.
- Never overwrite corrupt/incompatible data with defaults. Never restore bytes cached before outgoing same-core flush.
- Development loads are volatile. Library mode is explicit through host, target and runtime, derived from admitted bytes.
- Keep existing kit lease, target coordination, runtime mutation boundary and save-failure ownership. Resume the frozen driver before input.
- No UI redesign, no new console save transport, no migrations, background saving, synchronization or package format revision.
- Component worker roots: `/home/deano/fes/out/dev/core-persistence/{mister-packages,misteross,libmister-runtime,FogCast}`. Parent: `/home/deano/fes/out/dev/rbf-abi/fes`.
- Only integrator moves parent pins, runs whole-image builds or operates the kit. Workers commit local results, never push or deploy.

## Task 1: Shared wire definitions and durable fixtures

**Files:** modify mister-packages `packages/abi/fes_simple_game.yaml`, `docs/schema.md`, `Makefile`; create `testdata/core-persistence-v1/records.json`, `testdata/core-persistence-v1/exchanges.json`, `internal/pack/core_persistence_test.go` and a deterministic fixture generator if useful.

**Interfaces:** emit existing C++/Go/Verilog consumers with new `FesGpOpcodeDataControl/Read/Write/Info`, `FesGpDataFreeze/Begin/Commit/Resume`, `FesGpErrorInvalidState`, `FesGpPongProgressTag/WordCount`, info indexes and word indexes. Interface names continue existing emitter naming conventions. Fixtures contain record hex, revision, decoded words and invalid cases, independently checked against the spec.

- [x] Write a test checking the real YAML contains exact approved constants and interfaces, leaving base versions unchanged. Run `go test ./internal/pack -run Persistence` and record the missing-constant failure.
- [x] Add constants/interfaces to YAML, then require emitter tests and `go test ./...` to pass.
- [x] Generate/check canonical records using the actual formula:

```python
prefix = (b'FESDATA1' + sha256(core_id.encode()).digest()
          + struct.pack('<HHHH', len(layout), 1, 0, 2)
          + layout.encode() + struct.pack('<HH', speed, best))
record = prefix + sha256(prefix).digest()
revision = sha256(record).hexdigest()
```

- [x] Include defaults, nonzero rally, saturation, corrupt checksum, wrong identity/layout/count/version, truncation and trailing bytes. Wire fixtures cover legal exchange words, incomplete commit, invalid state/index/value and preserved snapshot behavior.
- [x] Document the extension, run `make test` with the repository's Python environment and `git diff --check`, commit; review before consumers are selected.

## Task 2: Runtime data codec, driver and lifecycle

**Files:** modify libmister-runtime `include/libmister-runtime/runtime.h`, `src/runtime.cpp`, `src/native/{hardware,core_driver,fes_gp}.{hpp,cpp}`, local protocol/daemon dispatch, Makefile source lists and `ARCHITECTURE.md`; create focused native core-data codec/storage implementation and tests. Regenerate `src/native/generated/fes_gp.hpp` using Task 1's emitter; use actual existing header filename if different. Copy shared fixtures into `tests/fixtures/core-persistence-v1/`.

**Interfaces:** retain protocol version 2. Add local operations `load_library_core`, `inspect_core_data`, `update_core_settings`. Consume existing `package_path`/`package_id` names used by package commands, plus trusted absolute `data_root`; update takes `expected_revision` and numeric `paddle_speed`. Return the existing response envelope with `core_data` containing `package_id`, `core_id`, `layout` {id,major,minor}, `mode` (`persistent`/`volatile`), `revision`, `paddle_speed`, `best_rally`. Publish the final concrete JSON examples to the FogCast worker before transport implementation; preserve existing package-path field spelling if discovery differs from this plan.

- [x] Read component AGENTS/architecture and use its documented native C++14 host-test and pinned ARM10.2 target-build workflow. Run baseline focused native tests.
- [x] Add codec tests against shared fixture bytes and missing/default behavior; record RED. Implement strict max-size, identity, checksum, exact version/count and enum validation. Avoid duplicating SNES assumptions.
- [x] Add real temporary-directory storage tests for no-follow paths, old-or-complete-new publication, absent/digest CAS, settings preserving progress, invalid data and failed publication. Implement retained trusted-directory access and atomic bounded records.
- [x] Add FES GP driver tests using actual fake-MMIO exchange mechanics: info contract, freeze/read completeness, begin/write/commit while reset held, timeout poisoning and resume. Implement bounded operations using generated constants; reject missing/multiple layouts and mismatched identity capabilities.
- [x] Add lifecycle regressions before implementation:

```text
launch persistent A -> play/capture [2,17] -> replace with same-core B
assert B restored [2,17], not the [1,0] preflight snapshot
flush file failure -> resume GP -> restore same generation/input -> retry
assert old complete record survives and no reset/program command occurred
```

- [x] Validate record/storage before retiring input, flush outgoing data, refresh incoming data under serialization, restore after identity before Start/input. Extend FlushSave/RestoreInput without altering SNES file identity or semantics. Retain complete snapshots on unsafe resume and honest recovery ownership.
- [x] Implement serialized inspect/update operations admitting exact package bytes, rejecting active-namespace writes or pending recovery, enforcing revision CAS. Read-only inspection does not program FPGA. Development `load_core` neither reads nor writes library data; status explicitly identifies mode.
- [x] Run native full/focused tests and protocol tests; update docs with exact local API/error contract. Commit and write review report with RED/GREEN evidence and final protocol examples.

## Task 3: Pong persistent registers and package export

**Files:** modify misteross `cores/fes-pong/rtl/{fes_gp.v,top.v}`, shared `cores/pong/rtl/pong_game.sv` and original wrapper/test instantiations; update `scripts/build_fes_pong.py`, package exporter metadata as needed, simulation tests and documentation. Regenerate its existing `fes_gp.vh` consumer; copy Task 1 fixtures where appropriate.

**Interfaces:** exact generated Task 1 wire contract. Shared game exposes player-return/point events or equivalent and configurable speed/freeze; original MiSTer wrapper ties defaults. Standalone shell owns persistent words independently from gameplay reset. Export package requires both new interfaces and preserves reproducible build/provenance checks.

- [x] Run existing Pong/mailbox tests. Add deterministic regression sim for restore [2,17], partial-commit rejection, freeze snapshot stability, resume without reset and speed6 boundary saturation; observe RED.
- [x] Implement staged words/bitmap and control-state validation. ACK freeze only after stable snapshot. Illegal commands leave data/state unchanged. Unknown or optional interfaces must not relax exact identity admission.
- [x] Add player-return and point events without sampling displayed scores. Count current rally with saturating u16 arithmetic; update best immediately, including unfinished rally. Preserve existing 0–9 score behavior and original wrapper defaults.
- [x] Exercise speed 2/4/6, walls/paddles, reset/restore, repeated freeze, invalid indexes, all-words-before-commit and max rally in simulation at 74.25 MHz.
- [x] Update producer metadata to a new persistent Pong package version. Run exporter tests and relevant simulation/lint/synthesis validation; do not run board builds concurrently with parent integration. Commit and report the exact build invocation/caches available for integration.

## Task 4: FogCast library context, APIs and transport

**Files:** modify FogCast `fogcast/{core_packages.go,service.go}`, `internal/{misterruntime,agent,httpapi,hostapi,fogcastcli}`, `host`, `protocol`, production agent config and docs. Prefer new focused `core_data.go` files alongside current core package handlers; add tests in owning packages. Do not modify UI presentation.

**Interfaces:** consume Task 2's frozen local protocol examples and shared record fixtures. Target defaults data root to `/media/fat/fogcast/core-data/`. Host public routes are approved settings GET/PUT and progress GET under `/api/v1/library/core-entries/{game_id}`. Settings PUT body uses `expected_package_id`, `expected_revision`, `paddle_speed` (numeric enum 0/1/2); response includes target identity and exact admitted data descriptor. CLI commands `core-settings GAME_ID`, `core-settings-set GAME_ID EXPECTED_PACKAGE_ID EXPECTED_REVISION SPEED`, `core-progress GAME_ID` use existing host API origin/formatting conventions.

- [x] Read component AGENTS/architecture. Run current package-library/service/adapter baseline tests before changes.
- [x] Add transport/admission tests for explicit library mode versus unchanged volatile development load, exact package staging/cleanup, malformed returned identity, rejected storage requests and cancelled/ambiguous writes. Record RED, then implement typed local/target operations. Never accept remote filesystem paths.
- [x] Coordinate target storage writes with existing lease/session ownership and runtime serialization; reads don't acquire physical ownership. Retain staging cleanup failures, and don't replay a lost mutation response.
- [x] Add selection tests: volatile-to-persistent allowed; persistent-to-missing/different layout rejected even absent record; same-layout rollback allowed; target offline unknown; selection CAS unchanged on rejection. Use runtime-supported interface metadata, not a second hardcoded ABI allowlist.
- [x] Add service/API tests with real immutable store/catalog: durable GET, settings revision CAS, invalid enums/identity, progress preserved, active namespace/recovery refusal, same-core replacement latest-data behavior and host cleanup regression. Implement APIs under existing lifecycle boundary.
- [x] Keep GameID and actual active package/generation attribution unchanged; source persistence mode only from resolved library entry. Bind data results to the currently selected target/package.
- [x] Add CLI tests and operator/UI API docs. Run focused tests, affected race tests, `go vet` and `go test ./...`; commit and report final protocol identities and errors.

## Task 5: Integration, review and exact acceptance

**Files:** parent pins and consistency/fixture checks if new copied fixtures are added; `docs/core-packages.md`, validation record and plan checklist. FogCast native input lock must match reviewed runtime commit.

**Interfaces:** only reviewed component commits become parent inputs. Keep exact cache/image identity receipts; root UI worktrees and normal host catalog are preserved.

- [x] Independently review each component for spec compliance and code quality, fix concrete failures, then review cross-component admission/flush/error behavior as one path.
- [x] Select reviewed package/runtime/RTL/FogCast commits, regenerate/compare every consumer and fixture. Update native input lock with runtime commit. Run `make check`, `make test`, `make host`.
- [x] Run `make dev` once per necessary integration change. Build the changed standalone RBF using pinned compiler tools; reuse unchanged compiler/Linux/console artifacts. Validate synthesized Pong behavior before kit deployment.
- [x] Claim the designated kit with existing lease API, inspect current image/boot/device identity and preserve working rollback artifact. Deploy diagnostic combination using documented workflow and private host/config/catalog.
- [x] Set speed, launch Pong, exercise actual rally events, Stop, inspect record/revision, relaunch and reboot to verify persistence. Test compatible version switch/back, incompatible/removing rejection, controlled write failure/retry, usable launcher return and SNES regression.
- [x] Once stable, run required reproducible build/verify and exact-image acceptance; distinguish from diagnostic evidence. Restore ordinary host pairing, release lease and preserve raw evidence privately.
- [x] Update guides and validation with tested hashes/revisions and limitations. Commit integrated pins/docs; report completion and any hardware acceptance requiring user observation. Do not merge or publish without applicable user authorization.

Completed against FES integration `47bf36e3b821202a1daed52240913d355c02a89e`;
see the [dated acceptance record](../../validation/2026-09-09-core-persistence.md).
The final documentation commit records acceptance without changing image inputs.
