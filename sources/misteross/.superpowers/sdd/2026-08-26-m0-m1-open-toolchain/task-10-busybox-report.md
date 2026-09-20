# Task 10 BusyBox compatibility report

## Root cause

The real read-only MiSTer Pi probe reached the ARM-side remote preflight and
stopped before any mutation because the target is BusyBox 1.33.1 and does not
provide the external `stat` applet. The target does provide the BusyBox
applets needed by the transport (`ls`, `readlink`, `sha256sum`, `awk`, `wc`,
and the shell/test primitives). The existing remote preflight, staging,
verification, and final-load scripts invoked `stat -Lc` repeatedly, so a
portable RBF dry-run could not get past metadata collection.

## TDD evidence

The initial RED run was performed after adding the regression tests and before
the implementation:

```text
$ python3 -m unittest tests.test_program_busybox -v
Ran 3 tests
FAILED (failures=5, errors=2)
AttributeError: module ... has no attribute '_remote_metadata_helper'
AssertionError: generated remote scripts still contained `stat`
AssertionError: generated load script did not contain `wc -c`
```

The GREEN focused run after implementation was:

```text
$ python3 -m unittest tests.test_program_busybox tests.test_program_preflight -v
Ran 53 tests
OK
```

The complete repository suite was also run:

```text
$ python3 -m unittest discover -s tests -q
----------------------------------------------------------------------
Ran 171 tests in 112.350s

OK
```

Additional checks passed:

```text
$ python3 -m py_compile scripts/program.py tests/test_program_busybox.py
$ git diff --check
4 generated remote scripts passed /bin/sh -n and the external-stat command scan
```

## Implementation

- Added one shared `misteross_metadata` POSIX shell helper. It uses
  `LC_ALL=C busybox ls -din` or `-Ldin`, ignores date/path fields, converts
  symbolic permissions (including set-id and sticky forms) with BusyBox awk,
  and emits `type|uid|gid|octal-mode|inode|nlink`.
- Replaced remote metadata calls in preflight, atomic staging-directory
  creation, upload verification, and the final load recheck. Symlink, type,
  owner, mode, inode, link-count, and hash checks remain fail-closed.
- Replaced remote byte-size probing with `wc -c < validated-absolute-path>`.
- Kept local Python `stat` calls unchanged and removed unsupported `--`
  operands from remote commands whose generated absolute paths are validated.
- Added real local BusyBox fixture tests for a 0700 directory, 0400 file,
  FIFO, symlink and followed `/proc` FD/executable, plus set-id/sticky modes
  and a dangling followed link.
- Recorded the target diagnosis in `docs/bringup-log.md`.

No target connection, RBF upload, FIFO write, or hardware mutation was
performed by this task. The final real MiSTer dry-run should be repeated by
the controller to confirm the target's BusyBox 1.33.1 shell behaves as the
local fixture does.
