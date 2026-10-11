# FES Atari 520ST

`fes.atari-st` is FES's first 16-bit computer. The functional motherboard
uses a full 8 MHz 68000, 512 KiB RAM, a pluggable 192 KiB ROM, MFP timers and
interrupts, keyboard and relative mouse ACIA/IKBD, YM2149 audio and a writable floppy/DMA path.
An unmodified EmuTOS 1.4 image boots in host simulation. Board synthesis,
routing and hardware acceptance are separate qualifications.

The source-build owner is [misteross](../../docs/cores.md). Shared host media
semantics live in [mister-packages](../../../mister-packages/docs/computer-io.md).

## CPU, memory and reset

The shared [FX68K](../fes-common/rtl/fx68k/README.md) retains exact upstream
bytes and GPL-3.0-or-later licensing. Alternating fractional phase enables
produce an average 8 MHz CPU in the 52.224 MHz system domain. CPU bus
transactions latch address, function code, write data and big-endian byte
strobes. A held request completes once; DTACK/BERR persists until AS or both
data strobes release. Rearming between strobes supports TAS. An unanswered
request faults after 128 CPU half cycles. The CPU RESET instruction resets
peripherals through the same exported reset signal as host Hold.

`st_memory.sv` shares the existing addon-SDRAM controller among CPU, scanout,
floppy DMA, upload and media reads. Round-robin arbitration bounds contention.
The MiSTer addon wires chip DQML/DQMH to A11/A12. The shared controller keeps
the full row during ACTIVATE, then places byte masks on those shared pins
before the column command and clears them for reads. The separate logical
DQM outputs alone cannot mask writes on this board.
The board selects phase-based grants from the native CPU/raster counter:
phase 0 serves CPU reads and other non-video clients, phase 1 CPU writes,
and phase 2 video. A grant that bypasses an earlier pending non-video client
preserves that client's queue position. Protected RAM transactions launch only
with valid byte strobes. Other motherboard targets retain registered dispatch.

One CPU write can be acknowledged after its address, data and masks are retained.
Physical completion and recovery precede subsequent clients. Request withdrawal,
changed inactive CPU pins and warm reset preserve an accepted write and suppress
stale completion. This is one ordered command, with no read cache. Hardware Hold
leaves the arbiter running for media/upload work. CPU RESET preserves RAM.

Runtime refresh selects four wait counts, leaving six chip clocks (114.9 ns at
52.224 MHz) before the next command, above the ISSI IS42S16320D's 60 ns period.
RAM and media use rank zero, so runtime refresh selects that rank; initialization
keeps the conservative two-rank sequence. The 335-clock schedule uses phase 2.
A pending refresh defers a late physical write that could occupy its admission
window. `raster_reset` opens the window while the native counter is held.
Slot refresh requires phase slots and single-rank refresh; posted writes require
phase slots and early completion. Board instances and Slang roots select the
same policy explicitly. Shared controller defaults retain RAM Tester behavior.

The ST consumes the first rate-0 DDR rising sample. The board retains its fabric
pad capture and the simulation fixture models the matching DDIO edge.
`make sim-fes-atari-st-memory` checks byte masks, physical posted-write commits
and following read order, cancellation, held requests, warm reset, all five
clients and every fabric offset of no-video read/write slots. It retains the
exclusive 180-clock fairness limit and checks physical refresh intervals against
408 clocks at 52.224 MHz. Registered-input and late-completion profiles remain
focused timing comparisons.

`make sim-fes-atari-st-ram-bus` runs authored firmware on FX68K with physical
SDRAM/DDIO and independent-clock video. The selected policy measures 511 byte
write intervals of 16 CPU cycles and 510 read intervals of 16 plus one of 17.
It checks both byte lanes, TAS rearming and RESET retention. Its RAM-resident
NOP/DBF workload checks 12,299 palette intervals of 12 cycles and 300 of 512
while video fetches continue without underruns. These host regressions do not
establish original BIG compatibility, routed timing or kit acceptance.

Physical halfword offsets `$00000–$3FFFF` contain RAM; `$40000–$A67FF` contain
the separate disk buffer (up to 820 KiB). Masks select even high or odd low bytes.
The controller preserves its packed row/bank/column mapping.

The original ST MMU configures two logical banks. This machine has one
physical 512 KiB bank and an empty second bank. `$FF8001` changes logical bank
sizes and the CPU's multiplexed address mapping, including the row/column
aliases ROMs use to discover RAM. Empty-bank reads acknowledge `$FFFF` and
writes have no effect. The remainder of the original ST RAM decode window
below `$400000` also acknowledges accesses and discards writes, even beyond
the configured logical bank sizes. Empty-memory reads use the existing all-ones
model; the original STF floating data-bus value is not reproduced. Addresses
from `$400000` upward remain unmapped until a separately decoded cartridge,
ROM or peripheral window. The populated bank follows the original ST wiring,
rather than the later STe mapping. In the normal `$04` configuration:

| Address | Behavior |
| --- | --- |
| `$000000–$000007` | Supervisor ROM-vector alias; writes fault |
| `$000008–$07FFFF` | RAM; user access below `$800` faults |
| `$080000–$3FFFFF` | Unpopulated RAM: reads acknowledge `$FFFF`, writes discarded |
| `$FA0000–$FBFFFF` | Read-only cartridge connector; empty reads acknowledge `$FFFF` |
| `$FC0000–$FEFFFF` | Exact 196,608-byte firmware image; writes fault |
| `$FF8001` | Logical bank configuration |
| `$FF8201/03` | Screen-base high/middle bytes, aligned to 256 bytes |
| `$FF8205/07/09` | Read-only functional scan counter |
| `$FF820A`, `$FF8260` | Sync mode and low/medium/high resolution |
| `$FF8240–$FF825F` | Sixteen palette words, masked to `$0777` |
| `$FF8604/06/09/0B/0D` | WD1772/DMA registers |
| `$FF8800/02` | YM2149 address/read and data-write registers |
| `$FFFA01–$FFFA2F` | MFP registers on the low byte lane |
| `$FFFC00/02`, `$FFFC04/06` | Keyboard and MIDI ACIAs on the high byte lane |
| Other `$FFxxxx` | Supervisor expansion MMIO |
| Other addresses | Bus error |

Primary references are Atari's [520ST service manual](https://www.atarimania.com/documents/atari_520st_service_manual.pdf),
Motorola's [68000 manual](https://www.nxp.com/docs/en/reference-manual/MC68000UM.pdf)
and the pinned [EmuTOS memory initialization](https://github.com/emutos/emutos/blob/978e37569bff95841e42675d11fcc6799aad8483/bios/memory.S).
Cycle-exact GLUE/MMU behavior beyond the selected RAM grant policy remains
outside this implementation.

## Peripherals

`st_mfp.sv` implements GPIP, edge selection, direction, interrupt enable,
pending, in-service, mask and vector registers, authentic priority and
software/automatic EOI, and timers A/B/C/D. MFP IRQ6 uses a supplied vector;
other IRQ levels use the CPU's VPA autovectors. Its independent 2.4576 MHz
clock enable gives EmuTOS's timer C `/64 × 192` exactly 200 Hz. Native
VBL/HBL and display enable share the CPU's phase-2 enable, independently of
HDMI. PAL uses 313 × 512 CPU cycles, NTSC 263 × 508, and monochrome
501 × 224. At the retained nominal 8 MHz these are approximately
49.920/59.878/71.286 Hz; the original PAL/NTSC crystal frequencies are not
reproduced. Timer B receives active display enable, including opened bottom
lines, so blank lines do not contribute events. The live shifter address counter advances by two bytes per four native CPU
cycles during DMA, including opened bottom lines. Its read visibility follows
Hatari 2.5.0's eight-cycle offset; blanking holds the address and VBL reloads
the aligned screen base. Exact MMU arbitration and horizontal border tricks
remain unimplemented.
The MFP UART has disconnected RX and timed TX status, without a physical serial port.

`st_io` has an optional `MFP_WAIT_STATES` access-timing probe, default zero.
It delays the peripheral request itself, including read sampling and writes,
in native CPU cycles. The simulator can select four states with
`--mfp-wait-states 4`, following
[Hatari 2.5.0's MFP accesses](https://github.com/hatari/hatari/blob/v2.5.0/src/mfp.c#L2468).
`make sim-fes-atari-st-mfp-bus` checks the real 68000's read/write instruction
timing across its fractional-clock phases and samples a Timer B edge during
the delay. It also checks held reads, cancelled writes, reset, byte lanes and
unaffected PSG accesses. The default production timing remains unchanged;
this option alone does not establish a fix for BIG's menu flashes.

`st_acia.sv` models MC6850 registers, timed byte transport, IRQs and receive
errors. The keyboard ACIA connects to original `st_ikbd.sv` protocol logic:
reset acknowledgement, HID make/break, mouse commands, joystick events,
inquiries and BCD calendar. Its 64-byte response FIFO writes packets through
eight address banks, preserving atomic enqueue and simultaneous receive.
Complete HID rows settle for 1 ms before use so
keys and separately delivered modifiers form one snapshot. Controller port 0
maps to ST joystick 1 and controller port 1 to ST joystick 0. Mouse and
joystick commands select ownership of the shared ST port 0. Relative mouse input uses the optional `fes.mouse.relative` mailbox extension;
local USB/SDL/evdev and host browser input deliver packets through the same
IKBD/ACIA path. Button state survives source merging; movement is never
replayed after disconnection or an uncertain acknowledgement. Physical UART/MIDI pins,
HD6301 program loading, monitor sampling and accelerated cursor modes are
absent; unsupported command modes consume their parameters safely.

`st_ym2149.sv` reuses the shared AY engine with a YM2149 option: sixteen
registers, 32-step envelopes, a 2 MHz enable and 48 kHz PCM. Port A drives
floppy side and active-low drive selects. Its DAC is a bounded approximation;
it does not reproduce analog filtering. Board transport uses the existing
signed stereo PCM/I2S path, duplicating the mono chip into both channels.

`st_floppy.sv` implements original WD1772 Type I positioning, Type II sector
reads and writes, force interrupts and ST DMA. Drive A accepts raw `.st`
images with 80–82 tracks, one or two sides, and nine or ten 512-byte sectors
per track. The required `fes.media.atari-st-floppy-geometry` 1.0 extension
opts into these twelve uniquely sized shapes; the base interface retains its
exact 737,280-byte contract for older packages. Committed upload length fixes
physical geometry even if the guest changes its boot BPB. Drive B is absent. Requests stay stable under storage/DMA stalls; DMA stays inside RAM.
The writable extension clears write protect. A complete sector is staged from
RAM before publication to the disk buffer; once publication starts it drains
through warm reset or force interrupt. Begin and Eject reject while collection
or an accepted sector commit is busy, including volatile disks; an explicit
later replacement cannot overlap the old image's writes. Freeze fences new writers and drains
accepted work before whole-image capture. Raw development loads stay volatile;
library loads explicitly bind durable data to the game and immutable base disk.
The runtime publishes a checksummed full image through a synced atomic rename,
then authorizes eject/replacement. A save failure retains the same frozen owner.
Motor/index behavior is functional; flux, CRC/deleted-sector metadata, formatting,
read-address/track, PIO streams and ACSI are absent.

## Pluggable ROM, expansion and video

The firmware socket accepts exactly 192 KiB. The board ROM is a synchronous
M10K implementation whose blank base can be linked using the existing
format-3 ROM map; no proprietary Atari ROM is distributed. EmuTOS is a
separate, freely available GPL test input.

The expansion connector exports a stable 23-bit word address, 16-bit data,
byte lanes, direction, function code, reset, phase enables and IACK level.
Responses contain read data, ACK/BERR, encoded IRQ and card presence.
`st_expansion_socket.sv` registers both boundaries in the reserved
`fes.atari-st-bus.socket/1` rectangle. The shared linker confines slot 1 to
that rectangle. The optional manifest interface is
`fes.expansion.atari-st-bus` 1.0; it grants no GP capability bit.
`expansions/st_probe.sv` exercises cartridge data, MMIO, wait states, faults
and byte lanes. `expansions/probe_cart.sv` wraps it in the registered connector.
`scripts/build_atari_st_slot_card.py` synthesizes the card onto the frozen
shell, checks all clock pins and timing, and rejects any change outside the
socket CRAM rectangle before publishing an exact-shell expansion archive.

`st_video.sv` reads interleaved RAM bitplanes: 320×200 four-plane low,
640×200 two-plane medium and 640×400 monochrome. The fixed 720p60 output uses
integer nearest-neighbor scaling: native low 2×2, indexed medium 2×3,
monochrome 2×1. The focused indexed low fallback retains 4×3. High mode
supports palette-bit inversion; color borders use palette entry 0.
`st_video_adapter.sv` defaults to native low-resolution capture. In the
52.224 MHz system domain, `st_native_low_video.sv` prefetches alternating
80-word RAM rows and selects the live palette at each nominal 8 MHz pixel.
Three RGB333 banks hold up to 320×247 pixels. Each bank uses 78 independent
1024-word chunks, avoiding the mapper's less compact deep one-bit layout.
Registered chunk selection aligns the synchronous words with the original
one-clock read boundary; frame publication and pixel capacity are unchanged.
The banks cross to the 74.25 MHz pixel domain through
publish/release toggles. Only complete native frames are published; the HDMI
reader pins one bank until output SOF and selects the newest available frame.
This repeats or drops complete source frames when native and output rates differ.
Registered RAM write data and a two-pixel address lookahead keep native RGB
sampling and scaled output aligned while shortening paths into the buffers.
Ordinary and opened-bottom frames use the same centred 2×2 canvas, preserving
square native pixels and image position. All 247 PAL or 226 NTSC rows remain visible.
Height travels with each immutable bank and changes only at output SOF.
`st_native_border.sv` records lossless palette-zero runs across the visible
border raster in those same three owned banks. Every row has an initial colour
and run offset/count, so repeated HDMI rows replay within-line changes. The
8192-event budget accommodates BIG psycho screen 3's dense palette stream;
the bound remains explicit: overflow increments capture underruns and
rejects the entire frame. HOLD and consumer backpressure retain the same bank
ownership rules for border runs and display pixels. Every low-resolution frame
uses a centred 416×276 PAL or 416×255 NTSC canvas at 2×2 scaling, with black
outside it. Border changes do not alter scaling or image position. The
canvas includes the 29 top-border rows and 48 pixels at each side of the
ordinary display. These are live palette-zero borders; they do not add top or
horizontal display-enable opening.
A blanking-time row counter avoids a vertical-coordinate multiply on the read path.
RGB and black-line validity have separate write registers. Three-bit publication
numbers order the bounded pending bank set across wrap and consumer pauses.
A missing RAM row displays black for its whole line; HOLD aborts an unfinished
capture, and an invalid base never wraps into populated RAM.

The [native palette qualification record](../../../../docs/validation/2026-10-08-atari-st-native-palette.md)
binds the sealed shell/Direct/Scanlines pair and bounded Kit A test. The B
scroller shows rainbow bands through the prepared Direct part; menu flashing
and exact native timing remain open. The original image and populated menu
are restored after the diagnostic.

Medium and monochrome retain the indexed double-line-cache renderer and its
coherent configuration bundle, activated at output frame boundaries. Their
palette remains fixed for that output frame. Synchronous cache reads and
ownership tags meet the original plane-capture edges. A held, fair arbiter
shares the existing video-memory port between the two renderers.
The indexed renderer compares constant current/next-line windows before mode
selection, preserving its pixel and lookup cycles while shortening timing paths.

The reduced native timing uses ordinary PAL lines 63–262 with DE cycles
56–375, and NTSC lines 34–233 with DE cycles 52–371, following the ordinary
[Hatari 2.5.0 timing table](https://github.com/hatari/hatari/blob/v2.5.0/src/video.c).
VBL's frame/capture pulse remains at the native frame boundary. IRQ4 asserts
60 CPU cycles later, matching the same STF WS1 wake-up timing selected for
line and bottom-stop samples. It remains pending until IACK; reset cancels a
scheduled event. This is independent of HDMI frame publication.

The MFP's Timer B input follows both DE edges by 24 CPU cycles, using
[Hatari's Timer B offset](https://github.com/hatari/hatari/blob/v2.5.0/src/includes/video.h).
DE mode settings are sampled at line/frame boundaries so brief writes do not
create extra display-enable edges. Color line length samples sync separately
at STF WS1 cycle 54: a bottom-stop pulse restored early on the following
line preserves the ordinary HBL and frame period. The vertical bottom-stop condition also
samples live sync at cycle 502 on the last ordinary color line, using the
STF WS1 timing table. Opposite sync extends PAL DE through line 309, or NTSC
DE through line 259, and clears at the next frame. This lets Timer B handlers
continue through the opened bottom region. The native RGB buffer includes
those extra lines. Publication waits until all potentially opened rows finish;
ordinary captures publish 200 rows. Only the first possible border row is
prefetched before DE confirms opening. The remaining mode samples are approximate, and prefetching
does not reproduce exact MMU/shifter arbitration, mid-line base writes,
horizontal or top-border opening.

Video uses the existing [RGB888 part contract](../../../mister-packages/docs/video-parts.md)
and shared direct/scanline implementations through two registered boundaries.
`st_video_socket.sv` provides the optional frozen raster socket
`fes.atari-st-video.socket/1`. The physical producer layout is
`fes.atari-st-video.parts/2`; the public Go/C++ composition layout remains
`fes.atari-st-video.parts/1`.
An empty socket uses the built-in Direct output; independently sealed Direct
and Scanlines archives bind the exact shell package and may coexist with the
CPU expansion and linked firmware. The socket owns 93 pinned boundary/clock
FFs with paired route-through buffers in LAB24..28 rows41..58. Its strict
half-open CRAM rectangle is (1769,3442)–(2806,5162). Full M10K configuration
footprints remain outside both CPU/video fences; RAM guards include rows40/59.
The part producer requires the frozen shell's manifest, RBF and ROM map to
match the sealed package. `routed.json` and `socket.qsf` are not package
members; the part recipe digest covers their bytes. It checks all three shell
clocks, pixel-only part state and every outside CRAM bit, including companion
columns. Rebuild parts after any shell identity change. HOLD blacks pixels while
timing continues. Horizontal/top display-enable opening, cycle-exact raster
timing and aspect-ratio correction remain absent.

## Validation

```sh
make -C sources/misteross sim-fes-atari-st
make -C sources/misteross sim-fes-atari-st-media-lifecycle
make -C sources/misteross fetch-fes-atari-st-emutos
make -C sources/misteross sim-fes-atari-st-emutos \
  EMUTOS_ROM=build/roms/emutos-1.4/etos192us.img
make -C sources/misteross sim-fes-atari-st-emutos-memory \
  EMUTOS_ROM=build/roms/emutos-1.4/etos192us.img
python3 scripts/sim_atari_st_disk_diagnostic.py \
  --rom sources/misteross/build/roms/emutos-1.4/etos192us.img \
  --output out/validation/atari-st/disk-guest --seconds 8
python3 scripts/atari_st_io_diagnostic.py --output out/validation/atari-st/io-media
python3 scripts/sim_atari_st_io_diagnostic.py \
  --rom sources/misteross/build/roms/emutos-1.4/etos192us.img \
  --output out/validation/atari-st/io-guest --seconds 10 --audio-phase-ticks 20
make -C sources/misteross build-fes-atari-st CACHE_ROOT=/absolute/toolchain-cache
make -C sources/misteross sim-fes-atari-st-video-parts
make -C sources/misteross build-fes-atari-st-video-part \
  ST_SHELL=/absolute/frozen/build/fes-atari-st-oss \
  ST_PACKAGE=/absolute/sealed/package ST_VIDEO_PART=direct \
  CACHE_ROOT=/absolute/toolchain-cache
make -C sources/misteross build-fes-atari-st-card \
  ST_SHELL=/absolute/frozen/build/fes-atari-st-oss \
  ST_PACKAGE=/absolute/sealed/package CACHE_ROOT=/absolute/toolchain-cache
```

The original `AUTO/IOTEST.PRG` diagnostic checks exact keyboard make/break,
modifier aliases and both joystick ports through the guest ACIA, then repeats
three-second YM channel A/B/C tone, noise, envelope and silence phases. Its
generated `schedule.json` contains the normal host input events and expected
guest bytes. On hardware, wait for each visible WAIT banner before sending its
group through the attached session input API; require PASS before continuing.
Launch the generated `io-auto.st` using normal library Play and Stop normally
after measurement. This tests host-delivered input; it does not qualify a
physical USB keyboard or controller. The host simulation executes stock
EmuTOS and the real CPU/peripheral RTL with bounded RAM/disk callbacks. Its
optional shorter audio holds have separately identified PRG/disk hashes; they
are not the hardware fixture, physical SDRAM or HDMI acceptance.

Before importing a demo, inspect an independently acquired classic MSA image:

```sh
python3 scripts/atari_st_demo_media.py /absolute/demo.msa --raw-output /absolute/demo.st
```

This offline tool checks every compressed track and reports original geometry
and the decoded raw-image hash. Explicit raw output preserves every decoded
byte without padding. Original BIG (80×1×10) and Cuddly (82×2×10) disks fit
the geometry extension. Admission and sector correctness do not qualify their
loaders or raster/border effects.


`scripts/sim_atari_st_demo.py` runs an original raw disk with independently
pinned 192 KiB firmware on the actual FX68K/chipset model. Its default ROM
digest selects stock EmuTOS; an explicit `--rom-sha256` admits an independently
identified original TOS image without changing its bytes. It freezes source,
ROM, disk and generated-model identities before execution. Its bounded fault
log records the first 64 bus errors with the latched address, direction and
function code; the first eight faults also capture RAM. The exported CPU PC
is diagnostic prefetch/exception state, not an instruction-retirement trace.
The diagnostic also records changed palette entries by native frame/line
after six simulated seconds (first eight changed bundles per frame and full
per-frame counts). Horizontal positions are CPU cycles within the native line.
It saves the first sixteen complete native RGB captures after 6.5 seconds,
and records IACK/MMIO request starts and sync changes between six and seven seconds.
These bounded traces locate guest handlers without an instruction-retirement
claim. `--trace-start` / `--trace-end` select a bounded interval within the run;
that interval records every committed palette write, including writes of the
same value, and every completed native capture's static-logo crop and row
hashes. The first eight distinct crop hashes also save their complete native
RGB images, so a rare outlier can be inspected after the run. The logo crop is native x=65..244, y=0..63. FNV-1a-64 hashes compare
expanded RGB bytes for equality; they are not cryptographic artifact identities.
Per-capture counters report video-prefetch and CPU RAM latency. Optional
`--ram-extra-wait 0..64` adds system-clock delay to each CPU RAM callback;
this probes sensitivity and does not model shared SDRAM arbitration.
`--ram-fixed-wait 0..64` instead replaces the variable CPU RAM callback delay
with a fixed system-clock delay, before any extra wait. Zero provides an
ideal-storage timing comparison; it does not change the physical controller
or establish that real SDRAM can meet that timing. Shared-memory mode rejects
callback overrides, so its reported timing always comes from the actual arbiter.
`--mfp-wait-states 0..8` selects a separate native-cycle MFP access probe;
its selected value is recorded in the source-bound result. Run
`python3 scripts/analyse_atari_st_raster_trace.py OUTPUT --output SUMMARY.json`
from the FES root after completion to check artifact digests and summarize
logo equality, connected-bit palette repeats and interrupt positions. `--shared-memory` instead uses the existing `st_boot_memory_sim_top` fixture:
the original disk is preloaded into the separate SDRAM buffer before CPU release,
and the real memory arbiter, addon controller, DDIO digital model, floppy DMA,
native capture and independent-clock 720p renderer execute together. It does not
exercise mailbox upload, electrical pad timing or a physical FPGA. This mode
requires at least six seconds; callback RAM delays are rejected. The original diskless EmuTOS
invocation keeps its boot assertions and supplies no media. A changing
logo is reported as noncanonical, without automatically declaring a visual fault. `--key-b-at SECOND` selects the scroller through the normal
HID/IKBD path with a 150 ms B press. Both storage models accept
`--key-at SECOND --key-usage USAGE` for other screens (1/2/3 use HID
usages 30/31/32). Every run saves `demo-audio.wav`, the unfiltered signed
mono 48 kHz chip PCM, and `demo-audio.json` with sample counts, amplitude
statistics and system-clock cadence. Shared-memory runs also save
`demo-ym.jsonl`: every accepted valid-register PSG data write, with its raw
value, selected register, system-cycle timestamp and diagnostic PC. The trace
covers the entire guest run independently of the raster trace interval. It
observes the PSG acceptance edge without changing its clock or audio model.
The PC is exception-unit state, not an instruction-retirement assertion.
Quiet boot audio is valid; these files
do not establish analog sound fidelity. `--timeout-seconds` controls only
the host deadline, independently of the guest duration. These are observations of the reduced
native timing model, not original GLUE/shifter timing equivalence. Per-second
static framebuffer images do not prove video timing or border behavior. Neither successful capture completion nor removal of one loader
fault establishes demo compatibility.

The aggregate uses original diagnostic firmware and focused CPU, video,
MFP, keyboard/audio, floppy, physical SDRAM, dual-clock cache and real GP
upload, writable sector/snapshot, mouse handshake and media arbitration tests. The CPU diagnostic covers ROM replacement, supervisor
protection, MMU aliases, big-endian byte/word/long access, TAS, exceptions,
expansion waits/errors and CPU-written pixels. The memory test checks real
SDRAM commands, CAS-2 capture, refresh, byte masks, fairness and warm reset.
Write masks are asserted during row setup two fabric clocks before WRITE; the
model checks zero, one and two clocks of added DQM delay and its two-clock read
latency.
The media test uploads a complete disk with odd chunk boundaries and checks
CRC, execution-independent insert/eject and completion before acknowledgement.

The optional EmuTOS test checks stock-ROM RAM discovery, screen setup and
running system/VBL timers. The memory boot test concurrently uses the physical
SDRAM command model and the dual-clock video adapter, checks bounded access
latency and zero underruns, and captures RGB after both video-part boundaries.
`scripts/fetch_atari_st_emutos.py` pins both the
official 1.4 archive and exact US image by SHA-256 and retains its license.
It does not put downloaded ROM bytes in Git or seal them into the blank ROM.
Tests copy pinned CPU microcode to a private working directory. FX68K needs
its documented simulator warning suppressions; the focused device, memory
and video tests use strict `-Wall`.

These are host checks. They establish neither SDRAM electrical timing nor
exact-artifact hardware acceptance. A board build must pass authenticated
OSS synthesis, routing, timing and ROM/socket checks before it can publish a
package. STE blitter, enhanced palette and DMA sound are outside the original
520ST scope.
