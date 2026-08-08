# FogCast Portable Target Runtime Design

## Status

Approved direction, written for review on 2026-08-08. This document defines
the long-term architecture after POC6. It does not reopen or broaden POC6's
accepted claims, and it does not authorize implementation or target mutation.

**2026-08-08 qualification:** [ADR 0002](../../adr/0002-disposable-local-development-target.md)
records the operator's standing authorization and rollback/security
qualification for one privately designated disposable local development kit.
That qualification does not change this design for production or any other
target.

## Decision summary

FogCast will evolve from a host application controlling a mostly stock
`Main_MiSTer` process into a portable appliance system with a stable host/target
contract and a library-shaped native target runtime.

The project will maintain a long-lived GPLv3 `Main_MiSTer` fork. Hardware-facing
behavior needed by FPGA cores will be extracted incrementally into
`libmister-runtime`. A thin compatibility executable and a headless FogCast
runtime will use the same library. Linux remains the first supported platform,
but Linux APIs, file paths, helper processes, and device nodes must be contained
behind platform interfaces rather than treated as FogCast architecture.

Overlord is the destination system generator for board, SoC, FPGA, software,
register-map, toolchain, and image composition. Migration is staged: Overlord
must first reproduce a narrow DE10-Nano/Cyclone V Linux vertical slice before
it becomes required for the retained POC6 path or replaces the current
Buildroot workflow.

## Context

POC1 deliberately kept `Main_MiSTer` as a compatibility runtime responsible
for FPGA programming, core communication, controller handling, video, audio,
storage services, and core lifecycle. That was the correct proof-of-concept
decision. POC6 then exposed the long-term cost of treating it as an opaque
external application: FogCast required an external FFmpeg process, Linux
framebuffer and command devices, a disposable native presentation hook,
multiple supervisors, SSH-based development, and operational reconciliation.

Those are acceptable testbed mechanisms, not the intended appliance
architecture. They must not become permanent host-visible contracts.

The existing DeanoC ecosystem changes the replacement calculus:

- `DeanoC/overlord` already models connected hardware and software systems,
  including boards, CPUs, buses, registers, libraries, programs, generated
  source, toolchains, and FPGA outputs.
- `DeanoC/ikuy_std_resources` contains platform, OS-service, memory, multicore,
  virtual-file, FAT, USB/HID, graphics, and bare-metal Zynq resources.
- Historical `ikuy*`, `ltgnt*`, `al2o3*`, and other repositories contain
  reusable libraries and design experience accumulated over many years.

The project should reuse that work selectively. It must not make the complete
historical library estate an implicit dependency or allow multiple copies of a
library to remain equally authoritative.

## Goals

1. Make the MiSTer target behave as a deterministic, single-purpose FogCast
   appliance rather than a collection of loosely coordinated Linux tools.
2. Preserve the proven MiSTer FPGA compatibility behavior while refactoring it
   behind explicit lifecycle and platform interfaces.
3. Keep the FogCast host and network protocol independent of
   `Main_MiSTer`, Linux device paths, codecs, and target process layout.
4. Make Linux a supported platform implementation, not a cross-cutting
   assumption.
5. Establish a credible path to other FPGA/SoC targets and, where justified,
   a future non-Linux target.
6. Use Overlord eventually to describe and generate the complete HW/SW target
   composition.
7. Preserve reproducibility, rollback, evidence boundaries, and safe hardware
   operation throughout migration.

## Non-goals

- A clean-room rewrite of `Main_MiSTer`.
- Immediate removal of Linux, Buildroot, Go, or the current target agent.
- Immediate replacement of the accepted POC6 media path.
- Pulling every historical DeanoC library into the dependency graph.
- Redesigning MiSTer FPGA cores or the accepted RTP/H.264 protocol without a
  separate evidence-backed decision.
- Claiming controller symmetry, physical latency, bare-metal readiness, or
  multi-hardware support before each has its own acceptance evidence.

## Architectural principles

### Stable contracts point inward

The public host/target session protocol is stable across target
implementations. Target internals may change from Go sidecar plus stock Main,
to Go sidecar plus native runtime, to a single native appliance process without
requiring the host catalog or UI to understand that composition.

### Compatibility before cleanup

Every extraction from `Main_MiSTer` must preserve an upstream-style executable
that uses the same extracted code as the headless runtime. The traditional
executable is the behavioral comparison point until the headless path passes
the same physical gates.

### Platform dependencies are explicit

Filesystem, clocks, threads, memory mapping, interrupts, USB, networking,
logging, storage, and presentation enter the runtime through explicit platform
interfaces. Linux implementations may use POSIX, sysfs, device nodes, or
kernel drivers; portable components may not.

### One authoritative owner per resource

At runtime, one component owns each FPGA generation, presentation surface,
audio route, input route, content object, process, socket, and credential.
At source level, one repository or catalog entry is canonical for each reused
library. Compatibility copies are pinned inputs, not competing authorities.

### Appliance does not mean one process

An appliance boots directly into one supported user workflow, has deterministic
lifecycle and recovery, and does not depend on an operator shell. It may still
use process isolation where that reduces crash impact or migration risk. A
single-process target is a later optimization, not an architectural goal by
itself.

### Evidence survives refactoring

Software tests do not substitute for physical FPGA, HDMI, controller, audio,
save, or latency evidence. Historical POC results remain historical. A rebuilt
or refactored artifact receives new hashes and must pass the gates appropriate
to the behavior it can affect.

## System architecture

```text
FogCast host (Go)
  catalog / content / UI / public API / orchestration / evidence
                         |
                versioned FogCast protocol
                         |
FogCast target appliance
  session lifecycle / cache / health / recovery / telemetry
        |                 |                  |
        |          media + input engines    |
        |                                    |
  libmister-runtime ---------------- platform services
  FPGA/core/user_io/save/video/audio        |
                                             |
                         MiSTer Linux implementation
                         future board or bare-metal implementation
```

### FogCast host

The host remains Go-based for the foreseeable future and owns:

- library and catalog management;
- NAS and source preparation;
- public API, UI, and user intent;
- execution policy and target selection;
- session composition across host and target;
- target discovery and, later, multi-target routing;
- evidence collection and privacy-safe reports.

The host must not depend on MGL files, `/dev/MiSTer_cmd`, `/dev/fb0`, FFmpeg
command lines, Linux process identifiers, or Main-specific temporary files.

### FogCast target appliance

The target appliance owns:

- authenticated target/session endpoints;
- target-local content cache and storage policy;
- core, media, input, and presentation lifecycle composition;
- health, startup reconciliation, crash recovery, and bounded shutdown;
- target-private credentials and transport identity;
- privacy-safe telemetry.

The existing Go `mister-agent` remains the compatibility control plane during
migration. It communicates with the native runtime through versioned local IPC.
Direct CGo/C++ linkage is not the first integration step because it couples the
Go process to the native runtime's crashes, toolchain, and global state.

During that transition, the Go agent owns authentication, network request
admission, content-cache operations, and session-to-process supervision. The
native runtime is the sole component allowed to commit target-local hardware
mode changes. At the destination, the host continues to own user intent and
cross-machine policy while the native runtime owns all target-local execution,
resource arbitration, observed state, reconciliation, and recovery. The Go
agent must never maintain a second competing hardware state machine.

Whether the Go agent remains as a permanent process or is absorbed by the
native appliance is a later measured decision. The public protocol must make
either composition possible.

### Target modes and resource arbitration

The native runtime exposes an explicit mutually exclusive target mode:

- `idle`: healthy and ready, with no game or cast generation active;
- `fpga_native`: an FPGA core generation owns FPGA bridges, native video/audio,
  core input, and its save lifecycle;
- `host_cast`: the media generation owns presentation, cast audio, and the
  controller route to the host emulator;
- `updating`: a bounded authorized target update owns affected artifacts and
  refuses launches;
- `recovering`: startup or failure reconciliation is determining ownership and
  refuses new work;
- `failed`: ownership or hardware state cannot be made safe automatically and
  requires an explicit recovery operation.

Every launch creates a fresh `(session, generation)` identity. Mode changes are
crash-consistent handoffs controlled by the native hardware coordinator:

1. Reservation and preparation may allocate or validate only non-owning,
   non-exclusive resources. They grant no hardware lease and do not expose the
   candidate generation as active.
2. Before quiescing, the coordinator durably records `recovering`, the old
   `(session, generation)` explicitly as the quiescing/release owner, and the
   candidate intent. The candidate still has no lease, and new admission is
   refused. The coordinator then quiesces the old generation and receives
   release of every exclusive hardware lease, or recovery proves it dead and
   resets each affected platform resource.
3. After every release/reset completes, the coordinator durably commits a true
   no-exclusive-owner checkpoint and remains in `recovering`.
4. From that checkpoint, one atomic durable transfer grants and records the new
   `(session, generation, mode)` and its complete exclusive lease set. Only
   after that commit may the new owner activate hardware or become externally
   visible. The compatibility Main process, headless runtime, media presenter,
   and any other hardware client never overlap as owners of an exclusive
   resource.

Failure before the `recovering` intent commit leaves the old generation active.
Failure after that intent but before the no-owner checkpoint causes startup to
see the old generation as quiescing/release owner and complete reconciliation;
it never resumes the old owner blindly. Failure after the no-owner checkpoint
but before transfer remains safely `recovering`. Failure after transfer unwinds
the new generation and enters `recovering`, or `failed` if safety cannot be
proven; it never silently returns control to the stale owner. Bounded
post-transfer cleanup may remove only non-exclusive residue and cannot delay or
qualify the exclusive ownership transfer.

The coordinator is the arbiter; the component holding a granted lease is the
resource owner. A lease names `(session, generation, mode, resource)` and is
revoked only by the coordinator after the owner confirms release or after
recovery proves the owner is dead and the platform resource has been reset.

| Resource | Arbiter | Owner while leased | Process-death rule |
| --- | --- | --- | --- |
| FPGA programming, bridges, HPS registers, core protocol, saves | Native hardware coordinator | `libmister-runtime` | Enter `recovering`; reset/reconcile hardware before another lease |
| Presentation surface and native video mode | Native hardware coordinator | `libmister-runtime` in `fpga_native`; media engine in `host_cast` | Revoke only after surface close or platform reset; never allow two mapped owners |
| Audio sink and route | Native hardware coordinator | `libmister-runtime` in `fpga_native`; media engine in `host_cast` | Mute, drain with a deadline, reset, then grant replacement lease |
| Physical controller devices | Native hardware coordinator | Input engine | Input engine releases all pressed state; coordinator selects exactly one route |
| Decoding, media buffers, RTP/control sockets | Native hardware coordinator | Media engine | Close transport, discard buffers, release presentation/audio leases, reconcile generation |
| Content-cache objects | Go agent during transition; native target storage service at destination | Cache service, with an active-generation read lease | Active objects cannot be evicted or replaced; orphaned leases are cleared only after native reconciliation |
| Target credentials | Authentication service | Credential store | Never transferred to runtime components; revoke/rotate through an authorized operation |

The host creates the public session identifier and a fresh generation for every
launch. The Go agent authenticates and admits that requested identity, but the
native runtime atomically persists and commits the authoritative target-local
mode/lease record. A successful mode-changing IPC reply is sent only after the
durable transfer and new-owner activation complete. An ambiguous or failed
reply is reconciled by querying the native runtime, never by assuming success,
idle, or restoration of a stale generation. Network status is a projection of
native state.

Stop and mode-changing operations carry the same identity and are rejected when
stale. Cache eviction or artifact replacement is rejected while the native
runtime reports an active read or execution lease. On either process restart,
the Go agent first queries the native record and the runtime validates it
against processes, devices, and hardware before new admission. The exact wire
encoding and durable-record format remain subjects of the focused IPC spec;
this authority and commit model do not.

### `libmister-runtime`

The long-lived Main fork will extract the hardware-compatibility behavior that
cores require:

- FPGA programming, reset, and bridge control;
- `user_io` and core-specific HPS services;
- ROM, disk-image, and virtual-storage delivery;
- controller and keyboard translation;
- core configuration and required OSD protocol;
- save and persistent-state lifecycle;
- video, scaler, HDMI, and audio configuration;
- authoritative core and failure state.

The first API is a narrow C ABI even if the implementation remains C/C++:

```c
typedef struct MisterRuntime MisterRuntime;
typedef struct MisterPlatform MisterPlatform;
typedef struct MisterLaunch MisterLaunch;
typedef struct MisterStatus MisterStatus;

MisterRuntime *MisterRuntime_Create(const MisterPlatform *platform);
bool MisterRuntime_Start(MisterRuntime *runtime);
void MisterRuntime_Tick(MisterRuntime *runtime);
bool MisterRuntime_Load(MisterRuntime *runtime, const MisterLaunch *launch);
MisterStatus MisterRuntime_Status(const MisterRuntime *runtime);
void MisterRuntime_Stop(MisterRuntime *runtime);
void MisterRuntime_Destroy(MisterRuntime *runtime);
```

The concrete types, ownership rules, error model, and ABI versioning require a
focused implementation spec before code is changed. The API above fixes the
shape of the boundary, not its final field layout.

Menu browsing, recents, updater behavior, shell scripts, and legacy UI assets
are excluded unless a compatibility test proves a core-facing dependency.

### Compatibility executable

The fork continues to build a traditional `Main_MiSTer` executable. Its
`main()` becomes a thin platform setup and polling wrapper over
`libmister-runtime`. This executable must remain usable as the rollback and
behavioral comparison path while the headless FogCast runtime matures.

### Media engine

Media transport and decode are separate from `libmister-runtime`. The runtime
arbitrates presentation ownership and FPGA-mode transitions; a media engine
owns authenticated receive, decode, frame conversion, audio decode, and frame
presentation.

The long-term appliance must not require spawning the `ffmpeg` command-line
tool. The Linux implementation may initially link a narrowly configured codec
library. Other platforms may provide software or hardware decoder backends.
This replacement is not part of the first extraction milestone.

### Input engine

Input is routed by session policy rather than hardwired to the FPGA or host:

- FPGA-native session: local target input flows to `libmister-runtime`.
- Host-emulated session: local target input flows over the session transport to
  the host emulator injector.
- Remote-control session: host-originated input flows to the selected target
  execution backend.

Press/release state, disconnect cleanup, exclusive ownership, and bounded
stuck-input recovery are required invariants. POC6 does not provide this
symmetry; its deferral remains open.

### Platform services

The platform boundary supplies only capabilities needed by portable runtime
code. Candidate groups are:

- time, timers, sleep, synchronization, and task execution;
- physical/virtual memory and register access;
- storage, virtual files, and durable atomic replacement;
- USB host, HID discovery, and input events;
- network datagrams, streams, and secure credential access;
- display surfaces, presentation modes, and audio sinks;
- logging, metrics, entropy, and watchdog/reboot hooks.

Existing ikuy/al2o3 APIs are candidates, not automatically frozen contracts.
Each is reviewed, assigned one canonical source, tested with the current
toolchain, and admitted only when required by a vertical slice.

### Overlord

Overlord is the intended canonical description and generation layer for:

- DE10-Nano board resources;
- Cyclone V SoCFPGA CPUs, buses, register banks, memory, and peripherals;
- FPGA/HPS connections and generated register definitions;
- compiler and linker configuration;
- software-library and program dependency closure;
- Linux and later bare-metal platform selection;
- artifact manifests and complete target composition.

Overlord does not become mandatory for the retained POC6 testbed until it can
reproduce a narrowly scoped MiSTer/Linux build and that output passes existing
software and HIL gates. Its historical hard-coded paths, mixed resource
generations, and incomplete outputs are modernized only as required by that
slice.

For the migration baseline, `ikuy_std_resources` is the canonical curated
catalog. Standalone historical library repositories are treated as upstream
sources or history until an explicit promotion decision makes one canonical.

## Repository and dependency strategy

1. Maintain a `DeanoC/Main_MiSTer` fork with the upstream repository configured
   as a read-only remote.
2. Keep behavior-preserving extraction commits small and separable from
   FogCast-specific behavior.
3. Record the upstream base commit and fork patch series in machine-readable
   provenance.
4. Keep Overlord itself in `DeanoC/overlord` and the curated catalog in
   `DeanoC/ikuy_std_resources` until a separate catalog split is justified.
5. Keep host protocol and migration adapters in this FogCast repository.
6. Do not create a separate target-runtime repository until the extracted
   runtime boundary exists and ownership would be clearer than keeping the
   initial headless executable alongside the fork.
7. Pin every cross-repository dependency by immutable commit and record its
   license and artifact hash.

Before a library enters a shipped target image, review and record license
compatibility, corresponding-source obligations, notices, patent concerns where
applicable, and the exact configured feature set. Recording a license name
alone is insufficient.

Upstream Main updates are integrated deliberately at milestone boundaries.
Every integration records the previous and new upstream commits, resolves the
patch series in reviewable groups, and reruns affected compatibility gates.

## Migration sequence

### Disposable local development target qualification

For the exact privately designated MiSTer Pi kit covered by
[ADR 0002](../../adr/0002-disposable-local-development-target.md), project
access, deployment, reboot, software/image/configuration/credential
replacement, and wipe/rebuild have standing operator authorization. The kit is
disposable project development hardware: preserving its prior physical state,
production hardening, and the POC6 rollback lock/runbook are not prerequisites
to those operations. Reports mark that rollback prerequisite `Not required per
ADR 0002`.

The exception cannot be inferred from target type, hostname, IP address, or
discovery. Before a destructive action, tooling resolves the designation from
operator-controlled private configuration and verifies the exact target
identity. Secret values and private identifiers stay out of source control and
reports.

This qualification does not relax secret handling, lifecycle/resource
ownership and reconciliation, reproducibility, provenance, HIL
classification, artifact hashing, or compatibility comparison. Existing POC6
hashes are historical evidence, not claims about the kit's current state. A
valid known-good comparator is still required for any behavior-equivalence
claim, even though preserving the kit's previous state is not. Other and
production targets retain the authorization, rollback, and security rules
below.

### Stage A: Describe and reproduce the current target

- Add the minimum DE10-Nano and Cyclone V resource descriptions to Overlord.
- Generate the memory map, register definitions, toolchain configuration, and
  software dependency closure required by the current Linux Main build.
- Build an upstream-style Main artifact without intentional behavior changes.
- Compare runtime behavior on the dedicated target; binary identity is not
  required where the upstream build embeds dates or other unstable metadata.

Exit condition: the generated description and build reproduce current FPGA
launch behavior and, where applicable, retain the established rollback path.
The ADR 0002 kit need not preserve its prior physical state, but a valid
known-good comparator with recorded provenance remains required to claim
behavior equivalence.

Before Stage A mutates a target for which the prerequisite applies, add a tracked
`build/poc6-rollback.lock.toml` and `docs/runbooks/poc6-rollback.md`. The lock
identifies by immutable commit or hash the target image, kernel, Main binary,
agent, decode bridge, presentation hook, supervisor configuration, cores, and
all non-secret configuration inputs required to restore POC6. Large/private
artifacts remain in the authorized artifact store; the tracked lock contains
only safe identifiers and hashes. The runbook records artifact retrieval,
hash verification, bounded restoration, health checks, and the exact physical
acceptance observations required after restoration.

### Stage B: Extract `libmister-runtime`

- Introduce explicit create/start/tick/load/status/stop/destroy lifecycle.
- Move global initialization behind an owned runtime context incrementally.
- Keep the traditional executable as a wrapper over the same library.
- Add host-side fakes for platform services and deterministic lifecycle tests.

Exit condition: traditional and headless wrappers pass repeated launch, core
transition, input, video, audio, save, shutdown, and failure-recovery gates on
the same target.

### Stage C: Compose the FogCast target runtime

- Add versioned local IPC between the compatibility Go agent and native
  runtime.
- Move authoritative core state and reconciliation into the native runtime.
- Preserve the public FogCast target protocol and existing host behavior.
- Generate fresh per-launch identity and enforce conditioned stop across the
  local boundary.

Exit condition: the current host can use either the retained adapter or native
runtime without API-visible semantic differences, and rollback is verified.

### Stage D: Internalize media and input

- Replace the external FFmpeg process with a linked decoder backend.
- Give the native runtime an explicit presentation-arbitration interface.
- Complete host-emulator input symmetry with physical state-change evidence.
- Add automatic orphan detection and recovery after ungraceful process death.

Exit condition: the appliance no longer requires the POC6 presentation hook,
external decoder command, or manual bridge reconciliation for normal supported
failure modes.

### Stage E: Evaluate non-Linux portability

- Implement a second platform backend or a constrained bare-metal spike only
  after the Linux boundary is proven.
- Measure missing kernel services, driver cost, boot time, memory, latency, and
  maintainability.
- Retain Linux unless evidence shows that removing it produces a material
  product benefit worth owning USB, networking, filesystems, codecs, and device
  support directly.

Non-Linux remains an architectural option, not a scheduled promise.

## Safety, security, and recovery invariants

1. Production images contain no SSH server or interactive development service
   unless explicitly selected by a separate development profile.
2. Development access never becomes a runtime dependency.
3. Credentials are never placed in process arguments, shared writable paths,
   logs, generated manifests, or source control.
4. Every start operation has an explicit owner and generation; every stop is
   conditioned on that identity. Every launch uses a fresh generation.
5. Startup performs reconciliation before accepting a new session.
6. Every teardown step has an independent finite deadline and preserves
   retryable ownership when cleanup fails.
7. The compatibility executable and known-good target image remain recoverable
   until replacement gates pass.
8. No agent or automation performs a target mutation, reboot, deployment,
   credential change, or rollback without explicit operator authorization.
   The standing authorization for the exact disposable local development kit
   is defined only by ADR 0002; it cannot be inferred for another target.
9. The retained POC1B production-named image is historical POC evidence, not a
   destination security baseline: it disables Dropbear but still contains a
   shared root password. A future production profile must remove interactive
   login credentials and explicitly define its recovery channel.
10. Automated target update remains disabled until a focused update design
    defines signed manifests, an immutable trust root, key rotation and
    revocation, downgrade policy, verification before installation, atomic or
    A/B activation, power-loss recovery, and automatic rollback. Every
    installed artifact is checked against the signed manifest; transport
    authentication and a hash without a trusted signature are not sufficient.

## Verification strategy

### Local and simulated

- Unit-test lifecycle, ownership, status, rollback, and platform contracts.
- Compile portable runtime code against a host fake platform.
- Compile the MiSTer platform for the pinned ARMv7 target.
- Test generated register maps and memory topology against locked known-good
  values.
- Run race, sanitizers, static analysis, formatting, dependency, provenance,
  and reproducibility checks supplied by each project.
- Produce two clean builds in independent output trees with the same pinned
  source, toolchain, environment, locale, timezone, and `SOURCE_DATE_EPOCH`.
  Outputs must be byte-identical. Any unavoidable nondeterminism is listed by
  artifact, byte range or section, cause, normalization rule, and verifier;
  an unexplained whole-binary hash difference fails the gate.
- Compare every generated memory map, register definition, compiler setting,
  dependency closure, and image manifest against a reviewed locked baseline;
  unexpected additions, removals, address changes, or privilege changes fail.

### Physical hardware

- FPGA core load and repeated transition.
- Recognizable game output over HDMI.
- Audio presence and clean transition.
- Controller press/release and disconnect cleanup.
- Save creation, shutdown, and reload.
- Normal stop, startup rollback, runtime crash, agent crash, power interruption,
  and recovery.
- Stock/compatibility rollback.

Each migration-stage roadmap fixes its run counts before execution. The
minimum behavior-preservation gate is ten consecutive normal launch/stop
cycles spanning at least the two previously accepted FPGA core families, plus
one observed run for each specified startup rollback, runtime death, agent
death, reboot reconciliation, and rollback path. Traditional and headless
wrappers are tested against the same content, controllers, display, audio
fixture, configuration, and observation checklist. Zero unexplained orphaned
processes, leases, device owners, cache mutations, or save differences are
permitted.

Hardware reports name the exact host, target image, Main fork, Overlord,
resource-catalog, runtime, core, and configuration commits or hashes. Unobserved
behavior remains untested.

## AI development operating model

The root agent remains accountable for scope, file ownership, integration,
user communication, and final evidence. Sub-agents are focused workers, not
independent project owners.

### Sol: architecture steward

Use Sol for architectural decisions, cross-repository interfaces, lifecycle or
ownership changes, portability boundaries, security invariants, and blockers
that would force Luna to change an approved design.

Sol is read-only by default. It produces decision options, trade-offs,
invariants, and review findings. When explicitly assigned implementation, Sol
receives a disjoint file set and the same test/review requirements as any
worker.

Prefer the current `gpt-5.6-sol` model when available. If unavailable, use the
strongest reasoning model available and record the fallback in the handoff.

### Luna: normal development worker

Use Luna for ordinary scoped implementation, tests, refactors within approved
boundaries, build integration, and documentation updates. Luna follows the
current spec and plan, works test-first where behavior changes, and escalates
rather than inventing a new architecture when blocked.

Prefer the current `gpt-5.6-luna` model when available. If unavailable, use a
balanced implementation model and record the fallback. Model choice never
overrides repository instructions or review gates.

### Independent reviewer

Every implementation milestone receives an independent read-only review from
an agent that did not author the change. Use a high-reasoning model for
architecture, concurrency, security, protocol, and hardware-boundary changes;
a balanced reviewer is sufficient for contained mechanical work.

The reviewer checks the approved spec, diff, tests, failure paths, privacy,
provenance, documentation, and rollback. Critical and important findings are
resolved before integration.

The review record names the reviewer role, model and any fallback, base commit,
head commit or exact uncommitted diff, governing decision, verification
evidence inspected, and disposition of every Critical or Important finding.
The author cannot mark their own unresolved finding accepted.

### Evidence/HIL verifier

Hardware acceptance uses a separate verifier context where practical. The
verifier distinguishes commands run, machine-observed counters, operator
observations, and inferences. It cannot authorize hardware mutation and does
not receive secrets in prompts or reports.

### Dispatch rules

- Delegate only bounded work with explicit inputs, outputs, file ownership,
  and verification commands.
- Keep nesting shallow and prefer a flat team controlled by the root agent.
- Never assign overlapping writable file sets concurrently.
- Use read-only agents for reconnaissance, architecture, test planning, and
  review.
- Luna handles normal work; escalate to Sol only for an architectural decision,
  a repeated blocker, or a cross-boundary change.
- Small one-file mechanical edits do not require a sub-agent unless risk or
  review requirements justify one.
- Sub-agent output is advisory until the root agent validates and integrates
  it.

### Worktrees, commits, and handoff

- Substantial or parallel work uses Git worktrees.
- One agent owns each writable worktree and file set.
- Agents do not commit, push, open pull requests, deploy, or mutate hardware
  unless the user explicitly authorizes that action.
- Durable decisions live in repository docs, not only chat history.
- Every handoff states the base commit, branch/worktree, files changed, tests
  run, artifacts produced, unresolved risks, and the next safe action.

## Documentation hierarchy

Long-lived documentation has distinct responsibilities:

- `IDEA.md`: product vision and long-term user outcome.
- `README.md`: current validated capabilities, entry points, and major limits.
- `docs/ARCHITECTURE.md`: current architectural direction and links to active
  decisions.
- this design: the rationale, boundaries, and staged target-runtime decision.
- top-level `AGENTS.md`: project-wide operating and multi-agent rules.
- scoped `AGENTS.md` files: only where a subtree has genuinely different build,
  safety, language, or hardware rules.
- POC roadmaps/results: immutable historical intent and evidence, amended only
  to link forward or correct factual errors.
- development guides/runbooks: current executable procedures and recovery.
- implementation plans: disposable execution sequencing, not architectural
  authority.

When documents disagree, current accepted evidence overrides plans; an
approved architecture decision overrides older forward-looking roadmap text;
and neither overrides the user's explicit current instruction.

## Open follow-up decisions

The following require focused designs before implementation reaches them:

1. Concrete C ABI types, threading model, allocation policy, and error model for
   `libmister-runtime`.
2. Versioned local IPC format and process composition.
3. Canonical source selection and modernization policy for each reused
   ikuy/al2o3/ltgnt library.
4. Cyclone V/DE10-Nano resource coverage and generated-artifact comparison.
5. Linked decoder choice, license/build profile, and presentation interface.
6. Whether the native target runtime eventually absorbs the Go agent.
7. Criteria that would justify creating a separate target-runtime repository.
8. Evidence threshold for attempting a non-Linux platform.

These are explicit design inputs, not placeholders in the approved direction.
