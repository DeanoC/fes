# FES component boundaries

FES means Fogger Entertainment System. This document distinguishes the
whole system from the narrower native build currently implemented in its
parent repository. The ownership direction below governs component work. The current integration
profile consumes the component graph; image-assembly migration remains separate.

## Ownership

| Repository | Responsibility | Output or contract |
| --- | --- | --- |
| fes | Select compatible component versions, check agreement, build and assemble the complete system, record integration evidence | Profiles, component pins, assembled images and manifests |
| mister-packages | Describe boards, SoCs, registers, system protocols and upstream core sources; generate consumer definitions | YAML, emitter, generated C++/Go definitions and reports |
| misteross | Build FPGA artifacts and validate their build provenance; maintain FPGA development tools and experiments | RBF bundles, simulations and compiler recipes |
| libmister-runtime | Execute the hardware lifecycle on the target: program FPGA, configure hardware, load media, handle input, stop and return to idle | Native library and daemon, runtime protocol |
| FogCast | Own the user-facing application, game library, launch selection, host services and target agent | UI, host binaries, agent and public APIs |
| Main_MiSTer | Original implementation and comparison/test reference | Reference behavior and optional test fixtures; not an FES production dependency |

An upstream core-source pin describes what to fetch; misteross owns fetching
and compiling it. FES selects component revisions and which resulting
artifacts belong in a system image. These are different responsibilities.

## FogCast agent, runtime and FPGA builder

The names obscure three distinct jobs:

- **FogCast target agent:** receives network requests from the host, manages
  transferred content and target-side request/session coordination, invokes
  the runtime, and reports results. It owns the network-facing API and
  bridges network input into the target's input path.
- **libmister-runtime:** contains both a C++ lifecycle library and the
  `mister-runtime` daemon. The daemon accepts local commands through
  `/run/mister-runtime.sock`. It owns actual hardware state: FPGA programming,
  reset ordering, media delivery, video setup, input delivery to the core,
  and return to idle. It has no game library or network transfer cache.
- **misteross:** runs on the development/build machine. It builds and tests
  FPGA designs and exports an RBF bundle. The resulting circuit executes on
  the FPGA; misteross itself is not a target daemon in the launch chain.

```mermaid
flowchart LR
  UI[FogCast UI] --> Host[FogCast host services]
  Host -->|network API and content| Agent[FogCast target agent]
  Agent -->|local socket commands| Runtime[mister-runtime daemon and library]
  Runtime -->|program and control| FPGA[FPGA core]
  Packages[mister-packages definitions] -.-> Host
  Packages -.-> Runtime
  Packages -.-> Builder[misteross build tools]
  Builder --> Bundle[RBF bundle]
  Bundle --> Assembly[FES image assembly]
  Assembly -.->|installed artifact| Runtime
```

This diagram expresses the intended component graph, including the package
integration checked by `make check`.

For a Sonic 2 launch, the host resolves the selected game and arranges its
content; the agent verifies/adopts the staged content and requests a native
launch; the runtime programs the selected RBF, performs the hardware reset
and transfer sequence, and confirms running state. A normal launch does not
invoke Quartus or misteross. Development loading follows the same separation:
builder produces a bundle, agent admits/transfers it, runtime loads it.

Keep transport/session state in the agent and physical lifecycle state in
the runtime. Some validation at both boundaries is useful, but reset recipes,
register values and hardware recovery decisions must not grow a second
implementation in the agent. The agent may request recovery and expose its
result; the runtime determines which physical transitions are possible.

Input similarly crosses a deliberate boundary: the agent accepts network
input and provides target input events; the runtime maps those events to the
running core's hardware protocol.

## Future FogCast review

FogCast contains UI, host services and target-agent software. Keep those as
three explicit architectural modules now; review whether separate repos are
useful after documenting their APIs, shared code, testing and release needs.
Do not equate a process boundary with a mandatory repository split.

The target agent is a candidate for later extraction if other host clients
need an independently released target service. UI extraction is useful only
if clients need independent development or release cycles. First remove
whole-system image ownership from the FogCast product boundary through the
separate migration below. Splitting repos before resolving that ownership
would distribute the existing coupling across more locations.

## Integrated source set

The gitlinks select matching merged implementations of native Mega Drive,
Pong, basic SNES, NES and renewable kit ownership. Exact revisions are recorded by
git; diagnostic artifact identities are in the multi-system and kit guides.
The default profile packages source-built Mega Drive, Pong, SNES and NES, each
with its own selection record. NES is software-supported and hardware-pending;
historical profiles remain Mega Drive-only.

`make check` validates source identity and cleanliness, the FogCast runtime
lock, package YAML, nine generated consumer files, and copied Mega Drive/SNES/NES
source pins. It regenerates to temporary files and never edits consumers. It
covers the selected DE10-Nano and four system definitions.

The old review's unmerged-consumer concern is resolved: FogCast main consumes
`generated.MegaDriveExpectedCore` and `generated.MegaDriveCartridgeIndex` through
PR126. No component source changes were needed for parent reconciliation.
The pinned mister-packages README still says “FogCast is not patched”; that
sentence is stale relative to the selected consumers and the regeneration check.

`native-integration-dev` uses FogCast's explicit source-bundle interface and
publishes `megadrive.selection.toml`. Only the historical `native-source-dev`
profile retains the lock overlay required by its older FogCast revision.
Historical profiles select exact earlier commits recorded in their TOML files;
current gitlinks remain the development starting point.

## Keep, combine, split and add

Keep all four component repositories. Keep the package schema and small emitter
together, and keep the runtime library and daemon together. Keep the target
agent with FogCast while they share an API and release lifecycle. Do not split
individual cores or introduce more repositories without an independent need.

Combine duplicated definitions under mister-packages authority. Retain checked-in
generated consumers so standalone target compilation does not require Go.
Changes to package definitions and affected consumers must land as one compatible
parent selection; `make check` detects drift.

Whole-system Buildroot/image configuration and final SD-card assembly should move
into FES in a separate migration. Component compilation remains component-owned.
Do not copy the scripts and leave two authoritative image builders: the selected
FogCast recipe remains authoritative until the migration is complete.

Use a small artifact interface: agent, runtime, core bundle and package-derived
definitions with component identities, hashes and installation destinations.
The present bundle/selection manifests already supply the FPGA part. Extend the
existing evidence rather than inventing a package framework or coordinator.
Main_MiSTer and frozen Overlord resources remain references or optional tests.

## Work through the parent

All agents start with [AGENTS.md](../AGENTS.md) and
[the development guide](development.md), then specialise in a component worktree.
The integrator owns parent pins, shared-contract reconciliation and system builds.
Component workers own disjoint implementation scopes and return a short handoff.
This does not require a fixed agent team for every change.

## Next integration milestone

The normal profile selects [Mega Drive, Pong, SNES and NES](multi-system-development.md).
NES remains software-supported and hardware-pending until an exact assembled
image is exercised on the designated kit.
Exact assembled-artifact results must remain distinct from the earlier hardware
diagnostics.
The following assembly milestones remain separate future work:

1. Use the incremental native development path for component integration; retain
   clean reproducibility checks at stabilized milestones. Extend its cache
   granularity only when measurements justify it.
2. Define the remaining assembly artifact inputs and move the image recipe once.
3. Produce a bootable native FES media layout without Main as a production input.
4. Extend supported systems/ABIs or package tenfoot only as separately scoped work.

Current checks and limitations are recorded in
[integration validation](integration-validation.md). Original image and hardware
records remain in the dated historical validation documents.
