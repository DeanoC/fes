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
