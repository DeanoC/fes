# RAM tester: effective placement options, 2026-09-27

Compiler-only follow-up to [FSM isolation and routing](2026-09-27-ramtest-fsm-routing.md)
for [FES #264](https://github.com/DeanoC/fes/issues/264). RTL, clock constraints
and embedded build ID remain unchanged. Placement controls use frozen synthesis
netlists; a separate mapping probe changes three combinational cells with exhaustive
Boolean equivalence checks. No hardware was accessed.

## Configuration defect

The previous experiments requested `--placer-heap-critexp 2`, but Mistral's
`Arch::place()` unconditionally replaced the parsed value with **7**. It also
replaced beta with **0.5**. Generic command setup and `PlacerHeapCfg` correctly
read the options; the architecture-specific overrides came afterward. Timing
weight was honored. This behavior is present in both `f95cef3d` and `e89caa68`.

The earlier recorded MHz and same-placement comparisons remain valid. Their
exponent-2 labels describe the requested CLI option, not the setting used by
the placer. With no explicit options, saved JSON misleadingly reported generic
defaults exponent 2 / beta 0.9 even though placement used 7 / 0.5.

HeAP weights a connection by `1 + timingWeight * criticality^exponent`, in
addition to its geometry/fanout weight. At criticality 0.7 and timing weight 10,
exponent 7 gives a multiplier of 1.824, versus 5.9 for exponent 2. This affects
moderately critical connections substantially; it does not by itself predict
which exponent yields better complete-design timing.

## Fix and regression

Published nextpnr candidate
[`19a5fe4a179cc5873dbd7641eca6b49d6f2284ad`](https://github.com/DeanoC/nextpnr/commit/19a5fe4a179cc5873dbd7641eca6b49d6f2284ad),
branch `fix/mistral-placer-options`, based on the GPU-convergence fix `e89caa68`.
No PR opened.

Architecture construction now supplies beta 0.5 and exponent 7 as settings,
before generic defaults and explicit CLI processing. The late overrides are
removed. Historical defaults remain intact; fresh synthesis JSON can now use
the requested values. Programmatic callers can also override the settings after
construction. No placement cost formula, timing model or source logic changes.

The new `mistral/tests/placer_options.py` synthesizes a small arithmetic fixture
and performs five real placements, without routing or a GPU:

| Option relative to defaults | Old compiler: cells moved | Fixed compiler: cells moved |
| --- | ---: | ---: |
| Exponent 2 | 0 | 188 / 197 |
| Beta 0.8 | 0 | 189 / 197 |
| Timing weight 30, positive control | 189 / 197 | 189 / 197 |

Old default, fixed default and fixed explicit exponent 7 / beta 0.5 have
**all 197 BEL assignments identical**. The fixed compiler's saved settings agree
with the selected values. The regression fails before the fix and passes after
it. The private HIP compiler rebuild succeeds, with existing narrowing warnings;
`ctest` has no configured tests in this `BUILD_TESTS=OFF` build and is not counted
as regression coverage. Independent source review found no implementation blocker.

The existing JSON importer applies saved settings after CLI setup. Loading an
old placed/routed JSON can therefore override the explicit flags. To reproduce
the old configuration, start from the original synthesis JSON and select
`--placer-heap-critexp 7 --placer-heap-beta 0.5`. Alternatively correct the stale
fields in a copy of the saved JSON before loading. This fix does not change the
generic JSON/CLI precedence. Placement comparisons start from the original
synthesis JSON, which contains no saved nextpnr settings. The separate mapping
probe starts from a copy of that synthesis JSON with three LUTs composed.

## Full-design comparison

All three new routes complete legally, generate RBFs and finish analogue signoff.
The older all-FSM exponent-7 row uses the same synthesis input and the `e89caa68`
router from the preceding record. All rows use the same source/build identity,
seed 2, timing weight 10 and beta 0.5.

| Synthesis | Effective exponent | Memory MHz | Pixel MHz |
| --- | ---: | ---: | ---: |
| Original | 7 | 101.94 | 69.68 |
| Original | 2 | 97.04 | 68.72 |
| All initialized FSMs recoded | 7 | 91.95 | 69.11 |
| All initialized FSMs recoded | 2 | **111.09** | **75.32** |

The new exponent-7 original-netlist control reproduces all **19,983** prior BEL
placements and all three exact Fmax values. This supports default preservation
at full-design scale, beyond the small regression fixture.

Honoring exponent 2 helps the FSM-recoded netlist but hurts the original netlist
in this single-seed comparison. The combined candidate improves memory Fmax by
about **9%** over the original control and about **21%** over the previous FSM
candidate. Pixel now meets the 74.25 MHz constraint; memory still misses 130 MHz.
This is a measured interaction between synthesis and placement, not evidence
that exponent 2 is universally better. Promoting the nextpnr change alone into
the existing recipe would select the slower original/exponent-2 row here.

The combined candidate's worst memory path is now:

`hps_ddr.cmd_ready_1` → `hps_ddr.port1.skid_burstcount[7].ENA`

It has two combinational arcs, 0.800 ns of cell logic and 7.006 ns of routing,
for 9.002 ns effective setup including clock-to-Q, setup and skew. Its hops are:

| Connection | Placement | Fanout | Routed delay ns |
| --- | --- | ---: | ---: |
| HPS ready → first slot-free LUT | (52,53) → (30,26) | 1 | 3.475 |
| First LUT → enable LUT | (30,26) → (24,20) | 6 | 1.407 |
| Enable LUT → skid register | (24,20) → (34,25) | 111 | 2.124 |

The next timing target is therefore locality of this HPS-ready/enable path,
including the shared register-enable load. Pairing data LUTs with FFs, discussed
below, is no longer directly aimed at the current worst path. No further physical
transformation is mixed into these measurements.

## Equivalent mapping probe

The two LUTs on the new HPS-ready path depend on only four distinct inputs.
The first computes `ready | !valid`; its enable consumer computes
`!slot_free && (enter_read || enter_write)`. Composing these yields
`!ready && valid && (enter_read || enter_write)` in one ALUT4. The first LUT
remains for its five other users.

The scratch probe applies this composition to the three structurally identical
DDR-port enable consumers, with 182 / 111 / 111 FF-ENA sinks. It changes exactly
three ALUT3 cells to ALUT4, preserving their outputs and all state, initialization,
module ports, design parameters and other cells. An independent Boolean oracle checks all
16 input combinations per port; restoring the three original cells reproduces
the complete input document. Independent regeneration reproduces the serialized
netlist byte for byte. This is a diagnostic netlist experiment, not a general
compiler optimization or an RTL change.

With the same compiler, seed, exponent 2 and constraints, this probe routes
legally, emits an RBF and completes final analogue analysis at **107.77 MHz
memory / 77.86 MHz pixel**. Memory regresses by about 3% from the unmodified
all-FSM control, while pixel improves. The limiting memory path moves to
`ddr0_status[296]` → `ddr0_test.expected[16].DATAIN`, with five logic arcs,
1.506 ns logic and 7.223 ns routing, totaling 9.279 ns including clock terms.
The removed local stage does not produce an overall timing gain in this run.
The transformation is not adopted; further mapping work must evaluate the
resulting placement and shared control loads rather than LUT depth alone.

## Other physical candidate

FF enables are modeled as registered timing inputs; no omitted ENA setup arc
was found. Sampled long connections already have substantial predicted delay.
For example, the previous all-FSM critical read-data LUT to FF crosses from
(15,8) to (27,25): 2.376 ns measured routing versus 3.020 ns placement estimate,
plus a 0.097 ns route-through buffer. This sample does not support underestimated
placement delay as its cause, and is not a physical delay-model calibration.

The normal full-design packer does not pair ordinary LUTs with their destination
FFs. The existing pairing routine applies only to partial-reconfiguration carts.
There are 2,309 original-netlist and 2,329 all-FSM ordinary LUTs with exactly one
consumer, a FF's DATAIN. The previous routed designs insert buffers on 894 and
928 of those connections respectively. The critical all-FSM `ddr1_read` pair
qualifies. These counts describe existing routes, not a promised whole-design
buffer reduction after re-placement.

A bounded subsequent probe can pair these eligible connections using the existing
local LUT/FF geometry. It must measure the packing cost: that geometry restricts
the root LUT to the first ALM half, and may reduce placement freedom. It should
be tested separately from the option fix, with real placement and routing checks.

## Reproduction and scope

Raw artifacts: `out/ramtest-control-mapping/`. The [JSON evidence](2026-09-27-ramtest-placer-options.json)
records compiler/input hashes, fixture comparisons, full route commands, final
paths and measurements. `run_route.py` runs the legacy or all-FSM frozen netlist,
or the separately identified composed probe, with a selected exponent;
`summarize.py` regenerates the record. Build ID remains
`cd3e4c315005fde83065dff7483ae0d7`, Mistral remains `7ed06e21`, and all runs use
GPU 1, seed 2, timing weight 10, beta 0.5 and the original QSF/SDC. The binary's
cached CMake version string names `f60b33aa`; its hash and source commit identify
the tested candidate. No RAM-test producer lock or shared interface is changed.

Validation: the option regression has a verified failing-before/passing-after
result, three timing-parser tests pass, the probe's 48 truth rows and exact
regeneration pass, and all four full routes finish normally. The evidence verifier
checks 70 artifact hashes and eight external-input hashes. The FES result is a
documentation-only follow-up to `a49f083b`; the compiler implementation is the
separately published `19a5fe4a` candidate. Neither changes a shared contract or
claims hardware acceptance. The next integration step is broader qualification
of the combined FSM/placement candidate before changing the producer lock;
memory-clock closure still needs physical/control-path optimization.
