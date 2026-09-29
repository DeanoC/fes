# RAM tester: FSM isolation and GPU routing, 2026-09-27

**Placement-option correction:** exponent 2 in this record is the requested
CLI value. The tested Mistral versions actually forced exponent **7** and beta
**0.5**. The measured results and same-placement comparisons still stand. See
the [placer-option investigation](2026-09-27-ramtest-placer-options.md).

Follow-up to [initialized-FSM extraction](2026-09-27-ramtest-fsm-init.md) for
[FES #264](https://github.com/DeanoC/fes/issues/264). Host-only compiler work;
no RAM-test RTL, producer lock, protocol or hardware acceptance change.

## Result

The initialized-FSM candidate now routes legally. A nextpnr stopping-condition
fix lets initial routing resolve the remaining conflict at iteration **78**,
instead of aborting at **67**. This fixes convergence for this experiment,
but does not improve whole-design timing or close 130 MHz.

| Same source, seed 2, weight 10, exponent 2, GPU 1 | Memory MHz | Pixel MHz |
| --- | ---: | ---: |
| Original cell graph, nextpnr f60b33aa | 101.94 | 69.68 |
| Only three DDR FSMs recoded, nextpnr f60b33aa | 94.12 | 72.68 |
| All six FSMs recoded, nextpnr f60b33aa | Routing fails | Routing fails |
| All six FSMs recoded, nextpnr e89caa68 | 91.95 | 69.11 |

These are final analogue results from legal routes and diagnostic RBFs, with
`--timing-allow-fail`; they do not mean the requested clocks pass. All variants
retain the original clock constraints. The failed run has no accepted Fmax.
The matched-source Quartus reference remains **139.30 / 99.54 MHz** from the
[original investigation](2026-09-27-ramtest-timing.md).

## Compiler fix

Published nextpnr candidate:
[`e89caa684fa5801ae677fcd2e43f33effa0dc544`](https://github.com/DeanoC/nextpnr/commit/e89caa684fa5801ae677fcd2e43f33effa0dc544),
branch `fix/gpu-initial-congestion`, based on `f60b33aa`. No PR opened.

The thirty-iteration small-congestion escape was intended for timing repair,
where frozen arcs can prevent convergence and a legal earlier route can be
restored. It also ran during initial routing, where its failure aborted the
whole design. The saturated-congestion escape had the same scope problem.

The fix captures whether any `ArcData.frozen` flag is set at each negotiation's
entry. Only those repair attempts use the two shortcuts. Thawing during an
attempt does not erase that classification; hard reservations and pre-routed
globals do not enable it. Initial routing keeps its existing congestion growth
and 2,000-iteration budget. Wire reservations, costs and timing models are
unchanged. The shared stopping policy is in
`common/route/gpu/congestion_plateau.h`, used by `gpurouter.cc`.

The full before/after run uses the same all-FSM netlist. All 55 main analytic
placement metric rows (excluding elapsed times) and the first 67 routing
iteration summaries match. The old
router aborts with the same unreserved `TD.11.18.44` conflict between
`ddr_speed.divisor[13]` and a `ddr_speed.fits` arithmetic net. The patched router
clears initial congestion at iteration 78 and completes final signoff.

## Synthesis isolation and remaining gap

Three netlists separate the effects of recoding the DDR channels from recoding
other state machines: legacy (all six disabled), DDR-only, and all-FSM. The
attributes are applied to the flattened Yosys design, not to source RTL.
The legacy control reproduces all **14,907** original cells exactly by name,
type, parameters, ports and connections. The complete module also matches after
removing only source-location and `fsm_encoding` metadata, including its initial
values. This checks that splitting the recipe around `proc; flatten` did not
change the control design.

The DDR-only netlist has 8,205 flip-flops and reduces the investigated
`hps_ddr.port2.skid` to `ddr2_address[23]` enable cone from six to four LUT
levels. The investigated pixel cone remains eleven levels. These are structural
depths, not full-design timing measurements. The all-FSM netlist and its
initialization/equivalence evidence are unchanged from the previous record.

The limiting memory paths have moved:

| Variant | Launch → capture | Cell logic ns | Routing ns | Effective setup ns |
| --- | --- | ---: | ---: | ---: |
| DDR-only | `ddr0_test.idle[15]` → `ddr0_address[25].ENA` | 1.698 | 8.394 | 10.625 |
| All-FSM, fixed router | `ddr1_test.idle[6]` → `ddr1_read.DATAIN` | 1.677 | 8.658 | 10.875 |

The reported paths have seven and eight combinational cell arcs respectively.
Their total also includes clock-to-Q, setup and clock skew. Routing dominates,
but multiple logic stages impose multiple routed connections. These results
point to the timeout/control cone's mapping, sharing and placement as the next
compiler target. They do not isolate those factors or establish that Mistral's
physical delay model is pessimistic. Safe one-hot extraction is useful compiler
functionality; it is not sufficient for this design's timing closure.

## Validation and reproduction

- Standalone production-policy regression: four expected failures with the old
  unconditional predicates; passes with the fix under
  `g++ -std=c++11 -Wall -Wextra -Werror`. Covers tiny and saturated initial
  plateaus, repair limits, counter resets, and classification between attempts.
- Private HIP Release compiler build for `gfx1100;gfx1201`: passes.
- Existing `mistral/tests/gpurouter/timing_gate.py`: passes. At 400 MHz, no-RBF
  routing fails at the legality check and RBF routing fails at analogue signoff;
  a 10 MHz RBF run succeeds.
- Independent code review: no blocking or important findings. The standalone
  test supplies repair eligibility directly; the real frozen-arc scan and
  thawing transitions were inspected, rather than tested in a dedicated fixture.
- Full unchanged-source routes as tabulated above. No hardware accessed.

Raw evidence is under `out/ramtest-fsm-isolation/`; the private compiler checkout
is its `nextpnr/` directory. The [JSON record](2026-09-27-ramtest-fsm-routing.json)
contains hashes, exact Fmax values, critical paths, synthesis scripts and route
settings. Expand `{fes_root}` in the synthesis scripts and execute them from
`sources/misteross`, using Yosys candidate `c156dd88`. The all-FSM input is the
unchanged `out/ramtest-fsm-init/synth.json` recorded previously. Policy test
instructions are in the compiler's `mistral/tests/gpurouter/README-congestion-plateau.md`.

The shared nextpnr installation advanced during another task, so every new
control uses a frozen **f60b33aa** executable and GPU **1** (Radeon AI PRO R9700).
The patched private build uses that source base and Mistral **7ed06e21**. Its
version string still names the base because it was built before the source
commit; the binary hash identifies the tested executable. The older f95cef3d /
GPU-0 figures are historical context, not the sole control. The new original-
graph control reproduces the historical Fmax values.

The next integration step is further compiler optimization of the measured
control paths, followed by a matched full-flow comparison. Neither compiler
candidate is promoted into the RAM-test producer lock by this record.
