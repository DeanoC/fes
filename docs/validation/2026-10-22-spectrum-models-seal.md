# FES ZX Spectrum models: Fuse-matched timing and sealed packages

On 2026-10-22 the Spectrum shell was rebuilt as a cycle-exact ULA with four
model packages (`fes.spectrum` 48K, `fes.spectrum128` 128K/+2,
`fes.spectrum-plus3` +2A/+3, `fes.pentagon` Pentagon 128), on the
session-display plane and HPS DDR like ZX81. This note records the host
simulation evidence and the HIP seals of the named packages. It is **host
evidence**: nothing here is a kit result, and no earlier Spectrum kit note
accepts these bytes.

## Timing model against Fuse 1.6.0

The reference was `libretro/fuse-libretro` at `e997e2bc` built from source
with instruction-trace and memory-access logging added for the comparison
(not committed anywhere; the method is in the core README). ROMs were the
local MAME-extracted Sinclair/Amstrad images; none are in git or a package.

| Check | Result |
| --- | --- |
| Fuse Z80 core tests (`z80/tests`, 1,356 instructions) replayed through `fes_z80_nmos` + `spectrum_contention.sv` | every contention point (`MC`/`PC` event) matches per T state; the LDIR/LDDR repeat address was corrected to DE |
| 48K ROM boot, 100 frames | 754,900 instructions match Fuse's trace (frame, T state, PC) |
| 128K ROM boot, 100 frames | 506,914 instructions match |
| +2A ROM boot, 100 frames | 484,319 instructions match |
| Pentagon (128K ROM pages), 100 frames | 568,722 instructions match |
| 48K BASIC border loop, 260 frames with typed keys | trace matches (1,056,763 instructions); frames 210, 230, 259 pixel-identical |
| Race sweeps (attribute, bitmap, border, floating bus, shadow screen, +2A port 1+4n), 32 frames each, all four models | every captured 320x240 frame pixel-identical to Fuse |

The CI machine simulation keeps one marker from this comparison: the frame
T state at which the diagnostic firmware's contended loop stores `$C3` is
14,950 (48K), 14,960 (128K), 14,955 (+2A/+3) and 14,946 (Pentagon), each one
T state after the write's T1 that Fuse logs for the same firmware.

## Sealed packages

Built by `scripts/build_fes_spectrum_oss.py --model …` from FES
`SOURCE_REVISION` on Powerboat (HIP, shared toolchain slot
`fac97e973f94adee…`, lock `cores/fes-spectrum/toolchain.lock`: Yosys
`3a5accbd`, nextpnr `9128800f`, Mistral `8fcc4cb4`), first-pass seed order
5, 4, 2, 1, 3, 6, 7, 8, 9, 10 at weight 2000, critexp 5.

SEAL_TABLE

Every seal passed the synthesis gate (exact M10K/TDP counts, three unsigned
display-plane products, read-only plane DDR port, firmware lane controls),
the socket validation, the ROM map (`expected_async_read=0`) and the
firmware-outside-sockets check. Package IDs:

PACKAGE_LIST

## What this does not show

No kit was available on the day (Kit A held by another HIL, Kit B awaiting
operator recovery). The packages have not been programmed, so keyboard,
joystick, tape, audio, the plane and the picture on HDMI remain open, as does
any game or demo on hardware. The simulation comparisons are against Fuse's
model of the machines, not against ULA captures.
