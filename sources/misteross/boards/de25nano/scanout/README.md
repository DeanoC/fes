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
most sixteen 16-byte beats, split at 4 KiB boundaries, and retain their full
32-bit byte addresses. The FPGA has a real memory connection and no local
pattern responder in this lane. DE25 enables the reader's optional read-ahead:
up to sixteen short bursts can overlap, while FIFO occupancy plus all accepted
and held response reservations never exceeds 256 beats. Its memory crossing
has a 256-beat response FIFO. STOP still drains every accepted command before
ownership can be released; read-ahead does not relax underflow detection.

The generic reader/video extensions default to the existing static-base,
128-beat and unchecked-response behavior. This lane opts into the dynamic
window, sixteen-beat bursts, read-ahead, response checking and complete-frame ACK. A
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

The external cold/configuration reset asserts both sides of both crossing
bridges asynchronously. The child has separate reset bridges for the pixel
and 100 MHz domains; each deasserts synchronously on its own clock. The pixel
reset drives the endpoint and pixel-clock sides of both `mm_ccb` bridges. The
100 MHz reset drives their peripheral-clock sides. The external source is the
Q of the preserved first-lock register, which asserts asynchronously with the
existing top reset. Initial release waits for the first qualified pixel PLL
lock. Later PLL-lock loss latches a fault in the running 100 MHz domain and
stops/drains the endpoint once the pixel clock resumes; it does **not** reset
live FIFO or reader accounting. The DE25 scanout instance keeps its fixed
raster and raw HS/VS running while the endpoint is stopped, faulted or reset.
HDMI data/DE at the pins use registered pixel-domain visibility state. The
visibility request is a 100 MHz register, so one source crosses into a
two-stage pixel synchronizer. Loss of the qualified lock clears a pixel-domain
reset release at once, even with the pixel clock stopped, and that release
clears the synchronizer; it releases through pixel-clock stages. Data blanks
immediately on lock loss or reset while HS/VS finish their current pulses. Visibility returns after
a complete horizontal sync pulse while VS is inactive. This keeps DE, HS and VS
gating away from active pixels and complete sync pulses, including a lock
failure in the middle of HS. No asynchronous fault, lock or reset term is
combined directly into the HDMI data/DE pins. A qualifier stretches brief
loss/relock events so video cannot briefly reappear before the sticky fault
latches. This detects PLL lock loss, not independent pixel-clock progress.
Permanent clock loss cannot advance the control bridge or local drain proof.
Forced common reset starts a new bridge and reader epoch and cannot prove
prior traffic drained.

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
or pointer-path cuts are added. The board SDC cuts ten asynchronous clear
pins: four on the configuration and lock-qualification synchronizers, four on
the generated pixel and 100 MHz reset-bridge synchronizers, and two on the
pixel-domain visibility release. The visibility synchronizer clears from that
release and the registered data/DE gate from the synchronizer, both
pixel-clock registers, so their recovery and removal stay timed; the SDC only
checks that those clear pins exist. It also cuts only the path from each
registered source to the first D input of the sticky-fault and
HDMI-visibility synchronizers; later synchronizer stages, line gating and
output paths remain timed. Generation rejects a board whose data/DE pins are not driven by the
registered gate or an SDC that does not name all ten clear pins. No HDMI IO
delays are fabricated.

Native tests exercise the real reader/video/control RTL with the same guarded
M20K hint adaptation used by the build. They cover complete frames and lane
ordering with delayed/zero-latency responses, descriptor/sequence/slot checks,
snapshots/byte enables, command stalls/drain, reset during accepted and stalled
bursts followed by SUBMIT/CLEAR, missing responses, underflow, response errors
and sticky clock failure. Separate benches cover the exact
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

Production binding remains closed. SID/firewall/SMMU and DMA backing,
firmware/DT/boot pairing, global reset fencing, the vendor HDMI clock-voltage
discrepancy and external IO timing, physical CDC/MTBF and exact-artifact board
acceptance remain gates. Warm-reset ACK stays inactive: probe/scanout-local
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
