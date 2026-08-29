# Linux Mailbox Development

`020_linux_mailbox` is the first HPS-facing experiment in this repository. It
uses one Cyclone V HPS general-purpose GPI/GPO primitive to return the exact
payload `OSS FPGA OK\n` to the private FogCast development loader. It does not
drive an LED, HDMI, SDRAM, audio, or another external FPGA output.

This document covers the build and host transport. The recorded M2 evidence is
**Software-tested**: simulation, OSS and Quartus compilation, semantic
comparison, bundle construction, and dry-run transport have passed. No command
in the software handoff loaded an RBF, rebooted a target, or performed HIL.

## Build and compare

Bootstrap the pinned open toolchain once, then build the mailbox in each lane:

```bash
make toolchain-check
make toolchain
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
QUARTUS_ROOTDIR=/path/to/17.0 make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
```

The OSS gate requires a routed 50 MHz design, exactly one
`cyclonev_hps_interface_mpu_general_purpose`, and zero forbidden hard blocks.
The oracle must be Quartus Prime Lite `17.0.2 Build 602`. Comparison requires
matching target, policy, protocol source, clock intent, resource semantics, and
passing timing. OSS and Quartus RBF bytes are not expected to match.

Source validation uses fixed forbidden tokens, an exact HPS-GP identifier
count, manifest source hashes, and both compilers' resource reports. It does
not attempt to parse the Verilog language independently.

Generated files stay below ignored build paths:

```text
build/oss/020_linux_mailbox/
build/oracle/020_linux_mailbox/
build/compare/020_linux_mailbox/
```

## Construct a reviewed bundle

Bundle generation requires a clean source worktree and a 32-character
lowercase hexadecimal run ID:

```bash
make dev-bundle \
  EXP=020_linux_mailbox \
  BUILD=oss \
  RUN_ID=0123456789abcdef0123456789abcdef
```

Select `BUILD=oracle` only when intentionally exercising the oracle artifact.
The output directory contains exactly four private regular files:

```text
build/dev-bundle/<lane>/020_linux_mailbox/manifest.json
build/dev-bundle/<lane>/020_linux_mailbox/resource_evidence.json
build/dev-bundle/<lane>/020_linux_mailbox/top.rbf
build/dev-bundle/<lane>/020_linux_mailbox/bundle.sha256
```

The compact manifest binds the run, source commit, lane, target, artifact size,
and artifact hash. Resource evidence is derived independently from the selected
lane's authenticated synthesis report. `bundle.sha256` has sorted membership
for the other three files. Rebuilding replaces the complete directory
atomically; it never publishes a partially updated bundle.

## Configure the host transport

The FogCast transport is intentionally separate from `make program`. Export
the deployment-specific values in the invoking shell; do not put credentials,
target identities, or private paths in the repository:

```bash
export FOGCAST_DEV_HOST=<approved-host>
export FOGCAST_DEV_USER=<approved-user>
export FOGCAST_DEV_EXPECTED_BOARD=misterpi
export FOGCAST_DEV_EXPECTED_MAIN_SHA256=<64-lower-hex>
export FOGCAST_DEV_TOOL_SHA256=<64-lower-hex>
export FOGCAST_DEV_SSH=/absolute/path/to/ssh
export FOGCAST_DEV_SCP=/absolute/path/to/scp
export FOGCAST_DEV_DRY_RUN=1
```

`FOGCAST_DEV_DRY_RUN` accepts only `0`, `1`, `false`, or `true`; unset defaults
to the safe dry-run mode. The client paths must resolve to safe executable
regular files. SSH uses strict host-key checking, argv-list subprocesses, and
fixed remote scripts. Operator values are never interpolated into a remote
shell program.

## Dry-run first

Run every selected action with the exact bundle run ID before considering live
use:

```bash
make dev-preflight EXP=020_linux_mailbox BUILD=oss RUN_ID=<run-id>
make dev-load EXP=020_linux_mailbox BUILD=oss RUN_ID=<run-id>
make dev-fault-inject EXP=020_linux_mailbox BUILD=oss RUN_ID=<run-id>
```

Dry-run prints one `FOGCAST_DEV_DRY_RUN argv=...` JSON line per subprocess and
does not open a network connection or create a run trace. Inspect the printed
commands and confirm the selected host, lane, run ID, bundle members, direct
remote `exec`, and fixed paths.

## Live preflight and load boundary

Live use requires separate operator authorization and an installed, reviewed
FogCast development profile. Set `FOGCAST_DEV_DRY_RUN=0` only for that approved
session.

Preflight attests the exact board, `/media/fat/MiSTer`, and
`/usr/bin/mister-fpga-dev`; creates one root-private staging directory; uploads
and re-hashes all four bundle members; invokes the exact command below; and
always removes the disposable stage:

```text
exec mister-fpga-dev preflight \
  --manifest /tmp/misteross-fpgadev-<run-id>/manifest.json \
  --artifact /tmp/misteross-fpgadev-<run-id>/top.rbf
```

Only exit zero, empty stderr, and exact stdout
`FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n` pass.

Load performs the same stage checks and invokes:

```text
exec mister-fpga-dev run \
  --manifest /tmp/misteross-fpgadev-<run-id>/manifest.json \
  --artifact /tmp/misteross-fpgadev-<run-id>/top.rbf
```

The expected reboot disconnect is provisional, never success by itself. The
host allows at most 120 seconds for reconnection, then one cumulative 30-second
window for target attestation, protected ready-v3 state, live Main/FIFO/FPGA
and two stable `MENU` observations, result retrieval, durable host storage,
remote result deletion, and a final exact preflight. Recovery must use a new
session and a strictly greater owner generation. A missing result, missing
disconnect, failed recovery request, stale identity, or failed readiness fails
the load.

## Deterministic fault cycle

`dev-fault-inject` is a recovery test, not an alternate load path. It stages
and arms one run, owns the direct-`exec` SSH child, polls exact inspection for
at most 10 seconds, invokes `fault-kill`, and reaps the original SSH process.
It then requests the authorized recovery reboot and applies the normal
120-second reconnect and 30-second readiness bounds.

Success additionally requires no result and no remaining stage, diagnostic,
armed hook, or fault socket. The fault path preserves private host traces and
does not clean an unresolved target fence. A timeout or identity mismatch
terminates and reaps the local child without signaling an unrelated process.

## Rollback and recovery

- Host-only dry-run changes no target state; remove ignored generated build or
  trace directories if they are no longer needed.
- A completed volatile load is cleared by the FogCast recovery reboot; it does
  not flash the FPGA or write an RBF to persistent target storage.
- If a live run fails after intent, preserve its run ID and host trace. Do not
  reuse the run ID or bypass the owner fence. Use the reviewed FogCast
  `inspect` and recovery workflow.
- Removal of the target development profile belongs to the FogCast install
  manager and the separately authorized integration procedure, not this
  repository's build commands.

The next safe action after this software handoff is the cross-repository
integration plan. It must bind the reviewed `misteross` and FogCast commits,
repeat dry-run checks, obtain explicit target authorization, and only then run
the limited HIL cycles.
