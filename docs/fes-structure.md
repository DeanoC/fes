# FES structure: next migration

Status: proposed implementation sequence, inspected at FES `4d3983d` and
FogCast `b1488d1` on 2026-09-15. This document does not claim the code has moved.

## Decision

FES owns the appliance platform; FogCast is its host application and client
suite. Put appliance boot software beside FES image assembly, and make shared
appliance storage and manifests usable by both boot software and the target
agent. Keep the existing four component repositories through this migration.
Use Go modules to establish the new build boundary inside the existing repos.

The first implementation slice extracts the shared appliance package into a
standalone Go module within FogCast. The following slice moves the boot
executable and its private helpers into FES. This ordering gives the existing
agent and the future FES boot binary one storage implementation, with no
FogCast-to-FES dependency cycle.

## What is already done

- FES `image/` owns Buildroot and system image assembly.
- FogCast UI packages live under `ui/tenfoot` and `ui/kitlauncher`.
- Host-to-target transport lives in `targetclient`.
- FES incremental builds retain compiler/base work; its normal FPGA producers
  use HIP/nextpnr for Pong, ZX81 and Coleco.
- The three-system target runner is merged. On 2026-09-15 the deployed
  development image passed its API/media/input/Stop checks and the operator
  reported seeing all three systems on the physical display. This is bounded
  display evidence, not exhaustive controller, audio or emulation coverage.

These completed changes are the baseline, not work to repeat.

## Source ownership and destination

| Responsibility | Current source | Proposed destination |
| --- | --- | --- |
| Image, media, release assembly | FES `image/`, `scripts/appliance*.py` | Remain in FES |
| Shared release schema | FogCast `appliance/release.go` | FogCast `appliance/`, standalone Go module |
| Immutable image store | FogCast `internal/appliance` | `appliance/store` in that shared module |
| Boot command | FogCast `cmd/fes-boot` | FES `platform/cmd/fes-boot` |
| Boot policy and Linux mechanisms | FogCast `internal/applianceboot`, `internal/bootlinux` | FES `platform/internal/applianceboot`, `platform/internal/bootlinux` |
| Network update admission | FogCast `internal/applianceupdate` | Remain with the target agent |
| Operator update client | FogCast `cmd/fes-update` | Remain with host clients initially |
| Target HTTP/cache/session coordination | FogCast `internal/agent`, `internal/httpapi` | Explicit target service boundary; separate extraction review later |
| Host/library/services and browser API | FogCast `host`, `fogcast`, `catalog`, `internal/hostapi` | Remain in FogCast |
| UI strategies | FogCast `ui/tenfoot`, `ui/kitlauncher`, browser assets | Remain clients of host/transport contracts |
| Physical hardware lifecycle | libmister-runtime | Remain library and daemon together |
| FPGA recipes and sources | misteross | Remain one builder component |
| Board/ABI definitions and generation | mister-packages | Remain shared definition authority |

Proposed layout after the first two implementation slices:

```text
fes/
  platform/                 Go module github.com/DeanoC/fes/platform
    cmd/fes-boot/
    internal/applianceboot/
    internal/bootlinux/
  image/
  scripts/
  sources/FogCast/
    appliance/              Go module github.com/DeanoC/FogCast/appliance
      release.go
      store/
    cmd/mister-agent/
    cmd/fes-update/
    internal/applianceupdate/
    host/ targetclient/ ui/
```

The shared module initially remains physically inside FogCast to keep its
standalone checkout buildable. Its ownership is the FES appliance contract,
not the game library. A new repository is justified only if independent
consumers/releases later need it. Directory placement and product ownership
are different decisions.

## Why this order

`cmd/fes-boot/main_linux.go` imports the release schema, image store, boot
policy and Linux helpers. The policy imports the schema and store; the Linux
helpers have no FogCast imports. This is a bounded boot dependency graph.

By contrast, `internal/applianceupdate.Service` directly holds an
`*agent.Coordinator` and uses its update-admission and idle checks.
`cmd/fes-update` imports `fogcast`, discovery and `targetclient`. Extracting
either alongside boot would broaden this change into live session handling.

The store and manifest must remain single implementations. Moving only their
boot copies into FES would let boot selection and network installation diverge.

## Build and contract rules

- Keep the existing manifest bytes, closed schema, boot ABI, filesystem layout,
  update admission and HTTP contracts throughout the shared-module slice.
- Pin the shared module for standalone consumers. A checked-in relative
  replacement within FogCast can support its nested module; FES must build
  against a selected immutable module revision and validate agreement with the
  selected FogCast tree. No developer worktree path belongs in published inputs.
- Run nested-module tests explicitly: root `go test ./...` skips them.
- After boot migration, image assembly builds boot from FES `platform/`.
  `scripts/appliance.py` and `scripts/appliance_media.py` currently build
  `./cmd/fes-boot` from FogCast and bind `binary_source_revision` to the factory
  manifest's `fogcast_revision`. Update both builders and evidence checks
  together; never relabel FES boot source as FogCast source.
- Boot receipts must cover FES platform source, selected shared-module content,
  Go toolchain and build flags. A module/lock change must invalidate boot output.
- Preserve release manifests as identities of the selected system components.
  Bootstrap evidence separately identifies the source of the boot binary.
  Specify compatibility with retained evidence before changing its format.
- Host/UI edits must not require rebuilding compilers or FPGA packages.
  Demonstrate changed-input invalidation and unchanged-input reuse for any new
  receipt; do not claim a speed improvement from directory moves alone.

## Reviewable sequence

1. **Shared appliance module:** move the image store beside the public schema,
   update imports, add explicit nested-module CI, and prove existing agent and
   boot behavior still passes. See the [implementation plan](superpowers/plans/2026-09-15-appliance-module.md).
2. **FES boot ownership:** move the boot command/helpers to `platform/`, update
   both parent builders, cache inputs, provenance and reconstruction tests.
   Build static ARM output and run isolated boot tests before any kit operation.
   Reproducible bootstrap assembly and designated-kit boot acceptance are
   separate gates for this slice; the current game display check does not
   qualify a replacement boot selector.
3. **Target service boundary:** inventory the full agent dependency closure,
   including kit launcher, input and update admission; decide on module/release
   independence using that evidence. Keep physical lifecycle in the runtime.
4. **UI/host consolidation:** assess duplicated models and presentation logic
   across browser, tenfoot and kit clients. Share transport/schema and genuinely
   common logic; retain rendering/input appropriate to each device.

One integrator owns component selection and cross-module contracts. A worker
can own the shared-module move while a reviewer checks import direction and
CI coverage. Boot integration follows that result; it is not a parallel edit
of the same dependency contract.
