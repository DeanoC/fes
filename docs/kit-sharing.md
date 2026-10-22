# Sharing a development kit

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

## Development host protocol

Keep the operator-selected shared host and its autostart setting unchanged.
Coding, builds, simulations and package preparation do not require stopping it.
For automated diagnostics use a private host with its own binary, configuration,
port, catalog and evidence directory through the [package acceptance
lane](package-acceptance.md). Do not replace the shared host or mount its live
library to test a branch. Stop only the private host you started.

Several idle hosts may inspect the same kit. The target lease admits one
execution owner; a competing launch must report busy without stopping another
session. Do not run two controllers for the same owned session. Keep the normal
menu available between probes and during long builds. Launcher changes on a
managed image use a bundle update; do not disable the shared host as routine
cleanup.

Claim the kit in its Powerboat status file before use and log the result
there when you finish. There is no separate Powerboat host claim. That
file is the single source of truth. GPU jobs follow the
[Shared GPUs](../AGENTS.md#shared-gpus) section. Do not open a Slack
reservation, and do not require a person to be present. The target lease
remains the execution lock for ordinary use:
loading a core, supplying media/input, collecting diagnostics and stopping
the session. It does not replace the status-file claim.

Reimage, an image or boot-config change, killing other people's processes,
and closing PRs need Deano's explicit OK. Deleting old images (keep the
running image and the previous one), an SD reburn, and bundle
install-then-restore are already approved. Never delete a factory or
known-good rollback image. Before an SD reburn, verify the device path is
that kit's SD card. Coordinate disruptive maintenance such as replacing or
restarting shared services. Coordinate the managing
service, not just its PID: restart policies can undo a process kill. Slack
is not a push notification channel for this harness; poll a thread when a
reply is needed. Coordination does not replace the status-file claim or the
lease, and it does not authorize another device.

Managed kit software is installed as an [immutable bundle](kit-software-releases.md)
and activated at the next released idle boundary. Install the PR bundle on the
kit's running image; a userspace bundle does not replace that image. Use
private hosts for branch diagnostics. Managed images reject temporary
binary/configuration swaps. After use, restore the designated baseline the
status file names and log that restore. Bundle restore is an explicit
activation of the baseline bundle against the current generation. That
activation does not change the booted image. The normal path installs the
PR bundle on the running image and leaves that image in place. Software goes
before the image: before an image change, activate a bundle the new image
accepts ([image compatibility](kit-software-releases.md#image-compatibility)).
If the booted image is not the named baseline, first activate the baseline
bundle if the running image accepts it, then restore the image through the
[appliance update or rollback](appliance-releases.md) path and verify the
booted image. Otherwise restore and verify the image, then activate the
baseline bundle. Verify the active selection before logging the kit
restored. Do not log a bundle activation as an image restore. Bundle
install-then-restore is pre-approved. [Kit software releases](kit-software-releases.md)
keep a successful selection installed and do not restore a stock bundle
automatically. This post-use bundle step is that explicit activation. It
is not a file-swap restore, and it does not cancel another owner's pending
activation. The stable supervisor owns the complete service group.

Host, agent and runtime revisions may differ when their advertised contracts are
supported. Diagnostics pin the active bundle and base-image digests; sealed
base-image runtime provenance does not identify the active bundle. Record each
exact artifact independently. A new core using a supported
ABI uses package import/selection/launch rather than a kit-file swap or OS update.
An appliance update is the designated baseline only when the status file names
it. After use, restore the baseline image and bundle that file names.

## Claim a development session

From `sources/misteross`, using your private FogCast configuration:

```sh
python3 scripts/kit.py --config /absolute/path/config.toml status
python3 scripts/kit.py --config /absolute/path/config.toml session \
  --owner agent-name --purpose 'OSS bring-up'
```

The session accepts these commands on stdin:

```text
load /absolute/path/to/core.rbf
stop
status
load /absolute/path/to/next-core.rbf
release
```

`stop` returns hardware to idle and retains ownership. `release`, EOF, or normal
process exit attempts cleanup and release. Repeat uploads in the same session;
compiling does not claim the kit. The CLI streams files through the existing
FogCast development-RBF endpoint; it does not change artifact/build recipes.

The normal FogCast host shares one lease between launches and remote input.
Explicit user Stop (empty-body `POST /api/v1/session/stop`) releases it after
cleanup. Sofa Soft-stop (tenfoot now-playing B, with `retain_lease: true`)
keeps the grant after that cleanup. Stop used when replacing a game retains
it. A client without ownership cannot claim merely to Stop another
session. Losing renewal disables that client's mutations rather than taking
the kit back automatically. Keep the host running and inspect current ownership
with `GET /v1/kit/lease`; host health (`GET /api/v1/health`) reconciles the
selected target identity and discards stale local authority without stopping
or releasing the replacement owner's session.

While another session holds the kit, leave it running. Lease-denied/busy
responses are not a reason to restart services, restore software or take over.
Once the kit is free, reconcile health/session status and issue one new explicit
Launch. The same host process can claim a new lease and create a new flight and
input stream; it does not replay the expired launch. A host restart is not a
normal lease-recovery step. Supported API/ABI contracts, rather than matching
host/kit source commits, determine software compatibility.

Keep the old session/flight/generation binding on delayed Stop requests. They
must not be rebound to a newer play. If a mutation's outcome is unknown, inspect
current session and ownership before deciding the next action; do not blindly
retry the mutation or infer ownership from the package ID alone.

## Expiry and operator override

A lease lasts **90 seconds** and clients renew every **20 seconds**. If an agent
crashes, disappears or stops renewing, the target revokes the lease. Its input
streams close, admitted operations finish, and input/hardware cleanup runs
before another owner can claim. The target uses monotonic time; relative expiry
in responses handles kits without a working real-time clock.

For an agent that remains alive and keeps renewing, or after fixing failed
cleanup, inspect status and explicitly take over using its current generation:

```sh
python3 scripts/kit.py --config /absolute/path/config.toml status
python3 scripts/kit.py --config /absolute/path/config.toml takeover \
  --owner operator-name --purpose 'recover kit for development' \
  --expected-generation GENERATION_FROM_STATUS \
  --reason 'previous agent is no longer responding'
```

Takeover uses the existing configured bearer credential and enters the same
interactive session. It never interrupts FPGA programming midway. A changed
generation rejects a stale takeover request; inspect again instead of blindly
forcing it. Delayed Stop, release and input requests from the previous owner
cannot affect the replacement lease.

Cleanup failure leaves status `blocked` with a recovery reason. Repair the
underlying problem, then use explicit takeover to retry cleanup. Reboot remains
an operator maintenance option for either designated test kit. Kits A and B
are interchangeable. Agent restart
invalidates old credentials and reconciles/cleans hardware before accepting a
new claim. An abandoned takeover request does not reserve a successfully
cleaned, free kit forever.

## API and boundaries

All routes use the existing bearer authentication:

| Operation | Target route |
| --- | --- |
| Public ownership status | `GET /v1/kit/lease` |
| Claim with owner, purpose and secret request ID | `POST /v1/kit/claim` |
| Renew / release current credential | `POST /v1/kit/renew`, `POST /v1/kit/release` |
| Explicit generation/reason takeover | `POST /v1/kit/takeover` |
| Maintenance status | `GET /v1/kit/maintenance` |
| Promote an owned grant into maintenance | `POST /v1/kit/maintenance/start` |
| Resume/end the exact reservation | `POST /v1/kit/maintenance/resume`, `POST /v1/kit/maintenance/end` |

Mutation requests carry `X-FogCast-Kit-Lease`. Status reports owner, purpose,
generation, remaining milliseconds and reason; credentials are not included.
Claim/takeover retries use the same random request ID to reconcile a lost
response. Cleanup may still be running when release returns `revoking`. Maintenance
reservations have separate operation/generation identities and survive agent
restarts on the same boot. Ordinary claim, takeover and mutation are refused
until matching maintenance completion has confirmed idle. Failed maintenance
stays reserved for explicit repair; it does not expire into another owner's play.

Lease admission covers game launches, development uploads/reboots, Stop, input
attach/detach/streams, session display open/close, cast start/stop, and mesh content pull and link. Mesh
content pull acquires the session grant: a fresh session's first pull claims a
free kit, and a kit held by another session fails closed. Mesh content link
requires that grant. A grant claimed for that ensure is released when Ensure
does not start execution. A grant the session already held, including after
Soft-stop, stays held. Node, slot, and source reads stay available to other
clients, as do status and content-cache operations. A batch slot read is
`GET /v1/mesh/content/slots`. Revocation interrupts stalled HTTP uploads
and input streams, while the existing coordinator retains already-dispatched
physical operations until completion.

Direct SSH/Main FIFO programming, JTAG and unrestricted runtime-socket access
bypass agent enforcement. `make program` is explicitly a maintenance bypass.
Use `kit.py` for ordinary native bring-up and coordinate direct maintenance
with the current owner. A workstation file lock cannot enforce sharing across
hosts; no second lock service or misteross ownership database is introduced.

## Validation

The [same-process recovery diagnostic](validation/2026-10-17-same-process-host-recovery.md)
records the current explicit-relaunch workflow on its named host binaries and
installed bundle. Earlier lease diagnostics below apply to their recorded
artifacts; they do not identify the currently selected kit software.

The state-machine, HTTP, input, agent, host and public session tests include
concurrent claims, expiry with an admitted operation, stale cleanup/takeover,
failed cleanup, retained input neutralization, stalled uploads, real TCP input
stream closure, shutdown ordering, and unsynchronised target clocks. The Python
client tests exercise actual local HTTP transfers, renewal loss and takeover.

On the designated kit, two development loads retained one owner across Stop.
Competing claims, unowned/foreign Stop, input and upload requests were rejected.
Operator takeover cleaned an active development core; delayed old-owner Stop
and release were rejected. An abandoned Pong lease expired after 90 seconds,
returned hardware to idle and allowed a new claim without rebooting. The host
then passed automatic renewal and Pong/Stop/SNES/Stop/Pong/Stop, releasing each
lease. The normal host service also passed Pong launch/Stop after installation.
A real CLI session loaded a development RBF, stopped it and released ownership.

Exact diagnostic binaries, source archives, test logs and hardware results are
retained in `out/kit-lease-diagnostic/`. Hardware classification is diagnostic;
it does not select or publish parent component revisions.

Those earlier diagnostics used target image SHA-256
`80f6d37dfcae4c05dc615367200352defe1df3900e1e4b20e8415e0df38a316d`.
Their final host binary SHA-256 is
`7004094ae2bb74a7f4c70259b684f78fcd5f42203140d391dd71b114e4e6ae39`.
The initial and reviewed host binaries in the evidence directory were superseded
by `fogcast-api-final`; `host-final.json` identifies the tested source snapshot.
The previous installed executable is preserved as
`out/kit-lease-diagnostic/previous-installed-fogcast-api`.
