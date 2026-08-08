# FogCast POC

FogCast is a host-side library, launch, and media system for the dedicated
MiSTer Pi development target. POC6 is accepted and shipped for its real-game
host-to-MiSTer HDMI and managed-lifecycle scope. A host-only catalog game can
run under RetroArch, cross the authenticated RTP/H.264 cast path, and appear on
the MiSTer-attached display through the single session API.

Start with the [POC6 results](docs/POC6-RESULTS.md) for the accepted claims and
evidence boundaries. Use the [POC6 development guide](docs/POC6-DEVELOPMENT.md)
to operate and extend the retained testbed. The
[POC6 roadmap](docs/POC6-ROADMAP.md) records the original scope and disposition;
earlier POC handoffs remain historical references.

## Local checks

Use the pinned toolchain for repository checks and builds:

```sh
mise exec go@1.26.5 -- go test ./...
mise exec go@1.26.5 -- make check build
```

The build produces `bin/fogcast`, `bin/fogcast-api`, `bin/misterctl`, `bin/mister-hil`,
`bin/fogcast-hil`, and the target ARMv7 agent. Build output, local catalogs,
staging content, and acceptance reports are ignored by Git.

The generic Makefile builds `fogcast-api` with `CGO_ENABLED=0`. That binary is
for non-hardware checks and cannot use the Darwin AVFoundation/VideoToolbox
capture backend. For POC6 physical development, follow the explicit
CGO-enabled build in [`docs/POC6-DEVELOPMENT.md`](docs/POC6-DEVELOPMENT.md)
after running the generic build.

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

POC5 resolved the POC4 transition decision by integrating the measured video
plane behind the POC3 session boundary. Its roadmap and scope are in
[`docs/POC5-ROADMAP.md`](docs/POC5-ROADMAP.md).

## POC6 cast-to-TV session

POC6 is complete for the accepted video and lifecycle scope. The proven path is
`host RetroArch -> screen capture -> VideoToolbox H.264 -> authenticated
RTP/control -> target-owned decode bridge -> /dev/fb0 -> native presentation
hook -> MiSTer HDMI`. The disposable presentation hook is intentionally
retained at the canonical `/media/fat/MiSTer` path for follow-on video-plane
development; this is not stock MiSTer presentation behavior.

Controller capture/injection into host RetroArch remains
[issue #3](https://github.com/DeanoC/FogCast-POC/issues/3). Physical
glass-to-glass latency remains [issue #1](https://github.com/DeanoC/FogCast-POC/issues/1)
and [issue #2](https://github.com/DeanoC/FogCast-POC/issues/2). These deferrals
do not reopen the accepted POC6 video/lifecycle result, but the broader "one
controller" and latency claims remain unmade. See the [results](docs/POC6-RESULTS.md),
[roadmap disposition](docs/POC6-ROADMAP.md), and
[development guide](docs/POC6-DEVELOPMENT.md).

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
repository helper. Supply the private target address through the local
environment; no target address is embedded in the tracked script. The helper
intentionally accepts regenerated target host keys. Without a configured
password file, it prompts once, stores the password in a mode-`0600` temporary
file for the tunnel process lifetime, and removes that file on exit:

```sh
export MISTER_TARGET_HOST=PRIVATE_TARGET_HOST
scripts/dev-target-tunnel.sh
```

For unattended development, set `MISTER_SSH_PASSWORD_FILE` to an existing
owner-only (`0600`) private file outside Git. The helper reads that file inside
its Expect process; it does not put the password in argv.

The helper reconnects after target or tunnel restarts and forwards
`127.0.0.1:18182` to the target API. Keep `MISTER_TARGET_HOST` local and do not
commit its resolved value.

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
