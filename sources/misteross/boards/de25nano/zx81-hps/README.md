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
frame boundary. The board shell tracks pixel reset/PLL lock: RGB and DE blank
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
HDMI/I2C timing, MTBF, the inherited DJ24 voltage discrepancy, firmware pairing
and global reset lifetime remain open. Serial/DHCP/SSH Linux boot is a separate
first-board check. No image, device tree or installed bundle is changed here.

[Validation and exact artifact](../../../docs/validation/2026-10-29-de25-zx81-hps/README.md)
records the successful producer, failed capacity candidate, simulation binding,
software checks and the remaining physical acceptance gates.

The [combined PR profile](../link-proof/zx81-computer/README.md) explicitly
selects this component's `EXTERNAL_RAM=1` parameter and its registered expansion
conduit. It gates execution with PR RUNNING as well as the mailbox's RUN and
transport health; freeze registers a complete idle memory request. This
standalone diagnostic retains its internal-RAM selection and no PR control.

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
