# Task 1 F1 repair 3 — atomic completed-backup cleanup

## Result

- Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`
- Branch: `feat/fes-package-set`
- Base: `28c8df5` (`docs: finalize FES package recovery handoff`)
- Scope: P1 completed-backup cleanup only; Task 2 was not started.
- Implementation commit: recorded after the commit below and finalized in this report.

## TDD evidence

Added `CoreBuildTest.test_completed_backup_cleanup_failure_handoffs_before_deletion`
before changing production code. It injects a selection replacement failure,
removes one payload while deleting the completed backup, and verifies that the
canonical recovery path is not left malformed and that the next publish
succeeds.

Red, against the base tree:

```
python3 -m unittest tests.test_core_build.CoreBuildTest.test_completed_backup_cleanup_failure_handoffs_before_deletion
F
Ran 1 test ...
FAILED (failures=1)
AssertionError: True is not false : completed backup cleanup left a malformed canonical backup
```

The smallest production change adds an atomic rename from
`.package-generation.previous` to a unique disposable sibling before
best-effort recursive deletion. Canonical marked backups are still validated
before cleanup; if the handoff fails, the valid canonical backup is retained.
The same helper is used for the pre-marker partial-backup cleanup path.

Green:

```
python3 -m unittest tests.test_core_build.CoreBuildTest.test_completed_backup_cleanup_failure_handoffs_before_deletion
.
Ran 1 test ...
OK
```

## Verification

- Focused recovery/publication tests: `python3 -m unittest ...` (8 tests, OK).
- Full core/media suite: `python3 -m unittest tests.test_core_build tests.test_media` (112 tests, OK).
- Full Python suite: `python3 -m unittest discover -s tests` (288 tests, OK; 39 skipped).
- Repository consistency: `make check` (OK; package YAML valid, generated consumers and fixture copies match).
- Syntax: `python3 -m py_compile scripts/*.py` (OK).
- Whitespace: `git diff --check` (OK).

## Changed files

- `scripts/build.py`
- `tests/test_core_build.py`
- `.superpowers/sdd/2026-09-14-fes-package-set/task-1-f1-repair-3-report.md`

No image-shell, Quartus, cold image build, hardware action, push, merge, or
Task 2 work was performed. Existing untracked review artifacts were left
untouched.
