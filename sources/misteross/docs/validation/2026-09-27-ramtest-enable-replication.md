# RAM tester: generic enable replication, 2026-09-27

Compiler follow-up to the [enable-locality probe](2026-09-27-ramtest-enable-locality.md)
for [FES #264](https://github.com/DeanoC/fes/issues/264), based on FES `2ce945f5`
and nextpnr `19a5fe4a`. The general pass reaches **116.4415 MHz memory** with
unchanged RTL. It remains opt-in; 130 MHz is not closed. No producer lock,
shared contract or hardware acceptance changes.

## General compiler change

`--replicate-enables N` enables a bounded pass after HeAP placement, with a
budget from zero to eight copies and a default of zero. The implementation is
committed as [`71c513e1`](https://github.com/DeanoC/nextpnr/commit/71c513e1539c5adc1e16c92882dc731fe24ebf9b)
on nextpnr branch `feat/mistral-enable-replication`; the adjacent
[evidence record](2026-09-27-ramtest-enable-replication.json) records its exact
commit and binary. It branches from the placement-option fix, not the
fixture-specific probe, and contains no RAM-test signal-name or coordinate
selection rules.

An eligible ALUT2–6 drives only ordinary FF enable pins on one clock and edge
across multiple LABs. The pass ranks failing endpoints using predicted setup
slack and criticality. It searches within Manhattan distance three of up to
four critical sink LABs, considering at most 32 original drivers. Every moved
sink must improve its predicted routing delay by at least 250 ps; every LUT
input must have nonincreasing predicted delay. Whole LAB groups move together,
and at least one group remains on the original driver.

Only one copy per original driver is allowed. Each upstream signal net may
gain at most one input pin across the pass. Folded constants and input/enable
polarities are preserved. Frozen, constrained, clustered and protected cells,
occupied memory LABs, global/routed nets, I/O and hard-block connections, and
clock inputs are excluded. Ordinary logic in a memory-capable LAB remains
eligible. Fresh timing analysis rejects new or worsened predicted hold
violations; legality and original placements are checked after transformation.
Rejected trials remove both their temporary net and its alias.

These admission rules are conservative geometric estimates. They do not model
the additional electrical load of a copy or guarantee routed improvement.
The final route is the qualification measurement.

## Controlled result

The full flow uses the same initialized-FSM synthesis JSON, BUILD_ID, seed 2,
HeAP exponent 2, beta 0.5, timing weight 10, original QSF/SDC and GPU 1 as the
111 MHz control. The budget is four, but only one copy qualifies.

| Candidate | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Original OSS control | 101.9368 | 69.6767 |
| Initialized FSMs and effective exponent 2 | 111.0864 | 75.3239 |
| Fixture-specific locality probe | 114.5607 | 78.8768 |
| Generic enable replication | **116.4415** | **77.9059** |

The generic selector independently finds `hps_ddr.port1.slot_free_MISTRAL_ALUT3_B`.
It creates an identical ALUT3 at `MISTRAL_COMB.37.25.0`, moving four burst-count
ENA users in LABs (34,25) and (35,25). The other 107 users retain the original
driver at (24,20). The minimum predicted output improvement is 995 ps; the
three input predictions change from 2125/1710/1855 ps to 1170/1245/300 ps.
Unlike the previous manual probe, the pass does not move the additional sink
in LAB (34,22).

All **14,890** original pre-route BELs match the control exactly and remain
unchanged by replication. Independent before/after checks preserve **103,057**
original pin states, LUT masks, initialization, other connectivity and metadata.
The only logical changes are one equivalent combinational copy, its output net,
and the four ENA rewires. Pin-state sidecars cover folded polarities omitted by
ordinary JSON; the proof also handles indexed pins grouped as buses in JSON.

The route completes legally, emits an RBF and finishes analogue signoff timing.
Capture-clock Fmax remains 332.4941 MHz. Final timing reports no hold violations;
the sole warning is the memory setup-frequency miss. Memory improves **4.82%**
over the 111 MHz control, **1.64%** over the manual probe and **14.23%** over the
original OSS result. Pixel remains above 74.25 MHz, though below the manual
probe. Whole-design rerouting means its change cannot be attributed solely to
the selected memory sinks.

The new limiting memory path is HPS `cmd_ready_1` to
`hps_ddr.port1.skid_address[27].ENA`, which still uses the original enable
driver. It has two LUT arcs: 0.800 ns logic, 6.633 ns routing, 1.177 ns
clock-to-Q, 0.174 ns skew and −0.196 ns setup, totaling **8.588 ns**.
This is a remaining physical enable-distribution limit. The result does not
establish delay-model pessimism or explain the entire Quartus gap.

## Verification and limits

The private HIP build and all 17 backend tests pass: nine replication tests and
eight existing FES tests. Replication tests exercise real Mistral contexts,
placements and timing, including successful copying, constants/inversion,
whole-LAB movement, protected/frozen/mixed consumers, ordinary logic in
memory-capable LABs, input-delay rejection and cleanup/retry, alias collisions,
and input nets shared with hard blocks or clock pins. The two boundary tests
failed before their guard was added and pass afterward.

Production-policy tests cover input-delay non-regression, whole-group gains,
criticality, hold rejection and budget bounds. Default and explicit-zero modes
both reproduce the entire 197-cell reference module exactly. Invalid budgets
are rejected. The independent proof checker passes historical, zero-copy and
two-copy controls and rejects eleven corruptions, including shorted replica
outputs. Three timing-parser tests pass. Independent source and snapshot
reviews found and verified fixes for alias cleanup, identifier-order changes,
and the proof checker's cross-replica output check.

Actual timing-analyser-triggered hold rollback and occupied-memory exclusion
lack dedicated backend fixtures; the hold policy and exclusion code were
reviewed. This is one design and seed, not broad qualification for enabling the
pass by default. The next targets are the remaining original enable fanout and
qualification across placements/designs, with routed timing as the criterion.

Raw artifacts are under `out/ramtest-enable-replication/`, including the frozen
binary, invocation, snapshots, route, timing, test logs and regeneration helpers.
Set `NEXTPNR_MISTRAL_ENABLE_REPLICATION_DUMP` to a filename prefix to obtain
before/after JSON, pin-state TSVs and a replica manifest. An earlier run was
stopped during placement to add the shared-input boundary guard; its saved
`budget4-c2-v1-stopped` artifacts supply no final timing result. No PR or
hardware operation was performed.
