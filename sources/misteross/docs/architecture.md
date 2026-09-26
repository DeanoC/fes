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

`fes.ramtest` is a separate utility on the same mailbox, fixed 720p
interface, and gamepad interface. `make build-fes-ramtest` writes
`build/fes-ramtest/core.rbf`. After execution release it pattern-tests the
SDRAM addon and an HPS DDR window and prints the pattern, address, clock
and error count. The SDRAM clock pin is the inverted DDR output used by
MiSTer controllers. The default OSS bitstream runs that clock at 50 MHz;
`make build-fes-ramtest-100` makes a separate experimental 100 MHz package.
The latter samples the bidirectional DQ pads with phase-shifted fabric
registers because the pinned OSS packer cannot put DDR input registers on
those pads. The 100 MHz build has a four-domain timing gate but has not yet
passed a full hardware scan. The pinned OSS PLL table stops at 100 MHz.
`make build-fes-ramtest-quartus` compiles a fixed 130 MHz diagnostic with
Quartus 17.0.2; `RAMTEST_MHZ=100` selects a separate 100 MHz diagnostic.
Each tests the full SDRAM range at one rate and keeps the six pattern counts
on screen. A gamepad button, or a keyboard key the host maps to one, stops
the scan. The ABI has no memory opcode. A `fes-gp-v1` package load
releases the HPS bridges after user mode. A raw development RBF stays
contained, so the HPS path fails until the bridges are released.

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
Reuse splash bytes for Stop idle; keep FAT `/menu.rbf` / `core=menu.rbf` as
the filename until a U-Boot reseal. A sealed RBF requires a clean committed
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
the separate video PLL. This raises the reduced CPU cadence by 0.43% from the
old 52 MHz profile; the CPU remains /16 (3.264 MHz), not cycle-accurate NTSC.
The TMS9918 logical raster now uses its independent fractional 60 Hz enable.
HDMI video timing stays 74.25 MHz and audio stays 48 kHz.
The producer checks all three timing domains and each audio output pad before
packaging. SG-1000/SMS keep their existing shared 52 MHz system PLL.

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
nextpnr `5dea3ecd`, Mistral `b28e30a`). SMS selects
`toolchains/fes-sms.lock` (Yosys `e2d425de`, nextpnr `469b6670`, Mistral
`7ed06e21`) so `make build-fes-sms` uses the router that ends the seed-3
plateau. Those two locks are different bytes and different HIP cache slots.
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


The `expansion` Go linker admits only the versioned ZX81 full-height socket or
the Coleco CPU-bus rectangle `(1769, 32, 2806, 1034)`, selected by the exact
slot/map pair. A Coleco manifest may also declare
`fes.coleco.response-boundary/4`: exactly two fixed shell-response CRAM
coordinates and each cart bit's resulting value. The linker requires both
bits to change as declared, applies them with the socket overlay and rejects
every other outside change. This patch does not enlarge the socket rectangle.
Changed Coleco frames regenerate their checksums; the existing ZX81 `Link`
behavior and output remain unchanged.

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
The locked nextpnr `469b6670` provides those flags after `make toolchain-fes`.
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
`5dea3ecd` with `--router gpu`, HeAP timing weight 100, criticality
exponent 5, and `--timing-allow-fail`, and Mistral
`b28e30a`; the selected toolchain enables the HIP device backend. Default
place-and-route is first-to-pass: seeds 5, 4, 1, 2, 3, 12, 7 and 10 at
weight 100, then the same seeds at weights 300 and 1000. nextpnr `5dea3ecd`
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
| Toolchain selection | The repository-wide lock remains on current mainline Yosys/nextpnr. Factory Coleco v2 selects `toolchains/coleco-sgm.lock`, builds it under `build/toolchain/fes-coleco-socket-v2`, and enables HIP. SG-1000 retains `toolchains/registered-memory.lock`. SMS selects `toolchains/fes-sms.lock` (nextpnr `469b6670`, Mistral `7ed06e21`, Yosys `e2d425de`). Quartus needs neither lock. |
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
| Route pressure | The OSS reproduction is `5CSEBA6U23I7`, nextpnr `5dea3ecd`, `--router gpu`, seed 5 first (order 5, 4, 1, 2, 3, 12, 7, 10), HeAP timing weight 100 before 300 and 1000, criticality exponent 5, `--timing-allow-fail`, no `--tmg-ripup`, at 74.25 MHz. The embedded `BUILD_ID` makes the seed part of the route recipe. The GPU router can report a provisional timing shortfall before final repair; the allowance only permits that intermediate result, while the recipe requires final structured `clk_sys` and `pixel_clk` timing to pass. The sealed recipe requires `backend hip:<device> ready` and rejects CPU-reference fallback; no missing BEL or pack feature was identified. |

The concrete build entry points are `make build-fes-coleco-quartus` and
`make build-fes-coleco`; both require a clean source checkout, seal format-2
packages and never program hardware. Exact-artifact kit acceptance remains a
separate FES integration step.

The Opcode SGM candidate uses a separate development shell with a 31-bit
request and 28-bit registered response. The response carries direct data,
claim, WAIT, INT, a shell-RAM claim and signed PCM. The shell owns a dormant
32 KiB M10K RAM and saturated SN+AY audio path; the separately synthesized SGM
owns the window-enable and AY register decode. `toolchains/coleco-sgm.lock`
pins nextpnr `469b6670` with frozen-scaffold BEL admission and bounded slot
placement. The v2-only socket reserves `24 1 28 19` placement and
`(1769,32,2806,1800)` CRAM; v1 retains its smaller rectangle. The v2
build scripts keep the v1 diagnostic's socket and archive contract untouched.
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
It reuses Coleco TV80 and the shared TMS9918-style VDP, which renders Graphics
I, Graphics II, Text and Multicolor on a fixed 256×192 logical raster. Text
suppresses sprites, Multicolor keeps them active, and unsupported selectors
render the backdrop. Its 256×262 logical raster shares Coleco's nominal 60 Hz
fractional enable. It also reuses the dual-port RAM wrappers,
the `fes.simple-computer` mailbox, both PLL wrappers and the 720p HDMI shell.
The SG-1000-specific RTL is the memory map (cartridge at `0x0000–0x3fff`, 1 KiB
RAM at `0xc000`) and the 8255 joystick ports `0xdc`/`0xdd`. There is no BIOS
shim.

`make sim-fes-sg1000` is the diagnostic Verilator machine check
(`-DTV80_REFRESH=1` only). `make sim-fes-sg1000-oss` compiles the same
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
Yosys defines `TV80_REFRESH=1`, `FES_SG1000_OSS=1`,
`FES_SG1000_ROM_LINK=1`, and `FES_COLECO_OSS=1`. The product RBF contains a
blank 16-lane M10K cartridge, and the format-3 package carries a validated
`rom-map.json` and exact 16 KiB `cartridge-rom` requirement. Reset is not
held for an application media upload.
`--synth-only` runs Yosys without a clean tree and does not seal. HIP
`--router gpu` of that synth-only netlist (BUILD_ID all zeros, seed 4) met
the 52 MHz and 74.25 MHz structured fmax rows on a live HIP backend. A sealed
BUILD_ID changes the placement search space. `fes.sg1000` is registered as a
package-only parent recipe and is not in the factory image. A historical
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
(about 48.5 frames/s), because its serial scanline builder exceeds the 60 Hz
line budget; optimizing that renderer is separate work. This timing model
covers logical frame pacing only, not composite sync, half-lines, PAL timing or
cycle-perfect raster effects. The OSS package uses 32 fixed blank M10K
cartridge lanes, authenticated by its format-3 ROM map; the target links an
exact 32 KiB `cartridge-rom` before download. Shorter fixed-map ROMs must be
explicitly padded with `0xff`. The Quartus oracle and default mailbox
simulation remain format-2 media-transport diagnostics. There is no BIOS shim. Mode 4 implements
16 KiB VRAM, 32-entry six-bit CRAM, tile attributes and scrolling, 8×8/8×16
zoomable sprites with collision/eight-sprite overflow, line interrupts and
VBlank interrupts on the fixed 256×192 logical raster. The PSG mix is a signed
16-bit sample; HDMI I2S0 is 16-bit 48 kHz against the existing runtime ADV7513
program (N=6144, CTS=74250). There is no host `fes.audio` mailbox. Mappers,
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
square wave, HDMI I2S covers 16-bit 48 kHz frames, and the machine covers the
32 KiB map, long-then-short `0xff` tails, and CPU execution of that diagnostic
(not reset-only peeks).
`make sim-fes-sms-oss` is the linked-ROM OSS-conditional check
(`-DFES_SMS_OSS=1 -DFES_SMS_ROM_LINK=1 -DFES_COLECO_OSS=1`). Both are host
simulation, not hardware acceptance. The OSS format-3 ROM-link mailbox omits
the legacy blob capability and rejects blob commands. Its package declares
only keyboard and fixed-video interfaces alongside its required ROM.

`make build-fes-sms-quartus` is the Quartus Prime Lite 17.0.2 oracle recipe.
It requires a clean committed tree to seal a format-2 package and never
programs hardware. `--compile-only` writes `build/fes-sms-quartus/core.rbf`
and timing evidence without sealing.

`make build-fes-sms` is the OSS producer
(`scripts/build_fes_sms_oss.py`). It uses `toolchains/fes-sms.lock`
(nextpnr `469b6670` with Mistral `7ed06e21` and Yosys `e2d425de`), SMS `constraints-oss.qsf` (Coleco
video/I2C pins plus ADV7513 I2S), and Coleco `clocks-oss.sdc`. Yosys defines
`TV80_REFRESH=1`, `FES_SMS_OSS=1`, `FES_SMS_ROM_LINK=1`, and `FES_COLECO_OSS=1`. `--synth-only` runs
Yosys without a clean tree and does not seal. The producer uses `--router gpu`
and a first-pass HIP seed/weight search (starts at seed 3 / HeAP 1000,
then the remaining `PLACER_SEEDS` and weight 300). Final structured `clk_sys` and
`pixel_clk` rows must meet 52 MHz and 74.25 MHz. `fes.sms` is registered for
package-only parent builds and is not in the factory image. Historical parent
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
plays that fixture and checks keyboard/media side effects.

## FES ZX81 machine simulation

`cores/fes-zx81/rtl/zx81_machine.sv` is the first-slice ZX81 extracted from
MiSTer-devel/ZX81_MiSTer `ZX81.sv` at Release 20260603: 16 KB RAM, PAL, no
CHROMA/QS/YM2149/joystick. Keyboard rows and `.p` tape bytes come from the GP
mailbox. Character ROM bytes are `cores/fes-zx81/rtl/zx8x.hex`, converted from
the pinned `rtl/zx8x.mif`. The Z80 is TV80 (`66a131c`) wrapped as `T80pa` with
Sorgelig half-cycle `CEN_p`/`CEN_n` timing, WAIT via CEN gating, and
`TV80_REFRESH`. NMI is sampled every clock, matching T80.vhd. `CEN_p` is
3.25 MHz from the 52 MHz enable divider.

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

## FES ZX81 Quartus bring-up

`make build-fes-zx81-quartus` is the Quartus Prime Lite 17.0.2 recipe for
`fes.zx81` 1.0.0 legacy oracle package. It is not a Mistral/nextpnr payload
and is not the standard socketed package. The board shell
`cores/fes-zx81/rtl/top.v` uses two `altera_pll` cells from the 50 MHz V11
reference: 52 MHz system (T80, ULA, mailbox) and 74.25 MHz pixel (HDMI
1650×750). HDMI RGB/HS/VS/CLK pins and U10/AA4 match FES Pong. The HPS I2C
cell is at `HPSINTERFACEPERIPHERALI2C_X52_Y60_N111`; `out_clk`/`out_data`
pull SCL/SDA low and `scl`/`sda` read the pads (Quartus assign-to-Z in place
of Pong's `MISTRAL_IO`). The Z80 is VHDL T80pa from ZX81_MiSTer Release
20260603; Verilator keeps TV80.

The compile defines `QUARTUS=1`. ROM, 16 KB RAM and the 16 KB media blob
instantiate `altsyncram` bidirectional dual-port M10K with unregistered
outputs; ROM init is `zx8x.mif`. Simulation keeps inferred combo-read RAM
and `zx8x.hex`. The 720p capture buffer is a one-dimensional M10K array
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
`fes.zx81` 1.2.0 package. It authenticates the scoped ZX81 expansion-bus tools, writes
`build/fes-zx81-oss/build-inputs.json` before synthesis, and embeds that
record's 128-bit id as `BUILD_ID`. Synthesis is `synth_intel_alm` with
M10K allowed and DSP/MLAB forbidden. The machine ROM is `zx81_rom_link`:
eight empty BEL-locked 1024×10 lanes at `MISTRAL_M10K.5.73.0` through
`MISTRAL_M10K.5.80.0`. `zx8x.hex` is not a package input; launch splices
the low 8 KiB with `link_static_rbf.py init --machine`. RAM and media stay
inferred asynchronous-read M10K tables; the two-write media path uses native
asynchronous TDP M10K. The 720p capture buffer is a dual-clock M10K SDP. The
Z80 is Verilog T80pa/TV80.
HDMI I2C uses Pong-style `MISTRAL_IO` open-drain pads at BEL X52/Y60
(`QUARTUS` is not defined). Place-and-route uses `constraints-oss.qsf` and `clocks-oss.sdc`.
The QSF omits Quartus `HPS_LOCATION`; the SDC constrains only the 50 MHz
reference and nextpnr derives the PLL outputs. The Quartus files keep
`HPS_LOCATION`, `derive_pll_clocks` and asynchronous clock groups.
The independent ZX81 cart producer reloads an already routed shell with
`--no-pack`, so it writes a separate generated SDC that explicitly constrains
`clk_sys` to 52 MHz and `pixel_clk` to 74.25 MHz. Its recipe records those
requirements and the SDC digest. Publication requires both clocks to meet
their nominal and reported constraints, with only the existing picosecond
quantization tolerance when identifying the reported frequencies. This does
not change the sealed base shell or infer requirements from achieved Fmax.
Socketed shells export a registered Z80-like edge (44-bit request, 20-bit
response). Vacant response FFs hold 0, so ROMCS/WAIT/DSEL/RAM_PRESENT are
active-high from the cart. CPU writes on that edge use TDP `A1WE` like the
validation cart; mixed-width `A1EN`/`A1BE` decoded but did not hold `POKE`/`OUT`.
Cart M10K keep a distinct top clock port (`FPGA_CLK1_50`) so
`--fes-slot-clock clk_sys` can splice the inferred IB onto the shell 52 MHz
net; naming that port `clk_sys` leaves M10K on the pad output. During `/RFSH` the shell presents the ULA character-ROM address as the QS
`8400–87FF` window so the same 1 KiB cell supplies glyphs. QS power-up
loads Sinclair glyphs 0–63 (ROM `1E00–1FFF`) into that cell so boot text
is readable; CPU writes still replace rows. The shifter loads a
registered `rfsh_chr` hold that keeps the first ROMCS byte; `/RFSH` is
not muxed into `cpu_din` (that loop stopped the FES GP mailbox). The validation
cart is the first consumer on that edge; Zon X and QS Character Board RTL share
the plugs but are not library assets yet. Zon X channel A is a digital square
on `peek_d` (R0/R1 period, R7 enable, R8 level); the shell mixes that sample
into HDMI I2S0 when `RAM_PRESENT` is 0. Channel A period uses nested 4-bit
LUT counters so the cart does not place `ALUT_ARITH` carry in the slot.
Diagnostic 904–907 remains an HPS bench and does not seal
`fes.zx81`. The cart route also receives the fixed `fes.zx81-bus.socket/1` CRAM rectangle
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
nextpnr `5909feb5` forms the 50→52 MHz integer on the 520 MHz feedback
profile (`M=52 N=5 C6=10`). Place-and-route uses the deterministic seed order
10, 5, 12, 2, 7, 1, 3, 4, 6, 8, 9, 11, 13, 34. For each seed it tries heap
timing weights 1000 and 300, then sweeps the same seeds at weights 2000, 100
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
Fmax; the selected seed and weight go into route evidence. This keeps native async-M10K address paths within the 52 MHz
system constraint. The recipe requires two
`altera_pll` cells (52 MHz system and 74.25 MHz pixel). Also required: the HPS GP
mailbox, the I2C bridge,
and at least one M10K. It seals the format-2 exporter only when both
clocks meet their constraints. The command never programs hardware.

A sealed OSS package has been used for a **hardware diagnostic** on the
designated kit (BASIC, sofa keyboard, empty `LOAD ""` → `0/0`, committed
`.p` → `10 PRINT "OK"`). That is not exact-artifact hardware acceptance
and does not inherit the Quartus bring-up result (the diagnostic used TV80
and the former registered-M10K workaround). A GPU-routed package of that
same registered-M10K recipe base (nextpnr 9c751533, misteross 9ad19189)
also booted to the ZX81 editor on the kit on 2026-09-12 and answered
`PRINT` + NEWLINE with `0/0` through the host keyboard route. The current
native async-M10K recipe has **not** booted on the kit: its sealed packages
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
| Combo-read block RAM | `assign q = ram[addr]` with `synth_intel_alm -nolutram` previously became LUT RAM. ABC ran 25+ minutes on an 8 MB XAIG / 23 MB symbol file and did not finish. | Native Yosys async M10K inference maps 10/20/40-bit SDP and two-write/two-read TDP shapes; the OSS recipe uses `ramstyle="M10K"` and nextpnr routes flow-through reads. Quartus keeps `altsyncram`. |
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
seed 1, `--router gpu`, the task-local QSF, the 50 MHz board SDC and an
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
