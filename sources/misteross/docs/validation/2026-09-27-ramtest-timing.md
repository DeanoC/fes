# RAM tester: Quartus versus OSS timing, 2026-09-27

**Placement-option correction:** exponent 2 in this record is the requested
CLI value. The tested Mistral versions actually forced exponent **7** and beta
**0.5**. The measured results and same-placement comparisons still stand. See
the [placer-option investigation](2026-09-27-ramtest-placer-options.md).

Issue: [FES #264](https://github.com/DeanoC/fes/issues/264).
This is host-only compiler research. It changes no RTL, package recipe,
compiler lock, ABI, or hardware acceptance status.

## Result

The tools do not implement the same logic network from the same RTL.
Quartus's port-2 guard-to-address-enable path uses two logic levels;
Yosys/nextpnr uses six LUT cells. The OSS path then pays for substantially
more interconnect. FSM extraction accounts for part of this difference:
Quartus recognizes and one-hot encodes the initialized DDR state machines,
while the selected Yosys skips them. Disabling Quartus recoding increases the
matched path from two to three levels and slows it by 1.202 ns, but it remains
4.216 ns faster than OSS. FSM recoding alone therefore does not explain the gap.

The GPU router is better than the default CPU router on this placement.
Replacing GPU routing with `router2`, with every placed cell unchanged,
reduces memory Fmax from 101.94 to 71.97 MHz. This does not prove the GPU
router is optimal; it rules out that particular replacement as the cure.

The next compiler work should address synthesis/control-cone mapping and
initialized-FSM support, alongside physical optimization. The measurements do
not establish that Mistral's timing model is pessimistic relative to TimeQuest:
that would require comparing equivalent physical routes and loads in both
models. Comparing the two designs' worst Fmax numbers cannot establish it.

## Controlled inputs

- RTL source: FES `5c15931aae47494a89b2a5a3cf673bc9f2f09a4a`.
- Embedded build ID in both fresh lanes: `cd3e4c315005fde83065dff7483ae0d7`.
- Device: `5CSEBA6U23I7`; target memory 130 MHz, pixel 74.25 MHz.
- OSS: Yosys `54ea7109ff08f7ecf8ca7b5ced58e8ba62d7f4b8`,
  nextpnr `f95cef3d90074c2e3c79c16fc5df73cdb5824817`,
  Mistral `7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`.
- Quartus Lite 17.0.2, seed 1, four compilation threads; reports use the
  slow 1100 mV / 100 C corner. Both fresh Quartus variants use those settings.
- All 13 RTL source files in the fresh Quartus recipe were checked against
  the SHA-256 entries in the saved OSS build record before compilation.
  The three generated Verilog headers also match that record.
- The two normal recipes still have their documented board-specific defines:
  Quartus I/O primitives versus OSS fabric SDRAM capture, and different PLL
  implementations. These were retained. This is identical source and build
  ID, not an identical elaborated whole-chip netlist.

The original saved Quartus package identified source `808bd7c2`, before the
`66706219` guard/display edits. Its reported 139.43/101.26 MHz is historical
context, not the controlled comparison here. A fresh compile was necessary.
Fresh Quartus reaches 139.30/99.54 MHz, confirming the gap survives the control.

## Measurements

Final same-clock Fmax, not placement estimates:

| Lane | Memory MHz | Pixel MHz | Result |
| --- | ---: | ---: | --- |
| Fresh Quartus, automatic FSM encoding | 139.30 | 99.54 | Both targets met |
| Fresh Quartus, `STATE_MACHINE_PROCESSING USER-ENCODED` | 122.28 | 98.08 | Memory misses |
| Saved OSS, seed 2 / HeAP weight 10 / GPU | 101.94 | 69.68 | Both miss |
| Fresh OSS, same netlist / seed 2 / weight 10 / `router2` | 71.97 | 65.64 | Both miss |

The CPU run starts from the original `synth.json`, including the original
build ID. All 19,983 routed-netlist cell placements match the saved GPU run.
Both OSS rows use final analogue signoff, and retain the memory, capture and
pixel clock constraints. The CPU path does not run the GPU-specific analogue
repair procedure; this comparison includes the complete routing policies.

Matched logical endpoints:

| Path | Quartus auto | Quartus user-encoded | OSS GPU |
| --- | ---: | ---: | ---: |
| `hps_ddr.port2.skid` → `ddr2_test.address[23]`, effective setup ns | 4.392 | 5.594 | 9.810 |
| Same path, setup slack ns | +3.300 | +2.098 | -2.118 |
| Same path, combinational depth | 2 levels | 3 levels | 6 LUT cells |
| `display.col_s[0]` → `display.ddr_ch_t[2]`, effective setup ns | 9.458 | 9.404 | 14.352 |
| Same pixel path, combinational depth | 8 levels | 9 levels | 11 arcs, including arithmetic and a route-through buffer |

“Effective setup” is setup relationship minus slack; this retains clock skew
and setup terms. Quartus's raw data delay is not directly interchangeable
with the sum of nextpnr's reported path terms. The pixel periods differ
slightly (13.464 versus 13.468 ns); that is far smaller than the gap.
The OSS generated cell suffix `_22` resolves to RTL address bit **23**, not 22.
The comparison helper resolves the register's Q bit through the netlist.

In OSS, the memory path contains 8.186 ns routing and 1.188 ns combinational
cell delay, plus clock-to-Q, setup and skew. The pixel path contains 11.286 ns
routing and 2.564 ns combinational cell delay. Quartus's corresponding data
routing contributions are 2.451 ns and 5.088 ns. These are routes through
different logic networks, not measurements of the same physical wires.

## What the source and reports explain

Yosys `fsm_detect.cc` explicitly declines all three `ddr*_test.state`
registers: “Register has an initialization value.” Quartus's mapping report
shows one-hot state encoding, and its fitter reports duplicated DDR state
registers. The controlled user-encoded Quartus run retains the original
three-bit state encoding and loses memory timing closure. Other synthesis,
packing, duplication and placement effects remain: binary Quartus still
needs only three levels on the matched path versus six in OSS.
This is a single-seed compiler control; its timing delta includes the resulting
physical implementation changes, not an isolated additive cost of encoding.

The pixel cone also differs structurally. The saved OSS critical path includes
three arithmetic-cell arcs and a route-through buffer, in addition to seven
ordinary LUT arcs. Its endpoints are near each other, but intermediate logic
visits distant LABs. Quartus maps the same source into a different cone.

nextpnr `mistral/delay.cc::predictDelay()` uses a distance model with a fixed
LAB-exit cost. Final signoff instead uses Mistral's analogue routing model.
The prediction has no explicit fanout/load or route-congestion term. HeAP
weights timing criticality while minimizing its wirelength objective; it does
not perform Quartus's register duplication and logic rewriting. Those are
concrete implementation differences and investigation targets, not a measured
apportionment of every nanosecond of the gap.

## Probes that are not fixes

- Doubling ABC9's wire cost from 600 to 1200 ps left the matched memory cone
  at six LUTs. A uniform six-input LUT cost model also left six levels.
- Classic ABC mapping produced five LUT levels on the memory cone, but increased
  the structural pixel-cone depth. It was not routed or selected for production.
- Raising HeAP timing weight from 10 to 1000 did not yield a legal GPU route
  before the diagnostic was stopped after approximately 13 minutes (iteration
  55, 126 overused wires). It has no final Fmax result; this is not proof that
  a longer run could not converge.
- Forcing Yosys FSM extraction reduced the memory cone from six to five LUTs.
  However, it explicitly warned that initialization is ignored and chose
  reset state `000`, whereas RTL initial state is `101` (`ST_DONE`). This is
  not an equivalent implementation candidate. No RTL or recipe change adopts it.
- Reloading the saved routed JSON failed on a frozen PLL physical pin. A
  diagnostic reroute after removing the routing state lost the 130 MHz clock
  constraint, so its timing is excluded. The valid CPU comparison performs a
  fresh full flow and verifies identical placement afterward.

## Reproduction and evidence

Research outputs are under `out/ramtest-timing-264/` in the issue worktree.
`baseline-sha256.json` hashes the copied original artifacts; `comparison-current.json`
and `comparison-binary.json` contain the matched results and input hashes.
`quartus-current/source-verification.json` records the frozen RTL hashes.
The small checked-in [evidence summary](2026-09-27-ramtest-timing.json) retains
identities, matched measurements and selected artifact hashes.

To reproduce the fresh Quartus control from this source tree, first verify the
RTL and generated-header hashes against the evidence summary, then create new
scratch projects from the existing recipe with these diagnostic overrides:

```sh
RAMTEST_MHZ=130 PYTHONPATH=sources/misteross python3 - <<'PY'
from pathlib import Path
from scripts import build_fes_ramtest_quartus as q
root = Path('sources/misteross').resolve()
q.BUILD_ID = 'cd3e4c315005fde83065dff7483ae0d7'
for variant in ('auto', 'binary'):
    output = Path('out/ramtest-timing-reproduce') / variant
    assert not output.exists(), 'use a fresh output directory'
    project = q.write_project(root, output.resolve())
    qsf = project / 'top.qsf'
    text = qsf.read_text().replace('NUM_PARALLEL_PROCESSORS ALL',
                                   'NUM_PARALLEL_PROCESSORS 4')
    if variant == 'binary':
        text += 'set_global_assignment -name STATE_MACHINE_PROCESSING "USER-ENCODED"\n'
    qsf.write_text(text)
PY
```

Run `quartus_sh --flow compile top` inside each generated `project/` directory,
using Quartus 17.0.2. These are diagnostic compiles; do not seal their output
as a normal recipe package. For the OSS CPU control, use the pinned
`nextpnr-mistral` with the saved `synth.json`, device `5CSEBA6U23I7`,
`cores/fes-ramtest/constraints.qsf`, `boards/de10nano/clocks.sdc`, `--freq 74.25`,
`--seed 2 --placer-heap-timingweight 10 --placer-heap-critexp 2 --router router2`,
and `--timing-allow-fail --detailed-timing-report`, with fresh paths for
`--rbf`, `--write`, and `--report`. Resolve QSF/SDC paths under `sources/misteross`.
Requesting RBF output runs final analogue signoff. Set
`NEXTPNR_MISTRAL_ARC_DUMP` to a fresh absolute TSV path to retain arc details.

To inspect an existing fitted Quartus database, use a private project copy:

```sh
quartus_sta -t sources/misteross/scripts/ramtest_timing_paths.tcl \
  /absolute/path/project/top.qpf /absolute/path/reports
python3 sources/misteross/scripts/ramtest_timing_compare.py \
  --timing /absolute/path/timing.json \
  --netlist /absolute/path/routed.json \
  --quartus /absolute/path/reports/memory-exact.rpt \
  --quartus /absolute/path/reports/pixel-exact.rpt
```

The Tcl script's exact-bit reports are specific to the issue's seed-2 failure;
its broader endpoint reports help inspect other seeds. It performs timing
analysis on an existing fitted database and does not compile, seal or deploy.
The Python helper likewise does not replace package timing validation.

No region constraints, manual RTL duplication, handshake pipeline, lowered
clock target or compiler-lock change was adopted. There is no newly qualified
130 MHz OSS package and no hardware was accessed. The next useful compiler
experiment is initialization-preserving FSM extraction plus comparison of the
remaining binary control cones; a safe change needs sequential equivalence
and multi-design timing regression evidence before entering a producer lock.
