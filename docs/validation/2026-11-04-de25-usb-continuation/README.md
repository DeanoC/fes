# Native USB readiness and two idle controller reports

Exact native/firmware and observer source:
`17729a0041f3038252d96d96e6846ef07aee3886`.
Chain `e0d66eba9233371b9254b17ae1750cdb04a4c7c8cc590a039974d8f73de490ed`.
One actual attempt ran on exclusive TRWU8CBL. It stopped at the first failure;
this image was not retried. The full trial failed; this record separates its
new physical progress from the uncompleted shutdown/input/core gates.

The driver queues at most one further direct-IN token per poll when request
queue space exists. Setup, HCTSIZ, expected PID, buffer and packet deadline are
preserved. Periodic tokens retain the advertised interval and next-frame parity.
New RX/status, completion, halt, zero remaining packet count and full queues
prevent continuation. Completion races wait for status without restarting a
spent packet. Unexpected packet counts fail.

The corrected model requires an IN token after notification acknowledgement at
descriptor offset88. The previous code reproduces the physical stall in that
model; the new code completes the descriptor without replaying setup or transfer
registers. Tests also cover full/wrong queues, frame wrap, interval limits,
completion races, aborts, wrong PID, absent data and owned closure. The final
focused and complete host race suites, `make check`, matched SDK/entry/BL31/
U-Boot/local-PFG build and source/actual-ELF selection audit pass.

Physical qualified recovery, one experimental boot-file upload/readback and a
fresh SD reread pass. Native boot, nine CPU checkpoints, trusted context and
328 HPS pin reads pass. The runtime completes the exact 98-byte report descriptor
SHA-256 `171f93692f2d718af6c6e9fc5b7d8c2580ac33fa2e3044ee2393c07ec839dc11`,
passes the live firmware confirmation and prints USB Ready. It then receives
two physical eight-byte reports:

```
7f7f0080800f0000 -> X=-129 Y=-129 other axes=0 buttons=0 hat=-1
7f7f0080800f0000 -> X=-129 Y=-129 other axes=0 buttons=0 hat=-1
```

The off-board tool compiled from the same committed parser independently
replays those raw bytes and reproduces both reported states. These are actual
idle reports; no button press, movement, menu mapping or core input was exercised.
The trace observer's completed-session `input_observed` remains false because
there was no state change and shutdown failed.

The next interrupt packet fails:

```
endpoint=1 IN expected PID=0 split=false received=false polls=497
toggle_rx=1 toggle_irqs=1 continuations=0 -> packet timeout
first RX peek/pop=0xb0000 channel=0xa05e8808 HCINT=0x400
HCTSIZ=0x80008 nonperiodic=0x1080040 periodic=0x100040
frame=0xb5690053
containment: channel=0xe05e8808 did not clear enable within 500ms
firmware status 4
```

No checked Stop, terminal Pass or CPU halt was observed. The current trace
does not identify which continuation guard prevented another interrupt-IN token.
Its snapshot is from notification time, not a later queue/frame observation.
The known descriptor failure is overcome in this attempt; sustained interrupt
operation and safe shutdown are still unproven.

References are the pinned Synopsys-authored
[PIO IN continuation path](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L1477)
and [interrupt handlers](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd_intr.c).
Exact files/hashes are retained under the build's `reference/`; no Linux driver
is linked. In particular the non-split ACK-after-error handler permits progress,
whereas our continuation helper currently waits on any pending channel status.
That is a relevant source gap to test; the physical trace does not prove a late
ACK occurred. The next implementation needs both appropriate status handling
and retained continuation/packet-state reasons, without accepting wrong data.

The independent replay verifies descriptor/Ready, both raw reports and their
normalized states, exact failure fields, upload protections and cleanup. Root
read-only cleanup confirms unchanged 15 MHz, exact USB permissions, private
JTAG process gone and a free UART. The continuing claim and spare stay held;
USB containment is unproven. The other kit and original factory card were
untouched, and no factory restore followed the failure per standing instruction.

Evidence: `out/nano-usb-continue-20261104/` and
`out/nano-usb-continue-trial-20261104/`. Physical control mapping, combined device
ownership, normal package/ROM/media admission and DE10-style core-load readiness
remain incomplete.
