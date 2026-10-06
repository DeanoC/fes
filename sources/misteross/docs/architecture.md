# Current build architecture

misteross has two jobs. [OSS place-and-route testing](oss-pnr.md) proves
Cyclone V primitives and freeze-scaffold carts with Yosys, nextpnr-mistral and
Mistral. [Cores](cores.md) changes or adds a described FES package, or the
board-firmware splash. This file is the build contract for both: identity,
lanes, compilers and package bytes. It is not a task list and not the
experiment-by-experiment record. That record is the
[OSS experiment catalog](oss-experiments.md). Files under `validation/` record
a date. They are not current instructions.

Network deployment and target lifecycle are outside this module. FogCast owns
selection and transfer. libmister-runtime programs the FPGA.

## Functional input identity

Pong, ZX81, Coleco, SMS, SG-1000, Catch and all demo variants use functional
identity 2 exclusively. Both Python and CLI entrypoints default to 2 and reject
other identity versions. Quartus oracle and board-firmware records retain their
current evidence schema. Rebuilt script closures receive new identities;
existing artifacts and caches are never relabelled or deleted.
ZX81, SMS and SG-1000 packages use format 3 to seal their ROM maps; other producers keep
format 2. The external `build-inputs.json` record gains format 2, while the exporter still
reads format 1 with its original full-record SHA256 correlation algorithm.

The package exporter and inspector also recognize format 4 with two ordered ROM
requirements and one sealed map. The Coleco development producer will select
this format when its separate blank shell and map are available; existing
registered recipes still emit their current formats.

Record 2 keeps repository/revision and `source_path` as original build provenance, but derives
its embedded 128-bit ID from a domain-separated functional projection excluding
those provenance fields. It includes every tracked regular file under `scripts/` and the owning core/board
modules of each declared input, including shared RTL, root or core compiler locks,
the recipe/ABI digests, authenticated
tool identities, routing options and controlled execution identity. This is a
conservative module closure: unrelated root documentation does not invalidate
it, but another producer helper under `scripts/` does. New records set `parameters.source_closure_policy = "compiler-markdown-v1"`.
This policy excludes tracked non-executable `.md`/`.markdown` files, including
new documentation, while retaining executable Markdown and every other file
regardless of a `docs/` directory name. Recipe, ABI and explicit producer inputs
must remain in the closure; declaring an excluded Markdown input fails closed.
Both working-tree and historical Git-object validation apply the recorded
policy. Existing format-1 and format-2 records without that parameter retain
their original semantics, including documentation in declared roots.

The functional producers trace synthesis and each route attempt with
`/usr/bin/strace --kill-on-exit -ff -yy`. The tracer and its libraries enter
execution identity. Successful source Markdown opens for reading (including
O_PATH) fail before sealing; output-only writes are distinguished. Trace size,
completion, pathname decoding and unsupported open mechanisms fail closed.
A forbidden read is fatal across placement search. A timed-out compiler group
is terminated; only a complete, valid and benign audit retains the existing
seed-timeout fallback. Ordinary parent Python `open`/`Path` reads are guarded
inside functional build/record contexts, including search worker threads; the
guard is inactive afterward. Support bytes explicitly hashed into execution
evidence are read through a narrow capture helper. This is not an adversarial
sandbox for native extensions or raw system calls. Successful-open tracing
also does not prove that arbitrary future code ignores Markdown metadata or
failed-open results; producer changes that depend on documentation in those
ways must revise the policy before reuse is qualified.

Root docs and unrelated UI files remain outside the source closure. Export
repeats closure enumeration and rejects missing, untracked, symlink or changed
functional inputs.
The producer requires a clean committed module before and after building.
Live producers require the exact tracked `sources/misteross` module.
Historical record readers preserve original source paths. The revision and origin remain those of
the real root commit, not a synthetic child commit. Export resolves that module
below the Git root and scopes source cleanliness to it; unrelated sibling work
is preserved. Module relocation alone does not enter the functional projection.

The functional lane supplies an explicit environment to synthesis and routing,
using an empty private home instead of user configuration and omitting ambient
loader, Yosys and GPU overrides. It records one explicit GPU device (default 0),
KFD topology, kernel, executable bytes (including the Yosys ABC9 helper
`yosys-abc`), dynamic-library bytes and installed tool support data. Missing
ABC or required Intel ALM support files reject the functional lane.
Only a single routing device is supported in this lane; use
`--identity-version 2 --gpu-devices 0` for Coleco/ZX81, optionally with
`--best-fmax`, or `--identity-version 2 --gpu-device 0` for Pong/SMS/SG-1000. Execution
inputs are rechecked before sealing and their full description is retained in
`build-summary.json`; the canonical record includes their digest. Installation
paths remain part of this conservative identity, so tool relocation does not
silently reuse hardware qualification. This is an input identity, not a promise
of identical placement bytes across runs. Authentication probes remain the
existing toolchain checks; actual compiler invocations use the controlled env.

A same-input later commit may have the same embedded ID but different original
provenance and package ID. This producer does not relabel existing manifests or
implement parent cache reuse. FES must separately authenticate the original
artifact against its original source and publish a current selection receipt
before reusing it. `verify_record_source_at_revision(root, record)` reads the
recorded commit's exact module closure from Git objects without changing HEAD,
the index or working files; absent history and altered member sets fail closed.
It does not substitute for current tool authentication or payload verification.
Existing FES whole-record matching continues to distinguish
commits until that coordinated change lands. No new hardware acceptance is
claimed by these host-side identity and exporter tests.

## Build lanes

### Composable application reference

`fes.catch` is an original ROM-less application using the same shell and ABI.
`fes_catch_game.v` advances paddle/target/score/lives state only on the shared
video frame tick; `fes_catch_core.v` supplies the shared raster. A synchronized
event toggle triggers a bounded stereo chime in `fes_catch_audio.v`, which
uses `fes_audio_i2s.v` and the existing audio PLL. The Catch producer reuses
the demo's board command/electrical checks and seals a functional-identity v2
package. `make sim-fes-demo` covers gameplay, event audio and board mailbox
integration. It adds no host/runtime core allowlist or factory package.

`cores/fes-common/rtl/fes_application_gp.v` implements the new
`fes.application` 1.0 mailbox (ABI tag 3). It preserves the existing GPO/GPI
framing but does not present a keyboard or Pong-specific persistence service.
Its checked-in `generated/fes_application.vh` and `generated/exchanges.json`
are unedited mister-packages outputs. Existing simple-game and simple-computer
packages keep their own ABI and behavior.

Video is mandatory for application v1. Parameters `ENABLE_GAMEPAD` and
`ENABLE_MEDIA` independently enable the eight-button, single-controller
gamepad and 1–16384-byte blob endpoints. Absent endpoint opcodes reject with
invalid-opcode. Optional `ENABLE_MEDIA_STREAM=1` requires `ENABLE_MEDIA=1`
and implements blob-stream 1.0 with a 32 KiB endpoint limit and IEEE CRC32.
Default applications still reject stream opcodes. Stream-enabled applications
receive fifteen-bit `media_write_addr` and sixteen-bit `media_size`; default
widths remain fourteen and fifteen bits. Accepted byte pairs still target address
and address+1, low byte first, including odd starting addresses. Successful
mutations return zero.

`ENABLE_CONTROLLER_PORTS` instead enables `fes.gamepad.ports` 1.0: exactly
two eight-button logical ports, mutually exclusive with `ENABLE_GAMEPAD`.
`ENABLE_KEYPAD_PORTS` additionally enables `fes.keypad.ports` 1.0 and requires
controller ports. Opcodes 13/14 publish full active-high button/keypad states
at index 0 or 1. Keypad bits 0..11 mean 0..9, *, #. Invalid ports, reserved bits
and absent interfaces reject without state changes. HOLD clears both ports.
`controller_buttons[7:0]` / `[15:8]` and `controller_keypad[11:0]` / `[23:12]`
carry player 1 / player 2 in the endpoint clock domain. Reuse these vectors
directly in custom applications; adapt button semantics only at the core edge.
Zero states release a disconnected input source; hardware does not track devices.

The endpoint starts held in reset with neutral buttons. Media BEGIN requires
held execution and no open transfer, clears readiness and starts replacement.
Pair writes are low byte first; the tail command accepts only the final odd
byte. HOLD neutralizes buttons and preserves staged or committed media.
RELEASE rejects until any required media has committed completely. Buttons
and identity requests may interleave with the transfer without altering its
state. The endpoint emits accepted byte writes at `media_write_addr`, with
`media_write_enable[0]` for the low byte and `[1]` for the following high byte;
applications own their storage. There is no implicit 16 KiB RAM allocation.
The first three bytes are also cached for small asset consumers and cleared
at each BEGIN. A consumer must honor execution hold/readiness while storage
contains an incomplete replacement.

`cores/fes-common/rtl/fes_video_720p.v` provides the proven fixed 1650×750
raster and centered 3× 320×240 image mapping derived from the Pong shell.
`cores/fes-demo/rtl/top.v` connects the application endpoint, HPS GP, fractional
74.25 MHz PLL, HDMI RGB and open-drain HPS I2C bridge. The common endpoint and
reference image operate in the same pixel domain; application reset does not
stop video timing. Pong now uses this same video module; its timing and image
mapping are unchanged, and its legacy mailbox remains separate.

The same source produces three independently identified packages:

| Command | Core ID | Required interfaces | Output |
| --- | --- | --- | --- |
| `make build-fes-demo` | `fes.demo` | fixed 720p video | `build/fes-demo/core.rbf` |
| `make build-fes-demo-media` | `fes.demo-media` | fixed 720p video, gamepad, blob | `build/fes-demo-media/core.rbf` |
| `make build-fes-demo-audio` | `fes.demo-audio` | fixed 720p video, gamepad, stereo audio | `build/fes-demo-audio/core.rbf` |

The first animates without input or media. The second requires an asset before
release, uses its first three bytes as RGB channel masks, and uses Left/Right
to reverse/increase animation speed. Missing palette bytes are zero, and bytes
after the first three are accepted but unused by this reference application.
A white palette can be created with
`python3 -c 'from pathlib import Path; Path("palette.rgb").write_bytes(bytes([255,255,255]))'`.
This demonstrates composition without a new emulated-machine implementation
or an application-name branch in host software.

`fes.ramtest` is a separate utility on the same mailbox with fixed 720p, the
gamepad and `fes.memory.hps-ddr` 1.0. Its SDRAM channel first runs a bounded
byte-preservation preflight with retained BE/payload/readback failures; only
a passing preflight releases the six full-span patterns. The addon shares chip
DQML/DQMH with row-address pins A11/A12; the controller keeps the full row for
ACTIVATE and drives masks on those pins during column setup and WRITE.
Simulations use that physical wiring, including high rows. After execution
release it tests the SDRAM addon and all three HPS DDR ports at the memory clock,
100 or 130 MHz. The DDR ports scan the whole core window `0x30000000-0x3fffffff`
together with seven patterns and report errors, failing bits and MB/s per port.
The SDRAM clock pin is the inverted DDR output used by MiSTer controllers. Both
OSS rates sample the bidirectional DQ pads with phase-shifted fabric registers
because the pinned OSS packer cannot put DDR input registers on those pads.
`make build-fes-ramtest-100` and `make build-fes-ramtest-130` seal packages into
`build/fes-ramtest-100/` and `build/fes-ramtest-130/` with the shared
`toolchain.lock` HIP compiler slot, whose Yosys declares every fpga2sdram port. Their
timing gate covers the memory, capture and video domains, and the recipe checks
the synthesized fpga2sdram layout constants. `make build-fes-ramtest-quartus`
compiles the same RTL with Quartus 17.0.2 at 130 MHz; `RAMTEST_MHZ=100` selects
a separate 100 MHz diagnostic. A gamepad button, or a keyboard key the host
maps to one, stops the scans. The ABI has no memory opcode. Hardware results
are recorded in the [core README](../cores/fes-ramtest/README.md).
`make diagnose-fes-ramtest-timing` selects a separate, opt-in compiler lock and
compares a baseline route with report-guided LUT remapping on the same
synthesis. It retains the remap only when final memory timing improves without
regressing the other clocks or reporting hold violations. Both routes remain
host-only diagnostics; neither RBF is sealed or exported as a package.

`cores/fes-common/rtl/fes_hps_ddr.v` is the shared `fes.memory.hps-ddr` port
module: the fpga2sdram cell in the generated layout (a 128-bit port and two
64-bit Avalon-MM ports) with a registered guard per port
(`fes_hps_ddr_guard.v`). The core's execution reset holds the ports; the guard
then finishes a write burst the core started, with byte enables cleared, and
hides read data issued before the hold, because the controller cannot recover
a burst stopped midway. The endpoint parameter `ENABLE_HPS_DDR` advertises
capability bit 8. The runtime releases the FPGA ports only after identity and
the SDR mirror registers prove the layout.

`scripts/build_fes_demo.py` reuses the existing board tool-authentication,
timing/resource and HDMI electrical checks from `build_fes_pong.py`. It records
its own recipe, ABI definition and variant parameters in the build identity,
pins the helper source, and includes the full input digests in build evidence.
`CACHE_ROOT=/absolute/cache` selects the same root-lock HIP slot as Pong.
Failed checks invalidate candidate artifacts; clean committed tracked source
is required before synthesis and again before export. It produces ordinary
format-2 packages under `build/packages/` after those checks, and never programs
hardware. Parent image selection is a separate integrator action.

`make sim-fes-demo` checks eight capability combinations, shared wire
fixtures, interleaved input/identity/HOLD during blob loading, invalid command
isolation, full 16 KiB transfer and short replacement. It also checks three
full video frames and both production tops with controllable board models:
pixel-clock isolation, I2C low-or-release/feedback, palette output and controller
effect. These digital models do not prove PLL lock, electrical timing or actual
HDMI output. This implementation has simulation and producer-unit-test evidence;
new routed artifacts and exact-artifact hardware acceptance remain outstanding.

### Board-firmware splash

`cores/fes-splash/` is the U-Boot / intended Stop-idle splash bitstream: a
FogCast/FES mark plus autonomous motion on HDMI. It is board firmware. It is
not rooms, not attract ABI, and not a format-2 play package. FES pins the
tracked `sealed/fes-splash.rbf` in `image/build/native-inputs.toml`. Work on
this recipe in FES `sources/misteross`; standalone `DeanoC/misteross` is
retired for day-to-day work.

Video is CTA-770.3 1280×720p60 at the checked 50→74.25 MHz fractional PLL,
full-frame (the play-shell 320×240 mapping is not used). Linux programs the
ADV7513 through the HPS I2C bridge at X52/Y60. The core has no HPS GP mailbox
and no MiSTer user-io: command `0x0014` Probe has no responder, and command
`0x002f` HPS framebuffer is absent. There is no observed core-ID string.
U-Boot does not Probe. FES IdleRecipe must omit Probe and HPS fb.

The splash also carries an idle fpga2sdram cell driving the
`fes.memory.hps-ddr` layout. U-Boot's `bridge enable` writes
`staticcfg.applycfg` with this bitstream loaded, and the SDR controller keeps
that layout until the next boot; no later core can change it from Linux. The
recipe rejects a netlist whose cell carries other `cfg_*` values or a driven
command input.

`make sim-fes-splash` checks three full 1650×750 frames, the mark, motion, and
the I2C low-or-release board path. It writes `build/sim/fes-splash-frames/`
for visual inspection. `make build-fes-splash` (`scripts/build_fes_splash.py`)
is the generic OSS Yosys/nextpnr-mistral lane (GPU-router OFF, default
router, no HIP). It reuses the Pong PLL wrapper and the shared 50 MHz SDC,
keeps splash-owned HDMI/I2C QSF, and writes:

```text
build/fes-splash/core.rbf
build/fes-splash/build-inputs.json
build/fes-splash/build-summary.json
build/fes-splash/native-inputs-snippet.toml
sealed/fes-splash.rbf
```

The tracked `sealed/` copy is the FES `[splash_rbf]` / `[idle_rbf]` pin.
The same bytes are the boot core, FAT `/idle.rbf` loaded by the FES U-Boot
(`core=idle.rbf`), and the Stop idle core. A sealed RBF requires a clean committed
tree and `make toolchain`. `--synth-only` is a dirty-tree Yosys probe.
Quartus is not part of this recipe. No build command programs hardware.

To author another application, reuse the endpoint and fixed-video shell,
implement its image/asset consumer, declare only implemented interfaces, and
add a producer with tracked source closure and an exact manifest. Keep board
control in the runtime, package schemas in mister-packages, and library/session
context in FogCast. Shared RTL retains GPL-2.0-or-later notices from its source.
Multiple controllers, alternative timings and durable data are future contract
work. The autonomous and palette variants remain silent and keep their existing
single-PLL configuration and board ports.

### Shared application audio

Coleco also declares `fes.audio.pcm-s16-stereo-48k` and capability bit 4.
Its shared `fes_sn76489.sv` consumes a one-cycle write strobe, chip-clock enable
and data byte; console address decoding stays in `coleco_machine.sv`. Three
10-bit tone dividers and a TI 15-bit noise register feed a signed 16-bit mono
mix with 2 dB attenuation steps, duplicated into stereo. The programmable
interface follows the [TI SN76489AN data sheet](https://map.grauw.nl/resources/sound/texas_instruments_sn76489an.pdf).
The level table and latch/data approach reuse the earlier SMS implementation
from misteross commit `6f56a8f`; Coleco uses TI SN76489A noise feedback/output
delay and a period-zero reload of 1024, following the hardware-verified
[MAME chip implementation](https://github.com/mamedev/mame/blob/master/src/devices/sound/sn76496.cpp).
A fractional enable produces an
average 3,579,545 Hz chip clock from the 52.224 MHz system clock. This preserves
audio pitch independently of the reduced machine's CPU/video cadence.

`fes_audio_output.v` connects system-domain signed stereo samples to the
existing audio-domain serializer. A request/acknowledge handshake captures and
holds both words together; only handshake bits pass through synchronizers.
The source clock must continue running. One transfer is requested per stereo
frame; the completed snapshot becomes a later output frame (bounded latency,
not an audio FIFO). HOLD synchronizes into the audio domain and substitutes
zero, while PLL unlock gates data immediately and resets framing. Transfer
state survives lock loss to avoid interpreting a stale acknowledgement as a
new sample. The custom tone demo remains a native audio-clock source and needs
no CDC. V11 reaches two qualified PLL sites, so Coleco uses a shared fractional
417.792 MHz VCO for system 52.224 MHz (C8) and audio 12.288 MHz (C34), plus
the separate video PLL. The shared `fes_z80_ce.sv` supplies Coleco, SG-1000
and SMS with alternating CPU half-cycle enables at an exact average
3,579,545 Hz, independently of the 52.224 MHz transport and reset. Its phase
error stays below one system clock.
The TMS9918 logical raster now uses its independent fractional 60 Hz enable.
HDMI video timing stays 74.25 MHz and audio stays 48 kHz.
The producer checks all three timing domains and each audio output pad before
packaging. SG-1000 and SMS use the same shared 52.224/12.288 MHz
system/audio PLL and coherent PCM-to-I2S crossing.

`make sim-fes-coleco-audio` checks tone periods, attenuation, noise, coherent
asynchronous stereo transfer, serial padding, hold and lock loss. The existing
machine tests execute Z80 PSG OUT instructions in both memory timing models.
These are host simulations, not proof of audible HDMI output on a receiver.

The audio variant declares required `fes.audio.pcm-s16-stereo-48k` 1.0 and
advertises application capability bit 4. It uses the existing execution hold
and gamepad commands; no audio mailbox opcode or host sample transport exists.
The producer's `FES_DEMO_AUDIO` define includes the four physical audio ports
and second PLL only for this variant. Media is not required or advertised.

`cores/fes-common/rtl/fes_audio_i2s.v` consumes signed 16-bit stereo PCM in its
12.288 MHz audio domain. It emits 3.072 MHz BCLK, 48 kHz LRCLK, 32-bit slots,
MSB-first data one BCLK after each LRCLK transition, and zero padding. LRCLK
low denotes left. `sample_tick` is high in the final MCLK cycle of a stereo
frame; at the next rising MCLK edge both input samples are captured together
for the next frame. A consumer advances its samples on that edge for the
following tick. Inputs from other domains need their own coherent CDC.

`fes_demo_audio.v` generates left 1000 Hz and right 500 Hz square waves at
signed amplitude 4096. Right doubles both frequencies. Hold and the used
controller bit cross from the pixel domain through two registers. Hold replaces
both samples with zero by the next stereo frame after synchronization while
MCLK/BCLK/LRCLK continue. PLL unlock asynchronously resets the framing and gates
serial data low even if the clock stops; release waits for two new audio edges.
This digital behavior does not establish the HDMI receiver's analog silence or
clock-loss holdover latency. Runtime transmitter mute remains its own lifecycle
responsibility.

`fes_audio_pll.v` uses the checked fractional 50-to-12.288 MHz profile. The demo
combines it with the separate 74.25 MHz video PLL. `constraints-audio.qsf`
preserves the Pong video/I2C pins and adds MCLK U11, BCLK T12, LRCLK T11 and
I2S data T13 at 3.3-V LVTTL, matching the authoritative shared board definition
and [MiSTer board mapping](https://github.com/MiSTer-devel/Template_MiSTer/blob/3ea1134cf05d62c2b1db30362277a823d739ced2/sys/sys.tcl).

The producer explicitly validates both PLL parameter sets, routed clock
evidence, both Fmax domains and every audio output pad's pin and electrical
standard. The original single-PLL validator remains the default. Source
closure includes the audio RTL and constraints. `make sim-fes-demo` decodes
actual serialized sample pairs, checks atomic updates, padding, clock ratios,
mute and stopped-clock reset; it measures both tone frequencies before and
after controller input and simulates the complete board with independent
pixel/audio clocks and real GP commands. The audio extension currently has
simulation and producer-test evidence; combined HIP routing and hardware
audio capture remain outstanding.

### Experiment lanes

```text
experiment RTL + constraints
  |-- sim ----> Verilator result
  `-- oss ----> Yosys -> nextpnr-mistral/Mistral -> top.rbf
```

`sim` checks the experiment's logical behavior with Verilator. Simulation jobs,
production source lists, and OSS synthesis flags come from the closed experiment
policy. Simulation-only models never enter synthesis.

`oss` uses only the pinned repository-local tools described by
`toolchain.lock` (core recipes may select a tracked qualified compatibility
lock and isolated toolchain root). nextpnr writes a compressed Cyclone V RBF
(`--compress-rbf`) so the FPGA manager can reach CONF_DONE. Generated
sources and tools live under `build/toolchain/`. Build output lives under
`build/oss/<experiment>/`.

Primitive experiments have no Quartus project. A described core may still
offer `make build-fes-*-quartus` as a manual development check. That recipe
is not the product bitstream and is not a fallback when the HIP seal fails.

## Shared compiler installation

FES Python Pong, ZX81, Coleco, SG-1000, and SMS recipes select a shared compiler
only through an explicit `--cache-root` argument or the Make `CACHE_ROOT=`
variable on `build-fes-pong`, `build-fes-zx81`, `build-fes-coleco`,
`build-fes-sg1000`, and `build-fes-sms`. Ambient `FES_TOOLCHAIN_CACHE_ROOT` is
not a policy selector for those producers. HIP (`gpu-router=HIP`,
`hip-architectures=gfx1100;gfx1201`) is the standard FES nextpnr lane:
nextpnr commands include `--router gpu`, build records store that HIP
configuration, and route evidence must name a live HIP backend rather than a
CPU-reference fallback. Pong uses the repository-wide `toolchain.lock` HIP
slot; the standard ZX81 socket selects `toolchains/zx81-expansion.lock`.
SG-1000 selects `toolchains/registered-memory.lock` (Yosys `e2d425de`,
nextpnr `a93fe013`, Mistral `7ed06e21`). SMS selects
`toolchains/fes-sms.lock` (Yosys `e2d425de`, nextpnr `a93fe013`, Mistral
`7ed06e21`), which keeps its own synthesis notes and ROM digests. Those two locks are different bytes and different HIP cache slots.
Local HIP tools
come from `make toolchain-fes` for Pong, `make toolchain-fes-zx81` for the
standard ZX81 socket, `make toolchain-fes-coleco` for
Coleco, `make toolchain-fes-sg1000` for SG-1000, and `make toolchain-fes-sms`
for SMS. `make toolchain` remains GPU-router OFF for generic OSS experiments.
`make toolchain` and `make toolchain-fes` share `build/toolchain`; the last
one run wins. `CACHE_ROOT=… make …` and `make … CACHE_ROOT=…` are both valid
shared-cache forms; Make clears `MAKEFLAGS`/`MFLAGS` for those producer
recipes. Omitting `--cache-root` / `CACHE_ROOT` preserves that local HIP
install. Quartus ZX81/Coleco/SG-1000/SMS recipes are oracle-only and are not a
nextpnr fallback. For a shared cache, the same CACHE_ROOT=/absolute/cache
spelling selects the FES HIP toolchain for toolchain-fes,
toolchain-fes-coleco, toolchain-fes-sg1000, and toolchain-fes-sms; doctor and
doctor-strict use that same selection. An explicit FES_TOOLCHAIN_CACHE_ROOT
remains accepted and wins when it agrees with CACHE_ROOT.

Version 1 supports one user on one Linux x86-64 glibc host. The request
identity combines the selected lock and recipe bytes, normalized host/compiler
probes, and the OFF/HIP/CUDA configuration. The per-key lock covers private
source/build directories and publication. The resulting slot keeps the
compiled-in absolute prefix and is consumed in place; its ready manifest
authenticates the complete install/support-file closure, internal links, and
evidence before any tool path is used.

A failed or interrupted build leaves a partial slot and no ready manifest.
The resolver reports that slot and refuses to retry until an operator performs
manual recovery. The legacy sourced `scripts/env.sh`, `scripts/run_sim.sh`,
Make simulation goals, `scripts/program.py`, and generic `make oss` paths
reject a set `FES_TOOLCHAIN_CACHE_ROOT` rather than selecting a local or
ambient tool; only the FES Python recipes have shared-slot consumers in this
version. FPGA output schemas and the `BUILD_ID` algorithm are unchanged,
although tool hashes and resulting artifacts may differ when a shared compiler
is used.

## Experiments

The closed record of what each `experiments/NNN_*` design proved is the
[OSS experiment catalog](oss-experiments.md). How to run or extend that lane
is [OSS place-and-route testing](oss-pnr.md). Those notes are not core recipes
and do not accept a later package that reuses the same primitive. Unknown
experiment names are rejected by `scripts/experiment_policy.py`.

## Freeze-scaffold cartridges

The DE10-Nano has no partial reconfiguration. A composed cartridge is one
full-chip RBF. Pass-1 place-and-route writes an empty 901 socket. Pass-2
merges cart JSON into that routed shell:

```
nextpnr-mistral --json routed.json --fes-scaffold --fes-cart cart.json \
  --no-pack --router gpu
```

nextpnr stitches `plug_addr` from primitive FF Q ports (Yosys aliases
`plug_addr[5:0]` onto `gp_in[15:10]`), unbinds `plug_rdata` without a logic
BUF, and forces cart M10K `CLK1` onto the shell clock. `scripts/link_static_rbf.py overlay`
then copies CRAM for tile columns 21–33
(`experiments/901_plugged_base/link.toml`, `overlay_mode = "cram_rect"`,
`require_slot_only`) onto the pass-1 shell and refuses bits outside that
rectangle. Classify ignores sx120f ECC/CRC columns 41, 42, 45 and 49.
A taller 904 occupancy also flips companion strips 43, 46, 47 and 50.
`overlay_mode = "m10k_ram"` remains INIT-only for the 890/891/892 isolation
trio. `overlay_m10k_init_bt` is the byte-file form of that same RAM-mux
replacement for a 1024×10 lane: payload bits `[7:0]`, padding bits
`[9:8]` clear, four lanes per 40-bit word. The stored mux is that chunk
after nextpnr's `permute_init` permutation and inversion. The ZX81 machine reads BASIC
through `zx81_dpram` (`ADDRWIDTH` 14, address `{1'b0, rom_a[12], rom_a[11:0]}`)
from the low 8 KiB of `zx8x.hex`. The OSS package uses `zx81_rom_link`
instead and leaves those lanes empty. `make sim-fes-zx81-rom` checks the
simulation port.
`link_static_rbf.py init` applies one such lane to a placed RBF and reads
the RAM muxes back from the recomposed bitstream. `init --basic` writes all
eight lanes onto the column-26 proof sites. `init --machine` writes them
onto column 5, rows 73–80, and the receipt carries the 8 KiB image digest.
`890_slot_m10k` is the BEL-locked block at `MISTRAL_M10K.26.1.0`.
`893_zx81_basic8` places the eight legal column-26 M10Ks
(`26.1`, `26.2`, `26.5`, `26.6`, `26.9`, `26.10`, `26.13`, `26.14`).
The OSS `fes.zx81` recipe instantiates `zx81_rom_link` under
`FES_ZX81_ROM_LINK`, so the package inputs do not include `zx8x.hex`.
Simulation keeps the `zx81_dpram` hex path. The sealed package stays the
empty socket; launch splices BASIC into the programmed bitstream.


The single-socket `expansion` Go path admits the versioned ZX81 full-height socket or
the Coleco CPU-bus rectangle `(1769, 32, 2806, 1034)`, selected by the exact
slot/map pair. A Coleco manifest may also declare
`fes.coleco.response-boundary/4`: exactly two fixed shell-response CRAM
coordinates and each cart bit's resulting value. The linker requires both
bits to change as declared, applies them with the socket overlay and rejects
every other outside change. This patch does not enlarge the socket rectangle.
Changed Coleco frames regenerate their checksums; the existing ZX81 `Link`
behavior and output remain unchanged.

The Commodore 64 cartridge bus `fes.expansion.c64-bus` 1.0 uses
`fes.c64-bus.sockets/1`: socket 1 is the ROM window and socket 2 is the I/O
window, reusing the Apple II slot 2 and slot 4 placement rows and CRAM
rectangles `(1769,32,2806,1722)` and `(1769,1722,2806,3442)`. The runtime
admits only those two sockets.

The Apple II slot bus `fes.expansion.apple2-bus` 1.0 uses the multi-socket
layout `fes.apple2-bus.slots/1`: physical sockets for slots 2, 4, 5 and 7,
stacked in placement columns 24–28 at rows 1–18, 21–38, 41–58 and 61–78, with
disjoint half-open CRAM rectangles `(1769,32,2806,1722)`,
`(1769,1722,2806,3442)`, `(1769,3442,2806,5162)` and `(1769,5162,2806,6882)`.
Each card manifest carries `slot_index` (sorted between `slot` and
`slot_major`); single-socket manifests must omit it. `ComposeSlotsContext`
links any combination of cards, comparing every card with the original
shell and admitting its changes only inside its own rectangle, and
`ComposeSlotsROM` adds the format-3 ROM link, rejecting ROM destinations in
any socket. The v2 composition ID is SHA256 of `fes-composition-v2`, NUL,
package ID, NUL, `slot:expansion-id` NUL per card in ascending slot order,
then the linked-payload SHA256. The single-socket `Compose` path rejects
multi-socket cards.

### Video parts

The native-pixel prototype is a separate internal fabric,
`fes.fabric.video.native-pixels` 1.0. `coleco_native_video.v` emits one
indexed4 active pixel after the registered VDP reads settle; it owns no frame
RAM or HDMI timing. `fes_native_cdc.v` transfers a held token from the 52.224
MHz machine clock to the 74.25 MHz output clock. It reports an overrun as an
invalid token and retains the latest undelivered HOLD control. The generic
`fes_native_video.v` owns packed double-buffer capture, complete-frame
publication at output frame boundaries, centered 2x scaling, and optional
scanlines. Source pauses repeat the last complete frame. HOLD keeps HDMI
timing running, mutes RGB and aborts incoming/pending frames.

`make sim-fes-native-video` exercises the consumer and clock crossing;
`make sim-fes-coleco-native` exercises the actual registered Coleco VDP source.
`make synth-fes-native-video CACHE_ROOT=/absolute/cache` runs authenticated
locked diagnostic synthesis of both effects, verifies 48 M10Ks and one RAM
clock, and records source hashes/tool identity beneath
`build/synth/fes-native-video/`. This accepts working-tree sources; it supplies
no placement, timing, sealed-package or hardware evidence.
The optional `FES_COLECO_NATIVE_VIDEO_DEV` top-level branch instantiates the
native consumer inline, with `FES_NATIVE_SCANLINES` selecting its effect.
The inline path remains a host simulation prototype. The separate native
producer and admission path use the wider closed layout below;
FES factory package selection uses that native layout. Native physical
linking requires fresh exact-shell placement, containment and timing evidence;
the raster reservation has only 16 M10Ks and cannot hold native frame capture. The
[native contract](../../mister-packages/docs/video-parts.md#native-active-pixels)
defines token validation and the initial geometry/encoding. Future RGB, SMS,
overlay, CRT and DDR profiles require separate implementation and evidence.

The first video-parts layout is `fes.coleco-video.parts/1`. It reserves a
Coleco bus 2.0 socket at placement columns 24–28, rows 1–19, and a video
socket at columns 24–28, rows 23–38. Their half-open CRAM rectangles are
`(1769,32,2806,1800)` and `(1769,1800,2806,3442)`. The video-socket
shell declares optional `fes.fabric.video.raster-rgb888` 1.0; it retains the
fixed-720p60 external interface and the base package's GP capabilities and
BUILD_ID during composition. Select `video_socket=True` or `--video-socket`
for this raster lane. The standalone producer default remains the CPU-only socket.

The native layout is `fes.coleco-native-video.parts/1`, selected by
the optional `fes.fabric.video.native-pixels` 1.0 package marker. Its video map
is `fes.coleco-native-video.socket/1`, at placement columns 5–38, rows 23–38,
with the wider half-open CRAM fence `(124,1800,3906,3442)`. The CPU bus 2.0
socket retains `(1769,32,2806,1800)`. The sealed native producer is selected by
`native_video=True` or `--native-video-socket`; it cannot also select the raster
socket. The native marker version fixes 256×192 TMS9918 Index4 source pixels.
The shell owns the source adapter, clock crossing, clocks and HDMI/audio
delivery; a required direct or scanline video part owns frame capture and
720p timing/output. A vacant native socket has no built-in output. These
assets use the existing archive grammar and exact-shell bindings. FES selects
this producer for factory publication; FogCast resolves the household preference
against matching installed parts. A missing selected part cannot fall back to
the vacant native shell. Older raster packages retain their existing behavior.

The native shell tries the ten seeds `3,4,5,1,2,6,7,8,9,10` at HeAP timing
weight 2000, then the same seeds at weight 1000 if needed. The bounded search
stops at the first complete route meeting all three clock requirements, with a
600-second limit per attempt. The CPU-only and raster variants retain their
ten-seed, weight-2000 search. The ordered weights, mode, attempt budget and
timeout enter the build identity; route evidence records the selected seed
and weight. This fallback does not establish timing margin for a later build.

The native producer proves every FF/RAM clock traces through only transparent
buffers to the declared input buffer, then normalizes all clock pins to that
boundary before frozen import. The locked importer reconnects every traced
clock alias, including both ports of an SDP M10K.
RAM modes, write enables and data paths remain identical. `cart-synth.json`
retains the original synthesis output; the build summary binds both netlist
hashes and clock-pin counts. Routed validation still requires every active
part clock pin to use `pixel_clk` before a part can be published.
Native composition checks every decoded CRAM bit against its exact rectangle
and copies every in-fence change. It uses no legacy whole-column exclusions:
those columns also contain real routing bits used by the wider native part.
The producer independently requires its preview to match the entire routed
CRAM, and the target Go linker applies the same strict rectangle. Frame CRCs
are regenerated by the codecs. Existing raster/CPU composition policy remains
unchanged.

The [shared fabric contract](../../mister-packages/docs/video-parts.md)
defines RGB888, DE/HS/VS, pixel enable, start-of-frame, end-of-line, HOLD
and a required-zero reserved bit. In the raster layout all video logic uses
`pixel_clk` at 74.25 MHz. The shell owns clocks, HDMI, HPS and I2S.
Request and response registers add two pixel clocks of latency to the
entire raster word. A vacant socket selects the equally delayed machine
raster; a linked direct or scanline part asserts response CE to select its
output. Audio follows the existing machine path.

`build_video_part.py` builds each part against the exact sealed shell,
adapting its public RTL interface to the compiler's packed cartridge ports.
It checks timing, clock ownership, the original configuration header and
every changed CRAM bit before publishing an archive. Pixel clock coverage
anchors lie inside the video fence; CPU anchors remain frozen. Part outputs
retain logic drivers through separate synthesis, avoiding input/output alias
collapse during packed-port merge. This uses the locked Coleco compiler
without a new compiler ABI.

`ComposePartsContext` requires one video part and permits one Coleco bus 2.0
expansion. It validates both against the original shell, rejects duplicate
roles, mismatched layouts and outside-region writes, then composes disjoint
regions in canonical role order. Its identity binds the base package ID,
layout, selected part IDs and resulting payload digest. Legacy single-socket
composition rejects video parts. The target independently recomposes the
developer transfer and retains separate part status alongside base identity.
Native and raster slot/map pairs are admitted only in their matching closed
layout; mixing them cannot relabel a shell or enlarge the old raster fence.
The host-only raw `fes-parts-link` diagnostic defaults to the raster layout;
native inputs require `-layout fes.coleco-native-video.parts/1`. The sealed
FogCast `fes-parts` command selects the layout from the exact package marker.
This is full-chip download-time composition; changing a selection requires
another load. FogCast owns library/profile selection and its installed part
inventory. FES builds the direct and scanline profiles against the selected
package and publishes a separate sealed `core-video-parts` tree. Its canonical
index binds the exact shell package, profile, part identity and archive
digest/size. Compiler evidence and the frozen routed netlist remain in host
caches; they are not installed on the target. Reuse validates the current
functional inputs and original provenance, then independently checks the
configuration header, actual CRAM fence, timing and clock/resource ownership.
Other native geometries, DDR scanout, overlays, variable modes and audio parts
remain subsequent work.

The `expansion` Go module also provides `LinkROM` and the standalone
`fes-rom-link` diagnostic. They patch mapped M10K INIT bits directly in decoded
frames and regenerate the affected EDCRC/CRC16 checksums, preserving other CRAM
bits, the ORAM/PRAM header and compressed/uncompressed representation. No Python,
Mistral executable or device database is needed by that linker. Context
cancellation is checked during decoding, validation, patching and compression.
The current encoding is 1024x10, with two zero padding bits per input byte;
ROM input must have the exact declared binary size. All-zero ROMs are valid.

`scripts/rom_map.py` extracts the eight ZX81 machine sites. The production
`build_fes_zx81_oss.py` recipe authenticates the three Mistral database files
against SHA256 pins taken from the selected compiler commit. The database
snapshot is checked before compilation and again before export; mutable compiler
source directories are not assumed to be covered by `FunctionalInvocation`'s
installed support inventory. The pins, ROM encoding and package format enter
functional build identity alongside the extractor and codec source closure.

Map export requires all eight routed `machine.rom.lane0` through `lane7` cells
at their fixed column-5, row-73–80 `NEXTPNR_BEL` placements, with 1024x10
asynchronous read parameters and empty INIT. It also decodes the actual base RBF
and checks every mapped INIT bit is blank. Explicit destinations are Mistral
linear CRAM addresses in stored-bit order, bound to the base RBF SHA256.

The producer writes a format-3 `manifest.toml`, `core.rbf`, and `rom-map.json`;
its sealed ROM is `machine-rom`, role `firmware`, source size 8192. The same exact
manifest and map bytes enter the immutable package and archive. Database hashes
are retained in the build summary. Failed validation removes the new map,
manifest and RBF. ROM uploads supply bytes only and cannot select destinations.
The standalone extractor remains a diagnostic tool; its unsealed output does
not constitute a production package.

Host diagnostic (use a blank ZX81 OSS RBF and its selected Mistral sources):

```sh
python3 scripts/rom_map.py --mistral-source /path/to/selected/mistral \
  --base /path/to/blank.rbf --output /tmp/rom-map.json
cd expansion
go run ./cmd/fes-rom-link -base /path/to/blank.rbf -map /tmp/rom-map.json \
  -rom /path/to/8192-byte-rom.bin -output /tmp/initialized.rbf
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build \
  -o /tmp/fes-rom-link-arm ./cmd/fes-rom-link
```

The diagnostic publishes its output only after a successful link; it does not
program hardware. Synthetic Mistral fixtures exercise bit ordering, frame
checksums and preservation outside INIT. They are not routed-core or physical
acceptance. The first ARMv7 exact-kit diagnostic linked a production ZX81 ROM in
3.20–3.26 seconds; that result does not establish image acceptance. A five-iteration host benchmark on an Intel Core Ultra 7
270K Plus measured 99.6 ms/link and 18.0 MB allocated/link for the eight-block
random-ROM fixture (Go 1.26.5, linux/amd64). Allocated bytes are not peak RSS;
this is not an ARM performance claim. Reproduce with `go test ./... -run '^$'
-bench BenchmarkLinkROM -benchtime=5x -benchmem` from `expansion/`.

Commands, cart-authoring rules and kit probes live in
[OSS place-and-route testing](oss-pnr.md#freeze-scaffold-cartridges).
`scripts/build_fes_slot.py` is the compose entry point; it fails
closed unless `nextpnr --help` advertises `--fes-scaffold` and `--fes-cart`.
The locked nextpnr `5063215e` provides those flags after `make toolchain-fes`.
It also corrects pass-through LUT masks for `MISTRAL_BUF` routing cells:
the earlier `d672fade` emitter could write all-ones masks despite successful
simulation and timing. The selected PR #73 revision has an emitted-bitstream
regression covering buffers, inversions, ordinary LUTs and initialized MLABs.
That compiler regression does not replace hardware acceptance of rebuilt cores.
`NEXTPNR_MISTRAL` overrides the binary. ZX81 carts compose onto
`904_zx81_socket` with the same linker. This path does not seal `fes.zx81`
and is not FogCast format-3.

## FES ColecoVision first slice

`cores/fes-coleco` is the next FES emulator bring-up after Pong and ZX81. It
uses the [MiSTer ColecoVision core](https://github.com/MiSTer-devel/ColecoVision_MiSTer)
as a system reference, but is a reduced Verilog-first adapter around the
shared `fes.application` 1.0 mailbox, with `fes.gamepad.ports` and
`fes.keypad.ports` 1.0, fixed video, blob and blob-stream media, and optional
`fes.firmware.blob` 1.0. A/B map to Fire 1/2; the
twelve keypad bits represent 0..9, *, #. Each active-high full-state write
addresses port 0 or 1. HOLD neutralizes both controllers. The console-owned
staging RAM preserves registered media-copy timing. The first slice contains a TV80
Z80-compatible CPU, the Coleco reset/cartridge/RAM map, a shared TMS9918-style
renderer for Graphics I, Graphics II, Text and Multicolor on the fixed
256×192 logical raster, two active-low controller views and the FES fixed-video
shell. A raw 1–32 KiB cartridge image uses the fixed
`0x8000–0xffff` aperture. Images up to 16 KiB preserve the prior C000 mirror;
larger images use all fifteen address bits and return FF beyond committed length.
The shared stream endpoint validates ordered chunks and complete CRC before
publishing the console-owned 32 KiB staging RAM. The registered cartridge copy
holds CPU/VDP reset through its final write. Legacy blob stays bounded to 16 KiB.
Audio is the shared SN76489 path under [Shared application audio](#shared-application-audio).
The shared 32-bit fractional raster enable emits 4,024,320 logical samples per
second from each core's configured system clock; 256 samples × 262 lines gives
a nominal 60 Hz frame cadence independent of CPU and HDMI pixel clocks. This
models frame/line pacing, not composite sync, half-lines or cycle-perfect raster
effects. The development Coleco machine edge has an inactive vacant response
and masks expansion read claims to `0x2000–0x5fff` or unclaimed I/O. The
physical socket is behind `FES_COLECO_EXPANSION_DEV`; a separate
`coleco-expansion.lock` pairs registered-memory Yosys with the socket-aware
Mistral and nextpnr pins. The development producer seals a timed shell with a
reserved `24 1 28 11` placement region containing only pinned boundary FFs.
It declares optional `fes.expansion.coleco-bus` 1.0. The diagnostic module
uses the frozen shell and restores the system PLL's
second output from exact routed metadata. The integrated shell/cart has no
outside CRAM changes. Its producer permits only the two fixed response-stub
changes declared by `fes.coleco.response-boundary/4` if a route needs them,
alongside the CPU-bus rectangle; the linker rejects every other outside change.
The selected factory recipe and FES registration remain unchanged. External
bus mastering, video/audio takeover, bank switching and retail-cartridge
compatibility remain outside this slice.

Graphics I groups color entries by eight character patterns. Graphics II uses
screen-third pattern/color addressing and the register masks for table mirroring.
Text renders 40×24 six-pixel glyphs with eight-pixel side margins and suppresses
sprites. Multicolor selects four 4×4 color blocks per character and keeps
sprites active. Unsupported mode selectors render the R7 backdrop. All four
modes retain the four-bit palette path and color-zero backdrop behavior; display
disable suppresses the playfield. The sprite renderer covers normal 8x8/16x16
sprites, magnification, early-clock positioning, signed/clipped X coordinates,
transparency/priority, four visible sprites per line, collision and
fifth-sprite status. The VDP uses four coherent
VRAM copies with broadcast CPU writes, registered read-ahead and a serial SAT /
pattern walker. Two alternating framebuffer line banks use packed 6-bit M10K
entries for pixel and visibility metadata; publication is interlocked with the
registered raster coordinate.
Every sprite column and magnified repeat has a separate line-bank read before
its write decision. Priority and collision use that pixel's metadata rather
than the previous address or a same-port read-during-write result.

The default reset ROM is an open `JP 0x8000` shim, not a Coleco BIOS. Quartus
(`set_parameter -name ENABLE_FIRMWARE 1`) and OSS (`chparam -set ENABLE_FIRMWARE 1`)
synthesize the shared mailbox overlay into the 8 KiB aperture; host simulations
keep `ENABLE_FIRMWARE` at its RTL default 0. The default package still ships the
open shim and declares `fes.firmware.blob` 1.0 optional so BIOS-free titles share
the bitstream. Runtime firmware is an exact 8192-byte begin/data/commit while
reset is held; it is not cartridge media and does not release execution.
Firmware mailbox behavior is software-tested; physical Coleco BIOS bind remains
pending. The OSS producer's explicit `--bios PATH` option embeds a privately
supplied 8192-byte BIOS using a read-only binary/HEX snapshot in ignored build
output. Identity v2 binds both digests and the BIOS mode, and the producer
rechecks the snapshot before and after compilation and before export. This
variant declares `fes.coleco.private-bios` and exports only to
`build/private-packages`, separate from the default image-selected package.
The BIOS is a build input, not an extension to the cartridge media protocol.
BIOS boot does not establish general retail-game compatibility; rendering and
input require per-game validation.

The serial renderer, replicated VRAM, registered request/wait schedule, packed
line banks, sequential clear and publication interlock are deliberate RTL
scaling accommodations shared by both compiler lanes. The original procedural
sprite loop expanded to roughly 42K mapped combinational cells; the registered
one-column/repeat schedule fits the fixed system-clock budget. The Coleco OSS
recipe uses its core-local lock with Yosys `e2d425de`, nextpnr-mistral
`a93fe013` with `--router gpu`, HeAP timing weight 100, criticality
exponent 5, and `--timing-allow-fail`, and Mistral
`7ed06e21`; the selected toolchain enables the HIP device backend. Default
place-and-route is first-to-pass: seeds 5, 4, 1, 2, 3, 12, 7 and 10 at
weight 100, then the same seeds at weights 300 and 1000. nextpnr `a93fe013`
times each GPU route with Mistral's analogue signoff model and re-routes
near-critical nets when a clock misses, so the table-model and signoff
results no longer diverge silently.
`make build-fes-coleco BEST_FMAX=1` synthesizes once, then searches the
weight list 10/100/300/1000/2000 and remaining seeds for the best Fmax on
one HIP device (functional identity v2 rejects more than one);
the winner is stored in route evidence, not written back into recipe
constants (that would change `BUILD_ID`). Because the
sealed build record changes the embedded `BUILD_ID`, the seed is part of the
route recipe. The GPU router can report a provisional timing shortfall before
its final repair/signoff pass; the allowance lets it complete, while the
recipe's structured final `clk_sys` and `pixel_clk` evidence still has to meet
the requested constraints. The Quartus wrapper retains the literal
`altsyncram` mode `NEW_DATA_NO_NBE_READ`; OSS preserves the registered
semantic schedule rather than that vendor literal. No missing nextpnr BEL or
pack feature is implied.

The open diagnostics and focused simulations exercise CPU-driven VDP writes,
controller modes, sprite status and the exact 720p frame in both conditional
lanes. The build recipes require a clean checkout and seal format-2 packages;
they never program hardware. Parent selection and exact-kit acceptance are
recorded in the FES validation documents.

### OSS/Yosys/nextpnr workarounds

These are the portability accommodations to hand to the Yosys/nextpnr/Mistral
owner. The RTL scheduling choices are shared by both compiler lanes; entries
marked as path-specific are not requirements of the other lane.

| Boundary | Current accommodation and ownership |
| --- | --- |
| Toolchain selection | The repository-wide lock pins DeanoC Yosys `5391eeb1` and nextpnr `5063215e` with Mistral `8fcc4cb4`. Factory Coleco v2 selects `toolchains/coleco-sgm.lock`, builds it under `build/toolchain/fes-coleco-socket-v2`, and enables HIP. SG-1000 retains `toolchains/registered-memory.lock`. SMS selects `toolchains/fes-sms.lock` (nextpnr `a93fe013`, Mistral `7ed06e21`, Yosys `e2d425de`). Quartus needs neither lock. |
| Verilog/VHDL frontend | OSS uses Verilog TV80/T80pa with `TV80_REFRESH=1`; Quartus may retain its VHDL T80pa path. This is an OSS frontend choice, not a nextpnr gap. |
| Machine RAM | Both lanes use registered-address RAM semantics. OSS selects `coleco_dpram` with registered `ram_style="m10k_tdp"`; Quartus uses `altsyncram`. Default simulation alone keeps asynchronous reads. |
| Registered media bridge | Both lanes prime the mailbox result, delay the cartridge write address, flush the final byte, and re-arm on `media_ready` falling or reset rising. This is required by the registered memory schedule in both lanes. |
| VDP multi-read VRAM | A single VRAM with one CPU port and three combinational raster reads fails OSS mapping and leaves Quartus with an oversized direct-memory implementation. Both lanes use four coherent copies, broadcast CPU writes, and pipelined name-to-pattern/color reads; the fourth copy feeds the serial sprite walker. |
| Sprite line banks | Both lanes use alternating 256-entry packed 6-bit M10K entries, registered renderer read/write phases, a sequential clear and a raster-coordinate publication interlock. This keeps the renderer inside the system-clock budget and avoids publishing a line into the preceding framebuffer row. |
| Read-during-write mode | Quartus 17.0.2 rejects `OLD_DATA` for the bidirectional packed sprite shape, so the Quartus primitive uses `NEW_DATA_NO_NBE_READ`. The renderer consumes `q_a` one phase later; OSS preserves that schedule without depending on the Quartus literal. |
| Sprite rendering | The procedural 16x2 loop expanded to about 42K mapped combinational cells and stalled routing. Registered column/repeat counters issue one source-pixel read/write pair per system clock and pack pixel, occupied and visible metadata, reducing the measured fabric to about 3.1K ALUT cells. This source-level scaling is shared by both lanes. |
| Bulk initialization | `initial` loops over 16 KiB VRAM, cartridge RAM or the 49,152-entry framebuffer create large memory initialization structures. The bring-up initializes scalar state only and clears active line storage sequentially. |
| Reset image | OSS consumes tracked byte-per-line `coleco_reset_rom.hex`; Quartus `altsyncram` consumes tracked range-form `coleco_reset_rom.mif`. This is a file-format split, not a different reset image. |
| PLL and I²C | Both retain the two existing `altera_pll` wrappers. Quartus uses tri-state HDMI I²C; OSS uses `MISTRAL_IO` open-drain pads and the HPS I²C BEL `cyclonev_hps_interface_peripheral_i2c.52.60.0`. |
| Constraints | OSS uses only its accepted pin QSF and 50 MHz `clocks-oss.sdc`; nextpnr derives PLL clocks. Quartus retains `HPS_LOCATION`, clock groups and the full SDC. |
| Route pressure | The OSS reproduction is `5CSEBA6U23I7`, nextpnr `a93fe013`, `--router gpu`, seed 5 first (order 5, 4, 1, 2, 3, 12, 7, 10), HeAP timing weight 100 before 300 and 1000, criticality exponent 5, `--timing-allow-fail`, no `--tmg-ripup`, at 74.25 MHz. The embedded `BUILD_ID` makes the seed part of the route recipe. The GPU router can report a provisional timing shortfall before final repair; the allowance only permits that intermediate result, while the recipe requires final structured `clk_sys` and `pixel_clk` timing to pass. The sealed recipe requires `backend hip:<device> ready` and rejects CPU-reference fallback; no missing BEL or pack feature was identified. |

The concrete build entry points are `make build-fes-coleco-quartus` and
`make build-fes-coleco`; both require a clean source checkout, seal format-2
packages and never program hardware. Exact-artifact kit acceptance remains a
separate FES integration step.

The Opcode SGM candidate uses a separate development shell with a 31-bit
request and 28-bit registered response. The response carries direct data,
claim, WAIT, INT, a shell-RAM claim and signed PCM. The shell owns a dormant
32 KiB M10K RAM and saturated SN+AY audio path; the separately synthesized SGM
owns the window-enable and AY register decode. `toolchains/coleco-sgm.lock`
pins nextpnr `655f3833` with frozen-scaffold BEL admission and bounded slot
placement. Its GPU router selects all users of congested wires before rip-up
and preserves the initial-routing budget through small congestion plateaus.
The shell and its matching video/SGM parts use this same locked compiler.
The v2-only socket reserves `24 1 28 19` placement and
`(1769,32,2806,1800)` CRAM; v1 retains its smaller rectangle. The v2
build scripts keep the v1 diagnostic's socket and archive contract untouched.
The frozen v2 shell also pins a clock-only FF at `MISTRAL_FF.24.4.56` and
validates its row 4 global-clock route. The compiler serializes boundary FF
data buffers as separate cells. Shell validation admits only a buffer at the
FF's paired COMB/MLAB site, with the expected pin map and an exclusive connection
to that FF's data input. Request and response buffers remain frozen during
composition. Clock anchors, their paired buffers and all serialized routes
also remain frozen: removing their cells alone leaves occupied constant-input
wires at sites the cart placer would otherwise reuse. Their LABs stay reserved
when placing a part; the part must fit the remaining capacity and pass the
unchanged clock and CRAM containment gates. Video preparation preserves the
complete CPU boundary outside the video fence.
The v2 linker admits
only an exact optional Coleco bus 2.0 shell and map `/2` archive, with no
outside-rectangle CRAM exception. The enlarged development shell and SGM cart
have separate diagnostic routes meeting all three timing gates, and the cart's
CRAM diff is contained. The sealed shell and SGM cart from FES commit
`8dfcc60f` passed an exact-artifact [kit diagnostic](../../../docs/validation/2026-09-25-coleco-sgm-v2-hil.md)
with the original BIOS-free SGM probe. Factory selection still uses the v1
Coleco package for the recorded run; the current FES factory recipe now selects
the v2 producer and requires fresh selection and image evidence.

## FES SG-1000 Quartus oracle and OSS recipe

`cores/fes-sg1000` is the Coleco sibling bring-up for package `fes.sg1000`.
It selects the first-party original NMOS Z80 and the shared TMS9918-style VDP,
which renders Graphics
I, Graphics II, Text and Multicolor on a fixed 256×192 logical raster. Text
suppresses sprites, Multicolor keeps them active, and unsupported selectors
render the backdrop. Its 256×262 logical raster shares Coleco's nominal 60 Hz
fractional enable. It also reuses the dual-port RAM wrappers,
the `fes.simple-computer` mailbox, system/video PLLs and the 720p HDMI shell.
The SG-1000-specific RTL is the memory map (cartridge at `0x0000–0x3fff`, 1 KiB
RAM at `0xc000`), the 8255 joystick ports `0xdc`/`0xdd`, and PSG write decode at
`0x40–0x7f`. Coleco's SN76489-compatible PSG is driven by a fractional
3,579,545 Hz enable. Its signed mono sample feeds both channels of the shared
PCM-to-I2S output. Coleco's two-output PLL supplies 52.224 MHz system and
12.288 MHz audio from one board PLL; video uses the other. There is no BIOS shim.
The VDP asserts the Z80's maskable INT input; NMI is inactive.
`make sim-fes-z80-timing` checks both CPU half-cycle enables over a full second;
each machine's simulation includes it.

`make sim-fes-sg1000` is the diagnostic Verilator machine check
(behavioral memory). `make sim-fes-sg1000-oss` compiles the same
machine with `-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1` so registered media,
`coleco_dpram` M10K TDP, and the registered four-copy VDP are the shapes
Yosys maps. `FES_SG1000_OSS` alone does not select those Coleco wrappers.
`make sim-fes-sg1000-rom-link` tests the product shape with an exact 16 KiB
diagnostic cartridge and no media handshake.

`make build-fes-sg1000-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe.
It requires a clean committed tree to seal a format-2 package and never
programs hardware. `--compile-only` writes `build/fes-sg1000-quartus/core.rbf`
and timing evidence without sealing.

`make build-fes-sg1000` is the OSS producer
(`scripts/build_fes_sg1000_oss.py`). It uses `toolchains/registered-memory.lock`
(Coleco compatibility pin), `constraints-oss.qsf`, and `clocks-oss.sdc`.
Yosys defines `FES_SG1000_OSS=1`, `FES_SG1000_ROM_LINK=1`, and
`FES_COLECO_OSS=1`. The cartridge bank-mux output is registered in the system
domain; its extra system-clock latency fits the native CPU read windows. Both
fractional CPU half-cycle enables are captured on the rising system edge before
the CPU and VDP consume them, preserving cadence while giving control paths a
full system-clock period. The product RBF contains a
blank 16-lane M10K cartridge, and the format-3 package carries a validated
`rom-map.json`, exact 16 KiB `cartridge-rom` requirement and required
`fes.audio.pcm-s16-stereo-48k` 1.0. The ROM-linked build reports GP mask
`0x13` (keyboard, video, audio), with no media-blob bit. The producer checks
two PLLs, four routed 3.3 V I2S outputs and passing system, pixel and audio
timing domains before sealing version 1.3.0. Reset is not held for an
application media upload. The open `sound-16k.rom` diagnostic alternates tone
and white noise alongside the Graphics I display for a later leased kit check.
`--synth-only` runs Yosys without a clean tree and does not seal. The HIP
first-pass search tries seeds 2, 3, 4, 1, 5–10 at weight 2000, then weight
1000, with at most 20 candidates and 1800 seconds per route. It must meet the structured 52.224 MHz system, 74.25 MHz
pixel and 12.288 MHz audio timing rows on the exact sealed BUILD_ID. `fes.sg1000` is in the
factory image. A historical
launch/Stop record does not accept the current bitstream.

## FES Master System Quartus oracle and OSS recipe

`cores/fes-sms` is the Coleco / SG-1000 sibling bring-up for package `fes.sms`
(FogCast `protocol.SystemSMS = "sms"`). Do not use `fes.mastersystem`. It
reuses Coleco TV80 and the shared legacy TMS9918-style VDP. Graphics I, Graphics
II, Text and Multicolor use its fixed 256×192 logical raster; Text suppresses
sprites, Multicolor keeps them active, and unsupported selectors render the
backdrop. Dual-port RAM wrappers, the `fes.simple-computer` mailbox and both
PLL wrappers are also shared. The SMS-specific RTL is
the Mode 4 VDP and six-bit 720p video shell plus the memory map (32 KiB fixed
cartridge at `0x0000–0x7fff`,
unmapped `0x8000–0xbfff`, 8 KiB RAM at `0xc000` mirrored at `0xe000`), the 8255
joystick ports `0xdc`/`0xdd`, VDP IRQ on Z80 INT rather than NMI, the SN76489
on ports `0x7E`/`0x7F`, FPGA→ADV7513 I2S, and the
`fes.simple-computer` mailbox. The TMS fallback uses the shared nominal 60 Hz,
262-line fractional raster enable. SMS Mode 4 retains its prior `/16` enable
(about 49 frames/s), because its serial scanline builder exceeds the 60 Hz
line budget; optimizing that renderer is separate work. This timing model
covers logical frame pacing only, not composite sync, half-lines, PAL timing or
cycle-perfect raster effects. The OSS package uses 32 fixed blank M10K
cartridge lanes. Each lane is a synchronous read (`CFG_ASYNC_READ=0`, live
`CLK1`, `A1EN` and `B1EN` held high): the address and bank select
`address[14:10]` register on the system clock, the lane mux is combinational,
and the output register is the second stage. The CPU sees the byte
two system clocks after the address. `T80pa` samples `DI` at T3,
several half-cycles later, so that byte is stable. Synthesis and the routed netlist
reject every asynchronous M10K, and the ROM map requires
`expected_async_read=0`. The format-3 ROM map authenticates those lanes; the
target links an exact 32 KiB `cartridge-rom` before download. Shorter
fixed-map ROMs must be explicitly padded with `0xff`. The Quartus oracle and default mailbox
simulation remain format-2 media-transport diagnostics. There is no BIOS shim. Mode 4 implements
16 KiB VRAM, 32-entry six-bit CRAM, tile attributes and scrolling, 8×8/8×16
zoomable sprites with collision/eight-sprite overflow, line interrupts and
VBlank interrupts on the fixed 256×192 logical raster. The PSG mix is a signed
16-bit sample. The shared coherent crossing produces 48 kHz stereo I2S0;
Hold and PLL loss mute it. The required `fes.audio.pcm-s16-stereo-48k` 1.0
interface makes the runtime program ADV7513 (N=6144, CTS=74250) after live
identity. There is no host audio-stream mailbox. Mappers,
banked/48 KiB retail images, 224/240-line modes and PAL timing remain outside
this slice.

`make sms-diagnostic` emits a 32 KiB-capable Mode 4 cartridge that jumps
from `0x0000` to code at `0x4000` and programs an SN76489 square wave. The sim
image HALTs after the RAM signature; the HIL image (`--interactive`) keeps the
controller poll loop with the tone running.
`make sim-fes-sms` is the default Verilator check (`-DTV80_REFRESH=1` only):
the mailbox consumes `cores/fes-sms/generated/stream-exchanges.json`, the VDP
unit covers legacy Text/Multicolor colors, Mode 4 VRAM buffering, CRAM color,
tile priority/palette, sprite collision,
line IRQ and VBlank IRQ, the PSG unit covers ports `0x7E`/`0x7F` and the tone-0
square wave, zero-period behavior and fixed/tone-driven Sega noise dividers,
shared HDMI I2S covers 16-bit 48 kHz frames and Hold/clock-loss
mute, and the machine covers the
32 KiB map, long-then-short `0xff` tails, and CPU execution of that diagnostic
(not reset-only peeks).
`make sim-fes-sms-oss` is the linked-ROM OSS-conditional check
(`-DFES_SMS_OSS=1 -DFES_SMS_ROM_LINK=1 -DFES_COLECO_OSS=1`). Both are host
simulation, not hardware acceptance. The OSS format-3 ROM-link mailbox omits
the legacy blob capability and rejects blob commands. Its package declares
keyboard, fixed-video and required PCM audio alongside its required ROM.

`make build-fes-sms-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe.
It requires a clean committed tree to seal a format-2 package and never
programs hardware. `--compile-only` writes `build/fes-sms-quartus/core.rbf`
and timing evidence without sealing.

`make build-fes-sms` is the OSS producer
(`scripts/build_fes_sms_oss.py`). It uses `toolchains/fes-sms.lock`
(nextpnr `a93fe013` with Mistral `7ed06e21` and Yosys `e2d425de`), SMS `constraints-oss.qsf` (Coleco
video/I2C pins plus ADV7513 I2S), and Coleco `clocks-oss.sdc`. Yosys defines
`TV80_REFRESH=1`, `FES_SMS_OSS=1`, `FES_SMS_ROM_LINK=1`, and `FES_COLECO_OSS=1`. `--synth-only` runs
Yosys without a clean tree and does not seal. The producer uses `--router gpu`
and a first-pass HIP seed/weight search (starts at seed 3 / HeAP 1000,
then the remaining `PLACER_SEEDS` and weight 300). Final structured `clk_sys` and
`pixel_clk` and audio rows must meet 52.224, 74.25 and 12.288 MHz; four routed
I2S pads and the shared system/audio PLL must match. `fes.sms` is in the
factory image. Historical parent
pins and launch/Stop records do not accept HDMI audio on a later bitstream.
Dated notes, not current instructions:
`docs/validation/2026-09-17-sms-oss-gap-ladder.md`,
`docs/validation/2026-09-17-sms-32k-fixed-map.md`, and
`docs/validation/2026-09-17-sms-32k-p2-diagnostic.md`.

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
`start`, `freeze`, 2-bit `paddle_speed` and 10-bit `pixel_x`/`pixel_y`. A one-clock `frame_tick` advances game
state; raster coordinates select RGB pixels independently. Outputs are 8-bit
`red`/`green`/`blue`, `tone`, `playing`, integer top-left positions
`ball_x`/`ball_y`/`player_y`/`ai_y`, and 4-bit decimal scores
`player_score`/`ai_score`, plus single-clock `player_return` and `point` event
pulses. Speed enums 0/1/2 select 2/4/6 pixels per frame; movement saturates at
0 and 208 for all three speeds. Freeze holds positions, scores, serve/start
state and tone counters; raster generation remains independent. The original
MiSTer wrapper ties freeze low and selects normal speed (enum 1).
Paddles are 4x32 at x=12 and x=304; the ball is 4x4.
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

## FES GP and fixed 720p Pong shell

`cores/fes-pong/rtl/fes_gp.v` implements `fes.simple-game` 1.0 on the Cyclone V
HPS general-purpose port. Its clock is the 74.25 MHz pixel domain. Ports include
32-bit HPS `gpo`, fixed 128-bit `build_id`, 32-bit FPGA `gpi`, gameplay
`game_reset`, `game_frozen`, `buttons[7:0]`, and `paddle_speed[1:0]`, plus
`player_return`/`point` inputs from the shared game. FPGA configuration state starts with a
stable `0xF5` signature, ACK zero, gameplay held in reset and neutral input.
The request toggle crosses two `async_reg` stages. The state machine consumes
the held payload only when that synchronized toggle differs from the retained
ACK, updates response and ACK together, and never changes ACK for gameplay
reset. Invalid opcode, index and argument responses have no gameplay effect.

The checked-in `cores/fes-pong/generated/fes_gp.vh` is the unedited Verilog
emitter output from `mister-packages/packages/abi/fes_simple_game.yaml`.
`cores/fes-pong/generated/persistence-exchanges.json` is the unedited shared
persistence fixture. It declares request toggle zero, synthetic build ID
`00112233445566778899aabbccddeeff`, and all four live capability bits (15).
The retained `exchanges.json` remains the original volatile fixture with bits
0/1 only; the persistent core runs the newer sequence. `make sim-fes-pong`
verifies exact GPI words, varied host-to-FPGA edge placement, field holding,
one effect per toggle and error isolation.

## FES simple-computer mailbox

`cores/fes-zx81/rtl/fes_computer_gp.v` implements `fes.simple-computer` 1.0 on
the same GPO/GPI transport. It holds execution in reset, keeps eight active-low
keyboard rows at `0x1f`, and accepts a 1..16384-byte media blob through
begin/data/commit. Hold reset clears in-flight media and the keyboard; a
committed blob stays. Media begin with eject index and argument 0 clears
readiness so the next empty `LOAD ""` reports `0/0`. Control-index begin with
argument 0 remains invalid argument. While `media_busy` is high (the ZX81
tape-loader is copying), begin and eject reject with invalid-state instead of
aborting the copy. Begin/data/commit do not require `exec_reset` held; that
hold is launch-time runtime policy. A media byte is written on the clock
after the command is accepted, and a pair uses the next clock for its high
byte. Response and ACK are published together with that write. The loader
reads the other RAM port at `media_addr`, which keeps the pointer compare
off that data path. The checked-in
`cores/fes-zx81/generated/fes_simple_computer.vh`
and `exchanges.json` are unedited mister-packages outputs. `make sim-fes-zx81`
plays that fixture and checks keyboard/media side effects. Its optional session
hook admits display opcodes 18–21 only in the ZX81 board shell and declares
HPS DDR plus `fes.video.session-display`. A held command/response crosses
52.224/74.25 MHz with `zx81_display_cdc` toggle handshakes and stable payload
registers. Execution hold rejects while the session plane is not quiesced;
normal show/return does not use execution hold.

## FES ZX81 machine simulation

`cores/fes-zx81/rtl/zx81_machine.sv` is the first-slice ZX81 extracted from
MiSTer-devel/ZX81_MiSTer `ZX81.sv` at Release 20260603: 16 KB RAM, PAL, no
CHROMA/QS/YM2149/joystick. Keyboard rows and `.p` tape bytes come from the GP
mailbox. Character ROM bytes are `cores/fes-zx81/rtl/zx8x.hex`, converted from
the pinned `rtl/zx8x.mif`. The Z80 is TV80 (`66a131c`) wrapped as `T80pa` with
Sorgelig half-cycle `CEN_p`/`CEN_n` timing, WAIT via CEN gating, and
`TV80_REFRESH`. NMI is sampled every clock, matching T80.vhd. `CEN_p` averages exactly
3.25 MHz from the rational machine clock scheduler.

`make sim-fes-zx81` also runs `Vzx81_machine`, which waits until NEW has built
a display file at `D_FILE` starting with `0x76`, the CPU has HALTed for slow
display, and the ULA has emitted visible pixels. It then types `LOAD ""` on
the 40-key matrix (J, SHIFT+P, SHIFT+P, ENTER) and checks that the `$0347`
tape-loader patch consumes a 16-byte `.p`. `LOAD ""` always hits that
patch: a committed mailbox blob is copied into RAM; with no blob the
patch sets carry immediately so BASIC reports `0/0` instead of hanging
in the original cassette waiter with the display off. A 720p raster module
`zx81_video_720p.v` integer-scales the 6.5 MHz capture into 1650×750 timing.
This is simulation, not a Quartus RBF or kit result.

`make sim-fes-zx81-session` combines the real Z80, machine RAM, capture,
mailbox, shared DDR guards and scanout with unrelated system/pixel clocks.
A diagnostic firmware retains a RAM marker and continuously updates another
RAM cell while the test opens, replaces frames, closes and reopens the UI.
The test verifies frame-completion ACK, shared HDMI timing, both slots, neutral
keys, live cassette begin/commit/eject, busy rejection, stalled-response drain,
hidden-underrun recovery and display-fault return without execution reset.
Hard-fault close and execution hold remain blocked while controller responses
or queued guard commands are stalled; both succeed only after physical drain.
The existing BASIC and expansion simulations remain part of `make sim-fes-zx81`.

## FES ZX81 Quartus bring-up

`make build-fes-zx81-quartus` is the Quartus Prime Lite 17.0.2 recipe for
`fes.zx81` 1.2.0 oracle package. It is not a Mistral/nextpnr payload
and is not the standard socketed package. The board shell
`cores/fes-zx81/rtl/top.v` uses two `altera_pll` cells from the 50 MHz V11
reference: 52 MHz system (T80, ULA, mailbox) and 74.25 MHz pixel (HDMI
1650×750). HDMI RGB/HS/VS/CLK pins and U10/AA4 match FES Pong. The HPS I2C
cell is at `HPSINTERFACEPERIPHERALI2C_X52_Y60_N111`; `out_clk`/`out_data`
pull SCL/SDA low and `scl`/`sda` read the pads (Quartus assign-to-Z in place
of Pong's `MISTRAL_IO`). The Z80 is VHDL T80pa from ZX81_MiSTer Release
20260603; Verilator keeps TV80.

The compile defines `QUARTUS=1`. ROM, 16 KB RAM and the 16 KB media blob
instantiate `zx81_dpram`. Its Quartus branch is `altsyncram` bidirectional
dual-port M10K: the address is registered, the output is unregistered, and
a same-port write returns `NEW_DATA_NO_NBE_READ`, which is one clock of
read latency. ROM init is `zx8x.mif`. Simulation registers `q_a` and `q_b`
on that clock and uses `zx8x.hex`, with the same one-cycle read. The 720p
capture buffer is a one-dimensional M10K array
written on `clk_sys` and registered on `pixel_clk`.

The recipe requires `QUARTUS_ROOTDIR`, version 17.0.2, a clean checkout and
tracked inputs. It writes canonical `build/fes-zx81-quartus/build-inputs.json`
before compile, embeds that record's 128-bit id as `BUILD_ID`, runs
`quartus_sh --flow compile top`, requires TimeQuest multicorner
Setup/Hold/Recovery/Removal/Minimum Pulse Width worst-case slack ≥ 0 with
the 52 MHz system clock and the derived 74.25/74.27 MHz pixel clock named,
then seals `manifest.toml` + `core.rbf` through the existing format-2
exporter. Failed compiles delete the RBF, manifest and passing summary.
The command never programs hardware. Quartus remains the kit-proven
bring-up lane.

## FES ZX81 OSS package

The HIP bus diagnostic writes disposable cart outputs to
`build/zx81-bus-validation-cart-diagnostic/`. Sealed validation-cart archives
live separately under `build/zx81-bus-validation-cart/<recipe-sha>/` and survive
diagnostic cleanup.

`make build-fes-zx81` is the Yosys/nextpnr-mistral recipe for the same
`fes.zx81` 1.5.0 package. It authenticates the scoped ZX81 expansion-bus tools, writes
`build/fes-zx81-oss/build-inputs.json` before synthesis, and embeds that
record's 128-bit id as `BUILD_ID`. Synthesis is `synth_intel_alm` with
M10K allowed and DSP/MLAB forbidden. The machine ROM is `zx81_rom_link`:
eight empty BEL-locked 1024×10 lanes at `MISTRAL_M10K.5.73.0` through
`MISTRAL_M10K.5.80.0`, each with a synchronous read (`CFG_ASYNC_READ=0`,
live `CLK1`, `B1EN` held high). The bank select is registered on that same
edge, the lane mux is combinational, and the output register is the second
stage, so the CPU-facing byte is two system clocks behind the address.
`zx8x.hex` is not a package input; launch splices the low 8 KiB with
`link_static_rbf.py init --machine`, and the ROM map requires
`expected_async_read=0`. The 1 KiB shell RAM and the 16 KB media blob use
`zx81_dpram`'s registered one-cycle read, which Yosys maps to synchronous
M10K. The 16 KB pack and the QS character board instantiate synchronous TDP
M10K (`CFG_ASYNC_READ=0`) and delay the bank select, or ROMCS and DSEL, with
that read so one response word describes one request. The shell producer
rejects every async M10K in `synth.json` and `routed.json`. The
validation-cart and QS producers reject every async M10K in `cart.json` and
`cart-routed.json`. The 720p capture buffer is a dual-clock M10K SDP with
its read registered on `pixel_clk`. The Z80 is Verilog T80pa/TV80.
HDMI I2C uses Pong-style `MISTRAL_IO` open-drain pads at BEL X52/Y60
(`QUARTUS` is not defined). Place-and-route uses `constraints-oss.qsf` and `clocks-oss.sdc`.

The session plane reuses `fes_menu_control`, `fes_menu_reader` and
`fes_hps_ddr`. `fes_menu_video` selects its optional external-raster and
complete-frame mode: ZX81 remains the only 1650×750 raster owner, and a
submission completes only after every pixel of one frame arrived on time.
`zx81_session_display` keeps the first launcher frame hidden, then selects
its RGB at the next frame boundary. Closing waits for issued DDR responses to
drain and returns machine RGB at a frame boundary before acknowledging. A
visible underflow contains the display and restores machine pixels; it never
reprograms the FPGA or resets CPU/RAM/ROM, expansion, capture or audio. Unexpected
PLL holds contain the DDR guard and disable further presentation for that
load. Faulted close requires synchronized guard hold plus a physical port-drain
proof covering queued commands, unfinished writes, owed reads and response
pipeline registers. The reader cannot restart after its responses were hidden.
If the pixel clock stops permanently, this proof and the close handshake cannot
advance; software cannot safely acknowledge or automatically reprogram that
load. Existing idle-menu scanout retains its original defaults.

The locked ZX81 Yosys library predates the FPGA-to-HPS DDR atom declaration.
The producer reads `fes_hps_ddr_atom.v` with `read_verilog -lib`; this complete
port declaration is a tracked, pinned functional input, with its upstream
source recorded in the file. The original compiler/socket/ROM pins remain.
Both synthesized and routed graphs must match the boot DDR layout, tie all
writes and unused ports low, and retain synchronous M10K reads for the DDR
FIFO. A new sealed shell and matching expansion archives require fresh
three-clock timing and exact-artifact kit qualification.
The QSF omits Quartus `HPS_LOCATION`; the SDC constrains only the 50 MHz
reference and nextpnr derives the PLL outputs. The Quartus files keep
`HPS_LOCATION`, `derive_pll_clocks` and asynchronous clock groups.
The independent ZX81 cart producer reloads an already routed shell with
`--no-pack`, so it writes a separate generated SDC that explicitly constrains
`clk_sys` to 52.224 MHz, `pixel_clk` to 74.25 MHz and `audio_clk` to 12.288 MHz.
Its recipe records those requirements and the SDC digest. Publication requires all three clocks to meet
their nominal and reported constraints, with only the existing picosecond
quantization tolerance when identifying the reported frequencies. This does
not change the sealed base shell or infer requirements from achieved Fmax.
For frozen replay, the cart producer makes an authenticated copy of the routed
shell and restores the system PLL's second physical output to its audio clock
net, dropping the obsolete scalar `outclk[0]` pin-map alias. The sealed routed
shell bytes remain unchanged.
Socketed shells export bus 2.0 (46-bit request, 20-bit response): CPU clock
and /RESET append to the original packing. Map `fes.zx81-bus.socket/2` has
unchanged CRAM bounds. Linker/host/runtime preserve matching historical v1
assets and admit current v2 only against its exact compatible shell. Vacant response FFs hold 0, so ROMCS/WAIT/DSEL/RAM_PRESENT are
active-high from the cart. CPU writes on that edge use TDP `A1WE` like the
validation cart; mixed-width `A1EN`/`A1BE` decoded but did not hold `POKE`/`OUT`.
Cart M10K keep a distinct top clock port (`FPGA_CLK1_50`) so
`--fes-slot-clock clk_sys` can splice the inferred IB onto the shell 52.224 MHz
net; naming that port `clk_sys` leaves M10K on the pad output. During `/RFSH` the shell presents the ULA character-ROM address as the QS
`8400–87FF` window so the same 1 KiB cell supplies glyphs. QS power-up
loads Sinclair glyphs 0–63 (ROM `1E00–1FFF`) into that cell so boot text
is readable; CPU writes still replace rows. The shifter loads a
registered `rfsh_chr` hold that keeps the first ROMCS byte; `/RFSH` is
not muxed into `cpu_din` (that loop stopped the FES GP mailbox). The validation
cart is the first consumer on that edge; Zon X and QS Character Board RTL share
the plugs. The cart producer accepts `--cart zonx` to seal the original write-only
AY8912 board under `build/zx81-zonx-cart/<recipe-sha>/`; QS is not a library asset.
`zonx.v` handles physical decode/clock/reset and one event per CPU write;
`zonx_ay.v` supplies three full tones, noise, envelope shapes/restarts and
masked registers. Its unsigned 8-bit summed sample on `peek_d` feeds shared
PCM/I2S, duplicated into stereo when `RAM_PRESENT` is 0. The nominal 3 dB DAC
is quantized to 85 units per channel. It does not model the analog card.
`zx81_machine_clock.v` schedules exact average 6.5 MHz ULA / 3.25 MHz CPU
rates from the existing 52.224 MHz transport with phase error below one cycle.
The oscillator schedule continues during reset. The cart halves the edge CPU
clock to 1.625 MHz. Hold/PLL loss mute shared output; edge reset clears the AY.
Sources and fidelity limits are in FES [ZX81 expansion bus](../../../docs/zx81-expansion-bus.md).
Diagnostic 904–907 remains an HPS bench and does not seal
`fes.zx81`. The cart route also receives the fixed `fes.zx81-bus.socket/2` CRAM rectangle
`1769,32,2806,7024` (exclusive upper bounds). The scoped compiler queries Mistral
for each routing mux's physical configuration bits; nominal wire/tile locations
do not determine those bits for long wires. Existing shell pip selections remain
fixed, and new muxes must fit the rectangle, including shared-net branches.
Outside-rectangle frozen muxes and their upstream paths also survive orphan
cleanup after vacant response inputs are detached. The compiler retains their
original net ownership and rejects missing or changed protected selections
before emitting the RBF; unused in-socket branches can still be removed.
Surviving cart constant inputs use local slot LUT drivers after control folding,
so they do not depend on extending distant shell constant trees. Publication
still compares the complete emitted header and CRAM against the original sealed
shell and refuses any non-CRC change outside the fixed rectangle.
The shared Go linker validates canonical frames directly in their encoded
column order, including all padding, first/last markers, EDCRC and outer CRC16.
The fixed socket spans every payload row (32 through 7023), so linking can copy
whole validated columns without repeatedly transposing the full CRAM bit matrix.
Outside columns must match except for the existing named CRC companion strips;
those strips retain the base shell's bytes. CRC16 uses a table checked against
the original bitwise algorithm. Golden Python output, a reference bit overlay,
malformed frames and compressed/uncompressed input ownership tests preserve the
previous byte and admission contracts.

On the designated ARM kit, the exact sealed shell and validation cart measured about
59.7 seconds for staging and 60.2 seconds for restart adoption with the original
transposing implementation. Direct frame validation reduced those measurements
to 7.9 and 7.4 seconds, with identical linked bytes and composition identity.
This is a host/target validation benchmark, not FPGA hardware acceptance; it
does not extend request deadlines or bypass target recomposition.
The system/audio PLL derives 52.224 and 12.288 MHz from the 50 MHz reference.
Place-and-route uses the deterministic seed order
10, 5, 12, 2, 7, 1, 3, 4, 6, 8, 9, 11, 13, 34. A flip-flop with no async
clear must not stay on a LAB clear another flop uses; nextpnr `c2bb4363`
assigns the unused ACLR slot and the dedicated inactive clear when a frozen
LAB snapshot is reloaded. A user-BEL socket flip-flop on a fresh route gets
LUT pin reassignment and a data route-through. A scaffold reload locks that
LAB and leaves the restored pin map in place. For each seed it tries heap
timing weights 300 then 1000, then sweeps the same seeds at weights 2000, 100
and 10 if needed (at most 70 attempts, stopping at the first passing route).
The build record seals
the effective weight order and budget. This fallback handles placement-sensitive
netlists without changing the clock requirements. The recipe uses
criticality exponent 5 and `--router gpu`, nextpnr's connection-based
router with a pure-delay timing-repair phase (merged PR #66; the
repository toolchain builds it without a GPU and its host backend
produces the same routing a GPU would). `--timing-allow-fail` permits an early
estimate to miss while the recipe checks final signoff and records the first
passing seed. `make build-fes-zx81 BEST_FMAX=1 GPU_DEVICES=1` keeps that synthesis and
searches weights 10/100/300/1000/2000 plus remaining seeds for the best
Fmax; the selected seed and weight go into route evidence. This keeps
synchronous M10K address paths within the 52.224 MHz system constraint. The recipe
requires two `altera_pll` cells (combined system/audio and 74.25 MHz pixel).
Also required: the HPS GP mailbox, the I2C bridge, and at least one M10K.
It seals the format-3 package with its ROM map only when system, pixel and
audio clocks meet their constraints. The command never programs hardware.

The repaired 1.3.0 package passed the
[2026-09-30 exact-package kit 1 diagnostic](../../../docs/validation/2026-09-30-zx81-shared-audio-hil.md):
ROM-backed library launch, GP/package identity, video, vacant-socket audio
silence and Stop. Its matching RAM validation cart passed all three clock
constraints and changed zero CRAM bits outside the socket. Factory-image
acceptance and audible Zon X qualification remain separate work.

A sealed OSS package has been used for a **hardware diagnostic** on the
designated kit (BASIC, sofa keyboard, empty `LOAD ""` → `0/0`, committed
`.p` → `10 PRINT "OK"`). That is not exact-artifact hardware acceptance
and does not inherit the Quartus bring-up result (the diagnostic used TV80
and the former registered-M10K workaround). A GPU-routed package of that
same registered-M10K recipe base (nextpnr 9c751533, misteross 9ad19189)
also booted to the ZX81 editor on the kit on 2026-09-12 and answered
`PRINT` + NEWLINE with `0/0` through the host keyboard route. The native
async-M10K recipe that followed, since replaced by synchronous reads, failed
to boot on the kit: its sealed packages
`74ef917a` (`--router gpu`, seed 2) and `247e2af4` (unchanged `router1`
control, seed 6, same toolchain) both load, pass signoff and show only a
black 720p frame for 40 s, while the older package re-loaded afterwards
shows the editor within 5 s. Evidence: FES `out/gpu-router-kit-diagnostic/`.
The regression is in the recipe or toolchain, not the router choice.
FogCast library install/launch of that package
is a host concern; this recipe only seals the `.fcore`.

### ZX81 OSS toolchain gaps

These are the Yosys/nextpnr-mistral/Mistral limits the ZX81 recipe currently
works around. A toolchain change that removes a gap should delete the
matching workaround rather than keep both.

| Gap | Observed failure | Current ZX81 workaround |
| --- | --- | --- |
| Combo-read block RAM | `assign q = ram[addr]` with `synth_intel_alm -nolutram` previously became LUT RAM. ABC ran 25+ minutes on an 8 MB XAIG / 23 MB symbol file and did not finish. Yosys maps that combinational read to an illegal `CFG_ASYNC_READ=1` M10K. | `zx81_dpram` registers the read (`ramstyle="M10K"`, one clock of latency). ROM lanes, the 16 KB pack and the QS character cell instantiate synchronous M10K (`CFG_ASYNC_READ=0`). Shell and cart producers reject every async M10K. Quartus `altsyncram` registers the address and leaves the output unregistered. |
| SDC subset | `ERROR: Unsupported SDC command 'get_clocks'` on the Quartus `set_clock_groups` / `derive_pll_clocks` file. | `clocks-oss.sdc` is only `create_clock` on `FPGA_CLK1_50`. nextpnr derives PLL outputs. |
| QSF `HPS_LOCATION` | Internal HPS I2C previously ignored the Quartus instance assignment. | nextpnr now converts `HPSINTERFACEPERIPHERALI2C_X52_Y60_N111` to `cyclonev_hps_interface_peripheral_i2c.52.60.0`. ZX81 OSS still also sets the RTL `BEL`. |

What already works in this design, so a toolchain fix should not regress it: two independent `altera_pll` cells on PIN_V11; 8-bit 16 K `m10k_tdp` infers 16 `MISTRAL_M10K_TDP` cells in under a second; Pong-style `MISTRAL_IO` HDMI I2C at X52/Y60.

Verilog T80pa/TV80 is an OSS language choice, not a nextpnr packing gap. Quartus keeps VHDL T80pa.

The format-2 package is `fes.pong` version 1.1.0 and requires
`fes.persistence.words` 1.0 and `fes.pong.progress` 1.0 in addition to gamepad
and fixed video. Base ABI and transport remain 1.0. Data-info opcode 7 reports
[2,1,1,0] for word count, layout tag, major and minor. Persistent words are the
speed enum (default 1) and best rally (default 0). Gameplay reset clears the
current rally but never the restored words. Player-return events increase the
current rally and immediately update best, both saturating at 65535; either
point event clears current. Display scores remain independent and wrap at 9.

Data-control opcode 4 selects freeze/begin/commit/resume with arguments 0–3.
Freeze first holds the game, drains its registered event pulse, then latches
both snapshot words and acknowledges; reads (opcode 5) are available only
while frozen. Repeated freeze retains the original snapshot. Resume releases
freeze without resetting gameplay. Begin requires held gameplay reset and
clears the staging bitmap. Writes (opcode 6) populate two staging words; commit
requires both words and a valid speed before atomically publishing either.
Invalid-speed validation occurs at commit, allowing a corrected staging write.
Commit closes staging and leaves reset held for ordinary gameplay release.
Invalid opcode/index/argument/state leaves live data and control unchanged.
Gameplay hold-reset or release discards unfinished staging; reset also ends
freeze. Volatile launches may release defaults without restoring.

The simulation covers restore [2,17], failed/partial commits, fresh staging,
invalid controls/indexes, reset preservation, speed saturation, and defaults.
The production board-top harness drives actual collision logic at directed
positions to test final-edge freeze draining, immediate unfinished records,
65535 saturation, both point events and unchanged decimal score wrap. These
are digital host checks, not timing or physical acceptance.

`cores/fes-common/rtl/fes_video_720p.v` advances one pixel on every supplied pixel
clock: 1280 active, 110 front porch, 40 positive-sync clocks and 220 back porch
for a 1650-clock line; 720 active, 5 front-porch, 5 positive-sync and 20
back-porch lines for a 750-line frame. It maps a 320x240 game image at 3x scale
into horizontal pixels 160 through 1119, emits black in both 160-pixel side
bars, drives RGB888 plus data enable, and keeps its counters independent of
gameplay reset. `fes_pong_core` connects the existing `pong_game` directly to
that pixel domain with `CLOCK_HZ=74250000` and one frame tick per raster frame.

`cores/fes-pong/rtl/top.v` exports `FPGA_CLK1_50`, `HDMI_TX_CLK`,
`HDMI_TX_D[23:0]`, `HDMI_TX_DE`, `HDMI_TX_HS`, `HDMI_TX_VS` and the
bidirectional `HDMI_I2C_SCL`/`HDMI_I2C_SDA` pins. It instantiates
the HPS GP primitive without fabric SDRAM and clocks the mailbox, gameplay and
raster from the same pixel clock. The GP request toggle remains the only
asynchronous HPS signal synchronized into that domain; accepted reset and button
state therefore reaches the game as one registered vector without an internal
multi-bit clock crossing. If the pixel clock is absent, the initial signature,
ACK-zero, reset and neutral-button state remains visible, but new requests do
not ACK. Activation consequently fails instead of accepting a core whose video
clock is stopped.

The HDMI control path uses `cyclonev_hps_interface_peripheral_i2c` at the
explicit `BEL` site `cyclonev_hps_interface_peripheral_i2c.52.60.0`, connecting
Linux's existing HPS I2C controller to SCL U10 and SDA AA4. Each explicit
`MISTRAL_IO` has constant-zero data, the matching HPS low-enable on OE, and
pad feedback returned to the HPS. This preserves low-or-release behavior
through OSS synthesis; neither line may actively drive high. The source `BEL`
attribute still places the internal hard block. nextpnr also honors QSF
`HPS_LOCATION` for this I2C cell (experiment `850_hps_location`).
Simulation covers all combinations of HPS and external-device low enables,
with digital pull-ups and observable drive intent; it does not model analog
bus timing or replace hardware validation.

Top has the synthesis parameter `BUILD_ID[127:0]`. The standalone build recipe
overrides that parameter with the 32 hexadecimal digits of the build-record ID; identity
indices 8 through 15 expose successive source-order byte pairs with the low byte
first. The all-zero default identifies an unset simulation/build integration
value rather than an accepted artifact.

The test target uses the pinned external Verilator when supplied through
`VERILATOR=...`. Its controllable `board_models.v` drives reference and pixel
clocks with independent phases and exposes the HPS GP boundary so `board_tb.cpp`
can test the production top. It verifies that reference-only clocks cannot
advance the pixel-domain mailbox, then commits multi-bit buttons and a
reset/button-clear vector immediately around frame tick. This model does not
model 74.25 MHz, PLL lock, or hardware. Production `pixel_pll.v` uses the same
checked 50→74.25 MHz single-output fractional-N declaration as
`610_pll_frac_7425`: direct operation, zero phase, 50% duty and
`fractional_vco_multiplier="true"`. Integer mode is not accepted for this
rate. `constraints.qsf` assigns the DE10-Nano 50 MHz input and the ADV7513
RGB888, DE, sync, pixel-clock and I2C pins.

`scripts/build_fes_pong.py`, invoked by `make build-fes-pong`, is the sole
standalone recipe. Its source set is `pixel_pll.v`, `top.v`, `fes_gp.v`,
the shared `fes_video_720p.v` and existing `pong_game.sv`, with the generated ABI include
directory. Yosys receives the build-record-derived 128-bit `BUILD_ID` and
forbids BRAM, LUTRAM and DSP inference. nextpnr targets `5CSEBA6U23I7` with
the first passing seed from 1 through 8, `--router gpu`, the task-local QSF, the 50 MHz board SDC and an
explicit 74.25 MHz target; all outputs stay under `build/fes-pong/`. The route
log must prove a live HIP backend.

Before synthesis, the recipe requires a clean source checkout, checks every
recipe/source/constraint/ABI/lock input is tracked and non-symlinked,
authenticates Yosys, Mistral and nextpnr-mistral against the recipe's expected
commits and `toolchain.lock` through their canonical cache stamps and executable
digests, then writes canonical `build-inputs.json`.
The record uses `scripts/build_fes_pong.py` as its recipe,
`cores/fes-pong/generated/fes_gp.vh` as its tracked ABI definition, and an empty
dependency map because the build is self-contained in this checkout.

Export remains unreachable until the routed JSON contains top, synthesis and
utilization each show exactly one `altera_pll`, one HPS GP primitive and one
HPS I2C primitive, no
forbidden memory/DSP synthesis cell or utilization resource is used, the known
`cyclonev_oscillator` utilization row is present with zero use, and the route
log proves normal completion. Synthesized and routed evidence must preserve the
I2C low-or-release topology and pad feedback; routed evidence must use the exact
HPS site and U10/AA4 pads. Newly reported utilization resources are retained
when their counts are valid and use is zero; unrecognized resources in use are
rejected. Known required and forbidden resource checks remain exact. The single
sequential timing domain must meet its
74.25 MHz pixel constraint. The 50 MHz reference has no sequential Fmax row;
the recipe instead requires the tracked SDC's exact 20.000 ns constraint, its
application in the route log, and identical fixed fractional PLL parameters in
the synthesized and routed designs. After creating the deterministic manifest, the recipe
reauthenticates tools and the clean source before calling the Task-3 exporter. A failed
build retains the pre-synthesis input record and diagnostic reports but removes
the RBF, manifest and passing summary so they cannot be mistaken for an
exportable result. The recipe checkpoint itself has no FES Pong RBF, physical
video result or hardware-support claim.

## FES Apple II

`cores/fes-apple2` is `fes.apple2`, an Apple II+ class home computer on the
`fes.computer` 1.0 mailbox. Its machine, video, Disk II, slot bus and open
diagnostic are described in [its README](../cores/fes-apple2/README.md).
Ctrl-Reset holds the CPU and backplane RESET for the whole key press while
retaining II/II+ video, annunciator and language-card switches. Host execution
reset initializes them. The machine regression installs an alternate reset
vector in language-card RAM and checks both reset paths through the real CPU.
Shared RTL it adds to `cores/fes-common`: the vendored NMOS 6502
`rtl/cpu6502` (Arlet Ottens, module names only changed) and the generic
`rtl/fes_computer_mailbox.v` endpoint, which replays the mister-packages
`computer-exchanges.json` golden exchanges in `make sim-fes-apple2-mailbox`.
The endpoint owns no storage: accepted media words appear on a write port for
their acknowledging clock and `unit0_state` gates presentation of unit 0.

`make build-fes-apple2` (`scripts/build_fes_apple2_oss.py`,
`toolchains/apple2.lock`, `make toolchain-fes-apple2`) seals a format-3
package from a clean committed tree. Its single ROM is `apple2-firmware`,
role `firmware`, 16,384 bytes on the blank column-5 lanes at rows 32–47.
The shell QSF adds the four named `FES_RESERVED_RECT` regions of
`scripts/apple2_slots.py`; the producer requires every socket to hold only
its pinned boundary flip-flops and every firmware destination to fall
outside all four socket CRAM rectangles. It searches seeds 5, 4, 2, 1, 3,
6–10 at HeAP weight 2000 and takes the first route that closes 52.224 MHz
system, 74.25 MHz pixel and 12.288 MHz audio timing. An inferred read-only
memory maps to an M10K without a clock in this toolchain, so the font M10K
is instantiated explicitly (`rtl/apple2_video.v`) and the audio mix is
pipelined. The sixteen firmware lanes and that font use synchronous M10K
reads (`CFG_ASYNC_READ=0`). Each firmware lane registers its address; the
sub-bank and group selects are delayed so the CPU-facing byte is still two
system clocks behind the address that `apple2_machine` captures on `cpu_ce`.
That byte is sampled at `cycle_clock == 16`. The font lane registers its
address on the 74.25 MHz pixel clock and `font_q` is the second stage. The
column sequencer writes `font_addr` at `csub == 4` and loads `next_glyph`
from `font_q` at `csub == 8`, which is still after the glyph is valid. The
ROM-map producer requires `CFG_ASYNC_READ=0`, and the shared netlist check
rejects every async M10K in the shell. The package declares `fes.expansion.apple2-bus` 1.0 optional.

`scripts/build_apple2_slot_card.py` builds one card for one physical slot
against the exact sealed shell and its frozen `routed.json`: the scaffold
renames only that slot's boundary flip-flops to `plug_addr_ff_N` /
`plug_rdata_ff_N` (the other sockets keep their instance names, so nextpnr
finds exactly one plug set), removes that slot's clock-coverage flip-flop,
and reattaches the system PLL's second output as the Coleco card flow does.
nextpnr pass 2 runs with `--fes-cart-region slotN` and that socket's
`--fes-cram-region`; the producer requires the three shell clocks and no CRAM
change outside the socket, then publishes a two-member archive whose
manifest carries `slot_index`. The probe card's `$Cn00` ROM is a synchronous
M10K. Address and IOSEL stay held for the 6502 cycle and the motherboard
samples the slot response at cycle 16, so one clock of ROM latency is inside
that window. The card producer rejects async M10K reads on `cart.json` and
`cart-routed.json`. `expansion/cmd/fes-slot-link` composes any set
of such archives, optionally with the firmware ROM map, onto the shell.

## FES ZX Spectrum

Spectrum's sixteen blank firmware lanes use synchronous 1024x10 M10K reads
on the live system clock, with reads enabled and active-low writes disabled.
The bank selector is registered with the M10K read address; a combinational
bank mux and the existing final data register preserve two-stage latency.
The ROM-map producer explicitly requires `CFG_ASYNC_READ=0`. Synthesized
control ports are checked separately: every lane must share
a live `CLK1`, hold `B1EN` high and active-low `A1EN` high, and keep clears
inactive without secondary-clock or byte-enable connections. Lane BELs and
INIT encoding are unchanged, but each newly built RBF needs its own map/base
hash. The optional `sim-fes-spectrum-rom` target exercises the production
branch with the selected Yosys `mem_sim.v` and nonzero linked-image contents;
the normal Verilator branch alone does not test primitive wiring.

`cores/fes-spectrum` is `fes.spectrum` 0.2.0, a 48K ZX Spectrum on the same
`fes.computer` 1.0 mailbox as the Apple II. The machine, ULA port `$FE`,
built-in Kempston port, `.tap` player and four edge sockets are described in
[its README](../cores/fes-spectrum/README.md). It reuses
`rtl/fes_computer_mailbox.v` with `ENABLE_SPECTRUM_TAPE` and selects the
first-party NMOS CPU at native 3.5 MHz cadence by default. The explicit
`--cpu fast` producer / `make build-fes-spectrum-fast` development lane uses
the documented-only transaction CPU at 56 MHz, a registered memory/socket
bridge and independent 3.5 MHz peripheral ticks. WAIT holds phase four until
readiness and selected read data are captured together; phase five delivers
the captured byte to the CPU. Internal writes commit once at that delivery,
while socket STROBE launches once. One inactive clock rearms edge consumers.
Both modes retain the ROM map and frozen 32/28-bit socket ABI. Normal mode
uses system/audio and video PLLs. Fast mode uses system and video PLLs, with
the audio serializer running on the 56 MHz system clock. Rational enables
produce 48 kHz stereo frames and an average 12.288 MHz MCLK pin waveform;
MCLK half-periods are two or three system clocks, and BCLK half-periods are
nine or ten. These outputs do not clock fabric logic. The fast timing gate
checks the actual 56 and 74.25 MHz domains. CPU mode, clocks, audio schedule
and resource expectations enter build identity. No Sinclair ROM bytes are in the tree.
The cassette player selects its pilot length from flag bit 7 and prefetches
the next byte so each encoded half-pulse keeps its standard ROM width.
`make sim-fes-spectrum-tape`, included in the aggregate, decodes all pilot,
sync and data edges across mixed bytes and consecutive blocks, then checks
partial tails and live eject/replacement.

`make build-fes-spectrum` (`scripts/build_fes_spectrum_oss.py`,
`toolchains/spectrum.lock`, `make toolchain-fes-spectrum`) is the format-3
seal. Its ROM is `spectrum-firmware`, 16,384 bytes, on the same blank column-5
lanes at rows 32–47. The shell reserves the four `FES_RESERVED_RECT` regions
from `scripts/spectrum_slots.py` (`fes.spectrum-bus.sockets/1`, sockets 1–4).
Socket validation admits a compiler-inserted `MISTRAL_BUF` only as the
verified `$ROUTETHRU` companion of an already pinned boundary flip-flop.
The shared `coleco_expansion` check requires the paired COMB/MCOMB half,
exact physical pin map and ports, and a dedicated buffer output connected
only to that flip-flop's data input. Clock-coverage outputs must remain
unused, and each effective boundary data input must have exactly one driver
or a direct defined constant. Older clock-only anchors may have disconnected
data inputs. Every other shell cell inside a socket is rejected. The helper
is a pinned functional source input; socket placement and CRAM fences retain
their existing identities.
Simulation is `make sim-fes-spectrum`; its turbo regression measures register,
RAM and expansion-I/O workloads and tests WAIT, ROMCS, pending NMI and native
frame timing in both modes. Every clock must close before either shell seals.
The previous TV80 shell seal is recorded in
`docs/validation/2026-09-28-spectrum-pathfinder-seal.md`. A kit link of the
48K BASIC ROM is recorded in
`docs/validation/2026-09-28-spectrum-basic-kit.md`. Probe cards, keyboard
checks, and tape checks remain open.

## FES Commodore 64

`cores/fes-c64` is `fes.c64` 0.1.0, a package-only Commodore 64 pathfinder on
the same `fes.computer` 1.0 mailbox as Apple II. The machine contract is
[its README](../cores/fes-c64/README.md). `make sim-fes-c64` boots the open
diagnostic: RAM, firmware signature, VIC text, both cartridge sockets,
joystick, keyboard, a SID sample and a read-only D64 LOAD of `BOOT`.
Both CIA timers expose their live counters, load stopped counters on high-byte
writes, treat force-load as a strobe, and implement continuous/one-shot counting.
Timer B can count Phi2 or timer A underflows. CIA1 asserts IRQ and CIA2 asserts
NMI. The diagnostic checks both CPU vectors, including NMI with IRQ disabled;
`make sim-fes-c64-cia` adds directed register/timer coverage. External CNT edges,
TOD, serial shifting and timer port outputs remain outside this slice.

`make build-fes-c64` (`scripts/build_fes_c64_oss.py`, `toolchains/c64.lock`,
`make toolchain-fes-c64`) is the format-3 seal. It is not run as part of this
pathfinder slice, and it does not pin synthesized M10K totals. The ROM is
`c64-firmware`, 16,384 bytes, on the blank column-5 lanes at rows 32–47.
Those lanes use synchronous M10K reads (`CFG_ASYNC_READ=0`) with the bank
selector delayed one clock so the CPU-facing byte is still two system clocks
behind the registered address. `c64_machine` samples that byte at
`cycle_clock == 16`, sixteen 52.224 MHz clocks after the phi2 address
capture. The ROM-map producer requires `CFG_ASYNC_READ=0`, and the shared
netlist check rejects every other async M10K. Color RAM is a synchronous
dual-port M10K, with independent system and pixel clocks; the CPU samples its
registered read at cycle 16 and the VIC aligns its result with main RAM.
`scripts/build_c64_slot_card.py` builds one card for socket 1 or 2 against a
frozen shell. No Commodore ROM is in the tree, and the core is not in the
factory image.

## FES Atari 520ST

`cores/fes-atari-st` assembles the original ST's full FX68K 68000 and
functional chipset in `st_system.sv`. Fractional alternating phase enables
run an average 8 MHz CPU in the 52.224 MHz system domain. `st_machine.sv`
latches big-endian bus transactions, applies supervisor protection and the
ROM-vector alias, rearms between TAS strobes, and implements the original
MMU's RAM-sizing address aliases. One physical 512 KiB bank is populated;
configured bank 1 reads all ones. A disconnected cartridge is acknowledged,
while unclaimed MMIO has a bounded bus-error timeout.

`st_io.sv` connects MFP IRQ6 vectors and timers, functional native VBL/HBL
autovectors, keyboard/MIDI ACIAs, original IKBD protocol logic, shared YM2149
sound and WD1772/ST DMA. HID rows settle for 1 ms before keyboard translation.
CPU RESET resets the devices. The optional expansion exports a wide stable
request and response through pinned registered boundaries in
`fes.atari-st-bus.socket/1`; the existing Go linker admits only socket 1 and
confines its CRAM writes to the shared rectangle.

`st_memory.sv` fairly arbitrates CPU, video, floppy DMA and both media paths
over the existing addon SDRAM controller at 52.224 MHz. That controller now
supports optional byte masks, initialization status and idle refresh while
preserving full-word behavior for callers that disable masks. Chip DQM shares
A11/A12 on the MiSTer addon, so the controller preserves row bits for ACTIVATE
and establishes masks on those shared pins two fabric clocks before WRITE.
Reads clear both masks. Warm CPU Hold
leaves memory and uploads running. Withdrawn requests drain without stale
acknowledgements.
The exact 720 KiB disk buffer is disjoint from the 512 KiB RAM, and the
big-endian media adapter handles arbitrary odd chunk boundaries before the
mailbox acknowledges a write. `fes.media.atari-st-floppy` 1.0 adds capability
bit 7 to the existing computer ABI, without changing its framing/opcodes.
The writable mailbox rejects Begin and Eject during sector collection or an
accepted commit, even for volatile disks. The sector writer drains before an
explicit later replacement can upload through the shared media arbiter.
`make sim-fes-atari-st-media-lifecycle` exercises these actual components with
delayed RAM and media completions, including rejected mutations and later retry.

`st_video_adapter.sv` uses held-bundle handshakes for frame configuration and
owned double line caches between system and 74.25 MHz pixel clocks. Low,
medium and monochrome rows advance through native row/repetition counters.
Fixed per-mode fetch windows select coordinates after constant arithmetic.
Synchronous cache reads and ownership tags are captured together; a second
pixel register selects the validated bank at the original plane-capture edges.
Displays feed shared RGB888 output through two registered boundaries. The
optional frozen raster socket admits independently sealed Direct/Scanlines
archives bound to the exact shell; an empty socket uses built-in Direct with
the same latency. Underflow blacks a whole affected line and later
lines recover; stale fills cannot cross a frame configuration change.

`make sim-fes-atari-st` runs the original CPU firmware and focused device,
SDRAM-command, dual-clock-video and complete-media tests. Optional EmuTOS
boot tests use a pinned official 1.4 192 KiB US image, supplied separately
from the blank firmware socket. FX68K source bytes remain immutable; a
compiler compatibility input may normalize its two simulation-only legacy
translate-off/on comments without changing RTL assignments or microcode.
The board producer must qualify the locked Slang/Yosys frontend, HIP route,
all clocks and sealed ROM/socket maps. Host simulation does not confer kit
or appliance hardware acceptance. See [the core README](../cores/fes-atari-st/README.md)
for maps, commands and the remaining original-ST timing/device limits.

## FES RISC-V

`cores/fes-riscv` places the original first-party RV32I CPU
(`cores/fes-common/rtl/riscv`: `fes_rv32_cpu`, `fes_rv32_csr`, `fes_rv32_alu`)
on the shared `fes.application` 1.0 shell with `fes.gamepad` 1.0 and the
fixed 720p raster. `fes_riscv_system.sv` owns the bus: 32 KiB of byte-lane
M10K RAM at 0 initialised from the checked-in firmware lane images, a
160x120 RGB332 framebuffer at `0x1000_0000` read by the raster through the
lanes' second port, and the I/O block at `0x2000_0000` (buttons, frame
counter, 64-bit `mtime`/`mtimecmp` timer interrupt, vertical-blank flag with
an optional external interrupt, identification and software interrupt).
Other addresses fault. The CPU, memories, raster and I/O share the 74.25 MHz
pixel clock; there is no second clock domain and no DDR.

The firmware is hand-written RV32I assembled by `cores/fes-riscv/firmware/assemble.py`
into `firmware.hex` and four lane images. Source, assembler and images are
pinned producer inputs, and `scripts/build_fes_riscv.py` re-assembles the
source and refuses images that differ. The producer authenticates
`toolchain.lock`, synthesises with M10K enabled (MLAB, DSP and the HPS SDRAM
bridge forbidden), runs a bounded first-pass HeAP seed search that stops at
the first placement meeting 74.25 MHz, validates the shared board, PLL and
I2C evidence and exports a format-2 package (`fes.riscv` 0.1.0, profile
`fes-gp-v1`). Output is `build/fes-riscv/`. `make sim-fes-riscv` covers the
CPU against an independent model, the firmware images, the system through
its HDMI pixel stream and the board shell through the mailbox. The CPU
contract is [its README](../cores/fes-common/rtl/riscv/README.md); the
machine contract is [the core README](../cores/fes-riscv/README.md). FES
registers the recipe as package-only; the factory image does not select it.

## Shared native kit client

`scripts/kit.py` is a thin operator client of FogCast's target lease and native
RBF upload APIs. It retains one in-memory lease during an interactive session,
renews every 20 seconds, streams regular RBF files with an explicit bounded
length (1 byte–32 MiB), and releases on exit. A development-RBF `CORE_TIMEOUT`
after programming keeps that lease so the operator can inspect a non-MiSTer
image before Stop. Stop follows FogCast's development reboot handshake when
the target reports `reboot_required`: it records `boot_id`, posts
`/v1/development/reboot`, and waits for a new boot ID and a free lease. A
successful reboot ends that lease because the target agent restarts. Release,
EOF, or Ctrl-C Stops first when a development image was loaded, so that
handshake runs instead of a raw release after a non-MiSTer bitstream. It stores
no credentials or lease database. FogCast remains authoritative for expiry,
takeover, serialization and cleanup; libmister-runtime performs the physical
transition. Experiment loads are in
[OSS place-and-route testing](oss-pnr.md#loading). Described-core kit use is
the FES [kit sharing](../../../docs/kit-sharing.md) lease, not a producer
step. Direct `make program` remains
a maintenance bypass outside this protection, and compilation never acquires a
lease.

## Core package boundary

`scripts/core_package.py` is the host inspector for format-2/3 package directories
and `.fcore` archives. Its public Python API is
`read_package(path: Path) -> CorePackage`,
`encode_manifest(fields: dict) -> bytes`, and
`package_identity(manifest: bytes, payload: bytes, rom_map: bytes | None = None) -> str`. `CorePackage` exposes
the original `manifest_bytes`, parsed `fields`, bounded `payload_bytes`, and
`package_id`. The inspector validates every manifest field and payload digest;
an unknown well-formed ABI remains inspectable. A format-2 directory has exactly two
regular non-symlink entries. Its archive is at most 33 MiB and is exactly two
canonical uncompressed POSIX ustar regular-file members, manifest first, with
zero member padding and exactly two final zero blocks. Alternate paths, links,
extensions, extra members, base-256 sizes and trailing bytes are rejected.

Format 3 adds an explicit required `[rom]` declaration and a third sealed member,
`rom-map.json`, after `core.rbf`, with an archive bound of 65 MiB. Other manifest
restrictions and canonical header rules remain the same. `CorePackage` exposes
optional `rom_map_bytes`, and `package_identity` accepts those exact bytes as an
optional third argument to select the format-3 domain. The inspector validates
map digest, size, exact JSON structure, integer coordinates, duplicate keys and
destinations, complete source coverage, and payload/source-size bindings. It
performs no FPGA frame decoding; the launch linker checks blank destinations.
The authoritative contract and fixture corpus live in mister-packages
`schema/core-bundle-v3.json`, `schema/rom-map-v1.json`, and
`testdata/core-bundle-v3`. ROM uploads cannot provide a map.

The exporter opts in through `export_package(..., rom_map=path,
rom_id="machine-rom", rom_role="firmware")`, which promotes a supplied format-2
manifest to format 3 with the map's declared source size and exact digest/length.
Alternatively supply an already encoded format-3 manifest and `rom_map=path`.
The CLI equivalents are `--rom-map`, `--rom-id`, and `--rom-role`. The map is
snapshotted, validated, sealed read-only and compared during reuse alongside the
manifest and RBF. ZX81 now exports format 3; the other production producers
continue to export format 2.

Repository URI syntax uses the host-only vendored
`rfc3986-validator` 0.1.1 module from
`https://github.com/naimetti/rfc3986-validator`, followed by the manifest's
lowercase `https://` and no-literal-userinfo authority policy. The unchanged
vendored module is `scripts/rfc3986_validator.py`, SHA-256
`95fc6d48642f111952b25c040947765bccba669210c8c140b7ed9647fd7e470c`.
Its MIT terms are retained in `scripts/rfc3986_validator.LICENSE`, SHA-256
`94e53eb4b94a5d33a7e66b0abb143ee95f4ec96f36ea4c54794ae6a46e624f04`.
This adds no installed Python or target dependency. Manifest parsing and
pre-synthesis build-record encoding share the same validator.

`scripts/export_core_package.py` provides
`export_package(manifest: bytes, payload: Path, destination: Path) -> Path`.
The destination is a package-store directory. Export derives the package ID from
the exact manifest and payload bytes, then publishes the read-only directory,
matching `.fcore`, and external `.build-inputs.json` with no-replace atomic
renames. A pre-existing result is reused only after all three outputs and their
permissions are verified byte for byte. The old format-1 raw-core bundle
exporter and RBF selector have been retired.

Normal FES producers emit canonical functional build records with `format: 2`.
Records retain repository/revision/source-path provenance while the functional
identity excludes those three fields. It includes the guarded tracked source
closure (`source_roots`, `source_inputs`), recipe and ABI inputs, dependencies,
authenticated compiler identities and controlled execution digest. A docs-only
commit can reuse an artifact without rewriting its original manifest or record;
changed scripts, shared RTL or execution inputs change its functional identity.

The producer authenticates the exact tracked `sources/misteross` FES module,
uses controlled compiler execution, and rechecks source/tool inputs before
sealing. `encode_build_record` validates canonical evidence and `build_identity`
derives the manifest/RTL identity from the functional projection. Current
explicit Quartus/oracle and splash firmware records retain their separately
specified diagnostic schema; they are not an alternate normal product route.
Historical source-record verification checks original immutable provenance
without admitting arbitrary live standalone producer roots.

The FES factory/catalog recipe for `fes.ramtest` selects the OSS 100 MHz
variant through `build_fes_ramtest.py`. Its parent-facing authentication and
functional-record defaults match that rate; explicit 130 MHz builds retain
their separate output and timing gates. Quartus remains an oracle.

## Shared FPGA producer and RTL ownership

Build entrypoints remain per core: `scripts/build_fes_pong.py` and the
ZX81/Coleco/SMS/SG-1000 OSS producers own their recipes and package metadata.
`scripts/fes_build_common.py` owns authenticated tools, clean input checks,
controlled tool invocation and shared evidence primitives. Tool invocations
explicitly name their output directory. `scripts/fes_de10nano_evidence.py`
validates the fixed video/audio board profile with resource expectations supplied
by Pong or the application demo; those policies are not universal core limits.
The demo imports these shared modules directly rather than importing Pong.

`cores/fes-common/rtl` owns the CPU wrapper and TV80 implementation used by
Coleco, SMS and SG-1000, their shared PLLs, RAMs, TMS9918 video path and legacy
simple-computer mailbox. Existing HDL module names remain unchanged. TV80 retains
its embedded MIT notices and pinned upstream attribution; other moved files
retain their original SPDX notices. Core-specific machines, reset ROMs, diagnostics,
and constraints remain in their existing directories. The Coleco OSS lane and
SG-1000 share `toolchains/registered-memory.lock`; its historical Coleco-specific
comments are preserved to keep the authenticated lock bytes and compiler cache
identity unchanged. SMS uses `toolchains/fes-sms.lock` so its router pin can
move without changing that shared slot. The different repository-wide
`toolchain.lock` remains separate.
Per-core generated simple-computer headers remain checked against mister-packages;
consumers use their own generated include directory.

The original shared Z80 implementation lives in
`cores/fes-common/rtl/z80`. `fes_z80_nmos` uses the shared instruction engine
and original pin-cycle adapter; `fes_z80_fast` selects documented instructions
and a direct request/completion interface with timing-only cycles removed.
The [CPU contract](../cores/fes-common/rtl/z80/README.md) describes public ports,
behavior references, undefined-encoding policy and qualification limits.
`make sim-fes-z80` exercises both variants and their independent ALU/bus tests.
The opt-in external-vector runner can also exercise the complete NMOS wrapper,
checking instruction T counts and ordered bus accesses against locked JSON
data; its software oracle and simplified strobe observations do not establish
physical-chip equivalence.
The contained `scripts/benchmark_fes_z80.py` diagnostic targets Cyclone V and
records routed timing from exact source/tool snapshots without producing an
RBF. Its optional `--require-target` timing gate fails if any selected seed
misses the requested clock, after preserving the reports. The CPU contract
records the selected 56 MHz route and the limits of its 16-times clock ratio.
SG-1000 and Spectrum now select the NMOS variant; Spectrum has a separate
documented-fast development producer. Coleco, SMS and ZX81 retain their existing
CPU selections. The synthetic raw-pin replay self-test is included in
`make sim-fes-z80`; operator-supplied genuine-chip captures use the same
strict half-edge comparator. No physical-chip equivalence is inferred from
software vectors, synthetic waveforms or a timing-passing FPGA build.
Shared RTL and producer changes enter the conservative functional source closure.

The source closure includes the shared helper files and `cores/fes-common`.
Moving these files changes authenticated source paths and therefore changes v2
build identities even though the HDL bytes are unchanged. Old packages retain
their original manifests and source provenance; this organization does not confer
routed-RBF or hardware acceptance on newly built packages.

Simulation Makefile targets probe whether Verilator recognizes the optional
PROCASSINIT warning category before suppressing it. CPU simulations narrowly
suppress BLKSEQ for the existing TV80 implementation, consistent with SMS and
SG-1000; other warnings remain fatal.

Pong, ZX81, Coleco, SMS and SG-1000 board tops name their endpoint instance `gp_mailbox`
to avoid the SystemVerilog `mailbox` keyword collision in Verilator 5.032.
Module names and wiring remain unchanged. This input change also requires a
new artifact identity and qualification.

All normal command-line producers require identity 2 and the tracked FES module.
Synthesis-only diagnostics remain unsealed; Quartus oracle records retain their
separate evidence schema.

## Native menu display

`python3 scripts/sim_fes_menu.py` checks exact frame length, burst bounds,
stalls, pixel ordering, cancellation drain, switching and underflow recovery.
The shared DDR wrapper owns port wiring and holds; menu scanout adds no second
bridge or memory reservation policy. The described menu package is selected
by the FES native image; exact image and kit acceptance are tracked separately.

### Menu scanout diagnostics

`cores/fes-menu/` has fixed 1280×720p60 scanout with a bounded read-only
128-bit burst reader and a synchronous M10K FIFO. The DDR-free diagnostic
feeds this reader with a local pattern responder; the DDR diagnostic adopts
`fes_hps_ddr` port 0 at the pixel clock and uses the generated window base.
Both are diagnostics, without a GP menu identity or a described launch package.

`build-fes-menu-pattern` and `build-fes-menu-ddr` use the authenticated HIP
producer with GPU 0, closed functional inputs and separate output directories.
The DDR mode uses the shared `toolchain.lock` HIP compiler slot; its artifact gates
check layout constants in both netlists, fixed pixel timing and inactive
writes/unused ports. Neither producer programs hardware or changes image inputs.
The runtime presenter and exact DDR scanout acceptance remain later work.

### Described menu display producer

`build-fes-menu-package` produces separate format-2 `fes.menu` 1.0.0 firmware
with required fixed video, HPS DDR and `fes.video.menu-display` 1.0, no playable
system identity. FES now selects it as the native image's idle display. It selects `toolchain.lock`
and authenticates the congestion-fixed nextpnr pin. The producer uses shared
board/electrical/provenance helpers; GP is required explicitly for this package
while diagnostics retain their no-GP gate. DDR layout and inactive write/port
checks remain mandatory in both netlists. The package routes a first-pass
pixel-clock ladder with seed 5 first, followed by 1, 2, 3, 4, 6, 7, 8.

The optional shared GP hook delegates menu requests to `fes_menu_control` in
the same pixel-clock domain. Configuration selects fixed slots; sequence ACK
means pending acceptance and completion is separately readable. Quiesce ACK
waits for drain before execution hold. Runtime buffer transport/admission and
exact-artifact menu acceptance are not established by source simulations.
