# FES SG-1000 first slice

This directory is the next FES emulator bring-up after ColecoVision. It is a
reduced SG-1000-compatible console slice that uses the existing
`fes.simple-computer` 1.0 mailbox and the DE10-Nano fixed 720p shell.

SG-1000 is a Coleco sibling, not a second console stack. TV80, the bounded
TMS9918-style VDP, dual-port RAM wrappers, the GP mailbox, both PLL wrappers
and the 720p HDMI shell are the Coleco modules. This tree supplies the
SG-1000 memory map, the 8255 joystick ports, the board top, Quartus pins and
the oracle recipe.

This package does not copy the MiSTer framework and does not claim retail-game
compatibility. The Quartus 17.0.2 recipe is the compiler/oracle lane.
`make build-fes-sg1000` is the OSS Yosys/nextpnr-mistral producer using the
Coleco compatibility lock (Yosys `e2d425de`, nextpnr `0fad53a7`). HIP format-2
seal, formic gap execution, FES parent pin and kit HIL remain later jobs.

## Implemented first slice

- Verilog TV80 Z80-compatible CPU, clock-enabled from the 52 MHz FES system
  domain (Coleco `t80pa` / `tv80`).
- Raw 1–16 KiB mailbox media blob mapped at `0x0000–0x3fff`. There is no BIOS
  and no reset shim; reset fetches the cartridge.
- 1 KiB CPU RAM at `0xc000–0xc3ff`, mirrored through `0xffff`.
- TMS9918-style VDP ports `0xbe` / `0xbf` and the Coleco Graphics I / bounded
  Graphics II path.
- Two joysticks on the SG-1000 8255 ports `0xdc` / `0xdd`, adapted from the
  existing 40-bit keyboard matrix.
- Centered 512×384 logical image in the established 1650×750 HDMI timing.

Audio (SN76489), SC-3000 keyboard, banked 32/48 KiB cartridges, expansion
hardware, cycle-perfect clocking, sealed HIP format-2 production and native
FogCast/runtime selection remain outside this first slice.

## Memory and host interfaces

| Address or port | Function |
| --- | --- |
| `0x0000–0x3fff` | 16 KiB cartridge aperture (mailbox blob) |
| `0x4000–0xbfff` | unmapped; reads `ff` |
| `0xc000–0xffff` | mirrored 1 KiB CPU RAM |
| I/O `0xbe` | VDP data |
| I/O `0xbf` | VDP control/status |
| I/O `0xdc`/`0xde` | joystick port A (P1 plus P2 left half) |
| I/O `0xdd`/`0xdf` | joystick port B (P2 right/fire; unused bits 1) |

The mailbox, keyboard rows, media handshake, build identity and fixed-video
interfaces are the existing `fes.simple-computer` boundary. The host holds
execution reset while uploading and commits media before releasing it. CPU and
VDP reset remain asserted until `media_ready && media_loaded`.

VDP interrupt still connects to Z80 NMI. The cartridge itself occupies
`0x0066`; there is no Coleco `JP 0x8066` shim.

## Standard joystick mapping

Keyboard bits 0..9 keep the Coleco first-slice matrix (P1 then P2
Up/Right/Down/Left/Fire1). The SG-1000 8255 permutes those bits onto SMS-style
DC/DD:

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

## Open Graphics I diagnostic

From the misteross root:

```sh
make sg1000-diagnostic
python3 cores/fes-sg1000/diagnostic/generate.py \
  --output build/diagnostics/fes-sg1000/graphics-i.rom \
  --preview build/diagnostics/fes-sg1000/graphics-i.ppm
```

The emitter is BIOS-free and MIT-licensed. The image is entered at `0x0000`,
uses RAM at `0xc000`, paints the same Coleco Graphics I border/checkerboard,
stores `A5` at `C000`, captures port `DC` at `C001` and HALTs.

The same target also emits `build/diagnostics/fes-sg1000/controller.rom` and
`controller.ppm`. The controller image keeps the Graphics I shell alive and
continuously renders the raw active-low `DC` and `DD` bytes as two rows of
eight indicators. Pressed bits are orange and released bits are green; the
cached bytes at `C001` and `C002` make the live polling path observable in
simulation. Its preview accepts the unchanged 40-bit keyboard matrix with,
for example, `--controllers --matrix 0xfffffffdfe`.

`make sim-fes-sg1000` is the cheap Verilator machine check (media copy, RAM
mirror, joystick ports, Graphics I diagnostic and live controller diagnostic).
It is host simulation, not hardware acceptance.

`make sim-fes-sg1000-oss` compiles the same machine with
`-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1`. Registered media is gated on
`FES_SG1000_OSS`; the shared `coleco_dpram` / `coleco_vdp` OSS shapes are
gated on `FES_COLECO_OSS`. Both defines are required.

`make sim-fes-sg1000-quartus` checks the Quartus `altsyncram` RAM/media
branches with a locally supplied Quartus 17 `altera_mf.v` and Icarus Verilog.

`make build-fes-sg1000-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for `fes.sg1000` 1.0.0. It requires a clean committed tree, writes
`build/fes-sg1000-quartus/build-inputs.json`, embeds that build id, and seals
a format-2 package when timing passes. It does not program hardware.

`make build-fes-sg1000` is the OSS recipe (`scripts/build_fes_sg1000_oss.py`).
It copies Coleco `constraints-oss.qsf` and `clocks-oss.sdc`, and selects
the shared `toolchains/registered-memory.lock`. Yosys defines `TV80_REFRESH=1`, `FES_SG1000_OSS=1`, and
`FES_COLECO_OSS=1`. `--synth-only` runs Yosys on a dirty tree and does not
seal. HIP `--router gpu` of the synth-only netlist is recorded in the gap
ladder (R13). Format-2 seal, FES parent pin and kit HIL remain later jobs.
