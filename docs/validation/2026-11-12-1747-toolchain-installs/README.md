# Per-tool toolchain installs (#1747): D12 for fes.coleco

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

Every other registered producer has no main reference build here (see the PR).
