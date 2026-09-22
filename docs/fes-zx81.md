# FES ZX81

The first slice is a ROM-less `fes.simple-computer` 1.0 package (`fes.zx81`
1.2.0) with 1 KiB internal RAM, original ROM, a 40-key matrix, one `.p` mailbox blob,
fixed 720p60 HDMI and a registered Z80-like expansion bus. There is no ZX80,
colour, YM2149, turbo, joystick or SDRAM in this slice. The standard OSS
package carries the vacant bus; carts are independent bus consumers.

FES installs this package as part of the ordered native package-only image set.
The host library path is `core-install` / `core-entry` /
`POST /api/v1/session/launch` with the returned `game_id`, as for other
ROM-less FPGA cores. See
[described FPGA core packages](core-packages.md) and the selected FogCast
[core package library](../sources/FogCast/docs/core-package-library.md).

## Contracts

| Item | Value |
| --- | --- |
| Core ID | `fes.zx81` |
| ABI | `fes.simple-computer` 1.0 |
| Profile | `fes-gp-v1` |
| Interfaces | `fes.keyboard`, `fes.media.blob`, `fes.video.fixed-720p60` (required); `fes.expansion.zx81-bus` (optional) |
| Persistence | none (library launches are volatile) |
| Input | 40-bit active-low matrix via runtime `set_keyboard`; no `fes.gamepad` |
| Stop | existing package Select+Start |
| Tape | launch `load_media` (hold-reset primary bind) or mid-session `replace_live_media` / `clear_media`; empty `LOAD ""` reports `0/0` |

Mid-session tape select/load while the core is already running is the
design lock in [ZX81 tape media](zx81-tape-media.md). Slice 1 delivers the
runtime mailbox path without hold-reset soft-reboot. Slice 2 adds the FogCast
host/agent session change-tape and eject API over core-media ids
(`POST /api/v1/session/live-media`, `…/clear`, CLI `change-tape` /
`eject-tape`). Sofa and kit HIL follow. That path is distinct from the
launch-time machine-ROM splice and from Stop→relaunch. This page describes the
mailbox contract that is true today.

`core-load` is the development loader and does not create a library entry.
The target agent must post `set_keyboard`; an agent without that path only
reaches uinput.

## Producers

Quartus Prime Lite 17.0.2 (`make build-fes-zx81-quartus`) remains the legacy
1.0 bring-up/oracle lane; it does not produce the standard socketed package.
`make build-fes-zx81` is the standard Yosys/nextpnr-mistral producer for the
1.1 socketed package. OSS uses TV80, a 52 MHz system PLL, registered M10K and
the scoped `toolchains/zx81-expansion.lock`; it does not inherit Quartus
acceptance.

## Menu / sofa UI

Library install and `session/launch` of `fes.zx81` are host APIs. Showing
that `game_id` in the sofa catalog grid is FogCast UI work, not this core
slice.

## Validation

Component tests and Verilator live in the misteross worktree. Hardware
diagnostics on the designated kit used a sealed OSS package and a derived
keyboard-agent rootfs. Those are not exact-artifact acceptance of an
assembled FES image.
