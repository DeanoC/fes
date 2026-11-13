# fes.coleco under per-tool installs (#1747)

The registered native-video shell, `build_fes_coleco_socket_v2.py
--native-video-socket`. Measured on Powerboat, 12–13 Nov 2026, against main
`2d895034c`. Coleco is the core whose main tool slot (`slots/9c81e35d…`, Coleco
lock, HIP) still exists and still authenticates with main's code.

**This meets AGENTS.md D12, so fes.coleco needs no reseal or HIL.**

## Builds

| Build | Checkout | Tools | BUILD_ID | Seed | system / pixel / audio MHz |
| --- | --- | --- | --- | --- | --- |
| main | clean `origin/main` `2d895034c` | legacy slot `9c81e35d…` | `7b9f7253673c572b83879c956f23f5cb` | 3 | 54.681 / 87.078 / 173.461 |
| forced | PR `c231a8db7`, run with [`forced_build_id.py`](forced_build_id.py) | per-tool installs | `7b9f7253673c572b83879c956f23f5cb` (main's) | 3 | 54.681 / 87.078 / 173.461 |
| own | PR `c231a8db7`, the producer's own command line | per-tool installs | `a00a95148f47a90f47a67948ec8ff1c2` | 3 | 54.840 / 89.262 / 180.213 |

The constraints are 52.225 / 74.250 / 12.288 MHz.

The PR's tools are the same commits as main's: Yosys `5391eeb1`, nextpnr
`bec82e36` (HIP gfx1100;gfx1201) and Mistral `8fcc4cb4`. Their binaries have new
sha256 values because each one embeds its install prefix:

| Tool | main sha256 | PR sha256 |
| --- | --- | --- |
| Yosys | `1ee1a387…` | `bb149f3f…` |
| nextpnr-mistral | `b8aaf1c7…` | `c5dfa44f…` |
| Mistral | — | `33d473e1…` |

## Against the D12 conditions

1. **Netlist (met).** With main's BUILD_ID forced, the PR's `synth.json` differs
   from main's only in file paths in `src` attributes: the checkout and Yosys's
   own techlib files (`…/install/bin/../share/yosys/abc9_model.v`). The techlib
   paths now name the per-tool install instead of the slot. Normalized, both are
   `bb7fd232…`. That hash was taken at PR `dff87afe8`; the forced build at
   `c231a8db7` produces the same RBF byte for byte (below), so the placed and
   routed design is unchanged.
2. **RBF (met).** The forced build's `core.rbf` is byte-identical to main's:
   `fbc2b284a93f021696621e3b3d8a333871cc026425986cd37d133b89695a7a87`. Re-measured
   at `c231a8db7` against the reprovisioned per-tool installs, so the nextpnr
   binary's own relocation is covered too.
3. **Timing and seal (met).** The own build passes all three clocks. The
   producer's own route, timing, ROM-map, source and tool-identity checks pass,
   and it exported its package
   (`d85d5251dd112903aa577c1324ad2b932ead8623a65740a631f9df7146f438e9`). The
   system clock reached 54.840 MHz against a 52.2248 MHz constraint. The
   producer's placer QoR search chose seed 3 at weight 2000 for this BUILD_ID.
