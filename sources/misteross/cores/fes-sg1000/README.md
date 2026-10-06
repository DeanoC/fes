# FES SG-1000 package

This directory is a described FES core, not an `experiments/` place-and-route
test. The core lane is [docs/cores.md](../../docs/cores.md).

This directory is the next FES emulator bring-up after ColecoVision. It is a
reduced SG-1000-compatible console slice that uses the existing
`fes.simple-computer` 1.0 mailbox and the DE10-Nano fixed 720p shell.

SG-1000 uses the original shared `fes_z80_nmos` CPU. The bounded TMS9918-style
VDP, dual-port RAM wrappers, GP mailbox, video/system PLLs and 720p HDMI shell
are shared with Coleco. This tree supplies the
SG-1000 memory map, the 8255 joystick ports, the board top, Quartus pins and
the oracle recipe.

This package does not copy the MiSTer framework and does not claim retail-game
compatibility. The Quartus 17.0.2 recipe is the compiler/oracle lane.
`make build-fes-sg1000` is the OSS Yosys/nextpnr-mistral producer using the
registered-memory lock (Yosys `e2d425de`, nextpnr `a93fe013`, Mistral
`7ed06e21`). It seals a
format-3 package from a clean tree. The parent image route is package-only,
and `fes.sg1000` is in the factory image.

## Implemented first slice

- Original clean-room NMOS Z80 RTL, clock-enabled from the 52.224 MHz FES system
  domain at exactly 3,579,545 Hz on average, with alternating fractional
  half-cycle enables from `fes_z80_ce` and native machine-cycle timing.
- Exact 16 KiB `cartridge-rom` linked into the RBF before FPGA download,
  mapped at `0x0000–0x3fff`. Pad shorter fixed-map images with `0xff` before
  library import. There is no BIOS or reset shim; reset fetches the cartridge.
- 1 KiB CPU RAM at `0xc000–0xc3ff`, mirrored through `0xffff`.
- TMS9918-style VDP ports `0xbe` / `0xbf` and shared Graphics I, Graphics II,
  Text and Multicolor rendering on the fixed 256×192 logical raster.
- Two joysticks on the SG-1000 8255 ports `0xdc` / `0xdd`, adapted from the
  existing 40-bit keyboard matrix.
- SN76489-compatible PSG writes at I/O `0x40–0x7f`, converted to signed stereo
  PCM and serialized as 48 kHz I2S over the board HDMI audio pins. System and
  audio clocks share Coleco's 52.224/12.288 MHz PLL; video uses the second PLL.
- Centered 512×384 logical image in the established 1650×750 HDMI timing.

Text mode suppresses sprites, Multicolor keeps them active, and unsupported
mode selectors render only the R7 backdrop. The shared 256×262 logical raster
uses a fractional enable for a nominal 60 Hz frame cadence. Composite sync,
half-line behavior and cycle-perfect raster effects remain outside this slice.
SC-3000 keyboard, banked 32/48 KiB cartridges and expansion
hardware remain outside this slice. The OSS producer can seal from a clean
tree; the package-only recipe does not change the factory image.

## Memory and host interfaces

| Address or port | Function |
| --- | --- |
| `0x0000–0x3fff` | 16 KiB linked cartridge aperture |
| `0x4000–0xbfff` | unmapped; reads `ff` |
| `0xc000–0xffff` | mirrored 1 KiB CPU RAM |
| I/O `0xbe` | VDP data |
| I/O `0xbf` | VDP control/status |
| I/O `0x40–0x7f` | SN76489 PSG write |
| I/O `0xdc`/`0xde` | joystick port A (P1 plus P2 left half) |
| I/O `0xdd`/`0xdf` | joystick port B (P2 right/fire; unused bits 1) |

The keyboard rows, build identity, fixed-video and audio interfaces use the
existing `fes.simple-computer` boundary. The ROM-linked build reports keyboard,
video and audio capability bits (`0x13`). The format-3 manifest requires no startup
`fes.media.blob`; the host and target agent link the selected cartridge before
programming, then release reset. The diagnostic mailbox build still exercises
the legacy media handshake in simulation and the Quartus oracle.

VDP interrupt connects to the Z80 maskable INT input. Software selects IM1
and enables interrupts with EI; DI masks delivery. The cartridge owns the
`0x0038` interrupt vector. The separate pause NMI remains unused in this slice.

The CPU consumes positive/negative enables captured together in local
system-clock registers. This shifts their phase by one system clock while
retaining their cadence, and avoids distributing a falling-edge enable through
the CPU's rising-edge control path. Each half-cycle spans seven or eight system
clocks, so the registered
RAM and VDP memory paths settle before the CPU samples reads. Held I/O writes
advance VDP and PSG state once per transaction; VDP reads retain the byte from
before their side effect until the read ends. Refresh cycles do not assert
read or write strobes. The [shared CPU contract and qualification
limits](../fes-common/rtl/z80/README.md) apply here. Version 1.3.0 requires a
fresh seal with passing 52.224 MHz system, 74.25 MHz pixel and 12.288 MHz audio
timing, followed by its own exact-artifact kit checks.

The linked cartridge uses synchronous M10K lanes (`CFG_ASYNC_READ=0`) and
keeps its ROM map encoding. Each lane registers its address on the system
clock, the bank select is registered on that same edge, and an output
register supplies the second stage. CPU-facing latency is two system clocks.
The producer
tries a bounded, sequential placement search on each exact BUILD_ID netlist:
seeds `2,3,4,1,5,6,7,8,9,10` at HeAP timing weight 2000, then 1000, stopping at
the first candidate that passes all three clocks. Its functional build record
includes this policy; `qor-ranking.json` and `build-summary.json` retain the
winning seed and weight. A diagnostic placement does not qualify a new seal.

On 2026-10-03, package
`d6914aedd470815144308c85f456d56c9c69902b9cdc9c28df0007a63fd21ebe`
sealed from `deb0f18d` with reported Fmax of 53.064 MHz system, 100.523 MHz
pixel and 178.859 MHz audio, passing all three clock gates. Every pinned input
still matches `792b24805`. A leased, volatile load on the designated kit
linked the original open 16 KiB sound diagnostic and verified the admitted
package, BUILD_ID and ROM identity. The capture showed the checkerboard and a
steady 437 Hz FFT peak; Stop left the final second of both audio channels
exactly zero, and the lease was released. This is an exact-package development
diagnostic. It does not establish assembled-image acceptance, commercial-game
compatibility or equivalence to a physical NMOS chip.

The VDP INT wiring and separate pause NMI follow the
[SG-1000 hardware reconstruction and scope measurements](https://www.leadedsolder.com/2022/05/20/sg1000-clone-v1.html).
The shared NTSC CPU cadence is checked by `make sim-fes-z80-timing`;
`make sim-fes-sg1000` and its OSS memory lane execute original ROM probes
that prove DI masks VBlank, EI/IM1 reaches `0x0038`, and VBlank never enters
`0x0066`. The TI PSG retains its 15-bit periodic-noise recurrence and
period-zero reload of 1024. Host simulation does not accept a new bitstream;
fresh sealing and exact-artifact hardware checks remain separate.

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
eight indicators. Pressed bits are red and released bits are green; the
cached bytes at `C001` and `C002` make the live polling path observable in
simulation. Its preview accepts the unchanged 40-bit keyboard matrix with,
for example, `--controllers --matrix 0xfffffffdfe`.

It also emits `build/diagnostics/fes-sg1000/sound-16k.rom`: the same visual
signature while alternating an approximately 437 Hz tone and white noise about
once per second. This is an exact-size open
cartridge for a leased kit audio check; it does not establish hardware audio
acceptance by itself.

`make sim-fes-sg1000` is the cheap Verilator machine check (media copy, RAM
mirror, joystick ports, Graphics I diagnostic and live controller diagnostic).
It is host simulation, not hardware acceptance.

`make sim-fes-sg1000-oss` compiles the same machine with
`-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1`. Registered media is gated on
`FES_SG1000_OSS`; the shared `coleco_dpram` / `coleco_vdp` OSS shapes are
gated on `FES_COLECO_OSS`. Both defines are required.

`make sim-fes-sg1000-rom-link` tests the production ROM path with a padded
16 KiB diagnostic cartridge. It executes with `media_ready=0`, proving that
the linked cartridge does not depend on a startup media upload.

`make sim-fes-sg1000-quartus` checks the Quartus `altsyncram` RAM/media
branches with a locally supplied Quartus 17 `altera_mf.v` and Icarus Verilog.

`make build-fes-sg1000-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe
for `fes.sg1000` 1.3.0. It requires a clean committed tree, writes
`build/fes-sg1000-quartus/build-inputs.json`, embeds that build id, and seals
a format-2 package when timing passes. It does not program hardware.

`make build-fes-sg1000` is the format-3 OSS recipe
(`scripts/build_fes_sg1000_oss.py`). It exports a blank 16-lane M10K ROM and
authenticated `rom-map.json` for the target agent's Go linker. The manifest
requires one exact 16 KiB `cartridge-rom`, required
`fes.audio.pcm-s16-stereo-48k` 1.0, and no startup media blob. The producer
requires two PLLs, the four routed HDMI audio pads, and passing system,
pixel and audio timing domains.
It copies Coleco `constraints-oss.qsf` and `clocks-oss.sdc`, and selects
the shared `toolchains/registered-memory.lock`. Yosys defines
`FES_SG1000_OSS=1`, `FES_SG1000_ROM_LINK=1`, and
`FES_COLECO_OSS=1`. `--synth-only` runs Yosys on a dirty tree and does not
seal. A prior synth-only gap ladder is not sealed-bitstream or hardware
acceptance. `fes.sg1000` is in the factory image; selecting these CPU sources
requires a fresh sealed package rather than relabeling an existing artifact.
