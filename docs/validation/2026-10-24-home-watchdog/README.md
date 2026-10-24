# Home watchdog investigation on Kit B

This is a hardware diagnostic for #761, not a fix or full-launcher acceptance.
Production callback budgets remain unchanged.

## Identities and setup

- FES base: `aab134d45a2ed9450dcc4188562c75f4c58fec18` (main after #1056).
- Kit B claimed in Powerboat `~/kit-status/kitB-STATUS.md` by Codex royal-spruce.
- Running/good OS: `4db7486452057d8a77479125c8ee8e9a27db4f378a9d9735900e90f6f3fe7a26`.
- Previous OS: `230eb8f5d5cc801bde3ab32b45adfe7a536e30c2cd659ce308d934614f12838d`.
- Boot: `18569994-46c7-4392-8e55-96190f63361d`.
- Baseline software: `35d584898755b4c84b68cccf2e08838b4a371cbe550b5af457481f3d5e776f9b` (generation 54).
- Current-main software built against that retained base/SDK and installed/activated through `fes-software`: `b1c3ee8b7d012e4a1acf18a7b482ec547ae01304d2aa5e529044d9e473fb26de` (generation 55).
- Idle RBF: `64f1f16daedba4ed19179bd5d7c6f3eb3058a688d15441c8c8d89e113060f182`.
- Launcher configuration: `5d4ee551f9aa36be1e1970bd6f6f6a90932d5f9013e82e2dc20c0299f5814168`.
- Kit B USB-storage quirk preserved. No OS/boot configuration, shared host or installed core changes.

HDMI showed a healthy empty Home before and after activation. The local cores
API returned `[]`; this kit cannot reproduce an eight-entry full launcher without
additional setup. No actual core launch, input or play acceptance is claimed.

## Eight-entry room diagnostic

The opt-in `TestHomeWatchdogDiagnostic` runs the embedded Home in the real room
engine on the kit's ARM CPU, at 1280x720 with `DefaultBudget()`: 12 ms soft draw,
48 ms hard draw, eight consecutive soft overruns before retirement. It uses eight
synthetic, launchable core rows with no images or host services, changes focus
every ten frames, and closes/reopens Home every 100 frames. This is a display-list
and Lua workload, not the actual tenfoot rendering/HDMI loop. Its additional Go
process runs alongside the real empty launcher, so contention differs from the
historical full-launcher incident.

Build from the recorded FES revision plus the diagnostic test in this PR:

```sh
cd sources/FogCast
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go test -c ./ui/rooms -o rooms-arm.test
# Only on an idle, claimed target with the ordinary renewable diagnostic lease:
FES_HOME_WATCHDOG_DIAGNOSTIC=1 ./rooms-arm.test \
  -test.run '^TestHomeWatchdogDiagnostic$' -test.v -test.timeout 5m
```

Test source SHA256: `dbdbf84c83863df0164bcf5ad769b51382fac846d915012fbb6071e5c4c86b44`.
Built ARM test SHA256: `7e439573c017d34ad7bc71bd9b63b6444de2732b767d35607becf6db30a80203`.
The first failure is retained in `fixture-first-failure.log`.

The first run failed at zero-based cycle 5/frame 78, after 578 completed frames.
The final eight draws took 15.3, 14.8, 13.0, 12.5, 30.5, 12.5, 15.9 and 13.7 ms.
Their thread CPU was 10.5–14.8 ms, with 4–21 involuntary switches per draw,
zero process-wide GC cycles and zero images loading. This reproduces soft-overrun
retirement on the target CPU. It does not reproduce the historical 48 ms hard
failure or establish its complete cause.

A one-second `/proc` sampler overlapped the first run. Its scheduling log is
retained on Powerboat. The confirmation without the sampler failed at zero-based cycle 71/frame 63
(after 7,163 completed frames, 195.14 seconds): `draw callback: script exceeded
48ms budget (elapsed 51.36ms; script run 51.32ms: thread cpu 13.41ms,
6 voluntary/63 involuntary switches)`. Its full log is `fixture-no-sampler.log`.
The script-run interval includes about 37.9 ms off-CPU. This reproduces the
hard-deadline error on ARM without the sampler, but does not identify the
competing work or distinguish every source of waiting. The fixture process,
Go runtime and background real launcher remain part of this workload.

The tests preserve the first failure rather than silently retrying it to green.

## Remaining work

Home currently reconstructs its static header, shelf and motif drawing every
frame; it does not use the existing `gfx.reuse`/`gfx.paint` path used by the
hardware room. The measured draw CPU is close to the soft budget. Reducing
unchanged redraw work is a concrete next experiment, with these budgets fixed.
It is not yet a demonstrated correction.

The managed-child SIGKILL recovery campaign was not run: automatic approval
review rejected the deliberate service disruption pending explicit approval.
The healthy post-activation HDMI frame does not substitute for fault recovery.
The eight-entry full launcher and its recovery sequence remain unverified;
the ARM fixture now reproduces both soft retirement and a hard-deadline error. Keep #761 open.

Raw build, activation, software status, lease, scheduler and HDMI artifacts:
Powerboat `/home/deano/tmp/codex-761-20261024/`. No bearer or lease tokens are
included in this record.

## Restore and verification

Baseline software was explicitly reactivated through the software API. At
software generation 56, selected and active both equal `35d58489…`, with no
pending operation or service fault. The boot ID and loop-backed OS remained
unchanged. Runtime/local control was idle, the target API lease was free, and
the baseline Home was visible on HDMI. The status-file claim was released with
these results. The inert diagnostic executable is retained under `/tmp` on the
kit; no diagnostic process remains running.

Focused host checks passed: runaway-script termination and the default-budget
assertions. The new target diagnostic skips by default as intended; the two
explicit ARM runs are recorded failures, not passing regression tests.
