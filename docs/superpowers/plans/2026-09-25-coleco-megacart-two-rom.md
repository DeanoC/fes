# Coleco MegaCart Two-ROM Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a development Coleco package that links a separately selected 8 KiB BIOS and 128 KiB MegaCart image on the target before FPGA download, with optional exact-shell SGM composition.

**Architecture:** Format 4 seals one blank base RBF and map for two required named sources. FogCast selects the household BIOS and title cartridge, while the target verifies both, composes an optional SGM module, patches the combined ROM map and passes the final RBF to libmister-runtime. A separate misteross producer supplies a banked Coleco shell; the factory format-2 package remains selected until independent promotion.

**Tech Stack:** JSON Schema and Python 3 fixtures/producers, Go package/linker/host/agent, C++14 runtime, SystemVerilog and Verilator, authenticated HIP Yosys/nextpnr/Mistral, Quartus 17.0.2 oracle.

**Spec:** `docs/superpowers/specs/2026-09-25-coleco-megacart-two-rom-design.md`

## Global Constraints

- Base is FES `d7545210de07f0c46c64931d1d3d05c38110fe1f`; work only in `out/dev/coleco-sgm-cartridge-map/fes` and preserve unrelated work.
- Both sources are required: firmware 8,192 bytes at logical offset 0 and cartridge 131,072 bytes at offset 8,192. The combined source is 139,264 bytes, under the existing map/linker 262,144-byte bound.
- Format 2, format 3, `rom_linking: 1`, existing `rom_link` status and their package identities keep their current meaning and bytes. Format 4 uses a separate identity domain, two-source transport and `rom_links` status.
- No application blob, blob-stream or firmware mailbox is declared by the new package. No private BIOS or retail ROM enters Git or fixtures.
- The new shell retains the v2 Coleco expansion socket. Its old SGM archive cannot attach to the changed shell; route and seal a new exact-shell SGM archive.
- MegaCart has eight 16 KiB banks: the final bank is fixed at `0x8000–0xbfff`, bank zero is selected on reset for `0xc000–0xffff`, and a read at `0xffc0–0xffff` selects `address[2:0]` before returning that read's data. Writes do not select.
- Final structured Fmax must be at least 52.224 MHz system, 74.25 MHz pixel and 12.288 MHz audio. A Quartus oracle result is not a product fallback.
- The factory image, current format-2 Coleco package and its `config/core-recipes.toml` row remain unchanged. The development package is built and imported explicitly because that registry permits one row per `core.id`. Synthetic kit evidence cannot qualify Time Pilot or another unverified mapper.
- The user's `AGENTS.md` requires authorization before any commit, push or PR. End each task with a reviewed diff and tests; proposed commit boundaries do not grant authorization. Use the existing kit lease for any later hardware test.

## Review Focus

- Swapping valid 8 KiB and 128 KiB media IDs or changing the household BIOS between selection and launch must reject before mutation; Task 5 tests both.
- A current host with an older target, or an older host reading a current format-2/3 status, must keep old launches working or receive a clear pre-programming format-4 refusal; Tasks 4 and 6 test the compatibility boundary.
- Bank-select reads must return data from the newly selected bank, including a held Z80 read and a following read; Task 7 tests the CPU sampling window.
- A map whose bits overlap the SGM socket or whose M10K BEL differs from the routed design must never seal even if the blank RBF parses; Task 8 tests both.
- A partial two-source stage, altered expansion or restart reconstruction with changed source bytes must not yield an active package; Tasks 3 and 6 test these cases.

---

### Task 1: Define format-4 bytes and cross-language fixtures

**Files:** Create `sources/mister-packages/schema/core-bundle-v4.json`, `sources/mister-packages/scripts/core_bundle_v4_fixtures.py`, `sources/mister-packages/tests/test_core_bundle_v4_fixtures.py`, and `sources/mister-packages/testdata/core-bundle-v4/`; modify `sources/mister-packages/docs/schema.md`, `sources/mister-packages/Makefile`, `scripts/consistency.py` and FES copied fixture consumers through `make generate`.

**Interfaces:** Format-4 manifest has required `[rom_map]` (`file`, `size`, `sha256`) and exactly two ordered `[[roms]]` (`id`, `role`, `source_size`, `source_offset`). Firmware precedes cartridge; roles and IDs are unique; offsets cover one 1–256 KiB logical buffer with no gap. Directory/archive members remain `manifest.toml`, `core.rbf`, `rom-map.json`; identity uses `FES-CORE-PACKAGE-4\n` plus length-delimited exact bytes. ROM-map format 1 is unchanged.

- [ ] Write a small positive format-4 fixture with two 1 KiB synthetic sources and negative cases for missing/duplicate roles, reversed or gapped offsets, total above 262,144, map-source mismatch, changed member digest, extra archive member and a format-3 manifest using `[[roms]]`. Keep the shared fixture small; Task 8 tests the actual Coleco 8 KiB + 128 KiB map. The positive fixture's ROM requirements are:

```toml
[[roms]]
id = "coleco-bios"
role = "firmware"
source_size = 1024
source_offset = 0
[[roms]]
id = "coleco-cart"
role = "cartridge"
source_size = 1024
source_offset = 1024
```

- [ ] Run `python3 -m unittest discover -s sources/mister-packages/tests -p 'test_core_bundle_v4_fixtures.py' -v`; confirm the new fixture validation fails before the schema/fixture implementation.
- [ ] Implement the closed schema and fixture generator. Derive actual size/digest from the generated map; do not retain the illustrative values above. Add the format-4 fixture copy to the existing FES generator and update `sources/mister-packages/docs/schema.md` with member order, bounds and identity domain.
- [ ] Run the focused test, `make generate`, `make check-generated` and `make check`; verify existing format-2/3 fixtures and package IDs are unchanged.

### Task 2: Make producer and readers agree on format 4

**Files:** Modify `sources/misteross/scripts/core_package.py`, `sources/misteross/scripts/export_core_package.py`, `sources/misteross/tests/test_core_package_v3.py`; modify `sources/FogCast/corepackage/package.go`, `sources/FogCast/corepackage/package_test.go`, `sources/FogCast/corepackage/rom_package_test.go`; modify `sources/libmister-runtime/src/native/core_package.hpp`, `sources/libmister-runtime/src/native/core_package.cpp`, `sources/libmister-runtime/tests/unit/core_package_test.cpp` and their shared fixture paths. Update each owner's current schema/architecture text with actual behavior.

**Interfaces:** Go `Descriptor.ROMs []ROMRequirement` and `Descriptor.ROMMap *ROMMapDescriptor` are populated only for format 4; existing `Descriptor.ROM` remains format 3. C++ `CoreDescriptor` mirrors this distinction. All readers verify the same package identity and sealed member set; runtime checks sealed map bytes/digest but leaves map semantics to the linker, as it does for format 3.

- [ ] Add one shared-fixture assertion to each Python, Go and C++ test suite. The assertions require format 4 to expose two ordered requirements and reject a one-byte change to manifest, payload or map. Example Go expectation:

```go
if got.Descriptor.Format != 4 || len(got.Descriptor.ROMs) != 2 ||
    got.Descriptor.ROMs[0].Role != "firmware" || got.Descriptor.ROMs[1].Role != "cartridge" {
    t.Fatalf("format-4 ROM requirements: %+v", got.Descriptor)
}
```

- [ ] Run `python3 -m unittest sources/misteross/tests/test_core_package_v3.py -v`, `go test ./corepackage` from `sources/FogCast`, then `make -C sources/libmister-runtime build/tests/unit/core_package_test` and `sources/libmister-runtime/build/tests/unit/core_package_test`; confirm format-4 assertions fail while prior fixtures still pass.
- [ ] Implement format-4 canonical parsing/encoding, exact keys and domain hashing in all three readers. Reject format 4 with `[rom]`, format 3 with `[[roms]]`, duplicate source IDs, missing member and map-size mismatch; retain format-2/3 paths unchanged.
- [ ] Rerun the three focused suites and `make check-generated`; review package-ID equality across Python, Go and C++ before touching launch behavior.

### Task 3: Stage and link two source bytes on the target

**Files:** Create `sources/FogCast/corepackage/rom_input_v2.go` and `sources/FogCast/corepackage/rom_input_v2_test.go`; use the existing `sources/misteross/expansion/rom.go` map patcher and `ComposeROM` rather than a second CRAM writer. Modify `sources/FogCast/corepackage/store.go` only for format-4 staged identity.

**Interfaces:** `ROMInputV2{Package []byte, BIOS []byte, Cartridge []byte, Expansion *expansion.Asset}` writes a canonical source-only archive with `rom-link-v2.json`, `package.tar`, `bios.bin`, `cartridge.bin`, optional `expansion.tar`. `ROMSourceIdentity{ID, Role, SourceSHA256 string; SourceSize int64}` and `ROMLinksIdentity{Sources []ROMSourceIdentity; MapSHA256, ProgrammedSHA256 string; ProgrammedSize int64}` carry exact target identity. `WriteROMInputV2(in ROMInputV2) ([]byte, error)` creates the envelope; `StageROMInputV2(ctx context.Context, root string, size int64, reader io.Reader) (Staged, error)` verifies every receipt field and stages only after linking succeeds.

- [ ] Test exact two-source link output against `expansion.LinkROM` over `append(bios, cart...)`, then repeat with `expansion.ComposeROM` and a valid exact-shell fixture. Reject swapped members, missing BIOS, wrong sizes, an altered source after receipt generation, wrong map, extra tar member, socket-overlapping map, interrupted reader and modified expansion. Example invariant:

```go
if got.Links.Sources[0].SourceSHA256 != sha256Hex(bios) ||
    got.Links.Sources[1].SourceSHA256 != sha256Hex(cart) {
    t.Fatal("both source identities must survive staging")
}
```

- [ ] Run `go test ./corepackage -run 'TestROMInputV2|TestStageROMInputV2' -v` from `sources/FogCast`; expect the new API tests to fail before implementation.
- [ ] Implement exact member order and canonical receipt checking, concatenate only after both source digests match, call existing Go `LinkROM`/`ComposeROM` once, and publish staged files atomically. Keep existing `ROMInput`, format-3 `ROMLinkIdentity` and `rom-link.json` unchanged.
- [ ] Rerun focused and full `go test ./corepackage`; prove no partial staged directory is admitted after each negative case.

### Task 4: Carry format-4 identity through native admission and status

**Files:** Modify `sources/libmister-runtime/include/libmister-runtime/runtime.h`, `sources/libmister-runtime/src/native/hardware.hpp`, `sources/libmister-runtime/src/native/hardware.cpp`, `sources/libmister-runtime/src/native/core_package.hpp`, `sources/libmister-runtime/src/daemon/protocol.cpp`, `sources/libmister-runtime/tests/unit/native_hardware_test.cpp`, `sources/libmister-runtime/tests/unit/protocol_test.cpp`, and format-4 response fixtures. Modify `sources/FogCast/internal/misterruntime/protocol_v2.go`, `sources/FogCast/internal/misterruntime/runtime.go`, `sources/FogCast/internal/misterruntime/rom_target_test.go` for the new typed identity.

**Interfaces:** Existing `rom_linking: 1` and format-3 `rom_link` stay valid. Format 4 requires a target-produced `rom_links` object with two ordered source identities and one final RBF digest. The runtime's linked load operation accepts the new object only when the verified package is format 4, programmed RBF bytes match its digest, and package/map/source metadata match; raw format-4 activation refuses. No global capability field is added.

- [ ] Add daemon/Go tests for a valid format-4 linked load, missing second identity, changed BIOS digest, wrong map digest, raw package activation, altered programmed RBF, and old format-3 status round trip. A valid response carries both entries, for example:

```go
identity := corepackage.ROMLinksIdentity{
    Sources: []corepackage.ROMSourceIdentity{
        {ID: "coleco-bios", Role: "firmware", SourceSize: 8192, SourceSHA256: strings.Repeat("a", 64)},
        {ID: "coleco-cart", Role: "cartridge", SourceSize: 131072, SourceSHA256: strings.Repeat("b", 64)},
    },
    MapSHA256: strings.Repeat("c", 64),
    ProgrammedSHA256: strings.Repeat("d", 64), ProgrammedSize: 4096,
}
```

- [ ] Run `make -C sources/libmister-runtime build/tests/unit/native_hardware_test build/tests/unit/protocol_test`, then both resulting binaries, and `go test ./internal/misterruntime/...` from `sources/FogCast`; confirm the format-4 case fails before implementation.
- [ ] Extend runtime admission and protocol parsing with a separate format-4 branch. Validate the package and typed two-source identity before `fpga_manager` is touched; keep the physical programming and Stop path shared with format 3. Decode `rom_links` in FogCast only for format 4 and keep format-2/3 fixtures byte-for-byte valid.
- [ ] Rerun both focused suites and the runtime/Go protocol fixture cross-check. Verify a format-4 load on an old-format test daemon is rejected before a programming call.

### Task 5: Select BIOS and MegaCart independently in the library

**Files:** Modify `sources/FogCast/fogcast/core_roms.go`, `sources/FogCast/fogcast/core_packages.go`, `sources/FogCast/fogcast/core_firmware.go`, `sources/FogCast/fogcast/core_roms_test.go`, `sources/FogCast/fogcast/core_firmware_launch_test.go`, `sources/FogCast/internal/hostapi/core_roms_test.go`, `sources/FogCast/internal/hostapi/ui_core_library.js` and its existing UI tests. Use existing `catalog/core_firmware.go` and `catalog/core_roms.go` selections unless a failing test proves a missing field.

**Interfaces:** Household `firmware` slot supplies the format-4 BIOS; the title's `CoreEntryROM` supplies its cartridge. `romLaunchSource` has a format-4 branch returning `ROMInputV2` and the two selected media IDs. `matchesLoadedIdentity` compares both ordered source digests, map digest, package ID and optional expansion ID. Declared readiness shows BIOS and cartridge separately; it never infers support from a filename or a 32 MiB storage limit.

- [ ] Add launch tests for exact 8,192/131,072-byte selections, missing BIOS, wrong BIOS size, swapped media IDs, changed household slot after snapshot, stale title ROM revision, wrong package and wrong expansion. Assert all failures leave target load-call count zero. Example negative assertion:

```go
if _, err := service.launchCoreEntry(ctx, gameID, snapshot); err == nil || target.loads != 0 {
    t.Fatal("invalid two-ROM selection reached the target")
}
```

- [ ] Run `go test ./fogcast ./internal/hostapi -run 'Test.*(ROM|Firmware|Core)' -count=1` from `sources/FogCast`; confirm at least the new format-4 launch test fails.
- [ ] Validate both roles against the sealed manifest, read the household BIOS and per-title cartridge separately, and use `corepackage.WriteROMInputV2`. Expose two explicit readiness indicators through the existing library/API/UI projection. Do not relax `validateROMMediaContract` for format 3 or add a mailbox to format 4.
- [ ] Rerun focused host/UI tests and the format-2/3 core-library tests. Verify a successful format-4 launch response matches both target-produced source digests before showing Ready.

### Task 6: Coordinate target staging, transfer and restart adoption

**Files:** Modify `sources/FogCast/internal/misterruntime/runtime.go`, `sources/FogCast/internal/agent/coordinator.go`, `sources/FogCast/internal/httpapi/core_data.go`, `sources/FogCast/corepackage/store.go`; add cases to `sources/FogCast/internal/misterruntime/rom_target_test.go` and `sources/FogCast/internal/agent/core_data_test.go`.

**Interfaces:** The target detects the distinct `rom-link-v2.json` archive, checks format-4 package support, stages both source bytes and optional expansion, then invokes the Task-4 native linked load. Restart adoption recomputes the programmed bytes and compares package, both source identities, map, expansion and final RBF. The old format-3 archive and endpoints remain accepted.

- [ ] Add target tests for accepted format-4 transfer, old agent/unsupported-format rejection before hardware mutation, truncated second source, context cancellation, mismatched expansion, lost load response reconciliation and restart with a changed BIOS or cartridge object. Assert active state only when all two-source fields and final digest match.
- [ ] Run `go test ./internal/misterruntime ./internal/agent ./internal/httpapi -run 'Test.*(ROM|CoreData|Restart)' -count=1` from `sources/FogCast`; confirm the new versioned-archive and adoption tests fail.
- [ ] Route only the new archive to `StageROMInputV2`, call the native linked load with `ROMLinksIdentity`, and compare the returned identity before publishing active state. Rebuild from retained source-only input on restart; never retain a caller-supplied programmed RBF as authority.
- [ ] Rerun focused target tests and `go test ./internal/misterruntime ./internal/agent ./internal/httpapi`; confirm format-3 restart cases still pass.

### Task 7: Implement the MegaCart CPU map with linked BIOS

**Files:** Create `sources/misteross/cores/fes-coleco/rtl/coleco_megacart_rom.v` and `sources/misteross/cores/fes-coleco/sim/megacart_tb.cpp`; modify `sources/misteross/cores/fes-coleco/rtl/coleco_machine.sv`, `sources/misteross/cores/fes-coleco/rtl/top.v`, `sources/misteross/cores/fes-coleco/rtl/coleco_application_gp.v`, `sources/misteross/Makefile`, and `sources/misteross/cores/fes-coleco/README.md` behind a development-only `FES_COLECO_MEGACART_LINK` build define.

**Interfaces:** New shell exposes no startup media/firmware mailbox. Its blank linked BIOS is 8 KiB at `0x0000–0x1fff`; blank linked cartridge ROM is 128 KiB. The v2 expansion request/response and core ABI remain unchanged. The ROM module consumes Z80 address/read phase and returns a registered byte; the CPU bank-select latch updates only on a qualifying memory read.

- [ ] Generate an original 8 KiB jump BIOS and 128 KiB cart with distinct per-bank bytes. Test bank zero after reset, fixed last bank, each selector address in `0xffc0–0xffff`, the selecting read's byte, held/back-to-back reads, writes that do not switch, SGM claim limits and a reset after Stop. Example expected address mapping:

```text
CPU 0x8000 -> ROM 0x1c000 (fixed bank 7)
CPU 0xc000 after reset -> ROM 0x00000 (bank 0)
CPU 0xffc3 -> select bank 3, return ROM 0x0ffc3 on that read
```

- [ ] Run `make -C sources/misteross sim-fes-coleco-megacart`; confirm the target/test fails before the development RTL exists.
- [ ] Add explicit blank M10K ROM lanes and the bank latch. Drive the physical ROM address from the newly selected bank for a selector read; retain the existing console/SGM arbitration outside the cart. Remove reset-held media copy only under `FES_COLECO_MEGACART_LINK`; keep the factory path's behavior unchanged.
- [ ] Run `make -C sources/misteross sim-fes-coleco-megacart sim-fes-coleco-oss`. Compare factory simulation outputs and inspect that no media endpoint is advertised by the new development build.

### Task 8: Seal a multi-column ROM map and matching SGM archive

**Files:** Modify `sources/misteross/scripts/rom_map.py` and `sources/misteross/tests/test_rom_map.py`; create `sources/misteross/scripts/build_fes_coleco_megacart.py` and `sources/misteross/tests/test_coleco_megacart_build.py`; modify `sources/misteross/scripts/build_coleco_sgm.py`, `sources/misteross/Makefile`, `sources/misteross/docs/architecture.md`, `sources/misteross/cores/fes-coleco/README.md`. Keep `build_fes_coleco_socket_v2.py` and factory recipe behavior intact.

**Interfaces:** Producer routes only the development shell, checks final timing, authenticates exactly 136 blank 1 KiB M10K lanes, emits format-4 manifest and one 139,264-byte map, then routes a new SGM archive against that exact shell. Map extraction derives each lane's real column/row from the routed BEL and selected Mistral database. No ROM destination lies inside `(1769,32,2806,1800)` or overlaps another. Python build-time and Go target-time links produce byte-identical final RBFs with and without SGM.

- [ ] Extend extractor tests for two legal M10K columns, a wrong routed BEL, occupied INIT, duplicate CRAM bit, missing lane and socket overlap. Add producer tests for a changed source/lock, route below any clock threshold, unsealed shell, wrong-shell SGM archive and Python-versus-Go digest mismatch.
- [ ] Run `python3 -m unittest sources/misteross/tests/test_rom_map.py sources/misteross/tests/test_coleco_megacart_build.py -v`; confirm new multi-column and producer cases fail before implementation.
- [ ] Generalize the extractor's column geometry without weakening existing ZX81/SMS/SG-1000 fixed-BEL checks. Build the new sealed package using the authenticated Coleco toolchain; require the full route's system/pixel/audio gates and socket-vacancy proof before export. Independently route SGM to the new frozen shell and enforce the v2 CRAM containment policy.
- [ ] Rerun Python tests and `make -C sources/misteross sim-fes-coleco-megacart`. The producer requires clean committed source: finish and review the uncommitted implementation first, then obtain the user's authorization to commit before running its sealed route and Go golden-link comparison. If authorization is pending, stop at an explicitly unsealed diagnostic. Run Quartus only as a timing oracle if authenticated nextpnr misses; record the exact critical path and open a routing issue for a separate owner instead of selecting a Quartus RBF.

### Task 9: Validate the explicitly imported development package

**Files:** Keep `config/core-recipes.toml` and `profiles/native-integration-dev.toml` unchanged. Update `docs/core-status.md`, `docs/core-packages.md`, `sources/FogCast/docs/core-package-library.md`, runtime support matrix and current component architecture pages for the implemented behavior. Add a dated `docs/validation/` record only after an actual kit run.

**Interfaces:** Factory `profiles/native-integration-dev.toml` continues to select the current format-2 `fes.coleco` artifact. Build the new producer directly in misteross and import its sealed package explicitly into a private FogCast library for diagnostics; the one-row-per-core FES registry is unchanged. A development package has exact package/map/BIOS/cart/optional SGM/programmed identities. No private bytes or screenshots are committed. Promotion to the factory recipe and image is a separate integration decision.

- [ ] Run focused Python, Go, C++ and Verilator suites, then `python3 scripts/test_changed.py --base origin/main`, `make check-generated`, `make check` and `git diff --check`. Check that all format-2/3 fixtures, current factory recipe and image profile remain unchanged.
- [ ] Confirm a sealed shell and matching SGM archive pass 52.224/74.25/12.288 MHz, vacant socket and all CRAM/link-byte checks. If they do not, stop at a source/route diagnostic; do not publish a development package as timed.
- [ ] With the designated kit's existing lease and exact authorization, run one synthetic two-ROM title without SGM and one with the newly sealed SGM module. Record visible bank-selection result, package/map/source/module/programmed digests, host/agent/runtime and image identity, Stop, relaunch, target idle and lease free. Classify this as an exact-artifact diagnostic, not retail or factory-image acceptance.
- [ ] Review the final FES diff and hand off base/result commit or uncommitted diff, commands and actual results, module/shared-contract effects, hardware classification and the remaining private-mapper check. Do not commit, push or open a PR until the user authorizes it.
