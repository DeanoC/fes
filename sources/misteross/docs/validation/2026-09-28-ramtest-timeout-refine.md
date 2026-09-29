# RAM tester: timing-aware timeout placement refinement, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), investigate the
DDR1 address-enable path exposed by the
[118.57 MHz retained-enable combination](2026-09-28-ramtest-retained-enable.md).
FES base is `1140bd1a`; the compiler starts from `bb5a3ba5`.
This is a host-only compiler diagnostic on unchanged RTL.

## Observed limitation

The current timeout placement walks replacement LUTs in topological order.
It scores each candidate using its worst input delay plus its worst delay to
already placed users. A future output consumer contributes nothing. In the
measured critical path, `timeout_partition_ch1_control9` has six placed inputs
but zero placed outputs when selected at (37,26). Its sole consumer,
`control0`, is later placed at (33,21). That final LUT drives 29 FF ENA pins,
including 13 in LAB (27,16), seven in (29,16), and three in (27,15).

The measured critical path ends at `ddr1_address[13].ENA` at (23,16),
travelling through `control3` (37,20), `control9` (37,26), and `control0`
(33,21). The last three routes total 4.052 ns. This is a concrete missing
consumer cost in the first placement pass, but does not by itself prove that
another placement improves final routed timing.

Moving control9 toward the address registers also lengthens some of its
five other FF input paths. Those launches must participate in the score.
A simple distance sum or optimization of only the currently reported launch
would miss that tradeoff.

## Bounded experiment

After the accepted timeout rewrite and both enable copies, refine only
`timeout_partition_ch1_control9` and `timeout_partition_ch1_control0`.
Their logic, input/output connections, pin polarities and all register state
remain exact. Every other cell retains its BEL, including both copies.

Use at most two coordinate-descent passes, control9 then control0, with a
fixed radius-six search around each original site. The current placement is
the no-op option. Only free, legal sites outside protected LABs qualify.
Within each LAB, examine one legal BEL: the applicable pre-route ALUT6 and
ordinary routing estimates do not distinguish those slots. This deliberately
bounds the search; it does not claim that the slots route identically.

Each trial reruns propagated timing after the placement change. Accept only
when no one of the 29 address-enable setup slacks regresses and their worst
slack improves. Because control9 feeds only control0, these are the entire
forward sequential boundary of the two changed cells. Final occupied-BEL
legality and a fresh clocked hold comparison must pass before routing.
A complete fresh route then measures QoR against the 118.57 MHz control.

## Preflight

The saved-design preflight accepts one move: control9 from
`MISTRAL_COMB.37.26.12` to `MISTRAL_COMB.37.20.12`. Control0 stays at
`MISTRAL_COMB.33.21.0`. This puts control9 in the same LAB as control3.
The search evaluates 176 legal per-LAB candidates plus four accepted-state
or no-op timing refreshes. Its second pass accepts no further move.

The independent checker preserves all 14,927 cells and 103,286 effective pin
states, with only the one BEL attribute changing. All 29 recorded setup
slacks improve by 1600 ps; their worst value changes from 3342 to 4942 ps.
The checker reproduces deterministic best-choice and tie-breaking from the
recorded legal trials and confirms the fixed original-centered domains and
158 protected LABs. It checks the compiler's recorded slack/hold guards;
it does not independently implement architecture timing or site legality.

As before, saved JSON lacks memory clock constraints and complete derived
clock metadata. The imported slack values and zero hold violations are
therefore preflight diagnostics. The fresh hook requires the memory constraint
and repeats the analysis. A fresh before snapshot must match the complete
118.57 MHz post-copy module and pin sidecar before this change is measured.

## Compiler and validation

Compiler commit `173bfa09206a7791393f359470818d472c323d4a` is on
`probe/ramtest-timeout-refine`. Enable the diagnostic with
`NEXTPNR_MISTRAL_TIMEOUT_REFINE=<prefix>`, after the unchanged timeout-roots
and retained-enable diagnostics. The frozen binary is
`out/ramtest-timeout-refine/tools/nextpnr-timeout-refine`, SHA256
`0ad1b7e63d6d19a97771c70d598f4eaa303d81aad24b6913305cc86dba2fd0df`.
The build manifest hashes its committed source bytes.

The HIP compiler builds. The backend CTest target passes: 25 cases, 24 passed
and one optional actual-design preflight skipped there. Eight focused cases
including that actual preflight pass separately. The timing-vector policy has
a recorded failing assertion before implementation and passes afterward,
including rejection of endpoint regressions, unchanged worst slack, gains
below 1 ps and nonfinite values. Absent and empty environment controls reproduce
the complete 197-cell reference module.

The independent snapshot checker passes one positive fixture and rejects 20
mutations. It has separate recorded failing-to-passing checks for deterministic
best-candidate and tie selection. Source and preflight review found no blockers.
The diagnostic is a bounded experiment: it aborts on a final guard failure
before routing, rather than promising a production rollback transaction.

## Fresh-flow checks

The fresh run uses the original initialized-FSM synthesis, GPU1, seed2,
HeAP exponent2, weight10, beta0.5 and replication budget4. It reproduces
all preceding stage snapshots, indexed pins, timeout sites and replica
manifests from the 118.57 MHz control. The complete pre-refinement module
matches that measured control exactly. The prior timeout-equivalence bridge,
region and retained-copy proofs also pass on the fresh artifacts.

The fresh clocked refinement chooses the same single move and leaves every
other BEL unchanged. All 29 predicted endpoint slacks improve by 1600 ps;
the worst changes from -2434 to -834 ps. Zero predicted hold violations are
reported before and after. Those values still miss the clock target and are
not a routed timing result.

The independently calculated geometric arc estimates explain the tradeoff:
the critical input from control3 falls from 1500 to 300 ps, and the output
to control0 falls from 1540 to 1140 ps. The other five control9 input estimates
increase by 200–850 ps. Their complete launch paths have sufficient predicted
margin to leave every endpoint's worst setup slack improved. This experiment
therefore tests propagated timing, not a rule that every local wire gets shorter.

## Final routed result

| Candidate | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Best retained-enable combination | **118.567703** | **76.958595** |
| Same combination plus timeout refinement | 118.217285 | 75.987846 |

The refinement is 0.350418 MHz below the control. Keep 118.57 MHz as the best
measured configuration; do not promote this refinement on its own. Both pixel
results pass 74.25 MHz, capture remains 332.494110 MHz, and memory still misses
130 MHz. The fresh run finishes normally in 598.30 seconds with a legal
7,007,204-byte RBF, no final reported hold violations, and one warning for the
memory miss. All 14,927 post-refinement BELs survive routing, with 5,087 ordinary
route-through buffers added.

The local effect is positive. All 29 targeted DDR1 address-enable maximum
arrivals improve by 0.407–0.758 ns. Their worst arrival, at the previous critical
`address[13].ENA`, improves from 9.913 to 9.417 ns. Across the six timeout output
groups, 102 arrivals improve and eight regress; the latter are DDR1 beats-left
endpoints. These propagated arrivals are not setup slack or matched-launch
path delay. Fresh rerouting can change any connection despite the single
placement move.

The new limiting path is HPS `cmd_ready_1` → port1 `skid_address[27].ENA`,
through the unchanged ALUT2 at (30,26) and ALUT3 at (24,20), ending at the
unchanged FF at (27,14). Its two logic arcs cost 0.800 ns and three routes cost
3.473, 1.407 and 1.624 ns. With 1.177 ns clock-to-Q, 0.174 ns skew and -0.196 ns
setup, the effective setup total is 8.459 ns.

On that unchanged HPS driver's 107 endpoints, 87 maximum arrivals improve and
20 regress. The worst rises from 9.898 to 9.969 ns; the newly critical endpoint
rises from 9.823 to 9.969 ns. The earlier copied DDR1 burst-end endpoint still
improves, 7.613 to 7.378 ns. The overall regression therefore accompanies a
shift back to the known HPS enable bottleneck, rather than failure of the
selected DDR1 address paths to improve.

## Retention and next investigation

Retain the equivalent refinement as a candidate with demonstrated local gains,
but leave it out of the best configuration. `stack.json` retains the 118.57 MHz
control identity and records this trial. This distinction matters: the previous
timeout candidate later stacked usefully with the retained-enable copy; a small
whole-design regression need not invalidate the local technique.

The next targeted investigation is the remaining HPS ready-enable cone. The
[saved Quartus comparison](2026-09-28-ramtest-quartus-control-cones.md) identifies
a broader five-input fitted function, whereas the earlier rejected OSS
composition merely combined two existing LUT boundaries. Trace that broader
Boolean cut and assess equivalent local mapping or duplication while preserving
the DDR1 gain. It needs its own equivalence and physical checks; the present
result does not justify retrying rejected global HPS placement settings or
assuming another copy will help.

This single-design, single-seed experiment does not qualify a general placement
policy. It changes both the availability of output costs and the use of
propagated arrival information relative to the initial greedy placement; no
ablation separates their individual contributions. Final routing remains free
to change throughout the design.

The adjacent JSON records the frozen compiler, structural proofs, timing,
endpoint comparisons, and raw/external input hashes. Measurement helpers
regenerate their outputs identically. Raw evidence is under
`out/ramtest-timeout-refine/`. No production RTL, recipe, compiler lock,
ABI/shared contract or hardware state changes; no PR is opened. This is
host-only timing evidence, not hardware acceptance.
