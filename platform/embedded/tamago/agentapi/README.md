# Bare-Metal FogCast Target Agent API (`agentapi/`)

Provides a FogCast-compatible `http.Handler` targeting bare-metal TamaGo appliances over `net/http` (wire-compatible with `github.com/DeanoC/FogCast/protocol` and `github.com/DeanoC/FogCast/corepackage`).

## Supported Endpoints

- `GET /v1/health` — agent health probe (omits `target_id` unless authenticated or `AllowUnauthenticated` is enabled)
- `GET /v1/status` — live runtime status (`protocol.Status`)
- `POST /v1/stop` — stop active core (`Controller.Stop`, empty request body required)
- `GET /v1/core-packages` & `GET /v1/core-selections` — durable package inventory and active per-core selections
- `POST /v1/core-packages` — bounded `application/octet-stream` package upload & install
- `PUT /v1/core-selections/{core_id}` — atomic CAS per-core package selection update
- `POST /v1/launch` — optional core/package/game launch trigger (`Launcher`)
- `GET /v1/diagnostics` — optional JSON diagnostic snapshot (`DiagnosticsProvider`)

## Verification

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./agentapi
```
