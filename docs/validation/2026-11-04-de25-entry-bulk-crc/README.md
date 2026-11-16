# Stopped aligned-copy and CRC startup diagnostic

One bounded attempt on `b1965e3e8caed5a8b17d84e0a063a8588ec97703` reduces native CPU
preparation from 91.412083 to 63.963775 seconds. The
complete Tenfoot command handler and Go runtime metadata remain in the same
14,921,903-byte ELF. The attempt still **fails** at the unchanged 100-second
SD-package stage deadline. Go initialization, main, SD open and FAT mount are
observed; retention begins but no completion, frame or final return is accepted.
There is one attempt on this source and no retry.

| CPU interval | Seconds |
| --- | ---: |
| Inflation | 62.316562 |
| Full IEEE checksum | 0.670444 |
| Complete decode/checksum interval | 62.995831 |
| ELF validation/copy/BSS zero | 0.950475 |
| Loader start to clean handoff | 63.963775 |
| Clean handoff to Go main | 0.049683 |
| SD open | 0.040191 |
| Loader start to package-retention begin | 64.113821 |

These inclusive 400 MHz CPU-counter intervals include UART trace output. The
preceding source did not sample inflation and CRC separately, so its CRC cost
cannot be compared directly. Copying previously took 13.149892 seconds. The
handoff interval is about 30.0% shorter
in this pair of captures; this is not a statistical performance qualification.
Inflation is now the dominant measured CPU cost. Its translated/data-cached
alternative has not been implemented or qualified by this test.

The new target checksum checks `ID_AA64ISAR0_EL1.CRC32` before decoding and uses
IEEE CRC32B/X with naturally aligned eight-byte loads. Unsupported feature
values reject before decoding. The host version is a portable test oracle.
ELF copies and BSS clearing use naturally aligned eight-byte accesses and exact
byte edges. Complete ELF validation still precedes all segment writes. The
loader still enables instruction caching alone, with translation and data
caching off, and restores the original clean handoff before Go.

All six checks selected by the affected planner pass. Seventeen focused tests
cover copy/clear bounds, alignment, checksum equivalence and decoder mutation
under sanitizers. Eleven QEMU guests pass. They include actual ARM target
copy/clear and CRC execution over 16,512 and 2,064 boundary cases, respectively,
plus an injected missing-feature read that exercises real early rejection. The
other rejection cases and native Go probe retain their existing bounds. These
are emulator results, not physical absent-feature or board qualification.

The complete model launch/return/failure suite passes through the changed entry.
Its retained model ELF has older VCS metadata; every selected source byte
matches the current commit. The final Nano SDK is rebuilt on the current
commit. All 2,039 selected native Go/board/module source files are byte-identical
to the preceding SDK. Actual native disassembly verifies the feature guard,
CRC32B/X, aligned loads/stores, bounded startup formatter and clean handoff.
Strong firmware/vendor-startup and matched SPL/BL31/FIT checks pass. The FPGA
producer remains `df234b524d0e1340b343df411c841bd1dc553817` with its unchanged payload;
no FPGA compile was made.

The entry is 7,199,137 bytes and its FIT is
7,743,892 bytes, below the unchanged 8 MiB limit.
The existing 32 MiB decode workspace remains at `0x91fe0000`; the conservative
entry-stack bound is 5,488 bytes against 16 KiB.
Source, ELF, staging, ownership, stop/drain and device limits remain unchanged.
A first read-only host preflight caught an obsolete seven-index expectation;
the final check verifies the complete nonempty six-command plan and every
contiguous index. No board operation occurred before that passed.

Recovery, both Ethernet uploads and fresh SD readbacks pass before native boot.
The [full capture](native-console.log) independently reproduces both the
incomplete native ownership prefix and the fifteen-of-sixteen startup records:
[replay](replay.json), [CPU checkpoints](startup.json). The final checkpoint is
`retain_begin`. SD100, configuration20, transfer/read/return45, native240,
phase270 and outer800 limits remain unchanged. Software passes do not admit
this failed physical attempt.

Independent host cleanup verifies `15M` readback, exact USB permissions, stopped
private JTAG and free UART. A separate 45-second passive follow-up receives zero
bytes and sends no commands. Final bridge state remains **unknown** and memory
is retained until reboot. The continuing exclusive claim and spare remain; the
factory card stays safe outside and no factory restoration was performed.
Five earlier failed procedures and the separate `26dbec9ce` twelve-frame pass
remain retained. No retry follows this stop.

The current stopped chain and software evidence are in `/home/deano/fes-retained/de25-entry-bulk-crc-20261104-stopped/stopped-native-chain.tar.gz`
(SHA-256 `6fdb1ad6856dd6becde6dc639b068fc76adb11bdf7b1fdb6a48e5ab2646cb63d`). All 45,628
archive members read back against the manifest. Its standalone source bundle
restores without alternates and passes strict fsck. Expanded files remain.
The prior stopped-chain pointer is `session/prior-startup-cache-retention.json`.

Next evaluate temporary data caching for explicitly CPU-owned decoder/loader
RAM, with checked mappings, cache maintenance and restoration of the clean
handoff. Keep the full payload and existing device bounds. Ordinary core
admission, ROM/parts/media handling, the native Tenfoot loop, physical launch/
return recovery, HDMI, hub input and native networking remain open. The
[result](result.json) preserves the full evidence boundaries.
