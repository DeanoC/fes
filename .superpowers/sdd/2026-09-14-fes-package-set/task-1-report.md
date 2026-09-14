# Task 1 report — parent ordered package resolution and receipts

Date: 2026-09-14

Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`

Branch: `feat/fes-package-set`

Base: `a9ac5ba558b3b3620fb9c3ed98402b086a2152c6`

Implementation commit: `ccc51c112ccb9dcccc44bede69074c50498fc5cd`

Commit message: `build: track ordered FES package sets`

## Scope

Task 1 generalizes the FES parent’s format-2 package plumbing from one
selected package to an ordered package tuple. The change is limited to
parent selection, independent resolution, fingerprints/receipts, closed
publication and verification, native development reuse, media reuse, and
the environment scrub needed to keep the new selector explicit.

The image shell helper, target-image mounting/install behavior, default
three-package profile selection, documentation examples, Quartus, hardware,
and image-build acceptance remain outside this task.

## Implemented behavior

- `selected_packages` now returns the profile’s ordered tuple and accepts the
  three registered recipes (`fes.pong`, `fes.zx81`, `fes.coleco`) while
  rejecting malformed, unknown, duplicate, and historical-profile selections.
- `package_arguments` emits `FES_PACKAGE_IDS` followed by each selected
  recipe’s directory and selection assignments in stable order. Ambient
  `FES_PACKAGE_IDS` is removed by `build_environment` along with the existing
  package selectors.
- The parent resolves each selected recipe independently and carries every
  child package directory, selection path, and input digest into the package
  tuple.
- Image fingerprints and persisted `inputs.json` retain the ordered package
  input records. Persisted package records reject empty/malformed IDs and
  duplicate package IDs before deriving a receipt key.
- Parent publication accepts a mapping from selection filename to child
  selection path, validates the complete set before replacing output, stages
  all package directories and selection records, and verifies the exact
  selected set. Extra selection files, extra package identities, duplicate
  package IDs, and changed bytes fail closed.
- `native_dev.py` copies and validates every selected child record and passes
  the complete tuple through package-only and historical format-1-compatible
  paths. `media.py` reconstructs and validates the complete ordered tuple
  from the profile and image receipt before exporting its environment.
- Existing single-package and package-free callers remain compatible while
  the new tuple contract is exercised by focused multi-package fixtures.

## TDD evidence

Tests were added before the production changes and the focused suite was run
against the old implementation. The expected failures were observed in all
six new behavior areas:

```text
python3 -m unittest tests.test_core_build tests.test_receipt tests.test_native_dev tests.test_media
Ran 128 tests
FAILED (errors=6)
```

After implementation and the final persisted-ID validation, the same focused
command passed:

```text
python3 -m unittest tests.test_core_build tests.test_receipt tests.test_native_dev tests.test_media
Ran 129 tests in 1.849s
OK
```

The full Python suite also passed:

```text
python3 -m unittest discover -s tests
Ran 279 tests in 3.893s
OK (skipped=39)
```

The existing recipe-registry expectation was updated because it previously
asserted that a valid multi-package selection must fail; that assertion is
contradictory to the Task 1 contract.

## Additional checks

```text
make check
python3 scripts/consistency.py
consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match
```

```text
python3 -m py_compile scripts/*.py
exit 0

git diff --check
exit 0
```

No Quartus process, cold full image build, target-image shell suite, hardware
test, push, pull request, or merge was run. The skipped image-shell coverage
belongs to Task 2; profile/docs coverage belongs to Task 3. Hardware
acceptance remains pending.

## Handoff

The next integration step is Task 2: consume `FES_PACKAGE_IDS` and all
per-recipe paths in the target-image Makefile/container/helper, then add the
multi-package shell fixtures. Task 3 can subsequently select all three
recipes in the default profile and update the documentation against the
shell helper’s final output contract.
