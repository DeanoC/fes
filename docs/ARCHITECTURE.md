# FogCast architecture

## Status and authority

**Status:** Approved current architectural direction as of 2026-08-08.

This document is the canonical current architecture for FogCast. It summarizes
the approved [portable target runtime design](superpowers/specs/2026-08-08-fogcast-portable-target-runtime-design.md);
the design retains the rationale, detailed constraints, and open focused
decisions. [ADR 0001](adr/0001-portable-target-runtime.md) records the
portable-runtime decision, [ADR 0002](adr/0002-disposable-local-development-target.md)
records the disposable local development target qualification, and the
[active roadmap](ROADMAP.md) records migration gates.

Accepted evidence overrides plans. Historical POC documents remain authoritative
for their stated evidence and scope, but do not define future architecture.
Documentation does not create operator authority by itself; ADR 0002 records
the operator's standing authorization for its exact private kit. No other
deployment, rollback, reboot, or target mutation is authorized here.

## Current baseline: accepted POC6 testbed

POC6 accepted a real-game host-to-MiSTer HDMI path with authenticated session
ownership and managed lifecycle. Its evidence is intentionally narrower than a
product claim: it does not establish controller symmetry, physical
glass-to-glass latency, a portable runtime, or a production appliance.

The retained POC6 testbed uses external FFmpeg decode, Linux framebuffer and
command devices, SSH-based development, and a disposable native
`Main_MiSTer` presentation hook. These are testbed mechanisms, not public
contracts or destination architecture. The accepted details, limits, hashes,
and deferred gates are in [POC6 results](POC6-RESULTS.md); operation and
reconciliation are in the retained [POC6 development guide](POC6-DEVELOPMENT.md).
The hashes in POC6 results identify historical accepted artifacts; they do not
describe the current software, image, configuration, credentials, or physical
state of any development device.

## Disposable local development target

The operator may privately designate one exact local MiSTer Pi kit as a
disposable project development target under
[ADR 0002](adr/0002-disposable-local-development-target.md). That designation
provides standing authorization for project access, deployment, reboot,
software/image/configuration/credential replacement, and wipe/rebuild without
per-operation confirmation. Preserving the kit's prior physical state,
production hardening, and the POC6 rollback lock/runbook are not required.

The designation cannot be inferred from hardware type, hostname, IP address,
or discovery. Before a destructive action, resolve it through
operator-controlled private configuration and verify the exact target identity.
If either step fails, stop. No secret value or private target identifier belongs
in source control or documentation.

This qualification does not relax secret handling, lifecycle/resource
ownership and reconciliation, reproducibility, provenance, HIL classification,
artifact hashes, or compatibility comparison, and it does not enable unattended
production updates. All other and production targets retain the general
authorization, rollback, and security rules.

## Destination boundaries

FogCast evolves into a portable target appliance with a stable versioned
host/target session protocol and a library-shaped native target runtime.

- The **host** owns catalog and content preparation, public API and UI, user
  intent, cross-machine policy, target selection, session composition, and
  privacy-safe evidence collection.
- The **target appliance** owns authenticated target admission, local cache and
  storage policy, lifecycle composition, health, reconciliation, recovery,
  target-private identity, and privacy-safe telemetry.
- `libmister-runtime` owns hardware compatibility behavior: FPGA programming,
  bridges, `user_io`, core services, storage delivery, controller translation,
  OSD, saves, native video/audio configuration, and authoritative core/failure
  state.
- A maintained GPLv3 `Main_MiSTer` fork remains the compatibility source. Its
  traditional executable becomes a thin wrapper over `libmister-runtime` and
  remains a behavioral comparison and rollback path while the headless runtime
  matures.

FogCast host code must not depend on MGL files, `/dev/MiSTer_cmd`, `/dev/fb0`,
FFmpeg command lines, Linux process identifiers, or Main-specific temporary
files. An appliance may use multiple processes; deterministic ownership and
recovery matter more than process count.

## Contracts and portability

The public host/target session protocol is versioned and stable across target
implementations. It carries user-visible session semantics, not target process
layout, codecs, `Main_MiSTer`, or Linux details.

During migration, the Go `mister-agent` remains the compatibility control plane
for authentication, request admission, cache operations, and supervision. It
uses **versioned private local IPC** to communicate with the native runtime;
an IPC reply represents the native runtime's durable target-local commit.
Network status is only a projection of that native state. The Go agent must
not maintain a competing hardware state machine.

`libmister-runtime` exposes a narrow, versioned **C ABI** to its wrappers. Its
concrete C types, allocation, threading, error model, and ABI details require a
focused design before implementation. Media transport/decode remains separate:
the runtime arbitrates target modes and presentation ownership, while a media
engine owns receive, decode, conversion, audio decode, and presentation.

Linux is the first supported platform implementation, not a cross-cutting
architecture. Portable runtime code reaches filesystems, clocks, threads,
memory mapping, interrupts, USB, networking, logging, storage, display, and
audio only through explicit platform interfaces. Linux-specific APIs, paths,
helpers, and device nodes remain behind those interfaces.

Overlord is the destination composition layer for the board, SoC, FPGA,
software, register map, toolchain, and image. It is not required for the
retained POC6 testbed until a narrow DE10-Nano/Cyclone V Linux slice reproduces
the necessary build and passes its existing software and HIL gates.

## Target modes and resource ownership

The native hardware coordinator exposes exactly one target mode:

- `idle`: healthy, ready, and without an active generation;
- `fpga_native`: one FPGA core generation owns bridges, native video/audio,
  core input, and saves;
- `host_cast`: one media generation owns presentation, cast audio, and the
  controller route to the host emulator;
- `updating`: an authorized update owns its affected artifacts and refuses
  launches;
- `recovering`: reconciliation owns admission while it makes state safe; or
- `failed`: automatic recovery could not make ownership or hardware safe.

The retained FPGA-development migration path has one narrow reboot
reconciliation exception. A canonical previous-boot `normal_main` /
`compat_main` record, or candidate-bearing `recovery_required` residue that
still names `compat_main` after a failed development handoff, may be rebound
only by committing a current-boot `recovering_intent` after read-only preflight
proves the current kernel boot, the protected terminal journal, and current
live compatibility Main readiness:
exactly one Main, a canonical command FIFO with mode `0600` or `0644`, an
operating FPGA manager, and stable canonical `MENU`. The boot-local ready
record is not part of this successor-boot proof because `/run` is cleared by
reboot and production publishes a new ready record only after ownership has
already been rebound.
The reclaim preserves the proven active compatibility-Main tuple, allocates a
fresh development candidate, and clears the prior failed-run fence at the
first durable intent boundary. Ordinary hardware admission, every same-boot
conflict (including `recovery_required`), previous-boot `fpgadev_active` and
`recovering_intent`, recovery owned by anything other than `compat_main`,
recovery without the retained development candidate, absent or invalid owner
state, missing or mismatched journal or live-Main proof, and stale fault
operations remain fenced. This migration exception does not make the host a
second hardware coordinator or generalize stale-boot admission.

Every launch has a fresh `(session, generation)` identity. The coordinator
alone grants and revokes leases for FPGA/HPS mappings, presentation, audio,
input, saves, media sockets/buffers, and their lifecycle transitions. A lease
names its session, generation, mode, and resource; it is revoked only after an
owner releases it or recovery proves the owner is dead and resets the platform.
The compatibility `Main_MiSTer` executable and headless runtime never run as
simultaneous hardware owners.

Source ownership follows the same rule: every reused library has one canonical
repository or catalog entry. `ikuy_std_resources` is the curated migration
catalog; other historical libraries remain upstream inputs or history until an
explicit promotion decision. Cross-repository dependencies are pinned by
immutable commit, license record, configured feature set, and artifact hash.

## Invariants

1. Starts and stops are conditioned on a fresh session/generation identity;
   stale operations cannot affect a replacement.
2. Mode transitions are crash-consistent exclusive-ownership handoffs.
   Reservation/preparation may allocate or validate only non-owning,
   non-exclusive resources and grants no hardware lease. The coordinator
   durably records `recovering` with the old session/generation explicitly as
   quiescing/release owner plus the candidate intent, refuses new admission,
   then quiesces and obtains release of every old exclusive lease or proves the
   owner dead and resets the platform resource. After all release/reset work,
   it durably commits a true no-exclusive-owner checkpoint. Only then may one
   atomic durable transfer grant and record the new session, generation, mode,
   and complete exclusive lease set; only after that transfer may the new owner
   activate hardware or become externally visible. `Main_MiSTer`, the headless
   runtime, presentation engines, and other hardware clients never overlap as
   exclusive owners.
3. A failure before the `recovering` intent commit leaves the old generation
   active. A failure after that intent but before the no-owner checkpoint makes
   startup see the old generation as quiescing/release owner and complete
   reconciliation; it never resumes that owner blindly. A failure after the
   no-owner checkpoint but before transfer stays `recovering`. A failure after
   transfer unwinds the new generation and enters `recovering`, or `failed` if
   safety cannot be proven; it never silently gives a stale generation control.
   Bounded post-transfer cleanup is permitted only for non-exclusive residue.
4. Startup reconciles native state against processes, devices, and hardware
   before admitting new work. An ambiguous local IPC result is queried and
   reconciled, never assumed successful, idle, or restored to a stale owner. A
   successful mode-changing reply is exposed only after the durable transfer
   and activation of the recorded owner.
5. Teardown operations have independent finite deadlines. Pressed input state,
   active cache objects, transports, surfaces, and audio routes are released or
   reset before replacement ownership is granted.
6. Credentials never enter process arguments, shared writable paths, logs,
   generated manifests, or source control. A production image contains no SSH
   server or interactive development service; either is allowed only in an
   explicitly selected separate development profile and is never a runtime
   dependency.
7. For every non-exempt target, compatibility execution and a known-good target
   image remain recoverable until replacement gates pass. No target mutation
   occurs without explicit operator authorization and the applicable rollback
   prerequisite in the roadmap. ADR 0002 is the only current exception and
   applies only to the exact privately designated disposable development kit.
8. Software tests do not replace physical evidence for FPGA, HDMI, audio,
   controller, save, recovery, or latency behavior. Rebuilt artifacts receive
   new hashes and pass the gates affected by their change.
9. Automated target update is disabled until a focused design defines signed
   manifests, an immutable trust root, key rotation and revocation, downgrade
   policy, verification before installation, atomic or A/B activation,
   power-loss recovery, and automatic rollback.

## Related authorities

- [Portable target runtime design](superpowers/specs/2026-08-08-fogcast-portable-target-runtime-design.md)
  — rationale, detailed architecture, and focused future decisions.
- [ADR 0001](adr/0001-portable-target-runtime.md) — accepted portable-runtime
  decision and superseded future guidance.
- [ADR 0002](adr/0002-disposable-local-development-target.md) — standing
  authorization and rollback/security qualification for the exact privately
  designated disposable local development kit.
- [Active migration roadmap](ROADMAP.md) — active gates and status vocabulary.
- [POC6 results](POC6-RESULTS.md) — accepted POC6 evidence boundary.
- [POC6 development guide](POC6-DEVELOPMENT.md) — retained-testbed operating
  and recovery procedure.
