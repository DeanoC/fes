# Task 2 — carry and install the ordered FES package set

## Scope

- Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`
- Branch: `feat/fes-package-set`
- Base: `9320cf181a9fdefb72fd6f133e52d42d2e90d9b5`
- Result: the commit containing this report, reported in the final handoff
- Commit message: `image: install closed FES package sets`

Implemented only the image Task 2 slice. Package-only mode now carries the
ordered `FES_PACKAGE_IDS` contract for `fes.pong`, `fes.zx81`, and
`fes.coleco`; validates the closed input set; mounts every selected pair
read-only at deterministic container paths; stages and installs one shared,
sealed package set; verifies exact package and selection closure; and emits
package inputs in selection order. The historical format1 lane remains
explicitly isolated. `fetch-native-runtime-inputs.sh` continues to delegate
package-only staging to the dynamic helper and required no independent edit.

## TDD evidence

Red before production changes:

- `sh image/scripts/tests/native-package-only_test.sh` — failed as expected
  because the current Pong-only helper returned package count `2` instead of
  the new three-package expectation `4`.
- `make -C image test` — failed at the new package-only fixture before the
  dynamic implementation was present.

Green after implementation:

- `sh image/scripts/tests/native-package-only_test.sh` — PASS.
- `GOCACHE=<temporary writable cache> NATIVE_RUNTIME_MODE=format1 sh image/scripts/tests/target-image_test.sh` — PASS.
- `GOCACHE=<temporary writable cache> NATIVE_RUNTIME_MODE=format1 sh image/scripts/tests/target-image-rootfs_test.sh` — PASS.
- `GOCACHE=<temporary writable cache> FOGCAST_DIR=$PWD/sources/FogCast make -C image test` — PASS; all image fixture targets completed successfully.
- `sh -n` on every changed shell script — PASS.
- `git diff --check` — PASS.

The focused fixture covers ordered Pong/ZX81/Coleco fetch, count, install,
sealed modes, build-input order, copied records, missing/extra/misidentified
package rejection, malformed/duplicate/unknown IDs, missing and extra input
rejection, and the read-only container mount/forwarding contract.

## Changed files

- `image/Makefile`
- `image/scripts/build-target-image.sh`
- `image/scripts/native-extra-cores.sh`
- `image/scripts/target-image-container.sh`
- `image/scripts/verify-target-image.sh`
- `image/scripts/tests/native-extra-cores_test.sh`
- `image/scripts/tests/native-package-only_test.sh`
- `image/scripts/tests/native-runtime-inputs_test.sh`
- `image/scripts/tests/target-image-rootfs_test.sh`
- `image/scripts/tests/target-image_test.sh`
- `.superpowers/sdd/2026-09-14-fes-package-set/task-2-report.md`

No profile or general documentation was changed. No Task 3 work, Quartus,
cold release build, hardware action, push, or merge was performed. The parent
checkout and old untracked review artifacts were left untouched. Hardware
acceptance is not claimed; the result is host-side image-script verification
only.
