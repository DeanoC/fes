# DE25 Nano reaches native Go without Linux

TRWU8CBL completed the matched SPL → BL31 → U-Boot → EL2 cleanup → EL1 →
TamaGo Go serial probe. The [trace](native-console.log) ends with
`FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano`. This is exact-artifact
hardware acceptance of the contained serial diagnostic. See [evidence](evidence.json).

The real probe passed timer, goroutine, allocation, garbage-collection and
volatile memory-filesystem checks. The filesystem check uses RAM. Native
SD/FAT32, USB, Ethernet, display, FPGA programming and physical core
launch/return have not passed. The cold firmware still advertises zero
configuration features and retains all unused masters and secondary CPUs.

The test used source `6107628ca2f6aa24da167c25191dd3a1e22c9e0b`, with native
target bytes built from `c07188ea3`. Refreshing the uploader and its package
input receipt left the SOF, FIT, SPL HEX and entry image byte-identical.
The exact native build ID is `b9df2a8c60e2121d`; the SOF SHA-256 is
`062e2ea14d2ede08cbfb07e1b7b9467d8d98c93cd17b2ed198fee33f310e0c4f`.
Independent SOF extraction had verified the sealed SPL before programming.

Fresh recovery boot and the corrected production upload core passed on the
spare card. It observed/configured the volatile MAC, explicitly probed Ethernet
and observed PHY status `0x7949 → 0x796d → 0x796d` before DHCP. TFTP transfer,
RAM hash, one `FES/UPLOAD.BIN` overwrite, independent SD hash/byte readback and
protected-prefix/root-FIT rechecks all passed. No full SD image was written.
The uploader now handles the loader's write-once MAC and lazy Ethernet probe.
Each upload attempt still requires fresh recovery because previous FAT reads
retain LMB reservations; that safety gate remains unchanged.

Native SPL accepted the initial CPU/reset proof, initialized 1 GiB DDR,
verified the sealed FIT and completed PIO SD drain/reset. BL31 reported
`provider=1 result=0`. One `fesboot` was sent. Both loader snapshots returned
`0x46424f31000000df`, owner `0xbdd1111bb8c8473f`, generation 1. Cleanup preserved
that authority after the [contained reset-driver repair](../2026-11-01-de25-cold-cookie-cleanup/README.md).
The final live check reported core 0, 400 MHz counter, other CPUs off,
`PER0=0xff7fbfbf` and closed bridges. EL2 cleanup and EL1 handoff passed before
the actual Go probe ran. Native sent no SD writes.

The source-bound cross-builds, package checks and affected checks passed:
1,239 parent tests with 40 skips, planner/whitespace and generated-consumer
consistency. Ten actual host-launch acceptance fixtures and twelve supervisor
failure fixtures passed. Injected tests remain separate from the physical
serial result; none establishes active peripheral or core acceptance.

Programming ran at 6 MHz on the exact top JTAG cable. Its original 15 MHz
setting was restored and read back. A fresh read-only query confirmed
`4362C0DD`; UART clients are closed and USB permissions restored. The diagnostic,
spare card and continuing claim remain on the exclusive kit under the user's
permission. The original factory card remains safe outside the board. Other
kits and the JTAG daemon were untouched.

Next, integrate native SD/FAT32 with explicit device resume/stop ownership,
then verified package and retained-ROM reads. The serial cold owner cannot be
silently reused after releasing a device reset. Active FPGA control and a
physical tenfoot launch/return cycle need their own qualified path.
