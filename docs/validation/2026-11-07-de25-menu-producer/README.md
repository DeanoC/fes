# DE25 two-clock menu producer, 2026-11-07

The HPS-first menu personality now builds a content-sealed diagnostic package
with the existing 100 MHz CPU control path, 74.25 MHz menu scanout and two
4 MiB framebuffer slots. The compiled source is `f6134935abc9402361fd2b7dca2b67366d87e6d2`;
its package/build identities and exact reports are in [result.json](result.json).
This record establishes neither native menu boot nor physical display/core-load
acceptance.

All ten standard Quartus tasks completed. Only menu placement has a 180-second
cap, supported by a 139-second same-input measurement; other tasks retain
120 seconds. No clocks, timing constraints, seed or optimization mode changed.
The actual sealed project also passes the existing three-frame Tenfoot/DDR model,
640-packet crossing checks and reset/timeout negative controls. Six CPU/firmware
resource descriptors were derived from that package and its generated adapter.
The actual HDMI startup model passes for both menu and computer wrappers,
including asynchronous reset assertion, two-edge pixel release, interrupt
reinitialization, NACK retry and the two broken controls.

Reported constrained setup, hold, recovery, removal and pulse-width slacks are
positive. Full timing signoff remains false: five unwaived rule classes remain.
In particular, `TMC-20023` reports combinational logic on the reply packet's
direct-net delay assignment. This is a new repair gate; the other positive
reports do not make that assignment valid. Local failure selection now stays
outside the reply capture bank, preserving the complete constraint file.
The repaired source passes the same 12 CDC cases, 640 packets, three frames and
negative controls; its fresh sealed compilation remains pending.
The remaining I2C/interrupt/LED delay and reset findings, board electrical
budgets, generated AXI crossing simulation, matched native boot and physical
menu/core/menu tests remain separate gates.

Earlier failed generation, packet-width and phase-timeout candidates are kept
in their private output directories with the exact sources and logs. No board
was programmed, no card was written and no previous hardware result is applied
to this new profile.
