# DE25 HDMI presentation error after picture recovery

The 2026-11-11 switcher trial at integration
`536029ba52f097726cae2c47c4f51fe564344831` restored all six monitor pictures:
5/5/60-second operator-reported outages during ordinary ZX81, then the same at
idle MENU. Fresh captures corroborate the returns. The same active guest passed
BASIC 7 → edit/run 9 → reload/run 7 and normal owned Stop. These observations do
not prove byte-identical RAM or instrumented HPD-edge timings.

The final MENU outage retained global `io_failed / menu / DE25 HDMI is not
ready`. Its public display generation changed 9 → 10; a later completed
sequence4 and clear capture proved picture recovery, while the global error
remained. Zero underflows were observed. One earlier MENU recorder separately
failed an SSH banner handshake; that failure is retained. New owned multiplexed
SSH observation supported the remaining checks without increased deadlines.
The kit was released idle/free after all owned instrumentation was removed.

## Repair and evidence boundary

`Runtime::PresentMenuFrame` used ordinary launch-failure cleanup for an idle
presentation failure. Successful cleanup created a new MENU generation but
published the original error globally; subsequent successful frames never
cleared it. The repair gives that recovered presentation an explicit failure
scope. It preserves the failed request and log, retains the error in
`menu_display.error` through preparation, and clears it only after an actual
completed redraw. An existing independent lifecycle error stays global.
Unavailable or faulted MENU and failed cleanup retain global failure behavior.
No programming, allocation, retirement, clock or deadline predicate changes.

The new regression fails on the original source at the global-error assertion.
The full affected command (`scripts/test_changed.py --base
536029ba52f097726cae2c47c4f51fe564344831 --jobs 2`) passes parent
regressions, generated consumers, host/race/UI consumers and the final full
native runtime suite. The final full
AArch64 runtime suite, including independent-lifecycle and unhealthy-cleanup
refusal cases, passes under QEMU using the existing read-only Buildroot SDK.
An earlier static-linked full-suite executable aborted with `terminate called
without an active exception`; it remains a failed check. A trace-only dynamic
control and the final unmodified test entrypoint both pass against the SDK
sysroot. The static abort's cause is unproven.

The physical trial and its failures belong to source536. This reporting repair
has not been deployed or accepted on hardware. No new kernel or FPGA artifact
is required by this userspace-only change. #1642 remains open pending focused
review and changed-bundle native acceptance; #1298's approved scoped recovery
exception and #1568's default-off persona waiver are separate.

Retained physical receipt:
`hdmi-hotplug-9c/hdmi-switcher-physical-20261111/complete.json`, SHA256
`acae6e63eb43c69498d27cd1f8b17201016336bb4f8135a3b5dc32cd642e5cd4`.
Software proof directory on Powerboat:
`/data/fes-de25-main-integration-20261105/hdmi-error-fix-20261111/`.
