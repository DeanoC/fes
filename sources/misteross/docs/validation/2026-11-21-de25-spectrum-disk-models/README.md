# DE25 Spectrum +2A/+3 and Pentagon: signoff and packages

Date: 2026-11-21. Source `916e4a9788a5af42b8091c8eadebb4eedb656a9a`. Part of
#2042.

The DE25 Spectrum now builds its two disk models. One `fes_media_sdram` holds
the tape (media unit 0, the first 16 MiB of board SDRAM) and the disk (media
unit 1, the next 16 MiB); see `boards/de25nano/spectrum/README.md`.

## Evidence class

Quartus Prime Pro 26.3 generation, fit, timing signoff and design-assistant
review on Powerboat, signoff compile plus repeat compile for each model, and
the component simulation of all four models. No hardware run: both packages
record `hardware_acceptance = false`.

## Results

| | +2A/+3 (`plus3`) | Pentagon 128 (`pentagon`) |
|---|---|---|
| Core | `fes.spectrum-plus3` | `fes.pentagon` |
| Firmware image | 65,536 bytes | 49,152 bytes |
| RAM blocks (of 358) | 151 | 151 |
| Block memory bits | 2,501,632 | 2,501,632 |
| ALMs | 10,882 | 10,495 |
| Worst setup slack (ns) | 2.056 | 1.895 |
| Worst hold slack (ns) | 0.000 | 0.000 |
| `BUILD_ID` | `e427e98e66e13465f3057c70aaa52e44` | `8aef1654f25b615ad1d4f12c904aea05` |
| `core.rbf` sha256 | `a4d0d719cb380308dd8caf10b38b588383e0cad2ee1de66411d6d926d8896a37` | `0da39bffdd75059b10a5824fd3e3dcbca16df5ebe7d439dd3db9de1428700046` |
| `.fcore` sha256 | `859028cc572987a0a319662dae1ac52cdc79544cf8bf1ead68329f7f57d5668b` | `cbda9a6deceb848a793572826a2cf1154737959e42219e9cacaa08274b171aff` |

Both report `all_reported_timing_pass = true`, `package_timing_admission =
true` and `unreviewed_failed_rules = 0` of 85 signoff rules. The three rules
with findings are the shell's reviewed ones, with the same counts as the 48K
and 128K (RDC-50003 once, TMC-20011 twice, TMC-20012 five times). Each
producer refuses a package unless its repeat compile reproduces the signoff
bitstream byte for byte. `hps_bootloader_handoff.bin` is
`a17de6f2dbf5f4e93643b50b4c8291a33f930064f9a43bad3ece24e9c3dda04a`, the
shell's usual one.

Each manifest declares `fes.media.spectrum-tape` 2.0 and
`fes.media.spectrum-disk` 1.0 beside the shell and
`fes.fabric.firmware-upload` 1.0. FogCast's `InspectPackage` reads both
packages and admits them as firmware-upload machines at their image sizes.
The audio-clock review uses `de25-machine-pll-audio-clocks-v1`, which is still
unreviewed (#2042).

## Simulation

`boards/de25nano/spectrum/sim.py` passes for 48K, 128K, +2A/+3 and Pentagon.
On the disk models it commits an 8 KiB disk to unit 1, checks it is stored 16
MiB up and that the tape is untouched, and waits for the floppy drive to read
the disk header (byte 8 = 14, 16 KiB slots) through the fetch port. With the
fetch pointed at unit 0 the Pentagon run fails, so the check catches a
misrouted fetch.

## Not validated

- The disk models on Kit D25: a launch, a `.dsk` (+3) and a `.trd` (Pentagon)
  loading, as in the DE10 game corpus (#2042).

## Commands

```
python3 boards/de25nano/producers/build_spectrum_package.py --model plus3 --root <src>/sources/misteross \
  --vendor-archive <DE25-Nano_revB_v.1.0.0_ResourcePackage.zip> --quartus-root /data/altera_pro/26.3 \
  --output build/claude-2042/plus3 --package-output build/claude-2042/plus3-package
python3 boards/de25nano/producers/build_spectrum_package.py --model pentagon (same arguments, pentagon outputs)
```

Both ran from a clean detached checkout of the built revision under
Powerboat's shared Quartus Pro lock.
