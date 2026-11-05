# ROM loading without per-call profiling

The matched private-direct trial confirmed 7,040 of 8,192 ROM bytes before its
one-second deadline. Its inner ownership check averaged 60.728 microseconds,
down from 94.051; full checks averaged 583.379 microseconds. The complete
[physical predecessor receipt](../2026-11-06-de25-rom-private-direct/physical-result.json)
and verified retained archive preserve that stopped attempt.

Remove detailed ownership cost counters and accumulators from the timed secure
ROM path. The previous instrumentation touched uncached diagnostic memory on
thousands of ownership and timer calls. The timer callback now reads CNTPCT
once and caches that sample. Each ownership call still reads and checks live
CNTFRQ; the initial accepted frequency is saved once, and a later mismatch
records the unexpected value. Reporting still uses cached failure phase,
scope, errors, byte/access counts, last register/value and timer samples without
new I/O or retry. Obsolete profiling fields and their tests are removed.

No live CPU, provider, master, callback identity, USB, clock, reset, bridge,
configuration, ROM identity or integrity check changes. Full observations still
frame each 16-byte step, every endpoint access retains before/after guards,
and the one-second and 64-poll limits remain. Test-only counters wrap the actual
ownership bodies to charge synthetic clock costs independently of production
instrumentation. They supply no physical timing evidence.

The 19 targeted tests pass, including 47 shared ownership cases and 82 joined
4/8 KiB step cases. Both actual C/RTL paths pass all 23 phases. The final
frequency cache is written only after the initial guard accepts; early failures
do not invent an observed timer rate. Exact inputs and logs are in the
[test receipt](result.json) and `out/nano-rom-unprofiled-20261106`.

Next rebuild and inspect the matched boot chain, complete changed-file checks,
then measure one fresh bounded board attempt. ROM completion, CPU Run, full
ROM-backed launch/return, ordinary package admission and DE10-equivalent
readiness remain open.

The SDK, native entry, BL31, U-Boot and HPS-first composition have now rebuilt
from `46c583a193eddbdde47b39a16862782a49dc1787`. Actual disassembly shows the
timer callback has one CNTPCT read, one cached store, no function calls and no
profiling accumulators. Both selected ROMs preflight without firmware I/O; the
USB/native Go packages and complete changed-file suite pass. The
[matched build receipt](matched-build-result.json) pins the evidence.

The [bounded operator source](operator-source.json) retains the predecessor
programmer/setup stops and both ROM-deadline trials. The new compiled firmware
has no attempt yet. One new attempt may use the tested 6 MHz programming link,
with restoration to 15 MHz and exact host permissions. Target-rate chip ID,
ownership, readback, launch, stop and deadline gates remain unchanged.
