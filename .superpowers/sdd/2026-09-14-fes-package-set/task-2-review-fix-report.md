# Task 2 review repair

## Scope

The independent Task 2 review found that the pinned FogCast selector at
2089b9e was Pong-only and that the FES shell helper did not bind a selected
environment ID to the selection record's core_id. The repair is split at
the repository boundary:

- FogCast selector commit: 68f1898 target-image: generalize package selector
- FES repair commit: 931fdd7 image: bind FES package selector identities

## Red evidence

- Before the production repair, sh image/scripts/tests/native-package-only_test.sh
  exited 1 at the new select-package --core-id fes.pong forwarding assertion.
- Before the production repair, go test ./internal/targetimage ./cmd/target-image-lock
  failed because the expected-core API was undefined and the CLI rejected
  --core-id.

## Repair

- FogCast now supports the selected fes.pong, fes.zx81, and fes.coleco package
  IDs with per-core closed selection filenames.
- The selector accepts optional --core-id, preserves omitted-argument Pong
  compatibility, validates the expected core against both selection and
  manifest, and emits per-core build-input prefixes.
- FES passes --core-id for package selection, cache verification, installed
  verification, and build-input emission.
- FES validates each source, cached, and copied selection record's core_id
  against its selected package ID.
- Historical format1 paths remain unchanged.

## Green evidence

- go test ./internal/targetimage ./cmd/target-image-lock - PASS.
- go test ./... in the FogCast submodule - PASS.
- sh image/scripts/tests/native-package-only_test.sh - PASS, including
  forwarding and misidentified-selection rejection.
- GOCACHE=<temporary writable directory> FOGCAST_DIR=$PWD/sources/FogCast make -C image test - PASS.
- sh -n on changed shell scripts and git diff --check - PASS.

No profile or Task 3 documentation changed. No Quartus build, hardware action,
push, or merge was performed. Hardware acceptance is not claimed.
