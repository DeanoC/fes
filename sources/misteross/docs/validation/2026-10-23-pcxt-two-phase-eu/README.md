# PC/XT two-phase execution-unit prototype

The implementation separates operand selection from ALU/writeback at the
unchanged 100 MHz fabric clock. The completed seed-1 native route reports all
3,491 CPU setup/hold rows passing, minimum +934/+802 ps, with no emitted CPU
unsupported record. **Whole-board closure remains open:** 72 setup failures,
WNS -1,575 ps, zero hold failures / minimum +789 ps, 105 unsupported records
and `complete=false`. This is host simulation and routed diagnostic evidence,
not physical, producer, sealed-package or hardware acceptance.

The source base is `e0bb083d37de176947f28e9f8f2ab640423347ec`; the final frozen
RTL is `be56eb29f72d72ba06c8b9b2521d815be1df203c`. The guest divider, board
firmware and 4096-word control image are unchanged. EU retirement throughput
changes; this implementation is not cycle-equivalent to the base.

The operand phase captures separate logic/ADD operands and the dispatch byte.
The execute phase updates architectural state, PC, local ROM addresses,
word/control outputs and jump squashing together. A private strobe-valid
signal makes the BIU consume each held command once; request/data levels and
completion capture retain their full physical-clock behavior. Reset initializes
the word and controls from the generated entry word, including one-cycle warm
reset. The intervening operand edge reads the four ROM lanes on the ordinary
system clock. Both address setup and registered word/control output paths have
one full 10 ns cycle. The obsolete +90-degree ROM clock and private ports are
removed; SDRAM capture remains +180 degrees and pixel remains 74.25 MHz. There
is no multicycle exception or reduced system clock.

All 16 PC/XT integration suites pass, including interrupts/IRET, REP, multiply/
divide, waits, reset/fault recovery, SDRAM/DQM, DMA/refresh and CGA/MDA video.
The 31 focused tests cover operand/ALU/carries, PC/stack/stall projections,
ROM addresses/sequencing/reset and full-rate completion capture. Mutations of
operand phase, ROM word enable, BIU strobe masking and ROM read phase reject.
A reset-word test first reproduced stale execution after a short reset; the
fixed implementation passes one/two-cycle warm resets after real execution.
These are compositional proofs, not whole-CPU equivalence or exhaustive ISA/
silicon-cycle conformance.

The required `make test-changed` against `e0bb083d` passes: 842 parent tests,
1,272 component tests (one optional Mistral host oracle skipped), generated
consistency and PC/XT RTL simulation. `make check` also passes. The original
sandboxed attempt is retained separately because isolated fixture socket binds
were denied; the complete rerun outside the sandbox passes. These working-tree
checks are separate from frozen synthesis/route receipts.
[host-checks.json](host-checks.json) binds the complete test log/results.

The checked `cpu-timing` workloads run with 0/31/173 fabric-cycle storage waits
against exact base RTL and identical testbench/workload bytes. All results and
guest-divider checks pass. The final system-clock-ROM implementation reproduces
all 12 intervals of the first two-phase prototype exactly.

| Zero-wait workload | Base guest-clock intervals | Two-phase intervals | Fabric-time increase |
| --- | ---: | ---: | ---: |
| 128 NOP | 532 | 577 | 8.47% |
| 64 dependent ADD and 32 ADC | 968 | 1281 | 32.33% |
| 128 dependent INC/LOOP | 2612 | 3506 | 34.23% |
| 256-word REP MOVSW | 6431 | 7460 | 16.00% |

Intervals include marker/verification overhead and the real prefetch/bus path;
they are measurements, not 8088 silicon acceptance limits. The older diagnostic
uses 56,410 zero-wait fabric clocks versus 63,094 (11.85% longer). Unchanged
pin frequency does not establish unchanged guest instruction timing or intended
8088 performance. [throughput.json](throughput.json) retains all wait variants,
frozen source/archive identities and receipt hashes. Initial baseline harness
build errors remain retained; fixture paths and the missing ISA fixture were
fixed without relaxing HDL checks.

Fresh synthesis uses frozen module archives, the same pinned Yosys and board
recipe, with successful input-identity postflight. The final candidate adds
73 FFs (64 operand, eight dispatch and one phase): 3,740 becomes 3,813. Ordinary
LUTs change from 4,882 to 4,878; arithmetic cells stay 1,349 and M10Ks stay 318.
Clock buffers reduce from four to three when the ROM clock is removed. Mapped
checks verify all 64 operand bits are FF outputs, all 16 CPU M10K clock pairs
and all 1,215 CPU FFs use the actual 100 MHz system net.

| Seed-1 profile | CPU setup failures / WNS | Whole-board setup failures / WNS | Board hold failures / minimum |
| --- | ---: | ---: | ---: |
| Base, fresh synthesis | 78 / -1,200 ps | 143 / -1,200 ps | 0 / +781 ps |
| Two-phase EU, phase-shifted ROM | 6 / -422 ps | 40 / -1,801 ps | 0 / +796 ps |
| Two-phase EU, system-clock ROM | 0 / +934 ps | 72 / -1,575 ps | 0 / +789 ps |

All profiles use the corrected compiler, same seed/repair recipe and unchanged
surviving clock frequencies/phases. The graph-specific reduction-root transform
is omitted on both sides because fresh candidate synthesis has no qualified
root. This is not an otherwise-identical replay of the historical best
checkpoint (33 failures / -482 ps). The initial six CPU failures are short
2.5 ns address-to-ROM paths; reading on the intervening system edge removes
that timing window architecturally. The final full union retains 20,598 rows
across the three profiles, including every added/removed identity. No report
intersection stands in for complete timing.

The corrected collector repeat passes all 172 bound-input checks and final
analogue/cache inventory checks, including 256 calibration types and zero
missing cached arcs. Its entire RBF, timing and endpoint reports reproduce the
first system-clock-ROM execution byte for byte. The original collector failure
is preserved: its adapted filename was paired with the old runner's hash,
so its postflight remains false. The successful repeat uses a corrected
preflight/postflight self-binding; it does not rewrite or waive that failure.
[system-rom-collector-rejection.json](system-rom-collector-rejection.json) and
[final-route-summary.json](final-route-summary.json) bind both evidence classes.
The complete artifacts and literal endpoint union are under
`out/pcxt-two-phase-eu-20261023/`.

Final failing captures comprise 27 display and 45 other endpoints, including
boot-ROM, DMA and framebuffer paths; PIC and CPU groups pass this route.
Native classification uses emitted capture identities, not a physical
completeness claim. Same-recipe two-of-three seed qualification, complete
physical coverage, authenticated producer and exact-artifact acceptance remain
open. No production lock, shared contract or hardware acceptance changes.
No hardware ran. PR #979 remains draft and #840 stays open.

Accounting retains 81 exact route/search receipts, including the rejected
collection, cumulative 90,896.13443564576979 seconds. The previous 80-row prefix
is exact, and every receipt object/hash/Decimal charge verifies. Host builds,
proofs, synthesis and benchmarks add no route charges.
