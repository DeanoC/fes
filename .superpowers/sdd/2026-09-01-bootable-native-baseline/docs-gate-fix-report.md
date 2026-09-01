# Documentation gate correction report

**Date:** 2026-09-01

**Scope:** Documentation-only correction of the approved Milestone 2 design
and implementation plan. No product code, tests, images, hardware, or runtime
checkout contents were changed.

## Changes

1. Task 1 now names `src/native/linux/mmio.cpp`,
   `src/native/linux/fpga_manager.cpp`, and `src/native/linux/spi.cpp` in both
   the focused native-hardware test prerequisites and its compile/link command,
   with the fixed idle-path macro.
2. Replaced the fixed `all 98 named tests` wording with the complete named-test
   set plus recorded actual count. The count is documented as 98 only for the
   current `d6e7ec2db1049a0d6bd9edfd44a233ac174729f9` baseline.
3. Recorded the canonical runtime response rules: `starting` plus `none`
   accepts null identity or a complete non-empty retained pair; `starting` plus
   `game` requires that pair; and `starting` plus `development` requires null
   identity. Added the corresponding focused client-test contracts.
4. Kept public Milestone 2 stop idle-only. An already-idle stop confirms idle
   without mutation; a non-idle native state at startup is unavailable. The
   direct adapter `Stop` translation remains an isolated future-milestone
   interface test, without coordinator-driven active stop.
5. Replaced Task 7's untracked-image assumption with clean, fast-forwarded
   FogCast and runtime main checkouts; reproducible `prod`, `dev`, and
   `native-dev` rebuild/verification; locked-runtime-to-HEAD assertion; a
   repeated native-input verification; then image hashing and deployment.
6. Replaced the single HDMI frame with a five-second, five-frame timestamped
   capture, V4L2 device/mode report, per-frame inspection requirement, and
   recorded hashes for the report and all frames. Binary capture frames remain
   uncommitted.

## Commands and results

- `git diff --check` — passed; no whitespace errors.
- Diff placeholder-marker scan — passed; no reserved placeholder markers are
  present.
- Balanced-fence scan — passed: 8 fences in the design and 148 in the plan.
- Command/path consistency scan — passed: each required native Linux source is
  present twice; the idle-path macro is present; clean-main rebuild, runtime
  lock equality, repeated native-input verification, native image verification,
  five-frame capture, V4L2 mode report, and `legacy_dev_image` rollback
  references are all present; the brittle fixed baseline claim is absent.

## Files changed

- `docs/superpowers/specs/2026-09-01-bootable-native-baseline-design.md`
- `docs/superpowers/plans/2026-09-01-bootable-native-baseline.md`
- `.superpowers/sdd/2026-09-01-bootable-native-baseline/docs-gate-fix-report.md`

## Self-review

- Preserved the fixed runtime and idle-RBF pins, zero supported-system claims,
  legacy-image preservation, no-fallback policy, and no hardware mutation
  before Task 7's rebuilt-image gate.
- Confirmed the Task 7 image variables remain defined at each deployment site,
  including rollback through the rebuilt `legacy_dev_image`.
- Confirmed all added shell commands and paths are part of the planned future
  implementation, not actions performed by this documentation correction.

## Concerns

- No implementation, image build, runtime checkout mutation, or hardware
  acceptance was run; those remain intentionally gated behind the documented
  Task 7 preflight and outside this correction's scope.
