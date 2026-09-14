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
