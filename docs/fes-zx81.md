# FES ZX81

The first slice is a ROM-less `fes.simple-computer` 1.0 package (`fes.zx81`
1.0.0) with 16 KB RAM, original ROM, a 40-key matrix, one `.p` mailbox blob
and fixed 720p60 HDMI. There is no ZX80, colour, YM2149, turbo, joystick or
SDRAM in this slice.

FES does not install this package in the native image catalog. The host
library path is `core-install` / `core-entry` / `POST /api/v1/session/launch`
with the returned `game_id`, as for other ROM-less FPGA cores. See
[described FPGA core packages](core-packages.md) and the selected FogCast
[core package library](../sources/FogCast/docs/core-package-library.md).

## Contracts

| Item | Value |
| --- | --- |
| Core ID | `fes.zx81` |
| ABI | `fes.simple-computer` 1.0 |
| Profile | `fes-gp-v1` |
| Interfaces | `fes.keyboard`, `fes.media.blob`, `fes.video.fixed-720p60` (all required) |
| Persistence | none (library launches are volatile) |
| Input | 40-bit active-low matrix via runtime `set_keyboard`; no `fes.gamepad` |
| Stop | existing package Select+Start |
| Tape | runtime `load_media` of a `.p`; empty `LOAD ""` reports `0/0` |

`core-load` is the development loader and does not create a library entry.
The target agent must post `set_keyboard`; an agent without that path only
reaches uinput.

## Producers

Quartus Prime Lite 17.0.2 (`make build-fes-zx81-quartus`) is the kit-proven
bring-up lane. `make build-fes-zx81` is the Yosys/nextpnr-mistral recipe for
the same package identity. OSS uses TV80, a checked 50 MHz system PLL and
registered M10K; it does not inherit Quartus acceptance.

## Menu / sofa UI

Library install and `session/launch` of `fes.zx81` are host APIs. Showing
that `game_id` in the sofa catalog grid is FogCast UI work, not this core
slice.

## Validation

Component tests and Verilator live in the misteross worktree. Hardware
diagnostics on the designated kit used a sealed OSS package and a derived
keyboard-agent rootfs. Those are not exact-artifact acceptance of an
assembled FES image.
