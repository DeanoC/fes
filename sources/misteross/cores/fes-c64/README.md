# FES Commodore 64

`fes.c64` 0.3.0 is a package-only Commodore 64 pathfinder on `fes.computer`
1.0. It follows the Apple II pattern: late-bound firmware, a live removable
disk, and linkable expansion cards. It is not in the factory image. No
Commodore ROM is in this tree. HIP seal and kit acceptance are still open.

The core lane is [docs/cores.md](../../docs/cores.md). The mailbox contract is
[home-computer I/O](../../../mister-packages/docs/computer-io.md).

## Machine

- 6510 is the shared NMOS 6502 plus the processor port at `$00`/`$01`. Phi2 is
  a fractional 0.985248 MHz PAL enable of the 52.224 MHz system clock.
- 64 KiB RAM. Standard banking, plus 8 KiB, 16 KiB and Ultimax cartridge maps
  from `/EXROM` and `/GAME`.
- 20 KiB linked firmware: BASIC at bytes 0..8191, KERNAL at 8192..16383,
  and the 4 KiB character ROM at 16384..20479. The OSS package leaves twenty
  1024×10 M10K lanes (column 5, rows 32–51) blank. The final four lanes use
  registered true dual-port reads for the CPU and VIC on the native system clock, with writes disabled.
  Unused byte-enable ports are omitted; both write enables remain disabled.
  CPU reads take two system clocks; VIC character reads take one native system clock.
- VIC-II monochrome/multicolor text and bitmap modes, plus extended-color
  text addressing. Matrix/color reads occur on native PAL badlines and glyph
  reads on each native display line. VC, VCBASE, RC and the matrix line follow
  native cycles, so late DMA, FLI, FLD and linecrunch affect the fetched data.
  BA warns the CPU three cycles before matrix bus ownership. CPU reads stall;
  pending writes complete. Native raster, CIA, SID and drive clocks keep running.
  `$D018` selects the screen, character or bitmap base in the active VIC bank.
  Character ROM is visible at `$1000–$1FFF` in banks 0 and 2 independently of
  CPU CHAREN. CPU CHAREN=0 uses the same character ROM.
  Color RAM is one synchronous dual-port M10K, 1024×4 at `$D800–$DBFF`;
  native DMA can read the final 24 scratch locations when VC wraps.
  CPU bus strobes occur at system cycle 26 and reads are sampled at cycle 43.
  Three indexed-color buffers retain complete 320×200 native frames for HDMI.
  Each buffer uses 32 explicit 2048×5 M10Ks, with a mailbox handoff across the
  system and HDMI clocks. Only complete frames reach the reader; an unconsumed
  publication keeps both the reader's buffer and queued frame protected.
  Nine-bit raster compare uses `$D011` bit 7 and `$D012`; `$D019` latches
  events and acknowledges written ones, while `$D01A` masks the CPU IRQ.
  The PAL beam has 63 Phi2 cycles per line and 312 lines per frame.
  Events occur once per line; writes can match the current line, and automatic
  line-zero comparison waits one Phi2 after frame wrap.
  Sprites, collision/lightpen, exact mode-transition quirks, subpixel clock
  phase and full native overscan remain open. HDMI still presents the scaled
  320×200 window with letterboxing.
- Reduced SID: three voices, pulse, saw, triangle and noise, a crude envelope
  and volume. No filter.
- CIA1: keyboard matrix, joystick and IRQ. CIA2: IEC, the VIC bank and NMI.
  VIC banking follows the effective PA0/PA1 pin levels, including pull-ups
  when their direction bits select inputs; it does not follow the output
  latch alone. The PASS-frame diagnostic checks this input-pin bank selection.
  Both CIAs have timer A and timer B with live counter reads, stopped high-byte
  loading, force-load strobes, continuous and one-shot operation. Timer B can
  count Phi2 or timer A underflows. CNT is held high without external edges,
  so CNT edge modes do not count and the gated timer A mode can count.
  TOD, serial shift and PB6/PB7 timer outputs remain unimplemented; internal
  6526 pipeline delays are not modelled. Port 2 (CIA1 port A) is controller port 0.
- HDMI 720p60. The 320×200 picture is scaled 4× horizontally and 3×
  vertically. Audio is the shared 48 kHz I2S serializer.

## Keyboard

HID keys map to all 64 native keyboard switches. CIA1 port A selects columns;
port B reads rows. The mapping follows the stock KERNAL's keyboard table;
A closes PA1/PB2 and Return closes PA0/PB1. Left/right/up/down cursor keys and
F1–F8 use native shift combinations. Ctrl and Alt supply Control and Commodore.
PC punctuation keys retain native C64 semantics: `[` is @, `]` is *, backslash
is pound, apostrophe is colon, keypad + is plus, grave is left arrow, and Page Up
is up arrow. Native Shift+2 is quotation mark. Right Shift remains its own switch.

`make sim-fes-c64-keyboard` checks each switch against all eight scan columns,
release, chords and modifier aliases. The open firmware tests A using the same
PA1/PB2 wiring as the real KERNAL. This forward scan does not yet emulate reverse
scanning or electrical ghosting between pressed keys.

## Disk

`fes.media.c64-disk` 1.0 is media unit 0 (the same unit number an Apple II
floppy uses; a core declares one of them). The image is an exact 174,848-byte
35-track D64. The built-in device 8 speaks CLK/DATA, LSB first, with EOI on
the last byte, well enough for LISTEN, OPEN, a filename, UNLISTEN, TALK and
SECOND. Writes are ignored. `$` returns a four-byte synthetic program.
CIA2 CLK/DATA inputs read high for released IEC wires; output controls pull
wires low when set. Byte transfers use logical wire levels, a CLOCK-high ready
handshake before bit 0, ACK after the last CLOCK-low edge, and EOI on filenames
and the final received byte. TALK turnaround holds CLOCK low before announcing
ready, and UNLISTEN releases an unaddressed idle bus. ATN acknowledgement
remains asserted until the host first pulls CLOCK low, including TALK begun
from a released idle bus.
`make sim-fes-c64-iec` exercises these handshakes with a standard host and the
synthetic D64, independently of the open CPU firmware. A linked-sector case
checks 260 payload bytes, omits both link headers and requires EOI only on
the last byte of the final sector. The firmware uses the
same physical polarity and byte protocol. This is still a read-only file device,
not an emulated 1541 CPU/VIA/GCR mechanism; drive-uploaded fastloaders are not
implemented.

## Cartridge sockets

One shared cartridge port is exposed as two physical sockets so multi-slot
composition is exercised. Socket 1 is the ROM window (`ROML`/`ROMH`, `/EXROM`,
`/GAME`). Socket 2 is the I/O window (`IO1` `$DE00`, `IO2` `$DF00`). The
layout is `fes.c64-bus.sockets/1`. The rectangles reuse the Apple II slot 2
and slot 4 rows. `scripts/c64_slots.py` generates
`rtl/c64_slot_sockets.v`.

`expansions/probe.v` is the open card. Mode 0 answers `FES1` at `$8000`. Mode
1 is scratch at `$DE00` and id `$C6` at `$DE01`. The machine simulation links
both. A sealed shell leaves the sockets vacant; `scripts/build_c64_slot_card.py`
builds one card into one socket of a frozen shell.
The producer selects probe mode 0 for socket 1 and mode 1 for socket 2;
it authenticates both the legacy PLL output aliases and current physical
C6/C7 pin layout, preserving an already-correct physical layout.
Each synthesized response is lowered to an independent identity ALUT2 before
the frozen merge: constants and aliased output signals must not be lost by the
pinned cart merger. Clock-pin checks, final analogue setup/hold on all required
clocks and exact CRAM containment are separate admission checks.

## Diagnostic

`diagnostic/firmware.py` assembles the open 20 KiB image, including the
original diagnostic character ROM. Success stores `$FF`
at `$C000` and paints the word PASS, in white, across three rows in the middle
of the character screen. The border and the screen background are black.
A green bar is the 720p letterbox and is not the pass mark. A failure stores
a stage at `$C000` and `1` at `$C001`:

Stages 7 and 8 with `$C001 = 0` mean waiting, not failure. Mount the
synthetic D64 and confirm media unit 0 is ready before pressing joystick Up,
then keyboard A. This gates LOAD on operator readiness rather than a fixed
sleep: C64 media is currently mounted after CPU release. The SID pulse uses
50% duty cycle so audio acceptance can require an AC tone rather than DC.
`--without-cartridges` skips only stages 5 and 6 for a vacant sealed shell; it
is not expansion acceptance.
The simulation covers both initially-ready input/media and delayed mount,
joystick and keyboard delivery, then captures the next full frame and requires
PASS in white, a black border, a black background and no stage digit.

The raster handler acknowledges through `INC $D019`, exercising the NMOS
6502 read-modify-write bus sequence: write the original operand, then write
the modified operand on the following CPU cycle. Omitting the first write
leaves the VIC source pending because the final incremented value clears bit
zero. `make sim-fes-c64-rmw` checks both writes for all 24 documented memory
RMW opcode/address-mode combinations, including absolute-index page crossing
and CPU enable gaps. The motherboard diagnostic checks IRQ delivery and
acknowledgement through the same shared CPU.

The shared CPU implements the stable undocumented SAX family in zero-page,
zero-page,Y, absolute and (zero-page,X) modes. SAX writes `A & X` and leaves
registers and flags unchanged. Edge of Disgrace uses `SAX $D016` in its
checkerboard section; missing decoding loses instruction alignment and corrupts
VIC register programming. `make sim-fes-c64-sax` checks all four modes, wrapping,
enabled cycle counts and unchanged registers/flags in 144 cases. Other
undocumented NMOS instructions remain incomplete.

The fractional PAL enable is predicted one system tick ahead into a register,
with the same pulse sequence and startup phase. This removes the phase adder
from the CPU's RDY timing path. `make sim-fes-c64-clock` compares the actual
motherboard enable against an independent fractional reference on every tick
for a full PAL second, including alternate parameter cases with adjacent
pulses. Rates at or above half the system clock retain the original direct enable.

C64's D64/IEC path remains read-only. Atari ST's initial-disk-before-release
and writable-disk save/restore path is a useful ordering reference, but its
sector commit and game/base-media persistence contract is not implemented by
C64 and is not implied by a successful C64 LOAD.

| Stage | Check |
| --- | --- |
| 1 | RAM |
| 2 | BASIC signature |
| 3 | VIC border |
| 4 | character generator |
| 5 | cartridge ROM |
| 6 | cartridge I/O |
| 7 | joystick |
| 8 | keyboard |
| 9 | D64 `BOOT` bytes `01 08 11 22 33 44` at `$0800` |
| 10 | CIA1 timer B one-shot, IRQ vector and interrupt acknowledgement |
| 11 | CIA2 timer A one-shot, NMI vector and acknowledgement while `SEI` is set |
| 12 | VIC raster IRQ through the CPU vector and `$D019` acknowledgement |

```sh
make -C sources/misteross sim-fes-c64
make -C sources/misteross sim-fes-c64-cia # directed timer/register checks
make -C sources/misteross sim-fes-c64-vic # raster compare/mask/acknowledgement
make -C sources/misteross build-fes-c64   # HIP seal; not part of the sim gate
```

## External firmware boot observation

`make sim-fes-c64-boot-build` builds a separate diagnostic harness with vacant
cartridge sockets. Run `python3 scripts/c64_boot_observe.py --basic /path/basic.rom
--kernal /path/kernal.rom --chargen /path/chargen.rom --disk /path/demo.d64 --output /path/ignored-output`
from this module, using your own exact 8 KiB BASIC and KERNAL ROMs, a 4 KiB character ROM
and a 174,848-byte D64. The script retains input hashes, simulator identity, RAM, the final complete
HDMI frame, a screen-code dump and the last 64 CPU bus cycles. The default
observation covers three simulated machine seconds; `--cycles` sets a longer
startup budget or a shorter trace (0.02 to 300 seconds). The actual
52.224/74.25 MHz system/pixel ratio is used; CPU cadence remains the current
0.985248 MHz PAL. By default no keyboard command is sent. `--type-command` schedules native
C64 text through HID key states after three seconds, holding each key for 50 ms
then releasing it for 50 ms. Include a carriage return and a sufficient `--cycles`
budget to execute a LOAD command. No keyboard-buffer or CPU-memory injection is
used. `--then-command` types a second command at `--then-at-cycles` (58
seconds by default), and saves RAM immediately before that command. This lets
a LOAD/RUN diagnostic compare loaded bytes before execution. Typed commands,
event counts and the snapshot hash are retained in the observation receipt.
Per-second bus/load-pointer/IEC observations and received IEC commands are
flushed to the run log while execution continues.
The generated bytes belong outside tracked source. This observation does not
assert successful boot or demo compatibility. Cycle-coupled video and 1541 emulation are not implied by the default
file-device harness.

`make sim-fes-c64-boot-drive-build` builds the same motherboard and harness
with `FES_C64_REAL_DRIVE_SIM`. Select it with `--real-drive --drive-rom
/path/1541.rom`. The helper requires an exact 16 KiB user drive ROM and converts
the D64 to raw GCR tracks in an ignored G64 file. The drive CPU, both VIAs, IEC
inverters and digital read channel execute in RTL. `sim/rotating_disk.h` supplies
only physical rotation, flux and head movement; it sends no decoded sectors or
DOS replies. The medium is read-only, uses nominal density timings and full
tracks, and models no analogue noise. `make sim-fes-c64-gcr`, included in the
aggregate simulation, checks 150 header/data blocks across all four density
zones with original synthetic sector data. The package still selects the
read-only file device; physical drive ROM linking and media integration remain
open.

`make sim-fes-c64-checkpoint`, included in the aggregate simulation, compares
uninterrupted/restored CPU/RAM trajectories, a held key, a pending capture,
complete frames and final serialized states using original test firmware.
Boot diagnostics expose only their read-only observation fields by default.
`C64_BOOT_FULL_VISIBILITY=1` retains full writable internal visibility for
specialized diagnostics. Both modes serialize their complete model state;
changing mode changes the executable/layout and requires a fresh checkpoint.
The selected-field mode matches the full mode's RAM, frames and observations
through real firmware boot and the start of native LOAD, with approximately
2.6× faster execution in that host comparison.

Both diagnostic builds support development checkpoints through Verilator
`--savable`. `--save-state /path/loaded.state` saves at the run's end;
`--save-at-cycles N` captures earlier and continues the run. Choose a point after
a native LOAD has completed and verify the loaded payload before using that
checkpoint for demo work. `--restore-state /path/loaded.state --cycles N` resumes
with an absolute ending cycle greater than the saved cycle. Supply the same
firmware, disk and diagnostic mode. CPU microstate, all RTL memories and
peripherals, both clock schedules, queued keyboard events, partial HDMI frames
and the real-drive medium's angle/head position are preserved. The metadata
binds the checkpoint to the exact executable and input hashes; an RTL rebuild
requires a fresh capture. Restores preserve the saved command queue and reject
new typing arguments. To repeat RUN, schedule it after the capture in the
original invocation. The harness checks checkpoint identity and integrity;
it does not certify native LOAD, demo compatibility or runtime save-state
support.

A real-drive observation accepts repeated `--disk-library D64` arguments.
The library is immutable and hash-checked with the firmware and simulator.
`--disk-change CYCLE D64` queues an initial physical insertion; it replaces
raw tracks without changing CPU/VIA state, head position or spindle angle.
A restore automatically retains the library and queued events. Use
`--append-disk-change CYCLE D64` for a later insertion from that library;
its cycle must follow both the checkpoint and existing queued events.
This allows later demo sides to be explored from the saved machine state.
The cycle budget is an absolute endpoint, bounded to 1800 machine seconds.
`--save-final-state PATH` retains an endpoint checkpoint in addition to the
intermediate capture. Both sidecars record complete replay inputs and their
own capture cycle. Restores preserve the capture policy, pending events and
model layout checks. Different firmware, library bytes or executable still
require a fresh capture.


`--late-video` is available only with the real-drive diagnostic. It holds the
current independent HDMI renderer until the final five frames while native
CPU, raster, CIA, SID and drive clocks continue. This accelerates disk/loader
investigation and supplies no evidence of live native graphics fidelity.

Firmware and disk memories accept Verilator-only `+C64_FIRMWARE_HEX=...` and
`+C64_DISK_HEX=...` overrides; the ordinary open diagnostic retains its defaults.
Version 0.2.0 links a 20 KiB firmware image and has no inherited seal or
hardware acceptance from the earlier 16 KiB package. The D64 media interface
remains read-only media unit 0.

`make sim-fes-c64-video` checks complete frames with alternating character
colors and all multicolor-bitmap palette selections in all four banks.
`make sim-fes-c64-rom` checks every firmware/character byte and the independent
read clocks. Both are included in the aggregate simulation.

The raster interrupt tests follow the Commodore
[Programmer's Reference Guide](https://www.devili.iki.fi/Computers/Commodore/C64/Programmers_Reference/Chapter_3/page_150.html)
and Christian Bauer's measured VIC-II description,
[section 3.12](https://www.cebix.net/VIC-Article.txt). These checks exercise
register/line behavior, the 312×63-cycle PAL frame and the CPU vector;
physical subcycle timing and native video/DMA coupling remain unqualified.

The CIA register behavior follows the MOS/Commodore
[6526 data sheet](https://www.retrodocs.fr/wp-content/uploads/pdf/MOS-6526.pdf),
pages 5–7: separate latches and counters, load conditions, clock selection,
one-shot stopping, ICR acknowledgement and the read-zero force-load bit.
The CIA2 NMI connection follows the Commodore
[Programmer's Reference Guide](https://www.zimmers.net/anonftp/pub/cbm/c64/manuals/c64-programmers-reference-guide.txt),
pages 348–349. `sim-fes-c64` includes the directed CIA regression and the
firmware's CPU interrupt checks. These are host simulations, with no sealed
artifact or kit acceptance implied.

The C64 development lane refreshes nextpnr to main `bd1c0909`, sampled on
2026-10-14. New shell/card timing and hardware validation remain required;
historical seals used the earlier pin.

`build-fes-c64` authenticates `toolchains/c64.lock` (Yosys `4b971bb1`,
Mistral `8fcc4cb4`, nextpnr `bd1c0909`). The socket check admits a compiler-inserted route-through
buffer only at a pinned boundary flip-flop's paired combinational half, with
the exact physical pin map and a dedicated connection to that flip-flop;
clock-coverage outputs stay unused and every other shell cell inside a socket
is rejected.
The first sixteen firmware lanes are synchronous 1024x10 M10Ks on the system
clock, with reads enabled and active-low writes disabled. The four character
lanes use true dual-port M10Ks with system clocks and both
active-high write enables disabled. The bank selector
is registered with the M10K address; a combinational lane mux and the final
data register keep the two-cycle latency the 6510 samples at cycle 43 of
phi2. The ROM map requires `CFG_ASYNC_READ=0`. After synthesis, and again
on the routed netlist, any `MISTRAL_M10K` or `MISTRAL_M10K_TDP` with
`CFG_ASYNC_READ=1` fails the build. Color RAM uses one synchronous dual-port
M10K: the system-clock port returns CPU reads before the cycle-43 sample and
the VIC system-clock port supplies the native matrix/color latch.
Before routing, the producer connects the unused write clocks of the
inferred read-only M10K (IEC track lookup) to its live read clock. It checks their names and disabled write ports so a changed
synthesis shape fails closed.

## 1541 development motherboard

`make sim-fes-c64-drive` checks an independent development motherboard with a
16 MHz reference divided by 16 for its 1 MHz 6502, mirrored 2 KiB RAM,
two VIA port/timer/handshake slices and the IEC
inverters/ATN acknowledge gate. Its open firmware checks RAM aliases, both VIA
interrupts through the CPU vector and the released bus. `sim-fes-c64-via` checks
input latches, PA pin/PB latch reads, handshake and pulse outputs, selectable
interrupt edges, IFR/IER, Timer 1/PB7 and Timer 2/PB6 pulse counting. These
register semantics follow the [6522-compatible WDC datasheet](https://www.westerndesigncenter.com/wdc/documentation/w65c22.pdf);
Timer 1 exposes the underflow cycle and reloads in one-shot mode as observed in
[VICE's VIA implementation](https://github.com/VICE-Team/svn-mirror/blob/main/vice/src/core/viacore.c).
The drive's VIA access and CPU sample occur together near the end of the
1 us bus cycle, retaining the input latch until its byte is sampled.
The drive enables the shared CPU's optional SO input, connecting the external
byte-ready pin to its overflow flag. `sim-fes-c64-so` checks both enabled and
default-disabled modes. Serial shifting and NMOS subcycle behavior remain
unimplemented. The C64 producer also pins these development RTL inputs without instantiating them.

`make sim-fes-c64-drive-build` followed by
`python3 scripts/c64_drive_observe.py --rom /path/1541.rom --output /path/ignored-output`
observes a user-supplied exact 16 KiB drive ROM for three simulated seconds.
It retains ROM/executable hashes, the drive RAM and bus/control observations.
The disk electronics use inactive input pins; GCR rotation and active byte-ready,
disk loading and fastloader execution are not provided. This motherboard is
not selected by the C64 package or its boot harness yet, and its ROM has no
physical download map. The existing read-only IEC file device remains selected.
