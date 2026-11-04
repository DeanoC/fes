# HPS-controlled DE25 ZX81 diagnostic

This Quartus Pro 26.3 lane adds a ZX81 computer endpoint to the locked Terasic
SCS GHRD. The lightweight HPS bridge can issue the existing `fes.computer`
keyboard, execution and CRC-checked tape commands. The real NMOS Z80, 16 KiB
RAM and BASIC ROM drive the board-local 720p renderer. Audio, SAVE, expansion,
gamepads and the session-display plane are disabled.

The GHRD's HPS/LPDDR4 configuration, debug paths, inherited DDR probe and boot
data remain selected. The new computer path adds no DMA master or DDR access.
It runs entirely on the existing 100 MHz peripheral clock, using 13/200
enables for the unchanged 6.5 MHz ULA and 3.25 MHz CPU. HDMI uses its separate
74.25 MHz PLL. Transport readiness crosses a two-stage pixel-clock
synchronizer, and the RGB/DE/HS/VS mask changes only at the renderer's 720p
frame boundary. The board shell qualifies configuration reset and PLL lock on
the existing 50 MHz reference clock: RGB and DE blank
immediately, while sync visibility and picture resume wait for a complete
horizontal sync pulse outside vertical sync. Pixel-clock loss does not stop
CPU, control or tape. The renderer packs eight monochrome pixels per RAM word
in two 328×270 M20K banks. Capture publishes only completed 328-pixel rows,
crops excess rows at 270, and renders any missing bottom rows as white paper.
Partial first/final source lines are excluded; malformed complete lines reject
the whole capture. Output stays black until the first complete capture.

A request/acknowledgement handshake keeps the published bank immutable until
scanout switches at a 720p frame boundary. A slow or stopped reader makes the
source skip whole frames; a stopped source repeats the last completed picture.
HOLD aborts partial capture and retains the last completed picture. An
endpoint reset asynchronously invalidates capture and publish ownership even
with clocks stopped. The raster, sync and selected scanout bank keep running
through the current frame; presentation is hidden at the frame boundary where
the synchronized readiness gate closes. Each domain releases reset through
its own two-stage synchronizer.
The renderer's final pixel-release stage also resets the computer readiness
crossing. There is one physical release chain for this peripheral-reset source
in the pixel domain. The board has no independent pixel reset chain. Its
reference controller retains one pending visibility token across repeated PLL
losses, waits for a pixel reply confirming allow-low and a closed visible line,
then permits rearming at a later complete horizontal sync pulse. The four
single-bit handshake crossings each have two stages; only first-stage data
paths are cut. HDMI completion and returned pixel activity are synchronized
into reference before registered readiness crosses to control. PLL loss also
restarts HDMI configuration. A stopped pixel clock cannot release a frame bank.
DE, sync and the scaled pixel selector share the RAM's registered read latency.
The two single-bit crossings cut only their first synchronizer stages. The
held row-count metadata has explicit 10 ns net-delay, datapath-delay,
maximum-skew and maximum-path bounds. No whole clock group is cut. Physical
M20K, metastability/MTBF, PLL recovery and external HDMI acceptance remain
board-validation work.

Tape storage uses two explicit even/odd M20K banks with common-clock
simple dual ports and old-data reads. A guarded board wrapper retains the
entire portable behavioral module for native simulation; shared RTL is unchanged.
The original hint-only adaptation exceeded device capacity because Quartus
could not infer its read-during-write behavior.

Run here:

```sh
make test
make sim
make video-sim  # focused frame ownership/clock/reset checks
make project VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
```

`project` permits inspection of local edits. Generation/build require clean
committed source and use a frozen source copy with the locked vendor ZIP.
`OUTPUT=/new/private/path` selects a fresh output; existing outputs are refused.
Builds use two CPU workers and never program hardware or flash. The diagnostic
SOF is `hps/project/output_files/golden_top.sof` under that output. Source,
vendor, generated clocks/handoff, tool and artifact identities are retained.
The build ID is a source/vendor/recipe fingerprint, not a sealed package ID.

JTAG loading requires a complete SOF with the preserved Terasic SPL. After an
HPS cold reset, FPGA-first JTAG cannot redeliver that loader automatically;
reload the exact complete SOF as documented in the
[cold-reset investigation](../../../../../docs/validation/2026-10-30-de25-reboot/README.md).
This starts a new FPGA/client epoch and does not admit warm reset or HPS DMA.

The endpoint sits at peripheral-relative offset `0x10300`, span 32 bytes.
The existing fabric JTAG master also reaches the peripheral bridge at
`0x00100000`, making the computer window `0x00110300` in that master only.
Generation verifies the actual elaborated route, full 32-byte span and
translated address map. This is an explicit maintenance path; it establishes
no CPU physical address or production MMIO admission.
The [physical control diagnostic](../../../../../docs/validation/2026-10-30-de25-zx81-hardware/README.md)
records the actual AArch64 client reaching this FPGA path, including identity
refusal, CRC tape delivery/eject, HID and HOLD/RUN. BASIC/HDMI output remains a
separate observed-picture check.
[DE25 computer](../../../../mister-packages/docs/de25-computer.md) owns its
register contract. It accepts exactly one full-word GPO submission with a new
request toggle; the host waits for the matching GPI ACK before another command.
There is no old-toggle preload write. A partial/busy/malformed access or a
post-start peripheral reset permanently faults this local transport. It keeps
GPO/ACK state, holds the CPU and masks cassette writes/ready; only FPGA
reconfiguration starts a new epoch. Faulted reads still complete after reset
release. The DE25 tape maximum is 16,375 bytes so BASIC LOAD stays within
`$4009..$7FFF`; its generated limit is checked by both the mailbox and client.
The transport readiness gate is synchronized into the pixel clock before it
masks RGB and video sync. This containment is not a fence for inherited
HPS/debug masters, and warm-reset ACK remains inactive.

The Python client uses generated constants and injectable `RegisterIO`
callbacks. Its CLI accepts only explicit regular-file storage for offline
testing; it supplies no physical MMIO backend, default address or runtime
device admission. For example:

```sh
python3 client.py --help
```

`--register-file /absolute/fixture.bin` requires an active offline fixture
that processes requests and updates ACK; a static register dump cannot answer
commands. The client tests supply the active protocol model through `RegisterIO`.

Execution powers up held. The client exposes `hold`, `run`, complete HID-state
`key`/`neutral`, `upload` and `eject`; uploading does not automatically run the
CPU or type `LOAD ""`. It checks identity/capabilities and fault before and
after commands. Uncertain completion poisons the client without retry. A
software polling timeout does not bound an actual bus cycle; native access
width/order, bounded MMIO, firmware/device binding and a production runtime
profile remain board-validation work.

The simulation exercises the real endpoint, mailbox, HID mapper, tape store,
machine, ROM and renderer, plus the HDMI shell's lock/reset gating. It includes
cold BASIC/editor boot, actual key-driven
LOAD and ordered RAM-write verification, CRC failures, stopped PIX operation,
HOLD neutralization, malformed-access faults and stopped-clock reset behavior.
The focused video bench checks every displayed image pixel across three clock
ratios with randomized unwritten RAM, plus startup, malformed lines, cropped/short
frames, HOLD, stopped clocks, stopped-clock epoch reset with presentation held
through the next raster boundary, and queued-frame recovery.
The board bench also checks asynchronous reset and PLL-loss blanking while
preserving an active HDMI sync pulse, then resumes data only at a complete line
boundary using digital PLL signals.
Memory uses behavioral RTL; HPS, physical M20K, PLL, I2C and HDMI are not modeled.
Compiled generation separately checks the real HPS-LW route, both PLL rates
and the slave map against all inherited peripherals.

`make client-proof OUTPUT=/new/private/output CLIENT_PROOF_ARGS=--native-only`
connects the actual native `De25Computer` implementation to this endpoint and
machine through a private Unix socket. `client_proof.py --help` lists the
explicit SDK image/volume/compiler and QEMU arguments for the static AArch64
client. Both architectures run the same source snapshot. The full matrix
includes real BASIC boot, complete HID snapshots typing `LOAD ""`, CRC/chunked
tape delivery, every actual loader RAM write, pixel-clock stoppage, live eject
and HOLD/RUN. Smaller scenarios cover maximum/odd tape sizes, wrong identity,
malformed observations, missing/late ACKs, ambiguous writes and later reset.

The existing machine bench and the socket server share `computer_sim.hpp` for
clock generation and RAM/video observations. Only the client emits mailbox
commands. Test observation operations advance simulation time explicitly;
the selected build ID identifies this simulation, not an existing SOF. Fault
injections alter read results or report uncertain writes after real RTL
transactions; the reset cases assert the actual endpoint reset. This proves
client/RTL integration, without modeling HPS bridge accesses, SDM programming,
physical memory primitives or a hardware bus-access deadline.
The [native/AArch64 client proof](../../../../../docs/validation/2026-10-30-de25-zx81-load-roundtrip/README.md)
records the complete session, uncertainty cases and frozen source/tool hashes.
The functional session types `1 PRINT 7`, captures the real ROM SAVE emitter,
loads and runs that image, accepts a `2 REM` edit, then reloads and reruns without
reset. ENTER is released after the CPU enters the ROM newline handler, before
waiting for command completion. Both runs must print 7 and return to the editor; reload must remove line 2.
The private SAVE-RAM observation creates a test fixture; it adds no SAVE download
API or advertised target capability. Raw live-memory snapshots are not loadable
tape fixtures: their active-display CDFLAG differs from actual SAVE output.
The separate maximum-byte-transfer test uses synthetic data to check bounds;
it does not claim a BASIC program round-trip.

`test_tape_store.py` compares the board primitive branch, native branch and
portable store using an explicitly supplied vendor `altera_mf.v` model, or
prepares a small isolated synthesis project. It never runs Quartus itself.
Model simulation and actual M20K mapping are separate checks.

This is an unadmitted JTAG SRAM diagnostic. Physical DE25 acceptance, external
HDMI/I2C timing, MTBF, firmware pairing and global reset lifetime remain open.
The exact Terasic pin and IO-standard assignments are the bring-up baseline.
Serial/DHCP/SSH Linux boot is a separate
first-board check. No image, device tree or installed bundle is changed here.

[Validation and exact artifact](../../../docs/validation/2026-10-29-de25-zx81-hps/README.md)
records the successful producer, failed capacity candidate, simulation binding,
software checks and the remaining physical acceptance gates.

The [combined PR profile](../link-proof/zx81-computer/README.md) explicitly
selects this component's `EXTERNAL_RAM=1` parameter and its registered expansion
conduit. It gates execution with PR RUNNING as well as the mailbox's RUN and
transport health; freeze registers a complete idle memory request. This
standalone diagnostic retains its internal-RAM selection and no PR control.

## Canonical source package

The normal board-selected producer is
`scripts/build_de25_zx81_package.py`, selected by the parent registry for
`fes.zx81` on `de25-nano`. It selects the shared standard ZX81 core, allocated
session display and fixed audio, with the same internal16K base as DE10.
The package declares `de25-linux-computer-v1` and its nine required interfaces,
including cassette SAVE and scanout1.1. It declares no reserved HPS DDR window.
The reduced BRAM project remains a separate diagnostic.

The complete machine/reader/audio closure, controlled execution and compiler
identities supply the functional BUILD_ID before generation. The generated
interfaces, clocks, reset/FIFO derivations, staged source bytes, top-level wiring,
firmware and PLL parameters are checked before and after compilation. Scanout
retains the managed allocated-MENU SID/nonsecure master and HPS/EMIF recipe.

`--compile-only` retains a candidate with every raw timing finding, without
reviewed-finding substitution or export. Package export still requires complete
passing timing, fresh fitted input/output-reference review and an independently
identical RBF. The standard board uses the same stock HDMI shell. Its two extra
audio-clock output findings remain outside the current30-output reference policy
and block export. No timing or seal gate is relaxed; no command programs a kit.

## HPS-first Linux loading candidate

For the Linux FPGA-manager path, explicitly select the HPS-first profile:

```sh
python3 build.py build --hps-first --quartus-root /absolute/altera_pro/26.3 \
  --vendor-archive /absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --output /absolute/new-private-linux-output
```

The profile adds the documented `HPS_INITIALIZATION "HPS FIRST"` assignment,
binds that choice into the computer build identity and records it with captured
inputs. Default maintenance builds explicitly select `AFTER INIT_DONE` and retain
the FPGA-first JTAG flow. RTL, clocks, memory and endpoint contracts are shared.
No timing or reset constraint is relaxed.

After full compilation, the HPS-first profile enriches the SOF with the preserved
Terasic SPL and asks Quartus PFG for the documented HPS split. The output `hps/`
contains `linux-complete.sof`, `linux-boot.hps.jic`, `linux-boot.core.rbf` and
`linux-programming.json`, binding roles, source/build identity and exact hashes.
The JIC is a prepared QSPI candidate only; the builder invokes no programmer.
HPS boot compatibility, Linux fabric load and production runtime admission remain
unqualified until their separate physical checks pass. Persistent flash changes
require explicit authorization and a concrete rollback procedure.

The existing FPGA-first SOF is rejected by PFG's HPS split with error 19921.
Merely asking for a core-only RBF does not establish a valid Linux-loading pair;
see [the failed hardware attempts](../../../../../docs/validation/2026-10-30-de25-linux-fpga/README.md).

The current endpoint is fabric1.1. It exports board video readiness through
read-only offset24: vendor HDMI configuration completion and the complete-line
PLL/reset gate cross as one synchronized bit to the control clock. The HPS
component conduit carries that input from the board wrapper. Managed runtime
Start waits for this field; it does not configure ADV7513 from Linux. Board and
endpoint simulations test synchronization and loss/recovery. PLL and HDMI
configuration are digital models there; new matching FPGA build and physical
acceptance remain outstanding. Older fabric1.0 receipts remain historical.

HDMI-ready status now crosses from a preserved pixel-clock source register into
its two control-clock stages. The 50 MHz HDMI READY source also has two pixel
stages. The SDC identifies all six registers and both first-stage D pins exactly
and cuts only the two asynchronous source-to-first-stage paths. Stage-to-stage
setup/hold and locally released reset recovery/removal remain timed. This adds
one pixel clock to the readiness observation; it does not change video pins or
qualify physical display output. The earlier uncorrected fabric1.1 compilation
at 16838e82c has negative crossing setup and is not suitable for board testing.

## Standard-core board integration

`fes_de25_zx81_standard.v` connects the shared standard `fes_zx81_core`, extracted
atomic register transport and kernel-owned session display. The standard core
runs with the 100 MHz clock fraction and no DE10 reserved DDR window. PCM,
expansion signals and separately qualified system/pixel reset releases remain
board interfaces. A transport fault fences machine execution and scanout.
The reduced wrapper shares the extracted transport; no second protocol was added.

`python3 sim.py --bench standard` uses the actual combined RTL with a CPU/RAM
loop diagnostic and internal16K memory. It checks running CPU writes and a retained
RAM marker across allocated display open/drain/CLEAR/new-generation reopen,
transparent/opaque/alpha frames and kernel-only publication. The normal `all`
selection includes this case. This is digital integration evidence; the wrapper
is selected by the standard canonical producer and explicit runtime profile. Physical
reset/clock/audio, ROM/RAM-pack composition, Quartus and native BASIC testing
remain required.

The existing builder exposes `prepare_standard_component(output, revision)` for
Quartus integration. It stages the new Qsys component from captured sources,
reuses guarded M20K machine/tape/video/reader adaptations, and records source and
derived-file hashes. The Qsys interfaces declare separate computer/scanout clocks
and reset releases, one read-only memory master, finished video, PCM and expansion
conduits. It creates component files only, without selecting a canonical board
recipe, compiling an image, admitting a runtime profile or programming hardware.

`fes_de25_audio.v` supplies the board audio wrapper: the exact Terasic HDMI_ASx4
`av_pll` driven by `CLOCK1_50`, plus the existing coherent stereo PCM crossing
and I2S serializer. The vendor PLL requests12.288MHz; its integer ratio is
50MHz×58/236 (about12.2881356MHz). Guest HOLD mutes samples rather than resetting
that clock/handshake. The standard-board integration below selects this module;
physical reset/timing, transmitter integration and native audio capture remain.

The same builder's `--standard-core` option composes that component into the
managed allocated-display GHRD, including the stock audio PLL. It selects the
standard internal-16K base configuration; expansion personas are not linked.
The Quartus RAM branch uses the guarded Agilex/M20K adaptations. Firmware keeps
all original 12 KiB and explicitly zero-fills its unreachable 4 KiB tail to
match the 16 KiB ROM primitive without an implicit initialization warning.
`standard-clocks.sdc` selects the standard memory-fault hierarchy in the common
board constraints, cuts only the five added synchronizer ingress paths and
bounds both held display-command/response bundles to 10 ns. Fault and drained
status launch from dedicated registers. The internal-16K base requires a
constant PCM path and constrains its two audio reset-release ingress pins;
live sound personas require their own coherent-transfer timing. These source
constraints still require mapped/fitted checks and external timing admission.
The I2S bit/word clock outputs are declared from the actual counter clock with
divide-by-4/256 ratios. Board visibility is registered before its readiness
crossing. Standard-board bridge FIFOs of depth four or less select MLAB storage;
the larger response FIFO keeps M20K, with unchanged clocks, depths and latency.
For a fresh output directory, run:

```sh
python3 build.py generate --standard-core \
  --vendor-archive /absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --quartus-root /absolute/altera_pro/26.3 --output /new/private/path
```

This generates the complete GHRD from committed sources and checks the actual
computer/scanout slaves, clock/reset routes, memory credits and managed master
inventory. It never programs the kit. `project` also allows local inspection.
`build` compiles this checked project and retains separate Linux phase artifacts.
It is a diagnostic candidate, with no package/timing admission. The canonical
producer separately binds its functional package identity and strict export
gates. Runtime software admits the standard profile; the board stays on its
working baseline until exact-artifact qualification and hardware acceptance.
