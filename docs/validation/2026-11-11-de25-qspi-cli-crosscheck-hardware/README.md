# Passing independent flash-window cross-check

A fresh CLI-only private JTAG service on TRWU8CBL passed its vendor-first listing
and forced scan, then completed the exact read-only window sequence at frozen
operator `b18099039`. All12288 bytes match the saved System Console read from
`fb1830181`: offsets0,0x200000 and0xfff000,4096 bytes each. AFh identity20bb18,
two zero status bytes, unchanged CONFIG_STATUS, unique acknowledged CLOSE and
22 root PRE/POST ownership grants were verified. Complete replies, CLI logs,
binary words and source identities independently replay correctly.

The earlier mixed-transport attempt remains failed: after its completed console
read, CLI's first scan returned87 without an SDM response. No console read was
repeated here. The fresh-server experiment passed, establishing a usable
transport boundary. It does not prove the exact cause of error87.

Independent cleanup passed: no task namespace members or private server survive,
15MHz/auto1 and exact USB permissions are restored, private client bytes/mode600
are unchanged and the top UART is free. The initial post-auditor expected a
console-specific receipt field; a separate corrected auditor verifies the
actual CLI file directly. Both versions and logs remain retained.

This qualifies the three main-array windows and byte order across two transports;
it is not a complete backup, a usable rollback or autonomous boot acceptance.
No flash/SD write, image programming, reset or reboot occurred. The spare card
remains installed, factory card/flash and the other kit are preserved, and CPU
Park was not reobserved. Native Ethernet and broader appliance coverage remain
open. Next obtain two complete independent16MiB main-array reads, verify hashes,
then prepare a concrete rollback before any flash-change approval request.
