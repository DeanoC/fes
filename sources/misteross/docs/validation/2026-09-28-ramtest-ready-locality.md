# RAM tester: bounded control-and-data locality, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), test whether a coherent
control-and-data group can move toward the passing Quartus placement while
preserving all unrelated placements and logic. FES base is `953c07ab` and
compiler base is `f2097df2`, with diagnostic result `ffe2351e` on
`probe/hps-ready-locality`. The accepted corrected baseline is 116.918037 MHz
memory; the prior 117.868927 MHz replica-reuse candidate remains a separate
memory/pixel tradeoff. Replica reuse is disabled in this experiment.

## What Quartus actually groups

A new connectivity query uses a private copy of the same saved Quartus fit.
Its compiled inputs and reports match the previous evidence. No synthesis,
fitter, STA or hardware run is performed.

| Saved Quartus group | Count | X range | Y range |
| --- | ---: | --- | --- |
| DDR1 writedata producer FFs | 64 | 28–37 | 60–66 |
| Their combinational nonclock fan-in | 111 | 28–37 | 59–66 |
| Register/hard boundaries of that fan-in | 74 | 28–39 | 56–65 |
| Port1 bulk data-bank FFs | 214 | 34–40 | 59–66 |

The upstream boundary includes the address and idle counters, phase state and
handshake controls. The passing fit keeps data generation and timeout/address
state nearby as well as the enable roots and destination banks.

Each Quartus writedata source bit connects to its corresponding skid-data
register. Of the 64 extracted paths, 34 terminate at D through an LCELL feeder
and 30 at SDATA with SLOAD tied high. This is connectivity evidence only; no
new path timing or Boolean-mask proof was performed. These different fitted
implementations do not provide equivalent-route timing-model calibration.

## Bounded hypotheses

The exact groups are derived from the retained OSS placed netlist:

- Five ready-control roots, their immediate producers and 223 controlled FFs
  make 231 cells guarding 224 ENA/SCLR pins.
- Adding 156 immediate DATAIN LUTs makes 387 cells.
- Adding 64 writedata producer FFs and their 204 combinational nonclock fan-in
  LUTs makes 655 cells: 287 FFs and 368 ordinary ALUT2–6 cells.

The larger group still leaves address/idle/state register boundaries outside.
Moving it can therefore exchange a faster ready path for slower data or
control inputs. It must be measured rather than inferred from distance.

Probe exactly two rigid translations: 387 cells by `(dx=0, dy=41)`, and 655
cells by `(dx=0, dy=43)`. Preserve each cell's z position and all connectivity,
parameters, pin states and unrelated placements. The target LABs are empty.
The smaller group includes all 27 directly paired FF/DATAIN producers, but
neither group is a complete independent packing macro.

Eight cells in the larger group share three source LABs with fixed carry
cells. Those carry cells and their constraints remain fixed; ordinary group
members can leave only if complete source and destination legality passes.
No legality rule is relaxed to force a placement.

Each probe binds the whole group before checking legality, records the
candidate, and restores the baseline exactly. An illegal candidate records
its failed predicates and is restored without a timing qualification. A legal
candidate records prediction changes, actual clock-domain pairs and global
clock results. These are imported-context placement predictions, not routed
Fmax or hardware results.

The affected boundary includes shared-input fanout siblings, moved-register
data/control inputs and downstream output endpoints. Vector ports are
expanded: the smaller group's 37 grouped JSON output-port labels represent
146 indexed pins. The complete shared-input/output guard closures contain
1,715 and 2,095 terminal pins, including 113 indexed HPS pins. Timed domains
and untimed exclusions must be explicit; the original 224 ready endpoints
alone are insufficient.

Individual setup regressions are diagnostic information. Any later routed
candidate must be judged against memory speed, the required pixel/capture
clocks and hold safety; a noncritical path need not improve individually.

## Results

Both complete translations pass occupied-BEL legality and restore the original
placement, connectivity, indexed users, pin states and cached LAB input counts.
Neither is selected for a full route: both substantially worsen the imported
placement prediction.

| Placement prediction | Original | 387 cells, +41 rows | 655 cells, +43 rows |
| --- | ---: | ---: | ---: |
| Memory MHz | 77.041603 | 48.081547 | 46.339203 |
| Pixel MHz | 84.990654 | 84.990654 | 84.990654 |
| Capture MHz | 397.569427 | 397.569427 | 397.569427 |
| Affected setup regressions | — | 789 | 885 |
| New/worsened hold violations | — | 0 | 0 |

These predictions are not comparable to the routed 116.918037 MHz baseline as
absolute Fmax measurements. The placement predictor uses BEL distance; HPS
arcs use the atom's location, not independently calibrated port-specific routes.
The result rejects these two rigid moves as promising placement candidates;
it does not reject Quartus-inspired locality in general.

The smaller trial's worst guarded endpoint becomes `port1.owed_one.DATAIN`
at −13,106 ps. The larger trial's worst is `ddr1_test.write_cycles[11].ENA`
at −13,888 ps. The largest individual regressions are at
`port1.beats_owed[1].DATAIN`: −14,667 and −15,430 ps. These registers are
outside both translated groups. Group size alone does not capture the feedback
and timeout boundaries that Quartus also places nearby.

The exported critical paths make the split concrete. In the smaller group,
the path from the moved skid register to fixed `owed_one` logic crosses the
group boundary three times: predicted routing arcs of 5,515, 5,170 and 5,105 ps.
The larger group's critical timeout path crosses twice, at 5,200 and 4,900 ps.
The ready controls also regress: 221 of the original 224 endpoints worsen in
each trial, while three improve. Moving the ready-control group toward the HPS does not
offset these longer feedback paths.

The 387-cell trial has 1,714 finite timed domain rows. The 655-cell trial has
2,091 finite timed rows and three explicitly untimed rows. Each also includes
one constant-driven FF input with no clock-domain pair. No omitted domain or
nonfinite slack is silently treated as a timing pass.

The first import stopped before either move because the JSON snapshot lacks
PLL phase metadata. The test harness now asserts the saved PLL phase parameters
and restores the production packer's shared `ram_clock.pll` phase group,
0/6,538 ps shifts and measured frequency constraints. The sidecar records these
values, and all three clocks appear in the final timing census. This bounded
test-only reconstruction does not establish complete fresh-context equivalence
or change production clock handling. The failed diagnostic is retained.

## Next compiler step

Keep both translations out of the optimization stack. The accepted routed
baseline remains 116.918037 MHz, with replica reuse retained separately as
the 117.868927 MHz tradeoff; 130 MHz is unresolved.

Before another move, use the actual boundary paths to form a group that keeps
skid/enter-write/owed feedback together and includes or localizes the DDR1
idle/state/timeout dependencies. Compare those exact cones with Quartus's
nearby and duplicated state registers. Evaluate the cut and legal packing
before selecting a bounded new placement trial; do not enlarge the group
solely by geometric proximity or assume a Quartus coordinate copies its QoR.

## Verification and scope

The backend suite passes 49 tests with the optional imported fixture skipped
in that default run; the actual imported fixture passes separately. Three
focused transaction tests cover atomic movement/restoration, illegal packing,
occupied destinations, exception restoration and disabled-mode neutrality.
Absent and empty diagnostic settings preserve the previous compiler's complete
197-cell control module. Nine independent checker tests pass, and both actual
trials pass structural, legality, indexed-boundary and phase checks.

The independent review also verifies the read-only path exporter and the
capture clock's shifted, falling-edge relationship. Its Fmax must not be
computed as the simple reciprocal of the exported path sum. The Quartus
grouping and analysis regenerate byte-identically across Python hash seeds.
Final validation reproduces six derived records byte-for-byte and checks
98 nested input hashes against the frozen compiler and source identities.

Host-only compiler research. No production RTL, recipe, compiler lock, ABI,
shared contract or hardware changes. The original placement is restored after
both probes. Raw evidence is `out/ramtest-ready-locality`; the adjacent JSON
records source and artifact hashes after validation.
