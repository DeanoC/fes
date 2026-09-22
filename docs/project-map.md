# Project map

FES means Fogger Entertainment System. It is the parent integration repository:
its first-party modules share one Git repository, with common build and validation
commands. Source paths remain under `sources/` while module ownership stays explicit.

## What each part does

| Module | Runs where? | Owns | Start reading |
| --- | --- | --- | --- |
| FES | Development/build machine | Component selection, compatibility checks, build orchestration and integration evidence | [README](../README.md), [build entrypoint](../scripts/build.py) |
| FogCast | Host machine and target | UI, library, host APIs, transfers and the network-facing target agent | [README](../sources/FogCast/README.md), [architecture](../sources/FogCast/docs/ARCHITECTURE.md) |
| libmister-runtime | MiSTer ARM CPU | Local hardware lifecycle, FPGA programming, media/input delivery and return to idle | [README](../sources/libmister-runtime/README.md), [architecture](../sources/libmister-runtime/ARCHITECTURE.md) |
| misteross | Development/build machine | FPGA sources, simulation, compilation and RBF bundle export. OSS place-and-route experiments and described cores are different jobs | [README](../sources/misteross/README.md), [OSS experiments](../sources/misteross/docs/oss-pnr.md), [cores](../sources/misteross/docs/cores.md) |
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

FPGA products run only as described FES packages through local protocol 2.
Explicit contained raw-RBF diagnostics remain available for hardware bring-up.
Main_MiSTer is a comparison reference, not a supported launch backend.

## How a build fits together

mister-packages definitions generate checked-in consumers used by the runtime
and FPGA source, and describe upstream core sources. FogCast consumes the
runtime-advertised ABI registry without a second static Go allowlist. misteross
produces the FES package set. FES snapshots committed module sources, checks that their
definitions and external-artifact policy agree, then invokes the FES `image/` recipe to assemble
the agent, runtime, libraries and ordered package set. FogCast remains an input
(agent, kit and extra-core selector) via `FOGCAST_DIR`.

## Directory guide

| Path | Purpose | Edit it? |
| --- | --- | --- |
| `AGENTS.md` | Common working instructions for every agent | For parent workflow changes |
| `Makefile` | User-facing parent commands | For parent command changes |
| `profiles/` | Active native integration build settings | Integrator-owned |
| `scripts/inputs.py` | Committed module selection and generated runtime assembly inputs | Parent implementation |
| `scripts/consistency.py` | Generated consumer, fixture and external source-pin checks | Parent implementation |
| `image/` | Native Buildroot, container and SD/rootfs assembly | Parent image recipe |
| `scripts/build.py`, `scripts/native_dev.py` | Clean and incremental orchestration | Parent implementation |
| `platform/` | FES-owned appliance boot selector (`fes-boot`) and Linux boot helpers | Parent boot source; built against the selected FogCast `appliance` module |
| `scripts/appliance.py`, `scripts/appliance_media.py`, `scripts/platform.py` | Versioned releases, bootstrap and provisioned appliance card files | Parent assembly; see [the release guide](appliance-releases.md) |
| `scripts/bundle.py`, `scripts/environment.py` | Bundle validation and build environment | Parent implementation |
| `tests/` | Parent regression tests | With relevant parent behavior changes |
| `sources/` | Tracked first-party modules in the FES repository | Edit the owning module in a task worktree |
| `out/dev/<task>/` | FES task worktree containing every module | Yes, within the assigned scope |
| `out/work/` | Builder-managed committed FES snapshots; commands run in module subdirectories | Do not edit |
| `out/<profile>/` | Published host/clean-image artifacts and receipts | Generated outputs |
| `out/native-integration-dev/development/` | Separate incremental diagnostic outputs | Generated outputs |
| `scripts/test_changed.py` | Affected software tests and consumer closure | FES development tooling |
| `scripts/generate.py` | Mapped contract generation and fixture copies | FES generation tooling |
| `config/source-imports.toml` | Original URLs, gitlinks, imported commits and trees | Historical import provenance |
| `docs/` | Current guides and dated validation evidence | Keep current instructions distinct from history |

`out/` is ignored by Git. Shared compiler and functional-artifact caches default
to the primary FES checkout's `out/cache` (override with absolute `FES_CACHE_ROOT`).
Buildroot's large compiled outputs also live in Docker volumes, so deleting a
published image does not remove its compiler cache.
Do not use blanket cleanup such as deleting all of `out/`: `out/dev/` can contain
uncommitted worker source changes. Inspect the specific generated output or
recorded cache volume before removing it.

## Terms used in the guides

- **Module:** a first-party source directory tracked in the FES commit.
- **Pin / gitlink:** historical internal component selection, or an external
  dependency selection; imported modules no longer need internal gitlinks.
- **Profile:** the TOML selection of the active native integration build settings.
- **Worktree:** another FES checkout with its own branch, index and module files,
  sharing the FES Git history.
- **RBF:** the binary FPGA configuration loaded by the runtime.
- **Bundle:** an RBF plus provenance describing its source and build recipe.
- **Core package:** a closed format-2 manifest and RBF with a content-derived immutable package ID.
- **Selection record:** the normalized description of the core installed in an
  image, including its origin and hash.
- **Snapshot:** a disposable checkout of the real selected FES commit, retaining
  its Git identity and module-relative paths for existing build commands.
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
