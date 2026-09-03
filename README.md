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
- Native SDL3 10-foot cover grid on Mac (`cmd/fogcast-tenfoot`) that calls the
  same public host API. See
  [docs/native-tenfoot-launcher/README.md](docs/native-tenfoot-launcher/README.md).
- A reproducible target image toolchain with a development image containing
  SSH and curl.
- A separate reproducible `native-dev` image that packages the native runtime,
  native agent backend, one locked idle RBF, and one locked Mega Drive RBF.
  Its idle path is hardware-tested on the designated kit. The Mega Drive
  launch translation and packaging are software-tested candidates awaiting
  exact-image physical acceptance; no native game system is supported yet.

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

The working FPGA game path remains the conventional `dev`/`prod` image path
described above. The `native-dev` image still has zero supported game systems
and remains the hardware-tested idle baseline. Its candidate agent path accepts
only registry system `megadrive`, sends the image-owned core and staged
cartridge path to `mister-runtime`, and continues to reject development RBFs
and every other system. Physical launch, input, Stop, and relaunch acceptance
remain pending.

## Milestone status

```text
legacy dev/prod = current game-capable path
native-dev = hardware-tested idle baseline, Mega Drive candidate pending acceptance
Milestone 2 = complete
Milestone 3 = software integration in progress; exact-image hardware acceptance pending
```

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
