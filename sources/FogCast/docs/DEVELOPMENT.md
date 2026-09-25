# FogCast development

## Local checks

```sh
go test ./...
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
node --test internal/hostapi/ui_browser_test.js
go vet ./...
git diff --check
```

`make test` runs these checks plus operator-script tests. Native image
fixture tests live in the FES `image/` recipe. Use
`TMPDIR=/home/deano/.cache/fogcast-tmp` on the development host if the
system `/tmp` is full.

## Binaries and target image

`make build-fogcast` and `make build-fogcast-api` use `FOGCAST_GOOS` /
`FOGCAST_GOARCH`, defaulting to this machine's `go env GOOS` / `GOARCH`.
FES `make host` passes `linux` / `amd64`. The signed sofa app remains
`make build-fogcast-host` on Darwin.

```sh
make build
make build-agent
```

Native Buildroot and rootfs assembly live in the FES `image/` recipe. From a
FES checkout, `make dev` / `make build` / `make verify` invoke
`make -C image FOGCAST_DIR=<this checkout>` after building the agent, kit
and lock selector.

The development image includes SSH and curl. The production image does not;
capture and decoding tools run on the host.

Build and inspect the native-runtime image from FES with a clean runtime
checkout at the runtime gitlink selected by FES. FES generates the concrete
assembly lock from that selection; the FogCast policy does not pin runtime source:

```sh
make build
make verify
```

FES publishes `out/<profile>/linux.img`. The cold build runs twice and
requires identical image digests. Verification inspects the locked idle RBF,
selected Mega Drive RBF and normalized build-input record, ARM runtime and
static ARM agent, and the runtime's target-library closure. QEMU proves only
the read-only root, volatile mounts, and init packaging. The designated-kit
idle and one-player Mega Drive launch/input/Stop/relaunch paths are
hardware-tested. The existing MiSTer-compatible native development-RBF
load/Stop lifecycle and its subsequent game regression are also
hardware-tested; exact evidence is in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md).
`native-dev` defaults to the Mega Drive core set. It can also package the
explicit four-system set described below. This does not establish a
generalized RBF ABI or generic development video/input guarantee.

### Optional Pong, SNES and NES image cores

For the FES four-system profile, keep the same selected runtime checkout and
Mega Drive selection, and supply three sealed misteross bundles:

```sh
make build
make verify
```

The FES `native-integration-dev` profile already selects the four-system set.

The only admitted sets are `megadrive` (the unchanged default) and
`megadrive pong snes nes`. Pong/SNES/NES use source-built bundles only. Each contains
exactly `<system>.rbf` and `<system>-rbf.toml`, using the existing closed format-1
manifest. The selector validates system, source identity, recipe, digest and
size before publishing a sealed artifact and selection pair. Pong identifies
its exact misteross producer commit; SNES identifies source commit
`93d359e6f23c734ae3928984e88bed1d9b53cbac`; NES identifies source commit
`9a63821173b6da4d6e95dcbe2e2a322ec8171144`. FES additionally binds producer and
recipe hashes to its selected component commits.

The normal Buildroot post-build hook installs all selected cores and their
provenance records. Returning to the default removes leftover Pong/SNES/NES cores
and records. Image verification takes the expected set from the caller and
compares installed bytes and records with the external selection sidecars;
it never infers acceptance from files found inside the image. Preserve the
`pong.selection.toml`, `snes.selection.toml` and `nes.selection.toml` files
alongside the existing Mega Drive sidecar when moving an image. Both reproducible passes compare
these sidecars, and the Make variables reach fetch, build, verification and
QEMU consistently.

These packaging checks are host-side evidence. QEMU checks init packaging;
physical video, input and audio require acceptance of the exact assembled
image. Basic SNES supports ordinary LoROM/HiROM cartridges, without enhancement
chips or persistent saves.

### FES format-2 image package set

FES may select one or more sealed packages from the supported format-2 core IDs
`fes.pong`, `fes.zx81`, and `fes.coleco`. The ordered, comma-separated package
set and each selected core's trusted absolute inputs are passed to image fetch,
build, and verification:

```sh
export FES_PACKAGE_IDS=fes.pong,fes.zx81,fes.coleco
export FES_PONG_PACKAGE_DIR=/absolute/path/to/<pong-package-id>
export FES_PONG_PACKAGE_SELECTION=/absolute/path/to/fes-pong.package-selection.toml
export FES_ZX81_PACKAGE_DIR=/absolute/path/to/<zx81-package-id>
export FES_ZX81_PACKAGE_SELECTION=/absolute/path/to/fes-zx81.package-selection.toml
export FES_COLECO_PACKAGE_DIR=/absolute/path/to/<coleco-package-id>
export FES_COLECO_PACKAGE_SELECTION=/absolute/path/to/fes-coleco.package-selection.toml
```

Each selected core requires its pair; unselected cores must not supply package
inputs. The package directory contains exactly `manifest.toml` and `core.rbf`;
the closed selection records format/kind, core and package IDs, payload
SHA-256, the selected misteross and mister-packages revisions, and the derived
install path. `target-image-lock select-package --core-id <core-id>` copies a
validated pair into the native cache. `verify-package --core-id <core-id>`
reinspects the exact bytes, while
`verify-package --core-id <core-id> --print-inputs` emits the canonical
package projection used by the installed build-input record and image verifier.

The canonical per-core names and build-input prefixes are:

| core ID | external selection | installed selection | input prefix |
| --- | --- | --- | --- |
| `fes.pong` | `fes-pong.package-selection.toml` | `fes-pong.package.toml` | `fes_pong` |
| `fes.zx81` | `fes-zx81.package-selection.toml` | `fes-zx81.package.toml` | `fes_zx81` |
| `fes.coleco` | `fes-coleco.package-selection.toml` | `fes-coleco.package.toml` | `fes_coleco` |

For every selected core, `--print-inputs` emits
`<input-prefix>_package_selection_sha256`, `<input-prefix>_package_id`,
`<input-prefix>_payload_sha256`, `<input-prefix>_misteross_revision`,
`<input-prefix>_mister_packages_revision`, and `<input-prefix>_install_path`.
The image installs each pair read-only beneath
`/usr/share/mister-runtime/core-packages/<package-id>/` and
`/usr/share/mister-runtime/selections/`. Both cold image passes compare the
external selection records byte-for-byte. Removing a selected input pair
removes its cached and installed package state. The FES orchestrator derives
these inputs from its selected recipe; they are not an ambient package lookup
or fallback mechanism.

### Native Mega Drive RBF selection

Source-built Mega Drive selection is the native image default; use the explicit upstream selection for fallback.

For the default source-built path, provide a sealed bundle containing exactly
`megadrive.rbf` and `megadrive-rbf.toml`:

- `make build MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle`

To exercise the locked upstream fallback explicitly:

- `make build MEGADRIVE_RBF_SOURCE=upstream`

There is no automatic fallback between the two RBF selections. A source-built
failure stops before Buildroot/image mutation, and the upstream mode resolves
the locked release independently. Both selections use the MiSTer ABI; generalized/custom/non-MiSTer RBF ABI support is deferred. The selection and
provenance flow is software-tested; source-built artifact qualification on the
designated kit is a separate later hardware gate.

### Fast target iteration policy

During active runtime or target debugging, do not rebuild the complete image
for every source change. Run the narrow host tests, cross-build only the
self-contained changed runtime artifact, and place it in a disposable copy of
the last verified image. Deploy that derived image for a bounded diagnostic,
preserving the verified source image and recording the derived hash. Derived
image results are diagnostic only; they do not establish reproducibility,
release readiness, or hardware acceptance.

When replacing a root image on the running kit, keep the old image under a
backup filename until after reboot. Never overwrite its contents or rename a
replacement directly over its last filename: the loop device still holds it
open, and reboot can leave orphaned FAT allocation chains. Stage and verify the
new image, rename the running image to an unused backup name, install the staged
image, sync and reboot; delete the backup only after verifying the new boot.
Keep power stable during the two renames.

Use the FES two-pass `make build` / `make verify` after a change has
stabilized and immediately before a major PR, merge, release, or formal
hardware acceptance. The full image rebuild is also required for any change to
Buildroot, init scripts, package contents, image configuration, or locked
inputs, because a runtime-only replacement cannot validate those changes.

### Milestone status

```text
legacy dev/prod = current game-capable path
native-dev = hardware-tested Mega Drive launch/input/Stop/relaunch
Milestone 2 = complete
Milestone 3 = complete for the defined one-player Mega Drive vertical slice
native development RBF = hardware-tested MiSTer-compatible load/Stop/game-regression path
Milestone 4 = complete for the defined MiSTer-compatible development lifecycle
```

## Dedicated fixture

The designated disposable kit is:

- Host: `powerboat` (`192.168.10.203`)
- Native MiSTer Pi: `192.168.10.84`, SSH `root` / `1`
- ROM share: `//DEANO-CLAWZ/Games`
- ROM directory inside the share: `Games`
- Powerboat mount: `/mnt/fogcast-games` (read-only, configured locally)
- Host config: `~/.config/fogcast/config.toml` (untracked, mode `0600`)
- Host API: `http://127.0.0.1:8787`
- Target API: `http://192.168.10.84:8182`
- HDMI capture: ShadowCast 3 on `/dev/video0`

Smoke and deploy scripts default to that Pi. Override with
`FOGCAST_TARGET_HOST` and `FOGCAST_TARGET_API` when using another device.
The private `config.toml` `[[targets]]` address must match the designated
kit. Do not commit credentials or print them in logs.

The Powerboat kit host (`192.168.10.203:8789`, `fogcast-api-sofa` plus
`launcher-host.json`) uses the same private `config.toml`. Run that binary
with `--headless` so the kit listener stays up without DISPLAY, DRM, or
V4L2/FFmpeg preview. On Powerboat the API binds `127.0.0.1:8787` and the
launcher binds `*:8789`. `fogcast-kit` reconnects to
`http://192.168.10.203:8789` with the existing `launcher.json` API URL.
Enable LaunchBox metadata there with `[metadata] provider = "launchbox"` and
an absolute archive path; see [kit launcher HOW_TO_RUN](kit-launcher.md#how_to_run-launchbox-covers-on-the-kit-host).

The designated kit runs the native appliance image. Init starts
`mister-runtime` and `mister-agent` from `/usr/sbin`, then
`fogcast-kit`. Agent configuration is `/media/fat/fogcast/agent.toml`. There
is no Main process and no `/dev/MiSTer_cmd`. HDMI idle uses the locked idle
RBF through the runtime. Health and lifecycle observations use runtime protocol 2. FAT holds agent config, launcher cache and content;
the root filesystem is the immutable loop image.

The fixture uses the stock MiSTer login and changing SSH host keys after a
rebuild is expected. Reflashing or replacing the image is normal. Do not use
a soft board reboot to recover a running FPGA session.

### Restarting target services

Overlay swaps and init restarts stay on the current boot:

1. Stop the session to idle from the host (`fogcast stop` or the session Stop control).
2. Confirm the kit lease is free.
3. Restart `mister-runtime` and `mister-agent` with SIGTERM through `mister-supervise` (`/etc/init.d/S40mister-runtime restart` and `/etc/init.d/S50mister-agent restart`).

Do not use `/sbin/reboot`, `POST /v1/development/reboot`, or `kill -9` mid-session for an overlay swap. Killing the agent during a session makes startup cleanup fail and leaves the kit lease blocked. `POST /v1/development/reboot` is unsafe after FPGA or HPS activity: it asks the runtime to program idle first, and it runs `/sbin/reboot` only when that LoadIdle fails with `idle_failed`. If the agent still reports `reboot_required`, recover with a hard power-supply cycle. A front-panel reset does not replace that power cycle.

Path B is that board-reboot arm. It is not part of the restart above. The arming rules and the held kit check are in the FES [soft-restart Path B](../../../docs/soft-restart-path-b.md) note. Do not run that kit check.

After separately authorized deployment of the selected native image, exercise
its installed package lifecycle through the running host:

```sh
make target-package-smoke \
  FES_PONG_PACKAGE_SELECTION=/absolute/path/fes-pong.package-selection.toml \
  FES_ZX81_PACKAGE_SELECTION=/absolute/path/fes-zx81.package-selection.toml \
  FES_COLECO_PACKAGE_SELECTION=/absolute/path/fes-coleco.package-selection.toml
```

This explicitly launches and stops the three installed packages through the
host session API, checks their exact selected package IDs, and uses the existing
kit lease. It does not deploy an image or request a reboot. Health and inventory
calls allow 30 seconds by default. Launch allows 90 seconds for first-time ROM
composition; `FOGCAST_LAUNCH_TIMEOUT` overrides launch separately, while
`FOGCAST_CALL_TIMEOUT` sets the other calls and the launch fallback. If a
launch request fails, the script watches for the selected package becoming
active or returning idle and sends Stop to release its lease before exiting;
it reports an unresolved outcome for operator inspection if status cannot be
reconciled. A passing API lifecycle diagnostic does not establish HDMI, audio,
controller, persistence or exact-image acceptance. Record those physical checks
separately against the selected image/runtime/package digests;
historical raw-core acceptance does not qualify the new artifacts.

## Deploy and exercise the kit

Use the FES [appliance release guide](../../../docs/appliance-releases.md) or
[bootable-media guide](../../../docs/bootable-media.md) for the exact authorized
device and existing kit lease. The old direct SCP/reboot deployment command
has been retired; builds and smoke tests do not automatically deploy.
Installed package library entries launch through `POST /api/v1/session/launch`;
explicit `.fcore` development loads use `core-load`. Neither path launches a
raw-core catalog record or depends on CORENAME/Main state.

Load a locally built development RBF through the host API:

```sh
curl --fail -H 'Content-Type: application/octet-stream' \
  --data-binary @/absolute/path/to/top.rbf \
  http://127.0.0.1:8787/api/v1/session/development-rbf
curl --fail http://127.0.0.1:8787/api/v1/session
curl --fail -X POST http://127.0.0.1:8787/api/v1/session/stop
```

The active response is `{"state":"active","execution":"fpga_development"}`.
Contained raw `development-rbf` is an explicit hardware diagnostic. It has no
package ABI, media or input contract. Stop restores the locked idle RBF; an
explicit `reboot_required` result requires separately requested recovery.
Format-2 `.fcore` packages (Pong, ZX81) use the native
`fes-gp-v1` path:

```sh
fogcast --json --api http://127.0.0.1:8787 core-load /absolute/path/core.fcore
```

That POSTs `/api/v1/session/development-core`. Success reports the package
id, ABI, build id and generation. `fes.keyboard` cores attach the matrix
path; raw RBF input stays disabled. Host `request_timeout_seconds` bounds
host-to-target RPCs and must cover inspect plus program of the archive,
not only the upload to the host API.

When target access is needed directly:

```sh
sshpass -p 1 ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null root@192.168.10.84 \
  'curl --version'
curl --fail http://192.168.10.84:8182/v1/health
```

## Change discipline

Before changing target behavior, trace the path from
`docs/ARCHITECTURE.md` into the named source files. Test host-only behavior
locally, then run the actual operation on the designated kit when the change
touches FPGA loading, input, video, audio, or target lifecycle.

Names in the active tree describe current use: the image toolchain is
`target-image`, the target FAT directory is `fogcast`, and the sender command
is `remote-play-sender`. Do not introduce numbered experiment names into
active code or docs.
