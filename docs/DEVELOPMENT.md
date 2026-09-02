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

Build and inspect the separate native-runtime candidate with a clean runtime
checkout at the commit pinned by `build/native-runtime.inputs.lock.toml`:

```sh
export LIBMISTER_RUNTIME_DIR=/absolute/path/to/libmister-runtime
make target-image-native
make target-image-native-verify
make target-image-native-qemu-smoke
```

This produces `build/output/target-image/native-dev/linux.img`. The build runs
twice and requires identical image digests. Verification inspects the locked
idle RBF, build-input record, ARM runtime and static ARM agent, and the
runtime's target-library closure. QEMU proves only the read-only root,
volatile mounts, and init packaging. Until Task 7 runs on the designated kit,
`native-dev` has no hardware acceptance, ready-state claim, supported game
systems, game launch, or development-RBF support. Continue to use the legacy
`dev` image for the working game and development-RBF paths below.

### Fast target iteration policy

During active runtime or target debugging, do not rebuild the complete image
for every source change. Run the narrow host tests, cross-build only the
self-contained changed runtime artifact, and place it in a disposable copy of
the last verified image. Deploy that derived image for a bounded diagnostic,
preserving the verified source image and recording the derived hash. Derived
image results are diagnostic only; they do not establish reproducibility,
release readiness, or hardware acceptance.

Use the full two-pass `target-images` or `target-image-native` build after a
change has stabilized and immediately before a major PR, merge, release, or
formal hardware acceptance. The full build is also required for any change to
Buildroot, init scripts, package contents, image configuration, or locked
inputs, because a runtime-only replacement cannot validate those changes.

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
