# RAM-test: fixed-input compiler controls and first-error enable

Dated host-only investigation, 2026-09-29, for
[issue #264](https://github.com/DeanoC/fes/issues/264), based on FES
`7785210bfd416dcd5ec8fd4ae43fcc3b03464bac` after PR #315 merged.

The lower baseline is caused by changed BUILD_ID-driven synthesis, not a nextpnr
regression in the tested flow. With either saved synthesis input held fixed,
the old and merged nextpnr binaries produce identical timing reports, routed
designs and RBF bytes. DDR0 first-error logic offers a concrete mapping/locality
lead, but this investigation adopts no additional optimization or timing gain.

## Compiler and input isolation

The preceding [merged-compiler measurement](2026-09-29-ramtest-merged-compilers.md)
changed compiler revisions and BUILD_ID together. Its lower baseline therefore
did not isolate a compiler regression. This investigation crosses the saved
synthesis inputs with the old and merged nextpnr executables, and resynthesizes
both BUILD_IDs with merged Yosys.

The old BUILD_ID is `52991ce22369904849b68bdd900f9ae1`; the new one is
`7168b508ab424b70f1c0f87b2035d821`. All 13 RTL files and the QSF/SDC match.
Every route uses GPU device 1, seed 2, HeAP weight 10, criticality exponent 2
and enable-replication budget 4. These are baseline routes without local remapping.
The old nextpnr revision is `d91c902b`; the merged revision is `24f9a1db`.
Each executable's hash matches its previously recorded identity. Cross-runs use
controlled environments and compiler file-read auditing; the relocated old
installation is a research control, not a newly authenticated production package.

| Baseline route | Old synthesis / BUILD_ID | New synthesis / BUILD_ID |
| --- | ---: | ---: |
| Old nextpnr `d91c902b` | 109.051254 MHz | 106.157112 MHz |
| Merged nextpnr `24f9a1db` | 109.051254 MHz | 106.157112 MHz |

Both cross-runs have identical routed module contents, timing-report bytes and
RBF bytes to the archived run using the other compiler on that same synthesis.
All three clock results match within each input column, not just memory Fmax.
All 19,863 routed cells on the new input and all 20,011 on the old input match;
no BEL changes. Only the routed JSON creator string differs. Both new routes
exit successfully, use the live HIP backend and complete final analogue signoff
without final reported hold violations. All four memory results remain below
the 130 MHz constraint.

Merged Yosys `22bf145d`, given the old BUILD_ID, reproduces the complete old
`acf441cc` synthesis `modules` dictionary exactly; only the creator metadata
differs. Given the new BUILD_ID, it reproduces the saved new synthesis JSON
byte-for-byte. Thus the Yosys merge does not explain the mapping change.

Changing only BUILD_ID with that same merged Yosys changes the mapped cell count
from 14,912 to 14,944. Of 11,056 common cell names, 570 change type or parameters,
including cells in HPS DDR, all three DDR testers, SDRAM control and video.
The identity constant perturbs the globally optimized flattened design beyond
the identity logic itself. Holding RTL files and seed constant is insufficient
for comparing compiler quality when an embedded constant also changes.

The diagnostic derives BUILD_ID from its recorded inputs, including compiler
identities and recipe sources, and supplies that constant to the application
mailbox before synthesis. Compiler integration can therefore change the mapped
design even when the executable's baseline mapping behavior is unchanged. These
cross-runs deliberately reuse saved synthesis and identity solely as research
controls; they do not replace the diagnostic's provenance-derived identity or
select a production artifact.

Both saved inputs contain 8,221 flip-flops, all with inactive constant-1 ACLR
connections. The nextpnr revisions also differ by asynchronous-clear correctness
fixes and the default-off remap pass. The cross-runs test their combined baseline
effect on these inputs; they are not a general claim that those revisions produce
identical results on designs with active asynchronous clears.

## DDR0 first-error path and Quartus reference

The retained merged remap experiment reaches 109.950516 MHz memory, but the
diagnostic still selects its 106.157112 MHz baseline because pixel Fmax decreases.
That selection policy is unchanged. This investigation uses the remapped path
as an optimization lead and does not claim a further timing gain.

Following the actual net aliases resolves the remapped critical path from
`ddr0_errors[15]` to `ddr0_status[202]`, which is `ddr0_test.fault_addr[4]`.
The path controls first-error capture: in `ddr_channel.v`, a bad returned group
increments the saturating error count and captures the fault address and phase
only when `errors == 0`. It is a memory-clock register-enable path.

The OSS route crosses four LUT levels and widely separated LABs:
FF (9,32) → NOR4 (13,28) → ALUT6 (6,10) → ALUT5 (6,10) →
ALUT6 (18,26) → destination FF (34,22). Its 9.095 ns effective setup total
contains 7.106 ns routing and 1.407 ns LUT delay, plus clock-to-Q, skew and setup.

A private copy of the existing Quartus 17.0.2 fitted database was queried
with TimeQuest, using the slow 1100 mV / 100 C model. It retains the **old**
BUILD_ID and Quartus-native PLL/SDC and pin models. Source files and DDR0 logic
match, but Quartus omits `RAM_OSS_HIGH_SPEED`, so SDRAM capture and sequencing
also elaborate differently. This is neither whole-design elaboration equivalence
nor an equivalent physical-route calibration or a fresh fit for the new identity.

The exact logical bit-15 to fault-address-bit-4 path uses
`errors[15]~DUPLICATE`, three LUT levels, 4.025 ns data delay, 2.918 ns routing
and **+3.432 ns setup slack at 130 MHz**. Its logic stays near the registers:
FF (29,47) → LUT (28,47) → LUT (29,46) → LUT (28,49) → FF (28,49).
The broader errors-to-fault-address query returns 28 paths, worst slack
+2.031 ns. The fitted design contains 32 original error-count registers and
12 duplicates. This supports investigating both reduction mapping and register
locality; it does not establish that duplicating those same bits in OSS would win.

The final exact query finds one path. An initial empty query caused by bus-index
filter syntax is retained separately. The two warnings in the corrected query
are existing SDC clock filters, not missing endpoints for this memory-clock path.

## Bounded Boolean probe

A read-only graph probe follows the critical path, decodes the physical LUT pin
maps and recursively recognizes conjunctions of literals. It finds the 16-input
low-half error-zero detector: four LUTs arranged across three levels. An abstract
6+6+4 leaf grouping followed by a three-input AND represents the same function in
four LUTs across two levels. Exhaustive evaluation of all **65,536** assignments
matches the original LUT network.

This is a Boolean feasibility result only. No compiler or routed netlist is
modified. The grouping has no legal placement or routed timing result, and both
existing root consumers must remain correct. The source registers span x=1..37,
y=4..32, so reducing logic depth alone cannot establish a gain.

The current generic remapper composes two adjacent LUTs with at most six distinct
inputs. This cone calls for a wider associative-reduction rewrite, with groups
chosen using arrival times and physical distance, legal local placement, preserved
side users, and the existing setup/hold and boundary guards. A controlled trial
should use the fixed new synthesis and retain the existing remap result, then
reroute and compare all clocks before accepting it. Register duplication remains
a separate, higher-scope possibility requiring sequential equivalence and full
fanout checks.

## Verification and retained evidence

The two cross-routes and two synthesis controls complete successfully, with
executable and input hashes checked and controlled invocation state verified.
The existing three timing-parser tests pass. The Boolean probe independently
regenerates with all 65,536 assignments equal. Independent review checked the
method, source/elaboration qualifications, endpoint aliases and path accounting.
All 42 raw artifact hashes verify, and the adjacent JSON regenerates
byte-for-byte. The matrix includes successful exit status and final hold-warning
screens; the Quartus record includes original fit hashes and the two cache files
regenerated in its private copy.

Raw evidence is under `out/ramtest-fixed-input-264/`, including the cross-run and
synthesis manifests, logs, reports, netlists and RBFs; Quartus query scripts and
reports; and the exhaustive Boolean probe. `build_record.py` assembles the adjacent
JSON and refuses incomplete runs. The original diagonal runs and compiler install
are preserved under `out/ramtest-merged-remap/`. Raw outputs and research helpers
are ignored and must be supplied separately to replay their hashes.

No RTL, compiler, lock, recipe, ABI or hardware changes are adopted here. The
production package is unchanged. OSS 130 MHz closure remains unresolved.
