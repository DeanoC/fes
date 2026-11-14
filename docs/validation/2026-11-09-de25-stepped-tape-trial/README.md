# Stepped tape board trial preparation

Candidate `1059bb64e5cac38b` pairs the version-2 stepped tape caller and secure
firmware. The previous `38c6b2fe551dcd88` configured ZX81 but failed its original
one-second whole-file tape policy at 512 of 917 bytes; that image and its
programming operations remain consumed and retired.

Each secure call is bounded to one second and at most 512 tape bytes. Firmware
total time is `ceil(bytes/512)+2` seconds; the caller allowance is
`ceil(bytes/512)+4` seconds, with a separate one-second control/fetch budget.
Every ownership, CPU/MMU, reset and USB predicate and the 64-poll ACK cap remain.
The selected 917-byte tape therefore has a four-second firmware total and a
six-second caller tape allowance. FPGA core payloads and protocol are unchanged.

The complete integration checks apply to protocol source `b8d788c0`. The later
frontend selection repair on compiled source `96465727` has focused checks and a
rebuilt matched SDK/entry/BL31/SPL/FIT/SOF chain. It removes the unused diagnostic
frontend, leaving the FIT below its unchanged 8 MiB bound. These are off-board
results, not physical qualification of this new image.

The procedure uses the continuing exclusive TRWU8CBL claim and installed spare
card. It keeps the original seven configurations, three core launches and four
menu sessions, with 13 verified uploads and fresh SD readbacks. Recovery and
native programming each require a fresh durable single-use reservation and scan.
The qualified programming link is fixed 3 MHz with auto-adjust disabled; device
cleanup must independently verify restoration to 15 MHz and the original auto
setting, exact USB permissions, no owned server and a free UART.

Bounds remain native 450 s, outer 800 s, host 890 s and initial Ready 100 s.
The first failure ends the attempt, with only a two-second passive serial tail.
There is no replay of a consumed candidate. No physical key action is requested.
The bottom board is excluded. Factory restoration remains waived; the original
card stays safe. Old host-file cleanup remains paused.

Ordinary package admission, visible display/input and full DE10 appliance
behavior remain open. The actual trial result is recorded below.


The trial is terminal: both programming operations and all 13 uploads/readbacks
passed, as did the MMU/loader/EL1/SD/FAT proof and menu configuration. The menu
submitted one frame with zero reported underflows. Before Ready or any core
request, USB keyboard polling reported DWC2 channel error `0x80`; channel-halt
containment then reached its 500 ms limit. Menu master-stop, reader drain and
bridge reset closure completed. The recorded selection-to-prewrite delay was
4,022 ns and the around-command observation took 3,740 ns. Those are software
durations, not available USB frame time. All three HFNUM samples remained in
the same frame with a decreasing remaining count. An initial interpretation
that the frame window had expired was incorrect and is superseded here.
`0x80` is XACTERR; frame overrun is the separate `0x200` bit, as defined in the
[primary DWC2 register definitions](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hw.h#L729-L745).

No tape transfer or core launch was attempted, so this does not qualify the
new tape policy on hardware. The first-failure stop and two-second passive tail
were preserved. Independent device cleanup passed: original 15 MHz/auto-adjust
settings, exact USB permissions, owned server gone and UART free. Firmware
return and CPU parking were not observed. Candidate `1059bb64e5cac38b` and both
single-use programming operations are consumed and retired; no retry is allowed.

The next investigation follows the transaction error and failed PIO channel
halt, using the retained command/register history. No scheduling repair follows
from comparing the two software durations. Any established repair needs focused
off-board tests and a fresh matched candidate. Full acceptance remains open.
