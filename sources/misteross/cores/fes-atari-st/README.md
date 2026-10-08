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
Initialization completes before CPU release. SDRAM refresh continues while
idle and while the CPU is held. An abandoned request drains its physical
command and suppresses its old completion. Warm Hold preserves RAM and media.
Physical halfword offsets `$00000–$3FFFF` contain RAM; `$40000–$A67FF` contain
the separate disk buffer (up to 820 KiB). RAM masks select the even high byte or odd
low byte. The controller preserves its established row/bank/column wiring
and uses its established rate-0 rising-edge sample plus its fabric capture stage.

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
Bus-master arbitration and cycle-exact GLUE/MMU contention are outside this implementation.

## Peripherals

`st_mfp.sv` implements GPIP, edge selection, direction, interrupt enable,
pending, in-service, mask and vector registers, authentic priority and
software/automatic EOI, and timers A/B/C/D. MFP IRQ6 uses a supplied vector;
other IRQ levels use the CPU's VPA autovectors. Its independent 2.4576 MHz
clock enable gives EmuTOS's timer C `/64 × 192` exactly 200 Hz. Functional
native VBL/HBL clocks use 50/60 Hz color and 71 Hz monochrome modes,
independently of HDMI. Timer B receives active display enable, so blank lines
do not contribute events. Border phases and the live video counter are reduced.
The MFP UART has disconnected RX and timed TX status, without a physical serial port.

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
integer nearest-neighbor scaling: color 4×3 or 2×3, monochrome 2×1. High mode
supports palette-bit inversion; color borders use palette entry 0.
`st_video_adapter.sv` defaults to native low-resolution capture. In the
52.224 MHz system domain, `st_native_low_video.sv` prefetches alternating
80-word RAM rows and selects the live palette at each nominal 8 MHz pixel.
Three 320×200 RGB333 banks cross to the 74.25 MHz pixel domain through
publish/release toggles. Only complete native frames are published; the HDMI
reader pins one bank until output SOF and selects the newest available frame.
This repeats or drops complete source frames when native and output rates differ.
A missing RAM row displays black for its whole line; HOLD aborts an unfinished
capture, and an invalid base never wraps into populated RAM.

Medium and monochrome retain the indexed double-line-cache renderer and its
coherent configuration bundle, activated at output frame boundaries. Their
palette remains fixed for that output frame. Synchronous cache reads and
ownership tags meet the original plane-capture edges. A held, fair arbiter
shares the existing video-memory port between the two renderers.

The reduced native timing uses ordinary PAL lines 63–262 with DE cycles
56–375, and NTSC lines 34–233 with DE cycles 52–371, following the ordinary
[Hatari 2.5.0 timing table](https://github.com/hatari/hatari/blob/v2.5.0/src/video.c).
Mode settings are sampled at line/frame boundaries so brief writes do not
create extra display-enable edges. These are approximate sampling boundaries,
not original GLUE latch positions. Prefetching also does not reproduce exact
MMU/shifter arbitration, mid-line base writes, or opened borders.

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
timing continues. Opened borders, cycle-exact raster timing and aspect-ratio
correction remain absent.

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
per-frame counts). `--key-b-at SECOND` selects the scroller through the normal
HID/IKBD path with a 150 ms B press. These are observations of the reduced
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
