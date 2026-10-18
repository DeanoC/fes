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

- One task, one FES worktree at `out/dev/<task>` and one branch, covering
  every affected module. Agree file ownership before parallel edits.
- A shared-definition change regenerates its consumers in the same change
  (`make generate`, then `make check`).
- Use the component's focused tests while developing, then `make test-changed`
  and parent checks. `make dev` is the incremental native integration build;
  keep cold `make build` / `make verify` and Quartus for stabilized integration.
- Parent builds use committed module bytes. Never claim a build tested edits it
  did not select.

## Hardware, media and releases

- Managed kits install immutable userspace bundles and queue activation at the next
  free lease boundary; see [kit software releases](docs/kit-software-releases.md).
  Successful selections remain installed. Do not restore another task's release,
  require matching host commits, or use file overlays on a managed kit.
- A build never authorizes deployment. Use the designated kit only as the
  selected FogCast `docs/DEVELOPMENT.md` and `AGENTS.md` authorize, holding a
  session claimed through [kit sharing](docs/kit-sharing.md); never bypass
  another owner's lease with direct programming.
- Report which kind of evidence you have: host-only check, hardware diagnostic,
  or exact-artifact hardware acceptance. A new profile inherits no acceptance.
- `make media`, `make verify-media` and `make rollback-media` hold the media
  lease and never write a block device; see [bootable media](docs/bootable-media.md).
  Change `media/current` only through these leased commands. Writing a physical
  card needs explicit device authorization plus the kit lease.
- Versioned appliance images follow [appliance releases](docs/appliance-releases.md):
  preserve factory, known-good, previous and loop-referenced images, and verify
  the booted image identity after reboot.

## Shared GPUs

Powerboat (AMD, HIP; GPU0 and GPU1) and the CUDA box `chip-30x0-ai-dev`
(RTX 3070 and RTX 3090) are shared by agents and by Deano's Codex harness.
All GPU work on those hosts goes through `gpu-run`. First come, first served.
Lander uses `--owner lander` and `GPU_LEASE_MAX_PER_OWNER=2`, so Lander holds
at most 2 GPUs at once. Nobody kills a GPU job. Deano's Codex harness jobs
wait their turn like everyone else.

`scripts/gpu-run` takes the lease and execs the command. The kernel drops the
flock when that process exits, including on crash. `scripts/gpu-status` shows
each GPU as free or held, the holder metadata and age, and the waiting queue.
Leases live in `/var/tmp/gpu-leases` unless `GPU_LEASE_DIR` is set.

Install on a host by symlinking the scripts into `~/bin` (the link target is
the checkout, so the scripts keep finding each other):

```sh
mkdir -p "$HOME/bin"
ln -sfn "$HOME/fes/scripts/gpu-run" "$HOME/bin/gpu-run"
ln -sfn "$HOME/fes/scripts/gpu-status" "$HOME/bin/gpu-status"
```

`gpu-run --help` lists the flags. Waiting is the default; `--no-wait` exits 75
when the GPUs are not free.

## Finish with a handoff

State scope, base and result commit (or uncommitted diff), tests and results,
hardware evidence class, effects on module/shared contracts, and the next
integration step. Commit, push and open PRs as needed without further approval.
Merging requires review by Deano or another agent/model reviewer; agents never
merge on their own authority.
