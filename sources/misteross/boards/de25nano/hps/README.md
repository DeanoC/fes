# DE25-Nano HPS mailbox and read-only DDR probe

This standalone diagnostic adds a small Avalon-MM register block to the
lightweight HPS bridge in Terasic's pinned revision-B GHRD. It preserves the
vendor HPS, LPDDR4, pin and boot-data inputs. Its identity, version, byte-enabled
scratch register, clock counter and ready status are defined by
[`fes.fabric.de25-hps-mailbox`](../../../../mister-packages/packages/fabric/fes_fabric_de25_hps_mailbox.yaml).
This is a diagnostic register interface; it does not declare a FES GP ABI,
runtime programming profile or Linux physical MMIO address.

The separate `fes.fabric.de25-ddr-probe` endpoint occupies 128 bytes at
peripheral-relative offset `0x10100`. Elaborated handoff validation checks
this range against every reachable peripheral slave, including the mailbox;
project-only preparation records that check as pending. Its 32-bit control
slave and 128-bit, byte-addressed read master share the existing 100 MHz
clock. The master exports a two-bit Avalon response and connects through
generated Platform Designer width/protocol adaptation to the unchanged
256-bit AXI F2SDRAM adapter. The vendor JTAG access path remains selected;
the new master has no write ports. This is not the DE10 DDR wrapper or GP ABI.

START snapshots the staged grant bus base/span, read offset/length, nonzero
64-bit generation and nonzero sequence. Base upper bits must be zero, the
grant base is 32-byte aligned, and span/offset/length are 16-byte aligned.
Length/span must be nonzero; 33-bit sums enforce both grant and transfer
bounds, including an end exactly at `2^32`. Low bus addresses are permitted;
the circuit does not equate a DMA bus address with a CPU physical address.
The register map and bit values come from the shared generated definition.

Only one burst is outstanding, with at most 16 128-bit beats and no crossing
of a 4 KiB boundary. Read address/burst stay stable under waitrequest,
including after CANCEL or a busy-command fault. CANCEL stops new commands
and drains every accepted response before IDLE. Missing responses keep BUSY
asserted indefinitely; no timeout claims a drain. Response errors latch a
fault and drain the current burst. Completed bytes count returned beats;
accepted bytes count accepted bursts; the checksum XORs all four 32-bit
words of each returned beat. Last-data registers preserve lane order.

DONE means successful completion only. Cancel/error drains end in locally
IDLE with their sticky flags, without DONE. Idle CANCEL is a no-op. START
requires idle/fault-free state; busy START/CLEAR and invalid commands fault
without orphaning traffic. CLEAR is accepted only while locally idle and
clears result/status registers while retaining the staged descriptor.
Stage writes honor byte enables and may occur during BUSY; the active
snapshot remains immutable. Reserved/read-only writes are ignored.

These descriptor checks confer no DMA allocation, SID/translation/firewall,
cache-coherency or lifetime permission. Local IDLE proves no owned request
or response remains; hard/warm reset cannot prove old external traffic has
drained. The pinned HPS warm-reset request/acknowledge is unconnected.
Actual allocation and bridge/reset lifetime coordination remain external
gates before hardware activation. No physical DDR address is selected.

From this directory:

```sh
make sim
make test
make project VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
```

`OUTPUT=/new/private/directory` selects an explicit fresh output; existing
outputs are refused. `project` checks the complete archive SHA-256, extracts
source inputs, records their original hashes and copies the FES endpoint.
Vendor compiler outputs, IP synthesis caches and generation stamps are excluded.
`generate` and `build` require clean committed FES source and record that
revision. Functional inputs are copied from that commit's Git blobs; the copied
vendor lock, generated constants and integration script select the project.
`generate` adds the component through Quartus 26.3's Platform Designer API,
clears IP generation directories and regenerates the project IP. `build` then
compiles with two CPU workers. Neither command programs a device or writes flash.

After IP generation, test the actual generated 128-bit Avalon to 256-bit AXI
transport with a fresh private simulation output:

```sh
python3 interconnect_sim.py --project /absolute/generated-hps/project \
  --output /absolute/new-interconnect-simulation
```

The helper snapshots unchanged generated dependencies and records their hashes,
the compiler command and per-case AXI traces. It checks narrow-read footprints,
lane order, boundary splits and SLVERR/DECERR propagation using the real probe.
It models the AXI responder, without the HPS, translation, firewall or physical
DDR. Direct probe simulation covers cancellation/reset accounting separately.

The mailbox occupies 32 bytes at relative peripheral-bridge offset `0x100a0`,
after the vendor's existing system-ID and PIO blocks. All offsets below are
relative to this endpoint:

| Byte offset | Register | Access |
| --- | --- | --- |
| `0x00` | Identity `0x44453235` | Read only |
| `0x04` | Version `0x00010000` | Read only |
| `0x08` | Scratch, reset zero | Read/write with four byte enables |
| `0x0c` | Free-running 32-bit clock counter, reset zero | Read only |
| `0x10` | Ready status, bit zero set outside reset | Read only |

Reads complete with a registered response; consecutive reads are supported.
Reset blocks requests and cancels pending responses. Reserved or misaligned
reads return zero, and writes to those addresses or read-only registers have
no effect. A simultaneous scratch read/write returns the previous value.
The Verilator bench exercises those behaviors through the real RTL, including
every byte-enable combination and every byte address in the endpoint span.
`make sim` also exercises the actual board-local reset RTL: all configuration
reset/PLL-lock input combinations, asynchronous assertion at startup and with
the clock stopped high or low, held reset, release on exactly the second
rising edge, and interrupted release/relock.
It additionally runs `make sim-probe`, which exercises the actual probe RTL
with stalls, byte enables, descriptor bounds, low bus addresses, snapshot
isolation, lane/checksum checks, burst boundaries, cancellation and missing
or erroneous responses. A modeled bus does not establish actual DDR access;
the generated interconnect's response preservation must be checked separately.

The board-local reset module combines configuration reset (`ninit_done`) and
loss of PLL lock, asserts asynchronously and releases through a preserved
two-register chain at 100 MHz. This common `top_reset_n` supplies the top's
debounce and heartbeat plus the existing generated subsystem reset input.
The generated controllers retain their own domain-specific release behavior;
their synchronous input stages receive the qualified common reset instead
of a combinational expression from the SDM and PLL clock domains. The PLL
reset remains `ninit_done` so waiting for lock cannot hold the PLL itself in
reset. Quartus identifies the preserved chain automatically; no vector-wide
synchronizer forcing splits its stages into separately declared chain heads.
Its narrow SDC exception covers exactly the
two synchronizer asynchronous clear pins; register D paths and the released
reset's recovery/removal paths remain timed. A changed pin match count fails.

The copied vendor SDC retains its unchanged 166.6666 MHz EMIF clock and now
targets the top's `LPDDR4A_REFCLK_p`, matching the pinned EMIF parameter and
top-level connection. The duplicate `MAIN_CLOCK` declaration is removed
because the generated PLL SDC owns its 50 MHz reference clock. Preparation
checks the pinned PLL's 50/100 MHz configuration and enabled lock output
before making these changes. Four obsolete exceptions
naming absent reset/LED nodes are removed; they are not retargeted onto active
signals. The four active outputs `LED[0]`, `LED[1]`, `LED[2]` and `LED[7]`
are explicitly classified as asynchronous human-visible indicators using
output-port-only exceptions. Their pinned locations and 1.1 V assignments are
verified first; no synchronous output delay is invented. The SDC requires
exactly four matched LED ports and reports both the LED and reset pin counts.
Active KEY/SW and generated IP/JTAG constraints remain selected.
Every reviewed top replacement and constraint repair is recorded in
`inputs.json`; an unexpected vendor patch target aborts preparation.
Original HPS/EMIF module parameters, including packaged LPDDR4 parameters,
are compared after generation; changes fail with a retained review report.

The vendor source contains older device metadata in some child IP and PIO
width differences in its top wrapper. Compiler generation/report inspection
must retain these observations; the mailbox does not silently change peripheral
widths or DDR/firmware settings. Generated SDC and handoff files, the SOF and
their hashes remain in the private output. All timing, electrical, DDR,
firmware/DT compatibility and physical acceptance are separate gates.

The vendor's separate `sof_with_hps.bat` combines the raw SOF with its bundled
U-Boot SPL hex; `sof_to_jic.bat` subsequently creates a QSPI image. This lane
does neither. Its SOF is not a replacement for the Linux BSP's factory QSPI
boot artifact, and it does not establish that the selected vendor kernel/DTB
can safely program or address this design. No Linux bridge window is guessed.

The [2026-10-28 host compile record](../../../docs/validation/2026-10-28-de25-hps-mailbox/README.md)
retains the successful committed-source Quartus build, constrained timing
rows and explicit remaining reset, electrical, firmware and physical gates.
Generated HPS/debug reset crossings still need their own protocol/release
review; the local top reset repair does not qualify those crossings or physical
metastability behavior. The [reset/clock compile record](../../../docs/validation/2026-10-28-de25-hps-mailbox/reset-sequencing/README.md)
binds the repaired source, mapped path checks and remaining findings to its own
artifact; the original mailbox SOF does not contain this repair.

The HPS builder also owns the shared off-board HPS-first phase converter used
by ZX81-HPS and allocated-buffer scanout. It requires matching captured and
project initialization order, regular nonempty inputs and fresh phase outputs.
It binds the complete SOF, HPS bootstrap and fabric RBF hashes without invoking
a programmer. Component producers add their own source/build identity receipt;
conversion alone does not qualify boot compatibility or Linux loading.

The shared generator also serves scanout's explicit Linux-DMA profile. That
profile removes the probe and memory JTAG path and requires a sole scanout
master in the elaborated graph. Its receipt selects `scanout-only` and verifies
the absence of diagnostic instances/window, every adapter/HPS ingress, zero
address translations and the complete nonoverlapping LW map. The default HPS
diagnostic still requires its probe and JTAG routes; an unknown or mismatched
profile fails. Neither profile admits a Linux production device or DMA domain.
