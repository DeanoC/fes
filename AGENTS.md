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
3. MUST install a PR bundle on the kit's running image. After use, restore
   the designated baseline image and bundle the status file names, and log
   that restore. Confirm the kit is idle and lease-free before you release
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
  Install the PR bundle on the kit's running image. After use, restore the
  designated baseline image and bundle named in the status file and log that
  restore. Bundle install-then-restore is pre-approved. A successful selection
  stays installed, and nothing restores a stock bundle automatically; the
  return to baseline is an explicit activation of the IDs in the status file.
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

## Finish with a handoff

State scope, base and result commit (or uncommitted diff), tests and results,
hardware evidence class, effects on module/shared contracts, and the next
integration step. Commit, push and open PRs as needed without further approval.
Merging requires review by Deano or another agent/model reviewer; agents never
merge on their own authority.
