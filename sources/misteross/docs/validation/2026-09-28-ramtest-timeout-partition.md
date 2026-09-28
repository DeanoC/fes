# RAM tester: local timeout/enable remapping, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), test a local
timeout/control rewrite after the
[global ABC9 recovery experiment](2026-09-28-ramtest-abc-area-recovery.md)
regressed timing. FES base is `0a747088`. This is a fixture-specific,
host-only synthesis diagnostic, not a production compiler pass.
The rewritten enable endpoints have earlier maximum arrivals in the measured
fit, but final memory Fmax regresses from 116.44 to 106.92 MHz. The unchanged
HPS-ready/skid logic becomes the limiting path after placement changes.
The tested full-flow candidate is not adopted; 130 MHz remains unresolved.

## Isolated transformation

Start from the exact synthesized design used by the best 116.44 MHz OSS run.
Each DDR channel has an existing combinational signal implementing
`idle == 1000000`. That signal dominates every idle-bit dependency in the
selected address and beats-left ENA cones. Cutting there leaves 15 inputs
for address enable and 11 for beats enable, including the equality result.
All other inputs are existing FF outputs. The three existing comparators
have three LUT levels and use six, seven and seven LUTs respectively.

The diagnostic extracts the mapped Boolean functions downstream of that
equality boundary and remaps the two outputs per channel with the unchanged
default `synth_intel_alm`/ABC9 policy and original LUT cost/delay models. It
feeds each local function with a new comparator: four parallel six-bit equality
LUTs followed by an AND4. The four masks, for contiguous low-to-high groups,
are `1`, `1 << 9`, `1 << 52` and `1 << 3`; the reduction mask is `0x8000`.
The comparison therefore takes two LUT levels. Local remapping also reduces
the downstream control depth, which was four or five levels in the baseline.

Only the six original ENA root drivers are removed. Their output nets and
consumers retain their identities: 28/29/29 address-register enables and eight
beats-left enables per channel, 110 FF ENA pins in total. All 14,935 other
original cells, original
netnames, module ports and metadata remain exactly unchanged. Shared old
interiors are retained; the normal physical flow can remove unused logic.
The replacement adds 41 ordinary combinational cells, giving 14,976 cells
before packing: a net increase of 35. It introduces no register, latency,
clock, reset, initialization or D-versus-ENA change.

| Structural path before placement | Baseline | Local rewrite |
| --- | ---: | ---: |
| Ready1 → skid address[27] ENA | 2 LUTs | 2 LUTs |
| Idle2[23] → address[25] ENA | 8 LUTs | 4 LUTs |
| Idle1[14] → beats_left[2] ENA | 7 LUTs | 4 LUTs |

These are longest connected LUT chains, not routed or sensitized delays.
Across every idle-counter bit, the maximum address-enable depth for channels
0/1/2 changes from 7/8/8 to 4/5/4. All three beats-enable maxima change from
seven to four. The selected examples above therefore do not imply that every
replacement is four levels.
The new timeout/control structure still differs from Quartus's three-level
fitted cone. The experiment combines parallel comparison with local control
factoring; it does not isolate the gain from comparison depth alone.
Unlike a full synthesis policy change, unrelated mapped logic is preserved.
Placement and routing can still change throughout the design.

## Equivalence and preservation

An independent checker expands each original and replacement comparator's
single satisfying LUT rows into exactly 24 consistent literals encoding
1,000,000. It verifies the old/new graph cuts, six root replacements, original
consumers, complete outside-cell preservation, valid new primitives and
unchanged metadata. It rejects mutations to comparator masks, FF metadata,
ENA connections, driver uniqueness, primitive types, net initialization and
an idle-bit bypass. Three comparator-construction tests plus eight checker
tests pass; the small comparator fixture exhausts all 256 input values for
five independently specified constants.

The existing full boundary miter proves equality of all 58,673 FF/hard-block
inputs and top outputs for all 8,696 shared source bits. All 8,221 FFs retain
their names, parameters and initialization semantics. Clocks, resets, enables
and loads are included. The SAT problem has 52,726 variables and 104,916
clauses after common-logic simplification and passes without state assumptions,
disabled-cycle relaxation or ignored unknown cells. No new FF identity mapping
is needed because the original FFs are unchanged.

The initial representative-cofactor exploration is retained separately under
`cofactor-exploration`. It is not used for routing. The measured candidate
uses the existing proven equality signal directly as its local synthesis
boundary, avoiding any representative-value assumption.

## Physical result

The run uses frozen nextpnr
`71c513e1539c5adc1e16c92882dc731fe24ebf9b`, Mistral
`7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`, GPU 1, seed 2,
HeAP exponent 2, weight 10, default beta 0.5 and enable-replication budget 4.
These match the 116.44 MHz baseline. The input is the rewritten synthesis
netlist; this is a fresh physical run, not a replay of routed JSON.

| Final analogue Fmax | Best baseline | Local rewrite |
| --- | ---: | ---: |
| Memory | 116.441544 MHz | 106.917572 MHz |
| Pixel | 77.905884 MHz | 74.565651 MHz |
| Capture | 332.494110 MHz | 419.101227 MHz |

The run finishes normally with a legal 7,007,204-byte RBF and no final reported
hold violations. The sole final warning is the memory-clock timing miss;
pixel and capture pass. No enable replica qualifies, versus one in the best
baseline. The snapshot checker verifies that the zero-copy pass leaves all
14,925 packed cells and 103,277 pin states unchanged. Placement takes 831.43
seconds, mostly in legalization; the whole invocation takes 960.26 seconds.

All 110 selected FF ENA pins have earlier maximum arrivals in this fit:

| Endpoint group | Baseline worst arrival | Candidate worst arrival |
| --- | ---: | ---: |
| DDR0 address ENA, 28 pins | 9.969 ns | 8.577 ns |
| DDR0 beats ENA, 8 pins | 9.293 ns | 7.917 ns |
| DDR1 address ENA, 29 pins | 10.042 ns | 9.124 ns |
| DDR1 beats ENA, 8 pins | 9.933 ns | 7.597 ns |
| DDR2 address ENA, 29 pins | 10.055 ns | 8.777 ns |
| DDR2 beats ENA, 8 pins | 10.118 ns | 7.289 ns |

These are matched endpoint maximum arrivals from any positive-edge memory
launch, as exported by `detailed_net_timings`. They are not setup slack or
delay from a fixed timeout-counter launch; they include arrival propagation
and omit capture-clock/setup adjustments. Placement, routing and clock-tree
changes contribute, so the table does not isolate the Boolean rewrite's
physical timing gain.

The new worst memory path is HPS `cmd_ready_1` to
`hps_ddr.port1.skid.ENA`: two unchanged LUTs, 0.800 ns logic and 7.415 ns
routing, with clock-to-Q, skew and setup giving 9.353 ns effective setup.
The first LUT moves from (30,26) to (24,19); the second moves from (30,26)
to (23,1); the skid FF moves from (30,26) to (2,1). The HPS remains at its
fixed site (52,53). Its physical ready-pin origin is a separate issue from
that BEL coordinate. The baseline's worst path ends at skid address[27],
so the two worst-path totals are not a matched-endpoint delay comparison.

This validates a shallower local mapping and earlier arrivals at the selected
endpoints in one fit, while rejecting this complete placement/route result.
The next controlled experiment should preserve the known-good baseline
placement and pin states of every retained original cell, including shared
old interiors and the baseline's accepted port-1 enable replica. Legally place
only the replacement cells, then reroute. That would test whether the local rewrite's benefit survives
without moving the unchanged ready/skid cone across the device. It requires
explicit placement/pin preservation, legality and equivalence checks; this
record does not establish that the combination will improve Fmax. Global ABC
policy and broad HPS-placement changes remain unadopted.

## Reproduction and scope

Raw evidence is under `out/ramtest-timeout-partition`. `rewrite.py` emits
three local control modules and their synthesis scripts, splices the new
logic, and records the exact cell changes. `check_rewrite.py` is independent
of that implementation; the existing boundary-proof helper is reused from
`out/ramtest-control-factoring/equivalence/prove.py`. `compare_cones.py` from
that earlier experiment independently recounts path depth. The adjacent JSON
records tool identities, source/artifact hashes, proofs and timing.
The full rewrite, all three local mapped modules, identity-preserving cell
manifest, depth measurements, arrival comparison and placement comparison
regenerate byte-for-byte. The adjacent record hashes 69 raw artifacts and 61
external inputs. Eleven focused tests and the full boundary proof pass;
independent structural, semantic and final-result reviews found no blocker.

The same 13 RTL files and BUILD_ID remain selected. No production RTL,
compiler source, recipe, producer lock, shared contract or hardware state is
changed by this diagnostic.
