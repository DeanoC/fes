# Returned-menu display observation

The [result](hardware-result.json) separates a stopped automated test from a
successful manual return. The collector verified recovery, all 13 uploads and
fresh SD readbacks, native cold boot, initial launcher and ZX81 plus tape.
It then stopped because no keyboard Stop appeared during its 60-second wait.
The planned returned-menu hold, ZX80 launch and final Park were not reached.
The original failed capture and its two-second passive tail remain unchanged.

The operator reported possibly mispressing, confirmed another key still worked
in ZX81, then reported Ctrl+Alt+Escape returned to the launcher. This occurred
outside the stopped capture; it does not turn that attempt into a pass or prove
why the first action was missing. The operator subsequently confirmed the
launcher remained visible throughout an independent one-minute observation.

Two separate top-only serial observations used **O_RDONLY** descriptors and
sent no serial commands, firmware calls, FPGA programming or SD writes. The
15-second capture contains menu frames 158–169. The 60.002-second capture
contains 50 consecutive frames 349–398. Their menu/build identity and source
hashes match the selected SDK, owner `bccd55323262a6e3`, menu 2/source generation
15, slot alternation and zero underflows. Initial/trailing capture fragments
remain preserved. These snapshots establish ongoing matching menu-frame
activity, without reconstructing the unobserved opening/return transition.

Independent cleanup verifies UART release, original termios restoration,
unchanged exact USB permissions and the earlier owned-server/clock cleanup.
The failed attempt's full 5,939-file closure and original source have archive
readback/mode and independent source import/fsck/pack verification at
`/home/deano/fes-retained/de25-menu-visible-hold-hardware-20261110-r1`.
The follow-up is separate from that immutable archive.

The kit is currently in its active returned menu, with a running DDR reader:
observed bridge generation 17/reset mask 5/masters-contained false. It is
**not parked or contained** by host detachment. The original failed attempt's
programming grants remain consumed; the follow-up replayed none. Visible
launcher return is now positively observed on this native image. Reliable first
shortcut delivery, the complete new five-exchange test and ordinary installation
remain unproven. The following records the original prepared procedure.

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
