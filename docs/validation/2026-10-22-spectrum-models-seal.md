# 2026-10-22: Spectrum models, Fuse-matched timing and host seals

Host-only record of the four Spectrum model packages built from one shell:
`fes.spectrum` 0.3.0 (48K), `fes.spectrum128` 0.1.0 (128K/+2),
`fes.spectrum-plus3` 0.1.0 (+2A/+3 without a disk drive) and `fes.pentagon`
0.1.0 (Pentagon 128). It records routed, timing-closed bitstreams and the
host simulations behind them. It is not a kit diagnostic and not hardware
acceptance; no bitstream was loaded on a board, and no earlier Spectrum kit
note accepts these bytes.

## Artifacts

Source FES `4ffbcc68f` (`sources/misteross`), producer
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (Yosys `3a5accbd`, Mistral `8fcc4cb4`,
nextpnr `9128800f`, HIP `gfx1100;gfx1201`), first-pass placement search in
the recipe's seed order 5, 4, 2, 1, 3, 6, 7, 8, 9, 10 at HeAP weight 2000,
critexp 5. Constraints: system 52.224 MHz, pixel 74.25 MHz, audio 12.288 MHz.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.3.0 | 5 | 52.91 | 97.01 | 240.15 | 8,250 | 3,866 | 190 (80 TDP) | 3 |
| `fes.spectrum128` 0.1.0 | 4 | 53.83 | 86.99 | 241.43 | 9,195 | 4,128 | 286 (96 TDP) | 3 |
| `fes.spectrum-plus3` 0.1.0 | 5 | 52.31 | 102.56 | 200.96 | 9,268 | 4,131 | 318 (96 TDP) | 3 |
| `fes.pentagon` 0.1.0 | 5 | 54.88 | 89.49 | 163.35 | 9,148 | 4,124 | 286 (96 TDP) | 3 |

The M10K column counts every block, including the true-dual-port screen
banks and tape image. The producer's synthesis gate checks the plain and TDP
counts exactly for each model, along with the three unsigned display-plane
products, the read-only plane DDR port, every pinned socket boundary and the
firmware lane controls.

- `fes.spectrum` 0.3.0: package `680e0846a1b9bbe946cb29f49e2b1bb860e3678f2184ac91b64ce3d81d8bb82f`, build `10422c661be4ed8c26cb372ee119fb5b`, RBF sha256 `87acdba3b9617deab5044d95fca5a08de99e911a35534e056bfb62e980600f70` (2,825,403 bytes)
- `fes.spectrum128` 0.1.0: package `0777ebf54c97edaf1730da81a0ce110fcb71cc664d45ffa43332da06fbe1274f`, build `0049496884cef6b76af6d98b2c965cc3`, RBF sha256 `effc58b40dd1661929de4d3a510a95d24443483d838703e492ea2362782facc6` (3,187,368 bytes)
- `fes.spectrum-plus3` 0.1.0: package `286151f0f9131b44fee8c9ae01591a4f2220277cf0b31bc58569f24fbbaa189e`, build `11c388d73e1e1611d64c9bb1087be9b8`, RBF sha256 `c4078086fd8ffc3cf5cdb2d2ec411b407d10dbfeb3fa4264cb9222cc6f07fce0` (3,193,306 bytes)
- `fes.pentagon` 0.1.0: package `6301cfab444d1909c6a92526ca26b0db8b5836afecc9131f26a7f6046cb29e4d`, build `8a50dad34e43b8ebd6dbdf33e0cc0270`, RBF sha256 `9d05b930c1b0b7c45c5a8156817ee17defe3ddfe75e6cd694549d709ba3a59f5` (3,127,993 bytes)

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

## Timing model against Fuse 1.6.0

The reference was `libretro/fuse-libretro` at `e997e2bc`, built locally with
an instruction trace at `opcode_delay` and a memory-access log; none of that
is committed. ROMs were local MAME-extracted Sinclair and Amstrad images;
none are in git or a package. These comparisons ran at `4ffbcc68f`'s
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
- `make sim-fes-z80`, `make sim-fes-sg1000` and `make sim-fes-zx81`, which
  share the NMOS core.
- `python3 -m unittest tests.test_build_fes_spectrum tests.test_pnr_recipes
  tests.test_fes_hip_lane tests.test_spectrum_socket_route_through` in
  `sources/misteross`; `make check` and `scripts/test_changed.py --base
  origin/main` in FES.

## Not established

Anything on a kit. No kit was free on the day (A held by another HIL, B
awaiting operator recovery), so HDMI output through the display plane,
keyboard and joystick input, tape loading, beeper and AY audio, and any game
or demo on hardware remain open. The simulation comparisons are against
Fuse's model of the machines, not against ULA captures. No image was built:
`fes.spectrum` stays one of the image selector's IDs, the other three are
registered for `make core-dev` only, and the default image profile selects
none of them.
