# Open ZX80 ROM (work in progress)

A clean-room, GPL-2.0-or-later integer BASIC for the `fes.zx80` model, written
for FES like the [open ZX81 ROM](../openrom). It fits the ZX80's 4 KiB ROM:
`make zx80-openrom` (in `sources/misteross`) writes `build/zx80-openrom/`
with 6 bytes free beside the 512-byte character set (`$0E00`), and
`make sim-fes-zx80-openrom` replays the recorded scripts on the RTL. It is not
yet documented as a user option.

## What it does

The 4 KiB image matches the Sinclair ZX80 ROM on every recorded probe
(memory and screen dumps in `dev/ref`), both in the fast harness and on the
FES ZX80 RTL (`dev/build_probe.sh`), and passes the regression scripts in
`dev/regress.py`:

- the published interface: system variables, program and variable record
  formats, character codes and keywords, `.o` images loaded through the
  core's trap at `$0207` (back at `$0203`), INT display at `$0038`;
- the K/L cursor and the edit-time syntax check (inverse S, NEWLINE refused);
- listing with the current-line marker, EDIT, cursor left/right/up/down,
  HOME, RUBOUT; INPUT with its line after the program's output;
- all statements; integer arithmetic with overflow (report 6); strings,
  `TL$`, `CODE`, `CHR$`, `STR$`; one-dimensional arrays; `FOR`, `GOSUB`;
  `RND`/`RANDOMISE` with the original's sequence; `PEEK`, `POKE`, `USR`;
  reports `m/n`, `CONTINUE`, BREAK; 1 KiB and 16 KiB RAM.

The display has no NMI on the ZX80: border lines are counted by the same
INT/HALT mechanism as the character rows, and an idle frame takes the same
time whatever is pressed. On the RTL, `dev/zx80_probe_tb.cpp geometry`
gives what the original gives: 310 lines, 6 of sync, the cursor cell at
lines 243-250, pixels 36-43 (`TOP_LINES`, `BOT_LINES` and `VSYNC_PAD` in
`hw.asm` set them).

Deliberate differences: SAVE does nothing (the core keeps no ZX80 tape
output); an overflow stops its statement at once (the original carries on with
effects suppressed); the listing pages rather than scrolling by one line; the edit line is not emptied while a report shows; a line that was never
checked (only a hand-made image can hold one) stops at a wrong symbol with
report 0.

## Working on it

    dev/build_dev.sh                                   # build/zx80-openrom-dev/dev.bin
    python3 dev/regress.py ../../../build/zx80-openrom/zx80-openrom.bin ../../../build/zx80-openrom/zx80-openrom.sym
    python3 dev/compare.py ../../../build/zx80-openrom/zx80-openrom.bin ../../../build/zx80-openrom/zx80-openrom.sym

(paths from this directory). The harness needs the `z80` Python package. On
the RTL, `dev/build_probe.sh ROM.hex OUTDIR` builds `zx80_probe_tb.cpp`;
`OUTDIR/Vzx81_machine probe "KEYS"` dumps memory and screen after typing, and
`dev/rtl_compare.py OUTDIR/Vzx81_machine` replays every recorded script and
types some with SHIFT pressed first, as a USB keyboard does.

The fit is tight. The source asks the assembler for `floatorder 91`, the
layout (from a seeded search) that keeps the most `jr` short and packs the
gaps; with another seed, or a few more bytes of code, it may not fit, and
`dev/build_dev.sh` (an 8 KiB image the harness can run whatever the size)
still works. The search takes a few seconds.
