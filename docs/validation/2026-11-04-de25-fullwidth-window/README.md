# DE25 full-width native frame read

TRWU8CBL passes one complete native tenfoot frame read through the new
256-bit HPS DDR boundary, followed by reader/master drain and bridge closure.
The procedure ran from 2026-11-03 22:06:15 to 22:14:43 UTC under the continuing
exclusive top-board claim. This is a bounded physical diagnostic.

FPGA producer `df234b524d0e1340b343df411c841bd1dc553817` supplies build ID
`7fa7e3c27ec2fb055e148514b8dac7a2`. Native Go, entry and firmware use
`74ef8736a1139f3de66e80337e6c6211fea6fa80`, with native boot ID
`d5aecf5deacf127c`. The intervening changes repair a host verifier's success
marker and its generated-handoff test fixture. The sealed FPGA package and
matched artifacts are identified in [the result](result.json).

The read adapter converts aligned pairs of 128-bit endpoint beats into
256-bit requests, halves ARLEN and delivers both response halves in order.
DISABLE preserves the buffered upper half until the endpoint consumes it;
elapsed time and an empty physical response channel supply no drain receipt.
Five cases through the actual generated interconnect enforce 32-byte HPS
transfers: a full frame, held addresses, held responses and both abort paths.
The generated handoff passes, while 29 altered copies reject. All 11 planned
affected checks complete across their explicitly recorded source commits.

One qualified recovery boot precedes the two fixed Ethernet uploads and fresh
SD readbacks. The original recovery files remain unchanged. Native package
retention takes 62.489 seconds; FPGA configuration takes 13.610 seconds, within
the existing 100/20-second limits. Native code performs no SD write.

Both fresh full-window permission gates pass. The reader delivers all
3,686,400 bytes from the 4 MiB CPU reservation. CRC `0xf7429b0e` matches the
native publication, whose frame SHA-256 is
`a58b682bcf4749139141601be2b1f655c1019c627befc941a511c27b388a856d`.
Host models use separately rendered pixels; host/native pixel equivalence
remains unqualified.

Secure master stop succeeds with two reader polls, 132 reads, one DISABLE,
two inherited-probe polls and `complete=1`. Bridge closure finishes at
generation 7/reset `0xf`, with hardware drain mask `0x8` and quiescent producers.
Data continuously contained is `0`, because this operation releases DDR.
The optional fabric-warning acknowledgement remains `0` and unqualified.

The [complete raw capture](native-console.log) independently decodes to
`window_read_closed` and matches the native receipt. Removing publication,
permission, frame, master-stop or closure records, or changing the frame CRC,
rejects: [replay result](replay.json).

One full-width physical attempt passes, with zero retries. The earlier
narrow-width failure and its matched artifacts remain retained. Clock `15M`
is restored and read back; exact USB permissions, stopped private JTAG service,
free UART and continuing claim are independently verified. The spare and parked
diagnostic remain on the top board; its factory card stays safe outside it.

The 4 MiB reservation remains owned until reboot. Continuous presentation,
buffer reuse, HDMI, USB hub input, playable-core behavior on this changed FPGA,
native networking and ordinary package admission remain open.
Full artifacts and frozen operator evidence remain under
`/home/deano/fes/out/dev/de25-embedded-os/out/nano-fullwidth-20261103/`.
