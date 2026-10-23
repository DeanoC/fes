# PC/XT two-phase execution-unit prototype

Scope: separate operand selection from ALU/writeback without changing the
100 MHz fabric clock, guest divider, firmware or 4096-word control image.
The source base is FES `e0bb083d37de176947f28e9f8f2ab640423347ec`.
This changes EU retirement throughput; it is not cycle-equivalent to that base.
The BIU and completion capture retain their full physical clock rate.

The operand phase captures separate logic/ADD operands and the dispatch byte.
The execute phase updates architectural state and advances PC, local ROM
addresses, word/control outputs and jump squashing together. The internal
strobe-valid signal makes the BIU consume a held command once per microword;
request/data levels remain available on both phases. All register paths retain
normal physical-clock constraints; there is no multicycle timing exception.

Host checks pass: all 16 PC/XT integration suites (the 15 pre-existing suites
and the new throughput suite), including CPU
interrupts, REP, multiply/divide, reset/fault recovery, SDRAM waits/DQM delays,
DMA/refresh and CGA/MDA video. The 31 focused tests include symbolic operand,
ROM-address/control, PC/stack/stall and new pipeline checks. The pipeline proof
covers arbitrary operand values, operation selections, resets, every carry,
ROM holds, strobe masking and full-rate completion capture. Separate mutations
of operand phase, ROM word enable and BIU strobe masking are rejected. A reset-word check first reproduced a stale-output failure; outputs now reset
to the generated entry word, and one/two-cycle warm resets pass after real
execution. These
are compositional checks, not a monolithic whole-CPU equivalence result.

The new `cpu-timing` suite runs checked NOP, dependent ADD/ADC, INC/LOOP and
REP MOVSW workloads. The paired baseline uses exact base-commit EU/BIU/ROM/
wrapper bytes with the same updated testbench and workload images as the
candidate. All results and guest-divider checks pass with 0/31/173 fabric-cycle
storage waits. Full interval measurements are in [throughput.json](throughput.json).

| Zero-wait workload | Base guest-clock intervals | Two-phase intervals | Fabric-time increase |
| --- | ---: | ---: | ---: |
| 128 NOP | 532 | 577 | 8.47% |
| 64 dependent ADD and 32 ADC | 968 | 1281 | 32.33% |
| 128 dependent INC/LOOP | 2612 | 3506 | 34.23% |
| 256-word REP MOVSW | 6431 | 7460 | 16.00% |

Intervals include their marker/verification overhead and the real prefetch/bus
path. They are measurements, not 8088 silicon cycle-conformance limits. The
older diagnostic completes its zero-wait run in 56,410 fabric clocks versus
63,094 for the prototype (11.85% longer). Its additional prefetch transfers
and polling iterations are retained in the logs. Unchanged pin frequency does
not establish unchanged instruction timing or the intended 8088 performance.

Artifacts, baseline source identities, symbolic mutation controls and native
logs are under `out/pcxt-two-phase-eu-20261023/`. Initial baseline harness build
errors are retained: source-path-scoped upstream lint waivers and the missing
ISA fixture were corrected in isolated build v3. No HDL was relaxed for them.

Fresh paired synthesis uses frozen base `e0bb083d` and candidate `47563669`
module archives, the same pinned Yosys and unchanged board recipe. Both pass
input-identity postflight checks. The candidate adds 73 flip-flops (64 operand,
eight dispatch and one phase bit): 3,740 becomes 3,813. Ordinary LUT count
changes from 4,882 to 4,878; arithmetic cells remain 1,349 and M10Ks remain 318.
The mapped structural check verifies all 64 operand bits are flip-flop outputs
clocked by the actual 100 MHz system net.

The final frozen throughput pair uses the same candidate testbench/workload
bytes against each committed RTL snapshot. All 12 measurements reproduce the
reported table/wait variants; receipt hashes and source/archive identities are
retained in `throughput.json`.

The first complete paired route uses the corrected compiler and unchanged
100/74.25 MHz clocks/constraints. The old reduction-root transform is omitted
on both sides because fresh candidate synthesis has no qualified root; this
is not an otherwise-identical replay of the older best checkpoint.

| Initial seed-1 profile | Setup failures | Worst setup | Hold failures / minimum |
| --- | ---: | ---: | ---: |
| Base, fresh synthesis | 143 | -1,200 ps | 0 / +781 ps |
| Two-phase EU, phase-shifted ROM | 40 | -1,801 ps | 0 / +796 ps |

CPU captures improve from 78 failures / -1,200 ps to six / -422 ps. All 3,209
same-system-clock CPU rows pass, minimum +273 ps; all 90 ROM-to-system rows
pass, minimum +1,764 ps. The six remaining CPU failures are address-register
to ROM paths in the 2.5 ns phase window. No CPU unsupported record is emitted.
The worse board WNS is on CGA display output; framebuffer and other peripheral
failures remain. Both complete reports still have 105 unsupported records and
`complete=false`. These figures do not establish physical coverage or signoff.
The full union retains 15,696 common, 1,503 baseline-only and 2,013
candidate-only rows; no intersection is used to hide changed identities.
[initial-route-summary.json](initial-route-summary.json) binds the committed
source profiles, complete report summaries and receipt hashes.

The follow-up removes the obsolete +90-degree ROM clock through the private
CPU hierarchy and the board PLL. The intervening operand edge can read the
ROM on the ordinary 100 MHz system clock, giving both address setup and ROM
output paths a full 10 ns without a timing exception or another microword of
latency. The CPU diagnostic has identical counts to the initial prototype,
including one/two-cycle warm reset recovery. All 31 focused tests pass;
induction checks arbitrary payloads and resets against the execute request
sequence, and rejects a mutation that reads only on execute. Full-system
simulation and fresh routed qualification for this follow-up are pending.
The SDRAM capture phase and pixel/system frequencies are unchanged.

Accounting appends exactly two full-route receipts: 79 retained receipts,
88,609.26647914992609 cumulative seconds, with the previous 77-row prefix exact
and every receipt object/hash/Decimal charge verified. The earlier corrected
checkpoint remains 33 setup failures / -482 ps with 105 unsupported boundaries;
it is not superseded by a passing candidate. No production lock, shared
contract, sealed package or hardware acceptance changes. No hardware ran.
