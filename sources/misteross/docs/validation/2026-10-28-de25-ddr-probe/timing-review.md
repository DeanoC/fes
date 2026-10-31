# Final constrained timing review

Quartus Pro 26.3.0 Build 114 completes the frozen
`8f54e4f89e5b12ae18df70c542bc5846d0452ce6` GHRD/probe build with zero errors
and 308 retained warnings. This is host-only evidence. Constrained Timing
Closure passes; overall **Design Closure fails** with the four Design Assistant
rules listed below. Neither result establishes hardware, DMA admission,
boot-firmware pairing or whole-board metastability acceptance.

The private output is `out/de25-ddr-probe-8f54e4f89e` in the
`de25-hps-interface` worktree. Its SOF SHA-256 is
`9c45d9371d108f1f1134864bb5bfbb9182c2af853f4a062b5904224c3a97367c`.
The top SOPCINFO SHA-256 is
`726b6d5c0c5a579fad5dd6ae0ae29cd36b8f82ef5ec79f753585007e9aab2bff`.
[timing-summary.json](timing-summary.json) binds the producer, SOF, inputs,
generation receipt and final reports, and contains every STA summary row,
all clock identities and the coverage comparison.

All 23 final STA summary rows have nonnegative slack and zero TNS. The
reported minima, at the report's precision, are:

| Check | Worst slack ns |
| --- | ---: |
| Setup | 3.581 |
| Hold | 0.000 |
| Recovery | 7.567 |
| Removal | 0.155 |
| Minimum pulse width | 0.220 |

The zero hold value is a displayed zero, not a claim of positive margin.
The final coverage report has zero illegal/unconstrained clocks, zero
unconstrained inputs or outputs, and zero paths involving unconstrained
ports for both setup and hold. All 35 clock names, types, periods and
frequencies exactly match the preserved `4ab2472b0b22b64d8756fd7c6e83d117e794b214`
reset-sequencing baseline. Its coverage counts also match. This compares
constraint coverage; it does not transfer hardware acceptance or make a
physical timing-model accuracy claim.

Final resources are 10,893 ALMs, 21,655 dedicated registers, 284 pins,
2,098,688 block-memory bits in 131 RAM blocks, zero DSPs, one HPS and two
PLLs. The fit selects `A5EB013BB23BE4SCS` with final timing models.

| Unwaived final rule | Severity | Violations | Comparison with baseline |
| --- | --- | ---: | --- |
| TMC-20012 — Missing Output Delay Constraint | High | 1 | Same retained `LED[7]` request |
| LNT-30023 — Reset Nets with Polarity Conflict | Medium | 1 | New generated data-lane steering mapping; traced below |
| RDC-50003 — Multiple Asynchronous Reset Synchronizers in the Same Clock Domain | Medium | 1 group | Same 12 heads in the 100 MHz domain |
| RES-50101 — Intra-Clock False Path Reset Synchronizer | Low | 2 | Same top-reset-to-root/peripheral-controller paths |

The RDC head identities and order match the baseline list. The two
RES advisories are from `fes_top_reset_inst|sync_regs[1]` to the existing
`soc_inst|rst_controller` and `soc_inst|subsys_periph|rst_controller` reset
chain heads. Counts alone do not prove reset/traffic coordination. These
rules remain visible; no waiver or relaxed constraint is applied.

MTBF identifies 42 chains, models 24 and excludes 18; the shortest detected
chain has two registers. The report finds zero chains whose MTBF calculation
fails for timing violations and labels the modeled result `1e+09` years.
The baseline found 38, modeled 24 and excluded 14. The excluded chains and
physical behavior are outside this partial compiler model, so its number
must not be described as whole-design or whole-board MTBF acceptance.

The new LNT-30023 driver is command data, not a system reset. Its report
names `soc_inst|mm_interconnect_0|limiter_pipeline|gen_inst[0].core|data1[40]`
and opposite SCLR uses on two `mux_pipeline` data banks of 128 registers
each. The final generated source explains the mapping:

- `qsys_top_altera_mm_interconnect_1920_7nkwjpa.v:861` assigns the legacy
  32-bit JTAG master's packet address to bits `[68:36]`. Packet bit 40 is
  therefore byte-address bit 4.
- The `qsys_top_altera_avalon_st_pipeline_stage_1930_oiupeiq.sv` wrapper
  copies `in_data` to the same low payload bits, then slices successive
  128-bit groups into its `gen_inst` cores. Its limiter preserves packet
  bit 40. The registered-ready branch of `altera_avalon_st_pipeline_base.v:85`
  captures `data0`/`data1` under enables, with no reset branch for these data
  registers.
- The legacy 32-to-256 command width adapter
  `qsys_top_altera_merlin_width_adapter_1970_uogzwqy.sv:1917` selects lanes
  using the low address bits. Its non-packing assignment at line 2001 shifts
  the 32-bit data into the selected 256-bit lane. Address bit 4 selects the
  lower versus upper 128-bit half; the other half is zero.
- The downstream `mux_pipeline` first two 128-bit groups carry exactly
  those lower/upper data halves. Mapping their complementary zeroing to
  opposite SCLR polarities accounts for the reported 128/128 bank counts:
  address bit 4 clears the lower bank when set and the upper bank when clear.

This is the documented case where ordinary datapath logic can use register
SCLR pins, rather than a conflicting system-reset connection. The
[official LNT-30023 rule description](https://resources.altera.com/quartushelp/25.1/da_rules/lnt_30023.htm)
explicitly distinguishes that synthesis mapping from incorrect reset wiring.
This source/netlist classification does not waive the failed report, assert
formal mapped equivalence, or qualify physical resets. The read-only probe
interconnect bench does not functionally exercise the retained JTAG write path.

Post-compile source binding is preserved: all **114** source hashes in
`out/de25-ddr-interconnect-8f54e4f89e-03/receipt.json` still match the final
project, with zero missing or changed inputs. The generated interconnect
source remains SHA-256
`69fe13fb37b94747d4ba8f4d14b7d82c65f15a3afcc6fa7befbd0ddcf83c3c9d`.
Every final compiler report hash still matches `build.json`. Thus the
generated-fabric functional test remains bound to the source selected by
this compile; it does not establish actual HPS DDR accesses or physical timing.
