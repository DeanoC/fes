# FogCast POC

FogCast is a host-side library scanner and cache-aware launcher for the
dedicated MiSTer Pi development target. The repository contains the POC 1
control path, the POC 1B reproducible target image, and the POC 2 cache and
offline acceptance workflow.

Start with [the POC2 handoff](docs/POC2-HANDOFF.md). It is the canonical
current-state document for a fresh agent; the [POC3 roadmap](docs/POC3-ROADMAP.md)
defines the next productization stage.

## Local checks

Use the pinned toolchain for repository checks and builds:

```sh
mise exec go@1.26.5 -- go test ./...
mise exec go@1.26.5 -- make check build
```

The build produces `bin/fogcast`, `bin/fogcast-api`, `bin/misterctl`, `bin/mister-hil`,
`bin/fogcast-hil`, and the target ARMv7 agent. Build output, local catalogs,
staging content, and acceptance reports are ignored by Git.

## POC4 remote-play plane

POC4 is accepted for the defined host-side transport scope. The measured path
is `MiSTer HDMI -> ShadowCast 3 UVC capture -> macOS AVFoundation /
VideoToolbox -> RTP/H.264 -> independent receiver`. Wi-Fi is an accepted
transport; wired Ethernet is not required for this POC. The receiver supports
authenticated control, RTP/H.264 validation, FU-A reassembly, telemetry, and a
local `ffplay` display backend. The deterministic impairment harness is
`bin/remote-play-impair`.

Read [`docs/POC4-RESULTS.md`](docs/POC4-RESULTS.md) for gate evidence and the
remaining deferred glass-to-glass latency issue. Do not treat a receiver UDP
bind or sender timing as physical latency evidence.

## POC5 unified play session

POC5 resolves the POC4 transition decision: the measured video plane is
integrated behind the POC3 session boundary so host-only games become playable
through one launch/session flow, and the deferred G7 glass-to-glass latency
measurement runs on the integrated fixture. The roadmap and scope are in
[`docs/POC5-ROADMAP.md`](docs/POC5-ROADMAP.md). The follow-on POC6 stage
([`docs/POC6-ROADMAP.md`](docs/POC6-ROADMAP.md)) casts host-emulated games to
the MiSTer-attached TV for the full "GoogleCast for games" appliance behavior.

## POC 3 local host API

The first POC3 slice exposes a privacy-safe, read-only application API for
future CLI, browser, and native clients. It binds to loopback only:

```sh
bin/fogcast-api --config /path/to/local/fogcast.toml --listen 127.0.0.1:8787
```

Initial endpoints are `GET /api/v1/health`, `GET /api/v1/status`,
`GET /api/v1/games?q=<optional query>`, `GET /api/v1/games/{id}`,
`GET /api/v1/session`, `GET /api/v1/session/events?after=<sequence>`,
`POST /api/v1/session/launch`, and `POST /api/v1/session/stop`. The root path
serves a self-contained browser shell. Launch requests contain only a `game_id`;
the host resolves catalog and target details internally. The initial host-only
execution boundary is `internal/hostexec`, with a RetroArch adapter that launches
via an argument vector (no shell interpolation) and reports `host_only` capability.
Game and session responses include only public models and the current `fpga_native`
execution capability; they intentionally omit NAS paths, library IDs, target
credentials, cache digests, and ROM filenames.

The input-only bridge is opt-in in the untracked FogCast config:

```toml
[remote_input]
enabled = true
```

When enabled, `fogcast-api` requests a per-session target-owned input lease
through the authenticated MiSTer API and opens the input stream through that
API. The target agent owns the bridge and `/dev/uinput`; the macOS host does not
execute a target path locally. Session tokens and target credentials remain
private; public input status
contains only lifecycle, bounded counters, timing summaries, measurability, and
canonical shutdown reason. The target bridge binary must be deployed through an
authorized target workflow before enabling this option; local tests do not prove
target `/dev/uinput` or physical input.

Evidence is recorded separately in `docs/remote-input-evidence.md`.

For the disposable development Pi, keep its SSH tunnel running with the
repository helper. It intentionally accepts regenerated target host keys and
prompts for the password without storing it:

```sh
scripts/dev-target-tunnel.sh
```

The helper reconnects after target or tunnel restarts and forwards
`127.0.0.1:18182` to the target API. Override `MISTER_TARGET_HOST` locally if
the development target address changes; do not commit that value.

## POC 2 acceptance

Read [the POC2 handoff](docs/POC2-HANDOFF.md) and [the POC2 deployment runbook](docs/runbooks/poc2-deploy.md) before
touching the dedicated target. The HIL command requires operator-supplied game
IDs and asks for confirmation before every reboot, process restart, share
change, or upload interruption:

```sh
bin/fogcast-hil --config /path/to/local/fogcast.toml \
  --sonic-id OPERATOR_SUPPLIED_MEGA_ID \
  --mario-id OPERATOR_SUPPLIED_SNES_ID \
  --uncached-id OPERATOR_SUPPLIED_UNCACHED_ID \
  --interrupted-id OPERATOR_SUPPLIED_INTERRUPTED_ID
```

No target address, bearer token, NAS path, ROM bytes, or game filename belongs
in this repository. Use distinct operator-supplied ZIP catalog IDs for the
uncached and interrupted fixtures. Keep the generated report ignored and review
it locally only. POC2's interrupted-upload gate remains open until a fixture is
slow enough to interrupt deterministically.
