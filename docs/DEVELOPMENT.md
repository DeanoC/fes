# FogCast development

## Local checks

```sh
go test ./...
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
node --test internal/hostapi/ui_browser_test.js
go vet ./...
git diff --check
```

`make test` runs these checks plus the target-image fixture tests and the
operator-script tests. Use `TMPDIR=/home/deano/.cache/fogcast-tmp` on the
development host if the system `/tmp` is full.

## Binaries and target image

```sh
make build
make build-agent
make target-image-fetch
make target-image-dev
```

The development image is
`build/output/target-image/dev/linux.img`. The release image and kernel use:

```sh
make target-images
make target-image-verify
make target-kernel-verify
```

The development image includes SSH and curl. The production image does not;
capture and decoding tools run on the host.

Build and inspect the separate native-runtime image with a clean runtime
checkout at the commit pinned by `build/native-runtime.inputs.lock.toml`:

```sh
export LIBMISTER_RUNTIME_DIR=/absolute/path/to/libmister-runtime
make target-image-native
make target-image-native-verify
make target-image-native-qemu-smoke
```

This produces `build/output/target-image/native-dev/linux.img`. The build runs
twice and requires identical image digests. Verification inspects the locked
idle RBF, selected Mega Drive RBF and normalized build-input record, ARM runtime and static ARM agent, and the
runtime's target-library closure. QEMU proves only the read-only root,
volatile mounts, and init packaging. The designated-kit idle and one-player
Mega Drive launch/input/Stop/relaunch paths are hardware-tested. The existing
MiSTer-compatible native development-RBF load/Stop lifecycle and its subsequent
game regression are also hardware-tested; exact evidence is in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md).
`native-dev` defaults to the Mega Drive core set. It can also package the
explicit four-system set described below. This does not establish a generalized
RBF ABI or generic development video/input guarantee. Continue to use the legacy `dev`
image for the broader established game and development-RBF paths below.

### Optional Pong, SNES and NES image cores

For the FES four-system profile, keep the same selected runtime checkout and
Mega Drive selection, and supply three sealed misteross bundles:

```sh
export NATIVE_RUNTIME_SYSTEMS='megadrive pong snes nes'
export PONG_RBF_BUNDLE=/absolute/path/to/pong-bundle
export SNES_RBF_BUNDLE=/absolute/path/to/snes-bundle
export NES_RBF_BUNDLE=/absolute/path/to/nes-bundle
make target-image-native
make target-image-native-verify
make target-image-native-qemu-smoke
```

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

### Native Mega Drive RBF selection

Source-built Mega Drive selection is the native image default; use the explicit upstream selection for fallback.

For the default source-built path, provide a sealed bundle containing exactly
`megadrive.rbf` and `megadrive-rbf.toml`:

- `make target-image-native MEGADRIVE_RBF_BUNDLE=/absolute/sealed/bundle`

To exercise the locked upstream fallback explicitly:

- `make target-image-native MEGADRIVE_RBF_SOURCE=upstream`

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

Use the full two-pass `target-images` or `target-image-native` build after a
change has stabilized and immediately before a major PR, merge, release, or
formal hardware acceptance. The full build is also required for any change to
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

- Host: `powerboat`
- MiSTer Pi: `192.168.10.239`, SSH `root` / `1`
- ROM share: `//DEANO-CLAWZ/Games`
- ROM directory inside the share: `Games`
- Powerboat mount: `/mnt/fogcast-games` (read-only, configured locally)
- Host config: `~/.config/fogcast/config.toml` (untracked, mode `0600`)
- Host API: `http://127.0.0.1:8787`
- Target API: `http://192.168.10.239:8182`

The target boots `/media/fat/linux/linux.img`. Its boot scripts start the
MiSTer/Main-compatible process and then the FAT-side agent using
`/media/fat/fogcast/agent.toml` and `/media/fat/fogcast/mister-agent`. Before
Main starts, the legacy image sets `osd_timeout=0` and `video_off=0` in
`/media/fat/MiSTer.ini` so an unattended Menu remains visible over HDMI. The
first changed file is retained as `/media/fat/MiSTer.ini.fogcast-backup`.

The fixture uses the stock MiSTer login and changing SSH host keys after a
rebuild is expected. Rebooting, reflashing, or replacing the image is normal.

After deploying the `native-dev` image, run its bounded idle-lifecycle checks:

```sh
make target-native-smoke
```

This checks the target and host ready/idle state, exact native child
executables and installed build inputs, absence of conventional Main and its
command FIFO, idle Stop, and fresh idle after an explicit reboot with a changed
Linux boot ID. Each API and SSH call is bounded to five seconds by default;
set `FOGCAST_CALL_TIMEOUT` to a positive decimal no greater than 60 seconds
when the fixture needs a different per-call bound. It does not inspect HDMI
output and does not perform the
mandatory legacy-image rollback and real-game launch; both remain separate
physical acceptance steps.

## Deploy and exercise the kit

Deploy an image and request a reboot:

```sh
make target-image-deploy TARGET_IMAGE=build/output/target-image/dev/linux.img
```

Run a real catalog launch through the same host API used by the browser:

```sh
make target-smoke GAME_ID=YOUR_GAME_ID EXPECTED_CORE=YOUR_CORE_NAME
```

The smoke command checks host and target health, calls
`POST /api/v1/session/launch`, polls `/tmp/CORENAME`, calls
`POST /api/v1/session/stop`, and waits for `MENU`. Direct equivalents are:

```sh
curl --get --data-urlencode 'q=Sonic the Hedgehog 2' \
  http://127.0.0.1:8787/api/v1/games
curl -H 'Content-Type: application/json' \
  --data '{"game_id":"GAME_ID_FROM_QUERY"}' \
  http://127.0.0.1:8787/api/v1/session/launch
curl -X POST http://127.0.0.1:8787/api/v1/session/stop
```

Load a locally built development RBF through the host API:

```sh
curl --fail -H 'Content-Type: application/octet-stream' \
  --data-binary @/absolute/path/to/top.rbf \
  http://127.0.0.1:8787/api/v1/session/development-rbf
curl --fail http://127.0.0.1:8787/api/v1/session
curl --fail -X POST http://127.0.0.1:8787/api/v1/session/stop
```

The active response is `{"state":"active","execution":"fpga_development"}`.
Stop may take roughly one target boot cycle. For a non-MiSTer RBF it first
receives `reboot_required` from the target, requests the reboot separately,
and waits for health to report a new Linux boot ID plus Menu idle before
returning `{"state":"idle"}`. Do not treat a sampled disconnect or the stale
`/tmp/CORENAME` left by an incompatible core as recovery evidence.

When target access is needed directly:

```sh
sshpass -p 1 ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null root@192.168.10.239 \
  'cat /tmp/CORENAME; curl --version'
curl --fail http://192.168.10.239:8182/v1/health
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
