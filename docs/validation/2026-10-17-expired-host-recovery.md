# Expired-host ownership errors and input reconnect

The production host from `dd4d4ba2` passed the
[two-workstation diagnostic](2026-10-17-expired-host-recovery/result.json)
in 429.910 seconds. Powerboat's resumed session read returned HTTP 409
`KIT_LEASE_BUSY`, its stale Stop returned HTTP 409 `SESSION_CHANGED`, and
20 competing launches plus 20 input requests returned HTTP 403
`KIT_LEASE_DENIED`. Chip's exact session, flight, lease generation and input
identity remained stable around the stale requests and throughout its 90-second
held interval. These responses replace the generic unavailable errors that
stopped the earlier probes. Refusals are host/API evidence; this does not claim
that every refused mutation reached target admission.

## Change and observed problem

Local lease loss was a plain error that could become `MISTER_UNAVAILABLE` or
`TARGET_UNAVAILABLE` during host normalization. The real input starter, remote
input controller and input reconciliation also erased explicit lease denial or
busy errors. They now preserve those classifications while keeping unknown
transport, parsing and bridge errors sanitized and unavailable. The existing
HTTP 403/409 codes are reused; no new wire schema, kit interface or ABI is added.

Mapping alone was insufficient. An
[instrumented diagnostic](2026-10-17-expired-host-recovery/diagnostic-summary.json)
recorded 241 attempts returning typed lease denial while the resumed input
reconnect loop retained its mutex. Local input reset took 2.885537761 seconds,
followed by a generic unavailable session response. This directly establishes
futile ownership retries and input-lock contention. The exact discovery error
branch was not logged, so the trace alone does not prove which lookup exhausted
its deadline.

Reconnect now returns immediately on explicit lease denial or busy, allowing
ownership reconciliation to obtain the input lock. Other stream failures retain
the existing reconnect grace. The heartbeat records `lease_lost` for that case.
A focused regression exercises the real reconnect method: against source
`c0911a94`, both denial and busy made 20 dial attempts before a 200 ms test-context
deadline; the fixed code returns the ownership error after one attempt. This is
not a change to claim, renewal, expiry, takeover or endpoint-adoption policy.
The existing read-only endpoint reconciliation still permits a subsequent
explicit claim and never automatically retries a hardware mutation.

## Exact artifacts and kit

Powerboat (`192.168.10.202`) ran source
`dd4d4ba2a0e929bd3a948f77a2dea79ff9f6da8f`.
The authoritative source identity is the full `hosts[0].source` and observed
`host_revision` in the result receipt; the host binary SHA-256 is
`55f51a56f23bbb8887ac99725bf0f12c01aff3df69c3f954d11cf1054b6b4c96`.
Chip (`chip-30x0-ai-dev`, kit-route source `192.168.10.145`) ran source
`521ca23e45e09371597cbd0da74c50703f3b3bba`, binary SHA-256
`11eafc0e103ae15e7d3bef231d2747e22fe21bde0d58118b4251d8854f446da1`.
Both used separate private homes, catalogs, credentials and listeners. Chip
reached the kit directly; SSH forwarded only control of its private loopback API.

The designated kit A (`192.168.10.84`, target
`73dc9f5f-1a12-4a95-a820-a9b4e600769a`) kept bundle
`0b6b7d8138f2147bc5f3a7b18a3117209365994550aa530b52d2bef819b5602f`
selected and active at software generation 15. Its base image remained
`ed721b84074b039daf66bad2aa24276b88dd8cd713b5cfcdb2c2f25489be4ad2`,
boot ID `14c05a47-1615-43f0-a084-a1dc8948c119`. There was no kit software update,
overlay, restore, service restart, OS replacement or reboot. No paired kit
release was necessary for the host change.

Powerboat first held Pong for 120 seconds with stable session/input identities
and acknowledged injected input. Only its exact private process was paused.
The kit expired its grant and reached free/native idle in
[86.651 seconds](2026-10-17-expired-host-recovery/expiry.json). Chip acquired a
different lease, survived Powerboat's stale operations and ran for 90 seconds.
Its exact owned Stop completed. A fresh private Powerboat process then claimed
a third lease, ran for 30 seconds, and stopped. This last step validates
fresh-process reacquisition, not automatic recovery of a whole host process.
The real target-client HTTP regression separately validates same-client explicit
reacquisition after read-only ownership reconciliation.

Fresh HDMI stills show Pong during
[Powerboat's held session](2026-10-17-expired-host-recovery/A-held-game.png),
[Chip after stale-host requests](2026-10-17-expired-host-recovery/B-after-A-reconnect.png)
and [fresh Powerboat](2026-10-17-expired-host-recovery/A-fresh-game.png).
The result brackets each game capture with exact ownership/input observations
and records monotonic capture times and file digests. All four retained images
were visually inspected. The [final menu](2026-10-17-expired-host-recovery/final-menu.png)
is populated Installed cores. [Final state](2026-10-17-expired-host-recovery/final-state.json)
is ready, idle and free on the same bundle/base/boot, with no pending software
operation or fault. Private hosts and credential files were removed; Chip's
remote temporary root was removed. The operator-requested persistent SSH
identity/configuration on Powerboat remains outside diagnostic cleanup.

All [six captured installed-core file hashes](2026-10-17-expired-host-recovery/core-persistence.json)
and paths match before and after the probes. This establishes persistence of
those archive/member/selection/lock-file bytes, not every user-data file,
permission or other core's execution. This is a hardware diagnostic using
software-injected input acknowledgments and frame progress. It does not prove
physical controller buttons or continuous HDMI behavior; physical acceptance
remains pending operator availability.

## Earlier probes and software checks

The first two production probes are retained separately:
[candidate 1](2026-10-17-expired-host-recovery/candidate-1-failure.json), source
`1220eec7`, and [candidate 2](2026-10-17-expired-host-recovery/candidate-2-failure.json),
source `c0911a94`. Both stopped at a resumed read's generic 503; exact Chip-session
cleanup succeeded and neither retained private state. Neither is a passing
hardware run. The separate
[instrumented probe](2026-10-17-expired-host-recovery/instrumented-failure.json)
used source `c0911a94` plus five logging-only Go overlays. Its binary/helper and
overlay file digests are bound by the receipts. The shortened one-second first
hold was diagnostic only; the accepted production run retains the full durations.
No logging overlay is included in the production candidate.

Focused Go race checks for lease, renewal, takeover, expiry, expected Stop,
reconciliation, bridge, stream and reconnect paths passed on `dd4d4ba2`:
`host` 1.114 s, `targetclient` 2.676 s, `fogcast` 5.135 s, `hostapi` 4.903 s.
The Linux amd64 host build and ARMv7 cross-build passed. Parent `make check`
passed (18 consumers, 37 fixture copies). The final `make test-changed` passed with umask `022`: 16 affected-planner tests,
26 test-driver tests, 941 parent tests (1123.434 seconds, including extended video
evidence), consistency/whitespace checks, host functional and race lanes, and
host UI/browser checks. The [software receipt](2026-10-17-expired-host-recovery/software-checks.json)
records the selected lane results. The earlier affected-suite run also passed;
readiness is based on this final run including the reconnect change.

The first affected-suite attempt used the shell's umask `0002` and hit parent
permission-fixture failures. Two representative failures reproduced on unchanged
source and passed with umask `022`. That superseded run was interrupted using
exact owned process identity checks. Subsequent affected runs use umask `022`;
no permission-policy or fixture workaround is shipped.

No raw private logs, configurations, credentials, host binaries, bundle archives
or SSH private keys are included in this evidence.
