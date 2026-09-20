# Project map

FES means Fogger Entertainment System. It is the parent integration repository:
it records which component versions work together and provides common build and
validation commands. Component source code stays in its own Git repository.

## What each part does

| Part | Runs where? | Owns | Start reading |
| --- | --- | --- | --- |
| FES | Development/build machine | Component selection, compatibility checks, build orchestration and integration evidence | [README](../README.md), [build entrypoint](../scripts/build.py) |
| FogCast | Host machine and target | UI, library, host APIs, transfers and the network-facing target agent | [README](../sources/FogCast/README.md), [architecture](../sources/FogCast/docs/ARCHITECTURE.md) |
| libmister-runtime | MiSTer ARM CPU | Local hardware lifecycle, FPGA programming, media/input delivery and return to idle | [README](../sources/libmister-runtime/README.md), [architecture](../sources/libmister-runtime/ARCHITECTURE.md) |
| misteross | Development/build machine | FPGA sources, simulation, compilation and RBF bundle export | [README](../sources/misteross/README.md), [build targets](../sources/misteross/Makefile) |
| mister-packages | Development/build machine | Shared board/register/system definitions and C++/Go generation | [README](../sources/mister-packages/README.md), [definitions](../sources/mister-packages/packages) |

FogCast contains several logical areas that agents can work on separately:

- Host/browser API: `cmd/fogcast-api` and `internal/hostapi`.
- Library and application services: `catalog`, `fogcast` and related packages.
- Tenfoot client: `cmd/fogcast-tenfoot` and its component guide.
- Target agent: `cmd/mister-agent`, `internal/agent` and `internal/httpapi`.
- Native runtime adapter: `internal/misterruntime`.
- Appliance updates: `cmd/fes-update`, `internal/applianceupdate`, and the
  public `appliance` schema/store module consumed by FES boot and the target agent.
- Stable boot selection and watchdog: FES `platform/cmd/fes-boot`,
  `platform/internal/applianceboot`, and `platform/internal/bootlinux`.

These are routing starting points, not permission to change every directory in
an area. Trace the relevant call path and choose a bounded scope first.

## A native game launch

```mermaid
flowchart LR
  UI[Browser or tenfoot UI] --> Host[FogCast host]
  Host -->|network requests and content| Agent[FogCast target agent]
  Agent -->|local socket| Runtime[mister-runtime]
  Runtime -->|program core and deliver media/input| FPGA[FPGA core]
```

The host chooses the game and its content. The target agent receives requests
and coordinates the session. The runtime performs the physical transitions.
The FPGA executes the loaded core. Quartus and misteross are build-time tools;
a game launch does not compile an FPGA design.

This is the FES native path. FogCast also retains a conventional Main-based
backend for its standalone use. Main_MiSTer is a reference for this parent
integration, not a production dependency of its native image.

## How a build fits together

mister-packages definitions generate checked-in consumers used by the runtime
and FPGA source, and describe upstream core sources. FogCast consumes the
runtime-advertised ABI registry without a second static Go allowlist. misteross
produces the FES package set. FES selects component commits, checks that their
definitions and locks agree, then invokes the FES `image/` recipe to assemble
the agent, runtime, libraries and ordered package set. FogCast remains an input
(agent, kit and extra-core selector) via `FOGCAST_DIR`.

## Directory guide

| Path | Purpose | Edit it? |
| --- | --- | --- |
| `AGENTS.md` | Common working instructions for every agent | For parent workflow changes |
| `Makefile` | User-facing parent commands | For parent command changes |
| `profiles/` | Active native integration build settings | Integrator-owned |
| `scripts/inputs.py` | Component pin and runtime-lock checks | Parent implementation |
| `scripts/consistency.py` | Package generation and source-pin checks | Parent implementation |
| `image/` | Native Buildroot, container and SD/rootfs assembly | Parent image recipe |
| `scripts/build.py`, `scripts/native_dev.py` | Clean and incremental orchestration | Parent implementation |
| `platform/` | FES-owned appliance boot selector (`fes-boot`) and Linux boot helpers | Parent boot source; built against the selected FogCast `appliance` module |
| `scripts/appliance.py`, `scripts/appliance_media.py`, `scripts/platform.py` | Versioned releases, bootstrap and provisioned appliance card files | Parent assembly; see [the release guide](appliance-releases.md) |
| `scripts/bundle.py`, `scripts/environment.py` | Bundle validation and build environment | Parent implementation |
| `tests/` | Parent regression tests | With relevant parent behavior changes |
| `sources/` | Clean submodule checkouts at integration pins | Move pins only through the integrator |
| `out/dev/<task>/<component>/` | Component worker worktrees | Yes, for that worker's assigned scope |
| `out/work/` | Builder-managed standalone component clones | No routine edits; builders consume them |
| `out/<profile>/` | Published host/clean-image artifacts and receipts | Generated outputs |
| `out/native-integration-dev/development/` | Separate incremental diagnostic outputs | Generated outputs |
| `docs/` | Current guides and dated validation evidence | Keep current instructions distinct from history |

`out/` is ignored by Git. Buildroot's large compiled outputs also live in Docker
volumes, so deleting a published image does not remove its compiler cache.
Do not use blanket cleanup such as deleting all of `out/`: `out/dev/` can contain
uncommitted worker source changes. Inspect the specific generated output or
recorded cache volume before removing it.

## Terms used in the guides

- **Pin / gitlink:** the exact component commit recorded by the parent Git index.
- **Profile:** the TOML selection of the active native integration build settings.
- **Worktree:** another checkout of a component, with its own branch and files,
  sharing Git history with the component repository.
- **RBF:** the binary FPGA configuration loaded by the runtime.
- **Bundle:** an RBF plus provenance describing its source and build recipe.
- **Core package:** a closed format-2 manifest and RBF with a content-derived immutable package ID.
- **Selection record:** the normalized description of the core installed in an
  image, including its origin and hash.
- **Receipt:** input fingerprint and output hashes used to detect stale or
  changed artifacts; it does not itself prove hardware behavior.
- **Cold two-pass build:** two independent image builds used to check matching
  results. An incremental build retains compiled work and has different evidence.
- **Artifact:** a named, OS/arch-specific output (host binary, agent, runtime,
  rootfs, core, ABI snapshot) identified by digest or commit, not by “whatever
  is in this checkout”. See [artifact identities](artifacts.md).

For boundary decisions and shared-definition changes, use
[component boundaries](component-boundaries.md). For practical assignments, use
[the agent workflow](agent-workflow.md).
