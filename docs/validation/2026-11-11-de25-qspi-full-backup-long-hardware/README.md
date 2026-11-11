# Two matching complete factory main-array reads

The2e05f7b4c operator completed two independent16MiB reads of TRWU8CBL's
main QSPI array. Each read used4096 fixed1024-word packets and8208 root PRE/POST
ownership grants. Every saved word/header/address and binary byte independently
replayed correctly. Both whole-array SHA256 values and all bytes match:
165afd94489af66ba86160d47b3d404263b4cb4a0c10a7a2a007a4f340cb7fc6.
The qualified0/0x200000/0xfff000 windows also match. AFh identity20bb18, two zero
status bytes, unique acknowledged CLOSE and unchanged CONFIG_STATUS pass in
both sessions. Fresh USB, source, private-server, vendor-first and forced-scan
gates were independently admitted; namespaces and servers were distinct and
sessions did not overlap. Each namespace/server was fully removed before the
next session. The first read took about16minutes, the second about14minutes.

Independent cleanup passed for both:15MHz/auto1, exact USB permissions, unchanged
private config bytes/mode600, no namespace/server survivors and free UART. The
first read was separately retained before the second completed; whole-cohort
retention follows publication. The earlier600-second hard stop and450-second
soft stop remain failed, separately archived prefixes and were never resumed.
Only the full-backup host duration changed; native boot/core-load/RAM limits did
not change. No image programming, reset/reboot, flash or SD writes occurred.

This is a complete16MiB main-array backup. OTP/security and exact die/package
identification are excluded. It does not establish factory restoration, native
QSPI power-on boot, current CPU Park or general appliance acceptance. The spare
card remains installed, factory card/flash and bottom kit are preserved, and the
continuing top-kit claim remains held. A separately generated restore JIC/RPD
now matches this array; physical restore and any flash change remain unapproved.
