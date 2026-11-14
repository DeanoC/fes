# Physical HID descriptor capture and parser rejection

One actual gamepad diagnostic attempt ran on exclusive TRWU8CBL with the spare
card. Native/firmware source `19a59c4f0`, observer checkout `fc8b94f1e`, chain
`586ca73ab290ad7afa0b4f053d11ad20143977e23211c301e448e74d4abfaa0d`.
It stopped at the first native failure and was not retried.

Qualified recovery and a single `FES/UPLOAD.BIN` Ethernet write/readback plus
fresh SD reread passed. Native boot, nine CPU checkpoints, source context and
328 HPS pin samples passed. The runtime fetched the connected device's exact
98-byte report descriptor, SHA-256
`171f93692f2d718af6c6e9fc5b7d8c2580ac33fa2e3044ee2393c07ec839dc11`.
It then rejected the schema with `absolute variable usages required`.

The captured Joystick application has two unsigned eight-bit X/Y axes, padding,
ten buttons, ten vendor bits and a separate seven-byte Output item. Its vendor
Input declares one usage for ten variable fields. Our parser incorrectly
required exactly as many usages as variable fields before it could ignore those
vendor controls. This is a parser limitation, not evidence of a firmware-call or
reset failure. No Ready, input report, checked Stop or CPU halt was observed;
ordinary core admission remains unproven.

The HID report offset/count and local usage rules are described in
[HID 1.11 sections 6.2.2.7–9](https://www.usb.org/sites/default/files/documents/hid1_11.pdf).
The explicit last-usage extension behavior can be checked in
[Linux v6.12 hid_add_field](https://github.com/torvalds/linux/blob/v6.12/drivers/hid/hid-core.c#L291).
These are protocol references; no Linux driver is linked into the native image.

`out/nano-gamepad-trial-20261104/` retains frozen procedure inputs, operator and
uploader negative fixtures, trace, descriptor binary, receipts and independent
replay. The replay checks descriptor bytes/hash, rejection, matched boot/CPU
evidence, upload protections and cleanup. A separate root read-only audit
confirmed unchanged 15 MHz, exact USB permissions, no remaining private JTAG
process and a free UART. The other kit was untouched. The continuing claim and
spare stay held; the original factory card remains safe outside the board and
was not restored under the user's standing instruction.

Next is an off-board regression using this captured schema and a generic fix for
repeated vendor usages, followed by a fresh matched image. This failed image
must not be retried. Full physical input, combined device ownership and normal
DE10-style package/ROM/media/core-load acceptance remain incomplete.
