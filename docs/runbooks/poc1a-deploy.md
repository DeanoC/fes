# POC 1A Deployment and Recovery

This procedure is only for the dedicated MiSTer Pi development kit. The SuperStation One must remain untouched. Packages contain the daemon and configuration only; ROMs, RBF cores, controller maps, and the stock MiSTer executable are never packaged by this repository.

## Prepare the stock system

1. Confirm the complete MiSTer Pi setup: 128 MB SDRAM, SD card, wired Ethernet, HDMI, and one wired Xbox-compatible USB controller.
2. Update the stock MiSTer installation, reboot it, and manually confirm that its menu, network, HDMI video/audio, and controller work before installing the daemon.
3. Confirm there is exactly one `/media/fat/_Console/MegaDrive_*.rbf` and exactly one `/media/fat/_Console/SNES_*.rbf`. Resolve duplicates before continuing.
4. Complete the normal MiSTer controller-mapping workflow. Current stock images store maps under `/media/fat/config/inputs`; older images used `/media/fat/config`. Launch each selected game once from the stock menu and confirm video, audio, a playable screen, and controller input.
5. Place your lawful test copies directly on the MiSTer SD card at `/media/fat/games/MegaDrive/test.md` and `/media/fat/games/SNES/test.sfc`. Do not copy either game into this repository.

## Package and install

From the repository on the MacBook, generate a new local token and keep it out of shell history where practical:

```sh
MISTER_TOKEN=$(openssl rand -base64 48 | tr -d '=+/' | cut -c1-48)
export MISTER_TOKEN
VERSION=0.1.0 mise exec go@1.26.5 -- make package-poc1a
shasum -a 256 -c dist/mister-remote-poc1a-0.1.0.tar.gz.sha256
MISTER_TARGET=root@MISTER_IP scripts/install-poc1a.sh dist/mister-remote-poc1a-0.1.0.tar.gz
```

The installer verifies the checksum, makes one-time backups of `user-startup.sh` and `MiSTer.ini`, installs under `/media/fat/mister-remote`, and starts the supervised daemon without rebooting.

`/media/fat` is FAT, so the running MiSTer synthesizes Unix mode bits and cannot enforce `0600` on `agent.toml` even though the package metadata is normalized. Treat the token as a secret, require authentication for Samba and SSH, and verify that guest Samba access is rejected before accepting a network-enabled installation.

POC 1A keeps `fb_terminal=1` because current `Main_MiSTer` gates its Menu inactivity and `video_off` state machine on that setting. With `osd_timeout=5` and `video_off=1`, allow about 11 seconds after returning to Menu before expecting a black HDMI frame.

Create `~/.config/mister-remote/config.toml` locally, using the same token and the MiSTer IP:

```toml
base_url = "http://MISTER_IP:8182"
token = "REPLACE_WITH_THE_SAME_TOKEN"
request_timeout_seconds = 12
manifest_path = "games.toml"
```

Create `~/.config/mister-remote/games.toml` with the two preloaded paths:

```toml
[[games]]
id = "megadrive-test"
title = "Mega Drive test game"
system = "megadrive"
rom_path = "/media/fat/games/MegaDrive/test.md"

[[games]]
id = "snes-test"
title = "SNES test game"
system = "snes"
rom_path = "/media/fat/games/SNES/test.sfc"
```

Check the initial control path:

```sh
bin/misterctl health
bin/misterctl games
bin/misterctl launch megadrive-test
bin/misterctl launch snes-test
bin/misterctl stop
```

Run the operator-assisted acceptance sequence and then capture the accepted target inventory:

```sh
bin/mister-hil --output artifacts/hil/poc1a.json
MISTER_TARGET=root@MISTER_IP scripts/capture-poc1a-lock.sh
MISTER_TARGET=root@MISTER_IP scripts/verify-poc1a-lock.sh
```

Keep the generated hardware report and `build/sources.poc1a.lock.toml` local until they have been reviewed for machine-specific data. The verification command must remain clean before any POC 1B image work begins.

## Recover the stock installation

SSH to the dedicated MiSTer Pi and stop the supervisor and daemon. Restore only backups created by this installer:

```sh
kill "$(cat /tmp/mister-agent-supervisor.pid)" 2>/dev/null || true
killall mister-agent 2>/dev/null || true
test ! -f /media/fat/linux/user-startup.sh.pre-mister-remote || mv /media/fat/linux/user-startup.sh.pre-mister-remote /media/fat/linux/user-startup.sh
test ! -f /media/fat/MiSTer.ini.pre-mister-remote || mv /media/fat/MiSTer.ini.pre-mister-remote /media/fat/MiSTer.ini
rm -r /media/fat/mister-remote
reboot
```

After reboot, manually verify the stock menu and both games. If ordinary restoration fails, reflash the dedicated MiSTer Pi SD card from a known-good stock image. Do not use the SuperStation One as a recovery target.
