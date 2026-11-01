# FES ZX81

The standard core is a `fes.computer` 1.0 package (`fes.zx81`
1.7.0) with 1 KiB internal RAM, original ROM, HID keyboard rows mapped to the
40-key matrix, one `.p` tape unit with BASIC SAVE, fixed 720p60 HDMI, the session-display
plane for kit launcher frames, and a registered Z80-like expansion bus. The
same shell also builds experimental package-only models `fes.zx80` (PAL, 4 KiB
ROM, 1 KiB internal RAM, no NMI), `fes.ts1000` (NTSC, 8 KiB ROM, 2 KiB internal
RAM) and `fes.ts1500` (NTSC, 8 KiB ROM, 16 KiB internal RAM). They are not in
the factory image. `fes.zx81` stays the PAL ZX81 with an 8 KiB `machine-rom`
and 1 KiB internal RAM. This slice still has no colour, YM2149, turbo, joystick
or SDRAM. The standard OSS package carries the vacant bus; carts are
independent bus consumers.

The native image selects this package. The
[session-display validation](validation/2026-10-02-zx81-session-display.md)
accepts an older simple-computer shell. It does not accept these computer-ABI
bytes. Physical operator input and expansion/audio acceptance remain separate.
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
| ABI | `fes.computer` 1.0 |
| Profile | `fes-gp-v1` |
| Interfaces | `fes.keyboard.hid`, `fes.gamepad.ports`, `fes.media.zx81-tape`, `fes.media.zx81-tape-write`, `fes.video.fixed-720p60`, `fes.audio.pcm-s16-stereo-48k`, `fes.memory.hps-ddr`, `fes.video.session-display` (required); `fes.expansion.zx81-bus` (optional) |
| Persistence | none (library launches are volatile) |
| Input | HID keyboard rows mapped to the 40-key matrix |
| Stop | ordinary session Stop; Select+Start held for one second on the kit |
| Live controls | Home or Select opens the launcher plane; Escape/B or Return to play closes it and restores ZX81 video |
| Tape | launch insert into unit 0, or mid-session tape insert/eject; empty `LOAD ""` reports `0/0`. BASIC SAVE on the ZX81 ROM stores a `.p` through `fes.media.zx81-tape-write` |

Live tape arm/eject uses `fes.media.zx81-tape` unit 0 without a
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
1.7.0 socketed format-3 package. The package seals `rom-map.json` alongside
the blank ROM RBF. The host sends the selected binary and optional expansion;
the target Go linker composes the expansion and patches ROM INIT before loading.
Python and Mistral remain producer/oracle tools, not kit dependencies. OSS uses the first-party NMOS Z80 (`fes_z80_nmos`), a 52.224 MHz system clock, registered M10K and
the scoped `toolchain.lock`, including the bounded HPS DDR atom
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

Kit software and core packages update independently. A 1.6.0 package declares
the shared plane, `fes.fabric.video.display-plane` 1.0, as a required interface
beside `fes.video.session-display` 1.0, and its plane reports pixel format 2. A
1.5.0 package's opaque plane declares only the session display and reports
format 1. Software with the shared plane refuses a session display without the
plane, and older software does not know the plane, so each refuses the other
version at compatibility, before programming. After programming, software with
the plane also checks that the bitstream reports format 2. Installing software
leaves library selections unchanged, so switch a ZX81 entry between installed
versions with `core-select` when changing software. There is no compatibility
path for the earlier plane.

A 1.6.0 kit check therefore needs software with the shared plane selected, not
just an image that contains it. A new image runs its built-in software only
during its trial boot; after confirmation the supervisor restores the selected
bundle ([kit software releases](kit-software-releases.md#activation-and-recovery)).
Install and activate the `make kit-software` bundle, then restore the baseline
bundle afterwards.

## Open ROM

`make zx81-openrom` (in `sources/misteross`) assembles a clean-room,
GPL-2.0-or-later ZX81 ROM from `cores/fes-zx81/openrom` into
`build/zx81-openrom/zx81-openrom.bin`, so the machine runs without Sinclair's
firmware. Select that 8192-byte file as the `machine-rom` like any other
firmware; the package and ROM map are unchanged. The assembler
(`scripts/z80asm.py`) and the character set (`font.txt`, an original design)
are part of the source.

The ROM keeps the published ZX81 interface that programs and `.p` files depend
on: system variables, restarts, the display interrupt and NMI routines, the
keyboard tables, the core's tape trap addresses and the commonly called entry
points (KEYBOARD `$02BB`, DECODE `$07BD`, PRINT-AT `$08F5`, CLS `$0A2A`, FAST
`$0F23`, SLOW `$0F2B`, BREAK-1 `$0F46`). The rest of the layout is its own.
Display timing matches the original on the core (PAL 310 lines, NTSC 262), with
the collapsed display file below 3.25 KiB of RAM. Tape `LOAD`/`SAVE` use the
core's `.p` traps, and a loaded program keeps its FAST/SLOW state and autorun
line.

This is stage 1: the editor, the BASIC statements and arithmetic needed by
ordinary programs, strings and slicing, numeric arrays, `FOR`/`GOSUB`, `RND`,
`INKEY$`, `PEEK`/`POKE`/`USR`, `PLOT`/`UNPLOT`, `SCROLL` and `PAUSE`. Known gaps:

- `SIN`, `COS`, `TAN`, `ASN`, `ACS`, `ATN`, `LN`, `EXP`, `SQR`, non-integer
  `**` and machine code calling the `RST 28` calculator stop with report A.
- No string arrays; a `FOR` loop that should run zero times runs once.
- No up/down listing cursor keys (use `LIST n` and EDIT); no E-notation input
  (output is supported); `POKE` takes 0 to 255 only.
- No syntax check on entry: errors are reported when the line runs.
- No printer: `LPRINT`, `LLIST` and `COPY` do nothing.
- `LOAD`/`SAVE` ignore the name and work only through the core's tape unit.
- `fes.zx80`'s 4 KiB ROM has no open substitute.

`make sim-fes-zx81-openrom` (part of `make sim-fes-zx81`) boots the ROM on the
machine RTL with 1 KiB and 16 KiB RAM, checks the display geometry against the
original, enters and runs a program, saves it through the tape trap, loads it
back with autorun, and scrolls the screen. `tests/test_zx81_openrom.py` checks
the assembler and the image's fixed interface bytes. The ROM has no kit
acceptance yet.

## Validation

Component tests and Verilator live in `sources/misteross` in this FES repository.
The earlier 1.5.0 opaque plane has a
[frozen image/HDMI record](validation/2026-10-02-zx81-session-display.md),
including manual BASIC load/list
and preserved program pixels during live swap/eject. Input events were injected
through keyboard/controller evdev paths; physical button acceptance is pending.
The 1.6.0 shared display plane has simulation and seal evidence only; its kit
check is pending.

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
