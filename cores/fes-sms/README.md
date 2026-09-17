# FES Master System first slice

This directory is the next FES emulator bring-up after SG-1000. It is a
reduced Master System-compatible console slice that uses the existing
`fes.simple-computer` 1.0 mailbox and the DE10-Nano fixed 720p shell.

The package id is `fes.sms` (FogCast `protocol.SystemSMS = "sms"` / library
SMS). Do not use `fes.mastersystem`.

Master System is a Coleco / SG-1000 sibling, not a second console stack.
TV80, the bounded TMS9918-style VDP, dual-port RAM wrappers, the GP mailbox,
both PLL wrappers and the 720p HDMI shell are the Coleco modules. This tree
supplies the SMS memory map, the 8255 joystick ports, VDP-to-INT wiring, the
board top, Quartus pins and the oracle recipe.

This package does not copy the MiSTer framework and does not claim retail-game
compatibility. The Quartus 17.0.2 recipe is the compiler/oracle lane.
`make build-fes-sms` is the OSS Yosys/nextpnr-mistral producer using the
Coleco compatibility lock (Yosys `da6373c0`, nextpnr `2d3c216`). FES parent
pin and kit HIL remain later jobs.

## Implemented first slice

- Verilog TV80 Z80-compatible CPU, clock-enabled from the 52 MHz FES system
  domain (Coleco `t80pa` / `tv80`).
- Raw 1–16 KiB mailbox media blob mapped at `0x0000–0x3fff`. There is no BIOS
  and no reset shim; reset fetches the cartridge.
- 8 KiB CPU RAM at `0xc000–0xdfff`, mirrored at `0xe000–0xffff`.
- TMS9918-style VDP ports `0xbe` / `0xbf` and the Coleco Graphics I / bounded
  Graphics II path. VDP IRQ drives Z80 INT (maskable). Pause NMI is unused.
- Two joysticks on the SMS 8255 ports `0xdc` / `0xdd`, adapted from the
  existing 40-bit keyboard matrix.
- Centered 512×384 logical image in the established 1650×750 HDMI timing.

Audio (SN76489), Mode 4, Sega mappers, banked 32/48 KiB cartridges, expansion
hardware, cycle-perfect clocking, sealed HIP format-2 production and native
FogCast/runtime selection remain outside this first slice.

## Memory and host interfaces

| Address or port | Function |
| --- | --- |
| `0x0000–0x3fff` | 16 KiB cartridge aperture (mailbox blob) |
| `0x4000–0xbfff` | unmapped; reads `ff` |
| `0xc000–0xdfff` | 8 KiB CPU RAM |
| `0xe000–0xffff` | mirror of the 8 KiB CPU RAM |
| I/O `0xbe` | VDP data |
| I/O `0xbf` | VDP control/status |
| I/O `0xdc`/`0xde` | joystick port A (P1 plus P2 left half) |
| I/O `0xdd`/`0xdf` | joystick port B (P2 right/fire; unused bits 1) |

The mailbox, keyboard rows, media handshake, build identity and fixed-video
interfaces are the existing `fes.simple-computer` boundary. The host holds
execution reset while uploading and commits media before releasing it. CPU and
VDP reset remain asserted until `media_ready && media_loaded`.

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

## Open Graphics I diagnostic

From the misteross root:

```sh
make sms-diagnostic
python3 cores/fes-sms/diagnostic/generate.py \
  --output build/diagnostics/fes-sms/graphics-i.rom \
  --preview build/diagnostics/fes-sms/graphics-i.ppm
```

The emitter is BIOS-free and MIT-licensed. The image is entered at `0x0000`,
uses RAM at `0xc000` with the stack at `0xdff0`, paints the same Coleco
Graphics I border/checkerboard, stores `A5` at `C000`, captures port `DC` at
`C001` and HALTs.

`make sim-fes-sms` is the cheap Verilator machine check (media copy, 8 KiB RAM
mirror, joystick ports, optional diagnostic ROM). It is host simulation, not
hardware acceptance.

`make sim-fes-sms-oss` compiles the registered-media machine and the Coleco
registered VDP/RAM wrappers with `-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1`. The
shared `coleco_dpram` / `coleco_vdp` mappers key off `FES_COLECO_OSS`, not
`FES_SMS_OSS`; do not drop the Coleco define.

`make sim-fes-sms-quartus` checks the Quartus `altsyncram` RAM/media
branches with a locally supplied Quartus 17 `altera_mf.v` and Icarus Verilog.

`make build-fes-sms-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for `fes.sms` 1.0.0. It requires a clean committed tree, writes
`build/fes-sms-quartus/build-inputs.json`, embeds that build id, and seals
a format-2 package when timing passes. `--compile-only` produces the RBF and
timing evidence without sealing. It does not program hardware.

`make build-fes-sms` is the OSS recipe (`scripts/build_fes_sms_oss.py`).
It copies Coleco `constraints-oss.qsf`, `clocks-oss.sdc`, and
`toolchain.lock`. Yosys defines `TV80_REFRESH=1`, `FES_SMS_OSS=1`, and
`FES_COLECO_OSS=1`. `--synth-only` runs Yosys on a dirty tree and does not
seal. The producer uses `--router gpu` and seed 4 with a live HIP backend
required. HIP `--router gpu` of the sealed netlist met the 52 MHz and
74.25 MHz structured fmax rows on a live HIP backend. FES parent pin and
kit HIL remain later jobs. The gap inventory lives in
`docs/validation/2026-09-17-sms-oss-gap-ladder.md`.
