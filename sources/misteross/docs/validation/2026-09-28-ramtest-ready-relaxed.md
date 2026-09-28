# RAM tester: relaxed ready routing and Quartus locality, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), test whether other
nets block a cheaper ready1 route and compare the passing Quartus solution
for concrete mapping and locality hints. FES base is `95ada903`; nextpnr
base is `7e4130f6`; the diagnostic is `2c620ac63d4497bef1388406a096be3d45c5df02`
on `probe/hps-ready-relaxed`. The [fixed-occupancy exact search](2026-09-28-ramtest-ready-shortest.md)
proves the retained 3602 ps ready1 route is already a minimum under its
captured calibrated costs and occupied graph. Memory remains 116.918037 MHz.

## Read-only experiment

Relax other-net wire and pip occupancy for one exact shortest-path query,
while preserving static architectural reserved-route and CRAM restrictions.
Record the owners blocking the hypothetical path. Keep all actual routes,
placements, calibration, endpoint timing and final bitstream unchanged.
The hypothetical route is never bound and its delay is not a measured Fmax.

The existing Dijkstra engine and independent certificate checker establish
the lower bound under frozen costs. Physical adjacency and cost arithmetic
are checked against the device data and captured calibration; generated
adjacency and architectural predicates retain the documented source-review
boundary. Removing occupancy does not mean the displaced nets could actually
be rerouted while preserving their timing.

## Quartus comparison

A new Quartus CDB connectivity query runs on a private copy of the saved
same-RTL fit, constrained to 130 MHz and reporting 139.30 MHz memory. The
compiled database and saved reports match the original. No synthesis, fitter
or STA rerun is needed.

Both tools identify the ready1 fabric exit at `(51,64,18)`; the HPS block's
logical `(52,53)` location is not the route origin. Quartus's reported route
to its first skid-enable LUT is 1.746 ns. Current OSS ready1 reaches the
slot-free LUT in 3.602 ns and then the skid-enable LUT in another 1.407 ns.
These are different mapped paths and physical fits, not a calibration.
The reported routes follow the same initial fabric corridor through H6
`(46,64)`, V2 `(47,62)`, H3 `(45,62)` and H14 `(34,62)`. Quartus then enters
its nearby `(35,62)` LAB. OSS instead traverses three V12 segments down to
rows 50, 38 and 26 to reach its distant slot-free LUT. This is a concrete
placement difference visible in the route topology.

| Group | Quartus | Current OSS |
| --- | --- | --- |
| Slot-free / skid / command roots | All in LAB `(35,62)` | `(30,26)` / `(24,20)` / `(27,22)` |
| Main skid-enable bank | 109 ENA, x34–40/y59–66; max driver distance 7 | 107 ENA, x22–34/y14–23; max distance 12 |
| Command-enable bank | 107 ENA, x34–40/y59–65; max distance 7 | 109 ENA, x24–36/y13–29; max distance 14 |

Distances are Manhattan grid measurements, not delay estimates. Quartus also
clusters immediate producers: m_read `(35,60)`, m_write `(35,61)`, filler
`(35,62)`, from_core `(35,60)` and skid FF `(35,62)`. The useful hint is a
compact group containing both enable roots, their producers and both register
banks. Quartus uses one shared skid-enable driver in this extraction; high
fanout alone does not establish a need for further replication.

The bank counts are not identical logical loads. Quartus has six named
burstcount FF bits in each bank (0,1,4,5,6,7), while OSS retains eight. The
complete atom listing has no separately named bits 2/3, but this extraction
does not prove why they disappeared or whether they were aliased/merged.
Every extracted bank load uses ENA, so no D/ENA substitution is observed here.
OSS has an additional four-user skid-enable replica and a local one-user root;
its complete ready-control closure remains 223 ENA plus one SCLR.

The current limiting OSS register is `skid_burstcount[5]` at `(34,22)`, ten
columns from its `(24,20)` driver. The existing replica at `(37,25)` serves
bits 0,4,6,7 at x34–35/y25. This leaves a specific residual sink group to
assess, with both skid and command timing guarded. Its whole-LAB group is
a singleton: it is the only original-skid ENA user in LAB `(34,22)`. The
original and replica are both ALUT3 mask `0x32` with identical A/B/C nets.
Reassigning this sink would change their fanouts from 107/4 to 106/5 without
adding a LUT or input loads. It still requires refreshed FF/control metadata,
full occupied-LAB legality and preservation of pin polarity before routing.

Clock-reference accounting prevents a misleading model comparison. Quartus's
0.097 ns output-cell delay starts at its internal HPS FF. Its preceding clock
CELL contributes 1.161 ns, giving 1.258 ns from the atom clock pin. OSS's
1.177 ns clock-to-output value uses that atom-pin reference. Comparing 0.097
with 1.177 directly would omit the internal clock delay and invent a roughly
1 ns discrepancy. The properly referenced values still describe different
loads/fits and do not prove equivalent-route model accuracy.

Earlier broad HPS anchoring changed most placements and exposed slower
timeout cones. The broader ready cut improved the targeted skid branch but
regressed the retained command branch. Those rejected attempts remain
separate; locality and logic depth must be assessed across both branches.

## Results and decision

The fresh GPU 1 / seed 2 flow finishes normally in 624.15 seconds. All six
pre-route stages match the corrected retained baseline. The relaxed query
again returns **3602 ps** and the same 15-wire route. Its blocker table is
empty. The search settles 759,455 nodes, expands 759,454, and examines
8,940,298 edges. The independent certificate checks 8,551,570 physical
edges against the device database and 388,728 generated edges through the
documented source-reviewed boundary. It discovers 967,193 nodes. All 224
endpoint setup/hold guards, clock results and actual routes are unchanged.

| Search | Minimum ready1 cost | Change from retained route |
| --- | ---: | ---: |
| Fixed occupancy, previous stage | 3602 ps | 0 ps |
| Other-net occupancy relaxed | 3602 ps | 0 ps |

No hypothetical route is bound. Final memory stays **116.918037 MHz**, pixel
76.958595 MHz and capture 332.494110 MHz. The final RBF and timing report are
byte-identical to the corrected baseline. The RBF SHA-256 remains
`b477f3d50f1bb9355d5ca8d6564ac0a78ddc82f90e2b7e06ca0674d10957d173`.

This rules out a cheaper ready1 path gained solely by freeing other nets'
wire/pip occupancy under the captured costs and retained static architectural
restrictions. It does not rule out changes to placement, mapping, those
restrictions or a separately justified timing model. Negotiated rerouting
of competitors is not supported by this result as the next experiment.

The Quartus evidence instead supports a bounded locality experiment that
accounts for both ready-control branches and their register banks. Preserve
unrelated placements and begin with the current mapped logic. Check legal
LAB controls, shared-input users and every moved register's data/clock/output
dependencies as well as the 224 ready-controlled endpoints. The existing
replica and residual sink tail offer a narrower alternative to assess before
moving an entire bank. Neither transformation is adopted by this read-only
stage; no gain is added to the stack and **130 MHz remains unresolved**.

## Verification

Eleven focused compiler tests pass; the full suite passes 43 cases with one
optional import case skipped. The new fixture distinguishes a 7 ps hypothetical
path through foreign occupancy from the 40 ps fixed-occupancy route, while
retaining static reserved-route restrictions and every actual binding. Three
197-cell default controls and the prior exact-search fixture certificate match
the parent. Twenty-five independent checker tests pass. The full-run certificate,
calibration trace, structural baseline checks and exact RBF/timing identity
checks pass. All eight derived evidence files regenerate byte-for-byte; 267
nested input hashes verify. The parent consistency check is run against the
committed selection before push.

This is host-only compiler research. No production RTL, recipe, compiler lock,
ABI, shared contract or hardware changes. Quartus and OSS paths are different
physical fits; this comparison does not calibrate either timing model against
an equivalent route. Raw evidence is `out/ramtest-ready-relaxed`; the adjacent
JSON records input and artifact hashes.

Frozen compiler SHA-256:
`86a104547d48e042c17ed5a6d3227155a96837a19b47a3d07253af773fe31cfe`.
