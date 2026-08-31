# FogCast current architecture

This document describes the system that works now. It is not a future runtime
roadmap.

## Normal FPGA game launch

```text
Browser UI
  -> POST /api/v1/session/launch
  -> host session service
  -> target /v2/cache and /v2/launch
  -> mister-agent
  -> transient MGL
  -> /dev/MiSTer_cmd: load_core <mgl>
  -> resident Main-compatible process
  -> FPGA core and game content
```

The important source entry points are:

- `internal/hostapi/server.go`: browser-facing session endpoints.
- `internal/mediasession/`: host session selection and lifecycle.
- `internal/systems/table.go`: platform, core, file-index, and library mapping.
- `internal/httpapi/content.go`: target cache and cached-launch HTTP endpoints.
- `internal/agent/content.go`: target-side cached content launch.
- `internal/mister/runtime.go`: MGL creation, Main command dispatch, core
  observation, and stop.

The browser sends only a game ID. The host resolves the catalog entry and the
system table determines the RBF selector and MGL file parameters. A cache miss
uploads the game to the target; a hit reuses the existing content.

The target agent writes the transient MGL atomically and sends
`load_core <mgl>` to `/dev/MiSTer_cmd`. The resident Main-compatible process
programs the selected core and delivers the game content. FogCast observes
`/tmp/CORENAME` until the expected core appears. Stop sends
`load_core <menu.rbf>` through the same path and waits for `MENU`.

## Ownership by process

The host application owns the catalog, UI, user intent, content selection,
and host-side media. The target agent owns its HTTP API, cache, transient MGLs,
and launch request handling. The resident Main-compatible process owns FPGA
programming and MiSTer core services.

These are practical process responsibilities, not a distributed ownership or
failover protocol. The dedicated target is on a local network and is
disposable.

## Other existing modes

FogCast also contains host-emulator execution, remote input, capture, and
host-to-target media paths. Those modes share the host session UI but are not
prerequisites for normal FPGA game launch. Their presence must not obscure or
replace the direct game path above.

## Related repositories

`Main_MiSTer` contains the upstream-derived source for the resident
Main-compatible behavior. Experimental native-coordinator branches are
separate work and are not used by the path described here.

`misteross` builds small FPGA experiments with Verilator, the open-source
Yosys/nextpnr-mistral flow, and Quartus. Its integration boundary is an RBF
artifact. FogCast owns selecting, transferring, and launching that artifact.

## Next extension: development RBF

The next feature adds a host action accepting an arbitrary local `.rbf`. The
host transfers the file to the target agent and asks it to load the staged RBF
through the existing Main command path. It does not require an MGL containing
game content, a new native FPGA programmer, or the old fpgadev supervisor.

The first milestone needs only selection, transfer, dispatch, basic status,
and an operator-visible failure. Rebooting the disposable kit is an acceptable
recovery when a development core does not implement enough of the MiSTer
framework for Main to remain healthy.
