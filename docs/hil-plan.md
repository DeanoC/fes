# HIL plan

The kit HIL baseline tracks main through promote and rollback scripts, and ABI PRs use core+sw mode. See "Kit HIL baseline tracking" in `AGENTS.md`.

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
  tools (including `scripts/check_deadcode.py`). Embedded assets and compiler
  inputs within Go package directories are classified through their consumers.
- `overlay`: the change ships without a new image, as a PR
  [kit software bundle](kit-software-releases.md) (`mister-runtime`,
  `mister-agent`, `fogcast-tenfoot`, other FogCast Go), a host binary
  (`host:fogcast-api`, `host:fogcast`) or a core package (`core:<name>`,
  `core:ALL`). `cmd/fogcast-api` is `kit-go+host`: the bundle ships it as
  `bin/fogcast-kit`, the kit's FogCast host.
- DE25 packages use `core:de25-menu`, `core:de25-zx81` and
  `core:de25-ALL` (both packages). These require a claimed DE25 and the
  corresponding Quartus package. A DE10 test cannot satisfy them; an executor
  without a DE25 route must stop for that hardware check. Shared DE25 board
  code selects both packages, while computer-specific code selects ZX81.
  The captured ZX81 firmware generator also selects the ZX81 hardware check.
  Package changes alone remain `overlay` and do not require rebuilding Linux.
  Standalone LED, RAM-test and link-proof diagnostics remain separate from
  deployed-package testing. The regression checks the actual MENU and standard
  ZX81 board-source closures so deployed inputs cannot silently become `none`.
- The DE25 permanent-owner kernel includes `image/kernel/de25-dma/` in native
  builds as well as QEMU tests. Driver changes and its native kernel/image
  recipe helpers require `FULL_IMAGE` and a DE25 boot test. Their original
  diagnostic names do not exempt them. The disabled KUnit test source itself
  remains software-only. Tests check the actual driver directory and owner-image
  recipe inputs against this classification.
- `full`: image, boot media, platform, profile, splash, video-part or kit
  software-format or expansion-archive changes. The decision is `FULL_IMAGE`: stop and build a
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
  (FAT `idle.rbf` refresh) and `sources/FogCast/internal/appliancedata/**`
  (the fes-kit-data gate). The image also supplies `fes-boot`,
  `fes-kit-software` and `fes-bluetooth` from the platform module.

The generated `config/hil-go-consumers.json` records package and embedded-asset
candidates by command and target. The inventory covers the Linux ARMv7 kit,
Linux amd64 and arm64 hosts (including SDL3), and Darwin arm64 hosts (including
CGO and SDL3). Platform image commands use their ARMv7 build graph and the
local appliance module. It records import reachability, **not** proof that a
source edit changed executable bytes: linking and inlining affect that result.

A shared change can also reach image-only commands, the supported host clients
`fes-update` and `fes-software`, or the bundle and image-lock builders. When
those candidates need analysis, `classify` returns `COMMAND_EVIDENCE_REQUIRED`
(exit 3), with the ordinary deployment result in `deployment_decision` and
per-command paths, roles and targets in `command_evidence`. Compare or build the
exact base and head artifacts and record the relevant command's execution or
other appropriate evidence; if an image binary changed, plan its image build
and boot test. A full image alone does not build or test an independent host
client or builder. Optional standalone `mister-bridge` and `fes-parts`, and
simulation tools, appear as `advisory_commands`; they are not required kit
artifacts. An embedded asset can select deployment even when it is a README.

The planner reads inventories from the requested base and head revisions. A
missing or malformed inventory, changed import/build/embed graph, unknown Go
package, or a paths-only plan whose graph cannot be checked also returns the
blocking decision and identifies what needs verification. In particular, a
base image or merge base older than this inventory is not silently substituted
with head data; use a baseline containing the inventory or independently
establish the historical graph and affected artifacts. `--strict` applies the
same rule to its image base. Regenerate with
`python3 scripts/hil_go_inventory.py`; `make check` and the parent
`make test-changed` lane reject a stale graph or failed discovery. The guard
uses the configured Go proxy and module cache, so a cold cache needs normal Go
module access; offline missing dependencies fail rather than silently passing.

Lander's `~/lander-hilq/hilq.py` uses its own classifier and does not consume
this planner's command-evidence fields. Carry the specific command and image
requirements into the Lander handoff; a queue dispatch alone does not resolve
them.

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
Expansion archive producers (`scripts/*slot_card*`,
`scripts/build_coleco_bus_diagnostic.py`, `scripts/build_coleco_sgm.py` and
`scripts/build_zx81_bus_validation_cart.py`) and their inputs also force
`FULL_IMAGE`. This is an escalation, not archive attestation: the default image
does not contain these archives. Identify and build the changed archive, record
its identity, and validate that artifact through its applicable hardware path;
an image build or core overlay alone cannot establish that evidence.

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
