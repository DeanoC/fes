# Open ZX80 ROM (parked work in progress)

A clean-room, GPL-2.0-or-later integer BASIC for the `fes.zx80` model, written
for FES like the [open ZX81 ROM](../openrom). **It does not fit the ZX80's
4 KiB ROM yet**, so it is not built by the Makefile, not packaged, and not
documented as a user option. `scripts/build_zx81_openrom.py --model zx80`
stops with "floating code does not fit"; the last measurement was 3691 bytes
of code and tables (3618 with every `jr` short) against 3512 bytes free
beside the 512-byte character set (`$0E00`).

## What it does

In an oversized development image it matches the Sinclair ZX80 ROM on every
recorded probe (memory and screen dumps in `dev/ref`) and passes the
regression scripts in `dev/regress.py`:

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
time whatever is pressed. `TOP_LINES`, `BOT_LINES` and `VSYNC_PAD` in
`hw.asm` are not yet tuned on the RTL (the original gives 310 lines, 6 of
sync, the cursor cell at lines 243-250, pixels 36-43, as
`dev/zx80_probe_tb.cpp geometry` shows).

Deliberate differences: SAVE does nothing (the core keeps no ZX80 tape
output); an overflow stops its statement at once (the original carries on with
effects suppressed); the listing pages rather than scrolling by one line; the edit line is not emptied while a report shows; a line that was never
checked (only a hand-made image can hold one) stops at a wrong symbol with
report 0.

## Working on it

    dev/build_dev.sh                                   # build/zx80-openrom-dev/dev.bin
    python3 dev/regress.py ../../../build/zx80-openrom-dev/dev.bin ../../../build/zx80-openrom-dev/dev.sym
    python3 dev/compare.py ../../../build/zx80-openrom-dev/dev.bin ../../../build/zx80-openrom-dev/dev.sym

(paths from this directory). The harness needs the `z80` Python package. On
the RTL, `dev/build_probe.sh ROM.hex OUTDIR` builds `zx80_probe_tb.cpp`;
`OUTDIR/Vzx81_machine probe "KEYS"` dumps memory and screen after typing.

To fit 4 KiB about 150-190 bytes must still go (it was 620). The source asks
the assembler for `floatorder`, the layout that keeps the most `jr` short;
about 35-45 stay long, so the code with every `jr` short has to come to
roughly 3470 bytes. `dev/build_dev.sh` takes a few seconds for that search.
