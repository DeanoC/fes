# Overlay HIL planner

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

Use this procedure on the kit claimed in its status file. Kits A and B are
interchangeable. Kit A is `192.168.10.84`; Kit B is `192.168.10.85`. Set
`KIT_API` to that kit's target API (`http://192.168.10.84:8182` or
`http://192.168.10.85:8182`). The private host `[[targets]]` address must be
the same kit, and the selected host target identity must match the
status-file claim before launch or capture. A Kit B run uses Kit B for every
target read, including `core-capture --target-url` and the update curls
below. An older note that names only kit A, or that says a person must be
present, does not apply. Follow the capture and login details in
`sources/FogCast/docs/DEVELOPMENT.md`. Never deploy to an unknown device.
`classify` decides from the pull request's own diff. It takes the merge
base of `origin/main` (or local `main`, when that ref is not `--head`) and
`--head`, then diffs that base through `--head` with `--no-renames`.
Commits between `--base-image-commit` and the pull-request base are a
warning. They do not change the decision. `--strict` classifies the image
commit through `--head`, which is the previous behaviour. `FULL_IMAGE`
means stop and build a full image. A full image swap or reimage still
needs Deano's explicit OK.

Kit file changes go through the [kit runtime manifest](kit-manifest.md):
`python3 scripts/fes kit claim`, `swap`, and `restore`. Do not copy binaries,
configs, or core-package files onto a kit directly, and do not install them
with `kit-deploy-script`. `python3 scripts/hil_plan.py kit-deploy-script`
refuses that install path. `--legacy` only reprints the old generator for
the syntax fixture and must not be run on a kit. HIL evidence includes
`python3 scripts/fes kit status` for every kit the run touched, passed to
`hil_plan.py evidence` as `--kit-status`. Evidence checks the boot id and
each overlaid kit binary's `current` hash, and copies the saved text into
the report. A persistent swap (including `launcher.json` and `agent.toml`)
has `--why` in that status. Release the kit.py execution lease before swap or
restore; those commands acquire their own target maintenance reservation across
service restart and do not reboot the board. A supporting installed agent is
required; follow the one-time adoption procedure in kit sharing.

1. Authorize the designated kit and prepare a clean worktree at the exact full PR head. Build the affected binaries and cores from that head.
2. For every SSH/SCP call create a throwaway known_hosts file and use `StrictHostKeyChecking=accept-new` (never `/dev/null`, the real known_hosts, or `StrictHostKeyChecking=no`).
3. Classify the pull request. Stop if the decision is `FULL_IMAGE`. An image-gap warning names commits the booted image has not caught up with; that warning alone does not require a full image. `--strict` folds that gap back into the decision.
4. Create a component-scoped manifest. Kit binary destinations must be `/usr/sbin/{mister-runtime,mister-agent,fogcast-kit,fogcast-tenfoot}`; host/core development artifacts use absolute host paths.

```sh
python3 scripts/hil_plan.py manifest --head "$PR_HEAD" --out /tmp/hil-manifest.json \
  mister-runtime=/build/mister-runtime=kit:/usr/sbin/mister-runtime \
  mister-agent=/build/mister-agent=kit:/usr/sbin/mister-agent \
  host:fogcast-api=/build/fogcast-api=host:/home/deano/tmp/hil/fogcast-api
```

Every binary must embed the head. FogCast Go binaries need the full revision
from the Makefile's `-X internal/version.Revision=$(REVISION)`, and
`mister-runtime` needs `git-<40 hex>` (image recipe) or `git-<12 hex>`, with or
without `-dirty` (component Makefile, whose dirty probe currently always
appends `-dirty`). A `host:<command>` component must target a file named
`<command>`, and its Go build info must name the main package
`github.com/DeanoC/FogCast/cmd/<command>`, so a `fogcast-api` binary cannot
stand in for `host:fogcast`. Evidence re-reads each local artifact, so generate it on the machine
where the manifest was built. For each `core:*` archive, the planner reads the local archive and derives its
package ID and core ID. The archive must identify the requested core, and its
embedded `build.revision` must equal the full PR head. A changed file under
`cores/<name>/` also requires every `build_fes_*` producer that names the file
or one of its directories, either in its own text or through a misteross script
it imports, transitively (for example `build_fes_catch` imports
`build_fes_demo`, and the splash pins `fes_application.vh` through
`fes_de10nano_evidence.HPS_DDR_HEADER`). The planner walks each script's AST and
follows the imported names it uses, including constants and default arguments.
Any import it cannot resolve to a misteross script (a missing or nested module,
a `..` relative import, a computed dynamic import, unparsable source) forces
`FULL_IMAGE`. A changed misteross script likewise requires every producer that
imports it, directly or transitively (so `build_fes_pong.py` or
`fes_de10nano_evidence.py` forces `FULL_IMAGE` through the splash). If a shared
helper names it, the plan requires `core:ALL`. Some changes cannot be
overlaid and force `FULL_IMAGE`. These are the splash, which becomes the boot
`/idle.rbf` (`cores/fes-splash/**`, `build_fes_splash*`, `sealed/**`), the video
part producers (`scripts/*video_part*`) and any core file they or the splash
producer read, directly or through imports (so `fes-pong` files that
`build_fes_pong` names also force it, because the splash imports that module), and Go packages that only image-build
commands use (`internal/targetimage`). Host-server
changes require the binary from the exact-head build to be started as `fogcast-api`
before the host attestation command is run.

5. **Binary-only and mixed plans:** claim the lease from `sources/misteross`
   with a `kit.py session` and keep it open (it renews every 20 s). Save public
   `kit.py status` and record `claimed`, then send `stop` to idle the kit. For
   core-only plans, skip the `kit.py session`; the host must claim the lease.

   ```sh
   python3 scripts/kit.py --config CFG session --owner KIT_OWNER --purpose P
   python3 scripts/kit.py --config CFG status > /tmp/lease-claimed.json
   python3 ../../scripts/hil_plan.py lease-record --log /tmp/lease.jsonl --step claimed \
     --status-json /tmp/lease-claimed.json --owner KIT_OWNER
   ```

6. **Binary-only and mixed plans:** release before deployment because restart
   invalidates a held lease. Save status, confirm `"state": "free"`, and record
   `released-for-restart`. Claim the kit if this session does not already hold
   it, then install each changed file with `python3 scripts/fes kit swap`. Each
   transaction is fenced by the target maintenance operation. On failure, repair
   and explicitly resume/finish it before attempting execution.

7. **Mixed kit binary and core sequence:** after deploy and restart, run
   `fogcast core-load` using the core archive from the exact-head build.
   `core-load` takes its own host lease through `fogcast-api`. Save `kit.py
   status` as `reacquired` (held by `HOST_OWNER`). While that lease is held,
   run the generated `kit-command` on the kit and `host-command` on the host
   and keep those captures for `--kit-sha256` and `--host-sha256`. A mixed
   manifest always has both sides, so evidence refuses without fresh hashes
   from this lease. Then run `core-capture` (below) and the HIL test.
   Explicitly stop the host session with `POST /api/v1/session/stop`; save
   status (free or revoking) and record `released`.

8. **Core-only sequence:** confirm `kit.py status` is free and run
   `fogcast core-load` from the exact-head build. This sequence does not
   install a bundle. The host command takes its own
   lease through `fogcast-api`. While the core runs, save status (held by
   `HOST_OWNER`) and record `claimed`. Run `core-capture` (below) and the HIL
   test. Explicitly stop the host session, save status (free or
   revoking), and record `released`.

   **Binary-only sequence:** reacquire with a new `kit.py session`, record
   `reacquired`, capture `/v1/update`, then run the `kit-command` output on the
   kit and `host-command` output on the host. For `host:fogcast-api`, run the
   command as the user running that server after starting the exact-head binary.
   It scans `/proc/*/exe`, including deleted mappings. Run the HIL test under the
   lease, then release and record `released`.

   `/v1/status` carries no boot ID, so a status file alone cannot prove which
   boot it came from. For plans with core entries, `core-capture` reads
   `/v1/update`, `/v1/status` and `/v1/update` again in one run and writes a
   timestamped bundle. Evidence requires both update captures in the bundle to
   pass the image checks and to carry the checked boot ID. The bundle must also
   fall inside the host lease window, between the host-held record (`reacquired`
   or `claimed`) and `released`. The token is read from `FOGCAST_TOKEN` and is
   never written out.

   ```sh
   # Claimed kit only. Kit A: http://192.168.10.84:8182
   KIT_API=http://192.168.10.85:8182
   fogcast --json --api http://127.0.0.1:8787 core-load /abs/path/core.fcore
   curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
     "$KIT_API/v1/update" > /tmp/kit-update.json
   python3 scripts/hil_plan.py core-capture --target-url "$KIT_API" \
     --out /tmp/core-capture.json
   ```

   **Launcher binaries:** S60 runs either `fogcast-tenfoot` or `fogcast-kit` as
   the supervised child (`/run/fogcast-kit.pid`), depending on `launcher.json`.
   Evidence requires the child's running hash to match the overlaid binary. If
   only `fogcast-kit` is planned, the kit must be running the `fogcast-kit` child
   (not tenfoot). If both launchers are planned, the child must be the overlaid
   tenfoot, and the `kit_ui` line from `kit-command` must show that the overlaid
   `fogcast-kit` completed `--print-kit-ui` and selected `tenfoot`.

9. Save `python3 scripts/fes kit status` for each kit in the run and pass
   those files to evidence as repeated `--kit-status` arguments. Then generate
   evidence with the base build's `release.json`, the update capture,
   the hash outputs, the lease log, and one `--core-status core:NAME=FILE` per
   core, where FILE is the `core-capture` bundle. Pass
   `--host-lease-owner HOST_OWNER` whenever cores are present. In mixed plans,
   pass `--lease-owner KIT_OWNER` too. Every update capture must pass the same
   image checks and carry the same boot ID. With kit entries, that ID must also
   match the kit command. Evidence checks component coverage, file and running
   executable hashes, core package identity and state, image identity, boot
   identity, supervisor counts, and exact lease order.

10. Release the kit.py lease, then restore the kit's designated baseline
    image and bundle and log that restore. A core-only run that did not
    change the bundle does not install one. When a bundle was installed,
    it was installed on the running image. Activating the baseline bundle
    does not change the booted image. If the booted image is not the named
    baseline, restore that image and verify the boot first, then activate
    a compatible baseline bundle and verify it before logging the kit
    restored. Bundle install-then-restore is pre-approved. The bundle
    step is the explicit activation described in
    [kit software releases](kit-software-releases.md), not an automatic
    stock restore. Confirm the kit is idle and
    lease-free, and log the result in that status file. On an unmanaged
    overlay, `python3 scripts/fes kit restore <kit>` returns swapped files
    under the same release-before-restart discipline, and the boot id must
    be unchanged. That file restore does not replace the baseline restore.

```sh
python3 scripts/hil_plan.py evidence --repo . --base-image-commit "$BASE_IMAGE_COMMIT" \
  --head "$PR_HEAD" --base-image-sha256 "$BASE_LINUX_IMG_SHA256" \
  --base-release-json /path/to/release.json --kit-update-json /tmp/kit-update.json \
  --manifest /tmp/hil-manifest.json --kit-sha256 /tmp/kit.sha256 \
  --host-sha256 /tmp/host.sha256 --lease-log /tmp/lease.jsonl --lease-owner KIT_OWNER \
  --host-lease-owner HOST_OWNER --kit-status /tmp/kit-a-status.txt \
  --core-status core:NAME=/tmp/core-capture.json \
  --out /tmp/hil-evidence.md
```

All target API reads use the bearer token from the private FogCast config's
target `agent` token. Set it in `FOGCAST_TOKEN` without printing it; never print
or commit the token. Every curl uses the authenticated, failing form:

```sh
# KIT_API is the claimed kit. Kit B: http://192.168.10.85:8182
curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
  "$KIT_API/v1/update" > /tmp/kit-update.json
```
