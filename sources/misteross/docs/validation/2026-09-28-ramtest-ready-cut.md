# RAM tester: broader HPS ready-enable cut, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), investigate the
remaining HPS ready-enable path following the
[timeout refinement](2026-09-28-ramtest-timeout-refine.md).
FES base is `cce82322`; the private compiler starts from `173bfa09`.
The 118.57 MHz best remains separate from the 118.22 MHz refinement candidate
used here to preserve its demonstrated DDR1 gains. This is a host-only
compiler experiment on unchanged RTL.

## Boolean boundary

The [saved Quartus fit](2026-09-28-ramtest-quartus-control-cones.md) uses a
shared request condition and a five-input ready-enable LUT. The current OSS
netlist has no standalone `from_core`: it separately maps accepted-read and
accepted-write conditions, then combines write with filler. Its final enable
also retains the shared `slot_free` boundary.

The actual packed seven-LUT cone, including all effective pin polarities,
is equivalent to:

```text
from_core = lock_qualified & ~draining & ~skid & ~finishing & (read | write)
filler    = finishing & ~skid & owed_notzero
skid_ENA  = (m_read | m_write) & ~cmd_ready_1 & (filler | from_core)
```

There are ten distinct boundary signals: nine FF Q outputs plus the HPS ready
output. Exhaustive comparison against the actual packed masks passes all 1,024
arbitrary assignments. No one-hot, reachable-state, or clock relationship
assumption is needed. Existing filler is the ALUT3 with mask `0x20`.

The `from_core` ALUT6 has A..F inputs lock qualification, draining,
skid, finishing, read and write, with mask `0x0002000200020000`. The final
ALUT5 has A..E inputs m_read, m_write, filler, from_core and ready, with mask
`0x0000eee0`. Ready uses the E input's 97 ps pre-route cell arc. The earlier
rejected composition retained enter_read/enter_write and cmd_valid boundaries;
this experiment reconstructs the broader shared request condition instead.

## Bounded physical experiment

Add one ALUT6 and replace the original enable root with the ALUT5. Preserve
its output net and all 107 ENA consumers. Keep the six shared interior LUTs,
the existing four-user HPS replica, the DDR1 enable copy, timeout mapping and
refinement, every register, and every other existing placement. Only the
original root may move; the new LUT needs a legal site. The direct ready
connection intentionally adds a load to the HPS output net.

First test the expanded root at its original BEL after wiring the new graph,
before binding `from_core`, and record legality. This particular trial rejects
the site; it is not an exhaustive proof about every possible placement.
Initialize the new LUT and root with bounded legal geometric searches, then
refine root and new LUT using propagated timing to all 107 endpoints. The
changed topology gets a new timing analyser. Each refinement move must improve
its current worst slack without worsening an endpoint; the final vector must
also dominate the original, pre-remap vector and strictly improve its worst
slack. A failed final guard aborts before routing. Fresh clocked hold checks
and full occupied-BEL legality remain mandatory.

This combines a Boolean remap with any necessary root relocation. Final routed
QoR tests that combination; without a paired relocation-only route, it does
not isolate the contribution of mapping from placement.

The search uses radius six around the original root and accepted-read sites,
excludes protected LABs, and evaluates one legal BEL per LAB. Geometric
initialization chooses `from_core` at `(33,27,0)` and the root at `(27,21,0)`.
Imported preflight accepts these immediately: no subsequent coordinate-descent
move qualifies in 102 STA runs. Every endpoint improves by at least 1,003 ps;
worst predicted slack changes from 4,002 to 5,775 ps. Imported clock constraints
are absent, so those absolute slacks are diagnostic, not signoff evidence.
Fresh execution must repeat the full guard with the real clock constraint.

## Compiler and verification

The opt-in diagnostic is published as nextpnr `341d3d944502d502531f41cb0468935e0673553c`
on `probe/ramtest-ready-cut`, based on `173bfa09`. Enable
`NEXTPNR_MISTRAL_READY_CUT=<snapshot-prefix>` after the expanded timeout rewrite,
retained DDR1 copy and timeout refinement. No production recipe or compiler lock
selects it. Disabled and empty-prefix modes reproduce the entire 197-cell
reference module exactly.

The HIP build and CTest target pass: 25 backend cases pass, one optional
import case is skipped there and executed separately among nine passing
focused cases. Truth-policy tests failed on the incorrect function before the
implementation and pass all 1,024 rows afterward. The independent checker
passes graph/load positive cases and rejects 21 mutations, including a changed
old-cone function, a changed FF boundary and incorrect fanout accounting.

Actual preflight preserves 14,926 nonroot cells, all 103,286 old pin states,
and adds nine pins. The checker verifies all 14 changed net fanouts from the
actual graphs. Besides HPS ready going from one to two users, lock-qualified
fanout rises from 415 to 416 and skid from 117 to 118. These are real physical
costs; Boolean equivalence does not establish timing or electrical benefit.

Raw evidence lives under `out/ramtest-ready-cut`; the adjacent JSON records
hashes, compiler identity and measurements. The frozen binary is
`tools/nextpnr-ready-cut`, SHA-256
`ede24722080c732d2de8e700cf66e7a22fca2d5d438154a15d9c890ab6e5315b`.
Yosys remains `c156dd886`, Mistral `7ed06e21`; original initialized-FSM synthesis,
13 RTL sources, generated headers and BUILD_ID are unchanged. Routing uses
GPU 1, seed 2, HeAP exponent 2, timing weight 10, beta 0.5 and generic
replication budget four.

## Fresh placement checks

Every prior stage matches the 118.22 MHz control exactly, including the complete
module and effective pin states immediately before this cut. The independent
checker repeats all 1,024 assignments on the actual fresh snapshots and
verifies the same placements and input-load changes as preflight.

With the real memory clock constraint, all 107 predicted setup slacks are
nonregressing and the worst improves from -1,774 to -1 ps. The selected sites
match preflight, no coordinate-descent move is accepted, every occupied BEL is
legal, and predicted hold violations remain zero. These are pre-route guards;
the routed result below determines QoR.

## Routed result and decision

| Configuration | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Best retained-copy stack | 118.567703 | 76.958595 |
| Direct timeout-refinement control | 118.217285 | 75.987846 |
| Broader ready cut plus local placement | **115.780945** | **77.724236** |

The candidate completes legally in 591.56 seconds and emits a 7,007,204-byte
RBF. Capture remains 332.494110 MHz. The only warning is the 130 MHz memory
miss; final signoff reports no hold violations. All 14,928 post-cut placements
survive routing; 5,088 ordinary route-through buffers are added.

**Do not add this configuration to the best stack.** The local transformation
works, but the overall result regresses. Keep the compiler branch and proofs
as a separate candidate for joint optimization. The 118.57 MHz best remains
unchanged; 130 MHz remains unresolved.

Across the 107 targeted skid-enable endpoints, every maximum propagated
arrival improves by 0.625–1.637 ns. Worst arrival drops from 9.969 to 8.655 ns; the former
critical `skid_address[27].ENA` drops to 8.494 ns, a 1.475 ns gain. The DDR1
address improvements survive: all 29 address arrivals improve further and
their worst changes from 9.417 to 9.360 ns. Across all 110 timeout endpoints,
37 improve and 73 regress. The copied DDR1 `burst_end.ENA` stays at 7.378 ns.
These measurements are arrivals, not setup slack or fixed-launch path delays.

The new worst path is HPS `cmd_ready_1` to **m1_burstcount[5].ENA**, through
unchanged logic:

```text
HPS BEL (52,53)
  --3.868 ns--> slot_free ALUT2 (30,26,48)
  --0.903 ns--> command-enable ALUT4 (27,22,13)
  --1.873 ns--> m1_burstcount[5] FF (34,29,4)
```

Two LUT arcs total 0.800 ns; routing totals 6.644 ns. Add 1.177 ns clock-to-Q,
0.212 ns skew and -0.196 ns setup for **8.637 ns effective setup**, consistent
with 115.780945 MHz. This ALUT4 drives 109 command-register enables. All 109
maximum arrivals regress by 0.364–0.437 ns; the worst changes from 9.728 to 10.109 ns.
The ready-to-ALUT2 physical endpoints are unchanged, but that routed arc grows
from 3.473 to 3.868 ns. The extra HPS load and global rerouting are both present;
this experiment does not isolate their causal contributions.

## Next bounded experiment

The retained command ALUT4 has mask `0xf0e0`, inputs skid, enter_read,
slot_free and enter_write, and function:

```text
command_ENA = slot_free & (skid | enter_read | enter_write)
            = (~(m_read | m_write) | cmd_ready_1)
              & (skid | from_core | filler)
```

The new `from_core` cut is already available. A read-only proof against all
1,024 actual packed boundary assignments validates two possible command cuts:
an ALUT6 taking m_read/m_write directly (`0xffff1111fff01110`), or an ALUT5
retaining the existing command-valid OR boundary (`0xddddddd0`). Exact pin
orders and negative controls are saved in `next-command-cut-proof.json`. Neither
is implemented or placed, and neither has a demonstrated timing benefit.

Investigate a joint mapping and
placement of both skid and command enable roots, proving the broader boundary
and guarding both endpoint groups together. Include the remaining shared
slot-free users and added input loads in the assessment. Do not infer that
another direct-ready connection will improve timing: the untouched branch's
regression is precisely the limitation exposed here. Compare this candidate
with the retained 118.57 MHz best, and preserve all earlier proof controls.

Source/preflight review and full fresh structural checks pass. Frozen source,
binary and proof-input hashes match; nine generated measurement/proof
artifacts regenerate byte-identically. This is host-only evidence: no board
programming or hardware acceptance, production RTL, recipe/lock, shared ABI or
wire-contract change, and no PR.
