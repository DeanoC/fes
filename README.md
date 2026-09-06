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

## Built-in native Pong (software integration)

Pong appears in the host catalog as game ID `pong` without adding a library or
ROM. Launch it through the existing browser/session path, or
`POST /api/v1/session/launch` with `{"game_id":"pong"}`. It requires the native
runtime and installed `/usr/share/mister-runtime/cores/pong.rbf`; the Main
backend does not support this ROM-less profile. Supplied media is rejected.
This integration is software-tested; Pong RBF/image packaging and playable
hardware acceptance is recorded separately by the FES integration task.

## Native SNES (software integration)

SNES uses the existing library, cache, and session launch path with the native
runtime and installed `/usr/share/mister-runtime/cores/snes.rbf`. FogCast passes
one unchanged cartridge path (`.sfc`, `.smc`, or `.bin`); the runtime owns format
validation, copier-header handling, and the required metadata transfer prefix.
The initial runtime contract is ordinary LoROM/HiROM up to 4 MiB; enhancement
chips, external firmware, and expanded mappings remain outside this slice.
The native agent stores ordinary SNES battery saves per game and ROM beneath
`/media/fat/fogcast/saves/snes`. The runtime restores them on launch and flushes
them before a clean Stop, including system switching and lease cleanup. Saves
survive cache eviction and target reboot; a write failure keeps Stop retryable
and prevents successful lease release. Stop before rebooting: there is no
power-loss autosaving, host synchronization, or save-state support. Cartridge
copier-header variants have separate save identities. This persistence path is
software-tested; hardware validation belongs to the selected FES integration. The native package uses cartridge index 1; the conventional Main
selector remains index 0.

The retained native gamepad includes A/B/X/Y/L/R/Select/Start and the D-pad.
Existing MD C and Start codes keep their meaning. Host event normalization,
lease delivery, and disconnect/Stop neutralization use the existing input path;
this does not add a browser gamepad-capture UI. Tests use fake runtime/target
and uinput calls; exact-image SNES hardware acceptance remains separate.

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
D-pad, A/B/C, and Start. Audio, Mega Drive saves, six-button input, multiplayer,
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

Native image assembly defaults to Mega Drive; the explicit
`NATIVE_RUNTIME_SYSTEMS="megadrive pong snes"` selection adds sealed Pong/SNES
source bundles through the same builder and verifier. See the
[image development guide](docs/DEVELOPMENT.md#optional-pong-and-snes-image-cores).

Native image preflight selects the Darwin host verifier on macOS and the Linux
verifier in the build container. `target-image-native-fetch` builds both when
running on macOS; `TARGET_IMAGE_LOCK_BIN` remains an explicit verifier override.
