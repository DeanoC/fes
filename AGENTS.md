# Working in FES

## Rules for all agents

Never rebase, force-push, or amend pushed commits on any PR branch or main. Update a PR branch by merging main into it. Don't push to a PR branch you weren't asked to work on.

Never pin a GPU vendor.

Nobody touches Deano's harness (~/harnesses on Powerboat) or its processes.

A core's locked nextpnr is only its last known good, not a required pin. If a newer nextpnr closes that core's timing, move the lock forward instead of changing RTL to fit the old version. Timing and seal checks still apply.

## PR workflow: lanes, merge gate, review, stacks, HIL, ops

This section is the shared rulebook for every agent (Codex, Cursor, Claude, Grok Bot) in DeanoC/fes, nextpnr, yosys and mistral.

### Lanes and labels
- `lane:lander`: Lander's team drives the PR to merge (fixes, review, CI, HIL).
- `lane:deano`: Deano and his side agents own it. Lander's team does not touch it.
- `needs:design`: too big for a merge-fix. Add it together with `lane:deano` to hand a PR back.
- An unlabelled ready PR counts as `lane:lander`.
- Draft means only "not ready for review". It is not an ownership signal.
- To hand off, swap the lane label and comment why and what is left.
- Take `lane:deano` work only when Deano assigned it to you.

### Merge gate
Merge only when all of these hold:
- The head SHA is exactly the one checked.
- CI is green: `gh pr checks N | awk -F'\t' '$2!="pass" && $2!="skipping"'` prints nothing (column 2 is the check state).
- 0 unresolved review threads.
- A clean SuperGrok or Codex review on the exact head. One exception: a clean review carries over to a new head that only adds a merge from the base branch, when the PR's own diff against its base is unchanged and CI is green on the new head.
- HIL is PASS on the exact head, N/A, or waived. HIL N/A means no kit test applies: the PR changes no kit-run bitstream, firmware or runtime (docs, CI, host tooling only), or the core has no kit (pc386/i386). DE25 HIL is waived until Deano says the boards are ready. A pass on a diagnostic build does not count.
- The base branch is live, and no open PR uses this head branch as its base.
- Stacks merge top-down. After each merge, the next PR's tree must equal the reviewed tree, or it needs a fresh review.

Lander merges directly with `gh pr merge N --squash --match-head-commit <sha>` and sends Deano an FYI.

### Review
- One full review, then re-reviews check only the fix.
- P3s never block. File a follow-up issue instead.
- Budget is 2 review rounds. After that the PR goes to one owner for a bigger fix, or to `needs:design` + `lane:deano`.

### Stacks
- Max stack depth is 2. Land deeper work on main behind feature flags.
- One owning agent per stack, end to end.
- Batch fixes. Merge main in only when there is a conflict.
- A clean review carries over across base-only merges (see the merge gate exception).

### PR size
- Prefer one PR per coherent feature or milestone, not one PR per step.
- Don't mix unrelated subsystems in one PR.
- Don't split work into stacked chains. Stacks stay at most 2 deep.
- Put unfinished work behind a feature flag and land it on main (trunk-based) instead of holding long stacks.
- Small PRs are still right for isolated fixes.

### HIL
- Run HIL in parallel with review. Use kit A if kit B is busy.
- Stop after 2 FAILs on the same head and fix instead.
- Report HIL as started only once a kit is locked and flashed.
- Afterwards, restore each kit to exactly the baseline bundle and image that kitA-STATUS.md / kitB-STATUS.md names. Activate the named bundle; reflash only if the booted image differs from the named image (then follow the bundle/image order in the kit procedure (step 3) below) and verify the boot. Verify the active bundle before logging the kit restored. Baselines are promoted only from main HIL passes.

### Toolchains
- Quartus is primary for DE25, pc386 and PC/XT. nextpnr is advisory for those.
- DE25 is Quartus-only.

### Hard rules
- No rebase, force-push or amend.
- Never relax timing or seal checks.
- Do not close PRs without Deano's OK.

### Ops
- Check disk before launching a job. Refuse under 30G free.
- Every job needs a heartbeat and a Quartus time cap, and notifies on finish or fail.
- When stuck or looping, change approach and say what changed.

### Machines
- Powerboat does HIL, the kits and capture. DE25 capture is `/dev/v4l/by-id/fes-ms2109-hil`; never open the raw MS2109 device.
- The 3090 box is becoming the second Quartus/CI builder.

## PR scope

- One scope per PR. Don't bundle unrelated fixes.
- The PR body says `Closes #N` for the issue it resolves. Agents may open that issue themselves when opening the PR.
- Small, self-explanatory PRs may skip the issue and the `Closes #N` line.
- Before opening a PR, check for an open PR that already covers the change, and don't duplicate it.
- Stay inside the repo and the scope the task names.

## Shared kits and Powerboat (READ FIRST)

Outside agent sessions have ignored these rules. Obey them.

1. MUST claim the kit before you touch it, and log the result when you
   finish. There is no separate Powerboat host claim. Record owner, PR,
   and start time in that kit's Powerboat status file:
   `~/kit-status/kitA-STATUS.md` or `~/kit-status/kitB-STATUS.md`. That
   log is the single source of truth. NEVER take a kit that is leased or
   claimed by someone else. NEVER use a Slack reservation, and NEVER
   require a person to be present.
2. Kits A and B are both designated test kits. They are interchangeable.
3. When a task installs a bundle, install that PR bundle on the kit's
   running image. A core-only run that does not change the bundle does not
   install one. Software goes before the image: before an image change,
   activate a bundle the new image accepts (`fes-update` refuses an image
   whose menu interfaces the selected bundle lacks). After use, restore the
   designated baseline image and bundle the status file names, and log that
   restore. Activating the baseline bundle does not change the booted image.
   If the booted image is not the named baseline, first activate the
   baseline bundle if the running image accepts it, then restore that image
   and verify the boot; otherwise restore and verify the image, then
   activate the baseline bundle. Verify the active bundle before logging the
   kit restored. Confirm the kit is idle and lease-free before you release
   it.
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

Firmware, ROMs, media and software may be placed on either kit. Use the
applicable kit lease and supported install, activation or media-delivery path;
these rules do not require keeping those artifacts on a host or Powerboat.

GPU jobs follow the [Shared GPUs](#shared-gpus) section.

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
- Delete superseded code; don't keep it. When a replacement lands, remove the
  old path, its tests, build targets and docs in the same or the next PR. Don't
  add compatibility shims, fallbacks or "kept for older kits" branches: both kits
  are test kits and an SD reburn is pre-approved. Removal needs no special
  justification. Keeping legacy code does. [#871](https://github.com/DeanoC/fes/issues/871)
  tracks the current list. `make deadcode` fails CI on unused Go functions,
  types, fields and variables; delete what it reports.

## Toolchain pin bump checklist

- A toolchain pin bump must build every core it touches before merging.

## Pre-release development and review

FES has not made a release. Optimize routine work for a short edit/test/play
cycle. Use the affected checks and the changed artifact's hardware path;
do not run a full host/image/core campaign for every ready PR.

- The default OS image contains MENU display firmware and the locked idle
  RBF. Install playable cores independently through the catalog. A software
  or OS change does not require rebuilding or qualifying every playable core.
- Host changes use a private host. Kit agent/runtime/UI changes use
  `make kit-software` and the existing install/test/restore procedure on the
  running image. Core changes prepare and test the affected package.
- Docs, tests and CI tooling need no kit deployment. Image/boot changes
  still need a boot test; use `scripts/hil_plan.py` to identify deployed
  consumers rather than treating a build-tool edit as deployed software.
- Start with focused tests, then `make test-changed` and relevant parent
  checks. Full regression campaigns, exhaustive instrumentation and
  reproducibility checks belong to scheduled runs or explicit integration
  milestones. Do not repeat successful checks without a changed input,
  failure or unresolved concern. Reuse passing CI software evidence for the
  tested inputs; a kit smoke test does not require rerunning the full host
  suite on Powerboat.
- Review is still required to merge. A ready PR carries its changed artifact,
  completed checks and the specific remaining hardware check so Grokbot can
  run the appropriate path. Do not substitute an all-core build for that plan.

See [the development-speed redesign](docs/development-speed.md) for the
remaining OS, software and room installer separation.

## Hardware, media and releases

- Managed kits install immutable userspace bundles and queue activation at the next
  free lease boundary; see [kit software releases](docs/kit-software-releases.md).
  When the task installs a bundle, install that PR bundle on the kit's running
  image. A core-only run that does not change the bundle does not install one.
  Software goes before the image: before an image change, activate a bundle
  the new image accepts ([image compatibility](docs/kit-software-releases.md#image-compatibility)).
  After use, restore the designated baseline image and bundle named in the
  status file and log that restore. Activating the baseline bundle does not
  change the booted image. If the booted image is not the named baseline,
  first activate the baseline bundle if the running image accepts it, then
  restore that image and verify the boot; otherwise restore and verify the
  image, then activate the baseline bundle. Verify the active bundle before
  logging the kit restored.
  Bundle install-then-restore is pre-approved. A successful selection stays
  installed, and nothing restores a stock bundle automatically; the return to
  the baseline bundle is an explicit activation of the ID in the status file.
  Do not put another task's release in place of that baseline, require matching
  host commits, or put file overlays on a kit.
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
All GPU jobs on those hosts go through `gpu-run`: one `flock` lease per GPU,
first come, first served, and `gpu-status` shows the holders.
Lander uses `--owner lander` and `GPU_LEASE_MAX_PER_OWNER=2`, so Lander holds
at most 2 GPUs at once. Nobody kills a GPU job. Deano's Codex harness jobs
are legitimate and must not be killed; they wait their turn like everyone else.
GPU work on an Apple silicon Mac (nextpnr built with `-DGPU_ROUTER=METAL`) goes
through the same `gpu-run`, which leases its one Metal GPU as id 0.

nextpnr routing on HIP, CUDA, Metal and CPU gives bit-identical route outcomes. HIP,
CUDA and Metal timing and route evidence are interchangeable: references, pins, seed
results and replays from any of them remain valid on the others. This is route-result
equivalence only; production builds, promotion and seal evidence still require an
authenticated live HIP or CUDA backend as producer validators require, and they do
not accept Metal yet. Registered producers reject CPU-reference fallback
(`NEXTPNR_REQUIRE_GPU=1`), so CPU replays can inform experiments only. HIP, CUDA
and Metal builds keep distinct `BUILD_ID`s and artifact identities because backend
is part of functional execution identity, even though their timing and route
evidence is interchangeable. Among current core
package producers, Apple II, Atari ST, C64, Coleco socket-v2, SG-1000, SMS,
Spectrum and ZX81 accept `--gpu-router {HIP,CUDA}`. Catch, Menu, Pong, RAM Tester
and RISC-V accept only `--gpu-device`; their authenticated toolchain determines
the backend. On a CUDA host, select CUDA and its matching toolchain for a
router-selectable producer; a device-only producer can use CUDA only with an
authenticated CUDA-built toolchain. `gpu-run` leases a device, not a backend;
record the backend in the build record. Never require a separate HIP run or
discount CUDA or Metal evidence. Never set `GPU_LEASE_IDS` per job (it is a box
inventory override).

`scripts/gpu-run` holds one flock per GPU in the wrapper and runs the command
as a child. That child does not inherit the lock, so the lease ends when the
command exits even if it backgrounded other processes, and the kernel drops
it if the wrapper exits or crashes. `scripts/gpu-status` shows each GPU as
free or held, the holder metadata and age, and the waiting queue. Leases live
in `/var/tmp/gpu-leases` (directory mode `0700`, files mode `0600`) unless
`GPU_LEASE_DIR` is set. The directory must be on a local filesystem. A waiter
is admitted under the cap it started with, so a capped waiter already at its
allowance does not block another job from taking a free GPU.

On macOS `gpu-run` reads process identity from libproc instead of `/proc` and
checks the lease directory with `statfs(2)`. The Metal GPU comes from ioreg:
a Mac with one Metal device (every Apple silicon Mac) leases it as id 0, which
is also nextpnr's Metal device index; a Mac with several needs `GPU_LEASE_IDS`
and `GPU_LEASE_VENDOR=apple`. Metal has no visibility mask, so a grant exports
no mask and `GPU_RUN_DEVICE` is the physical id. Metal has no device nodes
either. A GPU route proves it used the leased GPU when ioreg shows a Metal user
client that nextpnr opened on it, or when nextpnr logs
`backend metal:<registry id>:... ready` with the leased GPU's registry id. CPU
fallback output fails the route, as on Linux.

Install on a host by symlinking the scripts into `~/bin` (the link target is
the checkout, so the scripts keep finding each other):

```sh
mkdir -p "$HOME/bin"
ln -sfn "$HOME/fes/scripts/gpu-run" "$HOME/bin/gpu-run"
ln -sfn "$HOME/fes/scripts/gpu-status" "$HOME/bin/gpu-status"
```

A Mac installs the same links from its checkout. macOS does not put `~/bin`
on `PATH`, so add it to the login shell too:

```sh
FES="$HOME/Projects/fes"   # the main checkout, not a task worktree
mkdir -p "$HOME/bin"
ln -sfn "$FES/scripts/gpu-run" "$HOME/bin/gpu-run"
ln -sfn "$FES/scripts/gpu-status" "$HOME/bin/gpu-status"
grep -qs 'HOME/bin' "$HOME/.zprofile" || echo 'export PATH="$HOME/bin:$PATH"' >> "$HOME/.zprofile"
```

`gpu-run --help` lists the flags. Waiting is the default; `--no-wait` exits 75
when the GPUs are not free.

## Timing-failure playbook (P&R misses)

This applies when a factory build, sweep, replay or nightly run misses timing on any core. Don't just reroll seeds.

A job must never patch a shared script or repro directory in place; each job works on its own private copy.

1. **Confirm the miss is real.** Replay the core's production recipe (seed, weight, critexp, every flag, env, timeout) on the same frozen `synth.json` and the same binary, using whichever GPU is free via `gpu-run`. If the old binary is in question, run a paired old/new replay as well. Record the GPU backend/device and whether it was contended.
2. **Never relax a constraint.** Don't edit the SDC/QSF clocks, add `--timing-allow-fail` to a signoff, loosen a seal, or lower a required clock. If closing would need any of these, stop and report.
3. **Work through the core's option matrix in order, within the budget:**
   1. the production seed list at the production weight (what the producer does already)
   2. timing weight variants {production, 300, 1000, 2000} × critexp {production, 2, 5}
   3. known-helpful fork flags, one at a time, on the best two weight/critexp pairs: `--arrival-pin-assignment`, `--isolated-lut-placement`, then both
   4. any core-specific entries listed in that core's recipe `playbook` table
   - **Seeds:** use the production seed + 2 per combination, unless the recipe says otherwise.
4. **Budget:** at most 6 GPU-hours per core per incident (ST: 10). Route everything
   through `gpu-run`, at most 2 GPUs per owner, FIFO, never preempting anyone else's
   job. Heavy sweeps and confirmation can use whichever GPU is free; HIP, CUDA,
   Metal and CPU have interchangeable route outcomes, subject to the live-GPU
   production evidence requirement above.
5. **Record every run**: core, fes SHA, synth hash, toolchain slot, full argv, env, seed, GPU, contended or not, per-clock fmax, route time. Keep it in `docs/validation/<date>-<core>-timing-playbook/` as `runs.jsonl` plus a summary table.
6. **If a combination closes** on at least 2 of 3 seeds, treat it as a candidate, not promotion evidence. Before promotion, rerun the full production build with the proposed recipe and require fresh timing evidence from that build, with its embedded `BUILD_ID` reflecting the new recipe; a frozen-`synth.json` sweep alone is insufficient. Record this build in the campaign evidence. The recipe-change PR contains only that core's recipe change plus the campaign evidence files: `runs.jsonl` and the summary table under `docs/validation/<date>-<core>-timing-playbook/`. Include the run table and before/after per-clock fmax in the PR body as well. A recipe change follows the normal merge bar, including HIL for cores that ship in the image. A flag that helps one core is never made a global default.
7. **If nothing closes within budget**, the core stays on its current pin and recipe. File or update the core's timing issue with the run table and stop. Don't keep sweeping without Deano's OK.

Long term this matrix becomes an automatic, alphamister-style search over the same option space, with the same budget and recording rules.

## Finish with a handoff

State scope, base and result commit (or uncommitted diff), tests and results,
hardware evidence class, effects on module/shared contracts, and the next
integration step. Commit, push and open PRs as needed without further approval.
Merging requires review by Deano or another agent/model reviewer; agents never
merge on their own authority.
