# Same-process host recovery after lease expiry

The [two-workstation hardware diagnostic](2026-10-17-same-process-host-recovery/result.json)
passed in 422.740 seconds. The same Powerboat host process lost
its lease, left Chip's replacement session intact, then explicitly launched and
stopped Pong again after Chip released the kit. No production code fix was
needed. The [kit-sharing instructions](../kit-sharing.md) remove the obsolete
requirement to restart a client after resolving ownership.

## Recovery sequence and identity

Powerboat held Pong for 120 seconds with lease renewal, acknowledged injected
input, increasing frame counts and stable input/session identities. Only that
exact private process was paused with SIGSTOP. The kit naturally expired its
lease and reached free/native idle in
[90.774 seconds](2026-10-17-same-process-host-recovery/expiry.json). Chip then
claimed a distinct lease and launched Pong.

Powerboat resumed in its original process. Its session read returned HTTP 409
`KIT_LEASE_BUSY`; its old generation-bound Stop returned HTTP 409
`SESSION_CHANGED`. Across both held intervals, 22 competing launches and 22
input requests returned HTTP 403 `KIT_LEASE_DENIED`. The current owner's exact
session, flight, lease and input identities were checked around these requests.
This establishes host refusals, not that every refused request reached target
admission. Chip remained stable for 90 seconds and completed its owned Stop.

Powerboat then used existing health/session observations to reconcile ownership.
The kit was explicitly checked free/native idle afterwards, before one new
Launch request; reconciliation did not automatically claim or launch. That
explicit launch acquired a new lease, flight, native generation and input stream.
It ran for 30 seconds with acknowledged input and frame progress, then completed
its owned Stop.

The original and recovered host use PID `2909724`, executable
`/tmp/fes-same-process-hardware/A/host-binary` and `/proc` start ticks `308450244`.
Those three fields were checked at initial startup, every owned A sample,
before reconciliation, and after recovered Stop. No private host stop/start
occurred between the first and recovered launches. The public host session ID
also stayed the same; its flight and native generation changed. This is actual
same-process explicit reacquisition, rather than the fresh-process check in the
[earlier diagnostic](2026-10-17-expired-host-recovery.md).

This validates recovery through a new explicit launch. It does not resume or
replay the expired game automatically, retry an unknown mutation outcome, or
exercise every browser/tenfoot UI transition after disconnection.

## Artifacts and unchanged kit

Powerboat (`192.168.10.202`) ran source `bdc9cf92cdaa4f96a66b53a6be0252cb83b21c41`,
observed revision `bdc9cf92`, host binary SHA-256
`21ada6e54385900d74cbea570bbff6758c9f7f07d2ffdd78f3f6d08a2533e6ed`. Chip (`chip-30x0-ai-dev`, previously verified
kit-route source `192.168.10.145`) ran source
`521ca23e45e09371597cbd0da74c50703f3b3bba`, binary SHA-256
`11eafc0e103ae15e7d3bef231d2747e22fe21bde0d58118b4251d8854f446da1`. Both used separate private homes, catalogs,
configurations and listeners. Chip reached the kit directly; SSH forwarded only
control of Chip's private loopback API. Matching repository revisions were not
required.

Kit A (`192.168.10.84`) remained on selected/active bundle
`0b6b7d8138f2147bc5f3a7b18a3117209365994550aa530b52d2bef819b5602f` at software generation 15, base image
`ed721b84074b039daf66bad2aa24276b88dd8cd713b5cfcdb2c2f25489be4ad2`, boot `fe3774a0-6fa0-4aaa-9bf1-5eaec17962d5`.
The run performed no kit software installation, activation, overlay, restore,
service restart or reboot. Runtime hardware lifecycle still went through the
leased target API. No shared host, autostart or live host catalog was changed.
There are no shared interface/ABI or selected-artifact changes in this PR.

Fresh HDMI stills show Pong during
[Powerboat's first session](2026-10-17-same-process-host-recovery/A-held-game.png),
[Chip after stale-host requests](2026-10-17-same-process-host-recovery/B-after-A-reconnect.png)
and [Powerboat's recovered session](2026-10-17-same-process-host-recovery/A-recovered-game.png).
Each game capture has exact before/after identity guards, monotonic capture times
and file digests in the receipt. All retained images were visually inspected.
The [final still](2026-10-17-same-process-host-recovery/final-menu.png) shows the
populated Installed cores menu. [Final status](2026-10-17-same-process-host-recovery/final-state.json)
is ready, idle and lease-free on the same bundle/base/boot, with no software
operation or fault. Private hosts/configurations and Chip's temporary remote
root were cleaned up; the persistent SSH identity/configuration remains installed.

The [six captured installed-core file hashes](2026-10-17-same-process-host-recovery/core-persistence.json)
and path set match before and after. This checks those archive/member/selection
and lock-file bytes, not all user data or execution of other installed cores.
Evidence class is **hardware diagnostic**, with software-injected input
acknowledgments and frame progress. Physical controller input and continuous HDMI
were not retested by this automated run.

## Host validation

The committed-source Linux amd64 host build passed. Existing focused race tests
passed: `targetclient` 1.045 s, `host` 1.029 s, `fogcast` 1.139 s and `hostapi`
1.115 s, covering expired-client reconciliation, immediate explicit reacquisition,
expected Stop, input reconnect refusal and real bridge/session reconciliation.
The driver passed syntax and independent safety review. `make check` passed
(18 generated consumers, 37 fixture copies). Documentation-only
`make test-changed` passed: 16 affected-planner tests, 26 test-driver tests and
both whitespace checks. No ARM/image build or kit
release update is needed for this documentation/evidence-only change.

No raw private logs, configurations, credentials, host binaries, bundle archives
or SSH private keys are included in this record.
