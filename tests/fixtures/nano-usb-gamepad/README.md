# Captured joystick HID report descriptor

`joystick-report.bin` is the exact 98-byte USB report descriptor read on exclusive
TRWU8CBL by native source `19a59c4f0` and observer `fc8b94f1e` in the single
bounded gamepad trial. SHA-256:
`171f93692f2d718af6c6e9fc5b7d8c2580ac33fa2e3044ee2393c07ec839dc11`.
See `docs/validation/2026-11-04-de25-gamepad-hid/` for trace and receipt hashes.
The immediately preceding keyboard diagnostic enumerated `081f:e401`; this
successful descriptor transfer supplies no new vendor/product-ID observation.

The schema describes two unsigned eight-bit X/Y axes, 28 constant padding bits,
ten buttons at input bits 44–53 and ten vendor input bits at 54–63. It also
declares a separate seven-byte vendor Output item, which the input parser must
not count as input. One vendor Usage is repeated over ten variable fields.

Only this descriptor is a physical capture. Reports used by Go regression tests
are constructed vectors, not captured button presses or input qualification.
