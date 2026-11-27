# Reusable FogCast Target HTTP Handler (`targethttp/`)

Provides a lightweight, portable FogCast-compatible `http.Handler` targeting constrained or bare-metal targets (such as TamaGo) using FogCast's canonical `protocol` and `corepackage` wire types.

## Supported Endpoints

- `GET /v1/health` — agent health probe. A valid bearer reveals the target ID, boot ID and software identity. Explicit open mode also publishes the target ID and software identity to anonymous clients.
- `GET /v1/kit/lease` — current ownership reported by the target, with a valid bearer token. A missing provider returns HTTP 503.
- `GET /v1/status` — live runtime status (`protocol.Status`)
- `POST /v1/stop` — stop active core (`Controller.Stop`, empty request body required)
- `GET /v1/core-packages` & `GET /v1/core-selections` — consistent package inventory and per-core selections
- `POST /v1/core-packages` — bounded `application/octet-stream` package upload & install
- `PUT /v1/core-selections/{core_id}` — atomic CAS per-core package selection update
- `POST /v1/launch` — optional core/package/game launch trigger (`Launcher`)
- `GET /v1/diagnostics` — optional JSON diagnostic snapshot (`DiagnosticsProvider`)

A service configured with `ReadOnly` registers only observation routes.

## Verification

```sh
GOWORK=off go -C sources/FogCast test -race -count=1 ./targethttp
```
