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
  recovery back to Menu for non-MiSTer cores on the conventional Main backend.
- Browser UI, local media previews, and host-emulator/remote-media modes.
- Native SDL3 10-foot launcher (`cmd/fogcast-tenfoot`) with cover-grid, shelf,
  and list layouts that calls the same public host API. Mac is the primary
  sofa target; Linux uses the same Makefile target with system SDL3
  (`pkg-config sdl3`). See
  [docs/native-tenfoot-launcher/README.md](docs/native-tenfoot-launcher/README.md).
- A reproducible target image toolchain with a development image containing
  SSH and curl.
- A separate reproducible `native-dev` image that packages the native runtime,
  native agent backend, one locked idle RBF, and one selected Mega Drive RBF.
  Source-built Mega Drive selection is the native image default; use the
  explicit upstream selection for fallback. Its idle path and one-player Mega
  Drive launch, input, Stop, and immediate relaunch path are hardware-tested
  on the designated kit. See the dated
  [native Mega Drive baseline](docs/hardware/native-megadrive-baseline.md).

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
`application/octet-stream` body. On the conventional Main backend, FogCast
installs it temporarily and loads it through `/dev/MiSTer_cmd`; Stop uses the
existing reboot-required recovery handshake.

The native backend supports the same API path for the existing
MiSTer-compatible development ABI. It atomically stages the upload at
`/tmp/fogcast-development/core.rbf`, asks `mister-runtime` to power down HDMI,
program the FPGA, synchronize the core, and report development state without a
game or system identity. HDMI stays down until Stop reloads the locked idle
RBF. Raw development uploads have no video or input guarantee. The native
image packages no development RBF. Its Mega Drive RBF is selected at build
time as described below. The
exact two-cycle acceptance and legacy rollback evidence is recorded in
[native-development-rbf-baseline.md](docs/hardware/native-development-rbf-baseline.md).
There is no browser file picker.

## Native Mega Drive RBF selection

Source-built Mega Drive selection is the native image default; use the explicit upstream selection for fallback.

The native image resolves one sealed two-file bundle before Buildroot. The
bundle contains `megadrive.rbf` and its closed `megadrive-rbf.toml` provenance
record. Both files must be regular, sealed (no write bits), and byte-matched to
the declared source revision, recipe, toolchain, size, and SHA-256. The bundle
path is supplied by the operator and is never embedded in the image.

Build with the source-built default:

- `make target-image-native MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle`

Build using the locked upstream release explicitly:

- `make target-image-native MEGADRIVE_RBF_SOURCE=upstream`

There is no automatic fallback between the two RBF selections. Both modes
install exactly one Mega Drive RBF at the same role path and keep runtime
launch, Stop, video, media, and input behavior unchanged. Both selections use the MiSTer ABI; generalized/custom/non-MiSTer RBF ABI support is deferred.

The selection and provenance path is software-tested. Qualification of the
source-built artifact on the designated kit is a separate later hardware gate;
the existing hardware baseline does not silently qualify a different RBF.

## Repository boundaries

| Repository | Owns |
| --- | --- |
| `FogCast` | Host application, browser UI, catalog, target agent, content transfer, and launch requests |
| `Main_MiSTer` | The MiSTer/Main implementation used by the target image |
| `misteross` | Quartus, Verilator, and open-source FPGA builds that produce RBF files |

The current FogCast tree has one active target-image toolchain and one direct
launch path. Superseded experiments are removed from the working tree; Git
history is the archive.

The conventional `dev`/`prod` image remains the broad FPGA game and
development-RBF path described above. The separate `native-dev` image supports
only registry system `megadrive` for catalogue launches: it sends the
image-owned core and staged cartridge path to `mister-runtime` and rejects
every other catalogue system. Its separate MiSTer-compatible development-RBF
path is hardware-tested for the narrow MiSTer-compatible load/Stop lifecycle
and subsequent game regression. The accepted game slice is one player with
D-pad, A/B/C, and Start. Audio, saves, six-button input, multiplayer,
remapping, hot-plug recovery, generalized RBF ABIs, and development video/input
remain unsupported.

## Milestone status

```text
legacy dev/prod = current game-capable path
native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch
Milestone 2 = complete
Milestone 3 = complete for the defined one-player Mega Drive vertical slice
native development RBF = hardware-tested MiSTer-compatible load/Stop/game-regression path
Milestone 4 = complete for the defined MiSTer-compatible development lifecycle
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
