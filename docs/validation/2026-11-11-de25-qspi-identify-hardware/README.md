# Read-only QSPI identification rejected: zero JEDEC data

The bounded TRWU8CBL diagnostic at source `cad3aea8500b040c895912913a3d771208109cf1`
passed claim/source/USB/UART/private-service/fixed-clock/vendor-first/forced-scan
admission. The documented CLI response grammar matched exactly. Live
CONFIG_STATUS returned state zero, version 0x1a0300, pins 0xc0000089 and soft 3.
Separate CLI invocations successfully opened flash access, selected chip zero,
returned one data word for each register read and closed access.

The 0x9f three-byte JEDEC read returned 0x00000000; both 0x05 status reads also
returned zero. The group validation rejected the identity. No chip, capacity,
array backup or correct read byte order was established. Zero status can be
valid and is not independent evidence of valid flash reads. This failed attempt
was not retried. The returned data did not become authority for an array read.

The single close-only cleanup returned successful SDM status. Independent log
replay and host audit confirm that close, both clock parameters restored to
15 MHz/auto-adjust, absent owned server, exact USB permissions and free UART.
The CPU was not rebooted, programmed or queried for a fresh Park; the prior
native Park remains the last such observation. No array read, flash/SD write,
controller handover, generic flash opcode or image program occurred. Historical
image-programming failures remain three. Factory card/flash and bottom kit are
preserved. All raw command logs and receipts are retained under the stage in
`result.json`; the failed result remains terminal.

Static inspection confirms both matched projects select ACTIVE SERIAL X4.
A possible protocol mismatch remains to investigate: manufacturer documentation
[describes Micron Quad I/O ID reads using AFh rather than 9Fh](https://onlinedocs.microchip.com/oxy/GUID-5474CCCD-F385-4AD7-9759-539BB1019357-en-US-10/GUID-4F13BD82-4678-46E3-A8DB-759A4A056317.html).
This does not prove the board's current flash mode or populated part. A separate
source-bound, one-use AFh-only read experiment may test that hypothesis without
changing mode. It must validate the ID immediately before further register
reads, preserve all transport/cleanup gates and reject zero/unknown identity.
Full flash geometry/backup, rollback and standalone power-on remain open.
