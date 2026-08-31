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
`/media/fat/fogcast/agent.toml` and `/media/fat/fogcast/mister-agent`.

The fixture uses the stock MiSTer login and changing SSH host keys after a
rebuild is expected. Rebooting, reflashing, or replacing the image is normal.

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
