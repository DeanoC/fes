# TamaGo Target HTTP & Discovery Integration (`agentapi/`)

Hosts `agentapi.Service`, wiring [FogCast targethttp](../../../../sources/FogCast/targethttp/README.md) (`github.com/DeanoC/FogCast/targethttp`), `network.HTTPServer`, [mdns.Responder](../mdns/README.md) (`_fogcast._tcp`), and optional A/B OTA HTTP routes (`/v1/ota*` backed by [ota.Manager](../ota/README.md)) over a live `board.Network` / `network.Link` interface.

`POST /v1/ota/stage` sends the raw OTA payload as the request body. Version,
target, expected SHA-256, and rollback epoch use the `X-OTA-Version`,
`X-OTA-Target`, `X-OTA-SHA256`, and `X-OTA-Rollback-Epoch` headers. When a
signature is required, send its bytes as standard padded Base64 in
`X-OTA-Signature`; the handler decodes and passes those bytes to
`ota.Manager.Stage` and its configured `VerifySignature` callback. The callback
decides whether an absent signature is acceptable.

## Verification

```sh
GOWORK=off go -C sources/FogCast test -race -count=1 ./targethttp
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./agentapi
```
