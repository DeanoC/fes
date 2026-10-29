# Native Go networking on QEMU

This adapter connects the ordinary Go `net` and `net/http` APIs to a real
modern VirtIO network device. It uses the
[go-net](https://github.com/usbarmory/go-net) Lneto stack and a small polling
MMIO driver. The controller is QEMU `virt`'s test backend; this is not a
DE25-Nano XGMAC/PHY driver.

After `qemuvirt.InitDevices()`, call `network.Open(ctx, network.Config{})`
once, before opening application sockets. The default address is
`10.0.2.15/24`, with gateway `10.0.2.2`, matching QEMU user networking. Supply
`Config.Address` and `Config.Gateway` to change them. `MMIOAddress == 0` scans
the 32 QEMU VirtIO slots and rejects ambiguous multiple-NIC configurations.
The adapter installs `net.SocketFunc`; application clients must use actual
HTTP transport. `Open` neither replaces nor wraps an HTTP transport.

Run the image with 512 MiB of RAM and one CPU. Enable modern VirtIO devices:

```text
-machine virt,gic-version=3 -cpu cortex-a53 -m 512M -smp 1
-global virtio-mmio.force-legacy=false
-netdev user,id=net0,hostfwd=tcp:127.0.0.1:18882-:8081
-device virtio-net-device,netdev=net0,mac=52:54:00:12:34:56
```

A service bound to host loopback port 18881 is available from the guest at
`http://10.0.2.2:18881`. The optional forwarding example makes a guest HTTP
listener on port 8081 reachable at host `http://127.0.0.1:18882`.

## Ownership and limits

The board owns the global DMA arena. `Open` requires that arena to be initialized
outside Go's heap and mapped as Normal noncacheable memory. The NIC never
initializes it, frees a live descriptor, or borrows Go heap pointers for DMA.
RX buffers remain posted to the device until completion. TX copies into an
owned buffer and waits for completion before reuse. ARM barriers publish
both descriptor contents and queue indices. Interrupts are suppressed and
acknowledged during polling; no GIC handler or asynchronous preemption is
required. Empty polling and TX waits yield to other goroutines.

The adapter is a startup service lasting for the whole boot. `Err()` reports
cancellation or the first fatal NIC error, and `Done()` closes when service
becomes unavailable. A stopped device or failed TX stops RX polling and makes
new sockets fail. Existing socket operations still need application deadlines.
This does not shut down the stack, detach the global socket provider, or
reclaim DMA. A failed TX poisons the NIC because descriptor ownership is
uncertain; buffers stay reserved. Startup failure is likewise not retried.
Recovery currently means rebooting the image.

This prototype supports IPv4 TCP with static addressing and a 1500-byte MTU.
It enables neither checksum/segmentation offloads nor merged receive buffers.
The Lneto configuration limits active TCP ports to 16 and listener connections
to 8. DHCP, DNS configuration, UDP, IPv6, link-change recovery, network
throughput qualification and physical Ethernet remain separate work. Use
numeric IPv4 addresses for the current private HTTP service. The board's
deterministic entropy provider is unsuitable for authentication and TLS.

The pinned dependency is `github.com/usbarmory/go-net`
`v0.0.0-20260924083839-356cbf14db9e`, the revision compatible with the
TamaGo 1.26.5 compiler. Both TCP peers in the smoke test exchange packets
through QEMU VirtIO and user networking; a successful test is emulator
integration evidence, not physical Ethernet acceptance.
