# Bounded native USB report and shutdown cycle passes

Exact native/firmware and observer source:
`64be01ffe51f61a4076d28e3ea5b25fb4ba5d01b`.
Chain `8ed04a207c7db783dad5c18351755066268b1f6cfb5b5fd6b1c90d5927e91870`.
One actual attempt ran on exclusive TRWU8CBL and passed. No retry, factory
restore, native SD/core/display/network run or flash write was performed.

The selected gamepad program completes its five-second polling window with
416 real eight-byte idle controller reports, followed by a checked firmware
Stop receipt and terminal USB Pass. The descriptor remains the captured 98-byte
schema SHA-256 `171f93692f2d718af6c6e9fc5b7d8c2580ac33fa2e3044ee2393c07ec839dc11`.
Every report is `7f7f0080800f0000`, decoded as X/Y=-129, other axes=0, buttons=0,
hat=-1. Off-board replay with the same committed parser reproduces all 416
reported states. This validates physical idle-report transport, not button
presses, movement, menu mapping or core input routing.

The trace ends:

```
FES_NATIVE_USB STOP owner=8ed04a20e5628535 generation=3 data=held
FES_NATIVE_USB PASS scope=usb-only-pio
```

The strict observer verifies matched secure setup/boot, nine CPU checkpoints,
trusted source context, 328 HPS pin samples, descriptor hash/length, explicit
gamepad protocol, report bounds, Stop owner/generation and final ordering. Its
`complete`/`ready`/`stopped` fields are true with no errors. `input_observed` is
false because no state change occurred. CPU halt is not observable in this trace.

Qualified recovery, one fixed `FES/UPLOAD.BIN` Ethernet write/readback and fresh
SD reread pass with protected boot files, manifest/card/PHY and memory checks.
The independent root cleanup audit verifies unchanged 15 MHz, exact USB
permissions, private JTAG process gone and a free UART. The other kit and
original factory card were untouched. The claim and spare remain held for the
continuing base-system work; factory restoration remains waived by the user.

The driver now clears non-split IN ACK-after-error notifications without
completing a packet, changing PID, resetting HCTSIZ or extending its deadline.
Split ACK and OUT/PING handling remain separate. A late-ACK model reproduces the
previous two-report/zero-continuation stall before the repair, then completes
the third constructed report and owned closure after it. ACK alone and fatal
status cannot supply reports or bypass failure. Current packet flags and sampled
continuation reasons are retained on errors, with a bitmap distinguishing real
register samples from unsampled fields.

This matches the non-PING ACK behavior in the pinned Synopsys-authored
[DWC2 reference](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd_intr.c#L1334).
The successful physical trace does not report notification counters, so it
cannot prove a particular ACK/toggle recovery occurred. Model behavior and the
passing physical startup/report/Stop cycle are separate evidence levels. No
Linux driver is linked into the image.

Targeted tests, the complete TamaGo host race suite, `make check`, matched SDK/
entry/BL31/U-Boot/local-PFG composition and committed-source/actual-ELF audit all
pass. Operator, uploader, receipt, passive-tail and raw-report replay controls
pass. The exact files, receipts and trace are under
`out/nano-usb-status-20261104/` and `out/nano-usb-status-trial-20261104/`.

This closes the bounded standalone USB startup/idle-report/Stop gate. It does
not qualify long-duration USB, hub/device replacement, keyboard compatibility,
physical button mapping or full normal-kit behavior. Next is integration with
Tenfoot and explicit USB/SD/FPGA ownership composition: the current USB service
is closed-bridges-only and the native Tenfoot core path still selects UART.
Ordinary package/ROM/media admission, live core input/video and normal DE10-style
core-load acceptance remain incomplete.
