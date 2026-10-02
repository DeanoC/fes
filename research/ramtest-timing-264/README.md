# RAM-test timing checkpoint — 2026-10-02

This checkpoint preserves the work for [FES issue 264](https://github.com/DeanoC/fes/issues/264)
at the user's request to continue in a fresh session. It is a research snapshot,
not a change to the production RAM-test recipe. The 130 MHz goal remains open.

## Start here

The best qualified frozen combined result is **117.86892700195312 MHz** memory,
93.57162475585938 MHz pixel and 378.5823974609375 MHz capture. All six required
non-target clock windows pass and final signoff reports no hold violations.
Memory still misses its 130.0052032470703 MHz constraint: 8.484 ns complete path
against a 7.692 ns window. The result is about 12.131 MHz below nominal 130 MHz.

That result belongs to a particular experimental compiler and input set. The
latest PR #109 review correction changes net iteration order; its host checks
pass, but it needs a fresh route before claiming the same timing. Keep these
two facts separate when reporting progress.

The publication fixes and experiments are generic compiler transformations.
They do not match RAM-test RTL hierarchy names. Experiment selection and
measurement scripts naturally refer to this benchmark. No RTL, production
recipe, compiler lock, interface, timing constraint or board has changed in
this checkpoint.

## Pushed code

The FES checkpoint branch is `checkpoint/ramtest-timing-264-2026-10-02`, based on
`7ce9926941a5f72cc3ca4b0ed61b7b06e76e5304`. Compiler changes live in their own
repositories. All ten prior attached worktree heads and local paths are recorded
in [branches.json](branches.json); their working trees were clean at capture.

| Repository / branch | Checkpoint commit | State |
| --- | --- | --- |
| nextpnr: `feat/mistral-lut-pair-copies-264` | [1969db9a](https://github.com/DeanoC/nextpnr/commit/1969db9a861d5dde2a923b13575236c0e6704f1c) | PR #109, including its review correction and the already merged PR #110 |
| nextpnr: `experiment/mistral-lut-pair-copy-plan-264` | [5ac46a5a](https://github.com/DeanoC/nextpnr/commit/5ac46a5af641296fae10d24d023df8f98e27af55) | Earlier composed-copy staging experiment |
| nextpnr: `experiment/mistral-capture-pipeline-264` | [a0432321](https://github.com/DeanoC/nextpnr/commit/a0432321e5ab99e20ec83e4fe97b652128b3c4b8) | Frozen C0 source |
| nextpnr: `feat/mistral-capture-pipeline-locality-264` | [7c21599d](https://github.com/DeanoC/nextpnr/commit/7c21599d15c39ad94c629e8ce928622bebaa3113) | PR #110 publication source |
| nextpnr: `experiment/mistral-capture-pipeline-search-264` | [cdb6c401](https://github.com/DeanoC/nextpnr/commit/cdb6c4018628d8f75f746578b59aec1353e21617) | Saved measured wider-search work; production and native-test files match #109 |
| nextpnr: `experiment/mistral-hard-input-locality-264` | [cd557f5c](https://github.com/DeanoC/nextpnr/commit/cd557f5ccdc35c6a7793447dc874f859a811f59f) | Implemented, tested, no retained RAM-test moves |
| yosys: `experiment/intel-alm-ffmux-264` | [ac120c9d5](https://github.com/DeanoC/yosys/commit/ac120c9d526178a5693cd96faa16d82f8c11ba7c) | Implemented and functionally tested; RAM-test synthesis and timing pending |

## Completed validation and remaining limits

**PR #109 review correction.** Accepted LUT copies now preserve every original
cell/net/alias iteration prefix and append new owners. Repeated acceptance also
preserves earlier copies and existing GPU net indices. The production correction
is `ae372ba6`; `1969db9a` adds the final fixture correction and documentation.
The last run passed **45 native cases**; the same production binary passed
**38 CLI methods**. A legal same-LAB placement for the second copy required
correcting the fixture's overly broad different-LAB expectation; all identity,
net-index and native legality checks remain. The frozen benchmark documentation
now accurately says its old proof checked relative original-owner order, not a
complete prefix or stable GPU indices. See the
[exact evidence and limits](https://github.com/DeanoC/nextpnr/blob/1969db9a861d5dde2a923b13575236c0e6704f1c/docs/validation/ramtest-lut-pair-copy-2026-10-02.md).

**Capture-pipeline search.** The wider search retained 53 pairs / 106 moves and
reached **107.5037612915039 MHz**, versus a comparable fresh HeAP baseline of
105.77532958984375 MHz. This is a different compiler/options comparison from
the 117.869 MHz combined result. All 15,290 reported paths, seven clock pairs,
six non-target windows and final hold checks were considered. Memory still
fails 130 MHz. This source has already been carried into PR #109 through #110.

**Hard-input locality.** The generic default-off pass tries moving FFs toward
the actual physical routing pins of fixed registered hard-block inputs. It
passed 17 focused native cases, eight CLI methods, 43 prior native cases and
three focused CTest targets. Of 315 eligible FFs, selected budgets of 8 and 64
both retained zero moves; their entire placed netlists and timing outputs match
the disabled prefix. Improving outgoing paths failed incoming, feedback or
clock checks. Do not generalize that result to all 315 candidates or assume the
worst write-data launch was tried. There is no full-route gain. See the
[experiment record](https://github.com/DeanoC/nextpnr/blob/cd557f5ccdc35c6a7793447dc874f859a811f59f/docs/validation/ramtest-hard-input-locality-2026-10-02.md).

**Yosys native FF data selector.** `synth_intel_alm -ffmux` is opt-in and maps a
direct mux feeding an unreset native FF onto that FF's existing SLOAD/SDATA
input selector. It preserves enable, clock, initialization, reset priority and
shared consumers. The final fixture passed **11 initialized base proofs plus
11 induction proofs**, with 54 pass invocations; the existing `dffs`, `adffs`
and `fsm_init` fixtures also passed. This has **not** been run on the RAM-test
RTL. Native selector routing and shared LAB controls may offset any LUT saving.
See the [mapping contract and restart details](https://github.com/DeanoC/yosys/blob/ac120c9d526178a5693cd96faa16d82f8c11ba7c/docs/experiment-intel-alm-ffmux-264.md).

These are host-only tests and compiler measurements. No hardware acceptance is
claimed. Recorded binaries were built from the source snapshots identified by
their records; new commit metadata does not turn an old binary into a build of
the publication commit.

## Resume order

1. Read the linked compiler records and check current PR #109 status. Its review
   correction is pushed. Build the selected source explicitly and requalify the
   combined route with the corrected ordering before transferring the historical
   117.869 MHz claim to the current branch.
2. Keep the exact RTL, generated headers, BUILD_ID, constraints, seed, remap plan
   and route options fixed for comparisons. Use the existing saved inputs below;
   the ordinary producer recalculates BUILD_ID and is unsuitable for this control.
3. Continue the FF-selector experiment with a fresh reviewed paired-synthesis
   producer: default off versus `-ffmux`, differing only in that option. The
   saved `capture-pipeline-264/trial_ramtest_ffmux_synth.py` is **unrun** and has
   a known dependency-list problem: ABC9 reports temporary AIG inputs/outputs
   which it deletes, but the draft permits only final JSON and declared source
   inputs. Fix its accounting before running it, and refresh source/build
   bindings after the new commits. The Yosys document gives the intended
   one-shot `-P` snapshots and proof boundaries.
4. If the actual prepass delta is equivalent, perform paired placement/routing
   and check every required clock and phase window plus final hold signoff.
   Keep the user's acceptance rule: improve target memory timing while all
   other required timing checks still pass. A higher headline MHz alone does
   not qualify a gain.

Quartus remains a useful structural guide. Its write-data fit uses a nearby FF
and native data selector, motivating `-ffmux`. That particular query used a
different BUILD_ID, so it is a structural hint rather than a matched numerical
comparison. Rejected trials remain saved; they are not enabled by default.

## Inputs and preserved research files

Original worktree: `/home/deano/kepler/worktrees/fes-fes-ramtest-compiler-gains-264-17a49c0d`.
The fixed inputs live beneath `out/ramtest-merged-remap/measured-diagnostic/`:

| Input | Identity |
| --- | --- |
| BUILD_ID | `7168b508ab424b70f1c0f87b2035d821` |
| `run-inputs.json` SHA-256 | `1d5ec9f5e18bfffb6083860af8257bbfb5daf093014f20621a3b3bd623b189a8` |
| `synth.json` SHA-256 | `61ee68c657e49364425dd4ba4880a679e0759992f2750e284e256d1e4139de28` |

The adjacent `yosys.log` records the original ordered 13 RTL files and one-shot
synthesis command. Raw runs remain under
`out/ramtest-wide-reduction-264/`; recent runs are in `capture-pipeline-264/`.
Builds remain under `sources/misteross/build/toolchain-ramtest-timing/build/`.

[research-helpers.tar.gz](research-helpers.tar.gz) preserves 557 authored helper,
query, patch, diagnostic-source and note files from the wide-reduction research
directory, with their original relative paths and bytes. The per-file inventory
is [research-helpers.json](research-helpers.json). Archive SHA-256:
`13cfddf31fc46a53185acf982ad06ffe4d7d2060de9bd3146d4e6648cc27b267`.
It is historical source material, including abandoned and unrun trials; it is
not a self-contained build recipe. Inspect and adapt individual helpers to the
selected current source, paths and inputs rather than executing the archive as
a workflow. Old notes inside it describe their dated experiments.

[latest-validation-records.json](latest-validation-records.json) contains small
extracts and exact hashes/locations of the latest full records. Large raw
netlists, compiler binaries, generated model data, full logs and route outputs
remain on disk and were not added to Git. Nothing was deleted or overwritten.
