# Bounded native automatic startup and two-core cycle: PASS

Exact native boot `548caa50395f3d94`, producer `303771739`, observer
`50f9bc8cd`, operator `a3cfb83a5f23db0ee251b64df8db078a538efec6`.
This is serial hardware diagnostic evidence on TRWU8CBL, not full appliance
acceptance or standalone SD power-on boot.

A fresh recovery programming epoch boots the matching protected loader and
verifies fourteen staged files plus all three installed records. It performs
no network upload or SD-write command. A separate private native epoch passes
the vendor-first and forced-scan identity gates and programs the matched SOF.
The earlier rejected scan attempt remains failed and separately retained.

The exact two-second autoboot countdown is captured once, followed by the
matched secure cold handoff, native SD/USB startup and Tenfoot Ready. No serial
`fesboot` or U-Boot environment command is sent; startup UART write count is
zero. The installed library appears as `ZX81 trial`, `ZX81`, `ZX80 trial` without
Install. Normal App commands then verify ZX81 trial (including the pinned
917-byte tape) launch/return and ZX80 trial launch/return. Package/build/ROM
identities and FPGA fetch activity pass. Five FPGA configurations, two core
launches and three menu sessions complete; final Park has logical generation
5, firmware generation 29, reset mask 15 and contained masters.

The complete saved UART and polling offers replay both in the live collector
and in a separate post-cleanup file-only verification. Independent receipt
joins and host cleanup pass: both private servers are absent, both archived
transport lifetimes match their hashes, clock policy is restored to 15 MHz with
automatic adjustment, USB permissions are exact and the correct UART is free.
The host supervisor completed in 362.65 seconds without a timeout; all
native/phase deadlines are unchanged. Historical programming failures remain
three. Raw traces and receipts are under `out/nano-autoboot-discovery-20261111/`;
`hardware-result.json` records exact identities, limits and evidence hashes.

The board is parked with the spare card installed. Factory flash and the
original factory card are preserved; no restore, card swap or power cycle was
requested. This establishes automatic application startup after the matched
volatile JTAG boot and two selected installed-core cycles. Standalone power-on
boot, power-loss behavior, current physical HDMI/key actions, native Ethernet,
broader packages/devices and DE10-equivalent appliance acceptance remain open.
