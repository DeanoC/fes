# Shared toolchain.lock bump: Yosys 10ce0a10, nextpnr fdac4c7c

Dated host record, 2026-10-04. Mistral stays at `7ed06e21`.

* Yosys `886afa63` → `10ce0a10` (DeanoC/yosys#21): adds Cyclone V control
  block atom blackboxes only.
* nextpnr `3d4a5b35` → `fdac4c7c`: adds DeanoC/nextpnr#128 (SDRAM DQM fixture,
  origin BEL identity fix) and #121 (control block atoms, QSF device options).

## Method

Base is FES main `f70085c2` (old pins); bump is the same tree with only the lock
and its pins changed. Each worktree built its producer packages, then re-routed
every producer's own `synth.json` with seeds 1–8, keeping the producer's argv
and changing only `--seed` and output paths (HIP router, Powerboat). Ramtest was
rebuilt on the rebased bump (`92933f80` + bump).

## Timing (Fmax MHz, seeds 1–8)

| Core / clock | Constraint | Base median (min) | Bump median (min) | Median change | Bump margin |
| --- | ---: | ---: | ---: | ---: | ---: |
| pong core.game.clk | 74.25 | 85.56 (80.20) | 85.81 (81.00) | +0.3% | 15.6% |
| catch core.game.clk | 74.25 | 127.27 (111.02) | 126.47 (111.17) | −0.6% | 70.3% |
| catch audio.clk | 12.29 | 223.06 | 225.81 | +1.2% | — |
| menu endpoint.clk | 74.25 | 92.50 (86.72) | 90.42 (78.98) | −2.3% | 21.8% |
| demo core.pixel_clk | 74.25 | 226.33 | 252.05 | +11.4% | 239.5% |
| demo-media core.pixel_clk | 74.25 | 119.82 (111.04) | 107.84 (101.14) | **−10.0%** | 45.2% |
| demo-audio core.pixel_clk | 74.25 | 211.96 | 224.24 | +5.8% | 202.0% |
| splash (HIP control) pixel_clk | 74.25 | 207.25 | 207.25 | 0.0% | 179.1% |
| splash (OSS lane) pixel_clk | 74.25 | 207.25 (#501 sweep) | 207.25 | 0.0% | 179.1% |

No seed failed in either set. Against the DeanoC/nextpnr#119 reference
(catch median −9.1%, pong −5.0%, menu −3.6% with at least 15.9% margin), every
shipped core is within bounds. demo-media (not shipped) regresses 10% and is
tracked as a nextpnr QoR issue.

Ramtest (package build, seed 2 / weight 10 / exponent 2):

| | memory /100 | capture /100 | pixel /74.25 | SDROUT |
| --- | ---: | ---: | ---: | ---: |
| main `92933f80` package (#524) | 105.33 | 232.56 | 91.58 | 20 |
| bump | 102.98 | 214.87 | 95.35 | 20 |

## Bitstreams

Every producer bitstream changes (pong `848b0ca2`→`b658027e`, catch
`0a29bcf6`→`401bc33d`, menu `9870a4b8`→`07253130`, ramtest → `ea65dcaa`,
demo/demo-media/demo-audio also differ). The OSS-lane splash reproduces
`sealed/fes-splash.rbf` (`64f1f16d`) bit for bit, so no reseal is needed.

## Hardware

Pending: kit B HIL of a bump image (fes-update, menu, Pong, Catch, ramtest SDRAM
6/6 at 100 MHz, rollback to `24c93192`).
