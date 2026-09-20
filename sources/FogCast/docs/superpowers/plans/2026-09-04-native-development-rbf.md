# Native Development RBF Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Load a raw MiSTer-compatible development RBF through FogCast's existing public endpoint on the native runtime, Stop safely to idle, and retain normal catalogue launch support.

**Architecture:** Keep the current host -> target agent -> Unix-socket runtime chain and the runtime as sole FPGA owner. The target atomically stages one bounded upload; the native runtime quiesces HDMI, programs and synchronizes the core exactly once; Status reconstructs development ownership without inventing game or system identity or replaying the upload.

**Tech Stack:** Go, C++17, Unix-domain JSON protocol v1, Buildroot, ARMv7 cross-compilation, shell image fixtures, QEMU smoke tests, and the designated MiSTer Pi plus ShadowCast capture path.

**Spec:** `docs/superpowers/specs/2026-09-04-native-development-rbf-design.md`

## Global Constraints

- Keep the existing `POST /api/v1/session/development-rbf` and `POST /v1/development/rbf` endpoints. The body remains `application/octet-stream` with a known length from 1 byte through 32 MiB.
- Support only the existing MiSTer-compatible development ABI. Do not add an ABI selector, browser picker, non-MiSTer support, or a custom-core abstraction.
- Keep `mister-runtime` as the sole native FPGA programmer. The Go agent never writes FPGA, SPI, or HDMI devices directly.
- A successful development load has no game or system identity. The runtime may report a non-empty observed core independently of a null system.
- Add no catalogue row, profile, media role, video mode, input map, save path, or settings map for development execution.
- Quiesce HDMI before programming and leave it down until Stop reloads the locked idle RBF.
- Dispatch the upload and runtime mutation at most once. Resolve ambiguous responses with Status only; never replay bytes or the mutation request.
- Replace an active native game only after normal Stop reaches authoritative idle. Block catalogue launch while development remains active.
- Preserve conventional Main development loading and its reboot-required recovery.
- Allow host/agent restart to reconstruct `running_development`. Runtime restart establishes idle and never replays an upload.
- Package no development RBF. The upstream Mega Drive build is a physical fixture, not a production RBF.
- Use strict TDD for every production change: genuine focused RED, minimal GREEN, deliberate load-bearing mutation, restored GREEN, then proportional full gates.
- Do not claim hardware support until the exact reproducible image passes Task 8.

---

### Task 1: Complete the native hardware and lifecycle operation

**Repository:** `/home/deano/fes/libmister-runtime`

**Files:**
- Modify: `src/native/hardware.cpp:217`
- Modify: `src/runtime.cpp:205-253`
- Modify: `tests/unit/native_hardware_test.cpp:900-1035`
- Modify: `tests/unit/runtime_test.cpp:350-380`
- Modify: `ARCHITECTURE.md:60-75`
- Modify: `docs/support-matrix.md:1-45`

**Interfaces:**
- Consumes: existing `VideoBringup::Quiesce(uint64_t)`, `FpgaManager::Program(const Artifact&, uint64_t)`, `CoreLoader::Synchronize(uint64_t)`, and `CoreLoader::Probe(std::string*, uint64_t)`.
- Produces: `NativeHardware::LoadDevelopmentRBF(const std::string&) -> HardwareResult` with optional observed core; lifecycle state `running_development`, execution `development`, empty system, and optional non-empty core observation.

- [ ] **Step 1: Create an isolated runtime worktree**

```bash
git -C /home/deano/fes/libmister-runtime fetch origin
git -C /home/deano/fes/libmister-runtime worktree add \
  /home/deano/fes/libmister-runtime/.worktrees/native-development-rbf \
  -b feat/native-development-rbf origin/main
```

Read `AGENTS.md`, `README.md`, `ARCHITECTURE.md`, and `docs/support-matrix.md` before editing.

- [ ] **Step 2: Write the exact-order hardware RED**

In `native_hardware_test.cpp`, require:

```cpp
const mister::HardwareResult result =
    fixture.hardware.LoadDevelopmentRBF(fixture.rbf);
assert(result.error.ok());
assert(result.mutation_attempted);
assert(result.observed_core == "MegaDrive");
assert(fixture.events == std::vector<std::string>({
    "artifact.open:megadrive.rbf",
    "video.quiesce",
    "fpga.program",
    "core.sync",
    "core.probe:MegaDrive",
}));
assert(fixture.idle_video.calls == 0);
```

Also assert quiesce uses `now + video_ms`, programming uses `now + program_ms`, and synchronize plus probe share one fresh `now + core_io_ms` deadline.

- [ ] **Step 3: Run the RED**

```bash
make -j4 build/native_hardware_test
./build/native_hardware_test
```

Expected: FAIL because production currently jumps directly from artifact open to programming and returns no observation.

- [ ] **Step 4: Add failure-boundary REDs**

Cover preflight failure with zero hardware work; quiesce deadline/select/write failure; program failure; synchronize failure; and probe failure. Assert mutation is false before a quiesce write, reflects `quiesced.mutation_attempted` at the quiesce boundary, and is true after successful quiesce/program. At the lifecycle layer, inject each post-mutation failure and require exactly one `LoadIdle` cleanup; require retained primary error after successful cleanup and `idle_failed`/`reboot_required` when cleanup fails. Prove development never calls idle/fixed video bring-up, input Open/Neutralize/Start, reset assertion/release, initial status, or media attach.

- [ ] **Step 5: Implement the minimal hardware sequence**

Use this production structure:

```cpp
Artifact artifact;
Error error = OpenRBFArtifact(rbf, opener_, &artifact);
log_.Write({"load_development_rbf", "", "", "preflight", error});
if (!error.ok()) return {error, false, ""};

const VideoQuiesceResult quiesced = game_video_.Quiesce(
    Deadline(clock_, timeouts_.video_ms));
error = quiesced.error;
log_.Write({"load_development_rbf", "", "", "hdmi_quiesce", error});
if (!error.ok()) return {error, quiesced.mutation_attempted, ""};

const NativeResult programmed = fpga_.Program(
    artifact, Deadline(clock_, timeouts_.program_ms));
error = programmed.error.ok() ? Error{} : ProgramError(programmed.error);
log_.Write({"load_development_rbf", "", "", "program", error});
if (!error.ok())
    return {error, quiesced.mutation_attempted ||
        programmed.mutation_attempted, ""};

const std::uint64_t core_deadline =
    Deadline(clock_, timeouts_.core_io_ms);
error = core_.Synchronize(core_deadline);
if (!error.ok()) error = CoreIoError(error);
log_.Write({"load_development_rbf", "", "", "sync", error});
if (!error.ok()) return {error, true, ""};

std::string observed;
error = core_.Probe(&observed, core_deadline);
if (!error.ok()) error = CoreIoError(error);
log_.Write({"load_development_rbf", "", observed, "probe", error});
return {error, true, observed};
```

Do not call a video BringUp method or any game/profile/input operation.

- [ ] **Step 6: Preserve observed core in lifecycle status**

In the success branch of `Runtime::LoadDevelopmentRBF`:

```cpp
status_ = {};
status_.state = State::running_development;
status_.execution = Execution::development;
status_.core = result.observed_core;
busy_ = false;
```

Update `TestDevelopmentHasNoGameIdentity` to set a fake observation and require empty system plus core `MegaDrive`.

- [ ] **Step 7: Run GREEN and three mutations**

```bash
make -j4 build/native_hardware_test build/runtime_test
./build/native_hardware_test
./build/runtime_test
```

Separately omit HDMI quiesce, omit synchronization, and clear the successful observation. Each mutation must fail its intended assertion. Restore and rerun GREEN.

- [ ] **Step 8: Update runtime docs without claiming hardware acceptance**

Document the software order as open -> HDMI power-down -> program -> synchronize -> optional observation -> `running_development` with HDMI down. In the support matrix, record a separate MiSTer-compatible development capability as software implemented and physical status pending. Do not add a system row or alter the hardware-supported-system count.

- [ ] **Step 9: Run full runtime gates and commit**

```bash
make -j4 test
make sanitize
make tsan
make archive-audit
make active-tree-test
make profile-provenance-test
scripts/check-history.sh
git diff --check
git status --short
```

Run the repository's pinned ARM target build and target archive audit. Then:

```bash
git add src/native/hardware.cpp src/runtime.cpp \
  tests/unit/native_hardware_test.cpp tests/unit/runtime_test.cpp \
  ARCHITECTURE.md docs/support-matrix.md
git commit -m "feat: load native development RBFs"
```

---

### Task 2: Review and merge the runtime dependency

**Repository:** `/home/deano/fes/libmister-runtime/.worktrees/native-development-rbf`

**Files:**
- Review: the exact one-commit range from `origin/main` to Task 1
- Modify only when an evidence-backed review finding has a focused RED

**Interfaces:**
- Consumes: Task 1's exact commit and gate evidence.
- Produces: the canonical merged runtime SHA consumed by FogCast's immutable lock.

- [ ] **Step 1: Freeze the exact review range**

```bash
base=$(git merge-base HEAD origin/main)
test "$(git rev-list --count "$base..HEAD")" -eq 1
git diff --check "$base..HEAD"
git status --short --branch
git diff --binary "$base..HEAD" > /tmp/native-development-rbf-runtime.diff
sha256sum /tmp/native-development-rbf-runtime.diff
```

- [ ] **Step 2: Obtain independent review**

Require review of ordering, deadlines, mutation attribution, cleanup, observation semantics, absence of profile/video/input work, and truthful support status. Accepted findings require strict RED/GREEN and an amended single commit.

- [ ] **Step 3: Re-run exact-head verification**

Repeat every Task 1 test, sanitizer, audit, ARM build, mutation guard, clean-tree check, and independent review on the final hash.

- [ ] **Step 4: Open the focused runtime PR after user authorization**

```bash
git push -u origin feat/native-development-rbf
gh pr create --base main --head feat/native-development-rbf \
  --title "feat: load native development RBFs" \
  --body-file /tmp/native-development-rbf-runtime-pr.md
```

State software support only and name the pending physical gate. Wait for Bugbot and GitGuardian; verify findings before changing code.

- [ ] **Step 5: Merge after green checks and explicit authorization**

Use the repository's merge-commit convention, delete the remote feature branch, fast-forward local runtime `main`, and verify local HEAD, local main, `origin/main`, and remote main are the same clean commit. Record that full SHA for Task 7.

---

### Task 3: Add the typed native client request and shared staging seam

**Repository:** `/home/deano/fes/FogCast-POC/.worktrees/native-development-rbf-design`

**Files:**
- Modify: `internal/misterruntime/client.go:45-120,245-285`
- Modify: `internal/misterruntime/client_test.go:1-130,190-240`
- Modify: `internal/mister/atomic.go:35-70`
- Modify: `internal/mister/runtime.go:141-155`
- Modify: `internal/mister/runtime_test.go:100-180`

**Interfaces:**
- Consumes: protocol-v1 `load_development_rbf` and existing atomic staging.
- Produces: `Control.LoadDevelopmentRBF(context.Context, string) (Response, error)` and `mister.WriteAtomicDevelopmentRBF(string, int64, io.Reader) error`.

- [ ] **Step 1: Write the typed client RED**

Add this interface method and a real Unix-socket fixture:

```go
type Control interface {
    Status(context.Context) (Response, error)
    Launch(context.Context, LaunchRequest) (Response, error)
    LoadDevelopmentRBF(context.Context, string) (Response, error)
    Stop(context.Context) (Response, error)
}
```

Require the exact request:

```json
{"protocol":1,"operation":"load_development_rbf","rbf":"/tmp/fogcast-development/core.rbf"}
```

Reject empty, relative, unclean, NUL-containing, and overlong paths before dialing.

- [ ] **Step 2: Run the RED**

```bash
go test ./internal/misterruntime -run 'TestClient.*Development' -count=1
```

Expected: compile failure because the method does not exist.

- [ ] **Step 3: Implement exact request encoding and response validation**

Factor only the common socket send/receive code needed to encode Status/Stop, Launch, and development request shapes. Preserve launch validation, unknown-field rejection, response size, deadlines, and cancellation.

For `running_development` accept only:

```go
response.Execution == "development"
response.System == nil
response.Core == nil || *response.Core != ""
```

Reject non-null system, empty core, game identity, and malformed state/error shapes.

- [ ] **Step 4: Export the atomic writer without changing conventional behavior**

Rename it to:

```go
func WriteAtomicDevelopmentRBF(
    path string, size int64, content io.Reader,
) error
```

Update the Main runtime call. Test short-body cleanup, old installed-file preservation on failure, successful fsync/close/rename, final `0600` mode, and no remaining `.new` file.

- [ ] **Step 5: Run GREEN, race, and mutations**

```bash
go test ./internal/misterruntime ./internal/mister -count=1
go test -race ./internal/misterruntime ./internal/mister -count=1
```

Mutate the wire operation, relax absolute-path validation, and install before Sync/Close. Each must fail; restore GREEN.

- [ ] **Step 6: Commit**

```bash
git add internal/misterruntime/client.go internal/misterruntime/client_test.go \
  internal/mister/atomic.go internal/mister/runtime.go \
  internal/mister/runtime_test.go
git commit -m "feat: add native development RBF request"
```

---

### Task 4: Implement owned native loading and restart reconstruction

**Files:**
- Modify: `internal/misterruntime/runtime.go:15-55,120-235`
- Modify: `internal/misterruntime/runtime_test.go:175-260,1390-1555`
- Modify: `cmd/mister-agent/main.go:25-115`
- Modify: `cmd/mister-agent/main_test.go:35-90,480-535`

**Interfaces:**
- Consumes: Task 3's control method and atomic writer.
- Produces: `WithDevelopmentRBFPath(string) RuntimeOption`, `LoadDevelopmentRBFOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader)`, development-aware `Reconcile`, and development-aware `StopReady`.

- [ ] **Step 1: Write adapter admission/staging REDs**

Construct with:

```go
runtime := misterruntime.NewRuntime(
    control, bootIDFile, time.Millisecond, 25*time.Millisecond,
    misterruntime.WithDevelopmentRBFPath(stagedPath),
)
```

Require exact staged bytes, one idle admission Status, one development request, observation `MegaDrive`, and no launch/stop request. Add invalid size/path and canceled-admission tests that do not read the body or dial.

- [ ] **Step 2: Write lost-response and restart REDs**

Use real Unix sockets for: lost response followed by `starting/development -> running_development`; lost response followed by idle with retained primary error; exhausted observation followed by one fresh health-bound Status window rooted in process lifetime; process shutdown cancellation; caller cancellation before admission; agent restart reconstruction; and runtime restart observed as idle with zero replay.

- [ ] **Step 3: Run the RED**

```bash
go test ./internal/misterruntime ./cmd/mister-agent \
  -run 'Development|Reconcile|StopReady|NativeComposition' -count=1
```

- [ ] **Step 4: Add optional staging-path configuration**

```go
type RuntimeOption func(*Runtime)

func WithDevelopmentRBFPath(path string) RuntimeOption {
    return func(runtime *Runtime) {
        runtime.developmentRBFPath = path
    }
}

func NewRuntime(control Control, bootIDFile string,
    pollInterval, healthTimeout time.Duration,
    options ...RuntimeOption) *Runtime
```

Validate clean absolute NUL-free path before reading. Pass the existing `/tmp/fogcast-development/core.rbf` constant from native agent composition only.

- [ ] **Step 5: Implement one private load flow**

Expose both caller-bound and owned methods. Order: validate -> stage atomically under admission -> exact idle Status -> one control dispatch -> direct validation or Status-only reconciliation. Treat `starting/development` and clean idle as provisional within the observation budget; `running_development` as success; idle with retained error as terminal mapped failure; and game/wrong identity as terminal unavailable.

- [ ] **Step 6: Extend reconstruction and Stop admission**

Add `validDevelopmentStarting` and `validDevelopmentRunning`. Map running development to public `StateActive` plus `Development: true` and optional `ObservedCore`. Keep `Health.Ready` idle-only. Let `StopReady` accept exact idle, running Mega Drive, or running development. Never admit normal Launch from development.

- [ ] **Step 7: Run GREEN and mutations**

```bash
go test ./internal/misterruntime ./cmd/mister-agent -count=1
go test -race ./internal/misterruntime ./cmd/mister-agent -count=5
```

Prove no replay, caller-bound pre-admission, process-bound post-admission, strict null-system identity, retained idle error is not success, and running development is not healthy-ready.

- [ ] **Step 8: Commit**

```bash
git add internal/misterruntime/runtime.go internal/misterruntime/runtime_test.go \
  cmd/mister-agent/main.go cmd/mister-agent/main_test.go
git commit -m "feat: run native development RBF loads"
```

---

### Task 5: Coordinate native development ownership and Stop

**Files:**
- Modify: `internal/agent/coordinator.go:15-45,90-150,280-430`
- Modify: `internal/agent/coordinator_test.go:20-180,340-410,1100-1210`
- Modify: `internal/httpapi/server_test.go:115-170`

**Interfaces:**
- Consumes: Task 4's owned load method and development-aware Stop/Reconcile.
- Produces: capability-selected native development ownership while preserving conventional reboot recovery.

- [ ] **Step 1: Define the narrow capability in failing tests**

```go
type ownedDevelopmentRuntime interface {
    LoadDevelopmentRBFOwned(
        context.Context, context.Context, context.Context,
        int64, io.Reader,
    ) (string, bool, *protocol.APIError)
}
```

The fake records admission, bounded observation, and process owner separately.

- [ ] **Step 2: Add ownership/reconstruction REDs**

Require zero read/dispatch on canceled admission, caller cancellation not canceling admitted mutation, process shutdown cancellation, exact development identity, one dispatch under ambiguous response, and Initialize bypassing stale catalogue active records for a reconstructed development status.

- [ ] **Step 3: Add native-versus-conventional Stop REDs**

A runtime with `ownedDevelopmentRuntime` must receive normal `StopOwned` once and finish idle. A runtime without it must preserve exact `stopping + development + reboot_required` and the separate `RebootDevelopment` call.

- [ ] **Step 4: Run RED**

```bash
go test ./internal/agent ./internal/httpapi \
  -run 'Development|Owned|Reconcile|Stop' -count=1
```

- [ ] **Step 5: Implement capability selection**

For native loading, create `context.WithTimeout(c.operationContext, c.launchTimeout)` for observation and pass the unshortened process context as operation owner. Preserve the existing caller-bound call for conventional runtimes. Return valid development status before durable game-record reconciliation. Enter routine reboot-required Stop only when development is active and the runtime lacks the native owned-development capability.

- [ ] **Step 6: Run GREEN, race, and mutations**

```bash
go test ./internal/agent ./internal/httpapi ./cmd/mister-agent -count=1
go test -race ./internal/agent ./internal/httpapi ./cmd/mister-agent -count=10
```

Mutate native loading back to caller-owned, make native Stop reboot, and feed development into game-record reconciliation. Each must fail; restore GREEN.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/coordinator.go internal/agent/coordinator_test.go \
  internal/httpapi/server_test.go
git commit -m "feat: coordinate native development sessions"
```

---

### Task 6: Replace an active game and reconcile public results

**Files:**
- Modify: `internal/hostapi/session.go:275-440,590-645`
- Modify: `internal/hostapi/server_test.go:450-680`
- Modify: `fogcast/service.go:450-540,1020-1060,1150-1225`
- Modify: `fogcast/service_test.go:2210-2350,3160-3190`
- Modify: `host/client_test.go:80-185`

**Interfaces:**
- Consumes: public session serialization, `sessionService.Stop`, target Status, and Task 5 native development status.
- Produces: authoritative game-to-development replacement and bounded Status-only recovery for ambiguous uploads.

- [ ] **Step 1: Write active-game replacement RED**

Start from an active native game and require: input detach -> media stop -> service Stop to exact idle -> one development upload -> active `fpga_development`. If Stop errors, returns non-idle, or the parent cancels, assert the upload body is unread and target development count is zero. Start from an active development session and require `BUSY` before input/media teardown or body read.

- [ ] **Step 2: Write ambiguity REDs**

Use a real HTTP target fixture that accepts upload and mutation once but loses or delays the response. Each Status call remains bounded by `requestTimeout` and the original public parent bounds the loop. Exact active development becomes success; idle with retained operation error becomes canonical failure; wrong terminal state becomes unavailable; caller cancellation stops observation. Target upload count must remain one.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/hostapi ./fogcast ./host \
  -run 'Development|SessionReplace|LostResponse' -count=1
```

- [ ] **Step 4: Implement Stop-before-upload**

Before teardown, call `developmentActive(ctx)` and return the existing development-must-stop `BUSY` error when true. Otherwise, after input/media detach, call the existing service Stop when `previousExecution == fogcast.ExecutionFPGANative` and require `protocol.StateIdle` before passing the body to `LoadDevelopmentRBF`.

- [ ] **Step 5: Implement bounded Status-only upload reconciliation**

In `fogcast.Service.LoadDevelopmentRBF`, reconcile only when the target deadline expires while the public parent remains live and the error is an ambiguous mutation error. Poll Status with a fresh `requestTimeout` per call and the original parent overall. Reuse exact development-status validation and never invoke LoadDevelopmentRBF twice.

- [ ] **Step 6: Preserve restart and launch gates**

Test host restart reconstruction, native Stop clearing ownership, conventional reboot handshake unchanged, runtime restart clearing development to idle, and catalogue launch returning BUSY until explicit Stop.

- [ ] **Step 7: Run GREEN and mutations**

```bash
go test ./internal/hostapi ./fogcast ./host -count=1
go test -race ./internal/hostapi ./fogcast ./host -count=5
```

Mutate away Stop-before-upload, accept non-idle Stop, replay upload, and admit catalogue launch during development. Each must fail; restore GREEN.

- [ ] **Step 8: Commit**

```bash
git add internal/hostapi/session.go internal/hostapi/server_test.go \
  fogcast/service.go fogcast/service_test.go host/client_test.go
git commit -m "feat: replace games with native development sessions"
```

If tests are moved to `host/development_client_test.go`, stage that file instead and remove no active test copy.

---

### Task 7: Pin runtime and keep the native image development-artifact-free

**Files:**
- Modify: `build/native-runtime.inputs.lock.toml:4`
- Modify: `scripts/tests/native-runtime-inputs_test.sh`
- Modify: `scripts/tests/native-runtime-smoke_test.sh:174`
- Modify: `scripts/tests/target-image-rootfs_test.sh`
- Modify: `scripts/tests/target-image_test.sh`
- Modify: `README.md:55-75`
- Modify: `docs/ARCHITECTURE.md:50-75,140-215`
- Modify: `docs/superpowers/specs/2026-09-04-native-development-rbf-design.md:1-5`

**Interfaces:**
- Consumes: Task 2 canonical runtime SHA and Tasks 3-6.
- Produces: immutable native inputs using the new runtime while packaging no development RBF.

- [ ] **Step 1: Update fixture expectations first and capture RED**

```bash
runtime_commit=$(git -C /home/deano/fes/libmister-runtime rev-parse origin/main)
test "$(printf '%s' "$runtime_commit" | wc -c)" -eq 40
```

Change only the two authoritative fixture expectations, leaving the lock stale. Run `native-runtime-inputs_test.sh` and `native-runtime-smoke_test.sh`; both must fail at the intended mismatch.

- [ ] **Step 2: Update only the runtime lock**

Replace `[mister_runtime].commit` with the exact merge SHA. Leave idle and production Mega Drive rows byte-identical. Rerun both fixtures and the real locked-input verifier against a clean detached checkout.

- [ ] **Step 3: Add image absence tests**

Rootfs and structural fixtures must reject development artifacts at `/usr/share/mister-runtime/development.rbf`, `/usr/share/mister-runtime/cores/development.rbf`, and `/tmp/fogcast-development/core.rbf`. Require volatile staging configuration and exactly the locked idle plus production Mega Drive RBF inventory.

- [ ] **Step 4: Update proposed docs**

Describe implemented behavior but retain physical-acceptance-pending language. State HDMI remains down during development, raw upload has no video/input guarantee, browser picker is absent, and the fixture is not production-pinned. Change spec status to `Approved design; implementation complete, physical acceptance pending`.

- [ ] **Step 5: Run full software gates**

```bash
make -j4 test
make vet
git diff --check
```

Validate each changed shell file with its declared interpreter. Run focused race repetition for `internal/misterruntime`, `internal/agent`, `internal/hostapi`, `fogcast`, `host`, and `cmd/mister-agent`.

- [ ] **Step 6: Run integration mutations and commit**

Reject stale runtime lock, packaged development RBF, fake development system, upload replay, routine native reboot, and game launch before explicit development Stop. Restore GREEN and commit the coherent FogCast candidate.

---

### Task 8: Reproducible build, physical acceptance, documentation, and integration

**Files:**
- Create after PASS: `docs/hardware/native-development-rbf-baseline.md`
- Create after PASS: `scripts/tests/native-development-rbf-support-truth_test.sh`
- Modify after PASS: `README.md`
- Modify after PASS: `docs/ARCHITECTURE.md`
- Modify after PASS: `docs/DEVELOPMENT.md`
- Modify after PASS: `docs/superpowers/specs/2026-09-04-native-development-rbf-design.md`
- Modify after PASS: `Makefile`
- Modify after FogCast baseline is live: `/home/deano/fes/libmister-runtime/docs/support-matrix.md`

**Interfaces:**
- Consumes: exact reviewed FogCast candidate, merged runtime, immutable lock, and upstream fixture.
- Produces: reproducible images, twice-stable physical evidence, truthful support docs, merged FogCast implementation, and runtime documentation follow-up.

- [ ] **Step 1: Obtain independent review**

Freeze the exact FogCast range and review protocol, staging, contexts, state mapping, Stop distinction, host replacement, pin, and image absence. Accepted findings require strict RED/GREEN and all Task 7 gates rerun.

- [ ] **Step 2: Build all release images twice**

Run canonical source fetch, then two network-isolated builds each for `native-dev`, legacy `prod`, and legacy `dev`. Require pass-1/pass-2 byte identity, independent size/SHA checks, structural verification, clean filesystems, exact input manifests, and QEMU smoke.

- [ ] **Step 3: Seal a generic immutable acceptance manifest**

Bind exact FogCast/runtime revisions; native/legacy image hashes; installed host/agent/runtime/idle/Mega Drive/build-input hashes; upstream fixture size/hash; boot and counter baseline; ShadowCast `KT044001` node; and helper/template hashes. Helpers consume this manifest and contain no copied iteration number or prior 64-hex identity.

- [ ] **Step 4: Deploy native once and prove idle**

Run unchanged native smoke. Prove exact boot/hash/process/FPGA/input state and five inspected idle frames using `ffmpeg -nostdin`. Use no receiver or ADV reset.

- [ ] **Step 5: Run development-to-game cycle 1 without retries**

```text
upload /home/deano/fes/misteross-rebuild/build/current/megadrive.rbf once
-> staged size 4,306,912
-> staged SHA-256 195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e
-> public fpga_development and runtime running_development
-> null game/system/expected-core; record optional observed core
-> ADV7513 main output powered down
-> one Stop to exact visible idle
-> one canonical pinned Sonic 2 launch
-> fresh title gate, one Start, five gameplay frames
-> one right/jump sequence with visible response
-> one detach and one Stop to visible idle
```

Stop and preserve on first failure. Do not replay any upload, Launch, Stop, or input action.

- [ ] **Step 6: Repeat the full cycle once on the same boot**

Require exact cumulative counts of two development loads, two catalogue launches, two development Stops, two game Stops, and matching input attach/stream/detach. No reboot, receiver reset, ADV reset, retry, or manual correction.

- [ ] **Step 7: Roll back and smoke legacy once**

Deploy the exact reproducible legacy-dev image once, confirm a distinct ready boot, then run unchanged Sonic 2 -> Mega Drive -> Stop -> Menu once. Verify Main/FIFO/Menu, exact image, FPGA, visible Menu, and no native processes.

- [ ] **Step 8: Publish evidence-backed docs and truth test**

Only after PASS, write the hardware baseline with exact commits/images/fixture/boots/counters/status/HDMI/two cycles/game regression/rollback. Present-tense docs must state narrow MiSTer-compatible support, no generic video/input guarantee, no packaged fixture, no browser picker, and no generalized ABI.

The support-truth test must fail on zero cycles, hardware=no, stale image/runtime hashes, a packaged development artifact, or a generic ABI/video claim.

- [ ] **Step 9: Run final verification and mutations**

```bash
make -j4 test
make vet
sh scripts/tests/native-development-rbf-support-truth_test.sh
git diff --check
git status --short
```

Mutate cycle count to zero, hardware status to no, and accepted image hash to stale. Each must fail; restore GREEN and commit the evidence/docs.

- [ ] **Step 10: Review and integrate FogCast**

Obtain fresh independent review. After user authorization, push and open the FogCast PR. Verify Bugbot/GitGuardian findings. Any accepted production change invalidates images and requires the relevant rebuild and physical rerun. Merge only when the exact head is green and the user authorizes it; delete the remote branch.

- [ ] **Step 11: Publish runtime hardware status after FogCast is live**

Update runtime support matrix from software-only to hardware-tested for this narrow capability, link the merged FogCast baseline, keep hardware-supported game-system count 1, and exclude generic video/input/non-MiSTer ABIs. Run full runtime gates, review, and user-authorized docs-only PR integration.

- [ ] **Step 12: Final audit**

Verify both repositories match clean `origin/main`, feature branches are absent, target is on exact accepted legacy Menu image, capture/input/helpers are released, and evidence checksums validate. Report merge SHAs, image hashes, fixture hash, two-cycle result, legacy result, and deferred browser/generalized-ABI work.
