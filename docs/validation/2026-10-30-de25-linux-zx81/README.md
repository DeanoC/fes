# FES Linux and direct DE25 ZX81 control, 2026-10-30

The retained FES AArch64 rootfs boots on bottom board `TRWU8J8G`. The real
`De25Computer` client now controls the actual FPGA directly through the CPU's
lightweight bridge, using the existing `LinuxMmio` adapter. There is no host
RTL endpoint or JTAG proxy during these commands. Captured HDMI observes
BASIC LOAD, RUN displaying `7`, a `2 REM` edit, reload without reset, another
RUN displaying `7`, and LIST restoring only `1 PRINT 7`.

This is explicit maintenance of an already configured, exclusively claimed
image. Production FES package/daemon admission, Linux FPGA programming,
MENU/DMA, reset fencing, audio and PR personas remain separate integration.
A userspace deadline cannot interrupt an underlying hardware bus stall.
The observations cover the identified bottom board and listed artifacts;
the hidden full OPN, HDMI electrical/CDC/MTBF and warm/global reset are not qualified.

## Initial image, boot and findings

The first card is a user-authorized spare, 62,914,560,000 bytes, identified
through reader `usb-Generic_STORAGE_DEVICE_000000000819-0:0`. Full readback of
3,984,588,800 bytes matches image SHA-256
`e8836b89667899efe1350d94668644fda19ebf397a3c18d770f6e93dd2d3e854`,
rootfs/image source `3ede3890895aad82be69dfbe3e4c4cbbb1c02c6c`.
The factory card and factory QSPI remain untouched. The user inserts that
spare into the bottom board. Initial passive capture loses the USB serial
connection during physical power cycling; no complete initial power-on log
is claimed. Later software reboot retains the complete SPL, kernel and FES
login transcript through the stable physical UART.

UART verifies FES login, AArch64, vendor kernel `6.12.11-gd7d192a9ddd9`,
`/etc/fes-board=de25-nano` and the ext4 root. The first FES boot UUID is
`845b44b5-6704-4faf-b321-d078b6dc1cac`. Dropbear defers key creation until an
incoming connection, so a unique ed25519 key is generated through trusted
serial before SSH enrollment. That key persists across the ordinary
factory-QSPI reboot to `db75ae08-b67f-4fca-92a3-99f54ec8d0bc`.

The reboot exposes the original network configuration's failure: udhcpc
exhausts its initial discovery window, prints `no lease, failing`, and exits
while the PHY/switch is settling. The serial login works but automated SSH
is unavailable. A serial-started runtime DHCP command recovers Ethernet and
pinned SSH. No persistent network file overlay is applied. The read-only
collector then verifies the selected installed files, kernel, device tree and
root-device number. It does not attest every byte of the writable SD image,
and address observation alone does not establish automatic boot-time DHCP.

Source `24fc445a2c68a8cb8945bf852bc9c61846b06b01` adds `udhcpc_opts -b` to
background and continue discovery after the initial window, while boot
proceeds. This keeps the original initial-discovery budget. BusyBox's pinned
[DHCP implementation](https://github.com/mirror/busybox/blob/1_33_1/networking/udhcp/dhcpc.c)
explicitly gives `-b` priority over ifup's default `-n`; its
[ifup implementation](https://github.com/mirror/busybox/blob/1_33_1/networking/ifupdown.c)
passes the per-interface options to udhcpc.

## Actual CPU control

After graceful shutdown, the known bootloader-complete ZX81 SOF is programmed
through the explicit bottom-board cable. Full SOF SHA-256 is
`cce19b02476f67d300e317180138e0e6801a9d86a69613605ad94a280d7a3784`,
FPGA producer `0534e1ec71e04a00b377e4dd0be8d64391b1dbb5`, computer build
`489319c9b6eeb20f2b582dca10edf3cb`. It boots the same FES SD to UUID
`d3e030a3-e0d4-42af-8543-20e960329364`. A read-only JTAG design/endpoint
identity preflight closes its master before CPU access begins.

The generated HPS connection reaches the peripheral bridge at base zero,
then the computer at `0x10300`, span 32. Combined with the HPS lightweight
window at `0x20000000`, the CPU resource is **`0x20010300`**. The JTAG master
instead sees **`0x00110300`**. The actual elaborated route and span are verified
by the retained [binding audit](initial-cpu-binding.json), separately from
Altera's [architecture/example mapping](https://altera-fpga.github.io/rel-26.1.1/embedded-designs/agilex-5/e-series/premium-065a/fabric-config-linux/ug-fabric-config-linux-agx5e-premium-065a/).
That preflight receipt retains its original not-yet-tested physical field;
later CPU observations are recorded separately.

The static AArch64 CLI is compiled from source `24fc445a2`, with the separately
retained SDK/compiler. [client-build.json](client-build.json) binds its source,
binary and SDK identities. The headless image does not install this client;
an exact verified executable and fixture are temporarily uploaded for this
maintenance run. The CLI checks architecture, rootfs/model, selected boot and
explicit CPU address, then locks the maintenance resource and checks endpoint
ID/version. Controls additionally verify the full computer build ID.

The identity-only probe performs two reads and zero writes. Actual direct
HOLD/RUN, HID and tape commands pass. The first immediate-after-reset typing
sequence is retained as a pacing failure: a reset ACK is not a ROM prompt.
The sequence is repeated from an observed prompt, with no clock, constraint,
command timeout or protocol relaxation. The successful native frames are:

- [LOAD command](observed-load-typed.png) and [completion](observed-load-complete.png).
- [Loaded program displays 7](observed-loaded-seven.png), completion `0/1`.
- [Stored edit](observed-edited.png): `1 PRINT 7` plus `2 REM`.
- [Reload completion](observed-reload-complete.png), without resetting.
- [Reloaded program displays 7](observed-reloaded-seven.png), completion `0/1`.
- [Final LIST](observed-restored-list.png): only `1 PRINT 7`, confirming edit removal.

Each still is an unmodified native frame extracted two seconds into a retained
three-second clip of the user's `/dev/v4l/by-id/fes-ms2109-hil` feed, YUYV
1920x1080 at 30 fps. The shared producer supplies that resolution; it does not
establish native HDMI electrical timing. The producer and remote viewer are
untouched. [initial-native-proof.json](initial-native-proof.json) retains the
command receipts and native frame/clip hashes. Counters are client-side MMIO
call counts, not an independent physical bus trace. JTAG is not used for control.

After the proof, the CPU is held, the exact temporary upload directory is
removed and its absence checked, and owned clients/SSH/JTAG/UART/captures exit.
Linux is synced and shut down. The claim remains held for the corrected-image
test and eventual factory restore. The user reports the first FES card lost
while removing it, and explicitly supplies another 64-GB card for wiping;
that replacement is identified separately before the corrected image is written.

## Checks and remaining physical gates

Three native CLI guard tests and four rootfs tests pass. The selected parent
suite passes 1,131 tests and generated consistency checks. The aggregate host
suite hits a 48-ms room-rendering timing failure in the unchanged Tenfoot
`TestHomeHeroIsTheTapTargetAndFitsTheRoom`; its draw callback takes 50.42 ms
wall time but 10.14 ms thread CPU. All FogCast bytes equal the clean base.
Both base and candidate pass 30 focused repetitions, then both pass the full
race-enabled Tenfoot suite. The initial aggregate failure is retained rather
than relabeled as a clean first pass. The independently run selected runtime
suite passes, including daemon, protocol, hardware guard, active-tree and
hardware-support truth checks. Documentation links and whitespace pass.

The corrected image is freshly built from committed source `24fc445a2` and
independently verified and smoke-tested. Its SHA-256 is
`d1952df9a36843095f6e5bef6981ea37f8012c86c8c3d1db5d78fe8c7edb6e46`.
The replacement card has capacity 62,534,975,488 bytes. Its complete written
image readback matches the corrected image above; board observations identify
122,138,624 sectors and card CID `275048534436344760702d33e0017c00`.

The corrected image boots to UUID `ed24b52e-9a15-45bc-9160-f5e0bd9d1340`.
Its serial log actually shows the initial discovery window expiring and
`forking to background`; Ethernet and pinned SSH then become available
without a recovery command. The live process includes the selected `-b`
option. An ordinary factory-QSPI reboot returns to
`0e08d1ec-caa5-40e6-aac2-cbd9582b0447`, again with automatic DHCP and SSH.
The SSH host key and all seven selected installed-file hashes persist.
The two corrected-image observation summaries bind their unmodified private
collector receipts; they retain the writable-filesystem identity boundary.

The first final FPGA programming invocation reports cable-not-detected before
configuration. The premature following identity check refuses the still-factory
SLD hash `7BD008B4D4BF4BC62777`; no native MMIO control is attempted in that
setup. A fresh exact-cable discovery followed by the same SOF programs
successfully, zero errors/warnings. This setup failure is retained and is not
a failed core/control result or an uncertain command replay. The successful
boot capture and UART inspection identify the corrected FES rootfs at
`c291b236-7eed-4e65-86c5-fb039dde3e68`. The positive read-only JTAG design and
endpoint preflight closes before native control begins.

[corrected-native-proof.json](corrected-native-proof.json) records the repeated
physical CPU test on this corrected image. [Loaded RUN](corrected-loaded-seven.png)
and [reloaded RUN](corrected-reloaded-seven.png) both display `7` and `0/1`.
The [edit](corrected-edited.png) shows both program lines; the
[final LIST](corrected-restored-list.png) shows only `1 PRINT 7`.
No reset occurs between the first LOAD, edit and reload. Actual wrong-boot,
wrong-CPU-address and wrong-build requests refuse. A live second owner refuses
while a held-CPU HID command owns the cooperating lock; its owner completes
and releases it. Those checks are retained separately in
[corrected-guard-checks.json](corrected-guard-checks.json).

The CPU is held, all exact owned upload directories and target log files are
removed, all native clients/forwards/JTAG masters close, and Linux is synced
and shut down. Factory FPGA restoration and the final recorded baseline/release
are pending. No final release is claimed before that physical verification.
