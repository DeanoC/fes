# DE25 three-memory RAM Tester

This standalone Quartus Pro 26.3 diagnostic tests the DE25-Nano's three physical
memory banks through separate controllers. It produces a JTAG SRAM `.sof` and
retained build reports. It is not the DE10 `fes.ramtest` package and adds no
catalog entry, runtime launch profile, boot image or automatic programming step.

| Bank | Capacity and route | Diagnostic clock | Ownership boundary |
| --- | --- | --- | --- |
| HPS LPDDR4 A | 1 GiB physical bank, preserved GHRD F2SDRAM adapter | 100 MHz fabric | Destructive engine disabled in the board top; staged kernel grant/binding remains future work |
| FPGA LPDDR4 B | 1 GiB, Terasic RTL LPDDR4 example's B EMIF | 100 MHz fabric, vendor memory/PHY configuration | Destructive standalone diagnostic; calibration and controller-ready must both assert |
| Board SDRAM | 128 MiB, two 64 MiB chip selects, custom 16-bit controller | Fixed 50 MHz | Destructive standalone diagnostic; waits for controller initialization |

The SDRAM controller follows the locked package's
`Datasheet/Memory/42-45VM16320G.pdf` (ISSI Rev. 0A, May 2024,
SHA-256 `ca58df0eeb180528fe71bb329feace0ea5cd69435d38a11b4f9fe0abc8cbddf1`).
After 200 microseconds it precharges both chips, refreshes twice, programs
CAS2/BL1 with BA=0/address=0x20, and programs full drive/all-bank PASR with
BA=2/address=0 before admitting requests. A falling-edge input register captures
at nominal external T2 and returns that held sample on the next rising edge.
The pin model keeps BL1 data through T2 and expires it before that later rising
edge. Its 6.4-microsecond refresh budget covers the documented
8192-refresh/64-millisecond envelope at up to 85 C; A2-grade operation above
85 C needs a different policy. External data-window and board timing remain
unqualified.

The HPS capacity register describes the bank, not a grant to overwrite it.
There is no fixed CPU physical DDR address or reserved pool in this lane.
The generic endpoint's grant checks require nonzero generation/sequence and a
bounded staged DMA allocation, and are exercised in simulation. The current
board top ties the HPS endpoint's calibration/ready inputs low: raw register
writes cannot start its destructive engine. The host JTAG controller also
refuses every HPS register write, including SNAPSHOT. Enabling this bank requires
a real kernel grant, device binding and drain/epoch contract; no HPS readiness
or Linux ownership is inferred from endpoint identity.

Run from this directory:

```sh
make test
make sim
make project VENDOR_ARCHIVE=/absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make build VENDOR_ARCHIVE=/absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  QUARTUS_ROOTDIR=/data/altera_pro/26.3 OUTPUT=/absolute/new/output
```

`project` prepares a working-copy diagnostic without running Quartus. `generate`
and `build` require a clean committed source identity. `OUTPUT` must be new;
the default is a fresh directory beneath the FES worktree's `out/de25-ramtest`.
The recipe captures selected Git blobs into `functional-source/`, retains the
locked vendor ZIP, and records hashes in `hps/inputs.json`. Its 128-bit build ID
binds those selected inputs, the vendor archive and diagnostic clock choices.
The expected ID for the JTAG controller is `ramtest.build_id` in that receipt.

The pinned rev. B resource package supplies the GHRD and
`Demonstration/FPGA/RTL_LPDDR4_AXI4_Test/`. Preparation retains vendor source
hashes and records R1-to-SCS device metadata corrections on private copies. It
selects the B controller by the actual `emif_lpddr4b` logical-view mapping,
preserves its memory/PHY parameters, and reuses the vendor B pin/export map
and calibration helper. It preserves HPS/EMIF and boot inputs. This SCS target
does not qualify an R1-marked board. No DE10 HPS windows, PLLs, addon SDRAM
wiring or existing-core acceptance are inherited.

The retained B recipe sets `MEM_OPERATING_FREQ_MHZ=1066.667` and
`PHY_REFCLK_FREQ_MHZ=166.6667`. Those vendor parameters are preserved;
this lane makes no advertised-rate or maximum-bandwidth claim.

Generation retains the separate B-system integration log, root integration
log, IP generation logs and `hps/generation.json`. A successful build adds
`hps/build.json`, full Quartus reports and
`hps/project/output_files/golden_top.sof`. Inspect current reports and the
receipt before claiming fitting, timing or protocol coverage. Compilation
does not prove boot-handoff compatibility, pin/electrical safety, calibration,
DRAM reliability, Linux DMA admission or physical board operation.

The raw compiler SOF requires an HPS bootloader before JTAG programming;
Quartus rejects it without one. The [physical diagnostic record](../../../../../docs/validation/2026-10-30-de25-physical-memory/README.md)
documents the vendor `quartus_pfg` conversion with the unchanged, locked
Terasic SPL into a separate SOF. The programmed SOF, identified by Quartus
checksum `0x067AE616` in the retained transcript, boots the factory SD and
passes 4-KiB smoke and full-bank scans of FPGA LPDDR4 B and SDRAM. Only the
full 128-MiB SDRAM scan crosses byte-address bit 26 and exercises both physical
chip selects. The retained transcript identifies the Quartus cable and target
part, but does not record the USB serial or board UID. The printed SCS/R1 suffix
remains unconfirmed.
Software reboot does not return within the captured interval and requires a
manual power cycle; reset qualification remains open. This is a physical
diagnostic, not production HPS/DMA, electrical or whole-board acceptance.

The scan engine writes and verifies zero, ones, alternating data, walking-bit,
address and inverted-address patterns, then exercises byte enables. It checks
memory read/write responses, preserves an accepted command under backpressure,
and drains accepted traffic after STOP or a fault while clocks and responses
continue. Timeouts record faults; missing responses cannot be cancelled by a
host timeout. A transport reset or unsolicited memory response permanently
faults this tester until FPGA reconfiguration; CLEAR cannot recover ownership.
Snapshots report errors, the first failing address/expected/actual data,
accumulated bad bits, transferred bytes, cycle counters and bounded latency
samples. Latency is measured at the diagnostic fabric interface in that
endpoint's clock cycles; it is not intrinsic DRAM or CPU latency.

`make test` runs source/preparation guards and the offline Tcl master mocks.
`make sim` exercises the actual 128-bit and 16-bit scan engine, both endpoint
policies, and the board SDRAM controller against test models. The standard
misteross Python suite discovers both paths. Functional memory models do not
establish vendor EMIF primitive behavior, generated interconnect behavior,
analogue CDC or physical memory acceptance.

## Generated FPGA-DDR transport check

`interconnect_sim.py` selects the generated FPGA-B endpoint wrapper and the
actual 128-bit Avalon to 256-bit AXI interconnect from a completed, verified
RAM Tester project. It copies the exact reachable generated HDL and includes
into a new private output. Generated translators, packet routing, width/lane
adaptation and response accounting remain unchanged. The controller-side AXI
memory is an independent byte-addressed model; no hard RAM primitive is
substituted or permitted in the elaborated transport.

```sh
make sim-interconnect PROJECT=/absolute/producer/hps/project \
  OUTPUT=/absolute/new/transport-test
```

This needs Verilator and C++, not another Quartus compilation. It requires the
producer's original `inputs.json`, `generation.json`, `quartus-version.txt`,
QIPs and generated sources; copied RTL and the physical top must still match
the retained generation checks. The HPS destructive-enable field must remain
false. The helper records source, executable, wrapper and verifier identities,
case results and the explicit hardware exclusions in `simulation.json`.

The bench covers both 128-bit halves of the AXI bus, partial byte enables,
burst boundaries, independent AW/W/AR backpressure and payload stability. The
real endpoint runs all seven scan patterns and 1,024 latency samples. Corrupted
read data must retain its failing address and bit mask; AXI read/write errors
must reach the scan fault state. STOP cannot finish before accepted B/R
responses arrive. Reset retains an uncertain epoch instead of inventing
completion. A separate malformed-AXI run must trigger the actual generated
read-ID FIFO assertion; this is a simulation assertion, not a synthesized
hardware fault flag.

This check does not exercise the upstream peripheral CSR routing, HPS memory
path, EMIF controller/PHY, calibration, analogue timing or physical memory.
The original endpoint/control and SDRAM simulations remain separate checks.
No command programs hardware, changes the image or enables HPS writes.
The [2026-10-29 transport record](../../../docs/validation/2026-10-29-de25-ram-transport/README.md)
retains the actual generated-path cases, source identities and model boundaries.

## Explicit JTAG diagnostic control

[jtag.tcl](jtag.tcl) is an operator diagnostic for an already programmed,
identified board. Sourcing it makes no hardware calls. In System Console,
select the exact master service yourself, retain the matching build receipt,
and call:

```tcl
source /absolute/path/to/ramtest/jtag.tcl
set result [de25ramtest::run /explicit/master/service 0123456789abcdef0123456789abcdef fpga 60000 10]
puts $result
```

The example ID is a placeholder: pass the exact 32 hexadecimal digits from
the selected artifact's receipt. The controller claims only the explicit
service, checks identity/version/channel/capacity/clock/data width and the full
128-bit build ID at all three endpoints before writing, then releases its
claim when finished. Register addresses come from the generated Verilog
constants, including the JTAG bridge-relative base; they are not CPU MMIO
addresses. Use `sdram` for board SDRAM. An optional final argument selects a
power-of-two test length from 32 bytes through bank capacity; otherwise the
whole selected local bank is tested. Both choices overwrite existing contents.
The default host wait is 15 minutes; the conservative SDRAM controller needs
several minutes for a full-bank scan. Pass a shorter length for an initial smoke
test, or an explicit wait up to one hour. These are host wait budgets, not
hardware response timeouts or measured board performance.

The result contains per-bank calibration/status and fault flags plus a frozen
snapshot of individual counters and errors. Each snapshot requires an advanced
serial and rechecks it after all words are read. Exclusive access to these
CSRs is required. A bounded host wait issues STOP and returns `HOST_TIMEOUT`
with `drain_proven=0`; it does not free memory, reset a bridge or certify drain.
Transport/snapshot errors after START also attempt STOP and remain errors.
The script does not program an FPGA, change a boot pairing or allow HPS writes.

See the [FES integration boundary](../../../../../docs/design/de25-ramtest.md)
and the [board bring-up guide](../README.md). Physical use still requires the
applicable kit lease, exact board/artifact identity and authorized diagnostic
procedure.
