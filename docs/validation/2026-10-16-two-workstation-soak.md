# Two-workstation lease expiry and stale Stop diagnostic

The [bounded hardware diagnostic](2026-10-16-two-workstation-soak/result.json)
passed in 429.566 seconds on kit A (`192.168.10.84`). Two physical PCs were
used: Powerboat (`192.168.10.202`) and Chip (`chip-30x0-ai-dev`, direct kit-route
source `192.168.10.145`). Each ran an isolated host with its own temporary home,
configuration and catalog. Chip reached the kit directly; an SSH tunnel carried
only control requests to Chip's private loopback host API.

Powerboat used host source `d1a38ecfd1f1f461d207e0b88ea152475d7ac2e3`
(binary SHA-256 `93e37aaa4ebd419dc7161c19ab4936af4ff2baf6fafb90c1a8f6b1b602d1fcc7`).
Chip used source `521ca23e45e09371597cbd0da74c50703f3b3bba`
(binary SHA-256 `11eafc0e103ae15e7d3bef231d2747e22fe21bde0d58118b4251d8854f446da1`).
The accepted kit bundle remained
`0b6b7d8138f2147bc5f3a7b18a3117209365994550aa530b52d2bef819b5602f`,
software generation 15, throughout. The base image remained
`ed721b84074b039daf66bad2aa24276b88dd8cd713b5cfcdb2c2f25489be4ad2`,
boot ID `14c05a47-1615-43f0-a084-a1dc8948c119`. No bundle installation,
activation, overlay, OS update, kit service restart or reboot was performed.
There are no production code or interface changes in this evidence change.

Powerboat held Pong for 120 seconds, with 11 sampled input pairs and stable
session, flight, lease generation, input session and resynchronization counter.
Chip's 11 competing launches and 11 input requests each returned HTTP 403
`KIT_LEASE_DENIED` while Powerboat owned the kit. This proves host refusal,
not that those requests reached target admission. Only Powerboat's exact private test process was then paused with SIGSTOP.
The kit expired and cleaned up its lease to free/native idle in
[91.251 seconds](2026-10-16-two-workstation-soak/expiry.json), without takeover.
Chip acquired a distinct lease and launched its own session.

After Powerboat resumed, its session read returned HTTP 503 `TARGET_UNAVAILABLE`.
One stale Stop, bound to Powerboat's original session/flight/package/native
generation, returned HTTP 500 `MISTER_UNAVAILABLE`. Chip's exact session,
lease and input identity were verified immediately before and after. Chip
remained stable for another 90 seconds (nine sampled input pairs), then its
owned Stop completed. A fresh private Powerboat process subsequently acquired
a third lease, ran for 30 seconds (three sampled pairs), and stopped normally.
This tests fresh-process reacquisition, not automatic same-process recovery.
The Powerboat host record labels that fresh process A2. Its final process
identity is pid 3399212, start ticks 305168362. The paused process remains
pid 3387954, start ticks 305135470, on the A-private-process-paused event.

The retained HDMI stills show Pong during
[Powerboat's held session](2026-10-16-two-workstation-soak/A-held-game.png)
and the [fresh Powerboat run](2026-10-16-two-workstation-soak/A-fresh-game.png).
Chip's HDMI capture is unavailable. The file previously stored as
`B-after-A-reconnect.png` is byte-identical to Powerboat's held-session
capture in [attempt 3](2026-10-16-two-workstation-soak/attempt-3.json)
(SHA-256 `d331129df750112899fedcd904469b0fe349173bb64b59f28eae10ccc41d674c`)
and is retained as
[that capture](2026-10-16-two-workstation-soak/attempt-3-A-held-game.png).
No Chip frame is present in this evidence or its git history. Powerboat's two
game captures keep the session guards and monotonic timestamps recorded in
the result receipt. All retained images were visually inspected. Software-injected
input acknowledgment and increasing frame counters do not prove physical
controller input or uninterrupted video. The
[final still](2026-10-16-two-workstation-soak/final-menu.png) shows the populated
Installed cores menu. [Final state](2026-10-16-two-workstation-soak/final-state.json)
is ready, native idle and lease-free on the same bundle/base/boot, with no pending
operation or software fault. Both private hosts stopped, their private credential
files were removed, and Chip's temporary remote root was removed. The explicitly
requested persistent SSH identity/configuration on Powerboat remains installed;
ordinary SSH to Chip was reverified after cleanup.

The [six installed-core file hashes](2026-10-16-two-workstation-soak/core-persistence.json)
and path set match exactly before and after all attempts. This covers the
captured archive/member/selection/lock-file bytes, not all user data, permissions
or execution of the other installed cores.

## Earlier attempts and remaining recovery case

Three earlier attempts are preserved separately and are not passing runs:

- [Attempt 1](2026-10-16-two-workstation-soak/attempt-1.json) reached expiry and
  Chip launch, then a success-only session-read gate reported resumed A unavailable.
  It did not retain the HTTP status, so it establishes neither a transport failure
  nor a product regression. Known-session cleanup succeeded.
- [Attempt 2](2026-10-16-two-workstation-soak/attempt-2.json) retained the resumed
  read's 503 and stale Stop's 500/code, then rejected the latter under its narrow
  whitelist. Its known-session cleanup succeeded. Source review confirms that
  the older host can map failed status/lease operations to these responses; the
  response alone does not identify the exact internal branch.
- [Attempt 3](2026-10-16-two-workstation-soak/attempt-3.json) verified Chip survived
  the stale Stop, then an expired Powerboat competing launch returned 503
  `TARGET_UNAVAILABLE`. That generic response is not proof of pre-dispatch
  refusal. The helper halted and retained private state rather than retrying the
  mutation. Its exact Chip Stop succeeded. Powerboat's held-session still from
  this attempt is retained as
  [attempt-3-A-held-game.png](2026-10-16-two-workstation-soak/attempt-3-A-held-game.png).
  A separate ownership-checked
  [cleanup](2026-10-16-two-workstation-soak/attempt-3-cleanup.json) verified the kit
  ready/free/idle, stopped only the recorded private processes, removed their
  credentials/remote root, and verified unchanged kit state afterwards.

The passing fourth attempt deliberately omits post-expiry competing launch/input
mutations. It does not turn the ambiguous 503 into a successful refusal test.
Automatic same-process recovery and predictable expired-host API errors remain
follow-up work. The source's endpoint-adoption path can reset a lost lease client;
this record makes no claim that a process must always restart after expiry.
Physical controller acceptance remains deferred until the operator is available.

## Host checks

The source-521 Chip host build passed. Focused race checks passed:
`go test -race ./fogcast -run 'Lease|Takeover|Renew|Expir' -timeout120s`
(3.009 seconds), and
`go test -race ./internal/hostapi -run 'TestExpectedStop|Test.*Lease' -timeout120s`
(1.083 seconds). Helper syntax checks and independent review of each helper
revision passed. Parent `make check` passed (18 generated consumers, 37 fixture
copies). Documentation-only `make test-changed` passed (16 affected-planner tests,
26 changed-test-driver tests and both whitespace checks).
No ARM rebuild or appliance rebuild was required for this evidence-only change;
the hardware diagnostic reused the previously accepted exact bundle.

No raw host logs, private configurations, bearer credentials, host executables,
bundle archives or SSH private keys are included in this evidence.
