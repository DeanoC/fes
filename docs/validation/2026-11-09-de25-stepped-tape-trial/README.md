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
behavior remain open. This record will be updated with the actual trial result.
