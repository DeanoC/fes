# Kit software releases

Kit software is one immutable release containing the target agent, native
runtime, kit launcher and tenfoot launcher, plus their selected application
assets. The base OS owns the stable `fes-kit-software` supervisor. Installing a
release leaves the running services alone. Explicit activation queues a change
for the next released, idle kit boundary and persists that intent across reboot.

This path requires the supporting base image and an existing FESDATA3 ext4
volume. Adopting that image is an appliance update. Software installation never
creates or formats a partition. Core packages remain in their separate library;
installing or activating software does not rewrite their selections or settings.

## Prepare a bundle

For ordinary updates, use the [parent build command](#build-an-update-without-rebuilding-the-os)
to compile and package the complete software release from committed sources.

The producer is `sources/FogCast/cmd/fes-kit-bundle`. Its metadata declares the
board, architecture, required base contract, interface versions and provenance.
Its payload includes `bin/mister-agent`, `bin/mister-runtime`, `bin/fogcast-kit`,
`bin/fogcast-tenfoot` and the required assets beneath `share/`. The pack command
fills the closed file inventory with sizes, executable modes and SHA-256 digests.
A deterministic tar retains `manifest.json` and those exact members. Release
identity covers the complete canonical manifest, including its file digests.

From `sources/FogCast`:

```sh
go run ./cmd/fes-kit-bundle pack --manifest /absolute/path/release.json \
  --root /absolute/path/payload --output /absolute/path/kit-software.tar
go run ./cmd/fes-kit-bundle inspect --bundle /absolute/path/kit-software.tar
```

The supervisor verifies the installed bundle against the active base contract.
Equal host and kit Git revisions are not required. Retained earlier bundles are
recovery inputs subject to the same contract checks.

## Install and queue activation

Use the existing private FogCast host configuration and its recorded target ID.
The operator client verifies that target before sending a mutation:

```sh
go run ./cmd/fes-software --config /absolute/path/config.toml --action status
go run ./cmd/fes-software --config /absolute/path/config.toml --action list
go run ./cmd/fes-software --config /absolute/path/config.toml --action install \
  --bundle /absolute/path/kit-software.tar
go run ./cmd/fes-software --config /absolute/path/config.toml --action activate \
  --candidate INSTALLED_BUNDLE_ID --requester agent-task
```

Installation publishes the complete immutable bundle and returns its identity.
It does not select that bundle. Activation returns the durable operation status;
a queued response does not mean the services have restarted. The client prints
the generated operation ID before sending the request. Supply `--request-id` to
retain an explicitly chosen 64-character lowercase hexadecimal ID.

Activation and cancellation include an expected state generation. By default the
client reads that generation once before the mutation. `--expected-generation N`
uses an explicitly reviewed value. A competing operation returns a conflict;
inspect status rather than replace another agent's request.

```sh
go run ./cmd/fes-software --config /absolute/path/config.toml --action cancel \
  --request-id PENDING_OPERATION_ID --expected-generation N
```

Only a pending queued operation can be cancelled. A service replacement already
underway retains its maintenance ownership through completion or recovery.
Mutations are sent once. If a response is lost, inspect status and the release
inventory before repeating it. A matching completed receipt identifies the
outcome; a newer generation rejects stale changes. The client dials with a
10 second connect timeout and keeps `--timeout` (15 minutes by default) as
the whole-operation deadline. After the request body is written it waits at
most five minutes for response headers. Health, status, and list retry a
network failure up to three times, and each of those reads fails within
30 seconds. Install, activate, and cancel are not retried. A kit that still
does not answer reports that it is not ready.

## Activation and recovery

Queueing does not stop, take over or expire the current owner's session. That
owner can continue playing and renewing its lease. A soft Stop that retains the
lease still owns the kit; use ordinary explicit Stop/release to expose an idle
boundary. Once released cleanup finishes, the queued operation has priority over
new claims. It atomically enters target maintenance and confirms idle before any
service replacement.

The stable supervisor stops the complete old group and proves its processes
exited before starting replacements. Deleting a PID file or sending a signal is
not sufficient proof. It starts one selected bundle, checks the agent, runtime
and launcher, and completes the matching maintenance reservation only after the
startup checks pass. Accepted software remains selected and survives reboot.

The supervisor appends its own log and the stdout and stderr of the runtime,
agent and launcher to `/run/fogcast/software/supervisor.log`. The file rotates
once, at 2 MiB, to `supervisor.log.1`. The directory is root-only tmpfs, so a
stalled data disk cannot block the log or drop it. The same bytes are copied to
the supervisor process stderr. `start` puts that process in a new session and
points stdin, stdout and stderr at `/dev/null`, so a closed caller pipe cannot
deliver SIGPIPE. `run` ignores SIGPIPE for the same reason when stderr is a
pipe that closes.

Each service in the group receives `XDG_CONFIG_HOME=/media/fat/fogcast/state`
and `XDG_STATE_HOME=/media/fat/fogcast/state`. The supervisor creates that
directory when `/media/fat/fogcast` is already present. `launcher.json`,
`agent.toml` and `target-id` stay beside it. Tenfoot stores
`FogCast/tenfoot.json` and `FogCast/rooms/<id>.state.json` underneath it.
Home's last selected package is `package_id` in
`FogCast/rooms/example.fes-cores.state.json`. A state directory that cannot
be written is logged once; the launcher keeps running.

An accepted launcher that exits is restarted
without stopping the runtime or agent, so a menu crash does not leave a frozen
frame or drop an execution lease. An accepted agent or runtime exit restarts
the whole group automatically, including when an execution lease was active:
those processes are already gone, so the supervisor stops what remains, confirms
each exited process group on its own term and kill deadline, and starts the
runtime, agent and launcher again. The replacement does not replay the boot
admission credential. A durable activation that is still queued is passed into
the new agent's startup admission, so claims stay behind it. Applying,
recovering, or blocked maintenance stays on the reservation the new agent
reloads. The replacement remains in recovery until the agent and runtime are
idle and the launcher has been ready for ten uninterrupted seconds. If that
window does not arrive within sixty seconds, the supervisor stops the group and
retries with the same backoff. A group that dies quickly backs off from one
second to thirty seconds. If startup itself
fails, the supervisor stays up, records the error in that log, and retries with
backoff instead of exiting. A failure before the status socket is open retries
the whole startup; a failure after it is open keeps the socket serving the error.

Bundle verification completes before the service startup clock begins, while
the current group is still running. Each start checks that the verified tree
has not changed. Stopping services, starting replacements and observing ten
seconds of stable readiness share one sixty-second deadline, including startup
retries. Factory trial startup follows the same rule. Verification and elapsed
readiness do not require a working real-time clock on the kit.
The runtime must finish native initialization through its own authenticated
protocol socket before the agent or launcher starts. Clean idle permits normal
startup. The exact terminal `reboot_required` state with `idle_failed` in the
`recovery` phase permits diagnostic services to start inside maintenance, but
cannot pass readiness or reopen execution admission. A dependent service failure
therefore cannot terminate normal native initialization midway.

A failed startup permits one automatic attempt to restore the recorded prior
bundle. That attempt is recorded durably before the prior bundle starts. A
reboot cannot reset the attempt budget. Failed recovery or an unkillable old
runtime leaves explicit recovery-required status and keeps execution blocked.
Recovery changes only the matching software selection; it preserves the host,
base image, core library and unrelated deployments. Bundle updates must preserve
compatible configuration and persistent data formats for prior recovery.

During an appliance trial boot, the supervisor uses the factory software group
and preserves the data gate. After confirmation it reconciles persistent storage
and queued intent. A pending OS image change defers software activation. Software
installation joins appliance-operation draining; the stable supervisor also
serializes installation and activation and checks the appliance gate itself.
Queue and cancellation remain available while execution is deferred.

### Acknowledge a failed recovery

A failed automatic recovery stays fenced. Rebooting or restarting the supervisor
does not replenish its attempt budget. If the previous bundle's complete service
group is already running, an operator can explicitly acknowledge that group
without restarting it. Inspect the failed operation, selection generation, active
bundle, base identity and boot ID first. On the kit, as root, use a fresh action
identifier and those exact observed values:

```sh
fes-kit-software acknowledge \
  --operation FAILED_OPERATION_ID --generation OBSERVED_GENERATION \
  --selected ALREADY_RUNNING_PREVIOUS_BUNDLE_ID \
  --base RUNNING_BASE_IMAGE_SHA256 --boot CURRENT_BOOT_ID \
  --action FRESH_64_CHARACTER_LOWERCASE_HEX_ID
```

This command verifies the bundle, running supervisor and its complete service
group, then requires ten seconds of native idle and launcher readiness. It resumes
and completes only that operation's existing maintenance reservation. A separate
durable acknowledgment retains the original failure and operator action identity;
selection and generation remain unchanged. It neither starts a service nor
retries the candidate. Missing services, changed identities, another operation or
failed readiness leave execution blocked. Inspect status after an uncertain
reply; repeat only the same action and exact identities to reconcile it.

If the previous service group is absent or unhealthy, this command cannot repair
it. Preserve the journal and plan a separate operator recovery rather than editing
selection state or resetting the automatic recovery phase.

### Prepare a required physical power cycle

If the exact running native runtime reports `reboot_required`, prepare the failed
operation **before** the documented hard power-supply cycle:

```sh
fes-kit-software prepare-power-cycle \
  --operation FAILED_OPERATION_ID --generation OBSERVED_GENERATION \
  --selected COMMITTED_PREVIOUS_BUNDLE_ID \
  --base RUNNING_BASE_IMAGE_SHA256 --boot CURRENT_BOOT_ID \
  --action FRESH_64_CHARACTER_LOWERCASE_HEX_ID
```

This root-only action verifies the native runtime and existing reservation
without retrying cleanup. It preserves the original failure in a durable audit,
cancels that failed software request, and leaves the current boot fenced in its
existing maintenance reservation. It does not release maintenance, restart a
service, change the committed selection or initiate a reboot. Its result is
`prepared-awaiting-physical-cycle`, not successful recovery.

The operator must then perform the [documented hard power-supply
cycle](../sources/FogCast/docs/DEVELOPMENT.md#restarting-target-services).
After boot, verify the actual base, committed/active bundle, generation, native
idle readiness and free admission. A changed boot ID alone does not prove power
removal. Preserve any newer queued operation; preparation and delayed retries
cannot cancel it or restore an older selection. If another update activates,
report its new generation instead of treating the earlier selection as current.

## API and evidence

All network routes use the target agent's existing bearer authentication and
proxy to the fixed root-only supervisor socket at `/run/fogcast/software.sock`.
They do not require an execution lease:

| Route | Effect |
| --- | --- |
| `GET /v1/kit/software` | Active, selected, pending and completed operation state |
| `GET /v1/kit/software/releases` | Installed immutable bundle inventory |
| `POST /v1/kit/software/releases` | Install a bounded `application/octet-stream` tar |
| `POST /v1/kit/software/activate` | Queue `{id,candidate,requester,expected_generation}` |
| `POST /v1/kit/software/cancel` | Cancel `{id,expected_generation}` while queued |

Authenticated health has a separate `software` object with `active_bundle`,
`base_identity` and supervisor state. Existing sealed image `artifacts` retain
their base-image provenance. Public health omits software queue details.

The supervisor owns the entire group lifecycle. Managed diagnostics use a private host and an explicitly installed software
bundle. File-swap, baseline and restore commands refuse managed images before
staging files, taking maintenance or stopping services. Returning to an earlier
installed bundle is another explicit activation with the current generation;
there is no automatic stock restoration after a successful update. On a shared
kit, when the task installs a bundle, install the PR bundle on the running
image. After use, if the booted image is not the baseline named in the
Powerboat status file, restore that image through the appliance update or
rollback path and verify the boot before activating software. Then explicitly
activate the designated baseline bundle against the current generation, as
[kit sharing](kit-sharing.md) requires. That activation does not change the
booted image and leaves the successful selection installed. Verify the active
bundle before logging the kit restored. A core-only run that does not
change the bundle does not install one. Do not cancel another owner's pending activation to
clean up. Temporary configuration-only overlays are not supported on managed
images. [Kit sharing](kit-sharing.md) describes execution ownership;
[appliance releases](appliance-releases.md) cover OS changes and boot identity.
Software tests establish host behavior. They do not establish card power-loss
behavior, physical startup, HDMI/input/media behavior or exact-artifact kit
acceptance for a new bundle.

## Build an update without rebuilding the OS

On Linux amd64, retain the verified `linux.img` and its reviewed SHA-256 from the
appliance release manifest. Keep the matching verified SDK cache from that image
build, or set `FES_TARGET_IMAGE_SHARED_CACHE` to its existing absolute cache path.
Commit the new software sources, then run:

```sh
make kit-software \
  KIT_SOFTWARE_BASE=/absolute/path/linux.img \
  KIT_SOFTWARE_BASE_SHA256=REVIEWED_IMAGE_SHA256 \
  KIT_SOFTWARE_VERSION=kit-2026.10.16
```

The command freezes the committed FES HEAD, builds the agent and both launchers
with the selected Go version, and builds the runtime with the pinned container
and verified cached ARM SDK. It copies application assets from that same source
snapshot. It never runs an OS image build or an FPGA package producer. A missing
or mismatching SDK fails explicitly; qualify its base image first.

The image hash is required: an extracted filesystem or a self-authored base
receipt is not a substitute for the reviewed image identity. The command hashes
a private image copy before extracting the factory and system-library trees,
then validates factory integrity and the new software's architecture, dynamic
library dependencies and versioned symbols against the retained base contract.
The supplied image stays unchanged.

The default output is the new directory `out/kit-software/<version>/`, containing
`bundle.tar` and `receipt.json`. Override the directory with
`KIT_SOFTWARE_OUTPUT=/absolute/new-release-directory`. Existing output directories
are never overwritten, including when two builders compete. Failed builds leave
prior outputs intact. The receipt closes the bundle digest, file inventory,
selected source and tree identities, base image/factory/ABI identities, Go
version, and SDK/container identities. It explicitly records `deployed: false`.

Install `bundle.tar` through the software-release client and request activation
separately. Build receipts do not establish hardware acceptance. If the retained
base cannot satisfy the new software's ABI, change its dependencies or plan an
explicit base-image upgrade. Host software and installed core packages remain
independently managed.

For producers that already have a closed payload, the lower-level
`image/scripts/kit-software.py --base-root ... --payload ... --output ...
--version ... --fogcast ... --runtime-revision ...` command remains available.
Its caller is responsible for authenticating the extracted base against the
reviewed original image; prefer the parent command for ordinary updates.
