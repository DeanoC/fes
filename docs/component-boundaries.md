# FES component boundaries

FES means Fogger Entertainment System. This document distinguishes the
whole system from the narrower native build currently implemented in this
repository. All first-party source modules share the FES Git tree; the ownership
direction below governs module work. The current integration
profile consumes the component graph; native image assembly lives in FES `image/`.

## Ownership

| Module or reference | Responsibility | Output or contract |
| --- | --- | --- |
| FES | Integrate compatible module changes, check agreement, build and assemble the complete system, record evidence | Profiles, committed source snapshots, assembled images and manifests |
| mister-packages | Describe boards, SoCs, registers, system protocols and upstream core sources; generate consumer definitions | YAML, emitter, generated C++/Go definitions and reports |
| misteross | Build FPGA artifacts and validate their build provenance; maintain FPGA development tools and experiments | RBF bundles, simulations and compiler recipes |
| libmister-runtime | Execute the hardware lifecycle on the target: program FPGA, configure hardware, load media, handle input, stop and return to idle | Native library and daemon, runtime protocol |
| FogCast | Own the user-facing application, game library, launch selection, host services and target agent | `ui/tenfoot`, `ui/kitlauncher`, host binaries, agent and public APIs |
| Main_MiSTer | Original implementation and comparison/test reference | Reference behavior and optional test fixtures; not an FES production dependency |

An upstream core-source pin describes what to fetch; misteross owns fetching
and compiling it. FES selects a committed source snapshot and which resulting
artifacts belong in a system image. These are different responsibilities.

Within FogCast, the application UI is explicitly under
`sources/FogCast/ui/tenfoot` and `sources/FogCast/ui/kitlauncher`. Their Go
package names remain `tenfoot` and `kitlauncher` for compatibility. Host
services and public API ownership stays with FogCast, while target HTTP/cache
coordination remains in the agent and physical lifecycle remains in
libmister-runtime. The UI does not own target handlers, runtime lifecycle,
image assembly, or FPGA builds.

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
if clients need independent development or release cycles. Whole-system image
ownership lives in FES `image/`; the shared appliance module and FES-owned
boot executable have also landed. Target contracts and shared host-client
logic are separated with dependency guards. Keep the target implementation
in FogCast until an independent release/consumer need or measured build/test
coupling justifies extraction;
see the [current refactor decision](fes-structure.md).

## Integrated source set

The tracked modules contain the host, runtime, shared package definitions and
FPGA product sources in one FES commit. Original repository URLs and imported
histories are recorded in [config/source-imports.toml](../config/source-imports.toml).
The current profile remains package-only and installs the ordered closed
`fes.pong`, `fes.zx81` and `fes.coleco` set through the HIP/nextpnr route.

`make check` validates committed module selection and cleanliness, FES native
artifact policy, package YAML, generated consumers and shared fixtures. It
compares temporary generated outputs without editing consumers. `make generate`
updates mapped consumers and copies in the task worktree; `make check-generated`
checks that mapping without writing. Definitions remain authoritative in
mister-packages; the runtime owns serializer fixtures and physical semantics.
FogCast consumes the negotiated runtime ABI registry rather than a second unused
Go allowlist.

`native-integration-dev` uses FES's package-only interface and publishes the
ordered per-core selection records for `fes.pong`, `fes.zx81` and `fes.coleco`
plus its sealed `core-packages/` directory. Systems whose nextpnr route is not
implemented yet are checked explicitly with Quartus; that check is not an image
production route.

## Keep, combine, split and add

Keep the four first-party modules in the FES repository. Keep the package schema
and small emitter together, and keep the runtime library and daemon together. Keep the target
agent with FogCast while they share an API and release lifecycle. Do not split
individual cores or introduce more repositories without an independent need.

Combine duplicated definitions under mister-packages authority. Retain checked-in
generated consumers so standalone target compilation does not require Go.
Changes to package definitions and affected consumers land in one compatible
FES change; `make check` detects drift. External compiler/toolchain repositories
remain independently locked dependencies.

Whole-system Buildroot/image configuration and final SD-card assembly live in
FES `image/`. Component compilation remains component-owned. FogCast keeps the
agent, kit and extra-core selector; FES owns native artifact policy and invokes
`make -C image FOGCAST_DIR=...`. Do not restore FogCast `target-image-native`
as a second builder. The recipe list and operator path are in
[image assembly ownership](image-assembly.md).

Use a small artifact interface: agent, runtime, core bundle and package-derived
definitions with component identities, hashes and installation destinations.
The present bundle/selection manifests already supply the FPGA part. Extend the
existing evidence rather than inventing a package framework or coordinator.
Main_MiSTer and frozen Overlord resources remain references or optional tests.

## Work through the parent

All agents start with [AGENTS.md](../AGENTS.md) and
[the development guide](development.md), then work in the owning module within a FES task worktree.
The integrator owns shared-contract reconciliation, artifact policy and system builds.
Component workers own disjoint implementation scopes and return a short handoff.
This does not require a fixed agent team for every change.

## Next integration milestone

The current profile selects the ordered FES package set and provides package,
image and QEMU verification commands. Their availability does not establish that
the imported layout has passed them. It does not inherit NES, four-core, or physical
hardware acceptance from the historical [Mega Drive, Pong, SNES and NES
catalog](multi-system-development.md). The selected NES image's exact
assembled-image video and session-lifecycle acceptance remains tied to its dated
historical artifact; later image or core revisions need their own evidence.
Exact assembled-artifact results must remain distinct from the earlier hardware
diagnostics.
Current development and assembly capabilities:

1. Use the incremental native development path for component integration; retain
   clean reproducibility checks at stabilized milestones. Extend its cache
   granularity only when measurements justify it.
2. Bootable native FES media and versioned appliance assembly are implemented;
   use the [media guide](bootable-media.md) and
   [appliance guide](appliance-releases.md) for their distinct validation gates.
3. Extend supported systems/ABIs or package tenfoot only as separately scoped work.
   The selected FES ZX81 package uses `fes.simple-computer`; remaining
   Quartus bring-up, nextpnr/mistral and physical acceptance evidence are
   separate from the package-only assembly. See [FES ZX81](fes-zx81.md).

Completed migrations and remaining decisions are recorded in
[FES structure](fes-structure.md).
Earlier checks and limitations remain in the dated validation documents;
they describe their selected artifacts rather than the current profile.
