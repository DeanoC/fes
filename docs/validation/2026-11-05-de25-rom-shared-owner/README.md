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

## First matched hardware trace

One attempt on TRWU8CBL reached the unchanged one-second ROM limit after the
last written-byte observation reached 1,792 of 8,192 bytes. The preceding trace
reached 416 bytes; these are single-attempt progress observations, not statistical
throughput qualification. Recovery, all eight fixed uploads/fresh SD hashes,
matched native startup/shared USB and first FPGA configuration passed.

The new cached counters attribute 391,530,589 of 400,198,542 elapsed ticks to
1,638 completed ownership brackets: 0.9788264725 of 1.000496355 seconds (97.8%).
The maximum completed bracket was 242,317 ticks (0.6057925 ms). Accounting was
valid with no pending bracket, and no ownership predicate failed. The next
repair must reduce the cost of this proof without extending the ROM deadline.
A bounded trusted protocol transaction with complete ownership framing and fresh
reset/CPU guards is a candidate design, not implemented or qualified here.

The attempt stopped before ROM completion/CPU Run; there was no retry, second
launch, return or accepted ROM cycle. Independent cleanup verified exact USB
permissions, recorded 15 MHz JTAG readback, the owned private server's exit and
a free UART. Claim and spare card remain retained; factory card and other kit
were untouched. Cached Held does not independently prove physical CPU halt.
Full core-load readiness remains unachieved.
