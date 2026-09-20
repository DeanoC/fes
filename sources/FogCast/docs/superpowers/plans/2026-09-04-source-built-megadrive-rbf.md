# Source-Built Mega Drive RBF Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a sealed MisterOSS Mega Drive bundle the default native-image input while preserving the existing locked upstream RBF as an explicit build-time fallback.

**Architecture:** The existing target-image lock tool validates one source selection and atomically stages a normalized `megadrive.selection.toml` plus `megadrive.rbf` in the native cache. All later build, verifier, smoke, and provenance stages consume that selection record, while the runtime continues to receive one unchanged role-based RBF path.

**Tech Stack:** Go, strict TOML decoding, POSIX shell, GNU Make, Buildroot, Docker-compatible container runtime, ext4 image verification, QEMU, designated MiSTer/ShadowCast hardware.

**Spec:** `/home/deano/fes/FogCast-POC/.worktrees/source-built-megadrive-rbf-design/docs/superpowers/specs/2026-09-04-source-built-megadrive-rbf-design.md`

## Global Constraints

- Execute only after the producer bundle plan is integrated and the active native development-RBF milestone is safely integrated or rebased.
- `MEGADRIVE_RBF_SOURCE` defaults to `source-built`; only `source-built` and `upstream` are valid.
- `source-built` requires an explicit absolute `MEGADRIVE_RBF_BUNDLE`; no repository or workstation path is auto-discovered.
- Validation failure never silently selects upstream.
- The image packages exactly one Mega Drive RBF and one idle RBF; it never packages both Mega Drive candidates.
- Installed Mega Drive role path remains `/usr/share/mister-runtime/cores/megadrive.rbf`.
- ABI and system are fixed to `mister` and `megadrive`; generalized/non-MiSTer ABI work is excluded.
- No runtime, protocol, public API, browser, ten-foot, launch, Stop, video, media, or input behavior change.
- Physical support wording changes only after both selector modes pass the stated reproducibility and hardware gates.

---

### Task 1: Add typed bundle and selection validation

**Files:**
- Create: `internal/targetimage/megadrive.go`
- Create: `internal/targetimage/megadrive_test.go`
- Modify: `cmd/target-image-lock/main.go`
- Modify: `cmd/target-image-lock/main_test.go`

**Interfaces:**
- Consumes: the producer bundle's `megadrive-rbf.toml` and `megadrive.rbf`.
- Produces: `targetimage.PrepareMegaDriveSelection(MegaDriveSelectionRequest) (MegaDriveSelection, error)` and CLI command `target-image-lock select-megadrive`.

- [ ] **Step 1: Write closed-manifest REDs**

Define fixture TOML with exactly:

```toml
format = 1
abi = 'mister'
system = 'megadrive'
artifact = 'megadrive.rbf'
sha256 = '195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e'
size = 4306912
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
toolchain = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'
```

Reject unknown/missing fields, wrong schema/ABI/system/artifact, relative or
escaping artifact names, invalid repository/revision/digests/size, control
characters, symlinks, non-regular files, and any write bit on either file.

- [ ] **Step 2: Run RED**

```sh
go test ./internal/targetimage ./cmd/target-image-lock \
  -run 'Test.*MegaDrive' -count=1
```

Expected: compile failure because the manifest and CLI command do not exist.

- [ ] **Step 3: Implement typed closed structures**

Add:

```go
type MegaDriveBundleManifest struct {
    Format       int    `toml:"format"`
    ABI          string `toml:"abi"`
    System       string `toml:"system"`
    Artifact     string `toml:"artifact"`
    SHA256       string `toml:"sha256"`
    Size         int64  `toml:"size"`
    Repository   string `toml:"repository"`
    Revision     string `toml:"revision"`
    Recipe       string `toml:"recipe"`
    RecipeSHA256 string `toml:"recipe_sha256"`
    Toolchain    string `toml:"toolchain"`
    Label        string `toml:"label,omitempty"`
}

type MegaDriveSelection struct {
    Format       int    `toml:"format"`
    Origin       string `toml:"origin"`
    ABI          string `toml:"abi"`
    System       string `toml:"system"`
    Repository   string `toml:"repository"`
    Revision     string `toml:"revision"`
    Artifact     string `toml:"artifact"`
    SHA256       string `toml:"sha256"`
    Size         int64  `toml:"size"`
    InstallPath  string `toml:"install_path"`
    Recipe       string `toml:"recipe,omitempty"`
    RecipeSHA256 string `toml:"recipe_sha256,omitempty"`
    Toolchain    string `toml:"toolchain,omitempty"`
    Label        string `toml:"label,omitempty"`
}

type MegaDriveSelectionRequest struct {
    Source       string
    Bundle       string
    Artifact     string
    UpstreamLock string
    Cache        string
    Output       string
}
```

Use `toml.Decoder.DisallowUnknownFields`. Validate origin-specific optional
fields: source-built requires recipe identities; upstream forbids them.

- [ ] **Step 4: Implement atomic preparation**

Open the source RBF once with no symlink traversal, verify regular/read-only
metadata, stream it into a private temporary cache file while hashing, call
`Sync`, call `Close`, chmod `0444`, and rename. Write the selection record with
the same Sync/Close/Rename ordering. Preserve a prior exact complete cache on
failure, but reject it when selection identity differs.

- [ ] **Step 5: Add the CLI**

Support:

```text
target-image-lock select-megadrive \
  --source source-built|upstream \
  --bundle /absolute/bundle \
  --artifact /absolute/downloaded-upstream-rbf \
  --upstream-lock /work/build/native-runtime.inputs.lock.toml \
  --cache /work/build/cache/target-image/native \
  --output /work/build/cache/target-image/native/megadrive.selection.toml
```

`--bundle` is required and `--artifact` forbidden for source-built. The inverse
holds for upstream. Unknown flags or extra arguments exit 2.

- [ ] **Step 6: Run GREEN, race, vet, and mutations**

```sh
go test ./internal/targetimage ./cmd/target-image-lock -count=1
go test -race ./internal/targetimage ./cmd/target-image-lock -count=1
go vet ./internal/targetimage ./cmd/target-image-lock
```

Mutate unknown-field acceptance, allow writable input, accept the wrong ABI,
and rename before Sync/Close. Each must fail its intended test. Restore GREEN.

- [ ] **Step 7: Commit**

```sh
git add internal/targetimage/megadrive.go internal/targetimage/megadrive_test.go \
  cmd/target-image-lock/main.go cmd/target-image-lock/main_test.go
git commit -m "feat: validate Mega Drive RBF selections"
```

---

### Task 2: Wire explicit build-time selection into native input fetch

**Files:**
- Modify: `Makefile`
- Modify: `scripts/target-image-container.sh`
- Modify: `scripts/fetch-native-runtime-inputs.sh`
- Modify: `scripts/tests/native-runtime-inputs_test.sh`
- Modify: `scripts/tests/target-image-sources_test.sh`

**Interfaces:**
- Consumes: `target-image-lock select-megadrive` from Task 1.
- Produces: default `source-built` native fetch and explicit `upstream` fetch, both yielding `build/cache/target-image/native/megadrive.rbf` and `megadrive.selection.toml`.

- [ ] **Step 1: Write selector RED fixtures**

Require:

```sh
MEGADRIVE_RBF_SOURCE=source-built
MEGADRIVE_RBF_BUNDLE=/absolute/bundle
```

when unset, require source-built and fail before container startup if the bundle
is missing. Require `MEGADRIVE_RBF_SOURCE=upstream` to reproduce the current
locked download without a bundle. Reject unknown source values, relative bundle
paths, bundle symlinks, and any source-built failure that invokes `wget`.

- [ ] **Step 2: Run RED**

```sh
sh scripts/tests/native-runtime-inputs_test.sh
sh scripts/tests/target-image-sources_test.sh
```

Expected: default still fetches upstream and no selection file is produced.

- [ ] **Step 3: Add Make defaults and read-only bundle mount**

Add:

```make
MEGADRIVE_RBF_SOURCE ?= source-built
MEGADRIVE_RBF_BUNDLE ?=
```

Pass both values only through native fetch/build targets. For source-built,
canonicalize the absolute bundle directory and mount it at
`/megadrive-rbf-bundle:ro`; do not pass the workstation path inside provenance.
Upstream mode mounts no bundle.

- [ ] **Step 4: Split idle fetch from Mega Drive selection**

Keep the current locked idle fetch. Replace the unconditional Mega Drive fetch
with a closed case:

```sh
case ${MEGADRIVE_RBF_SOURCE:-source-built} in
  source-built)
    /work/bin/target-image-lock-linux-amd64 select-megadrive \
      --source source-built --bundle /megadrive-rbf-bundle \
      --upstream-lock "$lock" --cache "$cache" \
      --output "$cache/megadrive.selection.toml"
    ;;
  upstream)
    # Download the existing lock into a private temporary file, then call
    # select-megadrive --source upstream --artifact "$temporary".
    ;;
  *) exit 2 ;;
esac
```

Neither branch may call the other after a failure.

- [ ] **Step 5: Run GREEN and selector mutations**

```sh
sh scripts/tests/native-runtime-inputs_test.sh
sh scripts/tests/target-image-sources_test.sh
sh -n scripts/target-image-container.sh scripts/fetch-native-runtime-inputs.sh
git diff --check
```

Mutate the default to upstream, add fallback after source-built failure, and
reuse the opposite-origin cache record. Each must fail its intended fixture.

- [ ] **Step 6: Commit**

```sh
git add Makefile scripts/target-image-container.sh \
  scripts/fetch-native-runtime-inputs.sh \
  scripts/tests/native-runtime-inputs_test.sh \
  scripts/tests/target-image-sources_test.sh
git commit -m "feat: select the native Mega Drive RBF at build time"
```

---

### Task 3: Make image construction and verification consume selection truth

**Files:**
- Modify: `scripts/verify-native-runtime-inputs.sh`
- Modify: `scripts/build-target-image.sh`
- Modify: `buildroot/package/mister-runtime/mister-runtime.mk`
- Modify: `buildroot/board/fogcast-target/native-post-build.sh`
- Modify: `scripts/verify-target-image.sh`
- Modify: `scripts/native-runtime-smoke.sh`
- Modify: `scripts/tests/native-runtime-inputs_test.sh`
- Modify: `scripts/tests/target-image-rootfs_test.sh`
- Modify: `scripts/tests/target-image_test.sh`
- Modify: `scripts/tests/native-runtime-smoke_test.sh`

**Interfaces:**
- Consumes: cached `megadrive.rbf` and `megadrive.selection.toml` from Task 2.
- Produces: one image-owned role RBF and an embedded normalized provenance record checked by offline and live verifiers.

- [ ] **Step 1: Write provenance and exactly-one REDs**

For both origins, require the root filesystem to contain only:

```text
/usr/share/mister-runtime/idle.rbf
/usr/share/mister-runtime/cores/megadrive.rbf
```

among RBF files. Require packaged Mega Drive bytes to match the selection
record, and require the embedded build-input record to contain exact
`megadrive_origin`, `megadrive_abi`, `megadrive_system`, repository, revision,
artifact, SHA-256, size, install path, and origin-specific recipe/toolchain
fields.

- [ ] **Step 2: Run RED**

```sh
sh scripts/tests/native-runtime-inputs_test.sh
sh scripts/tests/target-image-rootfs_test.sh
sh scripts/tests/target-image_test.sh
sh scripts/tests/native-runtime-smoke_test.sh
```

Expected: existing consumers still assume the static `[megadrive_rbf]` lock.

- [ ] **Step 3: Verify the selection at every trust boundary**

Pass the selection file explicitly into `verify-native-runtime-inputs.sh` on
the host and inside the container. Copy it beside each reproducible image output
and pass that exact copy to `verify-target-image.sh`. Never reconstruct selected
origin from environment variables after input preparation.

- [ ] **Step 4: Install one role RBF and normalized record**

Have the Buildroot package install only the selected cache file at the existing
role path. Update `native-post-build.sh` to render the embedded record from the
selection file and fail on any packaged-byte mismatch. Preserve runtime/agent/
idle identities unchanged.

- [ ] **Step 5: Update offline and live verification**

`verify-target-image.sh` must reject a second Mega Drive RBF, a symlink, wrong
bytes, wrong selected origin, or missing origin-specific fields.
`native-runtime-smoke.sh` must compare the installed embedded record against the
host selection authority and continue to assert the same runtime status and
core role path.

- [ ] **Step 6: Run GREEN, race-independent shell gates, and mutations**

```sh
sh scripts/tests/native-runtime-inputs_test.sh
sh scripts/tests/target-image-rootfs_test.sh
sh scripts/tests/target-image_test.sh
sh scripts/tests/native-runtime-smoke_test.sh
sh -n scripts/verify-native-runtime-inputs.sh scripts/build-target-image.sh \
  buildroot/board/fogcast-target/native-post-build.sh \
  scripts/verify-target-image.sh scripts/native-runtime-smoke.sh
git diff --check
```

Mutate the embedded origin, package both candidates, accept a stale selection
record, and skip packaged-byte comparison. Each must fail its named fixture.

- [ ] **Step 7: Commit**

```sh
git add scripts/verify-native-runtime-inputs.sh scripts/build-target-image.sh \
  buildroot/package/mister-runtime/mister-runtime.mk \
  buildroot/board/fogcast-target/native-post-build.sh \
  scripts/verify-target-image.sh scripts/native-runtime-smoke.sh \
  scripts/tests/native-runtime-inputs_test.sh \
  scripts/tests/target-image-rootfs_test.sh scripts/tests/target-image_test.sh \
  scripts/tests/native-runtime-smoke_test.sh
git commit -m "feat: record selected Mega Drive RBF provenance"
```

---

### Task 4: Document pending selection and run software review gates

**Files:**
- Modify: `README.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DEVELOPMENT.md`
- Modify: `scripts/tests/native-megadrive-support-truth_test.sh`

**Interfaces:**
- Consumes: source selection, bundle contract, and image provenance from Tasks 1-3.
- Produces: truthful proposed/hardware-pending documentation and a reviewable software candidate.

- [ ] **Step 1: Write support-truth RED**

Require documentation to state source-built default, explicit upstream fallback,
one packaged Mega Drive RBF, no automatic runtime fallback, and hardware status
pending. Reject wording that claims generalized custom-RBF support.

- [ ] **Step 2: Run RED**

```sh
sh scripts/tests/native-megadrive-support-truth_test.sh
```

Expected: the new selection contract is absent.

- [ ] **Step 3: Document exact commands and boundaries**

Document:

```sh
make target-image-native MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle
make target-image-native MEGADRIVE_RBF_SOURCE=upstream
```

Explain that both are MiSTer ABI artifacts resolved before Buildroot, runtime
behavior is identical, and non-MiSTer/generalized ABI work is deferred.

- [ ] **Step 4: Run complete software gates**

```sh
make -j4 test
make vet
for file in $(git diff --name-only origin/main...HEAD -- '*.sh'); do
  interpreter=$(head -n 1 "$file")
  case "$interpreter" in
    *bash*) bash -n "$file" ;;
    *) sh -n "$file" ;;
  esac
done
git diff --check
git status --short
```

- [ ] **Step 5: Obtain independent review and fix accepted findings**

Review the exact range for fail-closed selection, no silent fallback, cache
identity, atomic ordering, closed schemas, one-RBF packaging, provenance truth,
unchanged runtime requests, and pending support wording. Accepted findings use
strict RED/GREEN and amend only the owning task commit.

- [ ] **Step 6: Commit documentation**

```sh
git add README.md docs/ARCHITECTURE.md docs/DEVELOPMENT.md \
  scripts/tests/native-megadrive-support-truth_test.sh
git commit -m "docs: describe Mega Drive RBF build selection"
```

Do not push or open a PR before user authorization.

---

### Task 5: Build both selections reproducibly and run offline acceptance

**Files:**
- Evidence only under an ignored task evidence directory.
- Modify production/tests only for an evidence-backed failure after strict RED.

**Interfaces:**
- Consumes: reviewed FogCast candidate, canonical runtime, sealed source-built bundle, and upstream lock.
- Produces: exact reproducible source-built and upstream native images approved for physical testing.

- [ ] **Step 1: Freeze immutable inputs**

Record exact FogCast/runtime commits, producer bundle manifest and RBF hashes,
upstream lock identity, container digest, source-cache index, and clean statuses.
Copy the sealed bundle into the evidence root without changing its bytes or
modes.

- [ ] **Step 2: Build source-built twice**

```sh
make target-image-fetch
make target-image-native \
  MEGADRIVE_RBF_SOURCE=source-built \
  MEGADRIVE_RBF_BUNDLE="$sealed_bundle"
make target-image-native-verify
make target-image-native-qemu-smoke
```

Preserve both independent pass images and the resolved selection record in the
evidence root before changing source selection. Require byte-for-byte pass
identity plus exact selection provenance.

- [ ] **Step 3: Build upstream twice in a separate output root**

```sh
make target-image-native MEGADRIVE_RBF_SOURCE=upstream
make target-image-native-verify
make target-image-native-qemu-smoke
```

The upstream preparation must replace the source-built cache identity rather
than reuse it. Preserve both independent upstream pass images and its selection
record in a distinct evidence directory, and require byte-for-byte pass identity
plus upstream provenance.

- [ ] **Step 4: Compare variants**

Assert that the two images differ only in the expected Mega Drive RBF bytes and
normalized provenance fields. Runtime, agent, idle RBF, services, modes,
ownership, library payload, and forbidden-path inventory must match.

- [ ] **Step 5: Review offline evidence**

Obtain independent review of exact input/output hashes, two-pass comparisons,
static verifiers, QEMU transcripts, and variant-diff inventory. No deployment
occurs before READY.

---

### Task 6: Physically accept default, fallback, and legacy rollback

**Files:**
- Modify after PASS: `README.md`
- Modify after PASS: `docs/ARCHITECTURE.md`
- Modify after PASS: `docs/DEVELOPMENT.md`
- Modify after PASS: `docs/hardware/native-megadrive-baseline.md`
- Modify after PASS: `scripts/tests/native-megadrive-support-truth_test.sh`

**Interfaces:**
- Consumes: independently reviewed exact source-built and upstream images from Task 5.
- Produces: evidence-backed default/fallback support wording and final integration candidate.

- [ ] **Step 1: Accept source-built default without retries**

Deploy the exact source-built image once. On one boot, perform two consecutive
cycles, each requiring exactly one public Sonic 2 Launch, exact MegaDrive core
identity, five visible gameplay frames, public right+jump input with visible
response, exactly one Stop, and five valid idle frames. Record cumulative counts
1 then 2 and forbid receiver/ADV reset or launch replay.

- [ ] **Step 2: Accept explicit upstream fallback**

Deploy the exact upstream image once. Perform one exact Sonic 2 launch,
gameplay, right+jump, Stop, and idle cycle. Verify installed RBF and embedded
origin are upstream and request counts are exactly one.

- [ ] **Step 3: Run legacy rollback**

Deploy the freshly reproduced legacy development image once and run the
unchanged `Sonic2 -> MegaDrive -> Stop -> MENU` smoke once. Verify Main/FIFO,
MENU, FPGA operation, and visible legacy output.

- [ ] **Step 4: Publish only evidence-backed wording**

Change proposed/pending wording to state that source-built is the native build
default and upstream is the explicit fallback. Record exact accepted commits,
bundle manifest/RBF, both native images, boots, cycle counts, and legacy image.
Continue to exclude generalized or non-MiSTer ABI support.

- [ ] **Step 5: Run final truth gates and mutations**

```sh
make -j4 test
make vet
sh scripts/tests/native-megadrive-support-truth_test.sh
git diff --check
git status --short
```

Mutate source-built acceptance to zero cycles, remove upstream fallback evidence,
substitute a stale image hash, and claim generalized ABI support. Each must fail
the truth test; restore GREEN.

- [ ] **Step 6: Commit, review, and request integration authorization**

```sh
git add README.md docs/ARCHITECTURE.md docs/DEVELOPMENT.md \
  docs/hardware/native-megadrive-baseline.md \
  scripts/tests/native-megadrive-support-truth_test.sh
git commit -m "docs: qualify source-built Mega Drive RBF selection"
```

Obtain final independent review. After READY, request explicit authorization to
push and open the FogCast PR. Wait for remote checks and separate merge approval.
