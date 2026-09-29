# FES ZX Spectrum

This directory is the described core `fes.spectrum`: a ZX Spectrum 48K on the
`fes.computer` 1.0 mailbox
([home-computer I/O](../../../mister-packages/docs/computer-io.md)). It follows
the Apple II pathfinder: late-bound firmware, removable media while the
machine runs, and independently linked expansion cards. The cross-component
contract is the
[ZX Spectrum pathfinder design](../../../../docs/superpowers/specs/2026-09-28-spectrum-pathfinder-design.md).

## Machine

- TV80 (the shared `T80pa` wrapper) at a 3.5 MHz average: 875/13056 of the
  52.224 MHz system clock. There is no ULA contention and no floating bus.
- 48 KiB RAM at `$4000–$FFFF`. The 16 KiB ROM window `$0000–$3FFF` is sixteen
  blank 1024×10 M10K lanes (column 5, rows 32–47). FogCast links a selected
  16,384-byte `spectrum-firmware` image at download time. No Sinclair ROM is
  in this repository or the package.
- Port `$FE` (A0 low, the original incomplete decode): border, MIC/beeper,
  keyboard half-rows and EAR. Port `$1F` is a built-in Kempston joystick fed
  by controller port 0. Other unclaimed I/O reads `$FF`.
- Maskable interrupt is low for 32 T-states every 69,888 T-states. That frame
  is not the 60 Hz HDMI raster.
- USB HID key state is mapped by the core. Rows must be quiet for about 1 ms
  before they take effect. Shift is CAPS SHIFT, Control is SYMBOL SHIFT.

## Video and audio

`spectrum_video.v` scans the bitmap and attributes from RAM port B in the
74.25 MHz HDMI domain. The 256×192 picture is scaled 4× by 3× to 1024×576 and
centred in 1280×720p60; the rest of the active raster is the border. The
beeper is mixed with the saturated sum of every socket's PCM into the shared
48 kHz I2S path. A card can play without asserting DRIVE.

## Edge sockets

Sockets 1–4 share one request word (`rtl/spectrum_bus.vh`). Each socket pins
32 request and 28 response flip-flops in the same column-24 bands the Apple II
shell uses, so a frozen shell routes both horizontal clock segments into every
socket row. The Go linker layout is `fes.spectrum-bus.sockets/1`.

`expansions/probe.v` is the open probe card (module `cart`). Socket N owns
ports `$E0+(N-1)*4`: id `$F5`, a scratch register, an access counter and the
socket index. The machine simulation links it into sockets 1 and 3.

## Cassette

Media unit 0 (`fes.media.spectrum-tape`) holds 1..65,536 bytes of `.tap`.
While the unit is ready the player emits EAR edges: 2168 T pilot pulses
(8063 for a flag of 0, otherwise 3223), sync 667/735, bit pulses 855 or 1710,
then a one-second pause. A trailing partial block is not played. MIC writes
are not returned to the host.

## Simulation

```sh
make sim-fes-spectrum
make sim-fes-spectrum-machine
make sim-fes-spectrum-board
```

The machine simulation boots the open diagnostic, checks the keyboard matrix,
Kempston port, probe id and scratch register, the 2168 T pilot, a red border
and a black ink pixel. The board simulation drives `top.v` through the
mailbox: identity capability bit 5, tape limits 1..65536, a 3-byte commit and
HID usage `A` landing on the Spectrum A key. These are host simulations, not
an RBF, timing or kit result.

## Not implemented

ULA contention, a floating bus, 128K paging, AY sound, Interface 1 / DivMMC,
and tape write-back. `make build-fes-spectrum` sealed the shell recorded in
`docs/validation/2026-09-28-spectrum-pathfinder-seal.md`. A kit ROM link of
the 48K BASIC ROM is recorded in
`docs/validation/2026-09-28-spectrum-basic-kit.md`. Probe cards, keyboard
checks, and tape checks are still open.
