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

Next: rebuild the matched native boot chain, prove that the actual native ELF
contains the new history, and prepare a fresh bounded TRWU8CBL trace. No fresh
hardware run has been performed for this change. ROM-step timing, ordinary
package admission, physical video and DE10-equivalent core loading remain open.
