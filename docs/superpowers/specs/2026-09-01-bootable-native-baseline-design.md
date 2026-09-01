# Bootable Native MiSTer Baseline Design

**Status:** Approved design; implementation not started

**Date:** 2026-09-01

**Milestone:** 2 of the native MiSTer runtime migration

**Repositories affected:** `FogCast` and `libmister-runtime`

**Umbrella design:**
[`2026-08-31-libmister-runtime-design.md`](2026-08-31-libmister-runtime-design.md)

## Purpose

Produce the first bootable image in which `mister-runtime`, rather than the
conventional MiSTer/Main process, owns the FPGA. The image must boot the
designated MiSTer Pi into a stable idle RBF, expose that state through the
existing FogCast target API, stop cleanly, and survive an explicit reboot.

This milestone proves the native runtime composition, packaging, boot order,
and real hardware idle path. It does not launch games or development RBFs.
Those capabilities remain later, independently accepted milestones.

## Baseline

The working FogCast target images remain the current `dev` and `prod` images.
They start the conventional MiSTer/Main-compatible process, wait for
`/dev/MiSTer_cmd`, and run the FAT-side `mister-agent`. They launch real games
and provide the known-good rollback path.

The standalone private repository
[`DeanoC/libmister-runtime`](https://github.com/DeanoC/libmister-runtime) is at
`d6e7ec2db1049a0d6bd9edfd44a233ac174729f9` when this design is written. It
provides one lifecycle library, the `mister-runtime` daemon, a four-operation
Unix-socket protocol, and tested Linux FPGA-manager, MMIO, SPI, and core-loader
primitives. Its production hardware constructor is intentionally unavailable,
its production profile table is empty, and its supported-system count is zero.

Nothing in this milestone changes those support claims until the specified
physical checks pass.

## Goals

- Add a separate reproducible FogCast image named `native-dev`.
- Make `mister-runtime` the only process that owns and programs the FPGA in
  that image.
- Package one immutable idle RBF in the Linux root filesystem.
- Construct the real Linux hardware stack needed to load that idle RBF.
- Start the image-owned FogCast agent after the runtime process, and report
  ready only after the runtime reports `idle`.
- Give the agent an explicitly selected native backend that speaks the local
  runtime protocol.
- Expose honest ready, idle, stop, reboot, unavailable, and failure states
  through the existing target HTTP API.
- Prove the result on `192.168.10.239`, including visible HDMI output and a
  tested return to the unchanged legacy image.

## Non-goals

- Normal catalogue-game launch.
- Development-RBF loading.
- Adding the first production system profile or claiming hardware support for
  any system.
- Input, saves, audio, or native framebuffer/capture integration.
- Replacing or restructuring the existing `dev` and `prod` image paths.
- Running conventional Main and `mister-runtime` in one image.
- Automatic backend discovery, fallback, image switching, or rollback.
- Persistent runtime state, session reconstruction, retry orchestration,
  watchdogs, authentication, attestation, or a recovery coordinator.
- Turning the runtime repository into an image builder or making FogCast copy
  runtime source into its own tree.

## Chosen approach

FogCast adds `native-dev` beside the unchanged legacy variants and continues
to own the Buildroot image. The image build receives a clean
`libmister-runtime` checkout as an explicit read-only input, verifies the
expected commit recorded by FogCast, and builds it with the Buildroot target
toolchain through a small external package.

The native image contains image-owned copies of `mister-runtime`,
`mister-agent`, and the idle RBF. Its init scripts start the runtime and then
start the agent with an explicit native-backend argument. The agent reads the
runtime's actual state and reports ready only for `idle`; it does not infer
readiness merely from process or socket existence. No component probes for
Main or falls back to it.

This keeps the working image intact, tests the new boundary without claiming
game parity, and preserves one obvious rollback: reinstall the legacy `dev`
image.

### Rejected alternatives

- **Replace the current development image:** this would remove the tested
  game-launch baseline before the native path can launch a game.
- **Build the image from `libmister-runtime`:** the runtime should not own the
  FogCast agent, target configuration, kernel, or root filesystem.
- **Use a Git submodule:** it adds a second checkout workflow without solving
  a current problem. A verified explicit source input is simpler.
- **Autodetect Main versus native at agent startup:** an ambiguous device state
  could silently choose the wrong owner. Image configuration selects exactly
  one backend.

## Repository ownership and source inputs

### FogCast owns

- the `native-dev` Buildroot defconfig and image assembly;
- the native image init scripts;
- the Go client for `/run/mister-runtime.sock`;
- explicit agent backend composition;
- the target HTTP behaviour presented to the host;
- the build-input lock and produced-image manifest; and
- deployment, hardware evidence, and legacy rollback instructions.

### libmister-runtime owns

- production construction of the native Linux hardware dependencies;
- the idle-RBF lifecycle and FPGA programming sequence;
- `/run/mister-runtime.sock` and protocol 1;
- lifecycle state and direct hardware errors; and
- runtime-side software and hardware acceptance evidence.

### Locked inputs

FogCast records the native-only inputs in one small, human-readable lock file:

- the expected `libmister-runtime` commit;
- the official idle-RBF repository and immutable commit;
- the idle-RBF path, SHA-256 digest, and byte size; and
- the installed idle-RBF path.

The initial idle artifact is the official MiSTer Menu RBF from
[`MiSTer-devel/Distribution_MiSTer`](https://github.com/MiSTer-devel/Distribution_MiSTer),
pinned at commit `f7bde4becb452ca28f604ad9802bbed5c6b58e01`:

- source path: `menu.rbf`
- SHA-256: `821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934`
- byte size: `2452588`
- installed path: `/usr/share/mister-runtime/idle.rbf`

The build fails when the runtime checkout does not exactly match its locked
commit or when the idle artifact does not match its digest and size. This is
ordinary reproducible-build input checking, not a security or attestation
system. Updating either pin is an explicit reviewed source change.

## Runtime production construction

`libmister-runtime` replaces its unavailable production constructor with the
smallest real composition needed to establish idle:

- a monotonic clock;
- `PosixArtifactOpener`;
- `LinuxMmio`;
- `LinuxFpgaManager`;
- `LinuxSpi`;
- `CoreLoader`; and
- `NativeHardware` configured with
  `/usr/share/mister-runtime/idle.rbf`.

The resulting objects have clear process-lifetime ownership; the constructor
must not return references to temporary dependencies. The daemon remains the
composition root and continues to own the runtime and hardware for its full
lifetime.

Startup deliberately calls the existing lifecycle start operation, which
loads the idle artifact. Success produces `idle`. A missing, empty, invalid,
or unprogrammable idle artifact produces the existing direct error and
`reboot_required`. The daemon remains available on its Unix socket so status
can report the failure; it does not substitute fake hardware or conventional
Main.

The production profile registry remains empty. `launch` therefore supports no
game system in this milestone. The support matrix remains at zero.

## Native agent backend

FogCast adds one Go client for the existing newline-JSON protocol at
`/run/mister-runtime.sock`. Each call opens one Unix-socket connection, sends
one bounded protocol-1 request, reads one bounded response, and closes. The
client does not reproduce runtime state or hardware rules.

The client accepts the canonical runtime producer's transition shapes exactly:
`starting` with `none` accepts either null `system`/`core` or one complete,
non-empty retained `system`/`core` pair; `starting` with `game` requires that
complete pair; and `starting` with `development` requires null identity. Idle
and `reboot_required` remain `none` with null identity. A partial or empty
pair is never valid.

Agent startup chooses its backend explicitly, using a usage-based command-line
option: `--runtime native`. The legacy image continues to start the agent
without that option and therefore retains the current direct Main
implementation. There is no detection order or fallback between them.

For this milestone the native adapter implements only the behaviour the image
can truthfully provide:

- initialization/status asks the daemon for its observable state;
- health is ready only when the daemon reports `idle`;
- public stop is idle-only: an already-idle stop confirms `idle` without a
  runtime or hardware mutation;
- a non-idle native state observed at startup is unavailable in this milestone,
  rather than an admission to coordinator-driven active stop; the adapter may
  unit-test its direct `Stop` translation as an interface method retained for a
  later milestone;
- explicit reboot continues to use FogCast's existing system reboot mechanism;
  and
- game launch and development-RBF loading return a clear unsupported result
  before a runtime mutation or hardware mutation.

The adapter may satisfy FogCast's current target-runtime interface, but it is
not allowed to pretend that catalogue identities, core profiles, or
development execution exist. The existing HTTP response shapes remain the
host boundary; the adapter translates the daemon's direct state and errors
without inventing a second state machine.

## Image layout and boot flow

The new build output is:

```text
build/output/target-image/native-dev/linux.img
```

The native root filesystem contains these authoritative artifacts:

```text
/usr/sbin/mister-runtime
/usr/sbin/mister-agent
/usr/share/mister-runtime/idle.rbf
/etc/init.d/S40mister-runtime
/etc/init.d/S50mister-agent
```

It does not contain or start `/media/fat/MiSTer`, wait for
`/dev/MiSTer_cmd`, generate transient MGL files, or use `/tmp/CORENAME` as
runtime authority. The image-owned `/usr/sbin/mister-agent` is executed; a
FAT-side agent binary is not.

The existing FAT-side `agent.toml` may continue to supply device-specific
network, token, cache, input, and capture settings. Backend choice belongs to
the image init command, not to mutable automatic detection.

Boot order is:

```text
native-dev Linux boots
  -> S40mister-runtime starts /usr/sbin/mister-runtime
  -> mister-runtime opens the packaged idle.rbf
  -> S50mister-agent starts image-owned mister-agent --runtime native
  -> mister-agent reads status through the Unix socket
  -> successful programming produces idle and target health ready
  -> failure produces reboot_required or unavailable and target health not ready
```

`S50mister-agent` is ordered after `S40mister-runtime`, but socket existence is
not treated as readiness. The native adapter reads protocol status and makes
the target ready only for `idle`. This also leaves `reboot_required` and socket
failures visible through the running agent. The existing simple process
supervisor may restart a failed process; this milestone does not introduce a
new retry framework.

If runtime startup fails, the daemon status remains observable as
`reboot_required`, the agent must not report ready, and the logs show the
direct runtime failure. If the socket cannot be reached, the agent also does
not report ready. Restarting the runtime deliberately reloads idle rather than
reconstructing a previous session.

## Legacy image preservation

The existing outputs remain:

```text
build/output/target-image/dev/linux.img
build/output/target-image/prod/linux.img
```

Their defconfigs, boot ordering, Main process, `/dev/MiSTer_cmd` path, FAT-side
agent selection, and verification expectations remain behaviourally
unchanged. Native files and exceptions must be scoped to `native-dev`; in
particular, allowing the one image-owned idle RBF must not weaken the legacy
root-filesystem rule that rejects bundled RBF files.

Prefer adding a native-specific overlay or post-build layer over restructuring
the shared legacy overlay. If a small shared refactor is unavoidable, its
tests must prove byte-relevant legacy behaviour is unchanged.

## Failure behaviour

Failure handling stays direct and finite:

| Condition | Required result |
| --- | --- |
| Runtime checkout differs from lock | Image build fails before compilation |
| Idle source differs from digest/size | Image build fails before packaging |
| Idle artifact missing or unreadable at boot | Runtime reports `reboot_required`; agent not ready |
| FPGA programming fails | Runtime reports the direct failure and `reboot_required`; agent not ready |
| Runtime socket unavailable | Agent not ready; no fallback |
| Runtime reports a non-idle state at agent startup | Unavailable for this milestone; no coordinator-driven active stop |
| Game launch requested | Clear unsupported response before hardware mutation |
| Development RBF requested | Clear unsupported response before hardware mutation |
| Stop while idle | Target confirms `idle` without hardware mutation |
| Runtime process restarts | Runtime deliberately reloads idle |
| Device reboots | Host confirms a changed boot ID and fresh `idle` |

There is no automatic image switch, Main fallback, launch retry, preserved
session, or durable recovery record.

## Software verification

### libmister-runtime

- Construct the complete production object graph without fake backends.
- Prove the production idle path selects the locked installed path.
- Test ownership/lifetime and error propagation at the construction boundary.
- Preserve all existing lifecycle, daemon, sanitizer, archive, active-tree,
  history, dependency, and Arm cross-build gates.
- Keep production profiles empty and reject game launch truthfully.
- Ensure production binaries contain no fake backend or conventional Main
  mutation symbols.

### FogCast

- Test the native client against a real temporary Unix socket using exact
  protocol framing and response mapping.
- Test explicit native and legacy agent composition; neither may silently
  select the other.
- Test native initialization, idle health, stop, unavailable socket,
  `reboot_required`, and unsupported game/development requests.
- Extend build scripts and fixtures to accept exactly `prod`, `dev`, and
  `native-dev`.
- Inspect the native root filesystem and prove the runtime, agent, and idle RBF
  are image-owned; their hashes/revisions are in the manifest; Main startup and
  `/dev/MiSTer_cmd` waiting are absent; and runtime precedes agent.
- Inspect the target executable and its target-library closure so the image
  contains every runtime dependency and no host-built binary.
- Preserve all existing legacy target-image tests and expectations.
- Build `native-dev` twice and retain the existing reproducibility comparison.
- Limit QEMU to root-filesystem, init-script, and agent/runtime packaging smoke
  tests. QEMU is not FPGA acceptance.

## Physical MiSTer Pi acceptance

The milestone is not complete until the disposable target at
`192.168.10.239` passes all of these checks:

Before touching the device, clean and fast-forward both repository main
checkouts. From that clean FogCast main, rebuild and verify the reproducible
legacy `prod` and `dev` images and the native image. Pull and verify a clean
runtime main checkout, extract the locked runtime commit, assert it equals
runtime `HEAD`, and re-run native-input verification. Only then hash and deploy
the resulting images.

1. Record the FogCast commit, `libmister-runtime` commit, native and legacy
   image SHA-256 values, idle source commit, and idle artifact SHA-256.
2. Install and boot the `native-dev` image.
3. Confirm no conventional Main process is running and no agent path depends
   on `/dev/MiSTer_cmd`.
4. Confirm `mister-runtime` reports `idle` and the existing FogCast target
   health reports ready.
5. Capture five timestamped HDMI frames over five seconds and a V4L2
   device/mode report. Inspect every frame for stable geometry and non-corrupt
   output, and record every capture and report hash without committing binary
   frames.
6. Call the existing idle-only stop path and confirm the runtime remains
   `idle` without rebooting.
7. Request an explicit reboot; confirm the Linux boot ID changes and the target
   returns to fresh `idle` and ready.
8. Reinstall the legacy `dev` image and launch one known catalogue game,
   proving that rollback still works.

The evidence records the date, commits and image identities, observed states,
all rebuilt-image and capture/report hashes, five-frame inspection result,
reboot boot IDs, and legacy game used. A short human-readable report is
sufficient.

## Delivery sequence

This milestone is delivered in three reviewable changes, not one mixed
cross-repository branch:

1. Land this FogCast design and its umbrella-roadmap update.
2. Implement and review idle-only production construction in
   `libmister-runtime`.
3. Implement and review the FogCast native client and `native-dev` image, then
   run and record physical acceptance and rollback.

The implementation plan must preserve that dependency order. FogCast can pin
only a reviewed runtime commit, and physical deployment can use only reviewed
FogCast and runtime commits.

## Exit criteria

Milestone 2 is complete only when:

1. `native-dev` is reproducibly buildable as a third image without changing
   the working legacy image behaviour.
2. Its runtime checkout and idle RBF are immutable, verified build inputs.
3. `mister-runtime` is the sole FPGA owner and loads the packaged idle RBF on
   the real device.
4. The image-owned agent uses only the local runtime socket and reports ready
   only from a confirmed `idle` state.
5. Status, stop, explicit reboot, direct startup failures, and unsupported
   game/development operations behave as specified.
6. Software gates pass in both repositories and the physical eight-step
   acceptance record passes.
7. Reinstalling the legacy image and launching a known game proves rollback.
8. Documentation still states zero native hardware-supported game systems.

Only then does work move to the Mega Drive vertical slice. Development-RBF
support remains Milestone 4 rather than being pulled into this baseline.
