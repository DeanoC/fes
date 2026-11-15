# Native keyboard return to launcher

The native Tenfoot keyboard adapter reserves Ctrl+Alt+Escape during play.
A fresh Escape press with either Ctrl and either Alt requests the existing App
Stop operation after neutralizing all core HID rows. The complete report must
validate and the current play must have observed a real all-up report first.
Ordinary typing, Escape alone, and modifiers added to an already held Escape
remain core input. One return is queued; subsequent reports wait for the owned
transition. Buffered input is discarded at generation/pause boundaries.

The direct keyboard API and gamepad route are unchanged. No firmware, FPGA,
register map, package contract, timeout, programming policy or driver capability
is broadened. Physical control and display acceptance remain open. The existing
passing queued-menu hardware image does not qualify this changed native code.

Focused race-enabled tests pass, including three actual Tenfoot App launch/USB
return cycles, four left/right Ctrl/Alt combinations, held-key fences, stale
input across relaunch and failed neutralization without normal return/retry.
All four new chord cases fail against the original input source as expected.
All seven affected checks pass, including parent regressions, generated-consumer
consistency and joined native firmware/FPGA/Tenfoot integration. The rebuilt
matched SDK/entry/BL31/SPL/FIT/SOF chain passes for native image
`ff0e7da217a21406`, compiled from `545f34022`. Both FPGA packages are unchanged
from the passing queued-menu trial. The FIT remains below the original8MiB limit.

The observer additionally accepts an explicitly selected physical Stop, with no
UART payload. Twelve action-observer tests, sixteen paired-controller tests and
all six affected checks pass; source, generation, ordering and complete firmware
closure remain mandatory. The first new negative test incorrectly removed a
reader-drain line absent from the modeled core return; its corrected mutations
remove the actual master-stop/USB-resume records instead. Physical acceptance
remains pending.
