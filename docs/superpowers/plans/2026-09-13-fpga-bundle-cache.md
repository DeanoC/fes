# FES FPGA Bundle Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reuse validated FPGA RBF bundles across unrelated `misteross` commits without weakening the existing bundle provenance checks.

**Architecture:** Keep the child bundle format unchanged. Add an ignored FES-local stable cache containing sealed, digest-addressed copies; validate stable candidates with the current recipe and existing `bundle.load` policy, requiring exact `misteross` revision for Pong. Keep the selected-checkout path as the output of a real build, and publish a validated copy for later invocations.

**Tech Stack:** Python 3.11+, `pathlib`, `hashlib`, `shutil`, existing `scripts/bundle.py` validation, `unittest`, Git worktree.

**Spec:** `docs/superpowers/specs/2026-09-13-fpga-bundle-cache-design.md`

## Global Constraints

- Stable entries live below the ignored workspace-local path `out/cache/fpga-bundles/<system>/<closed-bundle-sha256>/`.
- Existing format-1 bundle manifests and child-repository exporters are unchanged.
- Mega Drive, SNES, and NES may reuse across unrelated `misteross` commits only when the current recipe and manifest policy validate the candidate.
- Pong requires the exact selected `misteross` revision and is never treated as equivalent across commits.
- `make rebuild` bypasses both stable and selected-checkout candidates.
- Invalid stable cache entries are advisory misses; malformed selected-checkout bundles remain errors.
- Distinct valid artifacts are ambiguous and must be rejected rather than selected nondeterministically. The error lists the conflicting candidate directories. Recover by removing `out/cache/fpga-bundles/<system>` and retrying; do not silently prefer the selected checkout.
- Do not add shared-cache support for legacy generic OSS builds or FES format-2 package lanes.
- Do not run Quartus or a full image build for unit-level implementation verification.

---

### Task 1: Add sealed stable-cache candidate primitives

**Files:**
- Modify: `scripts/build.py: imports/constants near ROOT and bundle helpers`
- Test: `tests/test_core_build.py`

**Interfaces:**
- Produces `FPGA_BUNDLE_CACHE: Path`, set to `ROOT / "out/cache/fpga-bundles"`.
- Produces `_bundle_directories(root: Path, system: str) -> tuple[Path, ...]`, returning digest-directory candidates below `root` without following a symlink root. Skip names starting with `.`, including `.new-*` staging leftovers.
- Produces `_require_sealed_bundle(directory: Path, system: str) -> None`, requiring a non-symlink directory containing exactly `<system>.rbf` and `<system>-rbf.toml`, with regular non-symlink files and no write bits.

- [ ] **Step 1: Write the failing tests**

Add tests that create temporary cache roots and assert:

```python
def test_stable_bundle_requires_the_closed_sealed_two_file_set(self):
    with tempfile.TemporaryDirectory() as tmp:
        directory = Path(tmp) / ("a" * 64)
        directory.mkdir()
        (directory / "megadrive.rbf").write_bytes(b"rbf")
        (directory / "megadrive-rbf.toml").write_bytes(b"manifest")
        build._require_sealed_bundle(directory, "megadrive")
        (directory / "extra").write_bytes(b"unexpected")
        with self.assertRaisesRegex(ValueError, "closed|unexpected"):
            build._require_sealed_bundle(directory, "megadrive")
```

Also cover a symlinked cache root, a writable bundle file, and a missing
manifest. Use `lstat()`-observable temporary paths so the tests prove the
helper does not follow untrusted cache links.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
python3 -m unittest discover -s tests -p 'test_core_build.py' -v
```

Expected: the new tests fail because the helper names do not yet exist.

- [ ] **Step 3: Implement the minimal primitives**

Add the constant and helpers near the existing bundle functions. Treat a
missing cache root as an empty candidate set. Reject symlink roots, candidate
directories, and files; require the exact two filenames and reject any write
bit on the directory or its contents. Return candidates in sorted order so
selection is deterministic.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run the same focused unittest command and confirm the new tests pass with no
unrelated failures.

- [ ] **Step 5: Commit the primitive slice**

```bash
git add scripts/build.py tests/test_core_build.py
git commit -m "build: add sealed FPGA cache primitives"
```

### Task 2: Select validated stable candidates

**Files:**
- Modify: `scripts/build.py: validate_bundle/build_bundle area`
- Test: `tests/test_core_build.py`

**Interfaces:**
- Produces `_validated_bundle_candidates(source: Path, revision: str, system: str) -> tuple[Path, ...]`.
- The helper searches the selected checkout's `build/bundles/<system>` and `FPGA_BUNDLE_CACHE/<system>`.
- It validates candidates with `validate_bundle`; stable-cache `ValueError` and `OSError` failures are ignored as advisory misses and the skipped path is recorded, while a malformed selected-checkout candidate raises.
- It deduplicates candidates by the validated manifest `sha256` and raises if more than one distinct valid artifact remains. The ambiguity error lists the conflicting candidate directories and does not prefer the selected checkout.

- [ ] **Step 1: Write the failing cross-revision and boundary tests**

Add focused tests that patch only the expensive source/build boundary:

```python
def test_upstream_bundle_from_another_misteross_revision_is_reused(self):
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        source = root / "source"
        cache = root / "cache" / "megadrive" / ("a" * 64)
        (source / "scripts").mkdir(parents=True)
        (cache).mkdir(parents=True)
        (source / "scripts/rebuild_core.py").write_text("recipe")
        (cache / "megadrive-rbf.toml").write_text("manifest")
        (cache / "megadrive.rbf").write_bytes(b"rbf")
        with patch.object(build, "FPGA_BUNDLE_CACHE", root / "cache"), \
             patch.object(build, "source_checkout", return_value=source), \
             patch.object(build.core_bundle, "load", return_value={"sha256": "a" * 64}), \
             patch.object(build, "run", side_effect=AssertionError("unexpected FPGA build")):
            self.assertEqual(build.build_bundle({"misteross": "b" * 40}, {}, system="megadrive"), cache)
```

Add separate tests proving a malformed stable entry falls through to a build
miss, two distinct valid stable artifacts raise an ambiguity error, and a Pong
candidate is passed the selected revision rather than being accepted by a
different revision.

- [ ] **Step 2: Run the new tests and verify RED**

Run:

```bash
python3 -m unittest discover -s tests -p 'test_core_build.py' -v
```

Expected: the new tests fail because `build_bundle` still searches only the
selected revision checkout.

- [ ] **Step 3: Implement candidate validation and selection**

Compute the current recipe digest once per selection. Preserve the existing
single-candidate/error behavior for the selected checkout. Validate stable
entries through `_require_sealed_bundle` and `validate_bundle`; catch only
validation errors from stable entries and continue. Deduplicate using the
manifest `sha256`, then return the sole validated path or raise for ambiguity.

Update `build_bundle` so non-forced actions call the helper before checking
`QUARTUS_ROOTDIR`; a valid stable candidate must return without invoking any
Quartus command. Emit the stable-cache and selected-checkout hit reasons
through `BuildDiagnostics` and use messages that say “validated”, not
“rebuilt”.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run the focused unittest command. Confirm the cross-revision upstream hit,
invalid-cache miss, ambiguity rejection, and Pong revision boundary all pass.

- [ ] **Step 5: Commit the selection slice**

```bash
git add scripts/build.py tests/test_core_build.py
git commit -m "build: reuse validated FPGA bundles across revisions"
```

### Task 3: Publish newly built bundles to the stable cache

**Files:**
- Modify: `scripts/build.py: bundle publication helpers and build_bundle`
- Test: `tests/test_core_build.py`

**Interfaces:**
- Produces `publish_bundle_cache(directory: Path, system: str) -> Path`.
- The destination is `FPGA_BUNDLE_CACHE / system / closed-bundle-sha256`.
- Publication copies exactly the RBF and manifest into a staged sibling,
  seals the files and directory, and atomically installs the digest directory.

- [ ] **Step 1: Write the failing publication tests**

Test that publication:

1. Creates the expected digest directory with byte-exact two-file contents.
2. Leaves the cache directory and files non-writable.
3. Accepts an already-present byte-identical destination.
4. Rejects an already-present destination whose bytes differ.
5. Rejects source or destination symlinks and does not modify an ambient file
   through a symlink.

- [ ] **Step 2: Run the publication tests and verify RED**

```bash
python3 -m unittest discover -s tests -p 'test_core_build.py' -v
```

Expected: failure because `publish_bundle_cache` is not defined.

- [ ] **Step 3: Implement atomic, closed publication**

Validate the source as a regular sealed bundle before copying. Stage with
`tempfile.mkdtemp` under the system cache parent, copy only the two expected
files, apply read-only file modes and a non-writable directory mode, then
install the staged directory atomically. If the destination exists, validate
its closed contents and compare both bytes before accepting it; never replace
different bytes. Clean up the staging directory on every error.

- [ ] **Step 4: Wire publication after a real build**

After `export-core-bundle` produces exactly one bundle and `validate_bundle`
accepts it, call `publish_bundle_cache` and retain the selected-checkout path
as the return value. Catch publication `ValueError` and `OSError`, record
`stable publish skipped: <destination/error>` as a miss, print the skip, and
still return the validated checkout bundle. Staging cleanup must not mask the
primary error. The `force` path must still build first, then attempt to
publish.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run the focused unittest command and confirm both publication and build
selection tests pass.

- [ ] **Step 6: Commit the publication slice**

```bash
git add scripts/build.py tests/test_core_build.py
git commit -m "build: publish validated FPGA bundles to stable cache"
```

### Task 4: Document the cache contract and run regression verification

**Files:**
- Modify: `README.md: FPGA build-cache guidance`
- Modify: `docs/development.md: incremental native image guidance`
- Test: `tests/test_core_build.py` and the repository test suite

**Interfaces:**
- Documentation states that reuse is manifest/recipe validated and
  workspace-local, rather than same-`misteross`-revision-only.
- No user-facing command or manifest field changes.

- [ ] **Step 1: Update documentation**

Replace the README’s “same-revision cached bundle” wording with the stable
cache behavior, including the Pong exact-revision exception and the fact that
`make rebuild` bypasses reuse. Add one paragraph to the incremental workflow
describing `out/cache/fpga-bundles` as an ignored, disposable workspace cache.

- [ ] **Step 2: Run focused and repository tests**

```bash
python3 -m unittest discover -s tests -p 'test_core_build.py' -v
python3 -m unittest discover -s tests -v
```

Expected: both commands exit 0 with no failures. Do not claim a Quartus or
hardware build result from these tests.

- [ ] **Step 3: Inspect the final diff and verify repository state**

```bash
git diff --check
git status --short --branch
git log --oneline --decorate -5
```

Confirm only the planned build, test, documentation, spec, and plan files are
changed on `feat/fpga-bundle-cache`; the parent FES checkout’s generated files
remain outside this worktree.

- [ ] **Step 4: Commit documentation and final test evidence**

```bash
git add README.md docs/development.md tests/test_core_build.py scripts/build.py
git commit -m "docs: describe stable FPGA bundle reuse"
```
