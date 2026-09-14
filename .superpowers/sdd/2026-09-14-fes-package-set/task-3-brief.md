# Task 3 brief: select the complete FES package set and update the contract docs

## Scope

Implement Task 3 from `docs/superpowers/plans/2026-09-14-fes-package-set.md` on
`feat/fes-package-set`, after the independent Task 2 repair review passed.

The default `native-integration-dev` profile must select the ordered package
set `fes.pong`, `fes.zx81`, `fes.coleco`. The documentation and parent tests
must describe the package-only image as a closed set, the shared HIP/nextpnr
producer/cache route as the standard FES path, Quartus as an explicit
historical/oracle/bring-up route only, and format-1 profiles as isolated
legacy lanes.

## Files in scope

- `profiles/native-integration-dev.toml`
- `README.md`
- `docs/core-packages.md`
- `docs/getting-started.md`
- `docs/development.md`
- `tests/test_core_build.py`
- `tests/test_image_assembly.py`
- this brief and a concise Task 3 evidence report under this SDD directory

Do not modify the parent checkout, component source, prior Task 1/2
implementation, or unrelated historical validation records. Preserve all
existing untracked SDD artifacts.

## Required workflow

1. Read `AGENTS.md`, the design, and the implementation plan.
2. Add focused assertions first and run the focused tests to capture the
   expected red result. Tests should cover the ordered profile IDs and stale
   Pong-only documentation/contract language, including the image assembly
   contract where appropriate.
3. Make the smallest profile/documentation/test changes that turn the focused
   tests green. Keep historical format-1 profile behavior and wording clear;
   do not claim Quartus, QEMU, or hardware work was run.
4. Run the focused tests and `git diff --check`, then write the evidence report
   with red/green commands and results, changed files, and explicit evidence
   boundaries.
5. Commit only this Task 3 slice with message:
   `docs: make the FES package set explicit`

The report must state that the Task 2 independent review was SPEC PASS / QUALITY
PASS and that Task 3 does not itself prove a cold build or hardware acceptance.
