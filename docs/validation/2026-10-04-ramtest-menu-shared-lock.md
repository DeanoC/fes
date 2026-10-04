# RAM test and menu on the shared toolchain.lock: rebuilds and timing (2026-10-04)

> **Split after kit B HIL (2026-10-04 14:00 EEST).** The rebuilt ramtest-100 package
> `01948465` (nextpnr `3d4a5b35`) **fails SDRAM on hardware**: about 0x01F9xxxx errors per
> pattern, reading `FFFF`, on 2 runs. The old `655f3833` package `3593d7e0` passed 6/6 on the
> same boot (https://github.com/DeanoC/fes/pull/502#issuecomment-5979280762). Timing passed
> on both, but the SDRAM pad paths are not constrained in the SDC, so the timing pass does
> not cover them. This is tracked as DeanoC/nextpnr#135. As a result, #502 now moves **only
> `fes.menu`** onto `toolchain.lock`. `fes.ramtest` stays on `toolchains/ramtest.lock`
> (nextpnr `655f3833`) with inputs unchanged from main. The ramtest rows below are the
> evidence behind that decision, not a shipped change. The menu input closure is unchanged
> from the `dfc3fb4d` build timed here.

DeanoC/fes#502 originally moved (stacked on #501) `fes.ramtest` and `fes.menu` off
`toolchains/ramtest.lock` and onto `sources/misteross/toolchain.lock`.

| Tool | Old (`ramtest.lock`, main `7953bf0f`) | New (`toolchain.lock`, `dfc3fb4d`) |
| --- | --- | --- |
| Yosys | `886afa63953e97407153e9f4aae25fcedb639696` | unchanged |
| nextpnr | `655f38334b8a1ba798cc05cf3744b6a897119b5d` | `3d4a5b352b4edb478b744b82cc61333353751a80` |
| Mistral | `7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039` | unchanged |

Only nextpnr changes. Synthesis is the same apart from the BUILD_ID constant
(ramtest-100 COMB is 6631 old and 6628 new; FF is 8207 in both).

Build setup:
- Host: Powerboat, under `flock ~/tmp/fes-image-build.lock`.
- Device: `5CSEBA6U23I7`.
- GPU router: HIP (gfx1100;gfx1201), `--gpu-device 0`.
- Each pin ran `make toolchain-fes-ramtest` and then its own producers.

## Producer rebuilds

| Build | Seed order | Old result | Old rbf | New result | New rbf |
| --- | --- | --- | --- | --- | --- |
| `make build-fes-ramtest-100` | 2,6,1,3,4,5,7,8, first pass | seed 2 PASS: memory 103.57, pixel 85.48, capture 284.74 MHz | `d3d6529b75644ba7…` / 2198440 | seed 2 PASS: memory 102.35, pixel 94.80, capture 276.70 MHz | `132037dfee1678a7…` / 2173050 |
| `make build-fes-menu-package` | 5, then 1–4, 6–8, first pass | seed 5 PASS: `endpoint.clk` 88.39 MHz | `358462c9fe8be032…` / 2012679 | seed 5 PASS: `endpoint.clk` 92.39 MHz | `6a12077bb065a8af…` / 2016227 |
| `make build-fes-ramtest-130` | 2,6,1,… | not rerun (see below) | — | seed 2: memory 104.08, pixel 96.96. Seed 6: memory 109.69, pixel 93.40. Memory misses 130.01. Seed 1 stopped in repair at −1.72 ns | — |

Constraints:
- ramtest-100: memory 100, pixel 74.25, capture 100 MHz.
- ramtest-130: memory and capture 130.01 MHz.
- menu: 74.25 MHz.

RAM test 130 MHz is a known OSS timing miss on the old pin as well. In
`sources/misteross/docs/validation/2026-09-27-ramtest-timing.md`, nextpnr
`655f3833` seed 2 reached memory 101.94 and pixel 69.68 MHz, missing both.
To keep the sweep bounded, the new 130 search was stopped after seeds 2 and 6 and
part of seed 1, and the old 130 search was not repeated. At the one seed measured on both pins (seed 2), the new pin is no worse: memory is
104.08 against 101.94 MHz, and pixel now passes at 96.96 MHz. The old figure comes from an earlier
source revision, so it is indicative only. The image
recipe ships the 100 MHz package.

## Menu seed sweep (seeds 1–8, both pins)

Each pin's menu producer ran synthesis. Its `synth.json` was then re-routed with
the producer's nextpnr argv (`--placer-heap-timingweight 10 --placer-heap-critexp 2
--router gpu`); only `--seed` and the output paths change. Seed 5 reproduces both
producer results exactly.

| Core | Clock | Constraint MHz | Old (655f3833) seeds 1–8 (MHz) | New (3d4a5b35) seeds 1–8 (MHz) | Old min/med/max | New min/med/max | Δ median | Pass |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| menu | `endpoint.clk` | 74.25 | 94.75 / 104.07 / 89.50 / 96.49 / 88.39 / 90.16 / 89.90 / 94.54 | 89.77 / 96.56 / 93.75 / 86.06 / 92.39 / 86.50 / 86.75 / 88.36 | 88.39 / 92.35 / 104.07 | 86.06 / 89.06 / 96.56 | -3.6% | 8/8 → 8/8 |

The menu median is −3.6% (max −7.2%). Every seed keeps at least 15.9% margin. This is
recorded on DeanoC/nextpnr#119 alongside the #501 catch/pong regressions.
RAM test ramtest-100 at seed 2: pixel +10.9%, memory −1.2%, capture −2.8%.
A full RAM test seed sweep (about 10–20 minutes per route) was not run, to keep
this bounded.

## Hardware status

The kit B check of the combined #501/#502 image is described above: menu renders, and
ramtest SDRAM fails at `3d4a5b35`. After the split, the remaining check is the
menu-only kit B check of a split-#502 image. It is still pending, scheduled after #364.
The menu package identity changes. The ramtest identity is unchanged from main.
