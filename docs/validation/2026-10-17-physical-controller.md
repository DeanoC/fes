# Installed-bundle physical controller acceptance

The operator confirmed **“all work”** after being asked to perform these steps
on designated kit A (`192.168.10.84`): select **FES Pong** on **Installed cores**,
press **A/Cross**, move the paddle with the physical controller, then hold
**Select+Start** for one second to return to **Installed cores**.
The [confirmation receipt](2026-10-17-physical-controller/operator-confirmation.json)
records the requested steps and exact reply. This closes the previously deferred
physical launch, movement and return check for this installed bundle.

The [before](2026-10-17-physical-controller/before.json) and
[after](2026-10-17-physical-controller/after.json) observations independently show
ready, native idle and a free execution lease. Both identify target
`73dc9f5f-1a12-4a95-a820-a9b4e600769a`, selected/active bundle
`0b6b7d8138f2147bc5f3a7b18a3117209365994550aa530b52d2bef819b5602f`,
software generation 15, base image
`ed721b84074b039daf66bad2aa24276b88dd8cd713b5cfcdb2c2f25489be4ad2`
and boot `fe3774a0-6fa0-4aaa-9bf1-5eaec17962d5`. No pending update, unreleased
operation, service fault or storage error is reported. This boot differs from
the earlier automated soaks; the selection remains the same. The execution
lease generation differs between these observations.

Evidence class is **operator-reported physical-controller acceptance for the
listed steps on the exact installed bundle**, with automated before/after
status checks. The agent did not observe the active game, controller packets or
continuous HDMI during this test. No earlier screenshot is reused as evidence
of this run. It does not qualify other cores, every button, another bundle,
automatic same-process host recovery, or a new appliance image.

The agent performed only read-only status requests, leaving local launcher
lease admission in control. It did not launch, inject input, Stop, update
software, restart services or reboot the kit. No production code, shared
contract or selected artifact changes accompany this record.
