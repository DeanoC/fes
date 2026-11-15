# Persistent native window service: three-session board diagnostic

TRWU8CBL passed one bounded procedure on native/firmware
`dc2cf47604ce003744ce2b1cf0e1e53beb1e4738` and observer
`f7e4a5bc3f041f83479c64ef9e585629bc6df53e`, with unchanged FPGA producer
`df234b524d0e1340b343df411c841bd1dc553817`. The explicit
`--window-service` image uses the real Tenfoot renderer and retained native
archive/transfer/window layers. It is still a fixed-package diagnostic,
not ordinary admission or the complete Tenfoot App/backend.

Nineteen exchanges cover startup status, three changing presentations, two
fresh SD/package relaunches, two returns, six expected stale-request rejections
and terminal Park. Each launch presents its first complete frame before
publishing the handle; the explicit presentation adds the second. All six
3,686,400-byte frames have matching CPU/FPGA CRCs and local reader drains.
Firmware independently confirms master stop and bridge closure at generations
7, 13 and 19, final reader generation/cookie 3, pending zero and reset mask 15.
The first two returns resume SD at generations 9 and 15; relaunch contains it
at 10 and 16. Final Park leaves SD contained and the frame arena pinned.

Initial native service startup took 77.720s (100s bound).
The fresh relaunches took 75.330s and 75.310s (100s each).
Presentations took 2.120s, 2.169s, 2.137s; returns/Park took
0.915s, 0.918s, 0.565s, each under the existing 5s control
limit. Configurations retain their 20s bound. The procedure inherits the
[accepted persistent request-service budgets](../2026-11-03-de25-service-hardware/README.md):
native 450s, native process 480s, outer 800s and wrapper 900s. These are the
request-service budgets; no deadline was extended after a failure.

Before opening a device, preflight proved that every compiled native input was
identical to the retained image source. The observer checkout differs only by
explicitly frozen host/docs changes. The top-only private JTAG service, qualified
recovery and two fixed-file Ethernet uploads/readbacks were retained. No native
SD writes, flash changes, recovery upgrade or factory restoration occurred.
The user's original card remains safe outside the board.

All sixteen CPU startup checkpoints passed. Independent replay reproduced all
nineteen command windows and rejected 44 damaged
captures, including removed configuration activations, master/drain/closure
records, repeated replies and borrowed reader drains. It also rejects each
missing CPU startup checkpoint. Replay opens no devices.

Host cleanup verifies the original 15 MHz JTAG rate, exact USB permissions,
stopped private server and free UART. The board is parked with SD contained;
its continuing exclusive claim and spare card remain held, and memory stays
pinned until reboot. Seven earlier failed window procedures remain separately
retained. This image had one physical procedure and zero retries.

The archive contains the exact source bundle, matched native chain, command
windows and raw trace. Every member was read back and the source bundle restored
without alternates and passed strict fsck. The retainer then hit a final
metadata-filename permission error after verification. A separate host-only
finalizer checked retained checksums and corrected that ownership; it neither
rebuilt the archive nor reran the board. Both logs remain retained, and
`retention-finalized.json` records the correction.

Next is the complete Tenfoot App/backend connection. Ordinary package/ROM/
parts/media admission, physical HDMI, USB hub input and native networking remain
open. Neither this diagnostic nor its retained image establishes DE10-style
ordinary core-loading readiness.
