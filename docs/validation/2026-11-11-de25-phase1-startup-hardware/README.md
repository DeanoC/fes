# Bounded phase1-only native startup and installed core cycles: PASS

The exact 831,488-byte HPS phase1 RBF for native boot `548caa50395f3d94`
was loaded through volatile JTAG on TRWU8CBL. The full matched native SOF
was a reference only and was not programmed in the native epoch. This is
serial hardware diagnostic evidence, not standalone power-on qualification.

A separate recovery epoch freshly verified fourteen staged files and all three
installed records without downloads or SD writes. Mandatory vendor-first and
forced-scan identity gates passed in both epochs. The native programmer then
loaded only the selected phase1 RBF. The exact automatic countdown, secure
handoff, SD/USB startup, initial menu configuration from SD and installed library
were observed without any UART startup write or `fesboot` command.

ZX81 trial with its pinned tape and ZX80 trial completed matching launch/return
cycles. Five configurations and three menu sessions finished at Park, logical
generation 5, firmware generation 29, reset mask 15 and masters contained. No
Install was issued. Complete saved-wire/poll-time replay, independent receipt
joins and independent host cleanup passed. Both private services are absent,
clock is restored to 15 MHz with automatic adjustment, USB permissions match
and the top UART is free. Existing native/phase deadlines were retained.

See `hardware-result.json` for exact images, source identities, observations,
limits and evidence hashes. The raw stage is `out/nano-phase1-startup-20261111/`.
The board finished parked with the spare card installed. Factory card/flash and
the other kit were preserved; no power cycle or factory restore was requested.

This qualifies the selected phase1-only volatile startup and two installed-core
cycles. It does not establish autonomous QSPI power-on boot, power-loss behavior,
current physical HDMI/key observations, native Ethernet, general package/device
coverage or DE10-equivalent appliance acceptance. Actual flash identification,
verified complete backup and rollback preparation remain prerequisites to any
reviewed persistent-flash change and separate power-on test.
