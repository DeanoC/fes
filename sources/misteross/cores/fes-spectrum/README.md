# FES ZX Spectrum

This directory builds four described cores from one shell, each on the
`fes.computer` 1.0 mailbox
([home-computer I/O](../../../mister-packages/docs/computer-io.md)):

| Core | Model | Firmware | RAM | Frame | Contention | Sound |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` | 48K | `spectrum-firmware`, 16 KiB | 48 KiB | 224 x 312 = 69,888 T, 3.5 MHz | ULA, from T 14,335 | beeper |
| `fes.spectrum128` | 128K / +2 | `spectrum128-firmware`, 32 KiB (editor, then 48 BASIC) | 128 KiB | 228 x 311 = 70,908 T, 3.5469 MHz | ULA, from T 14,361, odd banks | beeper, AY |
| `fes.spectrum-plus3` | +2A / +3 with its disk drive | `spectrum-plus3-firmware`, 64 KiB (ROMs 0-3) | 128 KiB | 228 x 311 = 70,908 T, 3.5469 MHz | gate array, from T 14,361, banks 4-7 | beeper, AY |
| `fes.pentagon` | Pentagon 128 with Beta 128 | `pentagon-firmware`, 48 KiB (128, 48 BASIC, then TR-DOS) | 128 KiB | 224 x 320 = 71,680 T, 3.5 MHz | none | beeper, AY |

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
  contended accesses exactly as the ULA holds its clock high. While the tape
  plays, T states run at turbo speed (see Cassette).
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

While the tape loads in turbo, frames come about three times faster than
HDMI shows them, so the picture skips frames, and the sound is muted.

The beeper level is OUT bit 4 plus MIC low or an EAR input, filtered as a
coupled speaker. The 128K-class AY-3-8912 (`zonx_ay` in AY mode) runs at half
the CPU clock and is DC-blocked. Socket PCM is summed with saturation. Audio
leaves through the shared 48 kHz I2S path on the paired 12.288 MHz PLL.

## Edge sockets

The Spectrum has one rear edge connector, so the shell has one socket. It
sees the registered request word (`rtl/spectrum_bus.vh`) and pins 32 request
and 28 response flip-flops in the Apple II shell's slot 2 band (column 24,
rows 1–18), so a frozen shell routes both horizontal clock segments into every
socket row. The Go linker layout is `fes.spectrum-bus.sockets/2`. Four sockets
down column 24 (`sockets/1`) left the shell only three two-row gaps to route
across once nextpnr kept shell routing out of reserved regions. WAIT delays
CPU completion and STROBE is one launch event per access; an expansion
receives the system clock.

`expansions/probe.v` is the open probe card (module `cart`). It owns ports
`$E0`–`$E3`: id `$F5`, a scratch register, an access counter and the socket
index (1), decoded on the low byte. On 128K-class models a port with A15
and A1 low is also `$7FFD`, so the diagnostic addresses the probe as `$80E0`.

## Cassette

Media unit 0 (`fes.media.spectrum-tape` 2.0) holds a PZX stream of up to
16 MiB in HPS DDR, at window offset 16 MiB on port 1 (`fes_media_ddr`). The
host converts `.tzx`, `.tap`, `.csw` and `.pzx` images to that stream so it
plays as the original plays in Fuse 1.6.0 (FogCast `internal/spectrumtapes`);
turbo and custom loaders, generalized data, direct and CSW recordings play
as recorded. `rtl/spectrum_tape.v` decodes the stream a byte at a time through
a four-event queue and plays each pulse on the free-running T-state clock, so
contention does not slow the tape and every edge lands on a T-state edge.
DATA pulse tables (up to 255 pulses per bit) are held in one M10K.

The tape starts and stops by Fuse's loader detection: 10 successive reads of
a ULA port at most 500 T apart, with B counting one up or down each time,
start it, and 2 reads more than 1000 T apart or with B jumping stop it. A
tape therefore waits at its start until a loader runs, and pauses when the
program stops loading. STOP blocks stop it (flag 1 only on the 48K) and the
end of the tape stops and rewinds it. MIC writes are not returned to the host.

While the tape plays, the machine runs in turbo: each T state takes 5 system
clocks instead of 14 or 15, so the CPU runs at 10.44 MHz and a tape loads
about 3 times faster (2.98x on the 48K and Pentagon, 2.94x on the 128K-class
models). Everything else is counted in T states, so software cannot tell:
contention, the frame interrupt, the screen and floating bus, the tape
pulses, the AY and the disk controllers' timers. The ULA reads screen memory
in the first three clocks of a T state, after a CPU write from the T state
before lands and before the next one can, and reads the floating-bus byte a T
state ahead. 5 clocks is the shortest exact T state: the CPU presents a new
bus cycle two clocks after ce_p, the contention decision registers it a clock
before tick_n, and ce_n and ce_p may not be on consecutive clocks. The machine
returns to its own speed at the T state after the deck stops, so a game that
stops the tape runs at its own speed from then on.

## Snapshots

The cassette unit also loads `.z80` (versions 1-3), `.sna` (48K and 128K)
and `.szx` (ZX-State 1.x, as Fuse writes it) snapshots. The host converts a snapshot into a restore stream (FogCast
`internal/spectrumsnapshots`, also `cmd/fes-spectrum-restore`): an 8-byte
header, `ZXRS`, version 1, the snapshot's class (0 48K, 1 128K, 2 +2A/+3)
and two zero bytes, followed by Z80 code. `rtl/spectrum_snapshot.v` reads the
first 8 bytes of every image. A tape goes to the deck after the stream
rewinds. A restore stream of a class the model runs resets the machine, and
then every CPU memory read, at any address, takes the next stream byte; WAIT
holds a read while the media ring is empty. Writes, I/O and contention are
unchanged. The code:

- fills RAM with `LD (HL),n` / `INC HL`: a 48K snapshot fills `$4000-$FFFF`,
  which every model maps to banks 5, 2 and 0 after reset; a 128K-class one
  pages each bank in at `$C000` through `$7FFD`;
- sets the border, and on 128K-class snapshots all 16 AY registers and the
  selected register;
- writes `$1FFD`, then `$7FFD`. On the 128K and Pentagon `$1FFD` decodes as
  `$7FFD`, so the second write replaces it; the 48K has neither. A 48K
  snapshot on a paged model selects 48 BASIC (ROM 3 on the +2A/+3) and locks
  paging; a 128K snapshot's ROM 1 is ROM 3 on the +2A/+3;
- loads the alternate and main registers, IX, IY, I and IM, sets R so that it
  holds the snapshot's value at the first fetch from real memory, pops AF from
  the stream, loads SP, executes EI if IFF1 was set, and jumps to PC.

Restore ends when the image's last byte has been read; a 128 KiB snapshot
takes about 0.64 s. It runs again whenever the machine leaves reset with the
snapshot inserted, and ejecting it gives the normal ROM boot. Packages
declare the optional interfaces `fes.media.spectrum-snapshot` 1.0 (every
model), `fes.media.spectrum-snapshot-128` 1.0 (128K-class models) and
`fes.media.spectrum-snapshot-plus3` 1.0 (+2A/+3); the host refuses a snapshot
the package does not run, and the core leaves a stream of another class to
the deck. IFF2 is restored only as a copy of IFF1, and the T state within the
frame is not restored. 16K, SamRam, MGT, Scorpion, Didaktik, Timex and
Spectrum SE snapshots are refused, as are snapshots with the Interface 1 or
TR-DOS ROM paged in. The `.szx` reader also refuses +3e, 128Ke, Pentagon 512
or 1024, and NTSC 48K snapshots; halted Z80 state; an enabled ULAplus palette;
a paged Multiface, DivIDE, DivMMC, +D or Opus; active Spectranet paging or its
programmable trap; an Interface 2 cartridge; or a custom ROM, LEC, ZXATASP or
ZXCF chunk.

## TR-DOS disks

The Pentagon has a Beta 128 disk interface (`rtl/spectrum_beta.sv`) on
`fes.media.spectrum-disk` 1.0, media unit 1, beside the cassette, and declares
`fes.media.spectrum-disk-trdos` 1.0. FogCast converts `.trd` and `.scl`
images into the unit's raw track image (`internal/spectrumdisks`, also
`cmd/fes-spectrum-disk`): every track's MFM bytes from the index hole, with
TR-DOS gaps and 2:1 interleave, as Fuse 1.6.0's `disk.c` builds them. The
image is kept at HPS DDR window offset 32 MiB, through `fes_media_ddr`'s
byte fetch port on DDR port 2.

- TR-DOS pages in over the ROM on an opcode fetch from `$3D00-$3DFF` and out on
  one from `$4000` up, both only while `$7FFD` selects 48 BASIC, as Fuse does.
  While it is paged, ports `$1F`, `$3F`, `$5F`, `$7F` and `$FF` are the Beta's,
  over the Kempston port and the Pentagon's `$FF` video read.
- The FD1793 follows Fuse's `wd_fdc.c` with the Beta wiring: READY and the
  motor are the head load, which drops 3 s after a command ends; Restore,
  Seek, Step, Step In/Out with update, 6/12/20/30 ms step rates, verify with a
  15 ms settle and five revolutions; Read Sector single and multiple with the
  30 ms E delay; Read Address; Read Track from index to index; Force
  Interrupt (immediate and at the next index). A Type II or III command on an
  unloaded head ends at once. The system register selects drive (only A holds
  a disk; B-D are empty, each keeps its own head position), side, HLT and MFM.
- Unlike Fuse, which hands the CPU the next byte whenever it reads the data
  register, the disk turns: one track byte passes the head every 112 T (300
  rpm on a 6250-byte track), with a 10 ms index pulse, and a byte the CPU has
  not read when the next arrives sets Lost Data. The controller decodes
  address marks from the image's missing-clock bits and checks every CRC.
- The disk is write protected: Write Sector and Write Track end with Write
  Protect.
- A reset with a disk inserted selects 48 BASIC and pages TR-DOS in, so TR-DOS
  boots the disk, as Fuse's disk autoload does; without one the 128 ROM
  starts with TR-DOS out. The host inserts a launch disk after the machine
  starts, with the image's boot flag set: when it becomes ready the Beta
  resets the machine for 64 clocks into that boot. A launch disk without a
  `boot` program gets Fuse's autoload loader, which runs its first BASIC
  program. A disk changed while the machine runs only changes the medium.

## +3 disks

The +2A/+3 has a uPD765A (`rtl/spectrum_upd765.sv`) on the same unit, with
`fes.media.spectrum-disk-plus3` 1.0. FogCast converts standard and extended
`.dsk` images as Fuse 1.6.0's `disk.c` opens them: the +3 gap layout,
recorded CRC errors and deleted marks, data in the gaps, weak sectors from
their multiple copies, and FM tracks. A dump's single copy of a Speedlock
weak sector (cylinder 0, sector 2, with a data CRC error) reads weak in the
bytes Fuse's Speedlock hack corrupts: bytes 29 and 58 of an E5-filled
sector, otherwise every 29th byte, the first eight of them. A track longer than 6,912 bytes makes
the image use 16 KiB slots. The Beta 128 and the uPD765A share the drive,
`rtl/spectrum_fdd.sv`.

- `$2FFD` (A15:A12 `0010`, A1 low) reads the main status register and `$3FFD`
  (`0011`) is the data register; `$1FFD` bit 3 runs the motor. The +3 wires
  only US0: drive A holds the disk, and drive B is empty, write protected
  and never ready, as in Fuse.
- The controller follows Fuse's `upd_fdc.c` in non-DMA mode: Read Data and
  Read Deleted Data with multi-track and skip, Read Diagnostic, Read ID,
  Recalibrate (77 steps), Seek, Sense Interrupt Status, Specify, Sense Drive
  Status and Version, with the head load and unload times. Without a terminal
  count, a read ends at its last sector with End of Cylinder, as on the +3. An
  ID search gives up after two index pulses. Scan is an invalid command.
- As on the Beta, the disk turns: one byte every 112 T, and one the CPU has
  not read when the next arrives ends the read with Overrun. The drive is
  ready 400 ms after the motor starts and stops 300 ms after it stops. A weak
  byte reads differently each time, as Fuse mixes in random bits.
- The disk is write protected: Write Data, Write Deleted Data and Format end
  with Not Writable.
- A launch disk resets the machine. Once the menu has read every keyboard
  half-row in a frame, ENTER is held through the third frame after, so the
  menu's Loader boots the disk, as Fuse's phantom typist does.

## Simulation

```sh
make sim-fes-spectrum              # every model's machine, video, board mailbox and DDR tape, deck, Beta 128
make sim-fes-spectrum-machine      # diagnostic firmware and snapshot restore on all four models, frame-bank video
make sim-fes-spectrum-board
make sim-fes-spectrum-tape
make sim-fes-spectrum-contention   # downloads the pinned Fuse Z80 tests
make sim-fes-spectrum-rom ROM_MEM_SIM=/path/to/yosys/share/yosys/intel_alm/common/mem_sim.v
```

The machine simulation runs the open diagnostic firmware
(`diagnostic/firmware.py`) on each model: paging through `$7FFD`, AY register
read-back, the keyboard matrix, Kempston port, probe id and scratch register,
a red border and a black ink pixel. Its own turbo loader, which counts port
`$FE` reads in B as tape loaders do, must start the diagnostic tape through
the deck's loader detection, see its 1200 T pilot and decode the four bytes of
its 350/700 T turbo block. It also checks the
frame T state at which a contended timing loop finishes, against the value
Fuse 1.6.0 gives for the same firmware on each model, and that a FLASH
attribute swaps ink and paper every 16 frames. The diagnostic tape plays in
turbo, and each model runs again with every T state in turbo (`--turbo`),
which must give the same results. The snapshot simulation repeats this check.
It writes a synthetic `.z80` for each model (48K on the 48K and Pentagon, 128K
with interrupts in IM 2, +2A/+3 in an all-RAM mode), converts it with
`cmd/fes-spectrum-restore` (Go), streams it with random gaps and checks every
register, the paging ports, border, AY registers and all RAM at the first
fetch from real memory, then that the restored program runs and restores
again after a machine reset with the snapshot still inserted; each model also
leaves a stream of a class it cannot run to the deck. The Beta 128 simulation
drives `spectrum_beta` through its ports, as TR-DOS does, against a synthetic
80-cylinder `.trd` converted with `cmd/fes-spectrum-disk`, from a fetch port
with varying delay: Restore and Seek with verify and their timing, Read
Sector on both sides at 112 T a byte, Read Address, Read Track, multi-sector
reads ending at the track's last sector, Record Not Found after five
revolutions, Write Protect, Lost Data, a CRC error, a verify on the wrong
track, Force Interrupt immediately and as the next index pulse ends, an
empty drive B, a Type II command on an unloaded head, the head unloading
after 3 s and the launch image's boot pulse. The uPD765 simulation drives `spectrum_upd765`
through `$2FFD` and `$3FFD` as +3DOS does, against a synthetic extended `.dsk`:
the motor's ready delay, Specify, Recalibrate and Seek with Sense Interrupt
Status, Sense Drive Status, Read ID, one sector and a run ending with End of
Cylinder at 32 us a byte, a missing sector after two revolutions, Overrun, a
data CRC error, a bad ID, a deleted sector, a weak sector, Read Diagnostic,
Write Data on a protected disk, Version, invalid commands and the boot pulse.
A mailbox simulation checks the two units of the Pentagon and +2A/+3: each
unit's Info, a transfer to each, transfers bound to their unit and one at a
time, and an eject of one unit leaving the other alone. The video simulation
streams 50 Hz frames into the 720p60 banks and checks that every HDMI frame
is one whole source frame at the 3x3 geometry. The board simulation drives
`top.v` through the mailbox and a model of the fpga2sdram ports
(`cores/fes-common/sim/hps_ddr_media_model.v`, which refuses commands and
delays data at random): identity capability bits 5, 12 and 13, tape limits
10..16 MiB, the diagnostic tape committed in two chunks so a byte pair
straddles two DDR words, its bytes in the DDR window, the 1200 T pilot played
back from DDR and the bytes the firmware's loader decodes from it, no DDR
protocol or window violation, HID usage `A` landing on
the Spectrum A key and controller port 1 pressing Sinclair keys. The tape
simulation plays PZX images through the deck from a stream that stalls at
random and compares EAR, T state for T state, with an independent expansion
of the format: ROM blocks with a full header pilot, every PULS encoding, DATA
pulse tables of up to 255 pulses with zero-length entries and partial bytes,
PAUS levels, skipped blocks, both STOP flags (once as a 48K), the end and
rewind, eject, the loader-detection thresholds and a heavily stalled stream.

`sim/run_tb.cpp` is a development runner for a model's machine. It loads a
ROM file, a machine state converted from a `.sna` or `.z80` snapshot by
`sim/spectrum_state.py` and a PZX tape (`--tape`), reports when the tape
starts and stops, presses keys by frame, writes captured frames as PNG
and palette-index files and can print an instruction trace with frame T
states. With `--disk` it inserts a raw track image (with `--disk-frame N`
at frame N, as a host inserts a launch disk after Start or a disk while the
machine runs). These are host simulations, not an RBF, timing or kit result.

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
FIFO, the CPU-only RAM banks, the tape's prefetch ring and the deck's pulse
tables as M10K; the screen banks as TDP blocks: 113/16 for the 48K, 193/32
for the 128K, 209/32 for the Pentagon, 225/32 for the +2A/+3), exactly three
unsigned display-plane DSP products, a read-only plane DDR port 0, the tape
on port 1, and port 2 the Pentagon's disk or tied off. The
producer checks every pinned socket boundary and admits compiler-inserted
route-through buffers only at the paired combinational half, with the exact
physical pin map and a dedicated connection to the boundary flip-flop.

The host seals of all four packages with TR-DOS disks on the Pentagon, with
their package identities, clock results and resources, and their kit checks
are in [the TR-DOS record](../../../../docs/validation/2026-10-25-spectrum-trdos.md);
snapshot loading is in
[the snapshot record](../../../../docs/validation/2026-10-25-spectrum-snapshots.md);
the tape path's design is in
[the tape record](../../../../docs/validation/2026-10-22-spectrum-tape-ddr.md);
the models' Fuse timing comparisons are in
[the models record](../../../../docs/validation/2026-10-22-spectrum-models-seal.md).

Firmware lanes 0-15 are column 5 rows 32-47, 16-23 rows 48-55, 24-31 rows
73-80 and 32-63 column 14 rows 32-63; the Pentagon uses lanes 0-47. Each is a blank 1024x10 M10K with a
registered read address; the lane selector is registered alongside it and the
output register is stage two. A define, not a generate block, selects the
lanes so routed cells keep their flat `machine.rom.laneN` names.

## Not implemented

Disk writes, the +3's drive B and Scan commands, Beta drives B-D, the Pentagon's
magic NMI button, tape write-back, a switch to load at the machine's own speed, saving snapshots, ULAplus, Timex video modes and
the IR-register snow effect. The previous TV80 shell is recorded in
`docs/validation/2026-09-28-spectrum-pathfinder-seal.md`, and a kit ROM link of
the 48K BASIC ROM in `docs/validation/2026-09-28-spectrum-basic-kit.md`. Those
records and the 2026-10-03 native-CPU diagnostics do not accept these models.
