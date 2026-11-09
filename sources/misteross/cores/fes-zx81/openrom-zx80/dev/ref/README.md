# ZX80 reference recordings

What Sinclair's ZX80 ROM does on the FES ZX80 machine RTL, recorded as the
behaviour the open ZX80 ROM has to match. They contain no ROM bytes: only the
keys typed and what the machine held afterwards.

Each `p*.txt` holds one or more blocks. A block starts `=== KEYS` with the
script typed from a fresh reset at 16 KiB, for example
`1 0 O SHIFT+Y H I SHIFT+Y NL` (K-mode keys, `SHIFT+` for shifted ones, `NL`
for NEWLINE). It then dumps:

- `sys`: the system variables at `$4000`, and the pointers decoded;
- `prog`, `vars` and `eline`: the program, the variables and the edit line;
- `screen`: the display file as text, one row per line.

`prog3.o` is a small program image (`10 LET A=1`, `20 LET A=2`,
`30 LET A=3`, `100 PRINT "X"`), used to check LOAD through the machine's `$0207` trap.

## Recording

`../build_probe.sh ROM.hex OUTDIR [EXTERNAL_RAM]` builds the probe bench
(`../zx80_probe_tb.cpp`) on the machine RTL with a 4 KiB ROM image, one hex
byte per line. `EXTERNAL_RAM` 0 (the default) is 16 KiB, and 1 is the stock
1 KiB. With a hex dump of Sinclair's ROM (not in this repository):

    OUTDIR/Vzx81_machine probe "KEYS|KEYS|..." > pN.txt

types each script from a fresh reset and writes its block.
`OUTDIR/Vzx81_machine geometry` reports the video timing, and
`load FILE.o "KEYS"` LOADs an image first.

Sinclair's ROM keeps its cursor (`b0`) in the edit line. A ROM that marks the
cursor some other way should drop that byte before comparing.
