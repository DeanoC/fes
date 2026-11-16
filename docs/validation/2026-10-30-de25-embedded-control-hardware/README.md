# DE25 embedded-control physical diagnostic

The embedded identity/drain endpoint was integrated into a private copy of the
existing DE25 ZX81 board producer and exercised on `TRWU8CBL`. The exact
bootloader-complete diagnostic SOF returned the producer's build ID through the
real FPGA registers. Incorrect build and ABI values were each rejected before
any register write; the accepted test then exercised a held address, partial and
final drain, and a deliberately bad local response. All JTAG masters closed.

This is **exact-image hardware diagnostic evidence**, not a production package,
Linux-free boot or whole-board acceptance. The endpoint's AXI reader is connected
to a bounded on-chip test responder, not DDR. The diagnostic does not prove
containment of inherited masters, CPU physical MMIO, firmware calls or native
runtime admission. Its board producer's build ID is a source/vendor fingerprint,
not a sealed FES package identity.

## Sources and build

FES task base: `5d0c9f8cec45b7dd31e645d266057e2c677af327`. The tested overlay
is `cddc80f2655416e0a5196a5103885228f23fd83c`; the selected DE25 ZX81 producer
is `c413228810a4f9e74318f161e75c952327e08da8`. They are distinct captured source
revisions. The producer includes the overlay, endpoint and board source bytes
in its fingerprint before generation and passes the same 128-bit ID to the ZX81
computer and embedded-control endpoints. The input snapshot and receipts are
retained beside this note.

Quartus Pro 26.3.0 Build 114 generated and compiled the design. Its actual
Platform Designer handoff contains the new 256-byte aperture at
peripheral-relative `0x10400` and JTAG-master address `0x00110400`. Neither
address is a verified CPU physical mapping. Mutated copies of the generated
handoff rejected a changed base, missing JTAG route, wrong span and altered RTL.

The 46 timing-summary rows retained their corresponding baseline row identities,
reported nonnegative slack and zero TNS where TNS is reported; minimum reported
slack was 0.000 ns. This summary does not establish complete timing coverage,
external HDMI/electrical acceptance, reset lifetime or MTBF.

| Artifact | SHA-256 |
| --- | --- |
| Raw compiled SOF | `496ad45e4ece69d38337fd22b340469d74790b9e0d1f330a338982c863504d3f` |
| SOF combined with locked Terasic SPL | `c3c176b0ee88e8c1313c2c85fddd9d95db7c376d6ec3e0de4cecca47497c48db` |
| Vendor SPL | `096f53ea0bb0ba6e7449b2acf012eae734840dbcdd2943b506567bad2f72f823` |

The locked vendor archive and copied SPL bytes were independently checked before
combination. The actual SOF was loaded over the serial-identified board's JTAG
cable without `--force`; the exact source, generation and artifact hashes are in
[evidence.json](evidence.json) and the adjacent compressed producer records.

## Checks and observations

- The generated project's actual Avalon RTL ran under Verilator with the same
  build-ID macro and default ABI as the FPGA build. The native Go adapter passed
  identity, wrong-ID, wrong-ABI, enable, disable and drain under race detection.
  The same physical JTAG script passed against that model, including a wrong ID
  that issued no writes.
- Physical wrong-ID and wrong-ABI checks each reported `writes=0`. The matching
  FPGA reported build ID `0e92b175ecf825e682b4a036cc2c65b4` and experimental
  control ABI `0x10203040` before writes. The local responder then demonstrated
  a held address, no false drain after the first beat, final generation/cookie
  acknowledgement and sticky fault on a bad response. The exact transcripts are
  [wrong ID](wrong-id.log), [wrong ABI](wrong-abi.log) and
  [identity/drain/fault](identity-drain-fault.log).
- The existing factory Ubuntu image booted under the temporary SOF. It is a
  factory-Linux observation, not the native-Go operating system. After a synced
  and UART-confirmed halt, the user power-cycled the board. UART and pinned-key
  SSH verified the factory AArch64 kernel and `/dev/mmcblk0p2` ext3 root on a
  new boot. The vendor FPGA reference linked before and after; after restoration,
  System Console rejected the diagnostic metadata against the factory SLD hash.
  The claimed USB node returned to `root:root 0664`, all owned sessions closed
  and the board claim was released. No flash programming, SD reimage, partition
  or boot-configuration change was performed; normal Linux login and shutdown
  can update the existing root filesystem.
- `make test-changed` passed for `cddc80f26`, including the native firmware/RTL,
  producer, Avalon model, host, runtime, contract and FPGA software lanes.
  `make check` passed at that commit. `make deadcode` passed at `b81b13250`;
  the later commits did not change the Go files it checked.
- An earlier compiler attempt received SIGTERM after successful synthesis;
  its cause was not established. A second private attempt was stopped by this
  task because the source revision changed during the run. Both outputs remain
  under `out/`; neither is used as evidence for the final SOF.

The next integration step is a real Nano producer/package identity and a
qualified CPU/firmware control path. DDR ownership, containment of all relevant
masters, cache/MMU behavior, native boot and full physical acceptance remain
separate work.
