# 2026-10-22: Spectrum models, Fuse-matched timing, host seals and a kit A check

Record of the four Spectrum model packages built from one shell:
`fes.spectrum` 0.3.0 (48K), `fes.spectrum128` 0.1.0 (128K/+2),
`fes.spectrum-plus3` 0.1.0 (+2A/+3 without a disk drive) and `fes.pentagon`
0.1.0 (Pentagon 128). It records routed, timing-closed bitstreams, the host
simulations behind them and a kit A check of the sealed archives. It is not
acceptance of an image or release, and no earlier Spectrum kit note accepts
these bytes.

## Artifacts

Source FES `4de7f082c` (`sources/misteross`), producer
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (Yosys `3a5accbd`, Mistral `8fcc4cb4`,
nextpnr `9128800f`, HIP `gfx1100;gfx1201`), first-pass placement search in
the recipe's seed order 5, 4, 2, 1, 3, 6, 7, 8, 9, 10 at HeAP weight 2000,
critexp 5. Constraints: system 52.224 MHz, pixel 74.25 MHz, audio 12.288 MHz.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.3.0 | 5 | 55.68 | 105.05 | 149.19 | 8,294 | 3,867 | 190 (80 TDP) | 3 |
| `fes.spectrum128` 0.1.0 | 5 | 52.71 | 95.05 | 222.52 | 9,227 | 4,129 | 286 (96 TDP) | 3 |
| `fes.spectrum-plus3` 0.1.0 | 4 | 52.30 | 100.74 | 230.31 | 9,317 | 4,132 | 318 (96 TDP) | 3 |
| `fes.pentagon` 0.1.0 | 5 | 52.34 | 103.95 | 240.62 | 9,176 | 4,125 | 286 (96 TDP) | 3 |

The M10K column counts every block, including the true-dual-port screen
banks and tape image. The producer's synthesis gate checks the plain and TDP
counts exactly for each model, along with the three unsigned display-plane
products, the read-only plane DDR port, every pinned socket boundary and the
firmware lane controls.

- `fes.spectrum` 0.3.0: package `d038e5974faeb39a6c51960a22a28941ceb73807c9a04071244a85b22992f81e`, build `71e39cdb022c409d97fb3904a1661abb`, RBF sha256 `fd3c333778966b1ed32beb765896355779d18de79ab9623779087517065158f1` (2,801,809 bytes)
- `fes.spectrum128` 0.1.0: package `a8260bfa87bc9ea8d7edc8b566b5d85bd1fcf570e4a70d46216d33b1d6f57d26`, build `03425f5f814ef80406a18cc69fba2ebb`, RBF sha256 `49745b421d2f425e3f4d80b6458cc609885649ba912a980cbbd942027afd0d12` (3,139,286 bytes)
- `fes.spectrum-plus3` 0.1.0: package `1c6d53151f71016a3b1194713666bbcce12e325cf61ec3b593b422050eb0b853`, build `807dee4a375f690d10feb5538907c92e`, RBF sha256 `d0e72d8ed6ff1584b93b8368de97bba33dd0f7f7838fa01358ff282b9acb88a0` (3,207,299 bytes)
- `fes.pentagon` 0.1.0: package `fb05129499824d1da34c320034e79edfef5598ba0df2679a463e53d31a7f02fc`, build `9b212f6c69cc42b4bf191ae4579b85b1`, RBF sha256 `3ffe502488574a4c3481c05acf06022759b6c3c145d7bd292cee1ace198b13b8` (3,133,155 bytes)

The system domain's margin depends on the placement seed; the seed order is
part of the recipe, and a new `BUILD_ID` changes placement.

## Revision

The first seals of this branch, at FES `53a78d5d`, closed the 48K on seed 5
(54.15 MHz system) and the 128K on seed 4 (52.35 MHz). The +2A/+3 ran out of
the recipe's 600-second route lease on seeds 5 and 4, as did the 128K's seed
5, with several hundred wires still overused; Pentagon's seed 5 was as
congested when that run was stopped. The 48K's
critical path ran from the tape image's blocks through its 64-way read mux
into the player's block-length check in the same clock. Two changes
followed. Each RAM bank became its own memory, so the ULA's fetch port
reaches only the screen banks instead of every block (96 fewer second ports
on the 128K-class models). The tape player registers its byte after the
image mux; it steps once per T state, so nothing it does moves. Neither
change alters the machine at any T state, and every comparison below was
rerun on it. The superseded packages (48K `ae8fbc81…`, 128K `62c718c2…`)
were never published.

Review then found FLASH swapping ink and paper every 8 frames instead of 16
(the frame counter was one bit short). `0f81ace0` fixed it, and the machine
simulation now checks the period on every model; it fails against the old
counter. The second seal, from `4ffbcc68`, had closed all four models (48K
`680e0846…`, 128K `0777ebf5…`, +2A/+3 `286151f0…`, Pentagon `6301cfab…`)
and passed a first kit check like the one below; those packages are
superseded by the ones above. The FLASH fix moves no CPU or fetch timing,
and every Fuse comparison below was rerun at the sealed RTL.

## Timing model against Fuse 1.6.0

The reference was `libretro/fuse-libretro` at `e997e2bc`, built locally with
an instruction trace at `opcode_delay` and a memory-access log; none of that
is committed. ROMs were local MAME-extracted Sinclair and Amstrad images;
none are in git or a package. These comparisons ran at `4de7f082c`'s
RTL.

| Check | Result |
| --- | --- |
| Fuse Z80 core tests (`z80/tests`, 1,356 cases) through `fes_z80_nmos` and `spectrum_contention.sv` (`make sim-fes-spectrum-contention`) | every contention point (`MC`/`PC` event) matches per T state |
| 48K ROM boot, 100 frames | 747,932 instructions match Fuse's trace (frame, T state, PC) after the reset instruction |
| 128K ROM boot, 100 frames | 501,447 instructions match |
| +2A ROM boot, 100 frames | 478,438 instructions match |
| Pentagon 128 (128K ROM pages), 100 frames | 562,706 instructions match |
| Race sweeps of attribute, bitmap, border and floating-bus reads and writes across the ULA fetch points, plus shadow-screen switches (128K-class) and the +2A's port 1+4n floating bus, 32 frames each | all 40 sweeps on the four models: 1,280 captured 320x240 frames, every one pixel-identical to Fuse |

The Fuse Z80 tests also exposed the LDIR/LDDR repeat address: the shared
NMOS core now keeps DE on the bus for the five repeat T states, as the chip
and Fuse do. The CI machine simulation keeps one marker per model from these
comparisons: the frame T state at which the diagnostic firmware's contended
loop stores `$C3` is 14,950 (48K), 14,960 (128K), 14,955 (+2A/+3) and 14,946
(Pentagon), each one T state after the T1 Fuse logs for the same write.

## Host checks

- `make sim-fes-spectrum`: each model's machine running the open diagnostic
  firmware (paging, AY read-back, keyboard matrix, Kempston, probe card,
  tape pilot, border and ink pixels, contended timing marker), the
  frame-bank video (31 source frames in 40 HDMI frames, no tearing), the
  board shell through the mailbox (capability bits, tape limits, HID and
  Sinclair joystick input) and the tape player.
- `scripts/test_changed.py --base origin/main` at `1e289e7c` passed every
  lane, including the RTL simulations of every core family (SG-1000 and ZX81
  share the NMOS Z80) and the misteross and FES Python tests; `--base
  1e289e7c` at this record's commit passed the lanes that the FLASH and
  snapshot fixes select.
- `make check`.

## Kit A check

On 2026-10-22 the four sealed archives above ran on kit A through an
isolated private host (`~/tmp/c982/hil982.py` driving a local `fogcast-api`
container). The image was not changed; the kit software was replaced for
the overlay run only and then restored.

- The kit's baseline software `f48b11f1` predates the session display plane
  (#956). It imports these packages but refuses them at compatibility,
  "required computer interface is unsupported", as it refuses any core that
  declares `fes.video.session-display` (run `c982a`).
- Run `c982c` (12:59–13:11) installed this branch's kit software
  (`kit-hil982-4de7f082-r1`, bundle `b3747709…`; it is `main`'s, the branch
  changes none) on the running baseline image `5af6162b`, activated it, ran
  the four sessions and restored `f48b11f1` (previous `7a65cc3e`). Image
  fingerprints before and after match, and the kit was released on its
  recorded baseline.

Each model imported, passed compatibility, linked its ROM from local media
(`48.rom`, `128.rom`, `plus3.rom`, and `128.rom` for the Pentagon; nothing
stored), launched, was reported by the kit as running that core on three
status samples, and stopped back to idle. Frames recorded during each
session showed:

| Core | Boot | Keyboard (USB HID through the private host) |
| --- | --- | --- |
| `fes.spectrum` | "© 1982 Sinclair Research Ltd", K cursor | `P` (PRINT), `2`, Control+`K` (+), `2`, Enter printed `4` and `0 OK, 0:1` |
| `fes.spectrum128` | 128 menu (Tape Loader highlighted), "© 1986 Sinclair Research Ltd" | Down, Enter opened 128 BASIC; `print 2+2` printed `4` and `0 OK, 0:1` |
| `fes.spectrum-plus3` | "128 +2A" menu, "©1982, 1986, 1987 Amstrad Plc.", "Drive M: available." | Down, Enter opened +3 BASIC; `print 2+2` printed `4` and `0 OK, 0:1` |
| `fes.pentagon` | 128 menu, "© 1986 Sinclair Research Ltd" | Down, Enter opened 128 BASIC; `print 2+2` printed `4` and `0 OK, 0:1` |

The 128K-class sessions then typed `flash 1: paper 2: cls`. The editors
discard the key that dismisses the `0 OK` report and missed the next one
while redrawing, so the line arrived as `ash 1: paper 2: cls` (`lash …` on
the Pentagon) and the editor flagged it; Fuse 1.6.0, given the same keys at
the same rate, shows the same line. The editor's error marker is a FLASH
cell, so the recordings time FLASH on hardware: 94 swaps in 30 s at 0.320 s
each on the 128K and +2A/+3, and 0.327 s on the Pentagon, which is 16
frames at 50.02 Hz and at the Pentagon's 48.83 Hz.

Each picture filled the 960x720 plane window: white border and paper, black
ink, and the 128 menus' coloured stripes and highlight. The audio capture
kept only about 13 s of each session's samples, and what it kept holds
beeper edges on every model: a 48K key click, and two-unit steps decaying
with the speaker filter's 1.1 ms on the 128K-class models. The frames and
recordings are in `~/tmp/c982/fr` on Powerboat (`fr-c982b` holds the run of
the superseded packages).

## Not established

Tape loading, the Kempston and Sinclair joysticks, AY music, expansion
cards, and any game or demo on hardware. Kit A's recorded baseline software,
and any kit still on `f48b11f1`, refuses these packages until it runs kit
software that includes #956. The simulation comparisons are against Fuse's
model of the machines, not against ULA captures. No image was built:
`fes.spectrum` stays one of the image selector's IDs, the other three are
registered for `make core-dev` only, and the default image profile selects
none of them.
