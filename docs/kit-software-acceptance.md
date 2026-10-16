# Kit software API acceptance

`python3 scripts/kit_software_acceptance.py` runs an explicitly authorized diagnostic against
one configured target. It installs a frozen software bundle while holding its
own renewable kit lease, checks queueing and cancellation, then releases that
lease to observe activation. Successful software remains selected. It does not
build software, take over a foreign lease, restore a previous selection or
repair failed recovery.

This is an API lifecycle diagnostic. Actual game launch, input delivery and
HDMI observations use the separate [package acceptance](package-acceptance.md)
path. Skipped optional stages stay `skipped`; a successful default run does not
claim overall hardware acceptance.

## Default run

Use the designated kit only with its existing hardware authorization and
[kit-sharing policy](kit-sharing.md). The configured target must be enabled,
selected and uniquely match the independently known target ID. It must already
be ready, idle and free, with a confirmed base and no pending software or
maintenance operation. The command refuses conflicting ownership.

Prepare the immutable bundle with [kit software releases](kit-software-releases.md).
Use its canonical FES archive, bundle ID and archive SHA-256. The runner snapshots
and hashes the archive and checks that its first `manifest.json` member hashes
to the expected bundle ID before contacting the target. The target still performs
complete format, payload and compatibility validation. Active software must
provide the supported runtime, maintenance and software-admission contracts;
provenance stays bound by manifest identity, and source revisions need not match.

```sh
python3 scripts/kit_software_acceptance.py \
  --config /absolute/private/config.toml \
  --expected-target-id AUTHORIZED_TARGET_UUID \
  --expected-base-image-sha256 RUNNING_CONFIRMED_IMAGE_SHA256 \
  --expected-bundle-id CURRENT_ACTIVE_BUNDLE_ID \
  --candidate CANDIDATE_BUNDLE_ID \
  --bundle /absolute/path/bundle.tar \
  --expected-bundle-sha256 BUNDLE_ARCHIVE_SHA256 \
  --evidence-dir /absolute/path/new-run \
  --execute
```

`--help` performs no network operations. Without `--execute`, execution is
refused; that refusal is not a compatibility dry-run. Inputs and the evidence
directory must be new/frozen for each run. An existing evidence directory is
refused. The default overall deadline is 600 seconds, followed by bounded cleanup;
`--timeout` may explicitly select up to 1,800 seconds.

The default checks:

- A competing independent HTTP client cannot claim the held lease. This does
  not represent two physical workstations.
- Installation changes neither the running selection nor owned lease generation.
- An exact duplicate activation is idempotent; conflicting IDs, changed request
  bodies and stale generations are rejected.
- An idle Stop retains the owned lease and leaves activation queued. This check
  does not launch a game or claim sofa-controller behavior.
- Exact cancellation commits without changing selection or generation.
- A fresh activation queued by a separate real process survives that process's
  successful exit while the original execution owner retains its grant.
- After owned release, the exact accepted receipt, selection, next generation,
  unchanged base/boot, readiness, idle runtime and free admission remain stable
  for ten seconds.

The current release may be supplied as the candidate for a repeatable restart
check. Its receipt says `activation_kind: reselect_current`; it does not establish
an update to changed software bytes. A differing candidate records
`change_selected`. No older release is automatically selected merely to make a
repeat run possible.

Where supported, `GET /v1/core-packages` inventory identities and selections are
compared before and after. This proves neither stored-byte persistence nor core
execution. An unavailable library fails the check; an absent endpoint is recorded
as skipped.

## Optional failed-startup recovery

Also supply all three options:

```text
--recovery-candidate EXPLICIT_FAILING_BUNDLE_ID
--recovery-bundle /absolute/path/failing-bundle.tar
--expected-recovery-sha256 FAILING_ARCHIVE_SHA256
```

The operator supplies and authorizes the deliberately failing bundle. The runner
never manufactures one or changes a production binary. After default activation,
it installs that bundle behind a fresh owned lease, queues it and releases only
that lease. Passing requires an exact `recovered` receipt, preserved committed
selection/generation/base/boot and ten seconds of stable ready/idle/free state.
Observed phases and one matching receipt are evidence; they do not count every
process start. A blocked or failed recovery is a failure requiring inspection,
not permission for another attempt, reboot or automatic restoration.

## Optional queue persistence across reboot

`--reboot-command-file /absolute/private/reboot.json` explicitly authorizes one
external operator command for the same target. The private, non-symlink file is
a bounded JSON argv array, for example a separately configured SSH command using
an SSH agent/key. The runner supplies no password, SSH fixture or shell. It uses
`subprocess` with `shell=False`, discards command output and records the command
file's SHA-256 and outcome. The operator must approve the exact command and kit;
the runner cannot infer an arbitrary command's effects or target.

After default activation, the runner claims anew, promotes only its grant into
maintenance and waits for its exact reservation to become ready. It queues an
explicit reselection of the current accepted release, persists an attempt record,
then invokes the command once with a bounded deadline. Every subsequent reboot
observation is read-only, including command timeout, nonzero exit or spawn failure.
The command is never retried. Passing requires one changed boot ID, unchanged
base, the exact accepted queue receipt and next generation, and ten seconds of
ready/idle/free state. This is queue persistence across an operator-requested
reboot, not proof of physical power removal.

Before promotion, `private/maintenance-promotion.json` durably stores the exact
request, original execution credential and boot/owner identities. After a grant
arrives, `private/maintenance-grant.json` stores its reservation credential. Both
are mode 0600 in a mode 0700 directory. **Do not publish the private directory.**
It is recovery material, not another coordinator or an execution entitlement on
a newer boot.

If a reboot did not occur, inspect current target/base/boot, queued operation and
maintenance identities first. An operator can use the existing APIs, with secrets
read from the private records rather than shell arguments:

1. If promotion's reply was lost, reconcile only that exact
   `POST /v1/kit/maintenance/start` request with its original execution credential.
   The existing API returns the same grant for the same request. Do not invent a
   new request or take over another reservation.
2. Cancel only the recorded still-queued software request with its recorded
   generation through `POST /v1/kit/software/cancel`.
3. Use the recorded reservation credential with matching `operation_id` and
   `expected_generation` at `/v1/kit/maintenance/resume` if needed, then
   `/v1/kit/maintenance/end` once ready. Both recheck the existing lifecycle.

A changed boot, selection, operation or reservation forbids replaying old
credentials. Preserve newer intent. These are explicit operator actions; this
runner never performs them after its reboot-attempt boundary.

## Optional local-admission probe

`--local-command-file /absolute/private/local-probe.json` opts into another
bounded JSON argv command. It runs while the execution lease is held, before
queueing software. The operator-provided on-kit probe should attempt the existing
root-only local-control launch and print only:

```json
{"http_status":409,"error":"in_use"}
```

`blocked` is also accepted. The runner requires exit zero, bounded JSON output,
and preserved owned generation/selection before and after. It retains only this
fixed projection and the command-file digest. This is external busy-admission
evidence; it does not establish a successful local launch or local core execution.
No local probe is synthesized and no kit helper is installed automatically.

## Failure and evidence

Mutations are sent once. The duplicate-ID test deliberately sends the same
request twice; it is not an automatic transport retry. A lost cancellation reply
is reconciled by GET and the exact durable cancelled receipt. Cleanup never
retransmits an uncertain cancellation, releases into an unresolved operation,
cancels another request, or changes software selection. Immediately before release
it checks owned lease generation, base/boot, selected software/generation and the
exact still-queued request again. These observations are not an atomic transaction
across every client; target admission remains authoritative.

Failure can leave durable intent or held maintenance requiring inspection. A
lost claim/promotion response is not proof that the operation did not commit.
`cleanup-unresolved` means do not assume the kit is available. The command never
keeps an ordinary lease alive indefinitely: its natural expiry still follows the
existing agent policy. Inspect pending intent before another run.

The evidence directory contains frozen archives and mode-0600 JSON observations,
per-stage passed/skipped results and a final `*-result.json` or failure record.
Public projections exclude bearer credentials, lease/reservation tokens, private
configuration and arbitrary remote/command error bodies. Preserve private recovery
records separately. Input and HDMI remain skipped here; link independently
classified evidence from the standard package runner and capture workflow.
