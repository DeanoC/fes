# FES ZX Spectrum pathfinder contract

**Status:** Machine and board simulation pass on this branch. No sealed package, host-library launch or kit evidence yet.
**Owners:** mister-packages for the `fes.media.spectrum-tape` interface on the existing `fes.computer` ABI; misteross for the machine, sockets, probe and producer; libmister-runtime for admission; FogCast for cassette projection and multi-socket selection; FES for the package-only recipe. The factory image is unchanged.

## Intended result

`fes.spectrum` 0.1.0 is a 48K ZX Spectrum brought up the way `fes.apple2` was:

- Z80 at a 3.5 MHz average, 48 KiB RAM, border, beeper, keyboard and EAR on port `$FE`, Kempston on `$1F`, fixed 720p60 HDMI.
- **Late-bound firmware:** one format-3 `spectrum-firmware` ROM, exactly 16,384 bytes, linked into `$0000–$3FFF` at download. No Sinclair ROM bytes are in git or in the package.
- **Removable media:** unit 0 accepts a 1..65,536-byte `.tap` image (`fes.media.spectrum-tape` 1.0) while the machine runs. Version 1.0 does not return MIC writes.
- **Expansion sockets:** sockets 1–4 share `fes.expansion.spectrum-bus` 1.0 (`fes.spectrum-bus.sockets/1`). Independently built cards are linked into their own CRAM rectangles. The open probe card is the first card.

The shell declares the computer operational interfaces required, including the tape unit, and the edge bus optional. It does not declare `fes.media.apple2-floppy`. A computer shell admits at most one unit-0 media interface.

## Evidence

`make sim-fes-spectrum` is the host simulation. `make build-fes-spectrum` sealed the shell recorded in [the 2026-09-28 seal note](../../validation/2026-09-28-spectrum-pathfinder-seal.md). A kit session still needs that package, a firmware digest, card archives, a composition id and a tape digest on a leased kit.
