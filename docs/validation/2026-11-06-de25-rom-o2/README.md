# Shared-ROM ownership compilation

The preceding unprofiled hardware trial confirmed 7,408/8,192 ROM bytes before
the unchanged deadline. Removing detailed profiling improved progress by about
5 percent, but did not complete loading. Its
[physical receipt](../2026-11-06-de25-rom-unprofiled/physical-result.json) and
verified archive preserve the stopped attempt and cleanup.

Select per-object `-O2` for the eight ownership/transport objects in the existing
shared-USB held-ROM profile. The vendor currently compiles them for size with
`-Os`. The generated Make rules append the speed setting only to those exact
BL31 objects; the receipt records the selection and rules. The C sources,
cache controls, full cold observations, private callbacks, 16-byte frames,
per-access checks, deadline and polling bounds remain identical.

An effective GNU Make test checks selected/unselected objects and profiles.
The scoped host compiler harness runs all 29 targeted tests, including 47
ownership cases and 82 joined 4/8 KiB step cases. Both C/RTL paths pass their
23 phases. An initial overbroad test wrapper also changed compilation of an
unselected SPL fixture, causing a longjmp clobber warning. That failed log is
retained; the corrected wrapper keeps that fixture's original flags. Production
startup was never selected by the new rule. Exact inputs, unchanged runtime C
hashes and results are in the [test receipt](result.json).

Next rebuild the matched ARM chain, inspect effective flags, code and size,
and complete changed-file checks before preparing any hardware test. This
source change supplies no physical speedup, ROM completion, CPU Run, ordinary
package admission or DE10-equivalent readiness.

The matched SDK, entry, BL31 and U-Boot rebuilt from
`ca2d6edf07f0a5ba081a2d98dc9b7d89e0c93fc5`. A private verbose rebuild produces
identical loadable BL31 bytes and verifies all 131 C compile commands: exactly
eight objects use O2, the independent SHA object retains O3, and other C objects
retain Os. BL31 grew from 118,845 to 127,037 bytes and links inside the existing
limits. Actual source/call-path checks, both-ROM preflight, USB/native package
tests and the complete changed-file suite pass. The
[matched receipt](matched-build-result.json) pins the evidence.

The first host bootloader build stopped at its 300-second cap during SPL
compilation without a compiler error. Its files/log remain intact. A fresh
private output passed in 345 seconds with a 480-second host allowance; native
bounds did not change. A premature read-only linked-image check rejected the
then-missing conversion receipt; the terminal conversion and subsequent check
pass. Both stopped host checks remain in the evidence.

The [frozen bounded operator](operator-source.json) permits one attempt of the
new compiled firmware, retaining the prior programmer/setup stops and three
ROM-deadline trials. It uses the tested 6 MHz programming link and restores
15 MHz and exact host permissions. No O2 hardware attempt has yet occurred.

The single matched O2 hardware attempt stopped at the unchanged deadline after
400018683 ticks (1.0000467075 seconds). The final direct WRITTEN sample was
7,824/8,192 bytes, versus 7,408 for the unprofiled size-optimized predecessor.
Recovery, eight uploads/fresh SD hashes, native boot, USB pause and sealed
FPGA configuration passed. No ROM completion, CPU Run or return was accepted;
no retry followed. The [physical receipt](physical-result.json) retains the raw
line and independent original-15-MHz/USB/private-server/UART cleanup.
The claim and test card remain retained; the factory card is untouched.

Next inspect the pinned vendor firmware memory layout for a supported private
on-chip RAM placement, without weakening ownership observations or increasing
frames, deadlines or polling bounds. This is a proposed investigation, not
permission to use an unverified address range or a hardware qualification.
