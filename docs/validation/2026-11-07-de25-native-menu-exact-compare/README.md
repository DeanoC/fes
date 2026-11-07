# Exact menu framebuffer comparison, 2026-11-07

The [CPU-feature board comparison](../2026-11-07-de25-native-cpu-feature-trial/README.md)
passes three menu frame acknowledgements and independent secure return, but
presentation still takes about 1.3 seconds. The next candidate removes work
from the full arena check without reducing its coverage.

`ReserveMenuMemory` already uses exact byte equality for its fresh 8 MiB arena.
`OpenMenuSession` and `checkArena` now use the same comparison against the
separate private expected image. A successful equality check covers every byte
of both slots and their padding. Mismatch rejects immediately. The previous
checks computed two hashes only to compare these already available images; no
frame digest was published by them. Source, package, map and initial publication
digests remain unchanged. The menu session no longer retains a 32 KiB hash
scratch field. Owner/generation checks, CPU store barriers, bounds, retirement,
independent master-stop/closure and retention until reboot are unchanged.

Fifty new corruption cases cover ten boundaries through five lifecycle phases:
opening, between frames, publication, after command/acknowledgement and return.
They include both pixel/padding areas, former scratch boundaries and the final
byte. Corruption prevents successful frame/return receipts, preserves memory,
completes independent containment where owned and rejects replay. Existing
positive, ownership, cancellation, concurrency and failure tests also pass.
The fresh native integration runs the actual prepared C firmware/control RTL
with injected platform state, plus real Tenfoot launch/return/failures. The
affected parent suite passes 1,638 tests with three existing skips. It uses the
same locally retained pinned TF-A source with its source identity validator
unchanged; actual command lines and logs are retained. The separate CPU crypto
model's selected inputs are verified unchanged and reused. This is software
evidence, not a new physical pass.

A fresh CPU-feature-enabled SDK, entry, BL31, SPL and HPS-first composition pass
the matched audit: 4,460 recorded inputs, actual firmware BSS end `0x5afc0`,
final SPL guard after trace and before ATF, rejection branch and private cookie
preserved. FPGA package/build/core bytes are unchanged. The FIT remains under
the unchanged 8 MiB limit. [result.json](result.json) records exact sources,
hashes and check receipts. The next distinct bounded top-kit attempt must pass
all frame, local-drain, independent-return and cleanup gates before its timings
can be compared with the saved control.

The continuing top-kit claim/spare remain held under the factory-restoration
waiver. The original card and bottom kit stay untouched. Physical HDMI, the
real App's menu/core/menu display ownership, ordinary package admission and
full platform/timing qualification remain open.
