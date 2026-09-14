# FES Multi-Package Image Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the FES package-only image install and verify the ordered Pong, ZX81 and Coleco format-2 package set with independent cache-backed resolution.

**Architecture:** Keep recipe resolution and package identity in the FES parent, then pass a validated package-set contract through the image Makefile and container wrapper. The shell helper owns closed-set staging and verification; parent receipts and media/development reuse retain the ordered package input list.

**Tech Stack:** Python 3.11 `unittest`/TOML, POSIX shell, GNU Make, Docker-compatible target-image scripts, existing FES recipe registry and target-image-lock selector.

**Spec:** `docs/superpowers/specs/2026-09-14-fes-package-set-design.md`

## Global Constraints

- `profiles/native-integration-dev.toml` is the source of truth and selects `fes.pong`, `fes.zx81`, `fes.coleco` in that order.
- `FES_PACKAGE_IDS` is the comma-separated ordered parent-to-image package-set contract.
- Package-only output contains idle plus exactly one sealed package directory and selection record per selected package.
- Reject unknown, duplicate, malformed, missing, extra, or misidentified packages; do not silently select a cache candidate.
- Preserve the independent HIP producer/cache lanes from PR #39; do not use ambient toolchain selectors.
- Do not invoke Quartus or reintroduce format-1 construction in package-only paths.
- Preserve historical format-1 profiles and the dirty parent checkout at `/home/deano/fes`.
- Do not claim hardware acceptance from these tests.

---

### Task 1: Generalize FES parent package resolution and receipts

**Files:**
- Modify: `scripts/build.py`
- Modify: `scripts/native_dev.py`
- Modify: `scripts/media.py`
- Test: `tests/test_core_build.py`
- Test: `tests/test_receipt.py`
- Test: `tests/test_native_dev.py`
- Test: `tests/test_media.py`

**Interfaces:**
- `selected_packages(profile, profile_name)` returns an ordered tuple of recipe IDs.
- `package_arguments(packages)` returns the flattened `FES_PACKAGE_IDS` plus per-recipe directory/selection assignments.
- `image_fingerprint(base_fingerprint, info, packages)` stores all package input records in order.
- Parent package publication and verification accept the ordered package tuple and a mapping of child selection filenames to paths.

- [ ] **Step 1: Add failing tests for the ordered package set.**

  Extend the existing build tests to assert that the three profile entries
  return `("fes.pong", "fes.zx81", "fes.coleco")`, that a duplicate or
  unknown entry fails, that `package_arguments` emits
  `FES_PACKAGE_IDS=fes.pong,fes.zx81,fes.coleco` and all six path assignments,
  and that a two-record image fingerprint changes when either record or its
  order changes. Add a parent publication fixture containing two package
  directories and two selection records; assert the closed output contains
  both and rejects an extra package.

- [ ] **Step 2: Run the focused tests and confirm the expected red failures.**

  Run:

  ```sh
  python3 -m unittest tests.test_core_build tests.test_receipt tests.test_native_dev tests.test_media
  ```

  Expected: failures identify the current single-package assumptions in
  selection, fingerprint, publication and media/development binding.

- [ ] **Step 3: Implement the minimal ordered parent contract.**

  Resolve one package per selected recipe, keep the tuple through `main`, and
  pass all package arguments to every child build, verify, development and
  media operation. Make output verification compare the exact selected set of
  selection files and package IDs, and make publication stage all packages
  atomically before verifying them.

- [ ] **Step 4: Run the focused tests and the parent consistency check.**

  Run the command from Step 2, then:

  ```sh
  make check
  ```

  Expected: all focused tests pass and the existing generated-consumer/source
  pin consistency check remains green.

- [ ] **Step 5: Commit the parent package-set slice.**

  ```sh
  git add scripts/build.py scripts/native_dev.py scripts/media.py tests/test_core_build.py tests/test_receipt.py tests/test_native_dev.py tests/test_media.py
  git commit -m "build: track ordered FES package sets"
  ```

### Task 2: Carry and install the package set in the target image

**Files:**
- Modify: `image/Makefile`
- Modify: `image/scripts/target-image-container.sh`
- Modify: `image/scripts/fetch-native-runtime-inputs.sh`
- Modify: `image/scripts/native-extra-cores.sh`
- Modify: `image/scripts/build-target-image.sh`
- Modify: `image/scripts/verify-target-image.sh`
- Test: `image/scripts/tests/native-package-only_test.sh`
- Test: `image/scripts/tests/target-image_test.sh`
- Test: `image/scripts/tests/target-image-rootfs_test.sh`

**Interfaces:**
- The Makefile exports `FES_PACKAGE_IDS` and the selected per-recipe path variables.
- The container wrapper mounts each selected host package directory and selection file at a deterministic per-core path and forwards the same IDs.
- `native-extra-cores.sh` stages and verifies the exact selected package set under one shared `core-packages` root.

- [ ] **Step 1: Add failing shell fixtures for three packages.**

  Extend the package-only fixture with synthetic Pong, ZX81 and Coleco
  package directories and selection records. Set `FES_PACKAGE_IDS` to the
  three comma-separated IDs and assert `count` is four (idle plus three),
  `install` creates three sealed package directories and three package
  selection records, and `verify-image` rejects a missing or extra package.
  Add a dry-run/container-wrapper assertion that all selected package paths
  are mounted and forwarded.

- [ ] **Step 2: Run the image fixtures and confirm the expected red failures.**

  Run:

  ```sh
  make -C image test
  ```

  Expected: the new three-package assertions fail against the current
  Pong-only helper and mount wrapper.

- [ ] **Step 3: Implement dynamic package-set staging.**

  Map each supported core ID to its existing environment names and selection
  filename. Clean only the validated package cache contents on fetch, require
  the exact selected package IDs on verify/install, install each package and
  record with sealed modes, and emit package build inputs in profile order.
  Keep format-1 cleanup/rejection unchanged. Update the container wrapper to
  validate absolute inputs and append one read-only mount pair per package.

- [ ] **Step 4: Run the image test suite and focused shell fixtures.**

  Run:

  ```sh
  make -C image test
  sh image/scripts/tests/native-package-only_test.sh
  sh image/scripts/tests/target-image_test.sh
  ```

  Expected: all image tests pass, including the legacy format-1 fixture lane.

- [ ] **Step 5: Commit the image package-set slice.**

  ```sh
  git add image/Makefile image/scripts image/scripts/tests
  git commit -m "image: install closed FES package sets"
  ```

### Task 3: Select all three lanes and update documentation

**Files:**
- Modify: `profiles/native-integration-dev.toml`
- Modify: `README.md`
- Modify: `docs/core-packages.md`
- Modify: `docs/getting-started.md`
- Modify: `docs/development.md`
- Test: `tests/test_core_build.py`
- Test: `tests/test_image_assembly.py`

- [ ] **Step 1: Add failing profile/documentation assertions.**

  Assert the default profile lists the three IDs in order and that the docs
  describe the exact package-only output and cache-backed HIP lanes rather
  than a Pong-only selector.

- [ ] **Step 2: Implement the profile and documentation update.**

  Replace the single profile entry with the three ordered entries. Document
  that `make build`, `make dev`, `make verify` and media reuse the same closed
  package set, while historical format-1 profiles remain separate and
  Quartus remains oracle-only.

- [ ] **Step 3: Run the parent tests and commit.**

  ```sh
  python3 -m unittest tests.test_core_build tests.test_image_assembly
  git diff --check
  git add profiles/native-integration-dev.toml README.md docs/core-packages.md docs/getting-started.md docs/development.md tests/test_core_build.py tests/test_image_assembly.py
  git commit -m "docs: make the FES package set explicit"
  ```

### Task 4: End-to-end verification and handoff

**Files:**
- Create: `docs/validation/2026-09-14-fes-package-set.md`
- Modify: `.superpowers/sdd/2026-09-14-fes-package-set/progress.md`

- [ ] **Step 1: Run the complete available software verification.**

  Run `make check`, `python3 -m unittest discover -s tests -p 'test_*.py'`,
  `make -C image test`, and `python3 -m py_compile scripts/*.py`.

- [ ] **Step 2: Exercise cache behavior without a cold Quartus or full release build.**

  Use isolated output/cache directories and fixture or already-validated
  HIP package lanes to record one package miss and one exact package hit for
  Pong, ZX81 and Coleco. Record producer commit, package IDs, cache paths,
  whether Yosys/nextpnr HIP, Quartus or QEMU ran, and the exact image receipt
  hashes. Do not use a green watcher or simulation as hardware acceptance.

- [ ] **Step 3: Write the dated validation record and inspect the final diff.**

  Include commands, exit codes, test counts, cache evidence and the explicit
  hardware-pending boundary. Run `git diff --check`, `git status --short` and
  `git log --oneline origin/main..HEAD`.

- [ ] **Step 4: Commit the validation record and prepare the review handoff.**

  ```sh
  git add docs/validation/2026-09-14-fes-package-set.md .superpowers/sdd/2026-09-14-fes-package-set/progress.md
  git commit -m "test: record FES package-set verification"
  ```

  The branch is ready for independent review once the verification output is
  captured; do not merge it without explicit approval.
