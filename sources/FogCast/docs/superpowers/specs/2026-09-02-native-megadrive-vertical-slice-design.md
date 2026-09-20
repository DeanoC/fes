# Native Mega Drive Vertical Slice Design

**Date:** 2026-09-02

**Status:** Implemented and physically accepted on 2026-09-03. This document
preserves the approved pre-implementation design and scope.

## Purpose

Milestone 3 adds the first native game system to FogCast. A real Sonic 2
catalogue entry launches through FogCast's existing public API on the
designated MiSTer Pi without starting conventional Main. The native runtime
programs the Mega Drive core, attaches the ROM, produces visible HDMI, accepts
one player of input, returns to the proven native idle state, and launches the
same game a second time without rebooting.

This is deliberately a vertical slice rather than general system parity. It
establishes the production profile and session boundaries that the later SNES
milestone can reuse.

## Approved scope

The slice includes:

- the production system identifier `megadrive` and observed core identity
  `MegaDrive`;
- one pinned, image-owned Mega Drive RBF;
- one required semantic media role, `cartridge`;
- ROM launch for the existing Mega Drive catalogue, with Sonic 2 as the exact
  physical acceptance title;
- fixed 1280x720 at 60 Hz HDMI using the already proven native transmitter
  path;
- one Mega Drive three-button player with D-pad, A, B, C, and Start;
- Stop to the proven native idle RBF and video path; and
- an immediate second launch of Sonic 2 from clean idle state.

The slice excludes:

- save RAM, save states, or other persistent game state;
- native audio;
- six-button X/Y/Z/Mode input, multiplayer, remapping, or hot-plug recovery;
- development-RBF loading or video acceptance;
- SNES or any other production profile;
- preservation of a running game across runtime restart or target reboot; and
- conventional Main, transient MGLs, or an automatic legacy fallback in the
  native image.

## Authorities and ownership

FogCast continues to own the catalogue, title selection, ROM staging, target
selection, public session API, media capture, and host-to-target input lease.
It sends stable semantic facts across the local runtime boundary and does not
encode Mega Drive SPI transactions.

`libmister-runtime` remains the sole native FPGA owner. Its one canonical
production profile table gains exactly one immutable Mega Drive profile. That
profile owns the expected core identity, cartridge media rule, core protocol
recipe, fixed video recipe, and player-1 input mapping. Validation copies the
selected immutable recipe into `PreparedLaunch`; native execution must not
perform a second lookup through another system-name table.

The target `mister-agent --runtime native` remains the HTTP/cache/input
boundary. It translates the existing FogCast launch request to the existing
local runtime `launch` request. It does not program the FPGA or interpret the
Mega Drive core protocol.

## Image-owned core artifact

The accepted native image contains exactly one Mega Drive RBF at:

```text
/usr/share/mister-runtime/cores/megadrive.rbf
```

The FogCast immutable-input lock records the artifact's authoritative source,
source revision or release identity, byte size, and SHA-256. The implementation
starts from the exact core currently proven on the designated kit:

```text
FAT name: MegaDrive_20260603.rbf
size:     4296864 bytes
sha256:   0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
```

Before this identity can enter the lock, the fetch task must reproduce the
same bytes from an immutable official MiSTer source. A mismatch stops the task;
the implementation must not silently lock different bytes merely because
they are newer.

Production launch always uses the image-owned absolute path. It never reads a
core from `/media/fat`. Active development may inject a candidate RBF into a
disposable diagnostic copy of the last verified native image, but that result
is not release or hardware-support evidence.

## Runtime profile and prepared launch

The public profile model is extended only with the immutable data required by
the generic native launch components. The Mega Drive production profile
contains:

- system `megadrive`;
- expected core `MegaDrive`;
- required media role `cartridge`, file index 1;
- accepted ROM extensions `.md`, `.gen`, and `.bin`;
- a bounded ROM size accepted by the selected production core;
- raw Mega Drive content with little-endian byte-pair file-I/O transfer;
- the initial status and software-reset recipe required by the selected core;
- the fixed native 720p60 video recipe;
- player count 1 for this milestone; and
- player command and digital bit mapping for D-pad, A, B, C, and Start.

`Profiles::Prepare` validates the request completely, rejects unknown media
and all settings, maps `cartridge` to index 1, and copies the complete recipe
into `PreparedLaunch`. Test-only profiles remain private fixtures. No fake or
synthetic profile can enter production construction.

## Runtime components

The implementation adds or extends three narrow runtime units.

### Game session bring-up

A native game-session component consumes only `PreparedLaunch`, opened
artifacts, the existing FPGA programmer, the existing SPI/core primitives,
the fixed-video component, the input component, a monotonic clock, and the log
sink. It implements the profile recipe and has no catalogue, HTTP, cache, or
FogCast game-ID knowledge.

### Profile-driven fixed video

The current Menu-only video component is split or generalized so the proven
ADV7513 initialization, fixed 720p60 timing, bounded wake, and link-status
checks are reused without retaining a hard-coded `MENU` admission rule.
Menu idle and Mega Drive each supply an immutable recipe. Generalization must
not change the accepted Menu transaction order or idle behavior.

### Native input session

Native agent startup creates and retains one real Linux uinput virtual
gamepad with a fixed name and fixed bus/vendor/product/version identity. Its
evdev node exists before a game launch. The input lease controls whether host
events are delivered; the device's existence is not permission to deliver
events.

The runtime input session resolves exactly that identity, opens only its evdev
node, and rejects ambiguous or absent matches before FPGA mutation. It maps
only D-pad, A, B, C, and Start for player 1. The FogCast input contract gains
an explicit C code; Select is not repurposed. Axis input may produce D-pad
state only if it uses the existing bounded digital threshold contract.

The input session owns its worker thread, descriptor, current button map, and
generation. It sends a neutral map before core release. Stop joins the worker
and sends final neutral input before idle programming. A worker from an older
generation cannot mutate a later session.

Legacy agent construction and its current input path remain unchanged.

## Launch data flow

```text
POST /api/v1/session/launch {game_id}
  -> FogCast resolves Sonic 2 and system megadrive
  -> FogCast stages or selects the target ROM path
  -> POST /v1/launch {game_id, system, rom_path}
  -> native agent validates the public request
  -> local runtime launch {
       system: megadrive,
       rbf: /usr/share/mister-runtime/cores/megadrive.rbf,
       media: {cartridge: <staged absolute ROM path>},
       settings: {}
     }
  -> runtime validates and opens every artifact
  -> native Mega Drive session
  -> runtime publishes running_game
  -> FogCast attaches the existing remote-input lease
  -> target agent feeds the already-created virtual gamepad
```

The local daemon protocol remains one request and one response per connection.
No RBF or ROM bytes cross that socket. A lost launch response is reconciled
only through `status`, as in the existing lifecycle.

## Exact hardware launch sequence

The game launch uses one bounded sequence:

1. Validate the production profile and complete request.
2. Resolve the exact persistent FogCast virtual gamepad identity.
3. Open and validate the image-owned RBF and staged ROM without mutation.
4. Sort the already validated media by profile-owned index.
5. Program the FPGA with the Mega Drive RBF.
6. Assert the Mega Drive core's software reset.
7. Toggle and sample the core-ID path, then require `MegaDrive`.
8. Apply the profile's initial status words while reset remains asserted.
9. Attach the cartridge at file index 1 with the selected core's raw
   byte-pair wire format.
10. Initialize the fixed 720p60 native video path and require ADV7513 HPD and
    monitor-sense.
11. Start the input session and deliver a neutral player-1 map.
12. Release software reset last.
13. Publish `running_game` only after every preceding phase succeeds.

All steps after artifact preflight use absolute monotonic deadlines. The
implementation derives exact wire values and ordering from the selected RBF's
source and the preserved, working Main/native lineage; tests freeze those
values. It does not copy the old broker, save, audio, recovery, or multi-system
framework.

## Input flow

FogCast's existing host capture and authenticated lease/stream remain the
network path. In native mode, the target controller writes decoded events to
the retained virtual gamepad. The runtime reads Linux input events from its
exact evdev node and maintains one 16-bit player-1 map.

Each accepted event produces one coherent Mega Drive player update over SPI.
Duplicate states may be suppressed. `SYN_REPORT` is the commit boundary for a
batch. Unsupported event kinds, devices, buttons, players, and malformed
records are rejected rather than guessed.

The public launch response does not precede runtime input readiness: the
virtual device must be present and opened during runtime launch. The later
input lease attachment only begins event delivery.

## Stop and immediate relaunch

Stop performs this order:

1. FogCast detaches the host input lease and releases its held input state.
2. The runtime prevents new game-input delivery for the active generation.
3. The runtime joins the input worker and sends a final neutral player-1 map
   when the core remains addressable.
4. The runtime loads the already accepted idle RBF and runs the proven Menu
   video bring-up.
5. The runtime publishes `idle` only after idle succeeds.

A second Sonic 2 launch repeats complete validation and hardware bring-up. It
must create a fresh input generation and reopen both artifacts. No pressed
buttons, descriptors, ROM handles, profile state, or worker thread survive
the first stop.

## Failure behavior

Named phases are:

```text
preflight -> program -> reset -> probe -> configure -> media
          -> video -> input-neutral -> release -> running
```

Profile, artifact, and virtual-input validation fail before mutation and leave
the runtime in `idle`.

After FPGA programming begins, any synchronous failure gets exactly one
cleanup attempt through the existing idle path. Cleanup success preserves the
original launch error and leaves observable state `idle`; cleanup failure
enters `reboot_required`.

An input-worker read error, device removal, invalid record, or SPI delivery
failure after launch is a session fault. The active generation attempts
player neutralization and exactly one cleanup to idle. Cleanup failure enters
`reboot_required`. There is no hot-plug reconnection, automatic launch retry,
Main fallback, timeout enlargement, or automatic reboot.

Logs record operation, system, expected or observed core, phase, and direct
error. They do not contain ROM bytes, input tokens, or unrestricted paths
beyond the existing diagnostic contract.

## FogCast and image changes

FogCast retains its public launch, status, stop, catalogue, target cache, and
session interfaces. Native-agent translation changes from truthful rejection
to one supported mapping for `megadrive`; every other system remains
`UNSUPPORTED_SYSTEM`.

The native image build:

- fetches and verifies the locked Mega Drive RBF;
- installs it once at the fixed image-owned path;
- includes the native agent's real uinput construction support;
- keeps conventional Main and its command FIFO absent; and
- records the runtime, agent, idle RBF, and Mega Drive RBF identities in the
  image manifest and build-input record.

Structural verification rejects a missing, duplicate, renamed, or mismatched
Mega Drive RBF and rejects reintroduction of Main/MGL startup wiring.

## Development and verification strategy

Implementation uses test-driven development and the project's fast target
iteration policy.

During active development:

- run focused runtime profile/core/video/input/session tests;
- run affected FogCast agent, input, protocol, service, and image fixtures;
- run host sanitizers or race tests appropriate to the changed component;
- cross-build only changed ARM runtime or agent artifacts;
- inject changed binaries and a candidate RBF into a disposable copy of the
  last verified native image; and
- use bounded Pi diagnostics without claiming reproducibility or hardware
  support.

Do not rebuild all images merely to investigate a runtime transaction. The
original verified images remain untouched. A full rebuild becomes mandatory
when the locked RBF, Buildroot/image contents, init wiring, or final merged
runtime/agent inputs change.

## Software gates

Before the runtime PR is ready:

- all runtime host tests pass;
- profile, media, video, input, lifecycle, daemon, and failure-order tests
  cover the real production path;
- deliberate mutations prove exact ordering and failure containment;
- ASan/UBSan and TSan gates pass;
- production archive, active-tree, history, and deterministic-build audits
  pass; and
- the pinned ARM toolchain produces the expected ARM EABI artifact closure.

Before the FogCast PR is ready:

- all focused native-agent request and input tests pass;
- full `make test`, Go race tests, `make vet`, shell syntax, and diff checks
  pass;
- the immutable-input and rootfs/image fixtures cover the exact RBF identity;
  and
- the runtime pin points to the reviewed runtime merge.

## Formal image and physical acceptance

After software behavior is stable and the reviewed runtime is pinned:

1. Build the native image twice from independent work directories and require
   byte-identical output.
2. Run native structural, ELF/library, immutable-input, manifest, and QEMU
   packaging verifiers.
3. Deploy that exact image once to the designated MiSTer Pi.
4. Run the unchanged native lifecycle smoke and require idle before launch.
5. Launch the known Sonic 2 catalogue entry through
   `POST /api/v1/session/launch`.
6. Require runtime `running_game`, system `megadrive`, expected and observed
   core `MegaDrive`, the exact game ID, and no last error.
7. Capture exactly five collision-proof 1920x1080 frames and inspect each.
   Require recognizable Sonic 2 gameplay, no red/green raster, and no
   ShadowCast no-signal output.
8. Deliver player-1 direction and jump through FogCast's existing input path
   and preserve visual or operator evidence that Sonic moves and jumps.
9. Stop and require the proven native idle state and visible idle output.
10. Immediately launch Sonic 2 again and require visible gameplay plus working
    player-1 input.
11. Stop again and require native idle.
12. Restore the freshly verified legacy development image and run its unchanged
    Sonic 2 launch/stop/Menu rollback check.

Any failed gate stops later acceptance work. Diagnostics remain read-only
except for the explicitly permitted legacy-image restoration. There is no
fallback, recapture with altered settings, or support claim from a partial
run.

## Support truth and delivery

The runtime support matrix changes `megadrive` to `software: yes` only in the
same reviewed change as the complete implementation and software evidence. It
changes to `hardware: yes` only after the exact pinned FogCast image passes the
formal physical sequence.

All other system rows remain `software: no` and `hardware: no`. Documentation
must explicitly retain the exclusions for audio, saves, six-button input,
multiplayer, hot-plug recovery, and development RBFs.

Delivery uses two dependent review units:

1. a `libmister-runtime` PR containing the production profile and native game,
   video, and input behavior; then
2. a FogCast PR pinning the reviewed runtime, packaging the locked RBF,
   completing agent/uinput integration, and recording exact hardware evidence.

Neither PR may claim support based only on scaffolding or diagnostic images.
