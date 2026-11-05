# Direct private ROM validation

The preceding physical cost-split trial passed recovery, eight uploads/readbacks,
native boot, USB pause and sealed FPGA configuration at a 6 MHz programming
link. ROM loading still hit its unchanged one-second deadline. Full ownership
checks consumed 458.9425975 ms across 783 samples; inner guards consumed
476.8392225 ms across 5,070 samples. Together they accounted for 93.5704% of
elapsed time. The source-bound [physical receipt](../2026-11-05-de25-rom-cost-split/physical-timing-result.json)
retains the complete failure and verified restoration to 15 MHz.

This change removes temporary callback-table construction from private binding
and ROM guard validation. The matcher takes context and four callback identities
as direct arguments; the supplier passes its fixed functions. The ROM guard
calls the same shared validator directly. Factories remain the one-time boot
and service binding interfaces. No binding result or hardware observation is
cached between callbacks.

During one active-step ownership callback, the shared validator checks the same
accepted boot/master binding before any MMIO, so the preceding duplicate query
is omitted. The common CPU/provider/configuration/identity/program checks remain.
The full path retains its accepted-binding check and live bridge observation.
Every 16-byte step still has complete cold/USB/clock proofs at both ends; each
endpoint access still has before/after ownership, CPU/reset/bridge checks and
the existing barriers. Abort, replay rejection, the one-second deadline and
64-poll limit are unchanged. Only internal C calls changed; no wire format or
FPGA producer changed.

The 19 targeted tests pass, including all 47 actual ownership cases and 82 joined
4/8 KiB driver/step cases. Both actual C/RTL paths pass all 23 phases. Additional
tests reject each substituted context/callback identity without invoking it or
reading hardware, compare direct and factory validation, and retain poisoning
of a replaced supplier. Register access and containment bounds stay intact.
The fixture's final callback-substitution test was separately rerun after its
test-only observer counter was made explicit.

Exact source and logs are in [the test receipt](result.json) and
`out/nano-rom-private-direct-20261106`. Next, build and inspect the actual native
firmware to verify the hot path no longer constructs those tables, then measure
it in a fresh bounded test. Software/RTL checks do not establish the speedup,
complete ROM loading, CPU Run, normal package admission or DE10-equivalent readiness.

The matched SDK, entry, BL31, U-Boot and HPS-first composition have now built from
`a1f8ac81e9670dbcca97a019646ff87de0b9d6e8`. Actual disassembly follows
ROM ownership through the direct service validator, shared validator, fixed
callback binding and accepted boot matcher without callback factories. Both
ROM selections preflight without firmware I/O. The complete changed-file checks
pass in 783 seconds, and the five USB/native Go packages pass. The
[matched build receipt](matched-build-result.json) hashes all evidence.

A [fresh bounded operator](operator-source.json) retains the predecessor
programmer failure, host setup stop and native ROM deadline. The new compiled
candidate has no attempt yet. Its tested 6 MHz programming-link procedure restores
15 MHz and exact host permissions, and admits the chip only after a fresh exact
ID and link-rate check. One attempt is permitted; the existing native bounds
remain unchanged. This preparation does not establish ROM speed or completion.
