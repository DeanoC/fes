# DE25-Nano DDR probe and MENU allocation boundary

[`fes.fabric.de25-ddr-probe`](../packages/fabric/fes_fabric_de25_ddr_probe.yaml)
1.0 defines a read-only bring-up endpoint in the copied Terasic GHRD. It is
separate from `fes.memory.hps-ddr` 1.0, whose fixed Cyclone V memory window and
registers do not describe Agilex 5. It assigns no GP capability, programming
profile, runtime admission or Linux device binding.

## Address domains

The [Agilex 5 GSRD address map](https://altera-fpga.github.io/rel-26.1/embedded-designs/agilex-5/e-series/premium-065a/gsrd/ug-gsrd-agx5e-premium-065a/)
places the CPU lightweight HPS-to-FPGA aperture at `0x20000000`, size
`0x20000000`. The probe's subsystem-relative offset is `0x10100`, span
128 bytes. The resulting architectural candidate `0x20010100` requires a
matching generated subsystem map, compatible boot firmware, enabled bridges
and a Linux device resource before software may use it. The pinned Linux DT
has no such endpoint. This definition does not authorize `/dev/mem` access.

The pinned GHRD configures the F2SDRAM interface for 256-bit data, 32-bit byte
addresses, 100 MHz and no coherent cache interface. The diagnostic presents
128-bit reads through Platform Designer's generated width adaptation. Its
descriptor carries a 64-bit DMA bus address; current hardware rejects a
nonzero upper word instead of truncating it. A valid grant has a 32-byte
aligned base, nonzero 16-byte aligned span and exclusive end at most `2^32`.
Its requested offset and nonzero length are multiples of 16, wholly inside
that grant. All additions include a carry bit.

CPU virtual addresses, CPU physical addresses and device DMA addresses are
different domains. In physical bypass mode the first DDR window starts at
`0x80000000`; an IOMMU mapping can instead assign a lower DMA bus address.
Bounds validation therefore does not impose a physical DDR lower bound on
`dma_addr_t`. Neither valid bounds nor an address in the physical DDR window
proves that an allocation or accessible mapping exists.

The generated vendor adapter supplies AxUSER low byte `0xe0`, AxCACHE `2` and
AxPROT `0b011`; its wrapper supplies MMUSECSID `1` and MMUSID `1`. Those bits
do not establish translation bypass. The pinned Linux DT also assigns SMMU
stream 1 to Ethernet. A routed stream conflict is not established, but the
fabric path needs an explicit translation/stream-domain binding before use.
The [F2SDRAM firewall](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/f2sdram-firewall?contentId=JZo73fDtBno9kTQOAdevlQ)
sits after the MPFE TBU, so translated physical backing must also lie in
enabled firewall regions. Stream translation and physical firewall coverage
are separate admission requirements.

## Registers and read lifecycle

The generated Verilog and C++ constants are authoritative for numeric values.
All offsets below are byte offsets; control registers are 32 bits.

| Offset | Register |
| --- | --- |
| `00`, `04` | Identity `0x44323550`, version `0x00010000` |
| `08`, `0c` | Status, command |
| `10`, `14`, `18` | Staged grant base low/high, span |
| `1c`, `20` | Staged read offset, byte count |
| `24`, `28`, `2c` | Staged activation generation low/high, sequence |
| `30`, `34`, `38` | Completed bytes, XOR32 checksum, error flags |
| `3c`, `40`, `44` | Active sequence, generation low/high |
| `48`–`54` | Last returned 128-bit beat, lowest 32-bit lane first |
| `58` | Accepted bytes |

Unused reads return zero. Byte enables apply to staged writes. START (`1`)
snapshots the descriptor only while locally idle and fault-free. Generation
and sequence must both be nonzero. They identify the operation; the probe
does not authenticate allocation ownership or enforce a cross-reset sequence.
Staged writes cannot retarget an active operation. CANCEL (`2`) stops new
bursts. CLEAR (`4`) clears sticky results only while locally idle. Other
command values fault; a busy START or CLEAR faults and stops new bursts
without abandoning outstanding traffic.

The master has at most one outstanding burst, with at most sixteen 16-byte
beats, split at each 4 KiB boundary. A presented request remains stable until
accepted, including during cancellation. Accepted reads remain outstanding
until every response arrives. Read-response errors stop further bursts and
drain the accepted burst. Completed bytes count returned beats; accepted
bytes count accepted burst lengths. The checksum XORs all four 32-bit lanes
of every returned beat and is a successful result only with DONE and no
fault or cancellation. It is a transport diagnostic, not a cryptographic hash.

Status bits are IDLE `1`, BUSY `2`, DONE `4`, FAULT `8`, CANCELLED `16`.
IDLE reports local quiescence; DONE reports a successful completed read.
Error bits are invalid descriptor `1`, busy command `2`, read response `4`,
unexpected response `8`, invalid command `16`. Faults are sticky. An error
does not make missing responses disappear: a stalled response keeps BUSY
asserted, and software must retain the allocation. There is no timeout that
claims the bus drained.

Hard reset erases the outstanding-response accounting. IDLE after reset
does not prove that traffic from the preceding configuration disappeared.
The current GHRD top also leaves HPS warm-reset request/acknowledge unconnected.
Before admitting DMA, the runtime needs a proven stop/drain/warm-reset
handshake and an external reset epoch. Unexpected reset or failed drain keeps
the old allocation pinned and display unavailable until recovery establishes
that the bus can no longer reference it.

## Planned Linux-owned framebuffer

The first interactive MENU allocation is planned as one kernel-owned 8 MiB
DMA allocation: two 4 MiB slots, 1280×720 BGRX8888, stride 5120 bytes and
3,686,400 visible bytes per frame. A single allocation preserves the existing
client's one-FD, contiguous 8 MiB mapping. These geometry constants describe
the offline planning profile; the probe can read smaller bounded grants.

The future driver must set the actual device DMA mask and translation
binding, allocate with the DMA API, and retain the returned DMA address
without substituting a CPU pointer or physical address. `dma_alloc_coherent`
and `dma_mmap_coherent` are the initial intended allocation/mapping path.
The pinned kernel enables CMA/DMA_CMA with a 32 MiB CMA default, but does not
enable DMA-BUF heaps. Capacity is not a guarantee of allocation success.
Coherent allocation still requires DMA memory barriers and ordered MMIO
publication; userspace atomics do not replace kernel DMA ordering.
See the pinned kernel's [DMA API guide](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/Documentation/core-api/dma-api-howto.rst).

The runtime owns the allocation and activation generation. Clients receive
CPU mappings and slot identities, never authority to choose DMA addresses.
A nonzero generation identifies each activation, with nonzero increasing
32-bit frame sequences and no sequence wrap inside that activation. Initial
scanout has one pending frame. Command acceptance and frame-boundary display
completion are distinct; the previous slot is reusable only after the
displayed completion. The read probe supplies no display-completion claim.

Shutdown must reject new preparation, invalidate the activation, revoke client
CPU writes while retaining the allocation, stop fetches, drain accepted
reads, then release memory after every owner/reference is gone. Client close
alone cannot free DMA backing. An allocation failure leaves the display
unavailable. The FPGA probe and offline planner do not implement this driver,
client revocation, scanout CDC or runtime admission.
