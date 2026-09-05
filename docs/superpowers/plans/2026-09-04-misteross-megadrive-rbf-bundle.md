# MisterOSS Mega Drive RBF Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Export the separately compiled Mega Drive RBF as a sealed, content-addressed two-file bundle that FogCast can consume without running Quartus.

**Architecture:** Extend MisterOSS with a dedicated exporter that validates the rebuild against its comparison evidence and core lock, then writes `megadrive.rbf` plus a closed TOML manifest under a digest-named directory. Selection and compilation remain separate; the exporter never deploys hardware or edits FogCast.

**Tech Stack:** Python 3 standard library, `unittest`, GNU Make, Git, SHA-256, TOML text output.

**Spec:** `/home/deano/fes/FogCast-POC/.worktrees/source-built-megadrive-rbf-design/docs/superpowers/specs/2026-09-04-source-built-megadrive-rbf-design.md`

## Global Constraints

- Begin from a clean committed MisterOSS branch containing the current `fetch-core`, `rebuild-core`, and `select-core` work; do not build atop the presently dirty primary checkout.
- Export only MiSTer-compatible system `megadrive` with ABI identifier `mister`.
- The bundle contains exactly `megadrive.rbf` and `megadrive-rbf.toml`.
- Both files have no write bits; the digest-named bundle directory has no write bits after completion.
- The upstream core authority remains `cores.lock` commit `7365a137cfd8fa6f041e964d8b953159c0ec42d9`.
- The accepted initial rebuild is 4,306,912 bytes with SHA-256 `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e`.
- No FogCast changes, image build, deployment, programming, push, PR, or support claim in this plan.

---

### Task 1: Add a closed bundle manifest and content-addressed exporter

**Repository:** `/home/deano/fes/misteross-rebuild` in a new isolated worktree from the clean integrated core-build branch.

**Files:**
- Create: `scripts/export_core_bundle.py`
- Create: `tests/test_export_core_bundle.py`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `CorePin` from `scripts/core_lock.py`, `build/rebuild/<core>/<core>.rbf`, `build/rebuild/<core>/compare.json`, and `scripts/rebuild_core.py`.
- Produces: `export_bundle(pin: CorePin, root: Path) -> Path` and `make export-core-bundle CORE=megadrive`.

- [ ] **Step 1: Write manifest and validation REDs**

Create fixtures requiring exact manifest keys and rejecting stale comparison evidence:

```python
expected = {
    "format": 1,
    "abi": "mister",
    "system": "megadrive",
    "artifact": "megadrive.rbf",
    "sha256": hashlib.sha256(payload).hexdigest(),
    "size": len(payload),
    "repository": pin.repo,
    "revision": pin.commit,
    "recipe": "scripts/rebuild_core.py",
    "recipe_sha256": sha256(root / "scripts/rebuild_core.py"),
    "toolchain": "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition",
}
```

Cover wrong core, commit, project, built digest, built size, missing RBF,
missing comparison JSON, unrecognized comparison fields, and a comparison path
that escapes `build/rebuild/<core>`.

- [ ] **Step 2: Run RED**

Run:

```sh
python3 -m unittest tests.test_export_core_bundle -v
```

Expected: import failure because `scripts.export_core_bundle` does not exist.

- [ ] **Step 3: Implement deterministic closed manifest encoding**

Add immutable data and deterministic output helpers:

```python
@dataclass(frozen=True)
class BundleManifest:
    format: int
    abi: str
    system: str
    artifact: str
    sha256: str
    size: int
    repository: str
    revision: str
    recipe: str
    recipe_sha256: str
    toolchain: str

def encode_manifest(value: BundleManifest) -> bytes:
    # Emit the fields above in that order with LF endings and strict TOML strings.
```

Reject control characters, non-HTTPS repository identity, non-40-character
lowercase revisions, non-64-character lowercase digests, non-positive sizes,
and any ABI/system/artifact value other than the fixed values above.

- [ ] **Step 4: Implement validated content-addressed export**

Implement:

```python
def export_bundle(pin: CorePin, root: Path) -> Path:
    # Validate compare.json, hash the RBF and recipe, build a temporary directory,
    # fsync both files, chmod files 0o444, rename to
    # build/bundles/<core>/<rbf-sha256>, then chmod the directory 0o555.
```

If the final digest directory already exists, byte-compare both regular,
non-symlink files and return it only when exact. Never replace a different or
partial directory. Clean only the exporter-owned temporary directory on error.

- [ ] **Step 5: Add the Make entrypoint**

Add:

```make
export-core-bundle:
	$(PYTHON) scripts/export_core_bundle.py --core "$(CORE)" --root "$(CURDIR)"
```

The command prints only the completed absolute bundle path on its final line so
FogCast can receive it explicitly.

- [ ] **Step 6: Run GREEN and full producer tests**

Run:

```sh
python3 -m unittest tests.test_export_core_bundle -v
python3 -m unittest discover -s tests -v
make test
git diff --check
```

Expected: all pass and no build output is tracked.

- [ ] **Step 7: Run deliberate mutations**

Separately mutate the exporter to trust `compare.json` without rehashing, make
the output files writable, and reuse a partial digest directory. Each mutation
must fail its named fixture. Restore and rerun GREEN.

- [ ] **Step 8: Commit**

```sh
git add scripts/export_core_bundle.py tests/test_export_core_bundle.py Makefile
git commit -m "feat: export sealed Mega Drive RBF bundles"
```

---

### Task 2: Document and verify the producer handoff

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `tests/test_repository_contract.py`

**Interfaces:**
- Consumes: `make export-core-bundle CORE=megadrive` from Task 1.
- Produces: a documented producer command and repository-contract checks for the two-file sealed bundle.

- [ ] **Step 1: Write repository-contract REDs**

Require README and architecture text to distinguish compile, select, and
export; require the exact bundle filenames and state that FogCast receives the
bundle path rather than a mutable build path.

- [ ] **Step 2: Run RED**

```sh
python3 -m unittest tests.test_repository_contract -v
```

Expected: failure because the export contract is not documented.

- [ ] **Step 3: Document the exact handoff**

Document:

```sh
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
make export-core-bundle CORE=megadrive
```

State that `build/current/megadrive.rbf` remains an operator selection and is
not the FogCast release handoff. The handoff is the printed digest directory
under `build/bundles/megadrive/`.

- [ ] **Step 4: Verify the real accepted bundle**

Run the exporter against the accepted rebuild and assert:

```sh
test "$(sha256sum "$bundle/megadrive.rbf" | awk '{print $1}')" = \
  195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e
test "$(wc -c < "$bundle/megadrive.rbf" | tr -d ' ')" = 4306912
test "$(find "$bundle" -maxdepth 1 -type f | wc -l)" -eq 2
test -z "$(find "$bundle" -maxdepth 1 -perm /222 -print)"
```

Record the manifest SHA-256 and bundle path in the task report, not in source.

- [ ] **Step 5: Run full gates and commit**

```sh
make test
python3 -m unittest discover -s tests -v
git diff --check
git status --short
git add README.md docs/architecture.md tests/test_repository_contract.py
git commit -m "docs: define Mega Drive RBF bundle handoff"
```

Obtain an independent review of the exact two-commit range. Do not push or open
a PR without explicit authorization.
