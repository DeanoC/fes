# DE25 standard Atari ST

This lane brings the FES Atari 520ST to the DE25-Nano behind the same shell
as the [standard ZX Spectrum](../spectrum/README.md): the managed board
recipe's atomic computer transport and allocated display, launch-time firmware
upload, the board SDRAM and a 52.222222 MHz machine clock. It has a simulated
component and a Quartus project on the shared builder. There is no recipe or
runtime admission yet, and nothing here programs a kit.

## What differs from the DE10 core

The machine sources under `cores/fes-atari-st/rtl` are compiled as they are,
including `st_memory` with its phase slots and posted CPU writes.
`../shared-rtl/fes_atari_st_core.v` replaces the DE10 shell:

| DE10 | DE25 |
| --- | --- |
| 192 KiB ROM linked into blank M10K lanes at install (format 3) | `fes_st_firmware_rom`: block RAM with `st_rom`'s request port and latency, filled through [`fes.fabric.firmware-upload`](../../../../mister-packages/docs/firmware-upload.md) |
| MiSTer addon SDRAM, masks shared with A11/A12, Cyclone V DDR clock cell | Board SDRAM rank 0 with its own mask pins; `derive.py` swaps the forwarded-clock cell for the inverted clock |
| 720p word straight to HDMI through the video socket | The direct video part's word into the session display, so the launcher plane works in a session |
| `fes_computer_mailbox_direct` | The mailbox with the session display, as on the Spectrum and ZX81 |

The expansion and video sockets are vacant. RAM (512 KiB) and the floppy image
keep their DE10 halfword offsets in the first SDRAM rank; the second chip is
never selected. The wrapper is the Spectrum's: the computer slave stays on the
100 MHz peripheral clock and `fes_gp_crossing` carries the GP words to the
machine clock.

## Run

```sh
make test   # derivation contracts; no simulator or Quartus
make sim    # the component through its real computer slave
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
```

`sim` assembles the open diagnostic firmware and drives the Avalon computer
slave on three unrelated clocks against a logical SDRAM rank that checks the
controller's command protocol, recovery times and refresh interval. It checks
identity and capabilities, that execution cannot be released without
firmware, the upload, that the 68000 runs the uploaded ROM with its RAM in
SDRAM until the vacant expansion fails the diagnostic's probe at stage 5 (the
DE10 machine bench's result), a 360 KiB floppy image committed while the
machine runs and stored big-endian in the media region without disturbing
RAM, a mouse report, and that a hold keeps the firmware. The HPS, scanout
memory master, PLLs, SDRAM pad timing and HDMI are not modeled.

## Open

- Naming each machine/pixel crossing of the video adapter and native video
  in `atari-st-clocks.sdc`. They carry one provisional ten-nanosecond bound
  for the whole clock pair today; that must go before any export.
- Fit. The DE10 build uses 513 M10K; the uploaded ROM alone is 1.6 Mbit of
  the device's M20K.
- SDRAM on the board. The chips are mobile SDRAM: the ramtest controller
  programs their extended mode register and this controller does not. Pad
  timing at the machine clock, with a fabric rising-edge sample, is unproven.
- A stock TOS or EmuTOS boot in simulation and on hardware, mono and medium
  resolution through the session display, and the picture rectangle the
  plane reports (the whole raster for now).
- Runtime and FogCast admission, as for the Spectrum.
