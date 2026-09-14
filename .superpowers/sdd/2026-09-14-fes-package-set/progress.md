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

## Task 1 F1 repair 2 — completed

- Base: `1d033d60b5e50a5a2491c50319b64dc42d9b89da` on
  `feat/fes-package-set`; result commit: `3412dfb`.
- F1-1: complete generation validation now accepts only an empty generation or
  a closed package set with lowercase 64-hex package identities, exactly
  `manifest.toml` and `core.rbf` per package, and matching selection count.
  Recursive lstat and symlink/special-file protections remain enforced.
- F1-2: staged, backup, restore, marker, live payload files and containing
  directories are fsynced with explicit error propagation before the next
  durable publication boundary.
- F1-3: an unmarked backup is discarded only after the current live output is
  validated as a generic complete generation; malformed or unsafe current
  output preserves both paths and fails closed.
- TDD red: the three new regressions ran `Ran 3`; `FAILED (failures=2,
  errors=1)` against the pre-fix implementation.
- TDD green: the same three regressions ran `Ran 3`; `OK`. Existing recovery
  regressions and the full core suite also passed.
- Focused core/media suite: `python3 -m unittest tests.test_core_build
  tests.test_media` — `Ran 111`; `OK`.
- Full Python suite: `python3 -m unittest discover -s tests` — `Ran 287`;
  `OK (skipped=39)`.
- Consistency: `make check` — package YAML valid; 14 generated consumers, 11
  fixture copies and 4 copied source pins match.
- Syntax/whitespace: `python3 -m py_compile scripts/*.py` and
  `git diff --check` — exit 0.
- Scope boundary: no parent checkout, other worktree, image shell suite,
  Quartus, cold image build, hardware, push, PR, merge, or Task 2 work was
  performed.

## Task 2 — completed

- Implementation commits: 931fdd7 and ddcc3e5.
- FogCast now resolves the selected format-2 package through the generalized
  selector; the FES parent binds the selector to fes.pong, fes.zx81 and
  fes.coleco descriptors and preserves the exact package identity.
- Focused FogCast tests and native-package-only_test.sh passed.
- Independent review: SPEC PASS / QUALITY PASS; no P1, P2 or P3 findings.

## Task 3 — completed

- Implementation commits: 4a2269e, 5b656c8 and c09a6c8.
- The default profile and current guides describe the ordered closed
  Pong/ZX81/Coleco package-only set, HIP/nextpnr as the normal FES route, and
  Quartus as a check/oracle lane.
- Focused profile/image tests: Ran 55; OK.
- Independent review after the documentation repair: SPEC PASS / QUALITY
  PASS; no P1, P2 or P3 findings.

## Task 4 — verification in progress

- The first full Python run exposed a stale one-package media fixture after
  the default profile became a three-package set: FAILED (failures=6,
  errors=35, skipped=36). The root cause was traced to the fixture's
  one-record inputs.json, not production cache or publication logic.
- The fixture repair creates and fingerprints Pong/ZX81/Coleco while keeping
  explicit single- and two-package compatibility cases.
- Final checks so far: media 62 tests PASS; full Python suite 289 tests PASS
  with 36 skipped; make -C image test PASS; make check, Python syntax
  compilation and git diff --check PASS.
- Isolated resolver evidence recorded one miss and one exact hit for each of
  Pong, ZX81 and Coleco; all three report router=HIP, the shared toolchain
  cache root, miss_builds=1 and hit_builds=0.
- The validation record now includes the exact fixture producer revisions,
  synthetic package IDs, package stores, shared cache root, selection paths,
  manifest/payload hashes, and explicit per-lane not-run/not-generated fields
  for Yosys/nextpnr, Quartus, QEMU and image receipts.
- No cold release build, Quartus run, physical hardware test, push, PR or
  merge was performed.
