# Working in FES

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
FogCast agent. Image assembly still uses the selected FogCast builder until
the separately scoped assembly migration is complete.

## Keep worker and integration checkouts separate

Keep `sources/` checkouts clean: they are pinned integration build inputs.
Develop component changes in separate worktrees under
`out/dev/<task>/<component>` using the commands in the development guide.
Different tasks touching one component need different worktrees and branches.
The integrator alone updates parent gitlinks and reconciles shared contracts;
component workers hand back revisions or diffs. Preserve unrelated changes.

Parent builds use selected component commits, not uncommitted worker changes.
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

## Finish with a handoff

Handoff: state scope, base and result commit (or uncommitted diff), tests and
results, hardware classification, effects on parent pins/shared contracts,
and the next integration step. Do not commit, push, or open a PR without user
authorization.
