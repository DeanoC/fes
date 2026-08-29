# M2 Linux Mailbox FogCast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a development-profile-only FogCast hardware coordinator mode that safely loads the M2 mailbox RBF through compatibility Main, reads `OSS FPGA OK\n` from HPS GPI/GPO, persists evidence, and recovers normal Main ownership through reboot.

**Architecture:** A new `internal/hardwareowner` package supplies the single crash-consistent lock, record, generations, and admission gate used by the agent, development command, and dev supervisor. A new `internal/fpgadev` package validates artifacts, performs the synchronous Main handoff, qualifies a true no-owner checkpoint, owns mailbox MMIO only after atomic transfer, records the result, and requests recovery. Linux mechanisms stay behind focused interfaces and the public FogCast API remains unchanged.

**Tech Stack:** Go 1.26.5, Linux `flock`, `/proc`, `/dev/mem`, Cyclone V FPGA-manager/bridge registers, TOML development-profile configuration, existing FogCast protocol types, shell packaging tests.

**Spec:** `docs/superpowers/specs/2026-08-27-linux-mailbox-dev-loader-design.md`, amended candidate SHA-256 `fe9f8ecc42f9be44a627863562f80a1c92eeb667d868be0071c858f4d388b514`, derived from the operator-approved lifecycle plus the reviewed schema-2 Main identity and signed descriptor-bound synthesis-resource evidence corrections, and pending operator re-approval.

## Global Constraints

- Software-only isolated implementation and review may proceed against the amended candidate. No staging, commit, cumulative integration, target contact, or HIL begins until the operator explicitly re-approves the exact amended spec hash recorded above.
- Root coordinator owns integration, evidence, and hardware authorization; one Luna-max worker owns each task in a fresh worktree branched from the last reviewed cumulative integration head, with no concurrent overlapping writers.
- Before every `git add` or `git commit`, the root coordinator confirms current explicit user authorization; plan text and reviewer approval are not authorization.
- Sol reviews owner/lifecycle/MMIO decisions; independent Vega reviews every implementation task before integration.
- The cumulative branch starts from `codex/m2-linux-mailbox-design`; each approved task is integrated before the next task worktree is created. Reviewers are read-only.
- Public HTTP/JSON protocol is unchanged. Fenced states project as existing `failed` plus `mister_unavailable`.
- Development paths exist only in the explicit SSH-enabled development profile and never in production packaging.
- Build-profile authority is the compile-time `fpgadev` Go build tag plus the dev package manifest; production builds omit tagged commands and package tests attest their absence.
- Hardware owner state is `/var/lib/fogcast/hardware-owner-v1.json`; lock is `/run/fogcast/hardware-owner-v1.lock`; root-owned directories are `0700` and files are `0600`.
- Only `active_leases` grants hardware ownership. `requested_resources` is non-owning intent.
- Any failure at or after durable `recovering_intent` remains fenced and requires reboot reconciliation.
- Fixed bounds are: 10 ms mailbox poll/double-sample spacing; 5 seconds for stable Main absence including 250 ms stability; 2 seconds for no-owner qualification; 2 seconds for post-transfer HELLO; 1 second for each DATA/END/DONE advance; 10 seconds START-through-DONE; and 5 seconds for the reboot request.
- Recovery readiness is exactly Main executable identity, root-owned command FIFO of FIFO type, FPGA manager `operating`, and two `MENU` reads 10 ms apart within 30 seconds.
- No target path, identity, credential, or MMIO address enters public protocol or logs.
- The implementation is Software-tested until the separate integration plan records physical HIL observations.
- Rollback is removal of the dev-only binaries/config plus reboot to the unchanged compatibility image; no task writes flash or SD-card partitions.

## File Responsibility Map

- `internal/hardwareowner/types.go`: exact owner enums, record schema, invariants, and transition validation.
- `internal/hardwareowner/store.go`: canonical JSON load/atomic replace and generation allocation.
- `internal/hardwareowner/lock_linux.go`: no-follow exclusive `flock`; `lock_stub.go` fails closed elsewhere.
- `internal/hardwareowner/gate.go`: normal agent/supervisor admission held through check-and-use.
- `internal/fpgadev/manifest.go`: exact development manifest validation.
- `internal/fpgadev/artifact.go`, `artifact_linux.go`, `artifact_stub.go`: descriptor-bound artifact access.
- `internal/fpgadev/designation.go`: private designation and exact target-identity interface.
- `internal/fpgadev/result.go`: canonical result schema, framing, and atomic persistence.
- `internal/fpgadev/fifo.go`: synchronous bounded FIFO writer with no late goroutine.
- `internal/fpgadev/process_linux.go`: complete Main executable/PID/start-time observer.
- `internal/fpgadev/mailbox.go`: pure protocol decoder and state machine.
- `internal/fpgadev/platform.go`: policy-facing Linux mechanism interfaces.
- `internal/fpgadev/platform_linux.go`: bounded bridge/FPGA-manager/GPI/GPO mapping and qualification.
- `internal/fpgadev/run.go`: ordered lifecycle orchestration and first-failure preservation.
- `internal/fpgadev/recover.go`: new-boot reset reconciliation and normal-Main lease restoration.
- `cmd/mister-fpga-dev/main.go`: private target CLI and exact preflight/result grammar.
- `cmd/fogcast-dev-supervisor/main.go`: development-image Main/agent start/restart gate.
- `internal/agent/coordinator.go`: hold normal owner gate across launch/stop transitions.
- `internal/cast/controller.go`, `internal/input/controller.go`, `internal/httpapi/cast.go`: development-profile construction refusal and unavailable public routes.
- `cmd/mister-agent/main.go`: owner-store/gate wiring and fenced health projection.
- `internal/agentconfig/config.go`: strict dev-profile and owner-path configuration.
- `deploy/fpgadev/`: dev-only service scripts/config; never included in production.
- `scripts/package-fpgadev.sh`: deterministic dev-only package allowlist and hashes.
- `scripts/install-fpgadev.sh`: authorized remote install/uninstall/dry-run interface.
- `scripts/tests/fpgadev-package_test.sh`: packaging and supervisor fence tests.

---

### Task 1: Canonical Hardware Owner Record and Lock

**Files:**
- Create: `internal/hardwareowner/types.go`
- Create: `internal/hardwareowner/types_test.go`
- Create: `internal/hardwareowner/store.go`
- Create: `internal/hardwareowner/store_test.go`
- Create: `internal/hardwareowner/lock_linux.go`
- Create: `internal/hardwareowner/lock_stub.go`
- Create: `internal/hardwareowner/lock_test.go`

**Interfaces:**
- Produces: `Parse([]byte) (Record, error)`, `Record.Validate() error`, `Store.Load() (record Record, exists bool, err error)`, `Store.Replace(Record) error`, `Locker.Lock(context.Context) (Unlock, error)`; `exists=false` is reserved for first-install initialization and every other consumer fails closed.
- Produces exact types:

```go
type Unlock func() error
type Owner string
type State string
type Phase string
type Record struct {
    Schema uint64 `json:"schema"`
    State State `json:"state"`
    Phase Phase `json:"phase"`
    BootID string `json:"boot_id"`
    RunID string `json:"run_id"`
    GenerationHighWater uint64 `json:"generation_high_water"`
    ActiveSession string `json:"active_session"`
    ActiveGeneration uint64 `json:"active_generation"`
    ActiveMode string `json:"active_mode"`
    CandidateSession string `json:"candidate_session"`
    CandidateGeneration uint64 `json:"candidate_generation"`
    CandidateMode string `json:"candidate_mode"`
    QuiescingOwner Owner `json:"quiescing_owner"`
    CandidateOwner Owner `json:"candidate_owner"`
    ActiveOwner Owner `json:"active_owner"`
    ActiveLeases []string `json:"active_leases"`
    RequestedResources []string `json:"requested_resources"`
    FirstFailure string `json:"first_failure"`
}
```

- [ ] Write table tests for every valid spec row and one independent mutation for every field, unknown/duplicate JSON key, session/mode, high-water monotonicity, candidate preservation through recovery, generation rules, lexical arrays, run ID, boot ID, phase monotonicity, and owner/resource inconsistency. Assert every derived active lease names `(session,generation,mode,resource)`.
- [ ] Run `go test ./internal/hardwareowner -run 'Test(Parse|Validate)' -count=1`; expect package/build failures.
- [ ] Implement a token-walking duplicate-key check, strict decoder with trailing-data rejection, immutable validation helpers, and canonical `MarshalCanonical` field order. Do not normalize hostile input.
- [ ] Add filesystem tests with an injected expected UID equal to the test process for regular/no-symlink paths, `0600` enforcement, atomic replace, file and parent `fsync`, failed rename preservation, and concurrent `flock` exclusion/cancellation; a production-constructor test must assert expected UID `0`.
- [ ] Run `go test ./internal/hardwareowner -run 'Test(Store|Lock|Replace|Fsync|Concurrent)' -count=1`; expect failures before the filesystem implementation exists.
- [ ] Implement `Store.Load`, canonical temporary-file write/fsync/rename/parent-fsync `Store.Replace`, and Linux no-follow `flock` with cancellation plus a fail-closed non-Linux stub. Preserve the old record on every failed replacement.
- [ ] Run `go test -race ./internal/hardwareowner -count=1`; expect PASS.
- [ ] Commit only these seven files with `git commit -m "feat: add durable hardware owner store"` after Vega approval.

### Task 2: Normal Admission Gate and Restricted Development Profile

**Files:**
- Create: `internal/hardwareowner/gate.go`
- Create: `internal/hardwareowner/gate_test.go`
- Modify: `internal/agent/coordinator.go`
- Modify: `internal/agent/coordinator_test.go`
- Modify: `cmd/mister-agent/main.go`
- Modify: `cmd/mister-agent/main_test.go`
- Create: `cmd/mister-agent/capability_dev.go`
- Create: `cmd/mister-agent/capability_prod.go`
- Modify: `internal/agentconfig/config.go`
- Modify: `internal/agentconfig/config_test.go`
- Modify: `internal/cast/controller.go`
- Modify: `internal/cast/controller_test.go`
- Modify: `internal/input/controller.go`
- Modify: `internal/input/controller_test.go`
- Modify: `internal/httpapi/cast.go`
- Modify: `internal/httpapi/cast_test.go`
- Modify: `internal/httpapi/server.go`
- Modify: `internal/httpapi/server_test.go`
- Modify: `internal/httpapi/content.go`
- Modify: `internal/httpapi/content_test.go`

**Interfaces:**
- Consumes Task 1 store/lock.
- Produces `type MaintenanceFence interface { Clear() error }` and `type NormalGate interface { Enter(context.Context) (hardwareowner.Unlock, error) }` for permitted core transitions. `Gate.Enter` checks both canonical owner state and the injected maintenance fence while holding the owner lock. Development-profile configuration prevents construction of cast, presentation, audio, input, and controller-route facilities.

- [ ] Add failing core tests that pause after admission and prove development intent cannot interleave, plus a fake nonterminal maintenance journal that blocks admission even with canonical `normal_main`. Add development-profile composition tests proving auxiliary controllers/workers/descriptors/streams are absent. Register explicit controller-free cast/input fallback handlers in the development profile and prove those public routes return existing `CodeMiSTerUnavailable`, rather than 404, without a hardware call.
- [ ] Run `go test ./internal/agent ./internal/agentconfig ./internal/cast ./internal/input ./internal/httpapi ./cmd/mister-agent -run 'Owner|Fence|Admission|Profile|Capability|Unavailable' -count=1` and the same command with `-tags fpgadev`; expect the new behavior tests to fail in both build profiles.
- [ ] Implement `Gate.Enter` to acquire the shared lock, require `MaintenanceFence.Clear`, reload/validate canonical current-boot `normal_main`, hold check/use across each permitted core transition, and release only after its durable terminal/teardown state. Do not add cross-process auxiliary quiescence IPC; those facilities are unavailable in this profile.
- [ ] Add strict config fields `build_profile`, `development_profile`, `hardware_owner_path`, `hardware_owner_lock`, `designation_path`, and `target_identity_path`; require profile `development` plus compile-time dev capability, and reject all dev fields in profile `production`.
- [ ] Wire the gate in `cmd/mister-agent`; health/status remains public-schema-compatible and path-free.
- [ ] Run `go test -race ./internal/hardwareowner ./internal/agent ./internal/agentconfig ./internal/cast ./internal/input ./internal/httpapi ./cmd/mister-agent -count=1` and the same package list with `-tags fpgadev`; untagged tests prove capability absence and tagged tests exercise the restricted development-profile composition. Expect PASS.
- [ ] Commit with `git commit -m "feat: fence agent transitions with hardware ownership"` after Sol and Vega approval.

### Task 3: Exact Manifest, Result, and CLI Framing Contracts

**Files:**
- Create: `internal/fpgadev/manifest.go`
- Create: `internal/fpgadev/manifest_test.go`
- Create: `internal/fpgadev/artifact.go`
- Create: `internal/fpgadev/artifact_linux.go`
- Create: `internal/fpgadev/artifact_stub.go`
- Create: `internal/fpgadev/artifact_test.go`
- Create: `internal/fpgadev/result.go`
- Create: `internal/fpgadev/result_test.go`
- Create: `internal/fpgadev/testdata/manifest-oss.json`

**Interfaces:**
- Produces `ParseManifest(raw []byte) (Manifest, error)`, `ArtifactAccess.Bind(Manifest, string) (ArtifactBinding, error)`, `ArtifactBinding.Revalidate() error`, `Result.Validate() error`, `ResultStore.Create(Result) error`, `ResultStore.MarkRecoveryFailed(expected Result) error`, `PreflightLine(Code) string`, and `ResultLine(Result) string`. `Create` is exclusive; `MarkRecoveryFailed` is the sole update and conditionally changes only matching `pending` to `failed`. `ArtifactBinding` retains the protected directory descriptor plus `(device,inode,size,sha256,path)` until FIFO dispatch.
- Manifest/result structs and enums exactly mirror spec schema v1, including result `session`, `generation`, and `mode` bound to the active development lease; maximum RBF size is `16_777_216` bytes.

- [ ] Write hostile parser matrices covering missing/extra/duplicate keys, noncanonical hex, unsafe filename, lane/experiment/board mismatch, file type/mode/owner/link count, size/hash mismatch, run collision, inconsistent payload/hash/terminal word, and exact field order/newline. Add result-store filesystem tests for root ownership/mode, no-follow directory/file handling, canonical temporary write, file and directory fsync, exclusive create/collision refusal, and old-result preservation on every injected failure. Test the sole conditional update: exact identity and all other fields equal permits `pending`→`failed`; any run/session/generation/mode/artifact/source/primary/payload/elapsed mismatch, non-pending state, repeat, or concurrent replacement is rejected byte-for-byte.
- [ ] Run `go test ./internal/fpgadev -run 'Test(Manifest|Artifact|Result|Framing)' -count=1`; expect package failures.
- [ ] Implement strict generic parsers without `map[string]any` policy decisions and implement exclusive `ResultStore.Create` plus identity-bound `MarkRecoveryFailed` with the tested canonical atomic persistence contract. In `artifact_linux.go`, open the staging directory descriptor with `O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW`, then open `top.rbf` through `unix.Openat2` with `O_RDONLY|O_CLOEXEC|O_NOFOLLOW` and `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS`; validate/hash the descriptor and revalidate the same binding immediately before FIFO write. `artifact_stub.go` fails unsupported.
- [ ] Assert preflight success has the exact one-line stdout and empty stderr, preflight failure and pre-intent `run` failure have their distinct exact one-line stderr grammars, and post-intent success/failure framing matches the spec byte-for-byte.
- [ ] Run `go test -race ./internal/fpgadev -run 'Test(Manifest|Artifact|Result|Framing)' -count=1`; expect PASS.
- [ ] Commit with `git commit -m "feat: define FPGA development evidence contracts"` after Vega approval.

### Task 4: Synchronous FIFO Dispatch and Complete Main Process Observer

**Files:**
- Create: `internal/fpgadev/fifo.go`
- Create: `internal/fpgadev/fifo_test.go`
- Create: `internal/fpgadev/process.go`
- Create: `internal/fpgadev/process_linux.go`
- Create: `internal/fpgadev/process_test.go`

**Interfaces:**
- Produces `FIFO.Dispatch(ctx context.Context, command string) (Attempt, error)` where `Attempt` is `NotInvoked`, `Invoked`, or `Completed`; no goroutine survives return.
- Produces `Observer.Snapshot() ([]ProcessIdentity, error)` and `Observer.WaitStableAbsent(ctx, baseline, 250*time.Millisecond) error`; identity is executable `(dev,inode,sha256)` plus PID and `/proc/<pid>/stat` start time.

- [ ] Write a real-FIFO test where no reader exists, cancel the bounded nonblocking/poll loop, attach a reader after return, and prove no bytes arrive. Cover short write, close error, exact one-line command, and every invocation treated as potentially consumed.
- [ ] Write fake `/proc` tests for original exit, double-fork replacement with the same executable identity, reparenting, PID reuse/start-time mismatch, transient 249 ms absence, stable 250 ms absence, unreadable proc entry, and executable replacement.
- [ ] Run `go test ./internal/fpgadev -run 'Test(FIFO|Process)' -count=1`; expect failures.
- [ ] Implement the writer synchronously with nonblocking open/write plus context-aware `poll`; never spawn a goroutine. Implement a full `/proc` rescan on each poll, not descendant-only tracking.
- [ ] Run `go test -race ./internal/fpgadev -run 'Test(FIFO|Process)' -count=1`; expect PASS.
- [ ] Commit with `git commit -m "feat: add bounded Main handoff adapters"` after Vega approval.

### Task 5: Pure Mailbox Protocol Engine

**Files:**
- Create: `internal/fpgadev/mailbox.go`
- Create: `internal/fpgadev/mailbox_test.go`

**Interfaces:**
- Produces `RunMailbox(ctx context.Context, regs Registers, clock Clock) (Observation, error)` with:

```go
type Registers interface {
    ReadGPI() (uint32, error)
    WriteGPO(uint32) error
    ReadGPO() (uint32, error)
    Close() error
}
type Clock interface {
    Now() time.Time
    After(time.Duration) <-chan time.Time
}
type Observation struct {
    Payload []byte
    TerminalWord uint32
}
```

- [ ] Write fake-clock/register tests for inherited nonzero GPO followed by mandatory zero/readback, stable HELLO `0xd3100000` within its distinct two-second bound, START `0xac100000`, every DATA/ACK byte of `OSS FPGA OK\n`, END/ACK, stable DONE `0xd3130c00`, terminal hold, and an accepted parameterized sequence `255` followed by `0`.
- [ ] Add independent failures for every signature/version/opcode/sequence/byte mutation, duplicate/skipped word, unstable double sample, early END/DONE, 257th byte, per-advance timeout, total timeout, and payload mismatch. Assert no ACK follows an invalid/unstable word.
- [ ] Run `go test ./internal/fpgadev -run TestMailbox -count=1`; expect failures.
- [ ] Implement the smallest explicit state machine: write/read back zero after the development mapping opens, use 10 ms double samples, a two-second HELLO deadline, one-second DATA/END/DONE advance deadlines, and a 10-second START-through-DONE deadline.
- [ ] Run `go test -race ./internal/fpgadev -run TestMailbox -count=1`; expect PASS.
- [ ] Commit with `git commit -m "feat: implement FPGA Linux mailbox protocol"` after Vega approval.

### Task 6: Linux No-Owner Qualification and MMIO Lease

**Files:**
- Create: `internal/fpgadev/platform.go`
- Create: `internal/fpgadev/platform_linux.go`
- Create: `internal/fpgadev/platform_stub.go`
- Create: `internal/fpgadev/platform_test.go`
- Create: `internal/fpgadev/mmio_linux_test.go`

**Interfaces:**
- Produces `Qualifier.Qualify(ctx context.Context, binding ArtifactBinding) (Receipt, error)` and `Mapper.OpenMailbox() (Registers, error)`; the returned `Registers.Close` owns the development mapping lifetime.
- `Receipt` proves exact readbacks of the three readable bridge reset
  registers, the exact issued source-bound write to write-only L3 remap, and
  the exact independently observed Linux bridge inventory: one each of
  `hps2fpga`, `lwhps2fpga`, `fpga2hps`, and `fpga2sdram`, resolved through
  dynamic entries' `name` attributes with exact `disabled\n` state and no
  missing/duplicate/unexpected logical names. These views do not claim to read
  back L3. The receipt also proves programming drive
  disabled, USERMODE, subsystem absence/inertness, GPO zero, stable HELLO, and
  all recovery mappings closed. Production construction composes register
  evidence from the exact recovery mapping with independent process/mapping and
  Linux-view evidence; injected verifiers may not replace mapped observations.

- [ ] Write register-fixture tests for page/alignment bounds, little-endian
  32-bit access, barriers/readbacks, the correct source-bound addresses,
  readable bridge values `(0,0,7)`, exact issued write-only L3 value `1`, the
  exact four-name dynamic Linux bridge inventory and state tokens, GPO zero,
  programming release (`CTRL.EN`,
  `CTRL.AXICFGEN`, and pull controls clear), and unmap on every injected error.
- [ ] Write qualification tests that fail each normal lease independently and refuse inert classification for wrong artifact hash/policy, enabled bridge, unexpected process/device, unstable HELLO, or external-resource evidence.
- [ ] Run `go test ./internal/fpgadev -run 'Test(Qualif|MMIO|Bridge)' -count=1`; expect failures.
- [ ] Implement the recovery-only mapping under the quiescing record, install
  cleanup immediately after acquisition, close it before returning `Receipt`,
  and require a separate post-transfer mapping for mailbox START/ACK. Pass the
  single qualification context through every verifier/open/observer seam and
  reject success if the cumulative two-second deadline expires during any
  register read, independent observation, cleanup, or final receipt validation.
- [ ] Add a Linux compile/static test that checks only the corrected four pinned
  register groups are reachable. The production backend is `linux/arm` only;
  `linux/amd64` exists solely for private anonymous-memory fixture seams and
  returns unsupported without them, while every other architecture uses the
  stable stub. `/dev/mem` is opened no-follow and validated as a root-owned
  character device with Linux device number major `1`, minor `1`; stub
  platforms return unsupported.
- [ ] Run `go test -race ./internal/fpgadev -run 'Test(Qualif|MMIO|Bridge)' -count=1`; expect PASS.
- [ ] Commit with `git commit -m "feat: qualify FPGA no-owner handoff"` after mandatory Sol and Vega approval.

### Task 7: Ordered Development Lifecycle and Target Command

**Files:**
- Create: `internal/fpgadev/run.go`
- Create: `internal/fpgadev/run_test.go`
- Create: `internal/fpgadev/designation.go`
- Create: `internal/fpgadev/designation_test.go`
- Create: `cmd/mister-fpga-dev/main.go`
- Create: `cmd/mister-fpga-dev/main_test.go`
- Create: `cmd/mister-fpga-dev/fault_dev.go`
- Create: `cmd/mister-fpga-dev/fault_prod.go`
- Create: `cmd/mister-fpga-dev/fault_test.go`
- Modify: `internal/fpgadev/mailbox.go`
- Modify: `internal/fpgadev/mailbox_test.go`
- Create: `internal/fpgadev/fault_dev.go`
- Create: `internal/fpgadev/fault_prod.go`
- Create: `internal/fpgadev/run_linux.go`
- Create: `internal/fpgadev/run_stub.go`

The internal fault implementation has build expression `linux && fpgadev` and
its stub `!linux || !fpgadev`. The real production runner has build expression
`linux && arm && fpgadev` and its stub `!linux || !arm || !fpgadev`.

**Interfaces:**
- Consumes Tasks 1–6.
- Produces `Runner.Run(context.Context, Request) Result`, `Designation.Verify(context.Context) error`, exact semantic `MaintenanceGate.Enter(ctx) (MaintenanceStatus,MaintenanceUnlock,error)`/`QuiescenceVerifier` inputs owned concretely by Task 8, CLI `mister-fpga-dev preflight --manifest /tmp/misteross-fpgadev-<run_id>/manifest.json --artifact /tmp/misteross-fpgadev-<run_id>/top.rbf` with exact success line and no generation/state write/FIFO/RBF action, and the corresponding `run` form. Task 7 acquires maintenance/install before owner and releases owner before maintenance on every path. Dev-tag builds additionally produce root-only `fault-arm`, `inspect`, `fault-kill`, and `recovery-reboot`, each with `--run-id <run_id>`; production builds return usage error for those names. Until Task 8 wires the concrete journal, lock, and boot-proof adapters, the Task 7 production constructor fails before durable intent; Task 7 must not invent a journal byte grammar or claim target readiness.

- [ ] Write hostile designation tests for absent/ambiguous/mismatched operator record and exact identity. Use injected implementations of exact `MaintenanceGate.Enter(ctx) (MaintenanceStatus,MaintenanceUnlock,error)` and `QuiescenceVerifier.VerifyPreDispatch/VerifyPostMain/VerifyPressedInput` to reject absent, nonterminal, wrong-boot, wrong-owner, stale-supervisor, stale-agent, and inventory-mismatch states before development admission; do not define Task 8 JSON. Add a real helper-process crash table that exits without defers immediately before and after each successful durable transition and asserts exact surviving owner state, result absence, resource closure, and reboot absence. Assert fresh admission for the crash before the first intent replacement and same-boot refusal for every post-intent crash. Keep injected store-error mapping as a separate table.
- [ ] Add the run-ID-bound dev-only fault marker, exact root-only diagnostic and inspect grammar `(run_id,session,generation,phase,pid,start_time,executable_sha256)` plus private executable device/inode, and the root-only no-symlink `SOCK_SEQPACKET` endpoint. `fault-kill` validates owner/endpoint/peer identity under lock and sends the exact request; the matched armed process verifies root `SO_PEERCRED` and self-SIGKILLs. Add exit-before-connect, peer replacement, PID-reuse-after-validation, stale socket, malformed request, and unrelated-process-survival tests, plus the matching fenced recovery-reboot command, boot-owned orphan cleanup, and production-build absence tests.
- [ ] Run `go test -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev -run 'Test(Run|CLI|Crash|Designation|Fault|Inspect|Kill|Recovery)' -count=1`; include a concurrency test proving the armed run releases the lock only after its durable diagnostic and `fault-kill` completes. Run the corresponding untagged CLI tests proving every fault subcommand is absent. Expect the new tests to fail.
- [ ] Implement the ten ordered spec phases against injected semantic maintenance/quiescence inputs: verified designation/profile/identity and retained artifact/evidence binding; fresh session/generation above high-water; durable intent plus current-boot `AdmissionUsable` live-supervisor/Main/agent proof that permits the one attested Main FIFO; binding/evidence and complete proof revalidation plus synchronous dispatch; full Main absence; post-Main exact-inventory qualification and boot-bound pressed-input nonconstruction proof; `no_owner`; atomic `fpgadev_active`; mailbox callbacks that durably record the actual HELLO/DATA/END/DONE boundary; terminal `recovery_required`, owner-then-install lock release, then one exclusive result, then bounded reboot. Hostile-test absent, replaced, PID-reused, extra, and executable-mismatched Main at both proof gates. Remove any production raw-uinput release, empty worker inventory, generic fabricated absence flags, and interim install-journal parser. Owner-store or unlock failure maps the result to `state_store_failed` only when it is the first failure; otherwise preserve the earlier primary. A result-create failure publishes no substitute, exits 2 with the exact result-unavailable stderr grammar, and cannot weaken the fence.
- [ ] Preserve the first primary error independently of later recovery errors. If owner-store, result-store, or unlock failure is first, map it to durable `state_store_failed`; after an earlier primary has already failed the run, join later errors to the private diagnostic chain without inventing a second durable schema. Persist reboot-request failure through the sole conditional recovery update, and make host reconnect/readiness the authoritative recovery verdict. Never clear a post-intent record on return.
- [ ] Run `go test -race -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev -count=1`, `go vet -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev`, and the untagged test/vet equivalents; tagged tests exercise the real fault controls and untagged tests prove their absence. Build untagged Linux/ARM as well as Darwin/amd64 and prove the fault API, hooks, and strings are absent. Expect PASS.
- [ ] Commit with `git commit -m "feat: orchestrate FPGA development sessions"` after mandatory Sol and Vega approval.

### Task 8: Boot Recovery and Development Supervisor

**Files:**
- Create: `internal/fpgadev/recover.go`
- Create: `internal/fpgadev/recover_test.go`
- Create: `internal/fpgadev/supervisor.go`
- Create: `internal/fpgadev/supervisor_test.go`
- Create: `internal/fpgadev/install_journal.go`
- Create: `internal/fpgadev/install_journal_test.go`
- Create: `cmd/fogcast-dev-supervisor/main_dev.go`
- Create: `cmd/fogcast-dev-supervisor/main_stub.go`
- Create: `cmd/fogcast-dev-supervisor/main_dev_test.go`
- Create: `cmd/fogcast-dev-supervisor/main_stub_test.go`
- Create: `cmd/fogcast-dev-supervisor/deps_arm.go`
- Create: `cmd/fogcast-dev-supervisor/deps_stub.go`
- Create: `cmd/mister-fpga-dev/install_dev.go`
- Create: `cmd/mister-fpga-dev/install_prod.go`
- Create: `cmd/mister-fpga-dev/install_dev_test.go`
- Create: `cmd/mister-fpga-dev/install_prod_test.go`
- Create: `cmd/mister-fpga-dev/install_deps_arm.go`
- Create: `cmd/mister-fpga-dev/install_deps_stub.go`
- Modify: `cmd/mister-fpga-dev/main.go`
- Modify: `cmd/mister-fpga-dev/main_test.go`
- Modify: `internal/hardwareowner/gate.go`
- Modify: `internal/hardwareowner/gate_test.go`
- Modify: `cmd/mister-agent/main.go`
- Modify: `cmd/mister-agent/main_test.go`
- Modify: `internal/input/controller.go`
- Modify: `internal/input/controller_test.go`
- Modify: `internal/bridge/bridge.go`
- Modify: `internal/bridge/bridge_test.go`
- Modify: `internal/fpgadev/run.go`
- Modify: `internal/fpgadev/run_test.go`
- Modify: `internal/fpgadev/run_linux.go`
- Modify: `internal/fpgadev/manifest.go`
- Modify: `internal/fpgadev/manifest_test.go`
- Modify: `internal/fpgadev/artifact.go`
- Modify: `internal/fpgadev/artifact_test.go`
- Modify: `internal/fpgadev/artifact_linux.go`
- Modify: `internal/fpgadev/platform.go`
- Modify: `internal/fpgadev/platform_test.go`
- Modify: `internal/agentconfig/config.go`
- Modify: `internal/agentconfig/config_test.go`
- Create: `deploy/fpgadev/start.sh`
- Create: `deploy/fpgadev/agent.toml.example`
- Create: `scripts/package-fpgadev.sh`
- Create: `scripts/install-fpgadev.sh`
- Create: `scripts/tests/fpgadev-package_test.sh`
- Modify: `Makefile`

**Interfaces:**
- Produces internal `InitializeOwner(ctx context.Context) error`, `InstallManager.Install/Recover/Uninstall`, the sole concrete immutable install-journal `MaintenanceGate`, the boot-bound `QuiescenceVerifier`, and `Supervisor.Run(ctx context.Context, deps Dependencies) error`. `MaintenanceGate.Enter` opens/validates/flocks the one protected install lock and returns `MaintenanceStatus` plus its unlock; the verifier implements Task 7's exact `VerifyPreDispatch`, `VerifyPostMain`, and `VerifyPressedInput` signatures, and Task 8 modifies `run.go`, `run_test.go`, and `run_linux.go` to inject both into the production constructor. Task 8 also modifies `hardwareowner.Gate` so normal admission uses the same install-then-owner order. Root-only CLI subcommands `install-profile`, `recover-install`, and `uninstall-profile` are the sole script-facing mutation interfaces. One `install-profile` process holds install then owner while it prepares the trampoline, initializes ownership, and completes source replacement; there is no lock-order inversion or unlocked handoff. Its completed-on-install-boot immutable `terminal` journal remains fenced by absence of a boot-local proof and requests reboot. Every nonterminal journal blocks all admission. On the successor boot `recover-install` holds the install lock, repairs journal/backups/sources, and passes the validated held fd across exec; the supervisor validates it before acquiring owner and alone owns reset/store/child startup. It starts Main exactly once through `Dependencies.StartMain`, verifies all four readiness observations within one cumulative 30-second context, commits `normal_main`, starts the exact development agent, and reads the exact canonical inherited-pipe receipt, resolves the full current-boot inventory with metadata-only descriptors, and publishes the boot proof within one cumulative five-second agent-start/receipt/resolution/publication deadline. It then releases owner before install. On every later reboot the same immutable journal is reused and a new proof may be published only after old-boot recovery and exact Main/agent startup; same-boot child replacement cannot republish it. The proof binds the live supervisor identity as well as the agent. Supervisor death invalidates admission immediately; a same-boot replacement fences `recovery_required`, uses identity-bound pidfds to terminate and prove disappearance of exact surviving children without adopting or restarting them while PID 1 owns orphan reaping, and requires reboot.

The platform-neutral supervisor CLI/manager behavior and install-command
registration use exact build expression `linux && fpgadev`, with the common
dispatcher modified to call a tag-selected registration hook, so host linux/amd64
tagged tests execute the real grammar/state machine with injected dependencies.
Only the target production dependency constructors that can reset, start
children, mutate launch sources, or reboot use `linux && arm && fpgadev`;
`deps_stub.go`/`install_deps_stub.go` use `!linux || !arm || !fpgadev` and fail
closed. The command-registration stubs use `!linux || !fpgadev`, contain no
command names, journal/proof paths, mutation interfaces, or fault/install
strings, and exist only so untagged/unsupported package tests remain defined.
Tagged amd64 tests cover real behavior through fakes, ARM cross-builds cover
production composition, and complementary tests scan the untagged binary/API/
strings for absence. No untagged public constructor can reach mutation.

- [ ] Write first-install tests for absent-record adoption of one exact healthy Main/Menu tuple before launch-source replacement and fail-closed ambiguity. Prove the install boot cannot admit development and requests reboot. Write successor-boot tests for absent-uninitialized and same-boot refusal, new-boot reset proof, `no_owner`, fresh session/generation/high-water `normal_main_starting`, exactly one Main start, all four Main readiness observations within one cumulative bounded context, final `normal_main`, exact agent-after-Main order, inherited-pipe receipt identity/capability validation, atomic boot-proof publication, and every reset/readiness/receipt failure remaining fenced.
- [ ] Write supervisor race tests for Main/agent and every inventoried legacy start hook; prove installation changes sources only after initialization, every non-normal state prevents restart, and unexpected Main, agent, or supervisor death commits `recovery_required` without same-boot adoption/restart. Cover supervisor death before/after receipt and boot-proof fsync with each child alive/dead; prove stale supervisor identity makes any surviving proof unusable, replacement uses identity-bound pidfds to terminate/prove orphan disappearance while PID 1 owns reaping, and the live supervisor directly reaps only its own failed-receipt child.
- [ ] Write install-journal crash tests at every phase, including before/after prepared-journal fsync and atomic recovery-trampoline installation/removal: prove one install process retains the same lock across preparation, internal initialization, and source-replacement completion; stop/prove old agent absent; prove the exact package-hash/argv-bound approved trampoline is the sole proven-earliest boot launcher and every other inventoried starter is absent or inert; wire the concrete semantic journal fence into `NormalGate`, development commands, and supervisors; prove every nonterminal journal and every terminal-journal/current-boot-proof absence or mismatch blocks each route; parse the protected prior config into immutable `InventoryV1` expectations (optional cast executable path/hash; input-uinput/framebuffer/native-command/token-file path/type/stable-rdev; input-listen/cast-RTP/cast-control network/address; Main FIFO path/type; ordered start-source original/backup plus exact disabled-absent, disabled-inert, or sole approved-trampoline binding) and reject every nonempty unrepresentable value; disable and fsync each legacy source; install the sole supervisor last; require the successor boot before proof publication; validate static expectations and terminal disabled-source state before child start, then resolve final current-boot device/inode identities after Main readiness and the agent receipt into the proof's `ResolvedInventoryV1`; prove Main-created FIFO timing, explicit expected-absent route appearance rejection, disabled source absence/inert replacement/original reappearance, exact trampoline launch/second-launcher rejection, alias handling, and inode churn across FIFO/devtmpfs recreation while the persistent terminal journal digest stays byte-identical; resume or roll back before any starter on boot; disable supervisor first on uninstall; restore byte-identically; and refuse uninstall unless canonical `normal_main`.
- [ ] Implement and hostile-test the exact spec schemas: root-owned `0700` journal/backup/proof directories; no-follow root-owned regular link-count-one mode-`0600` files; 1 MiB canonical journal with its exact states/top-level/source/inventory field order, 255-byte strings, at most 16 start sources, install-time worst-case proof serialization, and no persisted reboot-volatile inode values; protected separately hashed backups; 1024-byte one-line receipt and 16 KiB one-line schema-2 boot proof with exact field order/five-capability array/current-boot resolved inventory and live supervisor/Main/agent identities; duplicate/unknown/reordered/trailing/oversize/link/type/UID/mode/identity rejection; file and parent-directory fsync; immutable terminal digest across boot proofs. Add crash tests before/after every journal/proof rename and fsync, pipe EOF/extra-line/timeout/wrong-child tests that close pipes and terminate/reap the exact untrusted child, later-boot proof renewal plus same-boot remint refusal, boot-local device/inode churn fixtures, and distinct `AdmissionUsable`/`CleanupAuthoritative` matrices.
- [ ] Implement the exact `/var/lock/fogcast/fpgadev-install.lock` root-owned `0700` parent/root-owned regular link-count-one `0600` file/no-follow/context-bounded flock contract. Test every Install/Recover/Uninstall/status/normal/dev/supervisor caller shares it, global install-then-owner and reverse release order, held-fd exec handoff/identity validation, concurrent CLI and admission contention, wrong/reused fds, exec failure, and crash release with either/both locks held.
- [ ] Add agent/bridge shutdown tests proving a configured input sink performs `ReleaseAll` and closes with propagated errors on graceful teardown, but do not use a newly opened uinput control descriptor as development qualification. Add receipt tests proving the development agent writes only after cast/input/controller-route/presentation/audio facilities are absent and fallback routes are registered, producing the exact five-capability array. Add phase-aware inventory tests that permit the exact Main FIFO before dispatch and reject every inventoried worker/route/descriptor independently after Main exit.
- [ ] Run `go test ./internal/fpgadev ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev -run 'Test(Initialize|Boot|Supervisor|LegacySource|InstallJournal|RecoveryTrampoline|Uninstall|Package)' -count=1 && sh scripts/tests/fpgadev-package_test.sh`; expect missing command/scripts failures.
- [ ] Implement the single-starter supervisor, first-install initializer, durable install journal and ordered source replacement/restore, recovery cleanup, and dev/production package split.
- [ ] Add `make package-fpgadev` that itself builds all executable members with `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7` and `-tags fpgadev`, attests each using `file` and `readelf -h`, reads `FOGCAST_DEV_EVIDENCE_PUBLIC_KEY` as exactly 32 raw bytes from a regular link-count-one no-follow mode-`0600` file owned by the invoking user, embeds its lowercase hex in the tagged target verifier, and binds each member SHA-256 plus SHA-256 over those decoded public-key bytes into the fixed tar-member manifest. Reject PEM/DER/OpenSSH/hex text, wrong length, wrong metadata, empty input, a build that omits the key, or the governing test-fixture public-key hash. Test Go `crypto/ed25519` against the governing 625-byte interoperability vector through a test-only seam while proving the production package path refuses that fixture key. The private key is never read by this repository. Add `scripts/install-fpgadev.sh` modes `--install`, `--uninstall`, and `--dry-run`. Require `MISTER_TARGET=root@HOST`, argv-list SSH/SCP, exact remote package verification, and one remote `install-profile` invocation that stops/proves the old agent absent, installs the recovery trampoline, internally initializes ownership while Main/Menu is healthy, and completes the journaled source transition under one lock; no credential enters an argument.
- [ ] Run `go test -race -tags fpgadev ./internal/fpgadev ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev -count=1`, `go vet -tags fpgadev ./internal/fpgadev ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev`, the untagged equivalents, and `sh scripts/tests/fpgadev-package_test.sh`; the install/recover/uninstall CLI tests must pass in the tagged package configuration. Expect PASS.

**Task 8 approval amendment:** The governing spec's schema-2 boot proof supersedes
the earlier schema-1 wording in this task: it binds the exact supervisor, Main,
and agent tuples. A same-boot replacement durably fences first and may signal
only those proof-recorded Main/agent identities through revalidated pidfds; an
absent, malformed, incomplete, or stale proof permits no scan-derived signal and
requires reboot. Implement distinct `AdmissionUsable` and
`CleanupAuthoritative` predicates exactly as specified; both pre-intent and
pre-dispatch gates hostile-test absent/replaced/PID-reused/extra/mismatched Main.
Task 8 also consumes the descriptor-bound canonical signed
`resource_evidence.json` produced by both synthesis lanes, requires its
lane/source/artifact/key tuple and Ed25519 signature to verify against the
tagged ARM binary's package-bound public key, and derives static resource policy
only from authenticated observed counts. Add strict schema/order/type/range/
size/signature tests, arbitrary-RBF evidence-reuse rejection, every
forbidden-resource case, and before/after-proof supervisor-death tests for both
the signal and no-signal branches.

- [ ] Commit with `git commit -m "feat: recover FPGA development ownership on boot"` after mandatory Sol and Vega approval.

### Task 9: FogCast Verification and Handoff

**Files:**
- Create: `docs/runbooks/fpga-development-loader.md`
- Create: `docs/m2/fogcast-software-handoff.md`
- Modify: `docs/ROADMAP.md`

**Interfaces:**
- Produces a Software-tested FogCast implementation for the cross-repository integration plan; does not claim HIL.

- [ ] From the recorded approved-design commit, compute the cumulative implementation range and require a clean index/worktree. Run `gofmt -l` over every Go path in that full range and require empty output. For each intentionally untracked handoff file run `git diff --no-index --check /dev/null <file>` and require exit `1` with empty output, then record its SHA-256 before adding it. Run `go test ./...`, `go vet ./...`, the focused untagged race suite, the corresponding `go test -race -tags fpgadev` suite, `go vet -tags fpgadev ./internal/fpgadev ./cmd/mister-agent ./cmd/mister-fpga-dev ./cmd/fogcast-dev-supervisor`, the package test, both secret scans, and `git diff --check <approved-design-commit>..HEAD`; commands other than the documented no-index checks must exit 0.
- [ ] Run `make package-fpgadev`; inspect every packaged executable with `file` and `readelf -h`, recompute its SHA-256, and require equality with the package manifest. Build the normal untagged production targets and prove dev commands/package members are absent. Bind the exact package and member hashes in the handoff and integration ledger.
- [ ] Record the inherited `catalog/watch_test.go` race baseline separately; do not conceal it or attribute it to M2. Fixing that unrelated test race requires its own approved task if full-repository race-clean status is required.
- [ ] Sol reviews lifecycle/MMIO/rollback against the exact spec and commit; Vega independently reviews code, hostile tests, packaging, and verification. Resolve every Critical/Important finding and rerun affected checks.
- [ ] Write the handoff with exact base/head/tree hashes, commands and outputs, evidence class, untested physical behavior, rollback, and next safe action.
- [ ] Commit docs with `git commit -m "docs: hand off FogCast FPGA development loader"`; do not push or deploy.

The next safe action is the `misteross` implementation plan followed by the cross-repository integration plan. No task in this plan alone authorizes target mutation.
