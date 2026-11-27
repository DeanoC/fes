# Reusable FogCast Target HTTP Handler (`targethttp/`)

Provides a lightweight, portable FogCast-compatible `http.Handler` targeting constrained or bare-metal targets (such as TamaGo) using FogCast's canonical `protocol` and `corepackage` wire types.

## Supported Endpoints

- `GET /v1/health` — agent health probe (redacts `target_id` and `software` unless authenticated or `AllowUnauthenticated` is enabled)
- `GET /v1/status` — live runtime status (`protocol.Status`)
- `POST /v1/stop` — stop active core (`Controller.Stop`, empty request body required)
- `GET /v1/core-packages` & `GET /v1/core-selections` — consistent package inventory and per-core selections
- `POST /v1/core-packages` — bounded `application/octet-stream` package upload & install
- `PUT /v1/core-selections/{core_id}` — atomic CAS per-core package selection update
- `POST /v1/launch` — optional core/package/game launch trigger (`Launcher`)
- `GET /v1/diagnostics` — optional JSON diagnostic snapshot (`DiagnosticsProvider`)

## Verification

```sh
GOWORK=off go -C sources/FogCast test -race -count=1 ./targethttp
```
