# Task 1 F1 repair 2 report

Date: 2026-09-14

Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`

Branch: `feat/fes-package-set`

Base: `1d033d60b5e50a5a2491c50319b64dc42d9b89da`

Result commit: pending final commit creation

## Scope

This repair is limited to the FES parent package publication and recovery
path in `scripts/build.py` and its focused regressions in
`tests/test_core_build.py`. F2/F3 behavior and the accepted rollback
regressions remain intact. No image shell, profile, component pin, hardware,
Quartus, parent checkout, other worktree, Task 2, push, pull request, or merge
work was performed.

## F1-1 — semantic closed-generation validation

Generation validation now accepts only a completely empty package-free
generation or a complete package set. Complete sets require `core-packages`,
at least one package directory, and the same number of top-level format-2
selection records as package directories. Package directory names must be
lowercase 64-hex identities, and each directory must contain exactly the two
regular files `manifest.toml` and `core.rbf`. Nested directories, symlinks,
special files, unexpected entries, missing roots, and selection-only markers
are rejected before live output is touched.

The recursive marker path and digest checks remain in place. Safe stale output
is still allowed through the normal cleanup path; strict shape validation is
used when a generation is marked, restored, or treated as a generic live
generation for recovery.

## F1-2 — durability boundaries

Small private fsync helpers validate file and directory types without following
replacement symlinks. They fsync every staged, backup, restore, and live
payload/selection file plus containing directories. A backup marker is written
only after the copied backup is validated and durable; its temporary file,
renamed marker, and marker directory are each synchronized. Restore staging is
durable before live replacement, and the live package set is durable before a
completed backup is removed.

## F1-3 — unmarked backup recovery

An unmarked `.package-generation.previous` is never used as restore input. The
current live package output is first validated as a complete generic package
generation, independent of the next requested package tuple. Only then is the
unmarked backup discarded and the output directory synchronized. Malformed or
unsafe current output leaves both current output and backup untouched.

## TDD evidence

The three focused regressions were added before the production changes and
failed against the prior implementation:

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_selection_only_package_backup_is_rejected_without_touching_live_generation tests.test_core_build.CoreBuildTest.test_unmarked_partial_backup_is_discarded_after_validating_current_generation tests.test_core_build.CoreBuildTest.test_package_publication_fsyncs_payloads_and_replacement_boundaries
Ran 3
FAILED (failures=2, errors=1)
```

After implementation, the same tests passed:

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_selection_only_package_backup_is_rejected_without_touching_live_generation tests.test_core_build.CoreBuildTest.test_unmarked_partial_backup_is_discarded_after_validating_current_generation tests.test_core_build.CoreBuildTest.test_package_publication_fsyncs_payloads_and_replacement_boundaries
Ran 3
OK
```

Existing F1 recovery regressions and the full core suite passed:

```text
python3 -m unittest tests.test_core_build
Ran 49
OK
```

## Verification

```text
python3 -m unittest tests.test_core_build tests.test_media
Ran 111
OK
```

```text
python3 -m unittest discover -s tests
Ran 287
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

Hardware classification: host-only filesystem transaction tests; no hardware
acceptance is claimed. Parent pins and shared contracts are unchanged. The
next integration step is independent F1 re-review; Task 2 must not start.
