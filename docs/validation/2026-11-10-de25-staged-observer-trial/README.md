# Bounded keyboard test after the host observer repair

The [serial/keyboard result](hardware-result.json) passes on top TRWU8CBL: initial
launcher → ZX81 plus 917-byte tape → physical USB Ctrl+Alt+Escape → launcher →
ZX80 → owned Quit → launcher/final Park. All five exchanges independently
replay from the saved wire with their original polling times. Five FPGA
configurations, two computer launches and three menu sessions complete.

The operator confirmed pressing/releasing the chord; the trace binds that
USB keyboard Stop to owner `bccd5532330f95cd`/logical generation 1 with fresh
report progress. It arrived after 52.80 seconds, inside the 60-second human
wait. Core/menu return and the second launch/return pass their existing bounds.
The native phase takes 354.65 seconds and the complete experiment 481.60.
Tape transfer takes 1.213 seconds, with independent control/fetch under 0.1.
The subsequent [operator observation](operator-display-observation.json)
contradicts visible HDMI return: **the screen stayed on ZX81 after the chord**.
Visible launcher return fails and must be repaired. Serial frame presentation
and control closure do not establish the physical HDMI picture.
The operator further confirms seeing the initial launcher, with a blank or
no-signal screen after the final Park. This narrows the unresolved observation
to return/display recovery rather than a menu image that never displays.

The subsequent [HDMI investigation](hdmi-investigation.json) extends the actual
menu model through the selected board wrapper to its FPGA HDMI pins. Three
Tenfoot frames pass; disconnected RGB and a missing pixel clock fail. The
pixel-route model uses explicit PLL/I2C stubs. The separate actual captured
I2C model also passes startup, NACK retry, interrupt reinitialization and reset,
with its original negative controls. All eight affected software checks pass.
These off-board results do not establish a hardware fault cause. The next
diagnostic holds the verified returned menu before the next configuration,
separating monitor recovery time from the short automated transition window.

Final logical Park is generation 5; the firmware menu-return observation has
bridge generation 29/reset mask 15 and masters contained. Firmware return and
CPU park were not observed. Independent cleanup passes exact USB permissions,
recorded 15 MHz/automatic-adjustment-1 readback, owned private server gone and
UART free. Programming failures remain historical two/new zero.

The full 5,923-file hardware closure has archive readback/mode checks and an
independently imported source bundle with strict fsck/pack verification at
`/home/deano/fes-retained/de25-staged-observer-trial-hardware-20261110-r1`.
Its source is `29b61b9ea`; the immutable archive records HDMI confirmation as
pending because the answer arrived afterward. The later negative observation
is recorded separately here and does not rewrite the preserved capture.
The spare card stays installed, factory restoration remains waived, and the
top kit remains claimed. This bounded diagnostic does not establish ordinary
core installation, native Ethernet, writable native SD, broad USB/core support
or whole-board timing signoff. The previous failed attempt remains preserved.

The following records the frozen pre-test procedure.

This is a fresh, single-attempt procedure using the materially repaired host
observer from `a63d46d90`. It selects the unchanged matched native boot
`a02d808ea11eb54f`, compiled from `db5fc2d66`, on top TRWU8CBL only. The
previous attempt remains terminal and failed; none of its captures, cold-boot
proofs, scans or consumed grants can authorize a new device operation.

The earlier wording that retired the consumed image applies to that attempt's
authority. Reusing the verified boot bytes here is explicit: no native or
firmware defect was established, and the host decoder rejected a legitimate
held selected-plan snapshot. This new procedure records one prior native
reservation, one completed programming and one failed host capture. It does
not reset those counts or add a producer nonce. A checked history contract
requires that exact failure, passing programming/cold proof, independent
cleanup, fully retained evidence and the tested decoder repair. A different
failure, failed programming, changed native bytes or missing repair rejects.

All 44 operator-model tests pass, including the actual injected serial runner,
physical keyboard Stop without a UART substitute, firmware/RAM/MMU decoders,
stepped receipts, clock restoration, fresh programming policy and history
mutations. The earlier repair also passes 48 focused tests and all six parent
checks with 1,851 regressions. Fresh file admission covers 5,669 files/eight
trees; the top USB/UART identity is stable for 60 seconds. These are preparation
checks and grant no hardware authority. A committed exact-source live preflight
must independently recheck files, ownership, tools, unique top transport and a
free UART before the attempt.

The bounded sequence remains menu → ZX81 plus the 917-byte tape → physical
Ctrl+Alt+Escape → menu → ZX80 → owned Quit → menu/final Park. Five FPGA
configurations and three menu sessions must join exact package/ROM/tape and
ownership evidence. The physical Stop must have fresh USB report progress
and its original logical generation; no UART replacement is permitted.
The operator confirms availability at the kit and receives a separate signal
only after the verified first launch. Visible transitions require a separate
operator observation.

Bounds remain human wait 60 seconds, core close 5, menu restore 100, initial
Ready 100, native 450, outer 800 and host 890. First failure closes the attempt
with a two-second passive tail and no retry. Every hardware operation uses a
new private top-only transport, fresh scan and new one-use grant under the
shared Quartus lock, fixed 3 MHz/automatic-adjustment-off, then restores the
original 15 MHz/automatic-adjustment-on and exact USB permissions. Vendor
tools run as deano. Historical programming failures remain two, with none
since the qualified transport repair.

Only the installed spare card is used; the factory card stays protected and
restoration remains waived. Recovery performs the existing 13 fixed Ethernet
uploads and fresh SD readbacks. Native Ethernet and native SD writes stay
disabled, and preferences remain volatile. The bottom kit is excluded.
Old-output cleanup remains paused. No pass, containment, visible return or
ordinary appliance acceptance is predeclared.
