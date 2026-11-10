# Returned-menu display observation

This is a prepared, single-attempt diagnostic for the negative HDMI observation
from the [preceding keyboard cycle](../2026-11-10-de25-staged-observer-trial/README.md).
The operator saw the initial launcher, but reports ZX81 remained on screen after
the keyboard Stop; the final parked screen was blank/no signal. The serial
cycle, keyboard action and menu frame receipts passed. Neither result is
replaced or promoted by this preparation.

The new procedure adds one passive **60-second hold after the returned menu
is fully verified**, before selecting ZX80. It sends no UART input during the
hold, checks unchanged Ready/owner/generation and fresh menu-frame progress,
and records the exact capture extent and monotonic start/end. The operator
observes the monitor during this interval. This separates a short automated
display window from an actual failure to recover visible output. It is a
diagnostic hypothesis, not a claimed HDMI repair.

All existing native operation bounds stay unchanged: initial Ready/launch 100
seconds, configuration 20, core close 5, menu restore 100 and final Park 5.
The physical keyboard wait remains 60 seconds. Native/outer/host limits remain
450/800/890 seconds. The separate observation hold fits within those limits;
it does not extend an operation or permit a replay after failure. A first
failure closes the attempt with its original two-second passive tail.

The same verified native bytes (`a02d808ea11eb54f`, compiled `db5fc2d66`) and
sealed computer/menu packages are selected. The checked history now carries
two prior native reservations/programmings, one observer-rejected capture,
one serial/control pass and its negative visible-return observation. New
transport, scans, one-use grants, recovery readbacks and full native cold proof
are mandatory. No old grant, live owner or capture is resumed.

The exact injected runner tests the passive hold, missing frame progress,
lost ownership and refusal while a core runs. All 49 operator-model tests
pass; an initial model run exposed a floating-point subtraction boundary in
the harness, repaired by comparing the original absolute deadline. No device
was accessed by either model run. Fresh file admission covers 5,673 files and
eight trees; top USB/UART identity is stable for 60 seconds. A committed exact
source live preflight must still recheck file hashes, continuing ownership,
selected tools, unique top transport and free UART before any operation.

The FPGA menu model now verifies three actual Tenfoot frames at the selected
HDMI wrapper's FPGA pins and rejects disconnected RGB/missing clock. Its PLL
and I2C are explicit stubs. The separate captured I2C model passes real startup,
NACK retry and interrupt/reset cases with its existing negative controls. All
eight affected software checks pass (1,694 selected parent cases plus their
ancillary check, 1,595 FPGA cases and native integration). These model results
neither reproduce the physical fault nor qualify the transmitter/monitor.

Only top TRWU8CBL and its installed spare card are selected; the factory card
stays protected, restoration waived and the bottom kit excluded. The shared
Quartus lock, qualified 3 MHz/automatic-adjustment-off transport, normal-user
vendor tools and exact clock/USB/server/UART cleanup remain required. Recovery
only performs the existing 13 verified uploads/fresh SD readbacks. Native
Ethernet and SD writes remain disabled. Old-output cleanup stays paused.
No visible return, ordinary core installation or whole-board acceptance is
predeclared.
