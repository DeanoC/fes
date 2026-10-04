# Shared toolchain.lock bump: rebuilds, seed sweep, sealed splash (2026-10-04)

DeanoC/fes#501 moves `sources/misteross/toolchain.lock` to the DeanoC heads:

| Tool | Old pin (main `7953bf0f`) | New pin (`22749b1c`) |
| --- | --- | --- |
| Yosys | `fb879d81e0352f558297bdcc61bc7a4a922fa7b0` | `886afa63953e97407153e9f4aae25fcedb639696` |
| nextpnr | `a93fe013af841214ecb4f7be3af0de65f3de3a0f` | `3d4a5b352b4edb478b744b82cc61333353751a80` |
| Mistral | `7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039` | unchanged |

Host: Powerboat, under `flock ~/tmp/fes-image-build.lock`. Device: `5CSEBA6U23I7`.
The HIP lane uses GPU router HIP (gfx1100;gfx1201). The OSS splash lane runs with
`gpu-router=OFF`. Each pin bootstrapped its own lock with `make toolchain-fes`
(new: also the OSS lane for `make build-fes-splash`).

## Producer rebuilds (every consumer of toolchain.lock)

All producers exit 0 and pass timing. The values are producer receipts from each
pin's own worktree (seed 1). Old splash is the sealed receipt.

| Core | Producer | Old MHz | Old rbf sha256 / size | New MHz | New rbf sha256 / size |
| --- | --- | --- | --- | --- | --- |
| fes.pong | `make build-fes-pong` | pixel 84.62 | `cd1def2cbf4f…` / 1975241 | pixel 84.11 | `2d68abf0c6d5…` / 1974343 |
| fes.catch | `scripts/build_fes_catch.py` | pixel 132.77, audio 193.76 | `5e693a0c7832…` / 1965615 | pixel 118.44, audio 217.11 | `63d2c82b9d76…` / 1966168 |
| demo | `make build-fes-demo` | pixel 233.26 | `4aa1c8dbdf3a…` / 1959261 | pixel 253.94 | `275730ed5e5a…` / 1959563 |
| demo-media | `make build-fes-demo-media` | pixel 116.27 | `f08b67a6e4c5…` / 1964171 | pixel 104.29 | `50ec75929bd2…` / 1963908 |
| demo-audio | `make build-fes-demo-audio` | pixel 224.52, audio 171.56 | `a9454781f2e1…` / 1960545 | pixel 216.87, audio 191.79 | `2a14b2b0c405…` / 1960639 |
| fes-splash (sealed) | `make build-fes-splash` (OSS) | pixel 204.58 (sealed `808bd7c2`) | `43dc7e9db350…` / 1963100 | pixel 236.57 | `64f1f16daedb…` / 1963321 |

Ramtest and menu move onto this lock in the stacked DeanoC/fes#502, which has its own record.

## Seed sweep (seeds 1–8, both pins)

Method: each pin's producer synthesised the core. Then each `synth.json` was re-routed
with the producer's own nextpnr argv; only `--seed` and the output paths change, and the
argv is identical between the pins apart from the tool slot. `splash-hip` re-synthesises
the splash with the pin's HIP-slot tools, with the router default (it is the
like-for-like old-vs-new splash control). Its old seed 1 reproduces the sealed
204.58 MHz exactly.

| Core | Clock | Constraint MHz | Old seeds 1–8 (MHz) | New seeds 1–8 (MHz) | Old min/med/max | New min/med/max | Δ median | Pass |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| pong | `core.game.clk` | 74.25 | 84.62 / 86.17 / 87.05 / 87.90 / 85.85 / 88.18 / 82.43 / 86.48 | 78.09 / 75.04 / 86.10 / 81.69 / 88.14 / 82.30 / 81.24 / 84.35 | 82.43 / 86.33 / 88.18 | 75.04 / 82.00 / 88.14 | -5.0% | 8/8 → 8/8 |
| catch | `audio.clk` | 12.29 | 193.76 / 222.97 / 213.81 / 203.62 / 208.68 / 184.26 / 214.00 / 199.76 | 217.11 / 209.78 / 215.52 / 219.54 / 208.25 / 221.14 / 211.55 / 229.57 | 184.26 / 206.15 / 222.97 | 208.25 / 216.31 / 229.57 | +4.9% | 8/8 → 8/8 |
| catch | `core.game.clk` | 74.25 | 132.77 / 138.64 / 143.47 / 132.17 / 129.35 / 125.68 / 126.95 / 134.54 | 118.44 / 120.12 / 123.21 / 123.37 / 120.12 / 121.14 / 120.74 / 119.90 | 125.68 / 132.47 / 143.47 | 118.44 / 120.43 / 123.37 | -9.1% | 8/8 → 8/8 |
| demo | `core.pixel_clk` | 74.25 | 233.26 / 227.69 / 221.29 / 196.43 / 221.98 / 215.15 / 248.02 / 263.64 | 253.94 / 226.45 / 264.97 / 237.76 / 265.46 / 261.51 / 224.52 / 235.13 | 196.43 / 224.83 / 263.64 | 224.52 / 245.85 / 265.46 | +9.3% | 8/8 → 8/8 |
| demo-media | `core.pixel_clk` | 74.25 | 116.27 / 110.89 / 111.31 / 100.44 / 104.09 / 104.69 / 114.40 / 115.33 | 104.29 / 129.27 / 107.65 / 115.90 / 104.46 / 121.40 / 107.22 / 118.81 | 100.44 / 111.10 / 116.27 | 104.29 / 111.78 / 129.27 | +0.6% | 8/8 → 8/8 |
| demo-audio | `audio.clk` | 12.29 | 171.56 / 178.09 / 158.58 / 165.78 / 186.19 / 146.91 / 170.30 / 170.07 | 191.79 / 195.92 / 167.25 / 185.08 / 172.86 / 157.88 / 172.44 / 178.95 | 146.91 / 170.19 / 186.19 | 157.88 / 175.91 / 195.92 | +3.4% | 8/8 → 8/8 |
| demo-audio | `core.pixel_clk` | 74.25 | 224.52 / 219.88 / 208.03 / 222.72 / 219.68 / 231.16 / 246.49 / 230.15 | 216.87 / 211.06 / 241.90 / 236.63 / 213.31 / 237.64 / 211.64 / 212.77 | 208.03 / 223.62 / 246.49 | 211.06 / 215.09 / 241.90 | -3.8% | 8/8 → 8/8 |
| splash-hip | `pixel_clk` | 74.25 | 204.58 / 219.49 / 211.64 / 155.93 / 193.54 / 201.17 / 162.18 / 201.82 | 236.57 / 197.86 / 185.15 / 180.41 / 228.10 / 216.64 / 177.09 / 218.82 | 155.93 / 201.50 / 219.49 | 177.09 / 207.25 / 236.57 | +2.9% | 8/8 → 8/8 |

All 96 routes exit 0 and pass every constraint.

**Regressions, filed as DeanoC/nextpnr#119:**
- `fes.catch` `core.game.clk`: median −9.1%; every new seed is below the old minimum. Logic depth is unchanged; route delay is up about 1 ns on the critical path.
- `fes.pong` `core.game.clk`: median −5.0%; worst new seed is 75.04 MHz (1.1% margin).
- demo-audio pixel: −3.8% (large margin).

Pong's producer seed-1 route on the new pin gave 84.11 MHz; re-routing the same
`synth.json` and seed gave 78.09. Old-pin re-routes are repeatable. This is noted in #119.

## Sealed splash

`sources/misteross/sealed/fes-splash.rbf` and `fes-splash.build-summary.json` are
replaced with the new `make build-fes-splash` output from FES `22749b1c`:
- BUILD_ID `217e6df536d8aa8c0ad243f6f21dd043`, seed 1, pixel 236.57 MHz.
- sha256 `64f1f16daedba4ed19179bd5d7c6f3eb3058a688d15441c8c8d89e113060f182`, 1963321 bytes.
- All 11 recorded inputs hash-match the tracked tree.

`image/build/native-inputs.toml` `[splash_rbf]` and `[idle_rbf]` and the seal README pin these bytes.

## Not validated here: kit B image check required

There was no hardware run in this lane. Before release, an image built from this branch needs
a kit B check of the new splash/idle RBF (boot splash, Stop idle) and pong. Package
identities churn for every core on the next image build, because `fes_build_common.py`
is in every image package's input closure.
