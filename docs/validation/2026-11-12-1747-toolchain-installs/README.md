# Per-tool toolchain installs (#1747): D12 records

On Powerboat, 12 Nov 2026. This record compares fes.coleco, the registered native-video shell, with main. Coleco is the only core whose main tool slot (`slots/9c81e35d…`, Coleco lock, HIP) still exists and still authenticates with main's code.

**This meets AGENTS.md D12, so fes.coleco needs no reseal or HIL.** D12 counts a toolchain at the same commit as the same toolchain wherever it is installed (Deano, 12 Nov; written into D12 by this PR). Yosys records its own techlib file paths in `src` attributes. Those paths, and the checkout path, are the only netlist differences besides the BUILD_ID constant.

## Builds

| Build | Checkout | Tools | BUILD_ID | Seed | system / pixel / audio MHz |
| --- | --- | --- | --- | --- | --- |
| main | clean `origin/main` `2d895034c` | legacy slot `9c81e35d…` | `7b9f7253673c572b83879c956f23f5cb` | 3 | 54.681 / 87.078 / 173.461 |
| forced | PR `950ad704d`, run with [`forced_build_id.py`](forced_build_id.py) | per-tool installs | `7b9f7253673c572b83879c956f23f5cb` (main's) | 3 | 54.681 / 87.078 / 173.461 |
| own | PR `dff87afe8`, `build_fes_coleco_socket_v2.py --native-video-socket` | per-tool installs | `95e25d52dcee90ddf38fad88768bdc90` | 5 | 52.225 / 96.311 / 222.420 |

The constraints are 52.225 / 74.250 / 12.288 MHz. The commits after `dff87afe8` touch only this directory and the PC/XT producer, so neither is in Coleco's source closure.

The PR's tools are the same commits as main's: Yosys `5391eeb1`, nextpnr `bec82e36` (HIP gfx1100;gfx1201) and Mistral `8fcc4cb4`. Their binaries have new sha256 values because each one embeds its install prefix:

| Tool | main sha256 | PR sha256 |
| --- | --- | --- |
| Yosys | `1ee1a387…` | `4e32a016…` |
| nextpnr | `b8aaf1c7…` | `39d55335…` |

## Against the D12 conditions

1. **Netlist (met).**
   - With main's BUILD_ID, the PR's `synth.json` (`ce6b7aef…`) and main's (`eb970f1f…`) differ only in file paths in `src` attributes: the checkout and Yosys's own techlib files (`…/install/bin/../share/yosys/abc9_model.v`). The techlib paths now name the per-tool install instead of the slot.
   - With those paths normalized, both files are `bb7fd232…`.
   - After the same normalization, the own build's netlist differs from main's only through its BUILD_ID constant: the parameter itself, and the bits and nets that constant drives.
2. **RBF.** The forced build's `core.rbf` is byte-identical to main's: `fbc2b284a93f021696621e3b3d8a333871cc026425986cd37d133b89695a7a87`.
3. **Timing and seal.** The own build passes all three clocks. The producer's own route, timing, ROM-map, source and tool-identity checks pass, and it exported its package. The system clock met 52.225 MHz against a 52.2248 MHz constraint. The producer's placer QoR search chose seed 5 for this BUILD_ID; main's chose seed 3.

## The other registered OSS producers

On Powerboat, 12–13 Nov 2026. Each core had three builds:

- **main:** a clean checkout of main `2d895034c`, running main's producer with its local toolchain roots (`build/toolchain/<root>/install`). Those roots hold hardlinks of the PR's per-tool installs, which are the same commits as the main locks name. So these reference BUILD_IDs are main's code with those binaries; they are not the BUILD_IDs of main's sealed packages.
- **forced:** a clean checkout of the PR at `55ee0b659`, run with [`forced_build_id.py`](forced_build_id.py) and the main build's BUILD_ID.
- **own:** the same PR checkout, running the producer's own command line.

All routes ran through `gpu-run` on HIP.

fes.riscv's closure includes `scripts/doctor.py`, which `1e9d4ad83` edits, so its forced and own builds were rerun at `1e9d4ad83` (row below). No other core's closure includes a file changed after `55ee0b659`.

**Sixteen cores meet D12.** For every one of them:

- the forced `core.rbf` is byte-identical to main's;
- the forced `synth.json` equals main's once the checkout path and the tool install directory are normalized in `src` attributes;
- the own build passes every clock, and the producer's seal checks pass.

| Core | main / forced BUILD_ID | main = forced RBF sha256 | own BUILD_ID | own seed | own timing | own MHz (constraint) |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.pong` | `7f69032f` | `9b79c052284b` | `a6b74840` | 1 | pass | pixel 85.896 (74.250) |
| `fes.menu` | `796c04b7` | `de67d63aa73f` | `0ce60887` | 5 | pass | pixel 92.208 (74.250) |
| `fes.zx81` | `d664831d` | `6422d68ba1db` | `9f276744` | 1 | pass | audio 220.946 (12.288) / pixel 84.803 (74.250) / system 56.287 (52.225) |
| `fes.zx80` | `dd94d35c` | `ec8fe32d7846` | `7db935ca` | 3 | pass | audio 163.026 (12.288) / pixel 86.036 (74.250) / system 56.664 (52.225) |
| `fes.ts1000` | `0ebc5104` | `7ceb7bc7f199` | `ed345e64` | 1 | pass | audio 192.827 (12.288) / pixel 82.082 (74.250) / system 59.432 (52.225) |
| `fes.ts1500` | `9d108ce0` | `a56b85ee6f28` | `e4388077` | 1 | pass | audio 231.160 (12.288) / pixel 77.791 (74.250) / system 52.230 (52.225) |
| `fes.sms` | `d475f84f` | `775c2f2cd808` | `432dc8f6` | 3 | pass | audio 209.644 (12.288) / pixel 99.562 (74.250) / system 52.854 (52.225) |
| `fes.sg1000` | `14df46f8` | `afb1c39f5450` | `2e08c666` | 2 | pass | audio 258.598 (12.288) / pixel 110.375 (74.250) / system 68.404 (52.225) |
| `fes.apple2` | `19e4f628` | `65fa3a599485` | `fe42501e` | 5 | pass | audio 184.536 (12.288) / pixel 93.318 (74.250) / system 55.953 (52.225) |
| `fes.c64` | `e36c6722` | `de523abc4eae` | `1ec1dbc5` | 5 | pass | audio 194.024 (12.288) / pixel 83.132 (74.250) / system 53.801 (52.225) |
| `fes.spectrum` | `3cdcdf9c` | `263ffed3e117` | `4d6e290c` | 5 | pass | audio 182.083 (12.288) / pixel 97.714 (74.250) / system 69.464 (52.225) |
| `fes.spectrum128` | `fca07479` | `a6762b063ebf` | `f09163e9` | 5 | pass | audio 180.018 (12.288) / pixel 95.402 (74.250) / system 64.292 (52.225) |
| `fes.spectrum-plus3` | `69603f6b` | `7b6b76aa4a11` | `db543d3c` | 4 | pass | audio 199.283 (12.288) / pixel 88.215 (74.250) / system 54.183 (52.225) |
| `fes.pentagon` | `aa5e405b` | `38a105912281` | `f4837144` | 5 | pass | audio 215.146 (12.288) / pixel 84.739 (74.250) / system 58.792 (52.225) |
| `fes.riscv` | `bb19551b` | `b52dd2ee1dbf` | `490b78a3` | 1 | pass | pixel 78.493 (74.250) |
| `fes.atari-st` | `c4e94b20` | `9b8090fe7371` | `14a4878c` | 5 | pass | audio 164.908 (12.288) / pixel 87.002 (74.250) / system 56.980 (52.225) |

## fes.ramtest remains a merge gate

This head has no timing-closing replacement artifact or exact-artifact HIL for
`fes.ramtest`. D12 does not apply because the candidate does not pass its normal
timing and seal gates. The main package remains the existing artifact; neither
attempt below supplies a qualified replacement. Do not treat this record as
clearing the RAM Tester hardware gate.

- **main code, new BUILD_ID:** no placement met timing (best seed 8, worst ratio 0.9714).
- **PR, own BUILD_ID:** no placement met timing (best seed 7, worst ratio 0.9987).

Both builds tried every seed in the recipe. Main's sealed package closed only
with its own BUILD_ID. A BUILD_ID change alone is enough to break it, and this
PR moves every BUILD_ID. Timing repair, a sealed replacement and exact-artifact
HIL are required before the RAM Tester artifact is qualified on this head.

## Not covered here

- `fes.pcxt` and `fes.pcxt-dos` seal on Quartus. Their lock retains the main
  Yosys digest (`d45b5d6e…`); the relocated per-tool Yosys binary is not
  qualified by this record. Using a different digest needs fresh Quartus timing
  and seal checks plus exact-artifact HIL.
- pc386 is still in progress; its next sealing PR covers the tree.
