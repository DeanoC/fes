# DE25 standard Atari ST

This lane brings the FES Atari 520ST to the DE25-Nano behind the same shell
as the [standard ZX Spectrum](../spectrum/README.md): the managed board
recipe's atomic computer transport and allocated display, launch-time firmware
upload, the board SDRAM and a 52.222222 MHz machine clock. It has a simulated
component and a Quartus project on the shared builder. Its producer is
`../producers/build_atari_st_package.py`, selected by the `de25-nano`
`fes.atari-st` recipe. The 520ST signs off and packages, the runtime and
FogCast admit it, and Kit D25 has booted it to the EmuTOS desktop (#1937). The
work toward tier 1 on DE25 is tracked in #2042.

## What differs from the DE10 core

The machine sources under `cores/fes-atari-st/rtl` are compiled as they are,
including `st_memory` with its phase slots and posted CPU writes.
`../shared-rtl/fes_atari_st_core.v` replaces the DE10 shell:

| DE10 | DE25 |
| --- | --- |
| 192 KiB ROM linked into blank M10K lanes at install (format 3) | `fes_st_rom_sdram`: the ROM in SDRAM at halfword `c0000`, filled through [`fes.fabric.firmware-upload`](../../../../mister-packages/docs/firmware-upload.md) and read through the CPU's memory port |
| MiSTer addon SDRAM, masks shared with A11/A12, Cyclone V DDR clock cell | Board SDRAM rank 0 with its own mask pins; `derive.py` swaps the forwarded-clock cell for the inverted clock |
| Native video frame store: 234 hand-chunked 1024x9 memories written from one `case` | `derive.py` makes each bank one memory for Quartus to shape; as written Quartus builds 2.2 million registers, and one M20K per chunk does not fit |
| 720p word straight to HDMI through the video socket | The direct video part's word into the session display, so the launcher plane works in a session |
| One machine reset register, and the 68000's RESET output, each reaching the whole machine | A reset register each for the bus, the processor, the I/O decode, the keyboard and serial chips, the sound chip, and the memory and video raster, all loaded from the same logic on the same clock; `derive.py` adds the RESET register's copies to `fx68k` and the group reset ports to `st_system`, `st_machine` and `st_io` |
| `fes_computer_mailbox_direct` | The mailbox with the session display, as on the Spectrum and ZX81 |

The device has 358 M20K blocks. When this lane was built the GHRD's on-chip
memory took 128 of them, so the ROM could not sit in block RAM beside the frame
store. The v4 shell removed that memory (#1956) and the 520ST now fits in 172
blocks, which leaves room for a 192 KiB block-RAM ROM again; the ROM is still
in SDRAM. `derive.py` gives
`st_memory`'s CPU port a ROM region; a ROM read takes the CPU's phase slot as
a RAM read does. Firmware is written there while the machine is held, when the
memory's phase slots turn on a free count because the raster stands still.

The expansion and video sockets are vacant. RAM (512 KiB) and the floppy image
keep their DE10 halfword offsets in the first SDRAM rank; the second chip is
never selected. The wrapper is the Spectrum's: the computer slave stays on the
100 MHz peripheral clock and `fes_gp_crossing` carries the GP words to the
machine clock.

`atari-st-clocks.sdc` names every path between the machine and pixel clocks:
the video adapter's and native capture's request/done toggles as single-bit
first stages, and each held bundle with a bound taken from its handshake.
The inventory came from the fitted design, so a new crossing shows up as a
timing failure, not as an unnamed exception.

## Run

```sh
make test   # derivation contracts; no simulator or Quartus
make sim    # the component through its real computer slave
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/new/private/path
```

`sim` first runs the DE10 native video bench on the derived frame store. It
then assembles the open diagnostic firmware and drives the Avalon computer
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

- The 520ST [passes signoff and packages](../../../docs/validation/2026-11-18-de25-machine-signoff/README.md).
  The package is a candidate with no hardware acceptance, and its
  audio-clock review uses the new `de25-machine-pll-audio-clocks-v1` policy,
  which is unreviewed.
- ROM read timing against the DE10 machine. `st_rom` answered in five clocks;
  the SDRAM port answers in the CPU's slot. The diagnostic firmware runs, but
  no cycle-exact comparison or stock TOS boot has been made.
- SDRAM on the board. The chips are mobile SDRAM: the ramtest controller
  programs their extended mode register and this controller does not. Pad
  timing at the machine clock meets the datasheet delays in analysis, with a
  falling-edge sample, and is unproven on the board.
- A stock TOS boot. EmuTOS reaches its desktop on Kit D25; no user-supplied
  TOS has been booted on DE25, in simulation or on hardware.
- Mono and medium resolution through the session display, and the picture
  rectangle the plane reports (the whole raster for now).
