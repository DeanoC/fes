# DE25 native watchdog recovery

The bottom DE25, serial `TRWU8J8G`, recovered to authenticated AArch64 Linux
after genuine stock watchdog expiry in two FPGA states. No software reboot
command, JTAG redelivery or power intervention was used in either expiry test.
The retained HPS-first QSPI configuration was unchanged.

This qualifies the stock Linux watchdog reset path for the recorded kernel,
boot setup and MENU artifact. It is a hardware diagnostic, not acceptance of
the new bootstrap, managed daemon/agent/UI startup, image-update confirmation,
software replacement or arbitrary failed-programming recovery.

| Test | Result |
| --- | --- |
| Watchdog0 timeout and keepalive | Requested/read back 30 seconds; hardware counter decreased from 21 to 19 |
| Magic close | Same boot survived beyond the requested timeout |
| Baseline FPGA expiry | SSH disconnected after 44.762 seconds; new Linux boot authenticated |
| MENU with active scanout expiry | Production `Runtime.Start` reached idle after allocated scanout bootstrap; SSH disconnected after 47.822 seconds; new Linux boot authenticated |

The expiry observers require a new boot UUID, the pinned SSH host key, card
identity, kernel notes and original managed root image. Passive UART records
`Reset state: Cold` and full Linux startup. The persistent data probe survived.
Services were stopped normally after each recovered boot. The filesystem
volumes were synced and remounted read-only before each expiry.

The stock device is `/dev/watchdog0`, bound to the `dw_wdt` platform driver,
`snps,dw-wdt`, address `0x10d00200`, size `0x100`. Other watchdog instances were
not tested. The bootstrap preflight checks the exact DE25 model/compatible
bytes and AArch64 CPU before identifying this device. It checks the driver's
sysfs path and matching character-device identity without opening the device;
the guard rechecks that identity on its opened descriptor. It uses standard
Linux watchdog operations and never invokes the DE10 reset-register helper.

The new production preflight from `1790b1ff5fe13c419c2b9896c3e0d97b0c7451e1`
also passed on native AArch64, identifying actual watchdog0 `246:0` without
opening it. Its statically compiled opt-in test binary SHA-256 is
`a04abe6c9f07046dd9b7a2e2c9629026b4f633343b34ecbbb44c55f7f613db33`.
The focused platform suite and affected parent/consistency checks passed on
that source. This metadata test does not arm the production guard.

The MENU diagnostic links the real production hardware factory and runtime
library from `022eeb3457e6a812571dcbb279e2b342c15c6763`. A bounded watchdog
feeder covers startup, then stops after `Runtime.Start` reports idle. The
runtime and allocated scanout remain alive while the watchdog expires. This
does not substitute for starting the normal managed services. The capture
taken during this diagnostic has uncertain timing and supplies no video
acceptance evidence.

[evidence.json](evidence.json) records exact source, image, kernel, RBF and boot
identities. Private observer logs, pinned authentication and the diagnostic
tools are retained on Powerboat under
`out/artifacts/de25-vendor-reference/2ba9334c3/managed-kit-private/`;
credentials and raw console logs are not published.

The installed stable bootstrap rejected the earlier new-root trial before
executing that root and automatically returned to its known-good image.
That failure is preserved. It was not overridden, directly confirmed or
re-activated. The new DE25 guard source enables a subsequent bootstrap trial;
that new boot configuration needs its own deployment approval and native
acceptance. Full normal-kit qualification remains open.
