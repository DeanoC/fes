# FES

Fogger Entertainment System is the starting point for developing and building
FogCast and the native MiSTer system. Start here, select a component to work on,
then return here to verify the assembled combination.

Agents: read [AGENTS.md](AGENTS.md), then the
[development guide](docs/development.md) and the selected component's guidance.
[Component boundaries](docs/component-boundaries.md) describe ownership.

## Documentation

- [Getting started](docs/getting-started.md): setup, build choices, running the host and common failures.
- [Project map](docs/project-map.md): what runs where, component responsibilities and directory layout.
- [Agent workflow](docs/agent-workflow.md): assignments, worktrees, integration and handoffs.
- [Documentation index](docs/README.md): current guides, validation records and historical plans.

## Components

| Component | Responsibility |
| --- | --- |
| FES | Compatible revisions, integration checks, final system assembly and evidence |
| [FogCast](sources/FogCast) | Browser/tenfoot UI, game library, host services and network-facing target agent |
| [libmister-runtime](sources/libmister-runtime) | Local daemon/library controlling FPGA, media, input and hardware lifecycle |
| [misteross](sources/misteross) | FPGA development, compilation and exported RBF bundles |
| [mister-packages](sources/mister-packages) | Shared hardware/system definitions and generated C++/Go |

Main_MiSTer is an original implementation and test reference, not a production
dependency. The target agent talks to the runtime's local socket; only the
runtime controls hardware. Quartus runs on the build machine, not during launch.

## Start

Use Linux amd64 with Git, GNU Make, Python 3.11+, Go and Docker. Go selects the
version in the selected FogCast `go.mod` (currently 1.26.5). All component
repositories must be accessible with your Git credentials.

```sh
git clone --recurse-submodules git@github.com:DeanoC/fes.git
cd fes
make test
make check
make host
```

For an existing checkout, inspect local changes before running
`git submodule update --init --recursive`; preserve component work first.
`make check` verifies clean pinned sources, the runtime lock, package YAML,
seven generated consumers and copied Mega Drive/SNES source pins. It needs Go,
not Docker or Quartus. `make host` builds the Linux CLI and browser API server. Run `make doctor`
when preparing for container/image builds. See [getting started](docs/getting-started.md)
for Git authentication and a minimal host configuration.

## Build the integrated native system

For routine native development, run `make dev`. It keeps the compiler and base
packages and publishes a structurally checked diagnostic image under
`out/native-integration-dev/development/`. A compatible existing clean build can
seed that cache; otherwise the first run builds the base once. See
[the incremental workflow](docs/development.md#incremental-native-image).

For clean integration and release verification:

```sh
QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0 make build
make verify
```

The default `native-integration-dev` selects FogCast `1adc7c3`, runtime
`4398f41`, misteross `7912a3e` and mister-packages `a29f631` through submodule
gitlinks. It includes the merged native development-RBF loader and uses
FogCast's source-bundle interface without editing its input lock.

A fresh FPGA build requires Quartus Lite 17.0.2. A same-revision cached bundle
can be reused after payload and pinned-recipe validation. Downloads are checked
against component locks; image compilation runs twice in independent build
roots with networking disabled. Allow several GB for tools and outputs.

```text
out/native-integration-dev/
  fogcast-api                 Linux amd64 server with browser UI
  fogcast                     Linux amd64 CLI
  linux.img                   ARMv7 target root filesystem
  megadrive.rbf                selected source-built core
  megadrive-rbf.toml           FPGA build provenance
  megadrive.selection.toml     child's installed-core selection record
  inputs.json                 selected sources, profile, Go and parent recipe
  host.json / image.json      input fingerprints and output hashes
  reproducibility.txt         independent image hashes
  manifest.tsv                installed files
  library-report.tsv          target library closure
  verification.json           explicit verification results
  qemu-smoke.log              packaging smoke evidence
```

Run the host with an explicit local configuration:

```sh
out/native-integration-dev/fogcast-api --config /absolute/path/config.toml --listen 127.0.0.1:8787
```

The native product supports Mega Drive games and the existing MiSTer-compatible
development-RBF lifecycle. It does not promise generalized/custom RBF ABIs or
useful video/input from arbitrary development cores. The SDL tenfoot client
remains a component build, not a parent output. This produces a root filesystem,
not yet a complete bootable SD-card layout. Build and verify do not deploy or
contact the kit. QEMU checks packaging, not FPGA behavior; exact-image hardware
acceptance is separate. See [integration evidence](docs/integration-validation.md).

## Commands and profiles

| Command | Result |
| --- | --- |
| `make test` | Parent regression tests; no components or external services required |
| `make check` | Current component and shared-definition consistency |
| `make doctor` | Selected pins/lock, Linux architecture, Go and container availability |
| `make host` | Linux API server and CLI |
| `make dev` | Incremental diagnostic native image with retained compiler/base |
| `make build` | Host and two-pass native image, reusing matching checked outputs |
| `make image` | Image only with structural checks |
| `make verify` | Require host/image receipts, verify image, two-pass hashes and QEMU packaging |
| `make rebuild` | Force host, Quartus and both image passes |

| Profile | Selected source combination |
| --- | --- |
| `native-integration-dev` (default) | Current gitlinks, source-built core, package consistency |
| `native-dev` | Original FogCast `cd85971` / runtime `443b603`, upstream core |
| `native-source-dev` | Same original pair, source-built core and historical lock overlay |

Use `PROFILE=native-dev` or `PROFILE=native-source-dev` to rebuild a historical
combination. Their exact source overrides live in the profile files; those
commits must remain available in component history. Historical profiles lack the
new native development loader. They retain independent output directories and
Docker volumes. Historical hash comparison is informational; see
[original validation and provenance](docs/historical-parent-validation.md) and
[source-profile hardware evidence](docs/source-build-validation.md).

## Development and integration

Keep `sources/` clean at the indexed gitlinks. Components compile from disposable
standalone clones under `out/work/`, which preserve each child's Git identity.
Use separate worktrees under `out/dev/` for component changes. Builds in one
checkout are serialized and each profile uses a distinct Docker output volume.
Ambient Go and image-selection overrides are normalized.

The integrator updates component gitlinks together, runs `make check`, and
records the tested combination. See [development and handoffs](docs/development.md)
for exact commands and how several agents can work independently.

CI runs parent tests, consistency and host compilation. Configure the repository
secret `FES_COMPONENTS_TOKEN` with read access to FES and all four private
components; the default Actions token cannot read sibling private repositories.
CI deliberately fails with an actionable message when this credential is absent.
Quartus, full image builds and physical checks run on the development machine.

Next system milestone: package [Pong and SNES](docs/multi-system-development.md)
into the normal image and verify the assembled three-system artifact. The source
implementations and diagnostic switching checks are complete. Whole-system image
assembly migration and the native bootable media layout remain separate work.
