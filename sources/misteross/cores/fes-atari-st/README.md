# FES Atari 520ST

`fes.atari-st` is the first 16-bit FES machine simulation slice. It uses a
real Motorola 68000, a 512 KiB RAM address space, an independently supplied
192 KiB firmware image, an expansion port and the shared pluggable video
parts. This directory has no board shell, RBF producer or registered package
yet. It does not boot TOS or run ST applications: the remaining chipset and
device interfaces are required for that.

The core lane is [Cores](../../docs/cores.md), and the current build boundary
is [the architecture](../../docs/architecture.md#fes-atari-520st).

## CPU and storage

`rtl/st_cpu.sv` uses the shared [FX68K](../fes-common/rtl/fx68k/README.md),
preserved at an exact upstream commit with GPL-3.0-or-later licensing. A
fractional accumulator supplies alternating phase enables for an average
8 MHz CPU from 52.224 MHz without derived clocks. RAM and ROM are separate
request/ready ports on `rtl/st_machine.sv`; storage is outside the machine.
The simulation supplies byte arrays, rather than inferring an oversized
on-chip memory. Board integration requires SDRAM or HPS DDR and arbitration
between CPU and video.

The memory layout follows Atari's
[520ST service manual](https://www.atarimania.com/documents/atari_520st_service_manual.pdf).
The original 68000 has a 24-bit byte address and a 16-bit big-endian data bus:
upper lane D15..8 selects the even byte, lower lane D7..0 selects the odd byte.
Each external port's address is a **word** offset, with byte lanes explicit.
ROM offset zero is the first two bytes of the selected image. A firmware
input is exactly 196,608 bytes; no Atari ROM is distributed.

| Address | Implemented behavior |
| --- | --- |
| `$000000–$000007` | Supervisor reads alias the first eight ROM bytes, including after boot. Writes fault. |
| `$000008–$07FFFF` | 512 KiB RAM window; user access below `$000800` faults. |
| `$FA0000–$FBFFFF` | Read-only cartridge expansion window. Writes fault. |
| `$FC0000–$FEFFFF` | 192 KiB firmware ROM. Writes fault. |
| `$FF8001` | Memory-configuration latch; `$04` describes the 512 KiB bank. The slice's RAM capacity is fixed. |
| `$FF8201`, `$FF8203` | Screen-base high/middle bytes; base aligns to 256 bytes. |
| `$FF820A` | Synchronization-mode latch, even byte. Native PAL/NTSC timing is not reproduced. |
| `$FF8240–$FF825F` | Sixteen palette words, masked to `$0777`. |
| `$FF8260` | Resolution byte: 0 low, 1 medium, 2 high. |
| Other `$FFxxxx` | Supervisor MMIO forwarded to the expansion part. User I/O access faults. |
| Other addresses | Bus error. |

Register addresses and memory configuration are corroborated by EmuTOS's
[screen definitions](https://github.com/emutos/emutos/blob/02c2db75fc875265c1449efea014450466cf20b7/bios/screen.h)
and [memory initialization](https://github.com/emutos/emutos/blob/02c2db75fc875265c1449efea014450466cf20b7/bios/memory.S).
The MMU/GLUE bus schedule and shifter register latch timing are reduced.

## Expansion

The internal FPGA port exports a stable request, 23-bit word address,
16-bit write data, two byte enables, direction, function code, reset and CPU
phase enables. A selected part returns read data plus acknowledge or bus
error. Encoded interrupt priority 0 means none; priorities 1–7 drive the
68000 interrupt inputs, with VPA autovector acknowledgement. This is a
machine RTL connection, not a new host transport or declared package ABI.

The motherboard holds each memory or expansion request until acknowledgement.
CPU DTACK/BERR persists until AS or both data strobes release, and read data
stays latched. Rearming between the data-strobe halves supports the indivisible
TAS read-modify-write cycle. Writes happen once per completed request.
An unanswered request faults after 128 CPU half
cycles. An empty cartridge/MMIO port therefore terminates with a bus error.
The behavior follows the Motorola
[68000 hardware manual](https://www.nxp.com/docs/en/reference-manual/MC68000UM.pdf).

`expansions/st_probe.sv` is an original test part: `$5205,$6800` at
`$FA0000`, a scratch word at `$FF9000`, and two words at `$FF9008/$FF900A`.
`$FF9002` intentionally does not acknowledge and `$FF9004` returns bus error.
The simulation substitutes a vacant port and varies peripheral wait states.
This is source-level composition; a frozen physical socket and independent
part archives still require a qualified producer.

## Video

`rtl/st_video.sv` reads the actual RAM bitplanes: four interleaved words per
16 pixels in 320×200 low mode, two in 640×200 medium, and one in 640×400
monochrome. Bit 15 is the leftmost pixel. The palette has three bits per RGB
channel. High mode bypasses it for black/white, with inversion from palette
entry zero's bit zero. Color borders use entry zero; monochrome borders are
black.

For fixed 720p60, color modes use the full 1280-pixel width and 600-pixel
height (4×3 or 2×3). Monochrome uses 2×1 and is centered vertically. This
integer scaling is a bring-up choice; native ST cadence, raster effects and
aspect-ratio correction are not implemented.

The source emits the existing
[RGB888 video-part request](../../../mister-packages/docs/video-parts.md).
The simulation connects the real shared direct and scanline parts with the
same two registered boundaries. HOLD blacks the picture while timing and
scanline phase continue. The memory read port and configuration use one
clock in this slice. A board shell must supply coherent configuration
transfer and pixel-domain memory access; these connections are not a CDC
implementation or physical video socket.

## Diagnostic and validation

```sh
make -C sources/misteross sim-fes-atari-st
make -C sources/misteross sim-fes-atari-st-machine
make -C sources/misteross sim-fes-atari-st-video
```

`diagnostic/firmware.py` generates two original 68000 firmware inputs under
`build/diagnostics/fes-atari-st/`. The machine test boots them through the
real CPU with fast and delayed ROM/RAM/expansion responses, checks big-endian
byte/word/long and TAS access, RAM limits, supervisor protection, palette/base writes,
CPU-written bitplanes, error handlers and an expansion interrupt. It resets
and replaces the ROM on the same RAM, then checks a vacant expansion fails
cleanly. Success is `$C0DE` at `$000400`; `$000402` identifies a failure stage.
The dedicated video test checks every pixel over complete frames in all three
modes, the two shared output parts, HOLD, invalid modes and RAM bounds.

The machine target verifies all pinned CPU file digests and copies the CPU's
microcode into its private simulator working directory. The system firmware
is a separate input. Verilator 5.032 is the repository baseline; FX68K's
unpacked structs require the documented upstream warning suppressions. The
first-party video simulation builds with `-Wall`.

These are host simulations. There is no synthesis, timing closure, sealed
package or hardware acceptance. The shared FX68K frontend also needs OSS
synthesis qualification. The next integration step is a board memory/video
shell and a qualified producer, followed by the remaining ST chipset.

## Remaining machine devices

MFP timers/interrupts, IKBD keyboard/mouse and ACIA, YM2149 audio, floppy
controller/drives, DMA, MIDI/serial/parallel ports, native HBL/VBL interrupts
and cycle-exact memory contention are absent. CPU bus-master arbitration is
disabled, and the CPU RESET instruction's peripheral output is not connected
to the expansion reset; host reset is. STE features such as the blitter,
enhanced palette and DMA sound are outside this original 520ST slice.
