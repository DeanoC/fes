# Home frame reuse — ARM comparison and pending kit checks

This change reuses the presented Home drawing while its list, focus, status,
title and dimensions are unchanged. Completed redraws use the existing paint
semantics. No production watchdog deadline or overrun count changed.

## Source and artifacts

- Base/control: FES `99f3859f09f4659f0f1345b945a5d49c8f2bde74`.
- Candidate implementation: `e7c60deee` (full revision in Git history).
- Control ARM executable SHA256: `e1a4ca959e851b350543b1bee2de7f3bd16a1204fa3b7453a0664bd1954afafb`.
- Candidate ARM executable SHA256: `acf39a890efdea1dde4e965ef195d00c25279fa8db191bcd09e7963ecc3674d1`.
- Candidate software bundle: `dbf1a2ae8127e454cc04af4402d61ed5ef03c8b37d8819b98bbea4d43d8585dc`.
- Kit B image: `4db7486452057d8a77479125c8ee8e9a27db4f378a9d9735900e90f6f3fe7a26`.
- Boot: `18569994-46c7-4392-8e55-96190f63361d`.

The old host-side base-image/shared-cache paths were gone. The running kit's
retained image was copied to the task directory and its full SHA256 verified.
The SDK was authenticated through the primary checkout's existing
`image/build/cache/target-image` cache, SDK key
`22d5e8277dfc785abc941fd0a3e678a1618a74ac892b43aca3aac53309b175fd`.
Neither the base OS nor core packages were rebuilt or changed.

## Tests

New reuse tests failed on the base at all three sizes (1280x720, 470x350,
300x350), including the unchanged Checking frame. Candidate reuse/focus/resize/
async-result tests passed 100 race-enabled runs; the full room race suite
passed. The completed-slow-paint test passed ten race-enabled runs. Existing
state-save, room navigation, default-budget and runaway tests remain selected.

The local affected run passed parent/generated checks and all other selected
Go suites but FAILED at `TestFallbackUsesRebootWhenSysrqPathMissing`: 17.36 s
against a 5 s bound. That unrelated test passed five isolated race-enabled
reruns. The browser lane, skipped after that failed aggregate, passed separately
with 78 tests. The failed aggregate is not a pass. `make check` passed on Linux:
21 generated consumers, 37 fixture copies. All selected GitHub CI checks passed
on implementation commit `e7c60dee` before this evidence-only update.

## Matched ARM comparison

Both arms ran once on Kit B, under one renewable target lease, with the same
active candidate software bundle and unchanged boot. No scheduling sampler ran.
The control executable was compiled from an archive of the exact PR base;
only the candidate includes the frame-reuse change. Both use the unchanged
`TestHomeWatchdogDiagnostic`: eight synthetic launchable rows, 1280x720,
production budgets, focus movement every ten frames, 100 frames per room,
100 room visits maximum, sleep of 1/60 s after each frame, stop at first error.

| Arm | Result | Retained observation |
| --- | --- | --- |
| Control | FAIL after 2,601 completed frames, 70.75 s test time | Cycle 26/frame 1: 48 ms hard deadline, 54.12 ms elapsed, 17.62 ms thread CPU, 3 voluntary/90 involuntary switches |
| Candidate | PASS: 10,000 frames, 100 visits, 190.46 s test time | Maximum draw 26.24796 ms; no room watchdog error |

Full outputs: `control.log`, `candidate.log`. The shorter failed control run
is not a speed benchmark. This demonstrates completion of the candidate's
bounded workload where the control failed; it does not guarantee tolerance of
arbitrary scheduling stalls or establish an all-workload failure rate.

These are Lua/display-list diagnostics on the physical ARM CPU alongside the
real empty Home launcher, not eight-entry full-launcher or gameplay acceptance.
The real kit's local core list was empty.

## Pending navigation, recovery and restore

The candidate was installed and activated through the software API. A finite
keyboard sequence ran through Home route focus, Settings open/return and Rooms
open/return. Its capture script completed, but the HDMI images have NOT been
retrieved/visually checked; no navigation pass is claimed yet.

SSH access then failed. Tailscale reported Powerboat, chip-30x0-ai-dev and
chip-30x0-pve offline at about 10:21 UTC; the local client reported Running and
no health error. Both Powerboat's mesh address and its known LAN address timed
out. No VPN settings or shared services were changed.

The candidate remains active, Kit B remains claimed by Codex royal-spruce in
its Powerboat status file, and the baseline is NOT yet restored. No candidate
SIGKILL recovery fault has been issued. Keep PR #1061 draft and #761 open until
these remaining observations/restoration are completed. An earlier claim
attempt also returned HTTP 409 from an outside `fogcast@powerboat` lease;
that lease was left alone and testing started only after it was free.

Resume on Powerboat after revalidating host, worktree, claim and API lease:

- Worktree: `/home/deano/fes/out/dev/perf-home-frame-reuse`, branch
  `perf-home-frame-reuse`, implementation `e7c60dee`.
- Evidence/scripts: `/home/deano/tmp/codex-home-reuse-20261024/`.
- Inspect `fr/` and `logs/navigation.log`. Do not rerun the ARM comparison.
- `recovery.py` is prepared for the explicitly authorized single SIGKILL per
  candidate child. It validates exact executable paths, stops on failed
  recovery, records PIDs/status/boot/lease and captures HDMI after each recovery.
  Revalidate that it has not already run before issuing any fault.
- Owned keyboard helper PID is recorded in `logs/keyboard.pid`; verify its
  executable is `/tmp/codex-home-reuse-hilkb` before terminating only that PID.
- Baseline software is
  `35d584898755b4c84b68cccf2e08838b4a371cbe550b5af457481f3d5e776f9b`.
  Use `~/tmp/k1044/bin/fs-B --action activate --candidate <baseline>` and inspect
  selection/status until selected=active baseline, no pending operation/fault.
- Confirm the same booted image, USB quirk `usb-storage.quirks=152d:0583:i`,
  idle local control, free API lease and healthy HDMI before releasing the
  status-file claim. A bundle activation is not an image restore.

No shared-host or boot edits were made. Inert copied diagnostics and input
helper files are retained under `/tmp`; the keyboard helper process remains
ours and must be stopped during cleanup. No module/shared contracts change.
