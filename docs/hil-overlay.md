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

For each `core:*` archive, provide its sealed 64-hex package ID with a
repeatable `--package-id COMPONENT=ID` option. Host-server changes require the
binary from the exact-head build to be started as `fogcast-api` before the
host attestation command is run.

5. Claim the lease from `sources/misteross` and keep the session open in its own terminal (it renews every 20 s). From a second terminal, save the public lease status (`kit.py status` prints only public fields, never the credential) and record it as `claimed`. Then send `stop` to the session so the kit is idle:

```sh
python3 scripts/kit.py --config CFG session --owner O --purpose P      # terminal 1, keep open
python3 scripts/kit.py --config CFG status > /tmp/lease-claimed.json   # terminal 2
python3 ../../scripts/hil_plan.py lease-record --log /tmp/lease.jsonl --step claimed \
  --status-json /tmp/lease-claimed.json --owner O
```

6. An agent restart invalidates any held lease, so release before deploying: send `release` to the session, save `kit.py status` again, confirm `"state": "free"`, and record it as `released-for-restart`. Stage the kit files in tmpfs under `/run/hil-<head8>` (with `SHA256SUMS` from `--sha256sums`), then run the generated deploy script with `on`:

```sh
python3 scripts/hil_plan.py kit-deploy-script --manifest /tmp/hil-manifest.json --stage-dir /run/hil-pr \
  > /tmp/hil-deploy.sh
python3 scripts/hil_plan.py kit-deploy-script --manifest /tmp/hil-manifest.json --stage-dir /run/hil-pr \
  --sha256sums > /tmp/SHA256SUMS
KH=$(mktemp); SSH="sshpass -p 1 ssh -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$KH -o PubkeyAuthentication=no root@192.168.10.84"
$SSH 'mkdir -p /run/hil-pr'   # then copy each binary and SHA256SUMS to /run/hil-pr over the same $SSH (cat > file)
$SSH 'sh -s on' < /tmp/hil-deploy.sh; rm -f "$KH"
```

The deploy script checks every staged binary against the sha256 embedded in it, refuses if `/usr/sbin` already has mounts, stops S60, S50, S40, bind-mounts the exact staged binaries over `/usr/sbin`, starts S40, S50, S60 only when no `mister-supervise` is left running, and fails unless exactly one supervisor each runs for runtime, agent and kit.

7. For a core-only overlay, claim the lease and keep it held while loading the
   core from the exact-head build on the host:

   ```sh
   fogcast --json --api http://127.0.0.1:8787 core-load /abs/path/core.fcore
   curl http://192.168.10.84:8182/v1/status > /tmp/core-status.json
   curl http://192.168.10.84:8182/v1/update > /tmp/kit-update.json
   ```

   Save status while that core is running, then stop the core and release the
   lease. Record exactly `claimed` (held by the owner) followed by `released`
   (free or revoking). Core evidence checks status package ID and active state,
   plus kit image identity. It does not require a reboot or the four-step binary
   restart sequence.
8. For kit binary overlays, reacquire the lease with a new `kit.py session`
   (terminal 1), save `kit.py status` and record it as `reacquired`. Save
   `GET http://192.168.10.84:8182/v1/update` as `/tmp/kit-update.json`. Run the
   `kit-command` output on the kit (`$SSH sh -s < kit-cmd.sh > /tmp/kit.sha256`,
   with a fresh throwaway known_hosts) and the `host-command` output on the host
   (`> /tmp/host.sha256`). For `host:fogcast-api`, run `host-command` as the
   user running `fogcast-api`, after starting that server from the exact-head
   binary. It records file hashes and scans `/proc/*/exe`, including deleted
   executable mappings, so an old server still running a replaced binary fails
   attestation. `host:fogcast` is a one-shot CLI exec'd per invocation, so it
   needs only the file hash. Run the HIL test under the lease. Then send
   `release`, save `kit.py status`, and record it as `released`.
9. Generate evidence with the base build's `release.json`, kit update JSON,
   kit and host hash outputs, lease log, and one `--core-status
   core:NAME=/tmp/core-status.json` for every core entry. Evidence checks
   per-component coverage, file and running executable hashes, core package
   identity and running state, image identity, boot identity, supervisor counts
   and lease order.
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
