# FogCast POC

FogCast is a host-side library scanner and cache-aware launcher for the
dedicated MiSTer Pi development target. The repository contains the POC 1
control path, the POC 1B reproducible target image, and the POC 2 cache and
offline acceptance workflow.

## Local checks

Use the pinned toolchain for repository checks and builds:

```sh
mise exec go@1.26.5 -- make check build
```

The build produces `bin/fogcast`, `bin/misterctl`, `bin/mister-hil`,
`bin/fogcast-hil`, and the target ARMv7 agent. Build output, local catalogs,
staging content, and acceptance reports are ignored by Git.

## POC 2 acceptance

Read [the POC 2 deployment runbook](docs/runbooks/poc2-deploy.md) before
touching the dedicated target. The HIL command requires operator-supplied game
IDs and asks for confirmation before every reboot, process restart, share
change, or upload interruption:

```sh
bin/fogcast-hil --config /path/to/local/fogcast.toml \
  --sonic-id OPERATOR_SUPPLIED_MEGA_ID \
  --mario-id OPERATOR_SUPPLIED_SNES_ID \
  --uncached-id OPERATOR_SUPPLIED_UNCACHED_ID \
  --interrupted-id OPERATOR_SUPPLIED_INTERRUPTED_ID
```

No target address, bearer token, NAS path, ROM bytes, or game filename belongs
in this repository. Keep the generated report at the ignored default
`artifacts/hil/poc2.json` and review it locally only.
