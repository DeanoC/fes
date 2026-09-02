# Native Mega Drive Vertical Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Launch Sonic 2 through FogCast's public API on the native image with visible 720p60 HDMI, one three-button player, Stop to idle, and an immediate second launch.

**Architecture:** Add one immutable production Mega Drive recipe to the runtime's sole profile table and execute it through focused core, video, and evdev-input components. FogCast keeps catalogue/cache/session ownership, translates the existing native launch request, creates one persistent native virtual gamepad, and packages one locked Mega Drive RBF. Development uses disposable diagnostic images; only the stabilized merged inputs receive a two-pass image build and formal hardware acceptance.

**Tech Stack:** C++14 runtime and daemon, Linux evdev/uinput, Cyclone V MMIO/SPI/I2C, Go target agent and host service, POSIX shell/Buildroot image tooling.

**Spec:** `docs/superpowers/specs/2026-09-02-native-megadrive-vertical-slice-design.md`

## Global Constraints

- Preserve one runtime, one daemon, one production profile table, and one Linux production construction path.
- Production native launch supports only `megadrive`; every other system remains unsupported.
- Use image-owned `/usr/share/mister-runtime/cores/megadrive.rbf`, never a FAT-side core.
- The initial accepted artifact must reproduce `MegaDrive_20260603.rbf`, size `4296864`, SHA-256 `0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839`, from an immutable official source.
- Support only player 1 D-pad, A/B/C, and Start. Do not implement saves, audio, six-button mode, multiplayer, remapping, or hot-plug recovery.
- Keep conventional Main, MGL launch, and automatic legacy fallback absent from native execution.
- Validate the complete launch and exact persistent virtual-input identity before FPGA mutation.
- After mutation, attempt cleanup to idle exactly once; cleanup failure means `reboot_required`.
- Use the fast diagnostic-image loop during development. Full two-pass image builds occur only after stabilized reviewed inputs are pinned.
- Do not claim software support before complete runtime/FogCast software gates or hardware support before dated exact-image physical evidence.

## Repository and file map

Runtime work is based on fresh `DeanoC/libmister-runtime` `origin/main` in an isolated worktree:

- `include/libmister-runtime/runtime.h`: public profile, prepared-launch, lifecycle, and hardware contracts.
- `src/profile.cpp`: canonical profile validation and preparation.
- `src/native/core_loader.*`: core reset/status/probe/media wire operations.
- `src/native/video*`: shared fixed-video recipe and bring-up.
- `src/native/input.*`: new evdev input-session contract and state machine.
- `src/native/linux/input.*`: new Linux discovery/read implementation.
- `src/native/hardware.*`: launch/idle orchestration.
- `src/linux/production_hardware.*`: sole production object graph and profile registry.
- `tests/support/*` and `tests/unit/*`: fakes and focused tests.
- `tests/integration/daemon_server_test.cpp`: real protocol/lifecycle integration.
- `README.md`, `ARCHITECTURE.md`, `docs/support-matrix.md`: current runtime truth.

FogCast work continues from this plan branch after rebasing onto fresh `origin/main`:

- `internal/misterruntime/{client,runtime}.*`: native daemon request translation and status mapping.
- `internal/bridge/uinput_linux.go`: real uinput device creation and event writes.
- `internal/input/controller.go`: persistent native gamepad lifetime and lease gating.
- `cmd/mister-agent/main.go`: backend-specific composition.
- `protocol/{types,remote_input}.*`: explicit C input code and validation.
- `build/native-runtime.inputs.lock.toml`: runtime, idle, and Mega Drive artifact identities.
- `scripts/{fetch-native-runtime-inputs,verify-native-runtime-inputs}.sh`: immutable fetch/verification.
- `buildroot/package/mister-runtime/mister-runtime.mk` and `buildroot/board/fogcast-target/native-post-build.sh`: installation.
- `scripts/verify-target-image.sh` and image fixtures: structural exclusion/inclusion gates.
- `fogcast/service.*` and native smoke fixtures: public-path acceptance.
- `README.md`, `docs/ARCHITECTURE.md`, `docs/DEVELOPMENT.md`, `docs/hardware/*`: current product truth and evidence.

---

### Task 1: Freeze the real Mega Drive artifact and wire contract

**Files:**
- Create: `libmister-runtime/docs/provenance/megadrive-20260603.md`
- Create: `libmister-runtime/tests/fixtures/megadrive-profile-v1.txt`
- Test: `libmister-runtime/tests/profile_provenance_test.sh`
- Modify: `libmister-runtime/Makefile`

**Interfaces:**
- Consumes: designated-kit RBF identity from the spec; pinned `Main_MiSTer` and official Mega Drive core sources.
- Produces: a reviewed text fixture containing the exact core identity, reset/status words, file index/width, input command/bits, and fixed-video recipe identity used by Tasks 2–5.

- [ ] **Step 1: Write the failing provenance guard**

Add `tests/profile_provenance_test.sh` that requires one fixture with these immutable headers and rejects zero or duplicate fields:

```sh
grep -Fx 'system=megadrive' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'core=MegaDrive' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'rbf_size=4296864' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'rbf_sha256=0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'media_role=cartridge' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'media_index=1' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'file_wire=little_endian_byte_pairs' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'player_count=1' tests/fixtures/megadrive-profile-v1.txt
grep -Fx 'video_recipe=menu_720p60' tests/fixtures/megadrive-profile-v1.txt
```

- [ ] **Step 2: Run the guard and record RED**

Run: `make profile-provenance-test`

Expected: FAIL because the fixture and target do not exist.

- [ ] **Step 3: Derive and record the exact protocol facts**

Trace the selected official RBF source plus the matching pinned Main release. Record source repository, immutable commit, source paths/lines, all status/reset values, player command, and D-pad/A/B/C/Start bits in the fixture and prose provenance file. Compare the fetched official RBF against the designated-kit size/hash before accepting it. If it differs, stop this task instead of changing the approved hash.

Fixture schema (the characterization step appends the sourced numeric values;
the gate accepts only concrete decimal or `0x` hexadecimal integers):

```text
system=megadrive
core=MegaDrive
rbf_size=4296864
rbf_sha256=0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
media_role=cartridge
media_index=1
file_wire=little_endian_byte_pairs
player_count=1
video_recipe=menu_720p60
```

The characterization must add exactly one value for each key in this concrete
required-key set:

```sh
numeric_keys='reset_assert_word initial_status_word reset_release_word player_command dpad_up_bit dpad_down_bit dpad_left_bit dpad_right_bit button_a_bit button_b_bit button_c_bit button_start_bit'
for key in $numeric_keys; do
  count=$(grep -Ec "^${key}=(0x[0-9A-Fa-f]+|[0-9]+)$" tests/fixtures/megadrive-profile-v1.txt)
  test "$count" -eq 1
done
```

The committed values must be extracted from the cited immutable sources. The
guard also requires every supported input bit to be nonzero and disjoint.

- [ ] **Step 4: Make the provenance gate executable and GREEN**

Add the target to `Makefile`, run `make profile-provenance-test` and
`git diff --check`, and confirm every required assignment has one concrete
value and every cited source revision is immutable.

- [ ] **Step 5: Commit**

```bash
git add Makefile docs/provenance/megadrive-20260603.md tests/fixtures/megadrive-profile-v1.txt tests/profile_provenance_test.sh
git commit -m "test: freeze Mega Drive native protocol provenance"
```

### Task 2: Add the one production profile and prepared recipe

**Files:**
- Modify: `libmister-runtime/include/libmister-runtime/runtime.h`
- Modify: `libmister-runtime/src/profile.cpp`
- Modify: `libmister-runtime/src/linux/production_hardware.cpp`
- Modify: `libmister-runtime/tests/support/test_profiles.hpp`
- Modify: `libmister-runtime/tests/unit/profile_test.cpp`
- Modify: `libmister-runtime/tests/unit/native_hardware_test.cpp`

**Interfaces:**
- Consumes: exact Task 1 fixture facts.
- Produces: `CoreRecipe`, `InputRecipe`, profile-owned `PreparedLaunch::core`, `PreparedLaunch::input`, and `ProductionProfiles()` containing only Mega Drive.

- [ ] **Step 1: Write failing production-profile tests**

Assert:

```cpp
const mister::Profiles& profiles = mister::ProductionProfiles();
mister::Launch launch;
launch.system = "megadrive";
launch.rbf = "/usr/share/mister-runtime/cores/megadrive.rbf";
launch.media.push_back({"cartridge", "/media/fat/fogcast/cache/sonic2.bin"});
mister::PreparedLaunch prepared;
assert(profiles.Prepare(launch, &prepared).ok());
assert(prepared.system == "megadrive");
assert(prepared.expected_core == "MegaDrive");
assert(prepared.media.size() == 1 && prepared.media[0].index == 1);
assert(prepared.input.player_count == 1);
```

Also reject every other system, extra settings, extra/duplicate media, wrong RBF path, invalid extensions, zero/oversized ROM metadata, and malformed recipe records atomically.

- [ ] **Step 2: Run focused RED**

Run: `make build/tests/unit/profile_test build/tests/unit/native_hardware_test && ./build/tests/unit/profile_test`

Expected: FAIL because the production table is empty and recipe fields do not exist.

- [ ] **Step 3: Extend the plain-data profile contract**

Add value types, not callbacks or profile pointers:

```cpp
enum class FileWireFormat { little_endian_byte_pairs };

struct CoreRecipe {
  std::uint16_t reset_assert_word = 0;
  std::uint16_t initial_status_word = 0;
  std::uint16_t reset_release_word = 0;
  FileWireFormat file_wire = FileWireFormat::little_endian_byte_pairs;
};

struct InputRecipe {
  std::uint8_t player_count = 0;
  std::uint16_t player_command = 0;
  std::uint16_t up = 0, down = 0, left = 0, right = 0;
  std::uint16_t a = 0, b = 0, c = 0, start = 0;
};
```

Store these in `Profile` and copy them into `PreparedLaunch`. Validate nonzero, disjoint supported input bits and exactly one production player. Keep synthetic test recipes explicit.

- [ ] **Step 4: Build `ProductionProfiles()` once**

Construct one static validated registry from the Task 1 facts. Abort production startup if constructing that compile-time-owned record fails; never fall back to an empty or fake table.

- [ ] **Step 5: Run GREEN and mutations**

Run focused tests, then separately mutate core identity, media index, one reset word, player command, and one button bit. Each mutation must fail a named assertion. Restore and rerun GREEN.

- [ ] **Step 6: Commit**

```bash
git add include/libmister-runtime/runtime.h src/profile.cpp src/linux/production_hardware.cpp tests/support/test_profiles.hpp tests/unit/profile_test.cpp tests/unit/native_hardware_test.cpp
git commit -m "feat: add production Mega Drive profile"
```

### Task 3: Implement exact Mega Drive core and video bring-up

**Files:**
- Modify: `libmister-runtime/src/native/core_loader.hpp`
- Modify: `libmister-runtime/src/native/core_loader.cpp`
- Modify: `libmister-runtime/src/native/video.hpp`
- Modify: `libmister-runtime/src/native/video.cpp`
- Modify: `libmister-runtime/src/native/video_recipe.*`
- Modify: `libmister-runtime/tests/unit/core_loader_test.cpp`
- Modify: `libmister-runtime/tests/unit/video_test.cpp`
- Modify: `libmister-runtime/tests/unit/video_recipe_test.cpp`

**Interfaces:**
- Consumes: `CoreRecipe`, existing `Spi`, `I2c`, `Clock`, `Artifact`.
- Produces: `CoreLoader::AssertReset`, `ApplyInitialStatus`, `Attach(..., FileWireFormat, ...)`, `ReleaseReset`, and profile-driven `FixedVideoBringup::BringUp`.

- [ ] **Step 1: Write chronological RED tests**

Use one shared call ledger and require this exact prefix:

```text
core.reset.assert
core.probe:MegaDrive
core.status.initial
core.media.select:1
core.media.extension:.bin
core.media.enable
core.media.data:all bytes once
core.media.complete
video.adv.initialize
video.timing:menu_720p60
video.adv.mode
video.link.ready
input.neutral
core.reset.release
```

Inject failure at every SPI/I2C/read point and assert no later call occurs.

- [ ] **Step 2: Run focused RED**

Run the three focused binaries. Expected: missing methods and the Menu-only admission rule fail.

- [ ] **Step 3: Implement minimal profile-driven operations**

Replace synthetic key/value `Configure` usage in production launch with exact status/reset words. Preserve `Probe`. Make `Attach` consume the prepared wire format and reject unsupported formats before selecting file I/O. Generalize `MenuVideoBringup` to `FixedVideoBringup` while keeping the existing Menu test ledger byte-identical.

- [ ] **Step 4: Run GREEN and mutation audit**

Mutate each reset/status word, media index, chunk-boundary byte pairing, early reset release, one video timing word, and link predicate. Require named failures; restore all changes.

- [ ] **Step 5: Commit**

```bash
git add src/native/core_loader.* src/native/video* tests/unit/core_loader_test.cpp tests/unit/video_test.cpp tests/unit/video_recipe_test.cpp
git commit -m "feat: add Mega Drive core bring-up sequence"
```

### Task 4: Add the bounded native evdev input session

**Files:**
- Create: `libmister-runtime/src/native/input.hpp`
- Create: `libmister-runtime/src/native/input.cpp`
- Create: `libmister-runtime/src/native/linux/input.hpp`
- Create: `libmister-runtime/src/native/linux/input.cpp`
- Create: `libmister-runtime/tests/support/fake_input.*`
- Create: `libmister-runtime/tests/unit/input_test.cpp`
- Modify: `libmister-runtime/Makefile`

**Interfaces:**
- Consumes: `InputRecipe`, exact Linux input identity, `Spi`, absolute deadline, session generation.
- Produces: `InputSession::Open`, `Start`, `Neutralize`, `Stop`, and a one-shot fault callback carrying its generation and `Error`.

- [ ] **Step 1: Write failing state-machine tests**

Cover exact device selection, absent/duplicate identity rejection, complete `input_event` framing, SYN_REPORT batching, digital and axis D-pad mapping, A/B/C/Start, duplicate suppression, neutral first/last, stop/join, old-generation rejection, EOF/read/SPI faults, and unsupported inputs.

Interface:

```cpp
struct InputDeviceIdentity {
  std::string name;
  std::uint16_t bus, vendor, product, version;
};

class InputSession {
public:
  Error Open(const InputDeviceIdentity&, const InputRecipe&,
             std::uint64_t absolute_deadline_ms);
  Error Start(std::uint64_t generation,
              std::function<void(std::uint64_t, Error)> on_fault);
  Error Neutralize(std::uint64_t absolute_deadline_ms);
  Error Stop(std::uint64_t absolute_deadline_ms);
};
```

- [ ] **Step 2: Run RED**

Run: `make build/tests/unit/input_test`

Expected: FAIL because the files and target do not exist.

- [ ] **Step 3: Implement the platform-neutral state machine and Linux adapter**

Use `poll` plus a cancellation descriptor; never detach a worker. Discover `/dev/input/event*`, query `EVIOCGNAME` and `EVIOCGID`, and require one exact match. Commit only at `SYN_REPORT`. Permit one player and the recipe's eight controls. Device removal is a fault, not a reconnect.

- [ ] **Step 4: Run GREEN, sanitizers, and TSan**

Run focused tests under normal, ASan/UBSan, and TSan builds. Mutate identity matching, C mapping, SYN boundary, neutralization, cancellation, and generation checks separately.

- [ ] **Step 5: Commit**

```bash
git add Makefile src/native/input.* src/native/linux/input.* tests/support/fake_input.* tests/unit/input_test.cpp
git commit -m "feat: add native Mega Drive input session"
```

### Task 5: Integrate runtime launch, cleanup, and relaunch

**Files:**
- Modify: `libmister-runtime/src/native/hardware.*`
- Modify: `libmister-runtime/src/linux/production_hardware.cpp`
- Modify: `libmister-runtime/src/runtime.cpp`
- Modify: `libmister-runtime/tests/unit/native_hardware_test.cpp`
- Modify: `libmister-runtime/tests/unit/runtime_test.cpp`
- Modify: `libmister-runtime/tests/integration/daemon_server_test.cpp`
- Modify: `libmister-runtime/README.md`
- Modify: `libmister-runtime/ARCHITECTURE.md`
- Modify: `libmister-runtime/docs/support-matrix.md`

**Interfaces:**
- Consumes: Tasks 2–4 runtime components.
- Produces: complete production `launch -> running_game -> stop -> idle -> launch` behavior and one asynchronous input-fault cleanup path.

Use one explicit callback boundary for asynchronous hardware faults:

```cpp
struct HardwareFault {
  std::uint64_t generation;
  Error error;
};

class HardwareFaultSink {
public:
  virtual ~HardwareFaultSink() = default;
  virtual void ReportHardwareFault(HardwareFault fault) = 0;
};
```

`Hardware` gains `SetFaultSink(HardwareFaultSink*)`; `Runtime::Impl` is the sole
sink and assigns a monotonically increasing generation before each launch.
Production construction installs the sink before startup idle. Test hardware
may report through the same boundary, never by reaching into runtime state.
The callback only enqueues the fault into Runtime's mutation lane; it must not
perform cleanup on the input worker thread, which would make `Stop()` self-join.

- [ ] **Step 1: Write end-to-end RED tests**

Require complete preflight before `fpga.Program`, the exact chronological sequence, `running_game` only after reset release, one cleanup after every post-program failure, input-fault cleanup bound to the active generation, cleanup failure to `reboot_required`, Stop ordering, and immediate relaunch with new descriptors/thread/generation.

The integration ledger must start with this complete prefix before the Task 3
post-program operations:

```text
profile.validate:megadrive
input.resolve:FogCast Virtual Gamepad
artifact.open:megadrive.rbf
artifact.open:sonic2.bin
media.sort:1
fpga.program
```

- [ ] **Step 2: Run RED**

Run runtime, native-hardware, and daemon integration binaries. Expected: launch lacks video/input/reset orchestration and production profile assertions fail.

- [ ] **Step 3: Implement the single owner**

Extend `NativeHardware` with `FixedVideoBringup&` and `InputSession&`. Implement
`SetFaultSink`, and have the input callback emit one `HardwareFault` containing
the generation passed to `InputSession::Start`. `Runtime::Impl` ignores stale
generations; for the active generation it transitions through the existing
single mutation lane, neutralizes/stops input, and attempts idle cleanup exactly
once. `LoadIdle` must be safe both at process startup and after a game session.

- [ ] **Step 4: Update truthful runtime documentation**

Mark Mega Drive `software: yes`, `hardware: no`; document the exact sequence and all exclusions. Do not claim visible HDMI or playable input yet.

- [ ] **Step 5: Run full runtime gates**

Run `make -j4 test`, `make sanitize`, `make tsan`, `make archive-audit`, `make active-tree-test`, history/provenance checks, `git diff --check`, and the pinned ARM target build/audit. Perform deliberate sequence and cleanup mutations, then restore GREEN.

- [ ] **Step 6: Commit and obtain independent review**

```bash
git add include src tests Makefile README.md ARCHITECTURE.md docs/support-matrix.md docs/provenance
git commit -m "feat: launch native Mega Drive games"
```

Review the frozen range. Fix accepted findings under new RED/GREEN cycles. Only after READY, push and open the runtime PR; do not merge until its checks/review pass.

### Task 6: Create the persistent native virtual gamepad in FogCast

**Files:**
- Modify: `FogCast-POC/protocol/remote_input.go`
- Modify: `FogCast-POC/protocol/remote_input_test.go`
- Modify: `FogCast-POC/internal/bridge/uinput_linux.go`
- Modify: `FogCast-POC/internal/bridge/uinput_stub.go`
- Modify: `FogCast-POC/internal/bridge/bridge_test.go`
- Modify: `FogCast-POC/internal/input/controller.go`
- Modify: `FogCast-POC/internal/input/controller_test.go`
- Modify: `FogCast-POC/cmd/mister-agent/main.go`
- Modify: `FogCast-POC/cmd/mister-agent/main_test.go`

**Interfaces:**
- Consumes: native/main backend selection and existing authenticated input frames.
- Produces: a persistent native-only `FogCast Virtual Gamepad` with fixed Linux identity and explicit C input code; leases gate delivery.

- [ ] **Step 1: Write Linux-seam RED tests**

Assert exact uinput setup ioctls/capabilities, identity, one create/one destroy per agent lifetime, no writes without an active lease, release-all on detach, explicit C mapping, and unchanged legacy construction. Use a syscall seam; do not require `/dev/uinput` in host tests.

- [ ] **Step 2: Run focused RED**

Run: `go test ./protocol ./internal/bridge ./internal/input ./cmd/mister-agent`

Expected: C and persistent device construction do not exist.

- [ ] **Step 3: Implement a real persistent device**

Use Linux uinput ioctls to set EV_KEY/EV_ABS/SYN capabilities and create the fixed name/ID. Native composition creates it before coordinator initialization; Main composition retains the old path. `Attach` starts only the authenticated stream and `Detach` releases state without destroying the native device.

- [ ] **Step 4: Run GREEN and race tests**

Run focused packages and `go test -race` for bridge/input/agent. Mutate C mapping, identity, lease gate, release-all, and destruction ordering separately.

- [ ] **Step 5: Commit**

```bash
git add protocol/remote_input* internal/bridge internal/input cmd/mister-agent
git commit -m "feat: create native virtual gamepad"
```

### Task 7: Translate native launches and package the locked RBF

**Files:**
- Modify: `FogCast-POC/internal/misterruntime/client.*`
- Modify: `FogCast-POC/internal/misterruntime/runtime.*`
- Modify: `FogCast-POC/internal/agent/coordinator_test.go`
- Modify: `FogCast-POC/internal/agent/content_test.go`
- Modify: `FogCast-POC/fogcast/service_test.go`
- Modify: `FogCast-POC/build/native-runtime.inputs.lock.toml`
- Modify: `FogCast-POC/scripts/fetch-native-runtime-inputs.sh`
- Modify: `FogCast-POC/scripts/verify-native-runtime-inputs.sh`
- Modify: `FogCast-POC/buildroot/package/mister-runtime/mister-runtime.mk`
- Modify: `FogCast-POC/buildroot/board/fogcast-target/native-post-build.sh`
- Modify: `FogCast-POC/scripts/verify-target-image.sh`
- Modify: relevant `scripts/tests/*` fixtures

**Interfaces:**
- Consumes: reviewed/merged runtime commit and Task 1 official artifact identity.
- Produces: `Control.Launch(ctx, LaunchRequest)`, native `Prepare/Launch` support only for Mega Drive, and exact image packaging.

- [ ] **Step 1: Update fixtures first and capture RED**

Require the new runtime merge and `[megadrive_rbf]` lock section, exact source/revision/path/hash/size/install path, one packaged file, and rejection of mismatch/duplicate/FAT launch. Require native runtime requests exactly equivalent to:

```json
{"protocol":1,"operation":"launch","system":"megadrive","rbf":"/usr/share/mister-runtime/cores/megadrive.rbf","media":{"cartridge":"/media/fat/fogcast/cache/sonic2.bin"},"settings":{}}
```

- [ ] **Step 2: Run focused RED**

Run native-runtime client/runtime, agent content/coordinator, service, immutable-input, rootfs, and image fixtures. Expected: native Prepare/Launch still reject and no artifact lock exists.

- [ ] **Step 3: Implement strict client and runtime mapping**

Add typed launch payload encoding to `Control`. `Runtime.Prepare` accepts only registry system `megadrive`, validates the target ROM absolute path, and returns the fixed RBF plus cartridge role. `Runtime.Launch` maps only a valid `running_game/game/megadrive/MegaDrive` response to active. Map daemon errors directly to stable FogCast errors; reconcile a lost response through Status only.

- [ ] **Step 4: Lock, fetch, install, and verify the RBF**

Extend the existing lock parser without adding a second lock. Fetch to a temporary file, verify SHA/size, and atomically promote to cache. Install mode `0644` at the fixed path. Image manifests and build-inputs contain both RBF identities. Native verification requires exactly one of each and rejects Main/MGL/FIFO startup.

- [ ] **Step 5: Run affected and full software gates**

Run focused tests, `make -j4 test`, `make vet`, all changed shell scripts under `sh -n`, and `git diff --check`. Do not perform a full image build yet.

- [ ] **Step 6: Commit**

```bash
git add internal/misterruntime internal/agent fogcast protocol build buildroot scripts
git commit -m "feat: integrate native Mega Drive launch"
```

### Task 8: Fast diagnostic-image hardware loop

**Files:**
- Modify only when a diagnosed bug requires a tested source change.
- Record diagnostic evidence outside Git under the designated task TMPDIR.

**Interfaces:**
- Consumes: last verified native image, fresh ARM runtime/agent binaries, verified candidate RBF, designated Pi, ShadowCast.
- Produces: diagnostic evidence sufficient to stabilize code; never support or reproducibility claims.

- [ ] **Step 1: Cross-build changed artifacts**

Build the runtime with the pinned ARM 10.2 toolchain and the agent with `GOOS=linux GOARCH=arm GOARM=7`. Run `file`/`readelf` and archive closure checks.

- [ ] **Step 2: Create a disposable derived image**

Copy the last verified native image to a uniquely named diagnostic artifact. Inject only the verified runtime, agent, RBF, and build-input record. Hash original and derived images; never overwrite the verified source image.

- [ ] **Step 3: Deploy and diagnose in layers**

Verify idle first, then call the public Sonic 2 launch. Capture runtime phase logs, status, core identity, ADV state, process identity, input device identity, and five numbered frames. Exercise direction/jump, Stop, and relaunch only after earlier gates pass.

- [ ] **Step 4: Fix only demonstrated defects**

For each failure, preserve evidence, use systematic debugging, add a focused failing test, implement the smallest correction, rerun affected software gates, rebuild only changed artifacts, and repeat with a new disposable image. Do not tune timeouts or add fallback behavior to pass hardware.

- [ ] **Step 5: Stabilization gate**

Stop the diagnostic loop only when launch, visible gameplay, input, Stop, and immediate relaunch pass twice without source changes. Restore the last reviewed legacy dev/Menu image if the target is not needed for the next immediate diagnostic.

### Task 9: Final review, pin, reproducible build, and formal acceptance

**Files:**
- Modify: `FogCast-POC/README.md`
- Modify: `FogCast-POC/docs/ARCHITECTURE.md`
- Modify: `FogCast-POC/docs/DEVELOPMENT.md`
- Create: `FogCast-POC/docs/hardware/native-megadrive-baseline.md`
- Modify: approved design/support documents only where present-tense truth changes.

**Interfaces:**
- Consumes: merged runtime PR, stabilized FogCast branch, exact lock, designated hardware.
- Produces: exact reproducible image and dated hardware evidence; FogCast PR ready for review.

- [ ] **Step 1: Merge reviewed runtime and pin its merge commit**

Update the lock and the two real-lock fixtures under fixture-first RED/GREEN. Require clean local/origin runtime main and exact lock equality.

- [ ] **Step 2: Run fresh final software gates**

Run full runtime gates at merged HEAD and full FogCast `make -j4 test`, `make vet`, syntax, diff, scope, and lock verification. Stop on any failure.

- [ ] **Step 3: Perform the mandatory fresh image build**

Build native-dev twice from independent work directories with the approved container wrapper and exact runtime/RBF lock. Require byte-identical images. Run structural/library/input/manifest/QEMU verifiers and preserve hashes.

- [ ] **Step 4: Execute formal physical acceptance literally**

Deploy once; require native idle; launch Sonic 2 through the public API; require exact running identity; capture and inspect five collision-proof frames; prove directional movement and jump; Stop to visible idle; immediately relaunch and prove gameplay/input; Stop to idle again. On failure, stop later gates and restore legacy dev/Menu.

- [ ] **Step 5: Prove unchanged legacy rollback**

Build legacy prod/dev twice only now, verify both, deploy fresh legacy dev, launch Sonic 2 through the unchanged path, Stop, and require `MENU`.

- [ ] **Step 6: Write evidence-backed truth**

Record exact FogCast/runtime commits, source identities, image hashes, boot IDs, installed hashes, processes/FIFO, core identities, input identity, control evidence, five frame hashes/inspection, stop/relaunch results, and legacy rollback. Mark Mega Drive hardware yes only if every gate passed; retain every exclusion.

- [ ] **Step 7: Commit, review, and open the FogCast PR**

```bash
git add README.md docs build buildroot internal protocol fogcast scripts cmd
git commit -m "feat: support native Mega Drive launch"
```

Run an independent frozen-range review, fix accepted findings under RED/GREEN, rerun proportional gates, then push/open a PR depending on the merged runtime change. Do not merge without the user's authorization and passing automated review.
