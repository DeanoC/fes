# Working in FES

The four first-party modules develop only in this repository. Their former
standalone repositories are archived; never send work or PRs there. Go import
paths and dated provenance may keep the old names.

## Start every task here

1. Read [README.md](README.md) and [the project map](docs/project-map.md);
   add [core status](docs/core-status.md) when a described core is involved.
2. Pick the owner below, state the change and its bounded scope, and preserve
   unrelated work. The user's task defines the scope.
3. For component edits, read that component's `AGENTS.md`, `README.md` and the
   architecture section for the path you change, then trace that path.
4. Commands are in [the development guide](docs/development.md). Resolve stale
   prose against the code and [current boundaries](docs/component-boundaries.md).
   Plans and dated validation records are context, not instructions
   ([documentation index](docs/README.md)).

## Choose the owner

| Work | Owner |
| --- | --- |
| Component selection, compatibility, orchestration, image assembly (`image/`) and integration evidence | FES |
| UI, game library, host services, network-facing target agent | FogCast |
| FPGA programming, physical lifecycle, media/input delivery and hardware recovery | libmister-runtime |
| FPGA source builds, RBF provenance, simulations and compiler recipes | misteross (`sources/misteross`) |
| Shared board/protocol/source definitions and their schema/emitter | mister-packages |

Physical transitions stay in the runtime; network/session coordination stays in
the FogCast agent. Main_MiSTer is a comparison reference, not a dependency. Core
settings and progress follow the [core persistence guide](docs/core-persistence.md);
never infer persistence from a display name, package path or raw RBF.

## Change and validate

- One task, one FES worktree under `out/dev/<task>/fes` and one branch, covering
  every affected module. Agree file ownership before parallel edits.
- A shared-definition change regenerates its consumers in the same change
  (`make generate`, then `make check`).
- Use the component's focused tests while developing, then `make test-changed`
  and parent checks. `make dev` is the incremental native integration build;
  keep cold `make build` / `make verify` and Quartus for stabilized integration.
- Parent builds use committed module bytes. Never claim a build tested edits it
  did not select.

## Hardware, media and releases

- A build never authorizes deployment. Use the designated kit only as the
  selected FogCast `docs/DEVELOPMENT.md` and `AGENTS.md` authorize, holding a
  session claimed through [kit sharing](docs/kit-sharing.md); never bypass
  another owner's lease with direct programming.
- Report which kind of evidence you have: host-only check, hardware diagnostic,
  or exact-artifact hardware acceptance. A new profile inherits no acceptance.
- `make media`, `make verify-media` and `make rollback-media` hold the media
  lease and never write a block device; see [bootable media](docs/bootable-media.md).
  Change `media/current` only through `make rollback-media`. Writing a physical
  card needs explicit device authorization plus the kit lease.
- Versioned appliance images follow [appliance releases](docs/appliance-releases.md):
  preserve factory, known-good, previous and loop-referenced images, and verify
  the booted image identity after reboot.

## Finish with a handoff

State scope, base and result commit (or uncommitted diff), tests and results,
hardware evidence class, effects on module/shared contracts, and the next
integration step. Commit, push and open PRs as needed without further approval.
Merging requires review by Deano or another agent/model reviewer; agents never
merge on their own authority.
