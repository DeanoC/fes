# Refreshed Quartus timing review

Quartus Prime Pro 26.3.0 Build 114 compiled producer
`ae47a408588a41dddfb45ea0df4003a5ec3485db`. The selected probe RTL hash is
`4afe3497bca4bb991e855d1c40bd7e8e1d97ea9b943d40083f44eddede7c2d52`; the
diagnostic SOF hash is
`e9ca0b6e7d138e73d9e20c9ba0707aa0c96481e1460e909d3700cac7f38cb1b2`.
Full compilation completes with zero errors and 308 warnings. This is host-only
compiler evidence, not physical timing acceptance.

The fit uses 10,918 ALMs, 21,659 dedicated registers, 284 pins, 2,098,688
block-memory bits in 131 RAM blocks, zero DSP blocks, one HPS and two PLLs.
All 23 final STA rows have nonnegative slack and zero TNS:

| Check | Minimum slack (ns) |
| --- | ---: |
| Setup | 3.423 |
| Hold | 0.000 |
| Recovery | 7.236 |
| Removal | 0.158 |
| Minimum pulse width | 0.220 |

The zero hold value is a displayed zero, not positive margin. The current
report lists 35 clocks. All six coverage rows are zero for illegal clocks,
unconstrained clocks, unconstrained input/output ports and paths involving
those ports.

The Design Assistant still reports four failed rule classes, all unwaived:

| Rule | Severity | Violations |
| --- | --- | ---: |
| TMC-20012 — Missing Output Delay Constraint | High | 1 |
| LNT-30023 — Reset Nets with Polarity Conflict | Medium | 1 |
| RDC-50003 — Multiple Asynchronous Reset Synchronizers in the Same Clock Domain | Medium | 1 group |
| RES-50101 — Intra-Clock False Path Reset Synchronizer | Low | 2 |

The LNT-30023 report still names the limiter-pipeline JTAG packet address bit
40 and the two 128-register `mux_pipeline` synchronous-clear banks. This is a
datapath lane-selection mapping, not an applied system-reset waiver. The
compiler report remains visible and no constraints or acceptance checks were
relaxed. Because the Design Assistant failures remain, overall design closure
is **FAIL** despite the passing constrained timing rows.

Quartus reports 42 synchronizer chains, models 24 and excludes 18; zero chains
fail its timing-violation calculation and the shortest detected chain has two
registers. The reported `1e+09` years is a partial compiler model only. The
excluded chains and physical behavior are not qualified. No board was used,
and no DMA address, translation/firewall permission, reset-drain lifecycle or
whole-board timing claim is made. DE25 uses the Quartus-only lane; no nextpnr
result is part of this evidence.
