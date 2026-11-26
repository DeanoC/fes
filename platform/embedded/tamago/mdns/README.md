# Multicast DNS & DNS-SD (`mdns/`)

Pure-Go RFC 6762 mDNS and RFC 6763 DNS-SD responder and browser operating directly over `network.RawFramePort` (no POSIX UDP multicast sockets required).

- **Delegates to:** [network](../network/README.md) (`RawFramePort` / `*network.Link`).
- **FogCast compatibility:** `NewFogCastService` and `ParseFogCastTXT` match FogCast's `_fogcast._tcp` discovery record format (`v=1`, `target_id`, `node_id`, `mesh`, `cap`, `ttl`).

## Components

- `Responder` (`NewResponder`, `Start`, `Stop`, `AnnounceNow`, `Stats`): sends unsolicited startup announcements, responds to multicast and `QU` unicast queries (`PTR`, `SRV`, `TXT`, `A`, `_services._dns-sd._udp.local.`), tolerates `network.ErrPaused`, and emits a `TTL=0` goodbye packet on `Stop`.
- `Browse` and `ResolveHost`: active DNS-SD service discovery and `.local` hostname resolution over Ethernet frame taps.

## Verification

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./mdns
```
