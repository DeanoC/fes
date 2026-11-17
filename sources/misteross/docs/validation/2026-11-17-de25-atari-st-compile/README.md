# DE25 standard Atari ST: first Quartus compiles

2026-11-17, Powerboat, Quartus Prime Pro 26.3.0 Build 114. The retained
summaries are from source `86c3b2b0f`:

```sh
python3 boards/de25nano/atari-st/build.py build \
  --vendor-archive /data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --quartus-root /data/altera_pro/26.3 --output /data/claude-de25-atari-st-86c3b2b0f
```

These are diagnostic compiles of the board integration. They are not a
package: there was no reviewed input/output reference, no repeat-RBF identity
and no export, and nothing was programmed. Evidence class: host-only.

## Result

Generation, synthesis, fit, timing analysis, assembly and the HPS-first Linux
split all completed. The design fits and every one of the 58 analyses in
[`golden_top.sta.summary`](golden_top.sta.summary) has non-negative slack.

| | |
| --- | --- |
| `golden_top.sof` SHA-256 | `d1541b55e17d09adad6f60f7ed5343b52177053965a37bdd28cf0d34b1fbae69` |
| `linux-boot.core.rbf` SHA-256 | `f54850723e7e5090e089e35cd9af1bdf7b0dfc85d23dd1bd5d340c0edceb11c6` |
| Logic (ALMs) | 15,124 / 46,800 (32%) |
| Registers | 22,584 |
| Block memory bits | 5,210,056 / 7,331,840 (71%) |
| RAM blocks (M20K) | 306 / 358 (85%) |
| PLLs | 4 / 11 |

The whole managed shell is in those totals, including the GHRD's 256 KiB
on-chip memory (about 105 blocks).

| Clock | Required | Fmax (worst corner) | Worst setup slack |
| --- | --- | --- | --- |
| Machine (`machine_pll_outclk0`) | 52.222222 MHz | 131.86 MHz | 2.206 ns |
| Pixel | 74.25 MHz | 147.1 MHz | 1.818 ns |
| Peripheral 100 MHz | 100 MHz | 162.21 MHz | 3.835 ns |

The worst setup slacks are on the bounded crossing bundles, not on paths
within a clock. Worst hold slack is 0.000 ns.

## How the constraints got here

Four compiles, the last one clean. The path within each clock always had wide
margin; every miss was a bundle held across the machine/pixel boundary.

| Source | Crossing constraint | Result |
| --- | --- | --- |
| `5dabd2294` | One provisional 10 ns bound for the whole clock pair | -0.114 ns: `job_start_ok` into the machine clock |
| `75562d373` | Every crossing named; bundles at 10 ns, `job_start_ok` at 11 ns | -0.841 ns: publication metadata into the border pointers |
| (re-timing only) | Publication bundle at one pixel period | -0.595 ns: configuration bundle into the pixel clock |
| `86c3b2b0f` | Every bundle into the pixel clock at one pixel period | clean |

The inventory of crossings was taken from the fitted design: 34 groups of
register-to-register paths, all in `st_video_adapter` and the native capture.
The bounds come from the handshakes, written out beside each constraint in
`atari-st-clocks.sdc`: a bundle into the pixel clock is read at least two
pixel periods (26.94 ns) after it is launched and is bounded at one; and
`job_start_ok` has 11.36 ns and is bounded at 11. The bound counts the skew
between the two PLL networks, which the 10 ns used for same-source bundles
elsewhere does not allow for. These numbers were moved three times in one
session. Each move was derived, not fitted, but they want a second reader.

## What this does not show

- 16 input ports and 42 output ports have no external delay
  ([`sta-excerpt.txt`](sta-excerpt.txt)), as on the Spectrum compile: the
  SDRAM and I2S pins. SDRAM pad timing is unqualified.
- No producer review gate ran. One seed. No hardware.
- ROM read timing against the DE10 machine, and a stock TOS boot.

## Found on the way

- The 3090 box (23 GB) cannot synthesize the ST as the DE10 wrote its video
  frame store: Quartus built it from 2.2 million registers (1737% of the
  device) and `quartus_syn` reached 41 GB on Powerboat.
- With that store in block RAM the ST needed 495 M20K blocks of 358. The ROM
  moved to SDRAM and each frame bank became one memory.
