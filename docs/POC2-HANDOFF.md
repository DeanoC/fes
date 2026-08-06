# FogCast POC2 handoff

This is the canonical starting point for a fresh agent. FogCast is a Mac-hosted
library scanner and cache-aware launcher for a dedicated MiSTer Pi. POC2 keeps
the Pi deliberately appliance-like: the Mac owns discovery and ROM preparation;
the Pi owns bounded content caching and launch orchestration; Main_MiSTer owns
FPGA cores, HDMI, audio, controller input, and saves.

## What is complete

POC1/1B proved Mega Drive and SNES launch, HDMI video/audio, wired controller
playability, the reproducible ARMv7 image, read-only root, and rollback. POC2
adds:

- NAS-backed Mac catalog and ZIP member preparation;
- authenticated `/v2/cache/{system}/{sha256}` and `/v2/launch` APIs;
- persistent Mega Drive/SNES cache under `/media/fat/fogcast/cache`;
- cache hits across reboot and while NAS roots are offline;
- offline rejection without disturbing the active game;
- `fogcast` CLI and operator-assisted `fogcast-hil` acceptance workflow.

The dedicated MiSTer Pi has been exercised with Sonic the Hedgehog and Super
Mario World. The target is on an exFAT SD filesystem; the Linux agent includes
an exFAT-compatible publication fallback for filesystems without
`renameat2(RENAME_NOREPLACE)`.

POC2 implementation and the primary managed-target acceptance path are ready.
The HIL runner now allows 120 seconds for post-reboot readiness because the
development kit's exFAT remount, init ordering, command-pipe creation,
supervisor startup, and tunnel re-establishment can exceed the former
45-second budget. The latest authoritative run still predates that runner fix
and must not be treated as the final report.

The remaining formal POC2 gate is deterministic interrupted-upload testing.
Run the authoritative HIL workflow with its throttled upload option and retain
the generated report locally; do not claim the gate passed until the report
records transfer failure, no launchable partial content, and active-state
preservation.

## Repository map

- `cmd/fogcast`: host CLI.
- `cmd/fogcast-hil`: operator-assisted POC2 acceptance.
- `cmd/mister-agent`: target HTTP appliance service.
- `internal/targetcache`: bounded persistent cache and publication safety.
- `deploy/poc2/install-target.sh`: hash-gated target installer and checkpoint.
- `docs/runbooks/poc2-deploy.md`: deployment/recovery procedure.
- `docs/POC3-ROADMAP.md`: next-stage scope.

Historical POC1 plans and reports remain historical; do not rewrite them to
reflect POC2 behavior.

## Local development

Use the pinned Go toolchain:

```sh
mise exec go@1.26.5 -- go test ./...
mise exec go@1.26.5 -- make check build
```

For normal Linux development, do not use `poc1b-images` for every iteration.
That target is the strict provenance path: it builds both production and
development images twice in the locked `linux/amd64` container. Instead use:

```sh
# Static ARMv7 agent-only iteration
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  mise exec go@1.26.5 -- make build-agent

# Development rootfs iteration; one persistent Buildroot output and one build
mise exec go@1.26.5 -- make poc1b-dev-image
```

The fast development image is intentionally not reproducibility evidence. It
uses the development Buildroot output directory as a persistent cache and
publishes `build/output/poc1b/dev/linux.img`. Use `poc1b-images`, the image and
kernel verifiers, and the QEMU smoke test before recording or publishing a
new lock. The fast target does not alter the strict release target.

The full suite requires local networking for `httptest` integration tests. If
the sandbox blocks loopback listeners, run those tests in the normal developer
environment and report the restriction rather than changing the tests.

## Operator configuration

Keep the target origin, bearer token, NAS mounts, and game IDs in an untracked
local config. The repository must never contain those values. The expected
shape is documented in the deployment runbook. The operator's current NAS
roots are the SNES and Genesis directories on the private Games share; use
local mount paths, not SMB URLs, in FogCast config.

Useful CLI commands:

```sh
bin/fogcast --config /path/to/fogcast.toml scan
bin/fogcast --config /path/to/fogcast.toml games
bin/fogcast --config /path/to/fogcast.toml health
bin/fogcast --config /path/to/fogcast.toml launch OPERATOR_GAME_ID
bin/fogcast --config /path/to/fogcast.toml stop
```

## Target workflow

Read `docs/runbooks/poc2-deploy.md` before touching the Pi. In particular:

1. verify the accepted POC1B provenance and image inputs;
2. keep the one-time `linux.img.pre-poc2` rollback copy;
3. deploy only through the hash-gated installer when the image lock matches;
4. after every reboot, re-establish the local SSH tunnel before a HIL deadline;
5. reset only the documented local index/staging and target cache children.

For a development-only agent iteration where a full Buildroot image is not
available, use the runbook's isolated image-injection procedure and record the
new image hash. Never overwrite the accepted backup or kernel.

## Handoff checklist

- [ ] Read this file and the POC2 deployment runbook.
- [ ] Confirm `git status` and preserve unrelated work.
- [ ] Confirm the target is the dedicated MiSTer Pi, not the SuperStation One.
- [ ] Verify the local config is untracked and contains no repository path.
- [ ] Run targeted cache/API tests before hardware work.
- [ ] Run the authoritative HIL workflow with the throttled interruption
      fixture and retain a passing local report.
- [ ] Keep POC3 work separate from POC2 acceptance and rollback changes.
