# Nano XGMAC transport prototype

The Nano backend now supplies an XGMAC 2.10 Ethernet transport to the shared
Lneto IPv4/TCP socket service. This is host/model-tested driver code and an
AArch64 cross-build, not physical networking acceptance. QEMU continues to use
its separate VirtIO NIC. No Linux driver or userspace is linked into the image.

The first profile supports one DMA channel, static IPv4, Ethernet MTU 1500,
and a firmware-negotiated **1000BASE-T full-duplex** RGMII link. It performs
bounded Clause-22 MDIO reads, verifies the exact PHY ID and negotiated gigabit
capabilities, and polls link health. It deliberately rejects incomplete
negotiation, half duplex and 10/100 links. Firmware still owns PHY reset,
autonegotiation setup, RGMII routing/delays, 125 MHz reference, MAC CSR clocks,
reset/firewalls and SMMU configuration. There is no PHY scan or guessed address.

Four 16-byte TX and RX descriptors and private 1600-byte buffers live in an
explicit, permanently reserved Normal noncacheable DMA window outside Go RAM.
The driver copies TX bytes into that window, publishes descriptors with ARM
barriers, and reuses a TX slot only after completion. RX bytes reach the caller
only after ownership returns and bounds/errors are checked; valid retired slots
are reposted. No hardware DMA points at a Go heap object. Interrupts, jumbo
frames, checksum/segmentation offloads, promiscuous receive and pause negotiation
are disabled. RX CRC/length errors drop; unexpected descriptor contexts/fragments,
fatal DMA status, stopped engines, link loss and TX timeouts retire the device.

Failure stops new MAC/DMA activity as a best effort, but clearing start bits is
**not** a DMA fence. The entire window remains pinned until reboot, even when
those bits clear. Neither service cancellation nor a failed startup permits
reinitialization, buffer reclamation or a new network/socket provider. Runtime
FPGA ownership transitions must account for this lifetime before physical launch.
The shared [network controller](../network/README.md) now gates software traffic
and waits for active driver calls before invoking explicit platform stop/drain
and restart callbacks. Descriptor/IP-stack tests cover repeated HTTP across
short modeled pauses. No qualified Nano callback supplier is bound, and clearing
XGMAC start bits remains insufficient evidence of DMA containment.

`Device.BindControl` now supplies the actual driver stages for that controller.
Bind during device creation, before packet workers start. Stop disables TX DMA,
waits for its process-stopped acknowledgement, checks the MTL transmit FIFO and
receive queue, disables MAC traffic, drains the receive FIFO, stops RX DMA and
waits for both process-stopped acknowledgements. Each wait has checked readback
and 100 ms/finite-poll bounds. The explicit platform
Stop callback must then independently establish DMA drain. A successful callback
may revoke register access; the driver makes no later MMIO or descriptor access
while contained. The original memory remains pinned and Receive/Transmit refuse.

Restart first obtains a new platform grant for the same window and MAC/PHY, then
reruns the existing initialization and descriptor publication in the original
arena. FIFO idle alone permits no restart. Lost replies, cancellation after stop
starts, changed window/PHY and incomplete restart retire without retry or extra
register cleanup. The platform supplier must separately own PHY/clocks/resets,
SMMU policy and the private firmware MAC generation; this API does not provide
that supplier or enable a board profile.

When the platform supplies a Confirm hook, restart calls it after descriptor
initialization and PHY checks, while packet traffic is still paused. A failed or
cancelled confirmation retires without extra register access. The returned
driver controls consume that hook; the common controller rejects an unconsumed
platform confirmation. The HTTP model joins the real C MAC lease engine, native
client and XGMAC driver across three cycles, with modeled authority/drain. It
uses the actual owned dispatcher, whose default MAC setup remains unbound.

`OpenControlled` now performs initial Query/Acquire, driver initialization,
firmware Confirm and exclusive driver-control binding before returning packet
or lifecycle access. It keeps the private lease with a failed driver and
retires without extra register access after an uncertain firmware boundary.
An initialization failure may return a non-nil retired device so its pinned
resources and last lease remain retained. There is no startup retry or automatic
firmware cleanup. The HTTP test uses this same startup function.

The Nano's `OpenNetworkWithControl` hook accepts an independently admitted boot
owner, consumes this startup function and returns both the IP interface and its
lifecycle controller for `ProgramService.AttachNetwork`. It is single-attempt,
retains partial resources, and has no direct-driver fallback. The generic Nano
network diagnostic has no accepted boot owner and refuses startup; QEMU retains
its separate VirtIO path. The CPU transport admits the fixed MAC call only when
the compiled network profile is selected, with exact cookie, operation and
reserved arguments. Firmware still validates ownership and the fixed window.
The present boot profile disables networking, and the qualified secure supplier
and selection from the native boot admission remain open.

The ordered stages follow the vendor's [stopping and starting transmission
procedure](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/stopping-and-starting-transmission?contentId=aoHUqRdCxX615dKqdJAazA).
Both stopped flags must be clear during active health checks under the same
driver lock, so stale flags cannot satisfy the acknowledgement waits. DMA faults
or an early receive stop retire the device. The [CS/R1 register
map](https://docs.altera.com/v/u/resources/864455/agilextm-5-cs-and-r1-suffix-opn-hps-register-map)
defines the transmit read state as two bits; a reserved bit cannot make a busy
transmitter appear ready. Its extracted field facts and source hashes are
retained in `testdata/agilex5-stop-fields.json` and checked by the tests.

These checks also repair the matched Terasic BSP's omitted acknowledgement waits,
three-bit transmit-state mask and unchecked loop expiry. The three-cycle HTTP
test uses these real Go driver stages with explicitly modeled platform
drain/grant callbacks. Its evidence remains software-only: process-stopped status
does not independently establish interconnect DMA drain.

## Explicit firmware profile

Schema 6 includes `network` and the boolean `handoff.ethernet_platform_ready`.
`network: null` with readiness false disables Ethernet without touching its
controller or mapping an arena. With readiness true, `network` has exactly:

- `mac_index` (0..2), `phy_address` (0..31), exact `phy_id`, and an assigned
  six-byte unicast `mac_address`. These must describe the delivered Nano revision.
- `csr_clock_hz` in [100000000,150000000), the supported MDIO clock range.
- `address` and `gateway`, explicit usable IPv4 hosts in the same subnet.
- `dma_start` and `dma_size`, a disjoint 2 MiB aligned HPS DDR window, outside
  Go RAM and every firmware reservation. It must share the existing text L1
  section; the mapper refuses to allocate new page tables in unknown memory.
- `dma_identity: true`: firmware has established physical identity DMA for
  this MAC's stream, including SMMU/firewall access to exactly that window.
- `rgmii_125mhz_ready: true`: PHY routing/delays, reference clock and completed
  gigabit full-duplex autonegotiation have been established by matching firmware.
- `rx_buffer_shift`: the **verified** RX buffer-size register field, either
  1 or 4. Linux's generic XGMAC definitions use bits 14:1; the SoCFPGA U-Boot
  driver uses bits 14:4. The prototype supports either explicit contract and
  never autodetects or falls back. Resolve this discrepancy against the matching
  hardware manual/BSP and verify frame DMA bounds before enabling a real profile.

The test profile is synthetic: its MAC index, PHY address/ID, clocks and RAM
layout are not assertions about the Nano waiting for delivery. Superseded schemas and
missing/extra fields reject; no old-profile compatibility path remains.

## Source references and evidence

SoC register windows and DMA stream IDs were checked against the published
[Agilex 5 DTS](https://github.com/torvalds/linux/blob/master/arch/arm64/boot/dts/intel/socfpga_agilex5.dtsi),
retrieved Git blob `f54767d1526e119bc95d468c4dd4504822316267`: MAC0/1/2 are at
`0x10810000`, `0x10820000`, `0x10830000`, with DMA streams 1/2/3. This says
nothing about which MAC or PHY address the Nano connector actually uses.

Register/descriptor facts were checked against primary
[Linux v6.12 XGMAC definitions](https://github.com/torvalds/linux/blob/v6.12/drivers/net/ethernet/stmicro/stmmac/dwxgmac2.h)
and [SoCFPGA U-Boot v2025.01](https://github.com/u-boot/u-boot/blob/v2025.01/drivers/net/dwc_eth_xgmac.c).
The validation receipt binds the reference-file hashes, including the conflicting
RX definitions. The Go transport is independently implemented; no reference C
source is compiled into it.

Host tests exercise both RX layouts, ring wrap and data integrity, error/drop
handling, immutable ownership after fatal failures, and profile admission.
An actual HTTP catalog exchange traverses the modeled descriptors and real
Lneto ARP/IPv4/TCP stack to an independent gVisor TCP/HTTP peer. It uses no host
TCP socket or fake HTTP RoundTripper. This establishes software integration,
not wire timing, cache/interconnect correctness, SMMU isolation or PHY acceptance.

`cmd/network` mounts the selected FAT32 backend, reads the existing BOOT.JSON
API URL/Host header, fetches `/api/v1/games`, and prints catalog IDs/statistics
on serial. It needs no FPGA display or USB. `run.py network --board qemu`
checks the same path against a private FogCast host and an unchanged disk hash.
