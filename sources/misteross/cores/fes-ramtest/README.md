# fes.ramtest

Utility core for the MiSTer GPIO SDRAM addon and the FPGA-to-HPS DDR bridge.
The host mailbox is `fes.application` 1.0, with the fixed 720p picture and the
gamepad interface. There is no memory opcode. After execution release, each
path writes a span and reads it back for six patterns: `0000`, `FFFF`,
`5555`, `AAAA`, the address mixed with its high half, and the inverse.
The HDMI text shows the pattern, the address, the error count, and the first
mismatch.

The SDRAM span is the 128 MB addon: 64M halfwords, with a refresh between
commands. The HPS span is 256K steps starting at byte address `0x01000000`
in the same command field the bridge probe used. It does not walk all of
system RAM. A missing acknowledge stops that path, which is what a contained
HPS bridge does. A data mismatch is counted and the scan continues.

Any `fes.gamepad` button stops both scans and the status line says `STOP`.
The host maps a keyboard onto those buttons (arrows, Enter, Space, and the
letter keys it already forwards). Escape and Backspace leave the session in
the host and are not delivered to the core.

A `fes-gp-v1` package load releases the HPS bridges after user mode. A raw
development RBF stays on the contained profile, so the HPS path fails until
the bridges are released. The SDRAM addon does not need that release.

`make sim-fes-ramtest` runs a short span of the same patterns against
behavioral memory. It checks identity, the video and gamepad capability
bits, execution release, a green status glyph, and a button stop. The
sealed core keeps the full spans above. `make build-fes-ramtest` seals an
RBF. The package is not registered and is not in the factory image.
