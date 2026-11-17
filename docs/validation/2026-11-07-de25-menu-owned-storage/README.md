# Owned menu storage return and rearming, 2026-11-07

The native menu now retains the private storage handoff from its completed
configuration transfer. After a frame and independent secure closure, it checks
live ownership and the complete pinned arena before resuming the original store
once. A fresh SD read and containment can supply the existing configuration plan.
That plan verifies private store identity and return/SD generations, then checks
memory and handoff again before START. The completed transfer opens the next menu
on the same retained two-slot arena; no framebuffer memory is released.

The software integration passes three fresh cycles with closures at generations
7, 13 and 19. It rejects foreign stores even when public receipts match, stale,
copied and busy handles, missing/failed returns, changed owners/sources, corrupt
visible/padding bytes, uncertain SD grants, stale receipts, reservation and
configuration failures, changed state before START and failed opening/closure.
Failures cannot retry a consumed transition or release retained memory.

All 1,638 parent checks passed with three expected skips, generated consumers
matched, and native C/RTL/Go/real-Tenfoot integration passed. The Nano SDK also
cross-builds the committed implementation. [result.json](result.json) binds the
source and evidence hashes. Initial focused compilation failed on an unused
import; that was fixed before the passing focused and complete checks.

The new menu cycle uses injected firmware/SD/CSR responses in its host tests.
The broader suite's actual patched C/RTL execution does not establish the joined
menu secure-provider SD-return path. The SDK still selects the original cold
menu command, with storage return disabled. There is no new native firmware
selection, board attempt, physical HDMI, timing or ordinary core-load acceptance.
The preceding buffered-menu board evidence is retained separately and applies
to its exact earlier source and matched chain.

Next connect the menu and computer roles to the existing ProgramLoader and
ProgramService, select their matched firmware SD return, and use the real Tenfoot
application launch/return path. Additional fixed-menu repeats cannot substitute
for menu/core/menu acceptance.
