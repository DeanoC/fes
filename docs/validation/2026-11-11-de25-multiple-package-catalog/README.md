# Multiple retained packages in the native catalog

Against base `9c5992274`, the native backend retains up to 32 admitted install
plans instead of one. Entry IDs and package digests are unique across those
plans. The standard Tenfoot source/core/package reference selects exactly one
plan, including when two versions have the same core ID. Installation stays
explicit and uses the existing owned storage/menu/network transitions.

A single aggregate allowance caps retained upload file content at 64 MiB.
`InstallPlan.ContentBytes` counts private content-derived destinations once.
This allowance excludes parsed archive/ROM allocations and is not a whole-heap
limit. Foreign, cancelled, duplicate, conflicting, non-Ready, full and over-budget
attachments refuse without replacing existing rows or issuing device IO.
Catalog setup and installed status additionally require the installed entry's
exact title and request; an entry ID occupied by another package does not count
as the retained package being installed.

The race-enabled integration run verifies two synthetic versions of one core
from independent HTTP exports. Both exports close before installation. The
second version installs first, setup selects the exact package, the other row
stays available, and first → second → first launch/return cycles restore the
menu. Further tests cover crossed references, duplicate package aliases, a real
33rd-plan capacity rejection, budget boundary accounting with small real plans,
and a conflicting installed request. The budget test adjusts accounting rather
than allocating 64 MiB of archives; it is not a heap stress test.

All eight affected software gates pass, including 1,935 parent regressions
(three skips), generated consumers, storage lifecycle tests and native lifecycle
integration. The latter uses actual patched TF-A C and Verilated FPGA RTL with
simulated bridges, memory and SDM; 115 top-level program tests pass. The AArch64
TamaGo SDK cross-build also passes. All 2,131 selected SDK source hashes and 470
integration source hashes match. Artifact identity and retention are recorded
in `result.json`.

No hardware action or matched boot-chain assembly occurred. Native startup
still reads one optional `FES/INSTALL.JSN` record; this change does not introduce
a multi-record boot source or a network listener. Native Ethernet stays disabled.
Feeding multiple staged records at startup, physical multi-package qualification,
native Ethernet qualification and full DE10 appliance parity remain open.
