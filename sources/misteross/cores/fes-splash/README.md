# FES board-firmware splash

This is board firmware, not an `experiments/` place-and-route test and not a
play package. The core lane is [docs/cores.md](../../docs/cores.md).

The bitstream is the U-Boot / Stop-idle splash: a logo plus motion on HDMI so
the board is visibly alive. It is **not** rooms, **not** attract ABI, and
**not** a format-2 play package.

Do not retarget `fes-demo`, `fes-pong`, or sealed MiSTer `menu.rbf` as this
bitstream. The tracked seal is `sealed/fes-splash.rbf`. FES image policy pins
that file as both splash and Stop-idle. Replacing it means rebuilding,
updating `sealed/`, and changing the FES pin. Work on this recipe in FES
`sources/misteross`. Do not open day-to-day PRs against standalone
`DeanoC/misteross`.

## HDMI timing

CTA-770.3 **1280×720p60** at **74.25 MHz**, full-frame (not the 320×240
playfield used by play shells):

| Field | Value |
| --- | --- |
| Pixel clock | 74.25 MHz from the checked 50→74.25 fractional PLL |
| H total / active / front / sync | 1650 / 1280 / 110 / 40 |
| V total / active / front / sync | 750 / 720 / 5 / 5 |
| Sync polarity | positive H and V |
| Blanking | black |

The image is a teal FogCast-style fog disc and ring, block **FES** letters,
a marching underline, a fog band, and an orbiting mote. Phase advances once
per frame.

## MiSTer user-io and core-ID

The bitstream **does not** implement MiSTer user-io.

| Command | Present? |
| --- | --- |
| `0x0014` Probe (core name) | **No** |
| `0x002f` HPS framebuffer | **No** |

There is **no observed core-ID string**. Nothing answers Probe, so this
recipe does not invent `SPLASH` or `FESIDLE`. U-Boot does not Probe.
FES `IdleRecipe` must omit Probe and HPS framebuffer.

HDMI bring-up is **ADV7513 over HPS I2C** at BEL
`cyclonev_hps_interface_peripheral_i2c.52.60.0` (pads U10 / AA4), the same
open-drain bridge play shells use. Linux can program the transmitter without
Menu video bring-up. There is no HPS GP mailbox.

Recorded in `idle-contract.toml`.

## HPS DDR port layout

The SDR controller takes its FPGA-to-SDRAM port layout from the `cfg_*`
inputs of the loaded bitstream's fpga2sdram cell, but only when
`staticcfg.applycfg` is written. U-Boot writes it once, in `bridge enable`
right after it loads this splash as FAT `/idle.rbf`. Nothing on Linux can apply a
new layout safely, so every core that uses the HPS DDR depends on the splash.

The splash therefore instantiates the cell with the `fes.memory.hps-ddr`
layout from `cores/fes-common/generated/fes_application.vh`: a 128-bit port
and two 64-bit Avalon-MM ports (the MiSTer sysmem layout). Its clocks and
command inputs are tied off, so it never issues a command. The recipe rejects
a netlist with other `cfg_*` values or a driven command input.
`make sim-fes-splash` checks the same.

A boot bitstream without the cell latches all-ones values. The SDR controller
then accepts no command from any core, and the RAM tester reports `NACK` on
every DDR port.

## Compiler route and artifacts

Generic **OSS** Yosys / nextpnr-mistral / Mistral (`make toolchain`,
GPU-router **OFF**). Quartus is not required. HIP `--router gpu` is not
used.

```sh
make sim-fes-splash
make build-fes-splash
```

| Output | Path |
| --- | --- |
| RBF (local seal) | `build/fes-splash/core.rbf` |
| Canonical inputs | `build/fes-splash/build-inputs.json` |
| Timing / cells / RBF digest | `build/fes-splash/build-summary.json` |
| FES pin snippet | `build/fes-splash/native-inputs-snippet.toml` |
| **FES-local seal** | **`sealed/fes-splash.rbf`** (+ `sealed/fes-splash.build-summary.json`) |

FES image policy pins **`sources/misteross/sealed/fes-splash.rbf`**, not
`build/` and not a live fetch from standalone `DeanoC/misteross`. The
`build/` tree is gitignored; the tracked `sealed/` copy is the assembly input.

`--synth-only` is a dirty-tree Yosys probe and does not seal. A sealed RBF
requires a clean committed tree and the pinned local OSS tools. The producer
never programs hardware and never writes a format-2 `.fcore`.

Sim writes two 720p frames under `build/sim/fes-splash-frames/` (`frame0.png`,
`frame1.png`) so the mark and motion can be inspected.

## FES slice 4

Pin **`sealed/fes-splash.rbf`** (not `build/fes-splash/core.rbf`) in
`image/build/native-inputs.toml` as `[splash_rbf]` (FAT `/idle.rbf`, FES
U-Boot `core=idle.rbf`) and reuse the same digest as `[idle_rbf]`. IdleRecipe: no Probe, no `0x002f`. See
`sealed/README.md`.
