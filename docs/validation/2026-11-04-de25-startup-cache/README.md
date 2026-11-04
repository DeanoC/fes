# Stopped native startup/cache diagnostic on TRWU8CBL

One bounded attempt on `96d202a9aa5156d42270e1be90030874ae6dd6e7` passes the loader's
instruction-cache and clean-handoff checks and reaches native Go initialization,
`main`, SD open and FAT mount. It then stops at the unchanged 100-second
SD-package stage deadline, after the package-retention entry checkpoint. No
package-retention completion, frame, master-stop or final closure receipt is
accepted. This physical attempt **failed**; it has no retry.

The CPU counter locates the startup cost. These inclusive intervals also count
UART trace output; inflation and checksum costs have not yet been separated.

| Interval | Seconds |
| --- | ---: |
| Complete gzip decode and checksum | 78.244721 |
| ELF plan, segment copy and BSS zero | 13.149892 |
| Loader start to clean EL1 handoff | 91.412083 |
| Handoff to Go main | 0.049677 |
| SD open | 0.040200 |
| SD open completed to FAT mounted | 0.031412 |
| Loader start to package-retention begin | 91.562117 |

The trace verifies EL2 with translation and both caches disabled on entry,
instruction caching alone during decode/copy, the same clean EL1 handoff, and
EL1 with the pinned runtime's MMU/caches enabled at both early Go checkpoints.
The frequency stays 400 MHz and counters increase. Thirteen of fourteen
startup checkpoints are present. Instruction caching alone is insufficient to
bring CPU preparation plus package retention inside the existing stage limit.
Go reaches SD and FAT rapidly after the handoff; there is no observed Go startup
hang. The trace does not establish the later SD outcome or distinguish the
inflater's time from checksum processing.

All seven affected software checks pass on the exact source. Nine actual QEMU
entry cases verify successful Go execution and malformed-image rejection,
including clean cache state before parking. The complete Tenfoot model
launch/return/failure suite also passes through this loader. Native cross-build,
actual startup formatter/register instructions, strong firmware symbols, pinned
vendor startup and matched SPL/BL31/FIT composition pass their checks. These
software results supply no physical core-load acceptance.

The complete native ELF is 14,921,903 bytes,
the entry is 7,198,065 bytes, and the FIT is
7,742,820 bytes, below the unchanged 8 MiB limit.
The decoder still uses the existing 32 MiB owned workspace at `0x91fe0000`.
The conservative entry-stack bound is 5,424 bytes
against the existing 16 KiB allocation. Payload/ELF bounds and Go metadata
remain intact. The FPGA producer stays `df234b524d0e1340b343df411c841bd1dc553817`;
no FPGA compile or core-payload change was made.

Recovery, both Ethernet uploads and fresh SD readbacks pass before the new
native boot. An initial read-only host preflight exceeded an incorrectly chosen
120-second host budget, before any board operation. The same frozen image,
checks and inventory then passed with a 300-second host budget. No device,
firmware, memory or hardware deadline was extended: SD100, configuration20,
transfer/read/return45, native240 and phase270 remain unchanged.

The [complete capture](native-console.log) independently reproduces the native
receipt's incomplete EL1 ownership prefix and the separate `retain_begin`
startup checkpoint: [replay](replay.json), [CPU trace](startup.json). Independent
host cleanup verifies original `15M` readback, exact USB permissions, stopped
private JTAG and free UART. A separate 45-second passive serial follow-up
receives zero bytes and sends no commands. Final bridge state remains
**unknown** and memory remains retained until reboot. The exclusive claim and
spare card remain; the factory card is safe outside. No factory restoration was
performed, as requested.

All four earlier failed window/startup procedures and the prior successful
`26dbec9ce` twelve-frame chain stay separately retained. The current stopped
chain and fresh software evidence are in `/home/deano/fes-retained/de25-startup-cache-20261104-stopped/stopped-native-chain.tar.gz`
(SHA-256 `e745a42188ab34488445142f682055acf8f864cb5dd94b82763e331cde647002`). All 45,820
archive members read back against their manifest. Its standalone source bundle
restores without alternates and passes strict fsck. Expanded files remain.

Next separate inflate/checksum timing and reduce decoding/checksum/copy cost
while preserving the existing memory and hardware bounds. Aligned bulk copies
and the CPUs' checked CRC capability are candidates to evaluate. This attempt
is not repeated. The full objective remains ordinary native core admission,
ROM/parts/media loading, the running Tenfoot loop, launch/return recovery, HDMI,
hub input and native networking; those gates remain open. The
[result](result.json) retains the exact evidence boundaries.
