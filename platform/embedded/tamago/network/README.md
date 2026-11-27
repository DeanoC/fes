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
| IPv4 / TCP | `lneto` over MTU 1500 (`net.SocketFunc`, `Link.ListenTCP`, `Link.DialTCP`) | 16 active TCP ports, 8 listener connections; no checksum/segmentation offloads or IPv6. |
| HTTP server / client | `HTTPServer`, `NewHTTPTransport`, `NewHTTPClient` | Pause-aware HTTP/1.1 listener and DNS-backed transport with bounded concurrency and `ServerStats`. |
| Static IPv4 | Default (`10.0.2.15/24`, gw `10.0.2.2` on QEMU) | Set `Config.Address` and `Config.Gateway` to override. |
| DHCPv4 & renewal | Opt-in RFC 2131 client (`AcquireDHCP`, `OpenDeviceDHCP`, `Config{DHCP: true}`) + T1/T2 `LeaseRenewer` | Requires subnet mask, server ID, and usable router (option 3) in the leased subnet. |
| 100M / link-flap recovery | Opt-in `NetworkOptions` (`Allow100Full`, `RecoverLinkLoss`) + `LeaseRenewer.NotifyLinkUp()` INIT-REBOOT probe | Dormant on Nano `fpgaload`; host unit tests only. A `DHCPNAK`, or an ACK for another address or gateway, halts the link; a silent probe keeps the still-valid lease until T1/T2. |
| DNSv4 | Pure-Go RFC 1035 A-record resolver (`ResolveA`, `ResolveAWithTTL`) + bounded TTL cache (`DNSCache`, `Peek`/`Names`, `DNSResolver.ResolveFresh`) | Uses raw frame hooks on `Link`; supports CNAME chains, multi-server failover, and LRU/TTL caching. |
| SNTP | `QuerySNTP` wall-clock client | Optional UTC clock source for FAT32 (`SetClock`) and TLS certificate checks (`WallClock`). |
| TLS (opt-in) | Stdlib `crypto/tls` via `TransportConfig.TLSConfig` / `ServerConfig.TLSConfig` | Requires hardware/firmware RNG (`psci`) and a trusted clock/RootCA policy; plaintext HTTP remains default for local LAN use. |

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
- **DE25-Nano `fpgaload` (explicit selection):** `cmd/fpgaload/shared_network.go`
  parses `--network-dhcp`, `--network-dhcp-hostname`, `--network-sntp-server`,
  `--network-sntp-port`, `--network-allow-100m`, and `--network-recover-link-flap`
  with `ParseStartupFlags` and passes opt-in `NetworkOptions` (`Allow100Full`,
  `RecoverLinkLoss`, and an `OnLinkChange` hook calling
  `LeaseRenewer.NotifyLinkUp()`) into `OpenNetworkWithControlOptions` /
  `OpenNetworkWithControlDHCPOptions`. With DHCP selected it opens
  `OpenNetworkWithControlDHCPOptions` through
  `ProgramService.StartNetworkWithTimeout`, then starts a `LeaseRenewer`. On
  link-up before T1, `NotifyLinkUp()` triggers one debounced RFC 2131
  INIT-REBOOT DHCPREQUEST (`LinkUpDebounce`, `LinkUpProbeTimeout`): broadcast
  from 0.0.0.0 with `ciaddr` 0, the lease address in option 50 and no server
  identifier. RFC 2131 §4.3.2 asks a server to NAK that form when the address
  is on the wrong network; it sets no such rule for a RENEWING/REBINDING
  request with `ciaddr` set. A `DHCPNAK` from any server, or an ACK for
  another address or gateway, halts the link immediately; a probe with no
  answer keeps the still-valid lease until T1/T2.
- **RFC 4330 SNTPv4 periodic discipline (`sntp_sync.go`):** `SNTPSyncer`
  (`NewSNTPSyncer`, `(*Interface).NewSNTPSyncer`) serializes `SyncOnce` calls,
  checks `[MinValidTime, MaxValidTime]` plausibility on every sample, enforces a
  default 5-minute `MaxStep` guard (`ErrSNTPStepTooLarge`) against both prior
  syncs and pre-synchronized `WallClockSink` clocks (QEMU PL031) while allowing
  initial syncs when `WallClockSink.WallTime()` reports unsynced (DE25-Nano
  cold boot), polls on `PollInterval` with exponential backoff and `ErrPaused`
  tolerance, delivers UTC epochs to `WallClockSink.Sync` (`board.WallClock()`),
  and dispatches callbacks through a single bounded worker.
- The default PIO launcher keeps Ethernet disabled. Add `--paired-ethernet
  --network-dhcp` to the matched C64 picker SDK recipe to compile the Ethernet
  client. The verbatim PIO handoff still enters with MAC0 in reset. The SDK,
  entry and cold chain retain the adapter contract and exclude its 2 MiB EL1
  mapping from staging. The MAC pool occupies only the first five 4 KiB pages
  at `0x95c00000` (descriptor RW, TX R/R, RX W/W under stream ID 1).

## DE25-Nano target discovery

The paired `fpgaload` launcher reads an optional `FES/AGENT.JSN` while it
owns the SD card. On a qualified Ethernet profile, its read-only target API
and `_fogcast._tcp` advertisement share the interface that the native
`ProgramService` pauses and drains during configuration. The MAC installs
the mDNS multicast hash filter before packet workers start. The service uses
the provisioned target UUID for pairing and a fresh, hardware-random boot UUID
for host reboot detection. The lease observation binds that boot UUID to the
SPL-derived owner and the launcher's current state and generation. The target
API requires a hardware entropy source because the architectural counter used
in the SPL owner can repeat after cold reset.
MENU publishes its first frame before the launcher queries the MAC supplier.
The paired launcher derives a stable station address from the first six bytes
of SHA-256 over `FES DE25 paired station v1`, one NUL byte and the canonical
SD target UUID in ASCII. It sets the locally administered bit and clears the
multicast bit. Provision a separate stable target UUID for each kit. A missing
pair, missing hardware boot entropy or an explicit firmware MAC NOT_READY
keeps the local launcher available. A malformed ownership reply or uncertain
acquire/stop remains a failure. MENU retains the local lease while idle.

CBL’s factory cold-boot UART recorded Linux binding `socfpga-dwmac` to
`10810000.ethernet`, reporting user ID `0x76`, Synopsys ID `0x31` and
XGMAC2. U-Boot reported assigning a random MAC address. The wire capture’s
SHA-256 is `e0fb20231ef46620ccd34f18bd74ff35df74230d5f4364d28201fddf1a9e5333`.
The recovery loader on the same serial, TRWU8CBL, read MDIO registers 2/3
at PHY address 1 as `0x0022`/`0x1622` (KSZ9031, ID `0x00221622`).
It obtained a DHCP lease with a deliberately assigned MAC and transferred
data. [CBL MDIO and DHCP capture](../../../../docs/validation/2026-11-02-de25-atf-handoff/boot-upload-result.json)
and [recovery-loader SD readback](../../../../docs/validation/2026-10-31-de25-ethernet-sd/README.md).

The native R1 cold-boot capture (SHA-256
`a5b973468e117773cc57806e31626e15fb98311056985f633a4a027853675d5b`)
records MAC control `3`, read/write stream ID `1`
(`mac_ids=10001`), and secure and nonsecure SMMU CR0/CR0ACK all zero at
cold admission. The same observation explicitly reports `access=unqualified`.
The matched MAC0/KSZ9031 profile reserves 25 seconds for PHY training and
five seconds for the rest of device startup. DHCP adds its own ten-second
acquisition window. The same 30-second device deadline covers fresh grants
when returning to MENU or launching a core. CBL needed up to 9.85 seconds in
a standalone diagnostic before both gigabit receivers were ready. Each DMA
stop retains its five-second bound. A failed restart retires the controller.
These budgets leave PHY configuration and training with the secure supplier.

A native supplier still needs to establish exclusive EMAC clock/reset and
SMMU/firewall ownership, contain descriptor and buffer DMA within the
pinned window, and prove outstanding MAC transactions have drained before
returning the CPU to FPGA or SD operations. See the [driver's hardware
requirements](../xgmac/README.md) and [firmware lease contract](../maccontrol/README.md).

The next CBL qualification needs a source-bound supplier and an independently
observable stop/drain boundary for that port. On a leased board, verify the
grant, bounded packet transfer, drain and restart with a provisioned unicast
MAC, then repeat across the local MENU/core and SD transitions. A separate
LAN host can then discover the provisioned UUID, read authenticated health and
kit lease, check the core and generation across launch/return, and observe
the mDNS goodbye on Quit and Park.

## Verification

```sh
# Host unit tests (DHCP, renewal, DNS, SNTP, pause/resume control, NotifyLinkUp)
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./network

# QEMU VirtIO static + DHCP integration
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py network \
  --board qemu --dhcp \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 --output out/native-network-dhcp
```
