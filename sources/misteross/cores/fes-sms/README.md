# FES Master System fixed-map slice

This directory is a described FES core, not an `experiments/` place-and-route
test. The core lane is [docs/cores.md](../../docs/cores.md).

This directory is the next FES emulator bring-up after SG-1000. It is a
bounded Master System-compatible console slice that uses the existing
`fes.simple-computer` 1.0 mailbox and the DE10-Nano fixed 720p shell.

The package id is `fes.sms` (FogCast `protocol.SystemSMS = "sms"` / library
SMS). Do not use `fes.mastersystem`.

Master System is a Coleco / SG-1000 sibling, not a second console stack.
TV80, the legacy TMS9918-style VDP, dual-port RAM wrappers, the GP mailbox and
both PLL wrappers remain Coleco modules. This tree supplies the SMS memory map,
Mode 4 VDP and six-bit video shell, the SN76489 on ports `0x7E`/`0x7F`, HDMI
I2S into the ADV7513, the 8255 joystick ports, VDP-to-INT wiring, the board
top, Quartus pins and the oracle recipe.

This package does not copy the MiSTer framework and does not claim retail-game
compatibility. The Quartus 17.0.2 recipe is the compiler/oracle lane.
`make build-fes-sms` is the OSS Yosys/nextpnr-mistral producer using the
Coleco compatibility lock (Yosys `e2d425de`, nextpnr `5dea3ecd`). `fes.sms`
is registered for package-only parent builds and is not in the factory image.

## Implemented slice

- Verilog TV80 Z80-compatible CPU, clock-enabled from the 52 MHz FES system
  domain (Coleco `t80pa` / `tv80`).
- The OSS package seals a blank 32 KiB ROM map at `0x0000–0x7fff`.
  Library launch selects `cartridge-rom` as an exact 32 KiB binary; the target
  patches its bytes into the RBF before FPGA download. Pad shorter fixed-map
  images with `0xff` before import. There is no BIOS, reset shim or mapper;
  reset fetches the linked cartridge. The Quartus oracle and the default
  mailbox simulation remain explicit media-transport diagnostics.
- 8 KiB CPU RAM at `0xc000–0xdfff`, mirrored at `0xe000–0xffff`.
- VDP ports `0xbe` / `0xbf` with shared TMS Graphics I, Graphics II, Text and
  Multicolor modes on the 256×192 logical raster, plus separate SMS Mode 4.
  Unsupported TMS selectors show the R7 backdrop; Text suppresses sprites and
  Multicolor keeps them active. Mode 4 has 16 KiB VRAM, 32-entry six-bit CRAM,
  32×28 tilemaps, tile priority/palette and
  horizontal/vertical flip, horizontal/vertical scrolling and lock bits, the
  left-column mask, 8×8/8×16 zoomable sprites, eight-sprite overflow, sprite
  collision, line interrupts and VBlank interrupts on the fixed 256×192 logical
  raster. The TMS fallback uses a fractional enable for nominal 60 Hz across
  262 lines. SMS Mode 4 keeps its existing slower raster enable because its
  serial scanline renderer cannot complete a line at 60 Hz. Composite sync,
  half-line behavior, PAL timing and cycle-perfect raster effects remain outside
  this slice. VDP IRQ drives Z80 INT
  (maskable). Pause NMI is unused.
- Two joysticks on the SMS 8255 ports `0xdc` / `0xdd`, adapted from the
  existing 40-bit keyboard matrix.
- Centered 512×384 logical image in the established 1650×750 HDMI timing, with
  the SMS two-bit-per-channel CRAM expanded to 24-bit HDMI RGB.
- SN76489-compatible PSG on I/O `0x7E`/`0x7F` (write-only; both ports). Tone,
  noise and 2 dB attenuation follow the Sega PSG latch/data protocol. The mix
  is a signed 16-bit sample in the 52 MHz domain.
- FPGA→ADV7513 I2S0 on the Terasic HDMI pins (`HDMI_I2S0`, `HDMI_SCLK`,
  `HDMI_LRCLK`, `HDMI_MCLK`). The native runtime already programs the
  transmitter for 16-bit I2S at 48 kHz (N=6144, CTS=74250). There is no host
  `fes.audio` mailbox.

Sega mappers, banked/48 KiB cartridges, expansion hardware, 224/240-line
modes, PAL timing and cycle-perfect raster effects remain outside this slice.
`fes.sms` is registered for package-only parent builds and is not in the
factory image. Kit HDMI-audio acceptance of this bitstream is not recorded
here. An older parent pin or launch/Stop record does not accept it.

## Memory and host interfaces

| Address or port | Function |
| --- | --- |
| `0x0000–0x7fff` | 32 KiB fixed cartridge ROM linked at download time in the OSS package |
| `0x8000–0xbfff` | unmapped; reads `ff` |
| `0xc000–0xdfff` | 8 KiB CPU RAM |
| `0xe000–0xffff` | mirror of the 8 KiB CPU RAM |
| I/O `0x7e`/`0x7f` | SN76489 write (both ports; reads stay `ff`) |
| I/O `0xbe` | VDP data |
| I/O `0xbf` | VDP control/status |
| I/O `0xdc`/`0xde` | joystick port A (P1 plus P2 left half) |
| I/O `0xdd`/`0xdf` | joystick port B (P2 right/fire; unused bits 1) |

The production package declares keyboard and fixed video through
`fes.simple-computer` 1.0. Its `cartridge-rom` is required at library launch,
and the target patches the sealed ROM lanes before programming. Its ROM-link
mailbox omits the legacy blob capability and rejects blob begin, eject, data,
and commit commands as invalid opcodes; execution releases without a separate
media upload. The default simulation and Quartus oracle retain the earlier
mailbox media transport as explicit diagnostics; their format-2 seals are not
the OSS product package.

VDP interrupt connects to Z80 INT. The cartridge itself occupies `0x0038` if
it installs an IM1 handler; there is no Coleco `JP 0x8066` shim and no
SG-1000 NMI vector at `0x0066`.

## Standard joystick mapping

Keyboard bits 0..9 keep the Coleco first-slice matrix (P1 then P2
Up/Right/Down/Left/Fire1). The SMS 8255 permutes those bits onto DC/DD:

| DC bit | Function | Keyboard bit |
| --- | --- | --- |
| 0 | P1 Up | 0 |
| 1 | P1 Down | 2 |
| 2 | P1 Left | 3 |
| 3 | P1 Right | 1 |
| 4 | P1 Fire 1 | 4 |
| 5 | P2 Up | 5 |
| 6 | P2 Down | 7 |
| 7 | P2 Left | 8 |

| DD bit | Function | Keyboard bit |
| --- | --- | --- |
| 0 | P2 Right | 6 |
| 1 | P2 Fire 1 | 9 |
| 2..7 | unused, read as 1 | — |

Neutral ports read `ff`. This is a diagnostic mapping on the unchanged 40-bit
`fes.simple-computer` keyboard transport, not a new physical-gamepad API.

## Open Mode 4 diagnostic

From the misteross root:

```sh
make sms-diagnostic
python3 cores/fes-sms/diagnostic/generate.py \
  --output build/diagnostics/fes-sms/mode4.rom \
  --preview build/diagnostics/fes-sms/mode4.ppm
python3 cores/fes-sms/diagnostic/generate.py --interactive \
  --output build/diagnostics/fes-sms/mode4-hil.rom \
  --preview build/diagnostics/fes-sms/mode4-hil.ppm
```

The emitter is BIOS-free and MIT-licensed. Reset enters `0x0000` and jumps to
code at `0x4000`. Upper-half code initializes Mode 4 VRAM, both CRAM palettes,
the `0x3800` name table and `0x3f00` SAT, then paints the border/checkerboard
plus a flipped, priority-marked and alternate-palette plus tile. It writes `A5`
at `C000`, captures port `DC` at `C001`, stores distinctive upper-half data
`0x18` at `C002`, and programs SN76489 tone 0 (period 256, max volume) on
ports `0x7F`/`0x7E` with the other channels silent. `mode4.rom` is the sim
regression image and HALTs after that signature. `mode4-hil.rom`
(`--interactive`) keeps the controller poll loop on `DC`/`DD` so a HIL
display+USB+HDMI-audio check can Stop/relaunch; it does not HALT forever.
`--pad-to` admits 32 KiB. This is a bounded diagnostic, not a mapper or retail
claim. Kit HDMI-audio HIL remains later.

`make sim-fes-sms` is the cheap Verilator check: the stream-enabled mailbox
consumes `cores/fes-sms/generated/stream-exchanges.json`; the focused VDP unit
checks legacy Text/Multicolor colors, Mode 4 VRAM buffering, CRAM color, tile
priority/palette, sprite collision, line IRQ and VBlank IRQ; the PSG unit checks register writes on
`0x7E`/`0x7F` and the tone-0 square wave; HDMI I2S checks 16-bit 48 kHz frames;
then the machine checks 32 KiB fixed-map copy, 8 KiB RAM mirror, joystick
ports, long-then-short `0xff` tails and the diagnostic ROM (including the
programmed tone). It is host simulation, not hardware acceptance.

`make sim-fes-sms-oss` compiles the linked-ROM machine and the Coleco
registered VDP/RAM wrappers with `-DFES_SMS_OSS=1 -DFES_SMS_ROM_LINK=1 -DFES_COLECO_OSS=1`. The
shared `coleco_dpram` / `coleco_vdp` mappers key off `FES_COLECO_OSS`, not
`FES_SMS_OSS`; do not drop the Coleco define.

`make sim-fes-sms-quartus` checks the Quartus `altsyncram` RAM/media
branches with a locally supplied Quartus 17 `altera_mf.v` and Icarus Verilog.

`make build-fes-sms-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for `fes.sms` 1.2.0. It requires a clean committed tree, writes
`build/fes-sms-quartus/build-inputs.json`, embeds that build id, and seals
a format-2 package when timing passes. `--compile-only` produces the RBF and
timing evidence without sealing. It does not program hardware.

`make build-fes-sms` is the format-3 OSS recipe (`scripts/build_fes_sms_oss.py`).
It authenticates the selected Mistral ROM database, verifies all 32 routed
blank M10K lanes and seals `rom-map.json` with `core.rbf`. The kit uses the Go
linker; Python is needed only on the build machine.
It copies Coleco `clocks-oss.sdc`, selects the shared
`toolchains/registered-memory.lock`, and uses the SMS
`constraints-oss.qsf` (Coleco video/I2C pins plus ADV7513 I2S). Yosys defines
`TV80_REFRESH=1`, `FES_SMS_OSS=1`, and `FES_COLECO_OSS=1`. `--synth-only` runs
Yosys on a dirty tree and does not seal. The producer uses `--router gpu` and
a first-pass HIP seed/weight search (starts at seed 3 / HeAP 1000, then
the remaining `PLACER_SEEDS` and weight 300). Final structured `clk_sys` and
`pixel_clk` rows must meet 52 MHz and 74.25 MHz. `fes.sms` is registered for
package-only parent builds and is not in the factory image. Kit HDMI-audio
acceptance of this bitstream is not recorded here. The dated gap inventory is
`docs/validation/2026-09-17-sms-oss-gap-ladder.md`. It is not the current
schedule.
