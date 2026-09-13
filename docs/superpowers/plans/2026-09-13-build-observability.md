# Build Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add parent build stage timings and safe cache hit/miss explanations while separating the host receipt key from unrelated image, runtime and misteross inputs.

**Architecture:** Keep the existing cold image fingerprint and `reusable(output, kind, fingerprint) -> bool` contract intact. Add a host-specific fingerprint containing the selected FogCast revision, effective Go toolchain, host OS/architecture, version/build options and the actual parent host recipe. Add an atomic, best-effort diagnostic sidecar that records monotonic parent-stage elapsed time and static cache reasons without entering artifact receipts or fingerprints.

**Tech Stack:** Python 3 standard library (`time.monotonic`, `json`, `contextlib`, `unittest`), existing FES Make/Python build orchestration, SHA-256 receipt validation.

**Spec:** `out/orchestration/investigation/proposal.md`

## Global Constraints

- Parent diagnostics are sidecar evidence, not artifact receipt inputs or output files.
- Timing is monotonic elapsed time measured by the FES parent around its own stages and child subprocess calls; it must not be labeled as compiler-internal timing.
- Failure writes the partial diagnostic report and preserves the existing fail-closed build/receipt behavior.
- Diagnostic records contain stage labels, status, static reasons and elapsed seconds only; never record secret environment values or full commands.
- Host keys retain the FogCast commit, Go version, effective OS/architecture, version/build options and actual host recipe; binary embedded identity must remain bound.
- Existing `reusable(...)` remains a boolean API with equivalent valid-output semantics; old receipts may intentionally miss after the key split.
- Media and appliance verification must use the new host key where they consume host artifacts while continuing to require strict image, host, manifest and release evidence. Media preserves image provenance in the manifest and binds the separately preserved host provenance through `host_fes_revision` plus the exact host-receipt digest in the nested media receipt; host revision remains receipt-only because host binaries are not embedded in the disk image.
- Native development base-key changes are limited to excluding the non-build `image/scripts/tests/` tree; actual Buildroot inputs remain keyed.
- The slice edits key-bound recipe runners (`scripts/build.py` participates in the cold image recipe and `scripts/native_dev.py` is bound by the native-development base runner digest), so existing image and native-development base artifacts deliberately miss once when this revision is first integrated; subsequent invocations reuse matching keys. The diagnostic sidecar remains excluded, so diagnostic-only edits do not rotate those artifacts.
- No shared cache volumes, source moves, FPGA BUILD_ID changes, submodule pin changes, cold compiler/image builds or deployment are in scope.

---

### Task 1: Establish baseline and lock the affected interfaces

**Files:**
- Read: `scripts/build.py:84-177,472-658`
- Read: `scripts/native_dev.py:22-188`
- Read: `scripts/media.py:334-390,629-705`
- Read: `scripts/appliance.py:339-357`
- Read: `tests/test_receipt.py`, `tests/test_core_build.py`, `tests/test_native_dev.py`, `tests/test_media_host.py`

**Interfaces:**
- Preserve `build.reusable(output, kind, fingerprint) -> bool`.
- Preserve strict `build.load_verified_host(output, fingerprint, os_name, arch)` validation semantics.
- Introduce `build.host_fingerprint(revisions, profile, toolchain) -> (str, dict)` and use it only for host receipts.
- Keep the existing `(image_fingerprint, fogcast, cores, env)` behavior conceptually intact while extending the internal media selection result with the host fingerprint needed by host validation.

- [ ] **Step 1: Run the baseline parent regression suite.**

Run:

```sh
make test
```

Expected: the pre-change parent and image-script unit suites complete successfully; this is the baseline and does not build a compiler, image or FPGA artifact.

- [ ] **Step 2: Record the baseline result in the task notes.**

Record the command, exit status and test count in the final handoff; do not change source or receipts based on the baseline.

---

### Task 2: Add failing receipt/key and diagnostic tests

**Files:**
- Modify: `tests/test_receipt.py`
- Modify: `tests/test_core_build.py`
- Modify: `tests/test_native_dev.py`
- Create: `tests/test_build_diagnostics.py`

**Interfaces:**
- Tests consume `build.host_fingerprint`, `build.reuse_status`, `build.reusable`, `build.write_receipt` and `build_diagnostics.BuildDiagnostics`.
- Tests define the observable diagnostic schema: `build-diagnostics.json` contains `format`, `action`, `status`, `scope`, `elapsed_seconds` and `stages`; a stage has `name`, `status`, `elapsed_seconds`, and, when applicable, `reason`.

- [ ] **Step 1: Write host-key separation tests.**

Add tests that calculate two host keys from the same profile and toolchain and assert:

```python
base, base_info = build.host_fingerprint(
    {'FogCast': 'a' * 40, 'libmister-runtime': 'b' * 40,
     'misteross': 'c' * 40, 'mister-packages': 'd' * 40},
    {'host_os': 'linux', 'host_arch': 'amd64', 'version': '1'}, 'go1')
changed_system, _ = build.host_fingerprint(
    {'FogCast': 'a' * 40, 'libmister-runtime': 'e' * 40,
     'misteross': 'f' * 40, 'mister-packages': '0' * 40},
    {'host_os': 'linux', 'host_arch': 'amd64', 'version': '1'}, 'go1')
assert base == changed_system
assert base_info['sources'] == {'FogCast': 'a' * 40}
```

Add separate assertions that changing FogCast revision, Go version, OS,
architecture, version, a host option, or the host recipe changes the key.
Keep the existing image-key tests proving FPGA package selection still affects
the image key and media recipe files do not affect the cold image key.

- [ ] **Step 2: Write cache-status and receipt-failure tests.**

Use a temporary output with a real file and `write_receipt` to assert:

```python
hit, reason = build.reuse_status(output, 'image', 'inputs-a')
assert hit and reason == 'verified receipt and output digests match selected inputs'
miss, reason = build.reuse_status(output, 'image', 'inputs-b')
assert not miss and reason == 'selected inputs changed'
```

Cover missing receipt, malformed JSON/shape, empty output map, missing output
and modified output. Assert `reusable` remains `True` only for the valid case
and `False` for each invalid case. Retain the strict `load_verified_host`
tests for missing, malformed, noncanonical, symlinked and changed host inputs.

- [ ] **Step 3: Write diagnostic sidecar tests.**

Create a `BuildDiagnostics` instance in a temporary output, measure one
successful stage, finish it, then assert the JSON contains no `command`,
`argv`, `environment` or secret-like values and records non-negative elapsed
seconds. Exercise an exception inside a measured stage and assert the report
remains readable with `status == 'failed'` and the failed stage present.

Write an artifact receipt, change only `build-diagnostics.json`, and assert
`reusable` remains true. Also assert the diagnostic module is not included in
the recipe file sets used by the artifact fingerprints.

- [ ] **Step 4: Write the native base-key exclusion test.**

Extend the existing temporary Git image fixture with a tracked
`scripts/tests/diagnostic_test.sh`. Assert changing that file leaves
`native_dev.base_key` unchanged, while changing a tracked build script still
changes it. This proves only the non-build test tree is excluded.

- [ ] **Step 5: Run the focused tests and verify the expected red state.**

Run:

```sh
python3 -m unittest tests.test_receipt tests.test_core_build tests.test_native_dev tests.test_build_diagnostics -v
```

Expected: the new tests fail because the host-key, cache-status, diagnostics
and test-tree filtering interfaces are not implemented; existing tests should
continue to identify the unchanged receipt behavior.

---

### Task 3: Implement receipt-safe host keys and cache explanations

**Files:**
- Modify: `scripts/build.py:24-51,84-129,138-164`

**Interfaces:**
- `HOST_RECIPE_FILES`: parent files that directly define host invocation and environment normalization; include `scripts/build.py` and `scripts/environment.py`, but not the profile-dispatching parent `Makefile`.
- `host_fingerprint(revisions, profile, toolchain) -> (fingerprint, info)`: include only `sources.FogCast`, host-prefixed profile options plus `version`, the effective Go version string, and `HOST_RECIPE_FILES`.
- `reuse_status(output, kind, fingerprint) -> (bool, str)`: return static, non-sensitive explanations while validating the same receipt/output digest relationship.
- `reusable(output, kind, fingerprint) -> bool`: return `reuse_status(...)[0]` and remain usable by all existing callers.

- [ ] **Step 1: Add the host recipe and host-input projection.**

Keep `build_fingerprint` as the broad cold-image/base key. Add a host profile
projection that copies `version` and every `host_` option, including the
effective `host_os` and `host_arch`, and a host fingerprint containing only
the FogCast source revision from `revisions`. Do not remove the FogCast
revision or version because FogCast passes its revision/version into embedded
binary identity.

- [ ] **Step 2: Add fail-closed cache status reasons.**

Factor the current `reusable` read/compare logic into `reuse_status` without
loosening validation. Return these exact reason classes: `receipt missing`,
`receipt malformed`, `receipt metadata invalid`, `selected inputs changed`,
`receipt has no outputs`, `output missing or digest changed`, and
`verified receipt and output digests match selected inputs`. Catch malformed
JSON/object shapes and filesystem errors as misses; do not expose paths,
commands, environment values or receipt contents in the reason.

- [ ] **Step 3: Exclude only diagnostics from artifact recipe sets.**

Add the diagnostic source path to an explicit exclusion set used by both
`BUILD_RECIPE_FILES` and `image_recipe_files`. Do not exclude ordinary build
scripts, image files, locks, profiles or source revisions. The sidecar itself
is never passed to `write_receipt`.

- [ ] **Step 4: Run the focused receipt/key tests.**

Run:

```sh
python3 -m unittest tests.test_receipt tests.test_core_build -v
```

Expected: host separation, cache explanations, malformed-output rejection and
diagnostic non-invalidation tests pass; image receipt tests retain their
previous fail-closed behavior.

---

### Task 4: Implement monotonic parent diagnostics and integrate build stages

**Files:**
- Create: `scripts/build_diagnostics.py`
- Modify: `scripts/build.py:472-658`
- Modify: `scripts/native_dev.py:1-33,91-190`

**Interfaces:**
- `BuildDiagnostics(output: Path, action: str)` creates `output/build-diagnostics.json` with `scope == 'parent'` and writes an initial partial report.
- `BuildDiagnostics.measure(name: str)` is a context manager that records monotonic elapsed seconds and records `failed` before re-raising.
- `BuildDiagnostics.cache(name: str, status: str, reason: str)` records `hit`, `miss` or `forced` without timing or sensitive detail.
- `BuildDiagnostics.finish(status: str)` records final status and total monotonic elapsed time.

- [ ] **Step 1: Implement the sidecar writer.**

Use `time.monotonic()` only for elapsed values. Write each update through a
temporary sibling followed by replacement so a failure leaves the latest
complete partial JSON. Keep writes best-effort so a diagnostic filesystem
problem cannot turn a verified artifact into a false success or hide the
original build exception. The report records no commands or environment.

- [ ] **Step 2: Add parent stage boundaries.**

Create the sidecar after the selected profile output directory exists and
measure source staging, host receipt/cache decision, host subprocess stage,
image/FPGA subprocess stage and verification. Use static stage names and mark
the timing scope as parent/subprocess-wrapper. On cache checks, call
`reuse_status` to record hit/miss reasons. A forced rebuild records `forced`
without trying to reuse stale outputs.

- [ ] **Step 3: Preserve partial reports on all parent failures.**

Finish the diagnostic in the existing parent action `try/finally`, selecting
`failed` when an exception escapes and `success` on normal completion. Keep
existing exception propagation and receipt publication order unchanged.

- [ ] **Step 4: Instrument native development without changing artifact keys after the one-time rollout rotation.**

Pass the existing diagnostics object into `native_dev.build_development` (with
an optional default for unit callers). Record the development receipt hit/miss
and measure its parent setup, native-fetch and Buildroot parent subprocess
stages. Do not add the sidecar to `names`, `inputs.json`, the development
receipt or `seed_digest`.

- [ ] **Step 5: Run diagnostic and native-focused tests.**

Run:

```sh
python3 -m unittest tests.test_build_diagnostics tests.test_receipt tests.test_native_dev -v
```

Expected: successful and failed sidecars, cache explanations, diagnostic-only
changes, unchanged-output reuse and the narrowed native base key all pass.

---

### Task 5: Update media and appliance host-receipt consumers

**Files:**
- Modify: `scripts/media.py:334-344,361-368`
- Modify: `scripts/appliance.py:339-357`
- Modify: `tests/test_appliance.py:215-225`
- Add or modify: media/receipt tests covering host/image key distinction

**Interfaces:**
- Internal `media.select` returns `(image_fingerprint, host_fingerprint, fogcast, cores, env)`.
- `media.prepare` calls `load_verified_image` with the image fingerprint and `load_verified_host` with the host fingerprint.
- `appliance.verified_inputs` consumes both selected fingerprints and retains its existing release provenance checks.

- [ ] **Step 1: Add the failing consumer tests.**

Use distinct sentinel image and host keys in the media/appliance fixtures and
assert image validation receives the image key while host validation receives
the host key. Update the existing appliance mocked selection tuple to expose
the intended new internal return shape.

- [ ] **Step 2: Update selection and strict validation calls.**

Compute both keys from the same validated revisions/profile/toolchain. Do not
change media generation fingerprints, private provisioning, manifest checks,
appliance release provenance or `load_verified_host` strictness. A host-only
key miss must reject/rebuild the host prerequisite; it must never authorize
media or appliance publication from an unverified host binary. When the host
key is a hit across an unrelated image/runtime revision, preserve its original
receipt provenance through the exact host-receipt digest separately from the
image revision rather than relabelling the host artifact or rejecting the
composition solely because the revisions differ.

- [ ] **Step 3: Run affected media/appliance tests.**

Run:

```sh
python3 -m unittest tests.test_media_host tests.test_media tests.test_appliance tests.test_appliance_media -v
```

Expected: the new distinct-key assertions pass and existing media/appliance
fail-closed tests remain green.

---

### Task 6: Full bounded verification and scoped commit

**Files:**
- Verify: all changed files and plan

- [ ] **Step 1: Run the complete parent test command.**

Run:

```sh
make test
```

Expected: exit status 0 with all parent and image-script tests passing. Do not
run `make build`, `make verify`, `make dev`, Quartus, a cold image/compiler
build, media, kit or deployment.

- [ ] **Step 2: Run static and diff checks.**

Run:

```sh
python3 -m py_compile scripts/build.py scripts/build_diagnostics.py scripts/native_dev.py scripts/media.py scripts/appliance.py
git diff --check
git status --short
```

Expected: compilation and whitespace checks pass; status contains only the
scoped plan, receipt/key/diagnostic implementation and tests. Existing dirty
root files are outside this worktree and are not touched.

- [ ] **Step 3: Review the requirements against the diff.**

Confirm the diff has no shared volume changes, source moves, FPGA identity or
submodule pin changes, full-command/secret diagnostic fields, receipt inclusion
of diagnostics, weakened validation, or changed media/appliance publication
guarantees.

- [ ] **Step 4: Commit the verified scoped change.**

Run:

```sh
git add docs/superpowers/plans/2026-09-13-build-observability.md scripts/build.py scripts/build_diagnostics.py scripts/native_dev.py scripts/media.py scripts/appliance.py tests/test_receipt.py tests/test_core_build.py tests/test_native_dev.py tests/test_build_diagnostics.py tests/test_media_host.py tests/test_media.py tests/test_appliance.py
git commit -m "build: explain parent reuse and separate host inputs"
```

Report the base `a6ebdb2`, resulting commit, exact tests and results, and state
that no build/image/FPGA/hardware action was run. Do not push or merge; hand
the commit to the coordinator for independent review.
