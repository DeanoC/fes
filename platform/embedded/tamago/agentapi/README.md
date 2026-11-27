# TamaGo Target HTTP & Discovery Integration (`agentapi/`)

Hosts `agentapi.Service`, wiring [FogCast targethttp](../../../../sources/FogCast/targethttp/README.md) (`github.com/DeanoC/FogCast/targethttp`), `network.HTTPServer`, and [mdns.Responder](../mdns/README.md) (`_fogcast._tcp`) over a live `board.Network` / `network.Link` interface.

## Verification

```sh
GOWORK=off go -C sources/FogCast test -race -count=1 ./targethttp
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./agentapi
```
