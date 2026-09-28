# RAM tester: timeout remapping with retained placement, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), apply the
[proven local timeout/enable rewrite](2026-09-28-ramtest-timeout-partition.md)
after the best OSS placement and accepted enable replication. FES base is
`c2dd0b87`; the private nextpnr diagnostic starts from `71c513e1`.
The diagnostic compiler commit is `5dd493476bfcb15cb7671c1ea4b72a8e04c3daf1`.
This is a host-only, fixture-specific experiment, not a production pass.
The run preserves the baseline placement exactly and finishes legally, but
memory Fmax regresses to 112.51 MHz. This placement strategy is rejected;
the best result remains 116.44 MHz and 130 MHz remains unresolved.

## Controlled change

The previous synthesis rewrite shortened the timeout/control cones and improved
all 110 selected ENA maximum arrivals in one fit, but whole-design placement
changed and memory Fmax regressed to 106.92 MHz. This experiment starts from
the original synthesis JSON used by the 116.44 MHz baseline. It runs packing,
HeAP placement and enable replication with the same options, then replaces
only the six address/beats-enable root drivers with the same 41 ordinary LUTs.
The existing root nets and consumers remain connected throughout the change.

Every one of the 14,885 surviving packed cells retains its BEL, placement
strength, parameters, attributes, connections and indexed pin state. This
includes all FFs, shared old control interiors and the accepted port-1 enable
replica. The compiler also checks retained port user indices in process.
No register, latency, initialization, clock, reset or D-versus-ENA boundary
changes. All 114 external input references resolve to 112 existing FF Q pins
with normal polarity.

Only new cells are placed. A deterministic topological pass searches available
legal BELs within Manhattan distance six of the midpoint of each channel's
two old root locations. It minimizes the sum of the worst predicted input
and output arc delays among already bound endpoints, then distance and BEL
name. Future unplaced consumers are excluded from that score. Existing cells
are never displaced; all occupied BELs must pass legality afterward. Failure
aborts the private compiler invocation before routing. This diagnostic does
not implement a transactional production optimization pass or a global timing
objective; the fixed search region and topological greedy order are limitations.
All routing is subsequently rebuilt.

## Proof and controls

The earlier arbitrary-state SAT proof covers all 58,673 boundary sink bits
of the synthesis rewrite. An independent checker connects that proof to the
actual packed transformation. It compares effective LUT truth tables after
input inversion and constant handling, verifies 63 LUTs in the original
source-to-packed cones and all 41 replacement LUTs, and checks exact
preservation of surviving cells, original net metadata, root nets and sink
pin polarities. It rejects unexpected drivers, placement collisions and
combinational cycles.

The saved-placement preflight places all 41 cells legally. Its checker proves
14,885 retained cells and 103,034 retained pin states exact. Importing this
snapshot requires restoring the indexed pin-state sidecar and one PLL
scalar/indexed output connection omitted by JSON round-trip serialization.
That repaired import is used only for preflight; the measured run starts from
fresh synthesis input. A separate checker compares the fresh run's prepatch
snapshots with the best baseline, including the exact accepted replica.

The default-off and empty-prefix controls reproduce the complete 197-cell
reference module exactly. Three payload-generator tests pass. The backend
suite passes 18 tests; its optional snapshot test is skipped in the generic
run and separately passes with the real baseline snapshot. The two focused
backend checks cover identifier-neutral disabled operation and enabled legal
mutation. Independent checker coverage includes four positive cases and 15
rejected mutations, including inversion, constants, metadata, connectivity,
placement and real routing unexpectedly present on new nets.

## Physical result and next experiment

| Final analogue Fmax | Best baseline | Placed timeout rewrite |
| --- | ---: | ---: |
| Memory | 116.441544 MHz | 112.511253 MHz |
| Pixel | 77.905884 MHz | 75.414780 MHz |
| Capture | 332.494110 MHz | 332.494110 MHz |

The fresh prepatch snapshots reproduce both baseline placement and the single
accepted enable replica exactly, including all 103,061 indexed pin states.
The actual mutation passes the same independent proof as the preflight.
All 14,926 postpatch cells keep their BELs through routing; the backend adds
5,092 normal `MISTRAL_BUF` route-through cells, removing no postpatch cell.
The run completes normally in 568.36 seconds with a legal 7,007,204-byte RBF
and no final reported hold violations. Memory is the sole timing warning;
pixel and capture pass.

Of the 110 selected ENA endpoints, 84 have earlier maximum arrivals and 26
regress. Every regression is in the DDR1 address group:

| Endpoint group | Baseline worst arrival | Candidate worst arrival |
| --- | ---: | ---: |
| DDR0 address ENA | 9.969 ns | 9.541 ns |
| DDR0 beats ENA | 9.293 ns | 8.361 ns |
| DDR1 address ENA | 10.042 ns | 10.367 ns |
| DDR1 beats ENA | 9.933 ns | 8.773 ns |
| DDR2 address ENA | 10.055 ns | 9.041 ns |
| DDR2 beats ENA | 10.118 ns | 8.152 ns |

These are propagated maximum arrivals from any positive-edge memory launch,
not setup slack or fixed-launch timeout delay. They include changed routing
and clock propagation, despite retained FF placement.

The new limiting path is `ddr1_test.idle[20]` to `ddr1_address[13].ENA`:
five LUTs, 1.201 ns logic, 7.116 ns routing, 0.731 ns clock-to-Q, 0.036 ns
skew and −0.196 ns setup, totaling 8.888 ns. Its replacement logic runs
through sites (35,17), (34,18), (37,20), (33,27), (33,21), then reaches the
fixed destination at (23,16). The last three routing arcs contribute
1.500, 1.095 and 2.011 ns respectively. This is a new critical endpoint,
not a matched-path comparison with the baseline's worst ready/skid path.

The search bounds expose a concrete limitation: DDR1's old enable roots are
at (27,16) and (39,26), each Manhattan distance 11 from the chosen midpoint
(33,21). Neither old site is inside the radius-six candidate region. The
address driver therefore cannot be considered at its former location under
this policy. The corresponding old root sites for the other two channels
are inside their search regions. This geometric restriction and the lack
of future-consumer costs suggest the next controlled test: place new output
roots near their own fixed consumer groups and refine only the replacement
logic using propagated timing costs. Keep every surviving original cell and
replica fixed, retain the same proof checks, then reroute. The current result
does not establish that this next strategy will improve Fmax, or that the
local Boolean rewrite is intrinsically worse with fixed surrounding placement.

## Scope and reproduction

Raw evidence is under `out/ramtest-placed-timeout`. `generate_payload.py`
constructs the diagnostic fixture from the two hashed synthesis netlists;
`check_snapshots.py` independently checks the actual packed mutation.
`run_route.py` clears other experimental Mistral environment switches and
uses the unchanged Mistral device library `7ed06e21`, GPU 1, seed 2,
HeAP exponent 2, weight 10, default beta 0.5 and
replication budget 4. `check_baseline.py` verifies the baseline identity;
`compare_arrivals.py` compares matched ENA maximum arrivals, which are not
setup slack or fixed-launch path delays.

The adjacent JSON records 71 raw-artifact and 31 external-input hashes.
Payload generation, arrival/path measurements and placement checks regenerate
byte-for-byte; the final summary regenerates exactly. Independent source,
proof and evidence reviews found no material blocker.

No production RTL, recipe, compiler lock, shared contract or hardware state
changes. No equivalent-route calibration or timing-model pessimism is claimed.
