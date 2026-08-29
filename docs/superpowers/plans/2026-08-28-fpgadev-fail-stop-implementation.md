# FPGA Development Loader Fail-Stop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a working private MiSTer Pi RBF development loop that prevents ordinary ownership conflicts, recovers interrupted installation on the next boot, and reboots or power-cycles instead of attempting ambiguous same-boot recovery.

**Architecture:** A root-only install/recovery trampoline owns boot-profile changes under install-then-owner locks. A target supervisor starts exactly one Main and one development agent, publishes a small boot-local ready record, and requests reboot on any ambiguous post-mutation failure. Packages and RBF resource evidence are deterministically SHA-256-bound without a signing-key subsystem.

**Tech Stack:** Go 1.24 standard library, `golang.org/x/sys/unix`, POSIX shell, ARMv7 Linux cross-build, MiSTer-compatible RBFs.

**Spec:** `docs/superpowers/specs/2026-08-28-fpgadev-fail-stop-amendment.md`

## Global Constraints

- Threat model: one trusted operator/root user on a private disposable development target; malicious root and hostile concurrent mutation are out of scope.
- Ambiguous state after install or FPGA mutation is fenced and recovered by reboot/power-cycle, never same-boot adoption or scan-derived process killing.
- Keep correct Cyclone V MMIO addresses, masks, barriers, bridge/GPO order, USERMODE checks, mailbox grammar, artifact SHA-256 binding, timeouts, results, and reboot-after-attempt behavior.
- Keep global lock order install then owner; release owner then install.
- Real reset, child start, boot-source mutation, reboot, and target installation code uses `linux && arm && fpgadev`; linux/amd64 tagged tests use injected fakes.
- Untagged builds expose no development command registration or hardware-mutation constructor.
- No Ed25519 key, public-key embedding, signature field, fixture-key test, or signature verification remains.
- Use strict bounded parsers for ready schema 3 and unsigned resource-evidence schema 2; do not retain compatibility acceptance of superseded schemas.
- Use TDD for every behavior change: record a genuine failing test before production edits, then the minimal passing implementation.
- Use `/dev/shm` or a task-owned home-cache directory for `TMPDIR`/`GOTMPDIR`; do not delete unrelated `/tmp` content.
- Do not stage, commit, push, deploy, reboot, or access the target until the exact task snapshot passes required review and the operator authorizes that next action.
- Software tests are not HIL evidence.

## Authenticated Starting Snapshot

Execution resumes the existing isolated worktree
`/home/deano/FogCast-POC/.worktrees/m2-task8-boot-recovery`, not a clean checkout
of its HEAD. The baseline HEAD is
`bd7890f3080519c342c563efd53a013d588cb1e5`, tree
`5689f226e0aa55de85259b5a5271ccc287c2e2e8`. The exact 65-path unstaged
starting snapshot has sorted content-manifest SHA-256
`c7bb452a00e306f26ae38f733c29fb1ebf00c5b69463ba740c302bf3db4c0027`
and deterministic combined binary-diff SHA-256
`fb7b2f452b6c27300f7dc7c1d2d2fd4638c3816b8dfdb4d214107cd1ef835286`.
Every task preserves this work; files marked `Modify` may be untracked members
of that authenticated snapshot. If these hashes do not match before Task 1,
stop and reconcile the changed snapshot rather than falling back to HEAD.

## Cross-Repository Prerequisite

Before FogCast Task 4, execute and review
`/home/deano/misteross/.worktrees/m0-m1-open-toolchain/docs/superpowers/plans/2026-08-28-unsigned-resource-evidence.md`.
Task 4 consumes only the exact reviewed misteross snapshot named by that plan's
handoff. Both OSS and Quartus-oracle bundles must contain unsigned schema-2
`resource_evidence.json`; a signed schema-1 bundle is incompatible and fails
the prerequisite gate.

---

### Task 1: Small Ready Record and Normal Admission

**Files:**
- Modify: `internal/fpgadev/supervisor.go`
- Modify: `internal/fpgadev/supervisor_test.go`
- Modify: `internal/fpgadev/production_evidence_linux.go`
- Modify: `internal/fpgadev/production_evidence_linux_test.go`
- Modify: `internal/hardwareowner/gate.go`
- Modify: `internal/hardwareowner/gate_test.go`
- Modify: `cmd/mister-agent/capability_dev.go`
- Modify: `cmd/mister-agent/main.go`
- Modify: `cmd/mister-agent/main_test.go`
- Modify: `cmd/mister-agent/readiness_dev.go`
- Modify: `cmd/mister-agent/readiness_dev_test.go`

**Interfaces:**
- Consumes: `hardwareowner.Record`, owner/install locks, terminal journal digest, protected profile hash, and Linux `/proc/<pid>/stat` start times.
- Produces:

```go
type ReadyRecordV3 struct {
	Schema uint64
	BootID, JournalSHA256, OwnerSession, ProfileSHA256 string
	OwnerGeneration uint64
	Capabilities []string
	SupervisorPID, SupervisorStartTime uint64
	MainPID, MainStartTime uint64
	AgentPID, AgentStartTime uint64
}
func ParseReadyRecordV3([]byte) (ReadyRecordV3, error)
func (ReadyRecordV3) MarshalCanonical() ([]byte, error)
type ReadyStoreV3 interface {
	Load() (ReadyRecordV3, bool, error)
	Replace(ReadyRecordV3) error
	Remove() error
}
func (v *DevelopmentAdmissionVerifier) Verify(context.Context, hardwareowner.Record) error
```

- [ ] **Step 1: Write ready-schema RED tests**

Add table tests requiring `/run/fogcast/fpgadev-ready-v3.json`, schema integer `3`, this exact field order, one trailing newline, and a 4096-byte maximum:

```text
schema,boot_id,journal_sha256,owner_session,owner_generation,profile_sha256,
capabilities,supervisor_pid,supervisor_start_time,main_pid,main_start_time,
agent_pid,agent_start_time
```

Cover schema 0/1/2/4, reordered, duplicate, unknown, missing, float, string-number, negative, zero, trailing-object, bad hash/session/boot/capabilities, and oversized cases.

- [ ] **Step 2: Run the schema RED**

```sh
install -d -m 0700 /dev/shm/fogcast-task1
GOTMPDIR=/dev/shm/fogcast-task1 go test -tags fpgadev ./internal/fpgadev -run '^TestReadyRecordV3' -count=1
```

Expected: FAIL because `ReadyRecordV3` does not exist.

- [ ] **Step 3: Implement the exact ready record**

Implement the approved fields and canonical protected store. Remove executable device/inode/hash, resolved-inventory, `CleanupAuthoritative`, and pidfd-cleanup fields from normal admission. Publish with atomic replacement plus file and parent-directory fsync.

- [ ] **Step 4: Write admission RED tests**

Use real owner/install locks and injected boot/journal/profile/process readers. Cover valid admission plus mismatch/absence for every record field, context cancellation, and installer contention. Assert verification occurs while both locks are held and unlock order is owner then install.

- [ ] **Step 5: Run admission RED**

```sh
install -d -m 0700 /dev/shm/fogcast-task1
GOTMPDIR=/dev/shm/fogcast-task1 go test -race -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent -run 'Test(ReadyRecordV3|DevelopmentAdmission|GateAdmission|TaggedAgentStartup)' -count=1
```

Expected: FAIL because the production gate does not consume the ready record.

- [ ] **Step 6: Implement and wire admission**

Load terminal journal plus ready record; compare current boot, journal digest, owner tuple, protected profile hash, exact capabilities, and live supervisor/Main/agent PID/start-time pairs. Do not hash executables or scan the complete process table. Inject it from `newNormalGate`. Tagged development profiles require inherited write-only pipe fd `3`; manual startup fails before controllers/server startup. Hardware transitions remain blocked until the supervisor publishes ready and releases both locks.

- [ ] **Step 7: Verify Task 1**

```sh
install -d -m 0700 /dev/shm/fogcast-task1
GOTMPDIR=/dev/shm/fogcast-task1 go test -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent -run 'Test(ReadyRecordV3|DevelopmentAdmission|GateAdmission|TaggedAgentStartup)' -count=20
GOTMPDIR=/dev/shm/fogcast-task1 go test -race -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent -run 'Test(ReadyRecordV3|DevelopmentAdmission|GateAdmission|TaggedAgentStartup)' -count=20
go vet -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent
git diff --check
```

Expected: PASS. Append exact RED/GREEN output and file hashes to the Task 8 report; do not commit.

---

### Task 2: Fail-Stop Supervisor and ARM Runtime Boundary

**Files:**
- Modify: `internal/fpgadev/supervisor.go`
- Modify: `internal/fpgadev/supervisor_test.go`
- Modify: `internal/fpgadev/supervisor_runtime_linux.go`
- Modify: `internal/fpgadev/supervisor_runtime_test.go`
- Modify: `internal/fpgadev/child_linux.go`
- Modify: `internal/fpgadev/child_stub.go`
- Modify: `internal/fpgadev/supervisor_reset_linux.go`
- Modify: `cmd/fogcast-dev-supervisor/deps_linux.go`
- Modify: `cmd/fogcast-dev-supervisor/deps_arm.go`
- Modify: `cmd/fogcast-dev-supervisor/deps_stub.go`
- Modify: `cmd/fogcast-dev-supervisor/main_dev.go`
- Modify: `cmd/fogcast-dev-supervisor/main_dev_test.go`

**Interfaces:**
- Consumes Task 1 ready store, protected config, journal, owner store/locks.
- Produces `RebootRequester.Request(context.Context) error` and a one-Main/one-agent fail-stop `Supervisor.Run`.

- [ ] **Step 1: Write lifecycle RED tests**

Assert this exact order:

```text
install-lock -> owner-lock -> remove stale ready -> reset -> start Main ->
Main process/FIFO/FPGA-manager/CORENAME readiness -> normal_main -> start agent ->
receipt -> publish ready -> owner-unlock -> install-unlock -> wait children
```

Cover each start/readiness/receipt/publication failure, Main/agent exit, same-boot replacement, and reboot-request failure. Every post-reset failure removes ready, best-effort writes `recovery_required`, requests reboot, starts no replacement, and returns. Same-boot replacement requests reboot without scanning or signaling unknown processes.

- [ ] **Step 2: Run lifecycle RED**

```sh
install -d -m 0700 /dev/shm/fogcast-task2
GOTMPDIR=/dev/shm/fogcast-task2 go test -race -tags fpgadev ./internal/fpgadev ./cmd/fogcast-dev-supervisor -run 'TestFailStopSupervisor' -count=1
```

Expected: FAIL while old schema-2/pidfd/inventory recovery remains.

- [ ] **Step 3: Implement fail-stop lifecycle**

Retain typed children and parent-death signaling. Delete same-boot adoption, survivor scanning, `CleanupAuthoritative`, and remint logic. One cumulative 30-second readiness context checks the retained Main, root-owned mode-`0600` FIFO, FPGA-manager exact `operating\n`, and exact `MENU\n` twice 10 ms apart. Agent start, receipt, and ready publication share one five-second context.

- [ ] **Step 4: Enforce ARM boundary**

Use `linux && arm && fpgadev` for real reset, child start, launch-source mutation, and reboot constructors. Keep platform-neutral orchestration `linux && fpgadev`; stubs use `!linux || !arm || !fpgadev`. Add source/string tests proving linux/amd64 tagged construction cannot reach `/dev/mem`, reset, reboot, or real child start.

- [ ] **Step 5: Verify Task 2**

```sh
install -d -m 0700 /dev/shm/fogcast-task2
GOTMPDIR=/dev/shm/fogcast-task2 go test -tags fpgadev ./internal/fpgadev ./cmd/fogcast-dev-supervisor -run 'Test(FailStopSupervisor|SupervisorRuntime|ProductionSupervisorDependencies)' -count=20
GOTMPDIR=/dev/shm/fogcast-task2 go test -race -tags fpgadev ./internal/fpgadev ./cmd/fogcast-dev-supervisor -run 'Test(FailStopSupervisor|SupervisorRuntime)' -count=20
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -tags fpgadev ./cmd/fogcast-dev-supervisor ./cmd/mister-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -tags fpgadev ./cmd/fogcast-dev-supervisor
go vet -tags fpgadev ./internal/fpgadev ./cmd/fogcast-dev-supervisor
git diff --check
```

Expected: PASS. Append evidence; do not commit.

---

### Task 3: Boot-Recoverable Coarse Install Journal

**Files:**
- Modify: `internal/fpgadev/install_journal.go`
- Modify: `internal/fpgadev/install_journal_test.go`
- Modify: `internal/fpgadev/recover.go`
- Modify: `internal/fpgadev/recover_test.go`
- Modify: `cmd/mister-fpga-dev/install_dev.go`
- Modify: `cmd/mister-fpga-dev/install_dev_test.go`
- Modify: `cmd/mister-fpga-dev/install_deps_arm.go`
- Modify: `cmd/mister-fpga-dev/install_deps_stub.go`
- Modify: `deploy/fpgadev/start.sh`

**Interfaces:**
- Consumes global locks and Task 2 supervisor executable.
- Produces journal states `prepared`, `installed`, `terminal`, `uninstalling`, `restored`; the earliest recovery trampoline; and:

```go
func (m *InstallManager) Install(context.Context, packageRoot string) error
func (m *InstallManager) Recover(context.Context) error
func (m *InstallManager) Uninstall(context.Context) error
```

- [ ] **Step 1: Write recovery RED table**

Test the amendment's exact next-boot action for absent and every durable phase. For `prepared`, separately test: matching helper and payload completes install; matching helper with another missing/mismatched staged member restores backups; missing/mismatched helper starts nothing and remains fenced with a diagnostic. Add crash seams immediately before/after trampoline publication, each coarse journal fsync, midway through multi-source disable/restore, before/after `uninstalling` fsync, supervisor disablement, `restored` fsync, and original-dispatcher restoration. Every nonterminal recovery starts no Main/agent and keeps the trampoline until `restored`.

- [ ] **Step 2: Run recovery RED**

```sh
install -d -m 0700 /dev/shm/fogcast-task3
GOTMPDIR=/dev/shm/fogcast-task3 go test -race -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev -run 'Test(FailStopInstall|FailStopRecover|FailStopUninstall|RecoveryTrampoline)' -count=1
```

Expected: FAIL because current journal states/order differ.

- [ ] **Step 3: Implement trampoline-first install**

Validate the private transfer-stage package root and member manifest without modifying fixed paths. Copy the verified recovery executable and all required install members into `/var/lib/fogcast/fpgadev-staging/<software-manifest-sha256>/`; fsync every member and the protected persistent-stage directory. Back up original dispatcher and sources, fsync backups, install a trampoline that executes the persistent staged recovery command, make it earliest, then write `prepared`. Only after `prepared` may the manager install persistent staged members to fixed paths and advance to `installed`; then it disables legacy sources and advances to `terminal`. Absent state chains original. Nonterminal state locks install then owner, starts no child, executes the recovery table, and reboots. Every step is idempotent.

- [ ] **Step 4: Implement safe uninstall**

With trampoline earliest, write/fsync `uninstalling` first; idempotently disable supervisor; restore/fsync non-dispatcher sources; write/fsync `restored`; restore/fsync original dispatcher last; chain normal boot; then remove the persistent stage. Re-entry at every boot-critical seam produces identical final bytes.

- [ ] **Step 5: Verify Task 3**

```sh
install -d -m 0700 /dev/shm/fogcast-task3
GOTMPDIR=/dev/shm/fogcast-task3 go test -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev -run 'Test(FailStopInstall|FailStopRecover|FailStopUninstall|RecoveryTrampoline)' -count=20
GOTMPDIR=/dev/shm/fogcast-task3 go test -race -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev -run 'Test(FailStopInstall|FailStopRecover|FailStopUninstall)' -count=20
go vet -tags fpgadev ./internal/fpgadev ./cmd/mister-fpga-dev
git diff --check
```

Expected: PASS. Append phase-matrix evidence; do not commit.

---

### Task 4: Unsigned Resource Evidence and Real Package Installer

**Files:**
- Modify: `internal/fpgadev/manifest.go`
- Modify: `internal/fpgadev/manifest_test.go`
- Modify: `internal/fpgadev/artifact.go`
- Modify: `internal/fpgadev/artifact_test.go`
- Create: `internal/fpgadev/resource_evidence.go`
- Create: `internal/fpgadev/resource_evidence_test.go`
- Modify: `internal/fpgadev/platform.go`
- Modify: `internal/fpgadev/platform_test.go`
- Modify: `scripts/package-fpgadev.sh`
- Modify: `scripts/install-fpgadev.sh`
- Modify: `scripts/tests/fpgadev-package_test.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes the misteross four-member per-run artifact bundle: `top.rbf`, nine-field artifact `manifest.json`, unsigned evidence schema 2, and sorted `bundle.sha256` over those three payloads.
- Produces deterministic ARMv7 package, real private-temp remote installer, and:

```go
type ResourceEvidenceV2 struct {
	Schema uint64
	Experiment, Board, BuildLane, SourceCommit string
	ArtifactSHA256, SynthesisReportSHA256 string
	ClockInputs, ExternalInputPorts, ExternalOutputPorts uint64
	BidirectionalPorts, HPSGeneralPurposeInterfaces uint64
	PLLBlocks, DSPBlocks, BlockMemoryBits, LUTRAMBits, SDRAMInterfaces uint64
}
func ParseResourceEvidenceV2([]byte) (ResourceEvidenceV2, error)
```

- [ ] **Step 1: Write evidence RED tests**

Require and verify sorted `bundle.sha256` entries for exactly `manifest.json`, `resource_evidence.json`, and `top.rbf` before parsing. Then require evidence schema integer `2`, exact 17-key order, compact JSON plus newline, 2048-byte bound, fixed experiment/board/lane, lowercase commit/hashes, integer counts `0..4294967295`, exactly one clock and HPS GP interface, and zero forbidden counts. Reject missing/extra/duplicate/wrong member hashes, signed schema 1, signing fields, floats/exponents/strings/negative/null, mismatched experiment/board/lane/source/artifact tuple against the parsed artifact manifest and opened RBF, malformed synthesis-report hash, unknown/duplicate/reordered/trailing/oversized input. The target does not receive or independently compare the synthesis report.

- [ ] **Step 2: Run evidence RED**

```sh
install -d -m 0700 /dev/shm/fogcast-task4
GOTMPDIR=/dev/shm/fogcast-task4 go test -tags fpgadev ./internal/fpgadev -run '^TestResourceEvidenceV2' -count=1
```

Expected: FAIL because unsigned schema 2 is absent.

- [ ] **Step 3: Implement evidence and policy**

Bind evidence to the parsed artifact manifest and opened RBF, requiring identical experiment/board/lane/source/artifact values and canonical synthesis-report hash grammar. Derive fixed resource policy only from matching counts. Do not require an absent target-side synthesis report; the reviewed misteross producer owns that comparison. Remove Ed25519/public-key/signature inputs and unconditional static-policy success. Direct process/mapping checks cover known FPGA programming tools; unavailable/contradictory observation aborts before MMIO.

- [ ] **Step 4: Write package/installer RED tests**

Require all three ARMv7 tagged ELF executables, recovery trampoline/configuration, fixed member modes/order/mtime/ownership, deterministic software archive and `manifest.sha256`, no signing members, unique remote mode-`0700` transfer directory, and archive/member verification. The installer executes the verified transfer-stage `bin/mister-fpga-dev install-profile --package-root STAGE` before any fixed target member changes. `InstallManager` validates the transfer stage, makes the protected persistent stage durable, and alone installs fixed members during durable `prepared -> installed`. Assert argv-safe SSH/SCP, transfer cleanup, and network-free dry-run; reject any execution of a pre-existing or prematurely installed fixed-path binary.

- [ ] **Step 5: Run package RED**

```sh
install -d -m 0700 /dev/shm/fogcast-task4
TMPDIR=/dev/shm/fogcast-task4 sh scripts/tests/fpgadev-package_test.sh
```

Expected: FAIL because current installer uses shared `/tmp` and the old installed binary.

- [ ] **Step 6: Implement package and installer**

Use `mktemp -d`, fixed archive metadata, `sha256sum -c`, `file`, and `readelf -h`. Transfer to a unique target staging directory, verify/extract there, and execute that verified staged tool with `install-profile --package-root STAGE`. The staged command passes the validated package binding into Task 3 `InstallManager`, which publishes the trampoline and `prepared` before installing fixed members and advancing to `installed`. Cleanup staging after the command; keep credentials out of argv/logs/source.

- [ ] **Step 7: Verify Task 4**

```sh
install -d -m 0700 /dev/shm/fogcast-task4
GOTMPDIR=/dev/shm/fogcast-task4 go test -tags fpgadev ./internal/fpgadev -run '^TestResourceEvidenceV2' -count=20
TMPDIR=/dev/shm/fogcast-task4 sh scripts/tests/fpgadev-package_test.sh
TMPDIR=/dev/shm/fogcast-task4 make package-fpgadev
git diff --check
```

Expected: PASS. Record package SHA-256; do not commit or contact target.

---

### Task 5: Proportional Verification and Reviewed Snapshot

**Files:**
- Modify: `.superpowers/sdd/2026-08-27-m2-linux-mailbox-fogcast/task-8-report.md`
- Modify: `.superpowers/sdd/2026-08-27-m2-linux-mailbox-fogcast/progress.md`
- Modify: `docs/superpowers/plans/2026-08-27-m2-linux-mailbox-integration.md` only to cite this amendment/plan and remove signature/hostile-root gates.

**Interfaces:**
- Consumes Tasks 1-4 unstaged implementation.
- Produces one immutable reviewed software snapshot and honest HIL handoff.

- [ ] **Step 1: Run focused gates**

```sh
install -d -m 0700 /dev/shm/fogcast-task5
export GOTMPDIR=/dev/shm/fogcast-task5
go test -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev -count=1
go test ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev -count=1
go test -race -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev -count=1
go vet -tags fpgadev ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev
go vet ./internal/fpgadev ./internal/hardwareowner ./cmd/mister-agent ./cmd/fogcast-dev-supervisor ./cmd/mister-fpga-dev
TMPDIR=/dev/shm/fogcast-task5 sh scripts/tests/fpgadev-package_test.sh
```

Expected: PASS.

- [ ] **Step 2: Run repository and cross-build gates**

```sh
go test -tags fpgadev ./... -count=1
go test ./... -count=1
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -tags fpgadev ./cmd/mister-agent ./cmd/mister-fpga-dev ./cmd/fogcast-dev-supervisor
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -tags fpgadev ./cmd/mister-agent ./cmd/mister-fpga-dev ./cmd/fogcast-dev-supervisor
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./cmd/mister-agent ./cmd/mister-fpga-dev ./cmd/fogcast-dev-supervisor
```

Expected: PASS. A broad race failure is acceptable only if reproduced in unchanged out-of-scope code and every touched package passes independently.

- [ ] **Step 3: Audit removed complexity and untracked files**

Use `rg` to prove no production Ed25519/signing-key/public-key, `CleanupAuthoritative`, pidfd survivor cleanup, or schema-2 ready acceptance remains. Run `git diff --check`; for every untracked deliverable run `git diff --no-index --check /dev/null FILE` and require exit 1 with empty output. Record sorted per-file SHA-256 and one deterministic combined binary-diff hash.

- [ ] **Step 4: Obtain independent reviews**

Freeze the unstaged snapshot. Sol reviews lifecycle/MMIO/install recovery against the fail-stop amendment; Vega independently reviews functional correctness and tests. Resolve every in-scope Critical or Important finding. Reviewers must not reintroduce out-of-scope hostile-root, exhaustive race, same-boot cleanup, or signing-key requirements.

- [ ] **Step 5: Request commit/integration authorization**

Present exact reviewed snapshot/tree/diff hashes and limitations. After authorization only, stage reviewed files, commit, prove the commit tree equals the reviewed tree, integrate locally, and rerun focused tests.

- [ ] **Step 6: HIL handoff**

After the separate target gate, install the reviewed package; reboot into development; verify Main/menu and agent; run three OSS cycles, one Quartus-oracle cycle, and one post-intent kill/reboot recovery; uninstall; verify normal Main/menu. Label observations HIL only after they occur.
