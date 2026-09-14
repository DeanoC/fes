# Task 1 F1 repair report

Date: 2026-09-14

Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`

Branch: `feat/fes-package-set`

Base commit: `27962d9e90e406b195b929fc60ef167559bca447`

Repair commit: `6752660051752dc4a9ab4e6397eb4d77aa0bb92e`
(`fix: make FES package backup recovery fail closed`)

## Scope

This repair is limited to the F1 package publication/recovery path and its
focused regressions in `scripts/build.py` and `tests/test_core_build.py`. F2
and F3 behavior was retained and verified. No parent checkout, other
worktree, image shell, profile, component pin, hardware, Quartus, push, pull
request, merge, or Task 2 work was performed.

## F1 — failure-closed package publication and recovery

The publisher now copies the current package-owned destinations while they
remain live, recursively rejecting symlinks and special files. The copy is
sealed and checked as a closed generation, then receives the durable
`.package-generation.complete` JSON marker containing the exact directory set,
file set, and SHA-256 values. The marker is fsynced before the backup is
eligible for recovery, so a partial or unmarked backup is never used to clear
current output.

Backup validation checks the marker, every nested entry, unexpected entries,
missing destinations, and content digests before any live destination is
touched. Restore copies the validated backup into
`.package-generation.restore`, validates that complete restore generation,
replaces the live destinations, and validates the restored generation against
the backup manifest.

Rollback failures leave the sealed backup available for the next invocation
and do not replace the original publish exception. A later normal publish
recovers the old generation first, then installs the new exact package set.
Legacy `.core-packages.new` and `.package-selections.new` cleanup remains
supported, and destination symlink/non-regular protections remain enforced.

## TDD evidence

The required regressions were added before the production repair and were
observed failing against the prior implementation:

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_malformed_sealed_package_backup_preserves_the_live_generation tests.test_core_build.CoreBuildTest.test_rollback_failure_preserves_backup_for_the_next_normal_publish
Ran 2
FAILED — malformed backup recovery left the live package snapshot empty;
rollback failure was reported instead of the injected original publish error.
```

After the implementation, the same two regressions passed:

```text
python3 -m unittest tests.test_core_build.CoreBuildTest.test_malformed_sealed_package_backup_preserves_the_live_generation tests.test_core_build.CoreBuildTest.test_rollback_failure_preserves_backup_for_the_next_normal_publish
Ran 2
OK
```

## Verification

```text
python3 -m unittest tests.test_core_build
Ran 46
OK
```

```text
python3 -m unittest tests.test_core_build tests.test_media
Ran 108
OK
```

```text
python3 -m unittest discover -s tests
Ran 284
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

## Handoff

Hardware classification: host-only tests and filesystem transaction
regressions; no hardware acceptance is claimed.

Parent pins and shared contracts are unchanged. The next step is an
independent F1 re-review; Task 2 must not start until that review accepts the
repair. Concern: cold image, image-shell, and physical-kit validation remain
unrun by scope.
