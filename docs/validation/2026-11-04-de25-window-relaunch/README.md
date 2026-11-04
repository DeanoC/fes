# DE25 stored window reconfiguration and return contract

Source `134c80eb427e23f735a5d96f7e86daef61d2b351` prototypes repeated native window loads after a
verified return and a fresh SD cycle. The real secure dispatcher and both reader
RTL models pass two and three configurations with a new frame and new closure
for each. Native Go tests join the retained package/store, DMA transfer, frame
session, SD resumption and next transfer under the race detector.

This is software and cross-build evidence. No new board attempt was made. The
native repeated-load caller is not wired into the frontend or persistent
request service yet. The existing one-window native image cross-compiles, and
the new strong EL3 rearm gate is selected in its matched firmware. There is no
new repeated-load hardware acceptance or ordinary programming profile.

`WindowSession.NewStoredReconfigurationTransfer` consumes only its own
successful Close and completed SD resumption. The next plan must use a fresh
selection from the original private store, with the exact returned owner and
closure generation. It rechecks the whole pinned frame arena before reservation
and before START. Copies, stale handles, changed source, changed SD receipts,
damaged frame/padding, failed reservation, uncertain transfer/open/close and
replayed grants reject without releasing the frame arena.

The secure dispatcher captures its own complete return plus stopped SD receipt
before configuration changes those records. While reconfiguring it permits only
the existing configuration operations, context and read-only snapshot. A new
opening requires completed configuration with the captured owner/generation.
Completion rechecks the old closure and SD owner, epoch, return, grant and stop
records, replaces the master-stop state and invalidates the preceding closure.
A failed activation cannot reuse earlier reader/probe receipts. No wire schema,
MMIO address, master inventory, physical timeout or allocation release is added.

Sixteen new C/RTL scenarios include two/three successful loads, wrong owner and
stale generation, active SD, changed return provenance/live SD scope, damaged
core bytes, failed START, changed closure or SD records at completion, a new
reader drain failure, a denied data gate and early/replayed control requests.
SDM/card operations are modeled; the dispatcher, secure provider, reader and
independent probe logic are real source. Hardware models reset only after new
configuration completion; secure state remains live. The preexisting default
without the explicit SD-return capability continues to reject rearming.

The first software run caught an incorrect interpretation of the SD controller's
internal grant generation. It records the source generation before advancing;
the new check was corrected to that authoritative value. The next run exposed a
single-configuration assumption in the mailbox test: the assertion now compares
CSR reads with the baseline captured for each configuration, using the same
existing epoch checker as the computer tests. Both failed software runs and
subsequent passing evidence remain retained. No board retry occurred.

All eleven affected repository commands pass. The fresh native SDK, entry and
secure firmware cross-builds pass. Actual AArch64 disassembly confirms calls to
the strong platform return/SD/rearm gates, and copied producer sources match the
qualified commit. The native repeat method is exercised by Go tests; it is not
selected by the current single-window frontend. [Result](result.json),
[software execution](software.json) and [ARM binary proof](arm-binary.json)
retain these evidence boundaries.

The unchanged FPGA producer is `df234b524d0e1340b343df411c841bd1dc553817`; no Quartus compile
or FPGA deployment was performed. TRWU8CBL stays parked on the separately tested
`d0241f1c6` one-window SD-return diagnostic, with its arena pinned until reboot,
bridges closed, SD contained, exclusive claim and spare card retained. Original
15M JTAG settings/USB permissions were restored and UART was free at that prior
handoff. No new hardware access is claimed by this software record.

Next, connect this private operation to the native request service/frontend,
then prepare a matched, independently bounded two-launch hardware test. Normal
Tenfoot launch/return, ordinary package/ROM/parts/media admission, physical HDMI,
USB hub input and native Ethernet remain open. The full DE10-like loading goal
is active.

Archive `/home/deano/fes-retained/de25-window-relaunch-20261104/software-and-firmware.tar.gz` has SHA-256
`279c744965a4b688a01f1a4988363d991e710d36767b1f298603a349780023f5`. All 7,936 archive
members were read back. The standalone source bundle restores without alternates
and passes strict fsck. Expanded files remain available.
