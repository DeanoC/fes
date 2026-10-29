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

Boards, toolchains and support tiers for these cores are the parent
[core support matrix](../../../docs/support-matrix.md). Parent recipe rows
live in FES `config/core-recipes.toml`. The bootstrap
image installs the idle display `fes.menu` only (fes#919); every playable
package is a core-library installation with a standing in
`config/core-library.toml`. `fes.coleco` and `fes.atari-st` are video-socket
shells whose sealed Direct and Scanlines parts install alongside them. ST
firmware remains a separate user input. A producer in this module registers
nothing by itself.

| Package | Tree | Mailbox | Lock | OSS seal | Library standing |
| --- | --- | --- | --- | --- | --- |
| `fes.menu` | `cores/fes-menu` | `fes.application` with menu display and HPS DDR | `cores/fes-menu/toolchain.lock` | `make build-fes-menu-package` | bootstrap image; idle display, not playable |
| `fes.pong` | `cores/fes-pong` | `fes.simple-game` | `cores/fes-pong/toolchain.lock` | `make build-fes-pong` | supported |
| `fes.zx81` | `cores/fes-zx81` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-zx81/toolchain.lock` | `make build-fes-zx81` | supported |
| `fes.zx80` | `cores/fes-zx81` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-zx81/toolchain.lock` | `make build-fes-zx80` | experimental; PAL, 4 KiB ROM, 1 KiB RAM, no NMI |
| `fes.ts1000` | `cores/fes-zx81` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-zx81/toolchain.lock` | `make build-fes-ts1000` | experimental; NTSC, 8 KiB ROM, 2 KiB RAM |
| `fes.ts1500` | `cores/fes-zx81` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-zx81/toolchain.lock` | `make build-fes-ts1500` | experimental; NTSC, 8 KiB ROM, 16 KiB RAM |
| `fes.coleco` | `cores/fes-coleco` | `fes.application` with session display and HPS DDR | `cores/fes-coleco/toolchain.lock` | `make build-fes-coleco-native-video` (FES selects the native variant) | supported; optional Coleco bus 2.0 and mandatory linked native video; Direct and Scanlines parts ship alongside |
| `fes.sms` | `cores/fes-sms` | `fes.simple-computer` | `cores/fes-sms/toolchain.lock` | `make build-fes-sms` | supported |
| `fes.sg1000` | `cores/fes-sg1000` | `fes.simple-computer` | `cores/fes-sg1000/toolchain.lock` | `make build-fes-sg1000` | supported |
| `fes.catch` | `cores/fes-demo` | `fes.application` | `cores/fes-catch/toolchain.lock` | `python3 scripts/build_fes_catch.py` | demo |
| `fes.apple2` | `cores/fes-apple2` | `fes.computer` | `cores/fes-apple2/toolchain.lock` | `make build-fes-apple2` (HIP default; shared-pin seal still open) | experimental; per-slot cards via `scripts/build_apple2_slot_card.py` |
| `fes.c64` | `cores/fes-c64` | `fes.computer` with session display and HPS DDR | `cores/fes-c64/toolchain.lock` | `make build-fes-c64` (shared-pin CUDA seal recorded) | experimental; cartridge cards via `scripts/build_c64_slot_card.py` |
| `fes.spectrum` | `cores/fes-spectrum` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-spectrum/toolchain.lock` | `make build-fes-spectrum` | experimental; four edge sockets |
| `fes.spectrum128` | `cores/fes-spectrum` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-spectrum/toolchain.lock` | `make build-fes-spectrum128` | experimental; four edge sockets |
| `fes.spectrum-plus3` | `cores/fes-spectrum` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-spectrum/toolchain.lock` | `make build-fes-spectrum-plus3` | experimental; four edge sockets |
| `fes.pentagon` | `cores/fes-spectrum` | `fes.computer` with session display and HPS DDR; `fes_z80_nmos` | `cores/fes-spectrum/toolchain.lock` | `make build-fes-pentagon` | experimental; four edge sockets |
| `fes.demo`, `fes.demo-media`, `fes.demo-audio` | `cores/fes-demo` | `fes.application` | `toolchain.lock` | `make build-fes-demo`, `build-fes-demo-media`, `build-fes-demo-audio` | no; not registered |
| `fes.ramtest` | `cores/fes-ramtest` | `fes.application` with `fes.memory.hps-ddr` | `cores/fes-ramtest/toolchain.lock` (100 and 130 MHz) | `make build-fes-ramtest-100` (100 MHz), `make build-fes-ramtest-130` (explicit) | supported; 100 MHz catalog utility, not in the bootstrap image |
| `fes.atari-st` | `cores/fes-atari-st` | `fes.computer` | `cores/fes-atari-st/toolchain.lock` | `make build-fes-atari-st` | experimental; Atari 520ST firmware shell and Direct/Scanlines parts |
| `fes.riscv` | `cores/fes-riscv` | `fes.application` with `fes.gamepad` | `cores/fes-riscv/toolchain.lock` | `make build-fes-riscv` | experimental; original RV32I CPU from `cores/fes-common/rtl/riscv` |
| `fes.pc386-diag` | `cores/fes-pc386` | `fes.computer` with session display and HPS DDR; no CPU, a bus master runs commands from the DDR window | `cores/fes-pc386/toolchain.lock` | `make build-fes-pc386-diag` | no; not registered; the pc386 M4 hardware diagnostic |
| splash / idle | `cores/fes-splash` | none | generic `toolchain.lock`, GPU router off | `make build-fes-splash` | not a play package; pinned as `sealed/fes-splash.rbf` |

`cores/pong` is the standalone Pong game module (`make sim-pong`). It is not
a package. The package is `cores/fes-pong`. ZX81 has no
`cores/fes-zx81/README.md`. Its machine contract is the ZX81 sections of
[the architecture](architecture.md).

Do not use `fes.mastersystem`. The Sega Master System package id is `fes.sms`.

Currency against the shared HIP pin, the first-party Z80 and the two display
contracts is [core alignment](../../../docs/core-alignment.md). A dedicated
lock in the table above is the pin that producer authenticates today. It is
not permission to add another lock.

Quartus recipes (`make build-fes-zx81-quartus`, `build-fes-coleco-quartus`,
`build-fes-sg1000-quartus`, `build-fes-sms-quartus`) are bring-up oracles.
A failed HIP route does not fall back to Quartus. Quartus never programs a kit.
`--compile-only`, where the recipe has it, writes an RBF and timing evidence
without sealing. `--synth-only` on the OSS producers is a dirty-tree Yosys
probe and does not seal.

## Video-parts development lane

The native-pixel development lane moves framebuffer and HDMI timing into the
linked part. Run
`make sim-fes-native-video` for Direct/Scanlines, complete-frame publication
and unrelated-clock transport, and `make sim-fes-coleco-native` for the actual
registered VDP source at its fractional raster cadence. Its optional
`FES_COLECO_NATIVE_VIDEO_DEV` top branch supports an inline consumer for simulation;
`make build-fes-coleco-native-video` also selects
`FES_COLECO_NATIVE_VIDEO_PART_DEV` to seal the source adapter, clock crossing and
vacant physical socket. The shell needs one native video part to produce HDMI.
`make sim-fes-native-socket` tests Direct and Scanlines through that socket.
See [the architecture](architecture.md#video-parts) for that boundary and the
new native contract. `make synth-fes-native-video CACHE_ROOT=/absolute/cache`
checks Direct and Scanlines RAM mapping with the locked compiler and writes
diagnostics beneath `build/synth/fes-native-video/`; it accepts working-tree
sources and does not route, seal or program them. For a sealed native build,
use the commands below with `build-fes-coleco-native-video` and
`VIDEO_SHELL="$PWD/build/fes-coleco-native-video"`. The part producer chooses the
native profile from that package's exact marker. Its 48 M10Ks must fit the
wider reserved slot, meet timing and leave every CRAM bit outside the native
fence unchanged before publication. The factory selects this native shell and
publishes both exact-shell companions. Normal library Play links the saved
video preference and optional CPU expansion before download; it rejects an
uncomposed native shell. Developer parts loads remain available.

The [native Kit 2 record](../../../docs/validation/2026-10-03-native-video-parts-kit2.md)
binds routed Direct/Scanlines parts, full-CRAM composition equality and clean
HDMI video/audio captures. Acceptance is limited to those named developer
artifacts. The [native factory host record](../../../docs/validation/2026-10-03-native-video-factory-build.md)
binds the later exact-shell Direct/Scanlines and SGM composition proofs. The
[factory Kit 2 follow-up](../../../docs/validation/2026-10-03-native-video-factory-kit2.md)
accepts the exact `6ed1dad4` image with those FPGA bytes through normal library
Play, including saved preferences, optional SGM and captured video/audio from
the named open diagnostics.

The first slice is a Coleco shell with a fixed 720p60 RGB888
pixel-clock interface. Choose a direct part or a simple scanline part;
an independently built Coleco bus 2.0 expansion can share the same frozen
shell. Select this raster variant of `build_fes_coleco_socket_v2.py` explicitly;
both raster and native parts use `build_video_part.py`. The standalone
`make build-fes-coleco` default still
builds the CPU-only socket.
The [fabric contract](../../mister-packages/docs/video-parts.md) describes
the public RTL ports; [the architecture](architecture.md#video-parts)
describes physical containment and composition.

From a clean committed `sources/misteross` tree:

```sh
make sim-fes-video-parts
make build-fes-coleco-video CACHE_ROOT=/absolute/cache
make build-fes-video-part VIDEO_VARIANT=direct \
  VIDEO_SHELL="$PWD/build/fes-coleco-video" \
  VIDEO_PACKAGE="$PWD/build/packages/<package-id>" CACHE_ROOT=/absolute/cache
make build-fes-video-part VIDEO_VARIANT=scanlines \
  VIDEO_SHELL="$PWD/build/fes-coleco-video" \
  VIDEO_PACKAGE="$PWD/build/packages/<package-id>" CACHE_ROOT=/absolute/cache
```

Use the exact package directory printed by the shell producer. The part
producer prints its archive beneath `build/video-parts/<variant>/<recipe>/`;
that directory also contains timing, clock and CRAM containment evidence.
Both parts bind the exact base package. Rebuilding the shell requires
rebuilding its parts and any selected CPU expansion.

FES keeps authenticated frozen shell and part evidence in its host cache,
separate from the ordinary core-package cache. Its exported
`core-video-parts/index.json` binds the exact package ID, profile, part ID,
archive path, size and digest. The installed tree contains only that index
and the two sealed archives per selected shell. A cache hit rechecks the
current source/tool/execution recipe, timing, clock ownership and actual RBF
containment; older source revisions are retained as original provenance.

For offline composition, run `expansion/cmd/fes-parts-link` with `-shell`,
`-package-id`, `-build-id`, `-video`, optional `-expansion`, and `-output`.
For a contained developer transfer, FogCast's `cmd/fes-parts` packages the
base and selected parts for the target's parts inspect/load routes. See
[FogCast development](../../FogCast/docs/DEVELOPMENT.md). An inspect does
not program hardware; a load follows the existing designated-kit lease and
physical lifecycle. FogCast's library video preference selects the installed
direct or scanline part for its exact package and can combine it with a matching
CPU expansion. Audio follows the shell's existing machine path.

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

## PC/XT CPU and ISA fabric development

`make sim-fes-pcxt` runs the [PC/XT transaction fabric](../cores/fes-pcxt/README.md)
through eight portable registered slot boundaries with diagnostic cards in slots
1 and 8. It tests wait-state side effects, memory and I/O decode, interrupt and
DMA wiring, conflict rejection, a lost commit acknowledgement, and reset during
transfers including a commit already pending at the card. It also executes an
original diagnostic ROM on the pinned MCL86 8088 through the fabric with variable
memory waits, interrupts, word I/O and fault recovery. GNU x86 `as` and `ld` are
required alongside Verilator. The motherboard lane tests PIC/PIT/PPI, XT
keyboard transport and speaker PCM; a second ROM receives real timer, ISA and
keyboard interrupts. The DMA lane checks 8237 register behavior, real ISA/RAM
transfers, refresh and LOCK/INTA arbitration; the ROM also programs both DMA
directions while refresh runs. Separate ISA CGA and MDA cards own VRAM,
register decode and independently timed RGBI guest rasters. The display
suite checks those rasters in host simulation; the display-machine suite
boots a diagnostic ROM that programs both cards through the motherboard.
The memory and memory-machine suites exercise the existing addon SDRAM
controller at 100 MHz, reset-only firmware loading, byte preservation, reset
cancellation, physical refresh and two 8088 ISA display boots without a second
firmware load. `synth_fes_pcxt.py --suite memory --cache-root /absolute/cache`
emits a diagnostic backend/controller netlist; capture/pads are external and
its DDR clock primitive remains unmapped.
The `system` and `system-mda` suites assemble CPU, motherboard, real SDRAM,
DMA/refresh and one Direct/Scanlines pipeline, using the original diagnostic
firmware copied through a gated boot loader. `make build-fes-pcxt
CACHE_ROOT=/absolute/cache` attempts one bounded locked HIP board route; add
`PCXT_BUILD_ARGS='--mda --scanlines'` for MDA Scanlines or `--synth-only` for a
frontend probe. Separate run directories under `build/diagnostic/fes-pcxt/`
retain firmware, source/tool receipts and synthesis/route evidence, including
failures. The board top supplies PLLs, capture registers, SDRAM pads and HDMI.
This is a diagnostic builder, not a package producer. Routed timing and SDRAM
pad qualification, media, HID keyboard translation, physical socket routing and
kit hardware acceptance remain open.

## Original RV32I

`make sim-fes-riscv` validates the original machine-mode RV32I CPU in
`cores/fes-common/rtl/riscv` against an independent instruction-level model,
then the `fes.riscv` system and board shell with the checked-in firmware.
The [CPU contract](../cores/fes-common/rtl/riscv/README.md) lists the bus,
CSRs, exceptions and tests; the [core README](../cores/fes-riscv/README.md)
lists the memory map, firmware and seal. Firmware is assembled by
`cores/fes-riscv/firmware/assemble.py`; regenerate the lane images after
editing `firmware.S`, because the producer rejects stale images.

## Original Z80

`make sim-fes-z80` validates original NMOS and documented fast CPU variants in
`cores/fes-common/rtl/z80`. This is a shared CPU development lane, not a play
package. The [CPU contract](../cores/fes-common/rtl/z80/README.md) lists the
interfaces, published behavior sources, tests and contained Cyclone V timing
diagnostic. SG-1000, ZX81 and the Spectrum models select the NMOS CPU. Each
full shell must pass its clock constraints before sealing; CPU-only timing
does not establish that.
Optimizer handoffs can pass `--max-aluts` to `scripts/build_fes_spectrum_oss.py`
with a per-design area budget. The cap is recorded in the functional build
identity and checked on the production synthesis before any route, then on
routed utilization before export. This option requires a full production build;
zero-build-ID synthesis does not qualify a handoff.

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

Each registered package authenticates the lock named by its `lock_path` in
`config/core-recipes.toml`. A lock can move to another nextpnr that
`scripts/pnr_recipe.py` classifies; that core then gets its own slot. Pong's
lock has moved to nextpnr `552ba246`, ZX81's to `1eecf877` and RISC-V's to
`8f453ebc`, and Coleco's pins Yosys `5391eeb1`, so each has its own slot.
Apple II, Atari ST, C64, Coleco and Spectrum have moved to nextpnr `5fe55f3d`
for its F0/F1 boundary `$ROUTETHRU` map; the Apple II, C64 and Spectrum locks
are identical and share one slot, and the Atari ST lock has its own. The other
five per-core lock files still contain the same bytes as `toolchain.lock` and
therefore share one cached compiler slot. Use the
matching `make toolchain-fes-<core>` target to provision a local toolchain.
The unregistered demos and splash continue to select the repository-level
`toolchain.lock`.

For those HIP play-package producers, the route log must name a live HIP
backend. A command line that merely contains `--router gpu` is not enough.
CPU-reference fallback is rejected. The splash producer is the exception:
it stays on the generic GPU-off toolchain.

SMS place-and-route is a first-pass search: it starts at seed 3 / HeAP 1000,
then the remaining `PLACER_SEEDS` and weight 300. Final structured `clk_sys`
and `pixel_clk` rows must meet 52 MHz and 74.25 MHz.

SG-1000's format-3 producer uses a bounded 20-candidate first-pass placement
search from a clean tree. It exports a
blank 16 KiB cartridge ROM and authenticated map for download-time linking.
The 2026-09-16 gap ladder records a HIP route of the earlier synth-only
netlist (`BUILD_ID` all zeros), not acceptance of the new sealed bitstream.
The recipe is registered in `config/core-recipes.toml`; the package installs
through the core library.

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

The Atari ST producer selects `cores/fes-atari-st/toolchain.lock`
(Yosys `3a5accbd`, nextpnr `9128800f`). The factory recipe stays HIP.
A CUDA route is a separate package. RAM Tester and menu use their own
per-core lock files, which currently select the same HIP compiler slot. The
130 MHz timing comparison uses the RAM Tester lock.
The [2026-10-04 nextpnr-head validation record](../../../docs/validation/2026-10-04-atari-st-nextpnr-head.md)
qualifies the earlier private pin and its read-only shell. It does not
qualify this shared-pin seal. The October kit notes do not accept these bytes.

The [Atari 520ST](../cores/fes-atari-st/README.md) is the first 16-bit core.
`make sim-fes-atari-st` covers the full CPU, MMU aliases, peripherals,
physical SDRAM commands, dual-clock video and complete disk upload.
`sim-fes-atari-st-emutos` accepts an external 192 KiB EmuTOS image; the
fetch target pins the official free ROM. Native producer qualification and
hardware acceptance remain distinct from these host checks. The factory
image selects the firmware shell and its Direct/Scanlines parts without bundling
EmuTOS or Atari ROMs.

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
   a producer-derived ROM map in format 3. ROM-less cores keep format 2.
   The factory Coleco recipe emits format 4: an exact 8 KiB `coleco-bios` and
   16 KiB `coleco-cart`.
   Currency and retirement
   are the FES [core alignment](../../../docs/core-alignment.md) contract.
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

- `fes.sms` is a supported library package. Historical parent pins and launch/Stop
  records do not accept HDMI audio on a bitstream built later. Kit HDMI-audio
  acceptance of the current tree is not recorded here.
- `fes.sg1000` is a supported library package. A historical launch/Stop note does not
  accept the current bitstream.
- Coleco audio is in the core (`make sim-fes-coleco-audio`, shared SN76489
  and 48 kHz I2S). It is not an unimplemented slice.
- Host simulation of a diagnostic ROM is not a photograph of HDMI and not a
  recording of HDMI audio.

Parent validation for a named package id is under FES `docs/validation/`.
Read the artifact ids in that record before treating it as evidence for the
tree you have open.

Compiler source-read auditing retains bounded traces: 128 MiB per process and
256 MiB per invocation, including child compilers. The larger allowance covers
RAM-heavy Atari ST synthesis and its ABC partitions. Overflow cancels the
private compiler process group; excluded-source and completion checks remain
required before package qualification.
