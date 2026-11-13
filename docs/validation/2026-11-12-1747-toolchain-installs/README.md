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
  hardlinks of the PR's per-tool installs, which are the same commits as main's
  locks name. These reference BUILD_IDs are main's code with those binaries;
  they are not the BUILD_IDs of main's sealed packages.
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

## Registered OSS producers

**Fifteen cores meet D12 in full.** For every one of them:

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

Two cores do not clear the third condition, and neither failure is caused by
this PR's code:

- **`fes.ts1500`** (issue #1803): forced matches main byte for byte, but the own
  build met no placement (best seed 10, weight 2000, worst ratio 0.9630) across
  all fifteen seed/weight attempts — none was lost to a route timeout. At the
  previous head this same core's own build passed with a 0.01% margin
  (52.230 MHz against a 52.2248 MHz constraint). Its closure is unchanged by
  this PR; only the BUILD_ID moved, and that was enough.
- **`fes.ramtest`** (issue #1803): main's own producer also fails on this host,
  so there is no main reference and no forced build. See below.

## fes.ramtest remains a merge gate

This head has no timing-closing replacement artifact or exact-artifact HIL for
`fes.ramtest`. D12 does not apply because the candidate does not pass its normal
timing and seal gates. The main package remains the existing artifact; none of
the attempts below supplies a qualified replacement. Do not treat this record as
clearing the RAM Tester hardware gate.

| Code | nextpnr | BUILD_ID | Result |
| --- | --- | --- | --- |
| main `2d895034c` | `9128800f` (main lock) | new | no placement met timing, best seed 8, worst ratio 0.9714 |
| this PR | `9128800f` | own | no placement met timing, best seed 7, worst ratio 0.9987 |
| PR #1788 head | `bec82e36` | own | **passed**, seed 7, memory 101.698 / 100 MHz |
| this PR, with #1788 folded in | `bec82e36` | own | no placement met timing, best seed 7, worst ratio 0.9702 |

All eight seeds completed in each run; none was lost to a route timeout. The one
pass had about 1.7% margin, which is inside placer noise, so moving the lock
forward is not a fix. That fold has been reverted out of this PR: #1752 does not
touch `fes.ramtest`, and #1788 stays open on its own. Timing repair, a sealed
replacement and exact-artifact HIL are required before the RAM Tester artifact
is qualified on this head.

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
| `fes.pcxt-dos` | `e162041bf1ed5fbe1a54ff08b3f84e55` | `9db8fb89a0c25940…` | `1385062da52b2d69a32e7d6b281cfbe6` | **fails an audio seal check** |

Both profiles meet the netlist and RBF conditions: the forced builds reproduce
main's RBF byte for byte, and the Yosys netlists are identical for all three
seeds on both profiles once the build path is normalized. (The PC/XT net names
embed the absolute build directory mangled into identifiers, so that
normalization covers `…/build/fes-pcxt-quartus/<build-id>/attempt-<id>/` as well
as the checkout and install paths.)

`fes.pcxt` meets the third condition too. `fes.pcxt-dos` does not: its own build
stops at

```
Error (332000): Audio endpoint binding failed: speaker_output_audio_output_stage_acknowledge: 2
```

This is **not** caused by this PR. Running main's own code at the PR's BUILD_ID
`1385062d…` fails identically, which is the control recorded on issue #1611: the
`audio.sdc` acknowledge-register check is fragile against the BUILD_ID constant,
and any identity change can trip it. No seal or timing check was relaxed.

## fes.coleco

The registered native-video shell. Its record is in
[coleco.md](coleco.md).

## What revision this evidence is against

Everything here was measured against main `2d895034c`, which was the merge base
when the campaign ran. The branch has since merged main `53681975f`, and that
merge touches a closure file of every core in the table — `compiler_read_audit.py`
reaches all fifteen OSS cores, and `cores/fes-common/generated/fes_application.vh`
reaches most of them. So the BUILD_IDs recorded here are not the ones the
post-merge tree produces.

That does not weaken what the campaign demonstrates. The claim under test is
that **relocating a toolchain install changes nothing but file paths**, and that
claim is not a property of any one main revision: at `2d895034c` it held for
seventeen cores, with byte-identical RBFs in every case. Re-running the campaign
after each merge would not test anything further, and main moves faster than a
seventeen-core route campaign completes.

What the merge does mean is that the three non-closing cores above were measured
on the pre-merge tree. Their closures moved, so their placement dice have been
rerolled again — in either direction.

## Not covered here

- pc386 is still in progress; its next sealing PR covers the tree.
