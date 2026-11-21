# DE25 standard ZX Spectrum

This lane brings the FES ZX Spectrum to the DE25-Nano on the managed
MENU/ZX81 board recipe: the same GHRD, HDMI shell, kernel-owned allocated
display and atomic computer transport at peripheral offset `0x10300`. It
builds the 48K, the 128K/+2, the +2A/+3 and the Pentagon 128. Its producer is
`../producers/build_spectrum_package.py`, selected by the `de25-nano`
`fes.spectrum` recipe; the producer's signoff gate does not admit a package
yet (see Open), and nothing here programs a kit.

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
`fes_de25_sdram` controller on the machine clock, one halfword a command. On
the +2A/+3 and the Pentagon the disk (media unit 1) is the next 16 MiB. One
`fes_media_sdram` holds both: the unit number is its top address bit, the deck
streams the tape from byte 0, and the disk controller reads track bytes
through the fetch port. Fetches go before the stream, so the two never issue
SDRAM commands at once. The disk is read-only, as on the DE10.

## Run

```sh
make test   # derivation contracts; no simulator or Quartus
make sim    # all four models through the real computer slave
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
controller input, and that a hold keeps the firmware. On the +2A/+3 and the
Pentagon it also commits an 8 KiB disk to media unit 1, checks it lands 16 MiB
up without touching the tape, and waits for the drive to read the disk's
header back through the fetch port. The HPS, scanout memory
master, PLLs, SDRAM pad timing and HDMI are not modeled.

`build.py` is this machine's spec for the shared builder, `../machine/board.py`,
which the Atari ST uses too. `project` permits inspection of local edits. `generate` and `build` require
clean committed source and an output directory that does not exist. A build is
a diagnostic candidate with the HPS-first Linux artifacts beside it; it has no
timing review, no reviewed input/output reference and no package identity.

## Open

- Both models [pass signoff and package](../../../docs/validation/2026-11-18-de25-machine-signoff/README.md).
  Each package is a
  candidate with no hardware acceptance, and the audio-clock review uses the
  new `de25-machine-pll-audio-clocks-v1` policy, which is unreviewed.
- The frame-bank load at frame end assumes a machine frame (about 20 ms) is
  longer than a pixel frame (16.7 ms).
- SDRAM pad timing at the machine clock. The controller was qualified on the
  board at 50 MHz; the half-period sample is 4% shorter here.
- The disk models on hardware, snapshots through the tape unit on hardware,
  and the edge sockets. The DE10 packages also declare the optional snapshot
  interfaces; the DE25 ones declare only the tape and, on the disk models, the
  disk.
