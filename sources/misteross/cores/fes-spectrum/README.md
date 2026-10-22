# FES ZX Spectrum

This directory builds four described cores from one shell, each on the
`fes.computer` 1.0 mailbox
([home-computer I/O](../../../mister-packages/docs/computer-io.md)):

| Core | Model | Firmware | RAM | Frame | Contention | Sound |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` | 48K | `spectrum-firmware`, 16 KiB | 48 KiB | 224 x 312 = 69,888 T, 3.5 MHz | ULA, from T 14,335 | beeper |
| `fes.spectrum128` | 128K / +2 | `spectrum128-firmware`, 32 KiB (editor, then 48 BASIC) | 128 KiB | 228 x 311 = 70,908 T, 3.5469 MHz | ULA, from T 14,361, odd banks | beeper, AY |
| `fes.spectrum-plus3` | +2A / +3 (no disk drive) | `spectrum-plus3-firmware`, 64 KiB (ROMs 0-3) | 128 KiB | 228 x 311 = 70,908 T, 3.5469 MHz | gate array, from T 14,361, banks 4-7 | beeper, AY |
| `fes.pentagon` | Pentagon 128 | `pentagon-firmware`, 32 KiB (128, then 48 BASIC) | 128 KiB | 224 x 320 = 71,680 T, 3.5 MHz | none | beeper, AY |

It follows the Apple II pathfinder: late-bound firmware, removable media while
the machine runs, and independently linked expansion cards. The original
cross-component contract is the
[ZX Spectrum pathfinder design](../../../../docs/superpowers/specs/2026-09-28-spectrum-pathfinder-design.md).
No Sinclair or Amstrad ROM is in this repository or a package; FogCast links
the selected firmware image into the blank ROM lanes at download time.

## Machine

- The CPU is the first-party clean-room `fes_z80_nmos` with the original
  Zilog NMOS behavior, including undocumented instructions and flags.
- `rtl/spectrum_ula.sv` owns the T-state clock. It derives each model's CPU
  rate from the 52.224 MHz system clock by a rational enable (875/13056 for
  3.5 MHz, 11823/174080 for 3.5469 MHz) and stretches the CPU clock for
  contended accesses exactly as the ULA holds its clock high.
- `rtl/spectrum_contention.sv` decides which T states may be stretched, from
  the CPU's presented bus cycle. A Sinclair ULA checks T1 of every memory or
  fetch cycle and every internal T state that keeps a contended address on
  the bus; I/O cycles check T1, T2 and, with A0 high, T3/T4, and a ULA port
  (A0 low) is checked at T2. The +2A/+3 gate array contends only memory T1.
  A stretch is decided where the CPU first asks and lasts its whole length.
- Each model's frame length, interrupt length and first paper pixel follow
  Fuse's libspectrum timing table. The interrupt is accepted at instruction
  boundaries 0 to 31 (35 on the 128K and Pentagon), as on the hardware.
- Memory is ROM pages plus 16 KiB RAM banks, each its own memory; only the
  screen banks (5 and 7) carry the ULA's fetch port. The 128K and Pentagon page
  through `$7FFD` (A15 and A1 low): bits 2:0 bank at `$C000`, bit 3 shadow
  screen in bank 7, bit 4 ROM, bit 5 lock. The +2A/+3 decodes `$7FFD` with
  A15:A14 = 01 and `$1FFD` with A15:A12 = 0001; `$1FFD` bit 0 selects the
  all-RAM configurations {0,1,2,3}, {4,5,6,7}, {4,5,6,3} and {4,7,6,3}, and
  bit 2 is the high ROM bit. The 128K/+2 also latches the data of any read
  that decodes as `$7FFD`, as the hardware does.
- Port `$FE` (A0 low): border, MIC, beeper, keyboard half-rows and EAR. With
  no tape signal bit 6 reads back OUT bit 4 (Issue 3 and 128K), or 0 on the
  +2A/+3. Port `$1F` (A7:A5 low) is a built-in Kempston joystick. The AY is
  `$FFFD` (select and read) and `$BFFD` (write) with A1 low.
- Reading an unattached port returns the ULA's floating bus: the bitmap and
  attribute bytes it is fetching, or `$FF` between fetches. The +2A/+3 shows
  it only on ports 1 + 4n up to `$1000`, one T state later and with bit 0
  set; the Pentagon only on port `$FF`, as Fuse models them.
- USB HID key state is mapped by the core. Rows must be quiet for about 1 ms
  before they take effect. Shift is CAPS SHIFT, Control is SYMBOL SHIFT.
  Controller port 0 is the Kempston joystick (A or B fire), with Start as
  ENTER and Select as SPACE; port 1 is Sinclair joystick 1 (keys 6-0).

The timing model was checked against Fuse 1.6.0. All 1,356 Fuse Z80 core
tests produce the same contention points per T state through the NMOS core,
which also fixed the LDIR/LDDR repeat address (DE, as on the chip). Booting
each model's ROM matches Fuse's instruction trace T state for T state over
100 frames, and border, bitmap, attribute, floating-bus and shadow-screen
races sweep the ULA's fetch points pixel-exactly against Fuse on all four
models. These are simulation comparisons, not hardware measurements.

## Video and audio

The ULA renders each 8-pixel chunk in the system domain, so border and
attribute changes land on the exact chunk the hardware shows. A chunk shows
memory written by a cycle whose T1 began by its first T state, and the border
written by an OUT whose T2 began by its last. The visible picture is 320x240:
32 border pixels left and right, 24 lines above and below the 256x192 paper.
`rtl/spectrum_video.v` keeps three frame banks of 16-bit chunks (bitmap byte,
ink and paper, FLASH already applied); HDMI shows the newest complete bank
from its next frame start, so the 50 Hz picture repeats whole frames in
720p60 and never tears. The picture is scaled 3x3 to 960x720
and centred, and the shared session-display plane (`fes_display_plane`,
backed by HPS DDR) blends launcher frames over it.

The beeper level is OUT bit 4 plus MIC low or an EAR input, filtered as a
coupled speaker. The 128K-class AY-3-8912 (`zonx_ay` in AY mode) runs at half
the CPU clock and is DC-blocked. Socket PCM is summed with saturation. Audio
leaves through the shared 48 kHz I2S path on the paired 12.288 MHz PLL.

## Edge sockets

Sockets 1–4 share one request word (`rtl/spectrum_bus.vh`). Each socket pins
32 request and 28 response flip-flops in the same column-24 bands the Apple II
shell uses, so a frozen shell routes both horizontal clock segments into every
socket row. The Go linker layout is `fes.spectrum-bus.sockets/1`. WAIT delays
CPU completion and STROBE is one launch event per access; an expansion
receives the system clock.

`expansions/probe.v` is the open probe card (module `cart`). Socket N owns
ports `$E0+(N-1)*4`: id `$F5`, a scratch register, an access counter and the
socket index, decoded on the low byte. On 128K-class models a port with A15
and A1 low is also `$7FFD`, so the diagnostic addresses the probe as `$80E0`.

## Cassette

Media unit 0 (`fes.media.spectrum-tape`) holds 1..65,536 bytes of `.tap`.
While the unit is ready the player emits EAR edges: 2168 T pilot pulses
(8063 for flags below `$80`, otherwise 3223), sync 667/735, and two 855 or
1710 T pulses per bit, most significant bit first, then a one-second pause.
The next byte is prefetched so byte boundaries retain the same pulse widths.
These are the standard ROM timings recorded in the original
[TZX specification, blocks 10/11](https://worldofspectrum.net/TZXformat.html).
A trailing partial block is not played. MIC writes are not returned to the host.

## Simulation

```sh
make sim-fes-spectrum              # every model's machine, video, board mailbox, tape
make sim-fes-spectrum-machine      # diagnostic firmware on all four models, frame-bank video
make sim-fes-spectrum-board
make sim-fes-spectrum-tape
make sim-fes-spectrum-contention   # downloads the pinned Fuse Z80 tests
make sim-fes-spectrum-rom ROM_MEM_SIM=/path/to/yosys/share/yosys/intel_alm/common/mem_sim.v
```

The machine simulation runs the open diagnostic firmware
(`diagnostic/firmware.py`) on each model: paging through `$7FFD`, AY register
read-back, the keyboard matrix, Kempston port, probe id and scratch register,
the 2168 T pilot, a red border and a black ink pixel. It also checks the
frame T state at which a contended timing loop finishes, against the value
Fuse 1.6.0 gives for the same firmware on each model. The video simulation
streams 50 Hz frames into the 720p60 banks and checks that every HDMI frame
is one whole source frame at the 3x3 geometry. The board simulation drives
`top.v` through the mailbox: identity capability bits 5, 12 and 13, tape
limits 1..65536, a 3-byte commit, HID usage `A` landing on the Spectrum A key
and controller port 1 pressing Sinclair keys. The cassette regression decodes
every EAR pulse for flags `$00`, `$7F`, `$80` and `$FF`, mixed adjacent bytes,
consecutive blocks, partial tails and live eject/replacement.

`sim/run_tb.cpp` is a development runner for a model's machine. It loads a
ROM file and a machine state converted from a `.sna` or `.z80` snapshot by
`sim/spectrum_state.py`, presses keys by frame, writes captured frames as PNG
and palette-index files and can print an instruction trace with frame T
states. These are host simulations, not an RBF, timing or kit result.

## Build variants

```sh
make build-fes-spectrum            # 48K
make build-fes-spectrum128
make build-fes-spectrum-plus3
make build-fes-pentagon
python3 scripts/build_fes_spectrum_oss.py --model 128 --synth-only
```

`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}` selects
the model by the `SPECTRUM_MODEL_*` define. The model, its firmware id, size
and lanes, RAM banks, CPU clock and frame length are bound into its build
identity, and each core has its own FES recipe row. Every model seals only
after the 52.224, 74.25 and 12.288 MHz domains pass. Synthesis must keep the
exact block counts per model (firmware lanes, three frame banks, the plane
FIFO and the CPU-only RAM banks as M10K; the screen banks and the tape image
as TDP blocks: 110/80 for the 48K, 190/96 for the 128K and Pentagon, 222/96
for the +2A/+3), exactly three
unsigned display-plane DSP products and the read-only plane DDR port. The
producer checks every pinned socket boundary and admits compiler-inserted
route-through buffers only at the paired combinational half, with the exact
physical pin map and a dedicated connection to the boundary flip-flop.

The 2026-10-22 host seals of all four packages, with their package
identities, clock results and resources, are in
[the seal record](../../../../docs/validation/2026-10-22-spectrum-models-seal.md).

Firmware lanes 0-15 are column 5 rows 32-47, 16-23 rows 48-55, 24-31 rows
73-80 and 32-63 column 14 rows 32-63. Each is a blank 1024x10 M10K with a
registered read address; the lane selector is registered alongside it and the
output register is stage two. A define, not a generate block, selects the
lanes so routed cells keep their flat `machine.rom.laneN` names.

## Not implemented

The +3 disk drive, TR-DOS, `.tzx` tapes and tape write-back, snapshot
loading through the mailbox, ULAplus, Timex video modes and the IR-register
snow effect. The previous TV80 shell is recorded in
`docs/validation/2026-09-28-spectrum-pathfinder-seal.md`, and a kit ROM link of
the 48K BASIC ROM in `docs/validation/2026-09-28-spectrum-basic-kit.md`. Those
records and the 2026-10-03 native-CPU diagnostics do not accept these models.
