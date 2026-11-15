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

## Rejected system/media-control descriptor

`system-control-report.bin` is the exact 53-byte descriptor captured in the
single bounded TRWU8CBL USB trial with native boot `3c23bd7f67779909`, compiled
source `e9547a413594192de45e528d6688039f93d17c91` and operator
`9aa4a789f`. SHA-256:
`3d277392ac48c3c8d0afa6545e6b7c21d6e80286fcc78a89657e639f1b537e53`.
The source and raw trace are retained in
`/home/deano/fes-retained/de25-usb-control-nak-board-result-20261108-r1` and joined
in `docs/validation/2026-11-08-de25-usb-control-nak-board-trial/hardware-result.json`.

It contains System Control and Consumer Control applications, not a
Joystick/Game Pad. The native parser rejected it before readiness. No actual
vendor/product identity, complete interface inventory, input report or button
action was captured. Model identities used in tests are synthetic. The fixture
must remain rejected; it is not evidence for broadening gamepad admission.
