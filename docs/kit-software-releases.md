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

The supervisor and the authenticated target routes in
[API and evidence](#api-and-evidence) are in this revision. The operator client
`sources/FogCast/cmd/fes-software` is not. It arrives in the follow-up releases
change. Until that client is present, operators use those routes. The commands
in this section are that client's invocation, so the request fields match
those routes; they are not a program in this revision.

When that client is available, it uses the existing private FogCast host
configuration and its recorded target ID, and verifies that target before
sending a mutation:

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
a queued response does not mean the services have restarted. That follow-up
client prints the generated operation ID before sending the request. Supply
`--request-id` to retain an explicitly chosen 64-character lowercase hexadecimal
ID.

Activation and cancellation include an expected state generation. By default that
client reads the generation once before the mutation. `--expected-generation N`
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

## Package an update without rebuilding the OS

Retain an extracted, verified appliance root as the base. Build the four new ARMv7
executables and place them under `payload/bin/` with their deployed names
(`mister-agent`, `mister-runtime`, `fogcast-kit`, `fogcast-tenfoot`). Put required
application assets, including the launcher defaults `fogcast/config.toml`, under
`payload/share/`. Assets embedded in Go executables need no separate copy. Build
the selected FogCast's `build-fes-kit-bundle-container` helper; its adjacent
`kit-software.revision` records the selected source revision.

```sh
python3 image/scripts/kit-software.py \
  --base-root /path/to/verified-extracted-root \
  --payload /path/to/new-payload \
  --output /path/to/kit-update.tar \
  --version kit-2026.10.15 \
  --fogcast /path/to/selected/FogCast \
  --runtime-revision FULL_RUNTIME_SOURCE_SHA
```

This Linux/amd64 packaging command verifies the retained factory bundle and base
contract, checks the new executables' ARM ABI, interpreter, dynamic dependencies
and versioned symbols against that base, then publishes a deterministic archive.
It does not modify the extracted base or payload, rebuild the image, select the
release, or contact a kit. A failed build preserves an existing output archive.
Upload the resulting archive through the software-release install command, then
request activation separately. If the base ABI does not satisfy the new software,
change its dependencies or plan an explicit base-image upgrade.
