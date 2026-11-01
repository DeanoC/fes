# FES PC/XT development

This development slice runs an original diagnostic ROM on an 8088 CPU through
an eight-slot virtual ISA backplane. The [machine design](../../docs/architecture.md#proposed-fes-ibm-pcxt-core)
targets an 8088/8086 PC/XT with IBM graphics and extensions to the existing
linkable-card mechanism. The motherboard slice adds memory decoding, an 8259A
PIC, 8253 PIT, 8255 PPI, XT keyboard byte transport and speaker PCM, and a transaction-native 8237A DMA subset. Separate ISA CGA and MDA display cards add guest raster output. An optional read-only XT floppy adapter is exercised in host simulation. The diagnostic package has no
HID keyboard frontend or physical socket map. The registered `fes.pcxt` recipe
builds a diagnostic package with fixed CGA Direct video and `fes.computer` 1.1
read-only floppy media. The separate experimental `fes.pcxt-dos` recipe enables
GLaBIOS XT/5160 and boots a 360 KiB raw floppy delivered at runtime through
the mailbox and SDRAM. A previous exact DOS package booted the pinned FreeDOS
image on Kit B; later packages need their own hardware acceptance. The storage
lane connects conventional RAM and firmware to the existing
addon SDRAM controller and boots the display diagnostic through its physical
command interface in host simulation. The normal package producer is Quartus Lite 17.0.2, with a common 100 MHz
CPU/motherboard/SDRAM clock and 74.25 MHz CGA Direct output. It requires
all declared seeds to pass reported internal setup, hold, recovery, removal
and pulse-width checks, plus a byte-identical independent seed-1 rebuild.
External SDRAM/HDMI, HPS and physical reset qualification remain open for both
packages. Each new DOS package requires its own exact-artifact hardware acceptance.
Nextpnr is an advisory diagnostic, not the normal package producer.
The existing RAM Tester
[external timing model](../../docs/architecture.md#sdram-external-timing-model)
is the starting point for pad qualification. PC/XT's current board shell uses
fabric capture registers and an inverted forwarded SDRAM clock; its frozen
timing fixture has no external input-delay constraints. Applying the existing
model requires this shell's clock-route arrival and matching board/pad bounds,
not the RAM Tester example's unmeasured flight intervals. The two-stage reset
release chains also do not by themselves supply asynchronous recovery/removal
timing or metastability qualification.
The board synthesis recipe copies `async_reg` intent to registers before Intel
FF mapping, preserving the metadata on the mapped FFs instead of their polarity
inverters. This does not add timing exceptions or change reset behavior. The
[21 October board netlist comparison](../../../../docs/validation/2026-10-21-pcxt-reset-metadata/summary.md)
finds every variant's mapped netlist unchanged apart from that attribute on
the four reset-chain and six video-crossing FFs.
A scoped reset exception would additionally require validation of the complete
chain, its synchronous consumers and timed interstage paths; the attribute alone
is not a timing waiver.

### FreeDOS DOS-profile package

The `fes.pcxt-dos` recipe selects `HARDWARE_DOS` and keeps the diagnostic
profile unchanged. It includes GLaBIOS and a BAT-only XT keyboard responder;
keyboard input is not yet supported. A 360 KiB raw floppy is delivered at
runtime through `fes.media.pcxt-floppy` and the complete ordered image is
written into SDRAM. When an initial disk is being loaded, the guest stays in
reset until its final physical write is committed. If the host releases
execution with the drive empty, the BIOS starts and displays its POST/boot
status instead of leaving a black screen. Eject and replacement after startup
do not reset the running guest. A cold reset invalidates the image. Guest
floppy reads use the SDRAM media path; there is no disk ROM in the FPGA and no
write path.
The FreeDOS fixture uses kernel 2043 and FreeCOM 0.86a; see
[`firmware/THIRD-PARTY-NOTICES.md`](firmware/THIRD-PARTY-NOTICES.md) and
[`sim/freedos.lock`](sim/freedos.lock). Hardware prompt acceptance remains
separate from the host simulation and package build.

The DOS profile supplies an XT keyboard self-test response over the existing
serial cable after the BIOS reset handshake. It does not yet accept host keys.

The recipe registers `scripts/pcxt_dos_hil_acceptance.py` with preferred kit
order B then A. It requires at least three complete captures with a meaningful
change and glyph-matches the final CGA `A:\>` prompt against the font
embedded in the locked BIOS ROM. It rejects a static signature and reports a
prompt-capture result only; it does not qualify external timing, whole-board IO,
reset or broader PC/XT compatibility. A build or seal does not imply a kit run.

### Experimental CPU100 / board50 diagnostic

The default shell retains its existing common 100 MHz clock. A separate
`HALF_RATE_BOARD=1` parameter keeps the MCL86 CPU and byte-bus adapter at
100 MHz, and runs the motherboard, ISA cards, storage frontend/controller,
firmware loader and framebuffer capture at 50 MHz. Pixel output remains
74.25 MHz. A fourth memory-PLL counter clocks the final SDRAM chip-select
register at phase 18 ns, 2 ns before the other 50 MHz output registers. This gives
the addon chip-select inverter more setup time while retaining the existing
chip command/capture cycle. `EXTERNAL_CS_REGISTER` defaults to zero in the
shared controller; only this experimental top owns that final CS register.

The video PLL can supply a second 74.25 MHz output at phase 8979 ps through a
dedicated global buffer for `HDMI_TX_CLK`. Pixel registers stay on phase zero.
The receiver samples between pixel updates, giving its hold requirement
headroom without changing pixel order or rate. The standalone default retains
one video PLL output and forwards the pixel clock directly; the installable
package enables the phased output independently of the half-rate board profile.
An unused second-output frequency and phase remain zero. A focused native pack
regression covers the default, package and half-rate settings.
The pixel-order proof and conditional external timing results are recorded
with the native three-seed trial; physical board qualification remains open.

The bridge holds each request and response until acknowledged,
uses CPU falling-edge transfer registers, and transfers guest tick events
without changing the nominal 4.772727 MHz guest clock. Coordinated reset
cancels outstanding work in both domains. Display dot enables and 48 kHz
speaker integration use the selected board frequency. The keyboard serializer
is used only by the existing 100 MHz motherboard fixture.

`python3 scripts/build_fes_pcxt.py --half-rate-board` builds and routes this
experimental diagnostic with the pinned tools; add `--synth-only` to stop after
mapping. The corresponding
`constraints-board50.qsf` packs all sixteen SDRAM DQ input, data and output-enable
registers. The core lock selects reviewed nextpnr main `2fa5ec4c`, including
endpoint launch identities, harmonic-clock timing and the explicit FF clear
reference. Only this experimental profile enables the harmonic/reset-reference
options and fixes the cohort search budget at zero. Setup/hold failures remain
fatal. The separately merged [FF reference correction](https://github.com/DeanoC/nextpnr/pull/299)
validates the unchanged 170/270 ps model limits with 1,648 source/deassertion-checked
observations. The selected compiler and original route receipts predate that
reference-only correction; its timing engine is unchanged. Physical reset
qualification remains open.
The native trial and four-corner reference screen are
[recorded here](../../docs/validation/2026-10-22-pcxt-pad-pipeline/whole-board-pipeline.md).
They do not qualify the IO, reset chains, physical board or a runtime package.

The regular simulation command includes `clock-bridge`, `speaker100`,
`speaker50`, `system-board50` and `system-board50-mda` regressions. The two
whole-system cases retain the real diagnostic ROM, both boots, three SDRAM
mask-delay cases and pixel/DMA checks; they tighten the refresh bound to
390 controller cycles at 50 MHz. Existing common-clock suites remain enabled.

### Whole-board Quartus diagnostic

`scripts/quartus_fes_pcxt.py` fits the complete CGA Direct CPU100/board50
board on `5CSEBA6U23I7` with Quartus Lite 17.0.2. Select an installed Yosys
from the PC/XT lock and an explicit Quartus installation:

```sh
python3 scripts/quartus_fes_pcxt.py \
  --yosys /absolute/pcxt-locked/install/bin/yosys \
  --quartus-root /absolute/quartus
```

The default seeds are 1, 2 and 3. Repeat `--seed` for a different declared
set; `--output` must name a new directory. The command captures the selected
sources, generated ROMs, compiler identities, pin constraints and SDC under
`build/diagnostic/fes-pcxt-quartus/`, retaining failed attempts. Yosys
elaborates the same RTL, combines memory write ports, lowers small control
arrays to registers and preserves the large memories for Quartus inference.
The clock/IO adapters preserve active-high pad enables and the explicit HDMI
global buffer; disabled bus hold uses Quartus's `FALSE` spelling. Portable
internal naming is checked against every original cell and net record before
Quartus synthesizes, places and routes the elaborated Verilog.

The four-corner capture records worst paths per endpoint and clock pair,
setup/hold, recovery/removal, pulse widths, fitted clock waveforms, Fmax and
unconstrained paths. Missing or truncated captures reject. The initial SDC
derives PLL clocks and uncertainty without clock groups, false paths or
external delay assumptions. An exit code of zero means the diagnostic was
captured; negative slacks and uncovered interfaces remain explicit in
`summary.json`. The package producer selects `--package-profile` with an
explicit `--build-id` and requires `--video-mailbox-contract`. That configuration
enables the runtime mailbox, common 100 MHz board clock and phased HDMI.
The standalone default remains the CPU100/board50 experiment; its timing does
not qualify the package configuration.

`--video-mailbox-contract` is a separate diagnostic opt-in. It runs the real
CGA/MDA ownership proofs from the captured sources, binds the declarations
through portable naming, checks the three exclusive two-stage chains and
their common PLL-lock reset assertion, marks the six stages with Quartus 17's
`SYNCHRONIZER_IDENTIFICATION FORCED` assignment to protect them from fitter
duplication, then applies `quartus/mailbox.sdc`.
It false-paths only three asynchronous pairs: `received` to `ack_meta`, `sent`
to `sent_meta`, and `machine_reset` to `hold_meta`. The ownership proof checks
that each bank stays stable for two pixel samples before adoption; a one-pixel-
period `set_max_delay` constrains `published_bank` to `front` and leaves that
payload path timed. No clock group is cut. Every fit captures all 24 direct
interstage setup/hold checks across four corners. Reset pulse width,
metastability, external IO and HPS timing remain open. The ordinary invocation
keeps its original constraints.

### Normal Quartus package and acceptance

From the FES root, with the pinned installations available:

```sh
export QUARTUS_ROOTDIR=/absolute/quartus
export FES_PCXT_YOSYS=/absolute/pcxt-locked/install/bin/yosys
python3 scripts/core_dev.py prepare --core fes.pcxt --output /absolute/new-candidate
```

The recipe selects `scripts/build_fes_pcxt_quartus.py`. `quartus.lock` binds
the exact Lite version and Yosys binary. Functional identity includes the full
Quartus/Yosys installation digests, the invoked Python interpreter and standard
library, host helper/library bytes, selected recipe, complete source closure,
firmware, pin/SDC sources and clock profile. It has no GPU selection.
Builds retain each seed and the independent repeat under
`build/fes-pcxt-quartus/<build-id>/attempt-<unique>/`. A retry retains earlier
attempts. Builds reject any reported internal timing miss, invalid internal
clock/constraint checks, missing coverage inventory or changed repeat RBF.
Unclocked registers must belong to the two elaborated HPS hard primitives;
their HPS timing remains outside the diagnostic qualification. The resulting format-2
package remains an experimental diagnostic; external physical acceptance,
BIOS/DOS boot, keyboard and writable disk support are separate.

The ROM displays black/white stripes. Only after SDRAM, the applicable DMA
probe, both display adapters and status polling pass does it draw a solid white
16-dot by 8-line block at the CGA image's top left and write `AA` to RAM `00600`.
With the floppy enabled, it then polls for media and checks one sector using
the FDC and DMA. A matching diagnostic image adds a 64-by-32 white block at
x=64; ejection or a mismatch clears it. The first sector's bytes are
`(11 + 37 * index) mod 256`, with a 360 KiB image size. This marker checks one
sector, not the complete floppy. The package exposes no keyboard/debug interface.

For a leased kit run, use a private host and the normal package acceptance
path; do not replace the shared host. Capture three complete HDMI frames after
boot, then run this checker from the FES root (ffmpeg/ffprobe required):

```sh
python3 scripts/pcxt_diagnostic_acceptance.py frame1.png frame2.png frame3.png \
  --output /absolute/diagnostic-result.json
```

The checker compares the entire scaled stripe picture, each output row,
monochrome output and all eight completion-block scanlines. Partial drawing
prefixes fail acceptance. Identical frames are expected.
A cold launch must first clear the previous execution; verify package/build
identity and Stop/baseline restoration through the normal lease lifecycle.
For historical captures from the earlier ROM, `--historical-pattern` checks
the old stripes but explicitly leaves final firmware completion unverified.
It cannot accept the new package. Passing this image check is a hardware
diagnostic result, not whole-board physical timing or DOS compatibility.

### Physical timing inputs

The diagnostic's synthetic IO constraints exercise compiler coverage. They
cannot serve as the board's acceptance constraints. Prepare the following
inputs for the selected kit before generating physical budgets:

The operator identified Kit A as MiSTer Pi and Kit B as SuperStation One,
both using standard MiSTer SDRAM (2026-10-20). Target Kit A first and retain
separate qualification for Kit B. This identifies the platform family, not
the fitted memory speed grade or measured board delays. The recovered
[RetroRemake product photo](https://cdn.shopify.com/s/files/1/0713/7145/6535/files/ram1to1.png?v=1725021808)
shows two Etron EM63B165TS-6SG chips; its
[product page](https://retroremake.co/products/128mb-sdram-mister-fpga-compatible)
describes an XS-DS v3.0-based addon. Use that photo-derived Etron -6 reference
for Kit A investigation. The published manufacturer schematic instead lists
Alliance AS4C32M16SB-6TIN; these are separate published populations. Their CL2
access/setup/hold numbers match, but photo identification supplies no measured
board bounds or fitted-kit inspection. [Recovery evidence](../../docs/validation/2026-10-22-pcxt-pad-pipeline/retroremake-sdram-photo-reference.json)
retains the sources and the conditional inverter failure. The
[MiSTer XSDS reference design](https://github.com/MiSTer-devel/Hardware_MiSTer/tree/master/Addons/SDRAM_xsds)
provides board design files; equivalence to either fitted board still needs
establishing before deriving board-specific bounds.

| Interface | Required evidence |
| --- | --- |
| SDRAM | Addon revision, actual memory part/speed grade, clock/write/read flight-time intervals, any chip-select inversion delay, output loads and uncertainty. Use the current routed forwarded-clock arrival and the actual capture-clock phase. |
| HDMI video | Board clock/data skew and loads, ADV7513 capture configuration, and the complete clock/data pad models. Bind the budget to the runtime register recipe actually used by the diagnostic. |
| HPS I²C | HPS peripheral-clock and controller configuration, characterized hard-IP launch/capture bounds, fabric/pad delays, pull-ups and bus capacitance. Constant-zero output data does not qualify the separately controlled output enables or feedback inputs. |
| Reset | Validated chain topology and intent, timed interstage/consumer paths, reset pulse and PLL lock/relock behavior, and metastability qualification. A structural chain proof does not supply the physical requirements. |

The [ADV7513 Rev. B datasheet](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf)
lists video setup/hold requirements of 1.8/1.3 ns and a programmable clock/data
relationship. These receiver requirements alone do not include board skew or
FPGA pad delays. The runtime's
[`adv7513.hpp`](../../../libmister-runtime/src/native/adv7513.hpp) owns the
register recipe; a source default is not readback of the selected kit.
The [Cyclone V HPS I²C signal definition](https://www.intel.com/content/www/us/en/docs/programmable/683126/21-2/i2c-controller-signal-description.html)
places the open-drain output enables in the HPS peripheral-clock domain.
Neither the 100 MHz system clock nor the 74.25 MHz pixel clock supplies their
launch timing.

`scripts/audit_fes_pcxt_hps_i2c.py` checks all four native enable/feedback
identities, the pad locations/polarities, and constant-low output data against
an immutable ordinary-producer route receipt. An optional bound native
extraction retains unsupported analogue arcs as unknown; zero-filled failed
queries are not delay bounds. A successful audit is structural evidence only.

`scripts/characterize_fes_pcxt_hps_i2c.py` rebuilds the matching Quartus
17.0.2 reference at declared 0/30 pF load endpoints and validates 128 paths
covering both pins, directions, edges, early/late observations and four corners.
It disables automatic dedicated open-drain conversion so the reference retains
the native separate OE path. It assigns no fabric clock to the HPS source.

```sh
python3 scripts/audit_fes_pcxt_hps_i2c.py \
  --route-receipt /absolute/immutable-producer-wrapper/receipt.json \
  --output /absolute/hps-audit.json
python3 scripts/characterize_fes_pcxt_hps_i2c.py \
  --quartus-bin /absolute/quartus/bin --mistral-cv /absolute/mistral-cv \
  --output /absolute/new-hps-reference
```

These references cover their own HPS-boundary-to-pad routes, not the hard-IP
clock-to-output/capture timing or a native route's fast-corner delays. Receiver,
pull-up, capacitance and flight bounds remain separate. Neither command exports
a package, changes a native route or programs a kit.

The RAM Tester budget generator is specific to its EM63B165 -6 reference and
registered-pad timing boundary. The ordinary PC/XT profile retains fabric DQ
capture and unregistered DQ data/OE paths. The experimental CPU100/board50
profile packs those registers into the pads; its conditional IO replay and
[fresh four-counter reference](../../docs/validation/2026-10-22-pcxt-pad-pipeline/board50-cslead-quartus-reference.json)
remain separate from fitted-kit qualification. Recompute the forwarded-clock-dependent budgets after routing
changes, and check both setup/hold and the chip's pulse-width and turnaround
requirements. Missing board inputs remain unknown; do not substitute the
example flight intervals or the 30 pF reference load as measurements.

The [21 October budget preflight](../../../../docs/validation/2026-10-21-pcxt-interface-budget/summary.md)
shows why this is also a compiler-model gap: independent SDRAM clock pad
envelopes cannot prove the reference chip's 2 ns high/low pulse requirement
at 100 MHz, even with ideal upstream clock and board assumptions. Nonzero
minima pooled across 0/30 pF observations alone still do not close that gap.
The preflight retains the arithmetic, HPS launch-source gap and dedicated
mask-output coverage distinction; it supplies no physical SDC or timing waiver.

## SDRAM storage

`rtl/pcxt_memory.sv` implements the motherboard's byte VALID/READY/DONE port
using the shared `fes-ramtest/rtl/sdram_addon_port.v` controller. Its ordinary
profile runs at 100 MHz; the experimental split runs this port at 50 MHz.
CPU and DMA already share that port through the motherboard arbiter. Accepted
payloads are retained independently of caller pins, with one completion and no
repeat while VALID stays high. Reads select the requested halfword byte;
DQM writes preserve its neighbour. Physical halfword offsets are guest byte
addresses shifted right by one, interpreted in the controller's packed
bank/row/column map. The guest can address only `00000–9FFFF` RAM and
`F0000–FFFFF` firmware. Defensive decode returns `FF` for ISA holes and suppresses ROM writes;
video VRAM stays on the ISA cards. The router and defensive backend use
A19:16 for these aligned 64 KiB regions (RAM below `A`, firmware at `F`),
avoiding a wide carry-chain comparison in dispatch. Symbolic comparison with
the original full-width bounds covers every address, request kind and write
direction; a mutation admitting the `A` hole is rejected.

A separate firmware byte VALID/READY/DONE port writes the 64 KiB firmware
window, with a 16-bit offset. It admits writes only while the machine's warm
reset is asserted. The caller must wait for SDRAM `initialized`, load the
complete firmware window and wait for the final DONE before releasing reset.
There is no bundled BIOS, production ROM sealing or HPS loader in this slice.
The BIOS POST simulation below specifies and checks its own shorter-ROM mapping;
that fixture is not a production firmware admission path.

The optional floppy profile uses physical SDRAM byte addresses
`100000–159FFF` for a read-only 360 KiB image, outside the guest address map.
Ordered host uploads can run while the guest runs, and the medium stays hidden
until every byte has physically completed and the mailbox commits the image.
CPU/DMA, floppy reads and uploads share the controller with bounded arbitration
and refresh; an outstanding floppy read drains before an overwrite. Warm reset
retains the image and cold reset invalidates it.

`cold_reset` restarts controller initialization and cancels all responses. It
must assert during startup; the caller holds the CPU/motherboard reset until
initialization and firmware loading finish. Warm `reset` blocks CPU admission,
drains an accepted physical operation and suppresses its remaining completion.
An issued write may commit during reset and is never retried. RAM, firmware
and physical SDRAM refresh remain live. A new CPU request waits for the drain;
old motherboard VALID must be cancelled by that same reset. An accepted loader
write retires independently of warm reset; keep reset asserted until it does.
Cold reset does not establish a storage-retention guarantee.

The controller uses its conservative CAS-2 100 MHz rate-3 path, single-rank
refresh and the established additional fabric stage after input capture.
Its output-register mode adds one aligned cycle to command/address, write data
and output enable, with the matching read-capture count adjustment. Plain
command/address registers can pack into the pads without moving the controller
state/enable muxes there. This configuration still needs routed and hardware
qualification. The digital cold-reset model allows already-issued commands to
drain until CKE falls, then requires a fresh initialization sequence; it retains
bank recovery state and RAM contents across FPGA reset.
`clk_pin` is the phase-0 controller clock used for the inverted SDRAM clock
output. The board diagnostic supplies captured `dq_rise/dq_fall`, bidirectional
pads and PLL phases; their physical setup/hold constraints remain unqualified. The diagnostic synthesis retains
`altddio_out` as a technology primitive; it does not qualify a mapped pad or
clock network. These standalone functional checks supply host-only evidence;
the experimental profile's routed results are recorded separately and no kit
acceptance is inherited.

Run `python3 scripts/sim_fes_pcxt.py --suite memory` from `sources/misteross`
for independent packed-address/command/CAS/DQM checks, 4,000 random neighbour
pairs, RAM/ROM boundaries, a random scoreboard, blocked live firmware loading,
held VALID/payload changes, physical refresh and 80 warm/40 cold reset phases.
`--suite memory-machine` loads all 65,536 ROM bytes through the real loader,
boots the 8088 from `FFFF0`, programs CGA/MDA through ISA, verifies complete
raw frames and repeats a warm boot without reloading firmware. It checks new
guest success writes on each boot. Both suites run with zero-, one- and two-cycle model DQM delays in the default
matrix, checking neighbour preservation and boot behavior with delayed masks.
The memory suite also injects an FPGA driver on the CAS-2 return cycle after a
legal READ. The model rejects this overlap during NOP and deselection, as well
as checking OE at READ admission. These are sampled digital
checks; they do not bound the chip's tLZ/tHZ or the FPGA's enable/disable tails.
The SDRAM model checks digital command timing at 100 MHz, not analog setup/hold,
refresh retention or board routing. Other suites retain independent variable-
latency testbench storage. The `system` and `system-mda` suites below integrate CPU/DMA storage and
Direct/Scanlines in the reusable board system.

`python3 scripts/synth_fes_pcxt.py --suite memory --cache-root /absolute/cache`
runs the locked Cyclone V diagnostic frontend and `check -assert`, writing
`build/synth/fes-pcxt/memory/diagnostic.json`. It covers the backend/controller,
without the CPU, cards, framebuffer or board capture/pads.

The diagnostic boot ROM is emitted as eight contiguous 8 KiB banks. Each bank
has its own registered read address to limit address-counter fanout across the
RAM blocks. Bank selection follows the synchronous read; the loader's public
address, valid/ready/done protocol and reset behavior are unchanged. The full
image and interrupted-load tests cover every byte across all bank boundaries.
This is a timing candidate until a frozen whole-board route validates it.

## ISA display cards

`expansions/pcxt_cga_card.sv` and `pcxt_mda_card.sv` are separate registered ISA
cards. Their common implementation owns VRAM and an original MC6845 timing/
address subset; the motherboard has no video register or memory shortcuts.
The display fixture puts CGA in logical slot 2 and MDA in slot 3. Both coexist
without claims, IRQ or DRQ collisions; these are portable logical slots, not
qualified physical link regions.

| Card | Guest memory | I/O (A15:10 ignored) | Raster at IBM standard register values |
| --- | --- | --- | --- |
| CGA | 16 KiB, mirrored over `B8000–BFFFF` | `3D0–3D7` index/data aliases; `3D8` mode, `3D9` colour, `3DA` status, `3DB/3DC` light-pen clear/set | 912×262 master dots, 640×200 active; 40/80 text, 320 four-colour or 640 one-bit graphics |
| MDA display | 4 KiB, mirrored over `B0000–B7FFF` | `3B0–3B7` index/data aliases; `3B8` mode, `3BA` status | 882×370 dots, 720×350 active; 80×25 text, 9×14 cells |

CGA implements both RGBI graphics palettes, intensity, mode-5 RGBI cyan/red/
white, interleaved 8 KiB graphics banks, text foreground/background, blink,
border, start-address wrap, cursor and light-pen latch/readback. MDA implements
normal, invisible, bright, underline and reverse attributes, blink and the
ninth-dot continuation for characters `C0–DF`. Cursor start/end, steady/off and
16/32-frame blinking use the CRTC registers. R0–R13, mode and colour registers
are write-only; R14–R17 read back. Status is sampled from the running raster.
VRAM is not cleared by reset; register and protocol reset blanks video and
cancels pending transactions. Uninitialized memory contents are unspecified.
The unused MDA printer ports return `FF`; a printer interface and Hercules
graphics are not implemented.

Each card exports an independent 100 MHz synchronous guest raster: `pixel_ce`,
RGBI (`I,R,G,B` in bits 3..0), `de`, active-high `hs/vs`, `sof` and `eol`.
All fields belong to the emitted dot when `pixel_ce=1`; `sof/eol` mark the first
and last dots of the complete raster, including blanking. DE describes the
active aperture even with video disabled. CGA uses 14.318180 MHz fractional
master-dot enables; low-resolution pixels repeat on two dots. MDA uses
16.257 MHz. They run independently of CPU READY, DMA and each other. This is a
development egress, not a new shared video contract or a Coleco socket profile.
`pcxt_video.sv` now adapts this egress to the existing RGB888 raster fabric,
with the shared Direct/Scanlines modules described below.
CGA RGBI is not RGB888: the consumer must perform the monitor's palette mapping,
including brown, rather than directly wire nibble bits to HDMI.

The shell supplies a combinational font ROM through `font_address/font_data`:
4096 bytes, `character*16 + row`, MSB at the left. CGA uses rows 0–7; MDA rows
0–13. Font data must settle before the next master-dot enable. This normalized
layout does not accept a raw IBM ROM without conversion. No IBM glyph ROM is
bundled. The simulations supply original diagnostic glyphs. A normal text
session needs separately supplied, legally usable fonts.

The CRTC implements standard non-interlaced IBM programming (`R8=02`), totals,
adjust lines, start address, fixed 16-line VS and 1–15-character HS (zero disables
HS). Interlace sets `unsupported_interlace`; it is not rendered as interlace.
Original adapter contention/wait-state timing, CGA snow, composite NTSC/artifact
colours, light-pen electrical timing and clone-specific CRTC behavior remain
unqualified. These cards establish host simulation behavior, not DOS/game,
physical socket, timing-closure or hardware acceptance.

Behavior references: [IBM PC Technical Reference, April 1983](https://bitsavers.org/pdf/ibm/pc/pc/1502234_PC_Technical_Reference_Apr83.pdf),
printed pages 1-123–128 and 1-135–154 (adapter programming and schematics), and
[Motorola MC6845 datasheet](https://bitsavers.org/components/motorola/_dataSheets/6845.pdf).

## Shared Direct/Scanlines output

`rtl/pcxt_video.sv` captures a selected card's standard active surface at
100 MHz and emits `fes.fabric.video.raster-rgb888` 1.0 at a separately supplied
74.25 MHz pixel clock. `pcxt_video_output.sv` uses the existing
`fes_video_part_direct.v` or `fes_video_part_scanlines.v`, with the existing
two-register raster boundary. No shared definitions or other cores' effects are
changed. The fixed Coleco native-pixel contract is not used for PC geometry.

The output is 1650×750 total / 1280×720 active, with a centered 960×720 (4:3)
viewport and 160-pixel side bars. Rational nearest-neighbour DDA scaling retains
all 640×200 CGA or 720×350 MDA source pixels without a variable divider in the
RAM lookup path. CGA converts the sixteen RGBI values to RGB888, including brown
(`6` → `AA5500`). MDA uses black, normal (`AAAAAA`) and bright (`FFFFFF`)
luminance. Scanlines use the existing part's half-bright odd output lines.
This is digital presentation, not composite artifact or analog monitor fidelity.

Three packed frame banks separate capture, pending publication and current
scanout. A stable bank/toggle mailbox crosses the clock boundary; pixel data
stays in the existing dual-clock `coleco_video_dpram`. A bank is adopted only
at output SOF and remains immutable until released. One completed third bank
may wait while the preceding publication is acknowledged. If all banks are
occupied, the next entire source frame is skipped; a source stall repeats the
last complete frame. Guest clocks and CRTC timing are never stretched to 60 Hz.
Malformed active geometry or an early EOL aborts capture until the next source
SOF. HOLD aborts partial capture and mutes RGB, preserving complete banks and
output timing; releasing it may repeat the last complete frame until new capture.
Capture registers retain write address, data and enable together before the
framebuffer; the last byte commits before a published frame can be adopted.
A pixel-clock register separates the framebuffer read mux from subpixel/palette
conversion; all raster fields receive that same extra cycle. System and pixel
resets must be asserted together and released synchronously
in their respective domains. Independent in-flight domain resets are unsupported.

CGA packs two four-bit pixels per byte; MDA losslessly packs its three levels
as four two-bit pixels per byte. Each profile reserves three 64 KiB banks
(192 KiB total). `MDA` and `SCANLINES` are elaboration-time selections. A future
PC/XT shell selects one output profile; the simulation-only two-output machine
fixture does not demonstrate that two full pipelines fit the FPGA. Fonts remain
separate card inputs. Physical video sockets, timing closure, sealed Direct/Scanlines archives and kit
acceptance remain integration work. The autonomous diagnostic below supplies
board clocks, SDRAM capture/pads and inline HDMI output. The inline path provides concrete raster output for
that shell; it does not register another core's physical slot layout as PC/XT.

Run `--suite video`, `--suite video-mda` or `--suite video-machine` with
`scripts/sim_fes_pcxt.py`. Both synthetic profiles check 32 output frames under
unrelated clocks, every displayed pixel, scaling and bars, CGA palette/MDA
luminance, frame ownership/cadence, malformed capture, pauses, HOLD, coordinated
reset and exact Direct/Scanlines response alignment. Each profile then repeats
at twice the guest pixel rate: a completed third bank stays queued and the next
whole source frame is dropped, while every published frame stays intact. The
machine suite runs the original 8088 display ROM through both ISA cards and
checks guest and 720p pixels, the CGA Direct response and the MDA Scanlines
response, with 0/137-clock external storage waits. All three suites run in the
default PC/XT family, including the parent affected-test and hosted simulation lanes.

Diagnostic locked synthesis supports `--suite video-cga` and `--suite video-mda`,
with `--scanlines` selecting the second effect. Outputs and source/tool receipts
are under `build/synth/fes-pcxt/video-{cga,mda}/{direct,scanlines}/`. These synthesize
one adapter plus shared effect, not the CPU, ISA cards, font ROM or board shell.
They provide no routed timing, physical containment or hardware acceptance.

## XT floppy and DOS simulation

The whole-machine DOS simulation begins with BIOS POST on the assembled 8088,
motherboard, ISA CGA card and SDRAM controller. It loads the stock GLaBIOS
0.4.2 8088 XT/5160 ROM selected in `sim/bios.lock` through the reset-only
firmware port and uses the simulated XT keyboard. It checks POST status and
text, an empty BIOS keyboard buffer after the BAT self-test, 640 KiB detection,
the CGA plus one-floppy equipment profile, and the INT 19 bootstrap path before
continuing into DOS. The keyboard then enters `DIR` through the guest-facing
keyboard path. BIOS instructions and interrupts run on the RTL machine without
firmware-call interception.

The runner accepts only the exact BIOS bytes in the lock. It maps that 8 KiB
ROM to the top of the 64 KiB firmware window and fills the rest with `FF`, then
loads the complete window through the reset-only port. `--bios` supplies a
local copy; `--download` explicitly fetches and verifies the selected release.
The default simulation font derives the BIOS's lower 128 8-by-8 graphics glyphs
into a 256-by-16-byte table, with the upper characters and unused rows blank.
This is sufficient for readable ASCII during POST, not a complete CGA font.
`--font` instead accepts an explicit 4096-byte normalized font.

The optional inline `pcxt_fdc_card` implements the read-only 765-family command
subset for one XT drive. It exposes the XT adapter registers, IRQ6 and DMA2,
with guest-time motor, seek and transfer timing. The supported commands include
Specify, Sense Drive, Sense Interrupt, Recalibrate, Seek, Read ID and DMA-mode
MFM Read Data; writes report write protection. The controller obtains bytes
through the SDRAM-backed read service, not by intercepting BIOS disk calls or
treating the disk as guest RAM. The supported image is a raw 360 KiB disk:
40 cylinders, two heads, nine 512-byte sectors per track. It is not a complete
765 model or a representation of flux-level copy protection.

The DOS fixture uses the 8086-compatible FreeDOS kernel and FreeCOM selected
and hashed in `sim/freedos.lock`, with a FAT12 boot sector assembled using the
pinned NASM release. The image contains `FESXT.TXT` and is generated under
`build/sim/fes-pcxt/freedos/`; the generated fixture and external firmware are not
packaged. The builder needs mtools. Downloads occur only with `--download`,
which fetches missing pinned sources and the assembler; cached inputs are
verified before use.

```sh
# From sources/misteross; the DOS target permits fixture downloads.
make sim-fes-pcxt-fdc
make sim-fes-pcxt-dos
# Or build the disk separately, then provide it and a cached BIOS explicitly:
python3 scripts/build_pcxt_freedos_image.py --download
python3 scripts/sim_fes_pcxt_dos.py --bios /absolute/GLABIOS_0.4.2_8X.ROM --disk /absolute/freedos-360.img
```

The `ENABLE_PCXT_MEDIA` shell profile connects `fes.computer` 1.1's
`fes.media.pcxt-floppy` unit 0 through a little-endian mailbox writer to this
SDRAM buffer. Both registered PC/XT packages enable this profile. Begin and
eject hide the medium without resetting the running guest; aborted uploads can
be ejected and restarted. This path accepts only 360 KiB raw geometry and
reports write protection. Inserting a disk into a running guest does not reset
it or automatically restart its boot. The current shell lacks a session-display
plane and a host keyboard path.

The whole-machine test loads the pinned GLaBIOS and sends the complete floppy
image through the runtime mailbox and real SDRAM controller before releasing
the guest. It checks BIOS POST, keyboard BAT, the boot sector at `0000:7C00`
through DMA2, the DOS prompt and `DIR` output in text memory and the captured
display. Rendered directory text is checked against the selected font and
attributes. It compares uploaded and returned disk bytes with the selected
image, checks warm and cold reset behavior, and exercises eject, an abandoned
partial transfer and reload during a DOS session. The runner validates the
selected disk's boot code, FAT12 layout and file contents against the lock and
records the disk hash. `make sim-fes-pcxt-media` covers runtime loading and the
diagnostic; `make sim-fes-pcxt-mailbox` covers identity and mailbox handshake.
DOS outputs are under `build/sim/fes-pcxt/dos/`. These are host simulations;
hardware acceptance is tied to the exact package and source recorded in the
parent core-status matrix. Write persistence and broader compatibility are not
implemented.

## Assembled board diagnostic

`rtl/pcxt_machine.sv` owns the real 8088, motherboard and eight registered
logical slots. `pcxt_display_bus.sv` keeps CGA and MDA in separate ISA cards;
its diagnostic configuration also puts the DMA loopback probe in slot 1.
The older display/memory/video fixtures now use this same machine wiring.
`pcxt_system.sv` connects it to the real SDRAM backend and exactly one selected
CGA or MDA capture/Direct/Scanlines pipeline. CPU firmware accesses and DMA
storage share the motherboard byte port; video memory and registers stay on ISA.

`pcxt_boot.sv` copies an original 64 KiB diagnostic image into SDRAM through
the firmware port. Its ROM reads continuously into a clocked output; the
completion-to-next-offer gap supplies the read cycle without delaying a valid
offer. A focused test checks every byte under backpressure and a cold restart.
Motherboard I/O strobes and PIC/PIT/PPI chip selects are predicted at the
preceding edge, preserving setup, eight active clocks and nine release clocks.
An unbounded induction proof checks their original state/phase equations on
the actual motherboard after reset, with arbitrary bus inputs and later resets.
`pcxt_system` admits sequential addresses starting at zero,
counts completed writes and releases the machine only after the final DONE.
Execution reset is registered in the system domain, so startup decode does not
feed combinationally through the motherboard reset fanout. Warm reset/HOLD
requests take effect at that register; cold reset independently resets storage.
Cold reset restarts initialization/loading; warm reset and HOLD drain storage
without reinitializing it or reloading firmware. HOLD resets the machine and
mutes the video, while the raster, complete framebuffer ownership and physical
refresh continue. The two video domains receive coordinated cold resets.

`firmware/diagnostic.S` is assembled with GNU x86 `as`/`ld` at build time.
It programs both display cards, checks VRAM mirrors and enables PIT1/DMA0
refresh before enabling either display. In the standalone profile it also
transfers bytes in both directions through the ISA DMA probe. The floppy and
probe cannot share DMA2, so the media-enabled profile instead verifies FDC/DMA
with the diagnostic disk after startup. Warm HOLD may still repeat the previous complete frame
until new capture, as described above. A RAM success marker is checked by simulation. Each font
address supplies the original diagnostic glyph byte `81` (two vertical strokes),
so no IBM ROM/font is bundled. The reusable system exposes the firmware byte
port, keyboard cable signals and font addresses/data to simulation or a future
shell; the diagnostic board ties the keyboard inactive and supplies the same
fixed glyph. No production HPS loader or sealed firmware map is claimed.

`rtl/fes_pcxt_top.sv` supplies a 100 MHz controller clock, 180-degree 100 MHz
fabric input capture clock, system-clock CPU ROM reads,
inverted DDR SDRAM clock pin, bidirectional SDRAM
pads, a separate 74.25 MHz fractional pixel PLL and HDMI RGB/sync. Loss of either
PLL lock asserts both domain resets; each domain releases through two registers.
The retained `altddio_out` is passed to the locked nextpnr native output packer.
The diagnostic synthesis applies `synth/rom_clock_map.v` after RAM inference:
it connects a constant/undefined unused ROM write clock to the registered read
clock only when writes are provably disabled. This supplies the native packer
with a real clock without changing INIT, registered read latency or writable RAM.
The CPU import splits its unchanged microcode into four byte-wide ROMs, each
with a local system-edge address register driven by the same next PC and reset
as the CPU PC. The intervening operand-phase system edge reads the ROMs;
the next execute edge captures the word and decoded operand controls. Both
address setup and registered ROM output paths have one ordinary 10 ns system
cycle. The dedicated +90-degree ROM clock and its private ports are removed;
SDRAM capture remains at +180 degrees. Microcode lead is preserved.

The CPU now executes each microword in two 100 MHz phases: register operands
and dispatch byte, then execute/writeback. PC, local ROM address and word/control
registers advance together on execute edges, preserving their relative
microcode alignment. The BIU and guest pin-clock generator remain full rate;
one strobe-valid edge prevents held commands from repeating queue/timer actions.
Completion capture remains full rate. This changes microinstruction throughput,
so unchanged pin frequency does not imply unchanged instruction cycle timing.
Run `python3 scripts/sim_fes_pcxt.py --suite cpu-timing` for checked NOP,
dependent ADD/ADC, branch and REP-copy timing workloads. The two-phase datapath
has no new clock domain or timing exception. See the
[CPU import notes](vendor/mcl86/README.md) for the proof and timing boundaries.

The same generator emits `mcl86_facts.vh` from every control word. Separate ADD
operand muxes retain only sources used by ADD words; other operations use the
full muxes. The adder sum and carry flags are consumed only for ADD, so this
specialization covers arbitrary guest firmware and register values. A reserved
jump-source hold is retained automatically if the image uses it. Exhaustive
table checks and symbolic operand/jump-condition checks cover these boundaries. ADD controls use independent rising-edge register processes,
preserved through synthesis, to separate their physical fanout from the full
logic-operation muxes. The copies capture the same ROM stage on the same edge;
symbolic read-alignment checks include both copies. DMA arbitration evaluates
all four circular priority masks in parallel; whole-DMA sequential equivalence
checks its state and interface against the original indexed priority loop. Mode validity and operation flags are decoded when the guest programs each channel. Twelve configuration bits remove mode-byte decoding from grant dispatch without changing arbitration, completion latency or reset behavior; sequential equivalence checks all DMA state and interface bits. Motherboard local reads retain their phase-8 sample in an independent byte register and copy it to the shared memory/ISA return register at phase-17 completion. The existing nine release clocks absorb this staging; CPU and DMA completion cycles are unchanged. An inductive check proves the original sampled byte and completed return value with the actual peripherals.

Only the full logic operand-1 mux uses `pcxt_logic_operand1_mux` and the
post-synthesis `synth/logic_operand1_map.v` map. Six parallel masked groups
feed one OR level; the measured BIU-return and immediate groups use fast
inputs. ADD-specific muxes remain independently inferred. The generic helper
and mapped LUT truth tables are equivalent for every data/select vector,
including non-one-hot selects; structural checks require two data levels.

The native map disconnects inputs whose AND product is provably zero from
compile-time pin constants, preserving the Boolean result while avoiding
spurious primitive timing arcs through unused status data or selectors.
Each ISA boundary preserves its one-bit request-reset register separately
from the payload process, preventing cross-slot reset-flop merging without
changing request/response latency. Whole-boundary equivalence proves all 80
original state/interface bits for arbitrary request, response and reset streams.
The ISA endpoint records commit eligibility when preparation completes and clears
it on commit or reset. This removes the repeated state decode from device
write enables without changing bus latency. Whole-endpoint temporal induction
checks all outputs against the original protocol for arbitrary streams after
reset, including tag changes, wait states and reset during a transaction.


The PIC import uses formally checked parallel
priority masks; the PIT uses direct one/two-step saturated binary/BCD decrements
with parallel digit borrows. Both imports document the formally checked changes
in their vendor READMEs. The PIT precomputes both encodings between sampled
falling count-clock edges; induction checks freshness whenever a candidate is
consumed. Scalar terminal-count flags are evaluated alongside data and selected
in parallel with next-count data. Guest-visible count/output updates and bus
timing are unchanged.
HDMI transmitter I2C uses the existing HPS peripheral bridge. The standalone
`build_fes_pcxt.py` profile remains an autonomous power-on diagnostic without
a runtime ABI, host session control or audio pins. The registered Quartus package
producer enables the standard execution mailbox, fixed video identity and
read-only floppy media; the keyboard interface remains disabled.

```sh
# From sources/misteross; no shared compiler cache environment for simulations.
make toolchain-fes-pcxt CACHE_ROOT=/absolute/toolchain-cache
python3 scripts/sim_fes_pcxt.py --suite system
python3 scripts/sim_fes_pcxt.py --suite system-mda
FES_GPU_LEASE_OWNER=pcxt-agent GPU_LEASE_MAX_PER_OWNER=2 python3 scripts/build_fes_pcxt.py --cache-root /absolute/toolchain-cache
FES_GPU_LEASE_OWNER=pcxt-agent GPU_LEASE_MAX_PER_OWNER=2 python3 scripts/build_fes_pcxt.py --cache-root /absolute/toolchain-cache PCXT_BUILD_ARGS='--mda --scanlines'
# Optional frontend/resource probe; no route:
python3 scripts/build_fes_pcxt.py --cache-root /absolute/toolchain-cache PCXT_BUILD_ARGS='--synth-only'
# From the FES repository root, prepare the installable diagnostic package:
python3 scripts/core_dev.py prepare --core fes.pcxt --output out/pcxt-package
```

The board producer authenticates `cores/fes-pcxt/toolchain.lock`, with reviewed
nextpnr main `2fa5ec4c`, Yosys `3a5accbd` and the Mistral timing-line fix `9ab65220`. Without
`CACHE_ROOT`, the toolchain and build commands both use the repository-local
`build/toolchain/fes-pcxt` install. Select
`PCXT_GPU_ROUTER=CUDA` for both preparation and build on a CUDA host; HIP and
CUDA routes have equal standing. Neither backend's timing pass substitutes for
the separate Quartus cross-check.

Each invocation retains a separate directory beneath
`build/diagnostic/fes-pcxt/{cga,mda}/{direct,scanlines}/<UTC-run>/`, including
assembled firmware, microcode, synthesis/netlist/resource counts and a
`diagnostic.json` source/tool receipt. The normal command attempts one seed-3
GPU route with a bounded 1800-second timeout and retains failures. Placement
uses fixed timing weight 2000 and criticality exponent 5, as in the existing
Coleco socket lane. Override the unsigned 64-bit seed with `PCXT_BUILD_ARGS='--seed 2'`
or direct `--seed 2`; there is no automatic placement-parameter or seed search. The common runner acquires `gpu-run` leases around routing phases, leaving
synthesis outside the lease. Invoke the producer directly; an outer `gpu-run`
is rejected. GPU queue waits do not consume the 1800-second route work budget.
The receipt hashes all generated microcode, bank ROMs and facts headers, and
records the selected seed, route command, timeout, wall duration, initial visibility
restrictions and the backend/device identities reported by nextpnr. A completed
route additionally writes `routed.json`, `timing.json`, `nextpnr.log` and
`diagnostic.rbf`. Timing reports retain up to 64 setup endpoint paths per
clock-domain pair so remaining failures can be traced without rerouting.
A route is diagnostic: SDRAM pad setup/hold, retention,
physical containment, package sealing and hardware acceptance require separate
qualification. There is no automatic Quartus fallback or kit programming.

```sh
python3 scripts/screen_fes_pcxt_uncertainty.py \
  --diagnostic /absolute/diagnostic \
  --quartus-comparison /absolute/clock-reference \
  --output /absolute/screen.json
```

This command
checks a retained producer report against explicit four-corner setup/hold
uncertainty increments from its matched Quartus clock reference. It verifies
the producer receipt, RBF identity and retained reference summaries, preserves
all endpoint identities, and returns nonzero on negative headroom or incomplete
coverage. The reference includes both PLLs; its small pixel counter is not the
actual pixel fabric. This is a host-only reference screen, not a native timing
model or whole-board acceptance. External IO, reset/CDC and physical board
requirements remain separate. No producer constraints or artifacts are changed.

The retained default-off CPU100/board50 candidate also has a host-only
synchronizer audit:

```sh
python3 scripts/audit_fes_pcxt_cdc.py \
  --routed /absolute/route/routed.json \
  --timing /absolute/route/timing.json \
  --receipt /absolute/route/receipt.json \
  --output /absolute/cdc-audit.json
```

It requires the successful immutable route receipt and final analogue timing.
The audit checks the actual twelve FFs, six two-stage chains, effective native
clock/reset/control pins, exclusive interstage fanout through identity buffers,
and paired positive setup/hold checks. It traces each reset tail through
combinational logic to the next FF inputs, requires same-domain checked timing
for every consumer, and rejects loops, missing rows and unsupported terminals.
Additional asynchronous clear consumers retain separate pulse/control obligations. This diagnostic changes no constraints or artifacts and
grants no timing exceptions, bundled-data protocol proof or physical/MTBF
acceptance. It applies to the retained board50 recipe, whose normal producer
routing remains gated on compiler integration.

The video mailbox has a separate digital ownership proof:

```sh
YOSYS=/absolute/pinned/yosys python3 -m unittest discover -s tests \
  -p test_pcxt_video_mailbox.py -v
```

It uses the real capture/publication and adoption logic, with arbitrary clock
interleavings and first-stage sampling stalls. Frame-tail position and output
SOF are overapproximated; RAM outputs are arbitrary. ABC's property-directed
reachability proves the bank identifier remains stable while pending, source
write-bank separation, acknowledgement/adoption ownership, and two prior pixel
samples of the identifier and toggle before adoption. A bounded witness covers
queuing, publication, adoption, acknowledgement and republication; changed-bank
and shortened-synchronizer mutations produce counterexamples. Both logical
domains reset together in this proof. Analogue metastability, reset edge timing,
physical bundled-data delay/skew and framebuffer contents remain unqualified.
The test uses `yosys-abc` alongside the pinned Yosys; `YOSYS_ABC` can select it
explicitly. No source RTL or producer constraints change.

No factory package selection or shared contract changes are made.

The CGA Direct build from committed `6df7e982d6e72e6d9d172d56d5a12e5899f46ad5`
reports a nextpnr-only setup/hold pass with the old pre-per-pin timer
(`9128800f`), but **fails the Quartus STA cross-check and is not timing-qualified**.
It uses 6,280 routed `MISTRAL_COMB` cells, 3,628 FFs,
333 of 553 M10Ks, two PLLs and no DSPs. Its retained run is
`build/diagnostic/fes-pcxt/cga/direct/20261018T204544Z-89efeae9/`.

| Domain | Required MHz | Old nextpnr MHz | Old setup WNS ns | Old hold WNS ns |
| --- | ---: | ---: | ---: | ---: |
| System | 100 | 100.412 | +0.041 | +0.756 |
| Pixel | 74.25 | 78.119 | +0.667 | +0.721 |
| Capture/ROM phase | 100 | 119.190 | +0.805 | +5.940 |

RBF SHA-256: `92277530eeb51924e54ed57e231f34aac7fee0c362086294757c7ed1922820d3`.
Timing-report SHA-256: `195393ce0ed5591951a48ae8f5b68a555861c9c4c301cd328f5083a0e37ea751`.
The receipt records clean source inputs and the HIP backend; all input hashes
match this source revision. The [Quartus 17.0.2 cross-check](https://github.com/DeanoC/fes/pull/836#issuecomment-6261235615)
reconstructs the sampled paths at the same sites, pins and routes, at Slow
1100 mV 100 °C. Its worst sampled system path reaches 93.23 MHz (−0.726 ns)
with nextpnr launch/setup values. With TimeQuest's own launch/setup cells,
the sampled pixel and ROM-phase paths reach 73.30 and 83.26 MHz respectively.
These are sampled-path upper bounds, not whole-design Quartus signoff; clock
uncertainty is also excluded. The old timer undercharges physical LUT pins
and M10K clock-to-output. Timing closure remains open in [#840](https://github.com/DeanoC/fes/issues/840).
The later RTL checkpoint `e799cd7a` passes all reported fabric setup/hold checks
on seeds 2 and 3 with reviewed nextpnr main `b9ffde2d` and Mistral `9ab65220`,
at unchanged clocks; seed 1 fails. The complete 17187-row timing inventory,
authenticated compiler replay and remaining physical boundaries are retained in
[the whole-board evidence](../../docs/validation/2026-10-22-pcxt-pad-pipeline/whole-board-pipeline.md).
External IO/reset timing, whole-design Quartus qualification, other display
profiles, SDRAM retention, package sealing and hardware acceptance remain open.

Both system suites run in the default simulation family. They check interrupted
cold loading, a pause near the end of loading, reset during an issued CPU
SDRAM read, reset release, both real DMA
directions, guest and physical refresh, two complete boots without firmware
reload, HOLD/mute and every pixel of two 720p output frames after capture settles.
CGA uses Direct and MDA uses Scanlines; each runs DQM delays 0/1/2. The framebuffer
registers its read address before M10K fanout. Each of its three 64 KiB banks
has a separate RAM read mux and local output register; bank selection then
occurs alongside palette selection. Bank/subpixel/raster fields have matching
latency, preserving the existing complete-frame ownership and pixel timing. PIC priority changes also have
all-input Yosys SAT checks, as do the PIT decrements. CPU checks reconstruct every
control-ROM word from its four byte lanes, prove the PC/call-stack/stall
projection and local address-copy invariant, prove read alignment across the
operand/execute phases under arbitrary resets and payloads, and compare real operand
selection against the imported equations;
`cpu_roms` derives four byte-lane images from the unchanged raw image. The display's fractional
clock enable compares the current phase directly with its wrap threshold and
updates with parallel add/subtract candidates. SAT proves this is cycle-exact
for every phase value and both CGA/MDA rates. The CPU pin-clock divider decodes its wrap threshold directly from phase bits
before selecting `phase+21` or `phase-199`. It predicts the next enable from
phase 178..198 and registers it on the phase-update edge. This preserves the
original enable cycle while removing the comparator from DMA control fanout.
Sequential equivalence checks phase, pin clock and guest enable against the
original add-then-compare logic:

```sh
YOSYS=/absolute/locked/yosys python3 -m unittest discover -s tests -p 'test_pcxt_*.py' -v
```

Existing video suites retain detailed frame-ownership, overrun/drop and malformed-source
coverage. Digital SDRAM simulation does not establish pad timing or retention.

## Implemented path

`pcxt_cpu.sv` selects the pinned [MCL86 import](vendor/mcl86/README.md) in 8088
mode, with a four-byte prefetch queue, original reserved FLAGS behavior and
byte-wide external transfers. The fabric clock must be 100 MHz; the synchronous
fractional pin clock is nominally 4.772727 MHz. `pcxt_cpu_bus.sv` translates its
address/data phases to byte transactions and holds READY/data until sampled.
It propagates LOCK, reports both interrupt-acknowledge bytes as locked, and
stops on a latched fabric fault until reset. `halted` stays asserted from the
halt bus cycle, including later prefetches, until the next non-fetch bus
status. There is no
8086 selector, turbo setting, 80186 extension or fake-286 FLAGS mode in FES.
DMA ownership uses transaction backpressure through READY. The live CPU
`bus_locked` output protects instruction gaps; pin-level HOLD/HLDA timing
is not emulated.

`pcxt_motherboard.sv` latches one CPU byte request and routes it to conventional
RAM (`00000–9FFFF`), read-only system ROM (`F0000–FFFFF`), a local device or ISA.
ROM writes complete without reaching storage. Video/option-memory apertures
remain on ISA; vacant slots return `FF`. Storage has VALID/READY admission and
one DONE response, with stable address/data until completion. Reset cancels
pending work and must also reset the downstream storage and ISA participants.
The optional `pcxt_memory.sv` backend above connects this port to SDRAM.

The MIT-licensed [KF8259](../fes-common/vendor/kf8259/README.md) and
[KF8253](../fes-common/vendor/kf8253/README.md) and [KF8255](vendor/kf8255/README.md) imports are pinned to the same upstream commit
as the CPU. The PIC and PIT live in `fes-common` because pc386 shares them; the
PC/XT instantiates the KF8253 as an 8253 (`READ_BACK = 0`). PIC ports `20–3F` use A0; PIT ports `40–5F` and PPI ports `60–7F`
use A1:0. All ignore
A15:10, matching the XT motherboard decode. INTA bypasses I/O decode and drives
the real PIC's two-pulse sequence. IRQ0 comes from PIT channel 0, IRQ1 from the
XT keyboard receiver, and IRQ2–7 from ISA. The timer clock divides CPU guest
clock enables by four (nominal 1.193182 MHz); it continues through CPU waits and
HLT. All counter pins are synchronous data, not generated RTL clock domains.
Channels 0/1 have gates tied high; channel 2 uses PPI port B bit 0 and all three
outputs are exposed. Channel 1 requests DMA0 refresh through a latched edge.
Local reads/writes have one setup, eight active and nine release fabric clocks;
CS/address remain stable through release so imported bus-edge detectors retire
side effects before DONE. These timings are development integration choices,
not a claim of original motherboard wait-state equivalence.

`pcxt_xtio.sv` implements the IBM 5160 PPI wiring. Normal firmware writes `99`
to port `63` for mode 0 (A/C inputs, B output). Mode-set writes clear output
latches, including repeated values; port C also supports bit set/reset (BSR).
Imported handshake modes are outside this slice's qualification.

| Port | Mode-0 function |
| --- | --- |
| `60` | Received scan byte; reading does not clear IRQ1. Output mode reads the diagnostic output latch. |
| `61` | PB0: PIT2 gate; PB1: speaker AND gate; PB2: spare; PB3: select switch high nibble; PB4/5: active-low parity/channel-check enables; PB6: release keyboard clock; PB7: clear keyboard receiver/IRQ1. |
| `62` | PC0–3: selected switch nibble; PC4: zero; PC5: PIT2 output; PC6/7: external channel/parity status. |
| `63` | Mode control or port-C BSR write; reads are unspecified. |

Switch levels are an eight-bit profile input, SW1 in bit 0. Bit 0 set is SW1
released (normal POST). Bit 1 set is SW2 released (8087 installed). Bits 3:2
are SW4/SW3 (`11` is 640 KiB and `00` is 256 KiB on a 256/640K board). Bits
5:4 are SW6/SW5 (`10` is CGA 80-column, `11` is MDA). Bits 7:6 are SW8/SW7
(`00` is one floppy, `01` is two). The machine fixture executes `2D` on every
memory-wait run and `73` once (normal POST, 8087, 256 KiB, MDA, two floppies).
Error status
inputs and checking-enable outputs are wiring hooks: parity generation,
error latches and NMI integration remain unimplemented. PPI reset selects all
inputs; the wrapper then mutes the speaker, disables checking and inhibits the
keyboard clock. While PB7 is high, disabled keyboard data reads deterministic
`FF`; this does not model floating-bus residue.

The receiver synchronizes clock/data into 100 MHz fabric and samples falling
clock edges: a start one followed by eight LSB-first bits, without parity.
Leading zero RTS pulses are ignored. IRQ1 remains asserted until PB7 clears
it; the receiver pulls the **data** line low for busy/CTS and holds the byte
against overwrite. PB6 controls clock inhibit independently. PB7 itself does
not clamp clock or data: a cable keyboard that only watches those lines can
still shift a byte into a cleared receiver, and that byte raises no IRQ1.
The companion
`pcxt_xt_keyboard.sv` accepts a set-1 byte using VALID/READY, waits 80 us after
CTS, and clocks bits with 40 us high/low phases and at least 3 us data hold.
Clock inhibit pauses transmission. READY also stays low while PB7 holds the
receiver clear, so the frontend does not retire a byte the receiver cannot
collect. A byte already accepted still finishes its frame. Reset cancels a partial frame. This is a
single-byte transport. The DOS-only BAT responder sends the self-test byte
after the reset handshake; HID mapping, extended-key policy, typematic
generation and event queues remain future frontend work.
It is not an AT 8042 or a PS/2 protocol endpoint.

`pcxt_speaker.sv` integrates the gated PIT2 waveform over each 48 kHz sample
window (2083/2084 fabric clocks), then applies a roughly 30 Hz DC blocker.
Its signed 16-bit mono sample/valid outputs are available to a future board
adapter; they are not connected to the production audio shell. Fixed gain
introduces at most four counts of window-length jitter. This development
filter has no measured analog-speaker fidelity claim.

[`fes_dma8237.sv`](../fes-common/rtl/fes_dma8237.sv), shared with pc386, is
original transaction-native RTL; its default parameters implement the XT subset
of the [Intel 8237A](https://read.seas.harvard.edu/~kohler/class/04f-aos/ref/hardware/8237A.pdf).
Controller registers occupy `00–1F` (A3:0), with A15:10 ignored. `80–9F`
(A1:0) selects write-only four-bit XT page latches; reads return `FF`.
Page index 1 selects DMA2, index 2 DMA3, and index 3 DMA1/refresh. Index 0
is writable but unused. Low 16-bit address wrap never carries into the page.

The controller implements shared low/high byte selection, current/base
address and count, read-clear TC status, masks, command/disable, mode and
software-request writes, increment/decrement, auto-init, and fixed/rotating
priority. A new service resolves priority after CPU ownership is released,
so a higher-priority request arriving during a CPU wait or LOCK can win.
Demand rechecks DREQ before each byte; block runs to TC;
single yields to a waiting CPU request between bytes. Software requests are
serviced in block mode as Intel specifies. Count `FFFF` means 65,536 bytes.
Verify advances address/count without memory or peripheral strobes. Master
clear resets status, command, request, temporary, byte pointer and masks while
preserving address/count/mode/page programming. Undefined register reads use
`FF`; the temporary register reads zero because memory-to-memory is unsupported.

At a grant, the controller snapshots the candidate address and terminal count
before checking whether service is allowed. Rejected-service snapshots stay
inactive; every valid service overwrites them before use. The motherboard
consumes payload and TC only with a valid request. This removes the service-mode
and priority decision from the payload-register enables without adding a cycle.
Symbolic checks compare all other state and observable outputs and preserve
snapshot equality throughout active service; wrong address/TC mutations reject.

DMA2/3 exercise real registered ISA endpoints; DMA1 uses the same contract
(the current fixture has no DMA1 card). Memory-to-device first reads memory,
then commits the peripheral write; device-to-memory commits the peripheral
read before writing memory. AEN, channel and final-byte TC travel through the
existing ISA contract. Both halves complete before address/count retirement.
A byte receives at least five guest clock enables, plus transaction waits;
this is a development timing model, not original 8237 pin timing. Compressed/
extended-write and DACK polarity bits do not alter the abstract handshakes.
DREQ polarity is the 8237 pin sense for every channel, applied before
masking and priority resolution. Channel 0's pin is the refresh latch.

The router grants DMA between completed CPU requests and preserves ownership
for both halves of a byte. Continuous CPU LOCK and a separate two-INTA-pulse
latch exclude grants. CPU completion never pulses for a DMA request. Faults
halt DMA without advancing its address/count or retrying a committed device
operation. Reset cancels outstanding work; master clear also clears a DMA
fault. A previously committed device read or memory write is not rolled back.
Memory-to-memory, cascade, illegal transfer type and DMA0 device-to-memory
requests fail with diagnostic fault 3; external EOP and ISA bus mastering
remain unimplemented.

PIT1 rising edges latch refresh requests until DMA0 acknowledges service.
DMA0 read mode performs a memory read without an ISA peripheral packet;
its auto-init single mode continues while the CPU waits or halts. Edges during
a delayed refresh can latch one further request. This supplies the guest-visible
refresh bus activity; the separate physical SDRAM controller owns its actual
SDRAM refresh commands and retention qualification.

The fractional master-dot accumulator registers a prediction of the next wrap
flag. Reset establishes zero phase and no pulse; the inductive phase-range and
wrap-identity check proves every CGA/MDA pulse stays on its original cycle.
The downstream CRTC enables therefore start at a register rather than a wide
phase comparison. Threshold mutations reject for both dot rates. No guest or
fabric clock frequency, raster period or renderer latency changes.

CGA and MDA are separate ISA expansion cards. Each owns its VRAM, memory
and register decode, and raster state on the registered endpoint/commit
boundary. The motherboard routes the `B8000–BFFFF` and `B0000–B7FFF`
apertures to ISA and contains no display registers. See
[ISA display cards](#isa-display-cards). Physical socket placement,
production HDMI egress and kit hardware acceptance remain unqualified.

Each card registers its rendering inputs before colour selection. Pixel enable,
DE, HS/VS and SOF/EOL travel with the same snapshot, adding one 100 MHz cycle
to the complete output stream without changing raster cadence. MDA status uses
a separate equivalent nonzero predicate at its original bus-visible cycle.
VRAM byte-write eligibility is registered during ISA prepare, before commit;
this adds no bus cycle and keeps address/type decode off the write-enable path.
Symbolic checks cover arbitrary render inputs/resets/markers and every committed
request, including a rejected undelayed-frame-marker mutation.

`pcxt_isa_bridge.sv` latches a byte transaction, collects registered card decode
responses, waits for READY and sends one commit. `pcxt_isa_boundary.sv` supplies
one request and one response register, matching the existing linkable-slot
pipeline model. `pcxt_isa_endpoint.sv` separates device preparation from side
effects and acknowledges a commit exactly once. Vacant slots read as `FF`;
conflicting or stale card responses fault before commit. An owner that
withdraws after COMMIT also faults and drains, without a second commit. IRQ
and DRQ remain live while the CPU is waiting.

The [shared fabric contract](../../../mister-packages/docs/pcxt-isa.md) defines
the wire fields and timing. It is emitted from mister-packages, not duplicated
in the core. The portable boundaries have no BEL reservations; they do not
establish CRAM containment or admit PC/XT cards into the production linker.

`expansions/pcxt_isa_probe.sv` is an original diagnostic card with eight I/O
registers, 16 bytes of scratch memory and a DMA loopback byte. Its configurable
I/O mask exercises partial-address aliases. Default mappings are ports
`300–307` (ten-bit decode), memory `C8000–C800F`, IRQ5 and DMA2.

| Register offset | Read | Write |
| --- | --- | --- |
| 0 | Data byte | Replace data byte |
| 1 | Flags | Bit 0 raises IRQ; bit 1 raises DRQ |
| 2 | Read count before this read | Ignored |
| 3 | Write count | Ignored |
| 4 | Fabric wait cycles (0–255) | Set wait cycles for subsequent transfers |
| 5 | DMA byte | Set DMA byte |
| 6 | Committed DMA-byte count | Ignored |
| 7 | DMA terminal-count count | Ignored |

All I/O writes increment the write counter, including writes to read-only
registers; all I/O reads increment the read counter at commit. Counters wrap
at 256. Memory transfers and DMA use separate state. Terminal count is counted
once per committed DMA byte that carries it. The card is a test device, not a
historical ISA adapter.

## Validation

From `sources/misteross`:

```sh
make sim-fes-pcxt
```

Verilator runs the bridge, all eight boundaries and real probe endpoints in
slots 1 and 8. The C++ driver checks I/O aliases, full 16-bit I/O decode, AEN,
memory, unmapped accesses, delayed READY, held VALID, IRQ/DRQ, DMA direction and
terminal count. Fault injection checks duplicate claims, stale transaction tags
and missing decode acknowledgements, a claimant disappearing during a wait, and
the selected owner disappearing after COMMIT. A deterministic randomized memory test
uses an independent scoreboard. Reset is swept across 60 positions in a delayed
transfer. Each position checks that a commit already pending at the card does
not pulse on the reset edge, and that reset clears pending operations without
a phantom completion.

The CPU suite assembles the original `sim/diagnostic.S` with GNU binutils
(`as` and `ld`, x86 target, `.arch i8086`). It starts at physical `FFFF0` and
checks integer arithmetic, multiply/divide, CALL/RET, REP word strings at odd
addresses, segment and 20-bit address wrap, LOCK, native FLAGS, ISA word I/O,
port-FFFF wrap, partial decode, option memory and unmapped reads. Probe counters
check exactly-once side effects with 160 fabric clocks of card wait. Software
INT/IRET, two locked INTA cycles, a halt that stays asserted until wakeup, and
NMI use a testbench vector source;
the CPU-only fixture deliberately retains this independent stimulus. Port 80h
is a progress sink in that fixture only.
Seven complete runs vary memory latency/backpressure and recover from reset
during an outstanding fetch and an ISA conflict. No BIOS calls are intercepted.

The motherboard bus suite checks RAM/ROM boundaries, ROM write suppression,
ISA holes and option memory, decode aliases, PIC masking/IRR/ISR, priority,
specific/non-specific EOI, rotation, poll, automatic EOI, level/edge requests,
spurious IRQ7 and real ISA IRQ5 delivery. Timer tests cover all six modes:
mode-0 gating, latched two-byte reads, zero reload (65536), BCD borrow,
mode-2/3 frequency and odd/even duty cycles, mode-1 one-shot and mode-4/5
strobes. Channel 1 counts and channel 0 continues during a long storage wait.
PPI tests cover mode/direction/BSR writes, repeated-mode latch reset, switch
selection, port aliases, check-status hooks, keyboard make/break and all-zero/
all-one bytes, busy overwrite rejection, acknowledge, clock inhibit and partial
frame reset. Holding port `61` at `C0` while offering scan code `1E` must not
assert READY or IRQ1; releasing PB7 then delivers that byte. Speaker tests cover PIT gating, port-C feedback, 48 kHz cadence,
DC removal and integration of modulation faster than the sample rate.
The separate keyboard transport suite checks independent bit order and
setup/hold timing, CTS backpressure, mid-frame clock inhibit and reset phases.
Reset tests cancel an outstanding storage write and sweep all local bus phases.
These are bounded integration tests; cascade, special mask mode, all counter
reload/gate corner cases and exact original-silicon timing are not qualified.

The DMA suite checks both directions with delayed card/memory responses,
all service modes, verify, all-mask and clear-mask writes, shared byte selection,
page aliases/mappings and 64 KiB wrap, including an independent 65,536-byte
memory scoreboard. It checks auto-init, fixed/rotating priority, software
requests, DREQ polarity/withdrawal, LOCK and paired INTA exclusion, CPU fairness,
refresh cadence, inverted DREQ sense on that latch and on external DRQ,
a higher-priority request arriving before grant, DREQ
withdrawal between demand bytes, first/second-half faults and reset across
pending halves.
Unsupported bus modes must fault without committing a byte.

The integrated machine suite boots the original `sim/motherboard.S` ROM on the
8088. It programs PIC/PIT, wakes from HLT for three timer IRQ0 handlers, then
services ISA IRQ5 and clears its source. It programs the PPI, reads both switch
nibbles, plays/mutes a PIT2 speaker tone and handles make/break bytes via two
IRQ1 handlers with port-61 acknowledgement. It also transfers eight bytes
from one ISA card to RAM and four distinct RAM bytes to the second card,
checks TC, and executes 40 locked word increments while periodic DMA0 refresh
competes with CPU instructions and interrupts. The testbench supplies RAM/ROM and
set-1 bytes to the timed serializer, checks both locked INTA cycles and their vectors, and observes success in RAM.
Three runs vary storage latency/backpressure on switch profile `2D`. A fourth
run reads profile `73` with no added memory wait. Switch selection does not
depend on that wait. No host-supplied vectors or I/O
shortcuts participate in this fixture.

Verilator lint is fatal. `sim/cpu.vlt` and `sim/chipset.vlt` scope exceptions to
imported files and the fixtures' shared reset (asynchronous in imported chips,
synchronous in FES RTL). Run a suite alone with
`python3 scripts/sim_fes_pcxt.py --suite isa`, `--suite cpu`,
`--suite keyboard`, `--suite motherboard`, `--suite dma`, `--suite machine`,
`--suite display` or `--suite display-machine`.
Outputs live under `build/sim/fes-pcxt/{isa,cpu,keyboard,motherboard,dma,machine,display,display-machine}/`. The display suite checks full raster geometry, cadence, sync widths and every
pixel against independent byte-memory scoreboards, plus palette writes during a
scan line, AEN, aliases, mirrors, blink, cursor, light pen and reset phases. The
separate display-machine suite boots `sim/display.S` on the 8088 through the
motherboard, programs CGA then MDA with split word I/O, fills/verifies both VRAMs,
polls real status ports and verifies emitted frames with 0/137-clock storage waits.
The parent affected-test planner and
CI simulation matrix include this simulation-only family. Passing these tests
is host evidence only. Physical socket placement/routing, exact-shell archives,
firmware composition and hardware acceptance remain required before claiming a
physically qualified PC/XT package. The existing closed linker layouts are unchanged.

The optional diagnostic synthesis command uses the same authenticated toolchain
as the existing Coleco producer:

```sh
make synth-fes-pcxt-isa CACHE_ROOT=/absolute/misteross-toolchains
python3 scripts/synth_fes_pcxt.py --suite cpu --cache-root /absolute/misteross-toolchains
python3 scripts/synth_fes_pcxt.py --suite machine --cache-root /absolute/misteross-toolchains
python3 scripts/synth_fes_pcxt.py --suite display --cache-root /absolute/misteross-toolchains
```

It synthesizes the simulation fixture (two probe cards, eight portable slots,
fault-injection controls) and writes `build/synth/fes-pcxt/synth.json`,
`yosys.log` and `diagnostic.json` with source hashes and compiler identity.
The CPU variant includes the CPU and external memory ports, writing the same
files beneath `build/synth/fes-pcxt/cpu/`. The machine variant adds the
motherboard/PIC/PIT/PPI, DMA/refresh, keyboard serializer and speaker under
`build/synth/fes-pcxt/machine/`. The receipt records the locked compiler identity
and mapped cell counts.
Both CPU and machine synthesis regenerate the original microcode and its
derived operand-select ROM through the same verified helper as simulation.
CPU and machine fixtures use the same 100 MHz system edge for the CPU and
registered microcode reads; no separate ROM clock input is required.
The display variant synthesizes both ISA cards, their VRAM and the registered
backplane into `build/synth/fes-pcxt/display/`, with external font ROM inputs.
None of these variants synthesizes the testbench RAM or diagnostic system ROM.
The cell counts describe these fixtures only.
These commands perform no placement,
routing, package sealing or deployment.
