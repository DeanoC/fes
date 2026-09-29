# RAM tester: initialization-preserving FSM extraction, 2026-09-27

**Placement-option correction:** exponent 2 in this record is the requested
CLI value. The tested Mistral versions actually forced exponent **7** and beta
**0.5**. The measured results and same-placement comparisons still stand. See
the [placer-option investigation](2026-09-27-ramtest-placer-options.md).

Follow-up to [the timing diagnosis](2026-09-27-ramtest-timing.md) for
[FES #264](https://github.com/DeanoC/fes/issues/264). Host-only compiler work;
no RAM-test RTL, protocol, producer lock or hardware acceptance change.

## Compiler change

Published Yosys candidate:
[`c156dd886d87113c30bc2bf422ccb9ae0208b2a1`](https://github.com/DeanoC/yosys/commit/c156dd886d87113c30bc2bf422ccb9ae0208b2a1),
branch `fix/fsm-initial-state`, based on the existing RAM-test pin `54ea7109`.
No pull request has been opened.

The former detector declined initialized FSMs, while forced extraction ignored
initialization. The change adds an optional `$fsm.STATE_INIT` state-table index,
separate from `STATE_RST`. It reads initialization through signal aliases,
retains startup-only states during optimization, and maps the recoded initial
state to the replacement flip-flops. Inactive one-hot bits initialize to zero.
Legacy FSM cells without the parameter remain uninitialized. Partially defined
initial states remain unextracted, including when explicitly marked as FSMs.

This preserves the DDR tester's `ST_DONE` startup while its synchronous reset
still selects `ST_WRITE_START`. KISS2 export remains limited: its `.r` directive
represents the reset state, not a distinct power-up state. RTLIL retains both.

The implementation lives in Yosys, chiefly `passes/fsm/{fsm_detect,fsm_extract,
fsm_opt,fsm_map}.cc` and `fsmdata.h`, with cell validation, simulation-model and
documentation updates. There is no manual state encoding in the FES core.

## Validation

- Baseline regression fails because the initialized FSM is not extracted.
- New regressions cover distinct startup/reset states, startup-only states,
  alias initialization, partial initialization rejection, and original,
  binary and one-hot encodings. SAT checks startup traces and temporal induction.
- Yosys optimization suite: **96/96**; Intel ALM suite: **16/16**, including a
  new equivalence proof through Cyclone V flip-flop simulation models.
- Actual unmodified `ddr_channel.v`: **1,919/1,919** equivalence points for
  64-bit data and **2,907/2,907** for 128-bit data. The proof uses the recoding
  map, `equiv_simple` and `equiv_induct`, plus eight-cycle startup checks.
  Uninitialized unrelated registers remain unspecified (`-set-init-undef`,
  `-ignore_gold_x`); no initial reset is imposed.
- The `$fsm` simulation model passes a 100-cycle startup/reset/transition
  regression in Icarus Verilog 12.0. A Verilator model mismatch was reproduced
  with the original simlib after reset and is not counted as a passing check.
- Independent review found no blocking synthesis defect. Its KISS2 limitation
  is documented above. No hardware was accessed.

## Unchanged-source RAM-test experiment

The input source hashes and embedded build ID are those of the
[controlled comparison](2026-09-27-ramtest-timing.json):
`cd3e4c315005fde83065dff7483ae0d7`. The candidate uses the same `read_verilog`
and `synth_intel_alm -nobram -nolutram -nodsp -top top` commands. Its private
Release build disables the unused Slang frontend and uses the original ABC
executable; the Verilog frontend and target recipe remain the same.

Six initialized FSMs are now recognized: three DDR channels, DDR throughput
calculation, the SDRAM controller and its pattern tester. The investigated
`hps_ddr.port2.skid` to `ddr2_address[23]` enable cone reduces from **six LUT
levels to four**. This is structural depth, not a measured path delay.
The structural pixel-cone depth stays at eleven. Flip-flop count increases
from 8,196 to 8,221 and ALUT count from 6,505 to 6,506.

The matched GPU run retains nextpnr `f95cef3d`, Mistral `7ed06e21`, seed 2,
HeAP timing weight 10, criticality exponent 2, device `5CSEBA6U23I7`, and all
original clock constraints. Routing stops at iteration 67 with one overused
wire, `TD.11.18.44`, shared by two `ddr_speed` nets. It produces **no legal
route, final analogue Fmax or RBF**. Its printed pip-table timing is excluded
from the comparison.

The CPU `router2` control completes legally with final analogue signoff:

| Same seed / placement settings / CPU router | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Original Yosys | 71.97 | 65.64 |
| Initialized-FSM candidate | 63.82 | 62.94 |

Both clocks regress in this single-seed control. Its worst memory path is now
`ddr0_errors[11]` to the `ddr0_status[205]` register enable, with 13.798 ns of
routing and 1.310 ns of combinational cell delay. The shorter investigated
control cone has not translated into improved full-design Fmax. This does not
isolate whether placement, mapping of other cones, or their interaction causes
the regression. The diagnostic RBF is not a qualified package.

## Reproduction

Compiler tests are in the candidate commit:

- `tests/opt/fsm_init{,_alias,_edges,_partial}.ys`
- `tests/arch/intel_alm/fsm_init.ys`
- `tests/various/fsm_init_sim.sh` (uses `YOSYS`, `IVERILOG` and `VVP`, with
  executable-name defaults)

Run the `.ys` scripts with the candidate Yosys. The existing `tests/opt` and
`tests/arch/intel_alm` Makefile generators include the new tests; provide the
CMake `BUILD_DIR`. Tests explicitly requesting `<yosys-exe-dir>/yosys-abc`
need ABC available next to the built Yosys executable.

Full local evidence lives under `out/ramtest-fsm-init/`: `synth.ys`, both DDR
proof scripts and logs, compiler test summaries, simulation logs, and routing
outputs. The compiler checkout is `out/ramtest-fsm-init/yosys`; the published
commit preserves the change independently of this ignored scratch directory.
The [evidence summary](2026-09-27-ramtest-fsm-init.json) records hashes, results,
and the DDR proof/synthesis scripts. Expand `{fes_root}` in the synthesis script
and run it from `sources/misteross`; run the DDR proof scripts from FES root.

This stage establishes safe initialized-FSM optimization and a smaller DDR
control cone. A shorter cone alone does not establish 130 MHz closure. The
producer lock remains unchanged: the matched GPU run fails routing and the
CPU control regresses. The candidate is available for further compiler work,
not promoted as a timing improvement.
