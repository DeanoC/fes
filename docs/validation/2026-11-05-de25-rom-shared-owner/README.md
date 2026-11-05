# ROM ownership through the accepted shared boot observer

The shared-USB ROM path now requires the exact accepted private boot/master
binding and its coherent bridge observer. Its existing per-access observation
already supplies a complete fresh CPU/cold/USB/master proof; the adjacent
standalone cold audit is removed only for that qualified shared profile.
Non-shared profiles retain their explicit audit. Every ROM access keeps its
before/after ownership and timer checks; no observation survives across calls.
There is no new wire request, grant, retry, CPU release or relaxed bound.

A new fixture joins the unmodified production ROM predicate and strong CPU
callback bodies to the actual trusted boot constructor, USB board, cold provider,
shared composition and master observer. Its completed configuration and physical
registers are modeled. Forty-seven cases cover accepted repeated observations,
missing/substituted/stale private bindings, foreign owners/epochs, wrong CPU
state, active leases, timer mismatch, DMA/IRQ/clock/reset drift, late CPU changes,
failed cold reads, changed device generation and substituted containment policy.
Failures never invoke substituted callbacks or fall back, and rejected replay
performs no additional hardware-model reads. Pin/PHY drift remains distinct from
internal no-DMA containment. The fixture measures 52 cold-register reads per
callback, versus 104 when reconstructing the removed adjacent audit. This does
not establish physical transfer timing.

Cached failure diagnostics also report ownership-call count and total/maximum
intervals between the driver's existing timer samples. Pending and invalid flags
identify incomplete or non-monotonic accounting. Reporting still runs only after
containment and performs no endpoint, ownership or timer reads. The C/RTL fixture
charges seven synthetic ticks per supplier call and checks exact accumulated
cost and maximum interval on a valid transfer; these are modeled ticks.

Eight targeted tests pass, including the 47 new scope cases, 69 existing coherent
observer cases, 21 strong CPU cases, 21 shared dispatcher cases and existing
computer opening/return failures. The 23-phase ROM verifier passes for both
4 KiB and 8 KiB, including the actual strong provider's non-shared path, CSR/RAM
RTL, source snapshot, CPU execution, held return and transfer faults. The shared
scope fixture and C/RTL verifier cover distinct boundaries.

Evidence is under `out/nano-rom-shared-owner-20261105`. No hardware has been
programmed for these changes yet. They require a freshly matched chain and
bounded physical trace. The one-second/64-poll ROM limits remain unchanged.
Ordinary core admission and DE10-equivalent loading remain open.
