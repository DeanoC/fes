Source snapshot: FogCast commit `6da6fb9`.

The approved design is copied below so this standalone repository does not
depend on a sibling checkout.

# Native MiSTer Runtime Design

**Status:** Proposed; approved in discussion, not yet implemented

**Date:** 2026-08-31

**Repositories affected:** `FogCast`, new `libmister-runtime`, `Main_MiSTer`,
and `misteross`

## Purpose

Make `libmister-runtime` the primary MiSTer hardware-control implementation
used by FogCast. The finished native path must support every FPGA system and
behaviour FogCast uses today, plus loading development RBF files, without
recreating the conventional MiSTer Main application.

This is a hobby-development system on a disposable local device. The design
optimizes for a short, understandable path from the FogCast host to the FPGA.
It does not add automatic failover, distributed ownership, security
frameworks, or preservation of sessions across crashes and reboots.

## Current baseline

The working path remains the one documented in FogCast's `README.md` and
`docs/ARCHITECTURE.md`:

```text
FogCast host
  -> target HTTP API
  -> mister-agent
  -> transient MGL or development RBF
  -> /dev/MiSTer_cmd
  -> conventional MiSTer/Main-compatible process
  -> FPGA
```

That path launches real games from the existing catalogue on the designated
MiSTer Pi. It also loads arbitrary development RBFs, using a target reboot to
recover when an incompatible RBF causes Main to exit.

The experimental native work is currently embedded in branches of the large
`Main_MiSTer` repository. The lifecycle ABI archive called
`libmister-runtime.a`, the actual native hardware implementation called
`libfogcast-native-personality.a`, and a `fogcast-runtime` daemon are separate
pieces. Production platform construction is deliberately disabled and the
only profiles are synthetic/private Mega Drive and SNES profiles. No native
hardware acceptance has been recorded. A second obsolete
`mister_runtime_linux_v2` scaffold also remains in that history.

The most complete native lineage, rather than the older detached checkout, is
the source to extract when implementation begins. Nothing in this document
changes the currently working conventional path.

## Success definition

“Primary” has two meanings:

1. The native runtime becomes the primary development direction immediately.
   New MiSTer-control work targets it rather than extending conventional Main.
2. The native image becomes the primary everyday image only after it passes
   the complete hardware acceptance matrix in this document.

Parity means preserving behaviour FogCast actually uses now:

- real catalogue launches for all 16 current FPGA system mappings;
- the current host API and target-selection model;
- input, visible video/capture, stop, and immediate relaunch;
- current save behaviour for systems that use it;
- accurate status and recovery after an agent restart;
- arbitrary development-RBF loading and return to idle; and
- clean boot and explicit reboot on the designated Pi.

Features that are not working capabilities today, including full target audio,
do not block parity. They are separate future features and must not expand
this migration.

## Chosen architecture

### Repository and process ownership

`FogCast` owns:

- the browser UI and public host API;
- catalogue and NAS lookup;
- target selection, content transfer, and target cache;
- host session state and network input leases; and
- `mister-agent`, the only network service used to control the Pi.

The new standalone `libmister-runtime` repository owns:

- the public runtime library and lifecycle API;
- the native MiSTer hardware implementation;
- FPGA programming and bridge setup;
- core protocols and system profiles;
- media attachment and save handling;
- the hardware side of input and video/capture; and
- the `mister-runtime` daemon.

The repository and library retain the name `libmister-runtime`. The executable
is `mister-runtime`. Historic active names such as
`libfogcast-native-personality`, `fogcast-runtime`, numbered stages, and POC
labels are removed during extraction. Internal modules may remain separate,
but they form one runtime implementation with one public lifecycle boundary.

`Main_MiSTer` remains the conventional known-good implementation and a
reference for required hardware behaviour. It is not the home for new native
runtime development.

`misteross` owns reproducible FPGA experiments and RBF production and
comparison. It supplies artifacts; it does not control target sessions or
become a runtime dependency.

Exactly one process owns the FPGA in each system image. The legacy image runs
conventional Main. The native image runs `mister-runtime` and never starts
Main.

### Why a daemon boundary

`mister-agent` communicates with `mister-runtime` through a small local IPC
interface. The Go agent does not link the C++ implementation through cgo, and
the runtime does not absorb the agent's HTTP, cache, transfer, or host-session
responsibilities.

This boundary keeps both sides independently buildable and testable, contains
a runtime failure to the Pi, and avoids language-ABI coupling in the target
agent. It also gives an agent that restarts a direct way to observe the
current hardware owner.

Rejected alternatives are:

- **Direct cgo linkage:** fewer processes, but a tighter build, crash, and ABI
  coupling between the Go agent and C++ runtime.
- **Runtime absorbs the agent:** one target process, but it mixes hardware
  control with HTTP, caching, transfers, and FogCast session concerns. This
  recreates the oversized component the project is replacing.

## Local runtime protocol

The daemon listens only on `/run/mister-runtime.sock`. Each Unix-socket
connection carries one newline-terminated JSON request and one
newline-terminated JSON response, then closes. It is local, not
network-accessible, and has no authentication or TLS layer.

Every request includes `protocol: 1` and one of these operations:

- `status`
- `launch`
- `load_development_rbf`
- `stop`

A normal launch contains:

- a stable `system` identifier;
- the absolute local path to the selected RBF;
- local media paths labelled by semantic role, such as `cartridge`; and
- optional profile settings explicitly declared by that system profile.

Unknown request fields, media roles, and profile settings are rejected. There
is no unrestricted option map for passing core-specific commands through the
runtime boundary.

The request contains paths only. RBF and game bytes are staged before the
runtime call and never cross the local control socket.

The response always includes:

- `ok`;
- the observable runtime `state`;
- the active execution type when applicable; and
- either the confirmed system/core identity or a direct error code and
  message.

The initial error codes are `invalid_request`, `unsupported_protocol`,
`unknown_system`, `missing_media`, `busy`, `program_failed`, `core_mismatch`,
`io_failed`, and `idle_failed`. `idle_failed` is paired with state
`reboot_required`; the other launch errors report the state reached after the
single cleanup attempt.

The runtime states are:

- `idle`
- `starting`
- `running_game`
- `running_development`
- `reboot_required`

One hardware-changing operation may run at a time. A concurrent operation
returns `busy`; the daemon does not queue it.

The protocol is versioned only to reject an incompatible agent clearly. It
does not negotiate features or support multiple concurrent protocol versions
in the first implementation.

## Profiles and data ownership

FogCast continues to translate catalogue platforms and library locations into
a stable system ID and to select/stage the relevant files. It does not know
how a core consumes those files.

Runtime profiles own the low-level details currently mixed into FogCast's
system table or conventional Main behaviour:

- acceptable core identity;
- media roles and required/optional media;
- MiSTer file indices and core handshakes;
- bridge and reset sequence;
- input mapping requirements;
- save protocol; and
- any system-specific prerequisites.

The migration removes those low-level values from FogCast after the
corresponding runtime profile is live. Catalogue aliases, system display
names, and library lookup remain in FogCast. There must not be two canonical
copies of a profile table.

The existing FogCast input transport and lease mechanism remains. On the Pi,
the runtime consumes the resulting Linux input device and maps it through the
active profile. Video streaming remains a FogCast concern; the runtime
initializes the native framebuffer/capture path as part of launch, and the
existing capture process reads the target framebuffer without sending private
commands to conventional Main.

## Launch and stop lifecycle

### Normal game launch

```text
Browser
  -> FogCast public launch API
  -> catalogue resolution and target staging
  -> mister-agent target API
  -> local launch request
  -> mister-runtime profile and hardware implementation
  -> FPGA core, attached media, input, and framebuffer
```

The detailed sequence is:

1. FogCast resolves a game and system and stages cache misses on the Pi.
2. `mister-agent` sends the stable system ID and local file paths to the
   runtime.
3. The runtime validates the complete request before changing the FPGA.
4. The runtime programs the FPGA, performs profile-specific initialization,
   attaches media, initializes input/video, and confirms the active core.
5. Only after confirmation does the agent publish the active execution.

If dispatch may have reached the runtime but the response is lost, the agent
does not guess. It calls `status` and reconciles from the daemon's observable
state. This preserves the existing distinction between failure before and
after dispatch without introducing a second ownership database.

### Stop

`stop` flushes the current save state when applicable, tears down the active
profile, loads the packaged idle RBF, and reports `idle`. A second normal
launch may follow immediately.

The native image initially packages the already tested Menu RBF under the
role-based image path/name `idle.rbf`. It is only an FPGA idle implementation:
conventional Main is not started and it is not the user interface. A future
minimal idle RBF can replace it without changing the runtime API.

### Development RBF

The existing FogCast development endpoint remains the external API. The host
streams the bounded RBF to `mister-agent`; the agent atomically stages it and
sends its local path through `load_development_rbf`.

The runtime programs it without inventing a game, system, catalogue entry,
manifest, or standard-core profile. Status reports
`running_development`. Stop reloads the idle RBF directly. If that cannot be
done safely, the runtime reports `reboot_required` and FogCast may use its
existing explicit reboot operation.

## Failure and restart behaviour

Failure handling is intentionally finite:

1. Validate paths, profile, required media, and basic file properties before
   touching the FPGA.
2. Once programming begins, a failed launch gets one deterministic cleanup
   attempt: load the idle RBF.
3. If cleanup succeeds, report the original launch error with state `idle`.
4. If cleanup fails, report `reboot_required`.

There are no automatic launch retries, automatic fallback to Main, watchdog
orchestration, or preserved sessions across reboot.

Restart rules are similarly direct:

- A restarted `mister-agent` asks `status` and reconstructs its view from the
  running daemon.
- A restarted `mister-runtime` claims the hardware and deliberately loads the
  idle RBF. It does not try to preserve the game that was running when it
  exited.
- A reboot starts the native image cleanly in `idle`.

Logs contain the requested system, selected core, lifecycle phase, and direct
failure. The host exposes an unknown or `reboot_required` state rather than
silently replacing ownership.

## Image and rollback model

Migration uses two explicit images:

- **Legacy image:** the current conventional Main/FogCast path and known-good
  rollback.
- **Native image:** starts `mister-runtime`, waits for its local socket, then
  starts `mister-agent`. It never starts conventional Main.

The choice is made by selecting or reflashing the image. There is no runtime
autodetection or automatic switch between implementations.

In the native image, `/usr/sbin/mister-runtime` and
`/usr/sbin/mister-agent` are the authoritative executables installed by the
image build. A FAT-side configuration file may remain for device-specific
settings, but FAT-side duplicate binaries are not executed. The image records
the exact runtime and agent revisions it contains.

The legacy image is left unchanged throughout migration. After parity, its
last known-good source and image are tagged and documented as rollback. The
conventional backend can then be removed from the active FogCast development
tree; Git history and the tag retain it.

## Migration milestones

This document is the umbrella architecture, not permission to implement every
milestone on one long-lived branch. Each milestone is a reviewable delivery
unit with its own implementation plan, focused tests, documentation update,
and exit evidence. Work starts with the canonical repository milestone and
does not begin a later hardware profile merely because its software scaffold
can be written in parallel.

### 1. Canonical repository

- Create `DeanoC/libmister-runtime` from the most complete experimental native
  lineage.
- Preserve relevant file history where practical, without importing the
  upstream Main tree.
- Combine the lifecycle boundary and native hardware implementation under the
  new names.
- Delete the excluded `mister_runtime_linux_v2` scaffold and other superseded
  active-tree copies.
- Add accurate root documentation and a software-only test baseline.

### 2. Bootable native baseline

- Add the native-image build target and image-owned binaries.
- Boot runtime before agent.
- Prove version/status, idle loading, stop, and reboot on the designated Pi.
- Keep the legacy image unchanged and independently bootable.

### 3. Mega Drive vertical slice

- Launch a real Mega Drive catalogue game through FogCast's existing public
  API.
- Prove native FPGA programming, ROM attachment, visible capture, playable
  input, stop to idle, and an immediate second launch.
- Treat this as the first real hardware milestone; daemon startup alone is not
  success.

### 4. Development RBF

- Load an arbitrary RBF through the existing FogCast development endpoint.
- Prove accurate development status, stop to idle, and a subsequent normal
  game launch.

### 5. SNES profile

- Add SNES as the second real profile.
- Use the differences from Mega Drive to remove assumptions from the shared
  profile interface rather than adding a second ad hoc path.

### 6. Full current-system parity

Add and hardware-test the remaining profiles in small batches. The final
matrix contains:

1. Mega Drive
2. SNES
3. NES
4. Game Boy
5. Game Boy Color
6. Game Boy Advance
7. Master System
8. Game Gear
9. PC Engine
10. Atari 2600
11. Atari 7800
12. ColecoVision
13. Atari Lynx
14. WonderSwan
15. WonderSwan Color
16. Intellivision

Shared-core pairs are separate acceptance rows because their media indices or
prerequisites differ.

### 7. Primary-image switch

Switch the dedicated kit's everyday image to native only when the full matrix
passes. Update FogCast's canonical architecture documents in the same change.
Retain the tagged legacy image as the explicit rollback.

## Verification

### Software checks

The runtime repository tests:

- request parsing and incompatible protocol rejection;
- lifecycle state transitions and single-operation exclusion;
- complete profile validation;
- semantic media role to core file-index mapping;
- launch failure before and after programming;
- idle cleanup and `reboot_required` reporting;
- agent restart/status reconstruction; and
- native target cross-build and final link closure.

A small fake hardware backend is sufficient for lifecycle and profile tests.
It must not become a second production implementation or substitute for the
Pi tests.

FogCast retains contract tests around its existing public and target APIs.
Native integration tests verify that the agent calls only the local runtime
interface and does not fall back to `/dev/MiSTer_cmd`.

### Physical-Pi checks

Every hardware result records the date, FogCast commit, runtime commit, image
identity, system, and result. Evidence says “software-tested” or
“hardware-tested” accurately.

The Mega Drive gate requires:

1. boot native image and observe `idle`;
2. launch a real catalogue title through the host API;
3. confirm core identity and visible, non-corrupt capture;
4. confirm input reaches the game;
5. stop and observe the idle image;
6. launch again without rebooting; and
7. reboot explicitly and observe a new boot identity and `idle`.

The development gate requires:

1. upload and run a real development RBF through the host API;
2. observe `running_development` with no fake game/system identity;
3. stop to `idle` or receive the explicit `reboot_required` result;
4. if reboot was required, prove the new boot identity and idle state; and
5. launch a normal catalogue game afterward.

The parity gate repeats the relevant launch, visible-output, input, stop, and
relaunch checks for at least one real catalogue game in each of the 16 rows.
Save behaviour is exercised on representative systems that currently use it.

## Documentation policy

The new runtime repository begins with:

- `README.md`: purpose, current status, supported-system table, and shortest
  build/run path;
- `ARCHITECTURE.md`: library, daemon, profiles, hardware boundary, and launch
  flow;
- `DEVELOPMENT.md`: local build, native-image installation, Pi exercise, and
  rollback; and
- `AGENTS.md`: the same clarity and disposable-hardware principles already
  established for FogCast.

`AGENTS.md` specifically requires:

- documentation and support status to change with the code path they
  describe;
- one canonical execution path and one canonical profile table;
- names that describe current use, with no active POC or numbered-stage
  names;
- superseded implementations to be deleted from the active tree;
- physical-hardware claims to cite physical-hardware evidence; and
- no security, failover, coordinator, or recovery framework without a
  demonstrated need and explicit approval.

The support matrix has one canonical copy and is linked rather than copied
into conflicting documents. Hardware evidence remains concise: commits/image,
system, result, and useful failure detail. It is engineering evidence, not a
compliance system.

## Explicit non-goals

- Reimplementing the conventional MiSTer Menu or local game browser.
- Running Main and the native runtime at the same time.
- Automatic fallback between legacy and native implementations.
- Moving FogCast's catalogue, NAS access, cache, or HTTP API into the runtime.
- Preserving a running game across daemon restart or system reboot.
- Solving target audio before it becomes a current working-path requirement.
- Making `misteross` a prerequisite for the first native game launch.
- Retaining obsolete implementations in the active tree for reassurance.
