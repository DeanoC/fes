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
board, architecture, required base contract, interface versions, including the
[menu interfaces](#image-compatibility) its runtime admits, and provenance.
Its payload includes `bin/mister-agent`, `bin/mister-runtime`,
`bin/fogcast-tenfoot` and the required assets beneath `share/`, including the
[fes-window kernel driver](../image/kernel/fes-window/README.md) at
`share/fes-window/` that the runtime loads on start. It also carries
the `bin/fogcast-kit` stub, which answers `--print-kit-ui` with `tenfoot` for
bases whose supervisor still probes it. The pack command
fills the closed file inventory with sizes, executable modes and SHA-256 digests.
A deterministic tar retains `manifest.json` and those exact members. Release
identity covers the complete canonical manifest, including its file digests.

From `sources/FogCast`:

```sh
go run ./cmd/fes-kit-bundle pack --manifest /absolute/path/release.json \
  --root /absolute/path/payload --output /absolute/path/kit-software.tar
go run ./cmd/fes-kit-bundle inspect --bundle /absolute/path/kit-software.tar
```

The supervisor verifies the installed bundle against the active base contract
and the image's menu requirements. Equal host and kit Git revisions are not
required. Retained earlier bundles are recovery inputs subject to the same
checks.

## Software base ABI targets

The bundle ABI validator accepts the existing DE10 `armv7` ELF32/EABI target
and DE25 `aarch64` ELF64 target. Update validation selects the board from the
already verified base contract and checks every executable and base library
against that board's ELF architecture. A DE25 base must contain the exact
regular `/etc/fes-board` identity file. Its architecture participates in the
ABI digest, so equal symbol inventories on the two architectures produce
different contracts; existing DE10 ABI identifiers remain unchanged.

Only ELF objects contribute runtime-library interfaces. GNU linker scripts
and debugger helpers that also match `*.so*` are excluded; a dynamic dependency
on an excluded file is unresolved and refused. Library/symbol closure and the
existing ban on embedded runtime search paths apply to both targets.

This adds AArch64 ABI validation to the existing bundle-update packer. The
standalone cross-build producer and factory-image assembler still select DE10;
DE25 SDK selection, factory bundle assembly, physical runtime/MENU binding and
an actual managed update remain integration work. No daemon admission or
supervisor compatibility rule is relaxed by this change.

## Image compatibility

A bundle runs only on a base image it fits. Board, architecture, base ELF
contract and the base interfaces must match exactly. The image also records
the interfaces its sealed menu package declares in the `requires` object of
`/etc/fes/software-base.json`, as `menu.<interface>` keys with `major.minor`
values. The runtime counts every interface the menu declares, so optional ones
are required too. Beside its base interfaces, a bundle manifest lists the
`menu.<interface>` versions its runtime admits. The packer reads them from the
runtime binary's `.fes_menu_interfaces` section, which
`sources/libmister-runtime/src/native/menu_admission.hpp` defines next to
`CheckMenu`. A bundle accepts a required interface when it lists the same major
version and at least the required minor. Supervisors that predate these keys
ignore them, so a newer bundle still installs and runs on an older image. The
image build fails when its own factory runtime does not accept its menu.

The base image's supervisor checks this at install, activation, boot and
recovery:

- Install and activation refuse a bundle the image cannot run.
- At boot and in recovery, a selection the image cannot run is held back. The
  image's own factory software starts in its place instead of blocking. The
  selection stays recorded for an image that can run it, and
  `GET /v1/kit/software` reports `held_back` and `held_back_reason`. A
  selection built for another base contract is held back the same way after
  an image change.
- A queued activation of such a bundle never starts it and spends no
  recovery attempt. It completes as `recovered` with a `held back:` reason and
  leaves the running group alone. If the factory software is still running
  after an image change, the selection starts first while the request stays
  queued, so an interrupted start leaves the next boot to start it. Only a
  selection that then fails to start blocks.

Running the factory software in place of a held-back selection is a deliberate
exception to the no-fallbacks rule in `AGENTS.md`, approved by Deano on
2026-10-22.

### Software before the image

Change software first. Before an image update, install and activate a bundle
that both the running image and the new image accept, normally one built from
the new image's sources. `fes-update --action update` reads the new image's
`/etc/fes/software-base.json` before uploading anything. It refuses the image
when the kit's selected bundle does not accept every interface that image
requires. A new base contract cannot go first, because no bundle installs on
the old base and runs on the new one. In that case `fes-update` reports that
the image will hold the selection back and continues; the image runs its
factory software until you activate a bundle built for it. Returning to an
older image needs no reordering while the selected bundle still accepts that
image's menu, as a newer bundle normally does.

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
bundle. That attempt is recorded durably before the prior bundle starts. If the
image cannot run that bundle, the attempt starts the image's factory software
instead and holds the prior bundle back. A
reboot cannot reset the attempt budget. Failed recovery or an unkillable old
runtime leaves explicit recovery-required status and keeps execution blocked.
Recovery changes only the matching software selection; it preserves the host,
base image, core library and unrelated deployments. Bundle updates must preserve
compatible configuration and persistent data formats for prior recovery.

During an appliance trial boot, the supervisor uses the factory software group
and preserves the data gate. After confirmation it reconciles persistent storage
and queued intent. A selection the confirmed image cannot run stays held back
while the factory software keeps running. A pending OS image change defers
software activation. Software installation joins appliance-operation draining;
the stable supervisor also serializes installation and activation and checks
the appliance gate itself. Queue and cancellation remain available while
execution is deferred.

### Acknowledge a failed recovery

A failed automatic recovery stays fenced. Rebooting or restarting the supervisor
does not replenish its attempt budget. If the previous bundle's complete service
group is already running, an operator can explicitly acknowledge that group
without restarting it. Inspect the failed operation, selection generation, active
bundle, base identity and boot ID first. On the kit, as root, use a fresh action
identifier and those exact observed values. An action identifier names one
operator act: each repair below refuses one that another repair already
recorded.

```sh
fes-kit-software acknowledge \
  --operation FAILED_OPERATION_ID --generation OBSERVED_GENERATION \
  --selected ALREADY_RUNNING_PREVIOUS_BUNDLE_ID \
  --base RUNNING_BASE_IMAGE_SHA256 --boot CURRENT_BOOT_ID \
  --action FRESH_64_CHARACTER_LOWERCASE_HEX_ID
```

This command verifies the bundle, running supervisor and its complete service
group. It checks that the failed operation still holds this boot's maintenance
reservation and that the stored grant authenticates against the agent's
persisted copy. It then requires ten seconds of native idle and launcher
readiness before it changes anything. Only then does it record the
acknowledgment and end that reservation with the operation's stored grant; it
never resumes the reservation. A separate durable acknowledgment retains the
original failure and operator action identity; selection and generation remain
unchanged. It neither starts a service nor retries the candidate. Missing
services, changed identities, another operation or failed readiness leave the
journal and the reservation as they were, so execution stays blocked and
`prepare-power-cycle` still applies. Inspect status after an uncertain reply;
repeat only the same action and exact identities to reconcile it.

If the previous service group is absent or unhealthy, this command cannot repair
it. Do not edit selection state or reset the automatic recovery phase. Prepare a
power cycle as below when the runtime reports `reboot_required`. Otherwise a
hard power-supply cycle leaves the kit in [recovery
exhausted](#leave-recovery-exhausted), which the factory selection there repairs.

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
without retrying cleanup. The reservation must still be blocked; one in any
other state is reported as that state, not as a grant that does not match. It
preserves the original failure in a durable audit, cancels that failed software
request, and leaves the current boot fenced in its existing maintenance
reservation. It does not release maintenance, restart a service, change the
committed selection or initiate a reboot. Its result is
`prepared-awaiting-physical-cycle`, not successful recovery.

The operator must then perform the [documented hard power-supply
cycle](../sources/FogCast/docs/DEVELOPMENT.md#updating-target-software).
After boot, verify the actual base, committed/active bundle, generation, native
idle readiness and free admission. A changed boot ID alone does not prove power
removal. Preserve any newer queued operation; preparation and delayed retries
cannot cancel it or restore an older selection. If another update activates,
report its new generation instead of treating the earlier selection as current.

### Leave recovery exhausted

A boot that finds the failed operation still blocked starts nothing. The
supervisor reports `software recovery exhausted; operator intervention
required` and keeps retrying, but the kit has no launcher, video, agent or
local control. An unprepared hard power-supply cycle after a failed recovery
leaves a kit here. Read the identities from `fes-kit-software status` and
`/proc/sys/kernel/random/boot_id`, then on the kit, as root:

```sh
fes-kit-software select-factory \
  --operation FAILED_OPERATION_ID --generation OBSERVED_GENERATION \
  --selected COMMITTED_SELECTED_BUNDLE_ID \
  --base RUNNING_BASE_IMAGE_SHA256 --boot CURRENT_BOOT_ID \
  --action FRESH_64_CHARACTER_LOWERCASE_HEX_ID
```

This root-only journal action selects the running image's own factory software,
the one release that image is built to run. It verifies that software as the
supervisor starts it, and requires the retained copy that lets a later image
resolve the selection. It refuses while any supervised process runs, or while
this boot still holds a maintenance reservation; perform the [documented hard
power-supply cycle](../sources/FogCast/docs/DEVELOPMENT.md#updating-target-software)
first. It preserves the original failure in a durable audit and cancels the
failed request. It commits the factory software as the selection with the next
generation, and the earlier selection becomes the previous bundle; when the
factory software is already selected, only the failed request ends. It never
starts, stops or signals a service, calls the agent, mints a credential or
changes the image. Its result is `factory-selected`.

The waiting supervisor retries startup at most 30 seconds apart. Its next
attempt starts the factory software with a fresh boot admission and the usual
readiness checks. Verify that status reports the factory software as active and
selected, nothing pending and no storage error. Inspect status after a failed or
uncertain reply. While the operation is still blocked, repeat the same action
and identities, or use a fresh action identifier, which also works after a
reboot; only one action ends the operation. From there use the ordinary paths:
activating another bundle keeps its one automatic recovery, which now returns to
the factory software. To return to an earlier image, roll back through the
[appliance update path](appliance-releases.md#update-and-recover), then activate
a bundle that image runs.

## API and evidence

All network routes use the target agent's existing bearer authentication and
proxy to the fixed root-only supervisor socket at `/run/fogcast/software.sock`.
They do not require an execution lease:

| Route | Effect |
| --- | --- |
| `GET /v1/kit/software` | Active, selected, pending and completed operation state, and a held-back selection with its reason |
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
image. Software goes before the image: before an image update, activate a
bundle the new image accepts. After use, if the booted image is not the
baseline named in the Powerboat status file, explicitly activate the
designated baseline bundle against the current generation first when the
running image accepts it, then restore the baseline image through the
appliance update or rollback path and verify the boot. When the running image
refuses the baseline bundle, restore and verify the image first, then
activate the baseline bundle, as [kit sharing](kit-sharing.md) requires.
Activating a bundle does not change the booted image and leaves the
successful selection installed. Verify the active bundle before logging the
kit restored. A core-only run that does not
change the bundle does not install one. Do not cancel another owner's pending activation to
clean up. Kits take no file or configuration overlays.
[Kit sharing](kit-sharing.md) describes execution ownership;
[appliance releases](appliance-releases.md) cover OS changes and boot identity.
Software tests establish host behavior. They do not establish card power-loss
behavior, physical startup, HDMI/input/media behavior or exact-artifact kit
acceptance for a new bundle.

## Build an update without rebuilding the OS

On Linux amd64, retain the verified `linux.img` and its reviewed SHA-256 from the
appliance release manifest. Keep the matching verified SDK cache from that image
build, or set `FES_TARGET_IMAGE_SHARED_CACHE` to its absolute cache path. An empty
cache is filled with `make kit-sdk`, which publishes the current image key's SDK
at `$FES_TARGET_IMAGE_SHARED_CACHE/toolchains/<key>/` (or the checkout toolchain
cache when the variable is unset). That command uses the pinned Buildroot
sources, target-image builder, `fogcast_toolchain.fragment` and key that
`build-target-image.sh --ensure-toolchain native-dev` uses. It fetches the
Buildroot checkout and the pinned Linux kernel into the checkout's target-image
source cache. It does not fetch the image-creator tree, and it does not need core
packages, idle or splash RBFs, or the native runtime input lock.
Commit the new software sources, then run:

```sh
make kit-software \
  KIT_SOFTWARE_BASE=/absolute/path/linux.img \
  KIT_SOFTWARE_BASE_SHA256=REVIEWED_IMAGE_SHA256 \
  KIT_SOFTWARE_VERSION=kit-2026.10.16
```

The command freezes the committed FES HEAD, builds the agent and both launchers
with the selected Go version, and builds the runtime with the pinned container
and verified cached ARM SDK. In the same container it builds the fes-window
module against the pinned kernel commit. It reads that commit from the bare
repository `linux-kernel.git` in `FES_TARGET_IMAGE_SHARED_CACHE` when present,
and otherwise from the checkout's target-image source cache, which
`image/scripts/fetch-target-image-sources.sh` fills. This lets a cold shared
cache use the worktree copy fetched by `make kit-sdk`. In either cache,
`refs/target-image/pinned` must name the locked commit, as the fetch script leaves it.
A base image carries no kernel: every card boots the kernel in
`boot-media.lock.toml`, and appliance releases must keep it. The build refuses a
kernel source pin that the source lock does not pair with that boot kernel. It
copies application assets from that same source snapshot. It never runs an OS
image build or an FPGA package producer. A missing or mismatching SDK fails
explicitly and tells you to run `make kit-sdk`. A missing kernel source fails
explicitly; `image/scripts/fetch-target-image-sources.sh` fills the bare
repository the module build reads.

The image hash is required: an extracted filesystem or a self-authored base
receipt is not a substitute for the reviewed image identity. The command hashes
a private image copy before extracting the factory and system-library trees,
then validates factory integrity and the new software's architecture, dynamic
library dependencies and versioned symbols against the retained base contract.
It also refuses a runtime that does not accept every interface the base
image's menu requires, and lists the runtime's menu interfaces in the bundle
manifest.
The supplied image stays unchanged.

The default output is the new directory `out/kit-software/<version>/`, containing
`bundle.tar` and `receipt.json`. Override the directory with
`KIT_SOFTWARE_OUTPUT=/absolute/new-release-directory`. Existing output directories
are never overwritten, including when two builders compete. Failed builds leave
prior outputs intact. The receipt closes the bundle digest, file inventory,
selected source and tree identities, base image/factory/ABI identities, Go
version, SDK/container identities, and the fes-window module with the kernel
commit, release and boot kernel digest it was built for. It explicitly records
`deployed: false`.

Install `bundle.tar` through the software-release client and request activation
separately. Build receipts do not establish hardware acceptance. If the retained
base cannot satisfy the new software's ABI, change its dependencies or plan an
explicit base-image upgrade; that image runs its factory software until you
activate a bundle built for it. Host software and installed core packages
remain independently managed.

For producers that already have a closed payload, the lower-level
`image/scripts/kit-software.py --base-root ... --payload ... --output ...
--version ... --fogcast ... --runtime-revision ...` command remains available.
Its caller is responsible for authenticating the extracted base against the
reviewed original image; prefer the parent command for ordinary updates.
