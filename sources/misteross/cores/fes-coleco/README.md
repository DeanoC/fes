# FES ColecoVision first slice

This directory is a described FES core, not an `experiments/` place-and-route
test. The core lane is [docs/cores.md](../../docs/cores.md).

This directory contains the next FES emulator bring-up after Pong and ZX81.
It is a reduced ColecoVision-compatible console slice that uses the existing
`fes.application` 1.0 mailbox and the DE10-Nano fixed 720p shell. The
reference system is the [MiSTer ColecoVision core](https://github.com/MiSTer-devel/ColecoVision_MiSTer),
but this package does not copy the MiSTer framework and does not claim full
retail-game compatibility.

## Implemented first slice

- Verilog TV80 Z80-compatible CPU, clock-enabled from the 52.224 MHz FES system
  domain.
- Raw 1–32 KiB cartridge via `fes.media.blob-stream` 1.0. Images up to
  16 KiB retain the mirrored map; larger images map linearly at `0x8000–0xffff`.
  Legacy `fes.media.blob` remains bounded to 1–16 KiB.
- Open 8 KiB reset shim at `0x0000–0x1fff`; its vector is `JP 0x8000`, so no
  proprietary ColecoVision BIOS is embedded. Synthesized packages optionally
  overlay this aperture through `fes.firmware.blob` 1.0.
- 1 KiB CPU RAM at `0x6000–0x63ff`, mirrored through `0x7fff`.
- TMS9918-style VDP ports `0xbe` (data) and `0xbf` (control/status), 16 KiB
  VRAM, Graphics I, Graphics II, Text and Multicolor rendering on a fixed
  256×192 logical raster, four-bit tile and sprite colors, buffered VRAM reads,
  VBlank status and enabled VBlank NMI delivery.
- Two standard controllers with joystick/keypad mode selection, twelve encoded
  keypad keys and two fire buttons through shared native controller ports.
- Centered 512×384 logical image in the established 1650×750 HDMI timing.

- TI SN76489A tone/noise synthesis at ports E0–FF with shared 48 kHz stereo
  HDMI output. `make sim-fes-coleco-audio` checks PSG and coherent PCM transfer.
  System/audio clocks share one 52.224/12.288 MHz PLL. The reduced CPU stays
  on its /16 enable; a shared 32-bit fractional accumulator drives 4,024,320
  logical raster samples per second for 256×262 frames at a nominal 60 Hz.
  HDMI pixel timing stays fixed and independent.

Expansion hardware and bank switching remain outside this first slice. The
logical frame cadence follows the TMS9918A manual's 262-line, approximately
60-frame/s noninterlaced mode; composite sync details, half-line behavior and
cycle-perfect raster effects remain outside this slice. Native host/runtime
selection follows the declared interfaces. Graphics II supports screen-third pattern/color
addressing and register masks. Text mode renders 40×24 six-pixel glyphs with
eight-pixel side margins and suppresses sprites; Multicolor selects four 4×4
color blocks per character and keeps sprites active. Unsupported mode selectors
render the R7 backdrop. The bounded sprite path includes
normal 8x8/16x16 sprites, magnification, early-clock positioning,
clipping, transparency/priority, four visible sprites per line, collision and
fifth-sprite status. The raw media limit and reset shim are deliberate
compatibility boundaries.

## Private BIOS bring-up

The default package retains the open reset shim. It starts execution at
`0x8000` and does not boot a conventional Coleco cartridge header or provide
Coleco BIOS services. A successful cartridge transfer therefore does not prove
that a retail game can run.

The OSS producer accepts an explicit private 8192-byte BIOS for bring-up:

```sh
python3 scripts/build_fes_coleco_oss.py --bios /absolute/path/to/coleco-bios.bin
```

Run this from the misteross module in a clean, committed FES worktree. The
producer snapshots the BIOS into ignored build output and includes the binary
and generated initialization-file digests in its functional identity. This
variant uses `fes.coleco.private-bios` and the separate private package store;
it is not the default FES image selection. BIOS bytes and BIOS-bearing artifacts
must stay outside source control. The BIOS is embedded at build time; the
existing cartridge media interface does not load a BIOS at runtime.

## Runtime firmware overlay

Quartus and OSS producers set `ENABLE_FIRMWARE=1` on `coleco_application_gp`.
The shared mailbox then advertises capability bit 7 and accepts opcodes 15–17
for an exact 8192-byte overlay while reset is held. Writes land in the machine's
firmware dual-port RAM at `0x0000–0x1fff`. Firmware commit does not release
execution; cartridge media still owns release. Existing video board simulations leave
`ENABLE_FIRMWARE` at its RTL default 0, so they keep the open shim and do not
advertise firmware. `make sim-fes-coleco-firmware` and its `-oss` variant
enable the production endpoint and connect it to the real machine RAM/CPU.
They check every uploaded byte, malformed and incomplete transfers, reset
ordering, and execution of an open test firmware that writes CPU RAM and
halts. Both run with the normal Coleco unit suites; they use no private BIOS.
The default package declares `fes.firmware.blob` 1.0
optional so BIOS-free Graphics I titles share the firmware-capable bitstream.
Household firmware binds at launch through FogCast/runtime; BIOS bytes stay
out of git. Firmware mailbox behavior is software-tested; hardware acceptance
is pending. `--bios` remains the separate private build-time embed path above.

Use the BIOS-free diagnostic as a hardware control before diagnosing a retail
cartridge. Private BIOS boot and per-game rendering/input checks are separate
from that control and from general ColecoVision compatibility.

## Memory and host interfaces

| Address or port | Function |
| --- | --- |
| `0x0000–0x1fff` | default open reset ROM (reset to `0x8000`, NMI to `0x8066`); synthesized packages may overlay this from `fes.firmware.blob`, or an explicitly supplied private BIOS |
| `0x6000–0x7fff` | mirrored 1 KiB CPU RAM |
| `0x8000–0xffff` | fixed 32 KiB cartridge aperture; images up to 16 KiB mirror at C000 |
| I/O `0xbe` | VDP data |
| I/O `0xbf` | VDP control/status |
| I/O writes `0x80–0x9f` / `0xc0–0xdf` | select keypad / joystick mode for both players |
| I/O reads `0xe0–0xff` | controller 1 when A1=0, controller 2 when A1=1 (including FC/FF) |

The application mailbox advertises fixed video, blob media, blob-stream media, `fes.gamepad.ports`
1.0, `fes.keypad.ports` 1.0 and optional `fes.firmware.blob` 1.0. It does not advertise keyboard or legacy gamepad.
The common endpoint supplies accepted writes to a console-owned 32 KiB staging
RAM; `coleco_application_gp` preserves the machine's registered cartridge-copy path.
The host holds execution reset while uploading and commits media before
releasing it. GP commit acknowledges publication of the mailbox blob; the
machine then copies it into cartridge RAM. CPU and VDP reset remain asserted
until `media_ready && media_loaded`, even if the host releases immediately
after the commit ACK. Both compiler lanes' final registered write completes before
`media_loaded` permits execution. No host delay or new mailbox operation is
required. A release without committed media also keeps CPU and VDP in reset.

Stream media uses the existing opcodes 7–12, reflected IEEE CRC32, ordered
32-bit begin/chunk fields and chunks of at most 512 bytes. This endpoint reports
1..32768 bytes. Commit requires the entire declared length and matching CRC;
failed, partial or aborted streams cannot release execution. Abort invalidates
staging; HOLD preserves staging and neutralizes controllers. The shared endpoint
keeps streaming optional and disabled by default for existing applications.
For images larger than 16 KiB, CPU and peek reads past the committed length return
FF, including after a longer image was previously loaded. There is no bank mapper.

`make coleco-stream-diagnostic` generates original MIT-licensed 24 KiB, 32767-byte
and 32 KiB ROMs. Their real CPU reads C000, C001 and the final valid byte; shorter
images also check the first out-of-range address and FFFF for FF. Failure loops
before video initialization. Success stores A5 at RAM 6000 and paints the existing
Graphics I frame (`stream-pass.ppm`). `make sim-fes-coleco` transfers these through
the real mailbox, verifies reset through the final registered copy, checks every
cartridge byte and exact HDMI frame, and reloads 32 KiB → 24 KiB → 32767 → legacy.
Both conditional lanes run these checks. Shared golden stream vectors additionally
exercise CRC errors, incomplete transfers, invalid sequencing, abort, oversize,
odd chunks and odd-address RAM pairs. Hardware acceptance is a separate FES step.

The machine adapter qualifies each held CPU `OUT` cycle into one VDP write
strobe. TV80 holds IORQ/WR low across multiple CPU enables; passing every enable
through used to duplicate control bytes and VRAM writes. The VDP independently
consumes a held CPU read once and retains the returned byte until RD/IORQ
deasserts, including after a destructive status read. This does not establish
complete TMS9918 compatibility.

## VDP reads and interrupts

`make sim-fes-coleco-graphics` and `make sim-fes-coleco-graphics-oss` check
literal VRAM fixtures for Graphics I color grouping, Graphics II table masks
and screen thirds, Text margins/glyph colors/sprite suppression, Multicolor
byte pairs/nibbles/backdrop/sprite overlay, invalid mode selectors, backdrop
substitution, display blanking and full-color sprites. Both are included in the
corresponding Coleco regression targets. The video test carries all sixteen
color codes through the actual framebuffer and HDMI palette.

The data/control interface follows the read-ahead and interrupt behavior in
the [TI TMS9918A data manual](https://computers.baffa.tec.br/pages/datasheet/TMS9918A_TMS9928A_TMS9929A_Video_Display_Processors_Data_Manual_Nov82.pdf),
sections 2.1.3–2.1.6, and the pinned MiSTer
`vdp18_cpuio.vhd` reference. A read-address command (second byte bits 7/6=00)
prefetches the addressed byte and advances the 14-bit pointer. Data reads return
the buffer, request the next byte and advance once, wrapping at 3FFF. A write
address (01) does not prefetch; data writes update both VRAM and the buffer.
Status reads clear pending VBlank/collision/overflow/index and abandon a half-written
control command. Status bit 7 is VBlank, bit 6 is sprite overflow (a fifth
visible sprite), bit 5 is collision, and bits 4..0 report the first suppressed
sprite index. The bounded implementation only captures a fifth-sprite event
while the VBlank flag is clear, matching the TMS9918 status rule. A simultaneous
new VBlank event takes priority over
acknowledgement; an already-held read is not acknowledged again.

Read data is collected two system edges after a fetch request so the same
logic accommodates both actual FPGA lanes' registered RAM address. This is
not a cycle-accurate model of the original DRAM access windows. CPU instructions
provide ample spacing; callers of the standalone VDP simulation must allow the
fetch to complete. Registered RAM wrappers, raster copies, sprite line
buffers and compiler constraints are part of the bounded implementation.

The active-low VDP interrupt is pending VBlank gated by register 1 bit 5. It
connects to the Z80 **NMI**, not maskable INT. Enabling while VBlank is pending
asserts immediately; disabling releases the line without clearing the pending
flag. A status read acknowledges it; a later frame can interrupt again.

The BIOS-free shim forwards `0066` to cartridge address **8066**. Programs that
enable VDP interrupts must install their handler there, initialize a RAM stack,
acknowledge VDP status, preserve the registers they use and return with RETN.
This is an explicitly defined open-cartridge convention, not Coleco BIOS
services or a stock cartridge header. Existing graphics/controller diagnostics
leave the VDP interrupt-enable bit clear.

`make coleco-vdp-diagnostic` builds the original MIT-licensed `vdp_io.py`
cartridge at `build/diagnostics/fes-coleco/vdp-io.rom` and its 16 KiB padded
variant. The real CPU checks prefetch/sequential/wrap reads, status clearing,
interrupt-disabled behavior, enable-with-pending NMI, two acknowledged frame
interrupts and subsequent disabling. Only then does it paint a green one-tile
border with a black interior and HALT. Failure paints a red interior;
timeout never counts as success. RAM 6000 is 00 while running, A5 on pass,
E1..E7 on failure; 6001 is the NMI count, 6002/6003 the handler's status reads,
6004 its error flag and 6010..6014 the VRAM-read samples.

`make sim-fes-coleco-vdp-io` and `make sim-fes-coleco-vdp-io-oss` run the CPU
diagnostic through real media delivery, checking cartridge bytes, results,
reset-only reruns and actual logical pixels. They are included in the full
simulation suite. No CPU registers, NMI, RAM or VRAM are forced by these tests.
Fresh exact-artifact FPGA builds and leased hardware captures remain distinct
from these host simulations.

## Open Graphics II sprite diagnostic

From the misteross root:

```sh
make coleco-sprite-diagnostic
# Or generate the compact cartridge and reference image directly:
python3 cores/fes-coleco/diagnostic/sprite_io.py \
  --output build/diagnostics/fes-coleco/sprites.rom \
  --preview build/diagnostics/fes-coleco/sprites.ppm
```

`diagnostic/sprite_io.py` is a BIOS-free, MIT-licensed Z80 emitter. It uses
only the Python standard library, has no assembler or downloaded/commercial
ROM dependency, and emits a raw cartridge entered at `0x8000`. The compact
image is **1223 bytes**, with SHA-256
`b3aa3558e702272cdbd019d5f4553cc5e885e754ac7c29648137f2b6ce6a831c`.
`--pad-to 16384` produces `sprites-16k.rom`, whose expected SHA-256 is
`5bb58354ff5c49100aae1769270fe03d32524816464dcc99ee619cd09e5a054d`.
Unused bytes are `ff`.

The CPU first disables the display, clears all VRAM through port BE, writes a
Graphics I border/background and a one-bit sprite pattern, then places five
8x8 sprites on one scanline. Two overlap for collision; the fifth is suppressed
by the four-sprites-per-line limit. It polls status through BF until collision
and overflow/index 4 are present (`0x64` when VBlank is clear), storing `A5` at
RAM `6000`; timeout/failure stores `E1`. It then replaces the SAT with three
16x16 sprites, enables display/16 KiB/16x16/magnified mode (`R1=0xc3`) and
halts. The final sprites exercise early-clock placement, right-edge clipping
and a second color.

The expected `sprites.ppm` is 1280x720: a green Graphics I border, black
interior, red at logical `(188..189,81..82)` and `(255,101..102)`, and green
at `(10..11,131..132)`. The coordinates include the established one-HDMI-pixel
registered-framebuffer read latency and the fixed 2x logical scaling. The
board oracle independently checks the complete HDMI frame and expects 27,680
nonblack active pixels. The registered-memory sprite evaluator may sample the
reset-time empty SAT before a CPU diagnostic finishes setup; the direct VDP
test therefore lets one frame drain before asserting the first configured
line. This is a startup sequencing accommodation, not a sprite-coordinate
rule.

`make sim-fes-coleco` and `make sim-fes-coleco-oss` generate the compact/full/
compact sprite cartridges and run the CPU diagnostic through the default and
`FES_COLECO_OSS` top-level shells. The test observes the status sample,
cartridge/VRAM writes and exact 1650x750 timing/frame output. It is host-only
behavioral evidence; a newly sealed FPGA artifact and the separately leased
hardware capture are still required for hardware acceptance.

## Standard controller mapping

Reset selects keypad mode. Mode writes ignore their data byte; repeated clocks
within one held OUT select the same mode, not toggle it. Only the low I/O
address byte is decoded. Reads return bit 7=0 (stationary standard controllers,
no spinner), bits 5/4=1, bit 6=active-low Fire 1 in joystick mode or Fire 2 in
keypad mode. Bits 3..0 contain active-low Left/Down/Right/Up in joystick mode,
or an encoded keypad value. Neutral is `7f` in either mode.

Both ports accept full active-high states. Gamepad bits 0..7 are Up, Down,
Left, Right, A, B, Select, Start; A/B map to Fire 1/2. Select/Start are unused.
Each keypad has twelve bits: digits 0..9 followed by * and #. The mailbox port
index is 0 for player 1 and 1 for player 2. No keyboard emulation is involved.

Keypad keys `0 1 2 3 4 5 6 7 8 9 * #` return CPU low nibbles
`a d 7 c 2 3 e 5 1 b 9 6`; no key returns `f`. Multiple pressed keys use
the lowest index in that order. This follows the standard-controller path in
[MiSTer revision 5e8713c](https://github.com/MiSTer-devel/ColecoVision_MiSTer/tree/5e8713cbc91b7d7abe4806cb87834b16d7348011)
(`rtl/cv_addr_dec.vhd`, `rtl/cv_ctrl.vhd`, and the keypad pin mapping in
`ColecoVision.sv`); it does not model electrical multi-key combinations,
spinner quadrature or Super Action extras.

The CPU regression executes actual IN/OUT instructions over both mode-select
ranges and every read alias, with neutral, every control, mixed/all-pressed
states, unrelated writes and reset-only reruns. Historical matrix-shaped vectors
remain only in the independent test oracle; test stimulus converts them to the
new native states. Existing sealed simple-computer packages retain their old
runtime support. The legacy GP source remains because SG-1000 and SMS use it.

## Open Graphics I diagnostic

From the misteross root:

```sh
make coleco-diagnostic
# Or generate only the exact compact cartridge and a reference image:
python3 cores/fes-coleco/diagnostic/generate.py \
  --output build/diagnostics/fes-coleco/graphics-i.rom \
  --preview build/diagnostics/fes-coleco/graphics-i.ppm
```

`diagnostic/generate.py` is the annotated Z80 instruction source/emitter.
Python's standard library is sufficient; no assembler, downloaded ROM,
commercial cartridge or proprietary BIOS is used. Its original code, generated
cartridge and reference image are covered by `diagnostic/LICENSE` (MIT).
The raw cartridge is **1067 bytes**, entered at **0x8000** by the open reset
shim's `JP 0x8000`. Its SHA-256 is recorded by the generation command. There is
no header or container. The entry/code/data stay entirely within the portable
1..16384-byte media aperture. `--pad-to 16384` produces an equivalent full-size
image, with unused trailing FF bytes; `make coleco-diagnostic` generates this
as `graphics-i-16k.rom` too. Upload the `.rom` bytes through the FogCast media
path for a loaded `fes.coleco` package; the PPM is a host comparison artifact.

The CPU disables interrupts, initializes SP without reading or using stack RAM,
clears all 16 KiB VRAM through port BE, sets all eight VDP registers through BF,
then fills the name table at 0000, patterns based at 0800 and colors at 2000.
Tile names 0, 8, 16 and 24 select distinct eight-character color groups for
the border, green pattern, red pattern and blank interior.
It also writes a sprite-list terminator at 1B00, enables Graphics I display, and
halts with a static picture. All VRAM consulted for the final image is written
by the program; CPU RAM power-up values are irrelevant. The VDP honors
display-enable masking. Compare the settled image, allowing about a second
after execution release for this diagnostic.

Expected active HDMI image (`graphics-i.ppm`, 1280×720):

- Black surroundings; centered 512×384 picture at x=384..895, y=168..551.
- Solid green border, one 8×8 logical tile thick (nominally 16 HDMI pixels).
- Inside, 30 columns × 22 rows of alternating green/red 12×12 HDMI squares
  with black gaps. The upper-left interior square is green.
- Green is RGB `21c842`, red `d4524d`, black `000000`: 75,168 green,
  47,520 red, 798,912 black active pixels; 122,688 nonblack in total.

The current registered framebuffer read shifts the contents one HDMI pixel
right inside the fixed image window. The preview and board oracle include
that existing latency/clipping (left border 17 pixels, right border 15 pixels).
The VDP uses one Graphics I color byte per eight character patterns and the
full sixteen-code palette; this diagnostic selects green, red and black.
This cartridge diagnoses the open slice, not a stock BIOS cartridge format or retail compatibility. It does
not test audio, controller input, sprites, interrupts or BIOS services.

`make sim-fes-coleco` generates both cartridges and runs the default and
`FES_COLECO_OSS` lanes with the production `TV80_REFRESH=1` setting.
Each board simulation uploads 1067 → 16384 → 1067 bytes
through HOLD/BEGIN/DATA/COMMIT/RELEASE, including the odd tail and full aperture,
with **no wait before RELEASE**. It asserts CPU/VDP reset through the copy,
compares every loaded byte including the OSS final flush, observes one VDP
write per CPU OUT, and requires the CPU to write every VRAM address before
HALT. Randomized initial RAM exercises independence from power-up contents.
After raster settling, every RGB/DE/HS/VS sample of a complete 1650×750 HDMI
frame is checked against an independent C++ oracle for each load. Only the
simulated GP, clocks and I²C boundaries are driven; the test does not preload
the cartridge, VDP or framebuffer or force CPU execution state.

These are host-only behavioral simulations with controllable digital clocks,
not PLL/timing checks, FogCast/runtime integration, or hardware acceptance.
This step changes `coleco_machine.sv` (reset gating and VDP write strobes), so
the earlier FPGA packages below cannot establish this diagnostic's result.
The selected revision also changes BUILD_ID: the integrator must rebuild and
seal the final artifact, then perform the separately owned upload/capture test.
No full FPGA build or hardware operation is part of this diagnostic change.

## Commands

### Two-player input diagnostic

`make coleco-diagnostic` also generates `input.rom`, `input-16k.rom` and the
neutral `input.ppm` under `build/diagnostics/fes-coleco/`. The original
`graphics-i.rom` remains byte-for-byte unchanged. To generate a pressed-state
reference, use:

```sh
python3 cores/fes-coleco/diagnostic/generate.py --interactive \
  --output build/diagnostics/fes-coleco/input.rom \
  --preview build/diagnostics/fes-coleco/input-mixed.ppm --row0 26 --row1 13
```

Two rows of five solid panels show controller 1 (top) and controller 2
(bottom). Columns mean **Up, Right, Down, Left, Fire**, in bit order 0..4.
Released panels are red, pressed panels green; the surround is black with
a green border. This view shows joystick directions and Fire 1 only.

`--row0` and `--row1` retain active-low preview-only panel states for old
reference images. Live input uses native controller ports. For keypad and both
fire buttons use the complete controller diagnostic below.

The BIOS-free CPU initializes all VRAM and both previous-input bytes in CPU
RAM, selects joystick mode through C0, then continuously polls ports FC and FF.
It maps hardware Fire 1 bit 6 to panel bit 4. Only changed player rows repaint
their five panels, avoiding writes while the input is stable. Each panel is
four by four tiles: columns 2..5, 8..11, 14..17, 20..23, 26..29; rows 5..8
and 15..18. Input is active-low; no HALT, interrupt handler or RAM power-up
contents are required. As with the static diagnostic, initialization can be
visible briefly before the display settles.

GP HOLD clears both gamepad and keypad ports. Board tests exercise neutral
state and explicit restoration after held-input reload. Input detachment must
publish zero states through the normal runtime path.

### Joystick/keypad byte diagnostic

`make coleco-diagnostic` also generates `controller.rom`,
`controller-16k.rom` and `controller.ppm`. Four rows show raw controller bytes
in this order: player 1 joystick, player 1 keypad, player 2 joystick, player 2
keypad. Each row has eight panels, bits 0..7 from left to right; green means
zero and red means one. Panels occupy two-by-two tiles at columns
`4+3*bit .. 5+3*bit`, rows `3+5*bank .. 4+5*bank`. Black surroundings and a
green border retain the static diagnostic's geometry and framebuffer latency.

```sh
python3 cores/fes-coleco/diagnostic/generate.py --controllers \
  --output build/diagnostics/fes-coleco/controller.rom \
  --preview build/diagnostics/fes-coleco/controller-mixed.ppm \
  --buttons 0x11 0x28 --keypads 0x001 0x800
```

`--buttons` and `--keypads` accept the two players' active-high native states
for the preview only; they do not change ROM bytes or send input. The older
`--matrix` preview option remains for existing reference images and cannot mix
with native states. The CPU switches modes and polls both players, repainting
only changed banks. Its four cached bytes initialize to FF, outside the valid
controller range, so every bank is painted initially.
Board simulation observes actual CPU VRAM writes for all 40 matrix bits and
checks complete HDMI frames for representative mixed states and reloads in
both lanes; it does not inject controller values into the CPU or prefill VRAM.

### Build and simulation

```sh
make sim-fes-coleco VERILATOR=/absolute/path/to/verilator
make toolchain-fes-coleco
make build-fes-coleco-quartus
make build-fes-coleco
```

The Quartus recipe requires authenticated Quartus Prime Lite 17.0.2. The OSS
recipe authenticates the repository-local Yosys, nextpnr-mistral, and Mistral
tools, routes `5CSEBA6U23I7`, and seals a format-2 package only after the
timing/resource checks pass. The repository-wide `toolchain.lock` remains on
the current mainline pins. `make toolchain-fes-coleco` instead builds the
Coleco compatibility lock at `toolchains/registered-memory.lock` into
`build/toolchain/fes-coleco`, enabling the HIP device backend for
`gfx1100;gfx1201`. The selected OSS recipe uses Yosys
`e2d425dee148cc60c50f4e9b354a10d90eab15f4`, nextpnr
`0fad53a75a0218941c417ec6bb58bdede9070987`, `--router gpu`, seed 4,
`--timing-allow-fail`, and a 74.25 MHz request without `--tmg-ripup`; it
rejects a CPU-reference fallback in the route log. The seed is sealed with
the recipe's build record because the embedded `BUILD_ID` changes the
placement search space. The GPU router may report an early timing shortfall
before its final repair pass, so the allowance lets routing complete while the
recipe still requires the final structured 52.224 MHz, 74.25 MHz and 12.288 MHz timing rows to
pass. Quartus does not consume the OSS lock or GPU toolchain.
Neither build command programs hardware.

The earlier clean integration build of implementation revision
`b60e1aaccc5ec0c6f93d654c5cb2e6caf9f3e873` synthesized 85
`MISTRAL_M10K_TDP` and 48 `MISTRAL_M10K` cells, completed with no unrouted nets,
and reached 59.62 MHz on `clk_sys` and 90.88 MHz on `pixel_clk`. Its sealed RBF
was 2,484,053 bytes with SHA-256
`370e478fcb1706845bc39eb71a3ee3caeb9dedfed1855126f49579efbe0389e8` in
package `3b1b9dbcf2a30e8b389ee2aaf11dc0d9facfd3c4b9461b4c9f301afa6244f368`.

A clean Quartus Prime Lite 17.0.2 compile of that same implementation revision
completed analysis, fitting, assembly, and the required TimeQuest checks. It
used 2,168 logic cells and 100 RAM segments; its sealed RBF was 2,294,532 bytes
with SHA-256
`b0b315e758e8cb7ae0171ad4be501acfad3ea7c600fe473bfea479cb9a7dc973` in
package `6e863542effce1b60ff45f8665edfead60297f5bb31c15782c3de8bbb667114e`.

The earlier raw OSS RBF (`c6a060fa...a83eca9`) was loaded only as a development
probe and timed out because it has no MiSTer identity. The exact clean OSS
format-2 package above then loaded through the native FogCast path; the target
reported the package, ABI, build ID, and required interfaces, and the host-owned
stop returned it to idle. The HDMI sample was black, so this is exact-artifact
load/stop diagnostic evidence, not Coleco functional or video acceptance.

## Bring-up fixes and compiler workarounds

### Functional TV80/media fixes

Before the write-strobe guard, the real CPU's `OUT (BF),A` sequence for
register 1 (`C0` data, then `81` selector) left register 1 as `81`, not `C0`.
The default simulation also showed register 2=`82` instead of `00` and
register 4=`84` instead of `01`. Both lanes returned black at HDMI (384,168),
where the cartridge requires green. The board regression explicitly rejected
a second `bus_write` strobe while the same CPU IORQ/WR transaction remained
asserted: `VDP consumed the same CPU OUT transaction twice`. The fix adds
`vdp_write_seen` in the machine adapter, re-armed when IORQ or WR deasserts,
and suppresses further write strobes within that transaction. Direct VDP tests
had driven one strobe per write and did not expose the TV80 integration bug.

The immediate-release regression separately failed with
`CPU/VDP escaped reset before cartridge copy completed` before internal reset
gating was added. A commit ACK publishes media; it does not finish the copy.
The new test removes the old `size + 32` delay before RELEASE and observes
reset until the actual copy completes, then validates every copied byte and
CPU-generated pixel over 989 → 16384 → 989-byte loads in both lanes.
These two fixes change functional RTL. They are **not** Yosys/nextpnr/Quartus
workarounds and require fresh final FPGA builds.

### Quartus registered-read correction and vendor regression

At revision `5f239c9`, the operator's OSS capture showed the expected pattern
over 989 → 16384 → 989-byte loads. Quartus compact/full-size captures instead
showed the same orange border, green interior tiles and shifted patterns.
These operator observations prompted the following host-side reproduction;
the correction still requires a newly built Quartus artifact and hardware check.

Intel's **unmodified Quartus 17.0.2** `eda/sim_lib/altera_mf.v` establishes that
`coleco_dpram` reads take one clock: `outdata_reg="UNREGISTERED"` does not remove
the registered address. The Quartus media copier previously treated the GP
read as asynchronous, producing `40 40 41...` from uploaded `40 41 42...`.
The diagnostic's extra initial DI remains executable, but its absolute table
pointers then read one byte early. That explains the wrong colors and displaced
patterns. Quartus now selects the same existing registered-media prime/delayed
write/final-flush path as OSS. The OSS behavior is unchanged.

The framebuffer had both `address_reg_b="CLOCK1"` and
`outdata_reg_b="CLOCK1"`, introducing two read edges where the shell and OSS
expect one. Quartus now leaves the address registered and selects
`outdata_reg_b="UNREGISTERED"`. The vendor probe confirms the one-edge latency;
no custom behavioral RAM model is used to infer it.

```sh
# Icarus Verilog 12 (iverilog + vvp); no Quartus synthesis/build is invoked.
QUARTUS_ROOTDIR=/absolute/path/to/17.0/quartus make sim-fes-coleco-quartus
# Reproduce both failures from the old RTL, then require current RTL to pass:
QUARTUS_ROOTDIR=/absolute/path/to/17.0/quartus \
  python3 scripts/sim_fes_coleco_quartus.py --baseline 5f239c9
```

Set `IVERILOG`/`VVP` for non-PATH executables. For a relocated Icarus install,
derive `IVERILOG_BASE` from the installed `ivlpp` helper and verify the result:

```sh
iverilog_bin="$(command -v iverilog)"
iverilog_prefix="$(cd "$(dirname "$iverilog_bin")/.." && pwd)"
IVERILOG_BASE="$(find "$iverilog_prefix/lib" -type f -name ivlpp -print -quit)"
IVERILOG_BASE="$(dirname "$IVERILOG_BASE")"
test -x "$IVERILOG_BASE/ivlpp"
```

Then pass `IVERILOG`, `VVP`, `IVERILOG_BASE`, and `QUARTUS_ROOTDIR` as
absolute paths for the selected Icarus 12 and Quartus 17.0.2 installations.
No tools are downloaded by this target. Icarus is
used for these probes because Verilator 5.051 rejects the vendor model's
`i_good_to_write_a2`/`i_good_to_write_b2` feedback constructs. The existing
default/OSS full-board simulations remain Verilator-only and need no Quartus.
Vendor warnings about tri0/tri1 input ports coerced to inout are retained in
compile logs. Intel's model is neither patched nor copied into the repository.
Its MIF converter writes a `.ver` sibling, so the runner copies the tracked
reset MIF into its ignored simulation work directory before running.

Actual probe output, before → after:

```text
RAM address 1: A=32 B=32 framebuffer=31 expected=32 -> framebuffer=32
media: cartridge[1]=40 expected=41 size=3 -> exact copy passed
after media sizes: 1, 3, 989, 16384, 989; immediate RELEASE; all bytes passed
```

`build/sim/fes-coleco-quartus/{before,after}/quartus_{ram,media}_tb.log`
contains the actual stdout/stderr plus vendor-model SHA-256. The tested model
digest is `e7bc6f0200f8236986c4b255a4ce7937596946bdb646a93551057edc1e08ca69`.
The RAM probe observes both registered read ports and framebuffer output; the
media probe uses real GP commands and the real machine/Intel RAM branch,
checks every byte including odd tails and the 16 KiB boundary, and asserts
CPU/VDP reset until copy completion. A full vendor CPU/HDMI-frame simulation
is not claimed: the complementary full-frame regression is default/OSS, and
the operator owns exact-artifact Quartus hardware validation.

### OSS/Yosys/nextpnr workarounds

These are the concrete portability accommodations to hand to the
Yosys/nextpnr/Mistral owner:

| Boundary | Workaround in this bring-up |
| --- | --- |
| Toolchain selection | The repository-wide lock stays on current mainline Yosys/nextpnr. Coleco, SMS and SG-1000 OSS recipes select `toolchains/registered-memory.lock`, installs under `build/toolchain/fes-coleco`, and enables the HIP device backend. Bootstrap records the requested router and HIP architecture list beside the nextpnr commit/digest, and the recipe carries that attestation into the package manifest. Quartus uses its own vendor tools and needs neither lock. |
| Verilog/VHDL frontend | OSS uses only the Verilog TV80 files and `T80pa`, with `TV80_REFRESH=1`; it does not depend on the VHDL T80 path. |
| Inferred machine RAM | Cartridge, CPU RAM, and reset ROM use `coleco_dpram`; OSS selects registered `ram_style="m10k_tdp"` ports. Quartus also registers addresses despite UNREGISTERED outputs; only default simulation reads asynchronously. |
| Registered media bridge | Both compiler lanes return `media_q` one clock after `media_addr`; the machine primes the request, delays the cartridge write address, flushes the final byte, and re-arms when `media_ready` drops or reset rises. |
| VDP multi-read VRAM | A single inferred VRAM with one CPU port and three combinational raster reads fails Mistral memory mapping and also leaves Quartus with an oversized direct-memory implementation. Both compiler paths use four coherent `coleco_dpram` copies, broadcast CPU writes, and pipeline name → pattern/color reads by two clocks; the fourth copy is the serial SAT/pattern walker for sprites. |
| Quartus framebuffer inference | The original 49,152-entry async-read framebuffer expanded to 241,553 combinational nodes, exceeding the Cyclone V limit of 83,820. `coleco_video_dpram` uses independent-clock altsyncram with a registered B address and UNREGISTERED B output, matching the OSS wrapper's single read edge. |
| Quartus VDP inference | After the framebuffer fix, a direct VDP VRAM array still produced 186,906 combinational nodes and could not fit. The registered four-copy VDP path is therefore selected for `QUARTUS` as well as `FES_COLECO_OSS`; this is a Quartus resource-inference workaround, not a mailbox-contract change. |
| Registered sprite evaluator | The SAT and pattern bytes are walked serially through one registered M10K/altsyncram port. Each alternating 256-entry line bank is one packed 6-bit word: pixel, occupied and visible metadata share the M10K entry. Port A performs a registered read followed by a write for each source pixel; port B supplies the registered raster read. A sequential 256-word clear and matching-y publication interlock keep the renderer inside the nominal 3.3K system-clock logical-line budget at 60 Hz. The FSM's worst case is under 800 system clocks (clear 256 entries, scan 32 SAT entries, fetch/render four maximum-size magnified sprites), leaving more than 4× margin without unrolled reset/start loops. Yosys `e2d425de` (PR #14) keeps this registered `ramstyle=M10K` shape as a synchronous TDP with a live CLK2; the earlier `ec34fcf3` flow-through mapper had reclassified it as `CFG_ASYNC_READ` with a constant CLK2, which nextpnr rejects. |
| Sprite render fabric | A procedural 16x2 render loop synthesized to about 42K mapped combinational cells and left the fixed route running for more than 55 minutes without a report; `router2` also plateaued with tens of thousands of overused resources. The registered path therefore advances one source pixel per system clock with registered column/repeat counters and a read/write pair. Packing pixel and occupancy metadata into the M10K entry reduces the measured mapped ALUT fabric to about 3.1K while preserving priority, collision and clipping behavior. Do not restore the wide procedural write loop without a new fit/timing reproduction. |
| Sprite evaluator startup/interlock | The evaluator begins priming line zero immediately after reset, while the CPU may still be writing the SAT and VDP registers. The diagnostic allows one frame for the configured table to replace that reset-time sample before checking line-zero sprites; a production cartridge should likewise complete setup during its normal startup warm-up. A `!sprite_pending_valid` guard also prevents a new build from clearing the bank whose publication is still pending. |
| Bulk initialization | Clearing 16 KiB VRAM, 16 KiB cartridge, or the 49,152-entry framebuffer in an `initial` loop expands into thousands of `$meminit` cells and can exhaust the synthesis memory budget. The bring-up leaves those RAMs uninitialized and initializes only scalar state. |
| Reset image format | OSS/Yosys consumes the tracked byte-per-line `coleco_reset_rom.hex`; Quartus `altsyncram` consumes the tracked range-form `coleco_reset_rom.mif`. The Quartus recipe copies and pins both files. |
| PLLs | The two `altera_pll` wrappers are retained. OSS models them through the existing Mistral cells; the CPU frequency approximation is a clock-enable divider, not a fabric-generated clock. |
| HDMI I²C | Quartus uses tri-state assignments; OSS uses `MISTRAL_IO` open-drain pads and places the HPS I²C primitive at BEL `cyclonev_hps_interface_peripheral_i2c.52.60.0`. |
| Constraints | OSS uses only the accepted `constraints-oss.qsf` and `clocks-oss.sdc` subset: pin assignments plus a 50 MHz input `create_clock`; nextpnr derives the PLL clocks. |
| Route pressure | The selected settings are device `5CSEBA6U23I7`, nextpnr `0fad53a7`, `--router gpu`, seed 4, HeAP `--placer-heap-timingweight` 300 and `--placer-heap-critexp` 5, `--timing-allow-fail`, no `--tmg-ripup`, and a 74.25 MHz request. Default sealing is first-to-pass; `BEST_FMAX=1 GPU_DEVICES=0,1` searches weight and seed on both HIP devices after synthesis and records the winner in evidence. The embedded `BUILD_ID` makes the seed part of the sealed route recipe. The GPU router may emit an early timing shortfall before final repair; the allowance does not weaken acceptance because the recipe validates the final structured `clk_sys` and `pixel_clk` rows. The sealed recipe also requires a `backend hip:<device> ready` log entry, so a CPU-only nextpnr cannot be mislabeled as a GPU result. Timing-driven rip-up remains disabled because it was slower on the packed netlist. No missing nextpnr BEL or pack feature was identified. |
| Relocated Quartus/Icarus probe | Use `iverilog -V` to confirm Icarus 12, derive `IVERILOG_BASE` from the installed `ivlpp` path as shown above, and pass the absolute Quartus 17.0.2 `QUARTUS_ROOTDIR`. The runner uses Icarus for the unmodified Intel `altera_mf.v` model because Verilator rejects the model's `i_good_to_write_a2`/`i_good_to_write_b2` feedback constructs. Quartus 17.0.2 also rejects `OLD_DATA` on the packed bidirectional sprite RAM's registered port A; `NEW_DATA_NO_NBE_READ` is legal because the renderer consumes q_a one phase later. |
| Conditional simulation | `make sim-fes-coleco-oss` compiles GP, VDP, machine, and top-level tests with `FES_COLECO_OSS`; `make sim-fes-coleco` includes that target before the default lane. |

The initial failures and fixes are intentionally preserved in the source and
architecture notes so toolchain changes can remove a workaround instead of
silently retaining it. The earlier pre-packed OSS RBF was loaded through the
FogCast target-agent kit lease on the designated disposable kit. The target
agent's development probe timed out; the core was stopped and the lease was
released cleanly. No HDMI capture or functional/acceptance result was claimed;
the packed-sprite artifact still needs its own exact-artifact hardware check.
