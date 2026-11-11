# Generated scanout transport with modeled M20K storage

The seven required transport cases passed, including five complete pixel-checked
frames. This is host simulation of the actual generated control/memory CDC and
interconnect logic **WITH MODELED M20K STORAGE** and a modeled AXI256 responder.
It is not vendor primitive equivalence, physical CDC/timing/HDMI acceptance, or
admission of a Linux DMA device. No hardware was accessed.

The simulated FPGA generation producer is
`a54abd623b08fe2ed77f8c9847f036f421ea1c83`; its independently annotated generation
verification used `a0547320b160accf3d6e336e5b8ccf6693db92b3`. The tested helper and
bench bytes match Git commit `a2274503a8d375787741dd50107661ebe57a1db4` exactly.
The current FPGA producer is `6da029aa49eeef34698fde5336761580000bd0af`.
The earlier compiled `71203a8cf8780466041729a29784729141d21d80` candidate was
rejected for a new HIGH CDC-50011 finding in the external board reset shell.

The helper selects the unchanged generated `peripheral_subsys`, both actual
`mm_ccb` 19.4.1 bridges, endpoint/video/reader, and top-level
`qsys_top_altera_mm_interconnect_1920_p3mp6ry`. Unrelated masters are inactive.
The control/AXI clock is 100 MHz; pixel clock is 74.25 MHz, with an independent
initial phase and exact 297:400 half-period ratio. Temporary clock stalls do not
reset the live FIFO/controller/accounting logic.

## Reproduce

Use a completed, verified scanout generation and a new private output directory:

```sh
python3 sources/misteross/boards/de25nano/scanout/interconnect_sim.py \
  --project "$PWD/out/de25-scanout-reset-source/combined/hps/project" \
  --output "$PWD/out/de25-scanout-interconnect-a54abd623b-modeled-m20k-08"
```

That is the actual passing invocation. The retained output already exists;
choose another new directory to reproduce it. The helper copies its sources,
runs XML elaboration and Verilator with two build workers, and verifies that
producer source/inputs/verification receipts and helper/bench bytes stayed
unchanged. It never writes to the producer project or opens a Quartus database.
The tool was `/usr/bin/verilator`, version
`Verilator 5.032 2025-01-01 rev (Debian 5.032-1+b2)`.

## Results

| Case | Observed result |
| --- | --- |
| `reset_disabled` | No AXI or display traffic; direct RAM storage-contract self-test passes. |
| `atomic_snapshot` | Full 64-bit generation and descriptor cross control CDC; IOVA zero works; frozen fields stay unchanged while live outstanding reads drain, then the next serial captures the new tuple. |
| `complete_frames_and_slot_reuse` | Sequences 1→2→3, slots 1→0→1; five complete RGB frames, 1,152,242 returned 128-bit/narrow AXI beats, 72,021 bursts, and 4,239 AR stall cycles. |
| `stop_drains_accepted_reads` | A real AXI read is accepted before responses are withheld; STOP retains ownership until responses retire. |
| `pixel_clock_stall_preserves_responses` | Outstanding accounting stays non-drained with pixel clock stopped and no reset; memory responses remain queued and retire after resume. |
| `axi_error_crosses_cdc` | AXI RRESP 2 and 3 arrive with their exact two bits at the actual endpoint; both fault and drain without a displayed completion. |
| `clock_fault_is_sticky` | A sampled fault pulse returns low before CLEAR; fault/blanking persist and restart is rejected. |

Every returned 128-bit word is compared against an independent address-based
memory model. Actual AXI reads must retain aligned 16-byte transfers, remain
inside the granted allocation, and split at 4 KiB boundaries. Unused narrow
RDATA lanes are poisoned. Display acknowledgement is checked at the actual
pixel edge against fresh complete-frame counts captured at acceptance, then
checked again through the frozen register identity/slot tuple. The run observes
no RREADY stall cycles; withheld responses and AR stalls are separate cases.

## Storage model boundary

The installed FIFO simulation fileset uses the same `altera_syncram` primitive
as synthesis. No usable installed direct plaintext vendor model was available.
The explicit model supplies only the reviewed noncollision RAM storage behavior;
all generated FIFO pointers, synchronizers, credits, output stages and routing
remain unchanged.

XML elaboration proves exactly these active RAM cells; generic SC-FIFO source
branches containing other primitive modes do not elaborate:

| Actual instance | Width × depth |
| --- | --- |
| Control command FIFO | 47 × 4 |
| Control response FIFO | 35 × 4 |
| Memory command FIFO | 184 × 4 |
| Memory response FIFO | 131 × 32 |
| Isolated storage-contract self-test | 8 × 4 |

Both generated storage definitions have exact parameter/tie guards. Elaboration
checks every active cell's full parameter map and hierarchy. The model accepts
only DUAL_PORT, CLOCK1 registered B address, unregistered B output, BYPASS clock
enables, equal port width/depth, no ECC/reset, and the reviewed shapes. The direct
test checks independent clocks, one-edge address latency, absence of an extra
output register, disabled writes, and ignored BYPASS enable pins.

Runtime assertions check the actual RAM-to-output-stage transfer, not the
separate buffered `out_valid`: a valid transfer cannot consume an unwritten word,
and a write cannot overwrite the live unread RAM head. Mixed-port same-address
behavior defaults to `DONT_CARE`; it and undefined startup contents remain
unproven. Analogue RAM behavior, metastability and physical primitive acceptance
are excluded. [Storage source provenance](storage-contract.json) records the
installed primary-source hashes/anchors.

## Binding and retained evidence

[Final source binding](final-source-binding.json) compares the successful
simulation with the `6da029aa49` generation: all **175 RTL/header files and 16
QIPs match byte-for-byte**. The remaining SOPCINFO difference is its timestamp
comment only, and the scanout handoff dictionaries match. The SDC retains the
reviewed selector correction to supported `get_pins -compatibility_mode` syntax
with the actual extra wrapper level; it is unchanged from `71203`. The seven
transport cases are reused for these identical selected inputs.

[Previous candidate binding](source-binding.json) is preserved unchanged as
history. Its successful source comparison did not qualify the whole `71203`
candidate: the actual report found combinational `top_reset_n && first_lock_seen`
before a pixel reset synchronizer. The `6da` board shell drives the preserved
`first_lock_seen` register output directly; that register already asserts
asynchronously on `top_reset_n`. This board file is outside all 175 selected
transport inputs. The final binding records its changed hash, matching `6da`
Git blob, exact diff and independent board-bench log. Its reset/PLL/I2C boundary
bench and final DA/STA results supply separate board-shell evidence. This
transport source comparison supplies no timing or physical acceptance.

| Retained record | SHA-256 |
| --- | --- |
| [Passing receipt](receipt.json) | `5ec1639b0b413348803dce55a98ef02c2db3ac46f787acc274e246daae5b23ce` |
| [Final source binding](final-source-binding.json) | `67eac96f40478b172dfaffe9e2e3bf861db70e3c2d34806d70dae0015bffb001` |
| [Previous candidate binding](source-binding.json) | `ea9dd2f94662c3e2de8170b18eb5078649ede1bdb745d91759ca6be33d3b5cf6` |
| Raw elaboration XML, retained privately | `fc5a32cae4e3c3b26c2cdcd06f0f140db4f842522179bcb3d2ef3c57795bcd5d` |
| Explicit RAM model | `a19baa9bba68e7d6273f78e08d4a5111e87935c9580d63550a9ab37d641bd760` |

[Simulation output](simulation.log) retains all seven passing case rows and both
RRESP checks. [Attempts 01–07](attempts.json) retain private paths, hashes and
failure causes; attempt 06 passed XML elaboration only. Earlier rejected/build
failures and the corrected RAM-versus-output-stage assertion are not transport
passes. Raw generated RTL, the 6.35 MB XML, executable and full logs remain under
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-menu-scanout/out/` at the paths
recorded in those JSON files. They are not duplicated in Git.

The [board lane](../../../../boards/de25nano/scanout/README.md) retains the
electrical, external-IO, firmware, stream/translation/firewall, boot epoch and
physical lifecycle gates. Production DMA binding remains closed.
