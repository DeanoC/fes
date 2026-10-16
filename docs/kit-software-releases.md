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
outcome; a newer generation rejects stale changes.

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

Bundle verification completes before the service startup clock begins, while
the current group is still running. Each start checks that the verified tree
has not changed. Stopping services, starting replacements and observing ten
seconds of stable readiness share one sixty-second deadline, including startup
retries. Factory trial startup follows the same rule. Verification and elapsed
readiness do not require a working real-time clock on the kit.

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
there is no automatic stock restoration after a successful update. Temporary
configuration-only overlays are not supported on managed images. [Kit sharing](kit-sharing.md) describes execution ownership;
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
