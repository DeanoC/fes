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

ZX81, SMS, SG-1000, Apple II, C64, Spectrum and Atari ST seal format-3 packages
containing a blank base RBF and an authenticated ROM map. ZX81 requires an
exact 8192-byte firmware input; SMS requires an exact 32768-byte cartridge
input; SG-1000 requires an exact 16384-byte cartridge input. The factory
Coleco producer seals format-4 packages with an exact 8192-byte `coleco-bios`
and 16384-byte `coleco-cart`. Map extraction checks the selected Mistral
database, routed ROM placements and blank INIT bits. ROM-less producers
retain format 2. The Coleco MegaCart development producer emits format 4.
Which format is current is the FES [core alignment](../../docs/core-alignment.md)
contract. See [functional input identity](docs/architecture.md#functional-input-identity).

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

The bootstrap image installs `fes.menu` only (fes#919). Playable packages
install through the core library; `config/core-library.toml` registers
`fes.pong`, `fes.zx81`, `fes.coleco`, `fes.sms`, `fes.sg1000` and
`fes.ramtest` as supported, and `fes.spectrum`, `fes.spectrum128`,
`fes.spectrum-plus3`, `fes.pentagon`, `fes.atari-st`, `fes.c64`, `fes.apple2`
and `fes.riscv` as experimental. `fes.menu` is the idle display,
not a playable library entry. A package is not registered merely because its
producer exists.

The first 16-bit machine is the [Atari 520ST](cores/fes-atari-st/README.md):
real 68000, physical SDRAM arbitration, pluggable 192 KiB firmware and
expansion, peripherals, and all three ST video modes through shared
direct/scanline parts. Stock EmuTOS reaches GEM in host simulation through
the SDRAM and video paths. Run `make sim-fes-atari-st`; the registered HIP
producer is `make build-fes-atari-st`. The library install selects its firmware
shell and sealed Direct/Scanlines companions. Firmware remains a separate user
input; the image does not bundle EmuTOS or Atari ROMs.

The PC/XT development slice runs an original diagnostic ROM on a pinned 8088
through its motherboard, SDRAM and separate ISA CGA/MDA cards. The assembled
system also exercises DMA and refresh while one selected display crosses into
the shared Direct/Scanlines 720p output. `make sim-fes-pcxt` checks this on the
host. The host-only `make sim-fes-pcxt-fdc` tests the read-only 360 KiB XT
floppy adapter; `make sim-fes-pcxt-media` exercises the runtime mailbox and
physical SDRAM loader, and `make sim-fes-pcxt-mailbox` checks the PC/XT media
ABI. CI runs these focused floppy checks on pull requests. The complete
`make sim-fes-pcxt-dos` simulation boots the pinned GLaBIOS XT/5160 BIOS
and FreeDOS through the same whole-machine SDRAM, keyboard and floppy paths;
it permits downloads of pinned BIOS and FreeDOS fixtures. The DOS profile uses
the runtime PC floppy media path and does not accept host keyboard input.
Host simulations do not qualify hardware. `make build-fes-pcxt CACHE_ROOT=/absolute/toolchain-cache` builds the
normal Quartus diagnostic package, with authenticated tool/source identity,
three timing seeds and an independent byte-identical rebuild under
`build/fes-pcxt-quartus/`. The [core README](cores/fes-pcxt/README.md) describes
tool selection and the separate advisory nextpnr diagnostic. The exact package
in the [reviewed Kit A record](../../docs/validation/2026-10-29-pcxt-quartus-package/review-followup.json)
passes the static picture/completion-block and launch/Stop checks. External
SDRAM/HDMI timing, whole-board qualification, physically linkable ISA sockets
and BIOS/DOS/game compatibility remain open.

The pc386 machine's AT core (phase M1 of the [machine design](docs/pc386-machine-design.md))
boots the Bochs BIOS and FreeDOS to a prompt in host simulation with the Bochs
oracle as CPU. Phase M2 adds the RTL VGA, capture, presenter and display plane;
phase M3 adds the speaker, OPL2, Sound Blaster 2.0 DSP and mixer.
`make sim-fes-pc386` runs the device and audio checks plus the presenter,
capture and plane benches. `make sim-fes-pc386-boot` boots FreeDOS, sends a
relative-mouse packet and replays its VGA trace; `make sim-fes-pc386-vga`
checks the marked VGA modes; and `make sim-fes-pc386-sound` runs the audio
start-up exercise. See the
[core README](cores/fes-pc386/README.md).

## What builds now

Run the commands below from `sources/misteross/`. Preparing one registered
package without an image rebuild is the FES
[core developer workflow](../../docs/core-development.md).

### DE25-Nano board bring-up

The standalone [DE25-Nano diagnostic](boards/de25nano/README.md) exercises
the 50 MHz clock and eight active-low LEDs on the Agilex 5
`A5EB013BB23BE4SCS`. Run `make -C boards/de25nano sim` for host simulation
and `make -C boards/de25nano project` to prepare a local Quartus project.
The explicit Quartus Pro build produces a JTAG `.sof` diagnostic; it does
not export a described package. Compiler fitting, timing and physical
acceptance require the configured Pro installation and a DE25-Nano board.

The board definition lives in
[`mister-packages`](../mister-packages/packages/board/de25_nano.yaml).
The existing DE10-Nano producers, package admission and Quartus 17.0.2
oracles retain their current target. DE25-Nano HPS, boot media, runtime,
HDMI and memory integration are not implemented by this diagnostic.

The original RV32I CPU in `cores/fes-common/rtl/riscv` is the first-party
RISC-V lane; `make sim-fes-riscv` checks it against an independent model and
runs the [fes.riscv](cores/fes-riscv/README.md) system and board shell.
`make build-fes-riscv` seals that package through the shared HIP lane.

The original shared Z80 CPU has NMOS and documented fast variants. Run
`make sim-fes-z80` for standalone instruction, flag and pin timing checks; see
[the CPU contract](cores/fes-common/rtl/z80/README.md) for interfaces and the
Cyclone V timing diagnostic. SG-1000, ZX81 and the Spectrum models select the
NMOS variant; the Spectrum ULA stretches its clock enables for contention.
Coleco and SMS still instantiate TV80/T80. [Core alignment](../../docs/core-alignment.md)
schedules the move onto `fes_z80_nmos` one machine at a time. That swap has
not landed for those two packages.

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
| `fes.pong` | `make sim-fes-pong` | `make build-fes-pong` | none on the product path | library, supported |
| `fes.zx81` | `make sim-fes-zx81` (includes live session display) | `make build-fes-zx81` | `make build-fes-zx81-quartus` | library, supported |
| `fes.zx80` | `make sim-fes-zx81` | `make build-fes-zx80` | none | library, experimental, package-only |
| `fes.ts1000` | `make sim-fes-zx81` | `make build-fes-ts1000` | none | library, experimental, package-only |
| `fes.ts1500` | `make sim-fes-zx81` | `make build-fes-ts1500` | none | library, experimental, package-only |
| `fes.coleco` | `make sim-fes-coleco` | `make build-fes-coleco` | `make build-fes-coleco-quartus` | library, supported |
| `fes.sms` | `make sim-fes-sms` and `make sim-fes-sms-oss` | `make build-fes-sms` | `make build-fes-sms-quartus` | library, supported |
| `fes.sg1000` | `make sim-fes-sg1000-rom-link` (plus `make sim-fes-sg1000` and `make sim-fes-sg1000-oss` diagnostics) | `make build-fes-sg1000` | `make build-fes-sg1000-quartus` | library, supported |
| `fes.spectrum` (48K) | `make sim-fes-spectrum` | `make build-fes-spectrum` | none | library, experimental |
| `fes.spectrum128` (128K/+2) | `make sim-fes-spectrum` | `make build-fes-spectrum128` | none | library, experimental |
| `fes.spectrum-plus3` (+2A/+3) | `make sim-fes-spectrum` | `make build-fes-spectrum-plus3` | none | library, experimental |
| `fes.pentagon` (Pentagon 128) | `make sim-fes-spectrum` | `make build-fes-pentagon` | none | library, experimental |
| `fes.catch` | `make sim-fes-demo` | `python3 scripts/build_fes_catch.py` | no oracle | library, demo |
| `fes.ramtest` | `make sim-fes-ramtest` | `make build-fes-ramtest-100` | `make build-fes-ramtest-quartus` | library utility, supported, OSS 100 MHz |
| `fes.riscv` | `make sim-fes-riscv` | `make build-fes-riscv` | none | library, experimental |
| splash / idle | `make sim-fes-splash` | `make build-fes-splash` | none | `sealed/fes-splash.rbf`, not a play package |

Set `FES_ZX81_ROM` to the user-supplied edition 3 BASIC image for the BASIC,
expansion and SAVE simulations or the Quartus oracle. Pull-request CI runs
`make sim-fes-zx81-smoke`, which also exercises the ROM-independent model and
ZX80 tape-load probes.

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

The ZX81 1.7.0 source drives HDMI through the shared `fes.video.session-display`
plane, which blends runtime-owned DDR frames over the machine picture for
in-session HDMI controls. Opening and returning preserve CPU, RAM, firmware, expansion, capture
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
`make toolchain-fes` provisions the repository-level `toolchain.lock` HIP
lane for consumers that still select that lock. Every registered core has a
matching `toolchain-fes-<core>` target that provisions its
`cores/fes-<core>/toolchain.lock`: menu, Catch, Pong, ZX81, Coleco, SMS,
SG-1000, Apple II, C64, Spectrum, RISC-V, RAM Tester and Atari ST. Without a
cache, each producer reads the local install that its own target writes.
Pong's lock pins nextpnr `552ba246`, ZX81's `1eecf877` and RISC-V's
`8f453ebc`, so each has its own slot. Apple II, Atari ST and C64 use nextpnr
`5fe55f3d` from nextpnr #276; their shell validators require that producer
pin's exact F0/F1 `$ROUTETHRU` map. Apple II and C64 share one slot, while the
Atari ST lock has its own. Spectrum uses nextpnr main `bd7a1c0d`, which
contains #276 as merged, and has its own slot. Coleco has its own slot at
nextpnr `bec82e36`, which includes #296 and #299 and retains #276's exact F0/F1
`$ROUTETHRU` map; its lock also pins Yosys `5391eeb1`. The Coleco development shell still uses
nextpnr `9128800f`, whose exact route-through map is C/D. The validators
select the map from each producer's lock; they do not broaden the
reserved-boundary guard. The other five locks are byte copies of the
repository lock (nextpnr `9128800f`), so a shared `CACHE_ROOT` reuses its
cached slot for them. The factory recipe stays HIP. The October kit notes do
not accept a shared-pin seal. See [Cores](docs/cores.md).

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

`NEXTPNR_MISTRAL` overrides the binary. Locked nextpnr `9128800f` provides
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
| `toolchain.lock` | Shared OSS compiler pin (Yosys `3a5accbd`, Mistral `8fcc4cb4`, nextpnr `9128800f`) and the HIP cache slot for every described recipe whose lock matches it |
| `cores/fes-apple2/toolchain.lock`, `cores/fes-c64/toolchain.lock` | Per-core locks using nextpnr `5fe55f3d` for the #276 F0/F1 boundary route-through map |
| `cores/fes-spectrum/toolchain.lock` | Per-core lock using nextpnr main `bd7a1c0d` (#276 merged, same F0/F1 map) |
| `cores/fes-coleco/toolchain.lock` | Coleco's Yosys `5391eeb1` and nextpnr `bec82e36` pins; this lock has its own HIP cache slot |
| `boards/de10nano/` | Shared device and pin constraints |
| `sealed/` | Tracked splash/idle RBF |
| `build/` | Generated tools and RBFs, not source |
| `docs/validation/` | Dated evidence |

`make clean` is not implemented. Remove one experiment's `build/oss/<name>`
directory. Do not delete `build/toolchain` unless you intend to rebuild the
compilers.
