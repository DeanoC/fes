# FES ZX81 described computer

Status: proposed on 2026-09-10. This document defines the next custom-ABI
computer milestone. It is not present in the selected image.

Baseline: FES `de2b917d7b5bff2809f2cd0b868f7ad9445a55a3` (`origin/main` at proposal), selecting FogCast
`cd70be1167b8e99259654aaf6bf69093855ac62f`, libmister-runtime
`2bfff81a038e7bb33efef73e4d935557d1a2943f`, mister-packages
`98d9874d733a49d1dc13c4cd4f37a58a5d84b6f3`, and misteross
`4a8b8635cf338b22648e9e94272243b1fce42a79`.

## Outcome and scope

Build **FES ZX81**, a standalone described core package for a small computer,
following the FES Pong pattern: extract the machine from the MiSTer Quartus
implementation, drive it through a custom FES GP ABI, and ship a format-2
package. Unlike Pong, bring the custom-ABI core up on Quartus Prime Lite 17.0.2
before the Yosys/nextpnr-mistral recipe. A Quartus artifact that plays is
bring-up evidence; it does not complete the Mistral milestone.

The first complete example must:

1. Show the ZX81 BASIC screen over the existing fixed 1280x720p60 HDMI path.
2. Accept a host keyboard through the custom ABI and type on that screen.
3. Load a `.p` program and run it.
4. Stop to the current launcher and relaunch, including a return to a
   supported MiSTer game.

It is distinct from any future MiSTer-compatible ZX81 catalog core. Do not
reuse `fes.simple-game`, the catalog `pong` system, MiSTer `hps_io`, or the
`sys/` framework.

## Why ZX81, and why Quartus first

ZX81 is a small complete computer: T80 CPU, a few kilobytes of RAM and ROM, a
software-generated raster, and a 40-key matrix. That is the smallest step from
ROM-less Pong to a machine that loads programs.

The upstream core is [ZX81_MiSTer](https://github.com/MiSTer-devel/ZX81_MiSTer)
Release 20260603:

| Field | Value |
| --- | --- |
| Repository | `https://github.com/MiSTer-devel/ZX81_MiSTer` |
| Commit | `9b24af6d924f9032ddd4a7dee9cd52ba97d41bd2` |
| Project | `ZX81.qpf` |
| Artifact | `releases/ZX81_20260603.rbf` |
| Size | 2,914,308 bytes |
| SHA-256 | `a8d6d641f8e39497ab344957f285baf5e23c5ad487db8e72b1218209fb3eeacd` |

That tree uses VHDL T80, Quartus `altsyncram`, a 50→52 MHz PLL megafunction,
and MiSTer `hps_io` for PS/2, ioctl tape, and OSD options. Those are natural
Quartus inputs and are not a Mistral recipe. FES Pong could go straight to
nextpnr because its game was already a small Verilog module on one 74.25 MHz
clock. ZX81 cannot: CPU video timing, dual clocks, block RAM, and VHDL must be
proven behind the new ABI on the known-good compiler first.

A read-only checkout of that exact commit lives at
`out/dev/zx81/ZX81_MiSTer`. It is a comparison reference, not a production
submodule and not a format-1 catalog pin.

## First-slice machine

Fixed configuration; no OSD and no runtime option words.

| Item | First slice | Outside this milestone |
| --- | --- | --- |
| Model | ZX81 | ZX80 |
| RAM | 16 KB pack at `$4000` | 1/32/48 KB packs, 8 KB low RAM |
| ROM | ZX81 8 KB from the pinned `rtl/zx8x.mif` | Alternative ROM upload |
| Video | Monochrome ULA raster, scaled to fixed 720p60 | CHROMA81, QS CHRS, inverse/border OSD, NTSC machine timing |
| Sound | None | YM2149 / ZON X-81 |
| Input | Full 40-key matrix over GP | Joystick types, ZXpand, ghosting, Recreated ZX |
| Media | One `.p` blob, at most 16,384 bytes | `.o`, `.col`, `.chr`, analog tape ADC |
| Speed | Original slow-mode wait | NoWait / x2 / x8 turbo |
| Memory | Fabric RAM only | SDRAM, DDRAM, HPS bridges |

The ROM bytes come from the pinned upstream MIF. Do not fetch a third dump.
Preserve upstream GPL-2.0 notices on extracted RTL.

## Existing paths that constrain the change

- Keep format-2 packages, `fes-gp-v1` programming, live identity, and the
  existing FES Pong driver. A new ABI is an additional registry pair, not a
  rewrite of package admission.
- `fes.simple-game` 1.0 is an 8-button game plus optional 256-word persistence.
  It cannot represent a keyboard or a 16 KB program blob. Do not extend that
  ABI's major for ZX81.
- Runtime `FesGp` already owns the one-request-at-a-time GPO/GPI transport,
  100 ms exchange deadline, and 2 s identity deadline. Reuse that transport.
  Dispatch on ABI id inside `fes-gp-v1`; do not invent a second programming
  profile or a second MMIO path.
- FES Pong runs gameplay in the 74.25 MHz pixel domain. ZX81 video is generated
  from the 3.25/6.5 MHz CPU enables on a 52 MHz system clock. Do not clock the
  T80 from 74.25 MHz. HDMI presentation stays on the existing 74.25 MHz raster.
- FogCast `core-load` / `POST /v1/development/core` remains the package entry.
  Do not add a catalog ZX81 title or menu accelerator in this milestone.
- Bare `.rbf` remains MiSTer. An invalid ZX81 package never falls back to raw
  loading.

## Artifact and identity

`core.id = "fes.zx81"`, human name `FES ZX81`, programming profile `fes-gp-v1`,
platform `de10_nano`, device `5CSEBA6U23I7`. Omit `core.system`; this is not a
MiSTer game launch.

Package construction, identity hashing, `.fcore` ustar rules, and build-ID
embedding follow the [RBF ABI design](2026-09-08-rbf-abi-design.md). Quartus
and Mistral recipes produce different payloads and therefore different package
IDs. The manifest `build.toolchain` string must name the actual lane.

## ABI: `fes.simple-computer` 1.0

New ABI id `fes.simple-computer`, major 1, minor 0, tag **2**. Same mailbox
layout as FES Pong:

| Word | Bits |
| --- | --- |
| HPS to FPGA (GPO) | 31 request toggle; 30:24 opcode; 23:16 index; 15:0 argument |
| FPGA to HPS (GPI) | 31:24 signature `0xF5`; 23 acknowledged toggle; 22 error; 21:16 zero; 15:0 response |

Shared constants (signature, request/ACK/error masks, identity magic `FES1`,
transport 1.0, identity word count 16, error codes 1/2/3) stay numerically
identical. Capability bit 1 remains `fes.video.fixed-720p60` 1.0. New required
interfaces:

| Interface | Capability bit | Role |
| --- | --- | --- |
| `fes.keyboard` 1.0 | 0 | 8×5 ZX81 matrix |
| `fes.video.fixed-720p60` 1.0 | 1 | existing fixed HDMI |
| `fes.media.blob` 1.0 | 2 | `.p` tape image |

Unknown capability bits stay ignored. Required recognized capabilities must be
present in live identity. mister-packages owns the YAML, numeric opcodes, and
generated C++/Verilog consumers. The programming registry adds
`fes-gp-v1` / `fes.simple-computer` major 1 beside the existing Pong pair.

### Opcodes

| Opcode | Meaning |
| --- | --- |
| `0x01` | Read immutable identity word; argument must be zero |
| `0x02` | Execution control; index 0; argument 0 holds reset and clears keyboard/media-in-flight, argument 1 releases reset; success response zero |
| `0x03` | Set keyboard matrix row; index 0..7; argument bits 4:0 are active-low keys for that ULA row, bits 15:5 zero |
| `0x04` | Media begin; argument is byte length 1..16384; index 0; replaces any previous blob and resets the write pointer |
| `0x05` | Media data; argument holds two little-endian bytes (low byte first); one-byte tail uses index 1 and argument bits 7:0, bits 15:8 zero |
| `0x06` | Media commit; argument and index 0; makes the blob visible to the existing ZX81 tape-loader patch; further data opcodes error until the next begin |

Identity indices 0..15 match Pong except ABI tag 2 and the capability mask
above. Build-ID words remain the 16-byte build record, low byte first.

Reset never clears mailbox ACK. Keyboard rows power up to `0x1f` (no keys).
Opposite host modifiers are a software mapping problem; RTL stores the eight
rows it is given. Neutralize writes all eight rows to `0x1f` before Start and
on Stop.

Media is not Pong persistence: the existing 256-word data window is too small,
and a `.p` is not settings. Do not reuse opcodes 4–7 from `fes.simple-game`.

## Clocks, video and board shell

Reuse the FES Pong HDMI pin/I2C shell: `FPGA_CLK1_50`, ADV7513 RGB888/DE/HS/VS,
HPS I2C at `cyclonev_hps_interface_peripheral_i2c.52.60.0` with constant-low
`MISTRAL_IO` data, and HPS GP with contained bridges and no fabric SDRAM.

Use two PLL outputs from the 50 MHz V11 reference:

| Clock | Rate | Domain |
| --- | --- | --- |
| `clk_sys` | 52 MHz | T80, ULA, ROM/RAM, keyboard rows, tape buffer, GP mailbox |
| `pixel_clk` | 74.25 MHz | existing 1650×750 720p60 raster |

52 MHz matches the upstream `pll` output (`gui_output_clock_frequency0 = 52.0`).
Integer 50×26/25 through a 1300 MHz VCO is acceptable when the checked PLL
policy admits it; Quartus may keep the existing fractional-N megafunction.
74.25 MHz reuses the checked FES Pong / `610_pll_frac_7425` profile.

The GP request toggle is the only asynchronous HPS signal into `clk_sys`. If
`clk_sys` is absent, identity does not ACK and activation fails. The 720p
raster keeps running while execution is reset. Capture the ZX81 monochrome
pixel and blanking in the 6.5 MHz enable domain, then integer-scale that
captured raster into 1280×720 with black bars. Measure the exact captured
active size from the extracted ULA simulation; do not claim 256×192 until that
measurement exists. Keep synchronization independent of CPU reset.

## RTL extraction

misteross owns a new `cores/fes-zx81/` tree. Copy machine RTL from the pinned
ZX81_MiSTer sources, then delete MiSTer board coupling:

- Keep T80, the ULA/video generator, 16 KB RAM, ZX81 ROM mapping, the
  tape-loader patch, and the 8×5 matrix sampling.
- Drop `sys/`, `hps_io`, PS/2 `keyboard.sv` (replace with GP rows), YM2149,
  CHROMA81, QS CHRS, joysticks, ZXpand, ioctl ROM writes, and ADC tape.
- Quartus lane may keep VHDL T80 and `altsyncram` `dpram`.
- Mistral lane later needs Verilog T80 (or equivalent), inferred M10K RAM, and
  explicit `altera_pll` cells. Same functional RTL and ABI; different
  synthesis sources are allowed when tests prove identical machine behaviour.

Do not modify files under the read-only `ZX81_MiSTer` checkout.

## Software path

libmister-runtime adds a `fes.simple-computer` driver on the existing `FesGp`
transport. Identify still checks magic, transport, ABI tag/version, build ID
and required capabilities. After video bring-up it writes eight neutral rows,
releases reset, and accepts keyboard/media. `SetButtons` is not the ZX81 input
path; gamepad-to-matrix mapping is optional diagnostics and not acceptance.

FogCast stages and loads the package through the existing described-core
route. Keyboard events from the host session map to ZX81 rows in the runtime
or agent using a documented matrix table derived from the upstream decoder,
without sending PS/2 scancodes across GP. `.p` admission rejects other
extensions and sizes above 16,384 bytes before programming. Select+Start
remains the software Stop gesture.

FES selects the Quartus package for the first image/diagnostic slice, then the
Mistral package once that recipe exists. Parent pins move only through the
integrator after review.

## Bring-up sequence

The sequence is normative. Do not start Mistral place-and-route until the
Quartus kit checks below have passed for the custom-ABI core.

1. Shared ABI YAML, fixtures, generated consumers, and `fes-gp-v1` registry pair.
2. Verilator of mailbox, matrix, tape buffer, ULA raster and a BASIC-boot ROM
   path, with simulation-only clock/HPS models.
3. Quartus 17.0.2 recipe, sealed format-2 package, timing closure required.
4. Runtime/FogCast driver and keyboard/media path against fakes, then the
   Quartus package on the designated kit: BASIC screen, typing, `.p` load,
   Stop/relaunch.
5. Mistral/nextpnr recipe of the same ABI and machine, sealed as a new
   package, then kit evidence for that payload.

## Acceptance

Software acceptance is zero hardware mutation on preflight failures, plus
simulation of identity, reset, matrix rows, media begin/data/commit, and
raster timing.

Quartus hardware acceptance, designated leased kit, exact artifact record:

1. Load FES Pong or a supported MiSTer game; then load FES ZX81; existing
   sessions reject invalid ZX81 packages without mutation.
2. Visible BASIC cursor/screen at 720p60.
3. Host keyboard produces the corresponding ZX81 characters.
4. A documented `.p` loads and runs.
5. Stop returns a responsive launcher; relaunch ZX81 and a supported MiSTer
   game, including Menu between them.

Mistral hardware acceptance repeats 2–5 for the OSS payload. A Quartus-only
success does not mark the Mistral milestone complete.

## Out of scope

Menu accelerator, UI redesign, catalog ZX81 title, ZX80, colour, sound,
turbo, joysticks, analog tape, alternative ROM, SDRAM, downloadable drivers,
package signing, and moving image assembly into FES.
