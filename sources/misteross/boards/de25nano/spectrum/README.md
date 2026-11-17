# DE25 standard ZX Spectrum

This lane brings the FES ZX Spectrum to the DE25-Nano on the managed
MENU/ZX81 board recipe: the same GHRD, HDMI shell, kernel-owned allocated
display and atomic computer transport at peripheral offset `0x10300`. It
builds the 48K and the 128K/+2. It is not a package yet: no recipe selects it,
the runtime does not admit it, and nothing here programs a kit.

## What differs from the DE10 core

The machine sources under `cores/fes-spectrum/rtl` are compiled as they are.
Three things the DE10 shell relies on do not exist on this board, and each has
a DE25 replacement in `../shared-rtl/`:

| DE10 | DE25 |
| --- | --- |
| ROM bytes linked into blank M10K lanes at install (format 3) | `fes_spectrum_firmware_rom`: block RAM filled at launch through [`fes.fabric.firmware-upload`](../../../../mister-packages/docs/firmware-upload.md) |
| Tape image in the reserved HPS DDR window (`fes_media_ddr`) | `fes_media_sdram`: the same client ports over the board SDRAM |
| 52.224 MHz system PLL on the fabric | `../machine/machine_pll.ip` on `CLOCK1_50`: 52.222222 MHz for the machine and 12.287582 MHz for audio (both -34 ppm, in the DE10 ratio), in place of the stock audio PLL |

`fes_spectrum_core` is the portable core: firmware front end, mailbox,
keyboard, tape store, machine, 720p capture and the audio mix. Its edge
sockets are vacant. `derive.py` makes the only source changes, at build time
and with exact anchors: the machine's ROM instance gains the firmware write
port, three inferred memories take the `M20K` hint, and the shared transport
is sliced out of the ZX81 computer file. A changed anchor fails the build.

`fes_firmware_upload` sits between the transport and the mailbox. It answers
the four firmware opcodes, refuses an Execution release until a complete image
with a matching CRC-32 is committed, and passes everything else to the mailbox
unchanged. The mailbox and its capability word are the DE10's; the display has
no HPS DDR window bit, as on the ZX81. No Sinclair or Amstrad ROM bytes are in
this repository or in a build.

The computer slave and its transport stay on the 100 MHz peripheral clock, so
the Qsys topology and its qualified reset and crossing constraints are the
ZX81's. The machine clock enters the component as a bare clock.
`fes_gp_crossing` carries the GP request and reply words between the two
clocks with the mailbox's own toggle handshake; transport health and the reset
release cross as single levels. `spectrum-clocks.sdc` names each crossing; no
clock group is cut.

The tape is the first 16 MiB of the SDRAM, through the ramtest lane's
`fes_de25_sdram` controller on the machine clock, one halfword a command. The
+2A/+3 and the Pentagon need a second SDRAM client for the disk unit and are
refused at elaboration.

## Run

```sh
make test   # derivation contracts; no simulator or Quartus
make sim    # 48K and 128K through the real computer slave
make project VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
```

`sim` assembles the open diagnostic firmware and drives the component's
Avalon computer slave on three unrelated clocks, against a logical SDRAM
model. It checks identity and capabilities, that execution cannot be released
without firmware, a wrong size, a wrong CRC and out-of-order data, the stored
image, a PZX tape committed in two chunks (one byte pair straddling a
halfword) and read back from SDRAM by the firmware's loader, HID and
controller input, and that a hold keeps the firmware. The HPS, scanout memory
master, PLLs, SDRAM pad timing and HDMI are not modeled.

`build.py` is this machine's spec for the shared builder, `../machine/board.py`,
which the Atari ST uses too. `project` permits inspection of local edits. `generate` and `build` require
clean committed source and an output directory that does not exist. A build is
a diagnostic candidate with the HPS-first Linux artifacts beside it; it has no
timing review, no reviewed input/output reference and no package identity.

## Open

- The [first compile](../../../docs/validation/2026-11-17-de25-spectrum-compile/README.md)
  fits with clean timing. Owed: external delays for the SDRAM and I2S pins,
  then the ZX81 producer's export gates (fitted input/output review,
  repeat-RBF identity).
- SDRAM pad timing at the machine clock. The controller was qualified on the
  board at 50 MHz; the half-period sample is 4% shorter here.
- Runtime and FogCast: admit `fes.spectrum` on `de25-linux-computer-v1`, and
  deliver the user's ROM at launch.
- Disk models, snapshots through the tape unit on hardware, and the edge
  sockets.
