# RAM tester: enable locality, 2026-09-27

Compiler diagnostic follow-up to the [placement-option fix](2026-09-27-ramtest-placer-options.md)
for [FES #264](https://github.com/DeanoC/fes/issues/264), based on FES `0f1d00f6`
and nextpnr `19a5fe4a`. No RAM-test RTL, producer lock, shared contract or hardware
acceptance changes.

## Question and controlled probe

The best preceding candidate reaches 111.0864 MHz memory and 75.3239 MHz pixel.
Its memory-critical path is HPS `cmd_ready_1` through two LUTs to
`skid_burstcount[7].ENA`, totaling 9.002 ns. Removing a LUT in a separate mapping
experiment reduced whole-design memory timing to 107.7702 MHz. This experiment
instead tests physical locality while preserving the two-level logic cone.

The shared enable driver has 111 FF users across 22 LABs. It sits at (24,20),
near three dense LABs containing 20, 18 and 14 users. Five burst-count users are
farther away, in LABs (34,22), (34,25) and (35,25). All three driver inputs are
near these outliers: slot-free at (30,26), enter-write at (37,25), and enter-read
at (39,27).

The diagnostic hook runs after ordinary HeAP placement. It duplicates this one
ALUT3 with the same LUT mask and input polarities, moves all five enable users
in those three LABs onto the copy, and leaves every existing cell's placement
unchanged. It searches free legal LUT sites in x=30..39, y=19..29, minimizing
maximum predicted input delay plus maximum predicted output delay. This is a
bounded placement heuristic, not a proof of optimal arrival time. The original
LUT continues to drive the other 106 users.

Whole-LAB grouping matters: Cyclone V shares an enable signal within a LAB and
supports only three distinct enable signals there. Splitting one LAB between
equivalent but separately named nets would consume extra control resources.
HeAP's analytic weights normalize by logical fanout; its final refinement still
times all 111 users. A mismatch between logical multiplicity and physical load
is a possible cost-model concern, not a demonstrated omitted-timing bug.

## Result

The probe routes legally, emits an RBF and finishes final analogue analysis:

| Candidate | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| All-FSM, exponent 2 control | 111.0864 | 75.3239 |
| Same placement plus one local enable copy | **114.5607** | **78.8768** |

The copy is at `MISTRAL_COMB.37.26.0`, selected from 941 legal candidates.
All **14,890** pre-route cells retain their exact control BELs; the old routed
control has an additional 5,091 route-created cells. The before/after check
preserves **103,057** original pin states, including every bit of 23 grouped
buses, and finds exactly the five intended ENA rewires.

The selected burst-count ENA's reported maximum same-clock arrival decreases
from 10.471 to 8.854 ns. These are timing-analyser arrival values, not effective
setup totals or an assertion that the worst launch path remains the same.
The new limiting memory path is `ddr2_test.idle[13]` → `ddr2_address[28].ENA`:
eight LUT arcs, 1.382 ns cell logic and 6.774 ns routing, totaling 8.729 ns with
clock-to-Q, setup and skew. Pixel timing also changes because the full design
is routed again; its improvement cannot be attributed solely to the local
memory path.

Memory improves about **3.1%** over the preceding best candidate and **12.4%**
over the original 101.9368 MHz control. It still misses 130 MHz. This single-seed
result supports a general LAB-aware replication experiment; it does not qualify
the hardcoded diagnostic as a production pass. The next compiler step is to
select critical enable nets and sink LAB groups from timing/placement data,
with equivalence, legality and whole-design timing checks. The remaining
idle/timeout control path is another concrete limit.

## Matching Quartus path

TimeQuest was rerun on a private copy of the already fitted, source-matched
Quartus project; no new synthesis or placement was performed. The query uses
`-through [get_pins -hierarchical {*f2sdram*cmd_ready_1}]` and the exact logical
`skid_burstcount[7]` endpoint. The HPS pin is internal to a timed path; querying
it as a launch register does not report that path.

Quartus reports one logic level, 3.624 ns data delay and 3.196 ns slack against
a 7.692 ns relationship: **4.496 ns effective setup**, versus 9.002 ns in the
OSS control. The HPS interface is at (52,53), the enable LUT at (35,62), and the
register at (37,64). The enable LUT still has fanout 109. Thus high fanout by
itself does not explain the difference. This comparison includes different
mapping and physical routes; it is not a calibration of either delay model.

## Reproduction and validation

Raw artifacts are under `out/ramtest-enable-locality/`. The diagnostic patch is
committed as [`369d6e4b`](https://github.com/DeanoC/nextpnr/commit/369d6e4ba480dd8d622baf5e1ad690e544943846)
on nextpnr branch `probe/ramtest-enable-locality`, based on `19a5fe4a`.
It is deliberately specific to this measured fixture and is not a
production optimization. Set `NEXTPNR_MISTRAL_ENABLE_LOCALITY` to the snapshot
prefix to enable it. With the variable absent, the entire 197-cell placement
fixture matches the unmodified compiler output exactly.

Before/after JSON snapshots record connectivity, LUT parameters, initialization
and placements. Separate pin-state sidecars record folded polarities omitted by
ordinary nextpnr JSON. The checker requires exactly one added LUT and net,
exactly five changed ENA connections in the selected LABs, unchanged original
cells and metadata, and identical input states on the copy. Runtime checks also
verify every occupied BEL remains legal before routing. An early run was stopped
before the transformation to add these polarity diagnostics; it supplies no
timing result.

The checker passes the real snapshots and rejects eight negative controls:
absent clone, changed LUT mask, wrong input, wrong ENA wiring, moved original
cell, changed clone polarity, changed FF-enable polarity, and missing bus-pin
evidence. Independent review confirms the snapshot proof, baseline placement
match and completed route. The diagnostic build succeeds; its existing compiler
warnings do not prevent completion. No PR or production-lock update is made.

The full route uses the unchanged all-FSM synthesis input, seed 2, exponent 2,
beta 0.5, timing weight 10, GPU 1, original QSF/SDC and BUILD_ID
`cd3e4c315005fde83065dff7483ae0d7`. The [evidence record](2026-09-27-ramtest-enable-locality.json)
identifies exact compiler bytes, diagnostic source patch, input hashes and
final artifacts.
