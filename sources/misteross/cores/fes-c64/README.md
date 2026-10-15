# FES Commodore 64

`fes.c64` 0.4.0 is a package-only Commodore 64 pathfinder on `fes.computer`
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
- 36 KiB linked firmware: BASIC at bytes 0..8191, KERNAL at 8192..16383,
  character ROM at 16384..20479 and 1541 ROM at 20480..36863. Thirty-six blank
  1024×10 M10K lanes are mapped once: lanes 0–19 in column 5, rows 32–51;
  drive lanes 20–35 in column 14, rows 32–47. Character lanes 16–19 have
  registered CPU/VIC ports. All writes are disabled. CPU reads take two
  system clocks; VIC and independent drive reads take one.
- VIC-II monochrome/multicolor text and bitmap modes, plus extended-color
  text addressing. Matrix/color reads occur on native PAL badlines and glyph
  reads on each native display line. VC, VCBASE, RC and the matrix line follow
  native cycles, so late DMA, FLI, FLD and linecrunch affect the fetched data.
  Sprite pointer and three-byte reads occupy their PAL bus slots. Sprite DMA,
  display, MC/MCBASE and expansion-flop state include cycle-15 crunch behavior.
  Matrix and sprite BA warn the CPU three cycles before bus ownership. CPU reads stall;
  pending writes complete. Native raster, CIA, SID and drive clocks keep running.
  `$D018` selects the screen, character or bitmap base in the active VIC bank.
  Character ROM is visible at `$1000–$1FFF` in banks 0 and 2 independently of
  CPU CHAREN. CPU CHAREN=0 uses the same character ROM.
  Color RAM is one synchronous dual-port M10K, 1024×4 at `$D800–$DBFF`;
  native DMA can read the final 24 scratch locations when VC wraps.
  CPU bus strobes occur at system cycle 26 and reads are sampled at cycle 43.
  Three indexed-color buffers retain complete 384×272 PAL frames, including borders for HDMI.
  Each buffer uses 51 explicit 2048×5 M10Ks, with a mailbox handoff across the
  system and HDMI clocks. Only complete frames reach the reader; an unconsumed
  publication keeps both the reader's buffer and queued frame protected.
  Sprite beam X is prepared one system clock before each dot; guest-controlled
  inputs keep their dot sampling. Complete framebuffer address/color writes
  pass through one system-clock register, preserving dot and collision timing.
  Nine-bit raster compare uses `$D011` bit 7 and `$D012`; `$D019` latches
  events and acknowledges written ones, while `$D01A` masks the CPU IRQ.
  The PAL beam has 63 Phi2 cycles per line and 312 lines per frame.
  Events occur once per line; writes can match the current line, and automatic
  line-zero comparison waits one Phi2 after frame wrap.
  Eight sprite shift pipelines compose mono/multicolor pixels, independent X/Y
  expansion and foreground priority. Dot collision masks feed sticky `$D01E/F`
  registers, deferred read clearing and `$D019` sprite collision interrupts.
  Flags latch independently of `$D01A`; clearing a collision register permits
  a later event to retrigger. Light-pen capture, exact mode transitions and
  physical subpixel phase remain open.
- Reduced SID: three voices with eight-bit pipelined ADSR envelopes, ENV3
  readback and signed PCM with linear four-bit master volume. Envelope rate,
  exponential division, gate pipelines, zero hold and the ADSR delay bug follow
  reSID's digital model. Pulse, saw, triangle and noise generation remain
  simplified; filter, sync, ring modulation, accurate combined waveforms,
  OSC3/paddle reads and SID bus decay remain open.
- CIA1: keyboard matrix, joystick and IRQ. CIA2: IEC, the VIC bank and NMI.
  VIC banking follows the effective PA0/PA1 pin levels, including pull-ups
  when their direction bits select inputs; it does not follow the output
  latch alone. The PASS-frame diagnostic checks this input-pin bank selection.
  Both CIAs have timer A and timer B with live counter reads, stopped high-byte
  loading, force-load strobes, continuous and one-shot operation. Timer B can
  count Phi2 or timer A underflows. CNT is held high without external edges,
  so CNT edge modes do not count and the gated timer A mode can count.
  The old MOS 6526 count-enable, force-load, one-shot and interrupt pipelines
  preserve delayed starts, pending counts after STOP and separate ICR flags,
  bit 7 and interrupt-pin timing. TOD, serial shift, FLAG input and PB6/PB7
  timer outputs remain unimplemented. Port 2 (CIA1 port A) is controller port 0.
- HDMI 720p60. The 384×272 PAL view is scaled into a centered 960×720
  viewport using rational nearest-neighbor sampling; side pillars are black. Audio is the shared 48 kHz I2S serializer.

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
35-track D64. The native 1541 executes the selected drive firmware on a
1 MHz 6502 with 2 KiB mirrored RAM, two VIAs, IEC inverter/ATN logic and a
16 MHz digital GCR read channel. Its CPU handles DOS and uploaded fastloaders.
CIA2 CLK/DATA inputs read high when the open-collector wires are released.

`c64_d64_medium` converts the selected D64 into one cached GCR track. It
models nominal 300 rpm rotation, motor gating, halftrack stepping and the
physical track's density zone. Rebuilding a track takes at most 29,705 system
clocks (0.569 ms); flux is unavailable during that interval. Media loading
and ejection hide the disk without resetting either CPU, the head position or
spindle angle. Disk writes, raw/protected tracks and analogue drive effects
remain unsupported. The write-protect input is fixed; general DOS disk-change
sensing is not yet modelled.

`make sim-fes-c64-medium` compares complete revolutions on all 35 tracks with
an independent flux model. `make sim-fes-c64-medium-mailbox` exercises the
actual mailbox, D64 store and medium through upload, CRC/commit, replacement
and ejection. The open motherboard diagnostic reads an original sector through
the real drive CPU, GCR decoder and IEC wires; it does not implement a DOS ROM.

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

`diagnostic/firmware.py` assembles the open 36 KiB image, including the
original character and drive diagnostic ROMs. Success stores `$FF`
at `$C000` and paints the word PASS, in white, across three rows in the middle
of the character screen. The border and the screen background are black.
A green bar is the 720p letterbox and is not the pass mark. A failure stores
a stage at `$C000` and `1` at `$C001`:

Stages 7 and 8 with `$C001 = 0` mean waiting, not failure. Mount the
synthetic D64 and confirm media unit 0 is ready before pressing joystick Up,
then keyboard A. This gates the diagnostic disk transaction on operator readiness rather than a fixed
sleep: C64 media is currently mounted after CPU release. The SID pulse uses
50% duty cycle so audio acceptance can require an AC tone rather than DC.
`--without-cartridges` skips only stages 5 and 6 for a vacant sealed shell; it
is not expansion acceptance.
The simulation covers both initially-ready input/media and delayed mount,
joystick and keyboard delivery, then captures the next full frame and requires
PASS in white, a black border, a black background and no stage digit. The drive seeks track 1, matches the sector header,
captures and decodes its GCR data, checks the checksum and sends six payload
bytes over a diagnostic IEC handshake. Unresponsive media times out at stage 9.
This original firmware is a portable hardware diagnostic, not a general DOS substitute.

The raster handler acknowledges through `INC $D019`, exercising the NMOS
6502 read-modify-write bus sequence: write the original operand, then write
the modified operand on the following CPU cycle. Omitting the first write
leaves the VIC source pending because the final incremented value clears bit
zero. `make sim-fes-c64-rmw` checks both writes for all 24 documented memory
RMW opcode/address-mode combinations, including absolute-index page crossing
and CPU enable gaps. The motherboard diagnostic checks IRQ delivery and
acknowledgement through the same shared CPU.

JSR retains the NMOS dummy stack read before its two pushes and fetches the
target high byte afterward. Reordering those cycles changes when a call
stalls on a VIC badline; Edge of Disgrace's unpacker exercises that boundary.
`make sim-fes-c64-jsr` compares bus addresses, directions and pushed bytes
against the pinned transistor model in nine cases, including page/stack wrap,
stack/operand overlap and CPU enable gaps. It also compares 20 motherboard
call/write timings around the PAL matrix-DMA window. The vectors are generated
by `sim/jsr_oracle.c` using the same perfect6502 checkout as the IRQ oracle.

PHP/PHA write the stack before fetching the next opcode. Retaining the dummy
read as a prefetch can execute an overwritten opcode and advance one cycle
early when BA falls after a push. `make sim-fes-c64-push` checks 24 independent
bus/state cases and 40 motherboard badline timings, including stack wrap,
code-page crossing, overwritten next opcodes, SP decrement, A/P preservation and
CPU enable gaps. Its vectors come from `sim/push_oracle.c` and the pinned
transistor model.

The shared CPU implements the stable undocumented SAX family in zero-page,
zero-page,Y, absolute and (zero-page,X) modes. SAX writes `A & X` and leaves
registers and flags unchanged. Edge of Disgrace uses `SAX $D016` in its
checkerboard section; missing decoding loses instruction alignment and corrupts
VIC register programming. `make sim-fes-c64-sax` checks all four modes, wrapping,
enabled cycle counts and unchanged registers/flags in 144 cases. Other
undocumented NMOS instructions remain incomplete.

The shared CPU polls IRQ through its enabled-cycle prefetch pipeline rather
than accepting the live pin at opcode decode. This preserves CLI/SEI/PLP
mask timing and the additional taken-branch delay; the pushed B bit remembers
hardware entry after IRQ is withdrawn. `make sim-fes-c64-irq` compares entry
cycles and stack bytes against 480 transistor-model vectors, including PHP/PHA,
with three enable schedules and held/withdrawn IRQs. The CPU README describes the pinned
oracle and remaining subphase limits. The full demo's raster timing remains
under investigation.

NMI edges survive CPU-enable gaps and use the instruction polling pipeline,
including taken-branch timing, before entering the handler. NMI takes
priority over simultaneous IRQ. `make sim-fes-c64-nmi` checks 7,840 entry
and stack cases against pinned transistor-model results, including short
pulses entirely between CPU enables. NMI hijacking and subphase behavior
remain unqualified.

The motherboard's BA write exemption uses the active latched bus direction.
The synchronous CPU's `WE` describes its prepared following cycle; using it
can retire a stalled operand read early and let `$D011` change before sprite
DMA finishes. `make sim-fes-c64-ready` exercises the actual motherboard with
an open raster-read stabilizer across 51 initial VIC phases. Write timing
matches pinned transistor-model vectors, including the boundary where the
previous gate finished eleven cycles early. The unit fixture installs its
RAM program and aligns VIC/DMA state once, then runs the real clock and bus.
Its vectors can be reproduced with the perfect6502 checkout documented in
the shared CPU README:

```sh
READY_ORACLE_ROOT=/absolute/path/to/perfect6502
cc -O3 -I "$READY_ORACLE_ROOT" cores/fes-c64/sim/ready_oracle.c \
  "$READY_ORACLE_ROOT/netlist_sim.c" -o /tmp/fes-c64-ready-oracle
/tmp/fes-c64-ready-oracle > /tmp/fes-c64-ready-vectors.h
cmp /tmp/fes-c64-ready-vectors.h cores/fes-c64/sim/ready_vectors.h
```

The fractional PAL enable is predicted one system tick ahead into a register,
with the same pulse sequence and startup phase. This removes the phase adder
from the CPU's RDY timing path. `make sim-fes-c64-clock` compares the actual
motherboard enable against an independent fractional reference on every tick
for a full PAL second, including alternate parameter cases with adjacent
pulses. Rates at or above half the system clock retain the original direct enable.

`make sim-fes-c64-sprite-pal` checks 100,800 dots against offline output
from unchanged VICE 3.10 `draw_sprites8`, using its PAL cycle table and
eight positions near reload boundaries. It covers mono/multicolor, X expansion
and the registered final DMA byte arriving between dots4/5. Regenerate with
`scripts/generate_c64_sprite_pal_vectors.py --vice-source /path/to/vice-3.10`;
the generator checks both primary source hashes. The production caller uses
the current cycle for sprite X and control events, and latches row data at
dot5 before releasing halt at dot7.

`make sim-fes-c64-sprite` checks all eight sprite fetch slots across four banks:
21/42-row timing, pointer addresses, three-byte order, a late DMA enable's
unowned first byte and sustained cycle-15 expansion crunch. The motherboard
diagnostic enables eight expanded sprites and requires stalled CPU reads,
interrupt delivery and the unchanged PASS frame. Sprite and matrix
requests share the native video memory port and are checked for overlap in
simulation. Unowned reads use the current Phi2 CPU read/write bus value,
including writes. `make sim-fes-c64-sprite-video` checks complete mixed-sprite
frames across all four banks, priority and independent X/Y expansion masks.
`make sim-fes-c64-border` opens borders through timed CSEL/RSEL writes and
checks side, top, bottom, wrapped-X and frame-crossing sprites. The expected
picture includes PAL's second Y trigger, reload-halt suppression and late
sprite display clearing. These are VIC-bus fixtures; physical CPU write phases
remain a separate qualification.

`make sim-fes-c64-collision-cpu` runs an open RAM program on the actual
motherboard. Four sprite masks, including a lone sprite, require IRQ entry,
SS/background readback, deferred clearing, W1C and disabling/re-enabling the
IRQ mask through real6510 instructions. The fixture initializes original test
firmware, RAM graphics and vectors; it does not assert demo LOAD acceptance.

`make sim-fes-c64-collisions` replays 500,000 ticks against recorded VICE 3.10
source-oracle output: 15,832 reads and 3,838 acknowledgements cover deferred
clear, W1C, mask-independent flags and retriggering. Regenerate the vectors with
`python3 scripts/generate_c64_collision_vectors.py --vice-source /path/to/vice-3.10`;
the generator checks the two source hashes and preserves VICE's license in its
temporary adaptor. `make sim-fes-c64-readback` checks all 256 write values at
61 VIC addresses, including read-only light-pen result registers and unused
bits. Light-pen capture itself is not implemented.

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
| 9 | native drive reads sector 0 and sends bytes `01 08 11 22 33 44` to `$0800` |
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
--kernal /path/kernal.rom --chargen /path/chargen.rom --drive-rom /path/1541.rom
--disk /path/demo.d64 --output /path/ignored-output`
from this module, using your own exact 8 KiB BASIC and KERNAL ROMs, a 4 KiB character ROM
an exact 16 KiB 1541 ROM and a 174,848-byte D64. The script retains input hashes, simulator identity, RAM, the final complete
HDMI frame, a screen-code dump and the last 64 CPU bus cycles. The default
observation covers three simulated machine seconds; `--cycles` sets a longer
startup budget or a shorter trace (0.02 to 1800 seconds). The actual
52.224/74.25 MHz system/pixel ratio is used; CPU cadence remains the current
0.985248 MHz PAL. By default no keyboard command is sent. `--type-command` schedules native
C64 text through HID key states after three seconds, holding each key for 50 ms
then releasing it for 50 ms. Include a carriage return and a sufficient `--cycles`
budget to execute a LOAD command. No keyboard-buffer or CPU-memory injection is
used. `--then-command` types a second command at `--then-at-cycles` (58
seconds by default), and saves RAM immediately before that command. This lets
a LOAD/RUN diagnostic compare loaded bytes before execution. Typed commands,
event counts and the snapshot hash are retained in the observation receipt.
Per-second CPU/drive bus and load-pointer observations are flushed to the
run log while execution continues.
The generated bytes belong outside tracked source. This observation does not
assert successful boot or demo compatibility. The default harness uses the
production RTL drive, medium and D64 store; the host supplies only firmware,
media bytes and input events.

`make sim-fes-c64-boot-oracle-build` builds the explicit `--medium-oracle`
variant. It runs the same drive CPU, VIAs and reader, replacing only the medium
with the independent C++ rotation/flux model in `sim/rotating_disk.h`. The
helper converts each D64 to raw GCR tracks for this oracle. It supplies no
sectors or DOS replies. `make sim-fes-c64-gcr` checks 150 header/data blocks
across all four density zones using original synthetic data.

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
and the medium's angle/head position are preserved. Native checkpoints also
retain an in-flight D64 upload and its byte cursor. The metadata
binds the checkpoint to the exact executable and input hashes; an RTL rebuild
requires a fresh capture. Restores preserve the saved command queue and reject
new typing arguments. To repeat RUN, schedule it after the capture in the
original invocation. The harness checks checkpoint identity and integrity;
it does not certify native LOAD, demo compatibility or runtime save-state
support.

An observation accepts repeated `--disk-library D64` arguments.
The library is immutable and hash-checked with the firmware and simulator.
`--disk-change CYCLE D64` queues an initial physical insertion; it replaces
D64 bytes through the real store ports while media-ready is low, without
changing CPU/VIA state, head position or spindle angle. The oracle variant
replaces its raw tracks instead.
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


`--late-video` holds the
current independent HDMI renderer until the final five frames while native
CPU, raster, CIA, SID and drive clocks continue. This accelerates disk/loader
investigation and supplies no evidence of live native graphics fidelity.

Firmware and disk memories accept Verilator-only `+C64_FIRMWARE_HEX=...` and
`+C64_DISK_HEX=...` overrides; the ordinary open diagnostic retains its defaults.
Version 0.4.0 links a 36 KiB firmware image and requires a fresh seal and
hardware acceptance. The D64 media interface
remains read-only media unit 0.

`c64_vic_graphics_pixels` preserves graphics shift-register carry across byte
boundaries and latches matrix/color metadata at the selected scroll slot.
PAL 6569R3 graphics-mode changes use the dot4/dot6/dot7 stages. Scroll
latching is enabled during native cycles15–54. Outside the forty-byte
window, graphics go blank while the last matrix/color metadata are retained. A late
`$D016` write does not reinterpret already loaded bits; increasing scroll can
expose exhausted shift bits before the next byte loads. In 38-column mode,
the horizontal border opens at native X31 and closes at X335, one dot before
the following whole-byte boundaries.

`make sim-fes-c64-graphics` replays 800,000 synthetic dots against recorded
VICE3.10 source-oracle output at the prefetched-byte boundary. All eight mode
combinations, scroll values, carry and foreground priority are covered.
Regenerate with `python3 scripts/generate_c64_graphics_vectors.py --vice-source /path/to/vice-3.10`.
`make sim-fes-c64-scroll` drives cycle-26 writes at native cycles32/55/56 and
checks the central output for late scroll, carry and end-window freezing in all banks.
`make sim-fes-c64-border-position` checks all displayed pixels in static
38-column mode, and `make sim-fes-c64-graphics-modes` combines sprites with
all five valid graphics modes across all banks.
`make sim-fes-c64-open-graphics` checks complete PAL frames with open borders,
nonzero bitmap/ECM metadata and solid glyphs. It requires graphics to blank
outside the byte window while retaining metadata, including the first and
last display rows. These checks do not qualify
CPU palette-write latency, every physical subphase or fetch-mode transition.

`make sim-fes-c64-video` checks complete frames with alternating character
colors and all multicolor-bitmap palette selections in all four banks.
`make sim-fes-c64-rom` checks every firmware/character byte and the independent
read clocks. Both are included in the aggregate simulation.

`make sim-fes-c64-vic-timing` checks all 63×8 PAL cycle/dot coordinates at
the native fractional 53/54-clock cadence, writes around each dot, the
one-clock framebuffer transaction and reset cancellation. It exercises
collisions and complete-frame publication, including the original visibility
of a CPU palette write on the preparation edge. This checks the internal
pipeline contract; physical VIC subphase behavior remains unqualified.

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
pages 348–349. Timer and interrupt pipelines adapt the old-6526 model from
pinned VICE 3.10 sources. `sim-fes-c64-cia-reference` checks 377 bus programs
at the native fractional cadence and two boundary schedules, including
ordered interrupt edges within a guest cycle. Its checked-in fixtures need
no external firmware; [source provenance and regeneration](sim/cia_timing.md)
describe the independent oracle and qualification limits. `sim-fes-c64`
includes this replay, the directed CIA register/port regression and the
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
Before routing, the producer requires live clocks on every memory port.
The native drive needs no inferred-ROM clock repair. The independent drive
ROM uses sixteen single-port lanes; its bank selector preserves one-clock
read latency.

## 1541 component diagnostics

`make sim-fes-c64-drive` checks the production drive motherboard separately with a
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
unimplemented. These same drive, VIA and GCR modules belong to the
production source closure.

`make sim-fes-c64-drive-build` followed by
`python3 scripts/c64_drive_observe.py --rom /path/1541.rom --output /path/ignored-output`
observes a user-supplied exact 16 KiB drive ROM for three simulated seconds.
It retains ROM/executable hashes, the drive RAM and bus/control observations.
The disk electronics use inactive input pins; GCR rotation and active byte-ready,
disk loading and fastloader execution are not exercised by this isolated
component observer. The full boot harness couples the production drive to the
D64 medium and the C64.

### Digital SID envelope regression

`make sim-fes-c64-sid` first checks 21,240,086 envelope edges against a second
source-oracle fixture: all 16 rates, complete fast attack/release, 182,011
writes, 249,426 held clocks and 11 resets. It then replays 15,600,002
system-clock edges across all three
voices, 37,907 selected writes and 300,000 ENV3 reads. Writes occur at native
phase 26 and reads at phase 43. The offline fixture comes from unmodified
VICE 3.10 reSID `EnvelopeGenerator`; the ordinary test needs no reference
emulator, Commodore firmware or recordings. Regenerate it with
`python3 scripts/generate_c64_sid_vectors.py --vice-source /path/to/vice-3.10`.
The generator validates the reference source hashes and retains its GPL
notices in the temporary build. Constant-pulse input checks signed PCM and
linear gain arithmetic; this does not qualify the SID waveform or analog
filter model. The digital envelope starts at the reference's power-up value
`$AA`; reset retains its counter while clearing register/pipeline state.

### Development PRG launch

After proving a program's genuine cold disk LOAD, `--prg-fixture /path/boot.prg`
provides a faster launch for RTL development. The observation boots fresh user
firmware, requires BASIC READY with TXTTAB at `$0801`, and installs an `$0801`
PRG at three simulated seconds. It sets the BASIC program/variable end pointers
and selects device 8. A fresh observation types `RUN` by default; both medium
variants still boot native drive firmware and execute the demo's fastloader.
Programs must fit below `$A000`. The installation is saved separately as
`prg-fixture-ram.bin` and is explicitly labelled as a fixture, not a native LOAD.

PRG bytes can be reused across RTL builds. Full-state checkpoints remain bound
to the exact executable, firmware, disk library and PRG hashes. A fixture
checkpoint must follow installation; restores retain the captured keyboard and
media schedule. They never translate model state across builds. Changing the
PRG rejects restore before model execution. This is a simulation development
path, not runtime save-state or end-to-end disk-loading acceptance.
