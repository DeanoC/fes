# ZX81 starter cassettes

FogCast embeds three independent, redistributable `.p` programs for the ZX81
workspace's default shelf. These assets are not ROMs. A household supplies its
own BASIC ROM through the existing firmware slot. Guess the Number supports the bare 1 KiB machine. The other entries recommend
16 KiB RAM. All use a keyboard; none needs a sound or character expansion. This is a
conservative configuration recommendation, not a measured minimum.

| ID | Program | Author | Licence | Bytes | Controls |
| --- | --- | --- | --- | ---: | --- |
| `guess-number` | Number guessing game | FES contributors | MIT | 313 | Guess 1–20 and NEW LINE; RUN plays again |
| `aritm` | Mental arithmetic training game | Mikael O. Bonnier | GPL-3.0-or-later | 8672 | Type answers and NEW LINE; `-1` leaves the problem set; follow the menus |
| `character-display` | Character display demo | maziac (wrapper credited upstream to stevexyz and Lardo) | MIT | 980 | Hold `S` to start |

Select a cassette before Start, or use Home on a keyboard / Select on a
controller to open the hardware room in a running session when the selected
ZX81 package supports in-session HDMI controls. Its visible cassette picker
can arm another tape or eject it while the machine keeps running. Older kit
packages retain selection before Start; the separate host display also offers
live controls. Enter `LOAD ""` in BASIC, then `RUN` if needed; selecting a tape
does not enter those commands.

These files have passed host-side bounds, digest and memory-layout checks.
The FES validation records identify hardware observations against an exact
core, BASIC ROM and cassette; host checks alone do not establish load/run
acceptance.

## Provenance and reproduction

### Guess the Number

Original FES homebrew written for this shelf in 2026, licensed MIT; see
`licenses/guess-number-MIT.txt`. Complete source is `source/guess-number.bas`.
Generate using the pinned ZXText2P converter described below, then collapse its
empty 793-byte display to the ZX81 1 KiB format (25 HALT bytes):

```sh
./zxtext2p -o assets/guess-number.p source/guess-number.bas
python3 source/compact-display.py assets/guess-number.p
```

The included MIT script adjusts VARS, E_LINE, CH_ADD, STKBOT and STKEND after
removing 768 empty display bytes. D_FILE and DF_CC stay unchanged. The tape is
313 bytes: 116 system bytes, 170 program bytes, 25 empty display bytes and two
terminators. `CLS` before each hint limits displayed text to one 24-character
line, so repeated guesses cannot grow the display. Two scalar number variables
need 12 bytes; the memory budget check reserves a further 64 bytes for numeric
input/editing, 64 for the calculator and 128 for the machine stack. This checks
normal numeric guesses; it does not establish hardware load/run acceptance.

### Aritm

The unmodified corresponding source is `source/aritm-zx81.bas`, from
[mobluse/aritmjs](https://github.com/mobluse/aritmjs/blob/863e12a32830722acc041359a611cf364f97ea04/aritm-zx81.bas),
commit `863e12a32830722acc041359a611cf364f97ea04`.
Its own header explicitly licenses the program GPLv3 or later. The full licence
is retained in `licenses/aritm-GPL-3.0.txt`.

`assets/aritm.p` was generated without autorun modification, using
[ZXText2P 1.00](https://freestuff.grok.co.uk/zxtext2p/) by Chris Cowley:

```sh
cc -O2 zxtext2p.c -lm -o zxtext2p
./zxtext2p -o assets/aritm.p source/aritm-zx81.bas
```

Converter download: `https://freestuff.grok.co.uk/zxtext2p/zxtext2p.zip`.
ZIP SHA-256: `ce9ce3c3d4d7992de7db7a12dd5fc7d322637cbaabd2b1c360aca4c743886dd4`.
Extracted `zxtext2p.c` SHA-256:
`09626ebd2e6c2b8d69c786380af4e672ca2fcaca889e9136acca792a6f513453`.
The converter itself is not shipped here. The source and archive hashes pin
its bytes independently of the download site's current version.

### Character Display

`assets/character-display.p` is an unmodified copy of `zx81-program.p` from
[maziac/zx81-sample-program](https://github.com/maziac/zx81-sample-program/tree/c0298a7d61f7e221d25cf9eed16f05b1487a7ac5),
commit `c0298a7d61f7e221d25cf9eed16f05b1487a7ac5`. The full upstream MIT licence
is retained in `licenses/character-display-MIT.txt`. All six corresponding
assembler source files are retained in `source/character-display/`.
Extra blank lines at EOF in `fill.asm` and `wrapper.asm` were removed to satisfy
the repository whitespace check; their assembler statements are unchanged.
Upstream documents sjasmplus 1.20.2 as a known working assembler:

```sh
cd source/character-display
sjasmplus --raw=../../assets/character-display.p wrapper.asm
```

The committed upstream binary is the selected asset; assembly reproduction has
not been performed here. `SHA256SUMS` pins every tape, corresponding source and
licence file in this directory. Run `sha256sum -c SHA256SUMS` here to check them.

## Adding tapes

Keep complete corresponding source, author attribution, explicit redistribution
terms, an immutable upstream reference and a checked SHA-256 with each asset.
A downloadable game or an archive described as a public-domain collection is
not by itself enough to establish permission for each contained work. Goblin,
Space Shuttle and the reconstructed historical 1K Chess were considered but
are not bundled because their original author permissions were not verified.
Do not label a tape kit-tested until actual loading and play have been observed.

`Entries()` returns the ordered shelf; `Lookup(id)` finds an exact stable ID.
Both return owned data copies, so callers cannot mutate embedded asset bytes.
`Data` is excluded from JSON metadata. Keep these independent cassette licences
with exported files; do not replace them with the application's licence.
