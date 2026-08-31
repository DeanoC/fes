# FogCast

FogCast is a host application and MiSTer target agent for browsing and
launching a large multi-system game library. The host owns the UI, catalog,
and content selection; the MiSTer is a small, directly controlled target.

## What works now

- Thousands of catalogued games across the systems in
  `internal/systems/table.go`.
- Real FPGA game launches on the designated MiSTer Pi.
- Target-side content caching, input, stop, and active-core observation.
- Host API loading of arbitrary development RBF files, with automatic reboot
  recovery back to Menu for non-MiSTer cores.
- Browser UI, local media previews, and host-emulator/remote-media modes.
- A reproducible target image toolchain with a development image containing
  SSH and curl.

The normal FPGA launch path is:

1. The browser sends a game ID to `POST /api/v1/session/launch`.
2. The host resolves the catalog entry and uploads content to the target when
   the target cache does not already contain it.
3. The target agent creates a transient MGL and writes
   `load_core <mgl>` to `/dev/MiSTer_cmd`.
4. The MiSTer/Main-compatible process loads the RBF and game.
5. FogCast observes `/tmp/CORENAME` for the active core. Stopping sends
   `load_core <menu.rbf>` through the same command path.

## Development RBF path

`POST /api/v1/session/development-rbf` accepts one bounded
`application/octet-stream` body. FogCast streams it to the target, installs it
as a temporary RBF, and loads it through `/dev/MiSTer_cmd`. A development Stop
uses an explicit two-request target handshake, reboots the disposable kit,
and reports idle only after health shows a new Linux boot ID and the target
reports Menu idle.

The host API path works now. A browser file picker for the same endpoint is
the next UI extension; it is not a second loading path.

## Repository boundaries

| Repository | Owns |
| --- | --- |
| `FogCast` | Host application, browser UI, catalog, target agent, content transfer, and launch requests |
| `Main_MiSTer` | The MiSTer/Main implementation used by the target image |
| `misteross` | Quartus, Verilator, and open-source FPGA builds that produce RBF files |

The current FogCast tree has one active target-image toolchain and one direct
launch path. Superseded experiments are removed from the working tree; Git
history is the archive.

## Build and test

```sh
make build
make test
make vet
git diff --check
```

For the target image, fixture details, deployment, and live launch checks,
read [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). The current process
boundaries and source entry points are in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
