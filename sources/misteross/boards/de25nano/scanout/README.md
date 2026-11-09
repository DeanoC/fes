# DE25 allocated-buffer scanout diagnostic

This lane extends the combined Terasic GHRD/display shell with an actual
read-only framebuffer path. It starts **disabled**, issues no scanout reads
until a valid explicit submission, and creates no admitted runtime profile.
The default maintenance build preserves the HPS/EMIF parameters, mailbox,
diagnostic DDR probe and JTAG paths. The explicit Linux-DMA build removes the
probe and memory-debug path, as described below. Both keep inactive active-low
warm-reset acknowledgement. It uses Quartus
Prime Pro 26.3.0 for `A5EB013BB23BE4SCS` and never programs hardware.

For loading through Linux from the matching HPS-first bootstrap, select the
HPS-first build explicitly:

```sh
python3 build.py build --hps-first --quartus-root /absolute/altera_pro/26.3 \
  --vendor-archive /absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --output /absolute/new-scanout-output
```

The selected initialization order is captured in the project and input receipt.
After compilation, the shared HPS conversion helper enriches the SOF with the
unchanged vendor loader and emits `linux-complete.sof`, `linux-boot.hps.jic`
and `linux-boot.core.rbf` under `combined/hps/`. The Linux programming receipt
binds their hashes and the captured scanout source identity. Conversion does
not program flash or FPGA. Existing phase outputs are refused, and mismatched
captured/project boot order fails before conversion. Without this option the
explicit initialization order remains `AFTER INIT_DONE` for the JTAG diagnostic.

These artifacts still need bootstrap compatibility, Linux-load and allocated
DMA/frame-lifetime acceptance on the board. They confer no runtime admission
or DMA permission and do not change the installed QSPI baseline.

The [shared scanout contract](../../../../mister-packages/docs/de25-scanout.md)
owns register offsets, generation/sequence rules and allocation lifetime.
The CPU-facing 256-byte control slave is at lightweight peripheral offset
`0x10200`; its lower 128 bytes forward to the pixel-domain endpoint.
One immutable, dynamically supplied 8 MiB DMA grant contains two 4 MiB slots
for 1280×720 BGRX8888 frames. Base address zero is allowed; physical backing,
translation and permission are separate admission requirements. The first
submission selects slot 1 before any fetch. Accepted, current and completely
displayed sequences are distinct. Only a complete frame without missing
pixels or response errors acknowledges a flip; software retains the prior
slot until that completion. One flip may be pending.

The endpoint control registers, video and reader use the 74.25 MHz pixel clock.
A system-clock control guard sits before the control crossing. Its upper-half
identity, status, faults and write-once epoch registers remain local at 100 MHz.
It uses the inherited peripheral reset, independently of initial pixel PLL lock.
A request completes for the CPU within 100,000 system cycles after acceptance,
including when the downstream command or response is stalled. A timeout or
transport error permanently poisons the guard. An unaccepted downstream command
remains asserted with stable payload until acceptance; a late response is consumed
without a second CPU response. Poison prevents every subsequent endpoint access.
CPU responses remain OKAY with dummy zero read data on failure to avoid a bus
exception; software must validate local identity, epoch and faults before and
after endpoint access. Successful bus completion alone proves nothing. The guard
never resets a bridge or reports DMA drain. Its bound assumes the system clock,
peripheral bus and reset release continue to operate.

Explicit `mm_ccb` 19.4.1 bridges carry ordered 32-bit control traffic from
100 MHz and 128-bit memory traffic back to 100 MHz, then into the existing
256-bit F2SDRAM adapter. Both bridges preserve two-bit responses; the CSR
endpoint also returns explicit write completions. Reader commands have at
most sixty-four 16-byte beats, split at 4 KiB boundaries, and retain their full
32-bit byte addresses. The FPGA has a real memory connection and no local
pattern responder in this lane. DE25 enables the reader's optional read-ahead:
independent bursts overlap, while FIFO occupancy plus all accepted
and held response reservations never exceeds the 2,048-beat (32 KiB) local
pixel buffer. Accepted and held responses are independently capped at 511
beats within the memory crossing's 512-beat response FIFO. The existing nine-bit
outstanding count includes each entire held command and cannot wrap at 512.
AXI bursts have at most 64 transfers on the 256-bit interface and the generated
path retains its sixteen-transaction HPS limit. STOP still drains every accepted command before
ownership can be released; read-ahead does not relax underflow detection.

The generic reader/video extensions default to the existing static-base,
128-beat and unchecked-response behavior. This lane opts into the dynamic
window, sixty-four-beat bursts, read-ahead, response checking and complete-frame ACK. A
shared zero-latency response-accounting fix preserves an accepted burst's
budget, and the complete-frame controller retains a new submission accepted
on the previous frame's last visible pixel. Existing consumers explicitly
tie off the new optional ports; default DE10 package selections are unchanged.
The shared reader/video fixes still affect rebuilt existing consumers; their
package/HIL acceptance is separate from this undeployed DE25 build lane.

STOP halts new fetches while holding an asserted command stable until
acceptance, then drains all accepted responses and queued pixels. Missing
responses never time out into Drained. CLEAR is legal only after drain and
resets local video/reader state, invalidates the active grant/generation and
clears staged configuration. It never resets either crossing bridge. An
unexpected response invalidates accounting permanently for that load and
cannot report Drained or be cleared locally. SNAPSHOT freezes coherent status
and all multiword identities in one pixel-clock observation; its serial
changes only on the explicit snapshot command or cold reset.

The external cold/configuration reset asserts both crossing bridges. Each
receiving domain releases through its generated parent reset controller.
The source is the preserved first-lock register: initial release waits for
reference-clock qualification and the system acknowledgement. Later PLL loss
keeps this source released, so live FIFO pointers, credits and accepted reads
remain intact. STOP drains accepted responses after their clocks resume.
The endpoint keeps its fixed raster and raw HS/VS running through stop/fault.

A retained token at 50 MHz records even brief lock loss while peer clocks
are stopped. A running system epoch never acknowledges another token; its
clock fault latches within three system edges and remains terminal until the
external reset. The pixel domain observes qualification through ordinary
synchronizer data registers and closes DisplayReady within three pixel edges.
Its acknowledgement requires allow-low and a closed visible line. The
reference controller waits for both acknowledgements and observed inactivity
before it can permit another visibility epoch. Retaining acknowledgement state
across a global reset avoids aliasing a new token with a reset-to-zero ACK.

Reference qualification blanks HDMI data/DE immediately. Pixel visibility
closes at a complete HS trailing edge outside VS, preserving complete sync
pulses; rearming also waits for that boundary. Eight new crossings cut only a
single registered source to stage0 D, with unique-object guards. Their later
stages and consumers stay timed. The digital protocol does not establish
physical CDC/reset or HDMI output timing. A common reset begins a new epoch
and cannot prove prior traffic drained.

Canonical MENU selects `shared-fifo-reset-release-v2`. Each FIFO side uses
its existing local parent as the sole asynchronous reset of functional state.
One five-register flag pipeline per receiving clock holds those same reset
values synchronously until both parent releases have propagated for the
original five edges. The local parent also asserts the flags asynchronously;
only the peer parent enters the first data stage. This avoids a second
asynchronous reset source for functional state and keeps local release,
interstage and consumer timing required.

The derivation checks and retains all eleven generated bridge/parent files,
preserves command/response clock and reset polarity, and reproduces every
changed byte before and after compilation. Vendor reset and pointer SDCs are
unchanged. Canonical preparation adds only the two uniquely checked peer
source-to-first-D cuts; the standalone diagnostic retains its original FIFO
chains. The digital reset-envelope proof and generated transport tests do not
establish metastability, global-reset allocation fencing or native recovery.

Run from this directory:

```sh
make test
make sim
make project VENDOR_ARCHIVE=/absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/absolute/new/output
make build QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus VENDOR_ARCHIVE=/absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip OUTPUT=/absolute/new/output
```

`project` allows explicitly labelled working-copy inspection. `generate` and
`build` require clean committed source. The runner captures its dependency
closure from one Git revision, executes retained helper bytes and preserves
the locked archive/source identities. It reuses the combined preparation,
removes the private pattern/old video sources and replaces them with this
real endpoint and clock shell. The actual project and SOF are under
`combined/hps/project/`; the final artifact is
`combined/hps/project/output_files/golden_top.sof`. The root `snapshot.json`,
retained `functional-source/`, final `combined/hps/inputs.json`, generation
receipt and build receipt distinguish original and adapted M20K reader bytes.
The intermediate `upstream-preparation-inputs.json` describes preparation
before the private pattern sources were removed; it is not the final recipe.

Generation validates the elaborated CSR/memory routes, address-map region,
bridge parameters, response/write-completion interfaces, clocks and common
reset connections, in addition to inherited mailbox/probe and HPS/EMIF
checks. The pixel PLL parameters are checked after generation and compilation.
Generated bridge FIFO constraints remain authoritative: no broad clock groups
or pointer-path cuts are added. The standalone diagnostic board SDC cuts ten
asynchronous clear pins: four on the configuration and lock-qualification
synchronizers, four on the generated pixel and 100 MHz reset-bridge
synchronizers, and two on the pixel-domain visibility release. The canonical
MENU profile removes the two 100 MHz reset-bridge clear pins from that set,
leaving eight. Its source and receiver share the 100 MHz system PLL, so reset
recovery and removal must be timed. The visibility synchronizer clears from that
release and the registered data/DE gate from the synchronizer, both
pixel-clock registers, so their recovery and removal stay timed; the SDC only
checks that those clear pins exist. It also cuts only the path from each
registered source to the first D input of the sticky-fault and
HDMI-visibility synchronizers; later synchronizer stages, line gating and
output paths remain timed. Generation rejects a board whose data/DE pins are not driven by the
registered gate or an SDC that does not name the profile's exact clear pins. No HDMI IO
delays are fabricated.

The canonical `timed-system-reset-release-v3` profile also removes the generated
reset-controller clear exceptions only for `soc_inst|rst_controller`,
`soc_inst|subsys_periph|rst_controller` and `soc_inst|rst_controller_002`.
All three receive a reset released by the same 100 MHz clock. Canonical MENU
removes the vendor JTAG initiators, so their former reset receivers are absent.
The private build
requires exactly six generated reset SDC files matching the reviewed Quartus
26.3 bytes, retains their originals and changes only the two conditional clear
exceptions. Pixel, FIFO and internal JTAG reset exceptions remain intact. Generation
and compilation verify the derived board SDC, all six generated files and their
receipt; regenerated or changed constraints stop the build before Linux phase
conversion. The canonical record binds this policy before IP generation.

The canonical `managed-scanout-initiators-v3` profile removes the entire vendor
debug subsystem and its separate ACE5-Lite F2H translator. The maintenance
Linux-DMA profile above retains its other debug paths. FPGA configuration over
JTAG remains available; these removed components are Avalon transaction masters.
The normal HPS lightweight control route, HPS-to-OCM route and isolated SID13
scanout-to-F2SDRAM route remain required.

Generation checks all eight elaborated initiator endpoints, including the CPU
bridges and relay stages; they are not eight independent DMA devices. Exactly
one fabric path enters HPS, through the checked F2SDRAM adapter. Every control
stage must have its single expected ingress. The unused native F2H bridge is
disabled with the supported HPS `f2s_data_width=0` (Unused) parameter. Its child
exports, top-level clock/reset connections and generated HPS ports must be
absent. Missing/extra masters,
control bypasses, retained debug instances, wrong routes and stale generated
interfaces fail closed. The captured verifier and the complete generated
graph receipt are rechecked before and after compilation, before conversion.
The full vendor HPS/EMIF parameter review permits only the existing F2SDRAM
SMMU exposure and this F2H disable for canonical MENU. Standalone maintenance
keeps its existing bridge parameters. The disabled bridge is checked in the
actual HPS IP, elaborated interface definition and generated parent RTL; no
manual parent RTL tie-offs are used. This parameter change still requires
separate boot-firmware and native acceptance.
The GHRD `source ./jtag.sdc` statement is also retired because the fabric JTAG
interfaces it constrains are absent. Both reviewed SDC input hashes are required;
the original GHRD constraints are retained beside the derived file and the exact
derivation is checked before and after compilation. The original JTAG SDC stays
unchanged in the retained project. Other clock/reset/IO constraints remain.
The canonical record selects this policy before generation. This removes
unmanaged fabric paths; it does not establish physical DMA/reset lifetime or
relax any timing, IO, package or native acceptance gate.

Native tests exercise the real reader/video/control RTL with the same guarded
M20K hint adaptation used by the build. They cover complete frames and lane
ordering with delayed/zero-latency responses, descriptor/sequence/slot checks,
snapshots/byte enables, command stalls/drain, reset during accepted and stalled
bursts followed by SUBMIT/CLEAR, missing responses, underflow, response errors
and sticky clock failure. A complete-frame case pauses commands and responses
for 6,000 pixel clocks during active scanout, then checks every pixel, zero
underflows and complete STOP drain. This models buffer elasticity; it does not
bound physical DDR latency or qualify concurrent CPU rendering. Separate benches cover the exact
last-pixel submission corner, full HS/VS continuity through endpoint disable
and reset, clock-shell startup/brief unlock/relock, and pixel-domain HDMI
gating at a complete sync boundary using digital PLL/I2C models. The actual guard
RTL bench covers acceptance stalls, absent/late/error/wrong-kind responses,
exact-deadline completion, stable retained commands, permanent poison and local
register access throughout downstream uncertainty. The generated-interconnect
helper tests the actual two-clock bridges and width adapter after generation;
none of these modeled checks establishes physical memory or HDMI operation.

The generated-interconnect simulation uses an explicitly guarded M20K storage
model because the installed vendor primitive models are protected or black
boxes. Generated FIFO pointers, synchronizers, credits and routing remain
unchanged. An elaborated inventory permits only the four reviewed storage
instances, and a separate storage self-test checks the used clock, address,
output-latency and enable behavior. Runtime guards reject unwritten RAM data
at the actual FIFO transfer and collisions with its live unread head. The
receipt labels this **WITH MODELED M20K STORAGE**; vendor primitive equivalence,
undefined power-up data, mixed-port collision behavior and physical CDC remain
unproven.

Canonical package builds use Terasic's stock HDMI_ASx4/SCS golden board profile,
as selected by the user. The review binds the verified resource ZIP, all 37
actual fitted pins and I/O standards (including the vendor's 1.1-V clock pin),
all pixel PLL parameters and the unchanged transmitter register table. It also
checks the complete guarded I2C derivation. The 29 HDMI data/control outputs
and asynchronous status LED retain their raw missing-output-delay findings;
this reference acceptance does not claim an independent PCB timing model.
Other findings and all internal timing checks still block package admission,
and export requires an independently identical RBF. The standalone diagnostic
does not gain package admission from this policy.

Native production binding remains closed. SID/firewall/SMMU and DMA backing,
firmware/DT/boot pairing, global reset fencing, physical CDC/MTBF and
exact-artifact board acceptance remain gates. Warm-reset ACK stays inactive: probe/scanout-local
drain cannot acknowledge the retained JTAG and HPS-origin paths. No SD card,
QSPI, hardware, kit or harness is touched by this build lane.

The [HPS-first compilation record](../../../docs/validation/2026-10-31-de25-scanout-linux/README.md)
retains the exact source/artifacts and open timing, boot and DMA gates.

The additional `--linux-dma` option requires `--hps-first`. It exposes HPS
SMMU address USER ports, drives the generated nonsecure stream ID 13 in bits
24:9 with nonsecure bit 8 clear, and preserves vendor USER attributes 7:0
as 0xE0. The raw word is 0x0001AE0; the translator CSR layout is different. Generation verifies the
25-bit adapter/HPS interfaces and the constant driven by the QIP-selected HDL.
Only the captured `f2sdram_SMMU` false-to-true HPS parameter change is permitted;
other HPS/EMIF or boot-data changes still fail. This profile removes the DDR
probe instance/control window/export and the memory JTAG master/export/address
window. In this isolated profile only, the ID-preserving adapter's AXI
read and combined credits are raised from one to sixteen, matching the selected
HPS interface. Write credits remain one. Generation verifies both adapter
interfaces and the HPS read/combined limits from their actual handoff metadata.
Other JTAG/OCM/F2H paths remain. Generation requires exactly one route
into the F2SDRAM adapter, from scanout's memory crossing, and exactly one adapter
route into HPS F2SDRAM; retained probe/debug instances, duplicate/extra masters,
nonzero address translations and additional HPS F2SDRAM paths fail. The default
maintenance build still requires the probe and memory JTAG route.

Input, generation, compilation and Linux phase receipts capture the explicit
master profile. Phase receipts also bind input/generation hashes, so source
identity alone cannot mix maintenance and Linux-DMA artifacts. The Linux-DMA
profile remains unadmitted: generated topology does not establish physical
translation, Linux device/domain lifetime, control qualification, global reset
fencing or hardware acceptance. The kernel production probe remains closed.

The [Linux-DMA profile compilation record](../../../../../docs/validation/2026-10-31-de25-linux-dma/README.md)
retains the actual stream wiring, kernel build, and open device/lifetime gates.

The [bounded-control compilation and generated-interconnect proof](../../../../../docs/validation/2026-10-31-de25-bounded-control/README.md)
retains the system-clock guard, clock-loss case, exact artifacts and open kernel
DMA/device/domain and normal-kit acceptance requirements.

The [isolated-stream compilation and transport proof](../../../../../docs/validation/2026-10-31-de25-isolated-stream/README.md)
retains the actual sole-master graph, profile-bound artifacts, eight passing
transport cases, original failures and open Linux/physical acceptance gates.

The current endpoint is fabric version 1.1. Its read-only 128-bit build identity
is bound to the selected functional source/profile, passed as an actual Qsys
HDL parameter and checked in generated handoff metadata. The owner UAPI version
3 checks this identity before allocation and across later snapshots. Unbound
zero identities and the older endpoint/API versions are refused. These source
changes require a matching new kernel and FPGA image; they do not extend the
older physical diagnostic evidence or enable normal runtime startup.

DisplayReady reports registered HDMI visibility after configuration completion;
loss during scanout latches a Display fault and cannot acknowledge the frame.
The board still releases the control/memory bridges after initial clock lock,
independently of a slow transmitter configuration. Software can wait through
cold snapshots without enabling DMA. These changes await new exact-artifact
compiler, kernel and board validation.

The generated transport simulation requires Quartus’s explicit BUILD_ID forwarding
and compares all four live build words with the verified generation receipt.
This checks the actual generated endpoint wrapper as well as its IP metadata.

Add `--board` to compose the selected board with those generated reset
controllers and the existing FIFO/control/AXI logic:

```sh
python3 interconnect_sim.py --board --project /absolute/generated/combined/hps/project --output /absolute/new/board-proof
```

This mode checks board/reset/SDC bytes against the captured input receipt and
compares all four live endpoint identity words. One disabled-startup/storage
case and twelve queued-read cases cover three phases and independently stopped
reference, system and pixel clocks. Responses remain withheld across a brief
PLL-loss event and STOP; grant identity and outstanding reads must survive.
Only the exact responses can establish drain, and CLEAR cannot heal the
terminal clock fault. The PLL clock/lock, HDMI completion and physical memory
storage are explicit boundary models. This supplies no HPS/SMMU/DDR or global
reset/allocation epoch acceptance.

Add `--startup` to `interconnect_sim.py` to test finite startup combinations
using the compiled project's five actual parent reset-controller instances and
their separate root and first-lock reset sources. The proof guards the QIP,
passthrough reset bridges, controller parameters, clocks and seven downstream
reset connections before building the model. It retains and hashes those inputs.
Eight clock phases and nine first-lock delays exercise early control requests,
disabled/grant-free startup and bounded timeout with permanent poison after a
late release. Eight cases also submit a grant, traverse accepted AXI reads and
responses, then STOP and drain. The existing explicit M20K storage model and
FIFO validity guards remain active. This finite proof does not establish
physical reset/metastability behavior, outer HPS/JTAG coordination, global
reset or boot/allocation epoch acceptance; all raw reset-domain findings remain
subject to signoff.

HDMI configuration runs from CLOCK0_50 while the system PLL uses CLOCK2_50.
The readiness crossing has a registered source and two receiving registers;
its SDC names exactly that source and stage0 D, with one-object checks. Later
stages, fanout and reset recovery/removal remain timed. No clock group is cut.

The scanout project derives its own named HDMI configuration/controller cells
from the locked vendor inputs. SDA acknowledgements and the transmitter
interrupt pass through two CLOCK0_50 input stages. Only each external pin's
path to its first D input is cut; later stages, controller fanout and resets
remain timed. The open-drain SDA output and the divide-by-5002 bus clock are
preserved. The initializer waits for END to go low after GO before accepting
completion, so the reset/previous completion cannot skip the first LUT row.
Clocked controller assignments use nonblocking semantics to remove simulation
races. The original vendor files are retained outside the compiler project;
the project records and rechecks both original and derived hashes.

`hdmi_io_sim.py --project /absolute/prepared/combined/hps/project --output
/absolute/new/proof --yosys /absolute/yosys` exercises the actual retained
derived controller with a digital open-drain slave. It checks every byte in all
33 writes, a full interrupt-triggered reconfiguration, address/subaddress/data
NACK retries, delayed acknowledgements and the original clock period. It also
compares the controller assignment normalization with Yosys induction after
`async2sync` reset modeling. That proof does not cover physical asynchronous
reset timing or metastability. The clock/visibility bench retains its separate
modeled configuration-completion boundary.

External output timing remains unqualified. The
[ADV7513 data sheet, revision B](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf)
specifies 100 ns I2C SDA setup/hold and 1.8 ns video setup/1.3 ns hold. Board
propagation/skew budgets and physical acceptance are still required; no zero
board delay or synthetic output constraint is supplied here.
The scanout initializer delays its open-drain SDA request by eight 50 MHz
reference edges (160 ns), retaining the controller's SCL, divider, register
values and ACK sampling. Without that delay, the vendor controller updates
SDA on the same edge that lowers SCL. Reset releases the delayed output.
The existing protocol bench checks the 100 ns minimum on ordinary master
data-bit transitions as well as every register write, NACK retry and interrupt
restart. The digital hold result does not qualify physical pin timing or
the modeled slave's ACK edges.

The separate canonical MENU producer runs from the misteross module:

```sh
python3 scripts/build_de25_menu_package.py --compile-only --quartus-root /absolute/26.3 --vendor-archive /absolute/ResourcePackage.zip
```

It hashes the actual tool installation and controlled execution, writes
`build-inputs.json` before preparation, and passes its canonical identity into
the actual Qsys BUILD_ID parameter. Outputs default to a fresh attempt under
`build/de25-menu/<build-id>/`. Compile-only creates no manifest or package.
Without `--compile-only`, all reported timing checks must pass, the complete
85-rule inventory must have zero unreviewed or waived findings, and a second
compilation must reproduce the RBF,
before the unchanged normal package exporter runs. Current external IO/reset
findings block that export. Candidate compilation does not open any physical
or normal-kit acceptance gate.

The explicitly approved exception accepts only the missing-input-delay
findings on `HDMI_I2C_SDA` and `HDMI_TX_INT`. After each compilation,
`async_inputs.py` runs `async_inputs.tcl` on a private copy of the final fitted
project. It checks the exact raw-input/first-stage fanout, intended controller
consumers, same-tile two-stage chain, timed 50 MHz stage setup/hold and the
included MTBF calculation at Quartus's default 6.25 million transitions/second.
Both modeled worst-case MTBF values must exceed the tool's one-billion-year
reporting cap. This is a model result for those two chains, not physical or
whole-board qualification. The canonical build record includes the policy;
the review binds its raw reports to that record, RBF and compiled database.
No clocks, toggle rates, constraints or waived findings are changed.
Quartus `~DUPLICATE` copies of the reviewed ACK1/2/3 and LUT_INDEX0..5
registers are permitted after stage two. Raw inputs and stage zero still have
exactly one consumer each; other register names or copy suffixes are refused.

## Standard-core session display adapter

`fes_de25_session_display.v` combines the existing kernel-owned scanout CSR,
shared GP display control/CDC and the shared premultiplied compositor. It follows
the incoming machine raster. Kernel publication supplies the only memory source;
GP submit commands are rejected, and GP observations use actual kernel pending
and displayed counters. A close waits for STOP/drain and frame-boundary hiding.
A subsequent CLEAR and new grant restart the scanout sequence without resetting
the incoming machine video. Standalone MENU retains its existing internal-raster
configuration; unused session ports are terminated by its Qsys component.

Run `python3 sim.py --case session` for the actual RTL test of transparent/alpha
blending, continuous sync, relocated bounded memory reads, rejected GP publication,
held-response close and new-generation allocation/reopen. The normal `sim.py`
run includes it. Memory backing and machine video are test models. The adapter is
not yet selected by the ZX81 package producer or production runtime; no new
Quartus artifact or hardware support is claimed.
