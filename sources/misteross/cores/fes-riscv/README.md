# fes.riscv

An original RISC-V computer on the shared `fes.application` 1.0 shell with
the fixed 720p60 picture and `fes.gamepad` 1.0. It exists to carry the
[first-party RV32I CPU](../fes-common/rtl/riscv/README.md) through the
Yosys/nextpnr-mistral/Mistral lane onto the DE10-Nano. It is not an emulation
of any historical machine and ships no third-party software.

## System

Everything runs on the 74.25 MHz pixel clock.

| Address | Size | Contents |
| --- | --- | --- |
| `0x0000_0000` | 32 KiB | M10K RAM. The firmware image is loaded by the bitstream; the reset vector is 0 |
| `0x1000_0000` | 19,200 bytes | Framebuffer, 160x120 pixels, one RGB332 byte each, row stride 160 |
| `0x2000_0000` | 40 bytes | I/O registers, ten words |

| I/O register | Access | Meaning |
| --- | --- | --- |
| `+0x00` | R | gamepad buttons (`fes.gamepad` bit layout: Up 1, Down 2, Left 4, Right 8, A 16, B 32, Select 64, Start 128) |
| `+0x04` | R | frame counter, one per 720p frame |
| `+0x08` / `+0x0c` | R | `mtime` low / high: free-running 64-bit pixel-clock counter, also the CPU's `time` CSR |
| `+0x10` / `+0x14` | RW | `mtimecmp` low / high; the timer interrupt is `mtime >= mtimecmp` |
| `+0x18` | RW | vertical-blank flag, set at the end of every frame; any write clears it |
| `+0x1c` | RW | bit 0 enables the external interrupt from the vertical-blank flag |
| `+0x20` | R | identification `0x52495343` ("RISC") |
| `+0x24` | RW | bit 0 raises the software interrupt |

Any other address faults (load/store access fault, or an instruction access
fault when fetched), including the unused words `0x2000_0028`–`0x2000_003c`
and the bytes of the framebuffer window past the 19,200 that exist. Every bus request takes two cycles. The framebuffer is
shown 6x inside the shared 1280x720 raster (the 960x720 playfield of
`fes_video_720p`); the shell uses that module's timing and reads pixels from
the framebuffer's second M10K port four pixel clocks ahead of the output
registers. Pixels outside the playfield are black. There is no audio.

The mailbox execution hold resets the CPU, the timer registers and the
I/O block and restarts the firmware from address 0. RAM and framebuffer
contents survive the hold; the firmware re-initialises what it uses.

## Firmware

`firmware/firmware.S` is hand-written RV32I assembly, assembled by
`firmware/assemble.py` (a small two-pass assembler in this directory; no
external RISC-V toolchain is needed or used) into `firmware.hex` and the four
byte-lane images `firmware.lane0..3.hex` that the RAM lanes load. The images
are checked in; `python3 firmware/assemble.py` regenerates them and
`--check` verifies they match the source. The producer refuses stale images.

The demonstration firmware draws a gradient background, a white border and
the banner `FES RV32I`, then once per frame erases and redraws a 12x12 box
that the gamepad moves one pixel per frame inside the border. A timer
interrupt every 18,562,500 cycles (four per second) advances the box through
eight colours, so the interrupt path is visible on a display. Unexpected
exceptions are counted at `0x7e0c` and skipped.

## Simulation

```sh
make sim-fes-riscv
```

runs the CPU lane (`alu`, `cpu`), `firmware` (the images match the source),
`system` (`fes_riscv_system` with the real firmware: border, gradient, banner
glyphs, box position after gamepad input, the colour change from the timer
interrupt, clamping at the border and the restart after an execution hold,
all read back from the HDMI pixel stream) and `board` (`top.v` through the
mailbox: capabilities `0x3`, release, gamepad input, HDMI pixels and hold).
Simulation targets refuse the shared toolchain cache.

## Seal

From a clean committed `sources/misteross` tree:

```sh
make toolchain-fes
make build-fes-riscv CACHE_ROOT=/absolute/cache
```

`scripts/build_fes_riscv.py` authenticates `toolchain.lock`, synthesises with
`synth_intel_alm -nolutram -nodsp` (M10K is used; MLAB, DSP and the HPS
SDRAM bridge are forbidden), routes with the HIP router through a bounded
first-pass seed search (seeds 1–8, HeAP timing weight 10) that stops at the
first placement closing 74.25 MHz, validates the shared board/PLL/I2C
evidence, and exports a format-2 package. Output is `build/fes-riscv/core.rbf`
with `build-summary.json`, `qor-ranking.json` and
`build/packages/<package-id>/`. Package id `fes.riscv`, version 0.1.0,
required interfaces `fes.video.fixed-720p60` 1.0 and `fes.gamepad` 1.0.

A passing seal is host evidence of a routed, timing-closed bitstream. It is
not hardware acceptance; see the FES [core status](../../../../docs/core-status.md).

## Not implemented

- No M, A, F, D or C extensions; no supervisor or user mode; no PMP.
- No audio, media, firmware upload or persistence interfaces. Programs are
  the checked-in firmware; loading user programs through the mailbox would
  need a media interface and is a separate decision.
- No cache, no DDR and no SDRAM: the 32 KiB RAM is the whole memory.
