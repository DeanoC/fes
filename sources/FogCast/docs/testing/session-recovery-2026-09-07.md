# Native session recovery diagnostics — 2026-09-07

These are development diagnostics on the designated MiSTer Pi, not clean-image
release acceptance. The changed component is FogCast's native adapter and target
coordinator. No UI, FPGA, Linux, runtime source or parent pins changed.

## Source and artifacts

- FogCast worktree: `out/dev/session-recovery/FogCast` in FES;
  branch `feat/session-recovery`, base `ba9cd93`, uncommitted recovery diff.
- Target ID: `73dc9f5f-1a12-4a95-a820-a9b4e600769a`.
- Boot ID: `aa9f322c-d7b9-453e-b8ad-fd40531c9e39`.
- Final diagnostic ARMv7 agent SHA-256:
  `da858a222a11bd3cc44470a806ef05f96db3c7b4ffe20ca928f1a863beef6b4b`.
- Existing runtime: `git-bdf56ab`, SHA-256
  `15c1a9044f95b38d884162339ea601b79db2d2fa62866b908d4cbb13f1fcea14`.
- Agent was statically cross-built from `cmd/mister-agent` and temporarily
  bind-mounted over `/usr/sbin/mister-agent`. The read-only boot image was not
  changed. A reboot restores the image's original agent.
- Raw diagnostics and injector source are local FES artifacts in
  `out/dev/session-recovery/evidence`: `hardware-results.json`,
  `restart-results.json`, `save-results.json`, `runtime-proxy.go`,
  `validate-final.py`, `validate-restart.py`, `validate-saves.py`, and
  `go-race-tests.txt`. They contain no bearer or lease tokens.

The kit clock reports 1970; the report date is the development host's UTC date.

## Verified recovery behavior

| Fault | Observation |
| --- | --- |
| Native launch rejected | A one-shot proxy substitutes a non-profile RBF path. Runtime rejects it as `invalid_request`; agent returns HTTP 400, publishes idle with the original error, and permits Pong afterward. This tests validation rejection, not a mid-programming failure. |
| Lost successful Stop reply | Proxy forwards Stop to the real runtime, receives clean idle, then drops the response. Agent observes idle and returns success. Another Pong launch succeeds. Proxy records one Stop, with no replay. |
| Agent killed during Pong | Supervisor restarts the agent; existing startup cleanup returns idle. The stale token receives HTTP 403 `KIT_LEASE_REQUIRED`; a fresh claim launches and stops Pong. This ran on agent SHA `5657a69983fbd7519ed37518101ff5d368f8c7cd684c0161220a6e3c04f745fa`, before the final development-admission gate change. |
| Owner abandons renewal during Pong | Existing lease expiry cleans up to free/ready; the old token receives HTTP 403 `KIT_LEASE_REQUIRED`. No takeover is used. |
| Durable content cleanup fails | Software regression keeps native failed/unready and rejects both game and development launches. Explicit Stop succeeds after the content store recovers. |

The crash-test supervisor was initially started from SSH without detached output;
its inherited closed pipe interrupted the diagnostic restart. Restarting the
existing init script with `nohup` and redirected output corrected the test setup.
No production supervisor change was required.

## SNES save failure and retry

A unique diagnostic game ID used the existing cached 512 KiB SNES ROM with digest
`0838e531fe22c077528febe14cb3ff7c492f1f5fa8de354192bdff7137c27f5b`.
A small writable tmpfs was mounted over only that ID's save directory before
launch. Remounting that same filesystem read-only while the game ran produced
HTTP 500 and the retryable SNES save error on Stop. The coordinator stayed failed
and unready, and the lease remained held with the same generation. Restoring
write access permitted Stop to complete. Relaunch and another Stop preserved the
2,048-byte save digest
`d0ff1b294b5288d1ae1421eadf5b2d38a8752b76d472ff30bed9028e25b1c5b8`.
The temporary save mount was removed after clean Stop. Existing user saves were
not modified. This proves the native save-write error/retry path and file reload,
not FAT cold-boot durability or visible in-game progress restoration.

An initial fault injection mounted a different read-only filesystem after launch;
that did not fail Stop because the runtime retains an open directory descriptor.
The corrected injection remounted the filesystem opened before launch read-only.
This distinction matters when reproducing the test.

## Final kit state

The kit is idle/ready, lease free, with the launcher running. The temporary
runtime socket proxy and read-only save mount are removed. The final diagnostic
agent remains bind-mounted until reboot; no persistent image update is claimed.

## Software checks

`go test -race ./...` passed. The focused regressions were observed failing before
the respective fixes. Coverage includes malformed/active/retained-error Stop
observations, real Unix-socket response loss, cancellation/deadlines, content
cleanup, native relaunch, discovery identity/boot changes, input neutralization,
lease expiry/fencing and SNES save-error retry. Independent review found one
additional development-launch readiness bypass; it was fixed and covered before
rerunning the affected tests. FES `make check` and `git diff --check` passed.

## Acceptance boundary

These diagnostics do not establish physical held-controller unplug/replug,
in-game saved progress across a cold boot, host-process restart, target reboot,
or reproducibility of a newly assembled image. Those remain broader appliance
acceptance tasks. Review/publication of the component and FES pinning must precede
a clean assembled-image acceptance build. Existing input and save regression
tests are not substituted for those physical observations.
