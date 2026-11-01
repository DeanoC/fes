# Nano cold cookie and cleanup boundary

Two bounded TRWU8CBL diagnostics ran with the exact top JTAG cable at 6 MHz.
Both restored and read back its original 15 MHz setting, closed UART clients
and restored USB permissions. The continuing kit claim and spare card remain
in place; the original factory card is safe outside the board. Other kits and
the JTAG daemon were untouched. See [evidence](evidence.json).

At 6 MHz, unchanged v14 programmed successfully and its
[serial trace](v14-native-console.log) reported initial ownership, DDR/FIT
verification and final SD drain/handoff, then BL31 rejected the provider.
No `fesboot` was sent. This is new evidence beyond the earlier two 15 MHz
programmer failures; their empty consoles did not establish CPU entry.

The [compiled caller comparison](cookie-abi.json) identifies a concrete ABI
mismatch: v14 passes version-1 cookie `0x464e433100000001`, while BL31 requires
version 2. Commit `c768ef002` installs the shared headers on U-Boot's include
path and uses `FES_NANO_COLD_COOKIE` directly. The generated-call C regression
checks all four arguments; the consumer still rejects the old cookie.

The matched v15 chain was uploaded through the real Ethernet recovery path.
Independent SD hash/byte readback passed; the protected prefix and root
recovery FIT stayed unchanged. Only `FES/UPLOAD.BIN` was overwritten, not the
newly assembled full SD image. The [v15 native trace](v15-native-console.log)
then reported `provider=1 result=0`, reached the loader and passed the
`before-entry` owner/generation snapshot. One `fesboot` was sent. The
`after-remove` snapshot returned status 4 with no owner, and the loader
correctly rejected handoff before EL2/EL1/Go entry. It sent no native SD writes.

The pinned vendor reset driver's OS-removal callback unconditionally clears
USB, NAND and DMA reset bits (`0xff010030`). That contradicts the cold serial
provider's retained master ownership. The dedicated native transformation now
checks the exact reset source, removes its obsolete compatibility helper and
makes removal retain reset state. The actual pinned callback and transformed
callback run against the actual cold consumer in C/UBSan: the original retires
ownership, including after reset restoration; the transformed callback retains
it with either legacy environment setting. Other ownership checks stay strict.
Recovery does not select this transformation.

V15's affected checks passed: 1,235 parent tests with 40 skips, planner checks,
whitespace and generated-consumer consistency. The cleanup-focused suite has
10 passing tests. The next step is a fresh matched cross-build and bounded
hardware test of that cleanup repair. Native Go boot, active peripheral resume,
FPGA programming and physical core launch/return remain unverified.
