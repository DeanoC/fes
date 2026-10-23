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

Host checks pass: all 15 pre-existing PC/XT integration suites, including CPU
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

Fresh mapped synthesis and routed timing comparison remain pending. The prior
corrected-model checkpoint has 33 setup failures / -482 ps and 105 unsupported
boundaries; no timing, physical endpoint, production-lock or hardware acceptance
is inherited by this architectural profile. No hardware ran.
