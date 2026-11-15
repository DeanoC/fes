# Launcher and first core pass their native milestones; observer stops the trial

The single TRWU8CBL attempt on operator source `03c947d15`, with native producer
`f8653867f` and boot ID `aa11ddf99761b275`, stopped on a host trace-ordering
rejection. The original trial remains **failed**. Its 343-second host job did
not time out, and this procedure is consumed.

[hardware-result.json](hardware-result.json) binds the raw records and checks.
The complete capture remains in `out/nano-menu-usb-board-20261108/trial`.

Qualified recovery, twelve Ethernet writes/readbacks and twelve fresh final reads
passed. Native cold boot and actual shared-gamepad readiness passed. The App
reached complete two-title Ready and the menu acknowledged four consecutive
frames, alternating both slots with zero underflows. This passes the blocking
menu interval where the previous native image's first USB packet expired.

The operator sent one Enter. USB paused at Ready generation 0; the menu closed
through independent secure drain and bridge generation 7/reset `f`. The selected
ZX81 package configured and opened, USB resumed at Running generation 1, and the
actual held/released fetch observation reported an advancing core counter. The
App then published an active ZX81 session with the exact selected ROM receipt.
The launch response arrived within 54.1 seconds, inside its original 100 seconds.

The paired checker requires USB resume to follow the printed fetch observation.
Actual captured order within that response is:

1. USB pause: line 2.
2. Firmware ownership work: lines 3 through 36.
3. USB resume: line 37.
4. Fetch diagnostic: line 38, followed by Running/active App state.

The native backend completes `withInputPaused` before subsequent frontend
diagnostics. The fetch line is an observation, not the boundary for completing
firmware ownership work. The original checker rejection reproduces off-board
using the exact captured receive windows and elapsed times. Independent existing
menu-close, configuration, core-opening, identity and fetch checks pass. No
decoder was changed or failure suppressed to obtain these component results.

The checker stopped before Stop, Quit, ZX80 or a restored menu was tested. The
last captured native state is Running; there is no verified native Park or later
board-state observation. The continuing top-board claim remains held. No further
UART/JTAG commands or factory restoration were performed after the stop.

Independent host cleanup passes: exact original USB permissions, recorded 15M
clock readback, owned private server gone, UART free. Host cleanup does not prove
native shutdown. The bottom kit was excluded.

Next repair the launch observer's boundary and its synthetic ordering model,
regress the real captured response and premature/reordered resume failures, then
prepare a separate bounded full-cycle procedure. Preserve this failed attempt
and its original native image. Physical HDMI/button acceptance, full return,
ordinary admission and DE10-equivalent readiness remain unproven.
