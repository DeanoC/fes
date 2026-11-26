# TamaGo Target HTTP Integration (`agentapi/`)

Verifies [FogCast targethttp](../../../../sources/FogCast/targethttp/README.md) (`github.com/DeanoC/FogCast/targethttp`) over `network.NewLink` (`lneto`) Ethernet frames. All HTTP routing, authentication, error mapping, and session-facing target contracts live in FogCast (`sources/FogCast/targethttp`).

## Verification

```sh
GOWORK=off go -C sources/FogCast test -race -count=1 ./targethttp
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./agentapi
```
