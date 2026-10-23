# FES Commodore 64

`fes.c64` 0.5.0 is a package-only Commodore 64 pathfinder on `fes.computer`
1.0. It follows the Apple II pattern: late-bound firmware, a live removable
disk, linkable expansion cards, and an in-session display plane. It is not in
the factory image. No Commodore ROM is in this tree. Session display and live
D64 insert/eject passed the [Kit A diagnostic](../../../../docs/validation/2026-10-23-c64-session-display/README.md)
on HIP package `713b811056616b15…` (build `b7bf0ddbbf358a3b…`), with system
52.89/52.224 MHz and pixel 76.49/74.25 MHz.

The sealed 0.4.0 native-drive baseline with the default SID audio stage and
the wired-AND joystick ports is CUDA package `139d53f88b210e6e…` (build
`0d507d2bc92176ba…`) on
`cores/fes-c64/toolchain.lock` (Yosys `3a5accbd`, Mistral `8fcc4cb4`, nextpnr
`9128800f`), seed 4 (seed 5 failed), final analogue signoff system
55.33/52.224 (worst clock slack 1.07 ns), pixel 100.60/74.25, audio
213.72/12.288, from source `bf7d536f`. The #714 merge head `6463e5c0`, with
DDR-masked joysticks, sealed as `3678ad9e7135e327…` (seed 5, system
58.01/52.224); its earlier head `1966dbcf` sealed as `19dff102415d319a…`
(seed 4, system 58.38/52.224).
The 0.4.0 and earlier package seals below do not qualify the 0.5.0 source.
Earlier seals of the 0.4.0 RTL on the shared Yosys `5391eeb1` pin
(`79148f4e2c5a08d5…`, `e64ae33581d79e72…`, `99405f9cda6f4275…`) do not cover
the per-core lock. The earlier 0.4.0 seal was CUDA package
`1d599353eaef5502…` (build `6f865fddebe62184…`), seed 5, system 54.29/52.224.
A CUDA seal is valid on its own (Deano, 2026-10-13); HIP only adds GPUs.
Kit acceptance of those bytes is still open. The keyboard transposition in
[#750](https://github.com/DeanoC/fes/issues/750) is unrelated.

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
  CPU bus strobes occur at system cycle 26 and reads are sampled at cycle 43;
  both pulses are predicted one edge ahead into registers.
  The address decode, the main RAM block-select output and the cartridge
  socket responses are each registered one further system edge; every
  consumer samples them at edge 26 or later, so the bus-level timing is
  unchanged and those paths stay out of the device read mux.
  Three indexed-color buffers retain complete 384×272 PAL frames, including borders for HDMI.
  Each buffer uses 51 explicit 2048×5 M10Ks, with a mailbox handoff across the
  system and HDMI clocks. Only complete frames reach the reader; an unconsumed
  publication keeps both the reader's buffer and queued frame protected.
  Sprite beam X and pointer/row/pending controls are prepared one system clock
  before each dot; guest-controlled inputs keep their dot sampling. Original
  framebuffer addresses and color-source tags travel together for three native
  dots before D020/D021 are resolved, then a complete write packet reaches memory
  one system clock later. Sprite shifts and collisions retain their dot timing.
  D022-D024 and sprite-color write latencies remain unqualified.
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
- MOS6581 SID model: three digital oscillators with triangle, saw, delayed pulse,
  23-bit noise, TEST transitions and floating waveform decay; OSC3 and ENV3
  readback; eight-bit pipelined ADSR; and a fixed-point state-variable filter
  (`c64_sid_svf`) with MOS6581-shaped cutoff and resonance tables, LP/BP/HP
  routing, 3OFF and master volume. The exact reSID DAC, nonlinear filter and
  external output network translation (`c64_sid_audio`) remains available
  behind the `RESID_FILTER` parameter, which the package leaves off.
  The three voices' waveform stages run as three-edge lookup/finish
  sequences on system edges Phi+1..Phi+9 in reSID output order through one
  shared combined-wave table unit, so no table lookup chains with other
  logic within one edge.
  Single-wave digital timing, oscillator hard sync, triangle ring modulation
  and envelopes follow reSID's MOS6581 model. Non-noise combinations use its
  pinned 6581 samples, including saw-selected accumulator feedback and delayed
  pulse comparison. Three-voice output follows the pinned reference's 0, 1, 2
  loop: voice 0 reads source 2 before feedback; voices 1 and 2 read their sources
  after feedback. CONTROL writes see all final phases. This software ordering
  is a compatibility target, not evidence of physical SID voice ordering. The
  default filter is a listening approximation with signed-16 PCM saturation;
  only the opt-in reSID chain follows the reference's default 6581 bias. Noise-combined waveforms include MOS6581 pulse shaping, noise-register
  writeback and TEST falling-edge exceptions from the same model. Other chip
  models and bias settings, paddle reads, SID bus decay and physical audio
  fidelity remain open.
  Unsupported waveform combinations
  retain the previous OR formula; their update timing is not qualified.
- CIA1: keyboard matrix, joystick and IRQ. CIA2: IEC, the VIC bank and NMI.
  Joystick switches pull CIA1 port pins low whatever their direction bits
  select, as the wired-AND port does: port 2 reads through `$DC00` with the
  KERNAL's DDRA `$FF`. Diagnostic stage 7 reads port 2 that way.
  VIC banking follows the effective PA0/PA1 pin levels, including pull-ups
  when their direction bits select inputs; it does not follow the output
  latch alone. The PASS-frame diagnostic checks this input-pin bank selection.
  Both CIAs have timer A and timer B with live counter reads, stopped high-byte
  loading, force-load strobes, continuous and one-shot operation. Timer B can
  count Phi2 or timer A underflows. CNT is held high without external edges,
  so CNT edge modes do not count and the gated timer A mode can count.
  The old MOS 6526 count-enable, force-load, one-shot and interrupt pipelines
  preserve delayed starts, pending counts after STOP and separate ICR flags,
  bit 7 and interrupt-pin timing. Both CIAs receive a shared PAL 50 Hz
  power-supply input for TOD, independent of CPU stalls and raster timing.
  CRA bit 7 selects five/six-pulse division; the BCD clock supports AM/PM,
  hour-read latching, tenth-read release, hour-write stop, tenth-write restart
  and CRB-selected alarm writes with ICR bit 2. Serial shift, FLAG input and
  PB6/PB7 timer outputs remain unimplemented. Port 2 (CIA1 port A) is controller port 0.
- In-session menus use the shared `fes.video.session-display` plane over
  the finished machine video word, backed by read-only `fes.memory.hps-ddr`.
  The plane reports the centred 960×720 machine rectangle and blends launcher
  frames without resetting the CPU or changing media. Closing or faulting the
  plane returns the running machine picture. The package requires the display,
  DDR and display-plane interfaces; software alone cannot add them to older RBFs.
- HDMI 720p60. The 384×272 PAL view is scaled into a centered 960×720
  viewport using rational nearest-neighbor sampling; side pillars are black.
  The fixed RGB lookup uses Philip Timmermann's
  [Colodore PAL calculation](https://www.pepto.de/projects/colorvic/) (February
  2017), common VIC-II luma revision, brightness/contrast/saturation 50/100/50
  and PAL 2.8 to display 2.2 gamma. The graphics-mode simulation checks all 16
  colours through the frame store and HDMI output against an independent
  calculation. Composite chroma mixing, scanlines and monitor adjustments
  remain outside this fixed RGB output. Audio is the shared 48 kHz I2S serializer.

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

The drive's 16 MHz reference enable is predicted one system edge ahead into a
register, as the motherboard does for Phi2, with the same pulse sequence.
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
`--gpu-router` and `--toolchain-root` select the shell's GPU lane. HIP is
the default. CUDA sets `CUDA_DEVICE_ORDER=PCI_BUS_ID`. gpu-run leases any
free GPU; the producer does not set `GPU_LEASE_IDS` or a device visibility
mask. The route must prove that backend, and it must match the backend
recorded on the frozen shell.
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

The shared CPU implements stable NMOS AXS/SBX immediate (`$CB`): it stores
`(A & X) - operand` in X, updates N/Z and no-borrow C, and leaves A, Y, S,
D, I and V unchanged. It consumes two enabled read cycles; decimal mode and
the incoming carry do not alter the subtraction. `make sim-fes-c64-axs`
checks all 65,536 subtraction input pairs, independent A/X masks, flags and
CPU enable gaps. Other undocumented instructions remain incomplete.

`make sim-fes-c64-axs-oracle` adds pinned perfect6502 transistor fixtures:
132 programs across three enable cadences (396 bus/state cases), plus
144 IRQ/NMI entry/stack cases. It checks independent A/X bits, flags,
operand-page and PC wrap, and a short AXS/PLA/BEQ sequence with both branch
outcomes. Normal tests need no external model, firmware or demo. Regenerate
with unmodified perfect6502 commit
`09fc542877a84318291aa42dab143a3e2c3db974`:

```sh
AXS_ORACLE_ROOT=/absolute/path/to/perfect6502
cc -O3 -Wall -Wextra -Werror -I"$AXS_ORACLE_ROOT" cores/fes-c64/sim/axs_oracle.c "$AXS_ORACLE_ROOT/netlist_sim.c" -o /tmp/axs-oracle
/tmp/axs-oracle > /tmp/axs_oracle_vectors.h
cmp /tmp/axs_oracle_vectors.h cores/fes-c64/sim/axs_oracle_vectors.h
```

This bounded opcode check does not establish whole-demo completion or
interrupt-vector hijacking. The external model retains its MIT license.

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

The CPU consumes a registered VIC BA value when every Phi2 interval has at
least 29 system clocks. A phase-26 guest write commits at system edge 27;
BA is captured at edge 28 and is safe for CPU retirement at edge 29.
Shorter intervals retain the direct BA path, and the active write exemption
remains combinational. Raw VIC BA and DMA timing are unchanged.
`make sim-fes-c64-ba-timing` checks retirement against raw BA at each enabled
edge across native 53/54-clock intervals, the 29-clock boundary and faster
fallbacks, with late register writes, sprite/display boundaries and resets.

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
make -C sources/misteross build-fes-c64   # default HIP lane; not part of the sim gate
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

`--sid-trace` records every accepted SID store in `sid-writes.csv`, including
repeated values, register mirrors and both writes of a read-modify-write
instruction. Rows retain the full bus address, register and value, the absolute
zero-based SYS edge, the Phi count since power-on and the interval from the
previous accepted store. Reset does not restart this host epoch.
`--audio-capture` writes signed little-endian mono PCM16 to `audio.pcm` at
48 kHz, sampling the completed SID output after evaluation on SYS edges
1087 + 1088*n. This is host point sampling, without an added anti-alias filter;
it does not capture the board's CDC/I2S output. Both options default off and
may be enabled independently. `capture.json` records the mode and absolute
sample interval; the observation receipt hashes the enabled outputs.

Capture can be enabled or disabled after restore. Absolute sampling phase and
SID store counters are serialized, so successive captured segments concatenate
without duplicating or dropping a boundary sample or store. Output streams are
excluded from state. The private development checkpoint format is now v4;
earlier formats require recapture with the selected executable. The checkpoint
regression checks accepted stores against its original 6502 program, compares
cold/split streams and verifies identical RAM, frames and complete state with
capture enabled and disabled in both medium modes.

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

D020 border and D021 background colors are resolved three native dots after pixel-source
selection, followed by the one-SYS framebuffer packet. The complete original
address and direct-color index travel with explicit border/background source tags.
Standard text, multicolor text/bitmap and ECM background zero retain that tag;
border pixels retain their D020 tag; foreground, other ECM backgrounds, bitmap
cell colors and opaque sprites retain their original direct index, even when
it equals D020 or D021.
CPU register commits, sprite shifts, foreground and collision edges are unchanged.
The pipeline drains during invisible dots before frame publication and is
cancelled by reset. D022–D024 and sprite-color write latency remain outside
this timing correction.

`make sim-fes-c64-d021` runs twenty-four open motherboard programs (twelve per
color register) with the shared
6502 and native 53/54-SYS cadence. It checks all sixteen colors in both
directions, repeated and NMOS read-modify-write stores, architectural readback,
all native write cycles, graphics modes, equal-valued direct colors, sprite
priority, original framebuffer addresses, reset cancellation and frame drain.
The recorded PAL6569R3 transition vectors come from unchanged VICE 3.10 color
functions. Regenerate with
`python3 scripts/generate_c64_d021_vectors.py --vice-source /path/to/vice-3.10`,
adding `--register d020` for the separate border vectors.
The source oracle covers all 65,536 old/new-color and foreground-mask cases;
NTSC and 8565 grey-dot behavior remain unqualified.

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
last display rows. The D021 motherboard test separately qualifies its PAL
write transition; other palette writes, physical subphases and fetch-mode
transitions remain outside these static frame checks.

`make sim-fes-c64-video` checks complete frames with alternating character
colors and all multicolor-bitmap palette selections in all four banks.
`make sim-fes-c64-rom` checks every firmware/character byte and the independent
read clocks. Both are included in the aggregate simulation.

`make sim-fes-c64-vic-timing` checks all 63×8 PAL cycle/dot coordinates at
the native fractional 53/54-clock cadence, writes around each dot, the
three-dot color pipeline, one-SYS framebuffer packet and reset cancellation. It exercises
collisions and complete-frame publication, including the original visibility
of a direct border-color write on the preparation edge. This checks the
internal pipeline contract; the D021 source oracle supplies its separate PAL
write-timing expectation.

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
pinned VICE 3.10 sources. `sim-fes-c64-cia-reference` checks 385 bus programs
at the native fractional cadence and two boundary schedules, including
ordered interrupt edges within a guest cycle. Its checked-in fixtures need
no external firmware; [source provenance and regeneration](sim/cia_timing.md)
describe the independent oracle and qualification limits. `sim-fes-c64`
includes this replay, the directed CIA register/port regression and the
firmware's CPU interrupt checks. These are host simulations, with no sealed
artifact or kit acceptance implied.

`build-fes-c64` authenticates `cores/fes-c64/toolchain.lock` (Yosys `3a5accbd`,
Mistral `8fcc4cb4`, nextpnr `5fe55f3d` from nextpnr #276). The build record uses
the audited static closure in `scripts/source_closures.json`. The seal also requires both
achieved system-PLL outputs within 1 Hz of 52,224,000 Hz and 12,288,000 Hz.
The [C64 build contract](../../docs/architecture.md#fes-commodore-64) states
the packed M/N/K coefficients that fix those frequencies before the placer
seed runs. `--gpu-router CUDA` with
`--toolchain-root` is a separate package; slot cards take those same flags.
The 6 Oct kit diagnostic used
nextpnr `0c5ed400` and does not accept this pin. Supporting evidence from a
HIP build of [#749](https://github.com/DeanoC/fes/pull/749) head
`8b713d5d5716f47bfbc58582957e77ade4878565` on this lock, on Powerboat's
RX 7900 XTX: package
`22ecf46827b3f57b55f7b5e5187167c7177bf29dc931870721aba629639a9dbf`, build
`4c90ff27c072d8fbfa81366f2bd7d9a1`, timing system 54.51/52.22, pixel
82.79/74.25, audio 215.33/12.29. The
[kit B HIL](https://github.com/DeanoC/fes/pull/749#issuecomment-6229821313)
showed BASIC READY and clean video. The recorded seal is the CUDA package
above. That evidence belongs to the earlier core; this 0.4.0 native-drive
shell and both card profiles need fresh shell/card timing and hardware
validation.
The socket check admits a compiler-inserted route-through
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
The drive samples byte-ready for CPU SO at the end of Phi1 and presents that
level at the following Phi1 boundary. The VIA CA1 latch retains the raw pin.
`sim-fes-c64-drive-so` checks 112 phase/pulse cases against observations from
the original NMOS transistor netlist in
[perfect6502](https://github.com/mist64/perfect6502/tree/09fc542877a84318291aa42dab143a3e2c3db974),
including BVC polling, CLV under a held-low pin and the raw VIA path. Another
48 netlist cases check edges around CLV, including the suppressed window and
the CPU cycle in which polling finishes. The drive enables the shared CPU's
optional CLV inhibit so a delayed SO edge cannot undo CLV's flag write.
`sim/so_phase_oracle.c` reproduces these observations with that external
netlist checkout; it is not part of the build or a required test dependency.
Build it using the command in its header. `settled 7 1` observes a one-tick
pulse at the sampling boundary, and `clv 0 0` observes a held-low edge in
CLV's suppressed window. Width zero keeps the pin low; an optional final
argument zero selects a rising-edge negative control. The model toggles
the CPU clock every eight reference ticks, so it qualifies digital half-cycle
behavior without resolving analogue timing within a clock phase.
The shared CPU's optional SO input retains its asynchronous edge interface;
`sim-fes-c64-so` checks both enabled and default-disabled modes. Analogue SO
setup-time margins, other instruction-specific SO inhibit windows and VIA serial
shifting remain unimplemented. These same drive, VIA and GCR modules belong to the
production source closure.

`make sim-fes-c64-drive-build` followed by
`python3 scripts/c64_drive_observe.py --rom /path/1541.rom --output /path/ignored-output`
observes a user-supplied exact 16 KiB drive ROM for three simulated seconds.
It retains ROM/executable hashes, the drive RAM and bus/control observations.
The disk electronics use inactive input pins; GCR rotation and active byte-ready,
disk loading and fastloader execution are not exercised by this isolated
component observer. The full boot harness couples the production drive to the
D64 medium and the C64.

### SID regression

`make sim-fes-c64-sid` runs digital and staged-audio portable tests, using checked-in vectors and
no reference emulator, Commodore firmware or recordings:

- `sim-fes-c64-sid-envelope` checks 21,240,086 envelope edges: all 16 rates,
  complete fast attack/release, 182,011 writes, 249,426 held clocks and 11 resets.
- The existing three-voice envelope/gain replay checks 15,600,002 SYS edges,
  37,907 writes and 300,000 ENV3 reads. Its fixed 52-clock test schedule writes
  at offset 26 and reads at offset 43; constant pulse output isolates the
  envelope and signed linear-gain arithmetic.
- `sim-fes-c64-sid-oscillator` checks 614 scenarios over 11,793,999 SYS edges,
  10,183,907 Phi2 edges and 887,119 writes. It covers frequency extremes, noise
  bit-19 pipeline timing, TEST rise/fall and long reset holds, pulse-width
  updates, each single waveform, floating-output decay, warm reset, all
  relative write phases and simultaneous clock/register events. CPU writes
  prepare all 4,096 table addresses for every non-noise combination, including
  both ring settings, delayed pulse levels, phase feedback and repeated CONTROL.
  All seven noise combinations, all 256 selector-pair TEST falls and seeded
  writes across all 16 selectors extend the original 80 scenarios.
- `sim-fes-c64-sid-osc3` checks 1,200,010 SYS edges, 1,194,495 OSC3 reads and
  4,332 accepted writes across three voices, including deselected writes and
  warm resets. Its fractional 52.224 MHz / 985248 Hz clock uses 53/54 SYS
  periods. Writes land 27 SYS edges after Phi2: the motherboard's comparison
  with `cycle_clock==26` observes the old register value.

- `sim-fes-c64-sid-coupling` compares all three digital oscillator states,
  and pre-write audio waveforms over 2,858,439 SYS edges and 1,234,963 Phi2 edges.
  It exercises all 64 simultaneous
  MSB-rise/SYNC masks, both source-MSB values for every triangle-ring voice,
  sync suppression, TEST-held edge latches, fractional 53/54 cadence,
  writes at every relative SYS phase, and simultaneous reset/clock/write.
  Combined-saw feedback covers every source/destination rotation, same-Phi
  CONTROL writes, last-idle phase preparation, native Phi+27 bus writes and
  CS-blocked writes. All 16 selectors are covered, including noise-selected
  saw feedback. Direct and prepared variants share the reference fixture.

The oscillator runs on `cpu_ce`, including VIC CPU stalls, rather than the
BA-qualified CPU execution enable. A SYS edge first advances the oscillator
on Phi2 using its old registers, then accepts the bus write. Reset takes
priority over both. `$D41B` and its motherboard mirrors read the upper eight
bits of voice 3's digital waveform without a side effect. The direct fallback mixer uses the
same upper eight bits. The noise shift register uses taps 22/17 and output
bits 20/18/14/11/9/5/2/0; a rising accumulator bit 19 shifts it after two Phi2
edges. TEST holds phase at zero, resets the noise register through the
MOS6581 bit-fade sequence, and applies the falling-edge shift. Pulse output
retains the reference's one-cycle comparator pipeline. Noise combinations
mask the selected non-noise waveform with the tapped noise output; noise/pulse
also applies the pinned MOS6581 shaping function. With TEST clear, output bits
feed back into the eight tapped shift-register cells except during shift phase
one. A falling TEST edge applies the model's selector-dependent pre-writeback
before shifting. These model checks do not qualify physical mixed-noise audio.

Three-voice hard sync first advances every phase and noise pipeline using
old registers, then clears the eligible destination phases, then calculates
waveform output from the resulting phases. A source which is itself synced
on that edge does not sync its destination. Triangle ring modulation uses
the pinned output-loop order: voice 0 sees source 2 before combined-saw
feedback, while voices 1 and 2 see their sources after feedback. CONTROL
writes on Phi2 see all post-feedback sources; idle writes see current phases.
This reproduces software ordering without qualifying physical SID ordering.
The reference's MSB-rise latch is retained while TEST holds a source.
No guest clock or register-write acceptance edge moves.

The motherboard prepares each oscillator's next phase on idle SYS edges when
`SYSTEM_CLOCK_HZ / CPU_CLOCK_HZ >= 2`. Frequency writes on the final idle edge
and TEST or combined-wave phase clears are forwarded into that sum, including
unchanged CONTROL writes; reset prepares the preserved
phase with zero frequency. This shortens the path through cross-voice sync and
ring logic without moving Phi2, CPU writes or waveform updates. Standalone SID
instances default to the direct phase calculation. Ratios below two retain
that direct calculation because they can produce adjacent Phi2 enables.
`make sim-fes-c64-sid-phase` replays the same source fixture with physical idle
edges between dense logical Phi2 calls and checks the sum at every Phi2.

With `SEQUENCE_VOICES`, each oscillator defers
its waveform stage (output wave, saw-selected feedback, noise write-back,
decay and pulse compare) from Phi2 to a three-edge lookup/finish sequence:
the lookup edge presents the voice's combined-wave table index and ring state
to one shared table unit (`c64_sid_wave_tables`), which registers the index
and then the four table values, and the second edge after the request
finishes the stage from them. Voice 0 looks up on Phi+1, voice 1 on Phi+4
and voice 2 on Phi+7. Phi2 still advances every accumulator, noise
shift and hard sync. Each ring/sync source is then simply the source's current
phase, which reproduces the pinned reSID order without chaining three
combined-wave lookups in one system period. A CONTROL write that selects a
waveform issues its own lookup on the write edge and finishes two edges
later; the prepared phase sum is refreshed on the following idle edge.
The voice pipeline needs twelve SYS edges. The motherboard samples SID writes
at edge 27, so that finish falls on edge 29 and the refresh needs edge 30
before the next Phi. Sequencing is enabled only when
`SYSTEM_CLOCK_HZ / CPU_CLOCK_HZ` is at least 31, which leaves that idle edge
on every Phi gap, including the longer gap of a non-integer ratio. A 30-edge
gap would advance from the pre-feedback sum. The
audio stage captures on Phi+10 and starts on Phi+11. Those writes sit outside the
stage window. `make sim-fes-c64-sid-sequenced`, `sim-fes-c64-sid-sequenced-osc3`
and `sim-fes-c64-sid-sequenced-mix` replay the unchanged coupling, OSC3 bus and
ENV3/PCM fixtures with the stage, finish and idle edges folded into each
logical Phi2 or write step. Standalone instances default to the single-edge
evaluation with per-instance table lookups.
`make sim-fes-c64-sid-svf` checks the default filter sequencer and its shared
nibble-serial multiplier bit-exactly against the same fixed-point equations over
1,480,000 random register states, plus DC response and table monotonicity.
`scripts/c64_sid_svf_tables.py` regenerates `rtl/c64_sid_svf_tables.vh`.

The same cadence guard prepares the envelope's first state-pipeline transition
on idle SYS edges. Its 20-bit helper holds only the next state-pipeline count,
state, rate period and zero-hold flag. Gate and attack/decay/sustain/release
writes on the last idle edge are forwarded before this transition is prepared.
Phi2 consumes that helper before advancing the remaining envelope logic, then
applies CPU writes; reset retains the original envelope value and ENV3 sample.
Standalone envelopes and ratios below two keep the direct calculation.
`make sim-fes-c64-sid-envelope-prepared` replays the unchanged 21,240,086-edge
source fixture with physical idle edges, while `sim-fes-c64-sid-prepared`
checks the three-voice ENV3 and PCM fixture with both preparations enabled.
This changes internal timing structure; routed timing and hardware acceptance
require separate evidence.

The oracle links unchanged VICE 3.10 reSID `WaveformGenerator` and
`EnvelopeGenerator`, by Dag Lem, under GPL-2.0-or-later. The adapted RTL and
source-linked oscillator harnesses use the same license; the complete text
is [c64_sid.COPYING](rtl/c64_sid.COPYING). The generator checks every compiled
reference dependency, including both model waveform-table sets, before
copying them into a temporary build. It does not call RTL or duplicate the
waveform equations. All four non-noise combination tables are emitted as exact
4,096-entry RTL lookups with zero entries represented by the default case.
Noise combinations remain outside the qualified subset.

Reference: [VICE 3.10 source archive](https://github.com/VICE-Team/svn-mirror/releases/download/3.10.0/vice-3.10.tar.gz),
SHA256 `8e5bac18cbcb9f192380ad3ef881f8790f5b75c41d7b3da65d831985d864d6d1`.
From `sources/misteross`, regenerate or verify all five fixtures and the four waveform tables:

```sh
python3 scripts/generate_c64_sid_vectors.py --vice-source /path/to/vice-3.10
python3 scripts/generate_c64_sid_vectors.py --vice-source /path/to/vice-3.10 --check
make sim-fes-c64-sid
```

`--component oscillator-table` selects all four RTL non-noise combination lookups.
`--component oscillator` and `--component oscillator-bus` select the
single-voice and bus fixtures; `coupling` selects the three-voice sync/ring and
combined-wave fixture; `sid` and `envelope`
select the prior tests. `--output` selects one
fixture's destination. The oscillator regression fingerprints every SYS
state using 64-bit FNV-1a blocks, with explicit ending states for diagnosis.
These are regression checks, not cryptographic attestations.
`C64_OSC_TRACE_FROM` and `C64_OSC_TRACE_TO` print an inclusive SYS-edge range
from either the source oracle or the oscillator replay.

reSID's noise bit-fade operation leaves unused host-integer bits above bit
22 set. The comparison masks those bits to the physical 23-bit register,
and ignores the reset countdown only when TEST is clear or all 23 bits have
become one. In either case the countdown cannot change the visible output;
the next TEST rise starts it again. Phase, waveform, OSC3, register state,
shift pipeline, pulse pipeline and every active decay transition remain
compared. Power-up phase `$555555` and envelope `$AA` are deterministic
reSID fixtures, not measured hardware startup values. Warm reset retains
phase and the envelope counter while clearing their register/pipeline state.

The qualified software-model oscillator subset includes all 16 waveform
selectors, with three-voice hard sync and triangle ring modulation. Pulse/triangle applies ring index
substitution; saw-selected combinations ignore ring, calculate output before
accumulator-MSB feedback, and compare the next pulse level after feedback.
The existing MSB-rise latch and noise clock are not recalculated after a clamp.
CONTROL writes repeat waveform evaluation and feedback even when unchanged.
Noise combinations apply the selected waveform mask, MOS6581 noise/pulse
shaping and gated shift-register writeback. TEST falling applies the pinned
selector-pair pre-writeback exceptions. This extends model coverage without
qualifying physical combined-noise audio.
Paddles and write-only bus decay remain unimplemented. The standalone SID
defaults to the direct linear-volume audio path; the native motherboard enables
the staged MOS6581 path described below. Host replay does not establish audible
HDMI or real-SID fidelity. Whole-core mapping, routed timing and hardware
acceptance remain separate checks.

### Staged MOS6581 audio

The native motherboard enables waveform/envelope DACs, the nonlinear internal
filter and the external output network. It uses the pinned reSID MOS6581 model
at its default bias, with external input held at zero. Filter routing, cutoff,
resonance, voice-3 disable, modes and volume are guest-controlled. Each Phi2
captures the post-clock, pre-write waveforms and old filter controls; the next
SYS edge starts processing with the updated envelopes. A simultaneous CPU
write therefore affects the following sample. Reset discards pending work.
The signed-32 result is saturated at the signed-16 PCM boundary.

Output completes 39 SYS edges after Phi2. `FILTER_AUDIO` is enabled only when
`SYSTEM_CLOCK_HZ / CPU_CLOCK_HZ >= 41`; native PAL uses 53/54 SYS intervals.
Shorter clock configurations and default standalone SID instances retain the
direct linear-volume audio path. The shared 48 kHz I2S transport is unchanged.

The generated tables keep VCR current and cutoff entries exact. Piecewise
interpolation bounds each lookup error to 4 integer units for gain/summer,
16 for the mixer, and 1 for reverse-opamp/kVg. These are table-entry bounds,
not bounds on final PCM error. `scripts/generate_c64_sid_audio_tables.py`
reproduces the three RTL headers from hash-pinned, unchanged VICE sources.
`scripts/generate_c64_sid_audio_vectors.py` regenerates the independent
portable fixture using the same documented table policy. Both accept
`--vice-source /path/to/vice-3.10 --check`.

`make sim-fes-c64-sid-audio` checks direct and prepared digital implementations
with the opt-in reSID chain (`RESID_FILTER`) enabled over 23,052 Phi2 clocks and
1,257,472 SYS edges each.
It checks complete signed-32 output, PCM, OSC3, ENV3, bus reads and done/busy,
including writes, reset/restart and 41/53/54/64-SYS cadence. Only the reference's
indeterminate initial ENV3 sample is excluded until the first non-reset Phi2;
all later samples, including warm-reset retention, are compared. Other bus
registers retain this core's zero-read contract, not SID floating-bus behavior.
Other chip models/bias, paddles, bus decay, physical audio and current-package
timing/hardware acceptance stay open.

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
