# Core publication and guided setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution, or superpowers:subagent-driven-development if the user selects that method. Execute task-by-task; checkbox steps track progress.

**Goal:** Make prepared FES cores discoverable and installable through FogCast, then create ordinary package-backed games through guided ROM selection, including SMS and SG-1000.

**Architecture:** FES publishes a closed local package catalog from authenticated prepared candidates. FogCast reads that catalog, verifies/imports packages into the existing store, and exposes manifest-derived setup information through shared host APIs and the browser management panel. Source context is distinct from executor binding; no new mesh coordinator, lease or kit database is introduced.

**Tech Stack:** Python 3.11, Go 1.26.5, existing SQLite catalog, existing host HTTP/JSON APIs and browser JavaScript; authenticated HIP/nextpnr package preparation when an artifact is needed.

**Spec:** ../specs/2026-09-27-menu-core-library-design.md (approved software-first scope).

## Global Constraints

- Work in /home/deano/fes/out/dev/menu-core-library/fes; base aaf7de2c. Preserve other agents' work.
- No contact with or deployment to the occupied kit. GPU 0 only if preparation needs GPU compilation; coordinate compiler use before it starts.
- Publish package identity and per-ROM content identities independently. Never ship private BIOS/game bytes.
- Normal setup never invokes a compiler. Do not modify the factory image package set in this slice.
- No permanent Host/Kit role enum, second catalog, new lease, federation protocol or automatic host failover.
- Exact SMS 32768-byte and SG-1000 16384-byte cartridge requirements remain enforced by existing selection/launch paths.
- Existing catalog/game selections remain untouched by sync or package import. Offline cached browsing does not authorize launch.
- Native HDMI display, rooms rendering replacement and direct kit setup screens are separate display work; this slice exposes reusable services/models and browser setup.
- Return an uncommitted diff unless the user authorizes commit/push/PR. Planned commits are review boundaries, not advance authorization.

## Review Focus

- A catalog refresh preserves source_id while changing catalog_sha256; repeated sync or setup after a lost response must not create duplicates or replace an existing game's selection (Tasks 2–3).
- A catalog path escape, missing file or tampered archive must fail before package import (Tasks 1–2).
- Two service sources with the same title slug must remain distinct; setup must not switch the active coordinator (Tasks 2–4).
- Multiple candidate ROMs, wrong sizes and partial selections must remain explicit choices/Missing, never Ready (Tasks 3–4).
- A package registered but not built, an incompatible executor and an offline source must have distinct states (Tasks 1–4).

## Task 1: Publish prepared artifacts as a closed local catalog

**Files:** Create config/core-library.toml, scripts/core_catalog.py, tests/test_core_catalog.py; modify Makefile and docs/core-development.md. Consume scripts/core_dev.py, scripts/recipes.py and config/core-recipes.toml without duplicating package parsing.

**Interfaces:** `publish(root: Path, metadata: Path, prepared: dict[str, Path], output: Path) -> dict` writes catalog.json and immutable packages/<package-id>.fcore. CLI: `make core-catalog CORE_CATALOG_ARGS='--prepared fes.sms=/absolute/prepared.json ... --output /absolute/new-directory'`. Metadata version 1 includes stable source_id `fes-first-party` and rows: core_id, label, system, standing (supported/demo/experimental). Catalog JSON version 1 carries source_id (stable publication namespace), catalog_sha256 (digest of the canonical catalog body excluding that digest field), entries with those fields, and optional package_id/archive_sha256/archive_size/archive_path. Unproduced rows have no artifact fields; they are never installable. Paths are relative to the output directory.

- [x] Add tests `test_publish_binds_prepared_identity`, `test_unprepared_core_has_no_install_action`, `test_tampered_or_wrong_core_candidate_rejected`, `test_repeat_publication_same_bytes`, `test_output_collision_preserves_existing_catalog`. Verify authoritative parser identities against prepared.json, archive digest/size and selection provenance. Assert no compiler/network/kit invocation.
- [x] Run `python3 -m unittest discover -s tests -p test_core_catalog.py -v`; observe intended failures.
- [x] Implement metadata and publication using current prepared-receipt verification, bounded snapshots and canonical package parser. Publish atomically to a new directory; never use cache files without their receipts. Include all seven registered core metadata rows; Apple II experimental, Catch demo.
- [x] Re-run tests and `python3 -m unittest discover -s tests -p test_core_dev.py -v`. Expected zero failures.
- [x] Review Task 1 diff; user authorized committing the reviewed change.

## Task 2: Read the catalog and reconcile package installation

**Files:** Create sources/FogCast/corecatalog/catalog.go and catalog_test.go, fogcast/core_catalog.go and core_catalog_test.go, internal/hostapi/core_catalog.go and core_catalog_test.go; modify internal/hostapi/core_packages.go, existing FogCast configuration loader and cmd/fogcast-api wiring after locating their current startup path; update docs/ARCHITECTURE.md. Do not restructure unrelated startup code.

**Interfaces:** `corecatalog.Load(path string) (Catalog, error)` validates the versioned index and bounded relative paths; `Catalog.OpenPackage(coreID string) (io.ReadCloser, Entry, error)` validates archive size/digest before returning verified bytes. Optional host configuration `[core_catalog] path` points to local catalog.json; absent means existing manual APIs remain available. Service methods `AvailableCores(ctx context.Context) ([]AvailableCore, error)` and `InstallAvailableCore(ctx context.Context, sourceID, coreID, packageID string) (InstalledCorePackage, error)` reuse ImportCorePackage and CorePackages. GET /api/v1/core-catalog returns source_id and cores; POST /api/v1/core-catalog/install requires all three identity fields. Installation is explicit and idempotent; no game selection changes.

- [x] Add failing reader/service/API tests for missing/corrupt artifact, closed schema, source/package mismatch, repeat install, existing newer selection preservation, unproduced core state and catalog-disabled operation. Prove no target client mutation or session binding during listing/import.
- [x] Run `go test ./corecatalog ./fogcast ./internal/hostapi -run 'CoreCatalog|AvailableCore' -count=1` and observe intended failures.
- [x] Implement catalog verification and optional wiring. AvailableCore carries source_id, core_id, label, system, standing, package_id when available, artifact_state (unproduced/available/installed) and descriptor only from a verified package. Compatibility stays separate and uses existing checks, never recipe standing.
- [x] Re-run focused tests with -race; exercise disabled catalog startup. Expected zero failures.
- [x] Review Task 2 diff; user authorized committing the reviewed change.

## Task 3: Describe setup requirements and create game entries safely

**Files:** Create sources/FogCast/fogcast/core_setup.go and core_setup_test.go, internal/hostapi/core_setup.go and core_setup_test.go; extend hostclient/library_models.go and existing hostclient library methods/tests. Use catalog/core_entries.go, corepackage descriptors and existing media/ROM APIs rather than a new game table.

**Interfaces:** `CoreSetup(ctx context.Context, sourceID, coreID, packageID string) (CoreSetup, error)` returns verified descriptor requirements, existing entries and actionable setup state. GET /api/v1/core-catalog/{core_id}/setup requires source_id/package_id query values. `CreateCoreSetupEntry(ctx context.Context, request CoreSetupRequest) (catalog.CoreEntry, error)` behind POST /api/v1/core-catalog/entries. Request: source_id, core_id, package_id, title and explicitly selected media ids keyed by named ROM id, plus applicable blob/firmware selections. No server filesystem path accepted. Return source_id with game_id; source id scopes UI references but is not an executor id. Reuse deterministic existing core/title identity and conflict behavior; an exact existing selection returns the same entry, a different selection returns conflict requiring explicit edit.

- [x] Add failing tests for ROM-less default entry repeat, SMS 32768 bytes, SG-1000 16384 bytes, wrong sizes, absent required machine ROM, multiple named ROM requirements, stale package/source, existing title with different selection and retry after partial persistence. Unknown slot ids are rejected. No partially configured entry may become Ready; a repeat may finish the same explicit incomplete selection without overwriting another user's edits.
- [x] Run `go test ./fogcast ./internal/hostapi ./hostclient -run 'CoreSetup' -count=1`; observe intended failures.
- [x] Derive single ROM and multi-ROM requirements from Descriptor.ROM/ROMs; preserve blob, firmware and expansion mechanisms. Validate all supplied identities before creation. Use existing selection APIs with compare-and-swap; expose incomplete state if a later save fails. Avoid claiming atomic multi-step persistence unless implemented transactionally and tested. Do not alter firmware defaults as an implicit side effect of making a title.
- [x] Re-run focused tests with -race and existing core ROM/media/firmware/slot-expansion tests. Verify created entries project through the existing mesh library with independent package and ROM identities.
- [x] Review Task 3 diff; user authorized committing the reviewed change.

## Task 4: Guide setup in the existing browser management panel

**Files:** Modify sources/FogCast/internal/hostapi/ui_core_library.js, ui_core_library_test.js and ui.go only for required panel markup; add shared hostclient catalog/setup methods and tests if not completed in Task 3. Update docs/core-package-library.md.

**Interfaces:** Consume Tasks 2–3 APIs and existing bounded core-media upload. UI references are `{source_id, game_id}` or `{source_id, core_id, package_id}`; never treat the source as the executor. Existing manual import stays available. Catalog systems appear before game creation, with Install core/Choose cartridge/Choose BIOS actions. ROM choices use imported media or explicit user uploads; automatic filesystem scan/picker integration is a follow-up, not an unimplemented promise in this delivery.

- [x] Add failing UI tests for SMS/SG-1000 before games exist, unproduced artifact, install/reload, named ROM slots and exact-size hints, multiple candidates requiring choice, duplicate retry, source-context preservation, experimental badges and offline setup disabled. The existing browse/launch UI must still use normal games and placement APIs.
- [x] Run `node --test internal/hostapi/ui_core_library_test.js`; observe intended failures.
- [x] Implement a Systems/setup section in the existing panel. Keep descriptor requirements and package identities in service data, not JavaScript core-id branches. Display meaningful Missing/Unavailable states; no Ready from cached metadata. Creating a game does not launch it or switch a live session's coordinator.
- [x] Re-run UI tests and related hostclient/API tests. Document local catalog configuration and a complete SMS/SG-1000 import/select example without private ROM bytes.
- [x] Review Task 4 diff; user authorized committing the reviewed change.

## Task 5: Validate the integrated software lane and publish candidate evidence

**Files:** Extend tests/test_core_catalog.py and FogCast integration tests as needed; update docs/core-development.md and docs/core-packages.md. Evidence lives under ignored out/validation/menu-core-library, not credentials in source.

- [x] Exercise a synthetic published catalog through a private host: catalog list → install → ROM import → game creation → repeat sync/setup → host restart → same package/media/game identity. No kit endpoint is contacted. Verify missing/unproduced/experimental rows, source offline, existing selection preservation and mesh projection. Use existing canonical synthetic package fixtures; do not invent a second archive format.
- [x] Run `make check-generated`, `git diff --check` and `make test-changed TEST_CHANGED_ARGS='--base origin/main'`. Confirm every selected command passes; record all failures/skips.
- [x] Cross-build fogcast-api for ARMv7 with CGO disabled to demonstrate shared-host portability, without claiming kit-as-host deployment is delivered. Build the normal host artifact through the diagnostic snapshot lane when code is uncommitted; never call a committed-source parent build proof of unselected edits.
- [x] After committing the authorized source change, prepare current SMS and SG-1000 candidates using `make core-dev` and publish the local catalog. Check compiler pins/cache capacity before scheduling GPU 0 work. Missing candidates remain visibly unproduced until prepared; do not silently substitute historical artifacts.
- [x] Produce handoff with base/result, source diff, tests, publication identities and hardware classification. Request next integration: browser/operator review, then bounded exact-package hardware checks once kit ownership is available. Native menu display gets a separate spec; this branch does not claim HDMI menu completion.

## Self-review and next work

This plan implements approved software slices 1–2 and browser consumption. It deliberately does not implement FPGA display, cross-host catalog federation, automatic file scanning or a kit-as-host image. Service models preserve source scope and existing mesh placement/lease boundaries. Hardware candidate preparation follows source sealing; hardware acceptance follows explicit kit availability. Keep Apple II experimental until its owning agent's acceptance is established.

Recommended execution: native, because publication validation, service models and browser setup have sequential shared interfaces. Review this plan before implementation.

Execution note: source base fast-forwarded to `aaf32d3f` after Apple II PR #249. The user authorized source sealing and real candidate preparation; commits no longer require separate approval, while PRs and merges do. Serving-library context adds `core_catalog.library_source_id`, separate from the publication namespace; the test ledger records this review ruling.

Publication evidence: source commit `3bc63f68`; SMS and SG-1000 prepared with pinned HIP tools on GPU 0 and passed analogue signoff. Catalog generation `518e958f66160e9dc1cc47ffe64597ce4111555313c5930f12e39df2b0ff8223` contains seven rows and two verified packages. FogCast reader and canonical package inspection passed. No kit contact or hardware acceptance. Local evidence: `out/validation/menu-core-library/handoff.md`.
