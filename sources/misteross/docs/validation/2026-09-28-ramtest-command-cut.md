# RAM tester: joint HPS command/skid enable guard, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), extend the
[broader ready cut](2026-09-28-ramtest-ready-cut.md) to the command-enable
branch that became limiting. FES base is `aeb15d3a`; compiler base is
`341d3d94`. The direct measured control is 115.780945 MHz, with the retained
118.567703 MHz best kept separately. This is a bounded, host-only compiler
experiment on unchanged RTL.

## Hypothesis and scope

The previous cut improves every one of its 107 skid-enable endpoints but
regresses every one of the retained command root's 109 endpoints. Its critical
ready path still traverses slot-free ALUT2 plus command ALUT4. The equivalent
broader function is:

```text
command_ENA = (ready | ~command_valid) & (skid | filler | from_core)
command_valid = m_read | m_write
```

Use the already proven ALUT5 with inputs A..E command_valid, ready, skid,
filler, from_core and mask `0xddddddd0`. Keep the existing command-valid OR
boundary; the alternative direct-six-input cut would add loads to both command
FFs and require a wider LUT. The actual packed ten-leaf proof covers all 1,024
arbitrary assignments, without reachable-state assumptions.

Remap only `hps_ddr.port1.slot_free_MISTRAL_ALUT4_C`, preserving its output
net and all 109 FF ENA consumers. No cell or net is added. Keep the previously
optimized skid root and from_core cell at their measured placements, together
with the generic replica, all DDR1 improvements and every other existing cell.
Record each changed source-net load, including ready increasing from two to
three users and from_core from one to two.

## Physical selection and guard

Test the expanded root at its original `(27,22,13)` BEL, then search legal
sites within Manhattan radius six. Other cells stay fixed. Exclude protected
LABs and verify every occupied BEL. The selector uses a fresh timing graph for
the changed topology and requires a strict improvement in the worst of the
109 command-enable setup slacks.

Guard the union of old/new root-input fanout and the root output up to
sequential input pins. This is broader than just the 216 command/skid ENAs:
read-only traversal finds 719 FF inputs (497 ENA, 197 DATAIN, 25 SCLR) and the
HPS command-valid input. All must have finite analyzable setup slack and remain
nonregressing. The ready-only subset has 224 endpoint pins, including the four
replica sinks and skid SCLR. Stop at registers; never traverse from their
inputs to Q. Reject unsupported cells, clocks or combinational cycles.

Fresh clock constraints, predicted hold nonregression and full legality are
mandatory before routing. Predicted geometry does not model all electrical
costs of added loads, so passing these guards alone does not establish QoR.
The routed result qualifies the combined mapping and root placement; it does
not isolate Boolean mapping from relocation or global rerouting.

## Imported preflight

The ALUT5 is legal at the original `(27,22,13)` site. The bounded search
examines 60 legal LAB representatives and selects `(29,22,0)`. Command-enable
worst predicted slack improves from 4,067 to 5,337 ps; all 109 command pins
improve by 840–1,680 ps. The other 611 guarded pins are unchanged. Predicted
hold violations remain zero, and the HPS capture clock matches the FF clock.

These absolute slacks come from the imported, clock-unconstrained context.
They establish preflight feasibility, not timing closure; the fresh flow must
repeat the guards with its actual memory clock constraint. This experiment
combines the remap with relocation even though the original site is legal.

## Compiler and test evidence

Compiler `d664a6f76161caf38eae853d250f06b0f78ba847` on
`probe/ramtest-command-cut` adds the opt-in
`NEXTPNR_MISTRAL_COMMAND_CUT=<snapshot-prefix>` after the ready-cut hook.
The frozen binary `out/ramtest-command-cut/tools/nextpnr-command-cut` has SHA-256
`4042dff062a1f63b35e28785f10ce2ea65adbec96eb409c6597638c736d031d2`.
No production recipe or compiler lock selects this diagnostic.

The HIP build passes. CTest covers 27 cases: 26 pass and the optional import
case is skipped there, then exercised among ten passing focused cases.
Absent/empty-prefix modes reproduce the entire 197-cell control module.
Policy RED→GREEN checks cover all 1,024 truth assignments and the distinction
between improving the command subgroup and preserving an unrelated worst pin.

The independent checker passes actual preflight and rejects 25 graph/guard
mutations plus two load-table mutations. It verifies 14,927 unchanged nonroot
cells, all 103,295 old pin states, one new input pin, 109 unchanged consumers,
the exact 720-endpoint closure and its 224-pin ready subset. The load table
contains exactly eight old/new input nets: seven change; skid stays at 118
users. It rejects missing/incorrect counts, including sentinel timing values
and missing HPS clock evidence. Physical STA and packing legality remain
reviewed compiler checks, not independent physical simulations.

The fresh route uses the original initialized-FSM synthesis from Yosys
`c156dd886`, Mistral `7ed06e21`, GPU 1, seed 2, HeAP exponent 2, weight 10,
beta 0.5 and generic replication budget four. The timeout-root rewrite,
retained DDR1 copy, timeout refinement and ready cut all remain enabled.
Thirteen RTL inputs, generated headers and BUILD_ID remain hash-matched.

## Fresh clocked execution

Fresh execution selects the same `(29,22,0)` site after 60 STA candidates.
With the actual memory constraint, command worst setup slack improves from
-1,709 to -439 ps. All 720 guarded pins remain nonregressing and predicted hold
violations remain zero. HPS command-valid capture is explicitly timed on the
same rising-edge memory clock as the FF endpoints. The earlier ready cut
repeats its -1,774 to -1 ps guard before this transformation.

All earlier fresh stages, including the complete module and pin states before
the command cut, match the 115.78 MHz control exactly. The independent checker
repeats the actual 1,024-row proof and full preservation/closure audit.

## Routed result and decision

| Configuration | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Retained best stack | 118.567703 | 76.958595 |
| Direct ready-cut control | 115.780945 | 77.724236 |
| Command cut plus root relocation | **115.021851** | **80.951996** |

The route completes legally in 619.49 seconds and emits a 7,007,204-byte RBF.
Capture remains 332.494110 MHz. The sole warning is the 130 MHz memory miss;
no final hold violations are reported. All 14,928 post-cut BEL placements
survive routing, with 5,088 ordinary route-through buffers added.

**Do not include this configuration in the best stack.** Preserve the branch
and proof as a separate candidate. The best remains 118.567703 MHz and 130 MHz
is unresolved. The predicted joint guard passes, but routed nonregression does
not: global routing and its delay evaluation change after the local remap.

Maximum propagated arrival comparisons against the direct control:

- Command enables: 106 improve, three regress; worst 10.109→9.890 ns. Former
  critical `m1_burstcount[5].ENA` improves 10.109→9.760 ns.
- Skid enables: all 107 regress by approximately 0.688 ns; worst
  8.655→9.343 ns, with `skid_address[27].ENA` 8.494→9.182 ns.
- Four old replica sinks: each regresses by 0.812 ns; worst 9.351→10.163 ns.
- Full 720-pin guard: 298 improve, 345 regress, 77 are unchanged; worst
  10.109→10.163 ns. HPS command-valid improves 7.528→6.322 ns.
- DDR1 address: all 29 improve further, worst 9.360→9.295 ns. Across all
  110 timeout endpoints, 50 improve and 60 regress. The retained DDR1 copy's
  burst-end arrival changes 7.378→7.456 ns.

These are arrivals, not setup slacks or fixed-launch path delays. In particular,
the uniform skid-arrival change does not by itself identify the launch signal
or prove an added-load effect.

The new critical path is HPS `cmd_ready_1` to `skid_burstcount[7].ENA` through
the unchanged slot-free LUT and the original four-user replica:

```text
HPS BEL (52,53)
  --4.687 ns--> slot_free ALUT2 (30,26,48)
  --1.189 ns--> retained replica ALUT3 (37,25,0)
  --0.822 ns--> skid_burstcount[7] FF (34,25,22)
```

Logic totals 0.800 ns and routing 6.698 ns. Add 1.177 ns clock-to-Q,
0.215 ns skew and -0.196 ns setup for **8.694 ns effective setup**, consistent
with 115.021851 MHz. The ready-to-slot-free routed arc grows 3.868→4.687 ns.
Its logical and physical endpoints are unchanged, including slot-free A's
physical C pin mapping.

## Shared-route investigation and next step

The next question is why the shared ready distribution deteriorates, rather
than whether another output branch can be reduced to one LUT. Across the
refinement, ready-cut and command-cut runs, HPS-ready fanout grows 1→2→3,
while the same ready-to-slot-free reported arc grows 3.473→3.868→4.687 ns.
Physical route topology and global routing also change, so this is not a
controlled measurement of capacitance from added loads alone.

The source is physical `GIN.51.64.18` and the slot-free sink is
`GOUT.30.26.68`; the HPS BEL coordinates above are not the distributed physical
pin origin. Read-only reconstruction finds source-to-sink paths of 14, 15 and
17 pips, and whole ready trees of 15, 23 and 28 wires respectively.

The existing from_core-to-skid connection retains the same seven-hop route
classification. Summed analogue rise/fall hop delays change 1456/1151→1465/1160 ps,
only 9 ps. That local arc alone cannot explain the approximately 688 ps skid
arrival regression; upstream arrival and the alternate ready path require
separate attribution.

The reproducible `route_study.py` and `route-study.json` retain these route
comparisons and exact input/source hashes. HPS-ready is absent from all three
analogue hop dumps; the exact analogue failure reason is not recorded. The compiler's fallback
can use observed per-pip delays or per-type calibration, not only static table
entries (`mistral/analogue.cc`, `getPipDelayCalibrated`). Consequently topology,
added load and cross-run calibration are confounded in the current reports.
Do not infer delay-model pessimism or an intrinsic RTL limitation from these
results. Instrument the ready path's analogue success/failure and each fallback
delay contribution, then compare the frozen routes under a common delay basis
before choosing another mapping or placement change. A controlled reroute of
this shared net is a subsequent test once those contributions are understood:
keep the graph, load count and all placements fixed, freeze unrelated routes,
and evaluate alternative ready trees with the same calibration state. That
isolates route-tree choice, not true electrical-load causality.

No production RTL, recipe/compiler lock, ABI or wire-contract change, hardware
programming or hardware acceptance is part of this experiment. No PR is opened.


## Verification and handoff

Frozen compiler source and binary hashes match, as do all proof inputs.
Nine generated measurement/proof files regenerate byte-identically, including
the read-only route study. Independent final review verifies 164 raw and 65 external hashes, nested
proof/route-study inputs, exact regeneration and the report conclusions, with
no blockers. Parent consistency is checked on the committed record. Raw results and reproduction helpers are under `out/ramtest-command-cut`;
the adjacent JSON seals artifact identities and measurements. The retained
best's switches and compiler lineage remain in `stack.json`.
