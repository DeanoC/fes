# Native USB channel-command diagnostic

Base: `4cfa98363d33474f484c3286304cad4dae7e758c`. This change investigates
the USB stop preceding the bounded ROM-step trial; it does not repair or
qualify the failed hardware channel.

The captured direct low-speed periodic IN packet had a toggle notification
(`GRXSTSP=0xb0000`, `HCINT=0x400`) with `HCCHAR=0xe05e8808` and
`HCTSIZ=0x80008`. Both enable and disable bits were set, but the current
packet's software `halting` flag was false. Continuation refused the channel;
the unchanged packet deadline and subsequent bounded halt expired. No input
pause, FPGA configuration, ROM transfer or CPU Run was reached.

The driver now retains the last eight software channel commands across packet
changes: arm, continuation, retirement and failure containment. Sequence and
packet numbers, exact written value, expected PID and completion flags identify
which packet requested a halt. The first toggle observation freezes that
history; failure output retains the later commands too. These are software
intentions, not device readbacks or proof that the hardware executed a command.
The recording adds no register reads, clock samples or channel writes. All
existing channel decisions, deadlines, PID and ownership gates remain intact.

The regression replays the captured values after a real driver arm, checks
that no software disable preceded the observation, then exercises the failed
pause and failed halt. It verifies no report, PID advance, replacement transfer,
paused state or post-retirement device access. A separate ownership-loss case
verifies that unexpected DMA enable prevents a channel halt. Additional checks
cover a NAK retirement followed by a new packet and bounded history retention.
The injected disable bit reproduces the observation, not its physical cause.

The focused Go suites for `dwc2`, `usbcontrol`, `input`, `usb` and
`nativefabric` pass. Logs and bounded job receipts are under
`out/nano-usb-channel-history-20261105`.

The primary [Linux v6.12 DWC2 source](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c)
clears CHDIS when continuing a PIO IN request and checks request-queue space
when submitting a halt. Neither behavior alone establishes why this observed
channel became disabled or whether it can safely be continued. This diagnostic
keeps the conservative refusal until fresh evidence identifies the transition.

The matched native SDK, entry, BL31, U-Boot and final composition now build on
source `d66ed9e2232b76bd5e297546bae43511d7057d3d`. Actual native machine-code
checks bind all four channel-writing paths to the history helper, which retains
the original single dynamic write. All 2,075 compiled source inputs (394 Git
blobs and 1,681 pinned external files) were archived and read back. The parent
checks passed 1,492 tests with two skips (a macOS-only check and an absent
upstream U-Boot cache), in addition to both planner suites and consumer checks.
Both real retained ROM choices preflight against the fresh SDK without firmware
calls. No shared schema or wire protocol changed.

## Bounded hardware result

One new native attempt on TRWU8CBL passed qualified recovery, all eight fixed
uploads/fresh SD readbacks, fourteen native startup checkpoints, seven USB
reports and the owned USB pause. The earlier USB error did not recur; this
does not prove its cause or a repair. The sealed FPGA configuration completed
all 1,658,880 bytes with data held. The ROM-step path was reached physically for
the first time, but its unchanged one-second deadline expired before completion.

The last verified written counter was 5,888 of 8,192 bytes, compared with 1,792
in the preceding full-owner trial. The timer recorded 400,008,197 ticks at
400 MHz (1.0000204925 seconds). Ownership brackets consumed 377,958,595 ticks
(0.9448964875 seconds, 94.4877% of elapsed time), across 5,958 checks including
5,163 fast guards. The maximum bracket was 234,624 ticks (0.58656 ms); invalid
and pending measurements were zero, and no ownership scope failed. These
aggregate measurements do not separate full observations from fast guards.
Synthetic host timing had underestimated the total remaining cost.

No ROM completion, accepted CPU Run, second launch, return or USB resume was
observed. The cached native state is `reboot_required`, generation one, with
resources retained and `Held=true`; that is not independent physical CPU or
USB halt proof. No retry or factory restore was performed. The original card
and other kit were untouched; the claim and test card remain retained.
Independent host cleanup verified exact USB permissions, the recorded 15 MHz
readback, the owned private JTAG server's exit and a free UART.

An initial wrapper invocation failed before opening the board because root
could not open the existing protected shared lock file. Acquiring that same
lock as its normal owner before the privileged operator resolved the host
launch problem. It consumed no native attempt and did not change the lock file.

The source-bound [hardware receipt](hardware-result.json) and complete traces
are under `out/nano-usb-channel-history-board-20261105`. Next, separate full
observation and per-access guard costs using the existing cached timer samples,
then optimize the measured path without relaxing ownership or time/poll bounds.
Ordinary package admission, physical video and DE10-equivalent core loading
remain open.
