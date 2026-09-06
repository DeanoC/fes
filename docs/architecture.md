# Current build architecture

misteross turns small experiment RTL into local MiSTer RBF artifacts. Network
deployment and target lifecycle are outside this repository.

## Build lanes

```text
experiment RTL + constraints
  |-- sim ----> Verilator result
  |-- oss ----> Yosys -> nextpnr-mistral/Mistral -> top.rbf
  `-- oracle -> Quartus Prime Lite 17.0.2 -------> top.rbf

oss manifest + oracle manifest -> compare report
```

`sim` checks the experiment's logical behavior with Verilator. Simulation jobs,
production source lists, and OSS synthesis flags come from the closed experiment
policy. Simulation-only models never enter either synthesis lane.

`oss` uses only the pinned repository-local tools described by
`toolchain.lock`. nextpnr writes a compressed Cyclone V RBF
(`--compress-rbf`) so the FPGA manager can reach CONF_DONE. Generated
sources and tools live under `build/toolchain/`. Build output lives under
`build/oss/<experiment>/`.

`oracle` uses an explicitly configured Quartus Prime Lite 17.0.2 installation.
It uses the same production RTL and timing intent as the OSS lane. Output lives
under `build/oracle/<experiment>/`. Quartus is not an OSS or simulation
dependency.

`compare` reads the two lane manifests and writes its result under
`build/compare/<experiment>/`. Differences between compiler-produced RBF bytes
are expected; the comparison focuses on target, sources, resources, timing,
and successful artifact production.

## Experiments

`010_blinky` is a 50 MHz counter driving one LED. It is the smallest physical
output test.

`020_linux_mailbox` uses one Cyclone V HPS general-purpose interface to return
the constant message `OSS FPGA OK\n`. It has no external FPGA output. The
simulation substitutes a small HPS model; both synthesis lanes use the real
HPS primitive boundary.

`030_m10k_rom` walks an initialized 256-byte table on the 50 MHz clock and
drives one LED from stored bit 0. OSS synthesis maps the table to exactly one
M10K. PLL, DSP, MLAB, and HPS remain forbidden.

`040_mlab_ram` is a 32-by-8 writeable table on the HPS general-purpose
interface. Linux peeks and pokes GPO/GPI; there is no LED. The table is
marked `ramstyle = "mlab"`. Yosys maps it to eight `MISTRAL_MLAB` cells.
nextpnr packs those into LABs and does not report an MLAB utilization key,
so the closed policy counts the Yosys cells and requires the HPS primitive
in the route report. Quartus maps the same table to 256 MLAB bits and zero
M10K. PLL, DSP, and M10K remain forbidden.

`050_lut_mul` is an eight-by-eight unsigned product on the HPS
general-purpose interface. Linux peeks and pokes GPO/GPI; there is no LED.
The product is marked `multstyle = "logic"` so both lanes keep it in ALMs.
OSS synthesis keeps `-nodsp`; Yosys must not emit `MISTRAL_MUL*` cells.
Quartus must measure zero DSP blocks. PLL, M10K, and MLAB remain forbidden.

`060_dsp_mul` is an eight-by-eight unsigned product on the HPS
general-purpose interface. Linux peeks and pokes GPO/GPI; there is no LED.
The product is marked `multstyle = "dsp"`. OSS synthesis drops `-nodsp` and
emits one `MISTRAL_MUL9X9`. nextpnr-mistral has no DSP BELs for
`5CSEBA6U23I7`, so the OSS lane cannot place this experiment. Quartus maps
the same product to one DSP block. PLL, M10K, and MLAB remain forbidden.

## Standalone Pong game

`cores/pong/rtl/pong_game.sv` implements a deterministic 320x240 game module.
It is not a programmable MiSTer core: board timing, the HPS interface, HDMI
and audio transport require a separate compatible wrapper.

One player moves the left paddle with Up/Down (opposing inputs cancel), against
an opponent moving at most one pixel per frame. Paddle impact position determines
upward/downward return, with slower center shots and faster edge shots. Start
begins a rally on a fresh press; a point returns the ball to its center serve position and waits for
another press. Decimal scores wrap after nine. Reset clears scores and restores
the same positions and initial left/down trajectory every time.

The synchronous interface takes `clk`, `reset`, `frame_tick`, `up`, `down`,
`start` and 10-bit `pixel_x`/`pixel_y`. A one-clock `frame_tick` advances game
state; raster coordinates select RGB pixels independently. Outputs are 8-bit
`red`/`green`/`blue`, `tone`, `playing`, integer top-left positions
`ball_x`/`ball_y`/`player_y`/`ai_y`, and 4-bit decimal scores
`player_score`/`ai_score`. Paddles are 4x32 at x=12 and x=304; the ball is 4x4.
Out-of-range raster coordinates produce black. Seven-segment score glyphs
appear above the playfield. `CLOCK_HZ` sets a 1 kHz square-wave collision/score
tone lasting approximately 100 ms; set it to the wrapper's actual game clock
(at least 2 kHz). The tone is a logic signal, not an audio device interface.

Run `make sim-pong VERILATOR=/absolute/path/to/existing/verilator` (or omit
the override when Verilator is on PATH). This uses the installed compiler,
never bootstraps a toolchain, and writes `build/sim/pong/` and
`build/sim/pong-video/`. Its real RTL harness checks reset, input bounds,
frame gating, start/restart, wall and paddle
bounces, both scoring sides, pixels and tone expiry. The test uses an 8 kHz
clock parameter to exercise sound cheaply. The same target tests the separate
`pong_video.sv` raster: 320x240 active pixels, 424x262 totals, and a pixel enable
every three cycles of a 20 MHz clock (approximately 60.01 Hz). That module
supplies coordinates, syncs, active-video indication and one frame tick. Neither
module includes the board/HPS wrapper. Simulation is not hardware evidence.

## Pong MiSTer wrapper and Quartus build

`cores/pong/Pong.sv` connects the game/raster to the framework selected in
`cores/pong/framework.toml`: Template_MiSTer revision
`3ea1134cf05d62c2b1db30362277a823d739ced2`. This is a source-only framework pin,
not a fabricated upstream game/release-RBF pin. `sys/` is staged unchanged,
including the board pins, HPS I/O and video/audio infrastructure. Its existing
PLL supplies the 20 MHz game/video clock.

The wrapper reports core identity `Pong` and requests no media. The low word of
MiSTer joystick command `0x02` drives Up bit 3 (`0x0008`), Down bit 2 (`0x0004`)
and Start bit 7 (`0x0080`). Status bit 0, the framework reset input, or its user
reset button resets the game; normal profile reset words are assert/initial
`0x0001`, release `0x0000`. Raster synchronization continues through game reset.
The game tone drives identical signed left/right samples (zero while silent).
The native runtime remains responsible for enabling video/audio and lifecycle.

```sh
make stage-pong
QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0 make build-pong
```

Both commands accept `PONG_FRAMEWORK=/absolute/path/to/clean/template-checkout`
to reuse an existing checkout at the pin. Otherwise they clone into
`build/frameworks/template/`. They reject a dirty or wrong-revision checkout.
Staging archives the pinned Git tree, overlays the local Pong files, and fixes
the build date to the value in `framework.toml` through a project-level pre-flow
hook, without changing `sys/build_id.tcl`. It recreates only
`build/rebuild/pong/project/`; do not edit that generated tree.

`build/rebuild/pong/inputs.json` records hashes of local RTL and build helpers,
the framework revision, and every staged source file. A successful Quartus run
publishes `pong.rbf` and `build.json` there, with artifact size/hash, compiler
version, command and the input-record hash. `quartus.log` records compiler
diagnostics. A stage-only or failed compile produces no new success receipt.
The command uses an explicitly configured installed Quartus 17.0.2; it does not
bootstrap tools, deploy, or program hardware. Repeatable input staging is not
a claim of independently reproduced RBF bytes. Hardware acceptance is separate.

The 2026-09-06 local wrapper build completed full Quartus 17.0.2 Lite
compilation in 2m46s with 0 errors and 56 warnings. The resulting `pong.rbf`
is 2,437,696 bytes, SHA-256
`1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1`.
Its input record SHA-256 is
`63624330d39067eaa46a266225918c0effdd0924ac830bed97a11d94b46648bb`.
Those records identify the exact local sources used at build time. Parent
component selection and image acceptance are tracked separately by FES.

`project/output_files/Pong.fit.summary` reports 7,803/41,910 ALMs.
`project/output_files/Pong.sta.summary` reports positive slack for every listed
setup/hold/recovery/removal/pulse-width domain; worst setup is 0.399 ns and
worst hold is 0.247 ns. Warnings include inherited framework connectivity,
unused timing filters and PLL lock outputs, plus score-width narrowing in
the game. These reports do not establish physical output or input behavior.

Post-build hashing found all 57 staged `sys/` files unchanged and no local
source drift. Quartus changed only the staged `Pong.qsf` input, replacing its
`LAST_QUARTUS_VERSION` metadata from `17.0.2 Standard Edition` to
`17.0.2 Lite Edition`. The input record deliberately retains the original
staging hash. No second independent RBF build has been run. The diagnostic
image subsequently passed native Pong gameplay, Up/Down/Start controls, HDMI
audio, Stop/relaunch and switching with Mega Drive and SNES without rebooting.

## Artifact boundary

The integration outputs are:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
build/cores/<name>/releases/*.rbf
build/rebuild/<name>/<name>.rbf
build/current/<name>.rbf
build/bundles/megadrive/<rbf-sha256>/megadrive.rbf
build/bundles/megadrive/<rbf-sha256>/megadrive-rbf.toml
```

The core workflow has three separate operations:

```sh
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
make select-core CORE=megadrive
make export-core-bundle CORE=megadrive
```

Rebuild compiles the pinned source. Select copies an operator-chosen artifact
to the mutable `build/current/megadrive.rbf` convenience path. Export validates
the rebuild against its closed comparison evidence and writes an immutable,
content-addressed directory containing exactly `megadrive.rbf` and
`megadrive-rbf.toml`. FogCast receives the exporter's printed bundle path under
`build/bundles/megadrive/<rbf-sha256>/`, rather than a mutable rebuild or
selection path. No attestation record, run ID, recovery journal, or
fault-injection result is required.

Building an RBF never touches hardware. Ordinary native bring-up claims the
kit with `scripts/kit.py` and streams the RBF through the target
`POST /v1/development/rbf` path (`load_development_rbf`) under a held lease.
The FogCast host uses the same lease for launches and
`POST /api/v1/session/development-rbf`. That path programs the FPGA manager,
then probes MiSTer SPI identity on the same FPGA-manager GPO/GPI pair the HPS
general-purpose experiments use. A non-MiSTer image does not satisfy the
probe; Stop restores idle with the existing development reboot handshake.
`make program` remains a separate Main-FIFO or JTAG diagnostic outside this
protection and is not the native kit path.

## Pinned core trees

The lock also selects SNES revision `93d359e6f23c734ae3928984e88bed1d9b53cbac`
and its hashed upstream `SNES_20260823.rbf`, copied from mister-packages.
`make fetch-core CORE=snes` uses the existing named-core fetch lane. The local
SNES seed-1 rebuild completed compilation but failed timing. A separately staged
seed-3 diagnostic passed all timing checks (minimum setup 0.240 ns, hold
0.243 ns) and native LoROM/HiROM gameplay, controls, HDMI audio and switching
checks in FES. Its 4,440,332-byte RBF SHA-256 is
`fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226`.
The diagnostic changed only the staged QSF seed; the normal rebuild recipe
retains seed 1 and does not reproduce that artifact. Bundle export still
accepts only Mega Drive.

`cores.lock` is the upstream version pin: git identity plus the official
release RBF hash. `make fetch-core` checks out that exact commit under
`build/cores/<name>/` and hashes the official RBF. That hash check is the
lock test. It does not clone `HEAD`, does not reset dirty trees, and does
not run Quartus.

`make rebuild-core` copies the fetched tree into
`build/rebuild/<name>/project/` (excluding `.git` and prior compile
artifacts) and runs Quartus Prime Lite 17.0.2 `quartus_sh --flow compile`
on the locked project. The staged copy pins `BUILD_DATE` to the YYMMDD
from the locked release name (`MegaDrive_20260603.rbf` → `260603`) via
`MISTER_BUILD_DATE`; override with `--build-date`. The fetch checkout is
not modified. The produced RBF is copied to
`build/rebuild/<name>/<name>.rbf`. Its hash is recorded next to the
upstream hash; they are not required to match. Quartus is never taken from
`PATH`; `QUARTUS_ROOTDIR` is required.

The Mega Drive Lite rebuild has been loaded on real MiSTer hardware, so
fetch → Quartus 17.0.2 → RBF is a working path. Upstream remains the
fallback if a later rebuild is broken.

`make select-core` copies the rebuild to `build/current/<name>.rbf`.
`ARTIFACT=upstream` falls back to the official release. This selection is for
operator use and is not the FogCast release handoff.

`make export-core-bundle CORE=megadrive` accepts only the pinned Mega Drive
revision and the MiSTer ABI. It rehashes the rebuild and recipe, validates the
closed `compare.json`, writes the two-file bundle under its RBF digest, removes
all write bits from the files and directory, and prints the absolute bundle
path. FogCast owns which exported RBF is installed on a target.

## Shared native kit client

`scripts/kit.py` is a thin operator client of FogCast's target lease and native
RBF upload APIs. It retains one in-memory lease during an interactive session,
renews every 20 seconds, streams regular RBF files with an explicit bounded
length (1 byte–32 MiB), and releases on exit. A development-RBF `CORE_TIMEOUT`
after programming keeps that lease so the operator can inspect a non-MiSTer
image before Stop. It stores no credentials or lease database. FogCast remains
authoritative for expiry, takeover, serialization and cleanup; libmister-runtime
performs the physical transition. See the README's shared-kit commands. Direct
`make program` remains a maintenance bypass outside this protection, and
compilation never acquires a lease.
