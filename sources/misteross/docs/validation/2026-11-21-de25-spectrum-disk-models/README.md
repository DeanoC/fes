# DE25 Spectrum +2A/+3 and Pentagon: signoff and packages

Date: 2026-11-21. Source `242bc5fcce377d3e6bdb8e6b936acd93a06e67ba`. Part of
#2042.

The DE25 Spectrum now builds its two disk models. One `fes_media_sdram` holds
the tape (media unit 0, the first 16 MiB of board SDRAM) and the disk (media
unit 1, the next 16 MiB); see `boards/de25nano/spectrum/README.md`.

## Evidence class

Quartus Prime Pro 26.3 generation, fit, timing signoff and design-assistant
review on Powerboat, signoff compile plus repeat compile for each model, the
component simulation of all four models, and one disk boot of each package on
Kit D25 (J8G). The packages still record `hardware_acceptance = false`; the
kit run is evidence, not acceptance.

## Results

| | +2A/+3 (`plus3`) | Pentagon 128 (`pentagon`) |
|---|---|---|
| Core | `fes.spectrum-plus3` | `fes.pentagon` |
| Firmware image | 65,536 bytes | 49,152 bytes |
| RAM blocks (of 358) | 151 | 151 |
| Block memory bits | 2,501,632 | 2,501,632 |
| ALMs | 10,861 | 10,468 |
| Worst setup slack (ns) | 2.031 | 1.863 |
| Worst hold slack (ns) | 0.000 | 0.001 |
| `BUILD_ID` | `44745c0738af819539496844e8fe69dc` | `85d94de676c24b96bab5b4221da29e72` |
| `core.rbf` sha256 | `50dcf6def8f4bfffebed4f2a0747413398b81812c8511d59197f967d70b06f36` | `b5f61ccf7d8925c4273112caf772a264c4e466593fca314f9a2f965df98ce280` |
| `.fcore` sha256 | `541109ef350a3b56852532243d70544156444979911b676d19432c6e08c7f6bc` | `4a32ae9c3fb2ab05ce6b0ef8ddeb73e8b260728485bc46b62e7e5b77bd5077aa` |

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
`fes.fabric.firmware-upload` 1.0. The disk unit comes with its optional
controller, as on DE10: `fes.media.spectrum-disk-plus3` on the +2A/+3 and
`fes.media.spectrum-disk-trdos` on the Pentagon. FogCast reads that
controller to choose the disk format; without it the host refused a disk
entry on Kit D25. FogCast's `InspectPackage` reads both
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

## Kit D25

On J8G at `55c6b4202bf0c575cb1a2c3ccb43ca0e28efa1b3`, with kit software built
from that head and user-supplied firmware, each package installed, took a
disk-backed library entry and launched. The disk came up on media unit 1
(`fes.media.spectrum-disk`, ready) and the machine booted it with no input:

- +2A/+3: a `.dsk` auto-booted from the +3 loader to the game's title menu.
- Pentagon 128: a `.trd` booted through TR-DOS to the game's main menu.

The captured frames, scaled to half size: `plus3-disk-boot.png` and
`pentagon-disk-boot.png`.

Before the controller declaration the host refused both disk entries
(`UNSUPPORTED_OPERATION`), so neither model launched.

The runner's next step, typing to open the disk catalog, got
a 503 from its private host's input endpoint, so the catalog check did not
run. That is the runner's input path, not the package; the boots above did not
need it. The kit was restored to its baseline software and released
`FREE (baseline verified)`.

## Not validated

- Keyboard input on the disk models, and the DE10 game corpus on D25 (#2042).

## Commands

```
python3 boards/de25nano/producers/build_spectrum_package.py --model plus3 --root <src>/sources/misteross \
  --vendor-archive <DE25-Nano_revB_v.1.0.0_ResourcePackage.zip> --quartus-root /data/altera_pro/26.3 \
  --output build/claude-2046ctl/plus3 --package-output build/claude-2046ctl/plus3-package
python3 boards/de25nano/producers/build_spectrum_package.py --model pentagon (same arguments, pentagon outputs)
```

Both ran from a clean detached checkout of the built revision under
Powerboat's shared Quartus Pro lock.
