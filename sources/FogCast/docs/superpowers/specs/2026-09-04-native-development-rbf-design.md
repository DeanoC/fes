# Native Development RBF Design

**Status:** Implemented and hardware-tested for the MiSTer-compatible development ABI

**Date:** 2026-09-04

**Milestone:** 4 of the native MiSTer runtime migration

**Repositories affected:** `FogCast` and `libmister-runtime`

**Umbrella design:**
[`2026-08-31-libmister-runtime-design.md`](2026-08-31-libmister-runtime-design.md)

## Purpose

Extend the hardware-tested native runtime path so FogCast can load one raw,
MiSTer-compatible development RBF through the existing public development-RBF
API. The native runtime remains the sole FPGA owner. A successful load reports
an honest development session, Stop restores the known idle RBF without a
routine reboot, and a normal catalogue game can launch afterward.

This milestone proves the from-source development path using a real upstream
Mega Drive build as the physical fixture. It does not adopt that build as the
production Mega Drive RBF, add a browser file picker, or define a generalized
ABI for arbitrary non-MiSTer FPGA images.

## Baseline

The existing public endpoint is:

```text
POST /api/v1/session/development-rbf
Content-Type: application/octet-stream
Content-Length: 1..32 MiB
```

The conventional Main backend already streams the body to the target, installs
it atomically at `/tmp/fogcast-development/core.rbf`, and asks Main to load it
through `/dev/MiSTer_cmd`. A conventional development Stop uses the established
reboot handshake when Main cannot safely recover from a non-MiSTer image. That
working path remains unchanged.

The native image already has a separate, hardware-tested idle and Mega Drive
path. It starts image-owned `mister-runtime` and `mister-agent --runtime native`,
contains one locked idle RBF and one locked production Mega Drive RBF, and does
not run Main or use `/dev/MiSTer_cmd`.

The native protocol already defines version-1 operation
`load_development_rbf` with an absolute staged RBF path. The runtime lifecycle
already defines `running_development` with execution `development`, and Stop
already accepts that state. FogCast's native adapter currently rejects the
operation, and the native hardware implementation does not yet perform the
safe MiSTer-compatible core start sequence.

## Goals

- Preserve the existing public host and target endpoints and their bounded raw
  upload contract.
- Add native development loading without introducing a second FPGA programmer.
- Replace an active native catalogue game only after a normal Stop has reached
  confirmed idle.
- Stage the upload atomically in volatile target storage and dispatch it to the
  native runtime exactly once.
- Program and start a MiSTer-compatible RBF safely while keeping HDMI disabled
  for the development session.
- Report `running_development` without inventing a game, system, catalogue
  entry, media role, profile, or manifest.
- Stop a native development session by restoring the known idle RBF, using
  `reboot_required` only when safe idle recovery fails.
- Reconstruct a live native development session after a host or agent restart,
  and permit Status and Stop without replaying the upload.
- Prove the path on the designated kit with the exact upstream-built Mega
  Drive fixture, then prove a normal pinned catalogue launch still works.

## Non-goals

- A browser or ten-foot file picker.
- Packaging, pinning, publishing, or adopting the upstream-built fixture as the
  production Mega Drive RBF.
- A generalized RBF ABI or capability model for non-MiSTer standalone cores,
  custom FogCast cores, or future development interfaces.
- Development-RBF video, audio, media, input, settings, save, or remapping
  profiles.
- Promising visible output from an arbitrary raw development RBF.
- Durable upload storage, upload history, replay after restart, automatic
  retry, multiple named development images, or a rollback store.
- Changing the conventional Main development path or its reboot recovery.
- Changing the native image's supported catalogue systems.
- Adding a development artifact to any production image or lock file.

## Compatibility boundary

This milestone supports the existing MiSTer-compatible development ABI only.
That means the loaded RBF accepts the MiSTer core-start synchronization used by
the current runtime. Success guarantees:

- the exact staged bytes were handed to the sole native FPGA owner;
- the RBF was programmed and the MiSTer-compatible core start completed;
- runtime and public session state honestly report development execution;
- Stop can restore idle or report the explicit recovery failure; and
- a subsequent normal catalogue launch remains available after Stop.

Success does not guarantee useful HDMI, audio, input, or media behavior. The
raw endpoint carries no profile or video metadata from which FogCast could make
those promises. The physical fixture may produce recognizable output, but that
is fixture evidence rather than a universal API guarantee.

A later design may name and negotiate multiple RBF ABIs, including non-MiSTer
standalone images and custom FogCast cores. This milestone neither blocks nor
pre-designs that work.

## Chosen approach

Extend the existing development request through the existing native backend:

```text
Host tool
  -> POST /api/v1/session/development-rbf (bounded raw RBF)
  -> host session service
  -> POST /v1/development/rbf (bounded raw RBF)
  -> mister-agent atomic staging
  -> native adapter
  -> version-1 load_development_rbf request
  -> mister-runtime
  -> native hardware owner
  -> FPGA development image
```

The host continues to own user intent and session replacement. The target
agent continues to own upload validation and atomic staging. `mister-runtime`
continues to be the only native FPGA programmer and lifecycle authority. No
temporary catalogue profile, direct agent-to-FPGA path, or second protocol is
introduced.

### Rejected alternatives

- **Create a temporary game/profile:** a raw development upload has no honest
  system, cartridge, input, or video profile. Inventing one would silently
  define the ABI this milestone intentionally leaves open.
- **Program the FPGA directly from the agent:** this would create two native
  hardware owners and bypass the runtime's lifecycle, cleanup, and Stop
  contracts.
- **Treat the upload as a packaged production core:** that would turn a source
  path test into an unsupported production adoption and couple the image to an
  operator artifact.

## End-to-end behavior

### Admission and session replacement

The public body requirements remain unchanged: a known positive length no
greater than 32 MiB and `application/octet-stream`. Invalid metadata or a body
that cannot be staged fails before a runtime mutation.

The host serializes the request with other session mutations and detaches any
active remote input or media ownership. If a native catalogue game is active,
the host first performs the normal confirmed Stop flow. Development loading
continues only after authoritative native status is idle. A failed or
ambiguous Stop prevents upload dispatch.

While native development is active, a normal catalogue launch is rejected as
busy until the caller explicitly Stops the development session. FogCast does
not silently replace development execution with a game.

### Staging and dispatch

The target receives the upload once and atomically installs it at the existing
volatile path `/tmp/fogcast-development/core.rbf`. It validates the exact
declared length, rejects trailing or short content, and never dispatches a
partially written file.

After staging, the native adapter confirms exact runtime idle and sends one
version-1 `load_development_rbf` request with the absolute staged path. The
request does not include or derive game, system, media, profile, video, or
input identities.

An admitted native mutation is owned by the agent process lifetime, not by a
short inbound HTTP waiter. Caller cancellation may stop waiting but cannot
cancel an already admitted hardware operation halfway through. Ambiguous local
response loss is reconciled through bounded Status-only observation. Neither
the upload nor the runtime mutation is replayed.

### Native hardware sequence

The runtime opens and validates the staged RBF completely before mutation.
Once preflight succeeds, the native hardware sequence is:

1. quiesce HDMI by read-modify-writing the ADV7513 main power-down bit with the
   existing bounded video I/O contract;
2. program the FPGA exactly once from the already-open artifact;
3. perform the existing MiSTer-compatible core synchronization/start strobe;
4. sample the core identity when safely available as observation only; and
5. publish `running_development` only after programming and synchronization
   complete.

HDMI remains deliberately powered down throughout `running_development`.
FogCast does not apply a guessed game video mode or declare link readiness for
an unprofiled image. Stop reloads the known idle RBF through the existing idle
path, which restores the reviewed 720p60 output.

No catalogue media, game reset, native input worker, generic button profile,
or cartridge operation runs during development loading.

## State and identity

The native runtime response for a successful load is:

```text
state = running_development
execution = development
system = null
core = observed MiSTer core name when safely sampled, otherwise null
error = null
```

The public host session maps this to its established development representation
with `execution: fpga_development`, `development: true`, and null game/system
identity. An observed core remains an observation; it is not promoted into a
catalogue system or support claim.

Development is not healthy-idle readiness and is not game-launch admission.
It is, however, a valid reconstructable state for Status and Stop.

## Stop and recovery

A native development Stop calls the normal native runtime Stop operation. The
runtime reloads the locked idle RBF once and returns idle. A successful public
Stop reports idle without rebooting the target.

Failure behavior follows the existing native mutation boundary:

- failure before any hardware write leaves idle unchanged;
- successful HDMI power-down begins the hardware mutation boundary;
- programming, synchronization, observation, or later failure triggers one
  existing cleanup attempt to the known idle RBF;
- successful cleanup reports idle while retaining the primary operation error;
- failed cleanup reports `reboot_required`; and
- FogCast uses its existing explicit reboot recovery only for that fallback,
  never as the routine native development Stop.

Host or agent restart reconstructs `running_development` from native runtime
Status and permits Status and Stop. The upload bytes are not retransmitted.
Runtime process restart performs its normal startup idle load, so the public
state reconstructs as idle rather than pretending the prior development
session survived.

## Component changes

### `libmister-runtime`

- Complete `NativeHardware::LoadDevelopmentRBF` with artifact preflight, HDMI
  quiesce, FPGA programming, MiSTer core synchronization, optional observed
  core sampling, and the existing mutation/cleanup semantics.
- Keep protocol version 1 and the existing `load_development_rbf` operation.
- Preserve lifecycle state `running_development`, execution `development`, and
  the existing Stop-to-idle contract.
- Add no profile, video mode, input mapping, media role, or system table entry.

### FogCast native adapter

- Extend the native runtime client/control seam with the existing protocol
  request and strict response validation.
- Reuse the existing atomic volatile development staging contract.
- Give the admitted operation process-owned execution and bounded Status-only
  reconciliation without dispatch replay.
- Treat `running_development` as valid for reconstruction and Stop, but not as
  idle Health or normal launch readiness.

### Agent coordinator

- Stop an active native game and confirm idle before development dispatch.
- Preserve the conventional Main backend's existing development and reboot
  behavior byte-for-byte in semantics.
- Route native development Stop through normal native Stop; use the existing
  reboot-required handshake only when native idle recovery actually fails.
- Reconstruct native development ownership from runtime Status after agent
  restart and clear it only after confirmed idle.

### Host session

- Retain the existing endpoint, body limits, streaming client, and public
  development session shape.
- Serialize game-to-development replacement and require authoritative Stop to
  idle before upload.
- Reject game launch while development is active until explicit Stop.
- Preserve caller cancellation while allowing admitted target work to resolve
  through authoritative Status.

### Native image

- Package no development RBF.
- Continue using volatile `/tmp` staging.
- Change only the locked runtime revision after the runtime change merges.
- Retain the locked idle and production Mega Drive artifacts unchanged.

## Testing

Implementation uses strict test-driven development.

### Runtime tests

- exact preflight, HDMI-quiesce, program, synchronize, observe, and publication
  order;
- one shared or phase-specific deadline wherever the existing hardware
  contract requires it, with expired and I/O-failure coverage;
- read-modify-write preservation for ADV7513 power-down;
- no game profile, media, video bring-up, reset-release, or input operations;
- failure before mutation versus every post-mutation cleanup boundary;
- one cleanup attempt, retained primary error, and `reboot_required` on cleanup
  failure;
- successful Stop to idle and immediate subsequent game launch; and
- runtime restart returning to idle.

### FogCast tests

- exact content type, size, short-body, trailing-body, atomic staging, and
  staged-path behavior;
- active-game Stop-to-idle before upload and no dispatch when Stop is not
  confirmed;
- process-owned admitted mutation, caller cancellation, shutdown cancellation,
  lost-response Status reconciliation, and zero replay;
- strict native response state/execution/identity validation;
- host and agent restart reconstruction of `running_development`;
- explicit Stop before a normal catalogue launch;
- normal native and conventional development behavior remain unchanged outside
  the selected backend; and
- native image contains no development artifact.

### Repository gates

- focused and affected Go race tests;
- full FogCast tests and vet;
- full runtime tests, ASan/UBSan, TSan, archive, active-tree, history, and
  provenance audits;
- pinned ARM runtime and agent builds;
- native image structural, immutable-input, QEMU, and two-pass reproducibility
  gates; and
- unchanged legacy image build and smoke gates at final acceptance.

## Physical acceptance

Acceptance uses a freshly reproducible native image and the exact upstream
fixture already built at:

```text
/home/deano/fes/misteross-rebuild/build/current/megadrive.rbf
size = 4,306,912 bytes
sha256 = 195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e
```

That workstation path records the designated fixture for this acceptance run;
it is not a production package location or a durable product interface.

The no-retry physical sequence is:

1. Boot the exact native image and prove exact idle state, installed hashes,
   process ownership, FPGA operation, and visible known idle output.
2. Upload the exact fixture once through the existing public endpoint.
3. Prove the target staged the exact bytes and runtime/public status is
   `running_development`/development with no game or system identity; record
   any observed core; prove HDMI remains intentionally powered down.
4. Stop once and prove exact native idle plus restored visible idle output,
   without reboot unless the explicit fallback was genuinely required.
5. Launch the pinned Sonic 2 catalogue entry, prove visible gameplay and
   right/jump input, then Stop to visible idle.
6. Repeat development -> idle -> catalogue game -> idle once on the same boot,
   without retries, upload replay, receiver reset, or unexplained recovery.
7. Deploy the freshly reproducible legacy development image and run its
   unchanged Sonic 2 -> Mega Drive -> Stop -> Menu smoke once.

The hardware record names this capability separately from production Mega
Drive support. It does not claim generic development video or adopt the
fixture as a production artifact.

## Integration order

1. Implement and independently review the runtime change.
2. Open, check, and merge the focused runtime pull request.
3. Update FogCast's locked runtime revision and implement the native adapter,
   coordinator, host-session, tests, and proposed documentation.
4. Build a fresh reproducible native image and run the complete physical
   acceptance sequence.
5. Update support truth and present-tense architecture only from the accepted
   evidence.
6. Open, check, and merge the FogCast pull request.

No implementation pull request claims support before the exact image and
physical evidence are complete.

## Completion criteria

This milestone is complete only when:

- the runtime and FogCast changes are merged in dependency order;
- all software, image, reproducibility, and legacy regression gates pass;
- two consecutive development-to-game physical cycles pass without retries;
- runtime and public state remain honest throughout;
- the designated target is left in a verified safe state; and
- documentation states the narrow MiSTer-compatible development capability
  without implying a browser picker, production artifact adoption, generic
  video support, or a generalized RBF ABI.
