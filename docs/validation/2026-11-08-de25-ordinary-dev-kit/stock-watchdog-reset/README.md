# Stock-watchdog native diagnostic

These are the exact standalone Go source and pre-arm guard tests used for
[the 8 November native result](../stock-watchdog-reset.json). They are retained
diagnostic inputs, outside the production runtime and boot policy. The helper's
`expire` mode deliberately resets the board; it is not a general maintenance
command or an automatic recovery service.

The claimed bottom-board trial authenticates the selected software, kernel,
stock boot FIT, card and own host, stops FES through the existing supervisor,
syncs storage and makes all nine SD-backed mounts read-only before execution.
That includes the bootstrap, root image and all five data bind mounts. The
helper independently requires those mounts, no running FES processes, no
overlays, the exclusive physical configuration lock, the exact kernel and stock
DW watchdog resource/driver/device identity. The running embedded kernel config
must disable NOWAYOUT and build in the DW driver, with no boot override. This
kernel intentionally omits the optional watchdog status sysfs attributes.

The `ping-stop` control verifies a decreasing hardware counter, sends one magic
close and closes once. The external coordinator then verifies the original
read-only boot survives 75 seconds. Only after that control passes does one
`expire` invocation stop pinging. It invokes no software reboot; if no reset
occurs by its bound, it reports failure and attempts magic-close cleanup.

Both sources compile against the existing platform module's pinned
`golang.org/x/sys/unix`. The private build record retains the exact module
workspace, compiler commands, output hashes and logs. Eighteen native guard
vectors and the AArch64 build pass. The published source hash matches the
actual staged binary's build record; these tests do not open a watchdog.

Native expiry returns through the captured stock ATF banner and a fresh boot
UUID to authenticated MENU. One subsequent ordinary ZX81 launch observes two
fresh ROM prompts, passes BASIC 7/9/7 and Stop/MENU, and retains catalog and SAVE
bytes. The temporary helper disappears with the reset; the serial recorder
terminates and the board is released. Raw UART evidence retains existing
Bluetooth HCI and overlay-property warnings.

This qualifies a healthy-system watchdog reset with storage already quiescent.
It does not qualify reset during a blocked secure call, failed-upload recovery,
dirty-data safety, guest resumption or a new automatic reset policy.
