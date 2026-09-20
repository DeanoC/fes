# Coleco kit controls and recovery acceptance — 2026-09-16

Result: PASS for the defined Coleco controller diagnostic and native Stop
recovery slice. This is diagnostic acceptance for a dirty FogCast candidate,
not persistent-image, reproducibility, playable-game, or audio acceptance.

## Identity and boundary

- FogCast worktree: branch `feat/coleco-kit-controls`, base `b5a1eb6`, with
  uncommitted controls, coordinator, core-media, and documentation changes.
- The physical checks used host identity `b5a1eb6+diff` and the existing
  `fes.coleco` package (`281307c05117a10faa1f8fa7777fd3f45d506b943a2b2868973643303f7ecde1`).
- The recovery check used a diagnostic `mister-agent-idlestop` binary
  temporarily bind-mounted over `/usr/sbin/mister-agent`; the persistent
  `linux.img` and installed image were not updated. No image bake is claimed.
- Evidence is retained outside Git in
  `../ledger/coleco-kit-controls/` and `../ledger/idle-error-stop/`.

## Controls

The exact `fes.coleco` package with `fes.keyboard` 1.0 maps D-pad and left-stick
Up/Right/Down/Left to P1 keyboard bits 0..3, A to Fire1 bit 4, and B to Fire2
bit 10. The 16-state HIL sequence passed its positive controller-panel oracle
and rejected each deliberately wrong state: neutral, four directions, Fire1,
Fire2, overlap, release, four axis directions/neutral, held detach, and
relaunch-neutral (`evidence/01` through `evidence/16`).

The operator also confirmed physical PASS for all four directions, A, B,
release, Select+Start Stop, and relaunch. This validates the controller
diagnostic path; it makes no playable-retail-game or audio/HDMI-audio claim.

## Stop recovery

The idle-error-stop evidence first reproduced a native Pong runtime failure and
retained `MISTER_UNAVAILABLE` while idle. A normal Stop returned HTTP 200,
cleared `last_error`, released the lease, and restored ready/idle. The same
diagnostic run then launched the Coleco package through the Kit listener with
`state=active`, attached input, and the expected package identity; Stop returned
idle with `last_error=null`, ready connection, and a free lease. The controls
ledger separately records the Coleco launch, 16-state captures, Stop, and
relaunch evidence.

## Software verification

- `go test ./...`: PASS.
- `go test -race ./host ./internal/agent ./internal/hostapi ./internal/coremedia -count=1`: PASS.
- `git diff --check`: PASS.

No files were staged or committed by this worker; main owns integration and
image baking.
