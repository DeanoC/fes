# Home frame reuse — ARM comparison and kit checks

This change reuses the presented Home drawing while its list, focus, status,
title and dimensions are unchanged. Completed redraws use the existing paint
semantics. No production watchdog deadline or overrun count changed.

## Source and artifacts

- Base/control: FES `99f3859f09f4659f0f1345b945a5d49c8f2bde74`.
- Candidate implementation: `e7c60deee6af2d42d73ad3692911b898a9ece058`.
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

## Exact-artifact empty-Home navigation and recovery

The candidate bundle above was installed and activated through the software API.
Keyboard route focus, Settings open/return and Rooms open/return completed;
HDMI captures were retrieved and visually checked. Focus highlights, hero title
and artwork changed correctly; both return paths restored Home. No settings
were changed. Pointer/compact behavior was covered by focused host tests, not
physical pointer acceptance.

After the same running bundle, image and boot were reverified, one SIGKILL was
issued to each exact managed child PID. No fault was repeated. Full observations
are in `recovery-results.jsonl`.

| Fault | Recovery observed | Result |
| --- | --- | --- |
| Launcher | Healthy at 5.20 s observation | Only launcher PID changed; agent/runtime stayed up |
| Agent | Still recovering at 31.07 s, healthy at 36.57 s | All three child PIDs changed |
| Runtime | Still recovering at 30.90 s, healthy at 36.73 s | All three child PIDs changed |

After each fault, the same candidate bundle and boot were retained, local control
was idle, the API lease was free, and no new room error appeared. All three
post-recovery HDMI captures showed healthy empty Home. These are observation
times including probe overhead, not precise restart latencies or a comparison
against the earlier campaign's differently spaced probes.

The network outage after navigation capture delayed evidence retrieval and
recovery; it did not restart the kit or repeat a fault. An earlier claim attempt
also returned HTTP 409 from an outside `fogcast@powerboat` lease; it was left
alone, and testing started only after it became free. The kit's status-file
claim remained held until final restore was verified.

## Restore and evidence boundary

The owned keyboard helper PID 30952 was verified against its exact executable
path and terminated. Baseline software
`35d584898755b4c84b68cccf2e08838b4a371cbe550b5af457481f3d5e776f9b`
was explicitly activated and verified selected=active at generation 60, no
pending operation or service fault. Image and boot above were unchanged;
launcher configuration SHA256 remained
`5d4ee551f9aa36be1e1970bd6f6f6a90932d5f9013e82e2dc20c0299f5814168`,
and idle RBF remained
`64f1f16daedba4ed19179bd5d7c6f3eb3058a688d15441c8c8d89e113060f182`.
Kit B's `usb-storage.quirks=152d:0583:i` was preserved. Idle local control,
free API lease and baseline Home HDMI were verified before the status-file
claim was released.

This is exact-candidate-bundle hardware smoke for empty Home navigation and
idle managed-child recovery, plus a separate eight-row physical-ARM room-engine
diagnostic. Eight-entry full-launcher acceptance, physical controller/pointer
input and gameplay were not exercised; #761 remains open. No module/shared
contracts, shared-host services, OS/boot configuration or core packages changed.

Raw scripts, software/lease identities, supervisor logs and HDMI frames remain
on Powerboat at `/home/deano/tmp/codex-home-reuse-20261024/`. The tested source
remains implementation `e7c60dee`; subsequent commits only retain this evidence.
Copied diagnostics/input files are inert under kit `/tmp`; no owned diagnostic
or input-helper process remains running.
