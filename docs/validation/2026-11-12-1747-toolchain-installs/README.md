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
this PR: #1752 does not touch `fes.ramtest`, and #1788 stays open on its own.

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

**The gap, stated as a gap.** D12's second condition is that a forced-BUILD_ID
build reproduces main's `core.rbf` byte for byte. Every forced build here did
that against `2d895034c`. **None of it has been verified at this head**, because
the merge touches a closure file of every core in the table —
`scripts/compiler_read_audit.py` reaches all fifteen OSS cores, and
`cores/fes-common/generated/fes_application.vh` reaches most of them — so the
BUILD_IDs recorded here are not the ones the post-merge tree produces. This
record does not claim that condition is met at `02beac79b`, and it is not
claiming the condition does not apply. Whether the campaign must be rerun
against the new base before this PR merges is an open call, not something this
record settles.

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

## Two sweep failures this branch did not cause

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

**The sweep is green once those two are accounted for.** All 37 commands in
`test_changed.py --plan-only` pass: commands 0–11 at `19443449b`, where the
`test_*.py` lane reports `OK (skipped=86)` over 2202 tests, and the 25 RTL
simulation lanes that the early stop had left unstarted, run directly, 25 of 25
exit 0. The plan at `19443449b` is byte-identical in command set and argv to the
plan that produced those 25 results, so the documentation commits between them
change nothing about what runs.

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
