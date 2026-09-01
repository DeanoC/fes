# Bootable Native MiSTer Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and physically accept a separate FogCast `native-dev` image in
which `mister-runtime` is the sole FPGA owner, loads one pinned idle RBF, and
drives honest target readiness through an explicitly selected native agent
backend while the working legacy images remain unchanged.

**Architecture:** First make `libmister-runtime` construct its existing native
Linux primitives around the packaged idle path and land that repository change.
Then add a bounded Go Unix-socket client and native `agent.Runtime` adapter,
pin the reviewed runtime commit and official idle artifact as image inputs, and
assemble a third Buildroot image with runtime-before-agent service ordering.
Only reviewed commits proceed to the designated Pi, where idle, stop, reboot,
HDMI output, and reinstalling the legacy game-launch image are recorded.

**Tech Stack:** C++14, GNU/Apple Make, POSIX/Linux MMIO and FPGA manager, Go,
Unix domain sockets with bounded newline JSON, Buildroot 2021.02.4, POSIX shell,
Docker-compatible image builder, ARM EABI5 hard-float Linux, MiSTer Pi hardware

**Spec:**
`../specs/2026-09-01-bootable-native-baseline-design.md`

## Global constraints

- Land the approved design before implementation. Commit
  `aa756dda02c0ea741b393c6756012b5c39f10ab1` is the approved base; the plan
  review adds only the explicit runtime-before-agent readiness ordering and
  the already-idle stop behavior recorded in that same design file.
- Start runtime work from the reviewed `libmister-runtime` main baseline
  `d6e7ec2db1049a0d6bd9edfd44a233ac174729f9` plus any later user-approved main
  commits pulled before branching.
- Use an isolated worktree for each repository implementation branch. Do not
  edit the user's main checkout or reuse the documentation branch for code.
- Finish, independently review, merge, and pull the runtime change before
  recording its exact commit in FogCast.
- Existing FogCast `dev` and `prod` images remain conventional Main images.
  Their commands, defconfigs, init behavior, and verification expectations do
  not change.
- The new image and output name is exactly `native-dev` and
  `build/output/target-image/native-dev/linux.img`.
- The native image contains image-owned `/usr/sbin/mister-runtime`,
  `/usr/sbin/mister-agent`, and
  `/usr/share/mister-runtime/idle.rbf`.
- The initial idle artifact remains pinned to
  `MiSTer-devel/Distribution_MiSTer` commit
  `f7bde4becb452ca28f604ad9802bbed5c6b58e01`, path `menu.rbf`, SHA-256
  `821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934`,
  and size `2452588` bytes.
- FogCast receives a clean `libmister-runtime` checkout as an explicit
  read-only container mount. Do not add a submodule or copy its source.
- Agent backend selection is exactly `--runtime main|native`, with `main` as
  the default so the legacy init command remains unchanged.
- Runtime process ordering precedes agent process ordering. The agent reports
  ready only from a successful runtime `idle` status; process or socket
  existence alone is never readiness.
- Production profiles remain empty and every system-support row remains
  `software: no`, `hardware: no`. This milestone supports no game system.
- Native normal-game and development-RBF requests fail before runtime or FPGA
  mutation. Do not add a partial launch path.
- Do not add Main fallback, backend autodetection, automatic image switching,
  persistent state, retries beyond existing bounded readiness polling,
  authentication, attestation, a coordinator, or a recovery framework.
- QEMU proves packaging and init behavior only. It is not FPGA evidence.
- Physical acceptance uses only the designated disposable target
  `192.168.10.239`, with SSH `root` / `1`, and ends by reinstalling the legacy
  image and launching a real catalog game.
- Use test-first changes, focused commits, independent review between tasks,
  and fresh verification before every completion or merge claim.

---

## File structure and ownership

### libmister-runtime changes

```text
src/linux/production_hardware.cpp
    Owns the process-lifetime production dependency graph and idle path.
src/linux/production_hardware.hpp
    Retains the single production-construction API.
tests/unit/native_hardware_test.cpp
    Proves real construction, exact idle path, empty profiles, and no fake fallback.
Makefile
    Gives the production-construction unit test a deterministic missing idle path.
README.md
ARCHITECTURE.md
DEVELOPMENT.md
docs/support-matrix.md
    State idle-only construction truthfully while keeping supported systems at zero.
```

No new public lifecycle API is needed. Keep:

```cpp
Error CreateProductionHardware(LogSink& log,
    std::unique_ptr<Hardware>* hardware);
```

### FogCast native control changes

```text
internal/misterruntime/client.go
    Bounded one-request/one-response Unix-socket client.
internal/misterruntime/client_test.go
    Real Unix-socket framing, schema, boundary, and no-retry tests.
internal/misterruntime/runtime.go
    Adapter from runtime protocol state/errors to agent.Runtime.
internal/misterruntime/runtime_test.go
    Readiness, mapping, unsupported-operation, stop, and boot-ID tests.
protocol/types.go
internal/agent/coordinator.go
internal/agent/coordinator_test.go
internal/httpapi/server.go
internal/httpapi/server_test.go
fogcast/service.go
fogcast/service_test.go
internal/hostapi/server.go
internal/hostapi/server_test.go
    Preserve and map UNSUPPORTED_OPERATION through target, service, and host APIs.
cmd/mister-agent/main.go
cmd/mister-agent/main_test.go
    Select main or native explicitly without detection or fallback.
```

The Go package exposes this narrow internal boundary:

```go
const DefaultSocketPath = "/run/mister-runtime.sock"
const MaximumLineBytes = 65536

type RemoteError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

type Response struct {
    Protocol  int          `json:"protocol"`
    OK        bool         `json:"ok"`
    State     string       `json:"state"`
    Execution string       `json:"execution"`
    System    *string      `json:"system"`
    Core      *string      `json:"core"`
    Error     *RemoteError `json:"error"`
    Version   string       `json:"version"`
}

type Control interface {
    Status(context.Context) (Response, error)
    Stop(context.Context) (Response, error)
}
```

### FogCast image changes

```text
build/native-runtime.inputs.lock.toml
    Exact runtime commit plus official idle source/hash/size/install path.
scripts/fetch-native-runtime-inputs.sh
scripts/verify-native-runtime-inputs.sh
    Fetch the idle file and verify both native inputs.
buildroot/package/mister-runtime/Config.in
buildroot/package/mister-runtime/mister-runtime.mk
buildroot/Config.in
buildroot/external.mk
    Build and install the daemon from /runtime-source with Buildroot's toolchain.
buildroot/configs/fogcast_target_native_dev_defconfig
buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S40mister-runtime
buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S50mister-agent
buildroot/board/fogcast-target/native-post-build.sh
    Assemble native-only services, idle artifact, and input record.
scripts/target-image-container.sh
scripts/build-target-image.sh
scripts/verify-target-image.sh
scripts/qemu-smoke-target-image.sh
Makefile
    Add native-only fetch/build/verify/smoke commands without changing legacy commands.
scripts/tests/native-runtime-inputs_test.sh
scripts/tests/target-image-rootfs_test.sh
scripts/tests/target-image_test.sh
scripts/tests/target-image-sources_test.sh
    Exercise the new input and image contracts while retaining legacy assertions.
```

### Acceptance changes

```text
scripts/native-runtime-smoke.sh
scripts/tests/native-runtime-smoke_test.sh
    Automate process, ready/idle, stop, reboot, and changed-boot-ID checks.
docs/hardware/native-idle-baseline.md
    Canonical dated physical evidence after the checks pass.
README.md
docs/ARCHITECTURE.md
docs/DEVELOPMENT.md
docs/superpowers/specs/2026-08-31-libmister-runtime-design.md
docs/superpowers/specs/2026-09-01-bootable-native-baseline-design.md
    Record the accepted native baseline without implying game support.
```

---

### Task 1: Construct the idle-only production runtime

**Repository:** `libmister-runtime`

**Files:**
- Modify: `src/linux/production_hardware.cpp`
- Modify: `tests/unit/native_hardware_test.cpp`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `ARCHITECTURE.md`
- Modify: `DEVELOPMENT.md`
- Modify: `docs/support-matrix.md`

**Interfaces:**
- Consumes: `PosixArtifactOpener`, `LinuxMmio`, `LinuxFpgaManager`,
  `LinuxSpi`, `CoreLoader`, `NativeHardware`, and `LogSink`.
- Produces: a successful `CreateProductionHardware(LogSink&,
  std::unique_ptr<Hardware>*)` whose owned hardware uses
  `/usr/share/mister-runtime/idle.rbf` and whose production profiles stay empty.

- [ ] **Step 1: Create an isolated runtime worktree and confirm its baseline**

Run the `superpowers:using-git-worktrees` skill, branch from the freshly pulled
runtime main, then run:

```bash
make clean
make -j4 test
make archive-audit
git status --short
```

Expected: the complete named test set and active-tree checks pass; record the
actual named-test count in the task log. The current
`d6e7ec2db1049a0d6bd9edfd44a233ac174729f9` baseline has 98 named tests only;
a later approved runtime main may legitimately have a different count. The
worktree is clean.

- [ ] **Step 2: Replace the unavailable-construction assertion with a failing real-construction test**

In `tests/unit/native_hardware_test.cpp`, split the old construction test into
these exact contracts:

```cpp
void TestProductionConstructionOwnsRealIdleHardware()
{
    assert(mister::ProductionProfiles().empty());
    mister_test::CaptureLog log;
    std::unique_ptr<mister::Hardware> hardware;
    const mister::Error created = mister::CreateProductionHardware(log, &hardware);
    assert(created.ok());
    assert(hardware);
    const mister::HardwareResult idle = hardware->LoadIdle();
    assert(idle.error.code == mister::ErrorCode::io_failed);
    assert(idle.error.message.find(
        "/definitely-missing/libmister-runtime/idle.rbf") != std::string::npos);
    assert(!idle.mutation_attempted);
}

void TestUnavailableHardwareRemainsFailureOnly()
{
    const mister::Error reason = {
        mister::ErrorCode::io_failed, "injected construction failure"};
    std::unique_ptr<mister::Hardware> hardware =
        mister::CreateUnavailableHardware(reason);
    assert(hardware);
    assert(hardware->LoadIdle().error.code == mister::ErrorCode::io_failed);
    assert(hardware->Launch({}).error.code == mister::ErrorCode::io_failed);
    assert(hardware->LoadDevelopmentRBF("/x").error.code ==
        mister::ErrorCode::io_failed);
}
```

Add both calls to `main()`. In the focused native-hardware test recipe, add the
Linux implementation sources to both the prerequisites and the compile/link
command, alongside the idle-path macro. Retain the recipe's existing supporting
source inputs and flags:

```make
$(BUILD_DIR)/tests/unit/native_hardware_test: \
	tests/unit/native_hardware_test.cpp \
	tests/support/capture_log.cpp src/native/artifacts.cpp \
	src/native/core_loader.cpp src/native/hardware.cpp src/profile.cpp \
	src/linux/production_hardware.cpp \
	src/native/linux/mmio.cpp src/native/linux/fpga_manager.cpp \
	src/native/linux/spi.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) \
		-DMISTER_RUNTIME_IDLE_RBF=\"/definitely-missing/libmister-runtime/idle.rbf\" \
		tests/unit/native_hardware_test.cpp \
		tests/support/capture_log.cpp src/native/artifacts.cpp \
		src/native/core_loader.cpp src/native/hardware.cpp src/profile.cpp \
		src/linux/production_hardware.cpp \
		src/native/linux/mmio.cpp src/native/linux/fpga_manager.cpp \
		src/native/linux/spi.cpp -o "$@"
```

- [ ] **Step 3: Run the focused test and verify RED**

Run:

```bash
make build/tests/unit/native_hardware_test
build/tests/unit/native_hardware_test
```

Expected: FAIL because `CreateProductionHardware` still returns `io_failed`
and leaves the pointer empty.

- [ ] **Step 4: Implement one owning production composition**

In `src/linux/production_hardware.cpp`, retain `UnavailableHardware` only for
the daemon's explicit failure path and add this production structure:

```cpp
#ifndef MISTER_RUNTIME_IDLE_RBF
#define MISTER_RUNTIME_IDLE_RBF "/usr/share/mister-runtime/idle.rbf"
#endif

class SteadyClock final : public native::Clock {
public:
    std::uint64_t NowMs() const override
    {
        const auto elapsed = std::chrono::steady_clock::now().time_since_epoch();
        return static_cast<std::uint64_t>(
            std::chrono::duration_cast<std::chrono::milliseconds>(elapsed).count());
    }
};

class ProductionHardware final : public Hardware {
public:
    explicit ProductionHardware(LogSink& log)
        : opener_(), mmio_(), clock_(), fpga_(mmio_, clock_),
          spi_(mmio_, clock_), core_(spi_),
          hardware_(opener_, fpga_, core_, clock_, log,
              MISTER_RUNTIME_IDLE_RBF, {}) {}

    HardwareResult LoadIdle() override { return hardware_.LoadIdle(); }
    HardwareResult Launch(const PreparedLaunch& launch) override
    {
        return hardware_.Launch(launch);
    }
    HardwareResult LoadDevelopmentRBF(const std::string& path) override
    {
        return hardware_.LoadDevelopmentRBF(path);
    }

private:
    native::PosixArtifactOpener opener_;
    native::LinuxMmio mmio_;
    SteadyClock clock_;
    native::LinuxFpgaManager fpga_;
    native::LinuxSpi spi_;
    native::CoreLoader core_;
    native::NativeHardware hardware_;
};
```

Include `<chrono>` and the existing native headers. Member declaration order is
the lifetime order and must not change. Implement construction as:

```cpp
Error CreateProductionHardware(LogSink& log,
    std::unique_ptr<Hardware>* hardware)
{
    if (hardware == nullptr)
        return {ErrorCode::invalid_request,
            "missing production hardware output"};
    hardware->reset(new ProductionHardware(log));
    return {};
}
```

Do not open `/dev/mem` during construction, add profiles, or special-case the
test build with fake dependencies.

- [ ] **Step 5: Run the focused test and verify GREEN**

Run:

```bash
make build/tests/unit/native_hardware_test
build/tests/unit/native_hardware_test
```

Expected: the updated test count passes; the missing test path fails before
MMIO work with `mutation_attempted == false`.

- [ ] **Step 6: Update runtime documentation without claiming game support**

Make these exact truth changes:

- `README.md`: production construction is available for the image-owned idle
  baseline; physical acceptance is still pending; supported systems remain 0.
- `ARCHITECTURE.md`: list the owned production dependency graph and installed
  idle path; profiles remain empty.
- `DEVELOPMENT.md`: replace “construction unavailable” with “built by FogCast's
  reviewed native image; do not install ad hoc cross-builds.”
- `docs/support-matrix.md`: change only the trailing construction paragraph;
  leave all 16 rows unchanged.

- [ ] **Step 7: Run the complete runtime gate**

Run fresh:

```bash
make clean
make -j4 all
make -j4 test
make sanitize
make tsan
make archive-audit
scripts/check-active-tree.sh
scripts/check-history.sh
runtime_target_bin=/home/deano/.cache/toolchains/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin
make target \
  TARGET_CXX="$runtime_target_bin/arm-none-linux-gnueabihf-g++" \
  TARGET_AR="$runtime_target_bin/arm-none-linux-gnueabihf-ar"
file build/target/mister-runtime
git diff --check
```

Expected: all host, sanitizer, audit, history, and Arm checks pass; the target
daemon is ELF32 ARM EABI5 hard-float; no support row changes.

- [ ] **Step 8: Commit and stop at the runtime review gate**

```bash
git add Makefile README.md ARCHITECTURE.md DEVELOPMENT.md \
  docs/support-matrix.md src/linux/production_hardware.cpp \
  tests/unit/native_hardware_test.cpp
git commit -m "feat: construct native Linux runtime hardware"
git status --short
```

Expected: one focused clean commit. Request independent code review, fix all
Critical/Important findings, rerun Step 7, then open and merge the runtime PR.
Pull the reviewed runtime main before Task 2. Do not begin the FogCast image pin
from an unmerged runtime commit.

---

### Task 2: Add the bounded FogCast runtime-socket client

**Repository:** `FogCast`

**Files:**
- Create: `internal/misterruntime/client.go`
- Create: `internal/misterruntime/client_test.go`

**Interfaces:**
- Consumes: protocol 1 at `/run/mister-runtime.sock`.
- Produces: `Control`, `Client`, `NewClient`, `Status`, and `Stop` using the
  exact types in the file-structure section.

- [ ] **Step 1: Start a FogCast implementation worktree from the landed design**

Use `superpowers:using-git-worktrees`, pull FogCast main after the design PR is
merged, and verify:

```bash
go test ./...
git status --short
```

Expected: the baseline passes and is clean.

- [ ] **Step 2: Write real Unix-socket client tests**

Create `internal/misterruntime/client_test.go` with a temporary Unix listener.
Each fixture accepts exactly one connection, reads exactly one newline request,
and returns one newline response. Cover:

```go
func TestClientStatusUsesOneProtocolRequestAndCloses(t *testing.T)
func TestClientStopUsesOnlyTheStopOperation(t *testing.T)
func TestClientRejectsWrongProtocolAndUnknownResponseFields(t *testing.T)
func TestClientAcceptsExactly65536BytesAndRejects65537(t *testing.T)
func TestClientRejectsMissingNewline(t *testing.T)
func TestClientRejectsInvalidStateExecutionAndErrorShapes(t *testing.T)
func TestClientAcceptsStartingNoneWithNullOrRetainedIdentity(t *testing.T)
func TestClientRequiresCompleteIdentityForStartingGame(t *testing.T)
func TestClientRequiresNullIdentityForStartingDevelopment(t *testing.T)
func TestClientHonorsContextDeadlineWithoutRetry(t *testing.T)
```

The accepted idle fixture is exactly:

```json
{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}
```

Assert the status request is exactly:

```json
{"protocol":1,"operation":"status"}
```

and stop is exactly:

```json
{"protocol":1,"operation":"stop"}
```

- [ ] **Step 3: Run the package test and verify RED**

```bash
go test ./internal/misterruntime
```

Expected: FAIL because the client package does not exist.

- [ ] **Step 4: Implement the minimal client**

Implement `NewClient(socketPath string) *Client`, `Status`, and `Stop`. The
shared call path must:

```go
payload, err := json.Marshal(struct {
    Protocol  int    `json:"protocol"`
    Operation string `json:"operation"`
}{Protocol: 1, Operation: operation})
```

Then it must open one `net.Dialer.DialContext(ctx, "unix", socketPath)`, apply
the caller deadline to the connection when present, write `payload` plus one
newline, read at most `MaximumLineBytes+1`, require the newline within the
65,536-byte total line, decode with `DisallowUnknownFields`, require decoder
EOF, validate protocol `1`, and close the connection. Do not retry.

Accept only runtime states `idle`, `starting`, `running_game`,
`running_development`, and `reboot_required`; executions `none`, `game`, and
`development`. Enforce these exact state/identity rules:

| State | Allowed execution and identity |
| --- | --- |
| `idle` | `none`; `system` and `core` are null |
| `starting` | `none` with null identity or one complete non-empty retained `system`/`core` pair; `game` with the complete non-empty pair; `development` with null identity |
| `running_game` | `game`; `system` and `core` are non-null and non-empty |
| `running_development` | `development`; `system` and `core` are null |
| `reboot_required` | `none`; `system` and `core` are null; error code is `idle_failed` |

Except for the permitted retained pair during `starting` plus `none`, an
execution of `none` or `development` never carries system/core identity; an
execution of `game` always carries one complete non-empty pair. `ok=false`
requires a non-null error, while a successful status response may retain the
runtime's last direct error. Accept only the protocol-1 error codes `invalid_request`,
`unsupported_protocol`, `unknown_system`, `missing_media`, `busy`,
`program_failed`, `core_mismatch`, `io_failed`, and `idle_failed`. Require a
non-empty version. Return stable local errors such as `runtime response exceeds
65536 bytes` without embedding private paths.

- [ ] **Step 5: Run focused and race tests**

```bash
go test ./internal/misterruntime
go test -race ./internal/misterruntime
```

Expected: all socket/framing cases pass and the deadline fixture records one
accepted connection only.

- [ ] **Step 6: Commit the client**

```bash
git add internal/misterruntime/client.go internal/misterruntime/client_test.go
git commit -m "feat: add native runtime socket client"
```

---

### Task 3: Compose the explicit native agent backend

**Repository:** `FogCast`

**Files:**
- Create: `internal/misterruntime/runtime.go`
- Create: `internal/misterruntime/runtime_test.go`
- Modify: `protocol/types.go`
- Modify: `internal/agent/coordinator.go`
- Modify: `internal/agent/coordinator_test.go`
- Modify: `fogcast/service.go`
- Modify: `fogcast/service_test.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`
- Modify: `internal/hostapi/server.go`
- Modify: `internal/hostapi/server_test.go`
- Modify: `cmd/mister-agent/main.go`
- Modify: `cmd/mister-agent/main_test.go`

**Interfaces:**
- Consumes: the Task 2 `Control` interface and the existing `agent.Runtime`.
- Produces: `NewRuntime(Control, string, time.Duration, time.Duration)
  *Runtime` and explicit CLI values `main` and `native`.

- [ ] **Step 1: Write adapter mapping and no-mutation tests**

Create a recording `Control` fake and cover:

```go
func TestNativeHealthIsReadyOnlyForIdleAndKeepsLegacyBooleansFalse(t *testing.T)
func TestNativeReconcileMapsIdleWithoutIdentity(t *testing.T)
func TestNativeReconcileMapsRebootRequiredToFailedUnavailable(t *testing.T)
func TestNativeReconcileWaitsThroughStartingAndHonorsContext(t *testing.T)
func TestNativeReconcileTreatsNonIdleStartupAsUnavailable(t *testing.T)
func TestNativePrepareAndLaunchRejectEveryGameWithoutControlMutation(t *testing.T)
func TestNativeDevelopmentRejectsWithoutReadingBodyOrCallingControl(t *testing.T)
func TestNativeDirectStopTranslationMapsControlResultForLaterMilestone(t *testing.T)
func TestCoordinatorStopWhileNativeIdleDoesNotCallRuntimeStop(t *testing.T)
func TestNativeHealthReadsBootIDWithoutLeakingReadErrors(t *testing.T)
```

Use this exact target mapping:

| Runtime state | FogCast target status |
| --- | --- |
| `idle` | `StateIdle`, no game/system/core/development identity |
| `starting` | keep polling during `Reconcile`; `Health.Ready=false` |
| `running_game` or `running_development` at Milestone 2 startup | `StateFailed`, `CodeMiSTerUnavailable`, stable unavailable message; no public active stop admission |
| `reboot_required` | `StateFailed`, `CodeMiSTerUnavailable`, direct stable message |
| socket/protocol failure | `StateFailed`, `CodeMiSTerUnavailable`, stable message |

`Health` returns API version `v1`, the supplied agent version, the boot ID,
and `Ready=true` only for runtime `idle`. It leaves `MiSTerProcess` and
`CommandPipe` false because those legacy components do not exist.

- [ ] **Step 2: Add the explicit public unsupported-operation code tests**

Add:

```go
const CodeUnsupportedOperation ErrorCode = "UNSUPPORTED_OPERATION"
```

Test that `internal/httpapi.statusForError(CodeUnsupportedOperation)` is HTTP
422. Add the same code to `fogcast.canonicalError` so the host service does not
collapse it to `INTERNAL`. Give it the stable public message `requested
operation is unsupported`; map it to HTTP 400 in the public host session API,
consistent with the existing unsupported-system request mapping. Cover those
target, service, and host mappings in their package tests.

Replace the coordinator's three backend-specific unavailable messages with
`target runtime is unavailable` and assert the existing
`MISTER_UNAVAILABLE` code is unchanged. This prevents native failures from
claiming that Main or its command pipe exists without changing legacy control
flow.

Normal native game preparation returns `UNSUPPORTED_SYSTEM` because the
profile table is empty. Development loading and development recovery return
`UNSUPPORTED_OPERATION` before reading the body or calling `Control`.

- [ ] **Step 3: Add failing composition tests**

In `cmd/mister-agent/main_test.go`, add tests that call a factored backend
selector and assert:

```go
func TestProductionRuntimeBackendDefaultsToMain(t *testing.T)
func TestProductionRuntimeBackendSelectsNativeOnlyWhenExplicit(t *testing.T)
func TestProductionRuntimeBackendRejectsUnknownValueWithoutFallback(t *testing.T)
func TestNativeCompositionReportsIdleAndUnsupportedDevelopment(t *testing.T)
```

The accepted command syntax is:

```text
mister-agent [--config path] [--runtime main|native]
```

The default is `main`; the native socket is exactly
`/run/mister-runtime.sock`.

- [ ] **Step 4: Run the focused tests and verify RED**

```bash
go test ./internal/misterruntime ./internal/agent ./fogcast \
  ./internal/httpapi ./internal/hostapi ./cmd/mister-agent
```

Expected: FAIL because the adapter, error code, and selector do not exist.

- [ ] **Step 5: Implement the adapter and selector**

Use this constructor boundary in `runtime.go`:

```go
func NewRuntime(control Control, bootIDFile string,
    pollInterval, healthTimeout time.Duration) *Runtime
```

`Reconcile` polls only while the runtime says `starting` or the socket is not
yet reachable, using the caller context and `pollInterval`. A conclusive
`idle`, non-idle unavailable, or `reboot_required` response returns
immediately. `Health` uses one call bounded by `healthTimeout`; it does not
retry. Keep the public coordinator stop admission idle-only: it confirms an
already-idle state without calling runtime `Stop`. The direct adapter `Stop`
translation remains an isolated interface test for a later milestone; do not
add coordinator-driven active stop or change product behaviour here.

Factor production composition in `cmd/mister-agent/main.go` as:

```go
type runtimeBackend string

const (
    runtimeMain   runtimeBackend = "main"
    runtimeNative runtimeBackend = "native"
)

func productionRunDependencies(backend runtimeBackend) (runDependencies, error)
```

Thread that value explicitly through:

```go
func run(ctx context.Context, configPath string, backend runtimeBackend,
    logger *slog.Logger) error
```

Add a `--runtime` string flag whose default is `main`; parse it to a
`runtimeBackend` and reject any other value before loading configuration or
constructing dependencies. Update existing direct `run` tests to pass
`runtimeMain`.

For `main`, retain the existing `mister.NewRuntime` construction byte-for-byte
where practical. For `native`, construct:

```go
client := misterruntime.NewClient(misterruntime.DefaultSocketPath)
return misterruntime.NewRuntime(client, bootIDFile,
    25*time.Millisecond, 250*time.Millisecond)
```

Do not inspect processes, `/dev/MiSTer_cmd`, or `/tmp/CORENAME` in the native
branch. Do not fall back if socket calls fail.

- [ ] **Step 6: Run focused, race, and full Go gates**

```bash
gofmt -w internal/misterruntime protocol/types.go internal/agent/coordinator.go \
  internal/agent/coordinator_test.go internal/httpapi/server.go \
  internal/httpapi/server_test.go internal/hostapi/server.go \
  internal/hostapi/server_test.go fogcast/service.go fogcast/service_test.go \
  cmd/mister-agent/main.go cmd/mister-agent/main_test.go
go test ./internal/misterruntime ./internal/agent ./fogcast \
  ./internal/httpapi ./internal/hostapi ./cmd/mister-agent
go test -race ./internal/misterruntime ./internal/agent ./fogcast \
  ./internal/httpapi ./internal/hostapi ./cmd/mister-agent
go test -race ./...
go vet ./...
git diff --check
```

Expected: all tests pass; legacy composition tests still select Main; native
tests prove game/development rejection performs no `Stop` or hardware-changing
runtime operation; ordinary readiness status reads remain allowed.

- [ ] **Step 7: Commit and request the first FogCast review**

```bash
git add internal/misterruntime protocol/types.go internal/agent/coordinator.go \
  internal/agent/coordinator_test.go internal/httpapi/server.go \
  internal/httpapi/server_test.go internal/hostapi/server.go \
  internal/hostapi/server_test.go fogcast/service.go fogcast/service_test.go \
  cmd/mister-agent/main.go cmd/mister-agent/main_test.go
git commit -m "feat: add explicit native runtime backend"
```

Request independent review of Tasks 2-3 before image work. Fix all
Critical/Important findings and rerun Step 6.

---

### Task 4: Pin and package the native runtime inputs

**Repository:** `FogCast`

**Files:**
- Create: `build/native-runtime.inputs.lock.toml`
- Create: `scripts/fetch-native-runtime-inputs.sh`
- Create: `scripts/verify-native-runtime-inputs.sh`
- Create: `scripts/tests/native-runtime-inputs_test.sh`
- Create: `buildroot/package/mister-runtime/Config.in`
- Create: `buildroot/package/mister-runtime/mister-runtime.mk`
- Modify: `buildroot/Config.in`
- Modify: `buildroot/external.mk`
- Modify: `scripts/target-image-container.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: the clean, reviewed runtime main checkout and the immutable idle
  source named in Global Constraints.
- Produces: read-only `/runtime-source`, verified cached
  `build/cache/target-image/native/idle.rbf`, and Buildroot package
  `BR2_PACKAGE_FOGCAST_MISTER_RUNTIME`.

- [ ] **Step 1: Record the reviewed runtime commit without abbreviation**

```bash
export LIBMISTER_RUNTIME_DIR=/home/deano/fes/libmister-runtime
git -C "$LIBMISTER_RUNTIME_DIR" pull --ff-only
runtime_commit=$(git -C "$LIBMISTER_RUNTIME_DIR" rev-parse HEAD)
test -z "$(git -C "$LIBMISTER_RUNTIME_DIR" status --porcelain)"
printf '%s\n' "$runtime_commit" | grep -Eq '^[0-9a-f]{40}$'
printf 'runtime commit for native input lock: %s\n' "$runtime_commit"
```

Use the printed value directly in the lock. It must be the reviewed runtime
commit produced by Task 1, never the pre-implementation `d6e7ec2` baseline.

- [ ] **Step 2: Write failing native-input fixture tests**

The test creates a temporary Git repository and idle file, then verifies:

```text
accepted: exact clean runtime HEAD + exact idle hash and size
rejected: wrong runtime HEAD
rejected: dirty runtime checkout
rejected: wrong idle digest
rejected: wrong idle size
rejected: non-regular idle path
rejected: missing or non-40-character runtime commit
rejected: container run without a read-only /runtime-source mount
```

It also asserts the Buildroot package uses `SITE_METHOD = local`, builds with
`TARGET_CXX`/`TARGET_AR`, supplies the locked runtime version, and installs only
`build/mister-runtime` to `/usr/sbin/mister-runtime`.

- [ ] **Step 3: Run the fixture and verify RED**

```bash
sh scripts/tests/native-runtime-inputs_test.sh
```

Expected: FAIL because the lock, scripts, mount, and package do not exist.

- [ ] **Step 4: Add the exact native input lock**

Use the `runtime_commit` shell value from Step 1 directly in this patch, so the
created file contains a real 40-character commit rather than an instruction:

```bash
apply_patch <<PATCH
*** Begin Patch
*** Add File: build/native-runtime.inputs.lock.toml
+format = 1
+
+[mister_runtime]
+commit = '$runtime_commit'
+mount_path = '/runtime-source'
+
+[idle_rbf]
+repository = 'https://github.com/MiSTer-devel/Distribution_MiSTer'
+commit = 'f7bde4becb452ca28f604ad9802bbed5c6b58e01'
+path = 'menu.rbf'
+sha256 = '821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934'
+size = 2452588
+install_path = '/usr/share/mister-runtime/idle.rbf'
*** End Patch
PATCH
```

- [ ] **Step 5: Implement fetch and verification scripts**

`fetch-native-runtime-inputs.sh` reads only the `[idle_rbf]` values, downloads
the raw immutable URL into a temporary file, verifies SHA-256 and byte size,
then atomically moves it to:

```text
build/cache/target-image/native/idle.rbf
```

`verify-native-runtime-inputs.sh LOCK RUNTIME_SOURCE IDLE_FILE` requires:

```bash
test "$(git -C "$runtime_source" rev-parse HEAD)" = "$expected_commit"
test -z "$(git -C "$runtime_source" status --porcelain --untracked-files=all)"
test "$(sha256sum "$idle_file" | awk '{print $1}')" = "$expected_sha"
test "$(wc -c < "$idle_file" | tr -d ' ')" = "$expected_size"
```

It also validates absolute mount/install paths and the exact fixed repository,
idle commit, and source path. It prints only commits and hashes, never local
credentials or unrelated environment variables.

- [ ] **Step 6: Mount the runtime checkout read-only**

Extend `scripts/target-image-container.sh` so `LIBMISTER_RUNTIME_DIR`, when
present, must be an absolute clean Git checkout and is mounted as:

```text
--volume /absolute/runtime/checkout:/runtime-source:ro
```

Pass no runtime Git credentials into the container. Legacy fetch/run calls
without `LIBMISTER_RUNTIME_DIR` retain their existing command lines.

- [ ] **Step 7: Add the Buildroot external package**

`buildroot/package/mister-runtime/mister-runtime.mk` uses:

```make
MISTER_RUNTIME_SITE = /runtime-source
MISTER_RUNTIME_SITE_METHOD = local
MISTER_RUNTIME_LICENSE = GPL-3.0-or-later
MISTER_RUNTIME_LICENSE_FILES = LICENSE

define MISTER_RUNTIME_BUILD_CMDS
	$(TARGET_MAKE_ENV) $(MAKE) -C $(@D) \
		CXX="$(TARGET_CXX)" AR="$(TARGET_AR)" NM="$(TARGET_NM)" \
		CXXFILT="$(TARGET_CROSS)c++filt" \
		MISTER_RUNTIME_VERSION="git-$(FOGCAST_MISTER_RUNTIME_COMMIT)" \
		all
endef

define MISTER_RUNTIME_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/build/mister-runtime \
		$(TARGET_DIR)/usr/sbin/mister-runtime
endef

$(eval $(generic-package))
```

Export `FOGCAST_MISTER_RUNTIME_COMMIT` from the native build invocation after
the input verifier passes. Source the package Config.in from `buildroot/Config.in`
and include package makefiles from `buildroot/external.mk`.

- [ ] **Step 8: Run native-input tests and the normal repository gates**

```bash
sh scripts/tests/native-runtime-inputs_test.sh
go test ./...
go vet ./...
git diff --check
```

Expected: the fixture proves exact/dirty/hash failures and a read-only Docker
mount; ordinary legacy source tests still pass.

- [ ] **Step 9: Commit the input/package boundary**

```bash
git add build/native-runtime.inputs.lock.toml buildroot/Config.in \
  buildroot/external.mk buildroot/package/mister-runtime \
  scripts/fetch-native-runtime-inputs.sh \
  scripts/verify-native-runtime-inputs.sh \
  scripts/target-image-container.sh \
  scripts/tests/native-runtime-inputs_test.sh Makefile
git commit -m "build: package pinned native runtime inputs"
```

---

### Task 5: Build and inspect the separate native-dev image

**Repository:** `FogCast`

**Files:**
- Create: `buildroot/configs/fogcast_target_native_dev_defconfig`
- Create: `buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S40mister-runtime`
- Create: `buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S50mister-agent`
- Create: `buildroot/board/fogcast-target/native-post-build.sh`
- Modify: `scripts/build-target-image.sh`
- Modify: `scripts/verify-target-image.sh`
- Modify: `scripts/qemu-smoke-target-image.sh`
- Modify: `scripts/tests/target-image-rootfs_test.sh`
- Modify: `scripts/tests/target-image_test.sh`
- Modify: `scripts/tests/target-image-sources_test.sh`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DEVELOPMENT.md`

**Interfaces:**
- Consumes: Task 3 native agent backend and Task 4 verified Buildroot package.
- Produces: reproducible `native-dev/linux.img`, image manifest/library report,
  runtime-before-agent init, and software-only documentation.

- [ ] **Step 1: Extend fixture tests for exactly three variants**

Add failing assertions that:

```text
prod and dev keep S40mister-main and FAT-side agent startup
native-dev has S40mister-runtime and image-owned agent --runtime native
native-dev has no S40mister-main and no init wait for /dev/MiSTer_cmd
native-dev contains exactly one RBF at /usr/share/mister-runtime/idle.rbf
native-dev contains an exact native-input record
native-dev contains ELF32 ARM mister-runtime and static ELF32 ARM mister-agent
native-dev includes every runtime NEEDED library
all three variants retain read-only root and volatile tmpfs policy
```

Add path-validation and two-build reproducibility cases for `native-dev`. Keep
the existing exact `prod`/`dev` assertions rather than generalizing them away.

- [ ] **Step 2: Run image fixtures and verify RED**

```bash
sh scripts/tests/target-image-rootfs_test.sh
sh scripts/tests/target-image-sources_test.sh
sh scripts/tests/target-image_test.sh
```

Expected: FAIL on the missing native defconfig, services, and variant support.

- [ ] **Step 3: Add the native defconfig and overlay**

Base the new defconfig on `fogcast_target_dev_defconfig` and retain Dropbear and
curl. Its distinctive lines are:

```make
BR2_PACKAGE_FOGCAST_MISTER_RUNTIME=y
BR2_ROOTFS_OVERLAY="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/rootfs-overlay ${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/native-rootfs-overlay"
BR2_ROOTFS_POST_BUILD_SCRIPT="${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/post-build.sh ${BR2_EXTERNAL_FOGCAST_TARGET_PATH}/board/fogcast-target/native-post-build.sh"
```

`S40mister-runtime` starts and stops the image-owned daemon through the existing
`mister-supervise` loop and stores only the supervisor PID separately. Its
start command is:

```sh
/usr/sbin/mister-supervise mister-runtime /usr/sbin/mister-runtime &
printf '%s\n' "$!" > /run/mister-runtime-supervisor.pid
```

The native `S50mister-agent` retains the bounded wait for
`/media/fat/fogcast/agent.toml` and cache-directory setup, but removes every
`/dev/MiSTer_cmd` and FAT-side binary check. Its start command is:

```sh
/usr/sbin/mister-supervise mister-agent /usr/sbin/mister-agent \
  --config /media/fat/fogcast/agent.toml --runtime native &
printf '%s\n' "$!" > /run/mister-agent-supervisor.pid
```

- [ ] **Step 4: Add the native post-build policy**

After the shared post-build script, the native script must:

```sh
/bin/rm -f "$target/etc/init.d/S40mister-main"
/bin/mkdir -p "$target/usr/share/mister-runtime"
/usr/bin/install -m 0644 \
  /work/build/cache/target-image/native/idle.rbf \
  "$target/usr/share/mister-runtime/idle.rbf"
```

It verifies `/usr/sbin/mister-runtime` and `/usr/sbin/mister-agent` are
executable, verifies the installed idle hash/size again, chmods the two native
services, and writes `/usr/share/mister-runtime/build-inputs` containing the
locked runtime commit, idle repository/commit/path/hash/size, and installed
path. It does not embed the local runtime checkout path.

- [ ] **Step 5: Add native-dev build, verification, and QEMU routing**

In variant validation, accept exactly `prod|dev|native-dev`. Map the hyphenated
variant explicitly:

```sh
defconfig_for() {
  case "$1" in
    native-dev) printf '%s\n' fogcast_target_native_dev_defconfig ;;
    prod|dev) printf 'fogcast_target_%s_defconfig\n' "$1" ;;
  esac
}
```

Only native builds require `LIBMISTER_RUNTIME_DIR`, the read-only mount, and
`verify-native-runtime-inputs.sh`. Legacy builds run the existing path.

The verifier keeps its legacy forbidden-RBF rule unchanged. Its native branch
allows exactly `/usr/share/mister-runtime/idle.rbf`, checks its digest and size,
requires both native services, rejects `S40mister-main`, verifies the native
agent command, inspects the runtime ELF/NEEDED closure, and matches
`build-inputs` to the lock.

The QEMU log check for legacy still requires exactly one bounded Main payload
wait. The native branch requires the root/volatile smoke sentinel and rejects
any `mister-main: waiting` line; it makes no FPGA or ready claim. Use
`/target-image-output/work-2-native-dev/host/bin/arm-buildroot-linux-gnueabihf-`
as the QEMU smoke toolchain for `native-dev`, while legacy variants retain the
existing canonical prod toolchain path.

- [ ] **Step 6: Add native-only Make targets without changing legacy targets**

Add:

```make
LIBMISTER_RUNTIME_DIR ?= $(abspath ../libmister-runtime)

target-image-native-fetch: build-target-image-lock-container build-agent
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/target-image-container.sh fetch \
	  /work/scripts/fetch-native-runtime-inputs.sh
	LIBMISTER_RUNTIME_DIR="$(LIBMISTER_RUNTIME_DIR)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/build-target-image.sh --fetch native-dev

target-image-native: build-agent target-image-native-fetch
	LIBMISTER_RUNTIME_DIR="$(LIBMISTER_RUNTIME_DIR)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/build-target-image.sh native-dev

target-image-native-verify:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/verify-target-image.sh native-dev \
	  build/output/target-image/native-dev/linux.img \
	  build/output/target-image/native-dev/manifest.tsv \
	  build/output/target-image/native-dev/library-report.tsv

target-image-native-qemu-smoke:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/qemu-smoke-target-image.sh native-dev \
	  build/output/target-image/native-dev/linux.img
```

Do not add native prerequisites to `target-images`, `target-image-dev`, or the
legacy verify/smoke targets.

- [ ] **Step 7: Update current documentation as software-only**

Document a third candidate image and its build commands. Keep the normal
working game path as legacy. State explicitly that `native-dev` has only
software/QEMU packaging evidence until Task 7, launches no games or development
RBFs, and has zero supported systems.

- [ ] **Step 8: Run focused fixtures and the complete FogCast software gate**

```bash
sh scripts/tests/native-runtime-inputs_test.sh
sh scripts/tests/target-image-rootfs_test.sh
sh scripts/tests/target-image-sources_test.sh
sh scripts/tests/target-image_test.sh
make build-agent
make test
make vet
git diff --check
```

Expected: all ordinary tests pass and every legacy assertion remains present.

- [ ] **Step 9: Build, inspect, and QEMU-smoke the real native image**

```bash
export LIBMISTER_RUNTIME_DIR=/home/deano/fes/libmister-runtime
make target-image-native
make target-image-native-verify
make target-image-native-qemu-smoke
file build/output/target-image/native-dev/linux.img
sha256sum build/output/target-image/native-dev/linux.img
```

Expected: two image builds have the same digest; inspection passes; QEMU proves
root/init packaging and explicitly makes no FPGA claim.

- [ ] **Step 10: Commit and stop at the image review gate**

```bash
git add Makefile README.md docs/ARCHITECTURE.md docs/DEVELOPMENT.md \
  buildroot/configs/fogcast_target_native_dev_defconfig \
  buildroot/board/fogcast-target/native-rootfs-overlay \
  buildroot/board/fogcast-target/native-post-build.sh \
  scripts/build-target-image.sh scripts/verify-target-image.sh \
  scripts/qemu-smoke-target-image.sh scripts/tests/target-image-rootfs_test.sh \
  scripts/tests/target-image-sources_test.sh scripts/tests/target-image_test.sh
git commit -m "feat: add native runtime target image"
```

Request independent review of the full FogCast branch. Fix all
Critical/Important findings, rerun Steps 8-9, open the FogCast PR, merge it,
delete the remote branch, and pull clean FogCast main before hardware work.

---

### Task 6: Add the native physical-acceptance runner

**Repository:** `FogCast`

**Files:**
- Create: `scripts/native-runtime-smoke.sh`
- Create: `scripts/tests/native-runtime-smoke_test.sh`
- Modify: `Makefile`
- Modify: `docs/DEVELOPMENT.md`

**Interfaces:**
- Consumes: existing host API, target health API, SSH access, and explicit
  reboot.
- Produces: one bounded command that verifies processes, ready/idle, stop,
  reboot, changed boot ID, and fresh idle without testing games.

- [ ] **Step 1: Write the failing operator-script fixture**

Use fake `curl`, `sshpass`, and `sleep` executables. Cover:

```text
accept ready target and idle host status
require mister-runtime and mister-agent processes
reject any MiSTer process or /dev/MiSTer_cmd FIFO
POST one host stop and require idle afterward
record boot-before, issue one best-effort reboot, and require changed boot-after
reject unchanged boot ID, never-ready health, missing process, and Main process
never print configured SSH password or HTTP response details
```

- [ ] **Step 2: Run the fixture and verify RED**

```bash
sh scripts/tests/native-runtime-smoke_test.sh
```

Expected: FAIL because the runner does not exist.

- [ ] **Step 3: Implement the bounded runner**

Use the same fixture defaults already documented:

```sh
host_api=${FOGCAST_HOST_API:-http://127.0.0.1:8787}
target_api=${FOGCAST_TARGET_API:-http://192.168.10.239:8182}
target_host=${FOGCAST_TARGET_HOST:-192.168.10.239}
target_user=${FOGCAST_TARGET_USER:-root}
target_password=${FOGCAST_TARGET_PASSWORD:-1}
poll_attempts=${FOGCAST_POLL_ATTEMPTS:-60}
poll_interval=${FOGCAST_POLL_INTERVAL:-1}
```

Query target `/v1/health` and host `/api/v1/status`; require target
`ready:true`, non-empty `boot_id`, and host `state:"idle"`. Over SSH require
exactly one runtime and agent child, no process whose executable is `MiSTer`,
no `/dev/MiSTer_cmd` FIFO, and an exact installed `build-inputs` file. POST
`/api/v1/session/stop`, require idle again, reboot by the existing best-effort
SSH command, then poll until health is ready with a different boot ID and host
status is idle. Print only:

```sh
printf 'native runtime smoke passed: boot %s -> %s, idle -> idle\n' \
  "$boot_before" "$boot_after"
```

This polling belongs to the acceptance harness, not runtime recovery behavior.

- [ ] **Step 4: Add the Make target and development command**

```make
target-native-smoke:
	scripts/native-runtime-smoke.sh
```

Document that the command does not inspect HDMI and does not perform the
mandatory legacy rollback launch.

- [ ] **Step 5: Run script and repository gates**

```bash
sh scripts/tests/native-runtime-smoke_test.sh
make test
make vet
git diff --check
```

Expected: all cases pass with bounded polling and no fixture secret output.

- [ ] **Step 6: Commit and review the runner**

```bash
git add Makefile docs/DEVELOPMENT.md scripts/native-runtime-smoke.sh \
  scripts/tests/native-runtime-smoke_test.sh
git commit -m "test: add native runtime hardware acceptance"
```

Request review, fix Critical/Important findings, rerun Step 5, and merge before
using the runner as acceptance evidence.

---

### Task 7: Accept native idle on the Pi and prove legacy rollback

**Repositories:** `FogCast`, then `libmister-runtime` documentation only

**Files:**
- Create: `FogCast/docs/hardware/native-idle-baseline.md`
- Modify: `FogCast/README.md`
- Modify: `FogCast/docs/ARCHITECTURE.md`
- Modify: `FogCast/docs/DEVELOPMENT.md`
- Modify: `FogCast/docs/superpowers/specs/2026-08-31-libmister-runtime-design.md`
- Modify: `FogCast/docs/superpowers/specs/2026-09-01-bootable-native-baseline-design.md`
- Modify: `libmister-runtime/README.md`
- Modify: `libmister-runtime/DEVELOPMENT.md`
- Modify: `libmister-runtime/docs/support-matrix.md`

**Interfaces:**
- Consumes: clean, pulled runtime and FogCast main checkouts; rebuilt and
  verified reproducible native and legacy images; ShadowCast 3 at
  `/dev/video0`; and the known legacy image.
- Produces: dated physical evidence and truthful Milestone 2 completion state.

- [ ] **Step 1: Clean main checkouts, rebuild verified images, and freeze exact inputs before touching the device**

```bash
fogcast_root=$(git rev-parse --show-toplevel)
runtime_root=/home/deano/fes/libmister-runtime

git -C "$fogcast_root" switch main
git -C "$fogcast_root" pull --ff-only
test -z "$(git -C "$fogcast_root" status --porcelain --untracked-files=all)"
fogcast_commit=$(git -C "$fogcast_root" rev-parse HEAD)

git -C "$runtime_root" switch main
git -C "$runtime_root" pull --ff-only
test -z "$(git -C "$runtime_root" status --porcelain --untracked-files=all)"
runtime_commit=$(git -C "$runtime_root" rev-parse HEAD)
locked_runtime_commit=$(awk -F"'" '
  $0 == "[mister_runtime]" { in_runtime = 1; next }
  /^\[/ { in_runtime = 0 }
  in_runtime && /^commit = / { print $2; exit }
' "$fogcast_root/build/native-runtime.inputs.lock.toml")
printf '%s\n' "$locked_runtime_commit" | grep -Eq '^[0-9a-f]{40}$'
test "$locked_runtime_commit" = "$runtime_commit"

cd "$fogcast_root"
make target-images
make target-image-verify
export LIBMISTER_RUNTIME_DIR="$runtime_root"
make target-image-native
make target-image-native-verify
make target-image-native-qemu-smoke
sh scripts/verify-native-runtime-inputs.sh \
  build/native-runtime.inputs.lock.toml \
  "$runtime_root" \
  build/cache/target-image/native/idle.rbf

native_image=build/output/target-image/native-dev/linux.img
legacy_dev_image=build/output/target-image/dev/linux.img
legacy_prod_image=build/output/target-image/prod/linux.img
native_sha=$(sha256sum "$native_image" | awk '{print $1}')
legacy_dev_sha=$(sha256sum "$legacy_dev_image" | awk '{print $1}')
legacy_prod_sha=$(sha256sum "$legacy_prod_image" | awk '{print $1}')
printf 'fogcast=%s\nruntime=%s\nlocked_runtime=%s\nnative_image=%s\nlegacy_dev_image=%s\nlegacy_prod_image=%s\n' \
  "$fogcast_commit" "$runtime_commit" "$locked_runtime_commit" \
  "$native_sha" "$legacy_dev_sha" "$legacy_prod_sha"
```

Expected: both clean main checkouts are fast-forwarded; the locked runtime
commit equals clean runtime `HEAD`; the native-input verifier passes again;
the reproducible `prod`, `dev`, and `native-dev` images are rebuilt and
verified from clean FogCast main; and all three resulting image hashes are
recorded before any hardware action.

- [ ] **Step 2: Deploy native-dev and run the automated idle lifecycle**

```bash
make target-image-deploy TARGET_IMAGE="$native_image"
make target-native-smoke
```

Expected: no Main process/FIFO, runtime and agent present, ready idle, stop idle,
changed boot ID, and fresh ready idle.

- [ ] **Step 3: Capture and inspect the real HDMI output**

```bash
capture_root=build/output/target-image/native-dev
mkdir -p "$capture_root"
capture_dir=$(mktemp -d "$capture_root/evidence.XXXXXX")
{
  v4l2-ctl --list-devices
  v4l2-ctl --device /dev/video0 --all
  v4l2-ctl --device /dev/video0 --list-formats-ext
} > "$capture_dir/v4l2-video0-report.txt"
ffmpeg -hide_banner -loglevel error -f v4l2 -i /dev/video0 \
  -t 5 -vf fps=1 -frames:v 5 -strftime 1 -y \
  "$capture_dir"/idle-%Y%m%dT%H%M%S.png
set -- "$capture_dir"/idle-*.png
test "$#" -eq 5
sha256sum \
  "$capture_dir"/v4l2-video0-report.txt \
  "$capture_dir"/idle-*.png \
  > "$capture_dir"/capture-sha256.txt
```

Capture exactly five timestamped frames across the five-second interval and
retain the V4L2 device/mode report in the fresh run-specific `capture_dir`.
Open every PNG with the local image-view tool and inspect each one. Every frame
must show stable geometry and non-corrupt idle output; a blacked-out OSD due to
no input is acceptable only when the HDMI signal and frame geometry are stable
in all five frames. Record the V4L2-report hash and every frame hash in the
canonical evidence. Do not commit any binary capture frame.

- [ ] **Step 4: Collect direct logs and installed identities**

```bash
sshpass -p 1 ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null root@192.168.10.239 \
  'cat /usr/share/mister-runtime/build-inputs; \
   tail -n 80 /var/log/mister-runtime.log; \
   tail -n 80 /var/log/mister-agent.log; \
   ps | grep -E "mister-runtime|mister-agent"'
```

Expected: installed identities match Step 1, runtime reaches idle, agent uses
native mode, and no log reports fallback or conventional Main.

- [ ] **Step 5: Reinstall legacy dev and launch a real known game**

```bash
make target-image-deploy TARGET_IMAGE="$legacy_dev_image"
game_id=$(curl --fail --silent --show-error --get \
  --data-urlencode 'q=Sonic the Hedgehog 2' \
  http://127.0.0.1:8787/api/v1/games | \
  python3 -c 'import json,sys; games=json.load(sys.stdin)["games"]; assert games; print(games[0]["id"])')
make target-smoke GAME_ID="$game_id" EXPECTED_CORE=MegaDrive
```

Expected: the legacy image returns ready, launches the selected real game,
observes `MegaDrive`, stops, and observes `MENU`.

- [ ] **Step 6: Write the canonical FogCast evidence and status updates**

Set `acceptance_date=$(date -I)` and create
`docs/hardware/native-idle-baseline.md` with that date, full
FogCast/runtime commits, native, legacy-dev, and legacy-prod image hashes, idle
source commit/hash,
before/after boot IDs, process/FIFO result, stop result, V4L2 device/mode
report and hash, every timestamped HDMI frame name/hash/inspection result, and
rollback game ID/core result. Commit none of the binary capture frames. Mark
the run `pass` only if all Steps 2-5 passed.

Update FogCast docs so:

```text
legacy dev/prod = current game-capable path
native-dev = hardware-tested idle baseline, zero supported game systems
Milestone 2 = complete
Milestone 3 = Mega Drive vertical slice next
```

The support and architecture text must not claim native game or development
RBF support.

- [ ] **Step 7: Commit and review FogCast evidence**

```bash
git add README.md docs/ARCHITECTURE.md docs/DEVELOPMENT.md \
  docs/hardware/native-idle-baseline.md \
  docs/superpowers/specs/2026-08-31-libmister-runtime-design.md \
  docs/superpowers/specs/2026-09-01-bootable-native-baseline-design.md
git commit -m "docs: record native idle hardware acceptance"
make test
make vet
git diff --check
```

Request independent evidence/documentation review and merge the FogCast commit.

- [ ] **Step 8: Link the exact evidence from libmister-runtime**

After the FogCast evidence commit is merged, pull both repositories and record
the full FogCast evidence commit:

```bash
evidence_commit=$(git -C /home/deano/fes/FogCast-POC rev-parse HEAD)
printf '%s\n' "$evidence_commit" | grep -Eq '^[0-9a-f]{40}$'
```

Build the immutable evidence URL and print it:

```bash
evidence_url=$(printf \
  'https://github.com/DeanoC/FogCast/blob/%s/docs/hardware/native-idle-baseline.md' \
  "$evidence_commit")
printf '%s\n' "$evidence_url"
```

Update runtime `README.md`, `DEVELOPMENT.md`, and the support-matrix trailing
paragraph to say the idle-only production construction passed the designated
Pi, and insert the printed URL. Keep every system row unchanged.

- [ ] **Step 9: Run the final runtime documentation gate and commit**

```bash
make -j4 test
make archive-audit
scripts/check-active-tree.sh
scripts/check-history.sh
git diff --check
git add README.md DEVELOPMENT.md docs/support-matrix.md
git commit -m "docs: record native idle image acceptance"
```

Request review and merge. The designated Pi remains on the legacy image after
the test; the native image becomes the primary development direction, not the
everyday game image.

---

## Final milestone gate

Before calling Milestone 2 complete, verify all of the following from clean
main checkouts:

```text
[ ] Runtime production construction owns real Linux dependencies and exact idle path.
[ ] Runtime full host/sanitizer/audit/history/Arm gates pass.
[ ] FogCast native client is bounded, one-shot, protocol-1-only, and has no retry/fallback.
[ ] Agent selection is explicit; legacy default remains Main.
[ ] Game and development operations reject before runtime/FPGA mutation.
[ ] Runtime source and idle RBF pins are exact and image inputs verify cleanly.
[ ] prod/dev fixture behavior is unchanged.
[ ] native-dev builds reproducibly and passes rootfs/ELF/library/QEMU inspection.
[ ] Physical runtime/agent/idle/stop/reboot/boot-ID checks pass on 192.168.10.239.
[ ] ShadowCast capture is stable and non-corrupt.
[ ] Legacy dev reinstall and real Mega Drive launch/stop pass.
[ ] Evidence is linked from both repositories.
[ ] All 16 native system rows still say software: no and hardware: no.
[ ] Pi is left on the known-good legacy image.
```

Only after this gate is reviewed and merged should a new design/plan begin for
the Mega Drive vertical slice. Do not fold Mega Drive or development-RBF work
into fixes for this milestone.
