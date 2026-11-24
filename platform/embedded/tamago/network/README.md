# Native Go networking (`network/`)

`network` bridges Go's `net` and `net/http` APIs to a bare-metal Ethernet
driver using the pinned `lneto` TCP/IP stack
(`github.com/usbarmory/go-net v0.0.0-20260924083839-356cbf14db9e`).

- **Called by:** `cmd/network`, `cmd/tenfoot`, `cmd/programfetch`,
  [board/qemuvirt](../board/README.md), and [board/de25nano](../board/README.md)
  (`cmd/fpgaload/shared_network.go`).
- **Delegates to:** QEMU VirtIO-net (`Open` / `OpenWithPool`) or DE25-Nano
  [xgmac](../xgmac/README.md) (`OpenDevice` / `OpenDeviceDHCP`) over uncached
  [dmaops.Pool](../dmaops/README.md) buffers, plus optional
  [gic](../gic/README.md) RX IRQ wakeup (`RXReady()`).

## Capabilities and limits

| Feature | Implementation | Notes |
| --- | --- | --- |
| IPv4 / TCP | `lneto` over MTU 1500 (`net.SocketFunc`) | 16 active TCP ports, 8 listener connections; no checksum/segmentation offloads or IPv6. |
| Static IPv4 | Default (`10.0.2.15/24`, gw `10.0.2.2` on QEMU) | Set `Config.Address` and `Config.Gateway` to override. |
| DHCPv4 & renewal | Opt-in RFC 2131 client (`AcquireDHCP`, `OpenDeviceDHCP`, `Config{DHCP: true}`) + T1/T2 `LeaseRenewer` | Requires subnet mask, server ID, and usable router (option 3) in the leased subnet. |
| DNSv4 | Pure-Go RFC 1035 A-record resolver (`ResolveA`) | Uses raw frame hooks on `Link`. |
| SNTP | `QuerySNTP` wall-clock client | Optional UTC clock source for FAT32 (`SetClock`). |
| TLS / auth | Not supported | Deterministic board entropy is unsuitable for TLS/authentication; use private LAN HTTP. |

## Ownership and DMA pause/resume (`BindControl`)

- **Boot-lifetime DMA:** The NIC allocates descriptors and buffers from the
  board's uncached DMA arena (never Go heap). `Err()` and `Done()` report fatal
  NIC/TX errors; failed TX poisons the NIC and keeps buffers pinned until
  reboot.
- **Coordinated pause across FPGA loads:**
  `Interface.BindControl(DMAControl{Stop, Start})` wraps traffic in a software
  gate (`Controller.PauseNetwork` / `ResumeNetwork`, 5 s bound per phase).
  `PauseNetwork` blocks new calls, drains in-flight `Receive`/`Transmit`, and
  invokes the platform stop/drain callback; `ResumeNetwork` reopens traffic
  only after the paired platform restart and consumed `Confirm` hook.
  `ProgramService.AttachNetwork` / `StartNetwork` hooks this controller into
  every FPGA configuration, return, install, and Park transition.

## QEMU vs DE25-Nano status

- **QEMU `virt` (active in CI):** Runs VirtIO-net over QEMU user networking
  (SLIRP, `-global virtio-mmio.force-legacy=false`). `run.py network --board qemu`
  tests static IPv4, DNS, DHCP renewal, and SNTP; adding `--dhcp` sets
  `"dhcp": true` in `FES/BOOT.JSON` and verifies the `FES_NETWORK_DHCP` lease
  in `result.json`. Both static and `--dhcp` run in the `tamago` CI lane.
- **DE25-Nano `fpgaload` (dormant / disabled):** `cmd/fpgaload/shared_network.go`
  wires `OpenNetworkWithControlDHCP`, `LeaseRenewer`, and `QuerySNTP` behind
  build-time linker flags (`--network-dhcp`, `--network-sntp-server`, etc.),
  but **`build.py` rejects all of them** against the shipped
  `nano/sd-profile.json` (`network: null`) because closed-bridge SD ownership
  excludes Ethernet (`tests/test_tamago_build_network.py` enforces this
  refusal). Even a hand-linked binary fails closed in `prepareProgramNetwork`
  while `de25nano.NetworkEnabled()` is false. Enabling Nano Ethernet requires a
  network-enabled profile plus a qualified EL3 MAC lease/drain supplier.

## Verification

```sh
# Host unit tests (DHCP, renewal, DNS, SNTP, pause/resume control)
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./network

# QEMU VirtIO static + DHCP integration
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py network \
  --board qemu --dhcp \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 --output out/native-network-dhcp
```
