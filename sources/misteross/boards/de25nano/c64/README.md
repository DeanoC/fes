# DE25 standard Commodore 64

This lane brings the FES PAL Commodore 64 to the DE25-Nano on the managed
MENU/ZX81 board recipe: the same GHRD, HDMI shell, kernel-owned allocated
display and atomic computer transport at peripheral offset `0x10300`, through
the shared machine builder `../machine/board.py` that the Spectrum and Atari
ST use. Its producer is `../producers/build_c64_package.py`, selected by the
`de25-nano` `fes.c64` recipe. Nothing here programs a kit.

## What differs from the DE10 core

The machine sources under `cores/fes-c64/rtl` are compiled as they are, apart
from the derivations below. Two things the DE10 shell relies on do not exist
on this board:

| DE10 | DE25 |
| --- | --- |
| BASIC, KERNAL, character and 1541 ROMs linked into 36 placed blank M10K lanes at install (format 3) | `../shared-rtl/fes_c64_firmware_rom.v`: block RAM filled at launch through [`fes.fabric.firmware-upload`](../../../../mister-packages/docs/firmware-upload.md), one 36,864-byte image in `c64_rom`'s order |
| 52.224 MHz system PLL on the fabric | `../machine/machine_pll.ip` on `CLOCK1_50`: 52.222222 MHz for the machine and 12.287582 MHz for audio (both -34 ppm, in the DE10 ratio) |

The firmware RAM keeps `c64_rom`'s read ports and latencies: two clocks for
the CPU, one for the VIC's character fetch and one for the 1541. The VIC has
its own copy of the character ROM, written with the CPU's, so every region is
one write port and one read port.

Everything else stays where the DE10 keeps it. The 64 KiB RAM, colour RAM,
three frame pages and the 174,848-byte D64 disk store are block RAM, so the
board SDRAM is unused: the component holds its clock low, both chips
deselected and the data pins released. Both cartridge sockets are vacant
(an all-zero response), as the DE10 shell's are without a linked card.

`../shared-rtl/fes_c64_core.v` is the portable core: firmware front end,
mailbox and machine, with the picture scanned out in the pixel clock as on
the DE10 and handed to the session display. `derive.py` makes the only source
changes, at build time and with exact anchors, and a changed anchor fails the
build:

- `c64_machine.sv`: the `c64_rom` instance becomes `fes_c64_firmware_rom`,
  and the machine gains the firmware write port.
- `c64_machine.sv` also declares its CPU read bus, `read_data`, before the
  VIC's port list uses it. The DE10 source declares it later; Quartus then
  makes an implicit one-bit net and leaves the 6510's read data undriven,
  which Yosys and Verilator do not.
- `c64_machine.sv`: the system reset is registered, with one preserved copy
  for each of the CPU, both CIAs, the 1541, its GCR reader, the VIC and the
  SID. As one combinational term it reached some six thousand registers,
  which signoff refuses for endpoint tension. Every copy changes on the same
  machine clock, one clock after the DE10's term. `fes_c64_core` registers
  the machine's reset input the same way.
- `c64_ram.v`, `c64_color_ram.v`: the block RAM hint names `M20K`.
- `c64_disk_store.v`: the DE10 store is one byte array with two write ports,
  one shared with the 1541's read, which Quartus cannot map to block RAM. It
  becomes two byte lanes (even and odd addresses), each with one write and
  one read port. A mailbox byte pair always lands in both lanes, and the
  1541's read keeps its one clock. The DE10 read returned the byte being
  written while a pair was written; here it always returns the 1541's
  address.
- `c64_frame_store.sv`: the 153 placed `MISTRAL_M10K` primitives become three
  inferred pages, one write statement each, with the DE10 latencies (written
  in the machine clock, read with one pixel edge). Its crossings take the
  shape the Spectrum and Atari ST pass signoff with. The reader's reset was
  the machine's combinational reset sampled directly in the pixel clock; here
  it is registered once in the machine clock and enters the pixel clock
  through two registers. The toggle synchronizers drop their synchronous
  reset and power up zero. The page number crosses as a held code loaded on
  the synchronized strobe, so no crossing register feeds another register
  directly. The first Quartus signoff flagged the DE10 shape as four
  insufficiently constrained 1-bit transfers (CDC-50002).
- The shared transport is sliced out of the ZX81 computer file.

The opt-in reSID filter chain (`RESID_FILTER`, off in the package) uses
Cyclone V ROM primitives and is not compiled.

The computer slave and its transport stay on the 100 MHz peripheral clock;
`fes_gp_crossing` carries the GP words to the machine clock. `c64-clocks.sdc`
names each crossing, including the frame store's publish and acknowledge
toggles, its held page number and its reset level. No clock group is cut.

## Run

```sh
make test   # derivation contracts; no simulator or Quartus
make sim    # the open diagnostic through the real computer slave
make project VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
```

`sim` assembles the open diagnostic firmware (without its cartridge stages,
since the sockets are vacant) and the synthetic D64, then drives the
component's Avalon computer slave on three unrelated clocks. It checks
identity and capabilities (`0x204f`), that execution cannot be released
without firmware, a wrong size and a wrong CRC, every stored firmware byte in
each region, every byte of the D64 committed through the mailbox (a 7-byte
first chunk puts later pairs at odd addresses) into the disk store's lanes, joystick and keyboard input, the diagnostic running to its PASS
stage (CPU, CIA timers and interrupts, SID audio and the native 1541 reading
the committed disk), that the SDRAM stays idle and that a hold keeps the
firmware. It then runs the DE10 frame-store bench against the derived store:
page handoff at three producer rates, reader coherence and every stored
address. The HPS, scanout memory master, PLLs and HDMI are not modeled.

`project` permits inspection of local edits. `generate` and `build` require
clean committed source and an output directory that does not exist.

## Open

- Hardware acceptance on Kit D25, and the DE25 tier 1 bar in DeanoC/fes#2042:
  the game corpus against a reference emulator, and conformance on the kit.
- The audio-clock review uses `de25-machine-pll-audio-clocks-v1`, which is
  unreviewed (shared with the Spectrum and Atari ST).
- Linkable cartridge cards and the expansion socket.
- The C64 NTSC model, which the DE10 does not build either.

## Native Tenfoot

The opt-in `nativepal` package uses the same BL31 upload-shell role as native
Spectrum. Its 6510 opcode-decode counter and address use the shared
computer-aperture snapshot at `0x18` and `0x1c`. The C64 scanout reader uses
the existing fixed DDR grant and its identity and drain protocol. Guest RAM
and the 1541 disk store stay in block RAM.

Run the native component regression with `python3 sim.py --native` from this
directory. It checks two diagnostic execution cycles, ROM and D64 retention
across Hold, instruction activity while running, and the shared firmware
scanout checks with both returned and withheld read responses.

Use the `nativepal` model of `build_c64_package.py` for the normal repeated
Quartus signoff and seal. The shared-shell TamaGo SDK accepts
`--c64-build`, `--c64-package` and `--c64-rom`, with an optional
`--c64-d64`. The C64 export under `c64/export/FES` contains the SD catalogue and
selected inputs. TamaGo retains the standard 174,848-byte D64 before
closing SD access, uploads the 36 KiB firmware and disk while execution is
held, and checks the disk's commit acknowledgement before releasing the CPU.
The native path mounts one read-only 35-track image at launch.

The FAT-backed lifecycle tests exercise launch, return and relaunch with the
actual shared-shell C descriptor decoder. A lost D64 commit acknowledgement
keeps execution held and retires the session. The 5-second opening deadline
and 1-second control budget are unchanged. Physical startup performance
needs measurement on a matched, sealed boot chain.
