# DE25 RAM Tester timing review

This record covers producer `43c248d2dfbad7fa2d0ef4c72fa006fe21214dc9`,
Quartus Pro 26.3.0 Build 114, and SOF
`5b4bf7192dc58a2be0e8ba9b811fcac4aa082ede105c78231dcec8ffd06e1778`.
The retained private project is `out/de25-ramtest-calibration-contract/hps/project`.
[timing-summary.json](timing-summary.json) binds source, input, artifact and
complete report hashes. Full reports remain in that private output.

The [46 reported timing rows](timing/golden_top.sta.summary) are nonnegative,
with TNS zero in every row. One hold row displays 0.000 ns at the report's
precision; this is not a claim that every row has positive margin.

| Check | Rows | Minimum slack (ns) |
| --- | ---: | ---: |
| Setup | 3 | 2.320 |
| Hold | 5 | 0.000 |
| Recovery | 3 | 6.079 |
| Removal | 3 | 0.158 |
| Minimum pulse width | 32 | 0.220 |

The [clock inventory](timing/clocks-excerpt.rpt) has 73 clocks, and all 73
[status rows](timing/clock-status-excerpt.rpt) say Constrained. There are zero
illegal or unconstrained clocks. `ramtest_sdram50` has period 20 ns and edges
0/10 ns; `ramtest_sdram_forwarded` has period 20 ns and edges 10/20 ns, inverted
from the same `CLOCK1_50` source to `DRAM_CLK`. The B EMIF reference and PHY
clock periods are reported as 5.999 ns and 1.874 ns respectively. These are
the actual tool values, not an advertised memory-rate qualification.

## Scoped internal receiver and calibration checks

The completed-database [query](timing/receiver-query.tcl) and
[log](timing/receiver-query.log) cover all four published operating conditions:
Slow fix4 100C, Slow fix4 0C, Slow fix4a 0C and Fast fix4 0C. It changes no
constraints. All 52 checked original source, artifact and compiled report files
have identical [before/after hashes](timing/query-identity.json).

The [256 receiver paths](timing/receiver-paths.tsv) include each of 16 bits,
both setup/hold checks, both internal boundaries and all four corners.

| Internal boundary | Setup minimum (ns) | Hold minimum (ns) |
| --- | ---: | ---: |
| Falling-edge `sampled_dq` to rising-edge `readdata` | 8.095 | 10.106 |
| Rising-edge state/timer/reset control to falling-edge capture enable | 7.195 | 9.153 |

The setup relationship is 10 ns and the hold relationship is -10 ns.
All queried receiver reports include 0.030 ns uncertainty; the
[loaded SDC rows](timing/uncertainty-excerpt.rpt) identify it as Generated.
There is no demonstrated omission caused by declaring these clocks late in
the SDC order. External source jitter, clock-output/PCB delays, DQ arrival,
input setup/hold and electrical behavior remain separate open inputs.

The registered 50 MHz calibration level enters the existing two-stage 100 MHz
synchronizer. The compiled exception selects exactly one source and one
first-stage D pin; guards also require each of the two stages exactly once.
The [compiled CDC check](timing/calibration-cdc-compiled.rpt) reports zero
CDC-50002 violations and zero waivers. Second-stage setup/hold minima are
9.542/0.129 ns. Each corner retains 3,033 distinct timed downstream endpoints
for both setup and hold, with minima 6.416/0.380 ns; see the
[per-corner summary](timing/calibration-query-summary.json). The first-stage
asynchronous ingress is the only new cut. The
[04ba experiment](timing/calibration-experiment-04ba/receipt.json) records the
original one-finding result and its in-memory repair; that artifact remains
rejected in [candidate history](candidate-history.json).

## Remaining coverage and rule boundaries

The [coverage report](timing/coverage-excerpt.rpt) retains 16 unconstrained
input ports and 39 unconstrained output ports, with 16/55 setup and hold path
pairs respectively. These include external SDRAM timing. Constrained internal
slack does not close these paths.

The [unwaived Design Assistant failures](timing/design-assistant-excerpt.rpt)
remain eight rule classes:

| Rule | Violations | Meaning of this record |
| --- | ---: | --- |
| TMC-20012 | 40 | Missing output-delay contracts, including LED7 |
| TMC-20011 | 16 | Missing SDRAM DQ input-delay contracts |
| RDC-50003 | 2 | Two clock-domain groups, containing 14 and 5 reset chains; no whole-boundary reset proof |
| TMC-20021 | 1 | Vendor B EMIF minimum-only assignment to `hmc_core_reg`; no invented maximum |
| RES-50010 | 90 | Preserved constant AXIL helper outputs, including a duplicated BREADY register; not 90 demonstrated reset defects |
| TMC-20602 | 42 | Endpoint tension remains reported despite nonnegative timing |
| LNT-30010 | 2 | Reset nets also feed deliberate `first_seen` clock enables |
| RES-50101 | 2 | Reported intra-clock reset synchronizer false paths remain unwaived |

The [MTBF report](timing/mtbf-excerpt.rpt) finds 73 chains, includes 30 and
excludes 43 at every corner; the shortest chain has two registers. Zero chains
are uncomputed because of timing violations. The tool's reported 1e9-year
value applies to its modeled subset and does not establish whole-design or
physical MTBF acceptance.

Overall timing acceptance remains unqualified. This record proves neither
external SDRAM timing nor vendor EMIF/generated-interconnect functional
behavior, Linux DMA ownership, boot-handoff compatibility or physical board
operation. The FPGA timing-model temperatures also do not qualify SDRAM
temperature/refresh grades. See the [operator boundaries](../../../boards/de25nano/ramtest/README.md).
