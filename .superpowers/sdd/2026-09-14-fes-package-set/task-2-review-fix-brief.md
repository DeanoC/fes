# Task 2 review repair brief

## Scope

Repair the two independent-review findings against commit `218a046` in the
isolated FES worktree `/home/deano/fes/out/dev/fes-package-set/fes`:

1. The pinned FogCast selector at gitlink `2089b9e` is Pong-only. Its
   `select-package` and `verify-package` commands must support the selected
   FES IDs `fes.pong`, `fes.zx81`, and `fes.coleco`, use the corresponding
   closed record names, and emit per-core build-input keys.
2. The FES image helper must pass and enforce the selected core ID so a
   selection record from another core cannot be admitted through a different
   environment slot.

The image lane remains package-only, ordered, and HIP/nextpnr-backed. Keep
historical format1 behavior unchanged. Do not modify the parent checkout,
the profile, Task 3 docs, Quartus paths, or hardware state.

## Required TDD sequence

### Red

- In the FogCast submodule, add/extend unit tests proving the package
  selector accepts all three selected core IDs with per-core output names and
  per-core `--print-inputs` prefixes, while rejecting an expected-core mismatch.
- In FES shell tests, assert the helper passes `--core-id` for select, verify,
  install/verify-image, and build-inputs, and add a misidentified selection
  record whose selected environment ID differs from `core_id`; the fixture
  must fail before the production fix.
- Run the focused FogCast Go tests and the FES package-only fixture; capture
  the expected red result before production edits.

### Green

- Extend FogCast's target-image-lock package API without breaking existing
  Pong callers: default omitted `--core-id` to `fes.pong`, add an explicit
  expected-core argument for the generalized path, derive the closed output
  filename from the supported core ID, validate the record and package core
  identity against that ID, and prefix printed inputs from the core ID.
- Pass `--core-id "$selected_id"` from every package-only selector invocation
  in `image/scripts/native-extra-cores.sh`.
- Re-run the focused FogCast and FES tests, then the full `make -C image test`
  suite with a temporary writable `GOCACHE` and `FOGCAST_DIR`.

## Repository handling

- The FogCast submodule is a separate Git repository. Keep its change as a
  focused commit with message `target-image: generalize package selector`.
- Update the FES gitlink and FES shell/test/report evidence in a focused FES
  repair commit with message `image: bind FES package selector identities`.
- Do not push or create PRs in this repair step. Leave both commit SHAs and
  exact tests in a repair report at
  `.superpowers/sdd/2026-09-14-fes-package-set/task-2-review-fix-report.md`.
- Do not stage old untracked review artifacts. Do not claim hardware
  acceptance.
