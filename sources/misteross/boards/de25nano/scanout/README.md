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

STOP halts new fetches while holding an asserted command stable until
acceptance, then drains all accepted responses and queued pixels. Missing
responses never time out into Drained. CLEAR is legal only after drain and
resets local video/reader state, invalidates the active grant/generation and
clears staged configuration. It never resets either crossing bridge. An
unexpected response invalidates accounting permanently for that load and
cannot report Drained or be cleared locally. SNAPSHOT freezes coherent status
and all multiword identities in one pixel-clock observation; its serial
changes only on the explicit snapshot command or cold reset.

Both sides of both crossing bridges share a cold/configuration reset source.
Initial release waits for the first qualified pixel PLL lock. Later PLL-lock
loss blanks HDMI, latches a fault in the running 100 MHz domain and stops/drains
the endpoint once the pixel clock resumes; it does **not** reset live FIFO or
reader accounting. A qualifier stretches brief loss/relock events so video
cannot briefly reappear before the sticky fault latches. This detects PLL
lock loss, not independent pixel-clock progress. Permanent clock loss cannot
advance the control bridge or local drain proof. Forced common reset still
changes the ownership epoch and cannot prove prior traffic drained.

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
or pointer-path cuts are added. The board SDC cuts only four qualifier clear
pins and the dedicated sticky-fault synchronizer's first D ingress; subsequent
stages and release fanout remain timed. No HDMI IO delays are fabricated.

Native tests exercise the real reader/video/control RTL with the same guarded
M20K hint adaptation used by the build. They cover complete frames and lane
ordering with delayed/zero-latency responses, descriptor/sequence/slot checks,
snapshots/byte enables, command stalls/drain, missing responses, underflow,
response errors and sticky clock failure. Separate benches cover the exact
last-pixel submission corner and actual clock-shell startup/brief unlock/
relock behavior using digital PLL/I2C models. The generated-interconnect
helper tests the actual two-clock bridges and width adapter after generation;
none of these modeled checks establishes physical memory or HDMI operation.

Production binding remains closed. SID/firewall/SMMU and DMA backing,
firmware/DT/boot pairing, global reset fencing, the vendor HDMI clock-voltage
discrepancy and external IO timing, physical CDC/MTBF and exact-artifact board
acceptance remain gates. Warm-reset ACK stays inactive: probe/scanout-local
drain cannot acknowledge the retained JTAG and HPS-origin paths. No SD card,
QSPI, hardware, kit or harness is touched by this build lane.
