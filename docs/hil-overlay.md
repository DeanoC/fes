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

For each `core:*` archive, the planner reads the local archive and derives its
package ID and core ID. The archive must identify the requested core. Host-server
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
   status` as `reacquired` (held by `HOST_OWNER`), capture update, status, then
   update again (commands below), and run the HIL test. Explicitly stop the host
   session with `POST /api/v1/session/stop`; save status (free or revoking) and
   record `released`.

8. **Core-only sequence:** confirm `kit.py status` is free and run
   `fogcast core-load` from the exact-head build. The host command takes its own
   lease through `fogcast-api`. While the core runs, save status (held by
   `HOST_OWNER`) and record `claimed`. Capture update, status, then update again;
   run the HIL test. Explicitly stop the host session, save status (free or
   revoking), and record `released`.

   **Binary-only sequence:** reacquire with a new `kit.py session`, record
   `reacquired`, capture `/v1/update`, then run the `kit-command` output on the
   kit and `host-command` output on the host. For `host:fogcast-api`, run the
   command as the user running that server after starting the exact-head binary.
   It scans `/proc/*/exe`, including deleted mappings. Run the HIL test under the
   lease, then release and record `released`.

   Capture commands for plans with core entries:

   ```sh
   fogcast --json --api http://127.0.0.1:8787 core-load /abs/path/core.fcore
   curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
     http://192.168.10.84:8182/v1/update > /tmp/kit-update.json
   curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
     http://192.168.10.84:8182/v1/status > /tmp/core-status.json
   curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
     http://192.168.10.84:8182/v1/update > /tmp/kit-update-after.json
   ```

9. Generate evidence with the base build's `release.json`, the update captured
   before status and (for core plans) the update captured after status, hash
   outputs, lease log, and one `--core-status core:NAME=FILE` per core. Pass
   `--host-lease-owner HOST_OWNER` whenever cores are present. In mixed plans,
   pass `--lease-owner KIT_OWNER` too. The two update captures must pass the same
   image checks and have identical boot IDs; with kit entries that ID must also
   match the kit command. Evidence checks component coverage, file and running
   executable hashes, core package identity and state, image identity, boot
   identity, supervisor counts, and exact lease order.

10. Finally run `kit-deploy-script ...` with `off` under the same
    release/reacquire lease discipline to restore installed binaries.

```sh
python3 scripts/hil_plan.py evidence --repo . --base-image-commit "$BASE_IMAGE_COMMIT" \
  --head "$PR_HEAD" --base-image-sha256 "$BASE_LINUX_IMG_SHA256" \
  --base-release-json /path/to/release.json --kit-update-json /tmp/kit-update.json \
  --kit-update-after-json /tmp/kit-update-after.json \
  --manifest /tmp/hil-manifest.json --kit-sha256 /tmp/kit.sha256 \
  --host-sha256 /tmp/host.sha256 --lease-log /tmp/lease.jsonl --lease-owner KIT_OWNER \
  --host-lease-owner HOST_OWNER --out /tmp/hil-evidence.md
```

All target API reads use the bearer token from the private FogCast config's
target `agent` token. Set it in `FOGCAST_TOKEN` without printing it; never print
or commit the token. Every curl uses the authenticated, failing form:

```sh
curl --fail -sS -H "Authorization: Bearer $FOGCAST_TOKEN" \
  http://192.168.10.84:8182/v1/update > /tmp/kit-update.json
```
10. Finally run `kit-deploy-script ...` with `off` under the same
    release/reacquire lease discipline to restore installed binaries.

```sh
python3 scripts/hil_plan.py evidence --repo . --base-image-commit "$BASE_IMAGE_COMMIT" \
  --head "$PR_HEAD" --base-image-sha256 "$BASE_LINUX_IMG_SHA256" \
  --base-release-json /path/to/release.json --kit-update-json /tmp/kit-update.json \
  --manifest /tmp/hil-manifest.json --kit-sha256 /tmp/kit.sha256 \
  --host-sha256 /tmp/host.sha256 --lease-log /tmp/lease.jsonl --lease-owner O \
  --out /tmp/hil-evidence.md
```
