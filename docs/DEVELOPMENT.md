# FogCast development

## Local checks

FogCast uses Go for the host and target services and dependency-free
JavaScript tests for the browser UI.

```sh
go test ./...
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
go vet ./...
git diff --check
```

The browser integration suite can be run separately with:

```sh
node --test internal/hostapi/ui_browser_test.js
```

## Builds

Build the normal project binaries with:

```sh
make build
```

Important outputs include the host/API programs, CLI and hardware test tools,
remote-media helpers, and the ARMv7 target agent. Build only the target agent
with:

```sh
make build-agent
```

The resulting target binary is:

```text
bin/mister-agent-linux-armv7
```

## Dedicated fixture

The working local path is deliberately small and concrete:

- Host: `powerboat`
- MiSTer Pi: `192.168.10.239`, SSH `root` / `1`
- ROM share: `//DEANO-CLAWZ/Games`
- ROM directory inside that share: `Games`
- Powerboat mount: `/mnt/fogcast-games` (read-only, configured in `/etc/fstab`)
- Host config: `~/.config/fogcast/config.toml` (untracked, mode `0600`)
- Host API: `http://127.0.0.1:8787`, run by the `fogcast-api.service`
  user service

The MiSTer boots `/media/fat/linux/linux.img`. Its image services start the
resident `/media/fat/MiSTer` process and then the FAT-side FogCast agent. No
`user-startup.sh`, fpgadev recovery helper, tunnel, or manual agent command is
part of the working boot path.

Build the disposable development image on powerboat after fetching the pinned
inputs:

```sh
make poc1b-fetch
make poc1b-image-fetch
POC1B_CONTAINER_RUNTIME=docker scripts/build-poc1b-image.sh --fast-dev
```

The output is `build/output/poc1b/dev/linux.img`. The dev image includes SSH
and curl; the production image intentionally does not include curl. Video
capture and decoding tools run on the host, not in the target image.

After a target reboot, the useful smoke checks are:

```sh
sshpass -p 1 ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null root@192.168.10.239 \
  'cat /tmp/CORENAME; curl --version'
curl --fail http://192.168.10.239:8182/v1/health
```

## Host catalog and launch API

Scan the configured mounted libraries and restart the local API with:

```sh
fogcast --config ~/.config/fogcast/config.toml scan
systemctl --user restart fogcast-api.service
curl --fail http://127.0.0.1:8787/api/v1/health
```

The browser and command-line tests use the same launch endpoint. Query a game
ID, launch it, inspect the session, and return to the menu with:

```sh
curl --get --data-urlencode 'q=Sonic the Hedgehog 2' \
  http://127.0.0.1:8787/api/v1/games
curl -H 'Content-Type: application/json' \
  --data '{"game_id":"GAME_ID_FROM_QUERY"}' \
  http://127.0.0.1:8787/api/v1/session/launch
curl http://127.0.0.1:8787/api/v1/session
curl -X POST http://127.0.0.1:8787/api/v1/session/stop
```

## Private configuration

Service tokens and private media configuration stay in untracked local
configuration. The dedicated fixture address, stock MiSTer login, and ROM
mount belong in this document because they are required to operate the shared
disposable development kit.

The dedicated MiSTer Pi is disposable development hardware on a local
network. Its normal development login is `root` with password `1`, and its SSH
host key may change after a rebuild. Rebooting, reflashing, or replacing its
image is acceptable. Do not build production security, rollback, or failover
systems around this fixture.

## Hardware changes

Before changing target behavior, trace the relevant path from
`docs/ARCHITECTURE.md` into the named source files. Test host-only behavior
locally, then run the actual operation on the designated MiSTer Pi when the
feature touches FPGA loading, input, video, audio, or target lifecycle.

The current development-RBF goal will reuse `/dev/MiSTer_cmd` and the resident
Main-compatible process. A development core may make Main exit or leave the
screen unusable; a target reboot is an acceptable recovery during this work.
