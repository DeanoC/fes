# M2 Linux Mailbox Cross-Repository Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate the reviewed FogCast loader and `misteross` mailbox build, then obtain the exact four successful physical cycles and one crash-recovery cycle required for M2 HIL-observed status.

**Architecture:** Software handoffs from both repositories are frozen by commit/tree/artifact hashes before any target contact. The FogCast development profile is installed only on the privately designated disposable kit, each OSS/oracle bundle is driven through the dedicated transport, and reboot reconciliation restores normal compatibility Main ownership after every run. Evidence distinguishes host commands, target observations, operator observations, and inference.

**Tech Stack:** FogCast Go development binaries, `misteross` Python transport, SSH/SCP, MiSTer Pi Linux, Cyclone V FPGA manager/GPI/GPO, OSS RBF, Quartus Prime Lite 17.0.2 oracle RBF.

**Authority:** The original loader design is superseded for this integration by `docs/superpowers/specs/2026-08-28-fpgadev-fail-stop-amendment.md`, operator-approved SHA-256 `ceddb124a7decadbe8f010aeec178223107e5ddf3e6e6eda997fb630a0409dac`, and its FogCast implementation plan `docs/superpowers/plans/2026-08-28-fpgadev-fail-stop-implementation.md`, SHA-256 `75174cd8875f72e108053f711e606db0f68285e15e249cdc4ed8ea96c549ee7b`. The sibling unsigned-resource-evidence plan is `misteross/docs/superpowers/plans/2026-08-28-unsigned-resource-evidence.md`, SHA-256 `a394262a1f234cd60faa43829731bc55f507ec5296e428598c2269dff0237c18`.

## Global Constraints

- No integration or target-contact task starts until both sibling implementations have independently passed review and the operator explicitly authorizes the reviewed commits/integration. Target installation, reboot, fault injection, and HIL remain a later, separate authorization gate.
- Root coordinator alone performs integration, target mutation, reboot, evidence classification, commits, and rollback. Luna-max workers may prepare host-only checks in disjoint worktrees; no subagent receives credentials or mutates hardware.
- Before every staging, commit, install, reboot, SIGKILL, or uninstall action, the root coordinator confirms current explicit user authorization; plan/review approval alone is not authorization.
- Reviews apply the approved trusted-host, disposable-development-target threat model from the fail-stop amendment. Sol reviews lifecycle/ownership evidence; Vega independently reviews the exact software and evidence diff before any Accepted claim.
- Prerequisites are the independently approved FogCast and `misteross` software handoff commits produced by the two sibling plans; their handoff files supply exact immutable hashes before Task 1 starts.
- Target authority comes only from the private disposable-target designation governed by ADR 0002. Resolve it each cycle; never infer authorization from IP, hostname, MAC, board model, or host key.
- Do not write credentials, target identity, private paths, MAC addresses, host keys, or IP addresses into repository evidence.
- All FPGA programming is volatile. No flash, bootloader, kernel, production-image replacement, or raw SD partition/image write is authorized; persistent writes are limited to the reviewed development package/configuration, root-only owner/results state, and their rollback removal.
- Regenerated Dropbear keys may be trusted only through the operator-authorized private target record and must trigger full identity/Main re-attestation.
- Stop immediately and preserve the durable fence on any unexplained owner, process, bridge, MMIO, result, reboot, reconnect, or readiness observation.
- HIL-observed is limited to the mailbox/recovery behavior in the spec; it is not HDMI, audio, latency, input, save, portability, production-readiness, or full-stage acceptance.
- Rollback is removal of the development package/config followed by reboot and exact compatibility Main/Menu readiness verification.

## File Responsibility Map

- FogCast `docs/m2/integration-evidence.md`: redacted command/observation/evidence matrix.
- FogCast `docs/m2/integration-handoff.md`: immutable hashes, status, limitations, rollback, and next action.
- FogCast `docs/ROADMAP.md`: M2 evidence-status update only after gates pass.
- `misteross/docs/bringup-log.md`: build/transport-side redacted HIL observations and artifact hashes.
- No production source file is modified by this plan unless an observed defect is returned to the appropriate software plan as a new reviewed task.

---

### Task 1: Freeze Software and Artifact Preconditions

**Files:**
- Create: `docs/m2/integration-evidence.md`

**Interfaces:**
- Consumes the two software handoff files and produces one run ledger with generated 32-lowercase-hex run IDs and immutable bindings.

- [ ] In clean worktrees, verify each handoff's base/head/tree/spec hashes, review verdicts, normal test/vet results, focused race results, package contents, and stated inherited failures. Bind `manifest.json`, `resource_evidence.json`, `bundle.sha256`, and `top.rbf` as one descriptor-bound bundle; reject branch-name-only provenance.
- [ ] Run FogCast `go test ./...`, `go vet ./...`, and the focused race command from its handoff; run `misteross` `python3 -m unittest discover -s tests -v`, `make sim EXP=020_linux_mailbox`, `make oss EXP=020_linux_mailbox`, `make oracle EXP=020_linux_mailbox`, and `make compare EXP=020_linux_mailbox`; require every command to match the handoff result.
- [ ] Record exact SHA-256 and size for the FogCast package and each attested ARMv7 executable member, Main binary expectation, OSS/oracle `top.rbf`, both manifests, source commits, and target image/config. Require package-member hashes to equal the FogCast handoff manifest. Record only `private designation verified: pass`, with no designation identifier.
- [ ] Generate five unique run IDs with `python3 -c 'import secrets; print(secrets.token_hex(16))'`; store them only in the redacted run ledger, never as authorization tokens.
- [ ] Run the exact Task 5 `misteross` dry-run command twice with `BUILD=oss` and `BUILD=oracle` and distinct ledger `RUN_ID` values; verify argv boundaries, stage/result paths, reboot/readiness phases, and no credential exposure.
- [ ] Have Vega review the frozen ledger and package/artifact bindings before target contact.

### Task 2: Install and Reconcile the Development Profile

**Files:**
- Modify: `docs/m2/integration-evidence.md`

**Interfaces:**
- Consumes the FogCast dev package; produces canonical current-boot `normal_main` and a tested rollback bundle.

- [ ] Resolve the authorized target designation and capture read-only preflight: board/SoC, boot ID, exact Main executable hash/process set, command FIFO identity, FPGA manager state, bridge state, Menu core name, free space, and installed development-tool absence/version.
- [ ] Record rollback prerequisite status exactly `Not required per ADR 0002`, then verify the normal Menu can be restored by reboot before installation.
- [ ] Install through the reviewed `scripts/install-fpgadev.sh` interface with `MISTER_TARGET` supplied only in the environment. Verify the package allowlist and hashes before remote extraction.
- [ ] Before any installer mutation, require an existing root-owned 0600 protected `/etc/fogcast/agent.toml` development profile; reject missing, malformed, or placeholder-token config and record its SHA-256. Confirm installer output proves the old agent was stopped/absent, the recovery trampoline was installed and fsynced before source changes, `initialize-owner` succeeded, exact stable `InventoryV1` and backups were journaled, and the supervisor was installed last under protected locks. Record the immutable terminal-journal digest, then reboot once. After reconnect, consume ReadyRecordV3 bound to the current boot, journal digest, profile hash, capabilities, live supervisor/Main/agent PID-start tuples, and current semantic InventoryV1 checks, including FIFO/FPGA readiness and two stable `MENU` observations within one cumulative deadline. Repeat these checks after every later development reboot.
- [ ] Allocate a disposable sixth preflight-only run ID, then invoke the reviewed `misteross` `make dev-preflight EXP=020_linux_mailbox BUILD=oss RUN_ID=<preflight-id>` interface. It must stage the reviewed mailbox bundle, run the target preflight, verify stdout is exactly `FOGCAST_FPGA_DEV_PREFLIGHT code=ok`, prove no generation/state/FIFO/RBF mutation, and clean its stage. Attempt one harmless normal agent status call before it.
- [ ] Exercise the uninstall/rollback command in dry-run verification mode, then preserve the installed dev profile for Task 3.

### Task 3: Three Consecutive OSS Mailbox Cycles

**Files:**
- Modify: `docs/m2/integration-evidence.md`
- Modify: sibling `misteross/docs/bringup-log.md`

**Interfaces:**
- Produces three independent complete run records bound to the same reviewed OSS artifact and distinct generations/run IDs.

- [ ] For OSS cycle 1, re-resolve designation, attest target/Main/tool, invoke `make dev-load EXP=020_linux_mailbox BUILD=oss RUN_ID=<cycle-id> FOGCAST_DEV_DRY_RUN=0`, and capture host output separately from target result.
- [ ] Verify exact decoded bytes `4f53532046504741204f4b0a`, length 12, payload hash, terminal `d3130c00`, result framing, owner session/generation/mode, artifact/source binding, and ReadyRecordV3/current semantic status.
- [ ] After any post-intent disconnect, reconnect within 120 seconds and verify readiness within 30 seconds: new boot ID, reset reconciliation, canonical `normal_main`, exact Main/FIFO/FPGA/Menu gates, and no stale stage after cleanup. Retrieve and validate the result when present; the exact result-unavailable line or a missing result fails the run but never skips reconnect/recovery verification. Classify the disconnect as the expected successful intermediate event only after a valid result is retrieved and bound.
- [ ] Repeat the entire preflight/load/mailbox/reboot/readiness procedure independently for OSS cycles 2 and 3. Do not reuse SSH control state, stage paths, run IDs, or owner generations.
- [ ] Compare all three records: exact payload/protocol outcome equal; sessions/generations/run IDs distinct and each result mode is `updating`; artifact/source hashes equal; elapsed values may differ and are observations, not acceptance thresholds beyond fixed timeouts.

### Task 4: Quartus Oracle Mailbox Cycle

**Files:**
- Modify: `docs/m2/integration-evidence.md`
- Modify: sibling `misteross/docs/bringup-log.md`

**Interfaces:**
- Produces one complete oracle run record semantically comparable to OSS but bound to its distinct RBF hash.

- [ ] Re-run oracle manifest authentication for exact Quartus 17.0.2, semantic primitive/policy/source comparison, timing, artifact size/hash, and run ID.
- [ ] Execute the same designation, target, loader, mailbox, disconnect, reconnect, result, and readiness gates using `BUILD=oracle`.
- [ ] Require exact payload and terminal word; require oracle artifact binding; explicitly permit the oracle RBF hash to differ from OSS.
- [ ] Require the oracle run ID, session, and generation to differ from all three OSS cycles, its mode to be `updating`, and durable high-water to advance monotonically.
- [ ] Verify no remaining target stage/result, owner anomaly, process, mapping, or readiness difference from the three OSS cycles.

### Task 5: Durable-Intent SIGKILL Recovery Cycle

**Files:**
- Modify: `docs/m2/integration-evidence.md`

**Interfaces:**
- Consumes the development-only deterministic block hook and produces the required crash/recovery evidence without a mailbox success claim.

- [ ] Set `run_id` from the fifth ledger row and invoke the reviewed `make dev-fault-inject EXP=020_linux_mailbox BUILD=oss RUN_ID=<fault-id>` interface. It stages/arms, owns the blocking run in a dedicated local `Popen` SSH session with private output files, polls inspect over separate sessions, invokes `fault-kill`, and reaps the original session without detaching or using a remote shell background job.
- [ ] Verify the preserved host trace contains the separate exact inspect identity/`load_attempted` line and fault-kill success line, proves the blocking run used the pinned quiet SSH/direct-`exec` form, records SSH exit 255 for the signaled run with empty stdout/stderr and no result framing, and shows durable `recovering_intent`, with no private target path copied into repository evidence.
- [ ] Verify the self-SIGKILL endpoint matched the exact peer/run/session/generation and that no numeric PID was signaled; require both the install and owner/process locks to be available afterward while the durable fence remains.
- [ ] Require the fault run ID, session, and generation to differ from all four successful cycles, its active candidate mode to be `updating`, and durable high-water to advance monotonically.
- [ ] On the same boot, attempt an agent launch, stop, Main supervisor restart, agent supervisor restart, and second dev run. Require all to refuse before hardware dispatch with the existing public failure projection/private ownership conflict.
- [ ] Verify no primary result exists, no supervisor recreated Main, and the owner record still conservatively describes recovery. Do not inspect or write mailbox MMIO after the kill.
- [ ] Run `mister-fpga-dev recovery-reboot --run-id "$run_id"`, reconnect within 120 seconds, and prove reset, fresh normal session/generation/high-water, canonical `normal_main`, exact Main/FIFO/FPGA/Menu readiness, boot-owned removal of orphan stage/diagnostic, cleared hook, and no stale process/mapping/lock holder.

### Task 6: Evidence Review, Rollback Drill, and Status

**Files:**
- Modify: `docs/m2/integration-evidence.md`
- Create: `docs/m2/integration-handoff.md`
- Modify: `docs/ROADMAP.md`
- Modify: sibling `misteross/docs/bringup-log.md`

**Interfaces:**
- Produces the final M2 status decision and leaves the target in verified normal compatibility mode.

- [ ] Validate the ledger has exactly three OSS successes, one oracle success, and one fault recovery; across all five require unique run IDs/sessions/generations, monotonic generation high-water, exact `updating` operation mode, and every required phase, timeout, hash, payload, terminal word, reboot, reconnect, readiness, and cleanup field.
- [ ] Perform the reviewed uninstall/rollback, reboot, and exact normal Main/Menu readiness check. Confirm dev-only binaries/config/hooks are absent while normal FogCast behavior remains available.
- [ ] Run secret/private-identity scans across each repository's complete approved-base-to-HEAD range and generated evidence; require clean worktrees and `git diff --check <approved-base>..HEAD`. Before adding each untracked evidence file, run `git diff --no-index --check /dev/null <file>`, require exit `1` with empty output, and record its SHA-256. Redact rather than normalize any accidental private value and invalidate affected evidence if provenance changed.
- [ ] Sol reviews ownership/recovery and evidence classification. Vega independently reviews commands, raw-to-redacted traceability, hash bindings, cleanup, and status language. Resolve all Critical/Important findings.
- [ ] Mark only the narrow M2 mailbox/recovery behavior HIL-observed if every gate passes. Otherwise record the first failed gate and retain the prior status without reinterpretation.
- [ ] Commit FogCast evidence/status with `git commit -m "docs: record M2 Linux mailbox HIL"` and the `misteross` log with `git commit -m "docs: record Linux mailbox bring-up"`. Do not push without a new explicit instruction.

The next safe action after a passing M2 handoff is the separately designed HDMI test-pattern milestone. It is not implied or authorized by this plan.
