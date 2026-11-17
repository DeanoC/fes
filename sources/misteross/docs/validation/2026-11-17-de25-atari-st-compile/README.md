# DE25 standard Atari ST: first Quartus compile

2026-11-17, Powerboat, Quartus Prime Pro 26.3.0 Build 114. Source `5dabd2294`:

```sh
python3 boards/de25nano/atari-st/build.py build \
  --vendor-archive /data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --quartus-root /data/altera_pro/26.3 --output /data/claude-de25-atari-st-5dabd2294
```

This is a diagnostic compile of the board integration. It is not a package:
there was no reviewed input/output reference, no repeat-RBF identity and no
export, and nothing was programmed. Evidence class: host-only.

## Result

Generation, synthesis, fit, timing analysis, assembly and the HPS-first Linux
split all completed. The design fits. Timing is **not** met: three paths miss
by at most 0.114 ns, all under one provisional constraint (below).

| | |
| --- | --- |
| `golden_top.sof` SHA-256 | `ebc6c5a6be7bd842f321ffb999c82d78cca97021091dcb681fcfbb6868a22f51` |
| `linux-boot.core.rbf` SHA-256 | `da7fd7e482565a76d30105919871a17d787ccdcdbd2f5876b73738fc3d142ae7` |
| Logic (ALMs) | 15,138 / 46,800 (32%) |
| Registers | 22,484 |
| Block memory bits | 5,210,056 / 7,331,840 (71%) |
| RAM blocks (M20K) | 306 / 358 (85%) |
| PLLs | 4 / 11 |

The whole managed shell is in those totals, including the GHRD's 256 KiB
on-chip memory (about 105 blocks).

| Clock | Required | Fmax (worst corner) |
| --- | --- | --- |
| Machine (`machine_pll_outclk0`) | 52.222222 MHz | 128.02 MHz |
| Pixel | 74.25 MHz | 135.89 MHz |
| Peripheral 100 MHz | 100 MHz | 166.09 MHz |

Paths within each clock have wide margin. Of the 117 analyses in
[`golden_top.sta.summary`](golden_top.sta.summary), one is negative: setup on
the machine clock in the slow 0C corner, slack -0.114 ns, TNS -0.159 ns.

## The failing paths

All three launch from `core|video|job_start_ok[1]` in the pixel clock and end
at `core|video|legacy_req` or `core|video|memory_state.MEMORY_READ` in the
machine clock ([`sta-excerpt.txt`](sta-excerpt.txt)). Their requirement is the
10 ns of the provisional machine/pixel bound in `atari-st-clocks.sdc`, not a
clock period. `st_video_adapter` holds `job_start_ok` across a request/done
round trip and its comment says the system side reads it two system edges
after the request toggle synchronizes. The bound was a stand-in for naming
each of the adapter's crossings; this result says that stand-in is too crude
for this bundle, which feeds logic before its capture registers. The constraint
was not loosened to make the build pass. Naming each crossing with its real
hold window is owed, and until then this compile does not close timing.

## What this does not show

- 16 input ports and 42 output ports have no external delay, as on the
  Spectrum compile: the SDRAM and I2S pins. SDRAM pad timing is unqualified.
- No producer review gate ran. One compile, one seed. No hardware.
- ROM read timing against the DE10 machine, and a stock TOS boot.

## Found on the way

- The 3090 box (23 GB) cannot synthesize the ST as the DE10 wrote its video
  frame store: Quartus built it from 2.2 million registers (1737% of the
  device) and `quartus_syn` reached 41 GB on Powerboat.
- With that store in block RAM the ST needed 495 M20K blocks of 358. The ROM
  moved to SDRAM and each frame bank became one memory.
