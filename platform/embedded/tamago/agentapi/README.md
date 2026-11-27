# TamaGo Target HTTP & Discovery Integration (`agentapi/`)

Hosts `agentapi.Service`, wiring [FogCast targethttp](../../../../sources/FogCast/targethttp/README.md) (`github.com/DeanoC/FogCast/targethttp`), `network.HTTPServer`, and [mdns.Responder](../mdns/README.md) (`_fogcast._tcp`) over a live `board.Network` / `network.Link` interface.

## Verification

```sh
GOWORK=off go -C sources/FogCast test -race -count=1 ./targethttp
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./agentapi
```

On a Linux machine with user and network namespaces, iproute2 and ethtool,
the following test starts the ordinary FogCast host with a stale target
address. It discovers the native responder over multicast, authenticates
the target and observes a change in lease ownership on a private Ethernet
link.

```sh
FES_TEST_NATIVE_MULTICAST=1 GOWORK=off go -C platform/embedded/tamago test \
  -race -count=1 ./agentapi -run '^TestHostDiscoversNativeTarget$'
```
