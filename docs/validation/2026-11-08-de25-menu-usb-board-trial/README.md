# Bounded paired trial with serviced USB

This is a new candidate compiled from `f8653867f`, which services shared USB
during native menu publication. The earlier paired attempt is consumed. Its
failure and qualified recovery remain preserved separately.

[operator-source.json](operator-source.json) freezes the helper sources, admitted
assets, matched chain and nine fixture receipts. The
[matched build proof](../2026-11-08-de25-menu-usb-service/boot-result.json) binds
the SDK, entry, BL31, SPL/FIT and composed SOF. Boot ID is `aa11ddf99761b275`.
The FIT remains 8,284,720 bytes with 103,888 bytes available; low firmware ends
at `0x5de00`, below `0x60000`. Both slots, padding, ownership and original time
bounds remain checked. A full fresh candidate/profile check passed after build.

The operator performs one qualified recovery, twelve fixed Ethernet uploads with
write/readback and twelve fresh final reads, then a native paired menu/core/menu
cycle on **TRWU8CBL only**. It waits for a complete Ready launcher and subsequent
frame acknowledgement before each command. Three selected computer launches
exercise ZX81, ZX80 and ZX81, with two ordinary returns and final owned shutdown.
Expected totals are seven configurations, four menu sessions, logical generation
7 and bridge generation 40. No physical controller press is required.

Fresh helper checks pass: eight actual transport methods, four paired receipt
methods, 126 stateful upload cases, 529 receipt mutations and 16 lifecycle/cleanup
cases. Secure calls, card operations and console transport are modeled in these
fixtures. Recovery FIT, SPL, SOF, manifest and protected prefix are byte-identical
to the qualified recovery; this preparation writes no card or board.

Before starting, the root read-only preflight must verify the continuing claim,
unique top adapter, complete UART descriptor inventory, network route, committed
freeze, every source/input hash and all current chain checks. Preflight does not
open the UART. The existing one-attempt fuses and phase bounds are retained.
Programming uses the private top-only JTAG server with the qualified temporary
6M clock. The exact original permissions, 15M clock and private-server cleanup
are independently checked after the attempt. The bottom kit is excluded.

Stop at the first failed proof, retain the capture and do not retry this
candidate. The user's continuing authorization allows the top board to remain
on the spare test card without a factory restoration. An uncertain native stop
is recorded as uncertain; host cleanup does not prove the CPU is parked.

This frozen procedure is pre-device evidence. No hardware result, physical
HDMI/input acceptance, ordinary admission or DE10-equivalent readiness is claimed.
Stage: `out/nano-menu-usb-board-20261108`.

The subsequent [hardware attempt](hardware.md) reached the complete launcher
and a running ZX81, then stopped on the host observer's USB/fetch ordering check.
This procedure is consumed; full return and shutdown remain untested.
