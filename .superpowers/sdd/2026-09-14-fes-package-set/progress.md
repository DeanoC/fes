# SDD ledger — plan: docs/superpowers/plans/2026-09-14-fes-package-set.md

## Baseline

- Worktree: `/home/deano/fes/out/dev/fes-package-set/fes`
- Branch: `feat/fes-package-set`
- Base: `a9ac5ba558b3b3620fb9c3ed98402b086a2152c6`
- `make check` and focused parent tests passed after submodule initialization.

## Plan scan

| Scope | Shared interface/file | Finding | Ruling |
|---|---|---|---|
| Task 1 -> Task 2 | `package_arguments`, `FES_PACKAGE_IDS` | Parent must emit the exact ordered IDs and paths consumed by the image wrapper. | Task 1 owns the Python shape; Task 2 owns shell validation and mounting. |
| Task 1 -> Task 3 | `fpga_packages`, `inputs.json` | Profile order must be stable before fingerprint and documentation assertions. | Task 3 changes only the profile values after Task 1 supports the tuple. |
| Task 2 -> Task 3 | package-only output names and counts | Docs must describe the output emitted by the helper, not a future runtime catalog. | Task 2 defines filenames and closure; Task 3 documents them. |
| Task 1 -> Task 4 | receipts and cache evidence | Verification must use the multi-package receipt rather than infer success from a build log. | Task 4 reads `inputs.json`, image receipt and producer records. |
| Task 1 | parent Python files/tests | Tests are specified against tuple package inputs and exact output closure. | Self-consistent. |
| Task 2 | image scripts/tests | Fixture uses the same comma-separated contract and exact selected package set. | Self-consistent. |
| Task 3 | profile/docs/tests | Default profile and docs assert the same ordered three IDs. | Self-consistent. |
| Task 4 | validation/ledger | Validation records only commands actually run and keeps hardware acceptance pending. | Self-consistent. |

## Rulings

- Ruling: Keep the current default profile as the all-three FES package set — the user explicitly asked to include ZX81 and Coleco, and the merged recipe registry already defines their independent HIP lanes; the cost is a larger first cold image build, mitigated by per-lane cache reuse.
- Ruling: Keep historical format-1 profiles intact — the request removes format-1 from FES production, not from unrelated historical reproduction paths; the cost is retaining legacy code outside the default path.

## Task 1 — completed

- Implementation commit: `ccc51c112ccb9dcccc44bede69074c50498fc5cd` (`build: track ordered FES package sets`).
- Red: `python3 -m unittest tests.test_core_build tests.test_receipt tests.test_native_dev tests.test_media` — `Ran 128 tests`; `FAILED (errors=6)` at the ordered selection, package arguments, ordered fingerprint, multi-package publication, native-dev tuple, and media tuple regression tests.
- Green: `python3 -m unittest tests.test_core_build tests.test_receipt tests.test_native_dev tests.test_media` — `Ran 129 tests in 1.849s`; `OK`.
- Full Python suite: `python3 -m unittest discover -s tests` — `Ran 279 tests in 3.893s`; `OK (skipped=39)`.
- Consistency: `make check` — `consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match`.
- Syntax: `python3 -m py_compile scripts/*.py` — exit 0.
- Whitespace: `git diff --check` — exit 0 before the implementation commit.
- Scope boundary: no Quartus, cold full image build, image shell suite, hardware test, push, PR, or merge was performed; target-image shell mounting/install work remains Task 2 and the all-three default profile update remains Task 3.

## Task 1 review fixes — completed

- Fix commit: `14b5c71` (`fix: close FES package publication review findings`).
- F1 red: `python3 -m unittest tests.test_core_build.CoreBuildTest.test_package_publication_rolls_back_after_a_selection_replacement_failure` — `Ran 1`; `FAILED`, with the new root/selection bytes visible after the injected post-replacement failure. The sealed-staging regression also reproduced the pre-fix `PermissionError` when run alongside it.
- F1 green: `python3 -m unittest tests.test_core_build.CoreBuildTest.test_package_publication_rolls_back_after_a_selection_replacement_failure tests.test_core_build.CoreBuildTest.test_package_publication_cleans_sealed_interrupted_staging` — `Ran 2`; `OK`. `python3 -m unittest tests.test_core_build` — `Ran 44`; `OK`.
- F2 red: `python3 -m unittest tests.test_media.MediaTests.test_singular_published_package_preserves_legacy_shape` — `Ran 1`; `FAILED` because the empty result was `()` instead of `None`.
- F2 green: the compatibility test passed in the focused parent/media run below; singular lookup now returns `None` or the sole package dictionary and rejects plural use.
- F3 red: `python3 -m unittest tests.test_media.MediaTests.test_package_only_media_reuses_the_complete_ordered_package_set` — `Ran 1`; `FAILED` because the base receipt allowed reordered package inputs.
- F3 green: the same test — `Ran 1`; `OK` after binding the fixture receipt to `image_fingerprint` and checking reordered inputs; `python3 -m unittest tests.test_media` — `Ran 62`; `OK`.
- Focused parent/media suite: `python3 -m unittest tests.test_core_build tests.test_media` — `Ran 106`; `OK`.
- Full Python suite: `python3 -m unittest discover -s tests` — `Ran 282`; `OK (skipped=39)`.
- Consistency: `make check` — `consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match`.
- Syntax: `python3 -m py_compile scripts/*.py` — exit 0.
- Whitespace: `git diff --check` — exit 0.
- F1 retains symlink/non-regular destination rejection, stages and verifies the complete set before commit, keeps a prior generation for rollback, and cleans sealed current/legacy staging residue. F2 preserves the singular public shape. F3 binds the single- and multi-package media fixtures to the package-derived image receipt and rejects mutation/reordering.
- Scope boundary: no Quartus, cold full image build, image shell suite, hardware test, push, PR, or merge was performed.

## Task 1 F1 repair — completed

- Repair commit: `6752660051752dc4a9ab4e6397eb4d77aa0bb92e` (`fix: make FES package backup recovery fail closed`).
- F1 now copies the live package destinations into a recursively validated,
  sealed and fsynced `.package-generation.previous` before publishing a new
  set; `.package-generation.complete` records the closed directory/file set
  and hashes only after the copy validates.
- Restore prepares and validates `.package-generation.restore` before clearing
  live destinations, validates the restored set afterward, preserves the
  recovery backup when rollback fails, and re-raises the original publish
  failure. Unmarked or malformed backups are never used to clear current
  output. Existing symlink/non-regular protections and legacy staging cleanup
  remain in place.
- TDD red: the two new regressions failed against the pre-repair behavior —
  malformed-backup recovery erased the live set, and an injected rollback
  failure masked the original publish error while deleting the backup.
- TDD green: the two new regressions — `Ran 2`; `OK`.
- Focused core/media suite: `python3 -m unittest tests.test_core_build tests.test_media` — `Ran 108`; `OK`.
- Full Python suite: `python3 -m unittest discover -s tests` — `Ran 284`; `OK (skipped=39)`.
- Consistency: `make check` — package YAML valid; 14 generated consumers, 11
  fixture copies and 4 copied source pins match.
- Syntax/whitespace: `python3 -m py_compile scripts/*.py` and `git diff --check` — exit 0.
- Scope boundary: no Quartus, cold image build, image shell suite, hardware
  test, parent pin/shared-contract change, push, PR, merge, or Task 2 was run.
