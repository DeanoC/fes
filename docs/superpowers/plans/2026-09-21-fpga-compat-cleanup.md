# FES Package-only FPGA Support Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove obsolete FPGA product backends and compatibility branches while preserving current FES packages and explicit contained hardware diagnostics.

**Architecture:** Keep library-to-agent-to-runtime package activation and local protocol 2. Move surviving consumers off legacy types before deleting Main/MGL, raw-game profiles and old build/image paths. Keep physical transitions in the runtime and coordinate shared definitions and generated consumers in the same FES worktree.

**Tech Stack:** Go, C++14, Python 3.11+, shell/Make/Buildroot, YAML/TOML/JSON contracts, Verilog/SystemVerilog and Verilator.

**Spec:** [Approved removal design](../specs/2026-09-21-fpga-compat-cleanup-design.md).

## Global Constraints

- FES described packages are the sole supported FPGA product path; retire conventional Main_MiSTer launches and old raw-core game profiles; retain explicit hardware diagnostics.
- Worktree: `/home/deano/fes/out/dev/fpga-compat-cleanup/fes`; branch: `refactor/fpga-compat-cleanup`; base: `2d54817cdd6ee40922f99d4fc4d4383445528bf1`.
- Return an uncommitted diff; commits, pushes and PRs are not authorized.
- Retain `fes.simple-game`, `fes.simple-computer` and `fes.application`, all on `fes-gp-v1`.
- Keep the existing network API version independently: HTTP `/v1` is not local runtime protocol 1.
- Development package loads stay volatile; library loads bind explicit durable data.
- Preserve existing non-FPGA emulation/casting, library metadata, user ROMs, saved data, boot/appliance rollback and unrelated settings.
- Do not replace the selected splash, alter the default image's package set, or implement the future rooms/attract ABI.
- Parent builds select committed bytes. Never report a parent build as validation of uncommitted edits.
- No build/compiler operation deploys automatically. Hardware operations use the exact designated kit and its existing lease; report diagnostic and exact-artifact acceptance separately.

## Review Focus

1. A target that omits old core availability must not re-enable raw-game launch; a valid package and explicitly selected host-only execution must still work (Task 1).
2. Agent restart or a lost Stop reply must preserve persistent package identity, retryable save failures and ownership; it must not downgrade or replay an ambiguous mutation (Task 2).
3. Contained raw diagnostics must remain idle-only and must never probe unknown fabric or bind persistence; splash recovery remains bounded (Tasks 3–4).
4. Deleting an apparently old producer helper must not break Catch/demo, shared Pong gameplay or explicit Quartus oracles, or allow stale package reuse (Tasks 5–6).
5. Automatically generated agent configuration must load after Main fields disappear, and the kernel builder must not require a removed image variant (Tasks 2 and 7).

## File ownership and order

One implementer owns Tasks 1–4 in sequence because they change shared Go/C++
interfaces. Tasks 5–6 own FPGA producer files. Task 7 owns image scripts and
overlays. The integrator alone owns root generation/consistency mappings,
affected-test selection and final documentation reconciliation. If execution
uses agents, transfer each task's exact file ownership before edits; never
give two agents simultaneous ownership of `runtime.go`, shared contracts or
root mappings.

Tasks 1–4 form the runtime cutover. Tasks 5–6 form the producer cutover. Task 7
depends on the agent configuration/CLI result of Task 2. Task 8 integrates all
three. Each task runs its focused tests before proceeding. Intermediate
compatibility code is removed by its named successor task, not retained in the
final result.

Retain existing file boundaries unless extraction makes a surviving primitive
independent of a deleted subsystem. The planned new files are:

- `sources/FogCast/internal/misterruntime/development_artifact.go`: retained atomic raw diagnostic staging.
- `sources/FogCast/internal/misterruntime/development_artifact_test.go`: staging failure/cleanup tests transferred from the Main package.
- `sources/FogCast/internal/misterruntime/contained_diagnostic_test.go`: protocol-2 wire and recovery tests.
- `sources/misteross/scripts/quartus_tools.py`: Quartus location/version/error helpers used by FES oracles.
- `sources/misteross/scripts/source_provenance.py`: retained live FES module provenance, replacing the misleading `legacy_source.py` name.

## Task 1: Make FPGA library eligibility package-only

**Files:** Modify `sources/FogCast/fogcast/service.go`, `core_packages.go`,
`native_availability.go`, `internal/systems/table.go`, and their tests;
`sources/FogCast/internal/hostapi/session.go`, `ui/kitlauncher` callers and
related host/UI tests. Keep catalog metadata separate from physical recipes.

**Interfaces:** Retain `Service.LaunchOn` and `launchCoreEntry`. Retain
`PlatformLaunchable(protocol.System) bool` for UI callers, but remove its
dependency on old target core availability. No target package API changes.

- [ ] Add a regression in `fogcast/native_availability_test.go` for rejection without any old availability advertisement:

```go
func TestRawBuiltinRejectedWithoutLegacyAvailability(t *testing.T) {
    client := &fakeServiceClient{}
    s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
    _, _, err := s.launchGame(context.Background(), catalog.Game{
        ID: "pong", System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin,
    }, nil)
    var api *protocol.APIError
    if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation || api.Phase != "admission" {
        t.Fatalf("raw FPGA game must require a package: %v", err)
    }
    if client.nativeLaunchCalls != 0 { t.Fatal("retired launch was dispatched") }
}
```

- [ ] Run `go test ./fogcast -run TestRawBuiltinRejectedWithoutLegacyAvailability -count=1` from `sources/FogCast`; confirm failure is the old launch behavior.
- [ ] Change admission before target mutation: a package entry continues through `launchCoreEntry`; an explicitly resolved host-only/casting execution keeps its current path; any remaining FPGA catalog launch returns `CodeUnsupportedOperation`, phase `admission`, message `FPGA launch requires an installed core package`. In `LaunchOn`, perform this check before `stopPackageOwnedForCatalogLaunch`: a rejected old game must not stop an already running package. Extend the existing active-package service fixture to assert the old game request returns that error with zero target Stop/load calls and unchanged active package identity. Do not infer a package from `System`, name or RBF path.
- [ ] Replace the old availability tests with package eligibility tests, preserving the queued-writer/deadlock regression and `ExecutionHostOnly` selection test. Remove stale availability projections from UI/health consumers after the corresponding server fields are removed in Task 2.
- [ ] Run `go test ./fogcast ./internal/hostapi ./hostclient ./ui/kitlauncher`, then `make test-ui`. Confirm package import/select/launch and retained catalog browsing pass. Update FogCast README's launch description in this task.

## Task 2: Cut FogCast over to native protocol 2 and delete Main coordination

**Files:** Modify `sources/FogCast/internal/misterruntime/{client.go,protocol_v2.go,runtime.go,core_data.go}` and tests; `internal/agent/{coordinator.go,content.go}`;
`internal/httpapi/{server.go,content.go}`; `targetclient` launch/content callers;
`cmd/mister-agent/main.go`; `internal/agentconfig/config.go`; `internal/targetcache`;
`protocol/native_cores.go` and health types. Create the three diagnostic files
listed above. Remove `internal/mister`, old profile adapter files
`native_cores.go`, `saves.go`, `nes_test.go`, `pong_client_test.go`,
`snes_client_test.go` only after surviving assertions move. Modify
`scripts/media.py`, matching `tests/test_media*.py`, shipped config examples
and image native agent arguments together.

**Interfaces:** Replace the legacy `Control` requirements with:

```go
type Control interface {
    Protocol2Status(context.Context) (Protocol2Response, error)
    Protocol2Stop(context.Context) (Protocol2Response, error)
    Protocol2LoadDevelopmentRBF(context.Context, string) (Protocol2Response, error)
}
```

Keep existing package/media/controller extension interfaces. Remove
`Prepare`/`Launch` from `agent.Runtime` and `ownedLaunchRuntime`; change
`agent.New` to `(runtime Runtime, launchTimeout, stopTimeout time.Duration,
options ...CoordinatorOption) *Coordinator`. Remove `core.Registry` from
`targetcache.Open` while retaining any actually used content extension policy.
The extracted staging helper is package-private
`writeAtomicDevelopmentRBF(path string, size int64, content io.Reader) error`.

- [ ] Add protocol wire tests using `newSequenceSocketFixture`: health, idle confirmation, idle Stop, active-package Stop and restart reconciliation must send only protocol 2. Include a retained `save_failed` package response and an unsupported protocol reply; assert no protocol-1 request and no second mutation. Run `go test ./internal/misterruntime -run 'Protocol2|Stop|Reconcile|Health|Idle' -count=1` to expose old callers.
- [ ] Implement contained raw requests through the existing tracked transport; keep absolute canonical-path validation and sent/ambiguous error attribution:

```go
func (client *Client) Protocol2LoadDevelopmentRBF(ctx context.Context, rbf string) (Protocol2Response, error) {
    if !validRuntimePath(rbf) { return Protocol2Response{}, errInvalidRuntimeRequest }
    line, attempted, err := client.callRawTracked(ctx, struct {
        Protocol int `json:"protocol"`
        Operation string `json:"operation"`
        RBF string `json:"rbf"`
        ProgrammingProfile string `json:"programming_profile"`
    }{2, "load_development_rbf", rbf, "development-contained-v1"})
    if err != nil { return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted} }
    response, err := decodeProtocol2Response(line)
    if err != nil { return Protocol2Response{}, protocol2MutationError{error: err, attempted: true} }
    return response, nil
}
```

- [ ] Bind raw success to `running_development`, positive generation, null active package/core/system, and empty active interfaces. Reconcile ambiguous results with status only. Migrate health, `ConfirmIdle`, `StopReady`, `Reconcile`, recovery and every Stop branch to mandatory protocol 2; preserve original operation-owner contexts and persistence recovery attribution. Delete V1 `Response`, decoder/projection and fallback once callers compile against the new interface.
- [ ] Move atomic raw staging and its failure tests out of `internal/mister`. Remove Main construction and `--runtime` selection from `mister-agent`; remove `/v1/launch`, cached-cartridge launch dispatch and old targetclient/coordinator methods. Retain package endpoints, input bridges, leases, transfer/storage primitives still used by retained workflows, and non-FPGA cast routes. Remove old `core.Spec`/`PreparedLaunch` dependencies and their dead registry implementations. Replace tests using old launch fixtures when they test shared ownership/deadline behavior.
- [ ] Add a strict config regression with a minimal native document, then each removed Main key. The first must load; every Main key must fail with its field named:

```go
func TestNativeConfigHasNoMainSettings(t *testing.T) {
    path := filepath.Join(t.TempDir(), "agent.toml")
    base := "listen_address = \"127.0.0.1:18182\"\ntoken = \"test-token\"\n"
    if err := os.WriteFile(path, []byte(base), 0600); err != nil { t.Fatal(err) }
    if _, err := agentconfig.Load(path); err != nil { t.Fatal(err) }
    for _, key := range []string{"mister_process_comm", "command_pipe", "core_name_file", "menu_rbf", "mgl_directory"} {
        if err := os.WriteFile(path, []byte(base+key+" = \"/retired\"\n"), 0600); err != nil { t.Fatal(err) }
        if _, err := agentconfig.Load(path); err == nil || !strings.Contains(err.Error(), key) {
            t.Fatalf("%s must be rejected clearly: %v", key, err)
        }
    }
}
```

- [ ] Remove those fields from Go config structs, validation, examples and `scripts/media.py` output. Remove `--runtime native` from retained native init invocation. Update media tests to parse the generated config and verify only current fields plus the intended token are provisioned; do not read or rewrite the user's private configuration.
- [ ] Run `go test ./internal/misterruntime ./internal/agent ./internal/httpapi ./internal/agentconfig ./internal/targetcache ./targetclient ./cmd/mister-agent ./protocol ./fogcast` from FogCast and `python3 -m unittest discover -s tests -p 'test_media*.py' -v` from FES. Update canonical FogCast architecture and current native diagnostic instructions. Keep Main-absence assertions; retire Main/CORENAME game smoke.

## Task 3: Close legacy runtime protocol and package entrances

**Files:** Modify `sources/libmister-runtime/src/daemon/{protocol.hpp,protocol.cpp,controller.cpp}`,
`src/native/{core_package.cpp,core_driver.cpp,hardware.cpp}`, runtime unit and
daemon integration tests; `sources/mister-packages/packages/programming/de10_nano.yaml`;
generated `src/native/generated/de10_nano_programming.hpp`; both owners' protocol
fixtures and FogCast copied fixtures.

**Interfaces:** Protocol 2 retains its package operations and contained raw
request. Protocol 1 returns a protocol-2 `unsupported_protocol` error envelope
without mutation. MiSTer profile packages remain structurally inspectable but
incompatible, with `unsupported_programming_profile` before mutation. Retain
current package state names; do not rename `running_development`.

- [ ] Add and call this regression in `tests/unit/protocol_test.cpp`:

```cpp
void TestRetiredProtocolRejected()
{
    for (const char* operation : {"status", "stop", "launch", "load_development_rbf"}) {
        const std::string request = std::string("{\"protocol\":1,\"operation\":\"") + operation + "\"}";
        ExpectError(request, ErrorCode::unsupported_protocol);
    }
    Request contained;
    assert(Parse(R"({"protocol":2,"operation":"load_development_rbf","rbf":"/tmp/fogcast-development/core.rbf","programming_profile":"development-contained-v1"})", &contained).ok());
}
```

- [ ] Build and run the focused binary: `make build/tests/unit/protocol_test` then `./build/tests/unit/protocol_test` from the runtime. Confirm the old status/stop acceptance fails the regression.
- [ ] Make protocol 2 mandatory before operation dispatch; delete V1 serialization/projection, `Operation::launch`, request `Launch` payload and legacy raw dispatch. Keep malformed JSON/duplicate fields/size limits. Update daemon tests to prove rejected V1 requests cannot call hardware.
- [ ] Remove the `mister-v1` registry row and runtime compatibility branch. Add hardware admission assertions using the existing MiSTer package fixtures: inspection returns incompatibility and an attempted load leaves generation, input and hardware calls unchanged. Keep unknown-profile parser coverage and all FES ABI/interface fixtures.
- [ ] Regenerate with `make generate`, inspect only the intended registry/fixture differences, and run `make check-generated`. Update protocol fixture producers first, then copy canonical bytes; do not hand-edit generated headers or invent a Go allowlist.
- [ ] Run runtime `make run-tests`, FogCast `go test ./internal/misterruntime ./corepackage ./protocol` and the shared package schema tests. Record the removal in runtime README and the support matrix.

## Task 4: Remove old runtime profiles and their physical plumbing

**Files:** Modify `sources/libmister-runtime/include/libmister-runtime/runtime.h`,
`src/runtime.cpp`, `src/native/{hardware.hpp,hardware.cpp,core_driver.hpp,core_driver.cpp,artifacts.hpp,artifacts.cpp,input.hpp,input.cpp,video.hpp,video.cpp,idle_recipe.hpp}`,
`src/native/linux/fpga_manager.cpp`, `src/linux/production_hardware.cpp`,
`Makefile`, and corresponding unit/support tests. Delete `src/profile.cpp`
and unused CoreLoader/SPI/framebuffer files after their callers have gone.
Leave the unused generated system headers until Task 6 removes them together
with their root generation mappings.

**Interfaces:** Keep package admission/activation, composition, core data,
FES input callback and contained raw loading. The only `ProgrammingProfile`
enum values after this task are `fes_gp_v1` and `development_contained_v1`.
Keep `InputRecipe` and generic retained-artifact/media primitives where used.

- [ ] Add assertions to production-construction/native-hardware tests for splash start/Stop, persistent package replacement and contained raw diagnostics. Use the existing fake hardware event log: contained load has no Probe/GP/video/input calls; a failed transition performs at most one idle recovery; save publication failure retains package identity and retry data. Run `make run-tests` before deletion to establish the surviving behavior baseline.
- [ ] Extract ADV-only splash/fixed video construction from `MenuVideoBringup` and the callback path from `NativeInputSession`. Remove their CoreLoader/SPI/framebuffer constructor requirements. Keep timing/ADV7513 values, audio settings, callback mapping, neutralization and deadlines unchanged.
- [ ] Delete `LaunchGame`, old `Launch`/`Profile`/`PreparedLaunch` public types, profile construction and the MiSTer driver. Remove `TransitionalMenuIdle` and MiSTer-specific FPGA bridge-release/reset branches while retaining shared FPGA-manager containment and readback ordering. Keep raw load idle-only and contained.
- [ ] Remove SNES/NES cartridge parsing, `OpenLaunchArtifacts`, cartridge `SaveFile` and SNES SRAM flush branches. Retain `CoreDataFile`, captured described-core snapshots, resume-on-save-failure behavior, `OpenRBFArtifact`, computer media snapshots and composition no-follow/digest checks.
- [ ] Update Make source/archive guards and test fixtures. Before complete-file deletion, inspect repository references with `rg -n 'CoreLoader|LinuxSpi|LinuxFramebuffer|TransitionalMenuIdle|ProductionProfiles|PreparedLaunch' sources/libmister-runtime`; each surviving reference must be removed or explained by a retained test/primitive. Replace shared lifecycle test scenarios with FES packages instead of deleting coverage.
- [ ] Run `make -j2 test` in the runtime, FogCast `go test ./internal/misterruntime ./internal/agent`, and `make check-generated` from FES. Update runtime canonical architecture and support matrix. No hardware support upgrade is claimed.

## Task 5: Use functional identity 2 for every normal FES producer

**Files:** Modify `sources/misteross/scripts/{fes_build_common.py,build_fes_pong.py,build_fes_zx81_oss.py,build_fes_coleco_oss.py,build_fes_sms_oss.py,build_fes_sg1000_oss.py,build_fes_catch.py,build_fes_demo.py}`,
`functional_execution.py` only where required for retained common behavior,
their producer tests and `test_functional_identity.py`; parent
`scripts/{recipes.py,bundle.py}`, `tests/{test_recipes.py,test_bundle.py,test_artifact_cache.py}`.

**Interfaces:** Keep registered producer module names, authentication functions,
`build`, `create_build_record`, `--root`, `--package-output` and `--cache-root`.
Where retained for the parent contract, `identity_version` defaults to 2 and
rejects every other value; `--identity-version` accepts only `2`. Demo record
construction gains `execution` and uses the same functional projection as Catch.
Package manifests and public package reader behavior do not change.

- [ ] Replace the omitted-version compatibility test in `RecipeDataTest` with strict rejection and add rejection for explicit version 1:

```python
def test_identity_version_must_be_explicitly_two(self):
    for value in (None, 1):
        document = self.document()
        if value is None:
            del document["recipes"][0]["identity_version"]
        else:
            document["recipes"][0]["identity_version"] = value
        with self.subTest(value=value), self.assertRaises(ValueError):
            self.load_document(document)
```

- [ ] Run `python3 -m unittest tests.test_recipes -v` from FES and confirm failure. Make the registry field required and exactly 2; simplify canonical record dispatch, producer invocation and cache matching to the functional route. Do not change hashes on existing artifacts or delete caches.
- [ ] In each retained producer, remove identity-1 branches, set internal defaults to 2 and ensure compiler execution always uses `FunctionalInvocation`, guarded source closure and final tool/input rechecks. Reject live standalone/nested arbitrary first-party roots; historical source-record verification remains separate.
- [ ] Migrate demo/media/audio build records to functional fields and execution evidence; change Catch's reuse of demo record construction so it supplies execution and derives its own recipe/source closure once. Keep media/audio variant constraints and existing manifest ABI/interface declarations unchanged.
- [ ] Extend existing clean monorepo fixtures in producer tests: default record format is 2; docs-only changes preserve functional ID; script/shared RTL changes alter it; compiler/source mutation invalidates sealing; demo variants and Catch keep their own IDs and output paths. Run `python3 -m unittest discover -s tests -p 'test_build_fes*.py' -v` and `python3 -m unittest discover -s tests -p 'test_functional*.py' -v` from misteross, then FES recipe/bundle/cache tests.
- [ ] Update misteross README/canonical architecture and the parent developer guide to state one normal functional producer route, with separate explicit oracle/firmware evidence. Record expected package cache invalidation.

## Task 6: Retire old bundles, wrappers and shared game definitions

**Files:** Create `sources/misteross/scripts/{quartus_tools.py,source_provenance.py}`;
modify retained FES Quartus/splash producer imports and tests, `sources/misteross/Makefile`,
`scripts/program.py`, ZX81 OSS producer/tests and Pong source lists. Remove
`scripts/{export_core_bundle.py,select_core.py,build_pong.py,fetch_core.py,core_lock.py,rebuild_core.py,legacy_source.py}` and old `cores.lock` after dependent helpers migrate.
Remove obsolete conventional Pong wrapper/constraints while retaining gameplay
RTL. Modify shared `packages/system`, `packages/source`, dead schema/emitter
branches/tests, root `scripts/{generate.py,consistency.py}` and mapped consumers.

**Interfaces:** `quartus_tools` exports the existing `RebuildError`,
`locate_quartus` and `quartus_version_line` signatures unchanged.
`source_provenance` retains `context` / `require_clean_source` behavior for the
exact tracked `sources/misteross` module, with canonical path qualification.
Current diagnostic/firmware format-1 build records remain a supported current
record schema, not a production identity choice.

- [ ] Transfer Quartus helper tests to their new module before deleting upstream rebuild code. Update each FES Quartus import and pinned-input list; run its existing Python recipe tests with fake tool invocations. Verify helpers never select/export an old game bundle.
- [ ] Rename live source provenance and update every import/pinned source list. Replace live standalone fixture acceptance with exact FES module fixtures. Preserve historical immutable-record verification and canonical SSH-to-HTTPS origin handling in `source_repository.py`.
- [ ] Preserve `cores/pong/rtl/pong_game.sv` at its existing path for this cleanup; delete the conventional top, framework staging, unused video wrapper and old bundle producer targets. Keep the gameplay simulation and FES Pong source lists pointing to the retained file. No file move is needed merely for naming.
- [ ] Remove ZX81 `--legacy`, `LEGACY_OUTPUT_RELATIVE` and OSS `socketed=False` branches. Keep a standard socket shell unconditionally in the OSS producer; preserve explicit Quartus/non-socket simulation configurations. Add a CLI rejection test before removal:

```python
def test_zx81_rejects_retired_legacy_route(self):
    from scripts import build_fes_zx81_oss as zx81
    with self.assertRaises(SystemExit) as raised:
        zx81.main(["--legacy"])
    self.assertEqual(raised.exception.code, 2)
```

- [ ] Remove Main-FIFO behavior from `program.py`; retain only separately invoked maintenance/JTAG behavior with existing ownership guidance. Delete old bundle selection/fetch/rebuild entrypoints and their exclusively obsolete tests.
- [ ] Remove old system/source YAML, eight generated system consumers, copied game-source pins and dead system/core-source emitter branches after `rg` confirms no retained producer/reference diagnostic consumes them. Update root mappings and `image/build/native-inputs.toml` Mega Drive pin together. Keep all board/register/current ABI fixtures and generic format-2 package syntax coverage.
- [ ] Run `make generate`, `make check-generated`, shared `make test` with its prepared Python dependencies, producer/exporter tests and `make sim-fes-pong sim-fes-zx81 sim-fes-demo` from misteross. Run the affected simulator closure for any shared RTL changes; do not invoke Quartus or full FPGA placement for this software gate. Update component current documentation and root mapping tests.

## Task 7: Remove Main image variants without breaking native provisioning

**Files:** Modify `image/Makefile`, `image/scripts/{build-target-image.sh,verify-target-image.sh,qemu-smoke-target-image.sh,target-image-container.sh,build-target-kernel.sh}`;
native defconfig/post-build/overlay wiring and image tests. Delete Main-only
`fogcast_target_{prod,dev}_defconfig`, `post-build.sh`, `S40mister-main`,
`S49fogcast-target-smoke`, `mister-disable-menu-blanking` and Main-specific agent
overlay after native references are removed. Preserve shared network/SSH/
supervisor files. Update root image recipe tests and relevant FogCast build
inputs/selector tests if their assumptions include old variants.

**Interfaces:** Keep `native-dev` and parent `native-integration-dev` names,
package-only environment, media/release formats and current seals. Repoint
kernel compiler use to native `work-2-native-dev/host`; the kernel target
depends on the retained native image/toolchain target instead of `target-images`.

- [ ] Extend `target-image_test.sh` / `target-kernel_test.sh` to assert rejected `prod`, `dev` and `--fast-dev` requests and kernel use of `work-2-native-dev`. Use the existing fake container/make harness, not a real image build. Run the two scripts and confirm old accepted variants/dependencies fail the new expectations.
- [ ] Remove old Make targets and variant branches. Flatten native overlay composition so it includes needed common network/mount/supervisor services without copying then deleting Main services. Keep the current native SSH policy; do not inherit the removed `prod` policy accidentally.
- [ ] Change kernel compiler paths/dependencies and preserve pinned kernel source, config digest, release, modules and U-Boot/media seals. Remove obsolete per-system cache creation from native agent init. Confirm Task 2's generated config and removed `--runtime` flag agree with installed service arguments.
- [ ] Remove tests exclusively exercising old image variants; retain/rewrite their shared path validation, reproducibility, failure cleanup and source-lock checks against native-dev. Preserve explicit image assertions forbidding Main/MGL/FIFO services.
- [ ] Run `make -C image test` with documented container prerequisites, then FES image/media/appliance Python tests. Report any prerequisite failure explicitly; do not substitute a previous rootfs as proof of new image packaging. Update `docs/image-assembly.md`, development/media guides and project-map descriptions.

## Task 8: Integrate, review and hand off the uncommitted cleanup

**Files:** Reconcile `README.md`, `docs/{project-map.md,component-boundaries.md,core-packages.md,core-development.md,core-persistence.md,test-changed.md}`,
component canonical documentation/support matrix, `scripts/affected.py`,
`scripts/ci_simulations.py` and associated coverage tests. Update this plan's
checkboxes with actual results and remaining integration gates.

**Interfaces:** No new product contracts. All first-party consumers agree on
native package-only execution, local protocol 2, retained ABI registry and
functional producer identity 2. Existing historical evidence stays historical.

- [ ] Inspect the complete diff and search remaining production code for `runtimeMain`, `PreparedLaunch`, `TransitionalMenuIdle`, `mister-v1`, protocol-1 construction, old bundle exports and Main init. Classify remaining hits as negative tests, history/provenance or an actual missed production caller. Remove missed callers; do not add compatibility shims to pass old tests.
- [ ] Update affected-file rules and source-list coverage tests for removed/renamed producer scripts, generated mappings and simulations. Run `python3 scripts/test_changed.py --base 2d54817cdd6ee40922f99d4fc4d4383445528bf1 --plan-only`, inspect selected consumer coverage, then run the same command without `--plan-only`.
- [ ] Run `make check-generated` and `git diff --check`. Confirm generated fixture bytes match their canonical owner, all surviving package families remain represented, non-FPGA host execution is unchanged and no user data/artifact cache was removed.
- [ ] Perform a whole-diff independent review using the selected execution method's review skill; resolve findings and rerun only affected checks. Verify final source status against the base and preserve all unrelated changes.
- [ ] Return the uncommitted worktree diff with actual software checks, contract effects and known hardware limitations. Do not claim parent `make check`/image acceptance for unstaged edits. No commit/push/PR is performed by this plan.
- [ ] Record the next integration gate: after the user authorizes committing the reviewed changes, run committed-source consistency and a fresh native image build because packaging changed. Then use one designated-kit lease for exact-runtime/package splash, launch/media/input, replacement, Stop/relaunch and contained diagnostic recovery. Reserve cold two-pass image/media acceptance for the stabilized integrated artifact; never transfer old acceptance to new IDs.

## Execution choice

Recommended: **native execution**, one implementer retaining the cross-module
interface context, followed by a fresh whole-diff reviewer. Subagent-driven
execution is also possible with sequential task handoffs and fresh reviews;
it adds review gates and repeated context for these coupled changes.

The user approved the design and execution. Implementation uses one FES worktree,
with bounded component workstreams and an independent whole-diff review. Changes
remain uncommitted; hardware acceptance is separate.
