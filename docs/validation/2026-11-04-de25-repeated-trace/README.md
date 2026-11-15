# Stopped DE25 timed repeated-frame diagnostic

TRWU8CBL completes nine changing native tenfoot frames with matching FPGA CRCs
and local reader drains. The unchanged 45-second transfer/read/return bound
expires during presentation of frame ten. This attempt is **failed**: no final
master stop, bridge closure, HDMI or ordinary package admission is established.
The previous stopped attempt remains retained separately.

Native Go, entry and firmware use `c814590156ec6f6f8a5b8ed33e881f17e09743f9`. The reused
FPGA producer is `df234b524d0e1340b343df411c841bd1dc553817`, package `a2388c4d7d8640d6e70d51c4d164e3c803157e3fb21e91451d6febd74faf6017` /
build `7fa7e3c27ec2fb055e148514b8dac7a2`. Native boot ID is `9e73f40215b6eba5`. Seven affected
checks and the matched boot chain pass on that native source. The complete FIT
remains inside its unchanged 8 MiB limit.

Both Ethernet uploads and fresh SD readbacks pass. Native package retention
takes 62.510 seconds. Each frame delivers all
3,686,400 bytes, then acknowledges its newer reader generation and drain cookie.
Reader generations and drain cookies advance from 2 through 10. All nine frame
hashes and CRCs change, and each FPGA CRC matches its CPU publication.

CPU-only progress points distinguish rendering from presentation. Rendering
averages 15.065 ms, while presentation averages
2.958 seconds. Presentation includes full-arena
checks, CPU copy/publication, rearming, readback and local drain; these numbers
do not assign the cost to an individual phase. The last point is frame ten's
`present_begin` at 26906907 microseconds. See
[timings](timing.json), [raw capture](native-console.log) and the independent
[incomplete-capture replay](replay.json).

The native helper now reuses its fully checked candidate tuple within that
launch instead of performing the same validation twice. Exact source, content,
ownership and device-bound guards remain. No shared validation cache or longer
hardware bound is introduced. The 270/240/45-second limits are unchanged.

A separate model executes twelve complete reads, drains and rearms through the
same frozen generated FPGA interconnect with a mocked HPS. All frame CRCs and
44,236,800 delivered bytes match. It excludes the CPU renderer and physical DDR;
it supplies no hardware acceptance. The native trace's nine completed frames
also distinguish this run from a second-frame rendering or rearm deadlock.

One later 45-second [passive serial observation](passive-tail.json) receives zero
bytes. It sends no commands and does not retry, reboot or reprogram the board.
The original failure stays failed. Final bridge state remains **unknown** and
the arena remains pinned until reboot.

Independent host cleanup verifies clock `15M` readback, exact USB permissions,
stopped private JTAG and closed UART. The exclusive top-board claim and spare
card remain; the factory card stays safe outside it. This is one attempt and
one failure on this native source, with zero retries. Two prior failed window
procedures are retained.

The [result](result.json) identifies the matched artifacts and bounds. The
stopped chain and qualified recovery are retained at `/home/deano/fes-retained/de25-repeated-trace-20261104/stopped-native-chain.tar.gz`
(SHA-256 `50966f1925f3f6e15868d6e211edbbf3c58cc8f7bf0d915293a2ea0657ede079`). All 45,211 archive members
read back against the manifest. Its standalone source bundle restores without
alternates and passes strict fsck. Expanded files are preserved. The next step
is to reduce presentation cost without removing checksum, ownership or drain
checks, then test a new source-bound image within the same limits.
