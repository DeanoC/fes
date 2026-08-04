# FogCast POC 2 deployment, recovery, and acceptance

This runbook is for the dedicated MiSTer Pi development kit only. Do not use
the SuperStation One or another production target. Every value that identifies
the target, source roots, game IDs, or bearer token is supplied locally by the
operator and must stay out of shell history, reports, and Git.

Current target note: the MiSTer Pi SD card is exFAT. Linux `renameat2` with
`RENAME_NOREPLACE` is unavailable on that filesystem, so the target agent uses
an existence-checked ordinary rename fallback for cache publication. Do not
replace that fallback with an unconditional overwrite.

## Safety boundary

The POC 2 HIL runner is operator-assisted. `bin/fogcast-hil` never invokes SSH,
reboot, `kill`, `mount`, `umount`, or an upload-interruption command. It pauses
at each sabotage gate and proceeds only after the operator has performed and
confirmed the action. Do not proceed when a check fails; restore the accepted
POC 1B image first.

## 1. Baseline and recovery preparation

1. Identify the dedicated Pi by the local inventory procedure. Confirm HDMI,
   the accepted wired controller, wired Ethernet, and a spare SD card or
   verified full-card backup.
2. Start from a freshly verified POC 1B backup. Confirm the accepted Main/Menu,
   Mega Drive and SNES cores, controller map, and reproduced kernel hashes with
   the committed POC 2 provenance lock. Do not hand-edit any lock value.
3. Keep the offline restore command ready. In production it accepts exactly one
   directly mounted child of `/Volumes` (for example, an operator-supplied
   `/Volumes/POC2-MISTER` path); never pass `/`, a broad directory, or a link.
4. Record the local target connection and game IDs in an untracked local
   configuration. Do not copy that configuration into this repository.

Pre-hardware verification:

```sh
mise exec go@1.26.5 -- make check build
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- \
  go build ./cmd/fogcast ./cmd/fogcast-hil ./cmd/misterctl ./cmd/mister-hil
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- \
  go build -o /dev/null ./cmd/mister-agent
POC1B_CONTAINER_RUNTIME=docker mise exec go@1.26.5 -- \
  make poc1b-verify-images poc1b-qemu-smoke poc1b-verify-kernel
```

Review the POC 2 lock and all verifier output before deployment. The QEMU smoke
test is software evidence only; it is not a MiSTer hardware acceptance.

## 2. Build and deploy the development root

Build both POC 2 roots from the locked POC 1B inputs, verify them, and retain
the accepted kernel, Main/Menu, core, and controller hashes. Deploy only the
development root through the existing hash-gated wrapper:

```sh
mise exec go@1.26.5 -- make poc1b-images poc1b-verify-images poc1b-qemu-smoke
bin/poc2-lock verify \
  --lock build/outputs.poc2.lock.toml \
  --poc1a-lock build/sources.poc1a.lock.toml \
  --poc1b-lock build/sources.poc1b.lock.toml \
  --prod build/output/poc1b/prod/linux.img \
  --dev build/output/poc1b/dev/linux.img
MISTER_TARGET=root@OPERATOR_TARGET scripts/install-poc2.sh
```

`MISTER_TARGET` is a local shell value and must never be written into a
committed document or report. The wrapper and target installer validate the
complete provenance chain, keep one POC 1B root backup, and replace only the
locked development root. They do not alter the kernel, cores, controller map,
user data, or FogCast cache.

For routine application development, prefer the fast path before using the
full release build:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 mise exec go@1.26.5 -- make build-agent
mise exec go@1.26.5 -- make poc1b-dev-image
```

The first command only rebuilds the static ARMv7 agent. The second builds only
the development root once and retains its Buildroot output/toolchain for the
next iteration. It is not sufficient for provenance recording or publication;
run the strict `poc1b-images` workflow before updating a lock.

If a full image build is unavailable but the static ARMv7 agent has changed,
do not copy a host binary into the image. Build with
`CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`, verify it with `file`, inject it
only into a disposable copy of the development image, record the resulting
image hash, and preserve `linux.img.pre-poc2`. This is a development recovery
technique, not a substitute for updating the reproducible image lock.

After installation, cold-boot the dedicated Pi manually and wait for health.
If it does not become ready, stop and use the recovery procedure below.

## 3. Local FogCast configuration and exact reset policy

Create a local configuration using the operator-supplied target origin, token,
and exactly two mounted source roots. Generic examples (replace locally; do
not commit the file) are:

```toml
base_url = "http://OPERATOR_TARGET:8182"
token = "OPERATOR_SUPPLIED_TOKEN"
request_timeout_seconds = 12
upload_timeout_seconds = 90

[[libraries]]
id = "operator-megadrive-root"
system = "megadrive"
root = "/operator/sources/megadrive"

[[libraries]]
id = "operator-snes-root"
system = "snes"
root = "/operator/sources/snes"
```

The canonical local paths are the FogCast index
`~/.local/share/fogcast/library.sqlite3` and staging directory
`~/.cache/fogcast/staging`. The target cache is the two-system tree beneath
`/media/fat/fogcast/cache`. Reset only those exact paths, and only after an
operator confirms the target is stopped and the source state is disposable:

```sh
rm -f ~/.local/share/fogcast/library.sqlite3 \
  ~/.local/share/fogcast/library.sqlite3-shm \
  ~/.local/share/fogcast/library.sqlite3-wal
rm -rf ~/.cache/fogcast/staging
# On the target, remove only the exact FogCast cache children after explicit confirmation.
```

Never use a broad recursive path, delete the accepted root backup, or reset
cache state as an automatic HIL action. The HIL runner's empty-state check is a
human confirmation, not authorization to delete anything.

## 4. Hardware acceptance gates

Run the HIL command with explicit local IDs and keep its JSON report ignored:

```sh
bin/fogcast-hil --config /path/to/local/fogcast.toml \
  --sonic-id OPERATOR_SUPPLIED_MEGA_ID \
  --mario-id OPERATOR_SUPPLIED_SNES_ID \
  --uncached-id OPERATOR_SUPPLIED_UNCACHED_ID \
  --interrupted-id OPERATOR_SUPPLIED_INTERRUPTED_ID \
  --output artifacts/hil/poc2.json
```

Supply IDs from the local catalog: the uncached and interrupted fixtures must
be distinct ZIP entries that are available while their source roots are online;
the runner deliberately does not embed or infer these private fixture names.

The runner stops at the first failed check. Confirm these gates in order:

1. Local index and target cache are empty as intentionally prepared.
2. Both source roots scan and the requested IDs are present.
3. First transfer of the Mega Drive game completes and is launchable.
4. Mega Drive HDMI video, HDMI audio, controller, and playable screen are
   confirmed.
5. First transfer of the SNES game completes and is launchable.
6. SNES HDMI video, HDMI audio, controller, and playable screen are confirmed.
7. Repeating both launches is a zero-upload cache hit.
8. After an operator-confirmed target reboot, both cached launches work.
9. After an operator-confirmed NAS-root offline state, both cached launches
   work without the source roots.
10. An uncached offline request is rejected and the active game state remains
    unchanged.
11. An operator-confirmed interrupted upload leaves no launchable content and
    does not replace the current game. This gate is currently open in the
    dedicated run because the available fixtures complete before manual
    interruption; use a throttled or substantially larger fixture before
    declaring it passed.
12. Alternating launches between the two cached games work.
13. Stop reaches idle and HDMI is black after the configured blanking delay;
    invalid requests are rejected without disturbing state.
14. Agent restart reconciliation, target power-cycle confirmation, the
    unchanged POC 1 regression HIL, and the private-artifact audit are reviewed
    and recorded. Keep any open gate visible in the handoff; do not convert a
    partial hardware report into a pass by editing its JSON.

Do not record ROM bytes, source paths, bearer tokens, or live addresses in the
report. The report is local, mode `0600`, atomically published, and refused when
the output path is a symbolic link.

## 5. Recovery

If health, cache integrity, or any HIL check fails, stop using the target. Power
it off, remove the SD card, and mount only the directly supplied operator path
beneath `/Volumes`. Verify the expected POC 2 checkpoint state and one-time
backup, then run:

```sh
scripts/restore-poc1b-sd.sh /Volumes/OPERATOR_MOUNT
```

The restore script verifies the immutable POC 1B root, reproduced kernel, and
lock identities, copies the backup through a same-volume temporary file, and
renames it atomically. It does not delete FogCast cache data or the backup.
Eject the card normally, boot the Pi, and manually confirm stock menu, HDMI,
controller, and both games before any further work.

For an online development deployment, the target's cache and checkpoint files
may be inspected over SSH, but the SD card remains the rollback authority.
Never delete `linux.img.pre-poc2`, `linux.img.pre-poc1b`, the accepted kernel,
or the core/controller artifacts while diagnosing an agent iteration.

## 6. Final audit

Before committing implementation changes, run the complete pre-hardware gate
from the task brief, review every `rg` match, and confirm that only documented
generic examples or extension allowlists match. Never commit `artifacts/hil/`.
Physical reboot, mount/unmount, deploy, and all hardware confirmations remain
operator actions and are not claimed by pre-hardware automation.
