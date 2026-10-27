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

- `none`: docs, tests, simulation inputs and explicitly listed CI/checking
  tools (including `scripts/check_deadcode.py`). No hardware run is needed.
- `overlay`: the change ships without a new image, as a PR
  [kit software bundle](kit-software-releases.md) (`mister-runtime`,
  `mister-agent`, `fogcast-tenfoot`, other FogCast Go), a host binary
  (`host:fogcast-api`, `host:fogcast`) or a core package (`core:<name>`,
  `core:ALL`).
- `full`: image, boot media, platform, profile, splash, video-part or kit
  software-format changes. The decision is `FULL_IMAGE`: stop and build a
  full image. A full image swap or reimage still needs Deano's explicit OK.
  On a shared kit, software goes first: install and activate a bundle the
  new image accepts, normally the PR's own bundle, before the image update.
  `fes-update` refuses an image whose menu interfaces the selected bundle
  lacks (see [image compatibility](kit-software-releases.md#image-compatibility)).
  Trial, confirm, boot-ticket and storage-gate code is full too. On a trial
  boot `fes-kit-software` starts only the image's built-in software (it does
  not open the bundle store) and the storage gate runs as the image's
  `/usr/sbin/fes-kit-data`, so a bundle install cannot exercise that code.
  The paths are `sources/FogCast/appliance/store/**` (release confirm,
  boot-ticket rewrite, and splash prune), `sources/FogCast/internal/applianceupdate/**`
  (the agent's reconcile and repair), `sources/FogCast/internal/appliancesplash/**`
  (FAT `idle.rbf` refresh on confirm, and the restored image's splash on
  rollback confirm) and `sources/FogCast/internal/appliancedata/**` (the
  fes-kit-data gate). Packages under `appliance/` that an image-only binary
  imports are full too, even when overlay code also imports them. A bundle
  cannot replace those binaries, so an overlay install cannot exercise the
  changed code. The set comes from `go list -deps`, not a guess. fes-boot
  (`platform/cmd/fes-boot`) and the trial-startup supervisor
  (`platform/cmd/fes-kit-software`) are listed from a workspace that replaces
  `github.com/DeanoC/FogCast/appliance` with `sources/FogCast/appliance`.
  fes-kit-data (`sources/FogCast/cmd/fes-kit-data`) is listed from the FogCast
  module. Those three commands import:

  - `github.com/DeanoC/FogCast/appliance` (`sources/FogCast/appliance/*.go`,
    the release manifest; `release.go` is the only non-test file), from
    fes-boot and fes-kit-data.
  - `github.com/DeanoC/FogCast/appliance/blockprobe`, from all three.
  - `github.com/DeanoC/FogCast/appliance/softwarecontrol`, from
    fes-kit-software and from fes-kit-data through `internal/applianceupdate`
    → `internal/agent` → `protocol`.
  - `github.com/DeanoC/FogCast/appliance/store`, from fes-boot and fes-kit-data
    (already `appliance/store/**`).
  - `github.com/DeanoC/FogCast/appliance/software`, from fes-kit-software
    (already `appliance/software/**`).

  The first three are `sources/FogCast/appliance/*.go` and
  `sources/FogCast/appliance/blockprobe/**` (`appliance-trial`) and
  `sources/FogCast/appliance/softwarecontrol/**` (`kit-software`). `*_test.go`
  stays `none`. Importers outside those packages, including `cmd/mister-agent`,
  `internal/httpapi`, `catalog` and `protocol`, stay on their own rules.
  Trial startup (`platform/cmd/fes-kit-software`,
  `platform/internal/kitsoftware`), trial boot (`platform/cmd/fes-boot`,
  `platform/internal/applianceboot`) and the `fes-update` and `fes-kit-data`
  commands are already full through the platform and unmapped-command rules.
  `sources/FogCast/internal/ext4image/**`, which only `fes-update` imports, is
  full with them.

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
For a host-only plan, run the changed private host binary. That classification
does not bar placing firmware, ROMs, media or software on a kit when the planned
check needs them; use the existing lease and applicable install, activation or
media-delivery path. After installing a kit-software bundle, restore the
baseline image and bundle named in the kit status file and log that restore.
