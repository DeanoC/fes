# RAM tester 130 MHz removal: D12 evidence, 15 November 2026

PR [#1881](https://github.com/DeanoC/fes/pull/1881). Sections 1 to 4 are the
historical D12 measurements for the PR head
`984f6e29432976a4fc690da97c6cb1e1791a686b`. They did not discharge
[AGENTS.md](../../../AGENTS.md)'s D12 for the tree as it then stood, because
main's `scripts/core_package.py` source-closure change had altered the
production build identity after the last gated build, and the no-HIL
conclusion was withdrawn pending the current tree's production-ID timing and
seal gates.

**Section 5 supplies those gates**, at the head that will merge. With it, all
three D12 conditions hold for the current tree and the no-HIL conclusion is
restored. Read section 5 for the current determination; sections 1 to 4 remain
the evidence for the earlier heads they name.

Merge base: `8bbbed8017448873fb33bc42109bc65e296f40ed`. PR head:
`984f6e29432976a4fc690da97c6cb1e1791a686b`. Both sides ran on Powerboat from
the one shared toolchain cache at
`/home/deano/fes/out/cache/misteross-toolchains`, so every run below used the
same tool installs: Yosys `3a5accbd`, Mistral `8fcc4cb4`, nextpnr-mistral
`bec82e36` with `gpu-router=HIP`. Private worktrees and outputs are under
`/data/fes-1881/`. [runs.jsonl](runs.jsonl) carries each run's command,
working directory, working tree, digests and outcome. The two harnesses the
runs used are copied here beside it: [synth_only.py](synth_only.py) runs the
producer's Yosys step alone with a caller-chosen `BUILD_ID`, and
[route_only.py](route_only.py) routes an existing `synth.json` through the
producer's own `route_after_synth` call with the producer's device, QSF, SDC,
frequency, seed list, timing weight, criticality exponent, first-pass mode and
route timeout. Each was run with the tree's `sources/misteross` as the working
directory.

Yosys `3a5accbd` is unreliable on this hardware: it segfaults in `OPT_CLEAN`
about one run in five, and about one run in six exits 0 having written a
materially different, smaller netlist
([DeanoC/yosys#77](https://github.com/DeanoC/yosys/issues/77)). Every
synthesis below was therefore run at least twice per side, and a run that
exited 0 without writing its artifact would have been treated as a failure.
None did.

## 1. Netlist byte-identity

Measured two ways. The producer's Yosys step was run alone, with the tree as
the working directory, so the `src` attributes in `synth.json` are relative
and the two trees compare directly with nothing normalised away.

**With `BUILD_ID` pinned to 32 zeros on both sides**, so both sides synthesise
the same constant and D12's `BUILD_ID` exemption is not needed:

| Run | Tree | `synth.json` SHA-256 | Bytes |
| --- | --- | --- | ---: |
| base r1 | merge base | `391b7a6b53a8f8187a7c0ce66e622892d17df92806a1f5f97c5dd64b87b2294f` | 35,448,484 |
| base r2 | merge base | `391b7a6b53a8f8187a7c0ce66e622892d17df92806a1f5f97c5dd64b87b2294f` | 35,448,484 |
| head r1 | PR head | `391b7a6b53a8f8187a7c0ce66e622892d17df92806a1f5f97c5dd64b87b2294f` | 35,448,484 |
| head r2 | PR head | `391b7a6b53a8f8187a7c0ce66e622892d17df92806a1f5f97c5dd64b87b2294f` | 35,448,484 |

`cmp` confirms byte-identity for all four base/head pairings.

**With `BUILD_ID` set to the merge base's real value**
`7a60de20fc0fb448849cd24eed50a253`, the PR head reproduces the merge base's
own gated-build `synth.json` byte for byte:

| Run | Tree | `synth.json` SHA-256 | Bytes |
| --- | --- | --- | ---: |
| merge base gated build | merge base | `a5e0d01b501fb7721240a64b8a9b71065940e8d5b23af7c2b72ff76173543347` | 35,517,271 |
| head m1 at base `BUILD_ID` | PR head | `a5e0d01b501fb7721240a64b8a9b71065940e8d5b23af7c2b72ff76173543347` | 35,517,271 |
| head m2 at base `BUILD_ID` | PR head | `a5e0d01b501fb7721240a64b8a9b71065940e8d5b23af7c2b72ff76173543347` | 35,517,271 |

This is the stronger form: it compares against the artifact the merge base's
production build actually wrote, not a pair of side-by-side experiments.

The digest over the producer's own `PINNED_INPUTS` **does** differ between the
sides — `f6c258971fd229c71fb133f4cde6bf97b1d91cb50b86bf80d02b43fe4a34983e` at
the merge base against
`fa29c2e1971a4827e28e4a1552cabcd2d713973a3518578e548b0b4ead98b16f` at the head. That contrast is the argument: those inputs
are why the `BUILD_ID` moves, and the netlist did not follow them. Neither
tree had a modified pinned input when its runs were taken.

## 2. RBF reproduction with the merge base's `BUILD_ID`

The PR head's netlist at the merge base's `BUILD_ID` — `a5e0d01b501fb772…`,
the byte-identical netlist from section 1 — was routed with the PR's tooling,
through the producer's own `route_after_synth` call with the producer's device,
QSF, SDC, frequency, seed list, timing weight, criticality exponent, first-pass
mode and route timeout, and a `gpu-run` leased GPU.

| Side | Winning seed / weight | `core.rbf` SHA-256 | Bytes |
| --- | --- | --- | ---: |
| merge base gated build | 3 / 10 | `d3774d8027b61a943e77edac3f6815c7e8f3a526da1d18dd688bcf91f64fd925` | 2,320,202 |
| PR head at base `BUILD_ID` | 3 / 10 | `d3774d8027b61a943e77edac3f6815c7e8f3a526da1d18dd688bcf91f64fd925` | 2,320,202 |

`cmp` reports the two RBFs byte-identical, and the placement search selected
the same seed on both sides. The route was not repeated: routing the same
netlist with the same binary, seed, weight, constraints and timeout is
deterministic, and `AGENTS.md`'s "Shared GPUs" section records that nextpnr
route outcomes are bit-identical across backends. The repeated runs in this
record are the Yosys steps, which is where the known non-determinism is.

Independent corroboration: the #1806 session measured this core from a
different merge base, `1a4bb8d41`, and recorded the same `BUILD_ID`
`7a60de20fc0fb448849cd24eed50a253`, the same RBF `d3774d8027b61a94…` and the
same netlist `a5e0d01b501fb772…`. `fes.ramtest`'s compiled source set and its
route-deciding inputs are unchanged between those two bases, which is why the
artifacts agree.

## 3. Historical gated build at `984f6e2`

`python3 scripts/build_fes_ramtest.py --cache-root <shared cache>` on the PR
head, with no rate argument, since the PR removes `--memory-mhz`. It passed
every gate the producer applies and exported a package:
`build/packages/6c5559cfcb9e3776b27173a62f293d16f8de1d199e90cfa3f472cb3d7d4176f3`.

`BUILD_ID` `a3d1efd69769c809559fc6b4f30262d6`, winning seed 6 at weight 10,
`core.rbf` SHA-256 `732beb63a1cfda07660664c0f73f88ca939247308b69206a4c68f839970e4436`.

| Clock | Achieved MHz | Constraint MHz | Status |
| --- | ---: | ---: | --- |
| memory `ram_clock.clocks[0]` | 101.44046 | 100.0 | pass |
| capture `ram_clock.clocks[1]` | 279.01785 | 100.0 | pass |
| pixel `display.pixel_clk` | 79.12645 | 74.25007 | pass |
| reference `FPGA_CLK1_50` | — | 50.0 | pass |

The merge base's own build, for comparison: `BUILD_ID`
`7a60de20fc0fb448849cd24eed50a253`, seed 3, memory 102.37511, capture
249.75024, pixel 78.22278, all pass.

This build's `BUILD_ID`, winning seed and RBF all differ from the merge base's,
and are expected to. The `BUILD_ID` constant is synthesised into the netlist,
so a different `BUILD_ID` gives a different netlist and the placement search
settles on a different seed — which is precisely why D12's RBF condition
forces the merge base's `BUILD_ID` rather than comparing the two production
RBFs. Section 2 is that forced comparison; this section is the separate
requirement that the PR's own build passes its gates.

## Historical conclusion at `984f6e2`

At the time of those runs, all three D12 conditions were recorded for
`fes.ramtest` at `984f6e2`: the netlist was byte-identical to the then-merge
base, the PR tooling reproduced that base's RBF with its `BUILD_ID`, and the
then-current PR build passed the normal timing and seal gates. Those results
remain evidence for the exact inputs and heads listed above; they are not
current-head production-ID gate evidence.

## 4. Merge forward to `e9218eb5e` and current status at `4e943032`

After sections 1 to 3 were measured, main advanced to `e9218eb5e` (#1791) and
main was merged into this branch. The compiled source set and route-deciding
inputs — the toolchain lock, timing and pin constraints, and placement recipe
— did not change. `scripts/core_package.py` did change in the hashed source
closure. It is not compiled RTL, so it does not change synthesis with a fixed
`BUILD_ID`; it does change the production identity input and therefore makes
the current production-ID gate a separate requirement.

- `cores/fes-common/rtl/fes_embedded_control.sv` changed, and `fes.ramtest`
  does not compile it; it is in neither `RTL_SOURCES` nor the core's closure.
- `scripts/core_package.py` changed and **is** in the core's source closure,
  so the current production `BUILD_ID` differs from the values recorded in
  sections 1 to 3. Its current value was not recorded.
- `cores/fes-ramtest/toolchain.lock`, `cores/fes-ramtest/constraints.qsf`,
  `boards/de10nano/clocks.sdc` and `fes.ramtest`'s `[recipes.pnr]` entry are
  all untouched between `8bbbed801` and `e9218eb5e`.

Rather than rest on that reasoning, the netlist was re-measured with `BUILD_ID`
pinned to 32 zeros, twice per side, at the new base `e9218eb5e` and at the
merged head `46b3a1ba8`:

| Run | Tree | `synth.json` SHA-256 |
| --- | --- | --- |
| base2 r1, r2 | new merge base `e9218eb5e3927cd76a24b218cb0d39a41a752e07` | `391b7a6b53a8f818…` (both) |
| head2 r1, r2 | merged PR head `46b3a1ba8a7f3eae29bcc50f8f1fc5ceb299c51f` | `391b7a6b53a8f818…` (both) |

`cmp` confirms the new base, the merged head and the old base produce the
identical netlist with this forced ID. Eight synthesis runs in total agree on
`391b7a6b53a8f818…`. This is fixed-ID netlist evidence; it does not run the
current production identity through the timing and seal gates. Section 2's
RBF digest remains the comparison at the old merge base's forced production
ID, not a current production-ID result.

Section 3's gated build was taken at `984f6e29432976a4fc690da97c6cb1e1791a686b`,
before the merge. The fixed-ID synthesis and forced-old-ID RBF comparisons
above do not establish a current production-ID gate pass.
The current checked-out PR tree is `4e94303238e1d683d862386b79432aec27c2517b`.
No `python3 scripts/build_fes_ramtest.py` run, current production `BUILD_ID`,
current package digest, or current timing/seal result is recorded here. The
merge-forward measurements therefore do not establish the final D12 condition
for this tree, and this record makes no current no-HIL determination. To close
that gap, run the normal authenticated RAM Tester producer on the exact PR head
and record its production `BUILD_ID`, package and RBF digests, winning seed,
and every clock result. Reassess D12 and the HIL requirement from that
exact-head evidence.

## 5. Current-tree gates at `e27a20c86`

Main advanced again to `333900cbb` (#1860) and was merged forward, so the head
that will merge is `e27a20c86fa0465570fe85ac1874bdc16814162f`. That commit
changes nine files, all under `docs/validation/2026-11-14-pc386-u7-npc-pace/`
and none under `sources/misteross/`, so it touches no producer's source closure
and moves no build identity. A `BUILD_ID` hashes its producer's transitive
closure and nothing else, so a merge touching no closure file cannot move it.

Conditions 1 and 2 were taken against merge base `8bbbed801`, and the current
merge base is `333900cbb`. Rather than deduce that the comparison carries, the
zeroed-`BUILD_ID` netlist was measured at that base too:

| Run | Tree | `synth.json` SHA-256 |
| --- | --- | --- |
| base3 r1, r2 | current merge base `333900cbb` | `391b7a6b53a8f818…` (both) |

`cmp` confirms it is identical to the original base's. Ten synthesis runs
across three merge bases and two heads now agree on `391b7a6b53a8f818…`, so
main's own netlist is byte-identical at every base this PR has had, and
section 2's RBF comparison carries under D12's merge-forward clause.

**Condition 3, the production-ID gate, measured at `e27a20c86`.** The normal
authenticated producer was run with no forcing of any kind:
`python3 scripts/build_fes_ramtest.py --cache-root <shared cache>`, on
Powerboat with `FES_TOOLCHAIN_GPU_ROUTER=HIP`, a `gpu-run` leased GPU, and the
output path left to the producer. It passed and exported a package.

- production `BUILD_ID` `06f01b2c9c16de7d936136251d3f7a09`
- package `4ee83633a9e456a05d06f5c2408a7cc48965f89e49bc9e0af40e194166c02da6`
- `core.rbf` SHA-256 `0318faa9be02b3bfdb6c44c391a0e5d958c80ca7a76d3a5ef62cb8d53a61b4ea`, 2,337,005 bytes
- winning seed 6 at weight 10, two ranking entries in `qor-ranking.json`

| Clock | Achieved MHz | Constraint MHz | Status |
| --- | ---: | ---: | --- |
| memory `ram_clock.clocks[0]` | 100.97950 | 100.0 | pass |
| capture `ram_clock.clocks[1]` | 299.94000 | 100.0 | pass |
| pixel `display.pixel_clk` | 81.04385 | 74.25007 | pass |
| reference `FPGA_CLK1_50` | — | 50.0 | pass |

The runner checked the producer's exit status and then that
`build-summary.json`, `core.rbf`, `qor-ranking.json` and `manifest.toml` were
each non-empty, because an exit 0 that wrote no artifacts is a failure.

**Margin worth recording.** Memory passes at 100.97950 against 100.0, under
1% of headroom on the binding clock. The same core's earlier identities gave
102.37511 at the merge base and 101.44046 at `984f6e29`. The identity constant
is synthesised in and reshuffles placement, so the memory clock moves by a
megahertz or so between identities with no functional change at all. On #1806
the same `core_package.py` constant move made `coleco_v2` miss timing on all
ten seeds. A future identity move on this core coming back failing would not be
anomalous; this gate is a measurement, not a formality.

## Conclusion

All three of D12's conditions hold for `fes.ramtest` on PR #1881 at
`e27a20c86`:

1. the netlist is byte-identical to the merge base with nothing normalised,
   measured at every base this PR has had;
2. a build with the PR's tooling and the merge base's `BUILD_ID` reproduces
   that base's RBF byte for byte, same winning seed;
3. the PR's own unforced build at the merging head passes the core's normal
   timing and seal gates, with its production identity recorded above.

No reseal and no kit HIL are required for this change. This supersedes the
withdrawal above, which was pending exactly these gates.

## Scope of this record

The historical evidence covers artifact identity only. It is not hardware
acceptance, and no kit was programmed. The PR keeps the RTL's 130 MHz branches
and the Quartus comparison lane untouched for the reason D12 implies: deleting
RTL lines would shift the line numbers inside `src` attributes, which the rule
does not exempt, and netlist identity would genuinely fail.
