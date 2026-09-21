# Working in FES

The four first-party modules develop only in this FES repository. Their former
standalone repositories are archived; never send feature work or PRs there.
Go import paths and dated provenance may retain the old names.

## Start every task here

1. Read [README.md](README.md) and [the project map](docs/project-map.md).
2. Identify the owning component using the table below. State the intended
   change and bounded scope; preserve unrelated work.
3. For component edits, read that component's `AGENTS.md`, `README.md` and current
   architecture, then trace the relevant execution path.
4. Use [the development guide](docs/development.md) for worktree/build commands
   and [the agent workflow](docs/agent-workflow.md) for assignments and handoffs.

The user's task defines the scope. Resolve stale component prose against the
selected code and [current boundaries](docs/component-boundaries.md). Earlier
plans and dated validation records are context, not current task instructions.
See [the documentation index](docs/README.md) to distinguish them.

## Choose the owner

| Work | Owner |
| --- | --- |
| Component selection, compatibility, orchestration, system assembly and integration evidence | FES |
| UI, game library, host services, network-facing target agent | FogCast |
| FPGA programming, physical lifecycle, media/input delivery and hardware recovery | libmister-runtime |
| FPGA source builds, RBF provenance, simulations and compiler recipes | misteross |
| Shared board/protocol/source definitions and their schema/emitter | mister-packages |

Main_MiSTer is a comparison reference, not a production dependency. Keep
physical transitions in the runtime and network/session coordination in the
FogCast agent. Image assembly lives in FES `image/`; FogCast supplies agent,
kit and selector inputs through `FOGCAST_DIR`. FES owns external artifact policy
in `image/build/native-inputs.toml` and derives runtime source selection itself.

For described-core settings or progress, read the [core persistence guide](docs/core-persistence.md).
Keep the data layout and wire contract in mister-packages, capture and durable
record handling in libmister-runtime, and library context and APIs in FogCast.
Development loads stay volatile; library loads explicitly bind persistent data.
Do not infer persistence from a display name, package path or raw RBF.

## Keep worker and integration checkouts separate

`sources/` contains ordinary tracked modules in this FES repository. Develop a
feature in one FES worktree under `out/dev/<task>/fes`, including every affected
module. Different tasks use different worktrees and branches; agree file
ownership before parallel edits. There are no first-party gitlinks to update.
Regenerate shared consumers in the same change. Preserve unrelated changes.

Parent builds use committed FES module bytes, not uncommitted worker changes.
Return an uncommitted diff when committing has not been authorized. Never claim
a parent build tested edits that have not been selected.

## Coordinate and validate

Delegate independent scopes when useful; a task does not require multiple
agents. Agree file/component ownership before concurrent edits. Use the
component's focused tests during development, then parent consistency checks.
Use `make dev` for incremental native integration; it retains unchanged compiler
and base packages and writes separate diagnostic outputs. Reserve cold
`make build` / `make verify` for stabilized integration. Do not have every
agent run Quartus or rebuild the full image. Parent builds already serialize
per checkout; coordinate a single operator for the designated kit during each
hardware test. Claim a session through
the [kit sharing guide](docs/kit-sharing.md); never bypass another owner with
direct programming. Expiry and explicit operator takeover handle abandoned
sessions. The lease lives in the existing target agent, not an extra service.

A build does not authorize deployment to an unknown device. Follow the exact
kit designation and authorization in the selected FogCast
`docs/DEVELOPMENT.md` and `AGENTS.md`. Distinguish host-only checks, hardware
diagnostics, and exact-artifact hardware acceptance; the new integration
profile does not inherit acceptance from a historical profile.

`make media` publishes a flashable file under the selected profile's `out/`
directory. `make verify-media` revalidates that disk and can publish refreshed
immutable evidence after two current-recipe assemblies match its bytes.
`make rollback-media GENERATION=<image-sha>/<evidence-sha>` validates and selects
a retained candidate under the same exclusive media lease; never change
`media/current` through an unleased shell rollback. All three commands keep the
lease through validation, selection, sync, and failure rollback, and none writes
a block device. Read [the bootable-media guide](docs/bootable-media.md)
before a separately authorized physical-card operation; that operation needs
the exact device authorization and the existing kit lease.

For a local card, `make media` automatically derives and embeds
`/fogcast/agent.toml` from the owner-only `~/.config/fogcast/config.toml` (or
`FES_HOST_CONFIG`). It copies only the target token and fixed MiSTer paths;
the manifest stores only the generated file's digest. Use
`make media FES_UNPROVISIONED=1` for an intentional credential-free image;
`CI=true` suppresses automatic discovery. An explicit `AGENT_CONFIG` remains
available for a nonstandard target.

## Versioned appliance images

For versioned native updates, read the [appliance release guide](docs/appliance-releases.md).
`make release` exports the verified rootfs with its closed manifest; `make bootstrap`
builds the fixed boot selector from selected FogCast sources. `make appliance-media`
and `make verify-appliance-media` generate/reconstruct private card files using the
media lease and automatic configuration provisioning. These commands never write
block devices. Kernel/U-Boot/bootstrap changes require media provisioning; ordinary
network updates change only the immutable system image. Use the existing kit lease
and verify actual boot/image identity after reboot. Preserve factory, known-good,
previous and any image still referenced by a loop device. Distinguish isolated
root-switch tests, watchdog diagnostics and exact-artifact hardware acceptance.

## Finish with a handoff

Handoff: state scope, base and result commit (or uncommitted diff), tests and
results, hardware classification, effects on module/shared contracts,
and the next integration step. Do not commit, push, or open a PR without user
authorization.
