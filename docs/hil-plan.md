# HIL plan

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

GPU jobs follow the [Shared GPUs](../AGENTS.md#shared-gpus) section.

`python3 scripts/hil_plan.py classify` decides whether a pull request can be
hardware-tested on a kit's running image or needs a full image first.

```sh
python3 scripts/hil_plan.py classify --repo . \
  --base-image-commit "$BASE_IMAGE_COMMIT" --head "$PR_HEAD"
```

`classify` decides from the pull request's own diff. It takes the merge
base of `origin/main` (or local `main`, when that ref is not `--head`) and
`--head`, then diffs that base through `--head` with `--no-renames`.
Commits between `--base-image-commit` and the pull-request base are a
warning. They do not change the decision. `--strict` classifies the image
commit through `--head`. `--paths-file` classifies a list of paths instead
of a git range, and `--json` prints the plan as JSON.

Each changed path gets a class and a component:

- `none`: docs, tests and simulation inputs. No hardware run is needed.
- `overlay`: the change ships without a new image, as a PR
  [kit software bundle](kit-software-releases.md) (`mister-runtime`,
  `mister-agent`, `fogcast-tenfoot`, other FogCast Go), a host binary
  (`host:fogcast-api`, `host:fogcast`) or a core package (`core:<name>`,
  `core:ALL`).
- `full`: image, boot media, platform, profile, splash, video-part or kit
  software-format changes. The decision is `FULL_IMAGE`: stop and build a
  full image. A full image swap or reimage still needs Deano's explicit OK.

A changed file under `cores/<name>/` also requires every `build_fes_*`
producer that names the file or one of its directories, either in its own
text or through a misteross script it imports, transitively. The planner
walks each script's AST and follows the imported names it uses, including
constants and default arguments. Any import it cannot resolve to a misteross
script (a missing or nested module, a `..` relative import, a computed
dynamic import, unparsable source) forces `FULL_IMAGE`. A changed misteross
script likewise requires every producer that imports it. If a shared helper
names it, the plan requires `core:ALL`. The splash (`cores/fes-splash/**`,
`build_fes_splash*`, `sealed/**`), the video part producers
(`scripts/*video_part*`) and any core file they read force `FULL_IMAGE`.

For an `overlay` decision, use a private host for the changed artifacts. Install
the PR kit-software bundle on the claimed kit's running image only when the plan
includes changed kit software, as [kit sharing](kit-sharing.md) and
[kit software releases](kit-software-releases.md) describe. For a core-only
plan, load the core package through a private host without installing a bundle.
For a host-only plan, run the changed private host binary without touching kit
software. After installing a kit-software bundle, restore the baseline image and
bundle named in the kit status file and log that restore.
