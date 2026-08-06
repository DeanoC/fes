# Remote input evidence

## Local deterministic evidence

The local implementation is covered by the Go protocol, normalizer, host-session, target-controller, HTTP lease, and bridge tests. The host-side controller uses fresh random session/token identities, authenticated stream handshakes, bounded reconnect/state replay, idempotent detach/close, and privacy-safe status models. The target-side controller creates a target-owned bridge lease and uinput device; it is not spawned by the macOS host.

## Managed target API evidence

The existing managed tunnel forwards `127.0.0.1:18182` to the target agent API. From the same host context, the following were verified without recording the bearer token:

- `GET /v1/health` returned ready=true, mister_process=true, command_pipe=true.
- Authenticated `GET /v1/status` returned state=active, system=snes, observed_core=SNES for the currently running target session.
- Unauthenticated status remains protected by HTTP 401.

The target was subsequently deployed and exercised through the same tunnel. The
running target agent is the writable FAT-side binary at
`/media/fat/mister-remote/mister-agent`; the ARMv7 agent and bridge artifacts
were transferred with legacy-compatible `scp -O` and their SHA-256 values were
verified before installation. The target reported Linux armv7, `/dev/uinput`
was present, and direct target-loopback health remained ready after restart.

## Target-side uinput/deployment/HIL evidence

The target-side bridge and uinput device creation are implemented and wired
behind the target agent's authenticated input lease/stream routes. Verified
HIL results:

- authenticated lease attach returned `{"ready":true}`;
- authenticated HTTP CONNECT input stream completed successfully;
- real gamepad press and release frames were sent to the target bridge;
- detach returned `{"ready":false}`;
- four additional attach/detach cycles completed successfully;
- target health remained ready after all cycles;
- the target bridge listener was bound to `127.0.0.1:18183` and was removed
  after detach.

This proves the authenticated transport, target bridge, uinput path, and
cleanup lifecycle. It does not claim a measured physical FPGA-visible effect
or physical input latency; those require an observable game-screen fixture.

## Scope audit

Main_MiSTer, catalog/cache semantics, existing launch commands, and video paths were not modified.

## Review note

The target API input routes are intentionally new and backward-compatible with
existing `/v1/health`, `/v1/status`, `/v1/launch`, `/v1/stop`, and `/v2` content
routes. Video streaming remains outside POC3 and is tracked as a separate
post-POC3 experiment.

## Verification commands

- `mise exec go@1.26.5 -- go test ./...`
- `mise exec go@1.26.5 -- go test -race ./...`
- `mise exec go@1.26.5 -- go vet ./...`
- `mise exec go@1.26.5 -- make check`
- `mise exec go@1.26.5 -- make build`
- `git diff --check`
- `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- go build ./...`

## Acceptance boundary

This evidence closes the POC3 remote-input transport and target-lifecycle gate.
It intentionally does not close the separate POC2 interrupted-upload gate, and
it does not claim physical game-screen input observation or latency. Those
remain separately identified acceptance work rather than hidden assumptions in
the POC3 completion decision.
