# Queued menu source-bound board trial

Native candidate `a2bcd5ee46759986`, compiled from `21731d04e`, pairs the new
sealed menu package `385c0b50…`/build `87ebcfe5…` with its SDK, entry, BL31,
SPL/FIT and unchanged boot FPGA core. The menu holds one immutable submitted
descriptor until the video side can consume it. A second outstanding descriptor
rejects. The driver still submits once and uses the original bounds/ownership
checks; CPU slot reuse still needs the exact full-frame receipt.

The previous `833277b3abc25cab` completed the 917-byte tape in 1.205599140 s,
ZX81 launch/App return and ZX80 launch, then failed the menu-frame consistency
check on the second return. The last sampled receipt was still frame1 with
flags6 and no FPGA fault bit. Its exact physical cause remains unproven. The
ready/write timing mechanism is reproduced off-board with an explicit transport
response-handling assumption. That image and both grants are consumed/retired.

All nine selected checks pass, including 1,595 fresh FPGA tests, parent
regressions and native producer integration. The exact-source Quartus seal,
regular-file transfer, generated clock/reset/source checks and exact-project
100/74.25 MHz model pass. All constrained multicorner slacks are nonnegative.
Existing unwaived HDMI I2C findings remain: the physical output-pair count changes
from9 to10 because `SD_COUNTER[2]` is present as original and duplicate; full raw
paired netlist reports are retained. No new unconstrained clock or menu/DDR cone
was introduced. This is a bounded diagnostic, not full-board timing signoff.

The FIT is 8,309,400 bytes under the unchanged 8 MiB bound. New menu files come
from the matched SDK; computer packages, retained ROMs and tape fixture keep
their identities. This fresh single-use trial retains all 13 Ethernet uploads
and SD readbacks, seven FPGA configurations, three launches and four menus.
Only top TRWU8CBL is used, with its installed spare card; its original card
stays safe and factory restoration remains waived. No physical key action is
requested. Bottom TRWU8J8G is excluded.

Programming uses the qualified fixed3MHz/auto-adjust-off link; independent
cleanup must verify the original15MHz/auto-adjust, exact USB permissions, owned
server gone and UART free. Bounds remain native450s/outer800s/host890s and
initialReady100s. Tape retains bounded512-byte1s calls, size-derived total and
independent1s control/fetch. The first failure terminates the trial with only a
two-second passive tail. A consumed image is never replayed.

The hardware trial passes: all eight App commands independently replay from the
saved serial bytes. Seven FPGA configurations complete the sequence
ZX81 (+917-byte tape) → menu → ZX80 → menu → ZX81 (+same tape) → menu → Park.
Both tape transfers take about 1.206 seconds; each independent control/fetch
phase stays within one second. All four menu sessions report zero underflows.
The final service state is Parked at generation7, with firmware bridge generation40,
reset mask15 and masters contained. CPU parking and firmware return are unobserved.
Independent cleanup confirms15MHz/auto-adjust-on, exact USB permissions, no owned
programming server and a free UART. This consumed image is retired; no replay.

The first two offline replay attempts incorrectly checked the cached App view
and a nonexistent result field for final Park. The existing strict client records
that state in `exit_generation`; correcting those analysis assertions yields the
passing third replay. The raw trace and failed analysis logs are retained.

Ordinary core-loading/appliance acceptance, physical display/input, general USB
reliability and broader native network behavior remain open. One passing run
does not prove the previous menu failure's physical cause.
Old-file cleanup stays paused.
