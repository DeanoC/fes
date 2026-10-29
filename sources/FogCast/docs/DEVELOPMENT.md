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

Build and inspect the native-runtime image from FES with the runtime module
committed in the same FES revision. FES generates the concrete assembly lock
from that selection; the FogCast policy does not pin runtime source:

```sh
make build
make verify
```

FES publishes `out/<profile>/linux.img`. The cold build runs twice and
requires identical image digests. Verification inspects the locked idle RBF,
selected core packages and normalized build-input record, ARM runtime and
static ARM agent, and the runtime's target-library closure. QEMU proves only
the read-only root, volatile mounts, and init packaging. The earlier Mega
Drive/Pong/SNES/NES raw-RBF image lane is retired. Images install only the
sealed core packages described below. There is no NES, SNES, or Mega Drive
package in the factory set. Dated one-player Mega Drive
launch/input/Stop/relaunch results, and the
[development-RBF baseline](hardware/native-development-rbf-baseline.md) that
loaded a Mega Drive bitstream, are evidence for that retired lane. They do
not accept this image. See [core status](../../../docs/core-status.md).

### FES image package set

The image selector supports ten core IDs: `fes.menu`, `fes.pong`, `fes.zx81`,
`fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.c64`, `fes.spectrum`, `fes.ramtest`,
`fes.atari-st`, using sealed
format-2, format-3, or format-4 packages. The factory `fes.coleco` package uses
format 4 and requires `rom-map.json`. `fes.menu` is idle display firmware, not a
playable catalog entry. The current [FES default profile](../../../profiles/native-integration-dev.toml)
selects menu, Pong, ZX81, Coleco, SMS, SG-1000, Spectrum, the OSS 100 MHz RAM Tester
and Atari ST. ST firmware is separately selected; the image bundles no ROM.
C64 remains package-only without factory-image acceptance.
See [core status](../../../docs/core-status.md) for exact-artifact evidence.

The following is the complete ten-ID input example for the selector interface;
it requires sealed packages and sufficient image capacity before use. It does
not describe the current default image. The ordered, comma-separated package
set and each selected core's trusted absolute inputs reach image fetch, build
and verification:

```sh
export FES_PACKAGE_IDS=fes.menu,fes.pong,fes.zx81,fes.coleco,fes.sms,fes.sg1000,fes.c64,fes.spectrum,fes.ramtest,fes.atari-st
export FES_MENU_PACKAGE_DIR='/absolute/path/to/<menu-package-id>'
export FES_MENU_PACKAGE_SELECTION=/absolute/path/to/fes-menu.package-selection.toml
export FES_PONG_PACKAGE_DIR='/absolute/path/to/<pong-package-id>'
export FES_PONG_PACKAGE_SELECTION=/absolute/path/to/fes-pong.package-selection.toml
export FES_ZX81_PACKAGE_DIR='/absolute/path/to/<zx81-package-id>'
export FES_ZX81_PACKAGE_SELECTION=/absolute/path/to/fes-zx81.package-selection.toml
export FES_COLECO_PACKAGE_DIR='/absolute/path/to/<coleco-package-id>'
export FES_COLECO_PACKAGE_SELECTION=/absolute/path/to/fes-coleco.package-selection.toml
export FES_SMS_PACKAGE_DIR='/absolute/path/to/<sms-package-id>'
export FES_SMS_PACKAGE_SELECTION=/absolute/path/to/fes-sms.package-selection.toml
export FES_SG1000_PACKAGE_DIR='/absolute/path/to/<sg1000-package-id>'
export FES_SG1000_PACKAGE_SELECTION=/absolute/path/to/fes-sg1000.package-selection.toml
export FES_C64_PACKAGE_DIR='/absolute/path/to/<c64-package-id>'
export FES_C64_PACKAGE_SELECTION=/absolute/path/to/fes-c64.package-selection.toml
export FES_SPECTRUM_PACKAGE_DIR='/absolute/path/to/<spectrum-package-id>'
export FES_SPECTRUM_PACKAGE_SELECTION=/absolute/path/to/fes-spectrum.package-selection.toml
export FES_RAMTEST_PACKAGE_DIR='/absolute/path/to/<ramtest-package-id>'
export FES_RAMTEST_PACKAGE_SELECTION=/absolute/path/to/fes-ramtest.package-selection.toml
export FES_ATARI_ST_PACKAGE_DIR='/absolute/path/to/<atari-st-package-id>'
export FES_ATARI_ST_PACKAGE_SELECTION=/absolute/path/to/fes-atari-st.package-selection.toml
```

Each selected core requires its pair; unselected cores must not supply package
inputs. The package directory contains exactly `manifest.toml` and `core.rbf`,
plus the required `rom-map.json` for format 3 or 4. The closed selection records
format/kind, core and package IDs, payload SHA-256, the selected misteross and
mister-packages revisions, and the derived install path.
`target-image-lock select-package --core-id <core-id>` copies a
validated pair into the native cache. `verify-package --core-id <core-id>`
reinspects the exact bytes, while
`verify-package --core-id <core-id> --print-inputs` emits the canonical
package projection used by the installed build-input record and image verifier.

The canonical per-core names and build-input prefixes are:

| core ID | external selection | installed selection | input prefix |
| --- | --- | --- | --- |
| `fes.menu` | `fes-menu.package-selection.toml` | `fes-menu.package.toml` | `fes_menu` |
| `fes.pong` | `fes-pong.package-selection.toml` | `fes-pong.package.toml` | `fes_pong` |
| `fes.zx81` | `fes-zx81.package-selection.toml` | `fes-zx81.package.toml` | `fes_zx81` |
| `fes.coleco` | `fes-coleco.package-selection.toml` | `fes-coleco.package.toml` | `fes_coleco` |
| `fes.sms` | `fes-sms.package-selection.toml` | `fes-sms.package.toml` | `fes_sms` |
| `fes.sg1000` | `fes-sg1000.package-selection.toml` | `fes-sg1000.package.toml` | `fes_sg1000` |
| `fes.c64` | `fes-c64.package-selection.toml` | `fes-c64.package.toml` | `fes_c64` |
| `fes.spectrum` | `fes-spectrum.package-selection.toml` | `fes-spectrum.package.toml` | `fes_spectrum` |
| `fes.ramtest` | `fes-ramtest.package-selection.toml` | `fes-ramtest.package.toml` | `fes_ramtest` |
| `fes.atari-st` | `fes-atari-st.package-selection.toml` | `fes-atari-st.package.toml` | `fes_atari_st` |

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

### Fast target iteration policy

For active target/runtime development, run focused tests and build a compatible
[kit software bundle](../../../docs/kit-software-releases.md) against a verified
retained base image. Managed kits accept installation while busy and queue
activation until execution ownership is released. Successful selections remain
installed; neither binary/configuration overlays nor routine stock restoration
are supported on managed images. Use a private host for branch diagnostics.

A core using an installed ABI follows package import/selection/launch. A host
change uses its independent private-host build. Neither requires matching source
revisions or a new kit image. Diagnostics pin the active bundle and base-image
identities and check the advertised operation contracts; source revisions remain
provenance.

Changes to the base supervisor, startup, system libraries, kernel, image
configuration, factory-image packages or locked base inputs require a planned
appliance-image update and the two-pass FES `make build` / `make verify` checks.
Use the [appliance release workflow](../../../docs/appliance-releases.md) under
the designated kit lease, preserving factory, known-good, previous and referenced
images. Building an artifact does not authorize its deployment or establish its
hardware acceptance.

## Dedicated fixture

The designated disposable kit is:

- Host: `powerboat` (`192.168.10.202`)
- Native MiSTer Pi: `192.168.10.84`, SSH `root` / `1`
- ROM share: `//DEANO-CLAWZ/Games`
- ROM directory inside the share: `Games`
- Powerboat mount: `/mnt/fogcast-games` (read-only, configured locally)
- Host config: `~/.config/fogcast/config.toml` (untracked, mode `0600`)
- Host API: `http://127.0.0.1:8787`
- Target API: `http://192.168.10.84:8182`
- HDMI capture: ShadowCast 3 at `/dev/v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0`

Powerboat has two USB capture cards. `/dev/videoN` follows enumeration order,
so capture always uses a `/dev/v4l/by-id` path and never `/dev/videoN`.

- ShadowCast (kit HDMI, back USB-C, 5 Gbit/s): `/dev/v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0`
- ASUS TUF 4KPRO (front USB-A, 10 Gbit/s): `/dev/v4l/by-id/usb-ASUS_4KPRO_802B003090700329-video-index0`

Powerboat loads `uvcvideo` with `quirks=0x80` globally. The 4KPRO needs that
quirk, and the ShadowCast still produces normal frames with it. A udev rule
and retry script keep the 4KPRO working across replugs and reboots.

A ShadowCast still is:

```sh
ffmpeg -hide_banner -loglevel error -y -f v4l2 -input_format mjpeg -video_size 1920x1080 -framerate 30 -i /dev/v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0 -vf 'select=eq(n\,4)' -frames:v 1 OUT.png
```

Smoke and deploy scripts default to that Pi. Override with
`FOGCAST_TARGET_HOST` and `FOGCAST_TARGET_API` when using another device.
The private `config.toml` `[[targets]]` address must match the designated
kit. Do not commit credentials or print them in logs.

The Powerboat kit host (`192.168.10.202:8789`, `fogcast-api-sofa` plus
`launcher-host.json`) uses the same private `config.toml`. Run that binary
with `--headless` so the kit listener stays up without DISPLAY, DRM, or
V4L2/FFmpeg preview. On Powerboat the API binds `127.0.0.1:8787` and the
launcher binds `*:8789`. Kit tenfoot reconnects to
`http://192.168.10.202:8789` with the existing `launcher.json` API URL.
Enable LaunchBox metadata there with `[metadata] provider = "launchbox"` and
an absolute archive path; see [kit launcher HOW_TO_RUN](kit-launcher.md#how_to_run-launchbox-covers-on-the-kit-host).

The designated kit runs a native appliance image. Managed images start the
base-image `fes-kit-software` supervisor, which selects one immutable runtime,
agent and launcher group. Software identities and pending updates are available
through `fes-software --action status`; authenticated health reports them
separately from base-image artifacts. Agent configuration remains
`/media/fat/fogcast/agent.toml`. There is no Main process or `/dev/MiSTer_cmd`.
HDMI idle uses the locked idle RBF through runtime protocol 2. The root filesystem
is the immutable appliance loop image; installed software and core packages use
verified FESDATA3 storage. A build does not establish which image or software is
currently booted on the designated kit.

The fixture uses the stock MiSTer login and changing SSH host keys after a
rebuild is expected. Reflashing or replacing the image is normal. Do not use
a soft board reboot to recover a running FPGA session.

### Updating target software

On managed images, install an immutable bundle and queue its activation through
[kit software releases](../../../docs/kit-software-releases.md). The existing
execution owner keeps playing and renewing until explicit Stop/release. A soft
Stop that retains the lease keeps the update queued. At the released idle
boundary, target maintenance fences new sessions and the base supervisor replaces
the complete group. Accepted updates remain selected. Startup failure permits one
recorded attempt to recover the prior bundle; a failed recovery stays blocked.

Do not restart individual runtime, agent or launcher processes, kill them as a
routine update, or invoke retired per-service init scripts. The supervisor must
verify that the old processes exited before any replacement can control hardware.
Branch diagnostics use a private host and an explicitly installed bundle;
returning to a prior bundle is another generation-checked activation. There is
no file-overlay path.

A software update does not reboot the board. `/sbin/reboot` and
`POST /v1/development/reboot` are not service-restart operations. If physical
recovery reports `reboot_required`, keep execution fenced and arrange separately
authorized board recovery. A software reboot can clear the observed HDMI I2C
failure; verify the new boot, native idle and admission before resuming work.
If the fault persists, use the designated kit's hard power-supply cycle; a
front-panel reset does not replace that cycle. The separate
[soft-restart Path B](../../../docs/soft-restart-path-b.md) note describes the
board-reboot diagnostic and its arming rules. Do not run that kit check.

After separately authorized deployment of the selected native image, exercise
its installed package lifecycle through the running host:

```sh
make target-package-smoke \
  FES_PONG_PACKAGE_SELECTION=/absolute/path/fes-pong.package-selection.toml \
  FES_ZX81_PACKAGE_SELECTION=/absolute/path/fes-zx81.package-selection.toml \
  FES_COLECO_PACKAGE_SELECTION=/absolute/path/fes-coleco.package-selection.toml \
  FES_RAMTEST_PACKAGE_SELECTION=/absolute/path/fes-ramtest.package-selection.toml
```

This launches and stops Pong, ZX81 and Coleco through the host session API and
checks their exact selected package IDs. Supplying the fourth RAM Tester
selection enables that additional launch check. Before running it, install the
RAM Tester host package and set up its library `CoreEntry` so the host library
contains an entry for the installed package; installing the target package
alone does not create that host entry. RAM Tester is a ROM-less application
core with a library launch entry. The smoke uses the existing
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

## Install software and core packages

Use [kit software releases](../../../docs/kit-software-releases.md) for a runtime,
agent or launcher change. Use the [durable core library](../../../docs/kit-core-library.md)
for compatible core installation and future selection. Neither operation requires
rebuilding the base OS, and core installation does not restart services. Retain
the operator's shared host; branch diagnostics use private configurations, ports,
catalogs and evidence.

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

## Kit resource diagnostics

A read-only collector reports image id, uptime, load, CPU idle, `fogcast-*`
CPU and RSS, memory, swap, CMA, and filesystem size for `/`, `/media/fat`,
and `/tmp`. It does not write to the kit. From `sources/FogCast`:

```sh
scripts/kit-diagnostics-ssh.sh 192.168.10.84
```

Budgets for the local host and shell, and the kit B idle-CPU finding, are in
[kit resource limits](kit-resource-limits.md).

## Change discipline

Before changing target behavior, trace the path from
`docs/ARCHITECTURE.md` into the named source files. Test host-only behavior
locally, then run the actual operation on the designated kit when the change
touches FPGA loading, input, video, audio, or target lifecycle.

Names in the active tree describe current use: the image toolchain is
`target-image`, the target FAT directory is `fogcast`, and the sender command
is `remote-play-sender`. Do not introduce numbered experiment names into
active code or docs.
