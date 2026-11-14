# Per-tool toolchain installs (#1747): D12 records

On Powerboat, 12–13 Nov 2026, against main `2d895034c`. Every build below ran
through `gpu-run`; the OSS routes used HIP.

**D12 counts a toolchain at the same commit as the same toolchain wherever it is
installed** (Deano, 12 Nov; written into D12 by this PR). Yosys records its own
techlib file paths in `src` attributes, so relocating an install changes the
netlist text without changing the design. Those paths, and the checkout path,
are the only netlist differences besides the `BUILD_ID` constant.

Each core below has three builds:

- **main:** a clean checkout of main `2d895034c`, running main's producer with
  its local toolchain roots (`build/toolchain/<root>/install`). Those roots hold
  hardlinks of per-tool installs at the same commits main's locks name. These
  reference BUILD_IDs are main's code with those binaries; they are not the
  BUILD_IDs of main's sealed packages. The runs passed no `--cache-root`, and
  `FES_TOOLCHAIN_CACHE_ROOT` and `CACHE_ROOT` were stripped from the
  environment: `_authenticate_tools` returns from its shared-cache branch
  before it reads `toolchain_root`, so a cache root silently ignores
  `--toolchain-root` and resolves binaries no evidence here covers.
- **forced:** a clean checkout of the PR, run with
  [`forced_build_id.py`](forced_build_id.py) and the main build's BUILD_ID.
- **own:** the same PR checkout, running the producer's own command line.

Every PR build used the per-tool installs under
`out/cache/misteross-toolchains/tools/<tool>/<key>/install`. The tool commits
are identical to main's; each binary has a different sha256 only because it
embeds its own install prefix. For the ZX81 family, for example:

| Tool | commit | main sha256 | PR sha256 |
| --- | --- | --- | --- |
| Yosys | `3a5accbd` | `20740ef0…` | `a17b4c8a…` |
| nextpnr-mistral | `1eecf877` | `adc789d3…` | `4d52a674…` |
| Mistral | `8fcc4cb4` | `ef4a2753…` | `33d473e1…` |

That difference is why every own BUILD_ID in this record differs from main's:
BUILD_ID v2 hashes tool identities, and a tool identity carries the binary's
sha256.

**The two sides are genuinely different builds, not one install seen twice.**
They are separate installs with different keys and different recipe digests:
main's side `20685ac5…`, an earlier revision of this PR's `bootstrap.sh`, and
the PR's side `75baea6d…`, its current one. So the forced comparison varies the
producer and helper code *and* the binaries at once, and still reproduces main's
RBF byte for byte. That is the evidence that an install's prefix does not reach
the RBF — the premise this PR rests on — rather than an assumption behind it.
Nothing in this record needed the tooling held constant, and it was not.

## Registered OSS producers

**Fifteen cores met every D12 condition when measured, against main
`2d895034c`.** That is not the same as meeting D12 at this head: see [what
revision this evidence is against](#what-revision-this-evidence-is-against)
below, which states the gap. For every one of them, at `2d895034c`:

- the forced `core.rbf` is byte-identical to main's;
- the forced `synth.json` equals main's once the checkout path and the tool
  install directory are normalized in `src` attributes (the raw files differ, so
  the normalization is doing real work);
- the own build passes every clock and every producer seal check.

| core | main = forced BUILD_ID | main = forced RBF sha256 | PR BUILD_ID | seed/weight | tightest clock MHz (achieved / required) |
| --- | --- | --- | --- | --- | --- |
| `fes.apple2` | `19e4f6289ddcccfffad959d2e0714738` | `65fa3a599485a848…` | `94544eb9cf5ee78cfb29e20bdf18a5a6` | 5/2000 | system 57.66 / 52.22 |
| `fes.atari-st` | `c4e94b207e2bc833419e5e1753f994b4` | `9b8090fe7371b5a0…` | `7d97988256212aadffb792d66e187946` | 4/2000 | system 52.57 / 52.22 |
| `fes.c64` | `e36c6722499691ea47dc685ce75058ed` | `de523abc4eae40c0…` | `2002d695cf78fc472db5e9b5138debf9` | 4/2000 | system 53.38 / 52.22 |
| `fes.menu` | `796c04b7f92233cf4dbed193348171de` | `de67d63aa73f0839…` | `53aac94601722af9906f761bb1acca8b` | 5/10 | pixel 89.93 / 74.25 |
| `fes.pentagon` | `aa5e405b3fa87d298923d1a41658ea3f` | `38a1059122812df9…` | `a60fa4df58ffd9368914b94d8d50e776` | 5/2000 | system 57.94 / 52.22 |
| `fes.pong` | `7f69032fad0cb7b10989222f04f03346` | `9b79c052284bf3c5…` | `ec962ad3081bcc846ffb6ae5a322a27a` | 1/10 | pixel 85.51 / 74.25 |
| `fes.riscv` | `bb19551b45ba57bbdb855ae9a856c575` | `b52dd2ee1dbf6b8f…` | `4344119a54cbbe23886a963c574f0cd5` | 1/10 | pixel 77.56 / 74.25 |
| `fes.sg1000` | `14df46f864eefc2b14e77eaccc42d095` | `afb1c39f545064c2…` | `093dc79bd224aa9f057b60156d1100b1` | 2/2000 | system 67.63 / 52.22 |
| `fes.sms` | `d475f84f43893a2949edb5372372e689` | `775c2f2cd808218a…` | `41591c062266afd1f7335a8fb06c845e` | 3/1000 | system 57.62 / 52.22 |
| `fes.spectrum` | `3cdcdf9cbb245e771352ed02e3307fae` | `263ffed3e11731b3…` | `1f2cacc88e9b29f851456881b29c787a` | 5/2000 | pixel 97.14 / 74.25 |
| `fes.spectrum-plus3` | `69603f6bd53c16c6e5c2d9729cfa811e` | `7b6b76aa4a115fac…` | `1bdac0982bd709093ae829e4eb7a1584` | 1/2000 | system 54.96 / 52.22 |
| `fes.spectrum128` | `fca074790c562e5cbf32807975db80d3` | `a6762b063ebfd71c…` | `0767d4bc5dcfaa1d95c79119ddcc34f4` | 5/2000 | system 63.73 / 52.22 |
| `fes.ts1000` | `0ebc5104d7e678683e556f125476dd3f` | `7ceb7bc7f1999d70…` | `1a38dc7b50be59d80cec25ab6c8b71c2` | 1/300 | system 58.25 / 52.22 |
| `fes.zx80` | `dd94d35c798acf4da7f9f26a387ab5c3` | `ec8fe32d78467575…` | `4c9fe524cdb43d6035e50b19e75edc68` | 1/100 | system 52.30 / 52.22 |
| `fes.zx81` | `d664831dabef0eca18cefc1ac15fe492` | `6422d68ba1db9736…` | `f9f078f5d40c863e3750dae40f629fdb` | 1/300 | system 52.40 / 52.22 |

Two cores did not clear the third condition **at `2d895034c`**, and neither
failure was caused by this PR's code:

- **`fes.ts1500`** (issue #1803): forced matched main byte for byte, but the own
  build met no placement (best seed 10, weight 2000, worst ratio 0.9630) across
  all fifteen seed/weight attempts — none was lost to a route timeout. At the
  head before that, this same core's own build passed with a 0.01% margin
  (52.230 MHz against a 52.2248 MHz constraint). Its closure was unchanged by
  this PR; only the BUILD_ID moved, and that was enough.
- **`fes.ramtest`** (issue #1803): main's own producer also fails on this host,
  so there is no main reference and no forced build. See below.

Both of them close at `02beac79b`, as does `fes.pcxt-dos`. See
[re-measurement at this head](#re-measurement-at-02beac79b). That answers the
third condition only; it leaves the byte-for-byte condition where the gap
section puts it.

## fes.ramtest remains a merge gate

This head has no exact-artifact HIL for `fes.ramtest`, and no valid D12
comparison in either direction. The own build does close at `02beac79b` (see
[below](#re-measurement-at-02beac79b)), so the gate is no longer a timing miss —
it is the missing comparison and the missing HIL. The main package remains the
existing artifact; nothing in this record supplies a qualified replacement. Do
not treat it as clearing the RAM Tester hardware gate.

| Code | nextpnr | BUILD_ID | Result |
| --- | --- | --- | --- |
| main `2d895034c` | `9128800f` (main lock) | new | no placement met timing, best seed 8, worst ratio 0.9714 |
| this PR at `2d895034c` | `9128800f` | own | no placement met timing, best seed 7, worst ratio 0.9987 |
| PR #1788 head | `bec82e36` | own | **passed**, seed 7, memory 101.698 / 100 MHz |
| this PR, with #1788 folded in | `bec82e36` | own | no placement met timing, best seed 7, worst ratio 0.9702 |
| this PR at `02beac79b` | `9128800f` | `cc6eca8e8d73cbc7b5bf069b1ca2ed09` | **passed**, seed 7, worst ratio 1.0021 |

All eight seeds completed in each run; none was lost to a route timeout. The
margins on both passes are inside placer noise — 1.7% for the #1788 head and
0.21% at this head — so neither is evidence that anything was repaired, and
moving the lock forward is not a fix. The #1788 fold has been reverted out of
this PR: #1752 does not touch `fes.ramtest`.

#1788 itself is now in draft with its premise withdrawn, since this core closes
at this head without the lock move. Two things made it worth withdrawing rather
than merging. Its own comparison is void on its own terms: its evidence was
taken at `70fc97f61` on 13 Nov, and `090144ced` (#1767) changed this core's RTL
on main thirteen hours later. And since the D12 amendment in this PR, a nextpnr
pin is a route-deciding input, so folding it in would oblige a re-measurement
rather than let this record's evidence carry forward — the clause's first
application is to the PR that prompted it.

**Why there is no comparison to be had.** Main commit `090144ced` (#1767)
changed this core's RTL, so the pre-#1767 forced-build reference is void, not
merely stale; and main's own producer does not close on this host, so a fresh
reference cannot be built here either. The `02beac79b` row is therefore a bare
own-build result. No D12 comparison is constructed from it, and none should be
read into it.

## fes.pcxt and fes.pcxt-dos (Quartus)

The PC/XT sealer authenticates one Yosys install. Main's lock pinned
`d45b5d6e…`, the binary of a scratch install built for PR #1652 that no longer
exists on Powerboat or on chip-30x0-ai-dev. This PR repins it to `a17b4c8a…`,
the same Yosys commit `3a5accbd` installed under the per-tool layout, and makes
the producer report the digest mismatch by name instead of failing as "no
candidate found".

| Profile | main = forced BUILD_ID | main = forced repeat RBF sha256 | own BUILD_ID | own result |
| --- | --- | --- | --- | --- |
| `fes.pcxt` (diagnostic) | `f1a07fda62bdc50c76e7994fbc6a7de9` | `55614ec1156ad80c…` | `a2e52a8f8fbfe80dd426facbb5f0598c` | pass, seeds 1–3 plus the repeat build |
| `fes.pcxt-dos` | `e162041bf1ed5fbe1a54ff08b3f84e55` | `9db8fb89a0c25940…` | `1385062da52b2d69a32e7d6b281cfbe6` | **failed an audio seal check** at `2d895034c`; closes at `02beac79b` |

Against `2d895034c`, both profiles met the netlist and RBF conditions: the
forced builds reproduced main's RBF byte for byte, and the Yosys netlists are
identical for all three seeds on both profiles once the build path is
normalized. As everywhere in this record, that is unverified at this head. (The PC/XT net names
embed the absolute build directory mangled into identifiers, so that
normalization covers `…/build/fes-pcxt-quartus/<build-id>/attempt-<id>/` as well
as the checkout and install paths.)

`fes.pcxt` meets the third condition too. At `2d895034c`, `fes.pcxt-dos` did
not: its own build stopped at

```
Error (332000): Audio endpoint binding failed: speaker_output_audio_output_stage_acknowledge: 2
```

The conclusion is stronger than "pre-existing failure", and it is worth stating
precisely. Main's tree passes at main's BUILD_ID `e162041b…` and fails at
`1385062d…`; this PR's tree does the same. Both trees pass and both trees fail,
decided by nothing but which BUILD_ID constant is compiled in. Main is not
broken, and this PR does not break it — **in that controlled pair, the BUILD_ID
constant alone decides whether this fit closes.** That control is recorded on
issue #1611, and it is the same single cause as #1803 on a different core and a
different toolchain. No seal or timing check was relaxed.

## fes.coleco

The registered native-video shell. Its record is in
[coleco.md](coleco.md).

## What revision this evidence is against

Everything above was measured against main `2d895034c`, which was the merge base
when the campaign ran. The branch has since merged main `53681975f`.

**The gap, and how it was closed.** D12's second condition is that a
forced-BUILD_ID build reproduces main's `core.rbf` byte for byte. Every forced
build here did that against `2d895034c`, and none of it was verified at the
post-merge head when this record was first written: the merge touches a closure
file of every core in the table — `scripts/compiler_read_audit.py` reaches all
fifteen OSS cores, and `cores/fes-common/generated/fes_application.vh` reaches
most of them — so the BUILD_IDs recorded above are not the ones the post-merge
tree produces. That gap is now closed, by re-measurement for the four cores
whose compiled sources moved and by the carry-forward test for the thirteen
whose did not. The sections below give both.

**One core's comparison is void, not merely stale.** It would be convenient to
argue that relocating an install changes nothing but file paths, so the evidence
holds whatever main does. The merge itself is a counterexample. Main commit
`090144ced` (#1767, "Discard DDR latency windows after data faults") changed
**fes.ramtest's RTL**: `rtl/ddr_channel.v`, `rtl/lat_monitor.v`, `rtl/top.v` and
its simulation. That is a design change, not install-path churn. fes.ramtest's
main reference RBF has therefore moved, and its forced-build comparison is void
on its own terms rather than out of date. Any core the next merge touches can go
the same way, so the argument cannot be assumed to carry across merges — it has
to be checked per merge.

The three non-closing cores were also measured on the pre-merge tree. Their
closures moved, so their placement outcomes were rerolled; the next section is
that re-measurement.

**The call since made.** Condition 2 was re-measured against main `bb968862b`
for four cores and not all seventeen (Deano, 14 Nov). Condition 1
was re-measured for all seventeen and passed throughout — eight pairs on
Powerboat (apple2, c64, menu, pcxt-dos, pong, riscv, sg1000, sms) and eleven on
chip-30x0-ai-dev (atari-st, pentagon, sg1000, sms, spectrum, spectrum-plus3,
spectrum128, ts1000, ts1500, zx80, zx81), with sg1000 and sms measured
independently on both hosts and each passing against its own host's pair. And
thirteen cores have no change in their compiled source set between
`2d895034c` and `bb968862b`, so their recorded comparison is against the same
netlist it always was. Four cores are different because main changed RTL they
synthesise: `fes.c64` (`c64_d64_medium.sv`, a `!halftrack[0]` term on
`cache_ready` and the same gating on the track-rebuild reset), `fes.sg1000`
(`c0deb8e39`, #1755, moved it to the application controller ABI), and
`fes.menu` and `fes.riscv`, whose `fes_application_gp` instantiations lost four
empty firmware port connections — a logical no-op that nonetheless moved riscv
from 3,923 to 3,912 cells and menu from 9,665 to 9,664, so their recorded main
RBF is a different artifact. Nothing route-deciding moved for any campaign core
in that window: the only candidates were two new `board = "de25-nano"` recipe
entries, `cores/fes-pc386/toolchain.lock` and the i386 sub-lock, and
`boards/de10nano/` is untouched.

### Eight cores are closed by an argument, not by the clause

The partition has three classes, not two, and the middle one is where the
clause runs out. `cores/fes-common/generated/fes_application.vh` changed between
`2d895034c` and `bb968862b`, and it is **in the input lists** of eight cores
beyond the four:

| core | how the change reaches it | in the four? |
| --- | --- | --- |
| `fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon` | `cores/fes-spectrum/rtl/top.v` includes it, and is compiled | no |
| `fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500` | compiled `fes_display_plane.v` and `fes_hps_ddr.v` include it | no |
| `fes.c64`, `fes.menu`, `fes.riscv`, `fes.sg1000` | same, plus their own RTL | yes |
| `fes.coleco` | `cores/fes-coleco/rtl/top.v` includes it, plus three of its own RTL files | no — see below |

The partition classified these eight explicitly, as netlist-neutral by
derivation, and did not overlook them — the header is why their producers came
out as moved in the first place. `fes.ramtest` compiles both the header and
`fes_application_gp.v`, which also changed, but it has no comparison in either
direction anyway.

**The derivation.** The whole of the header's change is the removal of ten
`` `define ``s, all firmware-mailbox constants from #1714:
`FES_APPLICATION_OPCODE_FIRMWARE_{BEGIN,DATA,COMMIT}`,
`FES_APPLICATION_FIRMWARE_{BYTES,DATA_PAIR_INDEX,DATA_TAIL_INDEX}` and the four
`FES_APPLICATION_INTERFACE_FIRMWARE_BLOB_*`. `git grep -l` for those names at
`2d895034c` across `cores/**` and `boards/**` returns **exactly two files**: the
header that defined them and `cores/fes-common/rtl/fes_application_gp.v`, the
module that used them. Neither the Spectrum nor the ZX81 producer compiles that
module — the ones that do are coleco, sg1000, menu-package, riscv and ramtest.
So the deleted macros are unreferenced in every file these eight cores compile,
and a header of `` `define ``s alone gives no cell a `src` attribute. The change
moves their `BUILD_ID` and can reach nothing else, so main's netlist and RBF for
them are the same at both bases.

That argument is sound but it is **not** what D12 now says, and that is the
point worth recording. The clause asks whether the compiled source set is
unchanged. For these eight it is not: a file in their input lists changed. The
rule therefore does not license them, even though the derivation does — a gap
found by applying the clause on the day it was written.
**Deano accepted the derivation for these eight cores rather than ordering a
re-measurement** (14 Nov), on the stated ground that the change is the removal
of defines no core references. So they are closed by an accepted argument, not
by the clause, and this record says which — the difference between "the rule
licenses it" and "an argument rescues it" is the distinction it exists to keep.

Anyone applying the clause later should note that it is blunter than this
decision: it turns on the compiled source set changing at all, where the
decision turned on whether the change could reach the netlist. Nothing here
widens the clause, and a future case wants its own answer rather than this one
read across.

**The derivation has since been measured, so the eight no longer rest on it.**
Synthesising main at both bases with one Yosys install and `BUILD_ID` forced
identical leaves the checkout root as the only legitimate difference, and that
discriminates cleanly:

| core | main at `2d895034c` | main at `bb968862b` | |
| --- | --- | --- | --- |
| `fes.spectrum`, one of the eight | 24,842,631 bytes | 24,842,631 bytes | **byte-identical** |
| `fes.coleco` | 281,690 lines, 8,382 cells | 279,113 lines, 8,330 cells | **differs** |

Main's netlist for Spectrum is the same artifact at both bases, so the recorded
comparison describes the current one and the argument is confirmed rather than
trusted. Coleco's netlist moved in the same test, which is consistent with its
RBF having moved, so the test refuses the case that should be refused.

That is a third answer to "accept the derivation or re-measure", and a cheaper
one than either: two Yosys runs per core, no place-and-route and no GPU lease,
against eight full forced builds. It is worth knowing independently of whether
the clause ever changes, because it converts a question that was being argued
into one that can be run.

`fes.coleco` is not in that position. Main changed its own compiled RTL —
`rtl/{coleco_application_gp.v,coleco_machine.sv,top.v}`, five of the 43 sources
its producer lists — so no derivation rescues it and its comparison is void on
its own terms. Codex raised this as a P1 on `coleco.md`, correctly. It is being
re-measured.

### Condition 2 at the new base: four of four pass

Measured against main `bb968862b`. For every core, main's reference and the
forced build at main's BUILD_ID produce the **same `core.rbf`, byte for byte**.

| core | main's BUILD_ID | `core.rbf` sha256, both sides | bytes |
| --- | --- | --- | --- |
| `fes.c64` | `dd50efd6b5247d785b7a4a543da0982e` | `7c054cdd9f52500a…` | 3,799,950 |
| `fes.sg1000` | `86d3f8365140566088277b69623b602e` | `011bbf0e8de4d9a8…` | 2,509,696 |
| `fes.menu` | `39baa2f28e85d3a50db66d4118153eb7` | `f0ccbe3d990612a3…` | 2,171,815 |
| `fes.riscv` | `30ab3423b194bb198c18c5f0a489a282` | `d0845b6da9033e9d…` | 2,254,479 |

Forced builds took 12m41s, 2m28s, 3m14s and 5m09s; the main references 11m45s
and about two minutes each. First-pass found an early closing seed throughout.

**The amendment is now measured, not argued, four times over.** On every one of
the four the raw `synth.json` differs and is identical once the install prefix
and the checkout root are collapsed, checked by substitution rather than
assumed, while the RBF is byte-identical. The prefix reaches the netlist text
and reaches nothing in the RBF. The four span a real logic change (`fes.c64`),
an ABI migration (`fes.sg1000`) and two dangling-port removals (`fes.menu`,
`fes.riscv`), so this is not one core's accident. It also confirms the two sides
really were resolving tools from different prefixes — main's from its local
hardlinked root, the forced build's from the shared per-tool cache — which is
the isolation the campaign intended.

**An RBF is not reliably comparable across install prefixes, and the cause is
unestablished.** A cross-host pair, 138 characters of prefix against 47,
produced different serials in generated cell names from the same binary and
sources. Path length looked like the mechanism and is not: Coleco's two
prefixes are 132 and 138 characters, six apart, and its serials did not shift at
all — the two sides' RBFs are byte-identical. So the observation stands and the
explanation does not. Both sides of every comparison here ran in one place, and
a future comparison should do the same until someone establishes what actually
drives it.

## Re-measurement at `02beac79b`

Run 14 Nov on Powerboat in a clean checkout at
`02beac79b5fe2caa7939aefbbe860a79c466e9e0`. **All three cores that did not close
at `2d895034c` close here.** These are own builds under each producer's normal
gates — no seed hunting beyond the recipe's own list, no lock moves, and no
forced-BUILD_ID build was run for any of them.

**This answers D12's third condition only.** The byte-for-byte condition is
still unverified at this head, for these three as for every core in the table
above, and whether evidence measured against the merge base counts for this PR
is an open call. Nothing below closes either question.

| core | BUILD_ID | seed/weight | binding clock | achieved / constraint MHz | worst_ratio |
| --- | --- | --- | --- | --- | --- |
| `fes.ts1500` | `95fcfb5dd8ac25942ce37776f9b6aac8` | 1/1000 | `clk_sys` | 52.9156 / 52.22477 | 1.0132 |
| `fes.ramtest` | `cc6eca8e8d73cbc7b5bf069b1ca2ed09` | 7/10 | `ram_clock.clocks[0]` | 100.2104 / 100.0 | 1.0021 |

`fes.ts1500` also passes `display.control.clk` at 90.098 / 74.250 and
`audio_clk` at 237.586 / 12.288. `fes.ramtest` also passes `display.pixel_clk`
at 81.619 / 74.250 and `ram_clock.clocks[1]` at 281.849 / 100.0.

Both margins are thin, and `fes.ramtest`'s 0.21% is thinner than the 1.7% this
record already calls placer noise — its next three seeds failed at 0.9981,
0.9951 and 0.9814. Issue #1803 tracks why margins this thin move with the
BUILD_ID alone; this record does not analyse it.

`fes.ramtest` here is a bare own-build result with no valid D12 comparison from
any direction, for the reasons in [its own
section](#fesramtest-remains-a-merge-gate). None is constructed from it.

**`fes.pcxt-dos`.** The Quartus sealer has no nextpnr `worst_ratio`; per-seed
TimeQuest slack is the equivalent number. BUILD_ID
`59d28f98637177c0d739a6fb47c9ff30`, `internal_timing_pass: true`, signoff seeds
1–3 plus a repeat of seed 1. Every path class reports 0 violated in all four
compiles, and each audit carries exactly one `AUDIO_CONTRACT` line — five
asynchronous source pairs, bounded held payload, max delay 81.387 ns — with no
`Error (332000)`.

| run | worst setup slack ns | worst hold slack ns |
| --- | --- | --- |
| signoff seed 1 | 0.997 | 0.127 |
| signoff seed 2 | 0.747 | 0.122 |
| signoff seed 3 | 0.332 | 0.126 |
| repeat of seed 1 | 0.997 | 0.127 |

`repeat_rbf_sha256` `b1932849a5c3…` equals the packaged `core.rbf` sha256, so
the repeat compile reproduced the signoff RBF. The
`speaker_output_audio_output_stage_acknowledge: 2` seal failure does not recur
at this BUILD_ID.

Main changed nothing under `cores/fes-pcxt`, and every board file it changed is
under `boards/de25nano` while this core targets de10_nano. But main did change
shared files, `cores/fes-common/generated/fes_application.vh` and
`rtl/fes_application_gp.v` among them, and this core's closure has not been
re-derived — so **no claim is made here that the BUILD_ID constant is the only
input that moved for `fes.pcxt-dos`.** What is established is that its own code
and its board files are untouched.

## Three test failures this branch did not cause

`scripts/test_changed.py --base origin/main` stops the fpga lane at its first
failing command, so either of these leaves 25 RTL simulation lanes under
`not_started` and a sweep that hits one proves much less than it looks like it
does. Both are worth recording because the next person will hit them.

- **A timeout set too tight.** `test_de25nano_scanout` failed because
  `Vfes_de25_scanout` exceeded the 30 s `subprocess.run` timeout in
  `boards/de25nano/scanout/sim.py`. Verilation had succeeded. Re-run alone, with
  nothing else of mine in flight but a loadavg of 13.9 on 24 cores, the same
  three sims take 67.6 s of wall clock and exit 0 — the limit sits below the
  honest runtime whenever the host is busy. This branch does not touch
  `boards/de25nano` at all.
- **A race in a test helper** (issue #1830).
  `test_quartus_x87_spike`'s `state()` catches only `FileNotFoundError`, but
  reading `/proc/<pid>/stat` for a just-reaped task raises `ProcessLookupError`
  (ESRCH, errno 3), and the two classes are unrelated. So
  `test_stop_kills_a_child_that_ignores_term_after_the_leader_exits` errors on
  the outcome it is asserting: the child was killed, and the throw comes from
  the loop waiting to observe it. The window is narrow — the test passed 5/5 in
  isolation at loadavg 28 — so the traceback is better evidence than a repro.
  The helper arrived in #1786; this branch does not touch the file. Not fixed
  here, per one scope per PR.

The third is in CI rather than a local sweep, and it is latent on main.

- **`de25-combined-proof.json` is stale on main** (issue #1842). This PR's
  `fpga-producer` job fails `test_combined_client_proof_receipt_matches_captured_sources`
  and nothing else, 1 failure in 2203 tests. The fixture records a sha256 per
  captured source; the key sets still match exactly, 61 entries each, and five
  hashes differ — `include/libmister-runtime/runtime.h` and
  `src/native/{adv7513,hardware,menu_underflow,video}.hpp`, which are exactly
  the five captured files `ed410a1cb` (#1804) changed without regenerating the
  fixture. Reproduced on `origin/main` alone in a clean detached worktree, with
  no branch of this PR's in play, before attributing it. CI stayed green on main
  because `ed410a1cb` touches only `sources/FogCast/` and
  `sources/libmister-runtime/`, so impact analysis marked `fpga-producer` and
  `fpga-simulation` `completed/skipped` — the lane that owns the test never ran.
  It surfaces on the next PR whose diff does touch `sources/misteross/`, where it
  reads as that PR's regression. This PR touches no `boards/de25nano` or
  `libmister-runtime` file.

  The fixture half is fixed by #1843 against main, which regenerates it by
  re-running the proof rather than editing the five hashes. Verified here before
  relying on it: top-level key sets identical, three fields changed —
  `source_revision` `40c23ed3b` to `ed410a1cb`, the five hashes, and
  `binaries_sha256` for `Vfes_de25_combined_sim` alone while both clients stay
  unchanged — and the `tools` block untouched.

  Those two findings only work together, and the order matters. The unchanged
  `tools` block is what establishes the environment was the same: host
  Verilator 5.032 and `c++` 14.2.0, the same builder image and SDK volume, and
  the pinned qemu found inside the builder image at
  `/usr/bin/qemu-aarch64-static`, sha256 `4f55e2e88dc05dc0…`, matching the
  recorded `qemu_sha256`. Only on a fixed environment does the selective binary
  change mean anything: with the compiler held, exactly one artifact moving —
  the one that compiles those headers, while both clients do not — is something
  a hand-edited fixture could not produce without modelling the build graph.
  Selectivity alone would have been ambiguous, because a different compiler
  could also move one binary and not others.
  With that fixture in place this branch passes `build_test.RecipeTests`, 22
  tests; restored to main's, it fails again. So the red check here clears when
  #1843 lands, with no change to this PR.

  The structural half — the half worth more — is #1846, which makes a change to
  the receipt's captured inputs select the fpga lane that validates them.
  Checked here by reverting only `scripts/affected.py` to main's version in a
  clean worktree at that PR's head: its new test then fails with 51 subTest
  failures, one per captured file that did not select the lane before, and
  passes with the new predicate. So the test is wired to the predicate rather
  than restating it, and it would have caught #1842 before it happened. The two
  PRs are independent — #1846 is cut from `origin/main`, does not contain the
  fixture branch and does not touch the fixture — so they can land in either
  order, though #1843 first avoids turning a latent failure into a visible one
  with no fix available.

  **The mechanism then recurred the same afternoon, which settles which of the
  two PRs matters.** Main `e2a39aebb` (#1831) changed
  `libmister-runtime/src/native/video.hpp`, another captured file, about two
  hours after #1843's receipt was generated. Of the 61 keys in that fresh
  receipt exactly one no longer matched, and it was that file. #1831 touches no
  `sources/misteross/` file either, so `fpga-producer` and `fpga-simulation`
  were `completed/skipped` on main again, and the failure landed on the next PR
  to touch `sources/misteross/` — which this time was #1843 itself. Two
  independent instances from different commits in one afternoon, the second
  arriving while the fix for the first was in review.

  So #1843 regenerates a receipt that the next captured-file change will stale
  again; it is not a fix, it is a turn of the treadmill. #1846 is the fix, and
  not because it stops the receipt going stale — it does not. It makes the
  staleness land on the commit that causes it, at the moment it causes it,
  instead of on a stranger's PR hours later. Both instances would have selected
  the fpga lane under its predicate, so both would have been caught by their own
  author.

  **What #1846 does not close, and it is the same shape.** The capture set is now
  derived from disk in all three places rather than copied, so a new captured
  *file* is picked up everywhere — which is what #1842 was. The capture
  *patterns* are still hand-copied, and now exist three times: `proof.py:66-71`
  produces them, `build_test.py:233-243` checks them, `test_affected.py` routes
  them. A new pattern in the producer reaches neither of the others, and the
  symptom would be a receipt whose inputs the checker does not check and the
  planner does not route. The three agree in result today — 61 entries, identical
  sets — but they have **already drifted in form**: `proof.py` lists
  `zx81-hps/cosim/wire.hpp` both explicitly and under its own
  `zx81-hps/cosim/*.hpp` glob, so its raw list is 62 entries deduping to 61,
  where the checker's is 61 with no duplicate. Harmless, because both sides
  dedupe into dicts, and it is the ordinary first stage of exactly this failure.
  One helper exporting the patterns, imported by all three, ends the family
  rather than reducing it to two.

**The sweep is green once the first two are accounted for.** All 37 commands in
`test_changed.py --plan-only` pass: commands 0–11 at `19443449b`, where the
`test_*.py` lane reports `OK (skipped=86)` over 2202 tests, and the 25 RTL
simulation lanes that the early stop had left unstarted, run directly, 25 of 25
exit 0. The plan at `19443449b` is byte-identical in command set and argv to the
plan that produced those 25 results, so the documentation commits between them
change nothing about what runs.

## Reproducing these runs

Three traps, each of which cost a launch when this campaign's condition 2 was
re-measured against the new base.

- **Filling main's roots is not reproducible across time, and failing silently
  makes the experiment weaker rather than broken.** The populator resolved each
  install through *this PR's* `request_from_environment`, so what it hardlinks
  depends on `bootstrap.sh` at the moment it runs. On 12 Nov that returned the
  recipe-`20685ac5…` installs (Yosys `20740ef0…`, Mistral `ef4a2753…`); after
  `bootstrap.sh` changed it returns the recipe-`75baea6d…` ones (`a17b4c8a…`,
  `33d473e1…`). Re-running it today therefore puts the *PR's* binaries on main's
  side, which holds the tooling constant and tests only the code. That is not
  condition 2 and does not support what this record claims. To reproduce the
  campaign, pin main's side to the older-recipe install per tool rather than
  resolving it.

  **Keep that variant in mind for a failure, though, because it is not merely a
  weaker run.** Three shapes answer three different questions, and only the
  first is condition 2:

  | shape | code | binaries | isolates |
  | --- | --- | --- | --- |
  | the campaign | varies | varies | nothing; the strongest single claim |
  | main's side on the PR's installs | varies | constant | the producer and helper edits |
  | forced BUILD_ID, same sources | constant | varies | the install |

  If a forced RBF ever does differ from main's, re-running the campaign's own
  shape narrows nothing — it only reproduces the difference. The second shape is
  what says whether the cause was the producer and helper edits; the third,
  whether it was the install. Reach for one of those, not for a repeat.
- **Never `chmod` the linked files, and the campaign's own populator did.** The
  files in a filled root are hardlinks into the published cache installs, so a
  `chmod` on the link changes the install's own inode — 444 to 644, 555 to 755.
  The script ran `chmod -R u+w <root>` before `rmtree`, which damaged the
  published modes of every install it had linked (Yosys `7e027803` 333 files,
  Mistral `d6fe2f26` 13, and four nextpnr installs one each). Nothing errors at
  the time. What fails is later, and elsewhere: the PR's own closure check
  rejects the install with

  ```
  shared toolchain verification failed: published yosys install/evidence closure differs from ready manifest
  ```

  That message names the tool but not the cause, and the cause is a file mode
  rather than content, so it reads like a corrupted or half-published install.
  `rmtree` needs write permission on *directories*, which are fresh inodes from
  the link pass, so restricting the `chmod` to `-type d` both works and leaves
  the cache alone.

  Two things follow. The check did its job — it refused to build against
  installs whose published closure no longer matched, which is the behaviour
  this PR adds. And the method as documented is not repeatable: run as written
  it leaves the cache in a state the verification rejects. Repair by taking the
  modes from a healthy sibling install of the same tool; `find
  <cache>/tools -path '*/install/*' -type f -perm -u=w` should return nothing.
- **The producer must not be wrapped in `gpu-run`.** `fes_build_common` raises
  `invoke the producer outside gpu-run; GPU nextpnr calls acquire their own
  lease` when `GPU_RUN_DEVICE` or `GPU_LEASE_FD` is set, because an outer lease
  would be held across synthesis. GPU work still goes through `gpu-run`: each
  nextpnr call takes its own lease, with phase leases where the pinned nextpnr
  supports them. The guard fires on the first GPU-routed call, so a wrapped run
  fails only after synthesis has already completed.
- **`build_fes_menu_package.py` writes to `build/oss/fes-menu-package`**, not
  `build/fes-menu-package` as its siblings do. A runner that globs
  `build/**/build-summary.json` will not notice; one that joins a path will.
- **`config/core-recipes.toml` is no longer one entry per core.** `fes.menu` and
  `fes.zx81` now have two `[[recipes]]` entries each, the second
  `board = "de25-nano"`, added in the window this record spans (38 insertions,
  no deletions). An ad-hoc parser that keys on `core_id` alone merges the pair
  and can report a route input as unchanged when it is not. Filter on
  `toolchain` and `board`, as `scripts/pnr_recipe.py` does — it `continue`s past
  `toolchain == "quartus"` before its own duplicate check, so the registry it
  builds stays unambiguous, and a genuine collision between two OSS entries
  raises `duplicate P&R recipe for <core>` rather than picking one. The hazard
  is in reading the file by hand, not in the repository's reader.

## Why a review reported as absent

`gh pr view --json comments` returns a PR's **issue** comments and silently
omits its reviews and review comments. Codex posts as a review, so this PR read
as unreviewed through seven reviews and eleven findings, and that was reported
twice as "Codex has not posted". The failure direction is the dangerous one: it
hides work rather than inventing it, and nobody goes looking for a review that
reports as absent. Query `gh api repos/<owner>/<repo>/pulls/<n>/reviews` and
`.../pulls/<n>/comments`, or `gh pr checks` for state, and treat a quiet
`comments` listing as no evidence either way.

## Why CI appeared not to run on this branch

Worth its own line, because it cost two hours. For a `pull_request` event GitHub
builds `refs/pull/<N>/merge` first; while the PR conflicts with its base that ref
cannot be created, and **no Actions check suite is created at all**. Querying
`actions/runs?head_sha=…` returns `total_count: 0`, which reads exactly like a
workflow that never fired. It is a conflict symptom, not a workflow bug, and
pushing an empty commit does not fix it. Merging the base does: the run started
as soon as `877dd3360` landed.

## Not covered here

- pc386 is still in progress; its next sealing PR covers the tree.
