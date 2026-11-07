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

The standard machine, computer mailbox, tape/SAVE and video capture live in
`sources/misteross/cores/fes-zx81/rtl/fes_zx81_core.v`. Board wrappers provide
clocks, HPS transport, the session-plane memory adapter, expansion placement and
HDMI/audio output. DE10's registered expansion boundary remains at its original
top-level hierarchy. The shared core accepts an explicit guest-clock fraction,
so it can run the same 6.5 MHz ULA timing from another board's system clock.

The installed DE25 package is still the BRAM bring-up wrapper around the shared
ZX81 machine. It has keyboard and tape loading, but lacks the standard shell's
SAVE, audio, expansion and in-play display support. Its native bring-up results
do not qualify the full standard core. DE25 migration uses the shared standard
core and board adapters; that replacement still needs a matched Quartus build,
package, runtime support and hardware acceptance.

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
line. The FAST statement and `$0F23` clear the saved SLOW request; the internal
`$02E7` entry preserves it. FAST returns only once the frame being drawn has
ended, NEW goes FAST before it clears memory, and the editor restores SLOW
after an error, including one while a line is being entered. A key that is
still down when a report appears or `INPUT` starts is not typed, and `PAUSE`
ends only on a key pressed during it, in FAST mode too. `FRAMES` keeps bit 15
set while bits 0 to 14 count down. A program that runs out of memory while it prints still gets
report 4 on a 1 KiB or 2 KiB machine: the report is printed with a smaller
memory margin. A `GOSUB` that would leave too little room stops with report
4, keeping enough to type NEW or RUN. A slice assignment copies its source
first, so `LET A$(2 TO 4)=A$(1 TO 3)` gives `AABCE`. `PLOT` and `UNPLOT` take
|x| and |y|, as on the ZX81. The quote image (SHIFT+Q) in a string prints as
one quote and lists as two, and G mode gives the graphics drawn on the keys.

No Sinclair ROM image or file is used to build it. A few short sequences are
nevertheless byte-for-byte the same as in Sinclair's ROM, because the display
hardware and the published interface fix them: the 17-byte INT routine at
`$0038` (every scan line must take the same 58 T-states, reload R and jump into
the row), the first 14 bytes of the NMI at `$0066`, BREAK-1 (four instructions
reading the SPACE row) and the key tables at `$007E`, which programs index
directly.

### v1 compatibility

v1 must match the real ZX81 on the five points below, apart from the known
gaps listed after them; any other difference on one of them is a defect.
Everything else is best effort. A difference there is a known
gap: it is added to the lists below when found and does not block a release.
Where Sinclair's *ZX81 BASIC Programming* manual says nothing, v1's behaviour
is best effort too.

1. **Interface.** The system variable addresses and the meaning of each one
   the ROM uses, the `.p` layout from `VERSN`, the restarts, the display
   routines and their timing, the key tables, the character codes and tokens,
   the tape trap addresses and the entry points listed above.
2. **Tapes.** A `.p` program that uses only what v1 supports loads, lists and
   runs, and keeps its FAST/SLOW state and autorun line.
3. **BASIC.** Each supported statement, function and operator gives the
   result, priority and report that the manual describes. A report names the
   line that was running, or 0 for a command. After report p/q, CONT goes to
   line q, or to q+1 when p is 9 (STOP). Report 0 does not move it. BREAK is
   checked before each line runs, so CONT after report D runs that line.
4. **Keyboard and editor.** The K, L, F and G cursors, the keyword and shifted
   tokens, entering, editing and deleting lines, the listing, `INPUT`, and the
   behaviours this section states.
5. **Integrity.** A BASIC program that does not use `POKE` or `USR` cannot
   crash the machine or corrupt memory. It cannot lock the machine up so that
   BREAK, or STOP typed at `INPUT`, no longer stops it. When it runs out of
   memory, it stops with report 4.

### Known gaps

This is stage 1: the editor, the BASIC statements and arithmetic needed by
ordinary programs, strings and slicing, numeric arrays, `FOR`/`GOSUB`, `RND`,
`INKEY$`, `PEEK`/`POKE`/`USR`, `PLOT`/`UNPLOT`, `SCROLL` and `PAUSE`. Missing:

- `SIN`, `COS`, `TAN`, `ASN`, `ACS`, `ATN`, `LN`, `EXP`, `SQR`, non-integer
  `**` and machine code calling the `RST 28` calculator stop with report A.
- No string arrays; a `FOR` loop that should run zero times runs once.
- A variable name ends at a space, so `LET MY VAR=1` gives report C. A name
  can have up to 255 characters (a longer one gives report C), and an array
  up to 255 dimensions (more give report 3).
- `PAUSE 0` returns at once.
- No up/down listing cursor keys (use `LIST n` and EDIT); no E-notation input
  (output is supported); `POKE` takes 0 to 255 only.
- No syntax check on entry: errors are reported when the line runs.
- No printer: `LPRINT`, `LLIST` and `COPY` do nothing.
- `LOAD`/`SAVE` ignore the name and work only through the core's tape unit.
- `fes.zx80`'s 4 KiB ROM has no open substitute.

Best-effort differences:

- **Memory margins.** v1 keeps more memory spare than Sinclair's ROM, so
  report 4 can come sooner. `GOSUB` needs about 88 free bytes, where the ZX81
  needs about 45, so a program that only just fits a real 1 KiB ZX81 can stop
  at its first `GOSUB`. A program that fills memory to within about 40 bytes
  can leave too little room to type. A tape that leaves less than about
  40 bytes free may not get back to the editor after `LOAD`.
- **Speed.** How fast BASIC runs, and the key repeat and debounce timing.
- **Arithmetic.** Results can differ beyond the digits `PRINT` shows. An
  integer `**` with an exponent of 2^32 or more gives report 6. So does one
  with a negative exponent whose positive power overflows (`10**-40`), where
  the ZX81 gives 0 or a tiny number, and so does a number written with more
  than 38 digits after the decimal point.
- **CONT after a command's report.** Such a report shows line 0, but CONT
  still goes where the last program report left it, not to line 0.
- **Machine code and spare memory.** Only the entry points listed above are
  supported. Of the key tables, only the unshifted one at `$007E` is the same
  byte for byte. The shifted, function and graphics tables at `$00A5`,
  `$00CC` and `$00F3` give the ZX81's character for each key, but their
  layout is this ROM's own. This ROM keeps its flags in SPARE1 (`$4021`) and a pointer in
  SPARE2 (`$407B`), and uses the printer buffer (`$403C`) and `MEMBOT` as
  scratch. A program that keeps data or machine code there can fail. Printing
  follows the print position in `S_POSN`, so setting `DF_CC` alone (by `POKE`
  or machine code) does not move it; PRINT-AT `$08F5` sets both.
- **The assembler.** `scripts/z80asm.py` is this ROM's build tool, not a
  general Z80 assembler. It rejects malformed lines and instructions the Z80
  cannot encode. A mis-encoding of an instruction the ROM does not use is a
  gap, not a defect.

### Simulation

`make sim-fes-zx81-openrom` (part of `make sim-fes-zx81`) boots the ROM on the
machine RTL with 1 KiB and 16 KiB RAM and on the 2 KiB NTSC TS1000. On each it
checks the display geometry against the original, key rollover, the display
mode after a line-entry error, DF_CC after `PRINT AT`, `INPUT` into an existing
variable over two rows, repeated `RETURN` without `GOSUB`, `INKEY$` with no
key and with Q held, a BREAK held past its report, `PAUSE` started with the
`RUN` NEWLINE still down, `SCROLL` with `DF_SZ` 0, `DIM A (5)`, `PLOT -1,-1`,
FUNCTION followed by a key with no function, an overlapping slice assignment,
a `GOSUB` that recurses until report 4, BREAK then CONT on `10 GOTO 10` (also
after a report 0 in between), CONT after STOP typed at `INPUT`, a FAST
`PAUSE` with a key already down, an expression nested deeper than the
machine stack can go (report 4, then NEW), the quote image in a printed
string, and the graphics from each G-mode key. It enters
and runs programs, saves and loads them in both FAST and SLOW modes through the
tape trap, scrolls the screen, and exercises the FAST/SLOW end-of-frame loop.
On 1 KiB and 2 KiB it checks that a program that runs out of memory while
printing shows report 4 and returns to the editor. On 1 KiB it checks that a
`DIM` that runs out of memory reports 4, and that `RETURN` from a subroutine
that filled memory still returns. On 16 KiB it
checks EDIT of a line longer than the screen, 20 STOP/CONT reports in SLOW
mode, a NEWLINE held through an autorun LOAD, 255- and 256-character
variable names, and a `DIM` with 256 dimensions. When the FogCast starter
tapes are in the checkout, it also loads and lists two `.p` files made by other
tools. A tape
saved by Sinclair's own ROM is still a manual check (the `load` mode of
`cores/fes-zx81/sim/openrom_tb.cpp`): the repository has no tape saved that
way. `tests/test_zx81_openrom.py` checks the assembler, including that it
rejects malformed lines and instructions it cannot encode, and the image's
fixed interface bytes.
The ROM has no kit acceptance yet.

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
