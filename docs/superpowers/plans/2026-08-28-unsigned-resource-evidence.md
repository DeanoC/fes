# Unsigned FPGA Resource Evidence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce deterministic OSS and Quartus-oracle development bundles containing exact unsigned `resource_evidence.json` schema 2 for the FogCast fail-stop loader.

**Architecture:** A Python standard-library bundler reads the already authenticated build manifest and opened RBF, verifies the build/source/report tuple and fixed resource policy, emits canonical unsigned evidence, and copies exactly three development-run files. SHA-256 and exact tuple/count checks provide provenance for this trusted local development host; no signing-key subsystem exists.

**Tech Stack:** Python 3 standard library, `unittest`, GNU Make, existing misteross build manifests.

**Spec:** `/home/deano/FogCast-POC/.worktrees/m2-linux-mailbox/docs/superpowers/specs/2026-08-28-fpgadev-fail-stop-amendment.md`

## Global Constraints

- Work in `/home/deano/misteross/.worktrees/m0-m1-open-toolchain` from HEAD `d0a4bcddfed15f251aebf8338ce17cda334573ad`; preserve the existing uncommitted governing-plan edit.
- Support only experiment `020_linux_mailbox` and lanes `oss|oracle`.
- Emit unsigned schema 2 with exactly the 17 fields and 2048-byte bound from the approved amendment; reject signed schema 1 and all signing fields.
- Derive `source_commit` from exact `git rev-parse HEAD`; reject a dirty source/build selection and stale manifest/source/artifact/report binding. Tests use isolated temporary git fixtures to exercise clean/dirty behavior without requiring the working implementation tree itself to be clean.
- Generate exactly `top.rbf`, the distinct nine-field FogCast artifact `manifest.json`, unsigned `resource_evidence.json`, and sorted `bundle.sha256` over those three payloads in `build/dev-bundle/<lane>/020_linux_mailbox/`. The synthesis build manifest is input evidence and is never copied as the artifact manifest.
- No ambient or vendored cryptography, private/public key environment variable, signature, or fixture vector.
- TDD: genuine RED before implementation.
- Do not stage, commit, push, access the target, or perform HIL without later authorization.

## Required Mailbox Build Prerequisite

Before Task 1, execute Tasks 1-3 of
`docs/superpowers/plans/2026-08-27-m2-linux-mailbox-misteross.md`: create and
simulate `020_linux_mailbox`, implement the closed resource-policy/report
parsers, build OSS and Quartus-oracle RBFs, and compare them. Those tasks must
receive their required independent review. Record their exact reviewed
commit/tree/diff snapshot in this plan's execution ledger before Task 1; the
snapshot must provide passing real commands:

```sh
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
```

Task 1 must not fabricate absent build outputs from the declared `d0a4bcdd...`
base. It begins only from that reviewed prerequisite snapshot and consumes its
real per-lane synthesis manifest, report, and `top.rbf`.

---

### Task 1: Exact Unsigned Evidence Encoder

**Files:**
- Create: `scripts/dev_bundle.py`
- Create: `tests/test_dev_bundle.py`

**Interfaces:**

```python
@dataclass(frozen=True)
class ResourceEvidenceV2:
    schema: int
    experiment: str
    board: str
    build_lane: str
    source_commit: str
    artifact_sha256: str
    synthesis_report_sha256: str
    clock_inputs: int
    external_input_ports: int
    external_output_ports: int
    bidirectional_ports: int
    hps_general_purpose_interfaces: int
    pll_blocks: int
    dsp_blocks: int
    block_memory_bits: int
    lutram_bits: int
    sdram_interfaces: int

@dataclass(frozen=True)
class ArtifactManifestV1:
    schema: int
    run_id: str
    experiment: str
    board: str
    build_lane: str
    artifact_filename: str
    artifact_size: int
    artifact_sha256: str
    source_commit: str

def encode_resource_evidence(value: ResourceEvidenceV2) -> bytes: ...
def encode_artifact_manifest(value: ArtifactManifestV1) -> bytes: ...
def build_bundle(experiment: str, lane: str, run_id: str | None = None) -> Path: ...
```

- [ ] **Step 1: Write encoder RED tests**

Assert the resource evidence's exact field order, compact separators, one newline, schema integer `2`, fixed experiment/board/lane, lowercase 40-character source commit, lowercase 64-character hashes, JSON integer counts in `0..4294967295`, exactly one clock and HPS GP interface, and zero forbidden counts. Reject bool/float/exponent/string/null/negative/overflow counts, unknown lane/experiment, and payloads over 2048 bytes. Assert no output contains `signing`, `signature`, or a key hash. In the same encoder module, test the artifact manifest's exact nine fields, schema `1`, run ID, fixed filename/board/experiment, lane, size bound, hashes, canonical newline, and wrong/reordered/unknown/type cases.

- [ ] **Step 2: Run encoder RED**

```sh
install -d -m 0700 /dev/shm/misteross-evidence
TMPDIR=/dev/shm/misteross-evidence python3 -m unittest tests.test_dev_bundle.ResourceEvidenceTests -v
```

Expected: FAIL because `scripts.dev_bundle` is missing.

- [ ] **Step 3: Implement encoder**

Use `dataclasses`, `json.dumps(..., separators=(",", ":"), ensure_ascii=True)`, explicit ordered dictionary construction, and one appended newline. Validate types with `type(value) is int` so booleans are rejected. Keep the module standard-library-only.

- [ ] **Step 4: Verify Task 1**

```sh
TMPDIR=/dev/shm/misteross-evidence python3 -m unittest tests.test_dev_bundle.ResourceEvidenceTests -v
python3 -m py_compile scripts/dev_bundle.py tests/test_dev_bundle.py
git diff --check
```

Expected: PASS; do not commit.

---

### Task 2: Manifest-Bound OSS and Oracle Bundles

**Files:**
- Modify: `scripts/dev_bundle.py`
- Modify: `tests/test_dev_bundle.py`
- Modify: `Makefile`
- Modify: `.gitignore`

**Interfaces:**
- Consumes `build/<lane>/020_linux_mailbox/manifest.json`, its referenced synthesis report, and `top.rbf` from the same lane and source commit.
- Produces CLI `python3 scripts/dev_bundle.py --experiment 020_linux_mailbox --lane oss|oracle [--run-id 32-lower-hex]` and `make dev-bundle EXP=020_linux_mailbox BUILD=oss|oracle`.
- Generates a distinct FogCast artifact manifest as compact JSON plus newline,
  schema integer `1`, with exact key order:

```text
schema,run_id,experiment,board,build_lane,artifact_filename,artifact_size,
artifact_sha256,source_commit
```

`run_id` is 32 lowercase hex; experiment is `020_linux_mailbox`; board is
`misterpi`; lane is `oss|oracle`; filename is `top.rbf`; size is integer
`1..16777216`; artifact hash is 64 lowercase hex; source commit is 40
lowercase hex. This output is not the synthesis input manifest. The sorted
`bundle.sha256` has exactly three GNU sha256sum-format lines for
`manifest.json`, `resource_evidence.json`, and `top.rbf`.

- [ ] **Step 1: Write bundle RED tests**

Use isolated clean temporary git repositories and lane directories. Cover exact source commit, protocol/policy hash presence, passing build/timing, RBF filename/hash/size, synthesis-report hash, one clock/HPS GP count, zero forbidden resource counts, run-ID validation, output mode `0700` directory/`0600` files, atomic replacement, regular no-symlink inputs, stale/dirty/mismatched/non-passing rejection, and byte-identical repeated generation. Hostile-test the artifact manifest's exact nine-key schema/order/type/value/newline contract, including wrong run ID, filename, size, hash, source, lane, unknown/duplicate/reordered/trailing fields. Parse generated artifact-manifest fixtures with an independent test parser matching FogCast's documented contract. Require exact sorted `bundle.sha256` membership/hash binding and reject missing/extra/duplicate members. Exercise OSS and oracle fixtures independently and require identical evidence grammar without importing FogCast code.

- [ ] **Step 2: Run bundle RED**

```sh
TMPDIR=/dev/shm/misteross-evidence python3 -m unittest tests.test_dev_bundle.BundleTests -v
```

Expected: FAIL because `build_bundle` and Make target are incomplete.

- [ ] **Step 3: Implement manifest binding and atomic output**

Open and hash the selected regular RBF and report; parse the canonical synthesis input manifest; compare every required tuple/hash/count to the same HEAD and lane; independently construct the nine-field FogCast artifact manifest and unsigned evidence schema 2; construct sorted `bundle.sha256` over those records and `top.rbf`; write a private temporary output directory; fsync four files and directory; atomically replace the final lane bundle. Never copy the synthesis manifest under the artifact-manifest name, and never copy a key or signing field.

- [ ] **Step 4: Add Make target**

Add `dev-bundle` to `.PHONY` and help. Validate exact `EXP=020_linux_mailbox` and `BUILD=oss|oracle`, then invoke `$(PYTHON) scripts/dev_bundle.py --experiment "$(EXP)" --lane "$(BUILD)"` with optional `RUN_ID` passed as a separate argv token only when nonempty.

- [ ] **Step 5: Verify Task 2**

```sh
TMPDIR=/dev/shm/misteross-evidence python3 -m unittest tests.test_dev_bundle -v
python3 -m unittest tests.test_manifest tests.test_compare_builds tests.test_oracle_boundary -v
make help >/dev/null
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
make dev-bundle EXP=020_linux_mailbox BUILD=oss RUN_ID=0123456789abcdef0123456789abcdef
make dev-bundle EXP=020_linux_mailbox BUILD=oracle RUN_ID=fedcba9876543210fedcba9876543210
git diff --check
```

Expected: PASS. Record one OSS and one oracle fixture bundle hash; do not commit.

---

### Task 3: Freeze and Review Producer Snapshot

**Files:**
- Modify: `docs/superpowers/plans/2026-08-27-m2-linux-mailbox-misteross.md` only to supersede signed Task 4 with this plan.

- [ ] **Step 1: Run full safe software gates**

```sh
install -d -m 0700 /dev/shm/misteross-evidence
TMPDIR=/dev/shm/misteross-evidence python3 -m unittest discover -s tests -v
python3 -m py_compile scripts/*.py tests/*.py
git diff --check
```

Expected: PASS.

- [ ] **Step 2: Audit removed signing subsystem**

Require `rg -n 'ed25519|FOGCAST_DEV_SIGNING_KEY|FOGCAST_DEV_EVIDENCE_PUBLIC_KEY|signing_key_sha256|"signature"' scripts tests Makefile` to return no production/bundle match. Record every changed/untracked file SHA-256 and deterministic combined diff hash.

- [ ] **Step 3: Independent review**

Freeze the exact unstaged snapshot. Sol checks manifest/source/artifact/report binding; Vega independently checks exact schema/output/Make tests. Resolve all functional Critical/Important findings under the approved trusted-host model.

- [ ] **Step 4: Handoff to FogCast Task 4**

Provide exact reviewed snapshot/diff hashes plus the fixture bundle/evidence hashes. Do not commit until operator authorization. FogCast Task 4 must reject any different or signed producer snapshot.
