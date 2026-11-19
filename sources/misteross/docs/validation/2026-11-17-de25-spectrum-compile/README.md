# DE25 standard ZX Spectrum: first Quartus compile

2026-11-17, `fes-build-3090` (chip-30x0-ai-dev), Quartus Prime Pro 26.3.0
Build 114. Source `89191e47e`, 48K model:

```sh
python3 boards/de25nano/spectrum/build.py build \
  --vendor-archive /data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --quartus-root /data/altera_pro/26.3 --output /data/de25-spectrum-89191e47e
```

This is a diagnostic compile of the board integration. It is not a package:
there was no reviewed input/output reference, no repeat-RBF identity and no
export, and nothing was programmed. Evidence class: host-only.

## Result

Generation, synthesis, fit, timing analysis, assembly and the HPS-first Linux
split all completed.

| | |
| --- | --- |
| Build ID (integration source profile) | `inputs.json` `standard_zx81.build_id` in the retained output |
| `golden_top.sof` SHA-256 | `565aa650bc8f9c01a7028d901baa62851e2e826b0bd1da609bd5148674ef6c81` |
| `linux-boot.core.rbf` SHA-256 | `831e43ad8db7c51eb2657ba53c696a00ae223dd29418b6b623646c0373ada992` |
| Logic (ALMs) | 10,281 / 46,800 (22%) |
| Registers | 14,938 |
| Block memory bits | 3,484,672 / 7,331,840 (48%) |
| RAM blocks (M20K) | 211 / 358 (59%) |
| PLLs | 4 / 11 |

The whole managed shell is in those totals, not only the machine.

Timing: every one of the 119 analyses in [`golden_top.sta.summary`](golden_top.sta.summary)
has non-negative slack and zero TNS, in all corners.

| Clock | Required | Fmax (slow 0C) | Worst setup slack |
| --- | --- | --- | --- |
| Machine (`machine_pll_outclk0`) | 52.222222 MHz | 139.0 MHz | 2.836 ns |
| Pixel | 74.25 MHz | 147.6 MHz | 1.691 ns |
| Peripheral 100 MHz | 100 MHz | 173.58 MHz | 4.239 ns |

The worst setup slacks include the held crossing bundles, which are bounded
to 10 ns rather than a clock period. Worst hold slack is 0.000 ns on the
peripheral clock and 0.001 ns on the machine clock.

## What this does not show

- The analyzer reports the design as not fully constrained: 16 input ports
  and 43 output ports have no external delay
  ([`sta-excerpt.txt`](sta-excerpt.txt)). The managed ZX81 reference has 30
  such outputs. The additions here are the SDRAM pins and the I2S data and
  clock outputs. `spectrum-clocks.sdc` declares the inverted SDRAM clock and
  deliberately guesses no board delays, so SDRAM pad timing is unqualified.
- No fitted input/output review, asynchronous-input review or audio-clock
  review ran; those are the producer's gates and are keyed to the ZX81 today.
- One compile, one seed. No hardware.

## Clocking found on the way

Three arrangements failed in the fitter before this one. `CLOCK1_50` cannot
feed a second PLL beside the stock audio PLL; a cascade from the 100 MHz
system PLL needs that vendor PLL reconfigured as a cascade source. The build
therefore uses one PLL on `CLOCK1_50` for both the machine clock and the audio
master clock (12.287582 MHz), in place of the stock audio PLL. Both are
-34 ppm and in the DE10 cores' 17:4 ratio.
