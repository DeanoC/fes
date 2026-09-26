# Cores

Use this lane to change a described FES core or to add one, including the
board-firmware splash. A core is a directory under `cores/`, a simulation
target, and a producer that can seal an artifact from a clean committed tree.

If the task is "does this PLL / M10K / DSP / IO primitive place and route",
that is [OSS place-and-route testing](oss-pnr.md). Do not add an
`experiments/NNN_*` directory for a console feature, and do not build a
product with `make oss EXP=…`.

The build contract is [the architecture](architecture.md). Each core directory
that has a `README.md` is the contract for that machine. `make help` lists
every simulation target. Dated notes under `docs/validation/` describe the day
they were written. They are not the schedule for this tree.

## Current cores

Parent recipe rows live in FES `config/core-recipes.toml`. The factory image
installs only `fes.pong`, `fes.zx81` and `fes.coleco`. A producer in this
module does not put a package on that image.

| Package | Tree | Mailbox | Lock | OSS seal | In factory image |
| --- | --- | --- | --- | --- | --- |
| `fes.pong` | `cores/fes-pong` | `fes.simple-game` | `toolchain.lock` | `make build-fes-pong` | yes |
| `fes.zx81` | `cores/fes-zx81` | `fes.simple-computer` | `toolchains/zx81-expansion.lock` | `make build-fes-zx81` | yes |
| `fes.coleco` | `cores/fes-coleco` | `fes.application` | `toolchains/coleco-sgm.lock` | `make build-fes-coleco` | yes; optional Coleco bus 2.0 socket |
| `fes.sms` | `cores/fes-sms` | `fes.simple-computer` | `toolchains/fes-sms.lock` | `make build-fes-sms` | no; package-only recipe |
| `fes.sg1000` | `cores/fes-sg1000` | `fes.simple-computer` | `toolchains/registered-memory.lock` | `make build-fes-sg1000` | package-only |
| `fes.catch` | `cores/fes-demo` | `fes.application` | `toolchain.lock` | `python3 scripts/build_fes_catch.py` | no; registered |
| `fes.demo`, `fes.demo-media`, `fes.demo-audio` | `cores/fes-demo` | `fes.application` | `toolchain.lock` | `make build-fes-demo`, `build-fes-demo-media`, `build-fes-demo-audio` | no; not registered |
| `fes.ramtest` | `cores/fes-ramtest` | `fes.application` | `toolchain.lock` | `make build-fes-ramtest` | no; not registered |
| splash / idle | `cores/fes-splash` | none | generic `toolchain.lock`, GPU router off | `make build-fes-splash` | not a play package; pinned as `sealed/fes-splash.rbf` |

`cores/pong` is the standalone Pong game module (`make sim-pong`). It is not
a package. The package is `cores/fes-pong`. ZX81 has no
`cores/fes-zx81/README.md`. Its machine contract is the ZX81 sections of
[the architecture](architecture.md).

Do not use `fes.mastersystem`. The Master System package id is `fes.sms`.

Quartus recipes (`make build-fes-zx81-quartus`, `build-fes-coleco-quartus`,
`build-fes-sg1000-quartus`, `build-fes-sms-quartus`) are bring-up oracles.
A failed HIP route does not fall back to Quartus. Quartus never programs a kit.
`--compile-only`, where the recipe has it, writes an RBF and timing evidence
without sealing. `--synth-only` on the OSS producers is a dirty-tree Yosys
probe and does not seal.

## Three results you must not collapse

| Command kind | Proves | Does not prove |
| --- | --- | --- |
| `make sim-fes-*` | Host simulation of the RTL you ran | An RBF, timing, HDMI, audio, or a kit |
| `make build-fes-*` (OSS) | A sealed artifact when the clean-tree checks and timing pass | Kit behavior, or acceptance of an older seal |
| `make build-fes-*-quartus` | The Quartus oracle for that recipe | The product bitstream |

Simulation targets refuse a shared toolchain cache. Unset
`FES_TOOLCHAIN_CACHE_ROOT` and do not pass `CACHE_ROOT` to `make sim-fes-*`.

A new `BUILD_ID` changes placement. An older sealed package, parent pin, or
kit note does not accept the bitstream you just built. Record new evidence
for the new bytes.

## Update an existing core

1. Read that core's `README.md` if it has one, and the matching section of
   [the architecture](architecture.md). SMS and SG-1000 also have an `-oss`
   simulation lane. Run that lane when you touch OSS-conditional RTL
   (`FES_SMS_OSS`, `FES_SG1000_OSS`, `FES_COLECO_OSS`). Both defines are
   required on the SMS and SG-1000 OSS sims. `FES_SMS_OSS` or
   `FES_SG1000_OSS` alone does not select the Coleco M10K and VDP wrappers.
2. Edit that core's RTL, constraints and tests. `cores/fes-common/rtl` is
   shared. The CPU wrapper, TV80, PLLs, RAMs, TMS9918 path and
   simple-computer mailbox there are used by more than one of Coleco, SMS
   and SG-1000. `fes_video_720p.v` is also on the Pong and demo path. A
   change in `fes-common` is not local to the core directory you opened.
3. Run the focused simulation (`make help` for the full list). Coleco's
   aggregate is `make sim-fes-coleco`. One board case is
   `make sim-fes-coleco-board-CASE` with `graphics`, `stream`,
   `interactive`, `controllers`, `vdp-io` or `sprites`.
4. Seal only from a clean committed tree. From this module:

```sh
make toolchain-fes-sms
make build-fes-sms CACHE_ROOT=/absolute/cache
```

Use the toolchain target that matches the lock in the table above.
`CACHE_ROOT` is optional. Omit it to use the repository-local HIP install.
`CACHE_ROOT=/absolute/cache make build-fes-sms` and
`make build-fes-sms CACHE_ROOT=/absolute/cache` are both valid. The same
spelling works for `build-fes-pong`, `build-fes-zx81`, `build-fes-coleco`
and `build-fes-sg1000`. Ambient `FES_TOOLCHAIN_CACHE_ROOT` is not how you
select that cache. If both variables are set they must be the same absolute
path.

Pong authenticates `toolchain.lock`. ZX81 authenticates
`toolchains/zx81-expansion.lock`. Coleco uses `toolchains/coleco-sgm.lock`.
SG-1000 uses `toolchains/registered-memory.lock`. SMS uses
`toolchains/fes-sms.lock`. They do not alias another lock's slot.

For those HIP play-package producers, the route log must name a live HIP
backend. A command line that merely contains `--router gpu` is not enough.
CPU-reference fallback is rejected. The splash producer is the exception:
it stays on the generic GPU-off toolchain.

SMS place-and-route is a first-pass search: it starts at seed 3 / HeAP 1000,
then the remaining `PLACER_SEEDS` and weight 300. Final structured `clk_sys`
and `pixel_clk` rows must meet 52 MHz and 74.25 MHz.

SG-1000's format-3 producer seals from a clean tree at seed 4. It exports a
blank 16 KiB cartridge ROM and authenticated map for download-time linking.
The 2026-09-16 gap ladder records a HIP route of the earlier synth-only
netlist (`BUILD_ID` all zeros), not acceptance of the new sealed bitstream.
The package-only recipe is registered in `config/core-recipes.toml` and is not
in the factory image.

5. Stop at the RBF unless the task also includes a parent change. Registering
   a recipe, preparing a package, and kit use are FES steps:
   [core developer workflow](../../../docs/core-development.md) and
   [kit sharing](../../../docs/kit-sharing.md). This module does not acquire a
   kit lease while it compiles.

Sealed play-package bytes land in `build/fes-<name>-oss/core.rbf`
(`build/fes-pong/core.rbf` and `build/fes-catch/core.rbf` for those two) and
are exported under `build/packages/<package-id>/`. The external
`build-inputs.json` is evidence. It is not a member of the `.fcore`.

## Add a core

Copy the closest sibling. Do not start from an experiment, and do not fork a
second CPU, VDP or PLL when `cores/fes-common` already has the one this
mailbox uses.

1. Pick an existing ABI unless the task includes a new one. A new mailbox or
   transport is mister-packages plus libmister-runtime, then RTL. A producer
   alone cannot make the host accept an interface the runtime does not
   implement.
2. Add `cores/fes-<id>/` with RTL, constraints, simulation and a README that
   says what the slice implements and what it does not. Package id, FogCast
   system name and directory name must be one choice. SMS is the example:
   directory `fes-sms`, id `fes.sms`, not a second spelling.
3. Add a producer next to `scripts/build_fes_sms_oss.py` or the closest
   sibling. It authenticates one lock, requires a clean committed module,
   and never programs hardware. A new core with a ROM embedded in CRAM seals
   a producer-derived ROM map in format 3, as ZX81 does; ROM-less cores and
   existing media-mailbox cores retain format 2 until explicitly converted.
   Wire `make sim-…` and
   `make build-…` in the Makefile. Add the tests the sibling has for manifest
   identity and the simulation you claim.
4. Update this guide's table, the core README, and the architecture section
   in the same change. A build-lane or output-path change that is only in
   the script will be missed.
5. A parent recipe row in `config/core-recipes.toml` is a separate FES
   decision, made when `make core-dev` should prepare that package. Factory
   image membership is a further decision. Do not add it because the core
   simulates.

## Splash

`cores/fes-splash` is board firmware: the U-Boot and Stop-idle HDMI splash.
It is not rooms, not attract, and not a format-2 play package. Do not register
it as `fes.*` in `config/core-recipes.toml`. Do not retarget Pong, the demo,
or a MiSTer menu RBF as the splash.

```sh
make toolchain
make sim-fes-splash
make build-fes-splash
```

`make build-fes-splash` uses the generic OSS lane (GPU router off). It writes
`build/fes-splash/core.rbf` and provenance. The file FES pins is the tracked
copy `sealed/fes-splash.rbf` (`image/build/native-inputs.toml`, both splash
and idle). Replacing the pin means replacing those tracked bytes and the FES
digest. See [cores/fes-splash/README.md](../cores/fes-splash/README.md) and
[sealed/README.md](../sealed/README.md).

The bitstream has no MiSTer user-io. Nothing answers Probe `0x0014` or HPS
framebuffer `0x002f`. Idle handling must not send those commands.

## Status that is easy to get wrong

- `fes.sms` is already a parent package-only recipe. It is not in the factory
  image. Historical parent pins and launch/Stop records do not accept HDMI
  audio on a bitstream built later. Kit HDMI-audio acceptance of the current
  tree is not recorded here.
- `fes.sg1000` has a package-only parent recipe. A historical
  launch/Stop note does not accept the current bitstream, and it does not
  make the package part of the factory image.
- Coleco audio is in the core (`make sim-fes-coleco-audio`, shared SN76489
  and 48 kHz I2S). It is not an unimplemented slice.
- Host simulation of a diagnostic ROM is not a photograph of HDMI and not a
  recording of HDMI audio.

Parent validation for a named package id is under FES `docs/validation/`.
Read the artifact ids in that record before treating it as evidence for the
tree you have open.
