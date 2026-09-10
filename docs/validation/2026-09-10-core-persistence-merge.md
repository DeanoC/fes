# Persistence merge reconciliation — 2026-09-10

This follow-up reconciles the published persistence branches with current
component main branches. It preserves the previous commits through merge
commits, without rebasing or force-pushing.

The [September 9 hardware acceptance](2026-09-09-core-persistence.md) continues
to describe its exact source revisions and image. It does not automatically
cover the combined revisions recorded here. This follow-up does not deploy or
change the kit.

## Resolution

- mister-packages: main's ABI squash has exactly the same tree as the feature's
  ABI ancestor. Retaining the persistence additions produces an unchanged tree
  with corrected merge ancestry. Result: `98d9874d733a49d1dc13c4cd4f37a58a5d84b6f3`.
- Runtime: `2bfff81a038e7bb33efef73e4d935557d1a2943f` merges main's
  `f8a684cae177d199cc5e2a165160f04605145a1a`. The earlier ABI content was
  identical across the squash. The resolution preserves persistence and newer
  FIFO/FPGA diagnostics, combines archive ownership rules, and removes a
  duplicated FPGA initialization introduced by the ancestry merge. Persistence
  mutation RPCs emit diagnostic completion events. The inherited timestamp
  buffer is enlarged to satisfy GCC14's truncation check without changing format.
- FogCast: `cd70be1167b8e99259654aaf6bf69093855ac62f` merges current main,
  preserving its UI, offline-cache and diagnostic changes. Library load and
  protocol-v2 Stop retain diagnostic dispatch hooks. The native runtime lock and
  its fixture select the merged runtime above.
- FogCast tip later advanced to `cd70be1167b8e99259654aaf6bf69093855ac62f` (SuperGrok High fix: protocol-2 Stop must not be captured by retired staging). FES selects that tip.

FES selects these three merged commits. Its misteross selection remains the
  previously tested `4a8b8635cf338b22648e9e94272243b1fce42a79`; that PR did not
  have a merge conflict.

## Checks

- mister-packages full `make test` and `make vet` passed.
- Runtime: full native tests, 31 daemon tests, archive/history checks and the
  pinned ARM10.2 build passed. Combined diagnostic/persistence regressions were
  observed failing before the missing events were connected, then passing.
- FogCast: full Go tests, vet, 13 affected race packages, UI tests, ARMv7 agent
  and launcher builds, and native-runtime lock fixture checks passed. The UI
  suite passed 295 JavaScript tests and two fixture tests; one Chrome integration
  check was skipped because Chrome is unavailable.
- Independent review verified preservation of current main's UI/offline files,
  persistence recovery ownership and production forwarding, and reran focused
  FogCast regressions plus the runtime's 31 daemon tests.
- Parent: 192 tests run, 36 skipped, no failures. Final selected-source consistency
  checks cover 12 generated files, nine fixture copies and four copied source
  pins. The matched host CLI/API build passed.

These are combined-source and ARM build checks, not a fresh two-pass rootfs or
hardware acceptance. The previously accepted image remains on the kit. A future
release of the combined image requires its own image verification and acceptance.
