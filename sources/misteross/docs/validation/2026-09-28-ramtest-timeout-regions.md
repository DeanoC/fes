# RAM tester: timeout placement regions, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), isolate the search
region restriction found in the
[placement-preserving timeout experiment](2026-09-28-ramtest-placed-timeout.md).
FES base is `2722292a`; this private nextpnr diagnostic starts from `5dd49347`.
The original RTL and production compiler locks remain unchanged.

## Which changes accumulate

The best measured experimental stack combines initialized-FSM synthesis,
the GPU routing-convergence fix, correctly applied HeAP options, and generic
enable replication. Together these reach 116.44 MHz memory Fmax, versus the
original 101.94 MHz. The generic replication pass replaces the earlier manual
replication diagnostic; the two are not applied together.

Slower configurations are not added to this best stack. Their branches,
proofs and measurements remain available. A rejected fit does not reject
every underlying idea: initialized-FSM extraction initially regressed in
isolation, then contributed to the improved combined flow. The local timeout
rewrite is similarly retained as a proven equivalent candidate while its
physical implementation is investigated. The HPS placement variants and
global ABC area-policy variant remain excluded.

This experiment extends the previous private timeout diagnostic so its
implementation and proof machinery are reused. Its QoR must still beat the
best stack before becoming a new best candidate. None of these experimental
measurements has promoted the production compiler lock or established hardware
acceptance.

## Single changed variable

The previous timeout diagnostic preserves every surviving original cell and
adds 41 LUTs in place of six enable root drivers. Its radius-six search around
each channel's root midpoint excludes both old DDR1 roots: (27,16) and (39,26)
are distance 11 from midpoint (33,21). That run reaches 112.51 MHz, with the
DDR1 timeout/address path becoming critical.

The new `roots` region is the union of the original midpoint neighborhood and
radius-six neighborhoods around both old roots, for each channel. Candidate
sites must still be free and legal. The LUT graph, topological placement order,
input/output prediction score and midpoint-distance tie break are unchanged.
Every surviving original cell, indexed pin state and accepted baseline
replica remains fixed. Future unplaced consumers still do not contribute to
the greedy score. This isolates the candidate domain; it does not add timing
refinement or logic duplication.

Absent or empty region selection retains the earlier midpoint behavior.
With the entire timeout diagnostic disabled, region selection has no effect.
The same exact synthesis JSON, GPU 1, seed 2, HeAP exponent 2, weight 10,
default beta 0.5, replication budget 4 and Mistral library `7ed06e21` are used.
The full run starts from fresh synthesis and rebuilds all routing.

## Controls and verification

The diagnostic compiler is `35523128c7d22411f73800f00833b14106af44d2`.
Midpoint-mode preflight reproduces the previous before/after JSON and indexed
pin sidecars byte-for-byte; the original nine site-record columns also match.
Explicit empty-region selection reproduces that result. With the timeout
prefix absent or empty, the full 197-cell reference module matches exactly,
including when the region selector is set independently.

Both real placement preflights pass all four focused backend tests. The
backend suite passes 20 tests; its optional saved-snapshot case is exercised
separately. An independent region checker validates selected BELs, channel
identity, processing order, union membership and recorded distances. Its
five tests include two valid modes and 12 rejected mutations. The unchanged
semantic checker proves all 41 new LUT functions against the earlier
SAT-proven graph, preserving 14,885 existing cells, 103,034 pin states, root
nets and sink polarities. It also verifies the 63 original LUT functions
needed to connect the source-level proof to packed logic.

The expanded-region preflight changes 25 replacement-cell locations; 18
sites are outside the previous midpoint region. No surviving original cell
moves. Full routing may still change delays throughout the device; preservation
of placement alone is not a guarantee of unchanged timing on unrelated paths.

## Routed result

| Final analogue Fmax | Best stack | Midpoint timeout | Expanded region |
| --- | ---: | ---: | ---: |
| Memory | 116.441544 MHz | 112.511253 MHz | 116.333183 MHz |
| Pixel | 77.905884 MHz | 75.414780 MHz | 79.929665 MHz |
| Capture | 332.494110 MHz | 332.494110 MHz | 332.494110 MHz |

Expanding the region recovers 3.82 MHz relative to the midpoint configuration.
It remains 0.11 MHz below the best memory result, so it is retained as an
experimental candidate rather than promoted as a new memory best. The pixel
result improves. This single design/seed measurement does not establish a
robust ranking for such a small memory difference. The 130 MHz target remains
unresolved.

The fresh run's baseline placement and accepted replica match exactly before
the timeout rewrite. Its independent semantic and region proofs pass. All
14,926 postpatch cell BELs survive routing, with 5,092 ordinary route-through
buffers added and no postpatch cells removed. The run finishes normally in
584.71 seconds with a legal 7,007,204-byte RBF, no final reported hold violations
and one final warning for memory timing. The actual fresh placement changes
25 replacement sites relative to the previous fresh midpoint run, with 18
sites outside the midpoint region.

Of the selected 110 ENA endpoints, 84 have earlier maximum arrivals than the
best baseline and 26 regress; the regressions are all DDR1 address endpoints.
The DDR1 address group's worst arrival improves from 10.042 to 10.030 ns,
but that group maximum hides the 26 individual regressions. These propagated
arrivals include routing and clock effects and are not setup slack or delay
from a fixed launch register.

The new worst memory path lies entirely in retained logic:
`ddr1_test.idle[22]` to `ddr1_test.burst_end.ENA`, six LUTs,
1.412 ns logic, 6.612 ns routing, 0.731 ns clock-to-Q, 0.037 ns skew and
−0.196 ns setup, totaling 8.596 ns. The last two routing arcs take
1.748 ns from (37,21) to (24,16), then 2.299 ns back to the destination
at (37,23). The final ALUT4 drives 106 FF ENA pins, including many near
(24,16). This is a different endpoint from both previous worst paths;
the totals are not a matched-launch/endpoint comparison.

The next useful target is this remaining shared timeout/control net, including
whether a local enable copy can serve its distant consumer group under the
existing legality, polarity and input-delay guards. Broader mapping of the
retained timeout cone is another option. The result does not justify another
broad placement sweep or claim that replication on this particular net will
succeed. The existing best stack is preserved while this candidate provides
a more focused basis for the next controlled change.

## Saved-snapshot limitation and repair

The initial saved-snapshot preflights did not reproduce every internal packing
constraint. JSON preserves BELs and pin-state sidecars preserve polarity, but
it does not serialize carry-chain clusters and relative placement constraints.
The diagnostic protects LABs containing clustered cells. Consequently the
initial import admitted carry LABs that the fresh run correctly excluded,
causing five selected-site differences in midpoint mode and eight in roots
mode. This did not invalidate the fresh run's legality or semantic proofs;
it limited the preflight as a predictor of exact replacement placement.

Test-only follow-up `7a375fc8f555d0b73419c4641a36e549d013264d` reconstructs the original carry chains from CI/CO
connectivity using the packer's existing layout rules. It visits all 1,350
arithmetic cells in 94 chains and checks their actual relative BEL locations
before the diagnostic runs. The corrected preflights then reproduce all
41 selected sites and prediction scores from the authoritative fresh runs
in both modes. Original preflight artifacts are retained separately. The
frozen routing executable and its production source bytes are unchanged;
there is no reroute or substitution of measurements.

## Reproduction and scope

Raw evidence is under `out/ramtest-timeout-regions`. The frozen binary,
compiler identities, tests and original unchanged LUT payload are recorded
in `compiler-build.json`. `preflight-repair.json` records the separate test-only
follow-up. `run_route.py` records the exact command and experimental environment;
`check_baseline.py` and `check_snapshots.py` are reused unchanged from the prior
stage. `check_regions.py` independently checks the added site-domain rules.
The adjacent JSON includes the current experimental stack, proofs, measured
paths, matched endpoint arrivals, 107 raw-artifact hashes and 40 external-input
hashes. Measurements and the summary regenerate exactly; independent source,
proof and final-result reviews found no material blocker.

No production RTL, compiler lock, recipe, shared contract or hardware state
changes. No delay-model calibration or 130 MHz closure is claimed.
