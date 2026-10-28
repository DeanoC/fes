# DE25-Nano HPS mailbox diagnostic

This standalone diagnostic adds a small Avalon-MM register block to the
lightweight HPS bridge in Terasic's pinned revision-B GHRD. It preserves the
vendor HPS, LPDDR4, pin and boot-data inputs. Its identity, version, byte-enabled
scratch register, clock counter and ready status are defined by
[`fes.fabric.de25-hps-mailbox`](../../../../mister-packages/packages/fabric/fes_fabric_de25_hps_mailbox.yaml).
This is a diagnostic register interface; it does not declare a FES GP ABI,
runtime programming profile or Linux physical MMIO address.

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

The copied vendor SDC has one reviewed clock-name repair: its unchanged
166.6666 MHz EMIF clock now targets the top's `LPDDR4A_REFCLK_p`, matching the
pinned EMIF parameter and top-level connection. Four obsolete exceptions
naming absent reset/LED nodes are removed; they are not retargeted onto active
signals. Active KEY/SW and generated IP/JTAG constraints remain selected.
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
