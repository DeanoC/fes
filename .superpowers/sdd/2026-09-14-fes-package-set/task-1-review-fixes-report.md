# Task 1 review-fix report

Date: 2026-09-14

Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`

Branch: `feat/fes-package-set`

Base review-fix commit: `ee3ebb143b99477050386d2f6dc5272adc4ac726`

Fix commit: `14b5c71` (`fix: close FES package publication review findings`)

## Scope

This pass addresses the three verified findings in the independent Task 1
review. It is limited to parent package publication, the historical singular
media compatibility helper, and the package-derived media test fixtures. The
image shell helper, profiles, hardware, Quartus, and cold image assembly remain
outside this task.

## F1 — failure-atomic package publication

`scripts/build.py` now builds one closed `.package-generation.new` tree,
verifies it with the existing exact package-output verifier, and only then
starts replacement. Existing `core-packages` and every format-2 selection
record are retained under `.package-generation.previous` while the new root
and selection files are installed. Any replacement or final-verification
failure removes the new destinations and restores the complete prior set.
Successful publication removes the retained prior generation.

Before a new publication, interrupted `.package-generation.new`,
`.core-packages.new`, and `.package-selections.new` trees are safely cleaned.
An interrupted `.package-generation.previous` is either discarded when the
current output is already valid or restored before publication resumes. The
cleanup and mode transitions validate symlinks and special files rather than
following or silently accepting them.

The regression injects an exception after the first selection destination is
replaced and proves that the previous package root, both prior selection
records, and all staging/backup paths remain clean. A second regression starts
with sealed legacy staging and proves a new publication can recover it.

## F2 — singular compatibility helper

`media.published_package` now returns `None` when no package is selected, the
single package dictionary for exactly one package, and rejects a plural
selection rather than leaking the new tuple contract. `published_packages`
remains the tuple-valued helper used by package-only media assembly.

## F3 — receipt-bound media fixture

The media fixture now derives its image receipt through `build.image_fingerprint`
for the package tuple and records the derived inputs and `inputs.json` digest.
The multi-package test updates the derived receipt for its two-package set,
then reverses both the persisted package records and profile order. Media
preparation rejects that stale reordered input through the cold-image receipt
validation path.

## TDD evidence

Each review finding was exercised red before its corresponding fix:

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_package_publication_rolls_back_after_a_selection_replacement_failure
Ran 1
FAILED — the new core root and selection bytes remained after the injected failure
```

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_package_publication_rolls_back_after_a_selection_replacement_failure tests.test_core_build.CoreBuildTest.test_package_publication_cleans_sealed_interrupted_staging
Ran 2
FAILED — 1 failure and 1 PermissionError while removing sealed staging
```

```text
python3 -m unittest tests.test_media.MediaTests.test_singular_published_package_preserves_legacy_shape
Ran 1
FAILED — empty compatibility lookup returned () instead of None
```

```text
python3 -m unittest tests.test_media.MediaTests.test_package_only_media_reuses_the_complete_ordered_package_set
Ran 1
FAILED — reordered package inputs were accepted through the base image receipt
```

After the fixes:

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_package_publication_rolls_back_after_a_selection_replacement_failure tests.test_core_build.CoreBuildTest.test_package_publication_cleans_sealed_interrupted_staging
Ran 2
OK
```

```text
python3 -m unittest tests.test_core_build
Ran 44
OK

python3 -m unittest tests.test_media
Ran 62
OK
```

## Final verification

```text
python3 -m unittest tests.test_core_build tests.test_media
Ran 106
OK
```

```text
python3 -m unittest discover -s tests
Ran 282
OK (skipped=39)
```

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

No Quartus process, cold full image build, image shell suite, hardware test,
push, pull request, or merge was run.

## Handoff

The parent package-set implementation and its review fixes are committed on
`feat/fes-package-set`. The next integration step remains Task 2: consume the
ordered package IDs and per-recipe paths in the target-image shell helper and
fixtures. Hardware acceptance remains pending.
