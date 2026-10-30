# DE25 allocated-buffer scanout diagnostic

This lane extends the combined Terasic GHRD/display shell with an actual
read-only framebuffer path. It starts **disabled**, issues no scanout reads
until a valid explicit submission, and creates no admitted runtime profile.
It preserves the existing HPS/EMIF parameters, mailbox, diagnostic DDR probe,
JTAG paths and inactive active-low warm-reset acknowledgement. It uses Quartus
Prime Pro 26.3.0 for `A5EB013BB23BE4SCS` and never programs hardware.

The [shared scanout contract](../../../../mister-packages/docs/de25-scanout.md)
owns register offsets, generation/sequence rules and allocation lifetime.
The new 128-byte slave is at lightweight peripheral offset `0x10200`.
One immutable, dynamically supplied 8 MiB DMA grant contains two 4 MiB slots
for 1280×720 BGRX8888 frames. Base address zero is allowed; physical backing,
translation and permission are separate admission requirements. The first
submission selects slot 1 before any fetch. Accepted, current and completely
displayed sequences are distinct. Only a complete frame without missing
pixels or response errors acknowledges a flip; software retains the prior
slot until that completion. One flip may be pending.

Control registers, video and reader all use the 74.25 MHz pixel clock.
Explicit `mm_ccb` 19.4.1 bridges carry ordered 32-bit control traffic from
100 MHz and 128-bit memory traffic back to 100 MHz, then into the existing
256-bit F2SDRAM adapter. Both bridges preserve two-bit responses; the CSR
endpoint also returns explicit write completions. Reader commands have at
most sixteen 16-byte beats, split at 4 KiB boundaries, and retain their full
32-bit byte addresses. The FPGA has a real memory connection and no local
pattern responder in this lane.

The generic reader/video extensions default to the existing static-base,
128-beat and unchecked-response behavior. This lane opts into the dynamic
window, sixteen-beat bursts, response checking and complete-frame ACK. A
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
HDMI data blanks on reset/lock loss, then visibility changes after a complete
horizontal sync pulse while VS is inactive. This keeps DE, HS and VS gating
away from active pixels and complete sync pulses, including a lock failure in
the middle of HS. A qualifier stretches brief loss/relock events so video
cannot briefly reappear before the sticky fault latches. This detects PLL lock
loss, not independent pixel-clock progress. Permanent clock loss cannot advance
the control bridge or local drain proof. Forced common reset starts a new
bridge and reader epoch and cannot prove prior traffic drained.

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
or pointer-path cuts are added. The board SDC cuts the four qualifier and four
reset-bridge asynchronous clear pins, plus the first D ingress of the
sticky-fault and HDMI-visibility synchronizers; later synchronizer stages,
line gating and output paths remain timed. No HDMI IO delays are fabricated.

Native tests exercise the real reader/video/control RTL with the same guarded
M20K hint adaptation used by the build. They cover complete frames and lane
ordering with delayed/zero-latency responses, descriptor/sequence/slot checks,
snapshots/byte enables, command stalls/drain, reset during accepted and stalled
bursts followed by SUBMIT/CLEAR, missing responses, underflow, response errors
and sticky clock failure. Separate benches cover the exact
last-pixel submission corner, full HS/VS continuity through endpoint disable
and reset, clock-shell startup/brief unlock/relock, and pixel-domain HDMI
gating at a complete sync boundary using digital PLL/I2C models. The generated-interconnect
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
