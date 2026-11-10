# DE25 embedded-control board diagnostic

This private overlay combines `fes_embedded_control` with an explicitly selected
committed DE25 ZX81 diagnostic source snapshot. It uses the same board
producer's source/vendor fingerprint for both its ZX81 computer and embedded
control. The overlay and endpoint bytes enter that fingerprint before project
generation. The fingerprint is not a sealed FES package identity. An explicit
`--sealed-diagnostic` build instead computes the existing canonical functional
build ID before synthesis and installs it in both endpoints.

The overlay owns its version-1.1 `fes_de25_computer.v` transport, `video.v` and
generated register headers, installed over the captured board's original transport.
Its owned `clocks.sdc` retains the existing clock and metadata bounds and cuts
only asynchronous ingress. It rejects the removed video reset-release chains;
recovery/removal from the shared qualified resets to owner state remains timed.
Those bytes enter the same functional source snapshot, fingerprint and seal.
Generation and verification compare both installed and actual generated HDL
against them. The GP ABI, aperture size, clocks, master inventory and execution
commands retain their existing contracts.

Read-only offsets 24/28 observe actual Z80 fetch bus cycles and their sampled
last address. The count uses M1/MREQ/RD and the CPU bus clock; GP acknowledgements
do not advance or reset it. Reading the count captures a coherent address while
the CPU keeps running. Hold leaves the counter intact so a bounded observer can
check run progress, held stability and relaunch. This is not a retired-instruction,
video, memory-containment or production-acceptance measurement.

The opt-in `--reader-memory hps-ddr-menu --ddr-bus-base 0x95000000
--ddr-bytes 8388608 --hps-first` personality installs the owned menu pipeline
instead of the checksum reader. It requires the reviewed board revision and
cannot share the held-ROM selection. CPU control stays at 100 MHz; the reader,
128/256-bit adapter and video share the existing 74.25 MHz clock. The HDMI
wrapper exports its qualified pixel reset and its video export selects the
menu. The private source profile is
`de25-embedded-hps-first-menu-prototype-v1`, ABI `0x10203043`; the functional
record binds both slots, source files, clocks and the bounded CSR transport.
Project preparation works without Quartus. Generation/build also recheck the
actual exported clocks, reset/video routes and complete DDR master inventory.
Explicit parent clock/reset bridges let generated interconnect consume the
pixel owner; their parameters and connections are rechecked after generation.
The HDMI control model selects the wrapper from the prepared project's inputs,
including the menu reset export, and checks asynchronous assertion and two-edge
pixel reset release alongside its interrupt and I2C failure controls.

The CPU/pixel crossing uses one four-phase request and coherent reply. Reset
after use or a 1,048,576-cycle CPU timeout retires it without replay. An uncertain
request may have executed once; neither error nor local stop releases either
slot. Error data is `0xffffffff` as well as an Avalon error response, so failure
remains visible even through an upstream master that discards response codes.
Local retirement selects that error outside the captured reply bank, so local
failure handling does not insert a data mux into the constrained crossing.
Its separate `menu_clocks.sdc` bounds both stable packet buses and cuts
only asynchronous first-stage/reset ingress. Existing personalities retain
their own constraints. Physical timing, native boot selection and complete
menu/core/menu storage transitions are not established by this producer code.

Run the actual two-clock source model with:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify_menu.py --output out/MENU
```

Use `--allow-dirty` only for labelled working-tree models. `--project` also
requires the exact sealed generated menu project and matching current model
sources. The test checks real Tenfoot pixels through three frames and owned
drain, plus 640 CSR packets, stopped clocks and short/in-flight reset. Its pixel
oracle observes the selected board wrapper's FPGA HDMI pins, including RGB,
sync/data enable and the output pixel clock. Disconnected RGB and a missing
pixel clock must fail, alongside broken timeout/reset controls. The PLL and I2C
initializer are explicit digital stubs in this pixel-route model; the separate
`verify_hdmi.py` exercises the captured I2C circuit. Neither model simulates the
generated HPS AXI interconnect, transmitter/receiver or physical HDMI/DDR acceptance.

`fes_embedded_rom.sv` is a separate, currently unwired bulk-ROM receiver. A
Start latches the expected IEEE CRC32 while execution is held. Exactly 4 KiB or
8 KiB arrives as little-endian 128-bit beats; `stream_last` names the end of the
whole image, not the end of each DDR reader request. Backpressure protects its
private pending beat while it writes synchronous byte RAM. `loaded` becomes
true only after the last byte and CRC agree. Bad length/LAST, CRC, lost Hold,
abort or a second Start fail closed until reset. Read data remains zero before
completion and after failure. Source identity, pinned storage, reader-generation
binding, drain and the CPU reset gate belong to the enclosing platform.

The `rom_machine.sv` simulation wrapper wires the receiver into the real
ZX80/ZX81 machine using `EXTERNAL_ROM=1`, and keeps the CPU reset while held,
unloaded or failed. An independently CRC-checked synthetic ROM writes RAM at
`0x4000` and halts after release. Bad CRC produces no fetches or RAM writes.
Existing core and session shells retain `EXTERNAL_ROM=0`. This is a real RTL
model, not a physical ROM path: native/firmware hooks, a bulk source
adapter, sealed capability/profile and normal retained-ROM admission are still
needed. Existing hardware results apply only to their previously sealed inputs.

From the FES root, run the bounded model directly:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify_rom.py --output out/rom-stream-check
```

The standalone verifier below also runs that model for both ROM sizes. The
new receiver is compiled with warnings fatal; the real-machine model uses the
existing ZX81 simulator's source waivers. These checks perform no synthesis,
timing qualification or hardware operation.

`fes_embedded_rom_csr.sv` adds a relative 128-byte CPU-fed interface around that
receiver. Its matching constants come from `fes.fabric.embedded-rom`. Identity
and build words are read-only; an exact CRC is staged once before Start. Four
ordered full-width words form each 128-bit beat. Partial, out-of-order, busy,
unknown and repeated control writes retire the endpoint and abort its sink.
It exposes no physical address and initiates no bus-master traffic. The same
model can select this interface with `CSR_MODE=1`; the actual freestanding C
transfer in `platform/embedded/firmware/nano_rom.c` then feeds the real machine.
The tests also mutate the caller's source after capture and inject ownership,
identity, timer, counters, I/O and CRC failures. No production wrapper or native
launch currently selects this mode. The model additionally links the real opt-in
secure-provider include and computer control gate. It checks that ROM transfer
and private-image digest verification finish before a successful control-open
reply, and that failures contain/retire without replay. Platform operations and
the digest oracle are injected; this is not a boot image or hardware result.
The next physical binding must export the matched ROM aperture, identity and
held-execution conduits before a cold profile can enable it.

The explicit `--external-rom` producer variant now connects those peripherals in
Platform Designer. It requires HPS-first, `--reader-memory hps-ddr`, and the
reviewed board revision `79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. It refreshes
the machine, rational clock and mailbox from the committed overlay revision;
the canonical source closure and snapshot identify those three files explicitly.
The machine retains the 52.224 MHz default ratio for other shells, while this
board selects 13/200 at 100 MHz. The early WAIT/NMI declarations preserve the
reviewed Quartus behavior rather than creating implicit input nets.

The overlay's `video.v` uses a blank-history bit that
starts low and replaces the redundant `armed` register, so release during
active video still waits for a complete blank interval. The actual writer
control has an unbounded equivalence proof against the captured producer,
including arbitrary Hold, CE, reset, blanks and returned bank tokens. A broken
capture condition must produce a counterexample. Full dual-clock RTL comparison
separately checks RAM writes, pixels, publication/acknowledgement, Hold and
stopped clocks at identical qualified reset releases. Run
`verify_video.py --yosys /absolute/yosys --output out/VIDEO`
from the FES root. This does not prove metastability behavior or physical timing.

The computer supplies the video's two qualified reset inputs. The system side
uses the same 100 MHz Platform Designer reset as the mailbox and readiness
source, removing the video's extra two release cycles. One pixel release chain
now resets both the readiness synchronizer and video's bank-token state.
Asynchronous assertion still contains both owners with their clocks stopped;
release waits for the corresponding clock. The actual board ROM model checks
stopped-clock assertion/release and sticky epoch retirement, and rejects a
negative control that bypasses the shared pixel release. The outer HDMI wrapper
and vendor reset controllers remain separate reset cones to investigate; this
change does not claim complete reset-domain or physical qualification.

The video's RGB, DE and sync output mask uses the same qualified pixel reset
as its scanout controls. The system reset only owns the capture/system side;
it no longer directly gates HDMI-facing signals across clock domains. Pixel
mask assertion still works with stopped clocks and deassertion waits for pixel
edges. Full video comparison checks the retained frame/token and raster behavior;
external pin timing still requires the routed all-source pad checks and board
skew/load contract.

For the reviewed CPU-fed ROM/HPS-DDR variant, both Platform Designer reset
bridges now declare the actual 100 MHz clock and asynchronous-assert,
synchronous-deassert contract. The generated ROM, computer and control devices
share an owned bridge combining the memory reset with its early reset request.
The early request asserts asynchronously even when the memory reset output
itself waits for a clock edge. Both must clear before device release. The memory
sequencing parameters and OCM reset/request connections remain checked; the
remaining peripheral controller serves synchronous interconnect reset.
Generation and native context verification reject a changed clock, memory
request sequence, endpoint reset owner or independent peripheral release chain.
Vendor maintenance and HDMI reset ownership remain separate qualification work.
`verify_reset_domains.py --project /absolute/generated/project --output out/RESET`
models the actual generated vendor sequencer and owned bridge through short
pulses, PLL/configuration events and stopped-high/low clocks. A memory-reset-only
negative control must fail containment after a live epoch. Simulator style
waivers for vendor RTL do not add a production timing exception.

Compilation removes only the literal obsolete generated JTAG exception whose
two type-qualified register filters select no registers in the reviewed routed
netlist. No broader filter or replacement cut is added. Complete before/after
SDC hashes are recorded and independently checked after compilation. Additional
or changed constraint commands reject. External HDMI/I2C timing and independent
reset release remain separate qualification work; these changes waive no rules.

The source-bound timing file now forwards the actual pixel PLL clock to
`HDMI_TX_CLK` and applies the ADV7513 Rev B receiver minima (1.8 ns setup,
1.3 ns hold) to all 27 RGB/DE/sync pads. Missing or ambiguous clock targets or
pad inventories reject. All launch sources stay timed; there is no new cut.
These limits use a zero board-skew reference so the fitter can repair actual
receiver hold paths. Board skew and loading remain unqualified. The inherited
vendor 1.1-V clock assignment on DJ24 conflicts with the manual's 3.3 V table;
the electrical clock path must be resolved before board signoff. No voltage,
clock phase, receiver register or load value is changed to hide that gap.

`hdmi_control.py` derives the captured board wrapper with a two-stage,
50 MHz interrupt synchronizer sharing its existing qualified configuration
reset. Only the raw interrupt's first D pin is cut; stage-to-stage and the
related divided-clock consumer remain timed. Both reset to the inactive level.
The same guarded derivation makes the initializer wait for `END` to go low
(actual busy entry) before accepting completion. Otherwise stale completion
can end a new request before it starts. The captured register table, divider
and byte controller are unchanged; original/derived hashes are verified.

`verify_hdmi.py --project /absolute/prepared/board/hps/project --output out/HDMI
--yosys /absolute/yosys` runs the actual board/reset HDL and a process-synthesized
model of the captured initializer/controller against an open-drain slave model.
It checks 99 accepted writes across startup, IRQ reinitialization and reset
recovery, a NACK retry and immediate reset with the clock parked. Restoring
raw IRQ wiring or the stale-completion handshake must fail. The vendor's
blocking clocked processes race under direct event simulation; Yosys lowers
them to explicit register next-state logic for this digital circuit model.
This is not Quartus circuit equivalence, metastability or physical I2C timing
proof. External SDA/SCL timing/loading still needs qualification. The full
affected suite enables this model with `FES_NANO_HDMI_PROJECT` and the selected
`FES_YOSYS`; `FES_DE25_VENDOR_ARCHIVE` enables original-source mutation checks.

The ROM slave occupies peripheral-relative `0x10500`, spanning 128 bytes. It
shares the computer clock/reset and exports no bus master. This is not a verified
CPU address. Its Hold input reports explicit mailbox Hold; an independent
unloaded/failed gate keeps the machine reset. A premature Run retires the ROM
instead of automatically releasing execution when loading finishes. The actual
board wrapper and Avalon interfaces are included in the bounded host model for
both sizes, including malformed requests, partial writes, CRC failure, lost Hold,
reset retirement, exact clock enables and load/Run/Hold/return.

The distinct `de25-embedded-hps-first-rom-diagnostic-v1` package profile requires
`fes.embedded.rom` 1.0. Source checks bind ROM size, byte address, macro, component
files, generated slave/conduit ports, clock/reset routes and build identity.
These checks and host execution are separate from routed timing, a matched
ROM-capable boot profile, native retained-ROM admission and physical acceptance.
Its fresh sealed export uses format 3 with one `firmware` ROM requirement and a
`cpu-csr-v1` map bound to the unchanged core RBF and model-specific ROM size.
Firmware bytes are supplied separately. Other variants still use format 2.
The native command/build path requires a separately selected firmware source and
shared-linker receipt; a fresh matched ROM boot chain remains necessary before
a hardware trial. Existing
format-two ROM candidates are not relabeled or accepted as new exports.

The explicit `--sealed-computer` mode instead emits a source-bound computer
package candidate with `fes.computer` 1.0 and the shared
`de25-native-computer-v1` profile. It requires `build`, a clean committed source,
the reviewed HPS-first DDR variant and `--external-rom`; it excludes
`--sealed-diagnostic`. ZX80/ZX81 selection produces `fes.zx80`/`fes.zx81` with
separate 4/8 KiB firmware requirements. Fixed video, HID keyboard, ZX81 tape and
held-ROM delivery are required interfaces. The GP ABI remains 1.0; the Avalon
adapter's version 1.1 describes its separate transport.

The package choice, shared registry, computer ABI definition and generated GP
header enter the functional record before synthesis. Both endpoints receive
that record's build ID. Metadata publication and independent verification reject
a package choice that differs from the record; a diagnostic build cannot be
relabeled into this mode. This extends the existing private producer, with its
two captured revisions and unqualified electrical scope. It supplies no ordinary
runtime activation or hardware acceptance. Its normal Nano boot/firmware path
still needs integration and a fresh matched build and physical test.

The new 256-byte Avalon aperture is at peripheral-relative `0x10400`. The
generated handoff must also show it at `0x00110400` in the existing JTAG master.
Neither address is a verified CPU physical address. The default read responder
is an on-chip test fixture with experimental ABI `0x10203040`. The explicit
`--reader-memory hps-ddr` option connects the endpoint's read-only AXI port to
the existing HPS DDR adapter, with distinct ABI `0x10203041` and diagnostic
programming profiles. `--reader-memory hps-ddr-window` instead selects the
CPU-started fixed-window source with ABI `0x10203042` and its own diagnostic
programming profiles. These remain unregistered Nano runtime profiles.

`--computer-model zx81` (the default) retains the captured ZX81 machine and
firmware. `--computer-model zx80` selects the captured machine's ZX80 decoder,
NMI and tape-loader logic and installs the ZX80 firmware from the reviewed
combined ROM into its 4 KiB decode window. It rejects any other combined ROM
layout. Both diagnostics retain the existing built-in 16 KiB RAM; the ZX80
option is not a stock 1 KiB ZX80 product. The model enters the functional
fingerprint and canonical build record before synthesis. Verification checks
the actual model macro and both synthesis/simulation firmware copies, and the
native computer verifier uses that same macro. The HPS, DDR, clocks and master
inventory retain their existing contracts. These are distinct diagnostic
payloads, without ordinary Nano runtime admission or playable-core acceptance.

DDR selection requires `--ddr-bus-base` and `--ddr-bytes`: a nonempty,
page-aligned 32-bit **bus** window. Both the ordinary fingerprint and canonical
functional build record bind the backend and window. These numbers do not
establish the CPU mapping, a live DDR reservation or permission to release
bridges. The native cold DDR diagnostic can observe its pinned buffer, SMMU,
firewalls and source-bound idle master receipts with data held. Its earlier
on-chip physical pass does not qualify this changed producer or DDR traffic.

The manual DDR reader offers one burst at a time, rejecting unaligned, out-of-window,
wrapping and 4 KiB-crossing requests before AXI traffic. It snapshots a staged
descriptor, holds a stalled real address through disable, and consumes actual
RID/RRESP/RLAST and response backpressure. It has no synthetic response command
or write request producer. Platform Designer requires the complete AXI pin set;
AWVALID, WVALID and WSTRB are hardwired zero. The fixed zero read ID uses one
wire, fitting the existing shared HPS adapter without changing its ID width.
An unsolicited write response faults the reader. The generation/cookie drain endpoint remains
the authority; debug byte/data/checksum observations do not grant drain.

At the diagnostic aperture, manual DDR mode accepts only command bit 2 (reader ready)
and bit 3 (offer the staged request) at `0x80`; the on-chip responder's address
acceptance and injected-response bits reject. The existing address/length
registers are at `0x84`/`0x88`. Read-only offsets `0x94`/`0x98` report returned
bytes/XOR checksum, `0xa0..0xac` the last four data words, and `0xb0`/`0xb4` the
compiled bus window. They observe bus data even while disable drains a burst.
They do not implement scanout, CPU cache ownership or other-master shutdown.

The window mode requires exactly `--ddr-bytes 4194304` and a page-aligned bus
base. It reads packed 1280×720 XRGB frames, with up to four outstanding reads,
into a diagnostic IEEE CRC32 sink. Its private register map replaces the manual
diagnostic bank; the shared identity/enable/disable registers below `0x80`
retain their contract:

| Offsets | Window diagnostic operation |
| --- | --- |
| `0x80` | Full-width write `1`: START, only when ready and the staged generation/cookie are valid |
| `0x84`, `0x88` | Expected reader generation, low/high words |
| `0x8c` | Nonzero frame cookie, strictly increasing within a reader generation |
| `0x90` | STATUS; captures the following result registers together |
| `0x94`, `0x98`, `0x9c` | Captured frame generation low/high and cookie |
| `0xa0`, `0xa4`, `0xa8` | Requested bytes, delivered bytes and finalized CRC32 |
| `0xb0`, `0xb4`, `0xb8`, `0xbc`, `0xc0` | Compiled base, span, row bytes, stride and rows |
| `0xc4` | Signature `0x31435746` (`FWC1`) |
| `0xc8` | Local maintenance-reset hold output; global containment also requires the generated routes and other-master stop |

STATUS bits 0–5 are ready, busy, complete, aborted, fault and endpoint-aborting.
Staging during an active frame, stale/zero/reused tokens, unknown addresses,
partial accesses and invalid commands return a bus error without starting a
frame. The native `scanout.WindowReader` verifies identity, ownership and geometry,
submits START once, then checks the latched result and expected CPU-frame CRC.
It retires on uncertain failure and leaves drain and the boot-pinned arena to
its platform owner. Completion covers frame delivery into this sink; HDMI,
pixel clocks, buffer reclamation and global drain require their own proofs.

`verify_window.py` in the adjacent embedded-control prototype tests the actual
Avalon wrapper and native Go driver using rendered tenfoot frames. Its
`--project` option additionally checks a current sealed window project's
captured RTL and live model identity. The board `verify.py --project` dispatches
to that check for window mode. Default/labelled working-tree runs remain host
models. The dedicated native cold path supplies its trusted 4 MiB reservation.
The [matched full-width hardware diagnostic](../../../../docs/validation/2026-11-04-de25-fullwidth-window/README.md)
passes one complete frame, reader/master drain and bridge closure on TRWU8CBL.
The reservation remains pinned until reboot; HDMI, reuse and ordinary package
admission require their own qualification.

The HPS memory port uses 256-bit transfers, as required by the direct-DDR
[transfer-width contract](https://docs.altera.com/r/docs/814346/26.1/hard-processor-system-technical-reference-manual-agilex-5-socs/fpga-to-sdram-direct-cache-non-allocate).
`fes_de25_read_width` combines each aligned pair of 128-bit reader beats into
one 32-byte address transfer and splits the response in memory byte order.
Manual descriptors use 32-byte alignment and an even number of reader beats;
the frame geometry, byte counts and CRC remain the same. DISABLE retains the
buffered upper half until consumed, and raw response validity remains visible
to the endpoint even while the adapter is empty. Error responses and IDs pass
through both halves. The window verifier tests held halves, response errors,
unsolicited responses and a complete page alongside full-frame native reads.

DDR mode disables the unused coherent F2H hard interface and removes its JTAG
manager and translator. The DDR-only generator requires exactly the
`f2s_data_width=256` to `0` change and the five known Quartus 26.3 defaults,
both before and after compilation. It retains the original parameter inventory
and repeats the captured producer’s boot-data, handoff and PLL checks.
Unexpected HPS/EMIF changes reject. Otherwise constant reset containment lets synthesis
prune their connection while leaving the enabled HPS primitive illegally
unconnected. Both remaining JTAG Avalon managers stay held by the exported
active-low constant maintenance reset from configuration onward. The SDM
programming/recovery link is separate. The generated handoff checks disabled
F2H, the exact remaining manager inventory and every reset route. Read-only
offset `0xb8` reports the local hold output.
The inherited DDR probe keeps its separate control map and still requires an
independent CANCEL/idle/accepted-versus-completed proof. The DDR model joins
the actual probe RTL to the firmware's `nano_probe_stop.c` helper. Neither the
hold indicator nor a reader acknowledgement supplies complete master, memory
mapping or physical drain qualification.

Run the portable model and native adapter check without Quartus:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify.py --standalone
```

That command checks both backends. The DDR part also runs independently with
`python3 sources/misteross/prototypes/de25_board_control/verify_ddr.py`.
Its injected external AXI source checks held addresses, consumer stalls,
partial drain, window/page boundaries, maximum bursts, exact data, protocol
faults and unsolicited responses. These are RTL models, not physical DDR tests.
For a generated DDR project, the normal `verify.py --project` selects that
external-bus model; `test_handoff.py` checks the generated read-only interface,
real HPS route, source bytes and window parameters, including altered copies.

For a private generated project or a diagnostic SOF, supply a full Git commit
for an existing DE25 ZX81 producer, its matching locked Terasic archive, a
Quartus Pro installation, and a fresh path under this worktree's `out/`:

```sh
python3 sources/misteross/prototypes/de25_board_control/build.py build \
  --board-revision FULL_COMMIT --vendor-archive /absolute/vendor.zip \
  --quartus-root /absolute/altera_pro/26.3 --output out/de25-control-board
python3 sources/misteross/prototypes/de25_board_control/verify.py \
  --project out/de25-control-board --output out/de25-control-board-check
python3 sources/misteross/prototypes/de25_board_control/test_handoff.py \
  --project out/de25-control-board --output out/de25-control-handoff-check
```

`project` prepares source only; `generate` additionally validates the actual
Platform Designer handoff; `build` compiles the SOF. The handoff test also
checks that altered copies of that generated map or source are rejected. Board and overlay commits,
all captured sources, vendor hash, generated maps and artifact evidence remain
separately recorded. Working-tree overlay inputs require `--allow-dirty` and are
labelled diagnostic. No command programs hardware, writes media, creates an RBF
or registers a FES profile in the default mode. Timing, boot pairing, physical access, firmware
containment and board acceptance require separate evidence.

Compilation runs the standard Quartus Pro leaf tasks in order, through
elaboration/synthesis, all fitter stages, signoff timing and assembly. Each
task has its own 120-second limit, log and heartbeat. The reviewed HPS-first
`hps-ddr-menu` personality gives placement 180 seconds: a same-source,
same-constraints/seed placement measurement took 139 seconds. Other tasks and
personalities retain 120 seconds. The build receipt records every task's actual
limit. A failure stops before
sealing and terminates only that task's process group; partial outputs remain
for diagnosis. No timing option, seed, constraint or optimization mode changes.
The canonical record binds this recipe and the actual `quartus_syn` executable;
it does not use the older `quartus_map` identity as a synthesis proxy.
`--jobs 1..16` selects the compiler worker limit (default 2). The project,
functional build record and build receipt record the count; verification
requires all three to agree. Use the explicit available CPU count on the build
host when a two-worker fitter cannot finish within the per-task limit.

For a content-sealed diagnostic format-2 package, add `--sealed-diagnostic` to
the `build` command. This requires a clean checkout and uses the selected
board commit, all captured source hashes, vendor archive hash, selected
Quartus executable hashes and build parameters in the canonical record. The
standard package exporter verifies the overlay source closure and emits a
read-only package and build record under `board/hps/packages/`. The board
source belongs to a second revision; the diagnostic verifier checks those
captured bytes against that Git commit separately. The tool identities are not
production toolchain authentication, and a hash is not a signature.

The default FPGA-first diagnostic conversion adds the vendor SPL from the locked
archive and produces a full SoC RBF. Its effect on a running HPS and DDR has not
been established: **do not physically reconfigure the board with this package**.
Its profile and ABI are diagnostic and have no physical runtime registration.

To prepare a separate HPS-first candidate, select a board revision whose captured
producer supports `--hps-first` and add `--hps-first --sealed-diagnostic` to the
build command. The build selects `HPS_INITIALIZATION "HPS FIRST"` before
synthesis, includes that order in the functional identity, and uses Quartus PFG
to produce `hps-first.hps.rbf` and `hps-first.core.rbf`. It inspects both
outputs and requires their reported device and HPS I/O hashes to match the
assembler report. The format-2 archive contains the **core** RBF; the unsigned
`hps-first-pair.json` binds its package/build IDs to the exact phase-one file,
HPS I/O hash and PFG identity. These are diagnostic artifacts, not a booted or
qualified configuration. The producer does not write flash or program a board.
An unrelated earlier Linux experiment suffered a transfer timeout and a stuck
cleanup; HPS-first build success does not resolve that hardware failure.

The runtime's experimental HPS-first identity profile requires a trusted
platform handoff matching that pair before retirement of the old owner and
again before firmware START. The current test handoff is simulated. A sidecar
or package cannot prove what actually booted a board, and no physical boot
handoff provider or physical programming profile is registered here.

To exercise either sealed package and the same live ID through simulated
CPU/firmware/RTL:

```sh
python3 sources/misteross/prototypes/de25_board_control/verify_sealed.py \
  --project out/de25-control-board --reference /path/to/pinned/socfpga_sip_svc.c \
  --output out/de25-control-sealed-check
```

The model policy is explicitly `qemu-model-only`. The test uses the actual
format-2 archive, prepared TF-A functions and endpoint RTL but simulated SDM,
CPU accesses, memory and bridges. The separate Avalon model checks the board
wrapper with the same build ID. None is a board acceptance or a boot image.

`hardware.tcl` is a separately invoked System Console test for an explicitly
selected and already configured device and fabric master. It checks all build
words and ABI before writes, then exercises the on-chip held-address, drain and
response-fault cases. The portable verifier runs that same script against the
Verilated Avalon model, including the no-write wrong-identity path. A real
caller must bind the selected device and image, impose an external deadline,
and handle any uncertain access without replaying it.
