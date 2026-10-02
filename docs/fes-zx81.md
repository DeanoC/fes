# FES ZX81

The standard core is a `fes.simple-computer` 1.0 package (`fes.zx81`
1.5.0) with 1 KiB internal RAM, original ROM, a 40-key matrix, one `.p` mailbox blob,
fixed 720p60 HDMI, runtime-owned in-session launcher controls and a registered
Z80-like expansion bus. There is no ZX80,
colour, YM2149, turbo, joystick or SDRAM in this slice. The standard OSS
package carries the vacant bus; carts are independent bus consumers.

The native image selects this package. The
[session-display validation](validation/2026-10-02-zx81-session-display.md)
records the exact sealed shell,
verified image and designated-kit HDMI diagnostics. Physical operator input
and expansion/audio acceptance remain separate from that display smoke.
The host library path is `core-install` / `core-entry` /
`POST /api/v1/session/launch` with the returned `game_id`, as for other
described FPGA cores. Select the 8192-byte `machine-rom` binary explicitly;
the target links it through the package's sealed ROM map at download time. See
[described FPGA core packages](core-packages.md) and the selected FogCast
[core package library](../sources/FogCast/docs/core-package-library.md).

## Contracts

| Item | Value |
| --- | --- |
| Core ID | `fes.zx81` |
| ABI | `fes.simple-computer` 1.0 |
| Profile | `fes-gp-v1` |
| Interfaces | `fes.keyboard`, `fes.media.blob`, `fes.video.fixed-720p60`, `fes.audio.pcm-s16-stereo-48k`, `fes.memory.hps-ddr`, `fes.video.session-display` (required); `fes.expansion.zx81-bus` (optional) |
| Persistence | none (library launches are volatile) |
| Input | 40-bit active-low matrix via runtime `set_keyboard`; no `fes.gamepad` |
| Stop | ordinary session Stop; Select+Start held for one second on the kit |
| Live controls | Home or Select opens the launcher plane; Escape/B or Return to play closes it |
| Tape | launch `load_media` (hold-reset primary bind) or mid-session `replace_live_media` / `clear_media`; empty `LOAD ""` reports `0/0` |

Live tape arm/eject uses the existing `fes.media.blob` mailbox without a
hold-reset reboot. The captured host/agent session operations are
`POST /api/v1/session/live-media` and `…/clear`; CLI uses `change-tape` /
`eject-tape`. The visible kit picker offers imported `.p` files and attributed
starter tapes while the computer continues running. Arming does not type
`LOAD ""`, change next-start library selections or reload the machine. See
[ZX81 tape media](zx81-tape-media.md) and [hardware rooms](hardware-rooms.md).

`core-load` is the development loader and does not create a library entry.
The target agent must post `set_keyboard`; an agent without that path only
reaches uinput.

## Producers

Quartus Prime Lite 17.0.2 (`make build-fes-zx81-quartus`) remains the legacy
1.2 bring-up/oracle lane; it does not produce the standard socketed package.
`make build-fes-zx81` is the standard Yosys/nextpnr-mistral producer for the
1.5 socketed format-3 package. The package seals `rom-map.json` alongside
the blank ROM RBF. The host sends the selected binary and optional expansion;
the target Go linker composes the expansion and patches ROM INIT before loading.
Python and Mistral remain producer/oracle tools, not kit dependencies. OSS uses TV80, a 52.224 MHz system clock, registered M10K and
the scoped `toolchains/zx81-expansion.lock`, including the bounded HPS DDR atom
declaration. The producer closes over the shared display/DDR sources and checks
all three clocks, ROM patching and the reserved socket. It does not inherit Quartus
acceptance. Its combined 52.224/12.288 MHz system/audio PLL and shared PCM/I2S
output mute on Hold or lost audio lock. The repaired 1.3.0 package passed
vacant-socket silence on kit 1. Earlier packages emitted nonzero HDMI samples;
the [failed diagnostic](validation/2026-09-29-zx81-shared-audio-hil.md) is
historical and was superseded by the
[passing diagnostic](validation/2026-09-30-zx81-shared-audio-hil.md).
Zon X implements three-channel AY8912 sound on the bus 2.0 clock/reset edge;
see [expansion details and fidelity limits](zx81-expansion-bus.md). The
[historical channel-A diagnostic](validation/2026-09-30-zx81-zonx-hil.md) applies
only to the earlier artifacts. The [1.4.0 build record](validation/2026-10-01-zx81-zonx-ay.md)
passes all three clocks and unchanged socket containment. The
[full-cart Kit 2 diagnostic](validation/2026-10-01-zx81-zonx-kit2-hil.md) passes
identity, audio phases, Hold, Stop and relaunch with capture limits.
The transport schedules exact average 6.5 MHz ULA / 3.25 MHz CPU rates, replacing
the former 3.264 MHz CPU. The cart halves the edge clock to 1.625 MHz AY.
Enable jitter stays below one transport cycle; HDMI/audio clocks are unchanged.

## Menu / sofa UI

Library install and `session/launch` use the normal FogCast host APIs. The kit
launcher enables live workbench/tape controls only when the active package
advertises the supported session-display and HPS DDR interfaces. Its immutable
full frames reuse the idle MENU transport under a separate display generation;
opening and closing controls never programs MENU. The first complete frame
selects opaque launcher pixels on the shared 720p raster. Returning drains
physical scanout before machine input resumes. Held keys require release and a
fresh press. Failed or ambiguous close retains UI focus for retry.

Older packages retain prelaunch cassette selection, and the separate host
display retains its live controls. The
[shared contract](../sources/mister-packages/docs/session-display.md) and
[runtime presentation](../sources/libmister-runtime/docs/menu-display.md)
describe ownership and failure
behavior.

## Validation

Component tests and Verilator live in `sources/misteross` in this FES repository.
The current 1.5.0 display plane has a
[frozen image/HDMI record](validation/2026-10-02-zx81-session-display.md),
including manual BASIC load/list
and preserved program pixels during live swap/eject. Input events were injected
through keyboard/controller evdev paths; physical button acceptance is pending.

Earlier hardware diagnostics on the designated kit used a sealed OSS package and a derived
keyboard-agent rootfs. Those are not exact-artifact acceptance of an
assembled FES image. The [1.3.0 shared-audio diagnostic](validation/2026-09-30-zx81-shared-audio-hil.md)
passed vacant-socket silence, GP/package identity, ROM linking and Stop with
the repaired compiler. Its matching RAM cart passed timing and changed zero
CRAM bits outside the reserved socket. The bounded Zon X tone cart has its own
[kit diagnostic](validation/2026-09-30-zx81-zonx-hil.md). The three-channel AY implementation passes independent behavior and CPU-firmware
simulation plus frozen-shell routing. The full-cart [Kit 2 diagnostic](validation/2026-10-01-zx81-zonx-kit2-hil.md)
passes with temporary services and filtered audio capture; factory-image
acceptance remains a separate gate.
