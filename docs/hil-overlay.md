# Overlay HIL planner

Use this procedure only on the designated kit A (`192.168.10.84`), under the dedicated fixture rules in `sources/FogCast/docs/DEVELOPMENT.md`. Never deploy to an unknown device. The planner compares base image commit to the full PR head with `--no-renames`; `FULL_IMAGE` means stop and build a full image.

1. Authorize the designated kit and prepare a clean worktree at the exact full PR head. Build the affected binaries and cores from that head.
2. For every SSH/SCP call create a throwaway known_hosts file and use `StrictHostKeyChecking=accept-new` (never `/dev/null`, the real known_hosts, or `StrictHostKeyChecking=no`).
3. Classify against the commit that built the running image. Stop if the decision is `FULL_IMAGE`.
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
appends `-dirty`). Evidence re-reads each local artifact, so generate it on the machine
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
`FULL_IMAGE`. If a shared helper names it, the plan requires
`core:ALL`. Some changes cannot be
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
   `released-for-restart`. Stage files in tmpfs and run the generated deploy
   script with `on` (as described above).

7. **Mixed kit binary and core sequence:** after deploy and restart, run
   `fogcast core-load` using the core archive from the exact-head build.
   `core-load` takes its own host lease through `fogcast-api`. Save `kit.py
   status` as `reacquired` (held by `HOST_OWNER`), run `core-capture` (below)
   and the HIL test. Explicitly stop the host
   session with `POST /api/v1/session/stop`; save status (free or revoking) and
   record `released`.

8. **Core-only sequence:** confirm `kit.py status` is free and run
   `fogcast core-load` from the exact-head build. The host command takes its own
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
   fogcast --json --api http://127.0.0.1:8787 core-load /abs/path/core.fcore
   curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
     http://192.168.10.84:8182/v1/update > /tmp/kit-update.json
   python3 scripts/hil_plan.py core-capture --target-url http://192.168.10.84:8182 \
     --out /tmp/core-capture.json
   ```

   **Launcher binaries:** S60 runs either `fogcast-tenfoot` or `fogcast-kit` as
   the supervised child (`/run/fogcast-kit.pid`), depending on `launcher.json`.
   Evidence requires the child's running hash to match the overlaid binary. If
   only `fogcast-kit` is planned, the kit must be running the `fogcast-kit` child
   (not tenfoot). If both launchers are planned, the child must be the overlaid
   tenfoot, and the `kit_ui` line from `kit-command` must show that the overlaid
   `fogcast-kit` completed `--print-kit-ui` and selected `tenfoot`.

9. Generate evidence with the base build's `release.json`, the update capture,
   the hash outputs, the lease log, and one `--core-status core:NAME=FILE` per
   core, where FILE is the `core-capture` bundle. Pass
   `--host-lease-owner HOST_OWNER` whenever cores are present. In mixed plans,
   pass `--lease-owner KIT_OWNER` too. Every update capture must pass the same
   image checks and carry the same boot ID. With kit entries, that ID must also
   match the kit command. Evidence checks component coverage, file and running
   executable hashes, core package identity and state, image identity, boot
   identity, supervisor counts, and exact lease order.

10. Finally run `kit-deploy-script ...` with `off` under the same
    release/reacquire lease discipline to restore installed binaries.

```sh
python3 scripts/hil_plan.py evidence --repo . --base-image-commit "$BASE_IMAGE_COMMIT" \
  --head "$PR_HEAD" --base-image-sha256 "$BASE_LINUX_IMG_SHA256" \
  --base-release-json /path/to/release.json --kit-update-json /tmp/kit-update.json \
  --manifest /tmp/hil-manifest.json --kit-sha256 /tmp/kit.sha256 \
  --host-sha256 /tmp/host.sha256 --lease-log /tmp/lease.jsonl --lease-owner KIT_OWNER \
  --host-lease-owner HOST_OWNER --core-status core:NAME=/tmp/core-capture.json \
  --out /tmp/hil-evidence.md
```

All target API reads use the bearer token from the private FogCast config's
target `agent` token. Set it in `FOGCAST_TOKEN` without printing it; never print
or commit the token. Every curl uses the authenticated, failing form:

```sh
curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
  http://192.168.10.84:8182/v1/update > /tmp/kit-update.json
```
