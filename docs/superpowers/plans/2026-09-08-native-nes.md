# Native NES Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a software-complete native NES vertical slice to FES, using the pinned MiSTer NES core, one validated iNES/NES2 cartridge, one controller, and the existing image and lifecycle paths.

**Architecture:** Extend the shared package contract first, then regenerate its C++ and Go consumers. Extend misteross's existing generic fetch/rebuild/export lane and FogCast's existing extra-core image lane; extend libmister-runtime's single profile table and artifact preflight; finally select all four cores in the FES integration profile and update current support documentation. The UI, public API, lease protocol, boot selector, and update mechanism remain unchanged.

**Tech Stack:** Go 1.22+, C++14, Python 3.11+, POSIX shell, TOML/YAML package definitions, Quartus Prime Lite 17.0.2 when an FPGA rebuild is available, and the existing FES/FogCast test suites.

**Spec:** [Native NES design](../specs/2026-09-08-native-nes-design.md)

## Global Constraints

- The pinned upstream source is `https://github.com/MiSTer-devel/NES_MiSTer` at `9a63821173b6da4d6e95dcbe2e2a322ec8171144`.
- The locked artifact is `releases/NES_20260823.rbf`, SHA-256 `a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701`, size `3282472`, project `NES.qpf`.
- The native launch contract admits only `.nes`, a 32 MiB maximum, index `0`, raw little-endian byte-pair transfer, and one standard controller.
- Runtime preflight accepts iNES 1.0 and NES2 headers, rejects missing magic, zero PRG, trainers, impossible sizes, and truncated payloads before FPGA mutation.
- FDS, UNIF/UNF, NSF, saves, savestates, cheats, Zapper/Miracle Piano/SNAC, four-player accessories, mapper-specific policy, and UI changes are excluded.
- Keep all `sources/` integration checkouts pinned and clean; component edits live in `out/dev/nes-native-support/<component>` worktrees.
- Reuse native image/compiler caches; do not perform a cold rebuild of unchanged Linux, compilers, or base packages during development.
- Do not claim hardware support without exact-image evidence; software support and hardware-pending status are separate.

## File Map

- `sources/mister-packages/packages/source/nes_mister.yaml`, `packages/system/nes.yaml`, and generated C++/Go consumers define the shared contract.
- `sources/misteross/cores.lock`, `scripts/export_core_bundle.py`, and focused tests carry source/RBF provenance and bundle admission.
- `sources/libmister-runtime/include/libmister-runtime/runtime.h`, `src/profile.cpp`, `src/native/artifacts.*`, `src/linux/production_hardware.cpp`, generated `nes.hpp`, and unit tests implement native admission and transfer.
- `sources/FogCast/internal/systems/table.go`, `internal/misterruntime/client.go`, `internal/misterruntime/runtime.go`, target-image selection code, and shell tests install and dispatch NES without touching UI code.
- `profiles/native-integration-dev.toml`, `scripts/consistency.py`, `scripts/build.py`, parent tests, and current guides select and verify the four-system integration.

---

### Task 1: Add and validate the NES package contract

**Files:**
- Create: `sources/mister-packages/packages/source/nes_mister.yaml`
- Create: `sources/mister-packages/packages/system/nes.yaml`
- Modify: `sources/mister-packages/internal/pack/system.go`
- Modify: `sources/mister-packages/internal/emitcpp/emitcpp.go`
- Modify: `sources/mister-packages/internal/pack/snes_test.go` or a new `internal/pack/nes_test.go`
- Test: `sources/mister-packages/internal/pack/nes_test.go`

**Interfaces:**
- Consumes: the existing `CoreSourceFile`, `SystemFile`, `MediaRule`, and `MediaTransform` loader/emitter.
- Produces: validated `nes_cartridge` transform metadata, `packages/source/nes_mister.yaml`, and `packages/system/nes.yaml` for later generators.

- [x] **Step 1: Write the failing package test**

Add `TestNESContract` that loads `packages/system/nes.yaml`, checks `ExpectedCore == "NES"`, artifact `nes.rbf`, source commit `9a63821173b6da4d6e95dcbe2e2a322ec8171144`, cartridge index `0`, extension list `[".nes"]`, maximum `0x2000000`, transform `nes_cartridge`, and the standard A/B/Select/Start masks.

```go
func TestNESContract(t *testing.T) {
    sys, err := LoadSystem(filepath.Join(repoRoot(t), "packages/system/nes.yaml"))
    if err != nil { t.Fatal(err) }
    if sys.ExpectedCore != "NES" || sys.RBF.Artifact != "nes.rbf" || sys.CoreSource == nil || sys.CoreSource.Commit != "9a63821173b6da4d6e95dcbe2e2a322ec8171144" { t.Fatalf("wrong NES pin: %+v", sys) }
    if len(sys.Media) != 1 || sys.Media[0].Index != 0 || sys.Media[0].Transform != "nes_cartridge" || sys.Media[0].Extensions[0] != ".nes" || uint64(sys.Media[0].MaximumSize) != 0x2000000 { t.Fatalf("wrong NES media: %+v", sys.Media) }
    if sys.Input.A != 0x10 || sys.Input.B != 0x20 || sys.Input.Select != 0x400 || sys.Input.Start != 0x800 { t.Fatalf("wrong NES input: %+v", sys.Input) }
}
```

- [x] **Step 2: Run the focused test to verify it fails**

Run: `go test ./internal/pack -run TestNESContract -v`

Expected: FAIL because `packages/system/nes.yaml` and the `nes_cartridge` transform do not exist.

- [x] **Step 3: Implement the package files and transform admission**

Add the exact source pin values from the spec. Define the system with `expected_core: NES`, `rbf.artifact: nes.rbf`, cartridge index `0`, `.nes`, `0x2000000`, `transform: nes_cartridge`, reset words `0x1/0x1/0x0`, little-endian byte pairs, and one-player masks `up=0x8, down=0x4, left=0x2, right=0x1, a=0x10, b=0x20, select=0x400, start=0x800` with optional fields zero. Extend Go transform validation to accept exactly `nes_cartridge`; map it to the C++ literal in `MediaTransform()` emission.

- [x] **Step 4: Run the focused package test and emitter tests**

Run: `go test ./internal/pack ./internal/emitcpp ./internal/emitgo -v`

Expected: PASS, including the existing SNES transform tests.

- [x] **Step 5: Commit the package contract**

```sh
git add packages internal/pack internal/emitcpp internal/pack/nes_test.go
git commit -m "feat(packages): define native NES contract"
```

### Task 2: Generate checked-in consumers

**Files:**
- Create: `sources/libmister-runtime/src/native/generated/nes.hpp`
- Create: `sources/FogCast/internal/systems/generated/nes.go`
- Modify: `sources/mister-packages/Makefile`
- Test: `sources/libmister-runtime/tests/unit/profile_test.cpp`
- Test: `sources/FogCast/internal/systems/table_test.go`

**Interfaces:**
- Consumes: the validated `packages/system/nes.yaml` from Task 1.
- Produces: generated `generated::kNES`, `NESExpectedCore`, and `NESCartridgeIndex` used by runtime and FogCast.

- [x] **Step 1: Add failing consumer assertions**

Add runtime assertions that `generated::kNES.system == "nes"`, artifact `nes.rbf`, media index `0`, transform `nes_cartridge`, and expected core `NES`. Add FogCast assertions that the NES row’s expected core and file index equal the generated constants.

- [x] **Step 2: Run the focused consumer tests to verify they fail**

Run: `make -C sources/libmister-runtime test` and `go test ./internal/systems -run 'NES|Table' -v`.

Expected: FAIL because the generated files and generated table symbols are absent.

- [x] **Step 3: Generate the consumers from the package emitter**

Run from the package worktree:

```sh
go run ./cmd/mister-packages emit-cpp packages/system/nes.yaml > ../libmister-runtime/src/native/generated/nes.hpp
go run ./cmd/mister-packages emit-go packages/system/nes.yaml > ../FogCast/internal/systems/generated/nes.go
```

Keep the generated preamble and do not hand-edit generated values.

- [x] **Step 4: Register the generated files in package/build checks**

Add `NES_SYSTEM` and `NES_CORE_SOURCE` variables and validation commands to `sources/mister-packages/Makefile`. Extend `sources/FES/scripts/consistency.py` later in Task 7 for parent checks; do not duplicate generated values manually.

- [x] **Step 5: Run focused consumer tests and compile checks**

Run: `make -C sources/mister-packages test`; `make -C sources/libmister-runtime test`; `go test ./internal/systems` in FogCast.

Expected: PASS for package generation and current runtime/FogCast tests before native profile registration.

- [x] **Step 6: Commit generated consumers**

```sh
git add src/native/generated/nes.hpp tests/unit/profile_test.cpp
git commit -m "feat(runtime): generate NES profile consumer"
```

Commit the FogCast generated file and assertions in the FogCast worktree with its own message.

### Task 3: Add NES source provenance and bundle export

**Files:**
- Modify: `sources/misteross/cores.lock`
- Modify: `sources/misteross/scripts/export_core_bundle.py`
- Modify: `sources/misteross/tests/test_core_build.py` and export tests discovered by `rg -n "megadrive|snes|pong" tests scripts`
- Modify: `sources/misteross/README.md` and `docs/architecture.md`

**Interfaces:**
- Consumes: the NES source pin and generated package values from Task 1.
- Produces: `make fetch-core CORE=nes`, `make rebuild-core CORE=nes`, and `make export-core-bundle CORE=nes` with a sealed `nes.rbf`/`nes-rbf.toml` bundle.

- [x] **Step 1: Write failing lock/export tests**

Add a lock test that loads `cores.lock` and compares the NES repository, commit, RBF path, SHA-256, size, and `NES.qpf`. Add manifest tests that allow `system == "nes"`, artifact `nes.rbf`, recipe `scripts/rebuild_core.py`, and the authoritative repository/revision pair while continuing to reject unknown systems.

- [x] **Step 2: Run focused tests to verify they fail**

Run: `python3 -m unittest discover -s tests -p '*core*test.py' -v` and `python3 scripts/core_lock.py validate`.

Expected: FAIL because `core.nes` is absent and export admission allows only the existing systems.

- [x] **Step 3: Add the exact NES core lock**

Add:

```toml
[core.nes]
repo = "https://github.com/MiSTer-devel/NES_MiSTer"
commit = "9a63821173b6da4d6e95dcbe2e2a322ec8171144"
rbf_path = "releases/NES_20260823.rbf"
rbf_sha256 = "a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701"
rbf_size = 3282472
project = "NES.qpf"
rationale = "Official Release20260823 RBF and source pin for the native NES cartridge slice."
```

- [x] **Step 4: Extend generic export admission**

Change `BundleManifest.encode_manifest` to admit `nes`, update the identity map in `export_bundle`, and keep `scripts/rebuild_core.py` as the recipe. Do not add NES-specific timing gates until a real rebuild produces a timing summary; the existing generic Quartus compile and upstream-hash comparison remain the source-built lane.

- [x] **Step 5: Run fetch/hash and focused tests**

Run: `make -C sources/misteross fetch-core CORE=nes`; `python3 scripts/core_lock.py get nes commit`; the focused export/lock tests; and `git diff --check`.

Expected: the fetched checkout is at the locked commit and reports the exact RBF digest/size. A Quartus rebuild is optional in this environment and must not be faked if Quartus is unavailable.

- [x] **Step 6: Commit provenance/export changes**

```sh
git add cores.lock scripts/export_core_bundle.py tests README.md docs/architecture.md
git commit -m "feat(misteross): add pinned NES core bundle lane"
```

### Task 4: Implement runtime NES profile and cartridge preflight

**Files:**
- Modify: `sources/libmister-runtime/include/libmister-runtime/runtime.h`
- Modify: `sources/libmister-runtime/src/native/artifacts.cpp`
- Modify: `sources/libmister-runtime/src/profile.cpp`
- Modify: `sources/libmister-runtime/src/linux/production_hardware.cpp`
- Modify: `sources/libmister-runtime/tests/unit/artifacts_test.cpp`
- Modify: `sources/libmister-runtime/tests/unit/native_hardware_test.cpp`
- Modify: `sources/libmister-runtime/README.md` and `docs/support-matrix.md`

**Interfaces:**
- Consumes: `generated::kNES` from Task 2 and the `nes_cartridge` package transform.
- Produces: `MediaTransform::nes_cartridge`, `PrepareMediaContent` validation, and a production profile named `nes`.

- [x] **Step 1: Write failing runtime artifact tests**

Add helpers that create a valid iNES 1.0 mapper-0 image (`NES\x1a`, PRG pages `2`, CHR pages `1`, payload bytes), a valid NES2 image using the same bounded payload, and malformed variants. Assert that valid plans have `source_offset == 0`, `source_size == file size`, `prefix_size == 0`, and `battery_ram_size == 0`; assert invalid magic, trainer bit, zero PRG, declared payload larger than file, and >32 MiB files return `invalid_request`.

```cpp
assert(mister::native::PrepareMediaContent(artifact, mister::MediaTransform::nes_cartridge, &plan).ok());
assert(plan.prefix_size == 0 && plan.source_offset == 0 && plan.source_size == artifact.size());
```

Add a production-profile test that a `Launch{system="nes", rbf="/usr/share/mister-runtime/cores/nes.rbf", media={{"cartridge", rom}}}` prepares successfully and uses index `0`/expected core `NES`. Add a negative test proving malformed NES content is rejected before the fake hardware’s program call.

- [x] **Step 2: Run the focused tests to verify they fail**

Run: `make -C sources/libmister-runtime test`.

Expected: FAIL because the enum and profile are not registered.

- [x] **Step 3: Add the transform and parser**

Add `nes_cartridge` to the enum. In `PrepareMediaContent`, retain the raw plan after checking:

1. file size is at least 16 bytes and at most 32 MiB;
2. bytes 0–3 are `NES\x1a`;
3. flags6 trainer bit (bit 2) is clear;
4. iNES1 PRG pages are nonzero and `16 + prg_pages*16384 + chr_pages*8192` fits in the file and bound; or NES2 size fields decode to a positive PRG payload and the same fit/bound condition;
5. NES2 exponent/multiplier arithmetic cannot overflow a 64-bit intermediate.

Use bounded `pread` reads through the existing artifact descriptor. Return `invalid_request` with an NES-specific message for format failures and `io_failed` for short reads. Do not synthesize a prefix or inspect mapper numbers.

- [x] **Step 4: Register the generated profile**

Include `native/generated/nes.hpp`, call `profiles.Add(ProfileFromGenerated(native::generated::kNES))`, map `nes_cartridge` in `ProfileFromGenerated`, and keep `save_path` restricted to SNES.

- [x] **Step 5: Run runtime tests and C++14 build**

Run: `make -C sources/libmister-runtime test`; `make -C sources/libmister-runtime all`; `git diff --check`.

Expected: PASS, with the existing Mega Drive/Pong/SNES tests unchanged and the new NES parser tests green.

- [x] **Step 6: Commit runtime support**

```sh
git add include src tests README.md docs/support-matrix.md
git commit -m "feat(runtime): support validated native NES cartridges"
```

### Task 5: Extend FogCast native dispatch and image installation

**Files:**
- Modify: `sources/FogCast/internal/systems/table.go`
- Modify: `sources/FogCast/internal/misterruntime/client.go`
- Modify: `sources/FogCast/internal/misterruntime/runtime.go`
- Modify: `sources/FogCast/internal/misterruntime/client_test.go`, `snes_test.go`, and a new `nes_test.go`
- Modify: `sources/FogCast/internal/targetimage/core_selection.go` and tests
- Modify: `sources/FogCast/scripts/native-extra-cores.sh`
- Modify: `sources/FogCast/scripts/target-image-container.sh`
- Modify: `sources/FogCast/scripts/build-target-image.sh`
- Modify: `sources/FogCast/scripts/tests/native-extra-cores_test.sh`, `target-image_test.sh`, and `target-image-sources_test.sh`
- Modify: `sources/FogCast/docs/DEVELOPMENT.md`, `README.md`, and `docs/ARCHITECTURE.md`

**Interfaces:**
- Consumes: generated `NESExpectedCore`/`NESCartridgeIndex`, runtime socket contract, and `NES_RBF_BUNDLE`.
- Produces: validated native NES launch requests and four-system image installation/verification without UI changes.

- [x] **Step 1: Write failing FogCast tests**

Add a native client table case requiring `system: "nes"`, RBF `/usr/share/mister-runtime/cores/nes.rbf`, one absolute cartridge media entry, and empty settings; add rejection cases for a missing media map, extra media, wrong RBF, and save path. Add runtime adapter tests that `DefaultRegistry().Lookup(protocol.SystemNES)` has expected core `NES`, file index `0`, extension `.nes`, and sends the fixed NES RBF path. Add target-image tests for `extraCoreRecipe("nes")`, revision `9a63821173b6da4d6e95dcbe2e2a322ec8171144`, and `NES_RBF_BUNDLE` mounting.

- [x] **Step 2: Run focused FogCast tests to verify they fail**

Run: `go test ./internal/misterruntime ./internal/systems ./internal/targetimage -run 'NES|Native|Core' -v`; `sh scripts/tests/native-extra-cores_test.sh`.

Expected: FAIL because the client admits only Mega Drive/SNES/Pong and the helper admits only the three-system string.

- [x] **Step 3: Register NES in the product and native adapter**

Use `generated.NESExpectedCore` and `generated.NESCartridgeIndex` in the existing NES row, retain folder `NES`, RBF selector `_Console/NES`, target roots `/media/fat/games/NES`, one-second file delay, and only `.nes`. Add `/usr/share/mister-runtime/cores/nes.rbf` to `nativeRBFPath`, admit `SystemNES` as a cartridge launch with no save path, and leave Pong’s ROM-less branch intact.

- [x] **Step 4: Extend image helper and container mounts**

Admit exactly `megadrive pong snes nes`, set extras to `pong snes nes`, return RBF count `5`, select `NES_RBF_BUNDLE`, validate the NES source repository/revision, install `nes.rbf` and `nes.toml`, and remove stale NES files when the profile returns to Mega Drive-only. In `target-image-container.sh`, mount and pass `/nes-rbf-bundle:ro` during fetch. In `build-target-image.sh`, compare and copy NES selection records wherever the existing Pong/SNES records are compared/copied.

- [x] **Step 5: Run FogCast focused tests and Go formatting**

Run: `gofmt -w` on changed Go files; `go test ./internal/misterruntime ./internal/systems ./internal/targetimage`; `sh scripts/tests/native-extra-cores_test.sh`; `sh scripts/tests/target-image_test.sh`; `git diff --check`.

Expected: PASS, including default Mega Drive compatibility and four-system image fixture checks.

- [x] **Step 6: Commit FogCast support**

```sh
git add internal scripts README.md docs
git commit -m "feat(fogcast): add native NES launch and image selection"
```

### Task 6: Make FES select and verify the four-system integration

**Files:**
- Modify: `profiles/native-integration-dev.toml`
- Modify: `scripts/build.py`
- Modify: `scripts/consistency.py`
- Modify: `tests/test_consistency.py`
- Modify: `tests/test_native_dev.py`
- Modify: `README.md`, `docs/multi-system-development.md`, `docs/component-boundaries.md`, and `docs/README.md`

**Interfaces:**
- Consumes: selected child commits and the FogCast/misteross NES bundle contract from Tasks 3–5.
- Produces: a parent profile with `fpga_cores = ["megadrive", "pong", "snes", "nes"]` and consistency checks covering all generated consumers and source pins.

- [x] **Step 1: Write failing parent tests**

Extend the consistency fixture to include `core.nes`, validate `packages/source/nes_mister.yaml` and `packages/system/nes.yaml`, and expect generated file count `9` and source pin copy count `4`. Extend native-dev receipt tests to use the four core names and require `NATIVE_RUNTIME_SYSTEMS == "megadrive pong snes nes"`.

- [x] **Step 2: Run the parent tests to verify they fail**

Run: `python3 -m unittest tests.test_consistency tests.test_native_dev -v`.

Expected: FAIL because the profile and consistency lists allow only three systems.

- [x] **Step 3: Extend parent selection and consistency**

Set the integration profile’s `fpga_cores` to the four ordered systems. Change `selected_cores`, `bundle_arguments`, `build_bundle`, and `validate_bundle` to allow `nes` and pass `NES_RBF_BUNDLE`; keep Mega Drive’s existing source selection behavior. Add the NES generated C++/Go files and source YAML to `GENERATED`/validation lists and add `core.nes` to source-copy checks.

- [x] **Step 4: Update current documentation**

Describe NES as software-supported and hardware-pending in the parent map and multi-system guide. Record the exact source/RBF identity, `.nes`-only native contract, and explicit build command using `NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes'` and `NES_RBF_BUNDLE`. Do not change historical validation claims or call NES hardware-supported.

- [x] **Step 5: Run parent focused checks**

Run: `python3 -m unittest tests.test_consistency tests.test_native_dev -v`; `make check`; `make test`; `git diff --check`.

Expected: PASS, with current selected submodules still clean and parent pins unchanged until component commits are deliberately selected.

- [x] **Step 6: Commit FES integration**

```sh
git add profiles scripts tests README.md docs
git commit -m "feat(fes): select native NES in integration profile"
```

### Task 7: Assemble and verify the selected software path

**Files:**
- Modify: selected parent gitlinks under `sources/`
- Create: `docs/validation/2026-09-08-native-nes-software.md`
- Generated: `out/dev/nes-native-support/` logs and cached bundles (ignored)

**Interfaces:**
- Consumes: reviewed component commits from Tasks 1–6 and the locked upstream NES checkout.
- Produces: a reviewable FES branch whose selected component pins, generated consumers, bundle admission, and image selection agree.

- [x] **Step 1: Select reviewed child commits in the parent worktree**

Fast-forward or detach each `sources/<component>` checkout to the commits produced by its component task, then `git add sources/<component>`. Keep all unrelated root worktrees untouched and verify each selected child is clean.

- [x] **Step 2: Run the locked source check and package/runtime/FogCast suites**

Run:

```sh
make check
make -C sources/mister-packages test
make -C sources/libmister-runtime test
(cd sources/FogCast && go test ./...)
make test
```

Expected: all software tests pass. If Quartus is unavailable, record `fetch/hash verified; source rebuild not run` rather than inventing a bundle.

- [x] **Step 3: Check the NES source bundle when Quartus is unavailable**

Run `make -C sources/misteross fetch-core CORE=nes`; then, only with an installed exact Quartus 17.0.2, run `rebuild-core` and `export-core-bundle`. Verify the resulting sealed directory contains only `nes.rbf` and `nes-rbf.toml`, and pass it to FogCast as `NES_RBF_BUNDLE`. Here the locked checkout and official RBF hash were verified; Quartus was unavailable, so rebuild/export remains deferred.

- [x] **Step 4: Run image preflight without a cold rebuild**

Use the existing native development/image cache and run the target-image source/fixture checks with `NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes'` and sealed synthetic fixtures when no NES rebuild is available. The fixture confirms a five-RBF manifest, exact sidecars and missing-core rejection; an assembled image remains pending the bundle.

- [x] **Step 5: Record software evidence and verify clean state**

Write `docs/validation/2026-09-08-native-nes-software.md` with selected FES/component commits, upstream pin/digest, commands/results, cache reuse, and the explicit hardware-pending classification. Run `git status --short`, `git diff --check`, and `make check` once more.

- [x] **Step 6: Commit the integration evidence**

```sh
git add sources docs/validation/2026-09-08-native-nes-software.md
git commit -m "test(fes): verify native NES software integration"
```

Do not merge or claim hardware support from this plan; prepare the branch for review and a separately authorized exact-image kit test.
