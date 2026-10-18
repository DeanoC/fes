# Working in FES

## Shared kits and Powerboat (READ FIRST)

Outside agent sessions have ignored these rules. Obey them.

1. MUST claim the kit before you touch it, and log the result when you
   finish. There is no separate Powerboat host claim. Record owner, PR,
   and start time in that kit's Powerboat status file:
   `~/kit-status/kitA-STATUS.md` or `~/kit-status/kitB-STATUS.md`. That
   log is the single source of truth. NEVER take a kit that is leased or
   claimed by someone else. NEVER use a Slack reservation, and NEVER
   require a person to be present. GPU jobs on Powerboat go through the
   `gpu-run` wrapper: one `flock` lease per GPU, and `gpu-status` shows
   the holders.
2. Kits A and B are both designated test kits. They are interchangeable.
3. When a task installs a bundle, install that PR bundle on the kit's
   running image. A core-only run that does not change the bundle does not
   install one. After use, restore the designated baseline image and bundle
   the status file names, and log that restore. Activating the baseline
   bundle does not change the booted image. If the booted image is not the
   named baseline, restore that image, verify the boot, then activate a
   compatible baseline bundle and verify it before logging the kit
   restored. Confirm the kit is idle and lease-free before you release it.
4. MUST keep Kit B's `u-boot.txt` line `usb-storage.quirks=152d:0583:i`.
5. Destructive actions need Deano's explicit OK: reimage, an image or
   boot-config change, killing other people's processes, and closing PRs.
6. These stay pre-approved: deleting old images (keep the running image
   and the previous one), SD reburn, and bundle install-then-restore.
   Never delete a factory or known-good rollback image. Before an SD
   reburn, verify the device path is that kit's SD card.
7. NEVER use broad `pkill` or `killall` patterns on Powerboat (for example
   `pkill -f run.sh`). Kill only PIDs you started. Give your scripts
   distinctive names.
8. NEVER run interactive OAuth or browser logins on Powerboat. Its harness
   `BROWSER` setting makes `xdg-open` loop and fork-bomb the host.

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
  When the task installs a bundle, install that PR bundle on the kit's running
  image. A core-only run that does not change the bundle does not install one.
  After use, restore the designated baseline image and bundle named in the
  status file and log that restore. Activating the baseline bundle does not
  change the booted image. If the booted image is not the named baseline,
  restore that image and verify the boot first, then activate a compatible
  baseline bundle and verify it before logging the kit restored.
  Bundle install-then-restore is pre-approved. A successful selection stays
  installed, and nothing restores a stock bundle automatically; the return to
  the baseline bundle is an explicit activation of the ID in the status file.
  Do not put another task's release in place of that baseline, require matching
  host commits, or use file overlays on a managed kit.
- A build never authorizes deployment. Kits A and B are both designated test
  kits and are interchangeable. Claim one in its status file and hold the
  session described in [kit sharing](docs/kit-sharing.md). Never bypass
  another owner's lease with direct programming.
- Report which kind of evidence you have: host-only check, hardware diagnostic,
  or exact-artifact hardware acceptance. A new profile inherits no acceptance.
- `make media`, `make verify-media` and `make rollback-media` hold the media
  lease and never write a block device; see [bootable media](docs/bootable-media.md).
  Change `media/current` only through these leased commands. Writing a physical
  card needs an explicit device path plus the kit lease. An SD reburn of a
  designated test kit is pre-approved. Verify the device path is that kit's
  SD card before writing.
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

`scripts/gpu-run` holds one flock per GPU in the wrapper and runs the command
as a child. That child does not inherit the lock, so the lease ends when the
command exits even if it backgrounded other processes, and the kernel drops
it if the wrapper exits or crashes. `scripts/gpu-status` shows each GPU as
free or held, the holder metadata and age, and the waiting queue. Leases live
in `/var/tmp/gpu-leases` (directory mode `0700`, files mode `0600`) unless
`GPU_LEASE_DIR` is set. The directory must be on a local filesystem. A waiter
is admitted under the cap it started with, so a capped waiter already at its
allowance does not block another job from taking a free GPU.

Install on a host by symlinking the scripts into `~/bin` (the link target is
the checkout, so the scripts keep finding each other):

```sh
mkdir -p "$HOME/bin"
ln -sfn "$HOME/fes/scripts/gpu-run" "$HOME/bin/gpu-run"
ln -sfn "$HOME/fes/scripts/gpu-status" "$HOME/bin/gpu-status"
```

`gpu-run --help` lists the flags. Waiting is the default; `--no-wait` exits 75
when the GPUs are not free.

## Timing-failure playbook (P&R misses)

This applies when a factory build, sweep, replay or nightly run misses timing on any core. Don't just reroll seeds.

1. **Confirm the miss is real.** Replay the core's production recipe (seed, weight, critexp, every flag, env, timeout) on the same frozen `synth.json`, the same binary and the same physical GPU. If the old binary is in question, run a paired old/new replay as well. Note whether the GPU was contended.
2. **Never relax a constraint.** Don't edit the SDC/QSF clocks, add `--timing-allow-fail` to a signoff, loosen a seal, or lower a required clock. If closing would need any of these, stop and report.
3. **Work through the core's option matrix in order, within the budget:**
   1. the production seed list at the production weight (what the producer does already)
   2. timing weight variants {production, 300, 1000, 2000} × critexp {production, 2, 5}
   3. known-helpful fork flags, one at a time, on the best two weight/critexp pairs: `--arrival-pin-assignment`, `--isolated-lut-placement`, then both
   4. any core-specific entries listed in that core's recipe `playbook` table
   - **Seeds:** use the production seed + 2 per combination, unless the recipe says otherwise.
4. **Budget:** at most 6 GPU-hours per core per incident (ST: 10). Route everything through `gpu-run`, at most 2 GPUs per owner, FIFO, never preempting anyone else's job. Heavy sweeps go to the CUDA box only as pre-screens. The winning combination must be confirmed on HIP on Powerboat.
5. **Record every run**: core, fes SHA, synth hash, toolchain slot, full argv, env, seed, GPU, contended or not, per-clock fmax, route time. Keep it in `docs/validation/<date>-<core>-timing-playbook/` as `runs.jsonl` plus a summary table.
6. **If a combination closes** on at least 2 of 3 seeds, open a recipe-change PR that touches only that core's recipe. Include the run table and the before/after per-clock fmax. A recipe change follows the normal merge bar, including HIL for cores that ship in the image. A flag that helps one core is never made a global default.
7. **If nothing closes within budget**, the core stays on its current pin and recipe. File or update the core's timing issue with the run table and stop. Don't keep sweeping without Deano's OK.

Long term this matrix becomes an automatic, alphamister-style search over the same option space, with the same budget and recording rules.

## Finish with a handoff

State scope, base and result commit (or uncommitted diff), tests and results,
hardware evidence class, effects on module/shared contracts, and the next
integration step. Commit, push and open PRs as needed without further approval.
Merging requires review by Deano or another agent/model reviewer; agents never
merge on their own authority.
