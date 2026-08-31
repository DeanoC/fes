# FogCast

FogCast is a host application and target agent that browse and launch a
multi-system game library on MiSTer hardware. The host owns the UI, catalog,
and content selection. The MiSTer Pi runs a small agent and the normal
Main-compatible core-loading path.

## What works now

- Thousands of catalogued games across the FPGA-native systems declared in
  `internal/systems/table.go`.
- Real game launches on the dedicated MiSTer Pi.
- Target-side content caching, input, stop, and active-core observation.
- A browser UI with local media and preview support.
- Host-emulator casting and remote-media experiments in addition to native
  FPGA game launch.

The working FPGA launch path is:

1. The browser sends a game ID to `POST /api/v1/session/launch`.
2. The host resolves the game and uploads it through `/v2/cache` when needed.
3. The host calls the target agent's `/v2/launch` endpoint.
4. The agent creates a transient MGL and writes `load_core <mgl>` to
   `/dev/MiSTer_cmd`.
5. The resident Main-compatible process loads the RBF and game. FogCast reads
   `/tmp/CORENAME` to observe the active core.

Stopping a game loads `menu.rbf` through the same command path.

## Current goal

Add a development action beside normal game launch. It will accept an
arbitrary local `.rbf`, transfer it to the disposable MiSTer Pi, and load it
through the same proven Main-compatible command path.

This does not require a new FPGA programmer, automatic misteross artifact
discovery, the experimental native coordinator, or the abandoned fpgadev
supervisor/recovery system.

## Repository boundaries

| Repository | Owns |
| --- | --- |
| `FogCast-POC` | Host application, browser UI, catalog, target agent, content transfer, and launch requests |
| `Main_MiSTer` | The upstream-derived Main implementation behind the working Main-compatible target path |
| `misteross` | Verilator, open-source, and Quartus FPGA builds that produce development RBF files |

The `Main_MiSTer` native-coordinator branches are experiments, not a
prerequisite for the working system. FogCast commits after `3f27741` that add
the fpgadev supervisor, hardware-owner records, attestation, journals, fault
injection, and fail-stop recovery are abandoned and intentionally absent from
this recovery branch. Git history retains them.

## Build and test

The normal local checks are:

```sh
go test ./...
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
go vet ./...
```

Build the normal binaries with:

```sh
make build
```

For setup, configuration, and the target-agent build, read
`docs/DEVELOPMENT.md`. The current process boundaries and source entry points
are in `docs/ARCHITECTURE.md`.
