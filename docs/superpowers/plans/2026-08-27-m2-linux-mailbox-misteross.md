# M2 Linux Mailbox misteross Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and verify `020_linux_mailbox` in OSS and Quartus lanes, package its exact FogCast development manifest plus canonical synthesis-resource evidence, and transport it through the private FogCast loader without changing the existing MiSTer programming path.

**Architecture:** The new experiment contains a tiny HPS-GPI/GPO mailbox and a Verilator primitive model. Existing build/report tools become experiment-policy-driven so blinky remains zero-hard-block while mailbox requires exactly one Cyclone V HPS general-purpose primitive. A separate `fogcast_dev.py` transport stages an exact bundle, invokes `mister-fpga-dev`, survives the expected reboot disconnect, and retrieves the hash-bound result.

**Tech Stack:** Python 3 standard library, GNU Make, Bash, Verilog, Verilator, pinned Yosys/Mistral/nextpnr toolchain, Quartus Prime Lite 17.0.2 oracle, SSH/SCP.

**Spec:** sibling FogCast `docs/superpowers/specs/2026-08-27-linux-mailbox-dev-loader-design.md`, amended candidate SHA-256 `fe9f8ecc42f9be44a627863562f80a1c92eeb667d868be0071c858f4d388b514`, derived from the operator-approved lifecycle plus the reviewed schema-2 Main identity and signed descriptor-bound synthesis-resource evidence corrections, and pending operator re-approval.

## Global Constraints

- Software-only isolated implementation and review may proceed against the amended candidate. No staging, commit, cumulative integration, target contact, or HIL begins until the operator explicitly re-approves the exact amended spec hash recorded above.
- Root coordinator owns integration and evidence; basic tasks use fresh Luna-max worktrees branched from the last reviewed cumulative integration head, never concurrent writers to overlapping files.
- Before every `git add` or `git commit`, the root coordinator confirms current explicit user authorization; plan text and reviewer approval are not authorization.
- Sol reviews FPGA/HPS and lifecycle contract changes; Vega independently reviews each task before integration.
- The cumulative branch starts from `feature/m0-m1-open-toolchain` commit `6f58c27787c1dc7c74d8141e3e62fe8c7e44668d`; integrate each approved task before creating the next worktree.
- Preserve every M0/M1 purity, manifest, path, transport, and fail-closed gate unless this plan explicitly adds an experiment-specific policy.
- Existing `program`/`PROGRAM_TRANSPORT=mister|jtag` behavior is unchanged. FogCast uses a new `dev-load` target and script.
- Experiment is exactly `020_linux_mailbox`, board is `misterpi`, artifact filename is `top.rbf`, payload is exactly `OSS FPGA OK\n`, and RBF size is 1–16,777,216 bytes.
- Transport bounds are 120 seconds for target reconnect and 30 seconds for exact post-reconnect FogCast/Main readiness; it may observe earlier success but never lengthen either bound.
- OSS policy permits exactly one `cyclonev_hps_interface_mpu_general_purpose` and no other hard block for this experiment; blinky policy remains unchanged.
- No generated RBF, manifest, target result, credential, target identity, or private path is committed.
- Hardware programming is volatile; no flash or persistent target storage is written except root-only temporary/result state owned by FogCast.
- HIL status is deferred to the separate integration plan.

## File Responsibility Map

- `experiments/020_linux_mailbox/rtl/top.v`: synthesizable mailbox state machine.
- `experiments/020_linux_mailbox/sim/hps_gp_model.v`: simulation-only primitive boundary.
- `experiments/020_linux_mailbox/sim/tb.cpp`: exact handshake/negative/terminal tests.
- `experiments/020_linux_mailbox/oracle/top.qpf`, `top.qsf`: Quartus oracle project.
- `experiments/020_linux_mailbox/expected.md`: protocol intent and evidence limits.
- `scripts/experiment_policy.py`: one closed experiment policy table shared by scripts.
- `scripts/oss_summary.py`: experiment-aware hard-block/resource validation.
- `scripts/collect_manifest.py`: schema addition for experiment policy/protocol source bindings.
- `scripts/build_oss.sh`, `build_oracle.sh`: select experiment RTL/constraints/policy.
- `scripts/compare_builds.py`: compare semantic protocol/policy rather than RBF bytes.
- `scripts/dev_bundle.py`: exact FogCast manifest and bundle construction.
- `scripts/fogcast_dev.py`: dedicated private SSH/SCP/reboot/result transport.
- `Makefile`: `dev-bundle` and `dev-load` entry points.

---

### Task 1: Mailbox RTL and Verilator Protocol Proof

**Files:**
- Create: `experiments/020_linux_mailbox/rtl/top.v`
- Create: `experiments/020_linux_mailbox/sim/hps_gp_model.v`
- Create: `experiments/020_linux_mailbox/sim/tb.cpp`
- Create: `experiments/020_linux_mailbox/expected.md`
- Create: `tests/test_mailbox_sources.py`
- Modify: `Makefile`

**Interfaces:**
- Produces top module `top(input wire FPGA_CLK1_50)` with one `cyclonev_hps_interface_mpu_general_purpose` instance and protocol words from the spec.
- Simulation model exposes testbench-controlled GPO and observed GPI without entering synthesis source lists.

- [ ] Write source-policy tests asserting the top-level port list, exact one primitive, no LED/HDMI/SDRAM/PLL/BRAM/DSP/external GPIO tokens, exact 12-byte ROM, and simulation model exclusion from OSS/oracle production commands.
- [ ] Run `python3 -m unittest tests.test_mailbox_sources -v`; expect missing experiment failures.
- [ ] Write `top.v` with deterministic power-up state and constants:

```verilog
localparam [31:0] HELLO = 32'hD3100000;
localparam [31:0] DONE  = 32'hD3130C00;
localparam [95:0] MESSAGE = {8'h4f,8'h53,8'h53,8'h20,8'h46,8'h50,8'h47,8'h41,8'h20,8'h4f,8'h4b,8'h0a};
```

  Hold HELLO until exact `32'hAC100000`; hold each DATA until exact echo ACK; hold END until exact ACK; then hold DONE forever.
- [ ] Write `tb.cpp` cases for inherited nonzero GPO, wrong START, all DATA words/ACKs, wrong ACK, END/ACK, stable DONE, reset/reconfiguration restart, and a parameterized protocol instance that accepts sequence `255` followed by `0`.
- [ ] Add `Makefile` simulation selection for both exact experiment names without interpolating variables into shell code.
- [ ] Run `make sim EXP=020_linux_mailbox` and `python3 -m unittest tests.test_mailbox_sources -v`; expect PASS.
- [ ] Commit with `git commit -m "feat: add Linux mailbox FPGA experiment"` after Sol and Vega approval.

### Task 2: Closed Experiment Policy and OSS Build

**Files:**
- Create: `scripts/experiment_policy.py`
- Create: `tests/test_experiment_policy.py`
- Modify: `scripts/oss_summary.py`
- Modify: `scripts/build_oss.sh`
- Modify: `tests/test_oss_summary.py`
- Modify: `tests/test_oss_purity.py`
- Modify: `tests/test_repository_contract.py`

**Interfaces:**
- Produces `policy_for(name: str) -> ExperimentPolicy`; accepts only `010_blinky` and `020_linux_mailbox`.
- `ExperimentPolicy` fixes source list, top, clock/50 MHz constraint, allowed hard-block counts, and forbidden source/resource patterns.

- [ ] Write failing policy tests for unknown experiment, blinky zero-hard-block preservation, mailbox exact HPS primitive count, forbidden resource use, unknown resource, and wrong top/source list.
- [ ] Run `python3 -m unittest tests.test_experiment_policy tests.test_oss_summary tests.test_oss_purity tests.test_repository_contract -v`; expect failures naming the missing policy module/mailbox support.
- [ ] Implement the frozen policy table. For mailbox, hard-block expectation is exactly:

```python
{"cyclonev_hps_interface_mpu_general_purpose": 1}
```

  All M10K, oscillator, PLL, DSP, SDRAM, video/audio, and unclassified hard blocks remain forbidden.
- [ ] Refactor `build_oss.sh` to obtain paths and policy through fixed script output, synthesize the primitive, route 50 MHz, and pass `--experiment 020_linux_mailbox` to summary/manifest collection. Preserve command logging and OSS purity scans.
- [ ] Run `python3 -m unittest tests.test_experiment_policy tests.test_oss_summary tests.test_oss_purity tests.test_repository_contract -v` and `make oss EXP=020_linux_mailbox`.
- [ ] Inspect `build/oss/020_linux_mailbox/build-summary.json`: status pass, exact primitive count one, other hard blocks zero, timing pass, RBF `top.rbf`.
- [ ] Commit with `git commit -m "feat: build mailbox with experiment policy"` after Sol and Vega approval.

### Task 3: Quartus Oracle and Semantic Comparison

**Files:**
- Create: `experiments/020_linux_mailbox/oracle/top.qpf`
- Create: `experiments/020_linux_mailbox/oracle/top.qsf`
- Modify: `scripts/build_oracle.sh`
- Modify: `scripts/collect_manifest.py`
- Modify: `scripts/compare_builds.py`
- Modify: `tests/test_manifest.py`
- Modify: `tests/test_compare_builds.py`
- Modify: `tests/test_oracle_boundary.py`

**Interfaces:**
- Produces schema-compatible OSS/oracle manifests containing `experiment_policy`, `protocol_source_sha256`, and exact primitive semantic evidence.
- Comparison requires equal target, experiment, clock intent, policy hash, protocol source hash, allowed hard blocks, and passing timing; it explicitly does not require equal RBF hashes.

- [ ] Write failing manifest/comparison tests for missing/mismatched policy hash, protocol hash, target, clock, primitive count, unexpected hard block, timing failure, and distinct-but-valid RBF hashes.
- [ ] Write failing independent resource-evidence tests for reordered/unknown fields, wrong lane/source/artifact/report binding, any extra external port, HPS general-purpose count other than one, and every nonzero PLL/DSP/block-memory/LUTRAM/SDRAM count. Require both real lane report parsers to emit the same canonical unsigned evidence prefix without sharing the target parser.
- [ ] Run `python3 -m unittest tests.test_manifest tests.test_compare_builds tests.test_oracle_boundary -v`; expect the new policy/protocol-binding cases to fail.
- [ ] Create the minimal Quartus project using the identical production `top.v`, device `5CSEBA6U23I7`, `FPGA_CLK1_50` V11, 3.3-V LVTTL, and shared 50 MHz SDC. Do not add simulation model or physical output pins.
- [ ] Extend collection/comparison using the closed policy module; authenticate Quartus 17.0.2 exactly as M1 already requires.
- [ ] Run `python3 -m unittest tests.test_manifest tests.test_compare_builds tests.test_oracle_boundary -v && make oracle EXP=020_linux_mailbox && make compare EXP=020_linux_mailbox`; expect all tests and semantic comparison to pass.
- [ ] Commit with `git commit -m "feat: add mailbox oracle comparison"` after Sol and Vega approval.

### Task 4: Exact FogCast Development Bundle — superseded authority

The former signed Task 4 text is superseded in full. Use the approved unsigned
producer plan as the sole authority for this task:

`docs/superpowers/plans/2026-08-28-unsigned-resource-evidence.md`

Approved companion-plan SHA-256:
`a394262a1f234cd60faa43829731bc55f507ec5296e428598c2269dff0237c18`

That companion plan governs the unsigned schema-2 evidence encoder,
manifest-bound OSS/oracle bundles, freeze checks, and exact fixture handoff.
Do not apply any requirements from the superseded Task 4 text. Tasks 1–3 and
Tasks 5–6 of this plan are unchanged.

### Task 5: Dedicated FogCast SSH/Reboot/Result Transport

**Files:**
- Create: `scripts/fogcast_dev.py`
- Create: `tests/test_fogcast_dev.py`
- Modify: `Makefile`
- Modify: `tests/test_repository_contract.py`

**Interfaces:**
- Produces CLI `make dev-load EXP=020_linux_mailbox BUILD=oss RUN_ID=<32-lower-hex>`, preflight-only CLI `make dev-preflight ...`, and deterministic crash CLI `make dev-fault-inject ...` using environment `FOGCAST_DEV_HOST`, `FOGCAST_DEV_USER`, `FOGCAST_DEV_EXPECTED_BOARD`, `FOGCAST_DEV_EXPECTED_MAIN_SHA256`, `FOGCAST_DEV_TOOL_SHA256`, `FOGCAST_DEV_SSH`, `FOGCAST_DEV_SCP`, and `FOGCAST_DEV_DRY_RUN`.
- Remote execution uses the pinned OpenSSH client in quiet mode and the exact remote command `exec mister-fpga-dev run --manifest /tmp/misteross-fpgadev-<run_id>/manifest.json --artifact /tmp/misteross-fpgadev-<run_id>/top.rbf`, with every fixed token shell-quoted by the transport and no operator value interpolated; the login shell is therefore replaced by the run process and no credential enters argv.

- [ ] Build fake SSH/SCP tests for exact RUN_ID propagation through bundle/stage/invocation/result/cleanup, private `0700` directory/`0600` files, no-follow metadata, exact executable/board/Main attestations, run collision, one invocation, provisional post-intent disconnect handling, exact result-unavailable classification, 120-second reconnect bound, host-key re-resolution hook, result retrieval/binding including session/generation/mode/recovery request, 30-second exact readiness even when the result is missing, valid-result-backed classification of the expected successful reboot disconnect, durable host copy before remote deletion, and cleanup aggregation. Result-unavailable, `recovery_request=failed`, a missing result, missing expected disconnect, or failed readiness fails the run; recovery/readiness verification is never skipped.
- [ ] Add `dev-preflight` tests proving it stages the selected reviewed bundle, invokes exactly `mister-fpga-dev preflight`, accepts only stdout `FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n` with empty stderr, performs no reconnect/result phase, and always cleans the disposable stage. Add rejection tests for every malformed preflight/result line, duplicate/missing result, hash/source/session/generation/mode mismatch, premature disconnect, wrong target, unsafe executable, path injection, and dry-run value outside exact `0|1|false|true`.
- [ ] Add `dev-fault-inject` fake-transport tests: stage and arm, start the exact quiet/direct-`exec` `run` as one owned `subprocess.Popen` SSH session with private host stdout/stderr files, poll `inspect` through separate argv-list SSH sessions until the exact bound identity/phase, invoke `fault-kill`, wait/reap the original SSH child, require SSH exit 255 from the signaled remote process and empty run stdout/stderr with no result framing, and return only after preserving the separate exact `inspect` and `fault-kill` output plus durable private host trace. It must not detach, use a remote shell ampersand, reboot, clean the target stage, or lose child output; every timeout/error terminates/reaps local SSH and preserves the target fence.
- [ ] Run `python3 -m unittest tests.test_fogcast_dev -v`; expect missing module failure.
- [ ] Implement argv-list-only subprocesses, strict fixed remote paths under `/tmp/misteross-fpgadev-<run_id>`, existing ControlMaster safety rules, and no shell interpolation of operator values. The old `scripts/program.py` is not imported or modified.
- [ ] Before commit, run `python3 -m unittest tests.test_fogcast_dev -v`; fake transport tests must pass without requiring a generated real bundle in the dirty implementation worktree.
- [ ] Commit with `git commit -m "feat: add FogCast development transport"` after mandatory Sol and Vega approval and fresh explicit user authorization.
- [ ] Create a disposable clean verification worktree at that exact cumulative commit, rebuild the OSS bundle from that commit, then run `dev-load`, `dev-preflight`, and `dev-fault-inject` with `FOGCAST_DEV_DRY_RUN=1` and distinct fixed run IDs plus the exact environment fixture values; expect argv-only dry-run output and no network contact. Record the commit/tree and remove the disposable worktree.

### Task 6: Cross-Lane Software Gate and Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/oracle-method.md`
- Create: `docs/linux-mailbox-development.md`
- Create: `docs/m2/misteross-software-handoff.md`

**Interfaces:**
- Produces a Software-tested `misteross` slice and immutable inputs for the integration plan.

- [ ] In a disposable clean verification worktree at the exact cumulative implementation commit, run `python3 -m unittest discover -s tests -v`, `make sim EXP=020_linux_mailbox`, `make oss EXP=020_linux_mailbox`, save `sha256sum build/oss/020_linux_mailbox/top.rbf`, remove only `build/oss/020_linux_mailbox`, rebuild OSS and compare the hash, then run `make oracle EXP=020_linux_mailbox`, `make compare EXP=020_linux_mailbox`, `make dev-bundle` for `BUILD=oss` and `BUILD=oracle` with distinct fixed test run IDs, both Task 5 dry-run load commands, both preflight-only dry-run commands, one fault-inject dry-run command, and `python3 -m compileall -q scripts tests`; every command must exit 0 and the two OSS hashes must match.
- [ ] Against the recorded approved-plan base, require a clean index/worktree and run `git diff --check <approved-plan-base>..HEAD`; enumerate every Python/shell/Make/doc path in that complete range. Before adding any handoff file, run `git diff --no-index --check /dev/null <file>`, require exit `1` with empty output, and record its SHA-256; after commit, verify the cumulative tree contains exactly the reviewed content.
- [ ] Verify the existing `010_blinky` simulation/build/manifest/program tests remain unchanged and passing.
- [ ] Sol reviews FPGA primitive, protocol, policy, and transport lifecycle contract; Vega independently reviews all code/tests and verification. Resolve all Critical/Important findings and rerun affected gates.
- [ ] Write documentation with exact commands, hashes, evidence class, no-HIL limitation, rollback, and next safe action. Do not include target identity or credentials.
- [ ] Commit with `git commit -m "docs: hand off Linux mailbox build and transport"`; do not push or contact hardware.

The next safe action is the cross-repository integration plan after both software handoffs pass. This plan alone does not authorize deployment.
