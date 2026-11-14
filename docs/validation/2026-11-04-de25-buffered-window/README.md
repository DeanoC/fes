# DE25 owned repeated native frame read and return

TRWU8CBL passes twelve changing tenfoot frames through the native owned
presenter and the qualified 256-bit HPS DDR read path. Every frame completes a
matching reader drain before the CPU updates that same pinned arena. The final
secure master stop and bridge closure complete. This is a bounded physical
diagnostic; HDMI and ordinary package admission remain open.

Native Go, entry and firmware use `26dbec9cefcbbce6cff5bcd44076608a9e4ee404`. The unchanged FPGA producer is
`df234b524d0e1340b343df411c841bd1dc553817`, package `a2388c4d7d8640d6e70d51c4d164e3c803157e3fb21e91451d6febd74faf6017` and build
`7fa7e3c27ec2fb055e148514b8dac7a2`. Native boot ID is `0add3667ab08c90f`.
The [result](result.json) records exact source, artifact hashes and bounds.

The fixture supplies twelve changing view snapshots to the actual tenfoot
renderer. It does not select the UI's network, scripting or launch command
handlers. The first implementation linked those handlers and exceeded the
existing boot-file bound; its artifacts remain retained. The final native ELF
is 7,397,551 bytes and its complete FIT is
7,964,660 bytes, below the unchanged 8 MiB limit.

Full-arena hashes now read through private 32 KiB CPU scratch storage. Covered
bytes, expected SHA/CRC values, ownership checks, stores and drains remain.
Mean presentation is 2.074 seconds, compared with 2.958 seconds across nine
completed frames on the prior stopped source. That prior attempt remains
failed. The 45/240/270-second device/observer limits are unchanged.

All seven affected commands pass on the exact native source. Strong firmware and
actual RTL tests cover ownership, return and failure handling. Helper tests
cover 26 upload cases, 99 altered receipts, 59 controller cases and ten native
decoder cases. The five prior generated-interconnect model cases apply to
the unchanged FPGA; they do not qualify the new native executable.

One qualified recovery boot precedes two Ethernet uploads and fresh SD
readbacks. Protected recovery files remain unchanged. Native package retention
takes 62.430 seconds and configuration
13.610 seconds, within their 100/20-second
bounds. Native code sends no SD write.

The 4 MiB arena contains one 3,686,400-byte XRGB frame. Reader generations
advance from 2 through 13, with a fresh frame cookie of 1. Each local drain
acknowledges its exact generation and monotonically increasing cookie. All
twelve publication hashes and CRCs change; each FPGA CRC matches its CPU
publication. These are native observations; host/native pixel identity is
not claimed.

| Frame | Reader generation | Publication and read CRC | Drain cookie |
| --- | --- | --- | --- |
| 1 | 2 | `f7429b0e` | 2 |
| 2 | 3 | `faee45f1` | 3 |
| 3 | 4 | `07a3664a` | 4 |
| 4 | 5 | `354fe736` | 5 |
| 5 | 6 | `970f6c57` | 6 |
| 6 | 7 | `3de8d6d2` | 7 |
| 7 | 8 | `3e2bb3a2` | 8 |
| 8 | 9 | `ea804d14` | 9 |
| 9 | 10 | `9c3e09e3` | 10 |
| 10 | 11 | `e6242bf3` | 11 |
| 11 | 12 | `210c99f5` | 12 |
| 12 | 13 | `e2578030` | 13 |

Final frame SHA-256 is `0c3993f08059326535cd43fe2643f8abe26f36e7eefaf6057f390fb8abe952d5`. Secure firmware independently
confirms reader/request/ack generation 13 and cookie 13, status `0x4` and zero
pending work from its validated cached observation. It adds no diagnostic
register read. Master stop completes with one reader poll, 88 reads and zero
extra DISABLE commands. Probe stop takes two polls with zero cancellations.
Closure reaches generation 7/reset `0xf`, hardware drain `0x8` and quiescent
producers.
The optional fabric-warning acknowledgement remains zero and unqualified.

The [complete UART capture](native-console.log) independently replays to
`window_present_closed` and matches the native receipt. The replay rejects
20 captures with required records removed or
checksums/acknowledgements altered: [replay result](replay.json).

One physical attempt passes with zero retries. All three prior failed window procedures remain retained. Independent cleanup confirms clock `15M` readback, exact USB
permissions, stopped private JTAG and free UART. The exclusive claim and spare
card remain on the top board; the factory card remains safe outside it. Memory
stays pinned until reboot.

This qualifies bounded native buffer updates and acknowledged reader/firmware
return. Continuous scanout, HDMI output, hub input, native networking, general
package admission and the complete UI/core launch/return path still require
implementation and hardware acceptance. Full private evidence remains at
`/home/deano/fes/out/dev/de25-embedded-os/out/nano-present-buffered-20261104`.

The matched native chain and qualified recovery are retained at
`/home/deano/fes-retained/de25-buffered-window-20261104/matched-native-chain.tar.gz`
(SHA-256 `b6beee6c35c310ea101596b2db403cf4e9a7f64d3aa9e0150fa86b6bf4946993`). All
45195 archive members read back against their manifest. The standalone
source bundle restores without alternates and passes strict fsck. Expanded
working files remain preserved. Archive verification makes no additional
hardware claim.
