# Multi-host kit software diagnostic

Source `d0ca0f3e8f49f0dbef1af32acd21b728e09a4d09` built bundle
`67afde612b5d12cd18aa68485e81b524a56c0b55fc7c06b05dd81bf8906de080`.
The [corrected two-host diagnostic](2026-10-16-multi-host-kit/multi-host-result.json)
passed. The local API check and six-file core-byte comparison also passed.
[Identities and stage status](2026-10-16-multi-host-kit/evidence.json) and the
[bundle receipt](2026-10-16-multi-host-kit/bundle-receipt.json) bind the candidate
archive, retained build base and host executable separately.

Two independent private host processes on one physical workstation shared one
designated kit. While A owned an active Pong session, B's launch and input requests
were both refused by the host with HTTP 403 `KIT_LEASE_DENIED`. This establishes
host refusal; it does not claim those requests reached target admission. The
50,681,856-byte candidate archive was installed while A stayed active. Across
13 samples over 65 seconds with activation queued, A's exact session, flight,
input-session ID and lease generation remained unchanged and its injected input
events continued to be acknowledged.

A's Stop released ownership. Selection then changed from `eaff152f…`, generation
11, to `67afde61…`, generation 12, with ten seconds of stable acceptance on the
same boot `14c05a47-1615-43f0-a084-a1dc8948c119` and running image
`ed721b84074b039daf66bad2aa24276b88dd8cd713b5cfcdb2c2f25489be4ad2`. Both hosts'
health views converged to the new selection. B then completed its own successful
launch, two acknowledged input events and Stop. The separate
[A](2026-10-16-multi-host-kit/host-a-lifecycle.json) and
[B](2026-10-16-multi-host-kit/host-b-lifecycle.json) receipts bind each session
and its active-bundle provenance; A ran the bundle from `d1a38ecf`, B the
candidate from `d0ca0f3e`.
The helper shut down both private host processes and removed their private
credential-bearing configuration files. No automatic software restoration occurred.

The compatible host was built from `d1a38ecf…`; matching source commits are not
a compatibility requirement. The candidate was built against retained image
`32f16cd2…`; the target accepted the bundle on the actual running image
`ed721b84…` under the shared base contract recorded in the bundle receipt. `make check` and focused Go tests passed (`localcores` 3.378 seconds,
`hostapi` 0.522 seconds). Focused foreign-lease/busy-kit launch tests also passed
under the race detector in 1.245 seconds. The initial build attempt lacked the cached builder;
recreating the pinned builder from its locked Debian/package inputs allowed the
userspace bundle build to complete without rebuilding the OS.

The [first private probe](2026-10-16-multi-host-kit/first-attempt.json) ended with
`RuntimeError`. Its source contained a competing-launch assertion that accepted
only HTTP 409, although the source contract also permits HTTP 403
`KIT_LEASE_DENIED`. The actual failing check and HTTP status were not retained,
so neither that assertion as the cause nor an observed HTTP 403 is established.
This is not classified as a product regression. Its public result contains no
recorded observations or input events. The owner then checked the exact host
session, flight and kit lease before stopping it; no software queue or selection
change occurred (the previous bundle remained at generation 11). The corrected
diagnostic retained bounded HTTP status and symbolic error-code evidence and
passed as described above.

A [read-only HDMI capture](2026-10-16-multi-host-kit/queued-game.png) shows Pong
while the corrected run has the candidate installed and its exact request queued.
The [capture guards](2026-10-16-multi-host-kit/queued-game-observation.json)
retain Host A's exact session/flight/input identity and lease generation before
and after the capture; the prior bundle remains active at generation 11 while
the input-frame counter advances from 222 to 225. This shows the game during the queued update; it does not establish a physical
button path or continuous HDMI recording.

A separate [operator-exclusive kit-local API probe](2026-10-16-multi-host-kit/local-api-receipt.json)
confirmed one Launch and one Stop in 6,870 ms on the new bundle at generation 12,
with the same boot and base. This was a single local client, not physical buttons.
Local Stop has no atomic expected-generation argument: observing its lease
generation before Stop does not exclude a concurrent replacement local session.
This check therefore does not establish concurrent-local-Stop ownership safety.
The temporary ARM helper was hash-checked and
[removed under the next owned lease](2026-10-16-multi-host-kit/local-api-cleanup.json).

The [six captured installed-core file hashes](2026-10-16-multi-host-kit/core-persistence.json)
match exactly before and after both host sessions, software activation and the
local API probe. This establishes persistence of those archive/member/selection
and lock-file bytes, not Coleco or other core execution.

The [final launcher capture](2026-10-16-multi-host-kit/final-menu.png) shows the
populated FogCast menu under a brief owned read-only snapshot lease, which was
then released. The subsequent [final state](2026-10-16-multi-host-kit/final-ready.json)
is ready, idle and free at generation 12 with the candidate selected/active, the
same boot/base, and no pending, unreleased or fault state. No private host
processes/configurations or temporary local helper remained.

This run covers two host processes on one workstation, not two physical PCs.
Input evidence is software-injected acknowledgment and frame progress; physical
controller buttons and continuous HDMI behavior were not tested.
No raw health dump, software journal, private configuration, credential, operator
command, host binary or bundle archive is included here.
