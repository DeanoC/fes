# misteross

FPGA source for FES, built only from this repository under `sources/misteross/`.
The old standalone `DeanoC/misteross` repository is archived. Open PRs against
FES.

This module has two jobs. They share a board and some compilers. They are not
the same task.

| Job | You are here to… | Read |
| --- | --- | --- |
| OSS place-and-route testing | Prove a Cyclone V primitive or a small design through Yosys, nextpnr-mistral and Mistral, and sometimes compare it with Quartus | [OSS place-and-route testing](docs/oss-pnr.md) |
| Core update or new core | Change or add a described FES package, or the board-firmware splash | [Cores](docs/cores.md) |

An `experiments/NNN_*` directory is not a core. A `cores/fes-*` directory is not
an experiment. Do not move console RTL into an experiment to test
place-and-route, and do not grow an experiment into a package.

Build contracts — identity, lanes, compilers, package bytes — live in
[the architecture](docs/architecture.md). That file is not a task list.
Per-experiment results live in [the experiment catalog](docs/oss-experiments.md).
Files under `docs/validation/` are dated records, not current instructions.
The doc index is [docs/README.md](docs/README.md).

## What this module does not do

FogCast owns host selection, transfer and the target agent. libmister-runtime
programs the FPGA. misteross stops at the RBF. No build target programs a kit.
`make program` is an explicit JTAG SRAM maintenance diagnostic. Ordinary loads
go through FogCast as described packages; the contained development-RBF path
serves experiments.

ZX81, SMS and SG-1000 seal format-3 packages containing a blank base RBF and an
authenticated ROM map. ZX81 requires an exact 8192-byte firmware input;
SMS requires an exact 32768-byte cartridge input; SG-1000 requires an exact
16384-byte cartridge input. Map extraction checks the
selected Mistral database, routed ROM placements and blank INIT bits. Other
normal producers retain format 2. See [functional input identity](docs/architecture.md#functional-input-identity).

Coleco [video parts](docs/cores.md#video-parts-development-lane) link direct or
scanline processing into a frozen pixel-clock socket. The separate native-pixel
lane moves frame capture/scaling into a shared Direct/Scanlines consumer;
`make sim-fes-native-video` and `make sim-fes-coleco-native` validate it on the
host. `make synth-fes-native-video CACHE_ROOT=/absolute/cache` checks both
native consumers with the locked compiler; it does not route or seal them.
`make build-fes-coleco-native-video` seals a shell with a wider socket;
the existing part producer selects the matching native Direct/Scanlines backend
from its sealed marker. `make sim-fes-native-socket` checks the socket and both
parts together. FES selects this native shell for the factory package and
publishes both parts against that exact package. Native shells require a linked
part before launch. The raster lane remains available through
`make build-fes-coleco-video`, with separate slot/map/layout identities.
Hardware acceptance is recorded separately. The standalone `make build-fes-coleco`
command retains its CPU-only socket; `make build-fes-coleco-video` explicitly
builds the video variant. The shared RTL contract is owned by
[mister-packages](../mister-packages/docs/video-parts.md).

The factory image installs `fes.pong`, `fes.zx81`, `fes.coleco`, `fes.sms`,
`fes.sg1000`, `fes.spectrum`, `fes.ramtest` and `fes.atari-st`. Another
package is not added to that set merely because its producer exists.

The first 16-bit machine is the [Atari 520ST](cores/fes-atari-st/README.md):
real 68000, physical SDRAM arbitration, pluggable 192 KiB firmware and
expansion, peripherals, and all three ST video modes through shared
direct/scanline parts. Stock EmuTOS reaches GEM in host simulation through
the SDRAM and video paths. Run `make sim-fes-atari-st`; the registered HIP
producer is `make build-fes-atari-st`. The factory image selects its firmware
shell and sealed Direct/Scanlines companions. Firmware remains a separate user
input; the image does not bundle EmuTOS or Atari ROMs.

## What builds now

Run the commands below from `sources/misteross/`. Preparing one registered
package without an image rebuild is the FES
[core developer workflow](../../docs/core-development.md).

The original RV32I CPU in `cores/fes-common/rtl/riscv` is the first-party
RISC-V lane; `make sim-fes-riscv` checks it against an independent model and
runs the [fes.riscv](cores/fes-riscv/README.md) system and board shell.
`make build-fes-riscv` seals that package through the shared HIP lane.

The original shared Z80 CPU has NMOS and documented fast variants. Run
`make sim-fes-z80` for standalone instruction, flag and pin timing checks; see
[the CPU contract](cores/fes-common/rtl/z80/README.md) for interfaces and the
Cyclone V timing diagnostic. SG-1000 and Spectrum select the NMOS variant.
Spectrum also has an explicit documented-only 56 MHz development build;
`make sim-fes-spectrum-turbo` compares both modes and measures elapsed workloads.
Coleco, SMS and ZX81 retain their existing CPU implementations.

### OSS experiments

`make toolchain` (GPU router off), then `make sim EXP=…` and `make oss EXP=…`.
Output is `build/oss/<experiment>/top.rbf`.

The closed set is the keys of `scripts/experiment_policy.py`. Any other name
fails. The narrative list is the [experiment catalog](docs/oss-experiments.md).
Each design's contract is `experiments/<name>/expected.md`.

`make toolchain` and `make toolchain-fes` install into the same
`build/toolchain` prefix. The last one run wins. Freeze-scaffold pass 2 needs
the HIP tools from `make toolchain-fes`, not the GPU-off build.

### Cores

| Package | Simulate | OSS seal | Quartus oracle | Parent recipe |
| --- | --- | --- | --- | --- |
| `fes.menu` | `python3 scripts/sim_fes_menu.py` | `make build-fes-menu-package` | none | image idle display, not a playable core |
| `fes.pong` | `make sim-fes-pong` | `make build-fes-pong` | none on the product path | factory image |
| `fes.zx81` | `make sim-fes-zx81` (includes live session display) | `make build-fes-zx81` | `make build-fes-zx81-quartus` | factory image |
| `fes.coleco` | `make sim-fes-coleco` | `make build-fes-coleco` | `make build-fes-coleco-quartus` | factory image |
| `fes.sms` | `make sim-fes-sms` and `make sim-fes-sms-oss` | `make build-fes-sms` | `make build-fes-sms-quartus` | factory image |
| `fes.sg1000` | `make sim-fes-sg1000-rom-link` (plus `make sim-fes-sg1000` and `make sim-fes-sg1000-oss` diagnostics) | `make build-fes-sg1000` | `make build-fes-sg1000-quartus` | factory image |
| `fes.spectrum` | `make sim-fes-spectrum` | `make build-fes-spectrum` | none | factory image |
| `fes.catch` | `make sim-fes-demo` | `python3 scripts/build_fes_catch.py` | no oracle | registered, not in the factory image |
| `fes.ramtest` | `make sim-fes-ramtest` | `make build-fes-ramtest-100` | `make build-fes-ramtest-quartus` | factory utility, OSS 100 MHz |
| `fes.riscv` | `make sim-fes-riscv` | `make build-fes-riscv` | none | registered, not in the factory image |
| splash / idle | `make sim-fes-splash` | `make build-fes-splash` | none | `sealed/fes-splash.rbf`, not a play package |

`fes.sms` and `fes.sg1000` use the `fes.simple-computer` ABI with sealed, linked
cartridge ROMs. Diagnostic builds retain the media mailbox; the SMS ROM-link
OSS package disables its legacy blob commands. Do not use `fes.mastersystem`.
The SMS OSS producer (`fes-sms`) is a first-pass HIP
seed/weight search: it starts at seed 3 / HeAP 1000, then the remaining seeds
and weight 300. Core details are in [Cores](docs/cores.md).

Sealed outputs:

```text
build/fes-pong/core.rbf
build/fes-zx81-oss/core.rbf
build/fes-coleco-socket-v2/core.rbf
build/fes-sms-oss/core.rbf
build/fes-sg1000-oss/core.rbf
build/fes-catch/core.rbf
build/fes-riscv/core.rbf
build/packages/<package-id>/manifest.toml
build/packages/<package-id>/core.rbf
build/fes-splash/core.rbf
sealed/fes-splash.rbf
```

Quartus oracle RBFs, when a recipe has one, are
`build/fes-<name>-quartus/core.rbf`. They are not a fallback when the OSS
producer fails.

The ZX81 1.5.0 source includes a runtime-owned DDR plane for in-session HDMI
controls. Opening and returning preserve CPU, RAM, firmware, expansion, capture
and audio. Simulation covers complete-frame presentation, asynchronous display
control, live cassette operations and ordered drain/fault handling. A scoped
zero-build-ID route demonstrates three-clock feasibility with the locked ZX81
tools; it is not a sealed package or kit acceptance. A new clean-source seal and
exact-artifact HDMI/machine-state check remain required before hardware acceptance.

## Toolchains

```sh
make toolchain-check
make toolchain
```

`make toolchain` is the generic OSS lane (GPU router off), including splash.
`make toolchain-fes` is the shared HIP lane for FES Pong, menu, RAM Tester and
freeze-scaffold pass 2. `fes.menu` and both OSS `fes.ramtest` rates use the same
`toolchain.lock` HIP compiler slot (also provisioned by
`make toolchain-fes-ramtest`). Coleco uses `make toolchain-fes-coleco` against
`toolchains/coleco-sgm.lock`. SG-1000 uses `make toolchain-fes-sg1000`
against `toolchains/registered-memory.lock`. SMS uses
`make toolchain-fes-sms` against `toolchains/fes-sms.lock`. ZX81 uses
`make toolchain-fes-zx81` against `toolchains/zx81-expansion.lock`. Those
install under their own prefixes unless `CACHE_ROOT` selects the shared
cache. See [Cores](docs/cores.md).

## Freeze-scaffold cartridges

These are OSS place-and-route experiments (`900`–`907`), not a `fes.zx81`
seal. Cart rules, the ZX81 socket commands and kit probes are in
[OSS place-and-route testing](docs/oss-pnr.md#freeze-scaffold-cartridges).

Cart A is synth-only. Build it with `make oss EXP=900_expansion_bus`, then
compose it onto a routed 901 shell. Pass 2 needs HIP nextpnr from
`make toolchain-fes`.

```sh
make oss EXP=900_expansion_bus
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/901_plugged_base/routed.json \
  --shell-rbf build/oss/901_plugged_base/top.rbf \
  --cart 900_expansion_bus \
  --output build/oss/composed_901_plus_900.rbf
```

`NEXTPNR_MISTRAL` overrides the binary. Locked nextpnr `d869795b` provides
`--fes-scaffold` and `--fes-cart`. A binary without those flags fails closed.
This path does not seal `fes.zx81`.

## Layout

| Path | What it is |
| --- | --- |
| `experiments/` | OSS primitive and freeze-scaffold designs |
| `cores/` | Described packages and the splash |
| `cores/fes-common/` | RTL shared by more than one core |
| `scripts/experiment_policy.py` | Closed experiment list and checks |
| `scripts/build_fes_*.py` | Core producers |
| `toolchain.lock` | Generic OSS tools and the shared FES HIP slot |
| `toolchains/` | ZX81 and registered-memory locks |
| `boards/de10nano/` | Shared device and pin constraints |
| `sealed/` | Tracked splash/idle RBF |
| `build/` | Generated tools and RBFs, not source |
| `docs/validation/` | Dated evidence |

`make clean` is not implemented. Remove one experiment's `build/oss/<name>`
directory. Do not delete `build/toolchain` unless you intend to rebuild the
compilers.
