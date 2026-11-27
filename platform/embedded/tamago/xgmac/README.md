# DE25-Nano XGMAC2 Ethernet driver (`xgmac/`)

`xgmac.Device` implements a bare-metal Synopsys XGMAC2 Ethernet transport
for DE25-Nano (`MAC0/1/2` at `0x10810000`/`0x10820000`/`0x10830000`), supplying
frames to the shared [network](../network/README.md) (`lneto`) IPv4/TCP stack.
QEMU uses its separate VirtIO-net driver.

- **Called by:** [network](../network/README.md) (`Link`),
  [board/de25nano](../board/README.md) (`OpenNetworkWithControl`), and
  [nativefabric](../nativefabric/README.md) (`ProgramService.AttachNetwork`).
- **Delegates to:** uncached [dmaops.Pool](../dmaops/README.md)
  (`NewPoolMemory`), optional [gic](../gic/README.md) SPI (`EnableIRQ`), and
  EL3 `nano_mac.c` via [maccontrol](../maccontrol/README.md) (SMC `0xc2000305`).

> **Current status:** Host/model-tested (including real `lneto` TCP/HTTP over
> modeled descriptors against gVisor) and AArch64 cross-built, but disabled in
> the active DE25-Nano SD boot profile pending physical RGMII/PHY/SMMU lease
> qualification.

## Link, DMA, and interrupt architecture

- **Link speeds & recovery:** Supports one DMA channel, MTU 1500, and
  Clause-22 MDIO verification of exact PHY ID and **1000BASE-T full-duplex**
  RGMII by default (rejecting half duplex and 10 Mbps). Optional opt-ins:
  - `Config.Allow100Full`: admits negotiated **100BASE-TX full-duplex**
    (`XGMAC_CONFIG_SS_100_MII`) with optional `Config.OnSpeedChange` clock hook.
  - `Config.RecoverLinkLoss`: treats transient carrier loss/autonegotiation as
    non-fatal link-down (dropping TX frames and recovering automatically on
    link-up without poisoning DMA; DMA/MDIO/engine faults still fail closed).
- **Pinned DMA rings (`dmaops.Pool`):** The driver posts four 1600-byte buffers
  per direction. Revision 2.10 uses four 16-byte descriptors per ring. The
  Agilex 5 instance with device ID `0x76` and revision `0x31` uses 32 slots
  per ring to cover the documented maximum descriptor cache. Each completed
  receive buffer moves to the next free descriptor. The rings and buffers
  live in a permanently reserved Normal-noncacheable DDR
  window outside Go RAM (no Go heap pointers in DMA). Offloads, jumbo frames,
  promiscuous mode, and pause negotiation are disabled. Fatal DMA faults or TX
  timeouts retire the device and poison the `dmaops.Buffer` until reboot.
- **Optional GICv3 RX IRQ:** When `Config.IRQ` names an Agilex 5 SPI
  (`de25nano.IRQEMAC0/1/2` = `222`/`239`/`256`), `EnableIRQ(g)` sets
  `RDES3.IOC` and routes `NIE|RIE` to wake `network.Link` via `RXReady()`. The
  level-triggered handler masks `channelInterrupt = 0` immediately and
  `Receive` re-arms once the ring drains. `BindControl.Stop` suspends the SPI
  before platform containment so zero MMIO occurs while paused.
- **Multicast hash filtering & MMC counters:** `SetMulticastMACs` programs the
  Synopsys XGMAC 2.10 `MAC_Hash_Table_Reg0..7` and `MAC_Packet_Filter.HMC`
  with readback verification (re-applied across `BindControl.Stop/Start`).
  `SetAllMulticast` toggles `MAC_Packet_Filter.PM` (pass all multicast frames
  without enabling promiscuous mode) while `FilterStatus()` reports the
  configured filter state. `HardwareStats()` reads MMC RX/TX and error
  counters (with opt-in `Config.ResetMMCOnRead` accumulating clear-on-read
  deltas across polls and `Stop`/`Start` resets).

## Pause, stop, and restart control (`BindControl` / `OpenControlled`)

- **`OpenControlled`:** Performs EL3 `Query`/`Acquire`, driver init, EL3
  `Confirm`, and exclusive `BindControl` registration before exposing packets.
- **`BindControl.Stop`:** Follows the Agilex 5 TRM stop sequence—disables TX
  DMA (waiting for process-stopped), checks MTL TX/RX queues, disables MAC,
  drains RX FIFO, stops RX DMA (waiting for both process-stopped flags within
  100 ms / finite polls), then invokes the EL3 platform Stop/drain callback.
  Clearing XGMAC start bits alone is **not** a DMA fence; the window stays
  pinned until reboot.
- **`BindControl.Start`:** Obtains a fresh EL3 grant for the same window and
  MAC/PHY, reinitializes descriptors, calls the EL3 `Confirm` hook, and
  re-enables IRQ/traffic. Any mismatch or uncertainty retires permanently.

## Schema-8 firmware profile (`network`)

Disabled as `null` (`handoff.ethernet_platform_ready: false`) in the default
Nano SD profile. Enabling it requires:
- `mac_index` (`0..2`), `phy_address` (`0..31`), exact `phy_id`, unicast
  6-byte `mac_address`, `csr_clock_hz` in `[100 MHz, 150 MHz)`.
- `address` and `gateway` (same IPv4 subnet), disjoint 2 MiB-aligned
  `dma_start`/`dma_size`, `dma_identity: true`, `rgmii_125mhz_ready: true`.
- Explicit `rx_buffer_shift`. The Agilex 5 `0x7631` instance requires `1`.
  In `DMA_CH0_Rx_Control` the byte count starts at bit 1, and the low three
  alignment bits read as zero. A 1600-byte buffer encodes as `0x0c80`.
  Revision 2.10 retains the two explicitly configured layouts.

## References and tests

The Terasic Linux BSP selects its XGMAC 2.10 descriptor, DMA and MAC operations
for device ID `0x76` with revision `0x21` or newer, and writes the receive
buffer byte count shifted by one. CBL factory Linux reported `0x7631`.
[BSP revision matching](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/drivers/net/ethernet/stmicro/stmmac/hwif.c)
and [DMA programming](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/drivers/net/ethernet/stmicro/stmmac/dwxgmac2_dma.c).

Altera specifies ring length at least equal to descriptor cache size and up
to 32 descriptors per channel. The CS/R1 map supplies the MAC version, feature
words and implemented receive-buffer field used by the regression.
[DMA initialization](https://docs.altera.com/r/docs/814346/25.3/hard-processor-system-technical-reference-manual-agilextm-5-socs/dma-initialization),
[DMA features](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/dma?contentId=~j2QlXmCVvUdmq4xsiQ3Yg)
and [CS/R1 register map](https://docs.altera.com/v/u/resources/864455/agilextm-5-cs-and-r1-suffix-opn-hps-register-map).

- Verified against Agilex 5 HPS TRM / CS-R1 register map (extracted facts in
  `testdata/agilex5-stop-fields.json`), `socfpga_agilex5.dtsi` (`f54767d152`),
  Linux v6.12 `dwxgmac2.h`, and U-Boot v2025.01 `dwc_eth_xgmac.c`.
- Run tests:
  ```sh
  GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./xgmac ./maccontrol
  ```
